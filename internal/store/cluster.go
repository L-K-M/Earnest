package store

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

// clusterQuerier is the read subset of *pgxpool.Pool and pgx.Tx.
type clusterQuerier interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// LockClusterPass serializes a user's clustering pass for the transaction's
// duration. Take it before LockCore so the ordering never inverts.
func LockClusterPass(ctx context.Context, tx pgx.Tx, user int64) error {
	if _, err := tx.Exec(ctx, "SELECT pg_advisory_xact_lock(hashtextextended('earnest/cluster', $1))", user); err != nil {
		return fmt.Errorf("cluster pass lock: %w", err)
	}
	return nil
}

// PendingClusterEntry is one subscription-visible entry without an active
// cluster membership, in deterministic seq order.
type PendingClusterEntry struct {
	ID          int64
	FeedID      int64
	Title       string
	URL         string
	FirstSeenAt time.Time
}

// PendingClusterUsers lists users holding at least one unclustered entry,
// so the pass can serialize each user's work independently.
func PendingClusterUsers(ctx context.Context, q clusterQuerier) ([]int64, error) {
	rows, err := q.Query(ctx, `SELECT DISTINCT s.user_id FROM subscriptions s
		JOIN entries e ON e.feed_id = s.feed_id AND e.deleted_at IS NULL
		WHERE s.deleted_at IS NULL
		AND NOT EXISTS (SELECT 1 FROM entry_clusters ec
			WHERE ec.user_id = s.user_id AND ec.entry_id = e.id AND ec.deleted_at IS NULL)
		ORDER BY s.user_id`)
	if err != nil {
		return nil, fmt.Errorf("pending cluster users: %w", err)
	}
	defer rows.Close()
	out := []int64{}
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("scan pending cluster user: %w", err)
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

// PendingClusterEntries returns the user's unclustered entries, oldest seq
// first, bounded by limit. Private entries can only appear through the
// owner's own subscription, so they stay inside their user's clusters.
func PendingClusterEntries(ctx context.Context, q clusterQuerier, user int64, limit int) ([]PendingClusterEntry, error) {
	rows, err := q.Query(ctx, `SELECT e.id, e.feed_id, e.title, COALESCE(e.url, ''),e.first_seen_at
		FROM entries e
		JOIN subscriptions s ON s.feed_id = e.feed_id AND s.user_id = $1 AND s.deleted_at IS NULL
		WHERE e.deleted_at IS NULL
		AND NOT EXISTS (SELECT 1 FROM entry_clusters ec
			WHERE ec.user_id = $1 AND ec.entry_id = e.id AND ec.deleted_at IS NULL)
		ORDER BY e.seq LIMIT $2`, user, limit)
	if err != nil {
		return nil, fmt.Errorf("pending cluster entries: %w", err)
	}
	defer rows.Close()
	out := []PendingClusterEntry{}
	for rows.Next() {
		var e PendingClusterEntry
		if err := rows.Scan(&e.ID, &e.FeedID, &e.Title, &e.URL, &e.FirstSeenAt); err != nil {
			return nil, fmt.Errorf("scan pending cluster entry: %w", err)
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// ClusterMember is one live entry_cluster row with the member entry's
// deterministic match data and the feed it came from.
type ClusterMember struct {
	ClusterID   int64
	EntryID     int64
	FeedID      int64
	Title       string
	URL         string
	JoinedAt    time.Time
	FirstSeenAt time.Time
}

// RecentClusterMembers returns every active member of the user's live
// clusters with a member ingested since the cutoff. Processing an old
// backfill must not make the historical story recent again.
func RecentClusterMembers(ctx context.Context, q clusterQuerier, user int64, since time.Time) ([]ClusterMember, error) {
	rows, err := q.Query(ctx, `SELECT ec.cluster_id, ec.entry_id, e.feed_id, e.title, COALESCE(e.url, ''), ec.joined_at,e.first_seen_at
		FROM entry_clusters ec
		JOIN entries e ON e.id = ec.entry_id AND e.deleted_at IS NULL
		JOIN clusters c ON c.user_id = ec.user_id AND c.id = ec.cluster_id AND c.deleted_at IS NULL
		WHERE ec.user_id = $1 AND ec.deleted_at IS NULL
		AND EXISTS (SELECT 1 FROM entry_clusters r JOIN entries re ON re.id=r.entry_id AND re.deleted_at IS NULL
			WHERE r.user_id = ec.user_id AND r.cluster_id = ec.cluster_id
			AND r.deleted_at IS NULL AND re.first_seen_at >= $2)
		ORDER BY ec.cluster_id, ec.joined_at, ec.entry_id`, user, since)
	if err != nil {
		return nil, fmt.Errorf("recent cluster members: %w", err)
	}
	defer rows.Close()
	out := []ClusterMember{}
	for rows.Next() {
		var m ClusterMember
		if err := rows.Scan(&m.ClusterID, &m.EntryID, &m.FeedID, &m.Title, &m.URL, &m.JoinedAt, &m.FirstSeenAt); err != nil {
			return nil, fmt.Errorf("scan cluster member: %w", err)
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// ClusterSimilarity ranks one candidate member's cosine distance ascending.
type ClusterSimilarity struct {
	EntryID  int64
	Distance float64
}

// SimilarClusterEntries ranks the candidate entries nearest to the pending
// entry's stored vector under maxDistance. Both sides must carry a current
// embedding for model: content hash and input title matching the live entry,
// on a feed whose provider consent is still valid. An absent or stale vector
// returns no rows rather than comparing outdated text.
func SimilarClusterEntries(ctx context.Context, q clusterQuerier, user, entry int64, model string, candidates []int64, maxDistance float64, limit int) ([]ClusterSimilarity, error) {
	rows, err := q.Query(ctx, `SELECT m.entry_id, m.embedding <=> q.embedding
		FROM entries pe
		JOIN feeds pf ON pf.id = pe.feed_id
		JOIN embeddings q ON q.entry_id = pe.id AND q.model = $2
			AND q.content_hash = pe.content_hash AND q.input_title = pe.title
		JOIN embeddings m ON m.model = q.model AND m.entry_id = ANY($3::bigint[]) AND m.entry_id <> pe.id
		JOIN entries me ON me.id = m.entry_id AND me.deleted_at IS NULL
			AND me.content_hash = m.content_hash AND me.title = m.input_title
		JOIN feeds mf ON mf.id = me.feed_id
		WHERE pe.id = $1 AND pe.deleted_at IS NULL
		AND (pf.owner_user_id IS NULL OR pf.llm_allowed)
		AND (mf.owner_user_id IS NULL OR mf.llm_allowed)
		AND m.embedding <=> q.embedding <= $4
		ORDER BY m.embedding <=> q.embedding, m.entry_id
		LIMIT $5`, entry, model, candidates, maxDistance, limit)
	if err != nil {
		return nil, fmt.Errorf("similar cluster entries: %w", err)
	}
	defer rows.Close()
	out := []ClusterSimilarity{}
	for rows.Next() {
		var hit ClusterSimilarity
		if err := rows.Scan(&hit.EntryID, &hit.Distance); err != nil {
			return nil, fmt.Errorf("scan similar cluster entry: %w", err)
		}
		out = append(out, hit)
	}
	return out, rows.Err()
}

// InsertCluster creates a story cluster carrying the allocated change rev.
func InsertCluster(ctx context.Context, tx pgx.Tx, user int64, title string, rev int64) (int64, error) {
	var id int64
	err := tx.QueryRow(ctx, "INSERT INTO clusters(user_id,title,rev) VALUES($1,$2,$3) RETURNING id", user, title, rev).Scan(&id)
	if err != nil {
		return 0, fmt.Errorf("insert cluster: %w", err)
	}
	return id, nil
}

// TouchCluster stamps a cluster's last-change revision on membership change.
func TouchCluster(ctx context.Context, tx pgx.Tx, user, cluster, rev int64) error {
	if _, err := tx.Exec(ctx, "UPDATE clusters SET rev=$3 WHERE user_id=$1 AND id=$2", user, cluster, rev); err != nil {
		return fmt.Errorf("touch cluster: %w", err)
	}
	return nil
}

// UpsertClusterMember adds or resurrects one membership. A resurrected row is
// a new join: joined_at and rev move forward so done-mark comparisons treat
// it like any other late member.
func UpsertClusterMember(ctx context.Context, tx pgx.Tx, user, cluster, entry, rev int64, reason string, joinedAt time.Time) error {
	_, err := tx.Exec(ctx, `INSERT INTO entry_clusters(user_id,cluster_id,entry_id,rev,reason,joined_at)
		VALUES($1,$2,$3,$4,$5,$6)
		ON CONFLICT (user_id,cluster_id,entry_id) DO UPDATE
		SET deleted_at=NULL, rev=EXCLUDED.rev, reason=EXCLUDED.reason, joined_at=EXCLUDED.joined_at`,
		user, cluster, entry, rev, reason, joinedAt)
	if err != nil {
		return fmt.Errorf("upsert cluster member: %w", err)
	}
	return nil
}

// ClusterMembership is an entry's live cluster and how many other visible
// members share it.
type ClusterMembership struct {
	ClusterID int64
	AlsoIn    int
}

// ClusterMemberships resolves the live cluster and the count of other
// members still reachable through an active subscription, for each of ids.
func ClusterMemberships(ctx context.Context, q clusterQuerier, user int64, ids []int64) (map[int64]ClusterMembership, error) {
	out := make(map[int64]ClusterMembership, len(ids))
	if len(ids) == 0 {
		return out, nil
	}
	rows, err := q.Query(ctx, `SELECT ec.entry_id, ec.cluster_id,
		(SELECT count(*) FROM entry_clusters x
		 JOIN entries xe ON xe.id = x.entry_id AND xe.deleted_at IS NULL
		 JOIN subscriptions xs ON xs.feed_id = xe.feed_id AND xs.user_id = x.user_id AND xs.deleted_at IS NULL
		 WHERE x.user_id = ec.user_id AND x.cluster_id = ec.cluster_id
		 AND x.deleted_at IS NULL AND x.entry_id <> ec.entry_id)
		FROM entry_clusters ec
		JOIN clusters c ON c.user_id = ec.user_id AND c.id = ec.cluster_id AND c.deleted_at IS NULL
		WHERE ec.user_id = $1 AND ec.deleted_at IS NULL AND ec.entry_id = ANY($2)`, user, ids)
	if err != nil {
		return nil, fmt.Errorf("cluster memberships: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var entry int64
		var m ClusterMembership
		if err := rows.Scan(&entry, &m.ClusterID, &m.AlsoIn); err != nil {
			return nil, fmt.Errorf("scan cluster membership: %w", err)
		}
		out[entry] = m
	}
	return out, rows.Err()
}

// SuppressedClusterEntries returns the ids hidden by read-time suppression:
// late members of a cluster the principal finished through a cluster-scoped
// Done. A member joined after the recorded done revision, and nothing newer
// than that mark has touched its registers — an explicit unread lifts it.
// Stored state is untouched, so suppression stays a view concern only.
func SuppressedClusterEntries(ctx context.Context, q clusterQuerier, user, principal int64, ids []int64) ([]int64, error) {
	if len(ids) == 0 {
		return []int64{}, nil
	}
	rows, err := q.Query(ctx, `SELECT DISTINCT ec.entry_id
		FROM entry_clusters ec
		JOIN cluster_dones d ON d.user_id = ec.user_id AND d.cluster_id = ec.cluster_id AND d.principal_id = $2
		JOIN clusters c ON c.user_id = ec.user_id AND c.id = ec.cluster_id AND c.deleted_at IS NULL
		LEFT JOIN entry_state st ON st.principal_id = d.principal_id AND st.entry_id = ec.entry_id
		WHERE ec.user_id = $1 AND ec.entry_id = ANY($3) AND ec.deleted_at IS NULL
		AND ec.rev > d.rev
		AND COALESCE(st.seen_rev, 0) <= d.rev
		AND COALESCE(st.seen, 'unseen') = 'unseen'
		AND COALESCE(st.shelf, 'none') <> 'done'`, user, principal, ids)
	if err != nil {
		return nil, fmt.Errorf("suppressed cluster entries: %w", err)
	}
	defer rows.Close()
	out := []int64{}
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("scan suppressed entry: %w", err)
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

// ClusterVectorPending defers a similarity decision until the source vector
// exists. Private feeds without consent still cluster deterministically.
func ClusterVectorPending(ctx context.Context, q clusterQuerier, entry int64, model string) (bool, error) {
	var waiting bool
	err := q.QueryRow(ctx, `SELECT (f.owner_user_id IS NULL OR f.llm_allowed) AND NOT EXISTS(SELECT 1 FROM embeddings x WHERE x.entry_id=e.id AND x.model=$2 AND x.content_hash=e.content_hash AND x.input_title=e.title) FROM entries e JOIN feeds f ON f.id=e.feed_id WHERE e.id=$1`, entry, model).Scan(&waiting)
	if err != nil {
		return false, fmt.Errorf("cluster vector readiness: %w", err)
	}
	return waiting, nil
}
