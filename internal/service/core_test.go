package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/L-K-M/Earnest/internal/store"
	"github.com/L-K-M/Earnest/internal/store/storetest"
	"github.com/L-K-M/Earnest/internal/syncmodel"
	"github.com/jackc/pgx/v5"
)

type coreFixture struct {
	s                  *Service
	db                 *store.Store
	a                  Actor
	feed, subscription int64
	clock              atomic.Int64
}

func newCoreFixture(t *testing.T, template *storetest.Template) *coreFixture {
	t.Helper()
	f := &coreFixture{db: template.New(t)}
	f.clock.Store(time.Date(2026, 11, 2, 12, 0, 0, 0, time.UTC).UnixNano())
	var err error
	f.s, err = New(f.db, Deps{Clock: func() time.Time { return time.Unix(0, f.clock.Load()).UTC() }, SecretKey: []byte(strings.Repeat("s", 32))})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err := f.db.Pool().QueryRow(ctx, "INSERT INTO users(username,password_hash) VALUES('human','$argon2id$test') RETURNING id").Scan(&f.a.UserID); err != nil {
		t.Fatal(err)
	}
	f.a.Kind = PrincipalHuman
	f.a.Via = CredentialSession
	f.a.Scopes = HumanScopes
	if err := f.db.Pool().QueryRow(ctx, "INSERT INTO principals(user_id,kind,name) VALUES($1,'human','Human') RETURNING id", f.a.UserID).Scan(&f.a.PrincipalID); err != nil {
		t.Fatal(err)
	}
	if err := f.db.Pool().QueryRow(ctx, "INSERT INTO feeds(url) VALUES('https://example.test/feed') RETURNING id").Scan(&f.feed); err != nil {
		t.Fatal(err)
	}
	if err := f.db.Pool().QueryRow(ctx, "INSERT INTO subscriptions(user_id,feed_id) VALUES($1,$2) RETURNING id", f.a.UserID, f.feed).Scan(&f.subscription); err != nil {
		t.Fatal(err)
	}
	return f
}
func coreHash(s string) string { h := sha256.Sum256([]byte(s)); return hex.EncodeToString(h[:]) }
func coreItem(s string) IngestItem {
	return IngestItem{IdentityHash: coreHash(s), ContentHash: coreHash("body" + s), Title: s, ContentHTML: "<p>" + s + "</p>", ContentText: s, SanitizerVersion: 1, ContentSource: ContentFromFeed}
}
func (f *coreFixture) ingest(t *testing.T, name string) int64 {
	t.Helper()
	if _, err := f.s.RecordFetch(context.Background(), FetchOutcome{FeedID: f.feed, Status: FetchOK, Items: []IngestItem{coreItem(name)}}); err != nil {
		t.Fatal(err)
	}
	var id int64
	if err := f.db.Pool().QueryRow(context.Background(), "SELECT id FROM entries WHERE feed_id=$1 AND identity_hash=$2", f.feed, mustCoreHash(t, coreHash(name))).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}
