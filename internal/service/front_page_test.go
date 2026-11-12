package service

import (
	"encoding/json"
	"fmt"
	"reflect"
	"testing"
	"time"

	"github.com/L-K-M/Earnest/internal/llm"
	"github.com/L-K-M/Earnest/internal/store"
	"github.com/L-K-M/Earnest/internal/store/storetest"
)

func TestFrontPageSelectionCoverageAndVariety(t *testing.T) {
	now := time.Date(2026, 11, 12, 12, 0, 0, 0, time.UTC)
	candidates := []store.FrontPageCandidate{}
	add := func(id, feed, cluster int64, age time.Duration) {
		candidates = append(candidates, store.FrontPageCandidate{EntryID: id, FeedID: feed, ClusterID: cluster, PublishedAt: now.Add(-age)})
	}
	// Many contributions from one feed must not beat three distinct feeds.
	add(1, 1, 1, 0)
	add(2, 1, 1, time.Minute)
	add(3, 2, 1, time.Minute)
	add(4, 1, 2, time.Hour)
	add(5, 2, 2, time.Hour)
	add(6, 3, 2, time.Hour)
	for i := int64(10); i < 30; i++ {
		add(i, 4, 0, time.Duration(i)*time.Minute)
	}
	add(40, 5, 0, 2*time.Hour)
	add(41, 6, 0, 3*time.Hour)
	shared, singles := selectFrontPage(candidates)
	if len(shared) != 2 || shared[0].id != "cluster:2" || len(shared[1].feeds) != 2 {
		t.Fatalf("coverage ranking %+v", shared)
	}
	if len(singles) != 8 || singles[0].members[0].FeedID != 4 || singles[1].members[0].FeedID != 5 || singles[2].members[0].FeedID != 6 {
		t.Fatalf("variety %+v", singles)
	}
	// Input order and map iteration cannot alter the chosen order.
	for i, j := 0, len(candidates)-1; i < j; i, j = i+1, j-1 {
		candidates[i], candidates[j] = candidates[j], candidates[i]
	}
	again, other := selectFrontPage(candidates)
	if !reflect.DeepEqual(shared, again) || !reflect.DeepEqual(singles, other) {
		t.Fatal("unstable selection")
	}
}

func TestFrontPageWindowCadenceMutesReadAndHistory(t *testing.T) {
	f := newCoreFixture(t, storetest.NewTemplate(t))
	ctx := t.Context()
	now := f.s.now()
	today := f.ingest(t, "today")
	weekly := f.ingest(t, "weekly")
	old := f.ingest(t, "old")
	future := f.ingest(t, "future")
	history := f.ingest(t, "history")
	muted := f.ingest(t, "muted keyword")
	deleted := f.ingest(t, "deleted")
	for id, at := range map[int64]time.Time{today: now.Add(-24 * time.Hour), weekly: now.Add(-7 * 24 * time.Hour), old: now.Add(-8 * 24 * time.Hour), future: now.Add(time.Second)} {
		if _, err := f.db.Pool().Exec(ctx, `UPDATE entries SET published_at=$2 WHERE id=$1`, id, at); err != nil {
			t.Fatal(err)
		}
	}
	for _, q := range []string{fmt.Sprintf("UPDATE entries SET imported=true WHERE id=%d", history), fmt.Sprintf("UPDATE entries SET deleted_at=now() WHERE id=%d", deleted), fmt.Sprintf("UPDATE subscriptions SET cadence='week' WHERE id=%d", f.subscription), fmt.Sprintf("INSERT INTO mutes(user_id,kind,value) VALUES(%d,'keyword','muted')", f.a.UserID)} {
		if _, err := f.db.Pool().Exec(ctx, q); err != nil {
			t.Fatal(err)
		}
	}
	entriesTestState(t, f.db, f.a.PrincipalID, today, "seen", "seen")
	daily, err := f.s.FrontPage(ctx, f.a, EditionDaily)
	if err != nil {
		t.Fatal(err)
	}
	if len(daily.Sections) != 1 || len(daily.Sections[0].Stories) != 1 || daily.Sections[0].Stories[0].Entry.ID != today {
		t.Fatalf("daily %+v (muted %d)", daily, muted)
	}
	week, err := f.s.FrontPage(ctx, f.a, EditionWeekly)
	if err != nil {
		t.Fatal(err)
	}
	if len(week.Sections) != 1 || len(week.Sections[0].Stories) != 2 {
		t.Fatalf("weekly %+v", week)
	}
	if daily.Sections[0].Stories[0].Excerpt == "" {
		t.Fatal("missing publisher excerpt")
	}
	if _, err := f.db.Pool().Exec(ctx, `UPDATE entry_state SET seen='unseen' WHERE principal_id=$1`, f.a.PrincipalID); err != nil {
		t.Fatal(err)
	}
	again, err := f.s.FrontPage(ctx, f.a, EditionDaily)
	if err != nil || again.Revision != daily.Revision {
		t.Fatalf("state changed revision: %s / %s %v", daily.Revision, again.Revision, err)
	}
	if _, err := f.db.Pool().Exec(ctx, `UPDATE subscriptions SET muted=true WHERE id=$1`, f.subscription); err != nil {
		t.Fatal(err)
	}
	empty, err := f.s.FrontPage(ctx, f.a, EditionWeekly)
	if err != nil || len(empty.Sections) != 0 {
		t.Fatalf("muted %+v %v", empty, err)
	}
	if _, err := f.s.FrontPage(ctx, f.a, "monthly"); err == nil {
		t.Fatal("accepted invalid period")
	}
}

