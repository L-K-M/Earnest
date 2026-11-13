package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/L-K-M/Earnest/internal/llm"
	"github.com/L-K-M/Earnest/internal/store"
	"github.com/jackc/pgx/v5"
)

const llmSettingsPurpose = "earnest/llm-settings/v1"

var llmTasks = []llm.Task{llm.TaskEmbed, llm.TaskClassify, llm.TaskBlurb, llm.TaskSummary, llm.TaskEdition, llm.TaskCompileRule, llm.TaskAsk}

// LLMSettings is the redacted, server-wide configuration. It never contains keys.
// Revision zero represents the deployment's environment defaults.
type LLMSettings struct {
	Revision  int64                 `json:"revision"`
	Providers []LLMProviderSettings `json:"providers"`
	Tasks     []LLMTaskSettings     `json:"tasks"`
	Models    []LLMModelSettings    `json:"models"`
	Defaults  *llm.DefaultRoutes    `json:"defaults,omitempty"`
}

type LLMProviderSettings struct {
	Name            string `json:"name"`
	Kind            string `json:"kind"`
	BaseURL         string `json:"base_url"`
	HasAPIKey       bool   `json:"has_api_key"`
	MonthlyLimitUSD string `json:"monthly_limit_usd"`
	TimeoutSeconds  int    `json:"timeout_seconds"`
	TokenField      string `json:"token_field"`
}

// LLMProviderInput keeps a key when APIKey is empty. ClearAPIKey explicitly
// removes it. Changing the destination requires replacement or removal.
type LLMProviderInput struct {
	Name            string `json:"name"`
	Kind            string `json:"kind"`
	BaseURL         string `json:"base_url"`
	APIKey          string `json:"api_key,omitempty"`
	ClearAPIKey     bool   `json:"clear_api_key,omitempty"`
	MonthlyLimitUSD string `json:"monthly_limit_usd"`
	TimeoutSeconds  int    `json:"timeout_seconds"`
	TokenField      string `json:"token_field"`
}

type LLMTaskSettings struct {
	Task     string `json:"task"`
	Mode     string `json:"mode,omitempty" enum:"off,default,custom"`
	Provider string `json:"provider"`
	Model    string `json:"model"`
}

// Prices are decimal strings, not floating point, matching the budget layer.
type LLMModelSettings struct {
	Name                     string `json:"name"`
	Provider                 string `json:"provider,omitempty"`
	SupportsCustomDimensions bool   `json:"supports_custom_dimensions,omitempty"`
	InputPer1M               string `json:"input_per_1m"`
	OutputPer1M              string `json:"output_per_1m"`
	EmbeddingPer1M           string `json:"embedding_per_1m"`
	Dimensions               int    `json:"dimensions"`
	Truncatable              bool   `json:"truncatable"`
}

type LLMSettingsInput struct {
	Revision  int64              `json:"revision"`
	Providers []LLMProviderInput `json:"providers"`
	Tasks     []LLMTaskSettings  `json:"tasks"`
	Models    []LLMModelSettings `json:"models"`
	Defaults  *llm.DefaultRoutes `json:"defaults,omitempty"`
	OpID      string             `json:"op_id"`
}

func (s *Service) llmSettingsActor(ctx context.Context, a Actor) error {
	if a.Kind != PrincipalHuman {
		return ErrForbidden
	}
	return s.checkActor(ctx, a, ScopeAdmin)
}

func (s *Service) GetLLMSettings(ctx context.Context, a Actor) (LLMSettings, error) {
	if err := s.llmSettingsActor(ctx, a); err != nil {
		return LLMSettings{}, err
	}
	cfg, rev, err := s.loadLLMSettings(ctx, s.store.Pool())
	if err != nil {
		return LLMSettings{}, err
	}
	return redactedLLMSettings(cfg, rev), nil
}

