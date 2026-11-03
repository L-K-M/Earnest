package service

import (
	"context"
	"encoding/hex"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/L-K-M/Earnest/internal/llm"
	"github.com/L-K-M/Earnest/internal/pipeline"
	"github.com/L-K-M/Earnest/internal/store"
)

const (
	AskDailyBudget  = 20
	askSourceLimit  = 8
	askPassageBytes = 4096
)

// AskResult is an inert answer with passages, provenance and explicit trust labels.
type AskResult struct {
	Claims        []pipeline.GroundedClaim `json:"claims"`
	Sources       []pipeline.Source        `json:"sources"`
	Model         string                   `json:"model"`
	PromptVersion string                   `json:"prompt_version"`
	Retrieval     string                   `json:"retrieval"`
	ExcerptNotice string                   `json:"excerpt_notice,omitempty"`
	Untrusted     bool                     `json:"untrusted"`
}

// Ask uses hybrid publisher-only retrieval, falling back to full text when embeddings are off.
func (s *Service) Ask(ctx context.Context, a Actor, question string) (AskResult, error) {
	if err := s.checkActor(ctx, a, ScopeEntriesRead); err != nil {
		return AskResult{}, err
	}
	if !utf8.ValidString(question) || strings.TrimSpace(question) == "" || len(question) > 4096 {
		return AskResult{}, ErrInvalid
	}
	engine := pipelineEngine(ctx)
	model, err := engine.Model(llm.TaskAsk)
	if err != nil {
		return AskResult{}, pipelineError(err)
	}
	allowed, err := s.store.AskReserve(ctx, a.UserID, s.now(), AskDailyBudget)
	if err != nil {
		return AskResult{}, err
	}
	if !allowed {
		return AskResult{}, fmt.Errorf("daily ask budget (%d) exhausted: %w", AskDailyBudget, ErrPipelinePaused)
	}
	lexical, err := s.store.AskLexical(ctx, a.UserID, question, hybridCandidates)
	if err != nil {
		return AskResult{}, err
	}
	candidates := askResults(lexical)
	retrieval := "fulltext"
	if engine.Enabled(llm.TaskEmbed) {
		vector, embedModel, err := s.semanticVector(ctx, a.UserID, question)
		if err != nil {
			return AskResult{}, pipelineError(err)
		}
		semantic, err := s.store.SemanticSearch(ctx, a.UserID, 0, 0, 0, embedModel, vector, hybridCandidates)
		if err != nil {
			return AskResult{}, err
		}
		candidates = fuseResults(candidates, askResults(semantic))
		retrieval = "hybrid"
	}
	sources := []pipeline.Source{}
	excerpt := false
	for _, hit := range candidates {
		source, err := s.store.SummaryContent(ctx, a.UserID, hit.EntryID)
		if err != nil {
			return AskResult{}, domainError(err)
		}
		if !source.Allowed {
			continue
		}
		sources = append(sources, pipeline.Source{EntryID: source.EntryID, ContentHash: hex.EncodeToString(source.ContentHash), Text: askPassage(source.Text, question), ExcerptOnly: source.ExcerptOnly})
		excerpt = excerpt || source.ExcerptOnly
		if len(sources) == askSourceLimit {
			break
		}
	}
	out := AskResult{Claims: []pipeline.GroundedClaim{}, Sources: sources, Model: model, PromptVersion: pipeline.AskPromptVersion, Retrieval: retrieval, Untrusted: true}
	if excerpt {
		out.ExcerptNotice = pipeline.ExcerptNotice
	}
	if len(sources) == 0 {
		return out, nil
	}
	claims, err := pipeline.GenerateGrounded(ctx, engine, llm.TaskAsk, question, sources)
	if err != nil {
		return AskResult{}, pipelineError(err)
	}
	out.Claims = claims
	return out, nil
}

func askResults(hits []store.SemanticHit) []SearchResult {
	out := make([]SearchResult, 0, len(hits))
	for _, h := range hits {
		out = append(out, SearchResult{EntryID: h.ID, Seq: h.Seq, Rank: h.Score})
	}
	return out
}

func askPassage(text, question string) string {
	if len(text) <= askPassageBytes {
		return text
	}
	start := 0
	lower := strings.ToLower(text)
	for _, word := range strings.Fields(strings.ToLower(question)) {
		if len(word) < 3 {
			continue
		}
		if at := strings.Index(lower, word); at >= 0 {
			start = max(0, at-askPassageBytes/4)
			break
		}
	}
	for start > 0 && !utf8.RuneStart(text[start]) {
		start--
	}
	end := min(len(text), start+askPassageBytes)
	for end > start && !utf8.ValidString(text[start:end]) {
		end--
	}
	return text[start:end]
}
