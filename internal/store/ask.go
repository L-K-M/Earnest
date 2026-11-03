package store

import (
	"context"
	"fmt"
	"time"
)

// AskReserve atomically counts attempts before retrieval or answering can spend.
// Unknown outcomes keep their reservation so crashes cannot bypass the daily cap.
func (s *Store) AskReserve(ctx context.Context, user int64, day time.Time, limit int) (bool, error) {
	tag, err := s.pool.Exec(ctx, `INSERT INTO ask_daily_usage(user_id,day,attempts) VALUES($1,$2,1) ON CONFLICT(user_id,day) DO UPDATE SET attempts=ask_daily_usage.attempts+1 WHERE ask_daily_usage.attempts<$3`, user, day.UTC().Format("2006-01-02"), limit)
	if err != nil {
		return false, fmt.Errorf("reserve ask budget: %w", err)
	}
	return tag.RowsAffected() > 0, nil
}

// AskLexical searches publisher text only; notes cannot influence candidate selection.
func (s *Store) AskLexical(ctx context.Context, user int64, query string, limit int) ([]SemanticHit, error) {
	rows, err := s.pool.Query(ctx, `SELECT e.id,sub.id,e.feed_id,e.seq,e.title,coalesce(e.url,''),c.text,ts_rank(e.search_vector,websearch_to_tsquery(entry_search_config(effective_search_language(e.language,f.language)),$2))::float8 AS rank
 FROM entries e JOIN feeds f ON f.id=e.feed_id
 JOIN subscriptions sub ON sub.feed_id=e.feed_id AND sub.user_id=$1 AND sub.deleted_at IS NULL
 JOIN LATERAL(SELECT text FROM contents WHERE entry_id=e.id AND content_hash=e.content_hash ORDER BY (source='extracted') DESC,sanitizer_version DESC LIMIT 1)c ON true
 WHERE e.deleted_at IS NULL AND (f.owner_user_id IS NULL OR (f.owner_user_id=$1 AND f.llm_allowed))
 AND e.search_vector @@ websearch_to_tsquery(entry_search_config(effective_search_language(e.language,f.language)),$2)
 ORDER BY rank DESC,e.seq DESC LIMIT $3`, user, query, limit)
	if err != nil {
		return nil, fmt.Errorf("ask lexical retrieval: %w", err)
	}
	defer rows.Close()
	out := []SemanticHit{}
	for rows.Next() {
		var hit SemanticHit
		if err := rows.Scan(&hit.ID, &hit.SubscriptionID, &hit.FeedID, &hit.Seq, &hit.Title, &hit.URL, &hit.Text, &hit.Score); err != nil {
			return nil, fmt.Errorf("ask candidate: %w", err)
		}
		out = append(out, hit)
	}
	return out, rows.Err()
}
