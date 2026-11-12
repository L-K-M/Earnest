package llm

import (
	"context"
	"errors"
	"fmt"
	"net"

	"github.com/L-K-M/Earnest/internal/outbound"
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

// CheckBudget reports current availability without reserving spend or calling a
// provider. Each actual call still reserves its input-dependent estimate.
func (c *Client) CheckBudget(ctx context.Context, task Task) error {
	route, err := c.router.Task(task)
	if err != nil {
		return err
	}
	return c.budget.Authorize(ctx, route.Provider.Name())
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
	if req.MaxTokens <= 0 {
		req.MaxTokens = DefaultMaxTokens
	}
	estimate, err := EstimateCompletion(req, ModelFor(c.models, route))
	if err != nil {
		return nil, err
	}
	id, err := c.budget.Reserve(ctx, route.Provider.Name(), estimate)
	if err != nil {
		return nil, err
	}
	completion, err := route.Provider.Complete(ctx, req)
	actual := estimate
	var pricingErr error
	if completion != nil {
		actual, pricingErr = ModelFor(c.models, route).CompletionCost(completion.Usage)
		if pricingErr != nil || completion.Usage == (Usage{}) {
			actual = estimate
		}
	} else if !billingUnknown(err) {
		actual = 0
	}
	recordErr := c.budget.Reconcile(context.WithoutCancel(ctx), route.Provider.Name(), id, actual)
	if err := errors.Join(err, pricingErr, recordErr); err != nil {
		return nil, err
	}
	return completion, nil
}

// Embed resolves task, checks the budget, calls the provider, normalises the
// vectors to StorageDimensions and records spend. A zero Dimensions asks for
// the smaller of the known native width and the storage width.
func (c *Client) Embed(ctx context.Context, task Task, req EmbedRequest) ([]Vector, Usage, error) {
	route, err := c.router.Task(task)
	if err != nil {
		return nil, Usage{}, err
	}
	req.Model = route.Model
	info := ModelFor(c.models, route)
	if req.Dimensions == 0 {
		req.Dimensions = StorageDimensions
		if info.Dimensions > 0 && info.Dimensions < req.Dimensions {
			req.Dimensions = info.Dimensions
		}
	}
	if info.Dimensions > 0 && req.Dimensions > info.Dimensions {
		return nil, Usage{}, fmt.Errorf("llm: task %s requests %d dimensions but model %s produces %d",
			task, req.Dimensions, route.Model, info.Dimensions)
	}
	result, err := c.embedRaw(ctx, route, req, info)
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
	return vectors, result.Usage, nil
}

// embedRaw shares reservation and reconciliation between ingestion and setup checks.
// Unlike Embed it preserves a zero dimensions request and the native vector width.
func (c *Client) embedRaw(ctx context.Context, route Route, req EmbedRequest, info ModelInfo) (*EmbedResult, error) {
	// Gemini's embedding API supplies no billable usage; a priced, capped
	// configuration cannot safely authorize these calls.
	if _, gemini := route.Provider.(*Gemini); gemini && info.EmbeddingPer1M > 0 && c.budget.limits[route.Provider.Name()] > 0 {
		return nil, fmt.Errorf("%w: capped Gemini embeddings require billable token counts", ErrUnsupported)
	}
	estimate, err := estimateEmbedding(req, info)
	if err != nil {
		return nil, err
	}
	id, err := c.budget.Reserve(ctx, route.Provider.Name(), estimate)
	if err != nil {
		return nil, err
	}
	result, err := route.Provider.Embed(ctx, req)
	actual := estimate
	var pricingErr error
	if result != nil {
		actual, pricingErr = info.EmbeddingCost(result.Usage)
		if pricingErr != nil || result.Usage == (Usage{}) {
			actual = estimate
		}
	} else if !billingUnknown(err) {
		actual = 0
	}
	recordErr := c.budget.Reconcile(context.WithoutCancel(ctx), route.Provider.Name(), id, actual)
	if err := errors.Join(err, pricingErr, recordErr); err != nil {
		return nil, err
	}
	if result == nil || len(result.Vectors) != len(req.Texts) {
		return nil, ErrInvalidOutput
	}
	return result, nil
}

// A lost response may still have incurred a charge. Keep the estimate rather
// than refunding a timeout, server fault or unreadable successful response.
func billingUnknown(err error) bool {
	if err == nil || errors.Is(err, ErrUnsupported) {
		return false
	}
	switch outbound.ErrorClass(err) {
	case outbound.DNS, outbound.TLS, outbound.DeniedAddress:
		return false
	case outbound.Connect:
		// Connect also covers read/write failures after a request was sent.
		// Only a failed dial proves the provider could not have billed it.
		var op *net.OpError
		return !errors.As(err, &op) || op.Op != "dial"
	}
	var providerErr *Error
	if errors.As(err, &providerErr) {
		return providerErr.StatusCode < 400 || providerErr.StatusCode >= 500 || providerErr.StatusCode == 408
	}
	return true
}
