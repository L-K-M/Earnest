package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"testing"
	"time"

	"github.com/L-K-M/Earnest/internal/store"
	"github.com/L-K-M/Earnest/internal/store/storetest"
	"github.com/L-K-M/Earnest/internal/syncmodel"
)

func TestSubscribeCredentialOrigin(t *testing.T) {
	template := storetest.NewTemplate(t)
	for _, tc := range []struct {
		name, resolved   string
		private, allowed bool
	}{
		{"cross_host", "https://other.test/rss", true, false},
		{"cross_scheme", "http://origin.test/rss", true, false},
		{"cross_port", "https://origin.test:8443/rss", true, false},
		{"same_origin", "https://origin.test/discovered", true, true},
		{"default_port", "https://ORIGIN.test:443/discovered", true, true},
		{"public_discovery", "https://other.test/rss", false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newCoreFixture(t, template)
			f.s.prober = &subscriptionsProber{probe: func(context.Context, string, *FeedCredentials) (string, FeedMeta, []IngestItem, error) {
				return tc.resolved, FeedMeta{}, nil, nil
			}}
			opts := SubscribeOptions{OpID: "subscribe"}
			if tc.private {
				opts.Credentials = &FeedCredentials{Username: "reader", Password: "secret"}
			}
			_, err := f.s.Subscribe(context.Background(), f.a, "https://origin.test/start", opts)
			if tc.allowed {
				if err != nil {
					t.Fatal(err)
				}
				return
			}
			if !errors.Is(err, ErrInvalid) {
				t.Fatalf("cross-origin credentials persisted: %v", err)
			}
			var n int
			if err := f.db.Pool().QueryRow(context.Background(), "SELECT count(*) FROM feeds WHERE credentials_encrypted IS NOT NULL").Scan(&n); err != nil || n != 0 {
				t.Fatalf("saved credentials: %d %v", n, err)
			}
		})
	}
}

