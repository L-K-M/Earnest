package llm

import (
	"context"
	"errors"
	"testing"
)

func TestCheckModelEmbeddingResizeAndBudget(t *testing.T) {
	for _, resize := range []bool{false, true} {
		t.Run(map[bool]string{false: "ignores dimensions", true: "resizes"}[resize], func(t *testing.T) {
			var requested []int
			provider := &fakeProvider{name: "local", embed: func(req EmbedRequest) (*EmbedResult, error) {
				requested = append(requested, req.Dimensions)
				if len(req.Texts) != 1 || req.Texts[0] != "Earnest model compatibility check." {
					t.Fatal("unexpected sample")
				}
				width := 4096
				if resize && req.Dimensions > 0 {
					width = req.Dimensions
				}
				return &EmbedResult{Vectors: [][]float32{make([]float32, width)}, Usage: Usage{Input: 10}}, nil
			}}
			client, spend := newTestClient(t, provider, nil, map[string]ModelInfo{ModelKey("local", "test-model"): {EmbeddingPer1M: 1000000}})
			client.router.tasks[TaskEmbed] = client.router.tasks[TaskBlurb]
			got, err := client.CheckModel(context.Background(), TaskEmbed)
			if err != nil || got.Compatible != resize || got.Dimensions != 4096 || got.SupportsCustomDimensions != resize {
				t.Fatalf("result: %+v %v", got, err)
			}
			if len(requested) != 2 || requested[0] != 0 || requested[1] != 1024 {
				t.Fatalf("requests: %v", requested)
			}
			if len(spend.adds) != 2 || spend.adds[0] != 10 || spend.adds[1] != 10 {
				t.Fatalf("accounting: %v", spend.adds)
			}
		})
	}
}

func TestClientScopedModelPrice(t *testing.T) {
	provider := &fakeProvider{name: "local", complete: func(CompleteRequest) (*Completion, error) {
		return &Completion{Text: "Ready", Usage: Usage{Input: 100}}, nil
	}}
	client, spend := newTestClient(t, provider, nil, map[string]ModelInfo{"test-model": {InputPer1M: 1000000}, ModelKey("local", "test-model"): {InputPer1M: 2000000}})
	got, err := client.CheckModel(context.Background(), TaskBlurb)
	if err != nil || !got.Compatible || len(spend.adds) != 1 || spend.adds[0] != 200 {
		t.Fatalf("scoped pricing: %+v %v %v", got, err, spend.adds)
	}
}

func TestResizeSupportDoesNotPermitLocalTruncation(t *testing.T) {
	provider := &fakeProvider{name: "local", embed: func(EmbedRequest) (*EmbedResult, error) {
		return &EmbedResult{Vectors: [][]float32{make([]float32, 4096)}}, nil
	}}
	client, _ := newTestClient(t, provider, nil, map[string]ModelInfo{"test-model": {Dimensions: 4096, SupportsCustomDimensions: true}})
	_, _, err := client.Embed(context.Background(), TaskBlurb, EmbedRequest{Texts: []string{"example"}})
	if !errors.Is(err, ErrDimensions) {
		t.Fatalf("silently truncated: %v", err)
	}
}
