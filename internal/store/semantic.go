package store

import (
	"context"
	"fmt"
)

// SemanticMatch evaluates a current vector inside the user's subscription.
func (s *Store) SemanticMatch(ctx context.Context, user, entry int64, model string, vector []float32, threshold float64) (bool, error) {
	literal, err := formatHalfvec(vector)
	if err != nil {
		return false, err
	}
	var matched bool
	err = s.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM embeddings x JOIN entries e ON e.id=x.entry_id AND e.content_hash=x.content_hash AND e.title=x.input_title JOIN feeds f ON f.id=e.feed_id JOIN subscriptions s ON s.feed_id=e.feed_id AND s.user_id=$1 AND s.deleted_at IS NULL WHERE e.id=$2 AND x.model=$3 AND e.deleted_at IS NULL AND (f.owner_user_id IS NULL OR (f.owner_user_id=$1 AND f.llm_allowed)) AND 1-(x.embedding <=> $4::halfvec)>=$5)`, user, entry, model, literal, threshold).Scan(&matched)
	if err != nil {
		return false, fmt.Errorf("semantic predicate: %w", err)
	}
	return matched, nil
}
