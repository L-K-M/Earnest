package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/L-K-M/Earnest/internal/llm"
	"github.com/L-K-M/Earnest/internal/outbound"
	"github.com/L-K-M/Earnest/internal/pipeline"
	"github.com/L-K-M/Earnest/internal/store/storetest"
)

func llmSettingsTestInput() LLMSettingsInput {
	return LLMSettingsInput{OpID: "llm-settings", Providers: []LLMProviderInput{{Name: "local", Kind: "openai", BaseURL: "https://models.example/v1", APIKey: "sk-test-private", MonthlyLimitUSD: "20.000001", TimeoutSeconds: 60, TokenField: "max_tokens"}}, Tasks: []LLMTaskSettings{{Task: "embed", Provider: "local", Model: "local-embed"}}, Models: []LLMModelSettings{{Name: "local-embed", InputPer1M: "0", OutputPer1M: "0", EmbeddingPer1M: "0", Dimensions: 2}}}
}

func TestLLMSettingsValidation(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(*LLMSettingsInput)
	}{
		{"provider name", func(in *LLMSettingsInput) { in.Providers[0].Name = "unsafe_name" }},
		{"duplicate provider", func(in *LLMSettingsInput) { in.Providers = append(in.Providers, in.Providers[0]) }},
		{"unknown kind", func(in *LLMSettingsInput) { in.Providers[0].Kind = "other" }},
		{"credentials URL", func(in *LLMSettingsInput) { in.Providers[0].BaseURL = "https://secret:password@models.example/v1" }},
		{"query URL", func(in *LLMSettingsInput) { in.Providers[0].BaseURL = "https://models.example/v1?key=secret" }},
		{"empty query URL", func(in *LLMSettingsInput) { in.Providers[0].BaseURL = "https://models.example/v1?" }},
		{"empty fragment URL", func(in *LLMSettingsInput) { in.Providers[0].BaseURL = "https://models.example/v1#" }},
		{"file URL", func(in *LLMSettingsInput) { in.Providers[0].BaseURL = "file:///etc/passwd" }},
		{"bad port", func(in *LLMSettingsInput) { in.Providers[0].BaseURL = "http://models.example:65536/v1" }},
		{"timeout", func(in *LLMSettingsInput) { in.Providers[0].TimeoutSeconds = 0 }},
		{"token field", func(in *LLMSettingsInput) { in.Providers[0].TokenField = "other" }},
		{"negative limit", func(in *LLMSettingsInput) { in.Providers[0].MonthlyLimitUSD = "-1" }},
		{"limit overflow", func(in *LLMSettingsInput) { in.Providers[0].MonthlyLimitUSD = "100000000" }},
		{"key controls", func(in *LLMSettingsInput) { in.Providers[0].APIKey = "private\r\nheader" }},
		{"replace and clear", func(in *LLMSettingsInput) { in.Providers[0].ClearAPIKey = true }},
		{"half configured task", func(in *LLMSettingsInput) { in.Tasks[0].Provider = "" }},
		{"missing provider", func(in *LLMSettingsInput) { in.Tasks[0].Provider = "missing" }},
		{"unpriced model", func(in *LLMSettingsInput) { in.Tasks[0].Model = "missing" }},
		{"unknown task", func(in *LLMSettingsInput) { in.Tasks[0].Task = "other" }},
		{"duplicate task", func(in *LLMSettingsInput) { in.Tasks = append(in.Tasks, in.Tasks[0]) }},
		{"anthropic embed", func(in *LLMSettingsInput) { in.Providers[0].Kind = "anthropic" }},
		{"duplicate model", func(in *LLMSettingsInput) { in.Models = append(in.Models, in.Models[0]) }},
		{"model name", func(in *LLMSettingsInput) { in.Models[0].Name = " " }},
		{"negative price", func(in *LLMSettingsInput) { in.Models[0].InputPer1M = "-1" }},
		{"price precision", func(in *LLMSettingsInput) { in.Models[0].OutputPer1M = "0.1234567" }},
		{"geometry", func(in *LLMSettingsInput) { in.Models[0].Dimensions = 1536 }},
		{"geometry limit", func(in *LLMSettingsInput) { in.Models[0].Dimensions = 65537; in.Models[0].Truncatable = true }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			in := llmSettingsTestInput()
			tc.change(&in)
			if _, err := validateLLMSettings(in, llm.DefaultConfig()); !errors.Is(err, ErrInvalid) {
				t.Fatalf("accepted invalid settings: %v", err)
			}
		})
	}
	in := llmSettingsTestInput()
	cfg, err := validateLLMSettings(in, llm.DefaultConfig())
	if err != nil || cfg.Providers["local"].MonthlyLimitUSD != 20000001 {
		t.Fatalf("exact money: %v %v", cfg.Providers["local"].MonthlyLimitUSD, err)
	}
	in.Tasks[0].Provider, in.Tasks[0].Model = "", ""
	cfg, err = validateLLMSettings(in, llm.DefaultConfig())
	if err != nil || len(cfg.Tasks) != 0 {
		t.Fatalf("disabled route: %v", err)
	}
}

