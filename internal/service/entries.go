package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/L-K-M/Earnest/internal/store"
	"github.com/L-K-M/Earnest/internal/syncmodel"
	"github.com/jackc/pgx/v5"
)

// EntryView selects which slice of the user's entries a list returns.
type EntryView string

const (
	// ViewRiver is the undecided, unread flow. Subscription and content mutes,
	// per-cadence decay and subscription initial_since apply only here.
	ViewRiver EntryView = "river"
	ViewLater EntryView = "later"
	ViewDone  EntryView = "done"
	ViewKept  EntryView = "kept"
	ViewAll   EntryView = "all"
)

// PageDirection orders a page by seq: forward moves through the stream in
// chronological order, backward returns the newest entries first.
type PageDirection string

const (
	PageForward  PageDirection = "forward"
	PageBackward PageDirection = "backward"
)

const (
	defaultEntryLimit = 50
	maxEntryLimit     = 200
	maxEntryIDs       = 200
	maxOpIDLength     = 1024

	// today_decay_days in users.settings controls when a today-cadence entry
	// leaves the river; invalid or absent values fall back to the default.
	defaultTodayDecayDays = 2
	maxTodayDecayDays     = 365
	weekDecay             = 7 * 24 * time.Hour
)

// Entry is one item seen through a user's subscription plus the resolved
// principal's state. Lists never carry bodies; Content serves them on demand.
type Entry struct {
	ID             int64
	SubscriptionID int64
	FeedID         int64
	Seq            uint64
	FirstSeenAt    time.Time
	PublishedAt    *time.Time
	UpdatedAt      *time.Time
	Title          string
	Blurb          *string
	URL            string
	Author         string
	Rev            uint64
	Deleted        bool
	State          syncmodel.State
	Content        *EntryContent `json:"content,omitempty"`
}

// EntryFilter selects and bounds an entry page. The zero value lists the
// first page of the river in chronological order.
type EntryFilter struct {
	View           EntryView
	Cadence        Cadence
	SubscriptionID int64
	FolderID       int64
	LabelID        int64
	// PrincipalID selects whose state supplies shelf/kept/unread filtering and
	// the returned State. Zero resolves to the user's human principal, so an
	// agent reads the user's river unless it asks for its own state.
	PrincipalID int64
	UnreadOnly  bool
	// AfterSeq and BeforeSeq bound seq exclusively on either side; both may
	// combine into a window. Direction picks the ordering, not the bound.
	AfterSeq  uint64
	BeforeSeq uint64
	Limit     int
	Direction PageDirection
}

// UnreadCounts summarises the resolved principal's unread entries per
// subscription, plus the Later and Kept totals, in one aggregate query.
type UnreadCounts struct {
	// BySubscription is keyed by subscription ID, not feed ID: the sidebar
	// counts per subscription row.
	BySubscription map[int64]int64 `json:"by_subscription"`
	Later          int64           `json:"later"`
	Kept           int64           `json:"kept"`
	Total          int64           `json:"total"`
}

// EntryContent is the sanitised body at the entry's current content hash.
type EntryContent struct {
	EntryID          int64
	HTML             string
	Text             string
	SanitizerVersion int
	Source           ContentSource
}

// Entries lists entries visible through the actor's active subscriptions.
func (s *Service) Entries(ctx context.Context, a Actor, f EntryFilter) ([]Entry, error) {
	if err := s.checkActor(ctx, a, ScopeEntriesRead); err != nil {
		return nil, err
	}
	principal, err := s.statePrincipal(ctx, a, f.PrincipalID)
	if err != nil {
		return nil, err
	}
	sql, args, err := s.entryPageQuery(ctx, a, f, principal)
	if err != nil {
		return nil, err
	}
	if sql == "" {
		return []Entry{}, nil
	}
	rows, err := s.store.Pool().Query(ctx, sql, args...)
	if err != nil {
		return nil, fmt.Errorf("list entries: %w", err)
	}
	defer rows.Close()
	page := []Entry{}
	for rows.Next() {
		var e Entry
		var seq int64
		var rev int64
		if err := rows.Scan(&e.ID, &e.SubscriptionID, &e.FeedID, &seq, &e.FirstSeenAt,
			&e.PublishedAt, &e.UpdatedAt, &e.Title, &e.URL, &e.Author, &rev, &e.Deleted); err != nil {
			return nil, fmt.Errorf("scan entry: %w", err)
		}
		e.Seq = uint64(seq)
		e.Rev = uint64(rev)
		page = append(page, e)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list entries: %w", err)
	}
	if err := s.attachBlurbs(ctx, a.UserID, page); err != nil {
		return nil, err
	}
	if err := s.attachStates(ctx, principal, page); err != nil {
		return nil, err
	}
	return page, nil
}