func TestSubscribeSharedEntryBackfill(t *testing.T) {
	template := storetest.NewTemplate(t)
	for _, mode := range []string{"subscribe", "rebind", "resubscribe", "tombstone"} {
		t.Run(mode, func(t *testing.T) {
			f := newCoreFixture(t, template)
			ctx := context.Background()
			oldID := f.ingest(t, "old-shared")
			kind := syncmodel.EntryCreated
			if mode == "tombstone" {
				if _, err := f.db.Pool().Exec(ctx, "UPDATE entries SET deleted_at=$2 WHERE id=$1", oldID, f.s.now()); err != nil {
					t.Fatal(err)
				}
				kind = syncmodel.EntryDeleted
			}
			other := subscriptionsOther(t, f, "other")
			f.s.prober = &subscriptionsProber{}
			var sub Subscription
			var err error
			var savedStates []syncmodel.State
			if mode == "rebind" || mode == "resubscribe" {
				opts := SubscribeOptions{OpID: "initial"}
				if mode == "rebind" {
					opts.Credentials = &FeedCredentials{Username: "reader", Password: "secret"}
				}
				sub, err = f.s.Subscribe(ctx, other, "https://example.test/feed", opts)
				if err != nil {
					t.Fatal(err)
				}
				if mode == "resubscribe" {
					agent := entriesTestAgent(t, f.db, other.UserID, "Reader", ScopeStateWriteHuman)
					for _, actor := range []Actor{other, agent, f.a} {
						for _, action := range []struct {
							register syncmodel.RegisterName
							value    syncmodel.Value
						}{
							{syncmodel.Seen, syncmodel.SeenValue},
							{syncmodel.Shelf, syncmodel.Later},
							{syncmodel.Kept, syncmodel.True},
						} {
							w := f.write(t, oldID, "saved-"+string(action.register), action.register, action.value)
							w.Principal = syncmodel.ID(actor.PrincipalID)
							results, err := f.s.Batch(ctx, actor, []syncmodel.Write{w})
							if err != nil || len(results) != 1 || results[0].Status != syncmodel.Applied {
								t.Fatalf("seed %s: %+v %v", action.register, results, err)
							}
						}
						state, err := store.LoadState(ctx, f.db.Pool(), actor.PrincipalID, oldID)
						if err != nil {
							t.Fatal(err)
						}
						if actor.UserID == other.UserID {
							savedStates = append(savedStates, state)
						}
					}
					if err := f.s.Unsubscribe(ctx, other, sub.ID, "unsubscribe"); err != nil {
						t.Fatal(err)
					}
				}
			}
			cursor, err := f.s.BootstrapCursor(ctx, other)
			if err != nil {
				t.Fatal(err)
			}
			client := syncmodel.NewClient(cursor)
			for _, resource := range []BootstrapKind{BootstrapEntries, BootstrapState} {
				page, err := f.s.Bootstrap(ctx, other, BootstrapRequest{Kind: resource})
				if err != nil || len(page.Items) != 0 {
					t.Fatalf("unsubscribed bootstrap %s: %+v %v", resource, page, err)
				}
			}
			// A bootstrap already passed this ID before the shared feed became visible.
			if mode == "rebind" {
				sub, err = f.s.UpdateSubscription(ctx, other, sub.ID, UpdateSubscriptionOptions{OpID: "rebind", CredentialsSet: true})
			} else {
				sub, err = f.s.Subscribe(ctx, other, "https://example.test/feed", SubscribeOptions{OpID: "subscribe"})
			}
			if err != nil {
				t.Fatal(err)
			}
			page, err := f.s.Bootstrap(ctx, other, BootstrapRequest{Kind: BootstrapEntries, AfterID: oldID})
			if err != nil || len(page.Items) != 0 {
				t.Fatalf("later page: %+v %v", page, err)
			}
			changes, err := f.s.Changes(ctx, other, cursor, 100)
			if err != nil {
				t.Fatal(err)
			}
			found := false
			for _, c := range changes.Changes {
				if c.Type != kind {
					continue
				}
				var body struct {
					Entry syncmodel.Entry `json:"entry"`
				}
				if err := json.Unmarshal(c.Payload, &body); err != nil {
					t.Fatal(err)
				}
				if body.Entry.ID == syncmodel.ID(oldID) && body.Entry.SubscriptionID == syncmodel.ID(sub.ID) && body.Entry.Rev == c.Rev {
					found = true
				}
			}
			if !found {
				t.Fatalf("entry %d missing from bootstrap replay: %+v", oldID, changes.Changes)
			}
			var replay []syncmodel.Change
			var backfilled []syncmodel.State
			for _, record := range changes.Changes {
				change := syncmodel.Change{Rev: record.Rev, Type: record.Type}
				if err := json.Unmarshal(record.Payload, &change); err != nil {
					t.Fatal(err)
				}
				for _, result := range change.Results {
					backfilled = append(backfilled, result.State)
				}
				replay = append(replay, change)
			}
			for range 2 {
				if err := client.ApplyPage(replay, changes.Next); err != nil {
					t.Fatal(err)
				}
			}
			for _, saved := range savedStates {
				server, err := store.LoadState(ctx, f.db.Pool(), int64(saved.Principal), oldID)
				if err != nil || server != saved {
					t.Fatalf("backfill changed saved state: got %+v, want %+v: %v", server, saved, err)
				}
				if got := client.State(saved.Principal, saved.EntryID); got != server {
					t.Errorf("replayed state: got %+v, want server %+v", got, server)
				}

				// A newer seen register cannot block older shelf/kept snapshots.
				ahead := syncmodel.NewClient(cursor)
				newer := syncmodel.InitialState(saved.Principal, saved.EntryID)
				newer.Seen = saved.Seen
				newer.Seen.Value = syncmodel.Unseen
				newer.Seen.Rev = changes.Next.Rev + 1
				newer.StateRev = newer.Seen.Rev
				ahead.MergeState(newer)
				if err := ahead.ApplyPage(replay, changes.Next); err != nil {
					t.Fatal(err)
				}
				want := saved
				want.Seen, want.StateRev = newer.Seen, newer.StateRev
				if got := ahead.State(saved.Principal, saved.EntryID); got != want {
					t.Errorf("register merge: got %+v, want %+v", got, want)
				}
			}
			if len(backfilled) != len(savedStates) {
				t.Fatalf("backfilled %d states, want %d for this user's principals", len(backfilled), len(savedStates))
			}
			for i, state := range backfilled {
				if state != savedStates[i] {
					t.Fatalf("backfilled state: got %+v, want %+v", state, savedStates[i])
				}
			}
			if mode == "resubscribe" {
				for _, opID := range []string{"subscribe", "subscribe-again"} {
					repeated, err := f.s.Subscribe(ctx, other, "https://example.test/feed", SubscribeOptions{OpID: opID})
					if err != nil || repeated.ID != sub.ID || repeated.Rev != sub.Rev {
						t.Fatalf("repeated subscribe: %+v %v", repeated, err)
					}
				}
				current, err := f.s.BootstrapCursor(ctx, other)
				if err != nil || current != changes.Next {
					t.Fatalf("repeated subscribe emitted changes: %+v %v", current, err)
				}
			}
			var stamped uint64
			if err := f.db.Pool().QueryRow(ctx, "SELECT rev FROM entry_revisions WHERE user_id=$1 AND entry_id=$2", other.UserID, oldID).Scan(&stamped); err != nil {
				t.Fatal(err)
			}
			page, err = f.s.Bootstrap(ctx, other, BootstrapRequest{Kind: BootstrapEntries})
			if err != nil || len(page.Items) != 1 {
				t.Fatalf("backfill bootstrap: %+v %v", page, err)
			}
			var entry struct {
				Rev uint64 `json:"rev"`
			}
			if err := json.Unmarshal(page.Items[0], &entry); err != nil || entry.Rev != stamped {
				t.Fatalf("backfill stamp mismatch: %+v %d %v", entry, stamped, err)
			}
			coreCheckGapless(t, f.db, other.UserID)
		})
	}
}

