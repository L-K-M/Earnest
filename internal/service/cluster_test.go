package service

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	"github.com/L-K-M/Earnest/internal/store"
	"github.com/L-K-M/Earnest/internal/store/storetest"
	"github.com/L-K-M/Earnest/internal/syncmodel"
)

func clusterItem(key, title, url string) IngestItem {
	return IngestItem{IdentityHash: coreHash("cluster:" + key), ContentHash: coreHash("clusterbody:" + key), Title: title, URL: url, ContentHTML: "<p>" + key + "</p>", ContentText: "text " + key, SanitizerVersion: 1, ContentSource: ContentFromFeed}
}

func clusterIngest(t *testing.T, f *coreFixture, feed int64, key, title, url string) int64 {
	t.Helper()
	ctx := context.Background()
	if _, err := f.s.RecordFetch(ctx, FetchOutcome{FeedID: feed, Status: FetchOK, Items: []IngestItem{clusterItem(key, title, url)}}); err != nil {
		t.Fatal(err)
	}
	var id int64
	if err := f.db.Pool().QueryRow(ctx, "SELECT id FROM entries WHERE feed_id=$1 AND identity_hash=$2", feed, mustCoreHash(t, coreHash("cluster:"+key))).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}

func clusterIDOf(t *testing.T, db *store.Store, user, entry int64) int64 {
	t.Helper()
	var id int64
	if err := db.Pool().QueryRow(context.Background(), "SELECT cluster_id FROM entry_clusters WHERE user_id=$1 AND entry_id=$2 AND deleted_at IS NULL", user, entry).Scan(&id); err != nil {
		t.Fatalf("entry %d cluster: %v", entry, err)
	}
	return id
}

func clusterReason(t *testing.T, db *store.Store, user, entry int64) string {
	t.Helper()
	var r string
	if err := db.Pool().QueryRow(context.Background(), "SELECT reason FROM entry_clusters WHERE user_id=$1 AND entry_id=$2 AND deleted_at IS NULL", user, entry).Scan(&r); err != nil {
		t.Fatalf("entry %d reason: %v", entry, err)
	}
	return r
}

// clusterTestEmbed writes a unit vector on one axis, current unless stale.
func clusterTestEmbed(t *testing.T, db *store.Store, entry int64, model string, component int, stale bool) {
	t.Helper()
	ctx := context.Background()
	var hash []byte
	var title string
	if err := db.Pool().QueryRow(ctx, "SELECT content_hash, title FROM entries WHERE id=$1", entry).Scan(&hash, &title); err != nil {
		t.Fatal(err)
	}
	if stale {
		hash = bytes.Repeat([]byte{0xAB}, 32)
		title = "stale"
	}
	vec := make([]string, 1024)
	for i := range vec {
		vec[i] = "0"
	}
	vec[component] = "1"
	if _, err := db.Pool().Exec(ctx, `INSERT INTO embeddings(entry_id,model,dims,embedding,content_hash,input_hash,input_title)
		VALUES($1,$2,1024,$3::halfvec,$4,$5,$6)`, entry, model, "["+strings.Join(vec, ",")+"]", hash, bytes.Repeat([]byte{7}, 32), title); err != nil {
		t.Fatal(err)
	}
}

