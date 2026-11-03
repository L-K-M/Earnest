package service

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/L-K-M/Earnest/internal/llm"
	"github.com/L-K-M/Earnest/internal/pipeline"
)

// prepareModelPredicates makes model data explicit before evaluating the pure
// AST. Unavailable predicates disable the whole tree, including negations.
func (s *Service) prepareModelPredicates(ctx context.Context, user int64, p RulePredicate, e ruleEntry) (ruleEntry, bool, error) {
	engine := pipelineEngine(ctx)
	e.SemanticMatches = map[string]bool{}
	var walk func(RulePredicate) (bool, error)
	walk = func(node RulePredicate) (bool, error) {
		switch node.Type {
		case RuleClassifierIn:
			if !engine.Enabled(llm.TaskClassify) {
				return false, nil
			}
			consent, err := s.store.EmbedConsent(ctx, e.ID)
			if err != nil {
				return false, err
			}
			if !consent {
				return false, nil
			}
			model, _ := engine.Model(llm.TaskClassify)
			labels, found, err := s.store.ClassifierLabels(ctx, e.ID, model, pipeline.ClassifierPromptVersion)
			if err != nil {
				return false, err
			}
			if !found {
				return false, ErrPipelinePaused
			}
			e.ClassifierLabels = labels
		case RuleSemantic:
			if !engine.Enabled(llm.TaskEmbed) {
				return false, nil
			}
			consent, err := s.store.EmbedConsent(ctx, e.ID)
			if err != nil {
				return false, err
			}
			if !consent {
				return false, nil
			}
			model, _ := engine.Model(llm.TaskEmbed)
			var current bool
			if err := s.store.Pool().QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM embeddings x JOIN entries e ON e.id=x.entry_id AND x.content_hash=e.content_hash AND x.input_title=e.title WHERE x.entry_id=$1 AND x.model=$2)`, e.ID, model).Scan(&current); err != nil {
				return false, fmt.Errorf("predicate embedding: %w", err)
			}
			if !current {
				return false, ErrPipelinePaused
			}
			match, err := s.semanticMatch(ctx, user, e.ID, node.Value)
			if err != nil {
				return false, err
			}
			e.SemanticMatches[node.Value] = match
		}
		for _, child := range node.Children {
			ready, err := walk(child)
			if err != nil || !ready {
				return ready, err
			}
		}
		return true, nil
	}
	ready, err := walk(p)
	return e, ready, err
}

func classifierMatches(terms, labels []string) bool {
	for _, term := range terms {
		if pipeline.ValidLabel(term) && slices.Contains(labels, term) {
			return true
		}
	}
	return false
}

func pipelineWaiting(err error) bool {
	return errors.Is(err, ErrPipelinePaused) || errors.Is(err, llm.ErrBudgetExhausted)
}