func TestSubscriptionStateBackfillRollback(t *testing.T) {
	f := newCoreFixture(t, storetest.NewTemplate(t))
	ctx := context.Background()
	id := f.ingest(t, "saved")
	results, err := f.s.Batch(ctx, f.a, []syncmodel.Write{f.write(t, id, "seen", syncmodel.Seen, syncmodel.SeenValue)})
	if err != nil || len(results) != 1 || results[0].Status != syncmodel.Applied {
		t.Fatalf("seed state: %+v %v", results, err)
	}
	saved := results[0].State
	if err := f.s.Unsubscribe(ctx, f.a, f.subscription, "unsubscribe"); err != nil {
		t.Fatal(err)
	}
	cursor, err := f.s.BootstrapCursor(ctx, f.a)
	if err != nil {
		t.Fatal(err)
	}
	var entryRev uint64
	if err := f.db.Pool().QueryRow(ctx, "SELECT rev FROM entry_revisions WHERE user_id=$1 AND entry_id=$2", f.a.UserID, id).Scan(&entryRev); err != nil {
		t.Fatal(err)
	}
	// Reject only new state events, after the entry backfill has been staged.
	if _, err := f.db.Pool().Exec(ctx, "ALTER TABLE changes ADD CONSTRAINT reject_state_backfill CHECK (type <> 'state.updated') NOT VALID"); err != nil {
		t.Fatal(err)
	}
	f.s.prober = &subscriptionsProber{}
	options := SubscribeOptions{OpID: "resubscribe"}
	if _, err := f.s.Subscribe(ctx, f.a, "https://example.test/feed", options); err == nil {
		t.Fatal("subscription committed without state backfill")
	}
	current, err := f.s.BootstrapCursor(ctx, f.a)
	if err != nil || current != cursor {
		t.Fatalf("failed backfill changed cursor: %+v, want %+v: %v", current, cursor, err)
	}
	subs, err := f.s.Subscriptions(ctx, f.a)
	if err != nil || len(subs) != 0 {
		t.Fatalf("failed backfill left subscription: %+v %v", subs, err)
	}
	var currentEntryRev uint64
	if err := f.db.Pool().QueryRow(ctx, "SELECT rev FROM entry_revisions WHERE user_id=$1 AND entry_id=$2", f.a.UserID, id).Scan(&currentEntryRev); err != nil || currentEntryRev != entryRev {
		t.Fatalf("failed backfill changed entry revision: %d, want %d: %v", currentEntryRev, entryRev, err)
	}
	if _, err := f.db.Pool().Exec(ctx, "ALTER TABLE changes DROP CONSTRAINT reject_state_backfill"); err != nil {
		t.Fatal(err)
	}
	if _, err := f.s.Subscribe(ctx, f.a, "https://example.test/feed", options); err != nil {
		t.Fatalf("retry after rolled-back backfill: %v", err)
	}
	page, err := f.s.Changes(ctx, f.a, cursor, 100)
	if err != nil {
		t.Fatal(err)
	}
	var snapshots []syncmodel.Result
	for _, record := range page.Changes {
		if record.Type != syncmodel.StateUpdated {
			continue
		}
		var change syncmodel.Change
		if err := json.Unmarshal(record.Payload, &change); err != nil {
			t.Fatal(err)
		}
		snapshots = append(snapshots, change.Results...)
	}
	if len(snapshots) != 1 || snapshots[0].State != saved {
		t.Fatalf("retry snapshots: %+v, want %+v", snapshots, saved)
	}
	coreCheckGapless(t, f.db, f.a.UserID)
}

