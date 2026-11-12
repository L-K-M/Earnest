package service

import (
	"context"
	"fmt"

	"github.com/L-K-M/Earnest/internal/llm"
	"github.com/L-K-M/Earnest/internal/outbound"
)

type LLMModelsInput struct {
	Revision int64            `json:"revision"`
	Provider LLMProviderInput `json:"provider"`
}

type LLMModels struct {
	Models []llm.DiscoveredModel `json:"models"`
}

// DiscoverLLMModels previews one draft destination without persisting its key,
// changing routing, or adding anything to the operation log.
func (s *Service) DiscoverLLMModels(ctx context.Context, a Actor, in LLMModelsInput) (LLMModels, error) {
	if err := s.llmSettingsActor(ctx, a); err != nil {
		return LLMModels{}, err
	}
	if in.Revision < 0 {
		return LLMModels{}, fmt.Errorf("nonnegative revision required: %w", ErrInvalid)
	}
	previous, revision, err := s.loadLLMSettings(ctx, s.store.Pool())
	if err != nil {
		return LLMModels{}, err
	}
	if revision != in.Revision {
		return LLMModels{}, fmt.Errorf("LLM settings changed; reload before fetching models: %w", ErrConflict)
	}
	// Use the same validation and credential inheritance as Save. No other
	// draft providers, prices or incomplete task routes are needed to browse.
	cfg, err := validateLLMSettings(LLMSettingsInput{Providers: []LLMProviderInput{in.Provider}}, previous)
	if err != nil {
		return LLMModels{}, err
	}
	client := s.outbound
	if client == nil {
		client, err = outbound.New(outbound.Config{})
		if err != nil {
			return LLMModels{}, ErrUnavailable
		}
	}
	models, err := llm.DiscoverModels(ctx, in.Provider.Name, cfg.Providers[in.Provider.Name], client)
	if err != nil {
		// Discovery errors contain only locally authored messages, never an
		// upstream response body, URL, headers or decoder input.
		return LLMModels{}, fmt.Errorf("%s: %w", err, ErrUnavailable)
	}
	return LLMModels{Models: models}, nil
}