// PutLLMSettings replaces the complete configuration atomically. Only the
// redacted response enters the operation log, never the input or plaintext keys.
func (s *Service) PutLLMSettings(ctx context.Context, a Actor, in LLMSettingsInput) (LLMSettings, error) {
	if err := s.llmSettingsActor(ctx, a); err != nil {
		return LLMSettings{}, err
	}
	if !validOpID(in.OpID) || in.Revision < 0 {
		return LLMSettings{}, fmt.Errorf("op_id and nonnegative revision required: %w", ErrInvalid)
	}
	var out LLMSettings
	err := s.store.WithTx(ctx, func(tx pgx.Tx) error {
		// This also serializes the first insert when no settings row exists yet.
		if err := store.LockCore(ctx, tx); err != nil {
			return err
		}
		replay, err := replayOp(ctx, tx, a.PrincipalID, "PutLLMSettings", in.OpID)
		if err != nil {
			return err
		}
		if replay != nil {
			return json.Unmarshal(replay, &out)
		}
		previous, revision, err := s.loadLLMSettings(ctx, tx)
		if err != nil {
			return err
		}
		if in.Revision != revision {
			return fmt.Errorf("LLM settings changed; reload before saving: %w", ErrConflict)
		}
		cfg, err := validateLLMSettings(in, previous)
		if err != nil {
			return err
		}
		if revision == 0 {
			if err := s.retainLegacyEditionConfig(ctx, tx, previous); err != nil {
				return err
			}
		}
		// Build without contacting a provider. Reject incompatible routing before
		// committing, so saving cannot install a runtime that fails to start.
		if _, err := s.buildModelRuntime(cfg, revision+1); err != nil {
			return fmt.Errorf("LLM routing is incompatible with the provider or model: %w", ErrInvalid)
		}
		raw, err := json.Marshal(cfg)
		if err != nil {
			return fmt.Errorf("encode LLM settings: %w", err)
		}
		envelope, err := s.sealSecret(raw, llmSettingsPurpose)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `INSERT INTO llm_settings(singleton,revision,config_encrypted) VALUES(true,$1,$2)
   ON CONFLICT(singleton) DO UPDATE SET revision=EXCLUDED.revision,config_encrypted=EXCLUDED.config_encrypted`, revision+1, envelope); err != nil {
			return fmt.Errorf("store LLM settings: %w", err)
		}
		out = redactedLLMSettings(cfg, revision+1)
		return store.FinishOp(ctx, tx, a.PrincipalID, in.OpID, out)
	})
	return out, err
}

type llmSettingsQuery interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}

func (s *Service) loadLLMSettings(ctx context.Context, q llmSettingsQuery) (llm.Config, int64, error) {
	var revision int64
	var envelope []byte
	err := q.QueryRow(ctx, "SELECT revision,config_encrypted FROM llm_settings WHERE singleton").Scan(&revision, &envelope)
	if errors.Is(err, pgx.ErrNoRows) {
		return s.modelDefaults, 0, nil
	}
	if err != nil {
		return llm.Config{}, 0, fmt.Errorf("load LLM settings: %w", err)
	}
	raw, err := s.openSecret(envelope, llmSettingsPurpose)
	if err != nil {
		return llm.Config{}, 0, fmt.Errorf("decrypt LLM settings: %w", ErrUnavailable)
	}
	var cfg llm.Config
	if err := json.Unmarshal(raw, &cfg); err != nil {
		// JSON decoder errors can contain plaintext input, including credentials.
		return llm.Config{}, 0, fmt.Errorf("decode LLM settings: %w", ErrUnavailable)
	}
	return cfg, revision, nil
}

func redactedLLMSettings(cfg llm.Config, revision int64) LLMSettings {
	out := LLMSettings{Defaults: cfg.Defaults, Revision: revision, Providers: []LLMProviderSettings{}, Tasks: []LLMTaskSettings{}, Models: []LLMModelSettings{}}
	for name, p := range cfg.Providers {
		timeout := int((p.Timeout + time.Second - 1) / time.Second)
		if timeout <= 0 {
			timeout = 60
		}
		field := p.TokenField
		if field == "" {
			field = llm.TokenFieldCompletion
		}
		out.Providers = append(out.Providers, LLMProviderSettings{Name: name, Kind: string(p.Kind), BaseURL: redactedLLMBaseURL(p.BaseURL), HasAPIKey: p.APIKey != "", MonthlyLimitUSD: p.MonthlyLimitUSD.String(), TimeoutSeconds: timeout, TokenField: string(field)})
	}
	slices.SortFunc(out.Providers, func(a, b LLMProviderSettings) int { return strings.Compare(a.Name, b.Name) })
	for _, task := range llmTasks {
		route := cfg.Tasks[task]
		out.Tasks = append(out.Tasks, LLMTaskSettings{Task: string(task), Mode: cfg.TaskModes[task], Provider: route.Provider, Model: route.Model})
	}
	for key, m := range cfg.Models {
		provider, name := llm.SplitModelKey(key)
		out.Models = append(out.Models, LLMModelSettings{Provider: provider, SupportsCustomDimensions: m.SupportsCustomDimensions, Name: name, InputPer1M: m.InputPer1M.String(), OutputPer1M: m.OutputPer1M.String(), EmbeddingPer1M: m.EmbeddingPer1M.String(), Dimensions: m.Dimensions, Truncatable: m.Truncatable})
	}
	slices.SortFunc(out.Models, func(a, b LLMModelSettings) int {
		return strings.Compare(llm.ModelKey(a.Provider, a.Name), llm.ModelKey(b.Provider, b.Name))
	})
	return out
}

