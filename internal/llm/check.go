package llm

import (
	"context"
	"fmt"
	"math"
)

// ModelCheck contains observed compatibility, not inferred truncation permission.
type ModelCheck struct {
	Compatible               bool   `json:"compatible"`
	Dimensions               int    `json:"dimensions,omitempty"`
	SupportsCustomDimensions bool   `json:"supports_custom_dimensions,omitempty"`
	Message                  string `json:"message"`
}

// CheckModel sends fixed sample text through the same budget and provider path
// as production requests. It neither changes routing nor writes content caches.
func (c *Client) CheckModel(ctx context.Context, task Task) (ModelCheck, error) {
	if task != TaskEmbed {
		_, err := c.Complete(ctx, task, CompleteRequest{Messages: []Message{{Role: RoleUser, Content: "Reply with the word Ready."}}, MaxTokens: 64})
		if err != nil {
			return ModelCheck{}, err
		}
		return ModelCheck{Compatible: true, Message: "Text generation succeeded. Feature-specific output is checked when used."}, nil
	}
	route, err := c.router.Task(task)
	if err != nil {
		return ModelCheck{}, err
	}
	info := ModelFor(c.models, route)
	req := EmbedRequest{Model: route.Model, Texts: []string{"Earnest model compatibility check."}}
	native := info.Dimensions
	if native == 0 {
		result, err := c.embedRaw(ctx, route, req, info)
		if err != nil {
			return ModelCheck{}, err
		}
		if err := checkVector(result); err != nil {
			return ModelCheck{}, err
		}
		native = len(result.Vectors[0])
	}
	req.Dimensions = min(native, StorageDimensions)
	result, err := c.embedRaw(ctx, route, req, info)
	if err != nil {
		return ModelCheck{}, err
	}
	if err := checkVector(result); err != nil {
		return ModelCheck{}, err
	}
	width := len(result.Vectors[0])
	if width != req.Dimensions {
		if !info.Truncatable || width != native {
			return ModelCheck{Dimensions: native, Message: fmt.Sprintf("The model returned %d dimensions when %d were requested. Choose a model that supports this output size.", width, req.Dimensions)}, nil
		}
	}
	return ModelCheck{Compatible: true, Dimensions: native, SupportsCustomDimensions: native > StorageDimensions && width == req.Dimensions, Message: "Embedding model checked successfully."}, nil
}

func checkVector(result *EmbedResult) error {
	if result == nil || len(result.Vectors) != 1 || len(result.Vectors[0]) == 0 || len(result.Vectors[0]) > 65536 {
		return ErrInvalidOutput
	}
	for _, value := range result.Vectors[0] {
		if math.IsNaN(float64(value)) || math.IsInf(float64(value), 0) {
			return ErrInvalidOutput
		}
	}
	return nil
}
