package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/L-K-M/Earnest/internal/store"
	"github.com/L-K-M/Earnest/internal/syncmodel"
	"github.com/jackc/pgx/v5"
)

// Cadence chooses which river a subscription belongs to.
type Cadence string

const (
	CadenceNow   Cadence = "now"
	CadenceToday Cadence = "today"
	CadenceWeek  Cadence = "week"
)

// FeedHealth reports the last attempt, schedule and posting rate.
type FeedHealth struct {
	LastSuccessAt       *time.Time `json:"last_success_at,omitempty"`
	LastAttemptAt       *time.Time `json:"last_attempt_at,omitempty"`
	NextFetchAt         time.Time  `json:"next_fetch_at"`
	ErrorClass          string     `json:"error_class,omitempty"`
	ErrorMessage        string     `json:"error_message,omitempty"`
	ConsecutiveFailures int        `json:"consecutive_failures"`
	HTTPStatus          int        `json:"http_status"`
	PostsPerDay         float64    `json:"posts_per_day"`
	Firehose            bool       `json:"firehose"`
}

// Subscription projects user settings without revealing credentials.
type Subscription struct {
	InitialSince        *time.Time  `json:"initial_since,omitempty"`
	InitialThroughSeq   uint64      `json:"initial_through_seq"`
	InitialFetchPending bool        `json:"initial_fetch_pending"`
	ID                  int64       `json:"id"`
	FeedID              int64       `json:"feed_id"`
	Title               string      `json:"title"`
	URL                 string      `json:"url"`
	SiteURL             string      `json:"site_url"`
	FolderID            int64       `json:"folder_id"`
	Cadence             Cadence     `json:"cadence"`
	Extract             ExtractMode `json:"extract"`
	Rev                 uint64      `json:"rev"`
	Deleted             bool        `json:"deleted"`
	Private             bool        `json:"private"`
	Health              FeedHealth  `json:"health"`
}

// SubscribeOptions supplies user settings and optional private credentials.
type SubscribeOptions struct {
	Title       string
	FolderID    int64
	Cadence     Cadence
	Extract     ExtractMode
	Credentials *FeedCredentials
	OpID        string
}

// UpdateSubscriptionOptions patches provided fields. CredentialsSet permits
// clearing credentials; changing privacy rebinds to a separate feed.
type UpdateSubscriptionOptions struct {
	Title          *string
	FolderID       *int64
	Cadence        *Cadence
	Extract        *ExtractMode
	Credentials    *FeedCredentials
	CredentialsSet bool
	OpID           string
}

func normalizedFeedURL(raw string) (string, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u == nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" || u.User != nil || len(raw) > 8192 {
		return "", fmt.Errorf("feed URL: %w", ErrInvalid)
	}
	u.Host = strings.ToLower(u.Host)
	if port := u.Port(); (u.Scheme == "http" && port == "80") || (u.Scheme == "https" && port == "443") {
		host := u.Hostname()
		if strings.Contains(host, ":") {
			host = "[" + host + "]"
		}
		u.Host = host
	}
	u.Fragment = ""
	if u.Path == "" {
		u.Path = "/"
	}
	return u.String(), nil
}

func credentialOrigin(raw string) string {
	u, _ := url.Parse(raw) // Callers have already validated and normalized the URL.
	port := u.Port()
	if port == "" {
		port = "80"
		if u.Scheme == "https" {
			port = "443"
		}
	}
	return u.Scheme + "://" + u.Hostname() + ":" + port
}
func validCadence(c Cadence) bool { return c == CadenceNow || c == CadenceToday || c == CadenceWeek }
func subscriptionExtract(mode ExtractMode) (string, error) {
	switch mode {
	case "":
		return "inherit", nil
	case ExtractNone:
		return "never", nil
	case ExtractAlways:
		return "always", nil
	}
	return "", fmt.Errorf("extract mode: %w", ErrInvalid)
}
func readExtract(mode string) ExtractMode {
	if mode == "always" {
		return ExtractAlways
	}
	if mode == "never" {
		return ExtractNone
	}
	return ""
}