func TestClusterPendingDeterministic(t *testing.T) {
	f := newCoreFixture(t, storetest.NewTemplate(t))
	ctx := context.Background()
	user := f.a.UserID
	feedB := entriesTestFeed(t, f.db, "https://cluster-b.test/rss", 0)
	entriesTestSub(t, f.db, user, feedB, "now")

	e1 := clusterIngest(t, f, f.feed, "a1", "Acme buys Globex", "https://w.test/acme")
	e2 := clusterIngest(t, f, feedB, "a2", "Acme buys Globex - Site", "https://w.test/acme")
	e3 := clusterIngest(t, f, feedB, "a3", "Acme buys Globex - Other", "https://other.test/acme-2")

	if n, err := f.s.ClusterPending(ctx, "", 100); err != nil || n != 3 {
		t.Fatalf("first pass %d %v", n, err)
	}
	c1 := clusterIDOf(t, f.db, user, e1)
	if clusterIDOf(t, f.db, user, e2) != c1 || clusterIDOf(t, f.db, user, e3) != c1 {
		t.Fatal("same story split across clusters")
	}
	for e, want := range map[int64]ClusterReason{e1: ClusterNewStory, e2: ClusterSameURL, e3: ClusterSameTitle} {
		if r := clusterReason(t, f.db, user, e); r != string(want) {
			t.Fatalf("entry %d reason %s", e, r)
		}
	}
	if n, err := f.s.ClusterPending(ctx, "", 100); err != nil || n != 0 {
		t.Fatalf("idempotent pass %d %v", n, err)
	}

	// Per-feed cap: feedB already contributes two members, so a third feedB
	// entry on the same story starts its own cluster despite the exact URL.
	e4 := clusterIngest(t, f, feedB, "a4", "Acme buys Globex", "https://w.test/acme")
	if n, err := f.s.ClusterPending(ctx, "", 100); err != nil || n != 1 {
		t.Fatalf("cap pass %d %v", n, err)
	}
	if c4 := clusterIDOf(t, f.db, user, e4); c4 == c1 {
		t.Fatal("third member from one feed joined")
	}

	// f.feed contributes only e1; another f.feed member still fits, and the
	// most-recent/lowest-id tie-break keeps it in the original cluster.
	e5 := clusterIngest(t, f, f.feed, "a5", "Acme buys Globex", "https://w.test/acme")
	if n, err := f.s.ClusterPending(ctx, "", 100); err != nil || n != 1 {
		t.Fatalf("join pass %d %v", n, err)
	}
	if c5 := clusterIDOf(t, f.db, user, e5); c5 != c1 {
		t.Fatal("second feed member rejected")
	}

	// A private feed clusters inside its owner's user only.
	private := entriesTestFeed(t, f.db, "https://priv.test/rss", user)
	entriesTestSub(t, f.db, user, private, "now")
	eP := clusterIngest(t, f, private, "p1", "Acme buys Globex", "https://w.test/acme")
	if n, err := f.s.ClusterPending(ctx, "", 100); err != nil || n != 1 {
		t.Fatalf("private pass %d %v", n, err)
	}
	if cp := clusterIDOf(t, f.db, user, eP); cp != c1 {
		t.Fatal("private entry could not join its owner's cluster")
	}

	// A second user on the shared feed gets their own clusters for the same
	// public entries; nothing crosses users.
	other := entriesTestOtherUser(t, f.db, "other")
	entriesTestSub(t, f.db, other.UserID, feedB, "now")
	if n, err := f.s.ClusterPending(ctx, "", 100); err != nil || n != 3 {
		t.Fatalf("other user pass %d %v", n, err)
	}
	co := clusterIDOf(t, f.db, other.UserID, e2)
	if co == c1 {
		t.Fatal("other user shared a cluster id")
	}
	if clusterIDOf(t, f.db, other.UserID, e3) != co {
		t.Fatal("other user's same story split")
	}
	var c4dup int64
	err := f.db.Pool().QueryRow(ctx, "SELECT cluster_id FROM entry_clusters WHERE user_id=$1 AND entry_id=$2 AND deleted_at IS NULL", other.UserID, e4).Scan(&c4dup)
	if err != nil || c4dup == co || c4dup == c1 {
		t.Fatalf("other user feed cap: %d %v", c4dup, err)
	}
	coreCheckGapless(t, f.db, user)
	coreCheckGapless(t, f.db, other.UserID)
}