// Entry returns one entry visible to the actor, tombstones included.
func (s *Service) Entry(ctx context.Context, a Actor, id int64) (Entry, error) {
	if err := s.checkActor(ctx, a, ScopeEntriesRead); err != nil {
		return Entry{}, err
	}
	if id <= 0 {
		return Entry{}, fmt.Errorf("entry id: %w", ErrInvalid)
	}
	principal, err := s.statePrincipal(ctx, a, 0)
	if err != nil {
		return Entry{}, err
	}
	rows, err := store.LoadEntryRows(ctx, s.store.Pool(), a.UserID, []int64{id})
	if err != nil {
		return Entry{}, domainError(err)
	}
	if len(rows) == 0 {
		return Entry{}, ErrNotFound
	}
	e := entryFromRow(rows[0])
	states, err := store.LoadStates(ctx, s.store.Pool(), principal, []int64{id})
	if err != nil {
		return Entry{}, err
	}
	e.State = states[id]
	return e, nil
}

// GetByIDs returns the visible entries among ids in request order, silently
// omitting entries the user cannot see. Tombstones come back flagged.
func (s *Service) GetByIDs(ctx context.Context, a Actor, ids []int64) ([]Entry, error) {
	if err := s.checkActor(ctx, a, ScopeEntriesRead); err != nil {
		return nil, err
	}
	if len(ids) > maxEntryIDs {
		return nil, fmt.Errorf("more than %d entry ids: %w", maxEntryIDs, ErrInvalid)
	}
	principal, err := s.statePrincipal(ctx, a, 0)
	if err != nil {
		return nil, err
	}
	want := make([]int64, 0, len(ids))
	seen := map[int64]bool{}
	for _, id := range ids {
		if id <= 0 {
			return nil, fmt.Errorf("entry id %d: %w", id, ErrInvalid)
		}
		if !seen[id] {
			seen[id] = true
			want = append(want, id)
		}
	}
	rows, err := store.LoadEntryRows(ctx, s.store.Pool(), a.UserID, want)
	if err != nil {
		return nil, domainError(err)
	}
	states, err := store.LoadStates(ctx, s.store.Pool(), principal, want)
	if err != nil {
		return nil, err
	}
	byID := make(map[int64]store.EntryRow, len(rows))
	for _, r := range rows {
		byID[r.ID] = r
	}
	out := []Entry{}
	for _, id := range want {
		r, ok := byID[id]
		if !ok {
			continue
		}
		e := entryFromRow(r)
		e.State = states[id]
		if !e.Deleted {
			html, text, version, source, err := store.LoadContent(ctx, s.store.Pool(), id)
			if err != nil && !errors.Is(err, pgx.ErrNoRows) {
				return nil, err
			}
			if err == nil {
				e.Content = &EntryContent{EntryID: id, HTML: html, Text: text, SanitizerVersion: version, Source: ContentSource(source)}
			}
		}
		out = append(out, e)
	}
	if err := s.attachBlurbs(ctx, a.UserID, out); err != nil {
		return nil, err
	}
	return out, nil
}

// UnreadCounts returns counts for the same visibility rules as an
// all/unread_only entry page (active subscription, not deleted, unseen, not
// Done) without transferring one row per entry. A large backlog costs one
// aggregate scan instead of a page per 200 entries.
func (s *Service) UnreadCounts(ctx context.Context, a Actor) (UnreadCounts, error) {
	if err := s.checkActor(ctx, a, ScopeEntriesRead); err != nil {
		return UnreadCounts{}, err
	}
	principal, err := s.statePrincipal(ctx, a, 0)
	if err != nil {
		return UnreadCounts{}, err
	}
	rows, err := s.store.Pool().Query(ctx, `SELECT s.id, count(*), count(*) FILTER (WHERE st.shelf='later'), count(*) FILTER (WHERE st.kept)
		FROM entries e
		JOIN subscriptions s ON s.feed_id=e.feed_id AND s.user_id=$1 AND s.deleted_at IS NULL
		LEFT JOIN entry_state st ON st.principal_id=$2 AND st.entry_id=e.id
		WHERE e.deleted_at IS NULL AND COALESCE(st.seen,'unseen')='unseen' AND COALESCE(st.shelf,'none')<>'done'
		GROUP BY s.id`, a.UserID, principal)
	if err != nil {
		return UnreadCounts{}, fmt.Errorf("count unread: %w", err)
	}
	defer rows.Close()
	out := UnreadCounts{BySubscription: map[int64]int64{}}
	for rows.Next() {
		var id, total, later, kept int64
		if err := rows.Scan(&id, &total, &later, &kept); err != nil {
			return UnreadCounts{}, fmt.Errorf("scan unread count: %w", err)
		}
		out.BySubscription[id] = total
		out.Later += later
		out.Kept += kept
		out.Total += total
	}
	if err := rows.Err(); err != nil {
		return UnreadCounts{}, fmt.Errorf("count unread: %w", err)
	}
	return out, nil
}

