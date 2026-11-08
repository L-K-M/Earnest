package service

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"net/url"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/L-K-M/Earnest/internal/feed"
	"github.com/L-K-M/Earnest/internal/sanitize"
	"github.com/L-K-M/Earnest/internal/store"
	"github.com/L-K-M/Earnest/internal/syncmodel"
	"github.com/jackc/pgx/v5"
)

// Importing starred articles from another reader keeps them in Earnest:
//
//	export item ── prepare (validate, sanitise, identity) ── chunk transaction:
//	  resolve your subscription to its feed (or subscribe, if asked)
//	  find the entry a fetch already stored, or store the exported copy
//	  kept=true (deliberate) and, if the export says read, seen (passive)
//
// An exported copy is history, not an arrival. It keeps the time the
// exporting reader first stored it as first_seen_at, so river decay and the
// subscription's initial window keep it out of every river, and it enqueues
// no rule jobs, so rules, notifications and webhooks never fire for it. Its
// seq is new, so Kept lists imported articles in the exporting reader's order,
// newest first. Existing entries are never rewritten: the import only adds
// what a fetch did not store and changes your state. A later fetch of a stored
// copy adopts it (see adoptImported), because an export cannot carry the RSS
// isPermaLink flag that decides a fetch's identity.
const (
	maxKeptImportItems = 10000
	// keptImportChunkSize bounds one import transaction, like opmlChunkSize:
	// the global sequence lock is released between chunks.
	keptImportChunkSize = 100
	// maxKeptImportField bounds each exported text field except the body.
	maxKeptImportField = 8 << 10
	// maxKeptImportEnclosures bounds the attachments stored per article.
	maxKeptImportEnclosures = 32
	// keptReportTextBytes bounds the title and feed URL echoed in a result.
	keptReportTextBytes = 300
)

// MissingFeeds says what an import does with an article whose feed you don't
// follow. Entries are visible only through a subscription, so a skipped
// article can be imported later by subscribing and importing again.
type MissingFeeds string

const (
	MissingFeedsSkip      MissingFeeds = "skip"
	MissingFeedsSubscribe MissingFeeds = "subscribe"
)

// KeptImportItem is one exported starred article, independent of the export
// format. ContentHTML and SummaryHTML are untrusted markup the service
// sanitises.
type KeptImportItem struct {
	FeedURL     string
	FeedTitle   string
	SiteURL     string
	GUID        string
	URL         string
	Title       string
	Author      string
	PublishedAt *time.Time
	UpdatedAt   *time.Time
	ContentHTML string
	SummaryHTML string
	Enclosures  []Enclosure
	// Read marks the article seen. It is a passive write, so it never
	// overrides an article you deliberately marked unread.
	Read bool
	// CrawledAt is when the exporting reader stored the article.
	CrawledAt *time.Time
}

// KeptImportStatus is one article's outcome.
type KeptImportStatus string

const (
	// KeptImportCreated stored the exported copy as a new entry and kept it.
	KeptImportCreated KeptImportStatus = "created"
	// KeptImportMatched kept, or marked read, an entry Earnest already had.
	KeptImportMatched KeptImportStatus = "matched"
	// KeptImportUnchanged found the article already kept, with nothing to do.
	KeptImportUnchanged KeptImportStatus = "unchanged"
	// KeptImportNotSubscribed skipped an article whose feed you don't follow.
	KeptImportNotSubscribed KeptImportStatus = "not_subscribed"
	// KeptImportUnkept left out an article you removed from Kept in Earnest:
	// the import fills gaps and never overrides that decision.
	KeptImportUnkept KeptImportStatus = "unkept"
	// KeptImportGone found the entry removed by retention; it cannot return.
	KeptImportGone KeptImportStatus = "gone"
	// KeptImportFailed rejected the article; Error says why.
	KeptImportFailed KeptImportStatus = "failed"
)

// KeptImportResult reports one input article by its position in the export.
type KeptImportResult struct {
	Index          int              `json:"index"`
	Status         KeptImportStatus `json:"status" enum:"created,matched,unchanged,unkept,not_subscribed,gone,failed"`
	EntryID        int64            `json:"entry_id,omitempty"`
	SubscriptionID int64            `json:"subscription_id,omitempty"`
	FeedURL        string           `json:"feed_url,omitempty"`
	Title          string           `json:"title,omitempty"`
	Error          string           `json:"error,omitempty"`
}

