package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"image"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/L-K-M/Earnest/internal/media"
	"github.com/L-K-M/Earnest/internal/outbound"
	"github.com/L-K-M/Earnest/internal/store"
	"github.com/L-K-M/Earnest/internal/syncmodel"
	"github.com/jackc/pgx/v5"
)

// Feed icons and article thumbnails are fetched by a background worker from
// rows that ingest writes, never in response to a view. Showing a list then
// serves stored bytes only, so it cannot tell a publisher which articles a
// person or an agent picked (rule 7 in docs/research/llm.md). Agents cannot
// cause these fetches: the URLs come from feed documents and publisher pages.

const (
	// mediaLease is how long a claimed row waits before another pass may
	// retry it, should the worker die mid-fetch. A pass claims its whole
	// batch up front, so the lease exceeds the worst case for a pass: each
	// of its two fetchers working through half the batch with every
	// candidate timing out (at most seven fetches for an icon).
	mediaLease        = 30 * time.Minute
	mediaFetchTimeout = 20 * time.Second
	// mediaPageMax bounds a site's home page read for icon links;
	// mediaIconMax bounds one icon file.
	mediaPageMax = 2 << 20
	mediaIconMax = 1 << 20
	// mediaMaxAttempts counts passes over a row that ended only in
	// transient failures (timeouts, 5xx, 429) before it is given up.
	mediaMaxAttempts = 3
	mediaRetryBase   = 10 * time.Minute
	// iconRefresh rereads a found icon; iconRetryNone looks again for a site
	// that offered none.
	iconRefresh   = 30 * 24 * time.Hour
	iconRetryNone = 7 * 24 * time.Hour
	// Per-pass bounds. Fetches share the outbound per-host limiter with the
	// feed fetcher, so a pass stays small and two fetches run at once.
	mediaIconBatch     = 4
	mediaImageBatch    = 8
	mediaSeedBatch     = 100
	mediaConcurrency   = 2
	mediaBackfillBatch = 200
	// mediaSeedInterval spaces the scan for subscribed feeds without an icon
	// row once a scan has found none.
	mediaSeedInterval = time.Hour
	// mediaBackfillWindow is how far back entries stored before thumbnails
	// existed get one. Older entries keep showing only the feed icon.
	mediaBackfillWindow = 7 * 24 * time.Hour
	mediaAccept         = "image/webp,image/png,image/jpeg,image/gif;q=0.9,image/x-icon;q=0.8,*/*;q=0.1"
	mediaPageAccept     = "text/html,application/xhtml+xml;q=0.9,*/*;q=0.1"
)

// Media statuses mirror the CHECK constraints in migration 0011.
const (
	mediaPending = "pending"
	mediaReady   = "ready"
	mediaNone    = "none"
	mediaFailed  = "failed"
)

// MediaFile is a stored icon or thumbnail ready to serve.
type MediaFile struct {
	ContentType string
	Body        []byte
	ETag        string
}

// thumbnailPath and iconPath are the API-relative URLs clients load. The
// version query lets clients cache a body for good: a new body gets a new
// etag and therefore a new URL.
func thumbnailPath(entry int64, etag string) string {
	return "/v1/entries/" + strconv.FormatInt(entry, 10) + "/thumbnail?v=" + url.QueryEscape(etag)
}

func iconPath(subscription int64, etag string) string {
	return "/v1/subscriptions/" + strconv.FormatInt(subscription, 10) + "/icon?v=" + url.QueryEscape(etag)
}

func mediaETag(body []byte) string {
	sum := sha256.Sum256(body)
	return hex.EncodeToString(sum[:8])
}

// thumbnailCandidates chooses an ingested item's thumbnail image URLs, best
// first: its declared image, then images in its body.
func thumbnailCandidates(item IngestItem) []string {
	candidates := media.ThumbnailCandidates(item.Image, item.ContentHTML)
	if candidates == nil {
		return []string{}
	}
	return candidates
}

// sameImages reports whether two candidate lists name the same images,
// comparing host and path only. Some publishers sign image URLs or add a
// cache-busting query on every fetch; treating those as new images would
// download every article's image again on every poll.
func sameImages(a, b []string) bool {
	return slices.EqualFunc(a, b, func(x, y string) bool { return imageKey(x) == imageKey(y) })
}