func TestLLMSettingsSecretsAndRevision(t *testing.T) {
	f := newCoreFixture(t, storetest.NewTemplate(t))
	ctx := t.Context()
	original, err := f.s.GetLLMSettings(ctx, f.a)
	if err != nil || original.Revision != 0 || len(original.Providers) != 0 || len(original.Tasks) != 7 || len(original.Models) == 0 {
		t.Fatalf("defaults: %+v %v", original, err)
	}
	in := llmSettingsTestInput()
	saved, err := f.s.PutLLMSettings(ctx, f.a, in)
	if err != nil || saved.Revision != 1 || !saved.Providers[0].HasAPIKey {
		t.Fatalf("save: %+v %v", saved, err)
	}
	var envelope, op []byte
	if err := f.db.Pool().QueryRow(ctx, "SELECT config_encrypted FROM llm_settings").Scan(&envelope); err != nil {
		t.Fatal(err)
	}
	if err := f.db.Pool().QueryRow(ctx, "SELECT result FROM op_ids WHERE principal_id=$1 AND op_id=$2", f.a.PrincipalID, in.OpID).Scan(&op); err != nil {
		t.Fatal(err)
	}
	for _, raw := range [][]byte{envelope, op} {
		if bytes.Contains(raw, []byte(in.Providers[0].APIKey)) {
			t.Fatal("plaintext key persisted outside encrypted envelope")
		}
	}
	replay, err := f.s.PutLLMSettings(ctx, f.a, in)
	if err != nil || replay.Revision != saved.Revision {
		t.Fatalf("replay: %+v %v", replay, err)
	}
	in.OpID = "stale"
	if _, err := f.s.PutLLMSettings(ctx, f.a, in); !errors.Is(err, ErrConflict) {
		t.Fatalf("stale writer: %v", err)
	}
	in.Revision = 1
	in.OpID = "keep-key"
	in.Providers[0].APIKey = ""
	saved, err = f.s.PutLLMSettings(ctx, f.a, in)
	if err != nil || !saved.Providers[0].HasAPIKey {
		t.Fatalf("blank cleared key: %+v %v", saved, err)
	}
	in.Revision = saved.Revision
	in.OpID = "changed-endpoint"
	in.Providers[0].BaseURL = "https://another.example/v1"
	if _, err := f.s.PutLLMSettings(ctx, f.a, in); !errors.Is(err, ErrInvalid) {
		t.Fatalf("retargeted a saved secret: %v", err)
	}
	in.Providers[0].ClearAPIKey = true
	saved, err = f.s.PutLLMSettings(ctx, f.a, in)
	if err != nil || saved.Providers[0].HasAPIKey {
		t.Fatalf("explicit clear: %+v %v", saved, err)
	}
	// The setting belongs to the server, not the saving user's synced document.
	other := secondHuman(t, f)
	got, err := f.s.GetLLMSettings(ctx, other)
	if err != nil || got.Revision != saved.Revision {
		t.Fatalf("shared settings: %+v %v", got, err)
	}
	var count int
	if err := f.db.Pool().QueryRow(ctx, "SELECT count(*) FROM changes WHERE payload::text LIKE '%sk-test-private%'").Scan(&count); err != nil || count != 0 {
		t.Fatalf("secret in change feed: %d %v", count, err)
	}
}

