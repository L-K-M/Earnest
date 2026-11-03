package pipeline

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/L-K-M/Earnest/internal/llm"
	"github.com/L-K-M/Earnest/internal/outbound"
)

type batchSpend struct {
	mu           sync.Mutex
	spent        int64
	reservations map[string]int64
	next         int
}

func (b *batchSpend) LLMReserveSpend(_ context.Context, _ string, _ time.Time, limit, estimate int64, _ time.Time) (string, int64, bool, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if limit > 0 && (b.spent >= limit || estimate > limit-b.spent) {
		return "", b.spent, false, nil
	}
	if b.reservations == nil {
		b.reservations = make(map[string]int64)
	}
	b.next++
	id := fmt.Sprint(b.next)
	b.reservations[id] = estimate
	b.spent += estimate
	return id, b.spent - estimate, true, nil
}

func (b *batchSpend) LLMSettleSpend(_ context.Context, _ string, id string, actual int64) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	estimate, ok := b.reservations[id]
	if !ok {
		return nil
	}
	delete(b.reservations, id)
	b.spent += actual - estimate
	return nil
}

func (b *batchSpend) LLMEnsureMonth(_ context.Context, _ string, _ time.Time, limit int64) (int64, int64, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.spent, limit, nil
}
func (b *batchSpend) LLMAddSpend(_ context.Context, _ string, _ time.Time, cost int64) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.spent += cost
	return nil
}

func TestOpenAIEditionBatchWireBudgetAndSchema(t *testing.T) {
	var mu sync.Mutex
	calls := []string{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		calls = append(calls, r.Method+" "+r.URL.Path)
		mu.Unlock()
		if r.Header.Get("Authorization") != "Bearer test-key" {
			t.Error("missing auth")
		}
		switch r.Method + " " + r.URL.Path {
		case "POST /v1/files":
			if err := r.ParseMultipartForm(1 << 20); err != nil {
				t.Error(err)
				http.Error(w, "multipart", 400)
				return
			}
			defer r.MultipartForm.RemoveAll()
			file, _, err := r.FormFile("file")
			if err != nil {
				t.Error(err)
				return
			}
			defer file.Close()
			raw, err := io.ReadAll(file)
			if err != nil {
				t.Error(err)
				return
			}
			var line map[string]any
			if err := json.Unmarshal(raw, &line); err != nil {
				t.Error(err)
				return
			}
			if strings.Contains(string(raw), `"tools"`) || !strings.Contains(string(raw), `"model":"test-model"`) || !strings.Contains(string(raw), `"max_completion_tokens":2048`) {
				t.Error("unsafe batch", string(raw))
			}
			fmt.Fprint(w, `{"id":"file-one"}`)
		case "POST /v1/batches":
			fmt.Fprint(w, `{"id":"batch-one"}`)
		case "GET /v1/batches/batch-one":
			fmt.Fprint(w, `{"status":"completed","output_file_id":"file-output"}`)
		case "GET /v1/files/file-output/content":
			text := `{"claims":[{"text":"Beacon.","citations":[{"entry_id":1,"content_hash":"` + strings.Repeat("a", 64) + `","passages":["Beacon."]}]}]}`
			_ = json.NewEncoder(w).Encode(map[string]any{"custom_id": "0", "response": map[string]any{"status_code": 200, "body": map[string]any{"choices": []any{map[string]any{"message": map[string]string{"content": text}}}}}})
		case "POST /v1/batches/batch-one/cancel":
			fmt.Fprint(w, `{}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	u, _ := url.Parse(server.URL)
	outboundClient, err := outbound.New(outbound.Config{AllowHosts: []string{u.Host}, Spacing: time.Nanosecond})
	if err != nil {
		t.Fatal(err)
	}
	cfg := llm.Config{Providers: map[string]llm.ProviderConfig{"fake": {Kind: llm.KindOpenAI, BaseURL: server.URL + "/v1", APIKey: "test-key"}}, Tasks: map[llm.Task]llm.TaskConfig{llm.TaskEdition: {Provider: "fake", Model: "test-model"}}, Models: map[string]llm.ModelInfo{"test-model": {InputPer1M: 1000000, OutputPer1M: 1000000}}}
	router, err := llm.NewRouter(cfg, outboundClient)
	if err != nil {
		t.Fatal(err)
	}
	spend := &batchSpend{}
	batch := NewEditionBatch(router, llm.NewBudget(spend, nil), cfg, outboundClient)
	sources := []Source{{EntryID: 1, ContentHash: strings.Repeat("a", 64), Text: "Beacon."}}
	req, err := GroundedRequest(llm.TaskEdition, "This week", sources)
	if err != nil {
		t.Fatal(err)
	}
	id, err := batch.Submit(context.Background(), []llm.CompleteRequest{req})
	if err != nil || !strings.HasPrefix(id, "batch-one:") {
		t.Fatalf("submit %q %v", id, err)
	}
	outputs, ready, err := batch.Poll(context.Background(), id, []llm.CompleteRequest{req})
	if err != nil || !ready || len(outputs) != 1 {
		t.Fatalf("poll %+v %v %v", outputs, ready, err)
	}
	if _, err := DecodeGrounded(req, outputs[0], sources); err != nil {
		t.Fatal(err)
	}
	if err := batch.Cancel(context.Background(), id); err != nil {
		t.Fatal(err)
	}
	if spend.spent <= 0 {
		t.Fatal("batch did not record spend")
	}
	capped := NewEditionBatch(router, llm.NewBudget(spend, map[string]llm.USD{"fake": 1}), cfg, outboundClient)
	if _, err := capped.Submit(context.Background(), []llm.CompleteRequest{req}); err == nil {
		t.Fatal("budget bypass")
	}
	mu.Lock()
	defer mu.Unlock()
	if len(calls) != 5 {
		t.Fatalf("unexpected calls %+v", calls)
	}
}