func imageKey(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return raw
	}
	return strings.ToLower(u.Host) + u.EscapedPath()
}

// queueEntryImage records the thumbnail candidates for an entry's current
// version inside the ingest transaction. An unchanged candidate list leaves
// the row, and any stored thumbnail, alone; a changed one starts over.
func queueEntryImage(ctx context.Context, tx pgx.Tx, entry int64, candidates []string, at time.Time) error {
	if candidates == nil {
		candidates = []string{}
	}
	status := mediaPending
	if len(candidates) == 0 {
		status = mediaNone
	}
	if _, err := tx.Exec(ctx, `INSERT INTO entry_images(entry_id,candidates,status,next_attempt_at,updated_at) VALUES($1,$2,$3,$4,$4)
 ON CONFLICT(entry_id) DO UPDATE SET candidates=EXCLUDED.candidates,status=EXCLUDED.status,attempts=0,next_attempt_at=EXCLUDED.next_attempt_at,updated_at=EXCLUDED.updated_at,
  source_url=CASE WHEN EXCLUDED.status='none' THEN NULL ELSE entry_images.source_url END,
  content_type=CASE WHEN EXCLUDED.status='none' THEN NULL ELSE entry_images.content_type END,
  body=CASE WHEN EXCLUDED.status='none' THEN NULL ELSE entry_images.body END,
  etag=CASE WHEN EXCLUDED.status='none' THEN NULL ELSE entry_images.etag END
 WHERE entry_images.candidates IS DISTINCT FROM EXCLUDED.candidates`, entry, candidates, status, at); err != nil {
		return fmt.Errorf("queue entry image: %w", err)
	}
	return nil
}

// noteFeedIcon records what a successful fetch said about the feed's icon
// inputs and reschedules resolution when they changed.
func noteFeedIcon(ctx context.Context, tx pgx.Tx, feedID int64, meta *FeedMeta, at time.Time) error {
	declared := httpURL(meta.IconURL)
	if _, err := tx.Exec(ctx, `INSERT INTO feed_icons(feed_id,declared_url,site_url,next_attempt_at,updated_at) VALUES($1,nullif($2,''),nullif($3,''),$4,$4)
 ON CONFLICT(feed_id) DO UPDATE SET declared_url=EXCLUDED.declared_url,site_url=EXCLUDED.site_url,next_attempt_at=EXCLUDED.next_attempt_at,attempts=0,updated_at=EXCLUDED.updated_at
 WHERE (feed_icons.declared_url,feed_icons.site_url) IS DISTINCT FROM (EXCLUDED.declared_url,EXCLUDED.site_url)`, feedID, declared, meta.SiteURL, at); err != nil {
		return fmt.Errorf("note feed icon: %w", err)
	}
	return nil
}

// imageEnclosure is the first image attachment, the declared image of an
// imported article whose export carries no other.
func imageEnclosure(enclosures []Enclosure) string {
	for _, e := range enclosures {
		if strings.HasPrefix(strings.ToLower(strings.TrimSpace(e.Type)), "image/") {
			return e.URL
		}
	}
	return ""
}

// httpURL returns raw when it is an absolute http(s) URL with a host and no
// userinfo, and "" otherwise.
func httpURL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.User != nil {
		return ""
	}
	return raw
}

// attachThumbnails sets ThumbnailURL on live entries that have a stored
// thumbnail. Storing or replacing one raises the entry's revision; see
// announceEntryMedia.
func (s *Service) attachThumbnails(ctx context.Context, entries []Entry) error {
	ids := make([]int64, 0, len(entries))
	for _, e := range entries {
		if !e.Deleted {
			ids = append(ids, e.ID)
		}
	}
	if len(ids) == 0 {
		return nil
	}
	rows, err := s.store.Pool().Query(ctx, "SELECT entry_id,etag FROM entry_images WHERE entry_id=ANY($1) AND body IS NOT NULL", ids)
	if err != nil {
		return fmt.Errorf("list thumbnails: %w", err)
	}
	defer rows.Close()
	etags := map[int64]string{}
	for rows.Next() {
		var id int64
		var etag string
		if err := rows.Scan(&id, &etag); err != nil {
			return fmt.Errorf("scan thumbnail: %w", err)
		}
		etags[id] = etag
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("list thumbnails: %w", err)
	}
	for i := range entries {
		if etag, ok := etags[entries[i].ID]; ok && !entries[i].Deleted {
			entries[i].ThumbnailURL = thumbnailPath(entries[i].ID, etag)
		}
	}
	return nil
}