func mustCoreHash(t *testing.T, s string) []byte {
	t.Helper()
	b, err := hex.DecodeString(s)
	if err != nil {
		t.Fatal(err)
	}
	return b
}
func (f *coreFixture) write(t *testing.T, id int64, op string, register syncmodel.RegisterName, value syncmodel.Value) syncmodel.Write {
	t.Helper()
	generation, err := f.db.Generation(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return syncmodel.Write{OpID: op, Principal: syncmodel.ID(f.a.PrincipalID), EntryID: syncmodel.ID(id), Register: register, Value: value, Base: syncmodel.Revision{Generation: generation}, OccurredAt: f.s.now(), Kind: syncmodel.Explicit}
}
func coreCheckGapless(t *testing.T, db *store.Store, user int64) {
	t.Helper()
	var counter, count, maxRev int64
	if err := db.Pool().QueryRow(context.Background(), `SELECT coalesce((SELECT value FROM rev_counters WHERE user_id=$1),0),count(*),coalesce(max(rev),0) FROM changes WHERE user_id=$1`, user).Scan(&counter, &count, &maxRev); err != nil {
		t.Fatal(err)
	}
	if count != counter || count != maxRev {
		t.Fatalf("not gapless: counter=%d count=%d max=%d", counter, count, maxRev)
	}
}

func TestCoreIngest(t *testing.T) {
	template := storetest.NewTemplate(t)
	t.Run("identity_versions_health_and_lease", func(t *testing.T) {
		f := newCoreFixture(t, template)
		ctx := context.Background()
		due, err := f.s.ClaimDueFeeds(ctx, f.s.now().Add(time.Hour*365*24), 1, time.Minute)
		if err != nil || len(due) != 1 {
			t.Fatalf("claim: %v %v", due, err)
		}
		again, err := f.s.ClaimDueFeeds(ctx, f.s.now().Add(time.Hour*365*24), 1, time.Minute)
		if err != nil || len(again) != 0 {
			t.Fatalf("lease reused: %v %v", again, err)
		}
		item := coreItem("first")
		summary, err := f.s.RecordFetch(ctx, FetchOutcome{FeedID: f.feed, Status: FetchOK, Items: []IngestItem{item}, ETag: "v1", HTTPStatus: 200, Feed: &FeedMeta{Title: "Feed"}})
		if err != nil || summary.Created != 1 {
			t.Fatalf("ingest: %+v %v", summary, err)
		}
		summary, err = f.s.RecordFetch(ctx, FetchOutcome{FeedID: f.feed, Status: FetchOK, Items: []IngestItem{item}, ETag: "v1"})
		if err != nil || summary != (IngestSummary{}) {
			t.Fatalf("repeat: %+v %v", summary, err)
		}
		item.ContentHash = coreHash("new body")
		item.ContentHTML = "<p>changed</p>"
		summary, err = f.s.RecordFetch(ctx, FetchOutcome{FeedID: f.feed, Status: FetchOK, Items: []IngestItem{item}})
		if err != nil || summary.Updated != 1 {
			t.Fatalf("update: %+v %v", summary, err)
		}
		var versions int
		if err := f.db.Pool().QueryRow(ctx, "SELECT count(*) FROM contents").Scan(&versions); err != nil || versions != 2 {
			t.Fatalf("versions %d %v", versions, err)
		}
		known, err := f.s.KnownIdentities(ctx, f.feed, []string{item.IdentityHash, coreHash("absent")})
		if err != nil || len(known) != 1 || !known[item.IdentityHash] {
			t.Fatalf("identities %v %v", known, err)
		}
		if _, err := f.s.RecordFetch(ctx, FetchOutcome{FeedID: f.feed, Status: FetchFailed, ErrorClass: "rate_limit", RetryAfter: 2 * time.Hour, HTTPStatus: 429}); err != nil {
			t.Fatal(err)
		}
		var failures int
		var next time.Time
		if err := f.db.Pool().QueryRow(ctx, "SELECT consecutive_failures,next_fetch_at FROM feeds WHERE id=$1", f.feed).Scan(&failures, &next); err != nil || failures != 1 || next.Before(f.s.now().Add(2*time.Hour)) {
			t.Fatalf("backoff %d %v %v", failures, next, err)
		}
		if _, err := f.s.RecordFetch(ctx, FetchOutcome{FeedID: f.feed, Status: FetchNotModified, ETag: "not-modified"}); err != nil {
			t.Fatal(err)
		}
		if err := f.db.Pool().QueryRow(ctx, "SELECT consecutive_failures FROM feeds WHERE id=$1", f.feed).Scan(&failures); err != nil || failures != 0 {
			t.Fatal("health did not reset", err)
		}
		coreCheckGapless(t, f.db, f.a.UserID)
	})
	t.Run("invalid_and_deleted_never_resurrect", func(t *testing.T) {
		f := newCoreFixture(t, template)
		id := f.ingest(t, "deleted")
		if _, err := f.db.Pool().Exec(context.Background(), "UPDATE entries SET deleted_at=$2 WHERE id=$1", id, f.s.now()); err != nil {
			t.Fatal(err)
		}
		summary, err := f.s.RecordFetch(context.Background(), FetchOutcome{FeedID: f.feed, Status: FetchOK, Items: []IngestItem{coreItem("deleted")}})
		if err != nil || summary != (IngestSummary{}) {
			t.Fatal(summary, err)
		}
		for _, o := range []FetchOutcome{{FeedID: f.feed, Status: "bogus"}, {FeedID: f.feed, Status: FetchFailed, ErrorClass: "bogus"}, {FeedID: f.feed, Status: FetchOK, Items: []IngestItem{{IdentityHash: "bad"}}}} {
			if _, err := f.s.RecordFetch(context.Background(), o); !errors.Is(err, ErrInvalid) {
				t.Fatalf("invalid fetch accepted: %v", err)
			}
		}
	})
	t.Run("shared_concurrent_gapless", func(t *testing.T) {
		f := newCoreFixture(t, template)
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		var user int64
		if err := f.db.Pool().QueryRow(ctx, "INSERT INTO users(username,password_hash) VALUES('other','$argon2id$test') RETURNING id").Scan(&user); err != nil {
			t.Fatal(err)
		}
		if _, err := f.db.Pool().Exec(ctx, "INSERT INTO subscriptions(user_id,feed_id) VALUES($1,$2)", user, f.feed); err != nil {
			t.Fatal(err)
		}
		const n = 20
		start := make(chan struct{})
		failures := make(chan error, n)
		var wg sync.WaitGroup
		for i := range n {
			wg.Go(func() {
				<-start
				_, err := f.s.RecordFetch(ctx, FetchOutcome{FeedID: f.feed, Status: FetchOK, Items: []IngestItem{coreItem(fmt.Sprintf("item-%d", i))}})
				failures <- err
			})
		}
		close(start)
		wg.Wait()
		close(failures)
		for err := range failures {
			if err != nil {
				t.Fatal(err)
			}
		}
		coreCheckGapless(t, f.db, f.a.UserID)
		coreCheckGapless(t, f.db, user)
		var mismatches int
		if err := f.db.Pool().QueryRow(ctx, `SELECT count(*) FROM entry_revisions a JOIN entry_revisions b ON a.entry_id=b.entry_id WHERE a.user_id=$1 AND b.user_id=$2 AND a.rev<>b.rev`, f.a.UserID, user).Scan(&mismatches); err != nil || mismatches != 0 {
			t.Fatal("subscriber order differs", err)
		}
	})
	t.Run("slow_commit_never_exposes_higher_seq_first", func(t *testing.T) {
		f := newCoreFixture(t, template)
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		// A test-only trigger pauses the real service after its counter lock is held.
		if _, err := f.db.Pool().Exec(ctx, `CREATE FUNCTION pause_ingest() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.identity_hash=decode('`+coreHash("slow")+`','hex') THEN PERFORM pg_advisory_xact_lock(5433601); END IF; RETURN NEW; END $$;
  CREATE TRIGGER pause_ingest BEFORE INSERT ON entries FOR EACH ROW EXECUTE FUNCTION pause_ingest()`); err != nil {
			t.Fatal(err)
		}
		gate, err := f.db.Pool().Acquire(ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer gate.Release()
		if _, err := gate.Exec(ctx, "SELECT pg_advisory_lock(5433601)"); err != nil {
			t.Fatal(err)
		}
		defer gate.Exec(context.Background(), "SELECT pg_advisory_unlock(5433601)")
		slow := make(chan error, 1)
		go func() {
			_, err := f.s.RecordFetch(ctx, FetchOutcome{FeedID: f.feed, Status: FetchOK, Items: []IngestItem{coreItem("slow")}})
			slow <- err
		}()
		ticker := time.NewTicker(time.Millisecond)
		defer ticker.Stop()
		for {
			var waiting bool
			if err := f.db.Pool().QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM pg_locks WHERE locktype='advisory' AND objid=5433601 AND NOT granted)").Scan(&waiting); err != nil {
				t.Fatal(err)
			}
			if waiting {
				break
			}
			select {
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			case <-ticker.C:
			}
		}
		fast := make(chan error, 1)
		go func() {
			_, err := f.s.RecordFetch(ctx, FetchOutcome{FeedID: f.feed, Status: FetchOK, Items: []IngestItem{coreItem("fast")}})
			fast <- err
		}()
		var visible int
		if err := f.db.Pool().QueryRow(ctx, "SELECT count(*) FROM entries").Scan(&visible); err != nil || visible != 0 {
			t.Fatalf("uncommitted order leaked: %d %v", visible, err)
		}
		if _, err := gate.Exec(ctx, "SELECT pg_advisory_unlock(5433601)"); err != nil {
			t.Fatal(err)
		}
		if err := <-slow; err != nil {
			t.Fatal(err)
		}
		if err := <-fast; err != nil {
			t.Fatal(err)
		}
		var first, second uint64
		if err := f.db.Pool().QueryRow(ctx, "SELECT seq FROM entries WHERE title='slow'").Scan(&first); err != nil {
			t.Fatal(err)
		}
		if err := f.db.Pool().QueryRow(ctx, "SELECT seq FROM entries WHERE title='fast'").Scan(&second); err != nil {
			t.Fatal(err)
		}
		if first != 1 || second != 2 {
			t.Fatalf("commit order: slow=%d fast=%d", first, second)
		}
		coreCheckGapless(t, f.db, f.a.UserID)
	})
}

