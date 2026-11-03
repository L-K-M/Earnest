package pipeline

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/L-K-M/Earnest/internal/llm"
)

// Rubric checks required facts, forbidden claims and classifier labels independently of citations.
type Rubric struct {
	MustMention    []string `json:"must_mention"`
	MustNotMention []string `json:"must_not_mention"`
	Labels         []string `json:"labels"`
}

// GoldenItem is an original test article and a human-written support rubric.
type GoldenItem struct {
	ID          string `json:"id"`
	Title       string `json:"title"`
	Text        string `json:"text"`
	ExcerptOnly bool   `json:"excerpt_only"`
	Rubric      Rubric `json:"rubric"`
}

// EvaluationClient is the same governed engine used in production.
type EvaluationClient interface {
	Completer
	Classify(context.Context, Content) ([]string, error)
}

// EvaluationResult reports one case without printing its provider payload.
type EvaluationResult struct {
	ID             string `json:"id"`
	Passed         bool   `json:"passed"`
	Reason         string `json:"reason,omitempty"`
	CitedClaims    int    `json:"cited_claims"`
	VerifiedClaims int    `json:"verified_claims"`
}

// EvaluationReport separates support rubric success from mere passage existence.
type EvaluationReport struct {
	Task           llm.Task           `json:"task"`
	PromptVersion  string             `json:"prompt_version"`
	Total          int                `json:"total"`
	Passed         int                `json:"passed"`
	PassRate       float64            `json:"pass_rate"`
	CitedClaims    int                `json:"cited_claims"`
	VerifiedClaims int                `json:"verified_claims"`
	CitationRate   *float64           `json:"citation_rate"`
	Paused         bool               `json:"paused"`
	Results        []EvaluationResult `json:"results"`
}

// LoadGolden reads a bounded corpus, rejecting malformed rubrics and duplicate IDs.
func LoadGolden(r io.Reader) ([]GoldenItem, error) {
	dec := json.NewDecoder(io.LimitReader(r, 8<<20))
	dec.DisallowUnknownFields()
	var items []GoldenItem
	if err := dec.Decode(&items); err != nil {
		return nil, fmt.Errorf("golden corpus: %w", err)
	}
	var trailing any
	if err := dec.Decode(&trailing); err != io.EOF {
		return nil, fmt.Errorf("golden corpus has trailing data")
	}
	seen := map[string]bool{}
	for _, item := range items {
		if item.ID == "" || seen[item.ID] || strings.TrimSpace(item.Text) == "" || len(item.Rubric.MustMention) == 0 {
			return nil, fmt.Errorf("invalid golden item %q", item.ID)
		}
		for _, label := range item.Rubric.Labels {
			if !ValidLabel(label) {
				return nil, fmt.Errorf("invalid golden label %q", label)
			}
		}
		seen[item.ID] = true
	}
	if len(items) == 0 {
		return nil, fmt.Errorf("empty golden corpus")
	}
	return items, nil
}

// Evaluate runs summary, classifier or edition prompts against independently authored rubrics.
// All generated URLs must exist in the source; this rule is never optional in a rubric.
func Evaluate(ctx context.Context, client EvaluationClient, task llm.Task, items []GoldenItem) (EvaluationReport, error) {
	report := EvaluationReport{Task: task, Total: len(items), Results: []EvaluationResult{}}
	switch task {
	case llm.TaskSummary:
		report.PromptVersion = SummaryPromptVersion
	case llm.TaskClassify:
		report.PromptVersion = ClassifierPromptVersion
	case llm.TaskEdition:
		report.PromptVersion = EditionPromptVersion
	default:
		return report, fmt.Errorf("unsupported evaluation task %q", task)
	}
	for i, item := range items {
		result := EvaluationResult{ID: item.ID, Passed: true}
		text := ""
		var err error
		switch task {
		case llm.TaskSummary:
			var claims []Claim
			claims, err = GenerateSummary(ctx, client, item.Text)
			for _, claim := range claims {
				text += claim.Text + " "
				result.CitedClaims++
				if claim.Verified {
					result.VerifiedClaims++
				}
			}
		case llm.TaskClassify:
			var labels []string
			labels, err = client.Classify(ctx, Content{Title: item.Title, Text: item.Text})
			for _, expected := range item.Rubric.Labels {
				found := false
				for _, label := range labels {
					found = found || label == expected
				}
				if !found {
					result.Passed = false
					result.Reason = "missing classifier label: " + expected
				}
			}
		case llm.TaskEdition:
			key := llm.ContentKey(nil, item.Text, llm.CacheEdition, EditionPromptVersion, "eval")
			source := Source{EntryID: int64(i + 1), ContentHash: fmt.Sprintf("%x", key.ContentHash), Text: item.Text, ExcerptOnly: item.ExcerptOnly}
			var claims []GroundedClaim
			claims, err = GenerateGrounded(ctx, client, task, "This week", []Source{source})
			for _, claim := range claims {
				text += claim.Text + " "
				result.CitedClaims++
				if claim.Verified {
					result.VerifiedClaims++
				}
			}
		}
		if errors.Is(err, llm.ErrBudgetExhausted) || errors.Is(err, llm.ErrDisabled) || ctx.Err() != nil {
			report.Paused = true
			return report, err
		}
		if err != nil {
			result.Passed = false
			result.Reason = err.Error()
		} else if task != llm.TaskClassify {
			lower := strings.ToLower(text)
			for _, required := range item.Rubric.MustMention {
				if !strings.Contains(lower, strings.ToLower(required)) {
					result.Passed = false
					result.Reason = "missing required fact: " + required
				}
			}
			for _, forbidden := range item.Rubric.MustNotMention {
				if strings.Contains(lower, strings.ToLower(forbidden)) {
					result.Passed = false
					result.Reason = "forbidden claim: " + forbidden
				}
			}
			if result.VerifiedClaims != result.CitedClaims {
				result.Passed = false
				result.Reason = "citation verification failed"
			}
		}
		if result.Passed {
			report.Passed++
		}
		report.CitedClaims += result.CitedClaims
		report.VerifiedClaims += result.VerifiedClaims
		report.Results = append(report.Results, result)
	}
	if report.Total > 0 {
		report.PassRate = float64(report.Passed) / float64(report.Total)
	}
	if report.CitedClaims > 0 {
		rate := float64(report.VerifiedClaims) / float64(report.CitedClaims)
		report.CitationRate = &rate
	}
	return report, nil
}