// EntryThumbnail returns the stored thumbnail of an entry the actor can see.
func (s *Service) EntryThumbnail(ctx context.Context, a Actor, id int64) (MediaFile, error) {
	if err := s.checkActor(ctx, a, ScopeEntriesRead); err != nil {
		return MediaFile{}, err
	}
	if id <= 0 {
		return MediaFile{}, fmt.Errorf("entry id: %w", ErrInvalid)
	}
	visible, err := store.LoadEntryRows(ctx, s.store.Pool(), a.UserID, []int64{id})
	if err != nil {
		return MediaFile{}, domainError(err)
	}
	if len(visible) == 0 || visible[0].Deleted {
		return MediaFile{}, ErrNotFound
	}
	var f MediaFile
	err = s.store.Pool().QueryRow(ctx, "SELECT content_type,body,etag FROM entry_images WHERE entry_id=$1 AND body IS NOT NULL", id).Scan(&f.ContentType, &f.Body, &f.ETag)
	if errors.Is(err, pgx.ErrNoRows) {
		return MediaFile{}, ErrNotFound
	}
	if err != nil {
		return MediaFile{}, fmt.Errorf("load thumbnail: %w", err)
	}
	return f, nil
}

// SubscriptionIcon returns the stored icon of one of the actor's
// subscriptions. Ended subscriptions still resolve: their kept entries list
// them.
func (s *Service) SubscriptionIcon(ctx context.Context, a Actor, id int64) (MediaFile, error) {
	if err := s.checkActor(ctx, a, ScopeEntriesRead); err != nil {
		return MediaFile{}, err
	}
	if id <= 0 {
		return MediaFile{}, fmt.Errorf("subscription id: %w", ErrInvalid)
	}
	var f MediaFile
	err := s.store.Pool().QueryRow(ctx, `SELECT i.content_type,i.body,i.etag FROM subscriptions s JOIN feed_icons i ON i.feed_id=s.feed_id
 WHERE s.id=$1 AND s.user_id=$2 AND i.body IS NOT NULL`, id, a.UserID).Scan(&f.ContentType, &f.Body, &f.ETag)
	if errors.Is(err, pgx.ErrNoRows) {
		return MediaFile{}, ErrNotFound
	}
	if err != nil {
		return MediaFile{}, fmt.Errorf("load icon: %w", err)
	}
	return f, nil
}

// iconJob and imageJob are claimed rows; attempts includes the current pass.
type iconJob struct {
	feedID            int64
	declared, siteURL string
	feedURL           string
	attempts          int
}

type imageJob struct {
	entryID    int64
	candidates []string
	attempts   int
}

// RunMedia is one media worker pass: it seeds icon rows for subscribed
// feeds, backfills candidates for recent entries stored before thumbnails
// existed, then fetches a small batch of due icons and thumbnails. It
// returns how many rows it touched so the caller can pace passes.
func (s *Service) RunMedia(ctx context.Context, _ int) (int, error) {
	if s.outbound == nil {
		return 0, nil
	}
	now := s.now()
	seeded := 0
	if last := s.mediaSeededAt.Load(); last == 0 || now.Sub(time.Unix(0, last)) >= mediaSeedInterval {
		n, err := s.seedFeedIcons(ctx, now)
		if err != nil {
			return 0, err
		}
		// Keep seeding every pass while it finds feeds; afterwards ingest
		// covers new feeds and a periodic check catches the rest.
		if n == 0 {
			s.mediaSeededAt.Store(now.UnixNano())
		}
		seeded = n
	}
	backfilled, err := s.backfillEntryImages(ctx, now)
	if err != nil {
		return seeded, err
	}
	icons, err := s.claimFeedIcons(ctx, now)
	if err != nil {
		return seeded + backfilled, err
	}
	images, err := s.claimEntryImages(ctx, now)
	if err != nil {
		return seeded + backfilled + len(icons), err
	}

	work := make(chan func() error)
	errs := make(chan error, len(icons)+len(images))
	var wg sync.WaitGroup
	for range mediaConcurrency {
		wg.Go(func() {
			for job := range work {
				if err := job(); err != nil {
					errs <- err
				}
			}
		})
	}
	for _, job := range icons {
		work <- func() error { return s.resolveFeedIcon(ctx, job) }
	}
	for _, job := range images {
		work <- func() error { return s.resolveEntryImage(ctx, job) }
	}
	close(work)
	wg.Wait()
	close(errs)
	var failed error
	for err := range errs {
		failed = errors.Join(failed, err)
	}
	return seeded + backfilled + len(icons) + len(images), failed
}