func TestCoreState(t *testing.T) {
	template := storetest.NewTemplate(t)
	t.Run("batch_atomic_replay_provenance_and_done_clear", func(t *testing.T) {
		f := newCoreFixture(t, template)
		id := f.ingest(t, "article")
		done := f.write(t, id, "done", syncmodel.Shelf, syncmodel.Done)
		if _, err := f.s.Batch(context.Background(), f.a, []syncmodel.Write{done}); err != nil {
			t.Fatal(err)
		}
		unread := f.write(t, id, "unread", syncmodel.Seen, syncmodel.Unseen)
		shelf := f.write(t, id, "again-done", syncmodel.Shelf, syncmodel.Done)
		results, err := f.s.Batch(context.Background(), f.a, []syncmodel.Write{unread, shelf})
		if err != nil || len(results) != 2 || results[1].State.Shelf.Value != syncmodel.Done || results[0].State.Shelf.Value != syncmodel.Done {
			t.Fatalf("causal batch: %+v %v", results, err)
		}
		if results[0].State.Seen.Rev != results[1].State.Shelf.Rev {
			t.Fatal("batch has multiple revisions")
		}
		bad := f.write(t, id, "invalid", syncmodel.Seen, syncmodel.True)
		good := f.write(t, id, "not-committed", syncmodel.Kept, syncmodel.True)
		if _, err := f.s.Batch(context.Background(), f.a, []syncmodel.Write{good, bad}); !errors.Is(err, ErrInvalid) {
			t.Fatal("invalid batch accepted", err)
		}
		state, err := store.LoadState(context.Background(), f.db.Pool(), f.a.PrincipalID, id)
		if err != nil || state.Kept.Value != syncmodel.False {
			t.Fatal("partial batch committed", err)
		}
		replay, err := f.s.Batch(context.Background(), f.a, []syncmodel.Write{unread})
		if err != nil || !replay[0].Replay || replay[0].State.Shelf.Value != syncmodel.Done {
			t.Fatal("replay reran policy", replay, err)
		}
		if _, err := f.s.Batch(context.Background(), f.a, []syncmodel.Write{unread, unread}); !errors.Is(err, ErrInvalid) {
			t.Fatal("duplicate accepted", err)
		}
		coreCheckGapless(t, f.db, f.a.UserID)
	})
	t.Run("agent_activity_target_and_scope", func(t *testing.T) {
		f := newCoreFixture(t, template)
		id := f.ingest(t, "entry")
		agent := Actor{UserID: f.a.UserID, Kind: PrincipalAgent, Via: CredentialAgentToken, Scopes: []Scope{ScopeEntriesRead, ScopeStateWriteHuman}}
		if err := f.db.Pool().QueryRow(context.Background(), "INSERT INTO principals(user_id,kind,name) VALUES($1,'agent','Bot') RETURNING id", f.a.UserID).Scan(&agent.PrincipalID); err != nil {
			t.Fatal(err)
		}
		w := f.write(t, id, "agent", syncmodel.Shelf, syncmodel.Done)
		r, err := f.s.Batch(context.Background(), agent, []syncmodel.Write{w})
		if err != nil || r[0].State.Shelf.Source != syncmodel.SourceRuleOrAgent || int64(r[0].State.Shelf.Actor) != agent.PrincipalID {
			t.Fatal("agent provenance", r, err)
		}
		var count int
		if err := f.db.Pool().QueryRow(context.Background(), "SELECT count(*) FROM activity WHERE principal_id=$1 AND before IS NOT NULL AND after IS NOT NULL", agent.PrincipalID).Scan(&count); err != nil || count != 1 {
			t.Fatal("missing activity", count, err)
		}
		agent.Scopes = []Scope{ScopeEntriesRead}
		if _, err := f.s.Batch(context.Background(), agent, []syncmodel.Write{w}); !errors.Is(err, ErrForbidden) {
			t.Fatal("scope bypass", err)
		}
	})
	t.Run("ranges_expiry_generation_and_google", func(t *testing.T) {
		f := newCoreFixture(t, template)
		id := f.ingest(t, "first")
		second := f.ingest(t, "second")
		w := f.write(t, id, "unread", syncmodel.Seen, syncmodel.Unseen)
		if _, err := f.s.Batch(context.Background(), f.a, []syncmodel.Write{w}); err != nil {
			t.Fatal(err)
		}
		through := f.write(t, 0, "range", syncmodel.Seen, syncmodel.SeenValue)
		through.OccurredAt = through.OccurredAt.Add(-time.Hour)
		rr, err := f.s.Through(context.Background(), f.a, ThroughRequest{Write: through, Scope: syncmodel.Scope{Kind: syncmodel.ScopeAll}})
		if err != nil || rr.ThroughSeq != 2 || len(rr.Except) != 1 || int64(rr.Except[0]) != id || rr.Results[1].Status != syncmodel.Applied {
			t.Fatal("range", rr, err)
		}
		f.ingest(t, "late")
		replay, err := f.s.Through(context.Background(), f.a, ThroughRequest{Write: through, Scope: syncmodel.Scope{Kind: syncmodel.ScopeAll}})
		if err != nil || !replay.Replay || replay.ThroughSeq != 2 || len(replay.Results) != 2 {
			t.Fatal("range replay changed membership", replay, err)
		}
		expired := f.write(t, second, "expired", syncmodel.Kept, syncmodel.True)
		expired.OccurredAt = expired.OccurredAt.Add(-syncmodel.MaxQueueAge - time.Nanosecond)
		r, err := f.s.Batch(context.Background(), f.a, []syncmodel.Write{expired})
		if err != nil || r[0].Status != syncmodel.Expired {
			t.Fatal("not expired", r, err)
		}
		gone := expired
		gone.EntryID = 9999
		gone.OpID = "gone"
		r, err = f.s.Batch(context.Background(), f.a, []syncmodel.Write{gone})
		if err != nil || r[0].Status != syncmodel.Gone {
			t.Fatal("gone must beat expiry", r, err)
		}
		compat := f.a
		compat.Via = CredentialCompat
		r, err = f.s.GoogleWrite(context.Background(), compat, syncmodel.GoogleStar, []int64{id}, "google")
		if err != nil || r[0].State.Kept.Value != syncmodel.True {
			t.Fatal("google mapping", r, err)
		}
		old := f.write(t, id, "old-generation", syncmodel.Kept, syncmodel.False)
		if _, err := f.db.RotateGeneration(context.Background()); err != nil {
			t.Fatal(err)
		}
		if _, err := f.s.Batch(context.Background(), f.a, []syncmodel.Write{old}); !errors.Is(err, ErrGone) {
			t.Fatal("old generation accepted", err)
		}
		coreCheckGapless(t, f.db, f.a.UserID)
	})
	t.Run("cluster_late_member", func(t *testing.T) {
		f := newCoreFixture(t, template)
		id := f.ingest(t, "first")
		second := f.ingest(t, "late")
		var cluster int64
		if err := f.db.Pool().QueryRow(context.Background(), "INSERT INTO clusters(user_id,rev) VALUES($1,1) RETURNING id", f.a.UserID).Scan(&cluster); err != nil {
			t.Fatal(err)
		}
		if _, err := f.db.Pool().Exec(context.Background(), "INSERT INTO entry_clusters(user_id,cluster_id,entry_id,rev) VALUES($1,$2,$3,1)", f.a.UserID, cluster, id); err != nil {
			t.Fatal(err)
		}
		r, err := f.s.Cluster(context.Background(), f.a, cluster, f.write(t, 0, "cluster", syncmodel.Seen, syncmodel.SeenValue))
		if err != nil || len(r) != 1 {
			t.Fatal(r, err)
		}
		if _, err := f.db.Pool().Exec(context.Background(), "INSERT INTO entry_clusters(user_id,cluster_id,entry_id,rev) VALUES($1,$2,$3,1)", f.a.UserID, cluster, second); err != nil {
			t.Fatal(err)
		}
		state, err := store.LoadState(context.Background(), f.db.Pool(), f.a.PrincipalID, second)
		if err != nil || !state.Unread() {
			t.Fatal("late cluster member changed", state, err)
		}
		replayed, err := f.s.Cluster(context.Background(), f.a, cluster, f.write(t, 0, "cluster", syncmodel.Seen, syncmodel.SeenValue))
		if err != nil || len(replayed) != 1 || !replayed[0].Replay {
			t.Fatal("cluster replay changed membership", replayed, err)
		}
		state, err = store.LoadState(context.Background(), f.db.Pool(), f.a.PrincipalID, second)
		if err != nil || !state.Unread() {
			t.Fatal("cluster retry included late member", state, err)
		}
	})
}

