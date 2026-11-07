package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"math"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/L-K-M/Earnest/internal/store"
	"github.com/L-K-M/Earnest/internal/store/storetest"
	"github.com/L-K-M/Earnest/internal/syncmodel"
)

type entriesTestFixture struct {
	s     *Service
	db    *store.Store
	a     Actor
	clock atomic.Int64
}

var entriesTestBase = time.Date(2026, 11, 2, 12, 0, 0, 0, time.UTC)

func entriesTestNew(t *testing.T, template *storetest.Template) *entriesTestFixture {
	t.Helper()
	f := &entriesTestFixture{db: template.New(t)}
	f.clock.Store(entriesTestBase.UnixNano())
	var err error
	f.s, err = New(f.db, Deps{Clock: func() time.Time { return time.Unix(0, f.clock.Load()).UTC() }, SecretKey: []byte(strings.Repeat("e", 32))})
	if err != nil {
		t.Fatal(err)
	}
	var user, human int64
	ctx := context.Background()
	if err := f.db.Pool().QueryRow(ctx, "INSERT INTO users(username,password_hash) VALUES('entriesTest','$argon2id$test') RETURNING id").Scan(&user); err != nil {
		t.Fatal(err)
	}
	if err := f.db.Pool().QueryRow(ctx, "INSERT INTO principals(user_id,kind,name) VALUES($1,'human','Me') RETURNING id", user).Scan(&human); err != nil {
		t.Fatal(err)
	}
	f.a = Actor{UserID: user, PrincipalID: human, Kind: PrincipalHuman, Via: CredentialSession, Scopes: HumanScopes}
	return f
}

func (f *entriesTestFixture) setClock(at time.Time) { f.clock.Store(at.UnixNano()) }
func (f *entriesTestFixture) now() time.Time        { return time.Unix(0, f.clock.Load()).UTC() }

func entriesTestHash(s string) string {
	h := sha256.Sum256([]byte("entriesTest:" + s))
	return hex.EncodeToString(h[:])
}
func entriesTestHashB(s string) []byte {
	b, _ := hex.DecodeString(entriesTestHash(s))
	return b
}

func entriesTestFeed(t *testing.T, db *store.Store, url string, owner int64) int64 {
	t.Helper()
	var id int64
	var err error
	if owner == 0 {
		err = db.Pool().QueryRow(context.Background(), "INSERT INTO feeds(url) VALUES($1) RETURNING id", url).Scan(&id)
	} else {
		err = db.Pool().QueryRow(context.Background(), "INSERT INTO feeds(url,owner_user_id,credentials_encrypted) VALUES($1,$2,'cred') RETURNING id", url, owner).Scan(&id)
	}
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func entriesTestSub(t *testing.T, db *store.Store, user, feed int64, cadence string) int64 {
	t.Helper()
	var id int64
	if err := db.Pool().QueryRow(context.Background(), "INSERT INTO subscriptions(user_id,feed_id,cadence) VALUES($1,$2,$3) RETURNING id", user, feed, cadence).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}

func entriesTestItem(key string) IngestItem {
	return IngestItem{
		IdentityHash:     entriesTestHash("id:" + key),
		ContentHash:      entriesTestHash("body:" + key),
		Title:            key,
		Author:           "",
		ContentHTML:      "<p>" + key + "</p>",
		ContentText:      "text " + key,
		SanitizerVersion: 1,
		ContentSource:    ContentFromFeed,
	}
}

// ingest records one fetched item at the given clock, returning the entry id.
func (f *entriesTestFixture) ingest(t *testing.T, feed int64, key string, at time.Time) int64 {
	t.Helper()
	f.setClock(at)
	if _, err := f.s.RecordFetch(context.Background(), FetchOutcome{FeedID: feed, Status: FetchOK, Items: []IngestItem{entriesTestItem(key)}}); err != nil {
		t.Fatal(err)
	}
	var id int64
	if err := f.db.Pool().QueryRow(context.Background(), "SELECT id FROM entries WHERE feed_id=$1 AND identity_hash=$2", feed, entriesTestHashB("id:"+key)).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}

func (f *entriesTestFixture) ingestMany(t *testing.T, feed int64, prefix string, n int, at time.Time) []int64 {
	t.Helper()
	f.setClock(at)
	items := make([]IngestItem, n)
	for i := range items {
		items[i] = entriesTestItem(fmt.Sprintf("%s-%02d", prefix, i))
	}
	if _, err := f.s.RecordFetch(context.Background(), FetchOutcome{FeedID: feed, Status: FetchOK, Items: items}); err != nil {
		t.Fatal(err)
	}
	rows, err := f.db.Pool().Query(context.Background(), "SELECT id FROM entries WHERE feed_id=$1 ORDER BY seq", feed)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	ids := []int64{}
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			t.Fatal(err)
		}
		ids = append(ids, id)
	}
	if len(ids) != n {
		t.Fatalf("ingested %d of %d", len(ids), n)
	}
	return ids
}

