package service

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/L-K-M/Earnest/internal/llm"
	"github.com/L-K-M/Earnest/internal/outbound"
	"github.com/L-K-M/Earnest/internal/store/storetest"
)

func TestCheckLLMModelIsolation(t *testing.T) {
	f := newCoreFixture(t, storetest.NewTemplate(t))
	var calls atomic.Int64
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.URL.Path != "/v1/embeddings" || r.Header.Get("Authorization") != "Bearer sk-test-private" {
			t.Error("wrong request or credentials")
		}
		var body struct {
			Input      []string `json:"input"`
			Dimensions int      `json:"dimensions"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		if len(body.Input) != 1 || body.Input[0] != "Earnest model compatibility check." {
			t.Error("unexpected content")
		}
		width := body.Dimensions
		if width == 0 {
			width = 4096
		}
		json.NewEncoder(w).Encode(map[string]any{"data": []any{map[string]any{"index": 0, "embedding": make([]float32, width)}}, "usage": map[string]int{"prompt_tokens": 10}})
	}))
	defer provider.Close()
	u, _ := url.Parse(provider.URL)
	client, err := outbound.New(outbound.Config{AllowHosts: []string{u.Host}, Spacing: time.Nanosecond})
	if err != nil {
		t.Fatal(err)
	}
	f.s.outbound = client
	settings := llmSettingsTestInput()
	settings.Providers[0].BaseURL = provider.URL + "/v1"
	model := settings.Models[0]
	model.Dimensions = 0
	model.EmbeddingPer1M = "1"
	in := LLMModelCheckInput{Provider: settings.Providers[0], Model: model, Capability: "embedding"}
	got, err := f.s.CheckLLMModel(t.Context(), f.a, in)
	if err != nil || !got.Compatible || got.Dimensions != 4096 || !got.SupportsCustomDimensions || calls.Load() != 2 {
		t.Fatalf("check: %+v %v calls=%d", got, err, calls.Load())
	}
	loaded, err := f.s.GetLLMSettings(t.Context(), f.a)
	if err != nil || loaded.Revision != 0 || len(loaded.Providers) != 0 {
		t.Fatalf("check persisted settings: %+v %v", loaded, err)
	}
	var ops, cache, spent int
	if err := f.db.Pool().QueryRow(t.Context(), "SELECT count(*) FROM op_ids").Scan(&ops); err != nil {
		t.Fatal(err)
	}
	if err := f.db.Pool().QueryRow(t.Context(), "SELECT count(*) FROM llm_cache").Scan(&cache); err != nil {
		t.Fatal(err)
	}
	if err := f.db.Pool().QueryRow(t.Context(), "SELECT (spent_usd*1000000)::bigint FROM llm_spend WHERE provider='local'").Scan(&spent); err != nil {
		t.Fatal(err)
	}
	if ops != 0 || cache != 0 || spent != 20 {
		t.Fatalf("side effects ops=%d cache=%d spent=%d", ops, cache, spent)
	}
	in.Revision = 1
	if _, err := f.s.CheckLLMModel(t.Context(), f.a, in); !errors.Is(err, ErrConflict) {
		t.Fatalf("stale: %v", err)
	}
	in.Revision = 0
	in.Model.EmbeddingPer1M = ""
	if _, err := f.s.CheckLLMModel(t.Context(), f.a, in); !errors.Is(err, ErrInvalid) {
		t.Fatalf("unpriced: %v", err)
	}
	if calls.Load() != 2 {
		t.Fatal("invalid check contacted provider")
	}
	// A saved key is inherited, but cannot follow an edited destination.
	saved, err := f.s.PutLLMSettings(t.Context(), f.a, settings)
	if err != nil {
		t.Fatal(err)
	}
	in.Model = model
	in.Revision = saved.Revision
	in.Provider.APIKey = ""
	if _, err := f.s.CheckLLMModel(t.Context(), f.a, in); err != nil {
		t.Fatal(err)
	}
	in.Provider.BaseURL += "/other"
	if _, err := f.s.CheckLLMModel(t.Context(), f.a, in); !errors.Is(err, ErrInvalid) {
		t.Fatalf("key moved: %v", err)
	}
}

func TestModelCheckErrorsDoNotExposeProviderContent(t *testing.T) {
	for _, status := range []int{400, 401, 403, 404, 429, 500} {
		message := modelCheckError(&llm.Error{StatusCode: status, Err: errors.New("secret-key https://private.example/token")})
		if strings.Contains(message, "secret") || strings.Contains(message, "private.example") {
			t.Fatal("provider content exposed")
		}
	}
}