// KeptImportReport is every article's outcome, in export order, and the
// subscriptions the import created.
type KeptImportReport struct {
	Results    []KeptImportResult `json:"results"`
	Subscribed []Subscription     `json:"subscribed"`
}

var (
	errKeptNoFeed     = errors.New("the export does not say which feed this article came from")
	errKeptNoIdentity = errors.New("the article has no ID, link or title")
	errKeptTooLong    = errors.New("a field is longer than 8 KiB")
)

// keptWork is one validated article, ready to store.
type keptWork struct {
	index      int
	feedURL    string
	feedMeta   FeedMeta
	change     ingestChange
	identities [][]byte // candidates, the identity to create with first
	read       bool
}

// ImportKept keeps exported starred articles. It is idempotent: importing the
// same export again finds the entries the first run stored and reports them
// unchanged. Work commits in chunks; if a chunk fails, earlier chunks stay
// imported and running the import again completes the rest.
func (s *Service) ImportKept(ctx context.Context, a Actor, items []KeptImportItem, missing MissingFeeds) (KeptImportReport, error) {
	// Storing entries is feed management, which agents never hold.
	if err := s.checkActor(ctx, a, ScopeFeedsWrite); err != nil {
		return KeptImportReport{}, err
	}
	if err := s.stateActor(ctx, a); err != nil {
		return KeptImportReport{}, err
	}
	if a.Kind != PrincipalHuman {
		return KeptImportReport{}, ErrForbidden
	}
	switch missing {
	case "":
		missing = MissingFeedsSkip
	case MissingFeedsSkip, MissingFeedsSubscribe:
	default:
		return KeptImportReport{}, fmt.Errorf("missing feeds %q: %w", missing, ErrInvalid)
	}
	if len(items) > maxKeptImportItems {
		return KeptImportReport{}, fmt.Errorf("an import holds at most %d articles: %w", maxKeptImportItems, ErrInvalid)
	}
	generation, err := s.store.Generation(ctx)
	if err != nil {
		return KeptImportReport{}, err
	}
	var nonce [12]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return KeptImportReport{}, fmt.Errorf("kept import ID: %w", err)
	}
	importID := hex.EncodeToString(nonce[:])
	receipt := s.now().UTC()

	report := KeptImportReport{Results: make([]KeptImportResult, len(items)), Subscribed: []Subscription{}}
	works := make([]keptWork, 0, len(items))
	for i, item := range items {
		report.Results[i] = KeptImportResult{Index: i, FeedURL: reportText(item.FeedURL), Title: reportText(item.Title)}
		work, err := prepareKept(i, item, receipt)
		if err != nil {
			report.Results[i].Status = KeptImportFailed
			report.Results[i].Error = err.Error()
			continue
		}
		report.Results[i].FeedURL = work.feedURL
		works = append(works, work)
	}
	// Seq follows the exporting reader's history, so Kept lists newest first.
	sort.SliceStable(works, func(i, j int) bool { return works[i].change.firstSeen.Before(works[j].change.firstSeen) })

	for start := 0; start < len(works); start += keptImportChunkSize {
		if err := ctx.Err(); err != nil {
			return KeptImportReport{}, err
		}
		chunk := works[start:min(start+keptImportChunkSize, len(works))]
		var outcomes []KeptImportResult
		var subscribed []Subscription
		err := s.withEvents(ctx, func(tx pgx.Tx) error {
			var err error
			outcomes, subscribed, err = s.importKeptChunk(ctx, tx, a, generation, importID, chunk, missing, receipt)
			return err
		})
		if err != nil {
			return KeptImportReport{}, err
		}
		for _, outcome := range outcomes {
			outcome.Title = report.Results[outcome.Index].Title
			report.Results[outcome.Index] = outcome
		}
		report.Subscribed = append(report.Subscribed, subscribed...)
	}
	return report, nil
}

