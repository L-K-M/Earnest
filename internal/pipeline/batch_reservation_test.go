package pipeline

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/L-K-M/Earnest/internal/llm"
	"github.com/L-K-M/Earnest/internal/outbound"
)

func reservationBatch(t *testing.T, spend *batchSpend, limit llm.USD, handler http.HandlerFunc) (EditionBatch, *llm.Client, func() EditionBatch) {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	u, _ := url.Parse(server.URL)
	egress, err := outbound.New(outbound.Config{AllowHosts: []string{u.Host}, Spacing: time.Nanosecond})
	if err != nil {
		t.Fatal(err)
	}
	cfg := llm.Config{Providers: map[string]llm.ProviderConfig{"paid": {Kind: llm.KindOpenAI, BaseURL: server.URL, MonthlyLimitUSD: limit}}, Tasks: map[llm.Task]llm.TaskConfig{llm.TaskEdition: {Provider: "paid", Model: "model"}}, Models: map[string]llm.ModelInfo{"model": {OutputPer1M: 1000000}}}
	router, err := llm.NewRouter(cfg, egress)
	if err != nil {
		t.Fatal(err)
	}
	newBatch := func() EditionBatch {
		return NewEditionBatch(router, llm.NewBudget(spend, cfg.Limits()), cfg, egress)
	}
	client, err := llm.NewClient(router, llm.NewBudget(spend, cfg.Limits()), cfg.Models)
	if err != nil {
		t.Fatal(err)
	}
	return newBatch(), client, newBatch
}

func reservedBatchRequest(t *testing.T, maxTokens int) llm.CompleteRequest {
	t.Helper()
	schema, err := llm.NewSchema("answer", json.RawMessage(`{"type":"string"}`))
	if err != nil {
		t.Fatal(err)
	}
	return llm.CompleteRequest{Messages: []llm.Message{{Role: llm.RoleUser, Content: "article"}}, Schema: schema, MaxTokens: maxTokens}
}

func TestEditionBatchOversizedRequestNeverUploads(t *testing.T) {
	var calls atomic.Int32
	spend := &batchSpend{}
	batch, _, _ := reservationBatch(t, spend, 100, func(w http.ResponseWriter, r *http.Request) { calls.Add(1) })
	_, err := batch.Submit(t.Context(), []llm.CompleteRequest{reservedBatchRequest(t, 150)})
	if !errors.Is(err, llm.ErrBudgetExhausted) || calls.Load() != 0 || spend.spent != 0 {
		t.Fatalf("calls=%d spend=%d err=%v", calls.Load(), spend.spent, err)
	}
}

func TestEditionBatchAndSynchronousCallsShareReservations(t *testing.T) {
	uploadStarted := make(chan struct{})
	releaseUpload := make(chan struct{})
	spend := &batchSpend{}
	var calls atomic.Int32
	batch, client, newBatch := reservationBatch(t, spend, 100, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		switch r.URL.Path {
		case "/files":
			close(uploadStarted)
			<-releaseUpload
			io.WriteString(w, `{"id":"file-one"}`)
		case "/batches":
			io.WriteString(w, `{"id":"batch-one"}`)
		default:
			t.Error("unreserved provider call", r.URL.Path)
		}
	})
	req := reservedBatchRequest(t, 60)
	first := make(chan error, 1)
	go func() {
		_, err := batch.Submit(t.Context(), []llm.CompleteRequest{req})
		first <- err
	}()
	select {
	case <-uploadStarted:
	case err := <-first:
		t.Fatalf("first submission failed: %v", err)
	case <-time.After(10 * time.Second):
		t.Fatal("upload did not start")
	}
	_, syncErr := client.Complete(t.Context(), llm.TaskEdition, req)
	_, batchErr := newBatch().Submit(t.Context(), []llm.CompleteRequest{req})
	close(releaseUpload)
	if err := <-first; err != nil {
		t.Fatal(err)
	}
	if !errors.Is(syncErr, llm.ErrBudgetExhausted) || !errors.Is(batchErr, llm.ErrBudgetExhausted) || calls.Load() != 2 || spend.spent != 60 {
		t.Fatalf("sync=%v batch=%v calls=%d spend=%d", syncErr, batchErr, calls.Load(), spend.spent)
	}
}

func TestEditionBatchReconcilesUsageBeforeValidatingAllOutputs(t *testing.T) {
	for _, invalid := range []bool{false, true} {
		t.Run(fmt.Sprint("invalid=", invalid), func(t *testing.T) {
			spend := &batchSpend{}
			batch, _, newBatch := reservationBatch(t, spend, 200, func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/files":
					io.WriteString(w, `{"id":"file-one"}`)
				case "/batches":
					io.WriteString(w, `{"id":"batch-one"}`)
				case "/batches/batch-one":
					io.WriteString(w, `{"status":"completed","output_file_id":"output"}`)
				case "/files/output/content":
					for i := range 2 {
						text := `"ok"`
						if invalid && i == 0 {
							text = "invalid"
						}
						fmt.Fprintf(w, "{\"custom_id\":%q,\"response\":{\"status_code\":200,\"body\":{\"choices\":[{\"message\":{\"content\":%q}}],\"usage\":{\"prompt_tokens\":1,\"completion_tokens\":%d}}}}\n", fmt.Sprint(i), text, 20+i*10)
					}
				default:
					t.Error("unexpected request", r.URL.Path)
				}
			})
			requests := []llm.CompleteRequest{reservedBatchRequest(t, 60), reservedBatchRequest(t, 60)}
			id, err := batch.Submit(t.Context(), requests)
			if err != nil || spend.spent != 120 {
				t.Fatalf("submit spend=%d err=%v", spend.spent, err)
			}
			// A new transport instance must settle from the persisted handle alone.
			for range 2 {
				_, done, err := newBatch().Poll(t.Context(), id, requests)
				if invalid && !errors.Is(err, llm.ErrInvalidOutput) || !invalid && (err != nil || !done) {
					t.Fatalf("done=%v err=%v", done, err)
				}
				if spend.spent != 50 {
					t.Fatalf("settled spend=%d want=50", spend.spent)
				}
			}
		})
	}
}

