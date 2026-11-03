package llm

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"
)

// Configuration variables. Every task is disabled unless both its provider
// and model are set, so an unconfigured deployment makes no calls and spends
// nothing.
const (
	envProviderPrefix = "EARNEST_LLM_PROVIDER_"
	envTaskPrefix     = "EARNEST_LLM_TASK_"
	envModels         = "EARNEST_LLM_MODELS"
)

// providerSuffixes lists the provider setting suffixes. Provider names
// are lowercased, so a name may itself end with a suffix; the whole suffix
// is always the last one.
var providerSuffixes = []string{"_BASE_URL", "_API_KEY", "_LIMIT_USD", "_TOKEN_FIELD", "_TIMEOUT", "_KIND"}

// Task is a pipeline purpose that gets its own provider and model.
type Task string

const (
	TaskEmbed       Task = "embed"
	TaskClassify    Task = "classify"
	TaskBlurb       Task = "blurb"
	TaskSummary     Task = "summary"
	TaskEdition     Task = "edition"
	TaskCompileRule Task = "compile_rule"
	TaskAsk         Task = "ask"
)

// taskOrder lists every task the router knows, for enumeration and tests.
var taskOrder = []Task{
	TaskEmbed, TaskClassify, TaskBlurb, TaskSummary, TaskEdition, TaskCompileRule, TaskAsk,
}

// ProviderKind selects the wire dialect.
type ProviderKind string

const (
	// KindOpenAI is the OpenAI chat-completions and embeddings dialect,
	// which Ollama, llama.cpp and similar servers also speak.
	KindOpenAI ProviderKind = "openai"
	// KindAnthropic is the Anthropic Messages dialect.
	KindAnthropic ProviderKind = "anthropic"
	// KindGemini is Google's generateContent and embedContent dialect.
	KindGemini ProviderKind = "gemini"
)

// TokenField selects the request field carrying the completion token
// budget. OpenAI's current models want max_completion_tokens; most
// OpenAI-compatible servers still read max_tokens.
type TokenField string

const (
	TokenFieldCompletion TokenField = "max_completion_tokens"
	TokenFieldLegacy     TokenField = "max_tokens"
)

// Config is the LLM layer configuration, loaded from EARNEST_LLM_* variables.
type Config struct {
	Providers map[string]ProviderConfig
	Tasks     map[Task]TaskConfig
	Models    map[string]ModelInfo
}

// ProviderConfig configures one named provider.
type ProviderConfig struct {
	Kind    ProviderKind
	BaseURL string
	APIKey  string
	// MonthlyLimitUSD is the hard monthly spend cap. Zero means unlimited.
	MonthlyLimitUSD USD
	// Timeout bounds one request. Zero uses defaultRequestTimeout.
	Timeout time.Duration
	// TokenField selects the completion token budget field.
	TokenField TokenField
}

// TaskConfig routes one task to a provider and model.
type TaskConfig struct {
	Provider string
	Model    string
}

// ModelInfo is a model's price and embedding geometry.
type ModelInfo struct {
	InputPer1M     USD
	OutputPer1M    USD
	EmbeddingPer1M USD
	// Dimensions is the model's native embedding width, or zero when
	// unknown.
	Dimensions int
	// Truncatable allows vectors longer than StorageDimensions to be cut.
	// Models that are not Matryoshka-style must leave it false.
	Truncatable bool
}

// defaultModels carries the prices from docs/research/llm.md's cost table
// and the geometry of the local embedding models it names. Operators
// override entries and add their own with EARNEST_LLM_MODELS.
var defaultModels = map[string]ModelInfo{
	"text-embedding-3-small": {EmbeddingPer1M: 20000, Dimensions: 1536, Truncatable: true},
	"claude-haiku-5-5":       {InputPer1M: 100000, OutputPer1M: 500000},
	"claude-sonnet-5-5":      {InputPer1M: 2000000, OutputPer1M: 10000000},
	"nomic-embed-text":       {Dimensions: 768},
	"bge-m3":                 {Dimensions: 1024},
}