// Content returns the sanitised body at the entry's current content hash.
// A deleted or never-ingested body is not found; clients treat 404 as a tombstone.
func (s *Service) Content(ctx context.Context, a Actor, id int64) (EntryContent, error) {
	if err := s.checkActor(ctx, a, ScopeEntriesRead); err != nil {
		return EntryContent{}, err
	}
	if id <= 0 {
		return EntryContent{}, fmt.Errorf("entry id: %w", ErrInvalid)
	}
	visible, err := store.EntryVisible(ctx, s.store.Pool(), a.UserID, id)
	if err != nil {
		return EntryContent{}, err
	}
	if !visible {
		return EntryContent{}, ErrNotFound
	}
	html, text, version, source, err := store.LoadContent(ctx, s.store.Pool(), id)
	if err != nil {
		return EntryContent{}, domainError(err)
	}
	return EntryContent{
		EntryID:          id,
		HTML:             html,
		Text:             text,
		SanitizerVersion: version,
		Source:           ContentSource(source),
	}, nil
}

func entryFromRow(r store.EntryRow) Entry {
	return Entry{
		ID:             r.ID,
		SubscriptionID: r.SubscriptionID,
		FeedID:         r.FeedID,
		Seq:            uint64(r.Seq),
		FirstSeenAt:    r.FirstSeenAt,
		PublishedAt:    r.PublishedAt,
		UpdatedAt:      r.UpdatedAt,
		Title:          r.Title,
		URL:            r.URL,
		Author:         r.Author,
		Rev:            uint64(r.Rev),
		Deleted:        r.Deleted,
	}
}

// statePrincipal resolves whose entry_state a read resolves against: the
// requested principal of the same user, else the user's human principal.
func (s *Service) statePrincipal(ctx context.Context, a Actor, requested int64) (int64, error) {
	pool := s.store.Pool()
	if requested == 0 {
		var id int64
		err := pool.QueryRow(ctx, "SELECT id FROM principals WHERE user_id = $1 AND kind = 'human'", a.UserID).Scan(&id)
		if errors.Is(err, pgx.ErrNoRows) {
			return 0, ErrUnauthorized
		}
		if err != nil {
			return 0, fmt.Errorf("resolve human principal: %w", err)
		}
		return id, nil
	}
	if requested < 0 {
		return 0, fmt.Errorf("principal_id: %w", ErrInvalid)
	}
	var ok bool
	if err := pool.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM principals WHERE id = $1 AND user_id = $2)", requested, a.UserID).Scan(&ok); err != nil {
		return 0, fmt.Errorf("resolve principal: %w", err)
	}
	if !ok {
		return 0, ErrForbidden
	}
	return requested, nil
}

func (s *Service) attachStates(ctx context.Context, principal int64, page []Entry) error {
	ids := make([]int64, len(page))
	for i, e := range page {
		ids[i] = e.ID
	}
	states, err := store.LoadStates(ctx, s.store.Pool(), principal, ids)
	if err != nil {
		return err
	}
	for i := range page {
		page[i].State = states[page[i].ID]
	}
	return nil
}

// attachBlurbs reads only current publisher content annotations for this user.
func (s *Service) attachBlurbs(ctx context.Context, user int64, entries []Entry) error {
	if len(entries) == 0 {
		return nil
	}
	ids := make([]int64, len(entries))
	for i := range entries {
		ids[i] = entries[i].ID
	}
	rows, err := s.store.Pool().Query(ctx, `SELECT DISTINCT ON (e.id) e.id, a.value FROM entries e JOIN annotations a ON a.user_id=$1 AND a.kind='blurb' AND a.entry_ids=ARRAY[e.id] AND a.content_hash=e.content_hash AND a.deleted_at IS NULL JOIN principals p ON p.id=a.principal_id AND p.kind='pipeline' AND p.name='Summaries' WHERE e.id=ANY($2) ORDER BY e.id,a.id DESC`, user, ids)
	if err != nil {
		return fmt.Errorf("list blurbs: %w", err)
	}
	defer rows.Close()
	values := map[int64]string{}
	for rows.Next() {
		var id int64
		var value string
		if err := rows.Scan(&id, &value); err != nil {
			return fmt.Errorf("scan blurb: %w", err)
		}
		values[id] = value
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("list blurbs: %w", err)
	}
	for i := range entries {
		if value, ok := values[entries[i].ID]; ok {
			entries[i].Blurb = &value
		}
	}
	return nil
}