// seedFeedIcons creates rows for subscribed feeds that have none yet: feeds
// that only answered 304 since the upgrade never reach noteFeedIcon.
func (s *Service) seedFeedIcons(ctx context.Context, now time.Time) (int, error) {
	tag, err := s.store.Pool().Exec(ctx, `INSERT INTO feed_icons(feed_id,site_url,next_attempt_at,updated_at)
 SELECT f.id,f.site_url,$1,$1 FROM feeds f
 WHERE NOT EXISTS(SELECT 1 FROM feed_icons i WHERE i.feed_id=f.id)
  AND EXISTS(SELECT 1 FROM subscriptions s WHERE s.feed_id=f.id AND s.deleted_at IS NULL)
 ORDER BY f.id LIMIT $2
 ON CONFLICT(feed_id) DO NOTHING`, now, mediaSeedBatch)
	if err != nil {
		return 0, fmt.Errorf("seed feed icons: %w", err)
	}
	return int(tag.RowsAffected()), nil
}

// backfillEntryImages derives candidates from stored bodies for recent
// entries ingested before this feature, newest first. Ingest covers every
// later entry, so once a pass finds none the scan stops for the process's
// lifetime.
func (s *Service) backfillEntryImages(ctx context.Context, now time.Time) (int, error) {
	if s.mediaBackfilled.Load() {
		return 0, nil
	}
	rows, err := s.store.Pool().Query(ctx, `SELECT e.id,coalesce((SELECT c.html FROM contents c WHERE c.entry_id=e.id AND c.content_hash=e.content_hash
   ORDER BY (c.source='extracted') DESC,c.sanitizer_version DESC LIMIT 1),'')
 FROM entries e
 WHERE e.deleted_at IS NULL AND e.first_seen_at>=$1
  AND NOT EXISTS(SELECT 1 FROM entry_images i WHERE i.entry_id=e.id)
  AND EXISTS(SELECT 1 FROM subscriptions s WHERE s.feed_id=e.feed_id AND s.deleted_at IS NULL)
 ORDER BY e.id DESC LIMIT $2`, now.Add(-mediaBackfillWindow), mediaBackfillBatch)
	if err != nil {
		return 0, fmt.Errorf("find entries without images: %w", err)
	}
	type pending struct {
		id   int64
		html string
	}
	var found []pending
	for rows.Next() {
		var p pending
		if err := rows.Scan(&p.id, &p.html); err != nil {
			rows.Close()
			return 0, fmt.Errorf("scan entry without image: %w", err)
		}
		found = append(found, p)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return 0, fmt.Errorf("find entries without images: %w", err)
	}
	if len(found) == 0 {
		s.mediaBackfilled.Store(true)
		return 0, nil
	}
	err = pgx.BeginFunc(ctx, s.store.Pool(), func(tx pgx.Tx) error {
		for _, p := range found {
			// Ingest may have written the row since the scan; its candidates
			// win, so a backfilled row only fills a gap.
			candidates := media.ThumbnailCandidates("", p.html)
			if candidates == nil {
				candidates = []string{}
			}
			status := mediaPending
			if len(candidates) == 0 {
				status = mediaNone
			}
			if _, err := tx.Exec(ctx, `INSERT INTO entry_images(entry_id,candidates,status,next_attempt_at,updated_at) VALUES($1,$2,$3,$4,$4) ON CONFLICT(entry_id) DO NOTHING`, p.id, candidates, status, now); err != nil {
				return fmt.Errorf("backfill entry image: %w", err)
			}
		}
		return nil
	})
	if err != nil {
		return 0, err
	}
	return len(found), nil
}