func TestClusterPendingModel(t *testing.T) {
	f := newCoreFixture(t, storetest.NewTemplate(t))
	ctx := context.Background()
	user := f.a.UserID
	feedB := entriesTestFeed(t, f.db, "https://cluster-b.test/rss", 0)
	entriesTestSub(t, f.db, user, feedB, "now")
	const model = "embed-test"

	e1 := clusterIngest(t, f, f.feed, "m1", "First title", "https://v.test/1")
	e2 := clusterIngest(t, f, feedB, "m2", "Different title entirely", "https://v.test/2")
	e3 := clusterIngest(t, f, f.feed, "m3", "Third unrelated", "https://v.test/3")
	clusterTestEmbed(t, f.db, e1, model, 0, false)
	clusterTestEmbed(t, f.db, e2, model, 0, false)
	clusterTestEmbed(t, f.db, e3, model, 1, false)
	e4 := clusterIngest(t, f, feedB, "m4", "No vector here", "https://v.test/4")

	if n, err := f.s.ClusterPending(ctx, model, 100); err != nil || n != 3 {
		t.Fatalf("model pass %d %v", n, err)
	}
	c1 := clusterIDOf(t, f.db, user, e1)
	if clusterIDOf(t, f.db, user, e2) != c1 {
		t.Fatal("similar entry did not join")
	}
	if r := clusterReason(t, f.db, user, e2); r != string(ClusterSimilar) {
		t.Fatalf("reason %s", r)
	}
	if clusterIDOf(t, f.db, user, e3) == c1 {
		t.Fatal("unrelated entry joined")
	}

	// A stale vector (content_hash no longer matching the entry) cannot join.
	e5 := clusterIngest(t, f, feedB, "m5", "Stale vector", "https://v.test/5")
	clusterTestEmbed(t, f.db, e5, model, 0, true)
	if n, err := f.s.ClusterPending(ctx, model, 100); err != nil || n != 0 {
		t.Fatalf("stale pass %d %v", n, err)
	}
	var count int
	if err := f.db.Pool().QueryRow(ctx, `SELECT count(*) FROM entry_clusters WHERE entry_id=ANY($1)`, []int64{e4, e5}).Scan(&count); err != nil || count != 0 {
		t.Fatalf("premature cluster decision: %d %v", count, err)
	}

	// model "" never consults embeddings, even on a matching current vector.
	e6 := clusterIngest(t, f, f.feed, "m6", "Embedded but deterministic only", "https://v.test/6")
	clusterTestEmbed(t, f.db, e6, model, 0, false)
	if n, err := f.s.ClusterPending(ctx, "", 100); err != nil || n != 3 {
		t.Fatalf("deterministic pass %d %v", n, err)
	}
	if c6 := clusterIDOf(t, f.db, user, e6); c6 == c1 {
		t.Fatal("empty model used embeddings")
	}
}

