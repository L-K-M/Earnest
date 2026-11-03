package store

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
)

// LLMCacheGet returns the cached output and citations JSON for one cache key.
// found is false when no row matches; absence is not an error.
//
// ownerUserID is nil for public content and a user ID for private content,
// which the schema partitions so one user's cached output never reaches
// another's.
func (s *Store) LLMCacheGet(ctx context.Context, ownerUserID *int64, contentHash []byte, kind, promptVersion, model string) (output string, citations []byte, found bool, err error) {
	err = s.pool.QueryRow(ctx, `SELECT output, citations FROM llm_cache
		WHERE owner_user_id IS NOT DISTINCT FROM $1 AND content_hash = $2
			AND kind = $3 AND prompt_version = $4 AND model = $5`,
		ownerUserID, contentHash, kind, promptVersion, model).Scan(&output, &citations)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", nil, false, nil
	}
	if err != nil {
		return "", nil, false, fmt.Errorf("read llm cache: %w", err)
	}
	return output, citations, true, nil
}

// LLMCachePut inserts or replaces a cached output. citations is a JSON array.
func (s *Store) LLMCachePut(ctx context.Context, ownerUserID *int64, contentHash []byte, kind, promptVersion, model, output string, citations []byte) error {
	if _, err := s.pool.Exec(ctx, `INSERT INTO llm_cache
			(owner_user_id, content_hash, kind, prompt_version, model, output, citations)
		VALUES ($1, $2, $3, $4, $5, $6, $7::jsonb)
		ON CONFLICT (owner_user_id, content_hash, kind, prompt_version, model)
		DO UPDATE SET output = EXCLUDED.output, citations = EXCLUDED.citations, created_at = now()`,
		ownerUserID, contentHash, kind, promptVersion, model, output, string(citations)); err != nil {
		return fmt.Errorf("write llm cache: %w", err)
	}
	return nil
}