func TestFetchBackoff(t *testing.T) {
	for _, tc := range []struct {
		class       string
		failures    int
		retry, want time.Duration
	}{
		{"network", 0, 0, 15 * time.Minute}, {"network", 1, 0, 30 * time.Minute}, {"network", 100, 0, 24 * time.Hour}, {"gone", 0, 0, 7 * 24 * time.Hour}, {"parse", 0, 0, time.Hour}, {"challenge", 0, 0, 24 * time.Hour}, {"rate_limit", 0, 2 * time.Hour, 2 * time.Hour},
	} {
		t.Run(fmt.Sprintf("%s-%d", tc.class, tc.failures), func(t *testing.T) {
			if got := fetchBackoff(tc.class, tc.failures, tc.retry); got != tc.want {
				t.Fatalf("got %v want %v", got, tc.want)
			}
		})
	}
}

// Keep this compile assertion close to the tests that depend on the transaction API.
var _ store.StateReader = (pgx.Tx)(nil)

func TestCredentialEnvelope(t *testing.T) {
	for _, tc := range []struct {
		name string
		db   *store.Store
		key  []byte
	}{
		{"nil_store", nil, []byte(strings.Repeat("k", 32))}, {"short_key", &store.Store{}, []byte("short")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := New(tc.db, Deps{SecretKey: tc.key}); !errors.Is(err, ErrInvalid) {
				t.Fatal("invalid dependencies accepted", err)
			}
		})
	}
	s, err := New(&store.Store{}, Deps{SecretKey: []byte(strings.Repeat("k", 32))})
	if err != nil {
		t.Fatal(err)
	}
	want := &FeedCredentials{Username: "reader", Password: "secret", Headers: map[string]string{"Authorization": "opaque"}}
	one, err := s.sealCredentials(want)
	if err != nil {
		t.Fatal(err)
	}
	two, err := s.sealCredentials(want)
	if err != nil {
		t.Fatal(err)
	}
	if string(one) == string(two) || strings.Contains(string(one), want.Password) {
		t.Fatal("credentials are not randomly encrypted")
	}
	got, err := s.openCredentials(one)
	if err != nil || got.Password != want.Password || got.Headers["Authorization"] != "opaque" {
		t.Fatal("credential roundtrip", got, err)
	}
	one[len(one)-1] ^= 1
	if _, err := s.openCredentials(one); !errors.Is(err, ErrUnavailable) {
		t.Fatal("tampered envelope accepted", err)
	}
}