func TestThroughCapturesReceiptBoundary(t *testing.T) {
	f := newCoreFixture(t, storetest.NewTemplate(t))
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	oldID := f.ingest(t, "before")
	w := f.write(t, 0, "range", syncmodel.Seen, syncmodel.SeenValue)
	tx, err := f.db.Pool().Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(context.Background())
	if err := store.LockCore(ctx, tx); err != nil {
		t.Fatal(err)
	}
	var newID int64
	if _, err := tx.Exec(ctx, "UPDATE seq_counter SET value=value+1 WHERE singleton"); err != nil {
		t.Fatal(err)
	}
	if err := tx.QueryRow(ctx, "INSERT INTO entries(feed_id,seq,identity_hash,first_seen_at) SELECT $1,value,$2,$3 FROM seq_counter WHERE singleton RETURNING id", f.feed, mustCoreHash(t, coreHash("after")), f.s.now()).Scan(&newID); err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	var result syncmodel.RangeResult
	var callErr error
	go func() {
		result, callErr = f.s.Through(ctx, f.a, ThroughRequest{Write: w, Scope: syncmodel.Scope{Kind: syncmodel.ScopeAll}})
		close(done)
	}()
	// Wait for the mutation to block on this transaction, not for a guessed delay.
	for {
		var waiting bool
		if err := f.db.Pool().QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE $1=ANY(pg_blocking_pids(pid)))", int32(tx.Conn().PgConn().PID())).Scan(&waiting); err != nil {
			t.Fatal(err)
		}
		if waiting {
			break
		}
		select {
		case <-done:
			t.Fatalf("range returned before commit: %v", callErr)
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		case <-time.After(5 * time.Millisecond):
		}
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	<-done
	if callErr != nil {
		t.Fatal(callErr)
	}
	if len(result.Results) != 1 || result.Results[0].State.EntryID != syncmodel.ID(oldID) {
		t.Fatalf("receipt range included later commit %d: %+v", newID, result)
	}
	state, err := store.LoadState(ctx, f.db.Pool(), f.a.PrincipalID, newID)
	if err != nil || !state.Unread() {
		t.Fatalf("later entry changed: %+v %v", state, err)
	}
	coreCheckGapless(t, f.db, f.a.UserID)
}

