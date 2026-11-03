package llm

import (
	"context"
	"errors"
	"testing"
)

// fakeProvider records what the router asked it to do.
type fakeProvider struct {
	name     string
	complete func(CompleteRequest) (*Completion, error)
	embed    func(EmbedRequest) (*EmbedResult, error)
}

func (f *fakeProvider) Name() string { return f.name }

func (f *fakeProvider) Complete(ctx context.Context, req CompleteRequest) (*Completion, error) {
	if f.complete == nil {
		return &Completion{}, nil
	}
	return f.complete(req)
}

func (f *fakeProvider) Embed(ctx context.Context, req EmbedRequest) (*EmbedResult, error) {
	if f.embed == nil {
		return &EmbedResult{}, nil
	}
	return f.embed(req)
}

func TestRouterTask(t *testing.T) {
	provider := &fakeProvider{name: "openai"}
	router := &Router{
		providers: map[string]Provider{"openai": provider},
		tasks:     map[Task]TaskConfig{TaskBlurb: {Provider: "openai", Model: "gpt-test"}},
	}
	route, err := router.Task(TaskBlurb)
	if err != nil {
		t.Fatal(err)
	}
	if route.Provider != provider || route.Model != "gpt-test" {
		t.Errorf("route = %+v", route)
	}
	if !router.Enabled(TaskBlurb) {
		t.Error("blurb should be enabled")
	}
}

func TestRouterDisabledByDefault(t *testing.T) {
	router := &Router{providers: map[string]Provider{}, tasks: map[Task]TaskConfig{}}
	for _, task := range taskOrder {
		if router.Enabled(task) {
			t.Errorf("%s should be disabled", task)
		}
		if _, err := router.Task(task); !errors.Is(err, ErrDisabled) {
			t.Errorf("%s error = %v, want ErrDisabled", task, err)
		}
	}
}

func TestRouterMissingProvider(t *testing.T) {
	router := &Router{
		providers: map[string]Provider{},
		tasks:     map[Task]TaskConfig{TaskSummary: {Provider: "absent", Model: "m"}},
	}
	if _, err := router.Task(TaskSummary); !errors.Is(err, ErrDisabled) {
		t.Fatalf("error = %v, want ErrDisabled", err)
	}
}

func TestNewRouterRejectsUnpricedModel(t *testing.T) {
	_, err := NewRouter(Config{
		Providers: map[string]ProviderConfig{
			"openai": {Kind: KindOpenAI, BaseURL: "https://api.openai.com/v1"},
		},
		Tasks:  map[Task]TaskConfig{TaskBlurb: {Provider: "openai", Model: "unpriced-model"}},
		Models: map[string]ModelInfo{"priced-model": {}},
	}, testOutbound(t))
	if err == nil {
		t.Fatal("expected an error for a model with no price entry")
	}
}

func TestNewRouterBuildsProviders(t *testing.T) {
	router, err := NewRouter(Config{
		Providers: map[string]ProviderConfig{
			"openai": {Kind: KindOpenAI, BaseURL: "https://api.openai.com/v1"},
		},
		Tasks:  map[Task]TaskConfig{TaskEmbed: {Provider: "openai", Model: "text-embedding-3-small"}},
		Models: map[string]ModelInfo{"text-embedding-3-small": {}},
	}, testOutbound(t))
	if err != nil {
		t.Fatal(err)
	}
	route, err := router.Task(TaskEmbed)
	if err != nil {
		t.Fatal(err)
	}
	if route.Provider.Name() != "openai" {
		t.Errorf("provider = %q", route.Provider.Name())
	}
}

func TestNewRouterRejectsBadBaseURL(t *testing.T) {
	for _, baseURL := range []string{"", "ftp://example.com", "https://"} {
		t.Run(baseURL, func(t *testing.T) {
			_, err := NewRouter(Config{
				Providers: map[string]ProviderConfig{"p": {Kind: KindOpenAI, BaseURL: baseURL}},
			}, testOutbound(t))
			if err == nil {
				t.Fatalf("expected an error for base URL %q", baseURL)
			}
		})
	}
}
