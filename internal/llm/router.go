package llm

import (
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"github.com/L-K-M/Earnest/internal/outbound"
)

// Route is a resolved task: the provider to call and the model to use.
type Route struct {
	Provider Provider
	Model    string
}

// Router resolves tasks to configured providers and models. A task with no
// configuration is disabled, which is the default for the whole deployment.
type Router struct {
	providers map[string]Provider
	tasks     map[Task]TaskConfig
}

// NewRouter constructs the providers described by cfg and returns a router
// over them. It fails when a configured task names a model with no price
// entry, so an unpriced model cannot be called by accident.
func NewRouter(cfg Config, client *outbound.Client) (*Router, error) {
	if client == nil {
		return nil, errors.New("llm: outbound client is required")
	}
	providers := make(map[string]Provider, len(cfg.Providers))
	for name, providerConfig := range cfg.Providers {
		provider, err := newProvider(name, providerConfig, client)
		if err != nil {
			return nil, err
		}
		providers[name] = provider
	}
	for task, taskConfig := range cfg.Tasks {
		provider := cfg.Providers[taskConfig.Provider]
		info, priced := LookupModel(cfg.Models, taskConfig.Provider, taskConfig.Model)
		if task == TaskEmbed && provider.Kind == KindGemini && provider.MonthlyLimitUSD > 0 && info.EmbeddingPer1M > 0 {
			return nil, fmt.Errorf("%w: capped Gemini embeddings require billable token counts", ErrUnsupported)
		}
		if !priced {
			return nil, fmt.Errorf("llm: task %s model %q has no price configuration", task, taskConfig.Model)
		}
	}
	return &Router{providers: providers, tasks: cfg.Tasks}, nil
}

// Task returns the route for task. An unconfigured task is ErrDisabled.
func (r *Router) Task(task Task) (Route, error) {
	taskConfig, ok := r.tasks[task]
	if !ok {
		return Route{}, fmt.Errorf("%w: %s", ErrDisabled, task)
	}
	provider, ok := r.providers[taskConfig.Provider]
	if !ok {
		return Route{}, fmt.Errorf("%w: %s provider %q is missing", ErrDisabled, task, taskConfig.Provider)
	}
	return Route{Provider: provider, Model: taskConfig.Model}, nil
}

// Enabled reports whether task has a complete configuration.
func (r *Router) Enabled(task Task) bool {
	_, err := r.Task(task)
	return err == nil
}

// newProvider builds one provider from its configuration.
func newProvider(name string, cfg ProviderConfig, client *outbound.Client) (Provider, error) {
	base := strings.TrimRight(strings.TrimSpace(cfg.BaseURL), "/")
	if base == "" {
		return nil, fmt.Errorf("llm: provider %s has no base URL", name)
	}
	parsed, err := url.Parse(base)
	if err != nil {
		return nil, fmt.Errorf("llm: provider %s base URL: %w", name, err)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return nil, fmt.Errorf("llm: provider %s base URL must use http or https", name)
	}
	if parsed.Hostname() == "" {
		return nil, fmt.Errorf("llm: provider %s base URL must name a host", name)
	}
	ports, err := baseExtraPorts(parsed)
	if err != nil {
		return nil, fmt.Errorf("llm: provider %s: %w", name, err)
	}
	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = defaultRequestTimeout
	}
	switch cfg.Kind {
	case KindOpenAI:
		return &OpenAI{name: name, baseURL: base, apiKey: cfg.APIKey, tokenField: cfg.TokenField, client: client, timeout: timeout, extraPorts: ports}, nil
	case KindAnthropic:
		return &Anthropic{name: name, baseURL: base, apiKey: cfg.APIKey, client: client, timeout: timeout, extraPorts: ports}, nil
	case KindGemini:
		return &Gemini{name: name, baseURL: base, apiKey: cfg.APIKey, client: client, timeout: timeout, extraPorts: ports}, nil
	default:
		return nil, fmt.Errorf("llm: provider %s has unknown kind %q", name, cfg.Kind)
	}
}

// baseExtraPorts returns the non-standard port in u, which outbound requires
// before it will dial a local model server. Standard ports need no entry.
func baseExtraPorts(u *url.URL) ([]uint16, error) {
	port := u.Port()
	if port == "" {
		return nil, nil
	}
	number, err := strconv.Atoi(port)
	if err != nil || number < 1 || number > 65535 {
		return nil, fmt.Errorf("invalid port %q", port)
	}
	if number == 80 || number == 443 {
		return nil, nil
	}
	return []uint16{uint16(number)}, nil
}
