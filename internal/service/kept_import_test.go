package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/L-K-M/Earnest/internal/feed"
	"github.com/L-K-M/Earnest/internal/store"
	"github.com/L-K-M/Earnest/internal/store/storetest"
	"github.com/L-K-M/Earnest/internal/syncmodel"
)

const keptTestFeed = "https://example.test/feed"

func keptTime(s string) *time.Time {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		panic(err)
	}
	return &t
}

// ingestKept stores an entry the way a fetch would, under the identity the
// fetcher derives for it.
func (f *coreFixture) ingestKept(t *testing.T, it feed.Item) int64 {
	t.Helper()
	identity := feed.IdentityHash(it)
	item := IngestItem{IdentityHash: identity, GUID: it.GUID, URL: it.URL, Title: it.Title, ContentHash: coreHash("body" + identity), ContentHTML: "<p>fetched</p>", ContentText: "fetched", SanitizerVersion: 1, ContentSource: ContentFromFeed}
	if _, err := f.s.RecordFetch(context.Background(), FetchOutcome{FeedID: f.feed, Status: FetchOK, Items: []IngestItem{item}}); err != nil {
		t.Fatal(err)
	}
	var id int64
	if err := f.db.Pool().QueryRow(context.Background(), "SELECT id FROM entries WHERE feed_id=$1 AND identity_hash=$2", f.feed, mustCoreHash(t, identity)).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}

func (f *coreFixture) keptState(t *testing.T, entry int64) syncmodel.State {
	t.Helper()
	state, err := store.LoadState(context.Background(), f.db.Pool(), f.a.PrincipalID, entry)
	if err != nil {
		t.Fatal(err)
	}
	return state
}

func (f *coreFixture) viewIDs(t *testing.T, a Actor, view EntryView) map[int64]bool {
	t.Helper()
	entries, err := f.s.Entries(context.Background(), a, EntryFilter{View: view, Limit: 200})
	if err != nil {
		t.Fatal(err)
	}
	ids := map[int64]bool{}
	for _, e := range entries {
		ids[e.ID] = true
	}
	return ids
}

func (f *coreFixture) count(t *testing.T, query string, args ...any) int64 {
	t.Helper()
	var n int64
	if err := f.db.Pool().QueryRow(context.Background(), query, args...).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func statuses(report KeptImportReport) []KeptImportStatus {
	out := make([]KeptImportStatus, len(report.Results))
	for i, r := range report.Results {
		out[i] = r.Status
	}
	return out
}

func wantStatuses(t *testing.T, report KeptImportReport, want ...KeptImportStatus) {
	t.Helper()
	got := statuses(report)
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("statuses = %v, want %v (results %+v)", got, want, report.Results)
	}
	for i, r := range report.Results {
		if r.Index != i {
			t.Fatalf("result %d has index %d", i, r.Index)
		}
	}
}

