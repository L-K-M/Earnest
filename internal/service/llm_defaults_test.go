package service

import (
	"github.com/L-K-M/Earnest/internal/llm"
	"github.com/L-K-M/Earnest/internal/store/storetest"
	"testing"
)

func TestLLMDefaultsAndLegacyRoundTrip(t *testing.T) {
	in := llmSettingsTestInput()
	in.Defaults = &llm.DefaultRoutes{Embedding: llm.TaskConfig{Provider: "local", Model: "local-embed"}}
	in.Tasks[0].Mode = "default"
	in.Tasks = append(in.Tasks, LLMTaskSettings{Task: "ask", Mode: "off"})
	in.Models[0].Provider = "local"
	in.Models[0].Dimensions = 4096
	in.Models[0].SupportsCustomDimensions = true
	cfg, err := validateLLMSettings(in, llm.DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Tasks[llm.TaskEmbed].Model != "local-embed" || len(cfg.Tasks) != 1 {
		t.Fatalf("effective routes: %+v", cfg.Tasks)
	}
	got := redactedLLMSettings(cfg, 1)
	if got.Defaults == nil || got.Tasks[0].Mode != "default" || got.Models[0].Provider != "local" || !got.Models[0].SupportsCustomDimensions {
		t.Fatalf("roundtrip: %+v", got)
	}
	// An old writer sends effective routes but has no inheritance metadata.
	legacy := in
	legacy.Defaults = nil
	legacy.Tasks = []LLMTaskSettings{{Task: "embed", Provider: "local", Model: "local-embed"}}
	old, err := validateLLMSettings(legacy, cfg)
	if err != nil || old.Defaults != nil || len(old.TaskModes) != 0 || old.Tasks[llm.TaskEmbed] != cfg.Tasks[llm.TaskEmbed] {
		t.Fatalf("legacy route changed: %+v %v", old, err)
	}
	// Changing an unused default cannot enable disabled features.
	in.Tasks[0].Mode = "off"
	off, err := validateLLMSettings(in, cfg)
	if err != nil || len(off.Tasks) != 0 {
		t.Fatalf("default enabled features: %+v %v", off, err)
	}
}

func TestLLMProviderSpecificModels(t *testing.T) {
	in := llmSettingsTestInput()
	other := in.Providers[0]
	other.Name = "other"
	in.Providers = append(in.Providers, other)
	global := in.Models[0]
	local := global
	local.Provider = "local"
	local.EmbeddingPer1M = "1"
	second := global
	second.Provider = "other"
	second.EmbeddingPer1M = "2"
	in.Models = []LLMModelSettings{global, local, second}
	cfg, err := validateLLMSettings(in, llm.DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	for provider, want := range map[string]llm.USD{"local": 1000000, "other": 2000000, "legacy": 0} {
		got, ok := llm.LookupModel(cfg.Models, provider, global.Name)
		if !ok || got.EmbeddingPer1M != want {
			t.Errorf("%s price: %+v", provider, got)
		}
	}
	got := redactedLLMSettings(cfg, 1)
	if len(got.Models) != 3 {
		t.Fatalf("models collapsed: %+v", got.Models)
	}
}

func TestLLMDefaultValidation(t *testing.T) {
	for _, change := range []func(*LLMSettingsInput){
		func(in *LLMSettingsInput) { in.Tasks[0].Mode = "default" },
		func(in *LLMSettingsInput) { in.Tasks[0].Mode = "invalid" },
		func(in *LLMSettingsInput) {
			in.Defaults = &llm.DefaultRoutes{Text: llm.TaskConfig{Provider: "local", Model: "missing"}}
		},
		func(in *LLMSettingsInput) { in.Models[0].Provider = "missing" },
	} {
		in := llmSettingsTestInput()
		change(&in)
		if _, err := validateLLMSettings(in, llm.DefaultConfig()); err == nil {
			t.Fatal("invalid settings accepted")
		}
	}
}

func TestEditionSnapshotKeepsProviderSpecificPrices(t *testing.T) {
	cfg := llm.Config{
		Providers: map[string]llm.ProviderConfig{"a": {}, "b": {}},
		Tasks: map[llm.Task]llm.TaskConfig{
			llm.TaskEdition: {Provider: "a", Model: "same-id"},
			llm.TaskSummary: {Provider: "b", Model: "same-id"},
		},
		Models: map[string]llm.ModelInfo{
			"same-id":                    {InputPer1M: 1},
			llm.ModelKey("a", "same-id"): {InputPer1M: 2},
			llm.ModelKey("b", "same-id"): {InputPer1M: 3},
		},
	}
	snapshot := editionBatchConfig(cfg)
	for provider, want := range map[string]llm.USD{"a": 2, "b": 3} {
		got, ok := llm.LookupModel(snapshot.Models, provider, "same-id")
		if !ok || got.InputPer1M != want {
			t.Fatalf("%s original price lost: %+v", provider, got)
		}
	}
}

func TestLLMScopedDefaultsPersist(t *testing.T) {
	f := newCoreFixture(t, storetest.NewTemplate(t))
	in := llmSettingsTestInput()
	in.Defaults = &llm.DefaultRoutes{Embedding: llm.TaskConfig{Provider: "local", Model: "text-embedding-qwen3-8b"}}
	in.Tasks[0] = LLMTaskSettings{Task: "embed", Mode: "default"}
	in.Models[0].Name = "text-embedding-qwen3-8b"
	in.Models[0].Provider = "local"
	in.Models[0].Dimensions = 4096
	in.Models[0].SupportsCustomDimensions = true
	in.Models[0].EmbeddingPer1M = "0.012500"
	in.Models[0].InputPer1M = "0.000000"
	in.Models[0].OutputPer1M = "0.000000"
	if _, err := f.s.PutLLMSettings(t.Context(), f.a, in); err != nil {
		t.Fatal(err)
	}
	got, err := f.s.GetLLMSettings(t.Context(), f.a)
	if err != nil || got.Defaults == nil || got.Defaults.Embedding != in.Defaults.Embedding {
		t.Fatalf("persisted defaults: %+v %v", got, err)
	}
	if len(got.Models) != 1 || got.Models[0] != in.Models[0] || got.Tasks[0].Mode != "default" || got.Tasks[0].Model != in.Models[0].Name {
		t.Fatalf("persisted metadata and effective route: %+v", got)
	}
	for _, task := range got.Tasks[1:] {
		if task.Provider != "" || task.Model != "" {
			t.Fatal("saving a default enabled another feature")
		}
	}
}

func TestLLMBatchSnapshotDoesNotInventMissingPrices(t *testing.T) {
	cfg := llm.Config{Tasks: map[llm.Task]llm.TaskConfig{llm.TaskEdition: {Provider: "test", Model: "unpriced"}}}
	if _, found := llm.LookupModel(editionBatchConfig(cfg).Models, "test", "unpriced"); found {
		t.Fatal("snapshot invented a free price for an unpriced route")
	}
}