// prepareKept validates and sanitises one article outside any transaction.
// The link, body and identity follow the fetcher's rules, so a later fetch of
// the same article finds this entry instead of storing a duplicate.
func prepareKept(index int, in KeptImportItem, receipt time.Time) (keptWork, error) {
	if strings.TrimSpace(in.FeedURL) == "" {
		return keptWork{}, errKeptNoFeed
	}
	feedURL, err := normalizedFeedURL(in.FeedURL)
	if err != nil {
		return keptWork{}, errInvalidFeedURL
	}
	for _, field := range []string{in.FeedTitle, in.SiteURL, in.GUID, in.URL, in.Title, in.Author} {
		if len(field) > maxKeptImportField {
			return keptWork{}, errKeptTooLong
		}
	}
	guid := strings.TrimSpace(in.GUID)
	link := ""
	base := httpBase(in.URL)
	if base != nil {
		// Only absolute http(s) links are stored, as the fetcher does.
		link = strings.TrimSpace(in.URL)
	}
	if guid == "" && link == "" && strings.TrimSpace(in.Title) == "" {
		return keptWork{}, errKeptNoIdentity
	}
	item := feed.Item{GUID: guid, URL: link, Title: in.Title, Author: in.Author, Published: in.PublishedAt, Updated: in.UpdatedAt, ContentHTML: in.ContentHTML, SummaryHTML: in.SummaryHTML}
	identities := [][]byte{}
	for _, candidate := range feed.IdentityCandidates(item) {
		identity, err := decodeHash(candidate)
		if err != nil {
			return keptWork{}, err
		}
		identities = append(identities, identity)
	}
	content, err := decodeHash(feed.ContentHash(item))
	if err != nil {
		return keptWork{}, err
	}

	body := in.ContentHTML
	source := ContentFromFeed
	excerptOnly := false
	if strings.TrimSpace(body) == "" {
		body = in.SummaryHTML
		excerptOnly = true
	}
	doc, err := sanitize.Sanitize(base, body)
	if err != nil {
		// As in the fetcher: keep the article without a body rather than
		// storing the raw input.
		doc = sanitize.Document{Version: sanitize.Version}
	}
	enclosures := []Enclosure{}
	for _, enclosure := range in.Enclosures {
		if len(enclosures) == maxKeptImportEnclosures {
			break
		}
		if httpBase(enclosure.URL) == nil || len(enclosure.URL) > maxKeptImportField || len(enclosure.Type) > maxKeptImportField {
			continue
		}
		enclosures = append(enclosures, Enclosure{URL: strings.TrimSpace(enclosure.URL), Type: enclosure.Type, Length: max(enclosure.Length, 0)})
	}

	// The exporting reader's first sight of the article stands in for ours,
	// and never lies in the future.
	firstSeen := receipt
	switch {
	case in.CrawledAt != nil:
		firstSeen = in.CrawledAt.UTC()
	case in.PublishedAt != nil:
		firstSeen = in.PublishedAt.UTC()
	}
	if firstSeen.After(receipt) {
		firstSeen = receipt
	}

	feedMeta := FeedMeta{SiteURL: httpURLString(in.SiteURL)}
	if len(in.FeedTitle) <= 1024 {
		feedMeta.Title = in.FeedTitle
	}
	return keptWork{
		index:    index,
		feedURL:  feedURL,
		feedMeta: feedMeta,
		change: ingestChange{
			item: IngestItem{
				IdentityHash: hex.EncodeToString(identities[0]), GUID: guid, URL: link, Title: in.Title, Author: in.Author,
				PublishedAt: in.PublishedAt, UpdatedAt: in.UpdatedAt, ContentHash: hex.EncodeToString(content), Language: "simple",
				ContentHTML: doc.HTML, ContentText: sanitize.Text(doc.HTML), SanitizerVersion: int(doc.Version), ContentSource: source,
				Enclosures: enclosures,
			},
			excerptOnly: excerptOnly,
			identity:    identities[0],
			content:     content,
			firstSeen:   firstSeen,
			kind:        syncmodel.EntryCreated,
		},
		identities: identities,
		read:       in.Read,
	}, nil
}

// keptFeed is how one feed URL resolved within a chunk.
type keptFeed struct {
	subscription, feed int64
	missing            bool
	err                error
}

// keptTarget is an entry a chunk writes state for, and the result to fill.
type keptTarget struct {
	result  int
	entry   int64
	created bool
	read    bool
}

