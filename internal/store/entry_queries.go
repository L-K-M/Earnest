package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/L-K-M/Earnest/internal/syncmodel"
	"github.com/jackc/pgx/v5"
)

// entryQuerier is the read subset of *pgxpool.Pool and pgx.Tx.
type entryQuerier interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// EntryRow is the subscription-scoped projection of one entries row: the
// per-user change revision comes from entry_revisions, not the shared row.
type EntryRow struct {
	ID             int64
	SubscriptionID int64
	FeedID         int64
	Seq            int64
	FirstSeenAt    time.Time
	PublishedAt    *time.Time
	UpdatedAt      *time.Time
	Title          string
	URL            string
	Author         string
	Rev            int64
	Deleted        bool
}

const entryRowColumns = `e.id, s.id, e.feed_id, e.seq, e.first_seen_at, e.published_at, e.updated_at,
	e.title, COALESCE(e.url, ''), e.author, COALESCE(er.rev, 0), e.deleted_at IS NOT NULL`

const entryRowJoin = `FROM entries e
	JOIN subscriptions s ON s.feed_id = e.feed_id AND s.user_id = $1 AND s.deleted_at IS NULL
	LEFT JOIN entry_revisions er ON er.user_id = $1 AND er.entry_id = e.id`

func scanEntryRow(row pgx.Row) (EntryRow, error) {
	var r EntryRow
	err := row.Scan(&r.ID, &r.SubscriptionID, &r.FeedID, &r.Seq, &r.FirstSeenAt,
		&r.PublishedAt, &r.UpdatedAt, &r.Title, &r.URL, &r.Author, &r.Rev, &r.Deleted)
	return r, err
}