func secondHuman(t *testing.T, f *coreFixture) Actor {
	t.Helper()
	a, err := NewAuth(f.db, nil, bytes.Repeat([]byte("s"), 32)).CreateUser(t.Context(), "another", "password")
	if err != nil {
		t.Fatal(err)
	}
	return a
}

func TestLLMSettingsConcurrentWriters(t *testing.T) {
	f := newCoreFixture(t, storetest.NewTemplate(t))
	var wg sync.WaitGroup
	results := make(chan error, 2)
	for i := range 2 {
		wg.Go(func() {
			in := llmSettingsTestInput()
			in.OpID = fmt.Sprintf("concurrent-%d", i)
			_, err := f.s.PutLLMSettings(t.Context(), f.a, in)
			results <- err
		})
	}
	wg.Wait()
	close(results)
	ok, conflict := 0, 0
	for err := range results {
		switch {
		case err == nil:
			ok++
		case errors.Is(err, ErrConflict):
			conflict++
		default:
			t.Fatal(err)
		}
	}
	if ok != 1 || conflict != 1 {
		t.Fatalf("writers: ok=%d conflict=%d", ok, conflict)
	}
}

func TestLLMSettingsRuntimeReload(t *testing.T) {
	f := newCoreFixture(t, storetest.NewTemplate(t))
	var calls atomic.Int64
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.Header.Get("Authorization") != "Bearer sk-test-private" || r.URL.Path != "/v1/embeddings" {
			t.Errorf("wrong provider route or key")
		}
		var body struct {
			Model string `json:"model"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Model != "local-embed" {
			t.Errorf("model: %q %v", body.Model, err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"embedding":[1,0]}],"usage":{"prompt_tokens":1,"total_tokens":1}}`))
	}))
	defer provider.Close()
	u, _ := url.Parse(provider.URL)
	egress, err := outbound.New(outbound.Config{AllowHosts: []string{u.Host}})
	if err != nil {
		t.Fatal(err)
	}
	f.s.outbound = egress
	old, err := f.s.ModelContext(t.Context())
	if err != nil || pipelineEngine(old).Enabled(llm.TaskEmbed) {
		t.Fatalf("default runtime: %v", err)
	}
	in := llmSettingsTestInput()
	in.Providers[0].BaseURL = provider.URL + "/v1"
	if _, err := f.s.PutLLMSettings(t.Context(), f.a, in); err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 0 {
		t.Fatal("saving contacted provider")
	}
	fresh, err := f.s.ModelContext(t.Context())
	if err != nil || !pipelineEngine(fresh).Enabled(llm.TaskEmbed) || pipelineEngine(old).Enabled(llm.TaskEmbed) {
		t.Fatalf("snapshot reload: %v", err)
	}
	if _, err := pipelineEngine(fresh).Query(fresh, "hello"); err != nil || calls.Load() != 1 {
		t.Fatalf("live configured provider: %v, calls %d", err, calls.Load())
	}
	// Another process, with different environment defaults, loads the saved row.
	defaults := llm.DefaultConfig()
	defaults.Providers["env"] = llm.ProviderConfig{Kind: llm.KindOpenAI, BaseURL: "https://unused.example", APIKey: "env-secret"}
	restarted, err := New(f.db, Deps{SecretKey: []byte(strings.Repeat("s", 32)), Outbound: egress, LLMConfig: &defaults})
	if err != nil {
		t.Fatal(err)
	}
	got, err := restarted.GetLLMSettings(t.Context(), f.a)
	if err != nil || got.Revision != 1 || len(got.Providers) != 1 || got.Providers[0].Name != "local" {
		t.Fatalf("saved override after restart: %+v %v", got, err)
	}
	in.Revision = 1
	in.OpID = "disable"
	in.Tasks = nil
	in.Providers = nil
	in.Models = nil
	if _, err := f.s.PutLLMSettings(t.Context(), f.a, in); err != nil {
		t.Fatal(err)
	}
	disabled, err := restarted.ModelContext(t.Context())
	if err != nil || pipelineEngine(disabled).Enabled(llm.TaskEmbed) || pipeline.EditionBatchFrom(disabled) != nil {
		t.Fatalf("disable: %v", err)
	}
	// The old snapshot may finish its in-flight work but new work stays disabled.
	if !pipelineEngine(fresh).Enabled(llm.TaskEmbed) {
		t.Fatal("mutated in-flight runtime")
	}
}