type keptSubscriber struct{ user, subscription int64 }

func (s *Service) importKeptChunk(ctx context.Context, tx pgx.Tx, a Actor, generation, importID string, chunk []keptWork, missing MissingFeeds, receipt time.Time) ([]KeptImportResult, []Subscription, error) {
	if err := store.LockCore(ctx, tx); err != nil {
		return nil, nil, err
	}
	results := make([]KeptImportResult, len(chunk))
	subscribed := []Subscription{}
	feeds := map[string]keptFeed{}
	for i, w := range chunk {
		results[i] = KeptImportResult{Index: w.index, FeedURL: w.feedURL}
		if _, ok := feeds[w.feedURL]; ok {
			continue
		}
		f, sub, err := s.resolveKeptFeed(ctx, tx, a, w, missing)
		if err != nil {
			return nil, nil, err
		}
		feeds[w.feedURL] = f
		if sub != nil {
			subscribed = append(subscribed, *sub)
		}
	}

	// Find what a fetch, or an earlier import, already stored. Two articles
	// of one export can name the same entry; only the first writes state.
	targets := []keptTarget{}
	claimed := map[int64]bool{}
	pending := map[string]int{} // feed and identity of an entry to create → chunk index
	var create []int
	for i, w := range chunk {
		if err := ctx.Err(); err != nil {
			return nil, nil, err
		}
		f := feeds[w.feedURL]
		switch {
		case f.err != nil:
			results[i].Status, results[i].Error = KeptImportFailed, f.err.Error()
			continue
		case f.missing:
			results[i].Status = KeptImportNotSubscribed
			continue
		}
		results[i].SubscriptionID = f.subscription
		var id int64
		var deleted bool
		err := tx.QueryRow(ctx, `SELECT id,deleted_at IS NOT NULL FROM entries
 WHERE feed_id=$1 AND (identity_hash=ANY($2) OR ($3<>'' AND guid=$3))
 ORDER BY array_position($2,identity_hash) NULLS LAST,id LIMIT 1`, f.feed, w.identities, w.change.item.GUID).Scan(&id, &deleted)
		switch {
		case errors.Is(err, pgx.ErrNoRows):
			key := fmt.Sprintf("%d:%x", f.feed, w.change.identity)
			if first, ok := pending[key]; ok {
				results[i].Status = KeptImportUnchanged
				results[i].SubscriptionID = results[first].SubscriptionID
				continue
			}
			pending[key] = i
			create = append(create, i)
		case err != nil:
			return nil, nil, fmt.Errorf("find kept import entry: %w", err)
		case deleted:
			results[i].Status, results[i].EntryID = KeptImportGone, id
		case claimed[id]:
			results[i].Status, results[i].EntryID = KeptImportUnchanged, id
		default:
			claimed[id] = true
			results[i].EntryID = id
			targets = append(targets, keptTarget{result: i, entry: id, read: w.read})
		}
	}

	created, err := s.createKeptEntries(ctx, tx, chunk, feeds, create, receipt)
	if err != nil {
		return nil, nil, err
	}
	for _, i := range create {
		results[i].EntryID = created[i]
		targets = append(targets, keptTarget{result: i, entry: created[i], created: true, read: chunk[i].read})
	}
	// Results that pointed at an entry created later in this chunk.
	for i := range results {
		if results[i].Status == KeptImportUnchanged && results[i].EntryID == 0 {
			key := fmt.Sprintf("%d:%x", feeds[chunk[i].feedURL].feed, chunk[i].change.identity)
			results[i].EntryID = created[pending[key]]
		}
	}

	if err := s.keepImported(ctx, tx, a, generation, importID, chunk, targets, results, receipt); err != nil {
		return nil, nil, err
	}
	return results, subscribed, nil
}