// Limits returns the configured monthly cap per provider. A provider
// capped at zero, or absent from the result, has no cap.
func (c Config) Limits() map[string]USD {
	limits := make(map[string]USD, len(c.Providers))
	for name, provider := range c.Providers {
		limits[name] = provider.MonthlyLimitUSD
	}
	return limits
}

// Load reads the EARNEST_LLM_* variables into a validated Config. With no
// variables set every task is disabled.
func Load() (Config, error) {
	providers, err := loadProviders()
	if err != nil {
		return Config{}, err
	}
	models, err := loadModels()
	if err != nil {
		return Config{}, err
	}
	tasks, err := loadTasks(providers, models)
	if err != nil {
		return Config{}, err
	}
	return Config{Providers: providers, Tasks: tasks, Models: models}, nil
}

// loadProviders scans the EARNEST_LLM_PROVIDER_* variables. A provider is
// usable only with a kind and a base URL; the key may be empty for a local
// server that needs none.
func loadProviders() (map[string]ProviderConfig, error) {
	providers := map[string]ProviderConfig{}
	for _, entry := range os.Environ() {
		name, value, _ := strings.Cut(entry, "=")
		if !strings.HasPrefix(name, envProviderPrefix) {
			continue
		}
		if strings.TrimSpace(value) == "" {
			continue
		}
		rest := strings.TrimPrefix(name, envProviderPrefix)
		suffix, ok := providerSuffix(rest)
		if !ok {
			return nil, fmt.Errorf("%s%s: unknown provider setting", envProviderPrefix, rest)
		}
		providerName := strings.ToLower(rest[:len(rest)-len(suffix)])
		if !validProviderName(providerName) {
			return nil, fmt.Errorf("%s%s: provider names are letters, digits and dashes", envProviderPrefix, rest)
		}
		config, exists := providers[providerName]
		if !exists {
			config = ProviderConfig{TokenField: TokenFieldCompletion}
		}
		switch suffix {
		case "_KIND":
			kind := ProviderKind(strings.ToLower(strings.TrimSpace(value)))
			switch kind {
			case KindOpenAI, KindAnthropic, KindGemini:
				config.Kind = kind
			default:
				return nil, fmt.Errorf("%s%s%s: unknown kind %q", envProviderPrefix, providerName, suffix, value)
			}
		case "_BASE_URL":
			config.BaseURL = strings.TrimSpace(value)
		case "_API_KEY":
			config.APIKey = value
		case "_LIMIT_USD":
			limit, err := ParseUSD(value)
			if err != nil {
				return nil, fmt.Errorf("%s%s%s: %w", envProviderPrefix, providerName, suffix, err)
			}
			config.MonthlyLimitUSD = limit
		case "_TIMEOUT":
			timeout, err := time.ParseDuration(strings.TrimSpace(value))
			if err != nil || timeout <= 0 {
				return nil, fmt.Errorf("%s%s%s: invalid duration %q", envProviderPrefix, providerName, suffix, value)
			}
			config.Timeout = timeout
		case "_TOKEN_FIELD":
			switch field := TokenField(strings.TrimSpace(value)); field {
			case TokenFieldCompletion, TokenFieldLegacy:
				config.TokenField = field
			default:
				return nil, fmt.Errorf("%s%s%s: unknown token field %q", envProviderPrefix, providerName, suffix, value)
			}
		}
		providers[providerName] = config
	}
	for name, config := range providers {
		if config.Kind == "" {
			return nil, fmt.Errorf("%s%s_KIND: provider kind is required", envProviderPrefix, name)
		}
		if strings.TrimSpace(config.BaseURL) == "" {
			return nil, fmt.Errorf("%s%s_BASE_URL: provider base URL is required", envProviderPrefix, name)
		}
		if config.TokenField == "" {
			config.TokenField = TokenFieldCompletion
			providers[name] = config
		}
	}
	return providers, nil
}

// providerSuffix reports which provider field rest ends with.
func providerSuffix(rest string) (string, bool) {
	for _, suffix := range providerSuffixes {
		if strings.HasSuffix(rest, suffix) {
			return suffix, true
		}
	}
	return "", false
}

