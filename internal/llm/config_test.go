package llm

import (
	"os"
	"strings"
	"testing"
)

// clearLLMEnv blanks every EARNEST_LLM_* variable for the duration of a test.
func clearLLMEnv(t *testing.T) {
	t.Helper()
	for _, entry := range os.Environ() {
		name, _, _ := strings.Cut(entry, "=")
		if strings.HasPrefix(name, "EARNEST_LLM_") {
			t.Setenv(name, "")
		}
	}
}

func TestLoadDisabledByDefault(t *testing.T) {
	clearLLMEnv(t)
	config, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(config.Providers) != 0 || len(config.Tasks) != 0 {
		t.Errorf("config = %+v, want no providers or tasks", config)
	}
	if _, ok := config.Models["text-embedding-3-small"]; !ok {
		t.Error("default price table should be present")
	}
}

func TestLoadProvidersAndTasks(t *testing.T) {
	clearLLMEnv(t)
	t.Setenv("EARNEST_LLM_PROVIDER_OPENAI_KIND", "openai")
	t.Setenv("EARNEST_LLM_PROVIDER_OPENAI_BASE_URL", "https://api.openai.com/v1")
	t.Setenv("EARNEST_LLM_PROVIDER_OPENAI_API_KEY", "sk-test")
	t.Setenv("EARNEST_LLM_PROVIDER_OPENAI_LIMIT_USD", "10.50")
	t.Setenv("EARNEST_LLM_PROVIDER_OLLAMA_KIND", "openai")
	t.Setenv("EARNEST_LLM_PROVIDER_OLLAMA_BASE_URL", "http://ollama:11434/v1")
	t.Setenv("EARNEST_LLM_TASK_EMBED_PROVIDER", "openai")
	t.Setenv("EARNEST_LLM_TASK_EMBED_MODEL", "text-embedding-3-small")
	t.Setenv("EARNEST_LLM_TASK_BLURB_PROVIDER", "ollama")
	t.Setenv("EARNEST_LLM_TASK_BLURB_MODEL", "nomic-embed-text")

	config, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	openai := config.Providers["openai"]
	if openai.Kind != KindOpenAI || openai.BaseURL != "https://api.openai.com/v1" || openai.APIKey != "sk-test" {
		t.Errorf("openai = %+v", openai)
	}
	if openai.MonthlyLimitUSD != USD(10500000) {
		t.Errorf("limit = %v", openai.MonthlyLimitUSD)
	}
	if openai.TokenField != TokenFieldCompletion {
		t.Errorf("token field = %q", openai.TokenField)
	}
	if ollama := config.Providers["ollama"]; ollama.APIKey != "" {
		t.Errorf("ollama key = %q, want empty", ollama.APIKey)
	}
	if got := config.Tasks[TaskEmbed]; got.Provider != "openai" || got.Model != "text-embedding-3-small" {
		t.Errorf("embed task = %+v", got)
	}
	if got := config.Tasks[TaskBlurb]; got.Provider != "ollama" {
		t.Errorf("blurb task = %+v", got)
	}
	if _, ok := config.Tasks[TaskSummary]; ok {
		t.Error("summary should be disabled")
	}
}

func TestConfigLimits(t *testing.T) {
	clearLLMEnv(t)
	t.Setenv("EARNEST_LLM_PROVIDER_A_KIND", "openai")
	t.Setenv("EARNEST_LLM_PROVIDER_A_BASE_URL", "https://a.example.com")
	t.Setenv("EARNEST_LLM_PROVIDER_A_LIMIT_USD", "5")
	t.Setenv("EARNEST_LLM_PROVIDER_B_KIND", "openai")
	t.Setenv("EARNEST_LLM_PROVIDER_B_BASE_URL", "https://b.example.com")
	config, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	limits := config.Limits()
	if limits["a"] != USD(5000000) {
		t.Errorf("a limit = %v", limits["a"])
	}
	if limits["b"] != 0 {
		t.Errorf("b limit = %v, want 0 (unlimited)", limits["b"])
	}
}