func TestClusterSuppression(t *testing.T) {
	f := newCoreFixture(t, storetest.NewTemplate(t))
	ctx := context.Background()
	user, human := f.a.UserID, f.a.PrincipalID
	feedB := entriesTestFeed(t, f.db, "https://cluster-b.test/rss", 0)
	entriesTestSub(t, f.db, user, feedB, "now")

	e1 := clusterIngest(t, f, f.feed, "s1", "Merger approved", "https://w.test/merger")
	e2 := clusterIngest(t, f, feedB, "s2", "Merger approved - Wire", "https://w.test/merger")
	if _, err := f.s.ClusterPending(ctx, "", 100); err != nil {
		t.Fatal(err)
	}
	cluster := clusterIDOf(t, f.db, user, e1)
	if clusterIDOf(t, f.db, user, e2) != cluster {
		t.Fatal("not clustered")
	}

	// No done mark yet: nothing is suppressed.
	sup, err := f.s.ClusterSuppressed(ctx, f.a, human, []int64{e1, e2})
	if err != nil || sup[e1] || sup[e2] {
		t.Fatalf("early suppression %v %v", sup, err)
	}

	// Done through Service.Cluster writes the batch; the state.go hook then
	// records the cluster-scoped mark for the human principal.
	if _, err := f.s.Cluster(ctx, f.a, cluster, f.write(t, e1, "done-cluster", syncmodel.Shelf, syncmodel.Done)); err != nil {
		t.Fatal(err)
	}

	// A member that joins after the mark is the only suppressed entry.
	e3 := clusterIngest(t, f, feedB, "s3", "Merger approved - Later", "https://w.test/merger")
	if n, err := f.s.ClusterPending(ctx, "", 100); err != nil || n != 1 {
		t.Fatalf("late join %d %v", n, err)
	}
	if clusterIDOf(t, f.db, user, e3) != cluster {
		t.Fatal("late member missed cluster")
	}
	sup, err = f.s.ClusterSuppressed(ctx, f.a, human, []int64{e1, e2, e3})
	if err != nil || sup[e1] || sup[e2] || !sup[e3] {
		t.Fatalf("suppression %v %v", sup, err)
	}

	// The stored registers stay untouched: the hide is read-time only.
	st, err := store.LoadState(ctx, f.db.Pool(), human, e3)
	if err != nil {
		t.Fatal(err)
	}
	if st.Seen.Value != syncmodel.Unseen || st.Shelf.Value != syncmodel.None {
		t.Fatalf("stored state mutated: %+v", st)
	}

	page, err := f.s.ClusterEntries(ctx, f.a, EntryFilter{})
	if err != nil || len(page) != 0 {
		t.Fatalf("late member visible in river: %+v %v", page, err)
	}
	all, err := f.s.ClusterEntries(ctx, f.a, EntryFilter{View: ViewAll})
	if err != nil || len(all) != 3 {
		t.Fatalf("suppression changed all view: %+v %v", all, err)
	}
	// Keeping an article is independent of the decision to finish its story.
	if _, err := f.s.Batch(ctx, f.a, []syncmodel.Write{f.write(t, e3, "keep-late", syncmodel.Kept, syncmodel.True)}); err != nil {
		t.Fatal(err)
	}
	page, err = f.s.ClusterEntries(ctx, f.a, EntryFilter{})
	if err != nil || len(page) != 0 {
		t.Fatalf("kept unexpectedly lifted suppression: %+v %v", page, err)
	}

	// Another principal never inherited the human's done mark.
	agent := entriesTestAgent(t, f.db, user, "scout", ScopeEntriesRead)
	sup, err = f.s.ClusterSuppressed(ctx, f.a, agent.PrincipalID, []int64{e3})
	if err != nil || sup[e3] {
		t.Fatalf("agent suppression %v %v", sup, err)
	}

	// An explicit unread newer than the mark restores the member.
	if _, err := f.s.Batch(ctx, f.a, []syncmodel.Write{f.write(t, e3, "un-suppress", syncmodel.Seen, syncmodel.Unseen)}); err != nil {
		t.Fatal(err)
	}
	sup, err = f.s.ClusterSuppressed(ctx, f.a, human, []int64{e3})
	if err != nil || sup[e3] {
		t.Fatalf("explicit unread still suppressed %v %v", sup, err)
	}
	// Reopening the whole cluster clears its suppression receipt for future joins.
	if _, err := f.s.Cluster(ctx, f.a, cluster, f.write(t, e1, "finish-again", syncmodel.Shelf, syncmodel.Done)); err != nil {
		t.Fatal(err)
	}
	if _, err := f.s.Cluster(ctx, f.a, cluster, f.write(t, e1, "reopen-cluster", syncmodel.Seen, syncmodel.Unseen)); err != nil {
		t.Fatal(err)
	}
	var receipts int
	if err := f.db.Pool().QueryRow(ctx, `SELECT count(*) FROM cluster_dones WHERE user_id=$1 AND cluster_id=$2`, user, cluster).Scan(&receipts); err != nil || receipts != 0 {
		t.Fatalf("reopened receipt %d %v", receipts, err)
	}
	e4 := clusterIngest(t, f, f.feed, "s4", "Merger approved again", "https://w.test/merger")
	if _, err := f.s.ClusterPending(ctx, "", 100); err != nil {
		t.Fatal(err)
	}
	sup, err = f.s.ClusterSuppressed(ctx, f.a, human, []int64{e4})
	if err != nil || sup[e4] {
		t.Fatalf("reopened cluster suppresses future member: %v %v", sup, err)
	}
}

