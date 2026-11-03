package store

import (
	"context"
	"fmt"
	"time"
)

// EditionCandidate is a chronological list item with its chosen content version.
type EditionCandidate struct {
	ID               int64
	Title            string
	Folder           string
	ClusterID        int64
	ContentHash      []byte
	ContentSource    string
	SanitizerVersion int
	ExcerptOnly      bool
}

// EditionCandidates lists This week entries by ingest time, with cluster membership.
func (s *Store) EditionCandidates(ctx context.Context, user int64, since, until time.Time) ([]EditionCandidate, error) {
	rows, err := s.pool.Query(ctx, `SELECT e.id,e.title,coalesce(folder.name,'This week'),coalesce(ec.cluster_id,0),e.content_hash,coalesce(c.source,''),coalesce(c.sanitizer_version,0),coalesce(c.excerpt_only,false)
 FROM entries e JOIN subscriptions sub ON sub.feed_id=e.feed_id AND sub.user_id=$1 AND sub.deleted_at IS NULL
 LEFT JOIN folders folder ON folder.id=sub.folder_id AND folder.user_id=sub.user_id AND folder.deleted_at IS NULL
 LEFT JOIN entry_clusters ec ON ec.entry_id=e.id AND ec.user_id=$1 AND ec.deleted_at IS NULL
 LEFT JOIN LATERAL(SELECT source,sanitizer_version,excerpt_only FROM contents WHERE entry_id=e.id AND content_hash=e.content_hash ORDER BY (source='extracted') DESC,sanitizer_version DESC LIMIT 1)c ON true
 WHERE e.deleted_at IS NULL AND sub.cadence='week' AND e.first_seen_at>=$2 AND e.first_seen_at<=$3
 ORDER BY e.seq DESC,e.id DESC`, user, since, until)
	if err != nil {
		return nil, fmt.Errorf("edition entries: %w", err)
	}
	defer rows.Close()
	out := []EditionCandidate{}
	for rows.Next() {
		var e EditionCandidate
		if err := rows.Scan(&e.ID, &e.Title, &e.Folder, &e.ClusterID, &e.ContentHash, &e.ContentSource, &e.SanitizerVersion, &e.ExcerptOnly); err != nil {
			return nil, fmt.Errorf("edition entry: %w", err)
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// EditionSource reads the snapshotted version, rechecking current consent and ownership.
func (s *Store) EditionSource(ctx context.Context, user, entry int64, hash []byte, source string, version int) (SummarySource, error) {
	var out SummarySource
	err := s.pool.QueryRow(ctx, `SELECT e.id,c.content_hash,c.text,c.source,c.sanitizer_version,c.excerpt_only,f.owner_user_id,(f.owner_user_id IS NULL OR (f.owner_user_id=$1 AND f.llm_allowed))
 FROM entries e JOIN feeds f ON f.id=e.feed_id
 JOIN subscriptions sub ON sub.feed_id=f.id AND sub.user_id=$1 AND sub.deleted_at IS NULL
 JOIN contents c ON c.entry_id=e.id AND c.content_hash=$3 AND c.source=$4 AND c.sanitizer_version=$5
 WHERE e.id=$2 AND e.deleted_at IS NULL`, user, entry, hash, source, version).Scan(&out.EntryID, &out.ContentHash, &out.Text, &out.Source, &out.SanitizerVersion, &out.ExcerptOnly, &out.OwnerUserID, &out.Allowed)
	if err != nil {
		return out, fmt.Errorf("edition version: %w", err)
	}
	return out, nil
}