// todayDecay reads the user's today-cadence decay window; a missing or
// invalid setting falls back to the default rather than breaking the river.
func (s *Service) todayDecay(ctx context.Context, user int64) (time.Duration, error) {
	var raw *string
	if err := s.store.Pool().QueryRow(ctx, "SELECT settings->>'today_decay_days' FROM users WHERE id = $1", user).Scan(&raw); err != nil {
		return 0, fmt.Errorf("read today_decay_days: %w", err)
	}
	days := defaultTodayDecayDays
	if raw != nil {
		if n, err := strconv.Atoi(strings.TrimSpace(*raw)); err == nil && n >= 1 && n <= maxTodayDecayDays {
			days = n
		}
	}
	return time.Duration(days) * 24 * time.Hour, nil
}

// entryPageQuery builds the filtered page. State-based views filter the
// resolved principal's registers through the LEFT JOIN on entry_state.
func (s *Service) entryPageQuery(ctx context.Context, a Actor, f EntryFilter, principal int64) (string, []any, error) {
	for _, v := range []int64{f.SubscriptionID, f.FolderID, f.LabelID} {
		if v < 0 {
			return "", nil, fmt.Errorf("negative filter id: %w", ErrInvalid)
		}
	}
	if f.Limit < 0 || f.Limit > maxEntryLimit {
		return "", nil, fmt.Errorf("limit outside 0..%d: %w", maxEntryLimit, ErrInvalid)
	}
	if f.Direction != "" && f.Direction != PageForward && f.Direction != PageBackward {
		return "", nil, fmt.Errorf("direction %q: %w", f.Direction, ErrInvalid)
	}
	if f.Cadence != "" {
		switch string(f.Cadence) {
		case "now", "today", "week":
		default:
			return "", nil, fmt.Errorf("cadence %q: %w", string(f.Cadence), ErrInvalid)
		}
	}
	// An after-bound above bigint cannot match any seq.
	if f.AfterSeq > math.MaxInt64 {
		return "", nil, nil
	}
	args := []any{a.UserID, principal}
	arg := func(v any) string {
		args = append(args, v)
		return fmt.Sprintf("$%d", len(args))
	}
	conds := []string{"e.deleted_at IS NULL"}
	switch f.View {
	case "", ViewRiver:
		conds = append(conds,
			"COALESCE(st.seen, 'unseen') = 'unseen'",
			"COALESCE(st.shelf, 'none') = 'none'",
			"NOT s.muted",
			"e.first_seen_at >= s.initial_since",
			"(e.seq > s.initial_through_seq OR e.published_at IS NULL OR e.published_at >= s.initial_since)",
			`NOT EXISTS (SELECT 1 FROM mutes m WHERE m.user_id = $1 AND m.deleted_at IS NULL AND
				((m.kind = 'author' AND lower(m.value) = lower(e.author)) OR
				 (m.kind = 'keyword' AND position(lower(m.value) in lower(e.title)) > 0)))`)
		decay, err := s.todayDecay(ctx, a.UserID)
		if err != nil {
			return "", nil, err
		}
		// Now-cadence items stay until acted on; today and week decay by age
		// from first_seen. A future first_seen sorts before either cutoff.
		conds = append(conds, "(s.cadence = 'now' OR e.first_seen_at >= CASE s.cadence "+
			"WHEN 'week' THEN "+arg(s.now().UTC().Add(-weekDecay))+"::timestamptz ELSE "+
			arg(s.now().UTC().Add(-decay))+"::timestamptz END)")
	case ViewLater:
		conds = append(conds, "st.shelf = 'later'")
	case ViewDone:
		conds = append(conds, "st.shelf = 'done'")
	case ViewKept:
		conds = append(conds, "st.kept")
	case ViewAll:
	default:
		return "", nil, fmt.Errorf("view %q: %w", f.View, ErrInvalid)
	}
	if f.UnreadOnly {
		conds = append(conds, "COALESCE(st.seen, 'unseen') = 'unseen'", "COALESCE(st.shelf, 'none') <> 'done'")
	}
	if f.SubscriptionID != 0 {
		conds = append(conds, "s.id = "+arg(f.SubscriptionID))
	}
	if f.FolderID != 0 {
		conds = append(conds, "s.folder_id = "+arg(f.FolderID))
	}
	if f.LabelID != 0 {
		conds = append(conds, "EXISTS (SELECT 1 FROM entry_labels el WHERE el.user_id = $1 "+
			"AND el.entry_id = e.id AND el.label_id = "+arg(f.LabelID)+" AND el.deleted_at IS NULL)")
	}
	if f.Cadence != "" {
		conds = append(conds, "s.cadence = "+arg(string(f.Cadence)))
	}
	if f.AfterSeq != 0 {
		conds = append(conds, "e.seq > "+arg(int64(f.AfterSeq)))
	}
	if f.BeforeSeq != 0 && f.BeforeSeq <= math.MaxInt64 {
		conds = append(conds, "e.seq < "+arg(int64(f.BeforeSeq)))
	}
	limit := f.Limit
	if limit == 0 {
		limit = defaultEntryLimit
	}
	order := "ASC"
	if f.Direction == PageBackward {
		order = "DESC"
	}
	sql := `SELECT e.id, s.id, e.feed_id, e.seq, e.first_seen_at, e.published_at, e.updated_at,
		e.title, COALESCE(e.url, ''), e.author, COALESCE(er.rev, 0), e.deleted_at IS NOT NULL
		FROM entries e
		JOIN subscriptions s ON s.feed_id = e.feed_id AND s.user_id = $1 AND s.deleted_at IS NULL
		LEFT JOIN entry_revisions er ON er.user_id = $1 AND er.entry_id = e.id
		LEFT JOIN entry_state st ON st.principal_id = $2 AND st.entry_id = e.id
		WHERE ` + strings.Join(conds, " AND ") +
		" ORDER BY e.seq " + order + " LIMIT " + arg(int64(limit))
	return sql, args, nil
}

