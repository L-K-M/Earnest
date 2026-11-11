package service

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/L-K-M/Earnest/internal/llm"
	"github.com/L-K-M/Earnest/internal/outbound"
	"github.com/L-K-M/Earnest/internal/pipeline"
	"github.com/L-K-M/Earnest/internal/store/storetest"
)

func TestLLMSettingsPreservesPendingEditionAcrossRestart(t *testing.T) {
	f := newCoreFixture(t, storetest.NewTemplate(t))
	body := "Weekly Beacon report."
	id := f.ingest(t, body)
	if _, err := f.db.Pool().Exec(t.Context(), "UPDATE subscriptions SET cadence='week' WHERE id=$1", f.subscription); err != nil {
		t.Fatal(err)
	}
	output := groundedOutput(id, coreHash("body"+body), "Beacon report.", body)
	var polls, newCalls atomic.Int64
	original := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer original-key" {
			t.Error("batch used a replacement credential")
		}
		w.Header().Set("Content-Type", "application/json")
		switch r.Method + " " + r.URL.Path {
		case "POST /v1/files":
			_, _ = w.Write([]byte(`{"id":"input-file"}`))
		case "POST /v1/batches":
			_, _ = w.Write([]byte(`{"id":"original-batch"}`))
		case "GET /v1/batches/original-batch":
			polls.Add(1)
			_, _ = w.Write([]byte(`{"status":"completed","output_file_id":"output-file"}`))
		case "GET /v1/files/output-file/content":
			_ = json.NewEncoder(w).Encode(map[string]any{"custom_id": "0", "response": map[string]any{"status_code": 200, "body": map[string]any{"usage": map[string]int{"prompt_tokens": 1, "completion_tokens": 1}, "choices": []any{map[string]any{"finish_reason": "stop", "message": map[string]string{"content": output}}}}}})
		default:
			t.Errorf("unexpected original-provider request: %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusBadRequest)
		}
	}))
	defer original.Close()
	replacement := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { newCalls.Add(1); w.WriteHeader(http.StatusBadRequest) }))
	defer replacement.Close()
	a, _ := url.Parse(original.URL)
	b, _ := url.Parse(replacement.URL)
	egress, err := outbound.New(outbound.Config{AllowHosts: []string{a.Host, b.Host}})
	if err != nil {
		t.Fatal(err)
	}
	f.s.outbound = egress
	cfg := llm.DefaultConfig()
	cfg.Providers["local"] = llm.ProviderConfig{Kind: llm.KindOpenAI, BaseURL: original.URL + "/v1", APIKey: "original-key", MonthlyLimitUSD: 20_000_000}
	cfg.Models["old-chat"] = llm.ModelInfo{InputPer1M: 1_000_000, OutputPer1M: 2_000_000}
	cfg.Tasks[llm.TaskEdition] = llm.TaskConfig{Provider: "local", Model: "old-chat"}
	f.s.modelDefaults = cfg
	ctx, err := f.s.ModelContext(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.s.TriggerEdition(ctx, f.a, EditionWeekly, f.s.now().Add(time.Hour), "batch-before-settings"); err != nil {
		t.Fatal(err)
	}
	if n, err := f.s.RunEditionJobs(ctx, 1); err != nil || n != 1 {
		t.Fatalf("submit: %d %v", n, err)
	}

	in := LLMSettingsInput{OpID: "replace-config", Providers: []LLMProviderInput{{Name: "local", Kind: "openai", BaseURL: replacement.URL + "/v1", APIKey: "replacement-key", MonthlyLimitUSD: "30", TimeoutSeconds: 60, TokenField: "max_tokens"}}, Tasks: []LLMTaskSettings{{Task: "edition", Provider: "local", Model: "new-chat"}}, Models: []LLMModelSettings{{Name: "new-chat", InputPer1M: "99", OutputPer1M: "99", EmbeddingPer1M: "0"}}}
	if _, err := f.s.PutLLMSettings(t.Context(), f.a, in); err != nil {
		t.Fatal(err)
	}
	paused := in
	paused.Revision, paused.OpID, paused.Tasks = 1, "pause-collection", nil
	if _, err := f.s.PutLLMSettings(t.Context(), f.a, paused); err != nil {
		t.Fatal(err)
	}
	f.clock.Add(int64(time.Hour))
	// A new service has no previous runtime in memory. It must recover the
	// original provider, key, model and prices from the pending batch's identity.
	restarted, err := New(f.db, Deps{SecretKey: []byte(strings.Repeat("s", 32)), Clock: f.s.now, Outbound: egress})
	if err != nil {
		t.Fatal(err)
	}
	fresh, err := restarted.ModelContext(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if n, err := restarted.RunEditionJobs(fresh, 1); err != nil || n != 0 || polls.Load() != 0 || newCalls.Load() != 0 {
		t.Fatalf("disabled collection made calls: %d %v", n, err)
	}
	in.Revision, in.OpID = 2, "resume-collection"
	if _, err := restarted.PutLLMSettings(t.Context(), f.a, in); err != nil {
		t.Fatal(err)
	}
	fresh, err = restarted.ModelContext(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if model, _ := pipelineEngine(fresh).Model(llm.TaskEdition); model != "new-chat" {
		t.Fatalf("new work did not adopt settings: %s", model)
	}
	if n, err := restarted.RunEditionJobs(fresh, 1); err != nil || n != 1 {
		t.Fatalf("collect: %d %v", n, err)
	}
	latest, err := restarted.LatestEdition(fresh, f.a)
	if err != nil || latest.Status != "ready" || polls.Load() != 1 || newCalls.Load() != 0 {
		t.Fatalf("old batch was stranded: status=%s polls=%d new_calls=%d err=%v", latest.Status, polls.Load(), newCalls.Load(), err)
	}
	if latest.Model != "old-chat" || len(latest.Sections[0].Claims) != 1 || !latest.Sections[0].Claims[0].Verified {
		t.Fatalf("original result/provenance lost: %+v", latest)
	}
	var spent int64
	if err := f.db.Pool().QueryRow(t.Context(), "SELECT (spent_usd*1000000)::bigint FROM llm_spend WHERE provider='local'").Scan(&spent); err != nil || spent != 3 {
		t.Fatalf("original batch prices not reconciled: %d %v", spent, err)
	}
	// Completion removes the only reference, so historical credentials are purged.
	var retained int
	if err := f.db.Pool().QueryRow(t.Context(), "SELECT count(*) FROM llm_batch_configs").Scan(&retained); err != nil || retained != 0 {
		t.Fatalf("unused batch credentials retained: %d %v", retained, err)
	}
}

func TestLLMEditionBatchContextIsOptional(t *testing.T) {
	// Explicit pipeline contexts used by local evaluation and tests do not
	// depend on the server-managed model configuration.
	ctx := pipeline.WithEditionBatch(t.Context(), &testEditionBatch{})
	s := &Service{}
	original, err := s.editionModelContext(ctx, "opaque-batch")
	if err != nil || original != ctx || pipeline.EditionBatchFrom(original) == nil {
		t.Fatalf("explicit batch context changed: %v", err)
	}
}

func TestLLMEditionMissingSnapshotFailsClosed(t *testing.T) {
	f := newCoreFixture(t, storetest.NewTemplate(t))
	ctx, err := f.s.ModelContext(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.s.editionModelContext(ctx, editionConfigPrefix+strings.Repeat("a", 64)+":batch"); err == nil {
		t.Fatal("a missing snapshot must not fall back to a different provider")
	}
}

func TestLLMEditionLegacyConfigurationSurvivesFirstSave(t *testing.T) {
	f := newCoreFixture(t, storetest.NewTemplate(t))
	cfg := llm.DefaultConfig()
	cfg.Providers["local"] = llm.ProviderConfig{Kind: llm.KindOpenAI, BaseURL: "https://original.example/v1", APIKey: "legacy-key-sentinel"}
	cfg.Models["old-chat"] = llm.ModelInfo{}
	cfg.Tasks[llm.TaskEdition] = llm.TaskConfig{Provider: "local", Model: "old-chat"}
	cfg.Providers["unused"] = llm.ProviderConfig{Kind: llm.KindAnthropic, BaseURL: "https://unused.example", APIKey: "unrelated-key-sentinel"}
	cfg.Tasks[llm.TaskAsk] = llm.TaskConfig{Provider: "unused", Model: "old-chat"}
	f.s.modelDefaults = cfg
	if _, err := f.db.Pool().Exec(t.Context(), `INSERT INTO jobs(kind,payload) VALUES('edition','{"batch_id":"legacy-batch:reservation"}')`); err != nil {
		t.Fatal(err)
	}
	if _, err := f.s.PutLLMSettings(t.Context(), f.a, llmSettingsTestInput()); err != nil {
		t.Fatal(err)
	}
	fresh, err := f.s.ModelContext(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	original, err := f.s.editionModelContext(fresh, "legacy-batch:reservation")
	if err != nil {
		t.Fatal(err)
	}
	if model, _ := pipelineEngine(original).Model(llm.TaskEdition); model != "old-chat" {
		t.Fatalf("legacy model lost: %s", model)
	}
	batch := pipeline.EditionBatchFrom(original).(*configuredEditionBatch)
	if _, err := batch.originalHandle("legacy-batch:reservation"); err != nil {
		t.Fatal(err)
	}
	var envelope []byte
	var hash string
	if err := f.db.Pool().QueryRow(t.Context(), "SELECT config_hash,config_encrypted FROM llm_batch_configs WHERE legacy").Scan(&hash, &envelope); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(envelope), "legacy-key-sentinel") {
		t.Fatal("legacy key stored in plaintext")
	}
	raw, err := f.s.openSecret(envelope, llmBatchConfigPurpose+":"+hash)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "unrelated-key-sentinel") {
		t.Fatal("batch retained credentials for an unrelated task")
	}
}