func TestFrontPageRanksBeyondFirstEntryPage(t *testing.T) {
	f := newCoreFixture(t, storetest.NewTemplate(t))
	ctx := t.Context()
	feed2 := entriesTestFeed(t, f.db, "https://second.test/rss", 0)
	entriesTestSub(t, f.db, f.a.UserID, feed2, "now")
	first := clusterIngest(t, f, f.feed, "shared-a", "Shared headline on the front page", "https://news.test/story")
	clusterIngest(t, f, feed2, "shared-b", "Shared headline on the front page", "https://news.test/story")
	if _, err := f.s.ClusterPending(ctx, "", 100); err != nil {
		t.Fatal(err)
	}
	items := []IngestItem{}
	for i := 0; i < 220; i++ {
		items = append(items, coreItem(fmt.Sprintf("noise-%d", i)))
	}
	if _, err := f.s.RecordFetch(ctx, FetchOutcome{FeedID: f.feed, Status: FetchOK, Items: items}); err != nil {
		t.Fatal(err)
	}
	page, err := f.s.FrontPage(ctx, f.a, EditionDaily)
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Sections) != 2 || page.Sections[0].Stories[0].FeedCount != 2 {
		t.Fatalf("older shared story missing %+v (first %d)", page, first)
	}
	if len(page.Sections[0].Stories[0].Coverage) != 2 || len(page.Sections[1].Stories) != 8 {
		t.Fatal("wrong selection sizes")
	}
	var user, principal int64
	if err := f.db.Pool().QueryRow(ctx, `INSERT INTO users(username,password_hash) VALUES('front-other','$argon2id$test') RETURNING id`).Scan(&user); err != nil {
		t.Fatal(err)
	}
	if err := f.db.Pool().QueryRow(ctx, `INSERT INTO principals(user_id,kind,name) VALUES($1,'human','Other') RETURNING id`, user).Scan(&principal); err != nil {
		t.Fatal(err)
	}
	isolated, err := f.s.FrontPage(ctx, Actor{UserID: user, PrincipalID: principal, Kind: PrincipalHuman, Via: CredentialSession, Scopes: HumanScopes}, EditionDaily)
	if err != nil || len(isolated.Sections) != 0 {
		t.Fatalf("account leak %+v %v", isolated, err)
	}
}

