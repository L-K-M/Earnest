package llm

import (
	"context"
	"errors"
	"fmt"
)

// Client is the pipeline-facing LLM client: routing, budgets, spend
// recording and embedding normalisation. The model for every call comes from
// the router's configuration, never from the caller.
type Client struct {
	router *Router
	budget *Budget
	models map[string]ModelInfo
}

// NewClient returns a client over router and budget. Both are required.
func NewClient(router *Router, budget *Budget, models map[string]ModelInfo) (*Client, error) {
	if router == nil {
		return nil, errors.New("llm: router is required")
	}
	if budget == nil {
		return nil, errors.New("llm: budget is required")
	}
	return &Client{router: router, budget: budget, models: models}, nil
}

// Complete resolves task, checks the budget, calls the provider and records
// spend. Invalid structured output is an error and is never returned. The
// model always comes from the router; a caller-supplied one is ignored.
func (c *Client) Complete(ctx context.Context, task Task, req CompleteRequest) (*Completion, error) {
	route, err := c.router.Task(task)
	if err != nil {
		return nil, err
	}
	req.Model = route.Model
	if err := c.budget.Authorize(ctx, route.Provider.Name()); err != nil {
		return nil, err
	}
	completion, err := route.Provider.Complete(ctx, req)
	if err != nil {
		return nil, err
	}
	if err := c.budget.Record(ctx, route.Provider.Name(), c.cost(route.Model, completion.Usage, false)); err != nil {
		return nil, err
	}
	return completion, nil
}

// Embed resolves task, checks the budget, calls the provider, normalises the
// vectors to StorageDimensions and records spend. A zero Dimensions asks the
// provider for the storage width, which is what the schema stores.
func (c *Client) Embed(ctx context.Context, task Task, req EmbedRequest) ([]Vector, Usage, error) {
	route, err := c.router.Task(task)
	if err != nil {
		return nil, Usage{}, err
	}
	req.Model = route.Model
	info := c.models[route.Model]
	if req.Dimensions == 0 {
		req.Dimensions = StorageDimensions
	}
	if info.Dimensions > 0 && req.Dimensions > info.Dimensions {
		return nil, Usage{}, fmt.Errorf("llm: task %s requests %d dimensions but model %s produces %d",
			task, req.Dimensions, route.Model, info.Dimensions)
	}
	if err := c.budget.Authorize(ctx, route.Provider.Name()); err != nil {
		return nil, Usage{}, err
	}
	result, err := route.Provider.Embed(ctx, req)
	if err != nil {
		return nil, Usage{}, err
	}
	vectors := make([]Vector, 0, len(result.Vectors))
	for _, raw := range result.Vectors {
		vector, err := NormaliseEmbedding(raw, info.Truncatable)
		if err != nil {
			return nil, Usage{}, err
		}
		vectors = append(vectors, vector)
	}
	if err := c.budget.Record(ctx, route.Provider.Name(), c.cost(route.Model, result.Usage, true)); err != nil {
		return nil, Usage{}, err
	}
	return vectors, result.Usage, nil
}

// cost prices a usage against the model's configured rates. An unpriced
// model costs nothing, which the router rejects at construction time.
func (c *Client) cost(model string, usage Usage, embedding bool) USD {
	info, ok := c.models[model]
	if !ok {
		return 0
	}
	if embedding {
		return info.EmbeddingPer1M.MulTokens(usage.Input)
	}
	return info.InputPer1M.MulTokens(usage.Input) + info.OutputPer1M.MulTokens(usage.Output)
}
