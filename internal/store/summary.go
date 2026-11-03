package store

import (
	"context"
	"fmt"
)

// SummarySource contains only the selected stored publisher version and consent.
type SummarySource struct {
	EntryID          int64
	ContentHash      []byte
	Text             string
	Source           string
	SanitizerVersion int
	ExcerptOnly      bool
	OwnerUserID      *int64
	Allowed          bool
}

// SummaryContent loads a live entry only through the user's active subscription.
func (s *Store) SummaryContent(ctx context.Context, user, entry int64) (SummarySource, error) {
	var out SummarySource
	err := s.pool.QueryRow(ctx, `SELECT e.id,e.content_hash,c.text,c.source,c.sanitizer_version,c.excerpt_only,f.owner_user_id,(f.owner_user_id IS NULL OR (f.owner_user_id=$1 AND f.llm_allowed))
 FROM entries e JOIN feeds f ON f.id=e.feed_id
 JOIN subscriptions sub ON sub.feed_id=e.feed_id AND sub.user_id=$1 AND sub.deleted_at IS NULL
 JOIN LATERAL(SELECT * FROM contents WHERE entry_id=e.id AND content_hash=e.content_hash ORDER BY (source='extracted') DESC,sanitizer_version DESC LIMIT 1)c ON true
 WHERE e.id=$2 AND e.deleted_at IS NULL`, user, entry).Scan(&out.EntryID, &out.ContentHash, &out.Text, &out.Source, &out.SanitizerVersion, &out.ExcerptOnly, &out.OwnerUserID, &out.Allowed)
	if err != nil {
		return out, fmt.Errorf("summary source: %w", err)
	}
	return out, nil
}