// Environment defaults predate the Settings URL validator. Do not expose
// credentials or query tokens from a legacy deployment URL to any client.
func redactedLLMBaseURL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	u.User, u.RawQuery, u.Fragment = nil, "", ""
	u.ForceQuery = false
	return u.String()
}

func validateLLMSettings(in LLMSettingsInput, previous llm.Config) (llm.Config, error) {
	cfg := llm.Config{Providers: map[string]llm.ProviderConfig{}, Tasks: map[llm.Task]llm.TaskConfig{}, Models: map[string]llm.ModelInfo{}, Defaults: in.Defaults, TaskModes: map[llm.Task]string{}}
	if len(in.Providers) > 32 || len(in.Models) > 128 || len(in.Tasks) > len(llmTasks) {
		return cfg, fmt.Errorf("too many providers, models or tasks: %w", ErrInvalid)
	}
	for _, p := range in.Providers {
		if !validLLMProviderName(p.Name) {
			return cfg, fmt.Errorf("provider name must use lowercase letters, digits and single dashes (1-64 characters): %w", ErrInvalid)
		}
		if _, exists := cfg.Providers[p.Name]; exists {
			return cfg, fmt.Errorf("duplicate provider name: %w", ErrInvalid)
		}
		kind := llm.ProviderKind(p.Kind)
		if kind != llm.KindOpenAI && kind != llm.KindAnthropic && kind != llm.KindGemini {
			return cfg, fmt.Errorf("provider kind: %w", ErrInvalid)
		}
		base, err := llmBaseURL(p.BaseURL)
		if err != nil {
			return cfg, err
		}
		limit, err := llmSettingsMoney(p.MonthlyLimitUSD)
		if err != nil {
			return cfg, fmt.Errorf("provider monthly limit: %w", ErrInvalid)
		}
		if p.TimeoutSeconds < 1 || p.TimeoutSeconds > 600 {
			return cfg, fmt.Errorf("provider timeout must be 1-600 seconds: %w", ErrInvalid)
		}
		field := llm.TokenField(p.TokenField)
		if field != llm.TokenFieldCompletion && field != llm.TokenFieldLegacy {
			return cfg, fmt.Errorf("provider token field: %w", ErrInvalid)
		}
		key := strings.TrimSpace(p.APIKey)
		if len(key) > 8192 || strings.ContainsFunc(key, unicode.IsControl) {
			return cfg, fmt.Errorf("provider API key: %w", ErrInvalid)
		}
		if p.ClearAPIKey && key != "" {
			return cfg, fmt.Errorf("replace or clear the API key, not both: %w", ErrInvalid)
		}
		old := previous.Providers[p.Name]
		if key == "" && !p.ClearAPIKey {
			if old.APIKey != "" && (strings.TrimRight(strings.TrimSpace(old.BaseURL), "/") != base || old.Kind != kind) {
				return cfg, fmt.Errorf("changing provider URL or kind requires a replacement API key or explicit key removal: %w", ErrInvalid)
			}
			key = old.APIKey
		}
		cfg.Providers[p.Name] = llm.ProviderConfig{Kind: kind, BaseURL: base, APIKey: key, MonthlyLimitUSD: limit, Timeout: time.Duration(p.TimeoutSeconds) * time.Second, TokenField: field}
	}
	for _, m := range in.Models {
		if !validLLMModelName(m.Name) {
			return cfg, fmt.Errorf("model name must be 1-256 characters without whitespace: %w", ErrInvalid)
		}
		if m.Provider != "" {
			if _, exists := cfg.Providers[m.Provider]; !exists {
				return cfg, fmt.Errorf("model provider is not configured: %w", ErrInvalid)
			}
		}
		key := llm.ModelKey(m.Provider, m.Name)
		if _, exists := cfg.Models[key]; exists {
			return cfg, fmt.Errorf("duplicate model name: %w", ErrInvalid)
		}
		input, e1 := llmSettingsMoney(m.InputPer1M)
		output, e2 := llmSettingsMoney(m.OutputPer1M)
		embedding, e3 := llmSettingsMoney(m.EmbeddingPer1M)
		if e1 != nil || e2 != nil || e3 != nil {
			return cfg, fmt.Errorf("model prices must be nonnegative decimal USD with at most six decimal places: %w", ErrInvalid)
		}
		if m.Dimensions < 0 || m.Dimensions > 65536 || (m.Dimensions > llm.StorageDimensions && !m.Truncatable && !m.SupportsCustomDimensions) {
			return cfg, fmt.Errorf("model embedding dimensions require a supported width or truncation: %w", ErrInvalid)
		}
		cfg.Models[key] = llm.ModelInfo{SupportsCustomDimensions: m.SupportsCustomDimensions, InputPer1M: input, OutputPer1M: output, EmbeddingPer1M: embedding, Dimensions: m.Dimensions, Truncatable: m.Truncatable}
	}
	if in.Defaults != nil {
		for _, def := range []struct {
			route llm.TaskConfig
			task  llm.Task
		}{{in.Defaults.Text, llm.TaskBlurb}, {in.Defaults.Embedding, llm.TaskEmbed}} {
			if def.route.Provider == "" && def.route.Model == "" {
				continue
			}
			if err := validateLLMRoute(cfg, def.route, def.task); err != nil {
				return cfg, err
			}
		}
	}
	seen := map[llm.Task]bool{}
	for _, t := range in.Tasks {
		task := llm.Task(t.Task)
		if !slices.Contains(llmTasks, task) || seen[task] {
			return cfg, fmt.Errorf("unknown or duplicate LLM task: %w", ErrInvalid)
		}
		seen[task] = true
		route := llm.TaskConfig{Provider: t.Provider, Model: t.Model}
		switch t.Mode {
		case "": // Older clients send concrete routes, never inheritance.
		case "off":
			route = llm.TaskConfig{}
		case "custom":
		case "default":
			if cfg.Defaults == nil {
				return cfg, fmt.Errorf("choose a default model before enabling this feature: %w", ErrInvalid)
			}
			route = cfg.Defaults.Text
			if task == llm.TaskEmbed {
				route = cfg.Defaults.Embedding
			}
		default:
			return cfg, fmt.Errorf("unknown task mode: %w", ErrInvalid)
		}
		if t.Mode != "" {
			cfg.TaskModes[task] = t.Mode
		}
		if route.Provider == "" && route.Model == "" {
			if t.Mode == "default" || t.Mode == "custom" {
				return cfg, fmt.Errorf("choose a model before enabling this feature: %w", ErrInvalid)
			}
			continue
		}
		if err := validateLLMRoute(cfg, route, task); err != nil {
			return cfg, err
		}
		cfg.Tasks[task] = route
	}
	return cfg, nil
}