func TestClusterGroups(t *testing.T) {
	f := newCoreFixture(t, storetest.NewTemplate(t))
	ctx := context.Background()
	feedB := entriesTestFeed(t, f.db, "https://cluster-b.test/rss", 0)
	entriesTestSub(t, f.db, f.a.UserID, feedB, "now")

	e1 := clusterIngest(t, f, f.feed, "g1", "Shared story", "https://g.test/s")
	e2 := clusterIngest(t, f, feedB, "g2", "Shared story - Wire", "https://g.test/s")
	e3 := clusterIngest(t, f, f.feed, "g3", "Solo item", "https://g.test/solo")
	if _, err := f.s.ClusterPending(ctx, "", 100); err != nil {
		t.Fatal(err)
	}
	shared := clusterIDOf(t, f.db, f.a.UserID, e1)
	if clusterIDOf(t, f.db, f.a.UserID, e2) != shared {
		t.Fatal("shared story split")
	}
	e4 := clusterIngest(t, f, f.feed, "g4", "Unclustered yet", "https://g.test/later")

	groups, err := f.s.ClusterGroups(ctx, f.a, EntryFilter{View: ViewAll})
	if err != nil || len(groups) != 3 {
		t.Fatalf("groups %v %v", groups, err)
	}
	byID := map[int64]ClusterGroup{}
	for _, g := range groups {
		byID[g.Entry.ID] = g
	}
	if byID[e1].ClusterID != shared || byID[e1].AlsoIn != 1 {
		t.Fatalf("shared cluster groups %+v", byID[e1])
	}
	if byID[e3].ClusterID == 0 || byID[e3].ClusterID == shared || byID[e3].AlsoIn != 0 {
		t.Fatalf("solo group %+v", byID[e3])
	}
	if byID[e4].ClusterID != 0 || byID[e4].AlsoIn != 0 {
		t.Fatalf("unclustered group %+v", byID[e4])
	}
}

func TestClusterTitleNear(t *testing.T) {
	cases := []struct {
		a, b string
		want bool
	}{
		{"Acme buys Globex", "acme buys globex", true},
		{"Acme buys Globex", "Acme buys Globex - The Verge", true},
		{"Acme buys Globex | Reuters", "acme buys globex", true},
		{"The Acme Corp buys Globex", "Acme Corp buys Globex", true},
		{"Short", "Short - Wire", true},
		{"Acme buys Globex", "Acme sells Globex", false},
		{"", "x", false},
	}
	for _, tc := range cases {
		m := clusterCandidate{key: clusterTitleKey(tc.b), core: clusterTitleKey(clusterTitleCore(tc.b)), tokens: clusterTitleSet(tc.b)}
		got := clusterTitleNear(m, clusterTitleKey(tc.a), clusterTitleKey(clusterTitleCore(tc.a)), clusterTitleSet(tc.a))
		if got != tc.want {
			t.Errorf("%q vs %q: got %v", tc.a, tc.b, got)
		}
	}
}

func TestClusterCanonicalURLAndWindow(t *testing.T) {
	f := newCoreFixture(t, storetest.NewTemplate(t))
	ctx := context.Background()
	first := clusterIngest(t, f, f.feed, "canonical-1", "First headline", "HTTPS://Story.test:443/article?utm_source=feed#fragment")
	secondFeed := entriesTestFeed(t, f.db, "https://other.test/feed", 0)
	entriesTestSub(t, f.db, f.a.UserID, secondFeed, "now")
	second := clusterIngest(t, f, secondFeed, "canonical-2", "Different headline", "https://story.test/article")
	if _, err := f.s.ClusterPending(ctx, "", 100); err != nil {
		t.Fatal(err)
	}
	cluster := clusterIDOf(t, f.db, f.a.UserID, first)
	if clusterIDOf(t, f.db, f.a.UserID, second) != cluster || clusterReason(t, f.db, f.a.UserID, second) != string(ClusterSameURL) {
		t.Fatal("canonical URL did not match")
	}
	f.clock.Add(int64(8 * 24 * time.Hour))
	third := clusterIngest(t, f, f.feed, "old-window", "First headline", "https://story.test/article")
	if _, err := f.s.ClusterPending(ctx, "", 100); err != nil {
		t.Fatal(err)
	}
	if clusterIDOf(t, f.db, f.a.UserID, third) == cluster {
		t.Fatal("expired cluster accepted new story")
	}
}

func TestClusterBackfillHonorsIngestWindow(t *testing.T) {
	f := newCoreFixture(t, storetest.NewTemplate(t))
	first := clusterIngest(t, f, f.feed, "historic-story", "Annual announcement", "https://history.test/story")
	f.clock.Add(int64(8 * 24 * time.Hour))
	second := clusterIngest(t, f, f.feed, "current-story", "Annual announcement", "https://history.test/story")
	if _, err := f.s.ClusterPending(context.Background(), "", 100); err != nil {
		t.Fatal(err)
	}
	if clusterIDOf(t, f.db, f.a.UserID, first) == clusterIDOf(t, f.db, f.a.UserID, second) {
		t.Fatal("backfill merged entries outside the ingest-time window")
	}
}
