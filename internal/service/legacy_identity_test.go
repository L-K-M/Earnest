package service

import (
	"context"
	"testing"

	"github.com/L-K-M/Earnest/internal/feed"
	"github.com/L-K-M/Earnest/internal/store/storetest"
)

func TestRecordFetchAdoptsLegacyIdentities(t *testing.T) {
	template := storetest.NewTemplate(t)
	comment := func(n string) IngestItem {
		guid := "https://example.test/?p=7#comment-" + n
		it := feed.Item{GUID: guid}
		return IngestItem{IdentityHash: feed.IdentityHash(it), LegacyIdentityHash: feed.LegacyIdentityHash(it), GUID: guid, Title: "Comment " + n, ContentHash: coreHash("comment" + n), SanitizerVersion: 1, ContentSource: ContentFromFeed}
	}
	// storeLegacy stores an entry the way the earlier rule did: fragment dropped.
	storeLegacy := func(t *testing.T, f *coreFixture) int64 {
		t.Helper()
		old := comment("1")
		old.IdentityHash = old.LegacyIdentityHash
		old.LegacyIdentityHash = ""
		if _, err := f.s.RecordFetch(context.Background(), FetchOutcome{FeedID: f.feed, Status: FetchOK, Items: []IngestItem{old}}); err != nil {
			t.Fatal(err)
		}
		var id int64
		if err := f.db.Pool().QueryRow(context.Background(), "SELECT id FROM entries WHERE feed_id=$1", f.feed).Scan(&id); err != nil {
			t.Fatal(err)
		}
		return id
	}

	t.Run("the_first_article_adopts_and_the_rest_are_new", func(t *testing.T) {
		f := newCoreFixture(t, template)
		ctx := context.Background()
		legacy := storeLegacy(t, f)
		summary, err := f.s.RecordFetch(ctx, FetchOutcome{FeedID: f.feed, Status: FetchOK, Items: []IngestItem{comment("1"), comment("2")}})
		if err != nil {
			t.Fatal(err)
		}
		if summary.Created != 1 || summary.Updated != 1 || f.count(t, "SELECT count(*) FROM entries") != 2 {
			t.Fatalf("summary = %+v; the stored comment adopts its new identity and the other is new", summary)
		}
		if f.count(t, "SELECT count(*) FROM entries WHERE id=$1 AND identity_hash=$2", legacy, mustCoreHash(t, comment("1").IdentityHash)) != 1 {
			t.Fatal("the legacy entry must take the current identity")
		}
		again, err := f.s.RecordFetch(ctx, FetchOutcome{FeedID: f.feed, Status: FetchOK, Items: []IngestItem{comment("1"), comment("2")}})
		if err != nil || again.Created != 0 || again.Updated != 0 {
			t.Fatalf("a repeated fetch must match both directly: %+v %v", again, err)
		}
		coreCheckGapless(t, f.db, f.a.UserID)
	})

	t.Run("a_legacy_tombstone_is_never_resurrected", func(t *testing.T) {
		f := newCoreFixture(t, template)
		ctx := context.Background()
		legacy := storeLegacy(t, f)
		if _, err := f.db.Pool().Exec(ctx, "UPDATE entries SET deleted_at=now() WHERE id=$1", legacy); err != nil {
			t.Fatal(err)
		}
		summary, err := f.s.RecordFetch(ctx, FetchOutcome{FeedID: f.feed, Status: FetchOK, Items: []IngestItem{comment("1")}})
		if err != nil || summary.Created != 0 || summary.Updated != 0 || f.count(t, "SELECT count(*) FROM entries") != 1 {
			t.Fatalf("summary = %+v, %v", summary, err)
		}
	})
}