// entriesTestState writes one deliberate register for the principal.
func entriesTestState(t *testing.T, db *store.Store, principal, entry int64, register, value string) {
	t.Helper()
	ctx := context.Background()
	var err error
	switch register {
	case "seen":
		_, err = db.Pool().Exec(ctx, `INSERT INTO entry_state(principal_id,entry_id,seen,seen_rev,seen_occurred_at,seen_actor,seen_source,state_rev)
			VALUES($1,$2,$3,1,now(),$4,'deliberate',1)`, principal, entry, value, principal)
	case "shelf":
		_, err = db.Pool().Exec(ctx, `INSERT INTO entry_state(principal_id,entry_id,shelf,shelf_rev,shelf_occurred_at,shelf_actor,shelf_source,state_rev)
			VALUES($1,$2,$3,1,now(),$4,'deliberate',1)`, principal, entry, value, principal)
	case "kept":
		_, err = db.Pool().Exec(ctx, `INSERT INTO entry_state(principal_id,entry_id,kept,kept_rev,kept_occurred_at,kept_actor,kept_source,state_rev)
			VALUES($1,$2,true,1,now(),$3,'deliberate',1)`, principal, entry, principal)
	}
	if err != nil {
		t.Fatal(err)
	}
}

func entriesTestAgent(t *testing.T, db *store.Store, user int64, name string, scopes ...Scope) Actor {
	t.Helper()
	var id int64
	if err := db.Pool().QueryRow(context.Background(), "INSERT INTO principals(user_id,kind,name) VALUES($1,'agent',$2) RETURNING id", user, name).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return Actor{UserID: user, PrincipalID: id, Kind: PrincipalAgent, Via: CredentialAgentToken, Scopes: scopes}
}

func entriesTestOtherUser(t *testing.T, db *store.Store, name string) Actor {
	t.Helper()
	var user, human int64
	ctx := context.Background()
	if err := db.Pool().QueryRow(ctx, "INSERT INTO users(username,password_hash) VALUES($1,'$argon2id$test') RETURNING id", name).Scan(&user); err != nil {
		t.Fatal(err)
	}
	if err := db.Pool().QueryRow(ctx, "INSERT INTO principals(user_id,kind,name) VALUES($1,'human','Other') RETURNING id", user).Scan(&human); err != nil {
		t.Fatal(err)
	}
	return Actor{UserID: user, PrincipalID: human, Kind: PrincipalHuman, Via: CredentialSession, Scopes: HumanScopes}
}

func entriesTestIDs(entries []Entry) []int64 {
	ids := make([]int64, len(entries))
	for i, e := range entries {
		ids[i] = e.ID
	}
	return ids
}

func entriesTestEqual(t *testing.T, got []Entry, want []int64) {
	t.Helper()
	ids := entriesTestIDs(got)
	if len(ids) != len(want) {
		t.Fatalf("entries %v, want %v", ids, want)
	}
	for i := range ids {
		if ids[i] != want[i] {
			t.Fatalf("entries %v, want %v", ids, want)
		}
	}
}