func TestFetchMetadataSubscriptionRevision(t *testing.T) {
	template := storetest.NewTemplate(t)
	for _, tc := range []struct {
		name        string
		override    bool
		title, site string
		wantChange  bool
	}{
		{"inherited_title", false, "After", "https://before.test", true},
		{"overridden_title", true, "After", "https://before.test", false},
		{"overridden_site", true, "Before", "https://after.test", true},
		{"unchanged", false, "Before", "https://before.test", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newCoreFixture(t, template)
			ctx := context.Background()
			f.ingest(t, "unchanged-entry")
			if _, err := f.db.Pool().Exec(ctx, "UPDATE feeds SET title='Before',site_url='https://before.test' WHERE id=$1", f.feed); err != nil {
				t.Fatal(err)
			}
			if tc.override {
				if _, err := f.db.Pool().Exec(ctx, "UPDATE subscriptions SET title_override='Custom' WHERE id=$1", f.subscription); err != nil {
					t.Fatal(err)
				}
			}
			before, err := f.s.Subscription(ctx, f.a, f.subscription)
			if err != nil {
				t.Fatal(err)
			}
			cursor, err := f.s.BootstrapCursor(ctx, f.a)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := f.s.RecordFetch(ctx, FetchOutcome{FeedID: f.feed, Status: FetchOK, Feed: &FeedMeta{Title: tc.title, SiteURL: tc.site}, Items: []IngestItem{coreItem("unchanged-entry")}}); err != nil {
				t.Fatal(err)
			}
			after, err := f.s.Subscription(ctx, f.a, f.subscription)
			if err != nil {
				t.Fatal(err)
			}
			changes, err := f.s.Changes(ctx, f.a, cursor, 100)
			if err != nil {
				t.Fatal(err)
			}
			if !tc.wantChange {
				if after.Rev != before.Rev || len(changes.Changes) != 0 {
					t.Fatalf("unchanged projection emitted updates: %+v", changes)
				}
				return
			}
			if after.Rev <= before.Rev {
				t.Fatalf("metadata changed at same revision: before=%+v after=%+v", before, after)
			}
			if len(changes.Changes) != 1 || changes.Changes[0].Type != syncmodel.SubscriptionUpdated {
				t.Fatalf("metadata events: %+v", changes)
			}
			var body struct {
				Subscription Subscription `json:"subscription"`
			}
			if err := json.Unmarshal(changes.Changes[0].Payload, &body); err != nil {
				t.Fatal(err)
			}
			if body.Subscription.Title != after.Title || body.Subscription.SiteURL != after.SiteURL || body.Subscription.Rev != after.Rev {
				t.Fatalf("stale metadata event: %+v", body)
			}
			coreCheckGapless(t, f.db, f.a.UserID)
		})
	}
}