func (s *Service) claimFeedIcons(ctx context.Context, now time.Time) ([]iconJob, error) {
	rows, err := s.store.Pool().Query(ctx, `WITH due AS (
  SELECT i.feed_id FROM feed_icons i
  WHERE i.next_attempt_at<=$1 AND EXISTS(SELECT 1 FROM subscriptions s WHERE s.feed_id=i.feed_id AND s.deleted_at IS NULL)
  ORDER BY i.next_attempt_at,i.feed_id LIMIT $2 FOR UPDATE OF i SKIP LOCKED)
 UPDATE feed_icons i SET attempts=i.attempts+1,next_attempt_at=$3 FROM due,feeds f
 WHERE i.feed_id=due.feed_id AND f.id=i.feed_id
 RETURNING i.feed_id,coalesce(i.declared_url,''),coalesce(i.site_url,''),f.url,i.attempts`, now, mediaIconBatch, now.Add(mediaLease))
	if err != nil {
		return nil, fmt.Errorf("claim feed icons: %w", err)
	}
	defer rows.Close()
	var jobs []iconJob
	for rows.Next() {
		var j iconJob
		if err := rows.Scan(&j.feedID, &j.declared, &j.siteURL, &j.feedURL, &j.attempts); err != nil {
			return nil, fmt.Errorf("scan feed icon claim: %w", err)
		}
		jobs = append(jobs, j)
	}
	return jobs, rows.Err()
}

func (s *Service) claimEntryImages(ctx context.Context, now time.Time) ([]imageJob, error) {
	rows, err := s.store.Pool().Query(ctx, `WITH due AS (
  SELECT entry_id FROM entry_images WHERE status='pending' AND next_attempt_at<=$1
  ORDER BY entry_id DESC LIMIT $2 FOR UPDATE SKIP LOCKED)
 UPDATE entry_images i SET attempts=i.attempts+1,next_attempt_at=$3 FROM due
 WHERE i.entry_id=due.entry_id
 RETURNING i.entry_id,i.candidates,i.attempts`, now, mediaImageBatch, now.Add(mediaLease))
	if err != nil {
		return nil, fmt.Errorf("claim entry images: %w", err)
	}
	defer rows.Close()
	var jobs []imageJob
	for rows.Next() {
		var j imageJob
		if err := rows.Scan(&j.entryID, &j.candidates, &j.attempts); err != nil {
			return nil, fmt.Errorf("scan entry image claim: %w", err)
		}
		jobs = append(jobs, j)
	}
	return jobs, rows.Err()
}

// mediaRetryDelay backs off transient failures: 10 min, 20 min, 40 min.
func mediaRetryDelay(attempts int) time.Duration {
	return mediaRetryBase << min(max(attempts-1, 0), 6)
}

// resolveEntryImage tries an entry's candidates in order and stores the first
// that makes a thumbnail. Only a pass that met a transient failure is
// retried; unusable images are final.
func (s *Service) resolveEntryImage(ctx context.Context, job imageJob) error {
	transient := false
	for _, candidate := range job.candidates {
		data, _, err := s.fetchMedia(ctx, candidate, outbound.Image, media.MaxSourceBytes, mediaAccept)
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			transient = transient || transientMedia(err)
			continue
		}
		thumb, err := s.decodeMedia(ctx, data, media.Thumbnail)
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			continue
		}
		return s.storeEntryImage(ctx, job, mediaReady, candidate, &thumb, time.Time{})
	}
	if ctx.Err() != nil {
		return nil
	}
	if transient && job.attempts < mediaMaxAttempts {
		return s.storeEntryImage(ctx, job, mediaPending, "", nil, s.now().Add(mediaRetryDelay(job.attempts)))
	}
	return s.storeEntryImage(ctx, job, mediaFailed, "", nil, time.Time{})
}