func TestEditionBatchSubmissionRefundsOnlyUnbilledErrors(t *testing.T) {
	for _, test := range []struct {
		name   string
		path   string
		status int
		want   int64
	}{
		{"upload failure cannot run tokens", "/files", 500, 0},
		{"submission rejected", "/batches", 400, 0},
		{"submission rate limited", "/batches", 429, 0},
		{"submission server fault unknown", "/batches", 500, 60},
	} {
		t.Run(test.name, func(t *testing.T) {
			spend := &batchSpend{}
			batch, _, _ := reservationBatch(t, spend, 100, func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == test.path {
					w.WriteHeader(test.status)
					return
				}
				io.WriteString(w, `{"id":"file-one"}`)
			})
			if _, err := batch.Submit(t.Context(), []llm.CompleteRequest{reservedBatchRequest(t, 60)}); err == nil {
				t.Fatal("failed submission returned success")
			}
			if spend.spent != test.want {
				t.Fatalf("spend=%d want=%d", spend.spent, test.want)
			}
		})
	}
}

func TestEditionBatchMissingUsageRetainsEstimate(t *testing.T) {
	spend := &batchSpend{}
	batch, _, _ := reservationBatch(t, spend, 100, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/files":
			io.WriteString(w, `{"id":"file-one"}`)
		case "/batches":
			io.WriteString(w, `{"id":"batch-one"}`)
		case "/batches/batch-one":
			io.WriteString(w, `{"status":"completed","output_file_id":"output"}`)
		case "/files/output/content":
			io.WriteString(w, `{"custom_id":"0","response":{"status_code":200,"body":{"choices":[{"message":{"content":"\"ok\""}}]}}}`)
		}
	})
	reqs := []llm.CompleteRequest{reservedBatchRequest(t, 60)}
	id, err := batch.Submit(t.Context(), reqs)
	if err != nil {
		t.Fatal(err)
	}
	if _, done, err := batch.Poll(t.Context(), id, reqs); err != nil || !done || spend.spent != 60 {
		t.Fatalf("done=%v spend=%d err=%v", done, spend.spent, err)
	}
}

func TestEditionBatchTerminalUsageAndUnbilledRequests(t *testing.T) {
	for _, test := range []struct {
		name   string
		status string
		output string
		errors string
		want   int64
	}{
		{"validation failed", "failed", "", "", 0},
		{"cancelled billing unknown", "cancelled", "", "", 120},
		{"expired partial usage", "expired", "success", "unbilled", 20},
		{"completed partial rejection", "completed", "success", "rejection", 20},
		{"missing output row", "completed", "success", "", 120},
	} {
		t.Run(test.name, func(t *testing.T) {
			spend := &batchSpend{}
			batch, _, _ := reservationBatch(t, spend, 200, func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/files":
					io.WriteString(w, `{"id":"file-one"}`)
				case "/batches":
					io.WriteString(w, `{"id":"batch-one"}`)
				case "/batches/batch-one":
					_ = json.NewEncoder(w).Encode(map[string]string{"status": test.status, "output_file_id": test.output, "error_file_id": test.errors})
				case "/files/success/content":
					io.WriteString(w, `{"custom_id":"0","response":{"status_code":200,"body":{"choices":[{"message":{"content":"\"ok\""}}],"usage":{"prompt_tokens":1,"completion_tokens":20}}}}`)
				case "/files/unbilled/content":
					io.WriteString(w, `{"custom_id":"1","response":null,"error":{"code":"batch_expired"}}`)
				case "/files/rejection/content":
					io.WriteString(w, `{"custom_id":"1","response":{"status_code":400,"body":{"error":"bad request"}}}`)
				default:
					t.Error("unexpected request", r.URL.Path)
				}
			})
			reqs := []llm.CompleteRequest{reservedBatchRequest(t, 60), reservedBatchRequest(t, 60)}
			id, err := batch.Submit(t.Context(), reqs)
			if err != nil {
				t.Fatal(err)
			}
			if _, done, err := batch.Poll(t.Context(), id, reqs); err == nil || done || spend.spent != test.want {
				t.Fatalf("done=%v spend=%d want=%d err=%v", done, spend.spent, test.want, err)
			}
		})
	}
}

func TestEditionBatchDefaultsOutputBound(t *testing.T) {
	spend := &batchSpend{}
	batch, _, _ := reservationBatch(t, spend, 10000, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/files" {
			if err := r.ParseMultipartForm(1 << 20); err != nil {
				t.Error(err)
				return
			}
			defer r.MultipartForm.RemoveAll()
			file, _, err := r.FormFile("file")
			if err != nil {
				t.Error(err)
				return
			}
			defer file.Close()
			data, err := io.ReadAll(file)
			if err != nil || !strings.Contains(string(data), `"max_completion_tokens":4096`) {
				t.Errorf("missing output bound: %s err=%v", data, err)
			}
		}
		io.WriteString(w, `{"id":"one"}`)
	})
	if _, err := batch.Submit(context.Background(), []llm.CompleteRequest{reservedBatchRequest(t, 0)}); err != nil || spend.spent != llm.DefaultMaxTokens {
		t.Fatalf("spend=%d err=%v", spend.spent, err)
	}
}
