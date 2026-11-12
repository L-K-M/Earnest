package store

import (
	"context"
	"fmt"
	"time"
)

// FrontPageCandidate is deliberately small: ranking examines the entire window
// before any article bodies or reading state are loaded.
type FrontPageCandidate struct {
	EntryID, FeedID, ClusterID int64
	SourceTitle                string
	PublishedAt                time.Time
	ContentHash                []byte
}

func (s *Store) FrontPageCandidates(ctx context.Context, user int64, since, until time.Time) ([]FrontPageCandidate, error) {
	rows, err := s.pool.Query(ctx, `SELECT e.id,e.feed_id,coalesce(c.id,0),coalesce(nullif(sub.title_override,''),nullif(f.title,''),f.url),coalesce(e.published_at,e.first_seen_at),e.content_hash
 FROM entries e JOIN feeds f ON f.id=e.feed_id
 JOIN subscriptions sub ON sub.feed_id=e.feed_id AND sub.user_id=$1 AND sub.deleted_at IS NULL
 LEFT JOIN entry_clusters ec ON ec.entry_id=e.id AND ec.user_id=$1 AND ec.deleted_at IS NULL
 LEFT JOIN clusters c ON c.id=ec.cluster_id AND c.user_id=$1 AND c.deleted_at IS NULL
 WHERE e.deleted_at IS NULL AND NOT e.imported AND NOT sub.muted
 AND coalesce(e.published_at,e.first_seen_at)>=$2 AND coalesce(e.published_at,e.first_seen_at)<=$3 AND e.first_seen_at<=$3
 AND NOT EXISTS (SELECT 1 FROM mutes m WHERE m.user_id=$1 AND m.deleted_at IS NULL AND
 ((m.kind='author' AND lower(m.value)=lower(e.author)) OR (m.kind='keyword' AND position(lower(m.value) in lower(e.title))>0)))
 ORDER BY coalesce(e.published_at,e.first_seen_at) DESC,e.id DESC`, user, since, until)
	if err != nil {
		return nil, fmt.Errorf("front page candidates: %w", err)
	}
	defer rows.Close()
	out := []FrontPageCandidate{}
	for rows.Next() {
		var c FrontPageCandidate
		if err := rows.Scan(&c.EntryID, &c.FeedID, &c.ClusterID, &c.SourceTitle, &c.PublishedAt, &c.ContentHash); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}