// validProviderName reports whether name is a non-empty word separated by
// single dashes, so env var names stay unambiguous. Names are lowercased.
func validProviderName(name string) bool {
	if name == "" || strings.HasPrefix(name, "-") || strings.HasSuffix(name, "-") || strings.Contains(name, "--") {
		return false
	}
	for _, r := range name {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' {
			continue
		}
		return false
	}
	return true
}

// loadTasks reads the per-task routing. A task is enabled only when both its
// provider and model are set, the provider exists and the model has a price
// entry, so a typo cannot silently make calls on an unpriced model.
func loadTasks(providers map[string]ProviderConfig, models map[string]ModelInfo) (map[Task]TaskConfig, error) {
	tasks := map[Task]TaskConfig{}
	for _, task := range taskOrder {
		prefix := envTaskPrefix + strings.ToUpper(string(task)) + "_"
		provider := strings.ToLower(strings.TrimSpace(os.Getenv(prefix + "PROVIDER")))
		model := strings.TrimSpace(os.Getenv(prefix + "MODEL"))
		if provider == "" && model == "" {
			continue
		}
		if provider == "" || model == "" {
			return nil, fmt.Errorf("%sPROVIDER and %sMODEL must be set together", prefix, prefix)
		}
		if _, ok := providers[provider]; !ok {
			return nil, fmt.Errorf("%sPROVIDER: unknown provider %q", prefix, provider)
		}
		if _, ok := models[model]; !ok {
			return nil, fmt.Errorf("%sMODEL: model %q has no price configuration; add it to %s", prefix, model, envModels)
		}
		tasks[task] = TaskConfig{Provider: provider, Model: model}
	}
	return tasks, nil
}

// modelJSON is one EARNEST_LLM_MODELS entry. Pointer fields distinguish an
// absent override from an explicit zero.
type modelJSON struct {
	InputPer1M     json.Number `json:"input_per_1m"`
	OutputPer1M    json.Number `json:"output_per_1m"`
	EmbeddingPer1M json.Number `json:"embedding_per_1m"`
	Dimensions     *int        `json:"dimensions"`
	Truncatable    *bool       `json:"truncatable"`
}

// loadModels returns the default price table overlaid with
// EARNEST_LLM_MODELS. An overlay entry starts from the default when one
// exists, so a partial entry only overrides the fields it names.
func loadModels() (map[string]ModelInfo, error) {
	models := make(map[string]ModelInfo, len(defaultModels))
	for name, info := range defaultModels {
		models[name] = info
	}
	raw := strings.TrimSpace(os.Getenv(envModels))
	if raw == "" {
		return models, nil
	}
	var overlay map[string]modelJSON
	if err := json.Unmarshal([]byte(raw), &overlay); err != nil {
		return nil, fmt.Errorf("%s: invalid JSON: %w", envModels, err)
	}
	for name, entry := range overlay {
		if strings.TrimSpace(name) == "" {
			return nil, fmt.Errorf("%s: empty model name", envModels)
		}
		info := models[name]
		if err := applyUSD(&info.InputPer1M, entry.InputPer1M, name, "input_per_1m"); err != nil {
			return nil, err
		}
		if err := applyUSD(&info.OutputPer1M, entry.OutputPer1M, name, "output_per_1m"); err != nil {
			return nil, err
		}
		if err := applyUSD(&info.EmbeddingPer1M, entry.EmbeddingPer1M, name, "embedding_per_1m"); err != nil {
			return nil, err
		}
		if entry.Truncatable != nil {
			info.Truncatable = *entry.Truncatable
		}
		if entry.Dimensions != nil {
			if *entry.Dimensions < 0 || (*entry.Dimensions > StorageDimensions && !info.Truncatable) {
				return nil, fmt.Errorf("%s: model %s dimensions %d out of range", envModels, name, *entry.Dimensions)
			}
			info.Dimensions = *entry.Dimensions
		}
		models[name] = info
	}
	return models, nil
}

// applyUSD sets target when a model overlay names the field, leaving the
// default in place when it does not.
func applyUSD(target *USD, number json.Number, model, field string) error {
	if number == "" {
		return nil
	}
	value, err := ParseUSD(number.String())
	if err != nil {
		return fmt.Errorf("%s: model %s %s: %w", envModels, model, field, err)
	}
	*target = value
	return nil
}