// changeRev allocates one revision for the user's change log ahead of the
// mutation so the resource row and its event carry the same rev.
func changeRev(ctx context.Context, tx pgx.Tx, user int64) (int64, error) {
	alloc, err := store.AllocateCounters(ctx, tx, 0, map[int64]int64{user: 1})
	if err != nil {
		return 0, err
	}
	return alloc.Revs[user].First, nil
}

// emitChange writes one change event at an already allocated revision. The
// payload is a syncmodel.Change field fragment, e.g. {"object": {...}}.
func (s *Service) emitChange(ctx context.Context, tx pgx.Tx, user, rev int64, kind syncmodel.ChangeType, field string, value any) error {
	payload := map[string]any{field: value}
	// Protocol objects accompany richer native projections so model clients
	// merge lifecycle events with the same identities used during bootstrap.
	switch object := value.(type) {
	case Subscription:
		payload["object"] = syncmodel.Object{Kind: syncmodel.SubscriptionObject, ID: syncmodel.ID(object.ID), FolderID: syncmodel.ID(object.FolderID), Name: object.Title, Rev: object.Rev, Deleted: object.Deleted}
	case Folder:
		payload["object"] = syncmodel.Object{Kind: syncmodel.FolderObject, ID: syncmodel.ID(object.ID), Name: object.Name, Rev: object.Rev, Deleted: object.Deleted}
	}
	return insertEvent(ctx, tx, user, rev, string(kind), payload, s.now().UTC())
}

// replayOp registers (principal, opID) inside the locked mutation. A fresh
// registration returns nil; a repeat returns the stored result when the
// method matches, and rejects the op_id when another method claimed it.
func replayOp(ctx context.Context, tx pgx.Tx, principal int64, method, opID string) (json.RawMessage, error) {
	fresh, err := store.InsertOpEnvelope(ctx, tx, principal, opID, method)
	if err != nil {
		return nil, domainError(err)
	}
	if fresh {
		return nil, nil
	}
	env, err := store.LoadOpEnvelope(ctx, tx, principal, opID)
	if err != nil {
		return nil, domainError(err)
	}
	if env.Method != method {
		return nil, fmt.Errorf("op_id already used by %s: %w", env.Method, ErrInvalid)
	}
	return env.Result, nil
}

func validOpID(opID string) bool {
	return opID != "" && len(opID) <= maxOpIDLength
}