// storeEntryImage writes a pass's result unless ingest replaced the
// candidates meanwhile; the replacement is then pending and wins.
func (s *Service) storeEntryImage(ctx context.Context, job imageJob, status, source string, thumb *media.Encoded, retryAt time.Time) error {
	now := s.now()
	var err error
	switch {
	case thumb != nil:
		etag := mediaETag(thumb.Body)
		err = s.withEvents(ctx, func(tx pgx.Tx) error {
			// The core lock comes first, as in ingest, which writes the same row.
			if err := store.LockCore(ctx, tx); err != nil {
				return err
			}
			var old *string
			err := tx.QueryRow(ctx, "SELECT etag FROM entry_images WHERE entry_id=$1 AND candidates=$2 FOR UPDATE", job.entryID, job.candidates).Scan(&old)
			if errors.Is(err, pgx.ErrNoRows) {
				return nil
			}
			if err != nil {
				return err
			}
			if _, err := tx.Exec(ctx, `UPDATE entry_images SET status=$2,attempts=0,source_url=$3,content_type=$4,body=$5,etag=$6,updated_at=$7 WHERE entry_id=$1`,
				job.entryID, status, source, thumb.ContentType, thumb.Body, etag, now); err != nil {
				return err
			}
			if old != nil && *old == etag {
				return nil
			}
			return s.announceEntryMedia(ctx, tx, job.entryID)
		})
	case status == mediaPending:
		_, err = s.store.Pool().Exec(ctx, `UPDATE entry_images SET status=$3,next_attempt_at=$4,updated_at=$5 WHERE entry_id=$1 AND candidates=$2`,
			job.entryID, job.candidates, status, retryAt, now)
	default:
		// A failed pass keeps an earlier version's thumbnail, if any.
		_, err = s.store.Pool().Exec(ctx, `UPDATE entry_images SET status=$3,attempts=0,updated_at=$4 WHERE entry_id=$1 AND candidates=$2`,
			job.entryID, job.candidates, status, now)
	}
	if err != nil {
		return fmt.Errorf("store entry image %d: %w", job.entryID, err)
	}
	return nil
}

// announceEntryMedia tells every subscriber that an entry's thumbnail
// changed, with an entry.updated at a new revision. Native clients keep
// entries from sync and refresh one when its revision rises, so a thumbnail
// stored after the entry was announced would otherwise never reach them.
func (s *Service) announceEntryMedia(ctx context.Context, tx pgx.Tx, entryID int64) error {
	var seq int64
	var firstSeen time.Time
	var published *time.Time
	var deleted bool
	if err := tx.QueryRow(ctx, "SELECT seq,first_seen_at,published_at,deleted_at IS NOT NULL FROM entries WHERE id=$1", entryID).Scan(&seq, &firstSeen, &published, &deleted); err != nil {
		return fmt.Errorf("load entry for media change: %w", err)
	}
	if deleted {
		return nil
	}
	subscribers, err := store.SubscribersForEntries(ctx, tx, []int64{entryID})
	if err != nil {
		return err
	}
	counts := map[int64]int64{}
	for _, sub := range subscribers[entryID] {
		counts[sub.UserID]++
	}
	if len(counts) == 0 {
		return nil
	}
	allocation, err := store.AllocateCounters(ctx, tx, 0, counts)
	if err != nil {
		return err
	}
	next := map[int64]int64{}
	for user, r := range allocation.Revs {
		next[user] = r.First
	}
	for _, sub := range subscribers[entryID] {
		rev := next[sub.UserID]
		next[sub.UserID]++
		entry := syncmodel.Entry{ID: syncmodel.ID(entryID), SubscriptionID: syncmodel.ID(sub.SubscriptionID), Seq: uint64(seq), FirstSeenAt: firstSeen.UTC(), Rev: uint64(rev)}
		if published != nil {
			entry.PublishedAt = published.UTC()
		}
		if err := s.emitChange(ctx, tx, sub.UserID, rev, syncmodel.EntryUpdated, "entry", entry); err != nil {
			return err
		}
		if err := store.StampEntryRevision(ctx, tx, sub.UserID, entryID, rev); err != nil {
			return err
		}
	}
	return nil
}