const subscriptionColumns = `s.id,s.feed_id,coalesce(s.title_override,f.title),f.url,coalesce(f.site_url,''),coalesce(s.folder_id,0),s.cadence,s.full_text_mode,s.rev,s.deleted_at IS NOT NULL,f.owner_user_id IS NOT NULL,
 f.last_success_at,f.last_attempt_at,f.next_fetch_at,coalesce(f.error_class,''),coalesce(f.error_message,''),f.consecutive_failures,coalesce(f.last_http_status,0),f.posts_per_day,CASE WHEN isfinite(s.initial_since) THEN s.initial_since ELSE NULL END,s.initial_through_seq,s.initial_fetch_pending`

func scanSubscription(row pgx.Row) (Subscription, error) {
	var sub Subscription
	var extract string
	err := row.Scan(&sub.ID, &sub.FeedID, &sub.Title, &sub.URL, &sub.SiteURL, &sub.FolderID, &sub.Cadence, &extract, &sub.Rev, &sub.Deleted, &sub.Private,
		&sub.Health.LastSuccessAt, &sub.Health.LastAttemptAt, &sub.Health.NextFetchAt, &sub.Health.ErrorClass, &sub.Health.ErrorMessage, &sub.Health.ConsecutiveFailures, &sub.Health.HTTPStatus, &sub.Health.PostsPerDay, &sub.InitialSince, &sub.InitialThroughSeq, &sub.InitialFetchPending)
	sub.Extract = readExtract(extract)
	sub.Health.Firehose = sub.Health.PostsPerDay > firehoseThreshold
	return sub, err
}
func subscriptionTx(ctx context.Context, q store.StateReader, user, id int64) (Subscription, error) {
	sub, err := scanSubscription(q.QueryRow(ctx, "SELECT "+subscriptionColumns+" FROM subscriptions s JOIN feeds f ON f.id=s.feed_id WHERE s.user_id=$1 AND s.id=$2", user, id))
	return sub, domainError(err)
}

// Subscriptions lists active subscriptions, including private feeds.
func (s *Service) Subscriptions(ctx context.Context, a Actor) ([]Subscription, error) {
	if err := s.checkActor(ctx, a, ScopeEntriesRead); err != nil {
		return nil, err
	}
	rows, err := s.store.Pool().Query(ctx, "SELECT "+subscriptionColumns+" FROM subscriptions s JOIN feeds f ON f.id=s.feed_id WHERE s.user_id=$1 AND s.deleted_at IS NULL ORDER BY s.id", a.UserID)
	if err != nil {
		return nil, fmt.Errorf("list subscriptions: %w", err)
	}
	defer rows.Close()
	out := []Subscription{}
	for rows.Next() {
		sub, err := scanSubscription(rows)
		if err != nil {
			return nil, fmt.Errorf("read subscription: %w", err)
		}
		out = append(out, sub)
	}
	return out, rows.Err()
}

// Subscription returns one active subscription and its health.
func (s *Service) Subscription(ctx context.Context, a Actor, id int64) (Subscription, error) {
	if err := s.checkActor(ctx, a, ScopeEntriesRead); err != nil {
		return Subscription{}, err
	}
	if id <= 0 {
		return Subscription{}, ErrInvalid
	}
	sub, err := subscriptionTx(ctx, s.store.Pool(), a.UserID, id)
	if err != nil {
		return sub, err
	}
	if sub.Deleted {
		return Subscription{}, ErrNotFound
	}
	return sub, nil
}

func validateSubscribe(options SubscribeOptions) error {
	if !validOpID(options.OpID) || options.FolderID < 0 || len(options.Title) > 1024 {
		return ErrInvalid
	}
	if options.Cadence != "" && !validCadence(options.Cadence) {
		return ErrInvalid
	}
	_, err := subscriptionExtract(options.Extract)
	return err
}
func ensureFolder(ctx context.Context, tx pgx.Tx, user, folder int64) error {
	if folder == 0 {
		return nil
	}
	var ok bool
	if err := tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM folders WHERE id=$1 AND user_id=$2 AND deleted_at IS NULL)", folder, user).Scan(&ok); err != nil {
		return fmt.Errorf("subscription folder: %w", err)
	}
	if !ok {
		return ErrNotFound
	}
	return nil
}

