package service

import (
	"context"
	"io"

	"github.com/L-K-M/Earnest/internal/llm"
	"github.com/L-K-M/Earnest/internal/pipeline"
)

// EvaluateGolden runs the configured, budgeted task against an original corpus.
// This administrative local operation never reads subscriptions or annotations.
func (s *Service) EvaluateGolden(ctx context.Context, task llm.Task, corpus io.Reader) (pipeline.EvaluationReport, error) {
	engine := pipelineEngine(ctx)
	if !engine.Enabled(task) {
		return pipeline.EvaluationReport{}, ErrPipelineDisabled
	}
	items, err := pipeline.LoadGolden(corpus)
	if err != nil {
		return pipeline.EvaluationReport{}, err
	}
	report, err := pipeline.Evaluate(ctx, engine, task, items)
	return report, pipelineError(err)
}
