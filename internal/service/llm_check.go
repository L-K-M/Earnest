package service

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/L-K-M/Earnest/internal/llm"
	"github.com/L-K-M/Earnest/internal/outbound"
)

type LLMModelCheckInput struct {
	Revision   int64            `json:"revision"`
	Provider   LLMProviderInput `json:"provider"`
	Model      LLMModelSettings `json:"model"`
	Capability string           `json:"capability" enum:"chat,embedding"`
}

// CheckLLMModel previews a draft using synthetic text. Only usage accounting
// persists; credentials, configuration and article content are untouched.
func (s *Service) CheckLLMModel(ctx context.Context, a Actor, in LLMModelCheckInput) (llm.ModelCheck, error) {
	if err := s.llmSettingsActor(ctx, a); err != nil {
		return llm.ModelCheck{}, err
	}
	if in.Revision < 0 || (in.Capability != "chat" && in.Capability != "embedding") {
		return llm.ModelCheck{}, ErrInvalid
	}
	previous, revision, err := s.loadLLMSettings(ctx, s.store.Pool())
	if err != nil {
		return llm.ModelCheck{}, err
	}
	if revision != in.Revision {
		return llm.ModelCheck{}, fmt.Errorf("AI settings changed; reload before checking the model: %w", ErrConflict)
	}
	if in.Model.Provider != "" && in.Model.Provider != in.Provider.Name {
		return llm.ModelCheck{}, fmt.Errorf("model belongs to a different provider: %w", ErrInvalid)
	}
	model := in.Model
	model.Provider = in.Provider.Name
	// A preview must be able to examine wide models before their resize support
	// is known. CheckModel verifies the actual response; this flag is never saved.
	if model.Dimensions > llm.StorageDimensions {
		model.SupportsCustomDimensions = true
	}
	task := llm.TaskBlurb
	if in.Capability == "embedding" {
		task = llm.TaskEmbed
	}
	cfg, err := validateLLMSettings(LLMSettingsInput{Providers: []LLMProviderInput{in.Provider}, Models: []LLMModelSettings{model}, Tasks: []LLMTaskSettings{{Task: string(task), Provider: in.Provider.Name, Model: model.Name}}}, previous)
	if err != nil {
		return llm.ModelCheck{}, err
	}
	p := cfg.Providers[in.Provider.Name]
	// Checking an unsaved edit cannot raise the saved spending limit.
	if limit := previous.Providers[in.Provider.Name].MonthlyLimitUSD; limit > 0 && (p.MonthlyLimitUSD == 0 || limit < p.MonthlyLimitUSD) {
		p.MonthlyLimitUSD = limit
	}
	p.Timeout = min(p.Timeout, 30*time.Second)
	cfg.Providers[in.Provider.Name] = p
	client := s.outbound
	if client == nil {
		client, err = outbound.New(outbound.Config{})
		if err != nil {
			return llm.ModelCheck{}, ErrUnavailable
		}
	}
	router, err := llm.NewRouter(cfg, client)
	if err != nil {
		return llm.ModelCheck{}, fmt.Errorf("this provider and budget do not support the selected capability: %w", ErrInvalid)
	}
	checker, err := llm.NewClient(router, llm.NewBudget(s.store, cfg.Limits()), cfg.Models)
	if err != nil {
		return llm.ModelCheck{}, ErrUnavailable
	}
	ctx, cancel := context.WithTimeout(ctx, p.Timeout)
	defer cancel()
	result, err := checker.CheckModel(ctx, task)
	if err != nil {
		return llm.ModelCheck{}, fmt.Errorf("%s: %w", modelCheckError(err), ErrUnavailable)
	}
	return result, nil
}

// Never expose provider bodies, credential-bearing URLs or raw transport errors.
func modelCheckError(err error) string {
	if errors.Is(err, llm.ErrBudgetExhausted) {
		return "The monthly budget has been reached. Review the provider budget before trying again."
	}
	if errors.Is(err, llm.ErrInvalidOutput) || errors.Is(err, llm.ErrDimensions) {
		return "The model returned an unusable result. Check the model and its supported output size."
	}
	var provider *llm.Error
	if errors.As(err, &provider) {
		switch provider.StatusCode {
		case http.StatusUnauthorized, http.StatusForbidden:
			return "Access was rejected. Check the API key and its permissions."
		case http.StatusTooManyRequests:
			return "The provider is rate limiting requests. Try again later."
		case http.StatusBadRequest, http.StatusNotFound, http.StatusMethodNotAllowed:
			return "The provider rejected this model or output size. Check the model ID and capability."
		}
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "The model check timed out. Your settings have not changed."
	}
	if outbound.ErrorClass(err) == outbound.DeniedAddress {
		return "The server blocks this destination. Check its outbound allowlist."
	}
	return "The model check failed. Check the provider URL and connection, then try again."
}