// LoadEntryRows returns the user's subscription-visible entries among ids,
// including retention tombstones, in entry ID order.
func LoadEntryRows(ctx context.Context, q entryQuerier, user int64, ids []int64) ([]EntryRow, error) {
	rows, err := q.Query(ctx, "SELECT "+entryRowColumns+" "+entryRowJoin+" WHERE e.id = ANY($2) ORDER BY e.id", user, ids)
	if err != nil {
		return nil, fmt.Errorf("load entries: %w", err)
	}
	defer rows.Close()
	out := []EntryRow{}
	for rows.Next() {
		r, err := scanEntryRow(rows)
		if err != nil {
			return nil, fmt.Errorf("scan entry: %w", err)
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// EntryVisible reports whether the entry is reachable through one of the
// user's active subscriptions and is not a retention tombstone.
func EntryVisible(ctx context.Context, q entryQuerier, user, entry int64) (bool, error) {
	var ok bool
	err := q.QueryRow(ctx, `SELECT EXISTS(
		SELECT 1 FROM entries e
		JOIN subscriptions s ON s.feed_id = e.feed_id AND s.user_id = $1 AND s.deleted_at IS NULL
		WHERE e.id = $2 AND e.deleted_at IS NULL)`, user, entry).Scan(&ok)
	if err != nil {
		return false, fmt.Errorf("check entry visibility: %w", err)
	}
	return ok, nil
}

// LoadContent returns the sanitised body for the entry's current content hash,
// preferring extracted text, then the highest sanitizer version within a
// source. That ordering matches update_entry_search so display and search
// never disagree about which body is current.
func LoadContent(ctx context.Context, q entryQuerier, entry int64) (html, text string, version int, source string, err error) {
	err = q.QueryRow(ctx, `SELECT c.html, c.text, c.sanitizer_version, c.source
		FROM contents c JOIN entries e ON e.id = c.entry_id
		WHERE c.entry_id = $1 AND c.content_hash = e.content_hash
		ORDER BY (c.source = 'extracted') DESC, c.sanitizer_version DESC
		LIMIT 1`, entry).Scan(&html, &text, &version, &source)
	if err != nil {
		return "", "", 0, "", fmt.Errorf("load content: %w", err)
	}
	return html, text, version, source, nil
}

// stateColumns is one entry_state row in scan order.
type stateColumns struct {
	entry                                int64
	seen, shelf                          string
	kept                                 bool
	seenRev, shelfRev, keptRev, stateRev int64
	seenAt, shelfAt, keptAt              *time.Time
	seenActor, shelfActor, keptActor     *int64
	seenSource, shelfSource, keptSource  *string
}

func (c *stateColumns) scan(row pgx.Row) error {
	return row.Scan(&c.entry, &c.seen, &c.seenRev, &c.seenAt, &c.seenActor, &c.seenSource,
		&c.shelf, &c.shelfRev, &c.shelfAt, &c.shelfActor, &c.shelfSource,
		&c.kept, &c.keptRev, &c.keptAt, &c.keptActor, &c.keptSource, &c.stateRev)
}

const entryStateColumns = `entry_id, seen, seen_rev, seen_occurred_at, seen_actor, seen_source,
	shelf, shelf_rev, shelf_occurred_at, shelf_actor, shelf_source,
	kept, kept_rev, kept_occurred_at, kept_actor, kept_source, state_rev`

// stateSource folds the retired per-kind provenance values into the combined
// rule_or_agent class the model and the 0002 migration use.
func stateSource(v string) syncmodel.Source {
	if v == "rule" || v == "agent" {
		return syncmodel.SourceRuleOrAgent
	}
	return syncmodel.Source(v)
}

func fillRegister(reg *syncmodel.Register, rev int64, at *time.Time, actor *int64, source *string) {
	if rev <= 0 {
		return
	}
	reg.Rev = uint64(rev)
	if at != nil {
		reg.OccurredAt = at.UTC()
	}
	if actor != nil {
		reg.Actor = syncmodel.ID(*actor)
	}
	if source != nil {
		reg.Source = stateSource(*source)
	}
}

func (c stateColumns) state(principal int64) syncmodel.State {
	out := syncmodel.InitialState(syncmodel.ID(principal), syncmodel.ID(c.entry))
	out.Seen.Value = syncmodel.Value(c.seen)
	out.Shelf.Value = syncmodel.Value(c.shelf)
	if c.kept {
		out.Kept.Value = syncmodel.True
	}
	out.StateRev = uint64(c.stateRev)
	fillRegister(&out.Seen, c.seenRev, c.seenAt, c.seenActor, c.seenSource)
	fillRegister(&out.Shelf, c.shelfRev, c.shelfAt, c.shelfActor, c.shelfSource)
	fillRegister(&out.Kept, c.keptRev, c.keptAt, c.keptActor, c.keptSource)
	return out
}

// LoadStates loads every present entry_state row for (principal, ids). Absent
// rows report InitialState, matching the service's unread default.
func LoadStates(ctx context.Context, q entryQuerier, principal int64, ids []int64) (map[int64]syncmodel.State, error) {
	out := make(map[int64]syncmodel.State, len(ids))
	for _, id := range ids {
		out[id] = syncmodel.InitialState(syncmodel.ID(principal), syncmodel.ID(id))
	}
	if len(ids) == 0 {
		return out, nil
	}
	rows, err := q.Query(ctx, "SELECT "+entryStateColumns+" FROM entry_state WHERE principal_id = $1 AND entry_id = ANY($2)", principal, ids)
	if err != nil {
		return nil, fmt.Errorf("load entry states: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var c stateColumns
		if err := c.scan(rows); err != nil {
			return nil, fmt.Errorf("scan entry state: %w", err)
		}
		out[c.entry] = c.state(principal)
	}
	return out, rows.Err()
}

// LabelRow is one labels row including its tombstone.
type LabelRow struct {
	ID      int64
	Name    string
	Rev     int64
	Deleted bool
}

// LoadLabels returns all of the user's label definitions, tombstones
// included, ordered by stable ID for bootstrap.
func LoadLabels(ctx context.Context, q entryQuerier, user int64) ([]LabelRow, error) {
	rows, err := q.Query(ctx, "SELECT id, name, rev, deleted_at IS NOT NULL FROM labels WHERE user_id = $1 ORDER BY id", user)
	if err != nil {
		return nil, fmt.Errorf("load labels: %w", err)
	}
	defer rows.Close()
	out := []LabelRow{}
	for rows.Next() {
		var l LabelRow
		if err := rows.Scan(&l.ID, &l.Name, &l.Rev, &l.Deleted); err != nil {
			return nil, fmt.Errorf("scan label: %w", err)
		}
		out = append(out, l)
	}
	return out, rows.Err()
}

// LoadLabel returns one label definition owned by the user.
func LoadLabel(ctx context.Context, q entryQuerier, user, label int64) (LabelRow, error) {
	var l LabelRow
	err := q.QueryRow(ctx, "SELECT id, name, rev, deleted_at IS NOT NULL FROM labels WHERE id = $1 AND user_id = $2", label, user).
		Scan(&l.ID, &l.Name, &l.Rev, &l.Deleted)
	if err != nil {
		return LabelRow{}, fmt.Errorf("load label: %w", err)
	}
	return l, nil
}

// InsertLabel creates a label definition carrying the allocated change revision.
func InsertLabel(ctx context.Context, tx pgx.Tx, user int64, name string, rev int64) (int64, error) {
	var id int64
	err := tx.QueryRow(ctx, "INSERT INTO labels (user_id, name, rev) VALUES ($1, $2, $3) RETURNING id", user, name, rev).Scan(&id)
	if err != nil {
		return 0, fmt.Errorf("insert label: %w", err)
	}
	return id, nil
}

// SetLabelName renames a label and stamps its last-change revision.
func SetLabelName(ctx context.Context, tx pgx.Tx, label int64, name string, rev int64) error {
	if _, err := tx.Exec(ctx, "UPDATE labels SET name = $2, rev = $3 WHERE id = $1", label, name, rev); err != nil {
		return fmt.Errorf("rename label: %w", err)
	}
	return nil
}

// TombstoneLabel marks a label deleted, keeping the revisioned tombstone.
func TombstoneLabel(ctx context.Context, tx pgx.Tx, label, rev int64, at time.Time) error {
	if _, err := tx.Exec(ctx, "UPDATE labels SET deleted_at = $2, rev = $3 WHERE id = $1", label, at, rev); err != nil {
		return fmt.Errorf("delete label: %w", err)
	}
	return nil
}

// VisibleEntries filters entries down to those reachable through one of the
// user's active subscriptions and not retention-tombstoned.
func VisibleEntries(ctx context.Context, q entryQuerier, user int64, entries []int64) (map[int64]bool, error) {
	out := make(map[int64]bool, len(entries))
	if len(entries) == 0 {
		return out, nil
	}
	rows, err := q.Query(ctx, `SELECT e.id FROM entries e
		JOIN subscriptions s ON s.feed_id = e.feed_id AND s.user_id = $1 AND s.deleted_at IS NULL
		WHERE e.id = ANY($2) AND e.deleted_at IS NULL`, user, entries)
	if err != nil {
		return nil, fmt.Errorf("check entry visibility: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("scan visible entry: %w", err)
		}
		out[id] = true
	}
	return out, rows.Err()
}

// MembershipRow is one stored entry-label membership.
type MembershipRow struct {
	ID      int64
	Rev     int64
	Deleted bool
}

// LoadMemberships returns every membership row of (user, label) across entries.
func LoadMemberships(ctx context.Context, q entryQuerier, user, label int64, entries []int64) (map[int64]MembershipRow, error) {
	out := make(map[int64]MembershipRow, len(entries))
	if len(entries) == 0 {
		return out, nil
	}
	rows, err := q.Query(ctx, `SELECT id, entry_id, rev, deleted_at IS NOT NULL FROM entry_labels
		WHERE user_id = $1 AND label_id = $2 AND entry_id = ANY($3)`, user, label, entries)
	if err != nil {
		return nil, fmt.Errorf("load label memberships: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var m MembershipRow
		var entry int64
		if err := rows.Scan(&m.ID, &entry, &m.Rev, &m.Deleted); err != nil {
			return nil, fmt.Errorf("scan label membership: %w", err)
		}
		out[entry] = m
	}
	return out, rows.Err()
}

// LoadMembership returns the entry-label membership row and whether it is tombstoned.
func LoadMembership(ctx context.Context, q entryQuerier, user, entry, label int64) (rev int64, deleted, found bool, err error) {
	var deletedAt *time.Time
	err = q.QueryRow(ctx, "SELECT rev, deleted_at FROM entry_labels WHERE user_id = $1 AND entry_id = $2 AND label_id = $3", user, entry, label).
		Scan(&rev, &deletedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, false, false, nil
	}
	if err != nil {
		return 0, false, false, fmt.Errorf("load label membership: %w", err)
	}
	return rev, deletedAt != nil, true, nil
}

// UpsertMembership creates or resurrects one membership at rev.
func UpsertMembership(ctx context.Context, tx pgx.Tx, user, entry, label, rev int64) error {
	_, err := tx.Exec(ctx, `INSERT INTO entry_labels (user_id, entry_id, label_id, rev) VALUES ($1, $2, $3, $4)
		ON CONFLICT (user_id, entry_id, label_id) DO UPDATE SET deleted_at = NULL, rev = $4`, user, entry, label, rev)
	if err != nil {
		return fmt.Errorf("add label membership: %w", err)
	}
	return nil
}

// TombstoneMembership marks one membership deleted at rev.
func TombstoneMembership(ctx context.Context, tx pgx.Tx, user, entry, label, rev int64, at time.Time) error {
	if _, err := tx.Exec(ctx, "UPDATE entry_labels SET deleted_at = $4, rev = $5 WHERE user_id = $1 AND entry_id = $2 AND label_id = $3", user, entry, label, at, rev); err != nil {
		return fmt.Errorf("remove label membership: %w", err)
	}
	return nil
}

// UpsertMemberships creates or resurrects each membership at the rev paired
// with its entry, returning each entry's stable membership identity. Entries
// must be distinct: an ON CONFLICT clause cannot touch the same row twice.
func UpsertMemberships(ctx context.Context, tx pgx.Tx, user, label int64, entries, revs []int64) (map[int64]int64, error) {
	if len(entries) != len(revs) {
		return nil, fmt.Errorf("entries and revisions must pair")
	}
	out := make(map[int64]int64, len(entries))
	if len(entries) == 0 {
		return out, nil
	}
	rows, err := tx.Query(ctx, `INSERT INTO entry_labels (user_id, entry_id, label_id, rev)
		SELECT $1, e, $2, r FROM unnest($3::bigint[], $4::bigint[]) AS u(e, r)
		ON CONFLICT (user_id, entry_id, label_id) DO UPDATE SET deleted_at = NULL, rev = EXCLUDED.rev
		RETURNING id, entry_id`, user, label, entries, revs)
	if err != nil {
		return nil, fmt.Errorf("add label memberships: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var id, entry int64
		if err := rows.Scan(&id, &entry); err != nil {
			return nil, fmt.Errorf("scan membership identity: %w", err)
		}
		out[entry] = id
	}
	return out, rows.Err()
}

// TombstoneMemberships marks each listed membership deleted at the rev paired
// with its entry.
func TombstoneMemberships(ctx context.Context, tx pgx.Tx, user, label int64, entries, revs []int64, at time.Time) error {
	if len(entries) != len(revs) {
		return fmt.Errorf("entries and revisions must pair")
	}
	if len(entries) == 0 {
		return nil
	}
	if _, err := tx.Exec(ctx, `UPDATE entry_labels el SET deleted_at = $4, rev = u.rev
		FROM unnest($2::bigint[], $3::bigint[]) AS u(entry, rev)
		WHERE el.user_id = $1 AND el.label_id = $5 AND el.entry_id = u.entry`, user, entries, revs, at, label); err != nil {
		return fmt.Errorf("remove label memberships: %w", err)
	}
	return nil
}

// ActiveMembershipEntries lists entry IDs holding an active membership in label.
func ActiveMembershipEntries(ctx context.Context, q entryQuerier, user, label int64) ([]int64, error) {
	rows, err := q.Query(ctx, "SELECT entry_id FROM entry_labels WHERE user_id = $1 AND label_id = $2 AND deleted_at IS NULL ORDER BY entry_id", user, label)
	if err != nil {
		return nil, fmt.Errorf("list label memberships: %w", err)
	}
	defer rows.Close()
	out := []int64{}
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("scan membership: %w", err)
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

// LoadPositions returns every resume position of one principal.
func LoadPositions(ctx context.Context, q entryQuerier, principal int64) ([]syncmodel.Position, error) {
	rows, err := q.Query(ctx, "SELECT view, anchor_seq, rev FROM positions WHERE principal_id = $1 ORDER BY view", principal)
	if err != nil {
		return nil, fmt.Errorf("load positions: %w", err)
	}
	defer rows.Close()
	out := []syncmodel.Position{}
	for rows.Next() {
		var p syncmodel.Position
		if err := rows.Scan(&p.View, &p.Seq, &p.Rev); err != nil {
			return nil, fmt.Errorf("scan position: %w", err)
		}
		p.Principal = syncmodel.ID(principal)
		out = append(out, p)
	}
	return out, rows.Err()
}

// LoadPosition returns one view's position, or pgx.ErrNoRows when never written.
func LoadPosition(ctx context.Context, q entryQuerier, principal int64, view string) (syncmodel.Position, error) {
	p := syncmodel.Position{Principal: syncmodel.ID(principal), View: view}
	err := q.QueryRow(ctx, "SELECT anchor_seq, rev FROM positions WHERE principal_id = $1 AND view = $2", principal, view).Scan(&p.Seq, &p.Rev)
	if err != nil {
		return syncmodel.Position{}, fmt.Errorf("load position: %w", err)
	}
	return p, nil
}

// UpsertPosition commits the latest writer's anchor unconditionally: positions
// are last-writer-wins, including backward moves.
func UpsertPosition(ctx context.Context, tx pgx.Tx, principal int64, view string, seq uint64, rev int64) error {
	_, err := tx.Exec(ctx, `INSERT INTO positions (principal_id, view, anchor_seq, rev) VALUES ($1, $2, $3, $4)
		ON CONFLICT (principal_id, view) DO UPDATE SET anchor_seq = $3, rev = $4`, principal, view, int64(seq), rev)
	if err != nil {
		return fmt.Errorf("put position: %w", err)
	}
	return nil
}

// CurrentSeq reads the ingest counter inside its lock during a mutation.
func CurrentSeq(ctx context.Context, tx pgx.Tx) (int64, error) {
	var seq int64
	if err := tx.QueryRow(ctx, "SELECT value FROM seq_counter WHERE singleton").Scan(&seq); err != nil {
		return 0, fmt.Errorf("read seq counter: %w", err)
	}
	return seq, nil
}

// OpEnvelope is the stored op_ids deduplication record: the method that ran
// plus its committed result for replay.
type OpEnvelope struct {
	Method string          `json:"method"`
	Result json.RawMessage `json:"result"`
}

// InsertOpEnvelope registers (principal, opID) inside the locked mutation.
// A fresh insert returns true; a conflict returns false and the caller loads
// the committed envelope with LoadOpEnvelope to replay or reject reuse.
func InsertOpEnvelope(ctx context.Context, tx pgx.Tx, principal int64, opID, method string) (bool, error) {
	tag, err := tx.Exec(ctx, `INSERT INTO op_ids (principal_id, op_id, result)
		VALUES ($1, $2, jsonb_build_object('method', $3::text)) ON CONFLICT DO NOTHING`, principal, opID, method)
	if err != nil {
		return false, fmt.Errorf("register operation: %w", err)
	}
	return tag.RowsAffected() == 1, nil
}

// LoadOpEnvelope reads the committed record for a replayed op_id.
func LoadOpEnvelope(ctx context.Context, tx pgx.Tx, principal int64, opID string) (OpEnvelope, error) {
	var body []byte
	if err := tx.QueryRow(ctx, "SELECT result FROM op_ids WHERE principal_id = $1 AND op_id = $2", principal, opID).Scan(&body); err != nil {
		return OpEnvelope{}, fmt.Errorf("load operation: %w", err)
	}
	var env OpEnvelope
	if err := json.Unmarshal(body, &env); err != nil {
		return OpEnvelope{}, fmt.Errorf("decode operation record: %w", err)
	}
	return env, nil
}

// FinishOp stores the mutation's committed result for future replays.
func FinishOp(ctx context.Context, tx pgx.Tx, principal int64, opID string, result any) error {
	body, err := json.Marshal(result)
	if err != nil {
		return fmt.Errorf("encode operation result: %w", err)
	}
	if _, err := tx.Exec(ctx, `UPDATE op_ids SET result = result || jsonb_build_object('result', $3::jsonb)
		WHERE principal_id = $1 AND op_id = $2`, principal, opID, string(body)); err != nil {
		return fmt.Errorf("complete operation: %w", err)
	}
	return nil
}

// MembershipID returns the stable identity of an entry-label assignment.
func MembershipID(ctx context.Context, q entryQuerier, user, entry, label int64) (int64, error) {
	var id int64
	if err := q.QueryRow(ctx, "SELECT id FROM entry_labels WHERE user_id=$1 AND entry_id=$2 AND label_id=$3", user, entry, label).Scan(&id); err != nil {
		return 0, fmt.Errorf("membership identity: %w", err)
	}
	return id, nil
}
