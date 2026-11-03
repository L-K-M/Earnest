package api

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/L-K-M/Earnest/internal/service"
)

func generatedPipelineError(err error) error {
	if errors.Is(err, service.ErrPipelineDisabled) {
		p := problemStatus(http.StatusConflict)
		p.Detail = "This model task is disabled. Configure its provider and model to enable it."
		return p
	}
	return pipelineErr(err)
}

type summaryInput struct {
	ID   int64                  `path:"id" minimum:"1"`
	Kind service.AnnotationKind `query:"kind" default:"summary" enum:"summary,blurb"`
}

type editionTriggerBody struct {
	Period  service.EditionPeriod `json:"period" enum:"daily,weekly"`
	ReadyAt time.Time             `json:"ready_at,omitempty"`
	OpID    string                `json:"op_id" minLength:"1" maxLength:"1024"`
}

type compileBody struct {
	Text string `json:"text" minLength:"1" maxLength:"4096"`
}
type askBody struct {
	Question string `json:"question" minLength:"1" maxLength:"4096"`
}

func (s *server) generatedPipelineRoutes() {
	route(s, http.MethodPost, "/v1/entries/{id}/summary", "generate-entry-summary", func(ctx context.Context, in *summaryInput) (service.SummaryResult, error) {
		result, err := s.svc.SummarizeEntry(ctx, actor(ctx), in.ID, in.Kind)
		return result, generatedPipelineError(err)
	})
	route(s, http.MethodGet, "/v1/editions/latest", "latest-edition", func(ctx context.Context, _ *empty) (service.Edition, error) {
		return s.svc.LatestEdition(ctx, actor(ctx))
	})
	route(s, http.MethodPost, "/v1/editions", "trigger-edition", func(ctx context.Context, in *bodyInput[editionTriggerBody]) (service.Edition, error) {
		return s.svc.TriggerEdition(ctx, actor(ctx), in.Body.Period, in.Body.ReadyAt, in.Body.OpID)
	})
	route(s, http.MethodPost, "/v1/rules:compile", "compile-rule", func(ctx context.Context, in *bodyInput[compileBody]) (service.CompiledRule, error) {
		result, err := s.svc.CompileRule(ctx, actor(ctx), in.Body.Text)
		return result, generatedPipelineError(err)
	})
	route(s, http.MethodPost, "/v1/ask", "ask-feeds", func(ctx context.Context, in *bodyInput[askBody]) (service.AskResult, error) {
		result, err := s.svc.Ask(ctx, actor(ctx), in.Body.Question)
		return result, generatedPipelineError(err)
	})
}