// resolveKeptFeed finds your subscription to the article's feed, by the URL
// you subscribed with or the one it permanently redirected to. With
// MissingFeedsSubscribe it subscribes as OPML import does, without a probe;
// a failed subscribe fails only that feed's articles.
func (s *Service) resolveKeptFeed(ctx context.Context, tx pgx.Tx, a Actor, w keptWork, missing MissingFeeds) (keptFeed, *Subscription, error) {
	var f keptFeed
	err := tx.QueryRow(ctx, `SELECT s.id,s.feed_id FROM subscriptions s JOIN feeds f ON f.id=s.feed_id
 WHERE s.user_id=$1 AND s.deleted_at IS NULL AND (f.url=$2 OR f.permanent_redirect_url=$2)
 AND (f.owner_user_id IS NULL OR f.owner_user_id=$1)
 ORDER BY f.url=$2 DESC,f.owner_user_id NULLS LAST,s.id LIMIT 1`, a.UserID, w.feedURL).Scan(&f.subscription, &f.feed)
	if err == nil {
		return f, nil, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return f, nil, fmt.Errorf("find kept import subscription: %w", err)
	}
	if missing != MissingFeedsSubscribe {
		f.missing = true
		return f, nil, nil
	}
	if _, err := tx.Exec(ctx, "SAVEPOINT earnest_kept_feed"); err != nil {
		return f, nil, err
	}
	sub, _, subErr := s.subscribeTx(ctx, tx, a, w.feedURL, w.feedMeta, SubscribeOptions{}, nil)
	if subErr != nil {
		if _, err := tx.Exec(ctx, "ROLLBACK TO SAVEPOINT earnest_kept_feed"); err != nil {
			return f, nil, err
		}
		f.err = domainError(subErr)
	}
	if _, err := tx.Exec(ctx, "RELEASE SAVEPOINT earnest_kept_feed"); err != nil {
		return f, nil, err
	}
	if subErr != nil {
		return f, nil, nil
	}
	f.subscription, f.feed = sub.ID, sub.FeedID
	return f, &sub, nil
}

// createKeptEntries stores the exported copies at the indexes in create, in
// that order, and announces each to every subscriber of its feed, as
// RecordFetch does. It enqueues no rule jobs: an import is not an arrival.
// Each is marked imported, so a fetch that sees the article under another
// identity adopts it instead of storing a duplicate.
func (s *Service) createKeptEntries(ctx context.Context, tx pgx.Tx, chunk []keptWork, feeds map[string]keptFeed, create []int, receipt time.Time) (map[int]int64, error) {
	created := map[int]int64{}
	if len(create) == 0 {
		return created, nil
	}
	subscribers := map[int64][]keptSubscriber{}
	counts := map[int64]int64{}
	for _, i := range create {
		feedID := feeds[chunk[i].feedURL].feed
		if _, ok := subscribers[feedID]; !ok {
			list, err := feedSubscribers(ctx, tx, feedID)
			if err != nil {
				return nil, err
			}
			subscribers[feedID] = list
		}
		for _, sub := range subscribers[feedID] {
			counts[sub.user]++
		}
	}
	allocation, err := store.AllocateCounters(ctx, tx, int64(len(create)), counts)
	if err != nil {
		return nil, err
	}
	seq := allocation.Seq.First
	revs := map[int64]int64{}
	for user, r := range allocation.Revs {
		revs[user] = r.First
	}
	for _, i := range create {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		p := chunk[i].change
		feedID := feeds[chunk[i].feedURL].feed
		p.seq = uint64(seq)
		seq++
		if err := tx.QueryRow(ctx, `INSERT INTO entries(feed_id,seq,identity_hash,first_seen_at,imported) VALUES($1,$2,$3,$4,true) RETURNING id`, feedID, int64(p.seq), p.identity, p.firstSeen).Scan(&p.id); err != nil {
			return nil, fmt.Errorf("store imported entry: %w", err)
		}
		if err := storeEntryVersion(ctx, tx, p, receipt); err != nil {
			return nil, err
		}
		for _, sub := range subscribers[feedID] {
			rev := revs[sub.user]
			revs[sub.user]++
			if err := announceEntry(ctx, tx, sub.user, sub.subscription, rev, p, receipt); err != nil {
				return nil, err
			}
		}
		created[i] = p.id
	}
	return created, nil
}

