package service

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"net/url"
	"strings"
	"time"

	"github.com/L-K-M/Earnest/internal/store"
	"github.com/L-K-M/Earnest/internal/syncmodel"
	"github.com/jackc/pgx/v5"
)

const (
	maxFetchClaim        = 1000
	minimumFetchInterval = 15 * time.Minute
	maximumFetchInterval = 24 * time.Hour
	goneFetchInterval    = 7 * 24 * time.Hour
	maxFetchItems        = 10000
	firehoseThreshold    = 20
)

// ClaimDueFeeds atomically leases due feeds. Fetching happens after this commits.
func (s *Service) ClaimDueFeeds(ctx context.Context, now time.Time, limit int, lease time.Duration) ([]FeedToFetch, error) {
	if now.IsZero() || limit <= 0 || limit > maxFetchClaim || lease <= 0 || lease > 24*time.Hour {
		return nil, ErrInvalid
	}
	feeds := []FeedToFetch{}
	err := s.store.WithTx(ctx, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `WITH due AS (
   SELECT f.id FROM feeds f WHERE f.next_fetch_at<=$1 AND (f.lease_until IS NULL OR f.lease_until<=$1)
   AND EXISTS(SELECT 1 FROM subscriptions s WHERE s.feed_id=f.id AND s.deleted_at IS NULL)
   ORDER BY f.next_fetch_at,f.id FOR UPDATE SKIP LOCKED LIMIT $2
  ) UPDATE feeds f SET lease_until=$3 FROM due WHERE f.id=due.id
  RETURNING f.id,f.url,coalesce(f.etag,''),coalesce(f.last_modified,''),f.credentials_encrypted,
  extract(epoch FROM f.fetch_interval)::double precision,f.lease_until,
  EXISTS(SELECT 1 FROM subscriptions s WHERE s.feed_id=f.id AND s.deleted_at IS NULL
  AND (s.full_text_mode='always' OR (s.full_text_mode='inherit' AND f.extraction_mode='always')))`, now.UTC(), limit, now.UTC().Add(lease))
		if err != nil {
			return fmt.Errorf("claim due feeds: %w", err)
		}
		defer rows.Close()
		for rows.Next() {
			var f FeedToFetch
			var encrypted []byte
			var seconds float64
			var extract bool
			if err := rows.Scan(&f.FeedID, &f.URL, &f.ETag, &f.LastModified, &encrypted, &seconds, &f.LeaseUntil, &extract); err != nil {
				return fmt.Errorf("scan feed lease: %w", err)
			}
			f.Extract = ExtractNone
			if extract {
				f.Extract = ExtractAlways
			}
			f.Interval = time.Duration(seconds * float64(time.Second))
			f.Credentials, err = s.openCredentials(encrypted)
			if err != nil {
				// Keep its lease so bad credentials cannot starve other feeds or spin.
				slog.Error("skip feed with unreadable credentials", "feed_id", f.FeedID)
				continue
			}
			feeds = append(feeds, f)
		}
		return rows.Err()
	})
	if err != nil {
		return nil, err
	}
	return feeds, nil
}