func TestEntriesRiver(t *testing.T) {
	template := storetest.NewTemplate(t)
	f := entriesTestNew(t, template)
	ctx := context.Background()
	user, human := f.a.UserID, f.a.PrincipalID
	base := f.now()

	feedA := entriesTestFeed(t, f.db, "https://entriesTest-a.test/rss", 0)
	feedB := entriesTestFeed(t, f.db, "https://entriesTest-b.test/rss", user) // private
	feedC := entriesTestFeed(t, f.db, "https://entriesTest-c.test/rss", 0)
	feedD := entriesTestFeed(t, f.db, "https://entriesTest-d.test/rss", 0)
	feedE := entriesTestFeed(t, f.db, "https://entriesTest-e.test/rss", 0)
	feedF := entriesTestFeed(t, f.db, "https://entriesTest-f.test/rss", 0)

	subA := entriesTestSub(t, f.db, user, feedA, "today")
	entriesTestSub(t, f.db, user, feedB, "now")
	subC := entriesTestSub(t, f.db, user, feedC, "today")
	subD := entriesTestSub(t, f.db, user, feedD, "today")
	entriesTestSub(t, f.db, user, feedE, "week")
	subF := entriesTestSub(t, f.db, user, feedF, "today")

	var folder int64
	if err := f.db.Pool().QueryRow(ctx, "INSERT INTO folders(user_id,name) VALUES($1,'entriesTest-folder') RETURNING id", user).Scan(&folder); err != nil {
		t.Fatal(err)
	}
	for _, q := range []struct {
		sql  string
		args []any
	}{
		{"UPDATE subscriptions SET folder_id=$1 WHERE id=$2", []any{folder, subF}},
		{"UPDATE subscriptions SET muted=true WHERE id=$1", []any{subC}},
		{"UPDATE subscriptions SET initial_since=$1 WHERE id=$2", []any{base, subD}},
		{"INSERT INTO mutes(user_id,kind,value,rev) VALUES($1,'keyword','spoiler',1)", []any{user}},
		{"INSERT INTO mutes(user_id,kind,value,rev) VALUES($1,'author','Blocked Writer',1)", []any{user}},
	} {
		if _, err := f.db.Pool().Exec(ctx, q.sql, q.args...); err != nil {
			t.Fatal(err)
		}
	}

	e1 := f.ingest(t, feedA, "go release", base.Add(-1*time.Hour))
	e2 := f.ingest(t, feedA, "later item", base.Add(-2*time.Hour))
	e3 := f.ingest(t, feedA, "done item", base.Add(-3*time.Hour))
	e4 := f.ingest(t, feedA, "kept item", base.Add(-4*time.Hour))
	e5 := f.ingest(t, feedA, "spoiler review", base.Add(-5*time.Hour))
	e6 := f.ingest(t, feedA, "blocked", base.Add(-6*time.Hour))
	if _, err := f.db.Pool().Exec(ctx, "UPDATE entries SET author='Blocked Writer' WHERE id=$1", e6); err != nil {
		t.Fatal(err)
	}
	e7 := f.ingest(t, feedA, "ancient", base.Add(-10*24*time.Hour))
	e8 := f.ingest(t, feedB, "private now", base.Add(-30*24*time.Hour))
	e9 := f.ingest(t, feedC, "muted feed", base.Add(-1*time.Hour))
	e10 := f.ingest(t, feedD, "pre-join", base.Add(-2*time.Hour))
	e11 := f.ingest(t, feedE, "old week", base.Add(-10*24*time.Hour))
	e12 := f.ingest(t, feedE, "fresh week", base.Add(-5*24*time.Hour))
	e13 := f.ingest(t, feedF, "foldered", base.Add(-1*time.Hour))
	e14 := f.ingest(t, feedA, "seen item", base.Add(-7*time.Hour))
	e15 := f.ingest(t, feedA, "gone", base.Add(-8*time.Hour))
	f.setClock(base)

	entriesTestState(t, f.db, human, e2, "shelf", "later")
	entriesTestState(t, f.db, human, e3, "shelf", "done")
	entriesTestState(t, f.db, human, e4, "kept", "")
	entriesTestState(t, f.db, human, e14, "seen", "seen")
	if _, err := f.db.Pool().Exec(ctx, "UPDATE entries SET deleted_at=now() WHERE id=$1", e15); err != nil {
		t.Fatal(err)
	}
	river := []int64{e1, e4, e8, e12, e13}
	t.Run("river", func(t *testing.T) {
		got, err := f.s.Entries(ctx, f.a, EntryFilter{View: ViewRiver})
		if err != nil {
			t.Fatal(err)
		}
		entriesTestEqual(t, got, river)
		for _, e := range got {
			if !e.State.Unread() {
				t.Fatalf("river entry %d not unread: %+v", e.ID, e.State)
			}
			if e.SubscriptionID == 0 || e.FeedID == 0 || e.Seq == 0 || e.Rev == 0 || e.Title == "" || e.FirstSeenAt.IsZero() {
				t.Fatalf("incomplete entry: %+v", e)
			}
		}
	})
	t.Run("river_backward", func(t *testing.T) {
		got, err := f.s.Entries(ctx, f.a, EntryFilter{View: ViewRiver, Direction: PageBackward})
		if err != nil {
			t.Fatal(err)
		}
		want := []int64{river[4], river[3], river[2], river[1], river[0]}
		entriesTestEqual(t, got, want)
	})
	t.Run("shelf_views", func(t *testing.T) {
		later, err := f.s.Entries(ctx, f.a, EntryFilter{View: ViewLater})
		if err != nil {
			t.Fatal(err)
		}
		entriesTestEqual(t, later, []int64{e2})
		done, err := f.s.Entries(ctx, f.a, EntryFilter{View: ViewDone})
		if err != nil {
			t.Fatal(err)
		}
		entriesTestEqual(t, done, []int64{e3})
		kept, err := f.s.Entries(ctx, f.a, EntryFilter{View: ViewKept})
		if err != nil {
			t.Fatal(err)
		}
		entriesTestEqual(t, kept, []int64{e4})
	})
	t.Run("all_and_unread", func(t *testing.T) {
		got, err := f.s.Entries(ctx, f.a, EntryFilter{View: ViewAll})
		if err != nil {
			t.Fatal(err)
		}
		entriesTestEqual(t, got, []int64{e1, e2, e3, e4, e5, e6, e7, e8, e9, e10, e11, e12, e13, e14})
		unread, err := f.s.Entries(ctx, f.a, EntryFilter{View: ViewAll, UnreadOnly: true})
		if err != nil {
			t.Fatal(err)
		}
		entriesTestEqual(t, unread, []int64{e1, e2, e4, e5, e6, e7, e8, e9, e10, e11, e12, e13})
	})
	t.Run("subscription_folder_cadence_filters", func(t *testing.T) {
		got, err := f.s.Entries(ctx, f.a, EntryFilter{View: ViewAll, SubscriptionID: subA})
		if err != nil {
			t.Fatal(err)
		}
		entriesTestEqual(t, got, []int64{e1, e2, e3, e4, e5, e6, e7, e14})
		got, err = f.s.Entries(ctx, f.a, EntryFilter{View: ViewAll, FolderID: folder})
		if err != nil {
			t.Fatal(err)
		}
		entriesTestEqual(t, got, []int64{e13})
		got, err = f.s.Entries(ctx, f.a, EntryFilter{View: ViewAll, Cadence: "week"})
		if err != nil {
			t.Fatal(err)
		}
		entriesTestEqual(t, got, []int64{e11, e12})
	})
	t.Run("label_filter", func(t *testing.T) {
		label, err := f.s.CreateLabel(ctx, f.a, "entriesTest-l", "filter-label-op")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := f.s.AddLabel(ctx, f.a, label.ID, e6, "filter-member-op"); err != nil {
			t.Fatal(err)
		}
		got, err := f.s.Entries(ctx, f.a, EntryFilter{View: ViewAll, LabelID: label.ID})
		if err != nil {
			t.Fatal(err)
		}
		entriesTestEqual(t, got, []int64{e6})
	})
	t.Run("seq_window", func(t *testing.T) {
		var s4, s8 int64
		if err := f.db.Pool().QueryRow(ctx, "SELECT seq FROM entries WHERE id=$1", e4).Scan(&s4); err != nil {
			t.Fatal(err)
		}
		if err := f.db.Pool().QueryRow(ctx, "SELECT seq FROM entries WHERE id=$1", e8).Scan(&s8); err != nil {
			t.Fatal(err)
		}
		got, err := f.s.Entries(ctx, f.a, EntryFilter{View: ViewAll, AfterSeq: uint64(s4), BeforeSeq: uint64(s8)})
		if err != nil {
			t.Fatal(err)
		}
		entriesTestEqual(t, got, []int64{e5, e6, e7})
		got, err = f.s.Entries(ctx, f.a, EntryFilter{View: ViewAll, AfterSeq: math.MaxInt64})
		if err != nil || len(got) != 0 {
			t.Fatalf("overflow bound: %v %v", got, err)
		}
	})
	t.Run("agent_reads_human_river_and_private", func(t *testing.T) {
		agent := entriesTestAgent(t, f.db, user, "bot", ScopeEntriesRead)
		got, err := f.s.Entries(ctx, agent, EntryFilter{View: ViewRiver})
		if err != nil {
			t.Fatal(err)
		}
		entriesTestEqual(t, got, river)
		// The agent's own state replaces the human river on request: the item
		// it saw drops out while items seen or shelved only by the human are
		// unseen to the agent and resurface.
		entriesTestState(t, f.db, agent.PrincipalID, e1, "seen", "seen")
		got, err = f.s.Entries(ctx, agent, EntryFilter{View: ViewRiver, PrincipalID: agent.PrincipalID})
		if err != nil {
			t.Fatal(err)
		}
		entriesTestEqual(t, got, []int64{e2, e3, e4, e8, e12, e13, e14})
		entry, err := f.s.Entry(ctx, agent, e1)
		if err != nil {
			t.Fatal(err)
		}
		if entry.State.Seen.Value != syncmodel.Unseen {
			t.Fatal("default read must be the human state")
		}
	})
	t.Run("privacy", func(t *testing.T) {
		other := entriesTestOtherUser(t, f.db, "entriesTestOther")
		got, err := f.s.Entries(ctx, other, EntryFilter{View: ViewAll})
		if err != nil {
			t.Fatal(err)
		}
		if len(got) != 0 {
			t.Fatalf("foreign entries visible: %v", entriesTestIDs(got))
		}
		if _, err := f.s.Entry(ctx, other, e1); !errors.Is(err, ErrNotFound) {
			t.Fatal("foreign entry readable", err)
		}
		if _, err := f.s.Content(ctx, other, e1); !errors.Is(err, ErrNotFound) {
			t.Fatal("foreign content readable", err)
		}
	})
	t.Run("scopes", func(t *testing.T) {
		noScope := f.a
		noScope.Scopes = []Scope{ScopePositionOwn}
		if _, err := f.s.Entries(ctx, noScope, EntryFilter{}); !errors.Is(err, ErrForbidden) {
			t.Fatal("missing scope accepted", err)
		}
		bad := f.a
		bad.PrincipalID = human + 999
		if _, err := f.s.Entries(ctx, bad, EntryFilter{}); !errors.Is(err, ErrUnauthorized) {
			t.Fatal("unknown principal accepted", err)
		}
	})
	t.Run("filter_validation", func(t *testing.T) {
		for _, filter := range []EntryFilter{
			{View: "bogus"}, {Cadence: "bogus"}, {Direction: "bogus"},
			{Limit: -1}, {Limit: maxEntryLimit + 1}, {SubscriptionID: -1}, {FolderID: -1}, {LabelID: -1}, {PrincipalID: -1},
		} {
			if _, err := f.s.Entries(ctx, f.a, filter); !errors.Is(err, ErrInvalid) {
				t.Fatalf("filter %+v accepted: %v", filter, err)
			}
		}
		agent := entriesTestAgent(t, f.db, user, "bot2", ScopeEntriesRead)
		if _, err := f.s.Entries(ctx, f.a, EntryFilter{PrincipalID: agent.PrincipalID}); err != nil {
			t.Fatal("same-user principal rejected", err)
		}
		other := entriesTestOtherUser(t, f.db, "entriesTestOther2")
		if _, err := f.s.Entries(ctx, f.a, EntryFilter{PrincipalID: other.PrincipalID}); !errors.Is(err, ErrForbidden) {
			t.Fatal("foreign principal state readable", err)
		}
	})
	t.Run("deleted_subscription_hides_feed", func(t *testing.T) {
		feedG := entriesTestFeed(t, f.db, "https://entriesTest-dead.test/rss", 0)
		subG := entriesTestSub(t, f.db, user, feedG, "now")
		e16 := f.ingest(t, feedG, "dead sub", f.now())
		f.setClock(base)
		got, err := f.s.Entries(ctx, f.a, EntryFilter{View: ViewAll, SubscriptionID: subG})
		if err != nil || len(got) != 1 {
			t.Fatalf("subscription entries: %v %v", got, err)
		}
		if _, err := f.db.Pool().Exec(ctx, "UPDATE subscriptions SET deleted_at=now() WHERE id=$1", subG); err != nil {
			t.Fatal(err)
		}
		got, err = f.s.Entries(ctx, f.a, EntryFilter{View: ViewAll, SubscriptionID: subG})
		if err != nil || len(got) != 0 {
			t.Fatalf("dead subscription still visible: %v %v", got, err)
		}
		if _, err := f.s.Entry(ctx, f.a, e16); !errors.Is(err, ErrNotFound) {
			t.Fatal("entry of dead subscription readable", err)
		}
	})
}