func feedSubscribers(ctx context.Context, tx pgx.Tx, feedID int64) ([]keptSubscriber, error) {
	rows, err := tx.Query(ctx, "SELECT user_id,id FROM subscriptions WHERE feed_id=$1 AND deleted_at IS NULL ORDER BY user_id", feedID)
	if err != nil {
		return nil, fmt.Errorf("imported entry subscribers: %w", err)
	}
	defer rows.Close()
	list := []keptSubscriber{}
	for rows.Next() {
		var sub keptSubscriber
		if err := rows.Scan(&sub.user, &sub.subscription); err != nil {
			return nil, fmt.Errorf("imported entry subscribers: %w", err)
		}
		list = append(list, sub)
	}
	return list, rows.Err()
}

// keepImported writes kept=true, and seen for articles the export marks
// read, through the ordinary state batch: the same conflict rules, op_id
// records and one compact state.updated. Registers that already hold the
// value are left alone, so importing again changes nothing. A kept register
// you set to false yourself is a decision, not a gap, so it is left alone
// too; the batch alone would let the import's newer clock win.
func (s *Service) keepImported(ctx context.Context, tx pgx.Tx, a Actor, generation, importID string, chunk []keptWork, targets []keptTarget, results []KeptImportResult, receipt time.Time) error {
	for _, t := range targets {
		if t.created {
			results[t.result].Status = KeptImportCreated
		} else {
			results[t.result].Status = KeptImportUnchanged
		}
	}
	if len(targets) == 0 {
		return nil
	}
	ids := make([]int64, len(targets))
	for i, t := range targets {
		ids[i] = t.entry
	}
	states, err := store.LoadStates(ctx, tx, a.PrincipalID, ids)
	if err != nil {
		return err
	}
	type writeRef struct {
		target   keptTarget
		register syncmodel.RegisterName
	}
	writes := []syncmodel.Write{}
	refs := []writeRef{}
	add := func(t keptTarget, register syncmodel.RegisterName, value syncmodel.Value, kind syncmodel.OperationKind) {
		writes = append(writes, syncmodel.Write{
			OpID:      fmt.Sprintf("kept-import:%s:%d:%s", importID, chunk[t.result].index, register),
			Principal: syncmodel.ID(a.PrincipalID), EntryID: syncmodel.ID(t.entry), Register: register, Value: value,
			Base: syncmodel.Revision{Generation: generation}, OccurredAt: receipt, Kind: kind,
		})
		refs = append(refs, writeRef{target: t, register: register})
	}
	for _, t := range targets {
		state := states[t.entry]
		switch {
		case state.Kept.Value == syncmodel.True:
		case state.Kept.Rev > 0:
			results[t.result].Status = KeptImportUnkept
		default:
			add(t, syncmodel.Kept, syncmodel.True, syncmodel.Explicit)
		}
		if t.read && !state.GoogleRead() {
			add(t, syncmodel.Seen, syncmodel.SeenValue, syncmodel.Passive)
		}
	}
	if len(writes) == 0 {
		return nil
	}
	current, err := stateRevision(ctx, tx, a.UserID)
	if err != nil {
		return err
	}
	applied, err := s.batchTx(ctx, tx, a, writes, current, receipt)
	if err != nil {
		return err
	}
	for i, r := range applied {
		ref := refs[i]
		result := &results[ref.target.result]
		switch {
		case r.Status == syncmodel.Applied && !ref.target.created && result.Status != KeptImportUnkept:
			result.Status = KeptImportMatched
		case r.Status == syncmodel.Applied:
		case r.Status == syncmodel.Gone:
			result.Status = KeptImportGone
		case ref.register == syncmodel.Kept:
			// A newer change in Earnest outranked the import.
			result.Status = KeptImportFailed
			result.Error = fmt.Sprintf("not kept: %s", r.Status)
		}
	}
	return nil
}

// httpBase returns raw as a base URL when it is an absolute http(s) URL,
// matching the fetcher's rule for item links.
func httpBase(raw string) *url.URL {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
		return nil
	}
	return u
}

func httpURLString(raw string) string {
	if httpBase(raw) == nil {
		return ""
	}
	return strings.TrimSpace(raw)
}

// reportText shortens an echoed value without splitting a character.
func reportText(text string) string {
	if len(text) <= keptReportTextBytes {
		return text
	}
	cut := keptReportTextBytes
	for cut > 0 && !utf8.RuneStart(text[cut]) {
		cut--
	}
	return text[:cut] + "…"
}