// Subscribe probes outside the commit lock, shares public feeds and encrypts
// private credentials. A replay never initiates another outbound request.
func (s *Service) Subscribe(ctx context.Context, a Actor, rawURL string, options SubscribeOptions) (Subscription, error) {
	if err := s.checkActor(ctx, a, ScopeFeedsWrite); err != nil {
		return Subscription{}, err
	}
	if err := validateSubscribe(options); err != nil {
		return Subscription{}, err
	}
	rawURL, err := normalizedFeedURL(rawURL)
	if err != nil {
		return Subscription{}, err
	}
	var previous []byte
	err = s.store.Pool().QueryRow(ctx, "SELECT result FROM op_ids WHERE principal_id=$1 AND op_id=$2", a.PrincipalID, options.OpID).Scan(&previous)
	if err == nil {
		var envelope store.OpEnvelope
		if err := json.Unmarshal(previous, &envelope); err != nil {
			return Subscription{}, fmt.Errorf("subscription operation: %w", err)
		}
		if envelope.Method != "Subscribe" {
			return Subscription{}, ErrInvalid
		}
		var sub Subscription
		if err := json.Unmarshal(envelope.Result, &sub); err != nil {
			return Subscription{}, fmt.Errorf("subscription replay: %w", err)
		}
		return sub, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return Subscription{}, fmt.Errorf("subscription deduplication: %w", err)
	}
	if s.prober == nil {
		return Subscription{}, ErrUnavailable
	}
	resolved, meta, items, err := s.prober.Probe(ctx, rawURL, options.Credentials)
	if err != nil {
		class := "network"
		var classified ProbeError
		if errors.As(err, &classified) && validFetchError(classified.Class()) {
			class = classified.Class()
		}
		return Subscription{}, fmt.Errorf("probe failed (%s): %w", class, ErrUnavailable)
	}
	resolved, err = normalizedFeedURL(resolved)
	if err != nil {
		return Subscription{}, err
	}
	if options.Credentials != nil && credentialOrigin(rawURL) != credentialOrigin(resolved) {
		return Subscription{}, fmt.Errorf("credentials require same-origin feed discovery: %w", ErrInvalid)
	}
	encrypted, err := s.sealCredentials(options.Credentials)
	if err != nil {
		return Subscription{}, err
	}
	var sub Subscription
	err = s.withEvents(ctx, func(tx pgx.Tx) error {
		if err := store.LockCore(ctx, tx); err != nil {
			return err
		}
		replay, err := replayOp(ctx, tx, a.PrincipalID, "Subscribe", options.OpID)
		if err != nil {
			return err
		}
		if replay != nil {
			return json.Unmarshal(replay, &sub)
		}
		sub, err = s.subscribeTx(ctx, tx, a, resolved, meta, options, encrypted)
		if err != nil {
			return err
		}
		return store.FinishOp(ctx, tx, a.PrincipalID, options.OpID, sub)
	})
	if err != nil {
		return Subscription{}, domainError(err)
	}
	if len(items) > 0 {
		if _, err := s.RecordFetch(ctx, FetchOutcome{FeedID: sub.FeedID, Status: FetchOK, Feed: &meta, Items: items}); err != nil {
			return sub, fmt.Errorf("subscription saved; initial content pending: %w", err)
		}
	}
	return sub, nil
}