func TestEntriesPaging(t *testing.T) {
	template := storetest.NewTemplate(t)
	f := entriesTestNew(t, template)
	ctx := context.Background()
	feed := entriesTestFeed(t, f.db, "https://entriesTest-p.test/rss", 0)
	entriesTestSub(t, f.db, f.a.UserID, feed, "now")
	ids := f.ingestMany(t, feed, "bulk", 60, f.now())
	f.setClock(entriesTestBase)

	got, err := f.s.Entries(ctx, f.a, EntryFilter{View: ViewRiver})
	if err != nil || len(got) != defaultEntryLimit {
		t.Fatalf("default page %d %v", len(got), err)
	}
	entriesTestEqual(t, got, ids[:defaultEntryLimit])
	got, err = f.s.Entries(ctx, f.a, EntryFilter{View: ViewRiver, Direction: PageBackward})
	if err != nil {
		t.Fatal(err)
	}
	if got[0].ID != ids[59] || got[len(got)-1].ID != ids[10] {
		t.Fatal("backward page not newest-first")
	}
	// Continuation: forward from the middle of the stream, and backward
	// through older entries, using the entry's own seq as the bound.
	mid, err := f.s.Entry(ctx, f.a, ids[30])
	if err != nil {
		t.Fatal(err)
	}
	got, err = f.s.Entries(ctx, f.a, EntryFilter{View: ViewRiver, AfterSeq: mid.Seq, Limit: 5})
	if err != nil {
		t.Fatal(err)
	}
	entriesTestEqual(t, got, ids[31:36])
	got, err = f.s.Entries(ctx, f.a, EntryFilter{View: ViewRiver, BeforeSeq: mid.Seq, Direction: PageBackward, Limit: 5})
	if err != nil {
		t.Fatal(err)
	}
	entriesTestEqual(t, got, []int64{ids[29], ids[28], ids[27], ids[26], ids[25]})
	got, err = f.s.Entries(ctx, f.a, EntryFilter{View: ViewRiver, AfterSeq: mid.Seq - 1, Limit: 2})
	if err != nil || len(got) != 2 || got[0].ID != ids[30] {
		t.Fatalf("after bound not exclusive-stable: %v", entriesTestIDs(got))
	}
}

