package llm

import (
	"context"
	"errors"
	"testing"
)

// newTestClient wires a router and budget over in-memory fakes.
func newTestClient(t *testing.T, provider Provider, limits map[string]USD, models map[string]ModelInfo) (*Client, *fakeSpendStore) {
	t.Helper()
	router := &Router{
		providers: map[string]Provider{provider.Name(): provider},
		tasks:     map[Task]TaskConfig{TaskBlurb: {Provider: provider.Name(), Model: "test-model"}},
	}
	spend := &fakeSpendStore{}
	client, err := NewClient(router, NewBudget(spend, limits), models)
	if err != nil {
		t.Fatal(err)
	}
	return client, spend
}

func TestClientComplete(t *testing.T) {
	provider := &fakeProvider{
		name: "openai",
		complete: func(req CompleteRequest) (*Completion, error) {
			if req.Model != "test-model" {
				t.Errorf("model = %q, want test-model", req.Model)
			}
			return &Completion{Text: "blurb", Usage: Usage{Input: 100, Output: 50}}, nil
		},
	}
	client, spend := newTestClient(t, provider, map[string]USD{"openai": 1000000}, map[string]ModelInfo{
		"test-model": {InputPer1M: 1000000, OutputPer1M: 2000000},
	})
	completion, err := client.Complete(context.Background(), TaskBlurb, CompleteRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if completion.Text != "blurb" {
		t.Errorf("text = %q", completion.Text)
	}
	// 100 input tokens at $1/1M and 50 output at $2/1M is 0.0002 USD.
	if len(spend.adds) != 1 || spend.adds[0] != 200 {
		t.Errorf("spend = %v, want [200]", spend.adds)
	}
}

func TestClientBudgetExhausted(t *testing.T) {
	var called bool
	provider := &fakeProvider{
		name: "openai",
		complete: func(req CompleteRequest) (*Completion, error) {
			called = true
			return &Completion{}, nil
		},
	}
	spend := &fakeSpendStore{spent: 100}
	router := &Router{
		providers: map[string]Provider{"openai": provider},
		tasks:     map[Task]TaskConfig{TaskBlurb: {Provider: "openai", Model: "test-model"}},
	}
	client, err := NewClient(router, NewBudget(spend, map[string]USD{"openai": 100}), map[string]ModelInfo{"test-model": {}})
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.Complete(context.Background(), TaskBlurb, CompleteRequest{})
	if !errors.Is(err, ErrBudgetExhausted) {
		t.Fatalf("error = %v, want ErrBudgetExhausted", err)
	}
	if called {
		t.Error("provider should not be called once the budget is exhausted")
	}
}

func TestClientDisabledTask(t *testing.T) {
	client, _ := newTestClient(t, &fakeProvider{name: "openai"}, nil, nil)
	_, err := client.Complete(context.Background(), TaskSummary, CompleteRequest{})
	if !errors.Is(err, ErrDisabled) {
		t.Fatalf("error = %v, want ErrDisabled", err)
	}
}

func TestClientEmbedDimensionMismatch(t *testing.T) {
	provider := &fakeProvider{name: "openai"}
	client, _ := newTestClient(t, provider, nil, map[string]ModelInfo{
		"test-model": {Dimensions: 768},
	})
	_, _, err := client.Embed(context.Background(), TaskBlurb, EmbedRequest{Dimensions: StorageDimensions})
	if err == nil {
		t.Fatal("expected an error when the request exceeds the model's width")
	}
}

func TestClientEmbedNormalises(t *testing.T) {
	provider := &fakeProvider{
		name: "openai",
		embed: func(req EmbedRequest) (*EmbedResult, error) {
			if req.Model != "test-model" {
				t.Errorf("model = %q", req.Model)
			}
			return &EmbedResult{
				Vectors: [][]float32{{0.5, -0.25}},
				Usage:   Usage{Input: 100000},
			}, nil
		},
	}
	client, spend := newTestClient(t, provider, nil, map[string]ModelInfo{
		"test-model": {EmbeddingPer1M: 20000},
	})
	vectors, usage, err := client.Embed(context.Background(), TaskBlurb, EmbedRequest{Dimensions: StorageDimensions})
	if err != nil {
		t.Fatal(err)
	}
	if len(vectors) != 1 || len(vectors[0].Values) != StorageDimensions || vectors[0].SourceDimensions != 2 {
		t.Fatalf("vectors = %+v", vectors)
	}
	if usage.Input != 100000 {
		t.Errorf("usage = %v", usage)
	}
	// 100000 tokens at $0.02/1M is 0.002 USD.
	if len(spend.adds) != 1 || spend.adds[0] != 2000 {
		t.Errorf("spend = %v, want [2000]", spend.adds)
	}
}