// KnownIdentities reports which feed identities need no extraction work.
// Live entries count as known only when their current content already caches
// an extracted version; feed-sourced entries stay unknown so the caller can
// retry extraction. Every retention tombstone is always known so a refresh
// can neither resurrect it nor fetch it again.
func (s *Service) KnownIdentities(ctx context.Context, feedID int64, hashes []string) (map[string]bool, error) {
	if feedID <= 0 || len(hashes) > maxFetchItems {
		return nil, ErrInvalid
	}
	raw := make([][]byte, len(hashes))
	for i, h := range hashes {
		var err error
		raw[i], err = decodeHash(h)
		if err != nil {
			return nil, err
		}
	}
	rows, err := s.store.Pool().Query(ctx, `SELECT e.identity_hash FROM entries e
 WHERE e.feed_id=$1 AND e.identity_hash=ANY($2::bytea[])
 AND (e.deleted_at IS NOT NULL OR EXISTS(SELECT 1 FROM contents c
  WHERE c.entry_id=e.id AND c.content_hash=e.content_hash AND c.source='extracted'))`, feedID, raw)
	if err != nil {
		return nil, fmt.Errorf("known identities: %w", err)
	}
	defer rows.Close()
	out := map[string]bool{}
	for rows.Next() {
		var hash []byte
		if err := rows.Scan(&hash); err != nil {
			return nil, err
		}
		out[hex.EncodeToString(hash)] = true
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

func decodeHash(value string) ([]byte, error) {
	raw, err := hex.DecodeString(value)
	if err != nil || len(raw) != sha256.Size {
		return nil, fmt.Errorf("hash must be 64 hexadecimal characters: %w", ErrInvalid)
	}
	return raw, nil
}

func feedURLValid(raw string) bool {
	u, err := url.Parse(raw)
	return err == nil && (u.Scheme == "http" || u.Scheme == "https") && u.Hostname() != "" && u.User == nil && u.Fragment == ""
}

type ingestChange struct {
	item              IngestItem
	excerptOnly       bool
	identity, content []byte
	id                int64
	seq               uint64
	firstSeen         time.Time
	kind              syncmodel.ChangeType
}

// RecordFetch commits identity/content versions, user events and health together.
// Seq is reserved only inside the locked commit, never while a fetch is in flight.
func (s *Service) RecordFetch(ctx context.Context, outcome FetchOutcome) (IngestSummary, error) {
	if outcome.FeedID <= 0 || len(outcome.Items) > maxFetchItems || outcome.RetryAfter < 0 {
		return IngestSummary{}, ErrInvalid
	}
	if outcome.Status != FetchOK && outcome.Status != FetchNotModified && outcome.Status != FetchFailed {
		return IngestSummary{}, ErrInvalid
	}
	if outcome.Status != FetchOK && len(outcome.Items) != 0 {
		return IngestSummary{}, ErrInvalid
	}
	if outcome.HTTPStatus != 0 && (outcome.HTTPStatus < 100 || outcome.HTTPStatus > 599) {
		return IngestSummary{}, ErrInvalid
	}
	if outcome.PermanentRedirectURL != "" && !feedURLValid(outcome.PermanentRedirectURL) {
		return IngestSummary{}, ErrInvalid
	}
	if outcome.Status == FetchFailed && !validFetchError(outcome.ErrorClass) {
		return IngestSummary{}, fmt.Errorf("unknown fetch error class: %w", ErrInvalid)
	}
	receipt := s.now().UTC()
	if outcome.AttemptedAt.IsZero() {
		outcome.AttemptedAt = receipt
	}
	prepared := make([]ingestChange, 0, len(outcome.Items))
	identities := map[string]bool{}
	for _, item := range outcome.Items {
		identity, err := decodeHash(item.IdentityHash)
		if err != nil {
			return IngestSummary{}, err
		}
		canonical := hex.EncodeToString(identity)
		if identities[canonical] {
			return IngestSummary{}, fmt.Errorf("duplicate feed identity: %w", ErrInvalid)
		}
		identities[canonical] = true
		if item.SanitizerVersion <= 0 {
			return IngestSummary{}, fmt.Errorf("sanitizer version must be positive: %w", ErrInvalid)
		}
		excerptOnly := item.ContentSource == ContentFromFeedExcerpt
		if excerptOnly {
			item.ContentSource = ContentFromFeed
		}
		if item.ContentSource != "" && item.ContentSource != ContentFromFeed && item.ContentSource != ContentFromExtracted {
			return IngestSummary{}, ErrInvalid
		}
		if item.ContentSource == "" {
			item.ContentSource = ContentFromFeed
		}
		content, err := decodeHash(item.ContentHash)
		if err != nil {
			return IngestSummary{}, err
		}
		if item.Language == "" {
			item.Language = "simple"
		}
		prepared = append(prepared, ingestChange{item: item, excerptOnly: excerptOnly, identity: identity, content: content})
	}
	summary := IngestSummary{}
	var newEntries []int64
	err := s.withEvents(ctx, func(tx pgx.Tx) error {
		if err := store.LockCore(ctx, tx); err != nil {
			return err
		}
		var intervalSeconds float64
		var failures int
		var private bool
		var oldTitle, oldSite string
		if err := tx.QueryRow(ctx, `SELECT extract(epoch FROM fetch_interval)::double precision,consecutive_failures,owner_user_id IS NOT NULL,title,coalesce(site_url,'') FROM feeds WHERE id=$1 FOR UPDATE`, outcome.FeedID).Scan(&intervalSeconds, &failures, &private, &oldTitle, &oldSite); err != nil {
			return domainError(err)
		}
		titleChanged, siteChanged := false, false
		if outcome.Status != FetchFailed && outcome.Feed != nil {
			titleChanged = oldTitle != outcome.Feed.Title
			siteChanged = oldSite != outcome.Feed.SiteURL
		}
		changes := make([]ingestChange, 0, len(prepared))
		for _, p := range prepared {
			var oldHash []byte
			var guid, rawURL, title, author, language string
			var pub, updated *time.Time
			var enclosures []byte
			var deleted, hasVersion bool
			err := tx.QueryRow(ctx, `SELECT e.id,e.seq,e.first_seen_at,e.content_hash,coalesce(e.guid,''),coalesce(e.url,''),e.title,e.author,e.language,e.published_at,e.updated_at,e.enclosures,e.deleted_at IS NOT NULL,
    EXISTS(SELECT 1 FROM contents c WHERE c.entry_id=e.id AND c.content_hash=$3 AND c.sanitizer_version=$4 AND c.source=$5 AND c.excerpt_only=$6)
    FROM entries e WHERE e.feed_id=$1 AND e.identity_hash=$2`, outcome.FeedID, p.identity, p.content, p.item.SanitizerVersion, string(p.item.ContentSource), p.excerptOnly).Scan(&p.id, &p.seq, &p.firstSeen, &oldHash, &guid, &rawURL, &title, &author, &language, &pub, &updated, &enclosures, &deleted, &hasVersion)
			if errors.Is(err, pgx.ErrNoRows) {
				p.kind = syncmodel.EntryCreated
				changes = append(changes, p)
				summary.Created++
				continue
			}
			if err != nil {
				return fmt.Errorf("find ingest identity: %w", err)
			}
			if deleted {
				continue
			}
			var oldEnclosures []Enclosure
			if err := json.Unmarshal(enclosures, &oldEnclosures); err != nil {
				return fmt.Errorf("decode stored enclosures: %w", err)
			}
			sameEnclosures := len(oldEnclosures) == len(p.item.Enclosures)
			if sameEnclosures {
				for i, v := range oldEnclosures {
					if v != p.item.Enclosures[i] {
						sameEnclosures = false
						break
					}
				}
			}
			if bytes.Equal(oldHash, p.content) && guid == p.item.GUID && rawURL == p.item.URL && title == p.item.Title && author == p.item.Author && language == p.item.Language && timePtrEqual(pub, p.item.PublishedAt) && timePtrEqual(updated, p.item.UpdatedAt) && sameEnclosures && hasVersion {
				continue
			}
			p.kind = syncmodel.EntryUpdated
			changes = append(changes, p)
			summary.Updated++
		}
		rows, err := tx.Query(ctx, "SELECT user_id,id,initial_fetch_pending,title_override IS NULL FROM subscriptions WHERE feed_id=$1 AND deleted_at IS NULL ORDER BY user_id", outcome.FeedID)
		if err != nil {
			return fmt.Errorf("ingest subscribers: %w", err)
		}
		type subscriber struct {
			user, id       int64
			initialPending bool
			update         bool
		}
		subscribers := []subscriber{}
		counts := map[int64]int64{}
		for rows.Next() {
			var sub subscriber
			var inheritsTitle bool
			if err := rows.Scan(&sub.user, &sub.id, &sub.initialPending, &inheritsTitle); err != nil {
				rows.Close()
				return err
			}
			sub.update = (sub.initialPending && outcome.Status == FetchOK) || siteChanged || (titleChanged && inheritsTitle)
			subscribers = append(subscribers, sub)
			if len(changes) > 0 {
				counts[sub.user] = int64(len(changes))
			}
			if sub.update {
				counts[sub.user]++
			}
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return err
		}
		allocation, err := store.AllocateCounters(ctx, tx, int64(summary.Created), counts)
		if err != nil {
			return err
		}
		seq := allocation.Seq.First
		for i, p := range changes {
			if p.kind == syncmodel.EntryCreated {
				p.seq = uint64(seq)
				seq++
				p.firstSeen = receipt
				if err := tx.QueryRow(ctx, `INSERT INTO entries(feed_id,seq,identity_hash,first_seen_at) VALUES($1,$2,$3,$4) RETURNING id`, outcome.FeedID, int64(p.seq), p.identity, receipt).Scan(&p.id); err != nil {
					return fmt.Errorf("create ingested entry: %w", err)
				}
				newEntries = append(newEntries, p.id)
			}
			if _, err := tx.Exec(ctx, `INSERT INTO contents(entry_id,content_hash,sanitizer_version,source,html,text,created_at,excerpt_only) VALUES($1,$2,$3,$4,$5,$6,$7,$8) ON CONFLICT(entry_id,content_hash,sanitizer_version,source) DO UPDATE SET excerpt_only=EXCLUDED.excerpt_only`, p.id, p.content, p.item.SanitizerVersion, string(p.item.ContentSource), p.item.ContentHTML, p.item.ContentText, receipt, p.excerptOnly); err != nil {
				return fmt.Errorf("store content version: %w", err)
			}
			enclosures := p.item.Enclosures
			if enclosures == nil {
				enclosures = []Enclosure{}
			}
			body, err := json.Marshal(enclosures)
			if err != nil {
				return fmt.Errorf("encode enclosures: %w", err)
			}
			if _, err := tx.Exec(ctx, `UPDATE entries SET guid=$2,url=$3,title=$4,author=$5,published_at=$6,updated_at=$7,content_hash=$8,language=$9,enclosures=$10 WHERE id=$1`, p.id, p.item.GUID, p.item.URL, p.item.Title, p.item.Author, p.item.PublishedAt, p.item.UpdatedAt, p.content, p.item.Language, body); err != nil {
				return fmt.Errorf("update ingested entry: %w", err)
			}
			for _, sub := range subscribers {
				rev := allocation.Revs[sub.user].First + int64(i)
				e := syncmodel.Entry{ID: syncmodel.ID(p.id), SubscriptionID: syncmodel.ID(sub.id), Seq: p.seq, FirstSeenAt: p.firstSeen, Rev: uint64(rev)}
				if p.item.PublishedAt != nil {
					e.PublishedAt = *p.item.PublishedAt
				}
				if err := insertEvent(ctx, tx, sub.user, rev, string(p.kind), map[string]any{"entry": e}, receipt); err != nil {
					return err
				}
				if _, err := tx.Exec(ctx, `INSERT INTO entry_revisions(user_id,entry_id,rev) VALUES($1,$2,$3) ON CONFLICT(user_id,entry_id) DO UPDATE SET rev=EXCLUDED.rev`, sub.user, p.id, rev); err != nil {
					return fmt.Errorf("stamp ingested entry: %w", err)
				}
			}
		}

		if len(newEntries) > 0 {
			if err := enqueueRuleJobs(ctx, tx, outcome.FeedID, newEntries, receipt); err != nil {
				return err
			}
		}
		interval := time.Duration(intervalSeconds * float64(time.Second))
		if err := s.fetchHealth(ctx, tx, outcome, receipt, interval, failures, private, summary); err != nil {
			return err
		}

		// Finalise only the initial cohort. Later backdated arrivals remain new
		// by first_seen, regardless of the publisher's display timestamp.
		for _, sub := range subscribers {
			if !sub.update {
				continue
			}
			rev := allocation.Revs[sub.user].First + int64(len(changes))
			if sub.initialPending && outcome.Status == FetchOK {
				if _, err := tx.Exec(ctx, `UPDATE subscriptions SET initial_through_seq=(SELECT coalesce(max(seq),0) FROM entries WHERE feed_id=$2),initial_fetch_pending=false,rev=$3 WHERE id=$1`, sub.id, outcome.FeedID, rev); err != nil {
					return fmt.Errorf("initial subscription boundary: %w", err)
				}
			} else if _, err := tx.Exec(ctx, "UPDATE subscriptions SET rev=$2 WHERE id=$1", sub.id, rev); err != nil {
				return fmt.Errorf("subscription metadata revision: %w", err)
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
	})
	if err != nil {
		return IngestSummary{}, err
	}
	return summary, nil
}

func timePtrEqual(a, b *time.Time) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return a.Equal(*b)
}
func validFetchError(class string) bool {
	switch class {
	case "dns", "tls", "network", "timeout", "gone", "rate_limit", "challenge", "http", "parse", "size", "policy", "extraction":
		return true
	}
	return false
}

func fetchBackoff(class string, failures int, retryAfter time.Duration) time.Duration {
	base := minimumFetchInterval
	switch class {
	case "gone":
		return goneFetchInterval
	case "challenge", "policy":
		base = maximumFetchInterval
	case "parse", "size", "extraction":
		base = time.Hour
	}
	exponent := min(max(failures, 0), 7)
	backoff := min(base*time.Duration(1<<exponent), maximumFetchInterval)
	return min(max(backoff, retryAfter), goneFetchInterval)
}

func (s *Service) fetchHealth(ctx context.Context, tx pgx.Tx, o FetchOutcome, receipt time.Time, interval time.Duration, failures int, private bool, summary IngestSummary) error {
	var status any
	if o.HTTPStatus != 0 {
		status = o.HTTPStatus
	}
	var errorClass, errorMessage any
	backoff := time.Duration(0)
	nextInterval := min(max(interval, minimumFetchInterval), maximumFetchInterval)
	if o.Status == FetchFailed {
		errorClass = o.ErrorClass
		message := o.ErrorMessage
		if len(message) > 1024 {
			message = strings.ToValidUTF8(message[:1024], "")
		}
		if private {
			message = "private feed fetch failed (" + o.ErrorClass + ")"
		}
		errorMessage = message
		backoff = fetchBackoff(o.ErrorClass, failures, o.RetryAfter)
		failures++
	} else {
		failures = 0
		if summary.Created == 0 {
			nextInterval = min(nextInterval*2, maximumFetchInterval)
		} else {
			nextInterval = max(nextInterval/2, minimumFetchInterval)
		}
	}
	next := receipt.Add(max(nextInterval, backoff))
	if !o.NextFetchAt.IsZero() && o.NextFetchAt.After(next) {
		next = o.NextFetchAt.UTC()
	}
	if _, err := tx.Exec(ctx, `UPDATE feeds SET last_attempt_at=$2,last_http_status=$3,error_class=$4,error_message=$5,consecutive_failures=$6,
 backoff=$7*interval '1 second',fetch_interval=$8*interval '1 second',next_fetch_at=$9,lease_until=NULL,
 last_success_at=CASE WHEN $10 THEN last_success_at ELSE $11 END,
 etag=CASE WHEN $10 OR ($12='' AND $13) THEN etag ELSE nullif($12,'') END,
 last_modified=CASE WHEN $10 OR ($14='' AND $13) THEN last_modified ELSE nullif($14,'') END,
 permanent_redirect_url=coalesce(nullif($15,''),permanent_redirect_url),item_count=item_count+$16,last_item_count=$17
 WHERE id=$1`, o.FeedID, o.AttemptedAt.UTC(), status, errorClass, errorMessage, failures, backoff.Seconds(), nextInterval.Seconds(), next, o.Status == FetchFailed, receipt, o.ETag, o.Status == FetchNotModified, o.LastModified, o.PermanentRedirectURL, summary.Created, len(o.Items)); err != nil {
		return fmt.Errorf("record feed health: %w", err)
	}
	if o.Status != FetchFailed && o.Feed != nil {
		language := o.Feed.Language
		if language == "" {
			language = "simple"
		}
		if _, err := tx.Exec(ctx, "UPDATE feeds SET title=$2,site_url=$3,language=$4 WHERE id=$1", o.FeedID, o.Feed.Title, o.Feed.SiteURL, language); err != nil {
			return fmt.Errorf("update feed metadata: %w", err)
		}
	}
	if o.Status == FetchOK {
		var rate float64
		if err := tx.QueryRow(ctx, `SELECT count(*)::double precision FROM entries WHERE feed_id=$1 AND deleted_at IS NULL
  AND coalesce(published_at,first_seen_at)>=$2 AND coalesce(published_at,first_seen_at)<=$3`, o.FeedID, receipt.Add(-24*time.Hour), receipt).Scan(&rate); err != nil {
			return fmt.Errorf("feed posting rate: %w", err)
		}
		if math.IsNaN(rate) {
			return ErrInvalid
		}
		if _, err := tx.Exec(ctx, "UPDATE feeds SET posts_per_day=$2 WHERE id=$1", o.FeedID, rate); err != nil {
			return fmt.Errorf("store posting rate: %w", err)
		}
	}
	return nil
}