func (s *Service) subscribeTx(ctx context.Context, tx pgx.Tx, a Actor, rawURL string, meta FeedMeta, options SubscribeOptions, encrypted []byte) (Subscription, error) {
	if err := ensureFolder(ctx, tx, a.UserID, options.FolderID); err != nil {
		return Subscription{}, err
	}
	var feedID int64
	if err := ensureFeed(ctx, tx, a.UserID, rawURL, meta, encrypted, s.now().UTC(), &feedID); err != nil {
		return Subscription{}, err
	}
	var existing int64
	err := tx.QueryRow(ctx, "SELECT id FROM subscriptions WHERE user_id=$1 AND feed_id=$2 AND deleted_at IS NULL", a.UserID, feedID).Scan(&existing)
	if err == nil {
		return subscriptionTx(ctx, tx, a.UserID, existing)
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return Subscription{}, err
	}
	cadence := options.Cadence
	if cadence == "" {
		cadence = CadenceToday
	}
	mode, err := subscriptionExtract(options.Extract)
	if err != nil {
		return Subscription{}, err
	}
	var folder any
	if options.FolderID != 0 {
		folder = options.FolderID
	}
	var title any
	if options.Title != "" {
		title = options.Title
	}
	rev, err := changeRev(ctx, tx, a.UserID)
	if err != nil {
		return Subscription{}, err
	}
	var id int64
	if err := tx.QueryRow(ctx, `INSERT INTO subscriptions(user_id,feed_id,title_override,folder_id,cadence,full_text_mode,rev,created_at,initial_since,initial_through_seq,initial_fetch_pending)
 VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,(SELECT coalesce(max(seq),0) FROM entries WHERE feed_id=$2),NOT EXISTS(SELECT 1 FROM entries WHERE feed_id=$2)) RETURNING id`, a.UserID, feedID, title, folder, string(cadence), mode, rev, s.now().UTC(), s.now().UTC().Add(-24*time.Hour)).Scan(&id); err != nil {
		return Subscription{}, err
	}
	sub, err := subscriptionTx(ctx, tx, a.UserID, id)
	if err != nil {
		return sub, err
	}
	if err := s.emitChange(ctx, tx, a.UserID, rev, syncmodel.SubscriptionCreated, "subscription", sub); err != nil {
		return sub, err
	}
	if err := s.backfillSubscription(ctx, tx, a.UserID, sub); err != nil {
		return sub, err
	}
	seq, err := store.CurrentSeq(ctx, tx)
	if err != nil {
		return sub, err
	}
	view := "river:" + string(cadence)
	var exists bool
	if err := tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM positions WHERE principal_id=$1 AND view=$2)", a.PrincipalID, view).Scan(&exists); err != nil {
		return sub, err
	}
	if !exists {
		positionRev, err := changeRev(ctx, tx, a.UserID)
		if err != nil {
			return sub, err
		}
		if err := store.UpsertPosition(ctx, tx, a.PrincipalID, view, uint64(seq), positionRev); err != nil {
			return sub, err
		}
		position := syncmodel.Position{Principal: syncmodel.ID(a.PrincipalID), View: view, Seq: uint64(seq), Rev: uint64(positionRev)}
		if err := s.emitChange(ctx, tx, a.UserID, positionRev, syncmodel.PositionUpdated, "position", position); err != nil {
			return sub, err
		}
	}
	return sub, nil
}

const subscriptionStateBatchSize = 500

// Backfill events cover entries whose IDs a concurrent bootstrap already passed.
func (s *Service) backfillSubscription(ctx context.Context, tx pgx.Tx, user int64, sub Subscription) error {
	rows, err := tx.Query(ctx, `SELECT id,seq,first_seen_at,published_at,deleted_at IS NOT NULL FROM entries WHERE feed_id=$1 ORDER BY id`, sub.FeedID)
	if err != nil {
		return fmt.Errorf("subscription backfill: %w", err)
	}
	entries := []syncmodel.Entry{}
	for rows.Next() {
		e := syncmodel.Entry{SubscriptionID: syncmodel.ID(sub.ID)}
		var published *time.Time
		if err := rows.Scan(&e.ID, &e.Seq, &e.FirstSeenAt, &published, &e.Deleted); err != nil {
			rows.Close()
			return fmt.Errorf("read subscription backfill: %w", err)
		}
		if published != nil {
			e.PublishedAt = *published
		}
		entries = append(entries, e)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return fmt.Errorf("read subscription backfill: %w", err)
	}
	for _, e := range entries {
		rev, err := changeRev(ctx, tx, user)
		if err != nil {
			return err
		}
		e.Rev = uint64(rev)
		if _, err := tx.Exec(ctx, `INSERT INTO entry_revisions(user_id,entry_id,rev) VALUES($1,$2,$3) ON CONFLICT(user_id,entry_id) DO UPDATE SET rev=EXCLUDED.rev`, user, int64(e.ID), rev); err != nil {
			return fmt.Errorf("stamp subscription backfill: %w", err)
		}
		kind := syncmodel.EntryCreated
		if e.Deleted {
			kind = syncmodel.EntryDeleted
		}
		if err := s.emitChange(ctx, tx, user, rev, kind, "entry", e); err != nil {
			return err
		}
	}

	// Replay saved state after the entries so clients can merge it by register
	// revision. These snapshots are not new writes and keep their original stamps.
	rows, err = tx.Query(ctx, `SELECT st.principal_id,array_agg(st.entry_id ORDER BY st.entry_id)
 FROM entry_state st JOIN principals p ON p.id=st.principal_id JOIN entries e ON e.id=st.entry_id
 WHERE p.user_id=$1 AND e.feed_id=$2 GROUP BY st.principal_id ORDER BY st.principal_id`, user, sub.FeedID)
	if err != nil {
		return fmt.Errorf("subscription state backfill: %w", err)
	}
	type stateTarget struct {
		principal int64
		entries   []int64
	}
	var targets []stateTarget
	for rows.Next() {
		var target stateTarget
		if err := rows.Scan(&target.principal, &target.entries); err != nil {
			rows.Close()
			return fmt.Errorf("read subscription state backfill: %w", err)
		}
		targets = append(targets, target)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return fmt.Errorf("read subscription state backfill: %w", err)
	}
	for _, target := range targets {
		for start := 0; start < len(target.entries); start += subscriptionStateBatchSize {
			ids := target.entries[start:min(start+subscriptionStateBatchSize, len(target.entries))]
			states, err := store.LoadStates(ctx, tx, target.principal, ids)
			if err != nil {
				return fmt.Errorf("load subscription state backfill: %w", err)
			}
			results := make([]syncmodel.Result, 0, len(ids))
			for _, id := range ids {
				results = append(results, syncmodel.Result{Status: syncmodel.Applied, State: states[id]})
			}
			if _, err := s.change(ctx, tx, user, syncmodel.StateUpdated, map[string]any{"results": results}); err != nil {
				return err
			}
		}
	}
	return nil
}