// announceFeedIcon bumps every active subscription to a feed whose icon
// changed and sends its subscription.updated, which carries the new
// icon_url, so sync clients see icons stored after they subscribed.
func (s *Service) announceFeedIcon(ctx context.Context, tx pgx.Tx, feedID int64) error {
	rows, err := tx.Query(ctx, "SELECT user_id,id FROM subscriptions WHERE feed_id=$1 AND deleted_at IS NULL ORDER BY user_id,id", feedID)
	if err != nil {
		return fmt.Errorf("feed icon subscribers: %w", err)
	}
	type subscriber struct{ user, id int64 }
	var subscribers []subscriber
	for rows.Next() {
		var sub subscriber
		if err := rows.Scan(&sub.user, &sub.id); err != nil {
			rows.Close()
			return fmt.Errorf("scan feed icon subscriber: %w", err)
		}
		subscribers = append(subscribers, sub)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return fmt.Errorf("feed icon subscribers: %w", err)
	}
	for _, sub := range subscribers {
		rev, err := changeRev(ctx, tx, sub.user)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, "UPDATE subscriptions SET rev=$2 WHERE id=$1", sub.id, rev); err != nil {
			return fmt.Errorf("subscription icon revision: %w", err)
		}
		subscription, err := subscriptionTx(ctx, tx, sub.user, sub.id)
		if err != nil {
			return err
		}
		if err := s.emitChange(ctx, tx, sub.user, rev, syncmodel.SubscriptionUpdated, "subscription", subscription); err != nil {
			return err
		}
	}
	return nil
}

// iconInputsUnchanged guards a pass's result: when ingest changed the
// feed's declared image or site during the pass, noteFeedIcon has made the
// row due again, and the next pass resolves the new inputs instead.
const iconInputsUnchanged = "feed_id=$1 AND coalesce(declared_url,'')=$2 AND coalesce(site_url,'')=$3"

// resolveFeedIcon looks for an icon in the site's home page links, then at
// /favicon.ico, then in the feed's own declared image, and stores the first
// that decodes.
func (s *Service) resolveFeedIcon(ctx context.Context, job iconJob) error {
	transient := false
	var candidates []string
	site := iconSite(job.siteURL, job.feedURL)
	if site != nil {
		page, final, err := s.fetchMedia(ctx, site.String(), outbound.Favicon, mediaPageMax, mediaPageAccept)
		switch {
		case err == nil:
			candidates = append(candidates, media.IconLinks(page, final)...)
			site = final
		case ctx.Err() != nil:
			return nil
		default:
			transient = transientMedia(err)
		}
		candidates = append(candidates, site.ResolveReference(&url.URL{Path: "/favicon.ico"}).String())
	}
	if job.declared != "" {
		candidates = append(candidates, job.declared)
	}
	seen := map[string]bool{}
	for _, candidate := range candidates {
		if seen[candidate] {
			continue
		}
		seen[candidate] = true
		data, _, err := s.fetchMedia(ctx, candidate, outbound.Favicon, mediaIconMax, mediaAccept)
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			transient = transient || transientMedia(err)
			continue
		}
		icon, err := s.decodeMedia(ctx, data, media.Icon)
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			continue
		}
		etag := mediaETag(icon.Body)
		err = s.withEvents(ctx, func(tx pgx.Tx) error {
			if err := store.LockCore(ctx, tx); err != nil {
				return err
			}
			var old *string
			err := tx.QueryRow(ctx, "SELECT etag FROM feed_icons WHERE "+iconInputsUnchanged+" FOR UPDATE", job.feedID, job.declared, job.siteURL).Scan(&old)
			if errors.Is(err, pgx.ErrNoRows) {
				return nil
			}
			if err != nil {
				return err
			}
			if _, err := tx.Exec(ctx, `UPDATE feed_icons SET status='ready',attempts=0,next_attempt_at=$2,source_url=$3,content_type=$4,body=$5,etag=$6,updated_at=$7 WHERE feed_id=$1`,
				job.feedID, s.now().Add(iconRefresh), candidate, icon.ContentType, icon.Body, etag, s.now()); err != nil {
				return err
			}
			if old != nil && *old == etag {
				return nil
			}
			return s.announceFeedIcon(ctx, tx, job.feedID)
		})
		if err != nil {
			return fmt.Errorf("store feed icon %d: %w", job.feedID, err)
		}
		return nil
	}
	if ctx.Err() != nil {
		return nil
	}
	if transient && job.attempts < mediaMaxAttempts {
		_, err := s.store.Pool().Exec(ctx, "UPDATE feed_icons SET next_attempt_at=$4,updated_at=$5 WHERE "+iconInputsUnchanged, job.feedID, job.declared, job.siteURL, s.now().Add(mediaRetryDelay(job.attempts)), s.now())
		if err != nil {
			return fmt.Errorf("reschedule feed icon %d: %w", job.feedID, err)
		}
		return nil
	}
	// Nothing usable: an icon found earlier stays until a later pass finds
	// a replacement, so a flaky site does not lose its icon.
	_, err := s.store.Pool().Exec(ctx, `UPDATE feed_icons SET status=CASE WHEN body IS NULL THEN 'none' ELSE status END,attempts=0,next_attempt_at=$4,updated_at=$5 WHERE `+iconInputsUnchanged,
		job.feedID, job.declared, job.siteURL, s.now().Add(iconRetryNone), s.now())
	if err != nil {
		return fmt.Errorf("record missing feed icon %d: %w", job.feedID, err)
	}
	return nil
}