func TestLoadRejectsInvalidConfiguration(t *testing.T) {
	tests := []struct {
		name  string
		setup func(t *testing.T)
	}{
		{
			name: "unknown kind",
			setup: func(t *testing.T) {
				t.Setenv("EARNEST_LLM_PROVIDER_P_KIND", "wat")
				t.Setenv("EARNEST_LLM_PROVIDER_P_BASE_URL", "https://example.com")
			},
		},
		{
			name: "missing base URL",
			setup: func(t *testing.T) {
				t.Setenv("EARNEST_LLM_PROVIDER_P_KIND", "openai")
			},
		},
		{
			name: "missing kind",
			setup: func(t *testing.T) {
				t.Setenv("EARNEST_LLM_PROVIDER_P_BASE_URL", "https://example.com")
			},
		},
		{
			name: "task without model",
			setup: func(t *testing.T) {
				t.Setenv("EARNEST_LLM_PROVIDER_P_KIND", "openai")
				t.Setenv("EARNEST_LLM_PROVIDER_P_BASE_URL", "https://example.com")
				t.Setenv("EARNEST_LLM_TASK_EMBED_PROVIDER", "p")
			},
		},
		{
			name: "unknown provider",
			setup: func(t *testing.T) {
				t.Setenv("EARNEST_LLM_TASK_EMBED_PROVIDER", "absent")
				t.Setenv("EARNEST_LLM_TASK_EMBED_MODEL", "text-embedding-3-small")
			},
		},
		{
			name: "unpriced model",
			setup: func(t *testing.T) {
				t.Setenv("EARNEST_LLM_PROVIDER_P_KIND", "openai")
				t.Setenv("EARNEST_LLM_PROVIDER_P_BASE_URL", "https://example.com")
				t.Setenv("EARNEST_LLM_TASK_EMBED_PROVIDER", "p")
				t.Setenv("EARNEST_LLM_TASK_EMBED_MODEL", "unknown-model")
			},
		},
		{
			name: "invalid limit",
			setup: func(t *testing.T) {
				t.Setenv("EARNEST_LLM_PROVIDER_P_KIND", "openai")
				t.Setenv("EARNEST_LLM_PROVIDER_P_BASE_URL", "https://example.com")
				t.Setenv("EARNEST_LLM_PROVIDER_P_LIMIT_USD", "-1")
			},
		},
		{
			name: "invalid timeout",
			setup: func(t *testing.T) {
				t.Setenv("EARNEST_LLM_PROVIDER_P_KIND", "openai")
				t.Setenv("EARNEST_LLM_PROVIDER_P_BASE_URL", "https://example.com")
				t.Setenv("EARNEST_LLM_PROVIDER_P_TIMEOUT", "soon")
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			clearLLMEnv(t)
			test.setup(t)
			if _, err := Load(); err == nil {
				t.Fatal("expected an error")
			}
		})
	}
}

func TestLoadModelsOverlay(t *testing.T) {
	clearLLMEnv(t)
	t.Setenv(envModels, `{
		"my-model": {"input_per_1m": 1.5, "output_per_1m": 2, "dimensions": 512, "truncatable": true},
		"text-embedding-3-small": {"embedding_per_1m": 0.01},
		"claude-haiku-5-5": {"input_per_1m": 0.2}
	}`)
	config, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	custom := config.Models["my-model"]
	if custom.InputPer1M != USD(1500000) || custom.OutputPer1M != USD(2000000) {
		t.Errorf("my-model prices = %+v", custom)
	}
	if custom.Dimensions != 512 || !custom.Truncatable {
		t.Errorf("my-model geometry = %+v", custom)
	}
	// A partial overlay keeps the defaults it does not name.
	small := config.Models["text-embedding-3-small"]
	if small.EmbeddingPer1M != USD(10000) {
		t.Errorf("embedding price = %v", small.EmbeddingPer1M)
	}
	if small.Dimensions != 1536 || !small.Truncatable {
		t.Errorf("embedding geometry = %+v", small)
	}
	// A partial overlay leaves the rates it does not name untouched.
	haiku := config.Models["claude-haiku-5-5"]
	if haiku.InputPer1M != USD(200000) || haiku.OutputPer1M != USD(500000) {
		t.Errorf("haiku prices = %+v", haiku)
	}
}

func TestLoadInvalidModelsJSON(t *testing.T) {
	clearLLMEnv(t)
	t.Setenv(envModels, "{")
	if _, err := Load(); err == nil {
		t.Fatal("expected an error")
	}
}

