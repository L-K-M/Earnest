package service

import (
	"context"
	"fmt"
	"math"
	"sort"
	"strings"

	"github.com/L-K-M/Earnest/internal/llm"
	"github.com/L-K-M/Earnest/internal/pipeline"
)

// SearchMode selects lexical search or reciprocal-rank hybrid retrieval.
type SearchMode string

const (
	SearchFullText SearchMode = "fulltext"
	SearchHybrid   SearchMode = "hybrid"
)
const hybridCandidates = 200
const reciprocalRankConstant = 60

func (s *Service) semanticVector(ctx context.Context, user int64, description string) ([]float32, string, error) {
	engine := pipelineEngine(ctx)
	model, err := engine.Model(llm.TaskEmbed)
	if err != nil {
		return nil, "", pipelineError(err)
	}
	vector, found, err := s.store.SemanticQueryGet(ctx, user, description, model)
	if err != nil {
		return nil, "", err
	}
	if found {
		return vector, model, nil
	}
	result, err := engine.Query(ctx, description)
	if err != nil {
		return nil, "", pipelineError(err)
	}
	if err := s.store.SemanticQueryPut(ctx, user, description, model, result.Values); err != nil {
		return nil, "", err
	}
	return result.Values, model, nil
}

func (s *Service) hybridSearch(ctx context.Context, a Actor, query string, f SearchFilter, cursor *SearchCursor) ([]SearchResult, error) {
	vector, model, err := s.semanticVector(ctx, a.UserID, query)
	if err != nil {
		return nil, err
	}
	lexical := f
	lexical.Mode = SearchFullText
	lexical.Limit = hybridCandidates
	text, err := s.Search(ctx, a, query, lexical, nil)
	if err != nil {
		return nil, err
	}
	hits, err := s.store.SemanticSearch(ctx, a.UserID, f.SubscriptionID, f.FolderID, 0, model, vector, hybridCandidates)
	if err != nil {
		return nil, err
	}
	semantic := make([]SearchResult, 0, len(hits))
	for _, hit := range hits {
		semantic = append(semantic, SearchResult{EntryID: hit.ID, SubscriptionID: hit.SubscriptionID, FeedID: hit.FeedID, Seq: hit.Seq, Title: hit.Title, URL: hit.URL, Snippet: passage(hit.Text), Rank: hit.Score})
	}
	out := fuseResults(text, semantic)
	limit := f.Limit
	if limit == 0 {
		limit = defaultEntryLimit
	}
	page := []SearchResult{}
	for _, hit := range out {
		if cursor != nil && (hit.Rank > cursor.Rank || (hit.Rank == cursor.Rank && (hit.Seq > cursor.Seq || (hit.Seq == cursor.Seq && hit.AnnotationID >= cursor.AnnotationID)))) {
			continue
		}
		page = append(page, hit)
		if len(page) == limit {
			break
		}
	}
	return page, nil
}

// fuseResults gives independent credit to each retrieval list. Agent notes
// keep their own identity and receive credit only from the full-text list.
func fuseResults(lexical, semantic []SearchResult) []SearchResult {
	type key struct{ entry, annotation int64 }
	results := map[key]SearchResult{}
	for _, list := range [][]SearchResult{lexical, semantic} {
		seen := map[key]bool{}
		for i, hit := range list {
			k := key{hit.EntryID, hit.AnnotationID}
			if seen[k] {
				continue
			}
			seen[k] = true
			prior, ok := results[k]
			if !ok {
				prior = hit
				prior.Rank = 0
			}
			prior.Rank += 1 / float64(reciprocalRankConstant+i+1)
			results[k] = prior
		}
	}
	out := make([]SearchResult, 0, len(results))
	for _, hit := range results {
		out = append(out, hit)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Rank != out[j].Rank {
			return out[i].Rank > out[j].Rank
		}
		if out[i].Seq != out[j].Seq {
			return out[i].Seq > out[j].Seq
		}
		return out[i].AnnotationID > out[j].AnnotationID
	})
	return out
}

// Similar uses the stored source vector; opening an entry never sends its
// body to a provider or causes a fetch. Missing backfill is a paused result.
func (s *Service) Similar(ctx context.Context, a Actor, id int64, limit int) ([]SearchResult, error) {
	if err := s.checkActor(ctx, a, ScopeEntriesRead); err != nil {
		return nil, err
	}
	if id <= 0 || limit < 0 || limit > maxEntryLimit {
		return nil, ErrInvalid
	}
	if limit == 0 {
		limit = defaultEntryLimit
	}
	entry, err := s.Entry(ctx, a, id)
	if err != nil {
		return nil, err
	}
	if entry.Deleted {
		return nil, ErrNotFound
	}
	engine := pipelineEngine(ctx)
	model, err := engine.Model(llm.TaskEmbed)
	if err != nil {
		return nil, pipelineError(err)
	}
	consent, err := s.store.EmbedConsent(ctx, id)
	if err != nil {
		return nil, err
	}
	if !consent {
		return []SearchResult{}, nil
	}
	// Current-content check prevents stale vectors after a publisher update.
	var current bool
	if err := s.store.Pool().QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM embeddings x JOIN entries e ON e.id=x.entry_id AND e.content_hash=x.content_hash AND e.title=x.input_title WHERE e.id=$1 AND x.model=$2)`, id, model).Scan(&current); err != nil {
		return nil, fmt.Errorf("source embedding: %w", err)
	}
	if !current {
		return nil, ErrPipelinePaused
	}
	_, vector, found, err := s.store.EmbeddingGet(ctx, id, model)
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, ErrPipelinePaused
	}
	hits, err := s.store.SemanticSearch(ctx, a.UserID, 0, 0, id, model, vector, limit)
	if err != nil {
		return nil, err
	}
	out := []SearchResult{}
	for _, h := range hits {
		if math.IsNaN(h.Score) {
			continue
		}
		out = append(out, SearchResult{EntryID: h.ID, SubscriptionID: h.SubscriptionID, FeedID: h.FeedID, Seq: h.Seq, Title: h.Title, URL: h.URL, Snippet: passage(h.Text), Rank: h.Score})
	}
	return out, nil
}

func passage(text string) string {
	r := []rune(strings.TrimSpace(text))
	if len(r) > 200 {
		r = r[:200]
	}
	return string(r)
}

func (s *Service) semanticMatch(ctx context.Context, user, entry int64, description string) (bool, error) {
	vector, model, err := s.semanticVector(ctx, user, description)
	if err != nil {
		return false, err
	}
	// Retrieve the requested article directly rather than limiting a global list.
	return s.store.SemanticMatch(ctx, user, entry, model, vector, pipeline.SemanticThreshold)
}