func TestEntriesGetAndContent(t *testing.T) {
	template := storetest.NewTemplate(t)
	f := entriesTestNew(t, template)
	ctx := context.Background()
	user, human := f.a.UserID, f.a.PrincipalID
	feed := entriesTestFeed(t, f.db, "https://entriesTest-g.test/rss", 0)
	entriesTestSub(t, f.db, user, feed, "today")

	e1 := f.ingest(t, feed, "first", f.now().Add(-time.Hour))
	e2 := f.ingest(t, feed, "second", f.now().Add(-2*time.Hour))
	e3 := f.ingest(t, feed, "third", f.now().Add(-3*time.Hour))
	f.setClock(entriesTestBase)
	entriesTestState(t, f.db, human, e2, "shelf", "later")

	t.Run("entry_fields_and_state", func(t *testing.T) {
		got, err := f.s.Entry(ctx, f.a, e2)
		if err != nil {
			t.Fatal(err)
		}
		if got.ID != e2 || got.Title != "second" || got.Seq == 0 || got.Rev == 0 || got.Deleted || got.SubscriptionID == 0 || got.FeedID != feed {
			t.Fatalf("entry: %+v", got)
		}
		if got.State.Shelf.Value != syncmodel.Later || got.State.Shelf.Rev != 1 || got.State.Shelf.Source != syncmodel.SourceDeliberate {
			t.Fatalf("state: %+v", got.State)
		}
		if _, err := f.s.Entry(ctx, f.a, e1+9999); !errors.Is(err, ErrNotFound) {
			t.Fatal("missing entry readable", err)
		}
		if _, err := f.s.Entry(ctx, f.a, 0); !errors.Is(err, ErrInvalid) {
			t.Fatal("zero id accepted", err)
		}
	})
	t.Run("get_by_ids_order_tombstone_and_cap", func(t *testing.T) {
		if _, err := f.db.Pool().Exec(ctx, "UPDATE entries SET deleted_at=now() WHERE id=$1", e3); err != nil {
			t.Fatal(err)
		}
		other := entriesTestOtherUser(t, f.db, "entriesTestOther3")
		got, err := f.s.GetByIDs(ctx, f.a, []int64{e3, e1, e2, e1})
		if err != nil {
			t.Fatal(err)
		}
		if len(got) != 3 || got[0].ID != e3 || got[1].ID != e1 || got[2].ID != e2 {
			t.Fatalf("order: %v", entriesTestIDs(got))
		}
		if !got[0].Deleted {
			t.Fatal("tombstone not flagged")
		}
		got, err = f.s.GetByIDs(ctx, other, []int64{e1, e2})
		if err != nil || len(got) != 0 {
			t.Fatalf("foreign entries leaked: %v %v", got, err)
		}
		big := make([]int64, maxEntryIDs+1)
		for i := range big {
			big[i] = int64(i + 1)
		}
		if _, err := f.s.GetByIDs(ctx, f.a, big); !errors.Is(err, ErrInvalid) {
			t.Fatal("oversized request accepted", err)
		}
		if _, err := f.s.GetByIDs(ctx, f.a, []int64{-1}); !errors.Is(err, ErrInvalid) {
			t.Fatal("negative id accepted", err)
		}
	})
	t.Run("get_by_ids_content_is_opt_in", func(t *testing.T) {
		plain, err := f.s.GetByIDs(ctx, f.a, []int64{e1, e2})
		if err != nil {
			t.Fatal(err)
		}
		for _, e := range plain {
			if e.Content != nil {
				t.Fatalf("GetByIDs loaded content for %d", e.ID)
			}
		}
		withContent, err := f.s.GetByIDsWithContent(ctx, f.a, []int64{e1, e2})
		if err != nil {
			t.Fatal(err)
		}
		for _, e := range withContent {
			if e.Content == nil || e.Content.HTML == "" {
				t.Fatalf("GetByIDsWithContent missing body for %d", e.ID)
			}
		}
		tomb, err := f.s.GetByIDsWithContent(ctx, f.a, []int64{e3})
		if err != nil {
			t.Fatal(err)
		}
		if len(tomb) != 1 || !tomb[0].Deleted || tomb[0].Content != nil {
			t.Fatalf("tombstone content: %+v", tomb)
		}
	})
	t.Run("content_prefers_extracted_then_version", func(t *testing.T) {
		hash := entriesTestHashB("body:first")
		stmt := "INSERT INTO contents(entry_id,content_hash,sanitizer_version,source,html,text) VALUES($1,$2,$3,$4,$5,$6)"
		for _, row := range []struct {
			version int
			source  string
			html    string
		}{{3, "feed", "<p>feed v3</p>"}, {2, "extracted", "<p>extracted v2</p>"}, {1, "extracted", "<p>extracted v1</p>"}} {
			if _, err := f.db.Pool().Exec(ctx, stmt, e1, hash, row.version, row.source, row.html, row.html); err != nil {
				t.Fatal(err)
			}
		}
		// Stale-hash bodies never surface.
		if _, err := f.db.Pool().Exec(ctx, stmt, e1, entriesTestHashB("stale"), 99, "extracted", "<p>stale</p>", "stale"); err != nil {
			t.Fatal(err)
		}
		got, err := f.s.Content(ctx, f.a, e1)
		if err != nil {
			t.Fatal(err)
		}
		if got.Source != ContentFromExtracted || got.SanitizerVersion != 2 || got.HTML != "<p>extracted v2</p>" {
			t.Fatalf("content: %+v", got)
		}
		// Without an extracted variant the highest feed version wins.
		got, err = f.s.Content(ctx, f.a, e2)
		if err != nil {
			t.Fatal(err)
		}
		if got.Source != ContentFromFeed || got.SanitizerVersion != 1 {
			t.Fatalf("content: %+v", got)
		}
		if _, err := f.db.Pool().Exec(ctx, "UPDATE entries SET content_hash=NULL WHERE id=$1", e2); err != nil {
			t.Fatal(err)
		}
		if _, err := f.s.Content(ctx, f.a, e2); !errors.Is(err, ErrNotFound) {
			t.Fatal("empty body served", err)
		}
		if _, err := f.s.Content(ctx, f.a, e3); !errors.Is(err, ErrNotFound) {
			t.Fatal("deleted body served", err)
		}
	})
}