func TestLoadModelsOversizedDimensions(t *testing.T) {
	for _, tc := range []struct {
		name    string
		raw     string
		model   string
		wantErr bool
	}{
		{"custom truncatable", `{"wide":{"dimensions":1536,"truncatable":true}}`, "wide", false},
		{"inherited truncatable", `{"text-embedding-3-small":{"dimensions":1536}}`, "text-embedding-3-small", false},
		{"not truncatable", `{"wide":{"dimensions":1536}}`, "wide", true},
		{"explicitly not truncatable", `{"text-embedding-3-small":{"dimensions":1536,"truncatable":false}}`, "text-embedding-3-small", true},
		{"negative", `{"wide":{"dimensions":-1,"truncatable":true}}`, "wide", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			clearLLMEnv(t)
			t.Setenv(envModels, tc.raw)
			cfg, err := Load()
			if tc.wantErr {
				if err == nil {
					t.Fatal("invalid dimensions accepted")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			info := cfg.Models[tc.model]
			if info.Dimensions != 1536 || !info.Truncatable {
				t.Fatalf("geometry=%+v", info)
			}

			provider := &fakeProvider{name: "local", embed: func(req EmbedRequest) (*EmbedResult, error) {
				if req.Dimensions != StorageDimensions {
					t.Errorf("requested dimensions=%d, want %d", req.Dimensions, StorageDimensions)
				}
				return &EmbedResult{Vectors: [][]float32{make([]float32, info.Dimensions)}}, nil
			}}
			client, _ := newTestClient(t, provider, nil, map[string]ModelInfo{"test-model": info})
			vectors, _, err := client.Embed(t.Context(), TaskBlurb, EmbedRequest{Texts: []string{"article"}})
			if err != nil || len(vectors) != 1 || len(vectors[0].Values) != StorageDimensions {
				t.Fatalf("embedding count=%d error=%v", len(vectors), err)
			}
		})
	}
}

func TestParseUSD(t *testing.T) {
	tests := []struct {
		in   string
		want USD
		bad  bool
	}{
		{in: "0", want: 0},
		{in: "1", want: 1000000},
		{in: "0.02", want: 20000},
		{in: "10.50", want: 10500000},
		{in: "0.000001", want: 1},
		{in: " 2 ", want: 2000000},
		{in: "", bad: true},
		{in: "-1", bad: true},
		{in: "1.0000001", bad: true},
		{in: "abc", bad: true},
		{in: "1.2.3", bad: true},
	}
	for _, test := range tests {
		t.Run(test.in, func(t *testing.T) {
			got, err := ParseUSD(test.in)
			if test.bad {
				if err == nil {
					t.Fatalf("expected an error, got %v", got)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if got != test.want {
				t.Errorf("ParseUSD(%q) = %d, want %d", test.in, got, test.want)
			}
		})
	}
}

func TestUSDString(t *testing.T) {
	tests := map[USD]string{0: "0.000000", 1: "0.000001", 20000: "0.020000", 10500000: "10.500000"}
	for amount, want := range tests {
		if got := amount.String(); got != want {
			t.Errorf("USD(%d).String() = %q, want %q", amount, got, want)
		}
	}
}

func TestUSDMulTokens(t *testing.T) {
	tests := []struct {
		price  USD
		tokens int
		want   USD
	}{
		{price: 20000, tokens: 1000000, want: 20000},
		{price: 20000, tokens: 500000, want: 10000},
		{price: 100000, tokens: 3, want: 0},
		{price: 100000, tokens: 1, want: 0},
		{price: 1500000, tokens: 1000, want: 1500},
		{price: 0, tokens: 100, want: 0},
	}
	for _, test := range tests {
		if got := test.price.MulTokens(test.tokens); got != test.want {
			t.Errorf("USD(%d).MulTokens(%d) = %d, want %d", test.price, test.tokens, got, test.want)
		}
	}
}

func TestValidProviderName(t *testing.T) {
	valid := []string{"openai", "ollama", "my-provider", "p1"}
	invalid := []string{"", "-x", "x-", "a--b", "a_b", "a.b", "a b"}
	for _, name := range valid {
		if !validProviderName(name) {
			t.Errorf("%q should be valid", name)
		}
	}
	for _, name := range invalid {
		if validProviderName(name) {
			t.Errorf("%q should be invalid", name)
		}
	}
}