func ensureFeed(ctx context.Context, tx pgx.Tx, user int64, rawURL string, meta FeedMeta, encrypted []byte, now time.Time, id *int64) error {
	language := meta.Language
	if language == "" {
		language = "simple"
	}
	query := `INSERT INTO feeds(url,title,site_url,language,next_fetch_at) VALUES($1,$2,$3,$4,$5)
 ON CONFLICT(url) WHERE owner_user_id IS NULL DO UPDATE SET url=EXCLUDED.url RETURNING id`
	args := []any{rawURL, meta.Title, meta.SiteURL, language, now}
	if encrypted != nil {
		query = `INSERT INTO feeds(url,title,site_url,language,next_fetch_at,owner_user_id,credentials_encrypted) VALUES($1,$2,$3,$4,$5,$6,$7)
  ON CONFLICT(owner_user_id,url) WHERE owner_user_id IS NOT NULL DO UPDATE SET credentials_encrypted=EXCLUDED.credentials_encrypted,next_fetch_at=EXCLUDED.next_fetch_at RETURNING id`
		args = append(args, user, encrypted)
	}
	if err := tx.QueryRow(ctx, query, args...).Scan(id); err != nil {
		return fmt.Errorf("save feed: %w", err)
	}
	return nil
}

// UpdateSubscription patches settings; credential changes never mutate shared
// ownership. This subscription instead binds to the matching public/private feed.
func (s *Service) UpdateSubscription(ctx context.Context, a Actor, id int64, options UpdateSubscriptionOptions) (Subscription, error) {
	if err := s.checkActor(ctx, a, ScopeFeedsWrite); err != nil {
		return Subscription{}, err
	}
	if id <= 0 || !validOpID(options.OpID) {
		return Subscription{}, ErrInvalid
	}
	if options.Title != nil && len(*options.Title) > 1024 {
		return Subscription{}, ErrInvalid
	}
	if options.FolderID != nil && *options.FolderID < 0 {
		return Subscription{}, ErrInvalid
	}
	if options.Cadence != nil && !validCadence(*options.Cadence) {
		return Subscription{}, ErrInvalid
	}
	var mode *string
	if options.Extract != nil {
		m, err := subscriptionExtract(*options.Extract)
		if err != nil {
			return Subscription{}, err
		}
		mode = &m
	}
	if options.Credentials != nil {
		options.CredentialsSet = true
	}
	encrypted, err := s.sealCredentials(options.Credentials)
	if err != nil {
		return Subscription{}, err
	}
	var sub Subscription
	err = s.withEvents(ctx, func(tx pgx.Tx) error {
		if err := store.LockCore(ctx, tx); err != nil {
			return err
		}
		replay, err := replayOp(ctx, tx, a.PrincipalID, "UpdateSubscription", options.OpID)
		if err != nil {
			return err
		}
		if replay != nil {
			return json.Unmarshal(replay, &sub)
		}
		sub, err = subscriptionTx(ctx, tx, a.UserID, id)
		if err != nil {
			return err
		}
		if sub.Deleted {
			return ErrNotFound
		}
		if options.FolderID != nil {
			if err := ensureFolder(ctx, tx, a.UserID, *options.FolderID); err != nil {
				return err
			}
		}
		if options.CredentialsSet {
			feedID := sub.FeedID
			var meta FeedMeta
			if err := tx.QueryRow(ctx, "SELECT title,coalesce(site_url,''),language FROM feeds WHERE id=$1", feedID).Scan(&meta.Title, &meta.SiteURL, &meta.Language); err != nil {
				return fmt.Errorf("load canonical feed metadata: %w", err)
			}
			if err := ensureFeed(ctx, tx, a.UserID, sub.URL, meta, encrypted, s.now().UTC(), &feedID); err != nil {
				return err
			}
			if _, err := tx.Exec(ctx, "UPDATE subscriptions SET feed_id=$2,initial_since=$3,initial_through_seq=(SELECT coalesce(max(seq),0) FROM entries WHERE feed_id=$2),initial_fetch_pending=NOT EXISTS(SELECT 1 FROM entries WHERE feed_id=$2) WHERE id=$1", id, feedID, s.now().UTC().Add(-24*time.Hour)); err != nil {
				return domainError(err)
			}
		}
		previousFeed := sub.FeedID
		var folder, title any
		if options.FolderID != nil && *options.FolderID != 0 {
			folder = *options.FolderID
		}
		if options.Title != nil && *options.Title != "" {
			title = *options.Title
		}
		rev, err := changeRev(ctx, tx, a.UserID)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE subscriptions SET title_override=CASE WHEN $2 THEN $3::text ELSE title_override END,
  folder_id=CASE WHEN $4 THEN $5::bigint ELSE folder_id END,cadence=coalesce($6,cadence),full_text_mode=coalesce($7,full_text_mode),rev=$8 WHERE id=$1`, id, options.Title != nil, title, options.FolderID != nil, folder, options.Cadence, mode, rev); err != nil {
			return domainError(err)
		}
		sub, err = subscriptionTx(ctx, tx, a.UserID, id)
		if err != nil {
			return err
		}
		if err := s.emitChange(ctx, tx, a.UserID, rev, syncmodel.SubscriptionUpdated, "subscription", sub); err != nil {
			return err
		}
		if sub.FeedID != previousFeed {
			if err := s.backfillSubscription(ctx, tx, a.UserID, sub); err != nil {
				return err
			}
		}
		return store.FinishOp(ctx, tx, a.PrincipalID, options.OpID, sub)
	})
	if err != nil {
		return Subscription{}, domainError(err)
	}
	return sub, nil
}

// Unsubscribe keeps a revisioned tombstone; other users keep their subscriptions.
func (s *Service) Unsubscribe(ctx context.Context, a Actor, id int64, opID string) error {
	if err := s.checkActor(ctx, a, ScopeFeedsWrite); err != nil {
		return err
	}
	if id <= 0 || !validOpID(opID) {
		return ErrInvalid
	}
	return domainError(s.withEvents(ctx, func(tx pgx.Tx) error {
		if err := store.LockCore(ctx, tx); err != nil {
			return err
		}
		replay, err := replayOp(ctx, tx, a.PrincipalID, "Unsubscribe", opID)
		if err != nil {
			return err
		}
		if replay != nil {
			return nil
		}
		sub, err := subscriptionTx(ctx, tx, a.UserID, id)
		if err != nil {
			return err
		}
		if sub.Deleted {
			return ErrNotFound
		}
		rev, err := changeRev(ctx, tx, a.UserID)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, "UPDATE subscriptions SET deleted_at=$2,rev=$3 WHERE id=$1", id, s.now().UTC(), rev); err != nil {
			return err
		}
		sub.Deleted = true
		sub.Rev = uint64(rev)
		if err := s.emitChange(ctx, tx, a.UserID, rev, syncmodel.SubscriptionDeleted, "subscription", sub); err != nil {
			return err
		}
		return store.FinishOp(ctx, tx, a.PrincipalID, opID, sub)
	}))
}