func TestUnreadCounts(t *testing.T) {
	template := storetest.NewTemplate(t)
	f := entriesTestNew(t, template)
	ctx := context.Background()
	user, human := f.a.UserID, f.a.PrincipalID
	base := f.now()

	feedA := entriesTestFeed(t, f.db, "https://counts-a.test/rss", 0)
	feedB := entriesTestFeed(t, f.db, "https://counts-b.test/rss", 0)
	subA := entriesTestSub(t, f.db, user, feedA, "today")
	subB := entriesTestSub(t, f.db, user, feedB, "today")

	f.ingest(t, feedA, "unread one", base.Add(-time.Hour))
	e2 := f.ingest(t, feedA, "later", base.Add(-2*time.Hour))
	e3 := f.ingest(t, feedA, "done", base.Add(-3*time.Hour))
	e4 := f.ingest(t, feedA, "kept", base.Add(-4*time.Hour))
	e5 := f.ingest(t, feedA, "seen", base.Add(-5*time.Hour))
	f.ingest(t, feedB, "unread two", base.Add(-time.Hour))
	e7 := f.ingest(t, feedA, "gone", base.Add(-6*time.Hour))

	entriesTestState(t, f.db, human, e2, "shelf", "later")
	entriesTestState(t, f.db, human, e3, "shelf", "done")
	entriesTestState(t, f.db, human, e4, "kept", "")
	entriesTestState(t, f.db, human, e5, "seen", "seen")
	if _, err := f.db.Pool().Exec(ctx, "UPDATE entries SET deleted_at=now() WHERE id=$1", e7); err != nil {
		t.Fatal(err)
	}

	got, err := f.s.UnreadCounts(ctx, f.a)
	if err != nil {
		t.Fatal(err)
	}
	if got.BySubscription[subA] != 3 || got.BySubscription[subB] != 1 {
		t.Fatalf("by subscription: %+v", got.BySubscription)
	}
	if got.Later != 1 || got.Kept != 1 || got.Total != 4 {
		t.Fatalf("totals: %+v", got)
	}

	// A soft-deleted subscription's entries no longer count.
	if _, err := f.db.Pool().Exec(ctx, "UPDATE subscriptions SET deleted_at=now() WHERE id=$1", subB); err != nil {
		t.Fatal(err)
	}
	got, err = f.s.UnreadCounts(ctx, f.a)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := got.BySubscription[subB]; ok {
		t.Fatalf("deleted subscription still counted: %+v", got.BySubscription)
	}
	if got.BySubscription[subA] != 3 || got.Total != 3 || got.Later != 1 || got.Kept != 1 {
		t.Fatalf("totals after unsubscribe: %+v", got)
	}

	// The aggregate must agree with the unread-only list it summarises.
	var listed []Entry
	var afterSeq uint64
	for {
		page, err := f.s.Entries(ctx, f.a, EntryFilter{View: ViewAll, UnreadOnly: true, Limit: 200, AfterSeq: afterSeq})
		if err != nil {
			t.Fatal(err)
		}
		listed = append(listed, page...)
		if len(page) < 200 {
			break
		}
		afterSeq = page[len(page)-1].Seq
	}
	bySub := map[int64]int64{}
	var later, kept, total int64
	for _, entry := range listed {
		bySub[entry.SubscriptionID]++
		if entry.State.Shelf.Value == syncmodel.Later {
			later++
		}
		if entry.State.Kept.Value == syncmodel.True {
			kept++
		}
		total++
	}
	if !reflect.DeepEqual(bySub, got.BySubscription) {
		t.Fatalf("per subscription: list=%v counts=%v", bySub, got.BySubscription)
	}
	if later != got.Later || kept != got.Kept || total != got.Total {
		t.Fatalf("totals: list later=%d kept=%d total=%d counts=%+v", later, kept, total, got)
	}
}