func TestFrontPageSummaryQueuedCachedAndInvalidated(t *testing.T) {
	f := newCoreFixture(t, storetest.NewTemplate(t))
	body := "Beacon launched today."
	id := f.ingest(t, body)
	p := &generatedProvider{outputs: map[string]string{"edition": groundedOutput(id, coreHash("body"+body), "Beacon launched.", body)}}
	ctx := generatedContext(t, f, p, []llm.Task{llm.TaskEdition}, 0)
	page, err := f.s.FrontPage(ctx, f.a, EditionDaily)
	if err != nil {
		t.Fatal(err)
	}
	if page.Sections[0].Stories[0].Summary != nil {
		t.Fatal("GET waited for summary")
	}
	if _, err := f.s.FrontPage(ctx, f.a, EditionWeekly); err != nil {
		t.Fatal(err)
	}
	var jobs int
	if err := f.db.Pool().QueryRow(ctx, `SELECT count(*) FROM jobs WHERE kind='front_summary'`).Scan(&jobs); err != nil || jobs != 1 {
		t.Fatalf("duplicate jobs %d %v", jobs, err)
	}
	if n, err := f.s.RunFrontPageJobs(ctx, 10); err != nil || n != 1 {
		t.Fatalf("worker %d %v", n, err)
	}
	ready, err := f.s.FrontPage(ctx, f.a, EditionDaily)
	if err != nil {
		t.Fatal(err)
	}
	if ready.Sections[0].Stories[0].Summary == nil || ready.Revision == page.Revision {
		t.Fatalf("missing summary %+v", ready)
	}
	if n, err := f.s.RunFrontPageJobs(ctx, 10); err != nil || n != 0 {
		t.Fatalf("cache miss %d %v", n, err)
	}
	// Changed publisher content cannot reuse the old prose, even with the same entry ID.
	if _, err := f.db.Pool().Exec(ctx, `UPDATE contents SET text='A different account.' WHERE entry_id=$1`, id); err != nil {
		t.Fatal(err)
	}
	fresh, err := f.s.FrontPage(ctx, f.a, EditionDaily)
	if err != nil || fresh.Sections[0].Stories[0].Summary != nil {
		t.Fatalf("stale prose %+v %v", fresh, err)
	}
}

func TestFrontPageBadCitationsAndRetryCooldown(t *testing.T) {
	f := newCoreFixture(t, storetest.NewTemplate(t))
	body := "Beacon launched today."
	id := f.ingest(t, body)
	p := &generatedProvider{outputs: map[string]string{"edition": groundedOutput(id, coreHash("body"+body), "Wrong claim.", "This passage does not exist.")}}
	ctx := generatedContext(t, f, p, []llm.Task{llm.TaskEdition}, 0)
	for i := 0; i < 3; i++ {
		if _, err := f.s.FrontPage(ctx, f.a, EditionDaily); err != nil {
			t.Fatal(err)
		}
		if n, err := f.s.RunFrontPageJobs(ctx, 1); err != nil || n != 1 {
			t.Fatalf("worker %d %v", n, err)
		}
		f.clock.Add(int64(2 * time.Minute))
	}
	page, err := f.s.FrontPage(ctx, f.a, EditionDaily)
	if err != nil || page.Sections[0].Stories[0].Summary != nil {
		t.Fatalf("bad summary %+v %v", page, err)
	}
	if n, err := f.s.RunFrontPageJobs(ctx, 1); err != nil || n != 0 {
		t.Fatalf("cooldown %d %v", n, err)
	}
}

func TestFrontPageBudgetPauseAndConsentRevocation(t *testing.T) {
	f := newCoreFixture(t, storetest.NewTemplate(t))
	body := "Beacon launched today."
	id := f.ingest(t, body)
	p := &generatedProvider{outputs: map[string]string{"edition": groundedOutput(id, coreHash("body"+body), "Beacon launched.", body)}}
	ctx := generatedContext(t, f, p, []llm.Task{llm.TaskEdition}, 1)
	if _, err := f.s.FrontPage(ctx, f.a, EditionDaily); err != nil {
		t.Fatal(err)
	}
	if n, err := f.s.RunFrontPageJobs(ctx, 1); err != nil || n != 1 {
		t.Fatalf("paused worker %d %v", n, err)
	}
	var attempts int
	if err := f.db.Pool().QueryRow(ctx, `SELECT attempts FROM jobs WHERE kind='front_summary'`).Scan(&attempts); err != nil || attempts != 0 {
		t.Fatalf("budget counts as failure %d %v", attempts, err)
	}
	if len(p.payloads()) != 0 {
		t.Fatal("spent beyond budget")
	}
	// Removing the subscription while work is queued prevents source use.
	if _, err := f.db.Pool().Exec(ctx, `UPDATE subscriptions SET deleted_at=now() WHERE id=$1`, f.subscription); err != nil {
		t.Fatal(err)
	}
	f.clock.Add(int64(2 * time.Minute))
	if n, err := f.s.RunFrontPageJobs(ctx, 1); err != nil || n != 1 {
		t.Fatalf("stale job %d %v", n, err)
	}
	if len(p.payloads()) != 0 {
		t.Fatal("used removed source")
	}
}