func validateLLMRoute(cfg llm.Config, route llm.TaskConfig, task llm.Task) error {
	p, exists := cfg.Providers[route.Provider]
	if !exists || route.Model == "" {
		return fmt.Errorf("each enabled feature needs a configured provider and model: %w", ErrInvalid)
	}
	if _, exists := llm.LookupModel(cfg.Models, route.Provider, route.Model); !exists {
		return fmt.Errorf("model needs explicit price configuration (zero for free models): %w", ErrInvalid)
	}
	if task == llm.TaskEmbed && p.Kind == llm.KindAnthropic {
		return fmt.Errorf("Anthropic does not support embeddings: %w", ErrInvalid)
	}
	return nil
}

func validLLMProviderName(name string) bool {
	if name == "" || len(name) > 64 || strings.HasPrefix(name, "-") || strings.HasSuffix(name, "-") || strings.Contains(name, "--") {
		return false
	}
	for _, c := range name {
		if !(c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '-') {
			return false
		}
	}
	return true
}

func validLLMModelName(name string) bool {
	return name != "" && len(name) <= 256 && !strings.ContainsFunc(name, func(r rune) bool { return unicode.IsSpace(r) || unicode.IsControl(r) })
}

func llmSettingsMoney(text string) (llm.USD, error) {
	value, err := llm.ParseUSD(text)
	// numeric(14,6), shared with the spend ledger.
	if err != nil || value > 99999999999999 {
		return 0, ErrInvalid
	}
	return value, nil
}

func llmBaseURL(raw string) (string, error) {
	base := strings.TrimRight(strings.TrimSpace(raw), "/")
	u, err := url.Parse(base)
	if err != nil || len(base) > 2048 || u == nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.ForceQuery || strings.Contains(base, "#") || u.Opaque != "" {
		return "", fmt.Errorf("provider base URL must be HTTP(S) with a host and no credentials, query or fragment: %w", ErrInvalid)
	}
	if port := u.Port(); port != "" {
		if n, err := strconv.Atoi(port); err != nil || n < 1 || n > 65535 {
			return "", fmt.Errorf("provider base URL port: %w", ErrInvalid)
		}
	}
	return base, nil
}
