package store

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/L-K-M/Earnest/internal/syncmodel"
	"github.com/jackc/pgx/v5"
)

// ChangeRow is one stored change-log event. The payload omits rev and type,
// which are columns; objects inside the payload carry the change rev.
type ChangeRow struct {
	Rev     int64
	Type    string
	Payload json.RawMessage
}

// ReadSyncCursor reads the database generation and the user's revision state
// in one statement, so cursor validation sees a single snapshot.
func ReadSyncCursor(ctx context.Context, tx pgx.Tx, user int64) (generation string, rev, floor int64, err error) {
	err = tx.QueryRow(ctx, `SELECT (SELECT generation::text FROM system WHERE singleton),
  COALESCE((SELECT value FROM rev_counters WHERE user_id=$1), 0),
  COALESCE((SELECT retention_floor FROM rev_counters WHERE user_id=$1), 0)`, user).
		Scan(&generation, &rev, &floor)
	if err != nil {
		return "", 0, 0, fmt.Errorf("read sync cursor: %w", err)
	}
	return generation, rev, floor, nil
}

// ReadChanges selects committed events after afterRev, oldest first.
func ReadChanges(ctx context.Context, tx pgx.Tx, user, afterRev int64, limit int) ([]ChangeRow, error) {
	rows, err := tx.Query(ctx, "SELECT rev, type, payload FROM changes WHERE user_id=$1 AND rev>$2 ORDER BY rev LIMIT $3", user, afterRev, limit)
	if err != nil {
		return nil, fmt.Errorf("read changes: %w", err)
	}
	defer rows.Close()
	out := []ChangeRow{}
	for rows.Next() {
		var c ChangeRow
		if err := rows.Scan(&c.Rev, &c.Type, &c.Payload); err != nil {
			return nil, fmt.Errorf("scan change: %w", err)
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// Page is one bounded slice of a stably ordered bootstrap resource. The
// composite cursor slots mirror the service request fields: ID is the primary
// ordering column, Principal the first member of composite cursors (position
// and state principals, label IDs for memberships) and View the position view.
// Unused slots stay zero.
type Page struct {
	Items     []json.RawMessage
	ID        int64
	Principal int64
	View      string
	HasMore   bool
}

// collectPage runs a keyset-paginated query returning a single JSON document
// per row, fetches limit+1 rows and reports whether more remain.
func collectPage(ctx context.Context, tx pgx.Tx, query string, args []any, limit int, cursor func(json.RawMessage) (int64, int64, string)) (Page, error) {
	rows, err := tx.Query(ctx, query, args...)
	if err != nil {
		return Page{}, fmt.Errorf("bootstrap page: %w", err)
	}
	defer rows.Close()
	out := Page{Items: []json.RawMessage{}}
	for rows.Next() {
		var body json.RawMessage
		if err := rows.Scan(&body); err != nil {
			return Page{}, fmt.Errorf("scan bootstrap item: %w", err)
		}
		if len(out.Items) == limit {
			out.HasMore = true
			break
		}
		out.Items = append(out.Items, body)
		out.ID, out.Principal, out.View = cursor(body)
	}
	return out, rows.Err()
}

func jsonID(body json.RawMessage, keys ...string) int64 {
	var doc map[string]json.RawMessage
	if err := json.Unmarshal(body, &doc); err != nil {
		return 0
	}
	for _, key := range keys {
		var id int64
		if raw, ok := doc[key]; ok && json.Unmarshal(raw, &id) == nil {
			return id
		}
	}
	return 0
}

func jsonText(body json.RawMessage, key string) string {
	var doc map[string]json.RawMessage
	if err := json.Unmarshal(body, &doc); err != nil {
		return ""
	}
	var text string
	if raw, ok := doc[key]; ok {
		_ = json.Unmarshal(raw, &text)
	}
	return text
}

// PageSubscriptions pages a user's subscription objects including tombstones.
// The feed projection never includes credentials.
func PageSubscriptions(ctx context.Context, tx pgx.Tx, user, afterID int64, limit int) (Page, error) {
	// icon_url matches the REST projection (service iconPath) and is left out
	// while no icon is stored, as there.
	return collectPage(ctx, tx, `SELECT CASE WHEN s.icon_url IS NULL THEN to_jsonb(s) - 'icon_url' ELSE to_jsonb(s) END FROM (
  SELECT s.id, s.feed_id, coalesce(s.title_override,f.title) AS title,
    f.url, coalesce(f.site_url,'') AS site_url, coalesce(s.folder_id,0) AS folder_id, s.cadence,
    CASE s.full_text_mode WHEN 'always' THEN 'always' WHEN 'never' THEN 'none' ELSE '' END AS extract,
    s.muted, s.mute_notifications, CASE WHEN isfinite(s.initial_since) THEN s.initial_since ELSE NULL END AS initial_since,
    s.initial_through_seq, s.initial_fetch_pending, s.rev, (s.deleted_at IS NOT NULL) AS deleted,
    (f.owner_user_id IS NOT NULL) AS private,
    jsonb_build_object('last_success_at',f.last_success_at,'last_attempt_at',f.last_attempt_at,
      'next_fetch_at',f.next_fetch_at,'error_class',coalesce(f.error_class,''),'error_message',coalesce(f.error_message,''),
      'consecutive_failures',f.consecutive_failures,'http_status',coalesce(f.last_http_status,0),
      'posts_per_day',f.posts_per_day,'firehose',f.posts_per_day>20) AS health,
    (SELECT '/v1/subscriptions/' || s.id || '/icon?v=' || i.etag FROM feed_icons i WHERE i.feed_id = s.feed_id AND i.body IS NOT NULL) AS icon_url
  FROM subscriptions s JOIN feeds f ON f.id = s.feed_id
  WHERE s.user_id = $1 AND s.id > $2 ORDER BY s.id LIMIT $3) s`,
		[]any{user, afterID, limit + 1}, limit,
		func(body json.RawMessage) (int64, int64, string) { return jsonID(body, "id"), 0, "" })
}

// PageFolders pages a user's folders including tombstones.
func PageFolders(ctx context.Context, tx pgx.Tx, user, afterID int64, limit int) (Page, error) {
	return collectPage(ctx, tx, `SELECT to_jsonb(f) FROM (
  SELECT f.id, f.name, f.rev, (f.deleted_at IS NOT NULL) AS deleted
  FROM folders f WHERE f.user_id = $1 AND f.id > $2 ORDER BY f.id LIMIT $3) f`,
		[]any{user, afterID, limit + 1}, limit,
		func(body json.RawMessage) (int64, int64, string) { return jsonID(body, "id"), 0, "" })
}

// PageLabels pages a user's native labels including tombstones.
func PageLabels(ctx context.Context, tx pgx.Tx, user, afterID int64, limit int) (Page, error) {
	return collectPage(ctx, tx, `SELECT to_jsonb(l) FROM (
  SELECT l.id, l.name, l.rev, (l.deleted_at IS NOT NULL) AS deleted
  FROM labels l WHERE l.user_id = $1 AND l.id > $2 ORDER BY l.id LIMIT $3) l`,
		[]any{user, afterID, limit + 1}, limit,
		func(body json.RawMessage) (int64, int64, string) { return jsonID(body, "id"), 0, "" })
}

// PageLabelMemberships pages entry-label memberships by the composite
// (label_id, entry_id) cursor, including tombstones.
func PageLabelMemberships(ctx context.Context, tx pgx.Tx, user, afterLabel, afterEntry int64, limit int) (Page, error) {
	return collectPage(ctx, tx, `SELECT to_jsonb(m) FROM (
  SELECT m.id, 'label' AS kind, m.label_id, m.entry_id, l.name,
    (SELECT p.id FROM principals p WHERE p.user_id=m.user_id AND p.kind='human') AS principal,
    m.rev, (m.deleted_at IS NOT NULL) AS deleted
  FROM entry_labels m JOIN labels l ON l.id=m.label_id
  WHERE m.user_id = $1 AND (m.label_id, m.entry_id) > ($2, $3)
  ORDER BY m.label_id, m.entry_id LIMIT $4) m`,
		[]any{user, afterLabel, afterEntry, limit + 1}, limit,
		func(body json.RawMessage) (int64, int64, string) {
			return jsonID(body, "entry_id"), jsonID(body, "label_id"), ""
		})
}

// PagePositions pages positions of every principal the user owns by the
// composite (principal_id, view) cursor.
func PagePositions(ctx context.Context, tx pgx.Tx, user, afterPrincipal int64, afterView string, limit int) (Page, error) {
	return collectPage(ctx, tx, `SELECT to_jsonb(p) FROM (
  SELECT p.principal_id AS principal, p.view, p.anchor_seq AS seq, p.rev
  FROM positions p WHERE p.principal_id IN (SELECT id FROM principals WHERE user_id = $1)
    AND (p.principal_id, p.view) > ($2, $3)
  ORDER BY p.principal_id, p.view LIMIT $4) p`,
		[]any{user, afterPrincipal, afterView, limit + 1}, limit,
		func(body json.RawMessage) (int64, int64, string) {
			return 0, jsonID(body, "principal"), jsonText(body, "view")
		})
}

// PageEntries pages the cached entry window: entries of the user's active
// subscriptions first seen since the cutoff, including retention tombstones.
// Each carries this user's entry_revisions stamp and no body columns.
func PageEntries(ctx context.Context, tx pgx.Tx, user int64, since time.Time, afterID int64, limit int) (Page, error) {
	// thumbnail_url matches the REST projection (service thumbnailPath) and
	// is left out while no thumbnail is stored, as there.
	return collectPage(ctx, tx, `SELECT CASE WHEN e.thumbnail_url IS NULL THEN to_jsonb(e) - 'thumbnail_url' ELSE to_jsonb(e) END FROM (
  SELECT e.id, s.id AS subscription_id, e.feed_id, e.seq, e.guid, e.url, e.title, e.author,
    e.published_at, e.updated_at, e.first_seen_at, e.language, e.enclosures,
    COALESCE(r.rev, 0) AS rev, (e.deleted_at IS NOT NULL) AS deleted,
    (SELECT '/v1/entries/' || i.entry_id || '/thumbnail?v=' || i.etag FROM entry_images i
      WHERE i.entry_id = e.id AND i.body IS NOT NULL AND e.deleted_at IS NULL) AS thumbnail_url
  FROM entries e
    JOIN subscriptions s ON s.feed_id = e.feed_id AND s.user_id = $1 AND s.deleted_at IS NULL
    LEFT JOIN entry_revisions r ON r.user_id = $1 AND r.entry_id = e.id
  WHERE e.first_seen_at >= $2 AND e.id > $3 ORDER BY e.id LIMIT $4) e`,
		[]any{user, since, afterID, limit + 1}, limit,
		func(body json.RawMessage) (int64, int64, string) { return jsonID(body, "id"), 0, "" })
}

// PageStates pages entry_state rows for every principal of the user over the
// cached entry window, by the composite (principal_id, entry_id) cursor.
// Returned states keep zero register provenance for untouched registers.
func PageStates(ctx context.Context, tx pgx.Tx, user int64, since time.Time, afterPrincipal, afterEntry int64, limit int) ([]syncmodel.State, int64, int64, bool, error) {
	rows, err := tx.Query(ctx, `SELECT st.principal_id, st.entry_id,
  st.seen, st.seen_rev, st.seen_occurred_at, st.seen_actor, st.seen_source,
  st.shelf, st.shelf_rev, st.shelf_occurred_at, st.shelf_actor, st.shelf_source,
  st.kept, st.kept_rev, st.kept_occurred_at, st.kept_actor, st.kept_source,
  st.state_rev
FROM entry_state st
WHERE st.principal_id IN (SELECT id FROM principals WHERE user_id = $1)
  AND st.entry_id IN (SELECT e.id FROM entries e JOIN subscriptions s ON s.feed_id = e.feed_id
                      AND s.user_id = $1 AND s.deleted_at IS NULL WHERE e.first_seen_at >= $2)
  AND (st.principal_id, st.entry_id) > ($3, $4)
ORDER BY st.principal_id, st.entry_id LIMIT $5`,
		user, since, afterPrincipal, afterEntry, limit+1)
	if err != nil {
		return nil, 0, 0, false, fmt.Errorf("state page: %w", err)
	}
	defer rows.Close()
	var states []syncmodel.State
	var lastPrincipal, lastEntry int64
	hasMore := false
	for rows.Next() {
		var principal, entry int64
		var seen, shelf string
		var kept bool
		var seenRev, shelfRev, keptRev, stateRev int64
		var seenAt, shelfAt, keptAt *time.Time
		var seenActor, shelfActor, keptActor *int64
		var seenSource, shelfSource, keptSource *string
		if err := rows.Scan(&principal, &entry,
			&seen, &seenRev, &seenAt, &seenActor, &seenSource,
			&shelf, &shelfRev, &shelfAt, &shelfActor, &shelfSource,
			&kept, &keptRev, &keptAt, &keptActor, &keptSource,
			&stateRev); err != nil {
			return nil, 0, 0, false, fmt.Errorf("scan state: %w", err)
		}
		if len(states) == limit {
			hasMore = true
			break
		}
		state := syncmodel.InitialState(syncmodel.ID(principal), syncmodel.ID(entry))
		state.Seen.Value = syncmodel.Value(seen)
		state.Shelf.Value = syncmodel.Value(shelf)
		if kept {
			state.Kept.Value = syncmodel.True
		}
		state.StateRev = uint64(stateRev)
		fillRegister(&state.Seen, seenRev, seenAt, seenActor, seenSource)
		fillRegister(&state.Shelf, shelfRev, shelfAt, shelfActor, shelfSource)
		fillRegister(&state.Kept, keptRev, keptAt, keptActor, keptSource)
		states = append(states, state)
		lastPrincipal, lastEntry = principal, entry
	}
	return states, lastPrincipal, lastEntry, hasMore, rows.Err()
}

// ChangeFloor pairs a user with the highest event revision retention removed
// and how many rows that deletion covered.
type ChangeFloor struct {
	UserID  int64
	Rev     int64
	Deleted int64
}

// DeleteOldChanges deletes each user's contiguous old prefix of change-log
// events: only revisions at or below the point where every earlier row is
// older than cutoff, so a clock moved backwards cannot open a hole in the
// retained log. It returns each user's highest deleted revision.
func DeleteOldChanges(ctx context.Context, tx pgx.Tx, cutoff time.Time) ([]ChangeFloor, error) {
	rows, err := tx.Query(ctx, `WITH bounds AS (
  SELECT c.user_id,
    COALESCE((SELECT MIN(n.rev) FROM changes n WHERE n.user_id = c.user_id AND n.created_at >= $1) - 1,
             MAX(c.rev)) AS floor
  FROM changes c GROUP BY c.user_id
),
del AS (
  DELETE FROM changes c USING bounds b
  WHERE c.user_id = b.user_id AND c.rev <= b.floor
  RETURNING c.user_id, c.rev
)
SELECT user_id, MAX(rev), count(*) FROM del GROUP BY user_id`, cutoff)
	if err != nil {
		return nil, fmt.Errorf("retain changes: %w", err)
	}
	defer rows.Close()
	var floors []ChangeFloor
	for rows.Next() {
		var f ChangeFloor
		if err := rows.Scan(&f.UserID, &f.Rev, &f.Deleted); err != nil {
			return nil, fmt.Errorf("scan change floor: %w", err)
		}
		floors = append(floors, f)
	}
	return floors, rows.Err()
}

// AdvanceChangeFloor raises a user's retention floor to the highest deleted
// revision. It never regresses and never exceeds the allocated revision.
func AdvanceChangeFloor(ctx context.Context, tx pgx.Tx, user, floor int64) error {
	if _, err := tx.Exec(ctx, "UPDATE rev_counters SET retention_floor = LEAST(GREATEST(retention_floor, $2), value) WHERE user_id = $1", user, floor); err != nil {
		return fmt.Errorf("advance retention floor: %w", err)
	}
	return nil
}

// DeleteOldOpIDs removes deduplication records older than cutoff. Their
// retention is independent of the change log's: clients must be able to retry
// operations for longer than the maximum offline queue plus margin.
func DeleteOldOpIDs(ctx context.Context, tx pgx.Tx, cutoff time.Time) error {
	if _, err := tx.Exec(ctx, "DELETE FROM op_ids WHERE created_at < $1", cutoff); err != nil {
		return fmt.Errorf("retain op_ids: %w", err)
	}
	return nil
}

// RetentionEntry is an entry whose body may be reclaimed by retention.
type RetentionEntry struct {
	ID          int64
	FeedID      int64
	Seq         int64
	FirstSeenAt time.Time
	PublishedAt *time.Time
}

// DiscoverRetentionEntries lists undeleted entries every active subscriber's
// retention window has expired: no subscriber's threshold still covers the
// entry's first_seen_at, and no principal keeps it or holds it on the Later
// shelf. A user settings retention_days overrides the fallback. Entries on
// feeds with no active subscribers qualify.
func DiscoverRetentionEntries(ctx context.Context, tx pgx.Tx, now time.Time, fallback time.Duration) ([]RetentionEntry, error) {
	rows, err := tx.Query(ctx, `SELECT e.id, e.feed_id, e.seq, e.first_seen_at, e.published_at
FROM entries e
WHERE e.deleted_at IS NULL
  AND (EXISTS (SELECT 1 FROM subscriptions s WHERE s.feed_id = e.feed_id AND s.deleted_at IS NULL)
       OR e.first_seen_at < $1::timestamptz - make_interval(secs => $2))
  AND NOT EXISTS (SELECT 1 FROM entry_state st WHERE st.entry_id = e.id AND (st.kept OR st.shelf = 'later'))
  AND NOT EXISTS (
    SELECT 1 FROM subscriptions s JOIN users u ON u.id = s.user_id
    WHERE s.feed_id = e.feed_id AND s.deleted_at IS NULL
      AND e.first_seen_at >= $1::timestamptz - COALESCE(
            CASE WHEN jsonb_typeof(u.settings -> 'retention_days') = 'number'
                      AND (u.settings ->> 'retention_days')::numeric BETWEEN 1 AND 36500
                 THEN make_interval(days => floor((u.settings ->> 'retention_days')::numeric)::int)
            END,
            make_interval(secs => $2)))
ORDER BY e.id`, now, fallback.Seconds())
	if err != nil {
		return nil, fmt.Errorf("discover retention entries: %w", err)
	}
	defer rows.Close()
	var out []RetentionEntry
	for rows.Next() {
		var e RetentionEntry
		if err := rows.Scan(&e.ID, &e.FeedID, &e.Seq, &e.FirstSeenAt, &e.PublishedAt); err != nil {
			return nil, fmt.Errorf("scan retention entry: %w", err)
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// Subscriber names one user's active subscription to a feed.
type Subscriber struct {
	UserID         int64
	SubscriptionID int64
}

// SubscribersForEntries returns each entry's currently subscribed users,
// ordered by user then entry so event assignment is deterministic.
func SubscribersForEntries(ctx context.Context, tx pgx.Tx, entryIDs []int64) (map[int64][]Subscriber, error) {
	rows, err := tx.Query(ctx, `SELECT e.id, s.user_id, s.id
FROM entries e JOIN subscriptions s ON s.feed_id = e.feed_id AND s.deleted_at IS NULL
WHERE e.id = ANY($1) ORDER BY e.id, s.user_id`, entryIDs)
	if err != nil {
		return nil, fmt.Errorf("entry subscribers: %w", err)
	}
	defer rows.Close()
	out := map[int64][]Subscriber{}
	for rows.Next() {
		var entry int64
		var sub Subscriber
		if err := rows.Scan(&entry, &sub.UserID, &sub.SubscriptionID); err != nil {
			return nil, fmt.Errorf("scan subscriber: %w", err)
		}
		out[entry] = append(out[entry], sub)
	}
	return out, rows.Err()
}

// TombstoneEntry soft-deletes an entry's body fields while retaining its
// identity, so ingestion can never resurrect it. The search trigger clears
// indexed text on the tombstone.
func TombstoneEntry(ctx context.Context, tx pgx.Tx, entry int64, at time.Time) error {
	if _, err := tx.Exec(ctx, "UPDATE entries SET deleted_at = $2 WHERE id = $1 AND deleted_at IS NULL", entry, at); err != nil {
		return fmt.Errorf("tombstone entry: %w", err)
	}
	return nil
}

// DeleteContents reclaims the stored bodies of a tombstoned entry. State rows
// remain for dedupe and the client reads a 404 as a tombstone.
func DeleteContents(ctx context.Context, tx pgx.Tx, entry int64) error {
	if _, err := tx.Exec(ctx, "DELETE FROM contents WHERE entry_id = $1", entry); err != nil {
		return fmt.Errorf("delete contents: %w", err)
	}
	return nil
}

// DeleteEntryImage drops an entry's thumbnail and its fetch state with the
// body it illustrated.
func DeleteEntryImage(ctx context.Context, tx pgx.Tx, entry int64) error {
	if _, err := tx.Exec(ctx, "DELETE FROM entry_images WHERE entry_id = $1", entry); err != nil {
		return fmt.Errorf("delete entry image: %w", err)
	}
	return nil
}

// StampEntryRevision records a user's last change revision for a shared entry.
func StampEntryRevision(ctx context.Context, tx pgx.Tx, user, entry, rev int64) error {
	if _, err := tx.Exec(ctx, `INSERT INTO entry_revisions (user_id, entry_id, rev) VALUES ($1, $2, $3)
  ON CONFLICT (user_id, entry_id) DO UPDATE SET rev = EXCLUDED.rev`, user, entry, rev); err != nil {
		return fmt.Errorf("stamp entry revision: %w", err)
	}
	return nil
}