func TestImportKept(t *testing.T) {
	template := storetest.NewTemplate(t)

	t.Run("keeps_existing_and_stores_missing_articles_outside_the_river", func(t *testing.T) {
		f := newCoreFixture(t, template)
		ctx := context.Background()
		existing := f.ingestKept(t, feed.Item{GUID: "https://example.test/a", URL: "https://example.test/a", Title: "A"})
		// A rule that matches everything proves imports enqueue no rule jobs.
		if _, err := f.s.CreateRule(ctx, f.a, Rule{Name: "All", Enabled: true, Predicate: RulePredicate{Type: RuleKeyword, Terms: []string{"b"}}, Actions: []RuleAction{{Type: RuleNotify}}}, "rule"); err != nil {
			t.Fatal(err)
		}
		jobs := f.count(t, "SELECT count(*) FROM jobs WHERE kind=$1", ruleJobKind)
		var existingSeq int64
		if err := f.db.Pool().QueryRow(ctx, "SELECT seq FROM entries WHERE id=$1", existing).Scan(&existingSeq); err != nil {
			t.Fatal(err)
		}

		b := KeptImportItem{
			FeedURL: keptTestFeed, GUID: "tag:example.test,2026:b", URL: "https://example.test/b", Title: "B", Author: "Bea",
			ContentHTML: `<p>B body<script>alert(1)</script><img src="/i.png"></p>`,
			PublishedAt: keptTime("2025-12-01T09:00:00Z"), CrawledAt: keptTime("2025-12-01T10:00:00Z"), Read: true,
			Enclosures: []Enclosure{{URL: "https://example.test/b.mp3", Type: "audio/mpeg", Length: 10}, {URL: "javascript:alert(1)"}},
		}
		items := []KeptImportItem{
			{FeedURL: keptTestFeed, GUID: "https://example.test/a", URL: "https://example.test/a", Title: "A from the export", Read: true, CrawledAt: keptTime("2026-10-01T00:00:00Z")},
			b,
			{FeedURL: "https://other.test/feed", GUID: "c", Title: "C"},
			{GUID: "d", Title: "No feed"},
			b,
			{FeedURL: keptTestFeed},
		}
		report, err := f.s.ImportKept(ctx, f.a, items, MissingFeedsSkip)
		if err != nil {
			t.Fatal(err)
		}
		wantStatuses(t, report, KeptImportMatched, KeptImportCreated, KeptImportNotSubscribed, KeptImportFailed, KeptImportUnchanged, KeptImportFailed)
		created := report.Results[1].EntryID
		if report.Results[0].EntryID != existing || created == 0 || report.Results[4].EntryID != created {
			t.Fatalf("entry IDs = %+v", report.Results)
		}
		if report.Results[1].SubscriptionID != f.subscription || report.Results[2].FeedURL != "https://other.test/feed" || len(report.Subscribed) != 0 {
			t.Fatalf("report = %+v", report)
		}
		if report.Results[3].Error != errKeptNoFeed.Error() || report.Results[5].Error != errKeptNoIdentity.Error() {
			t.Fatalf("errors = %q, %q", report.Results[3].Error, report.Results[5].Error)
		}

		// The existing entry is kept deliberately and read passively; the
		// import never rewrites it.
		state := f.keptState(t, existing)
		if state.Kept.Value != syncmodel.True || state.Kept.Source != syncmodel.SourceDeliberate || state.Seen.Value != syncmodel.SeenValue || state.Seen.Source != syncmodel.SourcePassive {
			t.Fatalf("existing state = %+v", state)
		}
		var title string
		if err := f.db.Pool().QueryRow(ctx, "SELECT title FROM entries WHERE id=$1", existing).Scan(&title); err != nil || title != "A" {
			t.Fatalf("existing title = %q, %v", title, err)
		}

		// The exported copy keeps its history and a sanitised body.
		var seq int64
		var firstSeen, published time.Time
		var url, author, enclosures string
		if err := f.db.Pool().QueryRow(ctx, "SELECT seq,first_seen_at,published_at,url,author,enclosures::text FROM entries WHERE id=$1", created).Scan(&seq, &firstSeen, &published, &url, &author, &enclosures); err != nil {
			t.Fatal(err)
		}
		if seq <= existingSeq || !firstSeen.Equal(*b.CrawledAt) || !published.Equal(*b.PublishedAt) || url != b.URL || author != "Bea" {
			t.Fatalf("created entry seq=%d first_seen=%v published=%v url=%q author=%q", seq, firstSeen, published, url, author)
		}
		if !strings.Contains(enclosures, "b.mp3") || strings.Contains(enclosures, "javascript") {
			t.Fatalf("enclosures = %s", enclosures)
		}
		html, _, _, _, err := store.LoadContent(ctx, f.db.Pool(), created)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(html, "script") || !strings.Contains(html, "https://example.test/i.png") {
			t.Fatalf("content = %q", html)
		}
		if state := f.keptState(t, created); state.Kept.Value != syncmodel.True || state.Seen.Value != syncmodel.SeenValue {
			t.Fatalf("created state = %+v", state)
		}

		kept := f.viewIDs(t, f.a, ViewKept)
		if !kept[existing] || !kept[created] {
			t.Fatalf("kept view = %v", kept)
		}
		// Unread too, so only the backdated first sighting keeps it out.
		f.markUnreadPassively(t, created)
		if f.viewIDs(t, f.a, ViewRiver)[created] {
			t.Fatal("an imported article must not enter the river")
		}
		if got := f.count(t, "SELECT count(*) FROM jobs WHERE kind=$1", ruleJobKind); got != jobs {
			t.Fatalf("rule jobs = %d, want %d: an import is not an arrival", got, jobs)
		}
		if f.count(t, `SELECT count(*) FROM changes WHERE user_id=$1 AND type='entry.created' AND (payload->'entry'->>'id')::bigint=$2`, f.a.UserID, created) != 1 {
			t.Fatal("the created entry must be announced once")
		}
		coreCheckGapless(t, f.db, f.a.UserID)
	})

	t.Run("importing_again_changes_nothing", func(t *testing.T) {
		f := newCoreFixture(t, template)
		ctx := context.Background()
		items := []KeptImportItem{
			{FeedURL: keptTestFeed, GUID: "g1", URL: "https://example.test/1", Title: "One", Read: true, CrawledAt: keptTime("2026-01-01T00:00:00Z")},
			{FeedURL: keptTestFeed, URL: "https://example.test/2", Title: "Two", CrawledAt: keptTime("2026-01-02T00:00:00Z")},
		}
		first, err := f.s.ImportKept(ctx, f.a, items, MissingFeedsSkip)
		if err != nil {
			t.Fatal(err)
		}
		wantStatuses(t, first, KeptImportCreated, KeptImportCreated)
		entries := f.count(t, "SELECT count(*) FROM entries")
		revs := f.count(t, "SELECT value FROM rev_counters WHERE user_id=$1", f.a.UserID)
		again, err := f.s.ImportKept(ctx, f.a, items, MissingFeedsSkip)
		if err != nil {
			t.Fatal(err)
		}
		wantStatuses(t, again, KeptImportUnchanged, KeptImportUnchanged)
		if again.Results[0].EntryID != first.Results[0].EntryID || again.Results[1].EntryID != first.Results[1].EntryID {
			t.Fatalf("re-import found other entries: %+v vs %+v", again.Results, first.Results)
		}
		if f.count(t, "SELECT count(*) FROM entries") != entries || f.count(t, "SELECT value FROM rev_counters WHERE user_id=$1", f.a.UserID) != revs {
			t.Fatal("a repeated import must store and announce nothing")
		}
		coreCheckGapless(t, f.db, f.a.UserID)
	})

	t.Run("respects_decisions_made_in_earnest", func(t *testing.T) {
		f := newCoreFixture(t, template)
		ctx := context.Background()
		unkept := f.ingestKept(t, feed.Item{GUID: "https://example.test/unkept", URL: "https://example.test/unkept"})
		unread := f.ingestKept(t, feed.Item{GUID: "https://example.test/unread", URL: "https://example.test/unread"})
		gone := f.ingestKept(t, feed.Item{GUID: "https://example.test/gone", URL: "https://example.test/gone"})
		if _, err := f.s.Batch(ctx, f.a, []syncmodel.Write{f.write(t, unkept, "keep", syncmodel.Kept, syncmodel.True), f.write(t, unread, "unread", syncmodel.Seen, syncmodel.Unseen)}); err != nil {
			t.Fatal(err)
		}
		f.clock.Add(int64(time.Minute))
		if _, err := f.s.Batch(ctx, f.a, []syncmodel.Write{f.write(t, unkept, "unkeep", syncmodel.Kept, syncmodel.False)}); err != nil {
			t.Fatal(err)
		}
		if _, err := f.db.Pool().Exec(ctx, "UPDATE entries SET deleted_at=now() WHERE id=$1", gone); err != nil {
			t.Fatal(err)
		}
		f.clock.Add(int64(time.Minute))
		report, err := f.s.ImportKept(ctx, f.a, []KeptImportItem{
			{FeedURL: keptTestFeed, GUID: "https://example.test/unkept", Read: true},
			{FeedURL: keptTestFeed, GUID: "https://example.test/unread", Read: true},
			{FeedURL: keptTestFeed, GUID: "https://example.test/gone"},
		}, MissingFeedsSkip)
		if err != nil {
			t.Fatal(err)
		}
		wantStatuses(t, report, KeptImportUnkept, KeptImportMatched, KeptImportGone)
		if state := f.keptState(t, unkept); state.Kept.Value != syncmodel.False || state.Seen.Value != syncmodel.SeenValue {
			t.Fatalf("un-kept article = %+v; it stays out of Kept but may be marked read", state)
		}
		if state := f.keptState(t, unread); state.Kept.Value != syncmodel.True || state.Seen.Value != syncmodel.Unseen {
			t.Fatalf("deliberately unread article = %+v; a passive read must not override it", state)
		}
		if report.Results[2].EntryID != gone || f.keptState(t, gone).Kept.Value != syncmodel.False {
			t.Fatalf("tombstone = %+v", report.Results[2])
		}
		coreCheckGapless(t, f.db, f.a.UserID)
	})

	t.Run("finds_entries_a_fetch_stored_under_the_link_identity", func(t *testing.T) {
		f := newCoreFixture(t, template)
		// The feed marked its opaque GUID isPermaLink="false", so the fetch
		// identified the entry by link; the export only has the GUID shape.
		existing := f.ingestKept(t, feed.Item{URL: "https://example.test/ghost"})
		report, err := f.s.ImportKept(context.Background(), f.a, []KeptImportItem{{FeedURL: keptTestFeed, GUID: "6915a1b2c3d4e5f6a7b8c9d0", URL: "https://example.test/ghost", Title: "Ghost"}}, MissingFeedsSkip)
		if err != nil {
			t.Fatal(err)
		}
		wantStatuses(t, report, KeptImportMatched)
		if report.Results[0].EntryID != existing {
			t.Fatalf("matched %d, want %d", report.Results[0].EntryID, existing)
		}
	})

	t.Run("subscribes_to_missing_feeds_only_when_asked", func(t *testing.T) {
		f := newCoreFixture(t, template)
		ctx := context.Background()
		item := KeptImportItem{FeedURL: "https://new.test/feed/", FeedTitle: "New Feed", SiteURL: "https://new.test/", GUID: "https://new.test/post", URL: "https://new.test/post", Title: "Post", CrawledAt: keptTime("2026-03-01T00:00:00Z")}
		skipped, err := f.s.ImportKept(ctx, f.a, []KeptImportItem{item}, "")
		if err != nil {
			t.Fatal(err)
		}
		wantStatuses(t, skipped, KeptImportNotSubscribed)
		if f.count(t, "SELECT count(*) FROM feeds WHERE url=$1", item.FeedURL) != 0 {
			t.Fatal("skip mode must not create a feed")
		}
		report, err := f.s.ImportKept(ctx, f.a, []KeptImportItem{item, item}, MissingFeedsSubscribe)
		if err != nil {
			t.Fatal(err)
		}
		wantStatuses(t, report, KeptImportCreated, KeptImportUnchanged)
		if len(report.Subscribed) != 1 || report.Subscribed[0].URL != item.FeedURL || report.Subscribed[0].Title != "New Feed" || report.Results[0].SubscriptionID != report.Subscribed[0].ID {
			t.Fatalf("subscribed = %+v, results = %+v", report.Subscribed, report.Results)
		}
		if !report.Subscribed[0].InitialFetchPending {
			t.Fatal("a new subscription's first fetch still runs in the background")
		}
		if !f.viewIDs(t, f.a, ViewKept)[report.Results[0].EntryID] {
			t.Fatal("the article must be visible in Kept through the new subscription")
		}
		coreCheckGapless(t, f.db, f.a.UserID)
	})

	t.Run("announces_to_every_subscriber_of_a_shared_feed", func(t *testing.T) {
		f := newCoreFixture(t, template)
		ctx := context.Background()
		other := Actor{Kind: PrincipalHuman, Via: CredentialSession, Scopes: HumanScopes}
		if err := f.db.Pool().QueryRow(ctx, "INSERT INTO users(username,password_hash) VALUES('other','$argon2id$test') RETURNING id").Scan(&other.UserID); err != nil {
			t.Fatal(err)
		}
		if err := f.db.Pool().QueryRow(ctx, "INSERT INTO principals(user_id,kind,name) VALUES($1,'human','Other') RETURNING id", other.UserID).Scan(&other.PrincipalID); err != nil {
			t.Fatal(err)
		}
		var otherSubscription int64
		if err := f.db.Pool().QueryRow(ctx, "INSERT INTO subscriptions(user_id,feed_id) VALUES($1,$2) RETURNING id", other.UserID, f.feed).Scan(&otherSubscription); err != nil {
			t.Fatal(err)
		}
		report, err := f.s.ImportKept(ctx, f.a, []KeptImportItem{{FeedURL: keptTestFeed, GUID: "shared", Title: "Shared", CrawledAt: keptTime("2026-02-01T00:00:00Z")}}, MissingFeedsSkip)
		if err != nil {
			t.Fatal(err)
		}
		wantStatuses(t, report, KeptImportCreated)
		created := report.Results[0].EntryID
		if f.count(t, `SELECT count(*) FROM changes WHERE user_id=$1 AND type='entry.created' AND (payload->'entry'->>'subscription_id')::bigint=$2`, other.UserID, otherSubscription) != 1 {
			t.Fatal("the other subscriber must receive the entry under their own subscription")
		}
		if f.count(t, "SELECT count(*) FROM entry_state WHERE principal_id=$1", other.PrincipalID) != 0 {
			t.Fatal("the import writes only the importer's state")
		}
		if f.viewIDs(t, other, ViewRiver)[created] {
			t.Fatal("an imported article must not enter another subscriber's river")
		}
		coreCheckGapless(t, f.db, f.a.UserID)
		coreCheckGapless(t, f.db, other.UserID)
	})

	t.Run("orders_seq_by_the_exporting_readers_history_across_chunks", func(t *testing.T) {
		f := newCoreFixture(t, template)
		total := keptImportChunkSize + 50
		items := make([]KeptImportItem, total)
		start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
		for i := range items {
			// Newest first, as some readers export.
			crawled := start.Add(time.Duration(total-i) * time.Hour)
			items[i] = KeptImportItem{FeedURL: keptTestFeed, GUID: fmt.Sprintf("tag:example.test,2026:%d", i), Title: fmt.Sprint(i), CrawledAt: &crawled}
		}
		report, err := f.s.ImportKept(context.Background(), f.a, items, MissingFeedsSkip)
		if err != nil {
			t.Fatal(err)
		}
		for i, r := range report.Results {
			if r.Status != KeptImportCreated {
				t.Fatalf("result %d = %+v", i, r)
			}
		}
		rows, err := f.db.Pool().Query(context.Background(), "SELECT first_seen_at FROM entries WHERE feed_id=$1 ORDER BY seq", f.feed)
		if err != nil {
			t.Fatal(err)
		}
		defer rows.Close()
		var previous time.Time
		n := 0
		for rows.Next() {
			var seen time.Time
			if err := rows.Scan(&seen); err != nil {
				t.Fatal(err)
			}
			if !seen.After(previous) {
				t.Fatalf("seq order breaks crawl order at row %d: %v after %v", n, seen, previous)
			}
			previous = seen
			n++
		}
		if n != total {
			t.Fatalf("stored %d entries, want %d", n, total)
		}
		coreCheckGapless(t, f.db, f.a.UserID)
	})

	t.Run("a_later_fetch_adopts_the_stored_copy", func(t *testing.T) {
		f := newCoreFixture(t, template)
		ctx := context.Background()
		// Ghost marks its opaque GUIDs isPermaLink="false", so a fetch
		// identifies the article by link while the export implies the GUID.
		report, err := f.s.ImportKept(ctx, f.a, []KeptImportItem{{FeedURL: keptTestFeed, GUID: "69a4a92bda9cdf00013f1ca5", URL: "https://example.test/ghost-post", Title: "Ghost", Read: true, CrawledAt: keptTime("2026-03-01T00:00:00Z")}}, MissingFeedsSkip)
		if err != nil {
			t.Fatal(err)
		}
		wantStatuses(t, report, KeptImportCreated)
		imported := report.Results[0].EntryID
		identity := feed.IdentityHash(feed.Item{URL: "https://example.test/ghost-post"})
		live := IngestItem{IdentityHash: identity, GUID: "69a4a92bda9cdf00013f1ca5", URL: "https://example.test/ghost-post", Title: "Ghost, live", ContentHash: coreHash("live"), ContentHTML: "<p>live</p>", ContentText: "live", SanitizerVersion: 1, ContentSource: ContentFromFeed}
		summary, err := f.s.RecordFetch(ctx, FetchOutcome{FeedID: f.feed, Status: FetchOK, Items: []IngestItem{live}})
		if err != nil {
			t.Fatal(err)
		}
		if summary.Created != 0 || summary.Updated != 1 || f.count(t, "SELECT count(*) FROM entries") != 1 {
			t.Fatalf("summary = %+v; the fetch must adopt the imported entry, not duplicate it", summary)
		}
		var stored []byte
		var title string
		var stillImported bool
		var firstSeen time.Time
		if err := f.db.Pool().QueryRow(ctx, "SELECT identity_hash,title,imported,first_seen_at FROM entries WHERE id=$1", imported).Scan(&stored, &title, &stillImported, &firstSeen); err != nil {
			t.Fatal(err)
		}
		if fmt.Sprintf("%x", stored) != identity || title != "Ghost, live" || stillImported || !firstSeen.Equal(*keptTime("2026-03-01T00:00:00Z")) {
			t.Fatalf("adopted entry identity=%x title=%q imported=%v first_seen=%v", stored, title, stillImported, firstSeen)
		}
		if state := f.keptState(t, imported); state.Kept.Value != syncmodel.True {
			t.Fatalf("adoption must keep your state: %+v", state)
		}
		again, err := f.s.RecordFetch(ctx, FetchOutcome{FeedID: f.feed, Status: FetchOK, Items: []IngestItem{live}})
		if err != nil || again.Created != 0 || again.Updated != 0 {
			t.Fatalf("a repeated fetch must match the adopted identity directly: %+v %v", again, err)
		}
		coreCheckGapless(t, f.db, f.a.UserID)
	})

	t.Run("adoption_by_link_needs_the_title_and_no_conflicting_guid", func(t *testing.T) {
		f := newCoreFixture(t, template)
		ctx := context.Background()
		const link = "https://example.test/shared-link"
		// Inoreader exports carry no publisher GUID, so only the link and
		// title can recognise the article; two articles may share the link.
		report, err := f.s.ImportKept(ctx, f.a, []KeptImportItem{
			{FeedURL: keptTestFeed, URL: link, Title: "Shared"},
			{FeedURL: keptTestFeed, GUID: "tag:example.test,2026:sponsor", URL: "https://example.test/other-link", Title: "Kept article"},
		}, MissingFeedsSkip)
		if err != nil {
			t.Fatal(err)
		}
		withoutGUID, withGUID := report.Results[0].EntryID, report.Results[1].EntryID
		live := func(guid, url, title string) IngestItem {
			return IngestItem{IdentityHash: feed.IdentityHash(feed.Item{GUID: guid, URL: url, Title: title}), GUID: guid, URL: url, Title: title, ContentHash: coreHash(guid + url + title), SanitizerVersion: 1, ContentSource: ContentFromFeed}
		}
		summary, err := f.s.RecordFetch(ctx, FetchOutcome{FeedID: f.feed, Status: FetchOK, Items: []IngestItem{
			// Same link, different title: not provably the same article.
			live("tag:example.test,2026:three", link, "Something else"),
			live("tag:example.test,2026:one", link, "Shared"),
			live("tag:example.test,2026:two", link, "Shared"),
			// Same link as the kept article would need, but another GUID:
			// a different article that must not replace it.
			live("tag:example.test,2026:linked", "https://example.test/other-link", "Kept article"),
		}})
		if err != nil {
			t.Fatal(err)
		}
		if summary.Created != 3 || summary.Updated != 1 {
			t.Fatalf("summary = %+v; only the first matching article adopts the GUID-less import", summary)
		}
		var adoptedGUID string
		if err := f.db.Pool().QueryRow(ctx, "SELECT guid FROM entries WHERE id=$1 AND NOT imported", withoutGUID).Scan(&adoptedGUID); err != nil || adoptedGUID != "tag:example.test,2026:one" {
			t.Fatalf("adopted guid = %q, %v", adoptedGUID, err)
		}
		var title string
		var stillImported bool
		if err := f.db.Pool().QueryRow(ctx, "SELECT title,imported FROM entries WHERE id=$1", withGUID).Scan(&title, &stillImported); err != nil || title != "Kept article" || !stillImported {
			t.Fatalf("an import with a conflicting GUID must stay untouched: %q imported=%v %v", title, stillImported, err)
		}
		coreCheckGapless(t, f.db, f.a.UserID)
	})

	t.Run("a_direct_match_reconciles_the_import", func(t *testing.T) {
		f := newCoreFixture(t, template)
		ctx := context.Background()
		const guid = "tag:example.test,2026:direct"
		report, err := f.s.ImportKept(ctx, f.a, []KeptImportItem{{FeedURL: keptTestFeed, GUID: guid, URL: "https://example.test/direct", Title: "Direct"}}, MissingFeedsSkip)
		if err != nil {
			t.Fatal(err)
		}
		live := IngestItem{IdentityHash: feed.IdentityHash(feed.Item{GUID: guid}), GUID: guid, URL: "https://example.test/direct", Title: "Direct", ContentHash: coreHash("direct"), SanitizerVersion: 1, ContentSource: ContentFromFeed}
		if _, err := f.s.RecordFetch(ctx, FetchOutcome{FeedID: f.feed, Status: FetchOK, Items: []IngestItem{live}}); err != nil {
			t.Fatal(err)
		}
		if f.count(t, "SELECT count(*) FROM entries WHERE imported") != 0 || f.count(t, "SELECT count(*) FROM entries") != 1 {
			t.Fatal("a fetch that matches an imported entry by identity must clear its import flag")
		}
		if report.Results[0].EntryID == 0 {
			t.Fatal("missing entry")
		}
	})

	t.Run("a_fetch_never_resurrects_a_removed_import", func(t *testing.T) {
		f := newCoreFixture(t, template)
		ctx := context.Background()
		report, err := f.s.ImportKept(ctx, f.a, []KeptImportItem{{FeedURL: keptTestFeed, GUID: "abc123", URL: "https://example.test/removed", Title: "Removed"}}, MissingFeedsSkip)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := f.db.Pool().Exec(ctx, "UPDATE entries SET deleted_at=now() WHERE id=$1", report.Results[0].EntryID); err != nil {
			t.Fatal(err)
		}
		live := IngestItem{IdentityHash: feed.IdentityHash(feed.Item{URL: "https://example.test/removed"}), GUID: "abc123", URL: "https://example.test/removed", Title: "Removed", ContentHash: coreHash("removed"), SanitizerVersion: 1, ContentSource: ContentFromFeed}
		summary, err := f.s.RecordFetch(ctx, FetchOutcome{FeedID: f.feed, Status: FetchOK, Items: []IngestItem{live}})
		if err != nil || summary.Created != 0 || summary.Updated != 0 || f.count(t, "SELECT count(*) FROM entries") != 1 {
			t.Fatalf("summary = %+v, %v; a tombstoned import stays removed", summary, err)
		}
	})

	t.Run("rejects_unauthorised_and_unbounded_imports", func(t *testing.T) {
		f := newCoreFixture(t, template)
		ctx := context.Background()
		noFeeds := f.a
		noFeeds.Scopes = []Scope{ScopeEntriesRead, ScopeStateWrite}
		if _, err := f.s.ImportKept(ctx, noFeeds, nil, MissingFeedsSkip); !errors.Is(err, ErrForbidden) {
			t.Fatalf("without feeds:write: %v", err)
		}
		if _, err := f.s.ImportKept(ctx, f.a, nil, "maybe"); !errors.Is(err, ErrInvalid) {
			t.Fatalf("unknown policy: %v", err)
		}
		if _, err := f.s.ImportKept(ctx, f.a, make([]KeptImportItem, maxKeptImportItems+1), MissingFeedsSkip); !errors.Is(err, ErrInvalid) {
			t.Fatalf("too many articles: %v", err)
		}
		report, err := f.s.ImportKept(ctx, f.a, []KeptImportItem{{FeedURL: "ftp://example.test/feed", GUID: "x"}, {FeedURL: keptTestFeed, GUID: strings.Repeat("x", maxKeptImportField+1)}}, MissingFeedsSkip)
		if err != nil {
			t.Fatal(err)
		}
		wantStatuses(t, report, KeptImportFailed, KeptImportFailed)
	})
}

// markUnreadPassively resets the seen register as a passive client would, so
// river membership depends only on the entry itself.
func (f *coreFixture) markUnreadPassively(t *testing.T, entry int64) {
	t.Helper()
	if _, err := f.db.Pool().Exec(context.Background(), "UPDATE entry_state SET seen='unseen' WHERE principal_id=$1 AND entry_id=$2", f.a.PrincipalID, entry); err != nil {
		t.Fatal(err)
	}
}