func TestFetchMetadataSharedSubscribers(t *testing.T) {
	f := newCoreFixture(t, storetest.NewTemplate(t))
	ctx := context.Background()
	f.ingest(t, "existing")
	f.s.prober = &subscriptionsProber{}
	other := subscriptionsOther(t, f, "custom-title")
	sub, err := f.s.Subscribe(ctx, other, "https://example.test/feed", SubscribeOptions{Title: "Custom", OpID: "custom"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.Pool().Exec(ctx, "UPDATE subscriptions SET initial_fetch_pending=true WHERE id=$1", f.subscription); err != nil {
		t.Fatal(err)
	}
	cursors := map[int64]syncmodel.Revision{}
	for _, a := range []Actor{f.a, other} {
		cursor, err := f.s.BootstrapCursor(ctx, a)
		if err != nil {
			t.Fatal(err)
		}
		cursors[a.UserID] = cursor
	}
	if _, err := f.s.RecordFetch(ctx, FetchOutcome{FeedID: f.feed, Status: FetchOK, Feed: &FeedMeta{Title: "After"}, Items: []IngestItem{coreItem("existing"), coreItem("new")}}); err != nil {
		t.Fatal(err)
	}
	for _, a := range []Actor{f.a, other} {
		changes, err := f.s.Changes(ctx, a, cursors[a.UserID], 100)
		if err != nil {
			t.Fatal(err)
		}
		want := 1
		if a.UserID == f.a.UserID {
			want = 2
		}
		if len(changes.Changes) != want || changes.Changes[0].Type != syncmodel.EntryCreated {
			t.Fatalf("user %d events: %+v", a.UserID, changes)
		}
		if want == 2 && changes.Changes[1].Type != syncmodel.SubscriptionUpdated {
			t.Fatalf("cohort and metadata updates not combined: %+v", changes)
		}
		coreCheckGapless(t, f.db, a.UserID)
	}
	custom, err := f.s.Subscription(ctx, other, sub.ID)
	if err != nil || custom.Title != "Custom" || custom.Rev != sub.Rev {
		t.Fatalf("overridden title changed: %+v %v", custom, err)
	}
	inherited, err := f.s.Subscription(ctx, f.a, f.subscription)
	if err != nil || inherited.Title != "After" || inherited.InitialFetchPending {
		t.Fatalf("cohort metadata: %+v %v", inherited, err)
	}
}

func TestSubscriptionStateBackfillBatches(t *testing.T) {
	f := newCoreFixture(t, storetest.NewTemplate(t))
	ctx := t.Context()
	const entryCount = 501
	ids := make([]int64, entryCount)
	for i := range ids {
		ids[i] = f.ingest(t, fmt.Sprintf("saved-%d", i))
	}
	agent := entriesTestAgent(t, f.db, f.a.UserID, "Reader", ScopeStateWriteHuman)
	var expected []syncmodel.State
	for _, actor := range []Actor{f.a, agent} {
		// Seed saved snapshots directly so setup does not depend on batch limits.
		if _, err := f.db.Pool().Exec(ctx, `INSERT INTO entry_state(principal_id,entry_id,seen,seen_rev,seen_occurred_at,seen_actor,seen_source,state_rev)
            SELECT $1,id,'seen',1,$3,$1,'deliberate',1 FROM entries WHERE feed_id=$2`, actor.PrincipalID, f.feed, f.s.now()); err != nil {
			t.Fatal(err)
		}
		states, err := store.LoadStates(ctx, f.db.Pool(), actor.PrincipalID, ids)
		if err != nil {
			t.Fatal(err)
		}
		for _, id := range ids {
			expected = append(expected, states[id])
		}
	}
	if err := f.s.Unsubscribe(ctx, f.a, f.subscription, "unsubscribe"); err != nil {
		t.Fatal(err)
	}
	cursor, err := f.s.BootstrapCursor(ctx, f.a)
	if err != nil {
		t.Fatal(err)
	}
	f.s.prober = &subscriptionsProber{}
	if _, err := f.s.Subscribe(ctx, f.a, "https://example.test/feed", SubscribeOptions{OpID: "resubscribe"}); err != nil {
		t.Fatal(err)
	}
	rows, err := f.db.Pool().Query(ctx, `SELECT payload FROM changes WHERE user_id=$1 AND rev>$2 AND type='state.updated' ORDER BY rev`, f.a.UserID, cursor.Rev)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var got []syncmodel.State
	var sizes []int
	for rows.Next() {
		var payload []byte
		if err := rows.Scan(&payload); err != nil {
			t.Fatal(err)
		}
		var change syncmodel.Change
		if err := json.Unmarshal(payload, &change); err != nil {
			t.Fatal(err)
		}
		sizes = append(sizes, len(change.Results))
		for _, result := range change.Results {
			if result.Status != syncmodel.Applied {
				t.Fatalf("status = %s", result.Status)
			}
			got = append(got, result.State)
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(sizes, []int{500, 1, 500, 1}) {
		t.Errorf("backfill batch sizes = %v, want [500 1 500 1]", sizes)
	}
	if !reflect.DeepEqual(got, expected) {
		t.Fatal("backfill changed snapshot order or register stamps")
	}
	coreCheckGapless(t, f.db, f.a.UserID)
}