// TestUnreadCountsMatchWalkForAgents pins the Google Reader total against the
// paging loop it replaced, for the human and for an agent actor (both resolve
// the human's registers), and checks the read scope is required.
func TestUnreadCountsMatchWalkForAgents(t *testing.T) {
	template := storetest.NewTemplate(t)
	f := entriesTestNew(t, template)
	ctx := context.Background()
	user, human := f.a.UserID, f.a.PrincipalID

	feedA := entriesTestFeed(t, f.db, "https://unreadwalk-a.test/rss", 0)
	feedB := entriesTestFeed(t, f.db, "https://unreadwalk-b.test/rss", 0)
	entriesTestSub(t, f.db, user, feedA, "today")
	subB := entriesTestSub(t, f.db, user, feedB, "now")

	ids := f.ingestMany(t, feedA, "uc", 8, f.now())
	f.ingestMany(t, feedB, "ucB", 4, f.now())
	f.setClock(entriesTestBase)

	// Later and kept stay unread; seen, done, tombstones and dead
	// subscriptions drop out.
	entriesTestState(t, f.db, human, ids[0], "seen", "seen")
	entriesTestState(t, f.db, human, ids[1], "shelf", "done")
	entriesTestState(t, f.db, human, ids[2], "shelf", "later")
	entriesTestState(t, f.db, human, ids[3], "kept", "")
	if _, err := f.db.Pool().Exec(ctx, "UPDATE entries SET deleted_at=now() WHERE id=$1", ids[4]); err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.Pool().Exec(ctx, "UPDATE subscriptions SET deleted_at=now() WHERE id=$1", subB); err != nil {
		t.Fatal(err)
	}
	// Other principals' registers never feed the human's count.
	other := entriesTestOtherUser(t, f.db, "unreadWalkOther")
	entriesTestState(t, f.db, other.PrincipalID, ids[5], "seen", "seen")
	agent := entriesTestAgent(t, f.db, user, "ucbot", ScopeEntriesRead)
	entriesTestState(t, f.db, agent.PrincipalID, ids[6], "seen", "seen")

	// walk replicates the greader unread-count loop it replaced.
	walk := func() int64 {
		var total int64
		filter := EntryFilter{View: ViewAll, UnreadOnly: true, Limit: 5}
		for {
			page, err := f.s.Entries(ctx, f.a, filter)
			if err != nil {
				t.Fatal(err)
			}
			total += int64(len(page))
			if len(page) < 5 {
				return total
			}
			filter.AfterSeq = page[len(page)-1].Seq
		}
	}
	want := walk()
	if want != 5 {
		t.Fatalf("walk counted %d, want 5", want)
	}
	for _, a := range []Actor{f.a, agent} {
		got, err := f.s.UnreadCounts(ctx, a)
		if err != nil {
			t.Fatal(err)
		}
		if got.Total != want {
			t.Fatalf("UnreadCounts total %d, walk %d", got.Total, want)
		}
	}
	noScope := f.a
	noScope.Scopes = []Scope{ScopePositionOwn}
	if _, err := f.s.UnreadCounts(ctx, noScope); !errors.Is(err, ErrForbidden) {
		t.Fatal("missing scope accepted", err)
	}
}