func TestLLMSettingsLegacyDefaultsAreRedacted(t *testing.T) {
	cfg := llm.DefaultConfig()
	cfg.Providers["legacy"] = llm.ProviderConfig{BaseURL: "https://user:password@models.example/v1?key=query-secret", APIKey: "api-secret"}
	raw, err := json.Marshal(redactedLLMSettings(cfg, 0))
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{"user:", "password", "query-secret", "api-secret"} {
		if bytes.Contains(raw, []byte(secret)) {
			t.Fatal("default config leaked secret")
		}
	}
}

func TestLLMSettingsTamperDoesNotEnableModels(t *testing.T) {
	f := newCoreFixture(t, storetest.NewTemplate(t))
	if _, err := f.s.PutLLMSettings(t.Context(), f.a, llmSettingsTestInput()); err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.Pool().Exec(t.Context(), "UPDATE llm_settings SET config_encrypted=decode(repeat('00',32),'hex')"); err != nil {
		t.Fatal(err)
	}
	if _, err := f.s.ModelContext(t.Context()); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("tampered config accepted: %v", err)
	}
}

type llmTestBatch struct{ calls int }

func (b *llmTestBatch) Submit(context.Context, []llm.CompleteRequest) (string, error) {
	b.calls++
	return "batch:reservation", nil
}
func (b *llmTestBatch) Poll(context.Context, string, []llm.CompleteRequest) ([]string, bool, error) {
	b.calls++
	return nil, true, nil
}
func (b *llmTestBatch) Cancel(context.Context, string) error { b.calls++; return nil }

func TestLLMSettingsBatchDestinationBinding(t *testing.T) {
	original := &llmTestBatch{}
	a := &configuredEditionBatch{EditionBatch: original, prefix: "llm-settings:a:"}
	handle, err := a.Submit(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	other := &llmTestBatch{}
	b := &configuredEditionBatch{EditionBatch: other, prefix: "llm-settings:b:"}
	if _, _, err := b.Poll(t.Context(), handle, nil); !errors.Is(err, llm.ErrUnsupported) {
		t.Fatalf("polled other destination: %v", err)
	}
	if err := b.Cancel(t.Context(), handle); !errors.Is(err, llm.ErrUnsupported) || other.calls != 0 {
		t.Fatalf("cancelled other destination: %v", err)
	}
	if _, done, err := a.Poll(t.Context(), handle, nil); err != nil || !done {
		t.Fatalf("same config failed: %v", err)
	}
	b.allowLegacy = true
	if _, _, err := b.Poll(t.Context(), "legacy-handle", nil); err != nil {
		t.Fatal(err)
	}
	if _, _, err := b.Poll(t.Context(), handle, nil); !errors.Is(err, llm.ErrUnsupported) {
		t.Fatal("legacy exception accepted another config")
	}
}