func TestFrontPageSummaryDistinctSourcesAndPrivateConsent(t *testing.T) {
	f := newCoreFixture(t, storetest.NewTemplate(t))
	ctx := t.Context()
	coverage := []FrontPageCoverage{}
	for n := 0; n < 7; n++ {
		owner := int64(0)
		if n == 0 {
			owner = f.a.UserID
		}
		feed := entriesTestFeed(t, f.db, fmt.Sprintf("https://coverage-%d.test/rss", n), owner)
		entriesTestSub(t, f.db, f.a.UserID, feed, "today")
		id := clusterIngest(t, f, feed, fmt.Sprintf("coverage-%d", n), "A shared headline", "https://shared.test/story")
		entry, err := f.s.Entry(ctx, f.a, id)
		if err != nil {
			t.Fatal(err)
		}
		coverage = append(coverage, FrontPageCoverage{Entry: entry})
		if n == 1 {
			coverage = append(coverage, FrontPageCoverage{Entry: entry})
		}
	}
	p := &generatedProvider{outputs: map[string]string{}}
	ctx = generatedContext(t, f, p, []llm.Task{llm.TaskEdition}, 0)
	if _, err := f.s.frontPageSummary(ctx, f.a, coverage); err != nil {
		t.Fatal(err)
	}
	var raw []byte
	if err := f.db.Pool().QueryRow(ctx, `SELECT payload FROM jobs WHERE kind='front_summary'`).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	var job frontSummaryJob
	if err := json.Unmarshal(raw, &job); err != nil {
		t.Fatal(err)
	}
	if len(job.IDs) != 4 {
		t.Fatalf("five distinct candidates minus unconsented private source: %+v", job.IDs)
	}
	for _, id := range job.IDs {
		if id == coverage[0].Entry.ID {
			t.Fatal("private source without consent")
		}
	}
	// Revocation invalidates queued work before any provider call.
	if _, err := f.db.Pool().Exec(ctx, `UPDATE subscriptions SET muted=true WHERE user_id=$1 AND feed_id=$2`, f.a.UserID, coverage[1].Entry.FeedID); err != nil {
		t.Fatal(err)
	}
	if n, err := f.s.RunFrontPageJobs(ctx, 1); err != nil || n != 1 {
		t.Fatalf("worker %d %v", n, err)
	}
	if len(p.payloads()) != 0 {
		t.Fatal("stale selected sources reached provider")
	}
}

func TestFrontPageLateEnqueueDoesNotRegenerate(t *testing.T) {
	f := newCoreFixture(t, storetest.NewTemplate(t))
	body := "Beacon launched today."
	id := f.ingest(t, body)
	p := &generatedProvider{outputs: map[string]string{"edition": groundedOutput(id, coreHash("body"+body), "Beacon launched.", body)}}
	ctx := generatedContext(t, f, p, []llm.Task{llm.TaskEdition}, 0)
	if _, err := f.s.FrontPage(ctx, f.a, EditionDaily); err != nil {
		t.Fatal(err)
	}
	var payload []byte
	var key string
	if err := f.db.Pool().QueryRow(ctx, `SELECT payload,unique_key FROM jobs WHERE kind='front_summary'`).Scan(&payload, &key); err != nil {
		t.Fatal(err)
	}
	if _, err := f.s.RunFrontPageJobs(ctx, 1); err != nil {
		t.Fatal(err)
	}
	calls := len(p.payloads())
	if _, err := f.db.Pool().Exec(ctx, `INSERT INTO jobs(kind,payload,unique_key,run_at) VALUES('front_summary',$1,$2,$3)`, payload, key, f.s.now()); err != nil {
		t.Fatal(err)
	}
	if _, err := f.s.RunFrontPageJobs(ctx, 1); err != nil {
		t.Fatal(err)
	}
	if len(p.payloads()) != calls {
		t.Fatal("late cache miss paid twice")
	}
}
