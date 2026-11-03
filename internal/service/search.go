package service

import (
	"context"
	"fmt"
	"math"
	"strings"
)

// SearchFilter narrows search to a subscription or folder. Zero selects all.
type SearchFilter struct {
	SubscriptionID int64 `json:"subscription_id,omitempty"`
	FolderID       int64 `json:"folder_id,omitempty"`
	Limit          int   `json:"limit,omitempty"`
}

// SearchCursor identifies the last result in a descending rank and sequence page.
type SearchCursor struct {
	Rank         float64
	Seq          int64
	AnnotationID int64
}

// SearchResult is an entry or a separate agent note attached to that entry.
type SearchResult struct {
	EntryID        int64
	SubscriptionID int64
	FeedID         int64
	Seq            int64
	Title          string
	URL            string
	Snippet        string
	Rank           float64
	AnnotationID   int64
	AgentName      string
}

// Search finds entries and agent notes in the caller's subscriptions. A note
// is a separate result so its text cannot be mistaken for publisher content.
func (s *Service) Search(ctx context.Context, a Actor, query string, f SearchFilter, cursor *SearchCursor) ([]SearchResult, error) {
	if err := s.checkActor(ctx, a, ScopeEntriesRead); err != nil {
		return nil, err
	}
	if strings.TrimSpace(query) == "" || len(query) > 4096 || f.SubscriptionID < 0 || f.FolderID < 0 || f.Limit < 0 || f.Limit > maxEntryLimit {
		return nil, fmt.Errorf("search parameters: %w", ErrInvalid)
	}
	if cursor != nil && (math.IsNaN(cursor.Rank) || math.IsInf(cursor.Rank, 0) || cursor.Rank < 0 || cursor.Seq <= 0 || cursor.AnnotationID < 0) {
		return nil, fmt.Errorf("search cursor: %w", ErrInvalid)
	}
	limit := f.Limit
	if limit == 0 {
		limit = defaultEntryLimit
	}
	var rank *float64
	var seq, annotation *int64
	if cursor != nil {
		rank = &cursor.Rank
		seq = &cursor.Seq
		annotation = &cursor.AnnotationID
	}
	rows, err := s.store.Pool().Query(ctx, `WITH visible AS (
 SELECT e.id,e.feed_id,e.seq,e.title,coalesce(e.url,'') AS url,effective_search_language(e.language,feed.language) AS language,e.search_vector,s.id AS subscription_id,e.content_hash
 FROM entries e JOIN feeds feed ON feed.id=e.feed_id JOIN subscriptions s ON s.feed_id=e.feed_id AND s.user_id=$1 AND s.deleted_at IS NULL
 WHERE e.deleted_at IS NULL AND ($3::bigint=0 OR s.id=$3) AND ($4::bigint=0 OR s.folder_id=$4)
 ), matches AS (
 SELECT v.id,v.subscription_id,v.feed_id,v.seq,v.title,v.url,
 ts_headline(entry_search_config(v.language),CASE WHEN to_tsvector(entry_search_config(v.language),coalesce(c.text,'')) @@ websearch_to_tsquery(entry_search_config(v.language),$2) THEN c.text ELSE v.title END,websearch_to_tsquery(entry_search_config(v.language),$2),'StartSel="", StopSel=""') AS snippet,
 ts_rank(v.search_vector,websearch_to_tsquery(entry_search_config(v.language),$2))::float8 AS rank,0::bigint AS annotation_id,''::text AS agent_name
 FROM visible v LEFT JOIN LATERAL (SELECT text FROM contents WHERE entry_id=v.id AND content_hash=v.content_hash ORDER BY (source='extracted') DESC,sanitizer_version DESC LIMIT 1) c ON true
 WHERE v.search_vector @@ websearch_to_tsquery(entry_search_config(v.language),$2)
 UNION ALL
 SELECT v.id,v.subscription_id,v.feed_id,v.seq,v.title,v.url,
 ts_headline('simple',a.value,websearch_to_tsquery('simple',$2),'StartSel="", StopSel=""'),
 ts_rank(to_tsvector('simple',a.value),websearch_to_tsquery('simple',$2))::float8,a.id,p.name
 FROM visible v JOIN annotations a ON v.id=ANY(a.entry_ids) AND a.user_id=$1 AND a.kind='note' AND a.deleted_at IS NULL
 JOIN principals p ON p.id=a.principal_id AND p.user_id=$1 AND p.kind='agent'
 WHERE to_tsvector('simple',a.value) @@ websearch_to_tsquery('simple',$2)
 ) SELECT * FROM matches WHERE $5::float8 IS NULL OR (rank,seq,annotation_id)<($5,$6,$7)
 ORDER BY rank DESC,seq DESC,annotation_id DESC LIMIT $8`, a.UserID, query, f.SubscriptionID, f.FolderID, rank, seq, annotation, limit)
	if err != nil {
		return nil, fmt.Errorf("search entries: %w", err)
	}
	defer rows.Close()
	out := []SearchResult{}
	for rows.Next() {
		var r SearchResult
		if err := rows.Scan(&r.EntryID, &r.SubscriptionID, &r.FeedID, &r.Seq, &r.Title, &r.URL, &r.Snippet, &r.Rank, &r.AnnotationID, &r.AgentName); err != nil {
			return nil, fmt.Errorf("scan search result: %w", err)
		}
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("search entries: %w", err)
	}
	return out, nil
}
