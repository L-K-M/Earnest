package service

import (
	"testing"

	"github.com/L-K-M/Earnest/internal/store/storetest"
	"github.com/L-K-M/Earnest/internal/syncmodel"
)

// The tallies must match listing view=all with unread_only and counting the
// rows client-side: seen and Done excluded, Later and Keep as subsets of the
// unread set.
func TestUnreadCountsMatchEntryListingSemantics(t *testing.T) {
	f := newCoreFixture(t, storetest.NewTemplate(t))
	ctx := t.Context()

	var secondFeed int64
	if err := f.db.Pool().QueryRow(ctx, "INSERT INTO feeds(url) VALUES('https://example.test/other') RETURNING id").Scan(&secondFeed); err != nil {
		t.Fatal(err)
	}
	var secondSub int64
	if err := f.db.Pool().QueryRow(ctx, "INSERT INTO subscriptions(user_id,feed_id) VALUES($1,$2) RETURNING id", f.a.UserID, secondFeed).Scan(&secondSub); err != nil {
		t.Fatal(err)
	}

	f.ingest(t, "plain unread")
	later := f.ingest(t, "later unread")
	done := f.ingest(t, "done entry")
	seen := f.ingest(t, "seen entry")
	kept := f.ingest(t, "kept entry")
	if _, err := f.s.RecordFetch(ctx, FetchOutcome{FeedID: secondFeed, Status: FetchOK, Items: []IngestItem{coreItem("other feed entry")}}); err != nil {
		t.Fatal(err)
	}

	writes := []syncmodel.Write{
		f.write(t, later, "later-op", syncmodel.Shelf, syncmodel.Later),
		f.write(t, done, "done-op", syncmodel.Shelf, syncmodel.Done),
		f.write(t, seen, "seen-op", syncmodel.Seen, syncmodel.SeenValue),
		f.write(t, kept, "kept-op", syncmodel.Kept, syncmodel.True),
	}
	if _, err := f.s.Batch(ctx, f.a, writes); err != nil {
		t.Fatal(err)
	}

	counts, err := f.s.UnreadCounts(ctx, f.a)
	if err != nil {
		t.Fatal(err)
	}
	bySubscription := map[int64]FeedUnreadCount{}
	for _, fc := range counts.Feeds {
		bySubscription[fc.SubscriptionID] = fc
	}
	// First feed: plain, later and kept are unread; done and seen are not.
	if first := bySubscription[f.subscription]; first.Unread != 3 || first.Later != 1 || first.Kept != 1 {
		t.Fatalf("first feed counts %+v", first)
	}
	if second := bySubscription[secondSub]; second.Unread != 1 {
		t.Fatalf("second feed counts %+v", second)
	}
	if counts.Later != 1 || counts.Kept != 1 {
		t.Fatalf("totals %+v", counts)
	}
}