// decodeMedia decodes and re-encodes one image while holding the decode
// slot. A camera original may take media.MaxDecodedBytes once decoded, and a
// pass fetches two items at once, so admitting one decode bounds the
// worker's memory to a single large image.
func (s *Service) decodeMedia(ctx context.Context, data []byte, encode func(image.Image) (media.Encoded, error)) (media.Encoded, error) {
	select {
	case s.mediaDecode <- struct{}{}:
	case <-ctx.Done():
		return media.Encoded{}, ctx.Err()
	}
	defer func() { <-s.mediaDecode }()
	img, err := media.Decode(data)
	if err != nil {
		return media.Encoded{}, err
	}
	return encode(img)
}

// iconSite is the page whose links name the feed's icon: the publisher's
// site, or the feed URL's origin when the feed names no valid site.
func iconSite(siteURL, feedURL string) *url.URL {
	if u, err := url.Parse(httpURL(siteURL)); err == nil && u.Host != "" {
		u.Fragment = ""
		return u
	}
	u, err := url.Parse(httpURL(feedURL))
	if err != nil || u.Host == "" {
		return nil
	}
	return &url.URL{Scheme: u.Scheme, Host: u.Host, Path: "/"}
}

// fetchMedia GETs one publisher URL through the checked outbound client and
// returns the body and the final URL after redirects. Any status other than
// 200 is an error.
func (s *Service) fetchMedia(ctx context.Context, raw string, purpose outbound.Purpose, limit int64, accept string) ([]byte, *url.URL, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, raw, nil)
	if err != nil {
		return nil, nil, err
	}
	req.Header.Set("Accept", accept)
	res, err := s.outbound.Do(req, outbound.Policy{Purpose: purpose, MaxBodySize: limit, Timeout: mediaFetchTimeout, ExtraPorts: s.mediaPorts})
	if err != nil {
		return nil, nil, err
	}
	defer res.Response.Body.Close()
	if res.Response.StatusCode != http.StatusOK {
		return nil, nil, fmt.Errorf("media fetch: HTTP %d", res.Response.StatusCode)
	}
	// The outbound client has already read the body within limit.
	body, err := io.ReadAll(res.Response.Body)
	if err != nil {
		return nil, nil, err
	}
	final := req.URL
	if res.FinalURL != nil {
		final = res.FinalURL
	}
	return body, final, nil
}

// transientMedia reports whether a later pass might succeed where this one
// failed. Refusals, missing files and oversized bodies are final.
func transientMedia(err error) bool {
	switch outbound.ErrorClass(err) {
	case outbound.Timeout, outbound.DNS, outbound.Connect, outbound.TLS, outbound.HTTP429, outbound.HTTP5xx:
		return true
	}
	return false
}
