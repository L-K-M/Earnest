package service

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/L-K-M/Earnest/internal/store"
	"github.com/L-K-M/Earnest/internal/store/storetest"
	"github.com/L-K-M/Earnest/internal/syncmodel"
	"github.com/jackc/pgx/v5"
)

type syncTestFixture struct {
	s  *Service
	db *store.Store
}

func newSyncTestFixture(t *testing.T, template *storetest.Template) *syncTestFixture {
	t.Helper()
	f := &syncTestFixture{db: template.New(t)}
	var err error
	f.s, err = New(f.db, Deps{SecretKey: []byte(strings.Repeat("y", 32))})
	if err != nil {
		t.Fatal(err)
	}
	return f
}

func (f *syncTestFixture) user(t *testing.T, name string) int64 {
	t.Helper()
	var id int64
	if err := f.db.Pool().QueryRow(context.Background(), "INSERT INTO users(username,password_hash) VALUES($1,'$argon2id$test') RETURNING id", name).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}

func (f *syncTestFixture) principal(t *testing.T, user int64, kind PrincipalKind, name string) int64 {
	t.Helper()
	var id int64
	if err := f.db.Pool().QueryRow(context.Background(), "INSERT INTO principals(user_id,kind,name) VALUES($1,$2,$3) RETURNING id", user, string(kind), name).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}

func (f *syncTestFixture) actor(user, principal int64, kind PrincipalKind) Actor {
	scopes := HumanScopes
	if kind == PrincipalAgent {
		scopes = AgentScopes
	}
	return Actor{UserID: user, PrincipalID: principal, Kind: kind, Via: CredentialSession, Scopes: scopes}
}

// emit writes one committed change at an explicit log time through the same
// lock and counter path the service uses.
func (f *syncTestFixture) emit(t *testing.T, user int64, kind syncmodel.ChangeType, payload any, at time.Time) int64 {
	t.Helper()
	ctx := context.Background()
	var rev int64
	err := f.db.WithTx(ctx, func(tx pgx.Tx) error {
		if err := store.LockCore(ctx, tx); err != nil {
			return err
		}
		alloc, err := store.AllocateCounters(ctx, tx, 0, map[int64]int64{user: 1})
		if err != nil {
			return err
		}
		rev = alloc.Revs[user].First
		return store.InsertChange(ctx, tx, user, rev, string(kind), payload, at)
	})
	if err != nil {
		t.Fatal(err)
	}
	return rev
}

func (f *syncTestFixture) feed(t *testing.T, name string, owner int64) int64 {
	t.Helper()
	var id int64
	if owner == 0 {
		err := f.db.Pool().QueryRow(context.Background(), "INSERT INTO feeds(url) VALUES($1) RETURNING id", "https://example.test/feed/"+name).Scan(&id)
		if err != nil {
			t.Fatal(err)
		}
		return id
	}
	err := f.db.Pool().QueryRow(context.Background(), "INSERT INTO feeds(url,owner_user_id,credentials_encrypted) VALUES($1,$2,$3) RETURNING id",
		"https://example.test/feed/"+name, owner, []byte("syncTestCredentialBytes")).Scan(&id)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func (f *syncTestFixture) subscribe(t *testing.T, user, feed int64) int64 {
	t.Helper()
	var id int64
	if err := f.db.Pool().QueryRow(context.Background(), "INSERT INTO subscriptions(user_id,feed_id) VALUES($1,$2) RETURNING id", user, feed).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}

func (f *syncTestFixture) entryAt(t *testing.T, feed, seq int64, firstSeen time.Time, published *time.Time) int64 {
	t.Helper()
	h := sha256.Sum256([]byte("syncTest-seq-" + strings.Repeat("z", int(seq))))
	var id int64
	err := f.db.Pool().QueryRow(context.Background(),
		"INSERT INTO entries(feed_id,seq,identity_hash,first_seen_at,published_at) VALUES($1,$2,$3,$4,$5) RETURNING id",
		feed, seq, h[:], firstSeen, published).Scan(&id)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func (f *syncTestFixture) label(t *testing.T, user int64, name string, rev int64, deleted bool) int64 {
	t.Helper()
	var id int64
	var deletedAt any
	if deleted {
		deletedAt = time.Now()
	}
	if err := f.db.Pool().QueryRow(context.Background(), "INSERT INTO labels(user_id,name,rev,deleted_at) VALUES($1,$2,$3,$4) RETURNING id", user, name, rev, deletedAt).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}

func (f *syncTestFixture) generation(t *testing.T) string {
	t.Helper()
	g, err := f.db.Generation(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return g
}

func (f *syncTestFixture) floor(t *testing.T, user int64) int64 {
	t.Helper()
	var floor int64
	if err := f.db.Pool().QueryRow(context.Background(), "SELECT COALESCE((SELECT retention_floor FROM rev_counters WHERE user_id=$1),0)", user).Scan(&floor); err != nil {
		t.Fatal(err)
	}
	return floor
}

func (f *syncTestFixture) changeCount(t *testing.T, user int64) int64 {
	t.Helper()
	var n int64
	if err := f.db.Pool().QueryRow(context.Background(), "SELECT count(*) FROM changes WHERE user_id=$1", user).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func syncTestRevs(t *testing.T, page ChangePage) []uint64 {
	t.Helper()
	out := []uint64{}
	for _, c := range page.Changes {
		out = append(out, c.Rev)
	}
	return out
}

func syncTestDecode(t *testing.T, item json.RawMessage) map[string]any {
	t.Helper()
	var doc map[string]any
	if err := json.Unmarshal(item, &doc); err != nil {
		t.Fatal(err)
	}
	return doc
}

func TestSyncChanges(t *testing.T) {
	template := storetest.NewTemplate(t)
	t.Run("page_validation_and_cursor", func(t *testing.T) {
		f := newSyncTestFixture(t, template)
		user := f.user(t, "syncTest-changes-a")
		p := f.principal(t, user, PrincipalHuman, "human")
		a := f.actor(user, p, PrincipalHuman)
		now := time.Date(2026, 11, 2, 12, 0, 0, 0, time.UTC)
		for i, kind := range []syncmodel.ChangeType{syncmodel.EntryCreated, syncmodel.StateUpdated, syncmodel.PositionUpdated, syncmodel.FolderUpdated, syncmodel.SubscriptionDeleted} {
			f.emit(t, user, kind, map[string]any{"n": i + 1}, now.Add(time.Duration(i)*time.Hour))
		}
		gen := f.generation(t)

		page, err := f.s.Changes(context.Background(), a, syncmodel.Revision{Generation: gen}, 0)
		if err != nil {
			t.Fatal(err)
		}
		if len(page.Changes) != 5 || page.Next.Rev != 5 || page.Next.Generation != gen {
			t.Fatalf("full page: %+v", page)
		}
		for i, c := range page.Changes {
			if c.Rev != uint64(i+1) || string(c.Type) == "" {
				t.Fatalf("record %d: %+v", i, c)
			}
		}

		page, err = f.s.Changes(context.Background(), a, syncmodel.Revision{Generation: gen}, 2)
		if err != nil {
			t.Fatal(err)
		}
		if got := syncTestRevs(t, page); len(got) != 2 || got[1] != 2 || page.Next.Rev != 2 {
			t.Fatalf("first page: %v next=%d", got, page.Next.Rev)
		}
		page, err = f.s.Changes(context.Background(), a, page.Next, 0)
		if err != nil {
			t.Fatal(err)
		}
		if got := syncTestRevs(t, page); len(got) != 3 || got[2] != 5 || page.Next.Rev != 5 {
			t.Fatalf("resume: %v", got)
		}
		page, err = f.s.Changes(context.Background(), a, page.Next, 0)
		if err != nil {
			t.Fatal(err)
		}
		if len(page.Changes) != 0 || page.Next.Rev != 5 {
			t.Fatalf("tail page: %+v", page)
		}

		for _, cursor := range []syncmodel.Revision{
			{Generation: "00000000-0000-0000-0000-000000000000"},
			{Generation: "older-generation"},
			{Generation: gen, Rev: 6},
			{Generation: gen, Rev: 9999},
		} {
			if _, err := f.s.Changes(context.Background(), a, cursor, 0); !errors.Is(err, ErrGone) {
				t.Fatalf("cursor %+v: want ErrGone, got %v", cursor, err)
			}
		}
		if _, err := f.s.Changes(context.Background(), a, syncmodel.Revision{Generation: gen}, -1); !errors.Is(err, ErrInvalid) {
			t.Fatalf("negative limit: %v", err)
		}
	})
	t.Run("user_isolation_and_agent_full_feed", func(t *testing.T) {
		f := newSyncTestFixture(t, template)
		userA := f.user(t, "syncTest-changes-a2")
		userB := f.user(t, "syncTest-changes-b2")
		pa := f.principal(t, userA, PrincipalHuman, "human")
		pb := f.principal(t, userB, PrincipalHuman, "human")
		agent := f.principal(t, userA, PrincipalAgent, "agent")
		now := time.Now()
		for i := range 3 {
			f.emit(t, userA, syncmodel.EntryCreated, map[string]any{"entry": map[string]any{"id": i}}, now)
		}
		f.emit(t, userB, syncmodel.FolderDeleted, map[string]any{"object": map[string]any{"id": 9}}, now)
		gen := f.generation(t)

		page, err := f.s.Changes(context.Background(), f.actor(userB, pb, PrincipalHuman), syncmodel.Revision{Generation: gen}, 0)
		if err != nil {
			t.Fatal(err)
		}
		if len(page.Changes) != 1 || page.Changes[0].Type != syncmodel.FolderDeleted {
			t.Fatalf("user isolation: %+v", page)
		}
		page, err = f.s.Changes(context.Background(), f.actor(userA, agent, PrincipalAgent), syncmodel.Revision{Generation: gen}, 0)
		if err != nil {
			t.Fatal(err)
		}
		if len(page.Changes) != 3 || page.Next.Rev != 3 {
			t.Fatalf("agent full feed: %+v", page)
		}
		page, err = f.s.Changes(context.Background(), f.actor(userA, pa, PrincipalHuman), syncmodel.Revision{Generation: gen}, 0)
		if err != nil {
			t.Fatal(err)
		}
		if len(page.Changes) != 3 {
			t.Fatalf("human feed: %+v", page)
		}
		// A different user's principal or a scope-less actor cannot read.
		stranger := f.actor(userB, pa, PrincipalHuman)
		if _, err := f.s.Changes(context.Background(), stranger, syncmodel.Revision{Generation: gen}, 0); !errors.Is(err, ErrUnauthorized) {
			t.Fatalf("foreign principal: %v", err)
		}
		scopeless := f.actor(userA, pa, PrincipalHuman)
		scopeless.Scopes = nil
		if _, err := f.s.Changes(context.Background(), scopeless, syncmodel.Revision{Generation: gen}, 0); !errors.Is(err, ErrForbidden) {
			t.Fatalf("scopeless actor: %v", err)
		}
	})
	t.Run("retention_floor_cursor_boundary", func(t *testing.T) {
		f := newSyncTestFixture(t, template)
		user := f.user(t, "syncTest-floor")
		p := f.principal(t, user, PrincipalHuman, "human")
		a := f.actor(user, p, PrincipalHuman)
		now := time.Now()
		for i := range 5 {
			f.emit(t, user, syncmodel.EntryCreated, map[string]any{"n": i}, now)
		}
		gen := f.generation(t)
		if _, err := f.db.Pool().Exec(context.Background(), "UPDATE rev_counters SET retention_floor=3 WHERE user_id=$1", user); err != nil {
			t.Fatal(err)
		}
		if _, err := f.s.Changes(context.Background(), a, syncmodel.Revision{Generation: gen, Rev: 2}, 0); !errors.Is(err, ErrGone) {
			t.Fatalf("below floor: %v", err)
		}
		page, err := f.s.Changes(context.Background(), a, syncmodel.Revision{Generation: gen, Rev: 3}, 0)
		if err != nil {
			t.Fatalf("at floor: %v", err)
		}
		if got := syncTestRevs(t, page); len(got) != 2 || got[0] != 4 {
			t.Fatalf("page at floor: %v", got)
		}
	})
}

func TestSyncBootstrapCursor(t *testing.T) {
	template := storetest.NewTemplate(t)
	f := newSyncTestFixture(t, template)
	user := f.user(t, "syncTest-cursor")
	p := f.principal(t, user, PrincipalHuman, "human")
	for range 4 {
		f.emit(t, user, syncmodel.EntryCreated, map[string]any{}, time.Now())
	}
	cursor, err := f.s.BootstrapCursor(context.Background(), f.actor(user, p, PrincipalHuman))
	if err != nil {
		t.Fatal(err)
	}
	if cursor.Generation != f.generation(t) || cursor.Rev != 4 {
		t.Fatalf("cursor: %+v", cursor)
	}
	fresh := f.user(t, "syncTest-cursor-fresh")
	fp := f.principal(t, fresh, PrincipalHuman, "human")
	cursor, err = f.s.BootstrapCursor(context.Background(), f.actor(fresh, fp, PrincipalHuman))
	if err != nil {
		t.Fatal(err)
	}
	if cursor.Rev != 0 {
		t.Fatalf("fresh user cursor: %+v", cursor)
	}
}

func TestSyncBootstrap(t *testing.T) {
	template := storetest.NewTemplate(t)
	t.Run("subscriptions_with_tombstone_no_credentials", func(t *testing.T) {
		f := newSyncTestFixture(t, template)
		user := f.user(t, "syncTest-subs")
		p := f.principal(t, user, PrincipalHuman, "human")
		a := f.actor(user, p, PrincipalHuman)
		public := f.feed(t, "public", 0)
		private := f.feed(t, "private", user)
		s1 := f.subscribe(t, user, public)
		s2 := f.subscribe(t, user, private)
		s3f := f.feed(t, "deleted-sub-feed", 0)
		var s3id int64
		ctx := context.Background()
		if err := f.db.Pool().QueryRow(ctx, "INSERT INTO subscriptions(user_id,feed_id) VALUES($1,$2) RETURNING id", user, s3f).Scan(&s3id); err != nil {
			t.Fatal(err)
		}
		if _, err := f.db.Pool().Exec(ctx, "UPDATE subscriptions SET deleted_at=now() WHERE id=$1", s3id); err != nil {
			t.Fatal(err)
		}
		page, err := f.s.Bootstrap(ctx, a, BootstrapRequest{Kind: BootstrapSubscriptions})
		if err != nil {
			t.Fatal(err)
		}
		if len(page.Items) != 3 || page.HasMore {
			t.Fatalf("subscriptions page: %d items", len(page.Items))
		}
		seen := map[int64]bool{}
		for _, item := range page.Items {
			doc := syncTestDecode(t, item)
			id := int64(doc["id"].(float64))
			seen[id] = true
			for key := range doc {
				if strings.Contains(key, "credential") {
					t.Fatalf("credential field leaked in %v", doc)
				}
			}
			var sub Subscription
			if err := json.Unmarshal(item, &sub); err != nil {
				t.Fatalf("decode subscription: %v", err)
			}
			if sub.FeedID == 0 || sub.URL == "" || sub.Private != (id == s2) {
				t.Fatalf("subscription projection: %+v", sub)
			}
			if _, ok := doc["rev"]; !ok {
				t.Fatalf("subscription missing rev: %v", doc)
			}
		}
		if !seen[s1] || !seen[s2] || !seen[s3id] {
			t.Fatalf("missing subscriptions: %v", seen)
		}
		if deleted := syncTestDecode(t, page.Items[2])["deleted"]; deleted != true {
			t.Fatalf("subscription tombstone: %v", page.Items[2])
		}
	})
	t.Run("folders_labels_tombstones", func(t *testing.T) {
		f := newSyncTestFixture(t, template)
		user := f.user(t, "syncTest-fol")
		p := f.principal(t, user, PrincipalHuman, "human")
		a := f.actor(user, p, PrincipalHuman)
		var f1, f2 int64
		ctx := context.Background()
		if err := f.db.Pool().QueryRow(ctx, "INSERT INTO folders(user_id,name,rev) VALUES($1,'keep',1) RETURNING id", user).Scan(&f1); err != nil {
			t.Fatal(err)
		}
		if err := f.db.Pool().QueryRow(ctx, "INSERT INTO folders(user_id,name,rev,deleted_at) VALUES($1,'gone',2,now()) RETURNING id", user).Scan(&f2); err != nil {
			t.Fatal(err)
		}
		l1 := f.label(t, user, "keep", 3, false)
		l2 := f.label(t, user, "gone", 4, true)
		page, err := f.s.Bootstrap(ctx, a, BootstrapRequest{Kind: BootstrapFolders})
		if err != nil {
			t.Fatal(err)
		}
		if len(page.Items) != 2 {
			t.Fatalf("folders: %d", len(page.Items))
		}
		if syncTestDecode(t, page.Items[1])["deleted"] != true {
			t.Fatalf("folder tombstone missing")
		}
		page, err = f.s.Bootstrap(ctx, a, BootstrapRequest{Kind: BootstrapLabels, Limit: 1})
		if err != nil {
			t.Fatal(err)
		}
		if len(page.Items) != 1 || !page.HasMore || page.NextID != l1 {
			t.Fatalf("labels page 1: %+v", page)
		}
		page, err = f.s.Bootstrap(ctx, a, BootstrapRequest{Kind: BootstrapLabels, AfterID: page.NextID})
		if err != nil {
			t.Fatal(err)
		}
		if len(page.Items) != 1 || page.HasMore || syncTestDecode(t, page.Items[0])["id"] != float64(l2) {
			t.Fatalf("labels page 2: %+v", page)
		}
	})
	t.Run("membership_composite_cursor", func(t *testing.T) {
		f := newSyncTestFixture(t, template)
		user := f.user(t, "syncTest-mem")
		p := f.principal(t, user, PrincipalHuman, "human")
		a := f.actor(user, p, PrincipalHuman)
		feed := f.feed(t, "m", 0)
		f.subscribe(t, user, feed)
		e1 := f.entryAt(t, feed, 1, time.Now(), nil)
		e2 := f.entryAt(t, feed, 2, time.Now(), nil)
		ctx := context.Background()
		l1 := f.label(t, user, "a", 1, false)
		l2 := f.label(t, user, "b", 1, false)
		memberships := [][3]int64{{l1, e1, 1}, {l1, e2, 2}, {l2, e1, 3}}
		for _, m := range memberships {
			if _, err := f.db.Pool().Exec(ctx, "INSERT INTO entry_labels(user_id,entry_id,label_id,rev) VALUES($1,$2,$3,$4)", user, m[1], m[0], m[2]); err != nil {
				t.Fatal(err)
			}
		}
		if _, err := f.db.Pool().Exec(ctx, "INSERT INTO entry_labels(user_id,entry_id,label_id,rev,deleted_at) VALUES($1,$2,$3,4,now())", user, e2, l2); err != nil {
			t.Fatal(err)
		}
		page, err := f.s.Bootstrap(ctx, a, BootstrapRequest{Kind: BootstrapLabelMemberships})
		if err != nil {
			t.Fatal(err)
		}
		if len(page.Items) != 4 {
			t.Fatalf("memberships: %d", len(page.Items))
		}
		// Equal label IDs across different entries must not be skipped.
		page, err = f.s.Bootstrap(ctx, a, BootstrapRequest{Kind: BootstrapLabelMemberships, AfterPrincipal: l1, AfterID: e1})
		if err != nil {
			t.Fatal(err)
		}
		if len(page.Items) != 3 {
			t.Fatalf("resume after (%d,%d): %d items", l1, e1, len(page.Items))
		}
		first := syncTestDecode(t, page.Items[0])
		if first["label_id"] != float64(l1) || first["entry_id"] != float64(e2) {
			t.Fatalf("composite cursor skipped equal label: %v", first)
		}
		last := syncTestDecode(t, page.Items[2])
		if last["deleted"] != true {
			t.Fatalf("membership tombstone missing: %v", last)
		}
	})
	t.Run("positions_composite_cursor", func(t *testing.T) {
		f := newSyncTestFixture(t, template)
		user := f.user(t, "syncTest-pos")
		human := f.principal(t, user, PrincipalHuman, "human")
		agent := f.principal(t, user, PrincipalAgent, "agent")
		a := f.actor(user, human, PrincipalHuman)
		ctx := context.Background()
		for _, pos := range []struct {
			p    int64
			view string
			seq  int64
		}{{human, "river", 10}, {human, "unread", 4}, {agent, "river", 7}} {
			if _, err := f.db.Pool().Exec(ctx, "INSERT INTO positions(principal_id,view,anchor_seq,rev) VALUES($1,$2,$3,1)", pos.p, pos.view, pos.seq); err != nil {
				t.Fatal(err)
			}
		}
		page, err := f.s.Bootstrap(ctx, a, BootstrapRequest{Kind: BootstrapPositions})
		if err != nil {
			t.Fatal(err)
		}
		if len(page.Items) != 3 {
			t.Fatalf("positions: %d", len(page.Items))
		}
		// After (human,"river"), the same principal's later views and other
		// principals must follow without skipping.
		page, err = f.s.Bootstrap(ctx, a, BootstrapRequest{Kind: BootstrapPositions, AfterPrincipal: human, AfterView: "river"})
		if err != nil {
			t.Fatal(err)
		}
		if len(page.Items) != 2 {
			t.Fatalf("position resume: %d", len(page.Items))
		}
		first := syncTestDecode(t, page.Items[0])
		if first["principal"] != float64(human) || first["view"] != "unread" {
			t.Fatalf("position cursor skipped equal principal: %v", first)
		}
	})
	t.Run("entries_window_tombstones_rev", func(t *testing.T) {
		f := newSyncTestFixture(t, template)
		user := f.user(t, "syncTest-ent")
		p := f.principal(t, user, PrincipalHuman, "human")
		a := f.actor(user, p, PrincipalHuman)
		feed := f.feed(t, "w", 0)
		f.subscribe(t, user, feed)
		recent := time.Now().Add(-time.Hour)
		e1 := f.entryAt(t, feed, 1, recent, nil)
		e2 := f.entryAt(t, feed, 2, recent, nil)
		e3 := f.entryAt(t, feed, 3, recent.Add(-100*24*time.Hour), nil)
		if _, err := f.db.Pool().Exec(context.Background(), "UPDATE entries SET deleted_at=now() WHERE id=$1", e2); err != nil {
			t.Fatal(err)
		}
		if _, err := f.db.Pool().Exec(context.Background(), "INSERT INTO entry_revisions(user_id,entry_id,rev) VALUES($1,$2,7),($1,$3,8)", user, e1, e2); err != nil {
			t.Fatal(err)
		}
		page, err := f.s.Bootstrap(context.Background(), a, BootstrapRequest{Kind: BootstrapEntries})
		if err != nil {
			t.Fatal(err)
		}
		if len(page.Items) != 2 {
			t.Fatalf("window entries: %d", len(page.Items))
		}
		first, second := syncTestDecode(t, page.Items[0]), syncTestDecode(t, page.Items[1])
		if first["id"] != float64(e1) || first["rev"] != float64(7) || first["deleted"] != false {
			t.Fatalf("entry: %v", first)
		}
		if second["id"] != float64(e2) || second["deleted"] != true {
			t.Fatalf("entry tombstone dropped from window: %v", second)
		}
		if _, ok := first["html"]; ok {
			t.Fatalf("body leaked into entries page")
		}
		// An explicit Since widens the window.
		page, err = f.s.Bootstrap(context.Background(), a, BootstrapRequest{Kind: BootstrapEntries, Since: recent.Add(-200 * 24 * time.Hour)})
		if err != nil {
			t.Fatal(err)
		}
		if len(page.Items) != 3 || syncTestDecode(t, page.Items[2])["id"] != float64(e3) {
			t.Fatalf("since window: %d", len(page.Items))
		}
	})
	t.Run("state_window_and_registers", func(t *testing.T) {
		f := newSyncTestFixture(t, template)
		user := f.user(t, "syncTest-st")
		human := f.principal(t, user, PrincipalHuman, "human")
		agent := f.principal(t, user, PrincipalAgent, "agent")
		a := f.actor(user, human, PrincipalHuman)
		feed := f.feed(t, "st", 0)
		f.subscribe(t, user, feed)
		recent := time.Now().Add(-time.Hour)
		e1 := f.entryAt(t, feed, 1, recent, nil)
		e2 := f.entryAt(t, feed, 2, recent, nil)
		old := f.entryAt(t, feed, 3, recent.Add(-100*24*time.Hour), nil)
		ctx := context.Background()
		// Human state on e1 (seen), agent state on e2 (later), human state on
		// the out-of-window entry (must not bootstrap).
		if _, err := f.db.Pool().Exec(ctx, `INSERT INTO entry_state(principal_id,entry_id,seen,seen_rev,seen_occurred_at,seen_actor,seen_source,state_rev)
  VALUES($1,$2,'seen',5,$3,$1,'deliberate',5)`, human, e1, recent); err != nil {
			t.Fatal(err)
		}
		if _, err := f.db.Pool().Exec(ctx, `INSERT INTO entry_state(principal_id,entry_id,shelf,shelf_rev,shelf_occurred_at,shelf_actor,shelf_source,state_rev)
  VALUES($1,$2,'later',6,$3,$1,'rule_or_agent',6)`, agent, e2, recent); err != nil {
			t.Fatal(err)
		}
		if _, err := f.db.Pool().Exec(ctx, `INSERT INTO entry_state(principal_id,entry_id,kept,kept_rev,kept_occurred_at,kept_actor,kept_source,state_rev)
  VALUES($1,$2,true,7,$3,$1,'deliberate',7)`, human, old, recent); err != nil {
			t.Fatal(err)
		}
		page, err := f.s.Bootstrap(ctx, a, BootstrapRequest{Kind: BootstrapState})
		if err != nil {
			t.Fatal(err)
		}
		if len(page.Items) != 2 {
			t.Fatalf("state window: %d", len(page.Items))
		}
		var seenState, shelfState syncmodel.State
		for _, item := range page.Items {
			var st syncmodel.State
			if err := json.Unmarshal(item, &st); err != nil {
				t.Fatal(err)
			}
			switch int64(st.EntryID) {
			case e1:
				seenState = st
			case e2:
				shelfState = st
			default:
				t.Fatalf("state outside window: %v", st.EntryID)
			}
		}
		if seenState.Seen.Value != syncmodel.SeenValue || seenState.Seen.Rev != 5 || seenState.StateRev != 5 || seenState.Seen.Source != syncmodel.SourceDeliberate || int64(seenState.Seen.Actor) != human {
			t.Fatalf("seen register: %+v", seenState.Seen)
		}
		if shelfState.Shelf.Value != syncmodel.Later || shelfState.Shelf.Rev != 6 || shelfState.Shelf.Source != syncmodel.SourceRuleOrAgent {
			t.Fatalf("shelf register: %+v", shelfState.Shelf)
		}
		// Composite resume: after (human, e1) only the agent's row remains.
		page, err = f.s.Bootstrap(ctx, a, BootstrapRequest{Kind: BootstrapState, AfterPrincipal: human, AfterID: e1})
		if err != nil {
			t.Fatal(err)
		}
		if len(page.Items) != 1 {
			t.Fatalf("state resume: %d", len(page.Items))
		}
	})
	t.Run("validation_and_isolation", func(t *testing.T) {
		f := newSyncTestFixture(t, template)
		userA := f.user(t, "syncTest-iso-a")
		userB := f.user(t, "syncTest-iso-b")
		pa := f.principal(t, userA, PrincipalHuman, "human")
		pb := f.principal(t, userB, PrincipalHuman, "human")
		feed := f.feed(t, "iso", 0)
		f.subscribe(t, userA, feed)
		f.entryAt(t, feed, 1, time.Now(), nil)
		page, err := f.s.Bootstrap(context.Background(), f.actor(userB, pb, PrincipalHuman), BootstrapRequest{Kind: BootstrapEntries})
		if err != nil {
			t.Fatal(err)
		}
		if len(page.Items) != 0 {
			t.Fatalf("bootstrap leaked across users: %d", len(page.Items))
		}
		for _, req := range []BootstrapRequest{
			{Kind: "nonsense"},
			{Kind: BootstrapEntries, Limit: -1},
			{Kind: BootstrapEntries, AfterID: -1},
			{Kind: BootstrapPositions, AfterPrincipal: -2},
		} {
			if _, err := f.s.Bootstrap(context.Background(), f.actor(userA, pa, PrincipalHuman), req); !errors.Is(err, ErrInvalid) {
				t.Fatalf("request %+v: %v", req, err)
			}
		}
	})
}

func TestSyncRetainChanges(t *testing.T) {
	template := storetest.NewTemplate(t)
	t.Run("prefix_floor_and_boundaries", func(t *testing.T) {
		f := newSyncTestFixture(t, template)
		user := f.user(t, "syncTest-retain-a")
		p := f.principal(t, user, PrincipalHuman, "human")
		a := f.actor(user, p, PrincipalHuman)
		now := time.Now()
		for i := range 6 {
			at := now
			if i < 3 {
				at = now.Add(-100 * 24 * time.Hour)
			}
			f.emit(t, user, syncmodel.EntryCreated, map[string]any{"n": i}, at)
		}
		deleted, err := f.s.RetainChanges(context.Background(), now, 0)
		if err != nil {
			t.Fatal(err)
		}
		if deleted != 3 || f.floor(t, user) != 3 || f.changeCount(t, user) != 3 {
			t.Fatalf("retention: deleted=%d floor=%d left=%d", deleted, f.floor(t, user), f.changeCount(t, user))
		}
		gen := f.generation(t)
		if _, err := f.s.Changes(context.Background(), a, syncmodel.Revision{Generation: gen, Rev: 2}, 0); !errors.Is(err, ErrGone) {
			t.Fatalf("pre-floor cursor: %v", err)
		}
		page, err := f.s.Changes(context.Background(), a, syncmodel.Revision{Generation: gen, Rev: 3}, 0)
		if err != nil {
			t.Fatal(err)
		}
		if got := syncTestRevs(t, page); len(got) != 3 || got[0] != 4 {
			t.Fatalf("post-floor page: %v", got)
		}
		// A second run with nothing old enough keeps the floor.
		deleted, err = f.s.RetainChanges(context.Background(), now, 0)
		if err != nil || deleted != 0 || f.floor(t, user) != 3 {
			t.Fatalf("second run: deleted=%d floor=%d err=%v", deleted, f.floor(t, user), err)
		}
	})
	t.Run("entire_log_deleted", func(t *testing.T) {
		f := newSyncTestFixture(t, template)
		user := f.user(t, "syncTest-retain-all")
		old := time.Now().Add(-120 * 24 * time.Hour)
		for i := range 4 {
			f.emit(t, user, syncmodel.EntryCreated, map[string]any{"n": i}, old)
		}
		deleted, err := f.s.RetainChanges(context.Background(), time.Now(), 0)
		if err != nil {
			t.Fatal(err)
		}
		if deleted != 4 || f.floor(t, user) != 4 || f.changeCount(t, user) != 0 {
			t.Fatalf("all deleted: %d floor=%d left=%d", deleted, f.floor(t, user), f.changeCount(t, user))
		}
		// A cursor exactly at the floor stays valid; below it is gone.
		p := f.principal(t, user, PrincipalHuman, "h")
		a := f.actor(user, p, PrincipalHuman)
		gen := f.generation(t)
		if _, err := f.s.Changes(context.Background(), a, syncmodel.Revision{Generation: gen, Rev: 4}, 0); err != nil {
			t.Fatalf("cursor at max floor: %v", err)
		}
		if _, err := f.s.Changes(context.Background(), a, syncmodel.Revision{Generation: gen, Rev: 3}, 0); !errors.Is(err, ErrGone) {
			t.Fatalf("below floor: %v", err)
		}
	})
	t.Run("clock_backwards_keeps_contiguous_prefix", func(t *testing.T) {
		f := newSyncTestFixture(t, template)
		user := f.user(t, "syncTest-retain-skew")
		now := time.Now()
		old := now.Add(-100 * 24 * time.Hour)
		// rev 3 is older than rev 2: the log's age is not monotonic.
		f.emit(t, user, syncmodel.EntryCreated, map[string]any{"r": 1}, old)
		f.emit(t, user, syncmodel.EntryCreated, map[string]any{"r": 2}, now)
		f.emit(t, user, syncmodel.EntryCreated, map[string]any{"r": 3}, old)
		deleted, err := f.s.RetainChanges(context.Background(), now, 0)
		if err != nil {
			t.Fatal(err)
		}
		if deleted != 1 || f.floor(t, user) != 1 || f.changeCount(t, user) != 2 {
			t.Fatalf("skew retention: deleted=%d floor=%d left=%d", deleted, f.floor(t, user), f.changeCount(t, user))
		}
	})
	t.Run("op_ids_independent_schedule", func(t *testing.T) {
		f := newSyncTestFixture(t, template)
		user := f.user(t, "syncTest-retain-ops")
		p := f.principal(t, user, PrincipalHuman, "h")
		now := time.Now()
		for _, op := range []struct {
			id string
			at time.Time
		}{{"old", now.Add(-100 * 24 * time.Hour)}, {"recent", now.Add(-2 * 24 * time.Hour)}} {
			if _, err := f.db.Pool().Exec(context.Background(), "INSERT INTO op_ids(principal_id,op_id,result,created_at) VALUES($1,$2,'{}',$3)", p, op.id, op.at); err != nil {
				t.Fatal(err)
			}
		}
		f.emit(t, user, syncmodel.EntryCreated, map[string]any{}, now.Add(-2*24*time.Hour))
		// A one-day log retention must not prune two-day-old op_ids.
		deleted, err := f.s.RetainChanges(context.Background(), now, 24*time.Hour)
		if err != nil {
			t.Fatal(err)
		}
		if deleted != 1 {
			t.Fatalf("log retention: %d", deleted)
		}
		var remaining []string
		rows, err := f.db.Pool().Query(context.Background(), "SELECT op_id FROM op_ids ORDER BY op_id")
		if err != nil {
			t.Fatal(err)
		}
		defer rows.Close()
		for rows.Next() {
			var id string
			if err := rows.Scan(&id); err != nil {
				t.Fatal(err)
			}
			remaining = append(remaining, id)
		}
		if len(remaining) != 1 || remaining[0] != "recent" {
			t.Fatalf("op_ids pruned early: %v", remaining)
		}
		if _, err := f.s.RetainChanges(context.Background(), now, -time.Hour); !errors.Is(err, ErrInvalid) {
			t.Fatalf("negative retention: %v", err)
		}
	})
}

func TestSyncRetainEntries(t *testing.T) {
	template := storetest.NewTemplate(t)
	t.Run("tombstones_events_and_stamps", func(t *testing.T) {
		f := newSyncTestFixture(t, template)
		userA := f.user(t, "syncTest-ent-a1")
		userB := f.user(t, "syncTest-ent-b1")
		f.principal(t, userA, PrincipalHuman, "human-a")
		f.principal(t, userB, PrincipalHuman, "human-b")
		feed := f.feed(t, "shared1", 0)
		subA := f.subscribe(t, userA, feed)
		subB := f.subscribe(t, userB, feed)
		now := time.Now()
		old := now.Add(-100 * 24 * time.Hour)
		e1 := f.entryAt(t, feed, 1, old, nil)
		c1 := sha256.Sum256([]byte("c1"))
		if _, err := f.db.Pool().Exec(context.Background(), "INSERT INTO contents(entry_id,content_hash,sanitizer_version,source,html,text) VALUES($1,$2,1,'feed','<p>x</p>','x')", e1, c1[:]); err != nil {
			t.Fatal(err)
		}
		count, err := f.s.RetainEntries(context.Background(), now, 0)
		if err != nil {
			t.Fatal(err)
		}
		if count != 1 {
			t.Fatalf("retained count: %d", count)
		}
		var deleted bool
		if err := f.db.Pool().QueryRow(context.Background(), "SELECT deleted_at IS NOT NULL FROM entries WHERE id=$1", e1).Scan(&deleted); err != nil || !deleted {
			t.Fatalf("entry not tombstoned: %v %v", deleted, err)
		}
		var bodies int64
		if err := f.db.Pool().QueryRow(context.Background(), "SELECT count(*) FROM contents WHERE entry_id=$1", e1).Scan(&bodies); err != nil || bodies != 0 {
			t.Fatalf("contents kept: %d %v", bodies, err)
		}
		// Each subscriber gets one entry.deleted with the entry tombstone
		// carrying the allocated rev, and a matching entry_revisions stamp.
		for user, sub := range map[int64]int64{userA: subA, userB: subB} {
			var rev int64
			var payload []byte
			if err := f.db.Pool().QueryRow(context.Background(), "SELECT rev,payload FROM changes WHERE user_id=$1 AND type='entry.deleted'", user).Scan(&rev, &payload); err != nil {
				t.Fatal(err)
			}
			var doc struct {
				Entry struct {
					ID             int64  `json:"id"`
					SubscriptionID int64  `json:"subscription_id"`
					Rev            uint64 `json:"rev"`
					Deleted        bool   `json:"deleted"`
				} `json:"entry"`
			}
			if err := json.Unmarshal(payload, &doc); err != nil {
				t.Fatal(err)
			}
			if doc.Entry.ID != e1 || doc.Entry.SubscriptionID != sub || doc.Entry.Rev != uint64(rev) || !doc.Entry.Deleted {
				t.Fatalf("tombstone payload: %s", payload)
			}
			var stamped int64
			if err := f.db.Pool().QueryRow(context.Background(), "SELECT rev FROM entry_revisions WHERE user_id=$1 AND entry_id=$2", user, e1).Scan(&stamped); err != nil || stamped != rev {
				t.Fatalf("entry_revisions: %d != %d (%v)", stamped, rev, err)
			}
		}
		coreCheckGapless(t, f.db, userA)
		coreCheckGapless(t, f.db, userB)
		// A second run is a no-op: already tombstoned entries are ignored.
		count, err = f.s.RetainEntries(context.Background(), now, 0)
		if err != nil || count != 0 {
			t.Fatalf("second run: %d %v", count, err)
		}
		if f.changeCount(t, userA) != 1 {
			t.Fatalf("duplicate tombstone events")
		}
	})
	t.Run("kept_later_and_recent_survive", func(t *testing.T) {
		f := newSyncTestFixture(t, template)
		user := f.user(t, "syncTest-ent-k")
		p := f.principal(t, user, PrincipalHuman, "human")
		feed := f.feed(t, "keep", 0)
		f.subscribe(t, user, feed)
		now := time.Now()
		old := now.Add(-100 * 24 * time.Hour)
		kept := f.entryAt(t, feed, 1, old, nil)
		later := f.entryAt(t, feed, 2, old, nil)
		recent := f.entryAt(t, feed, 3, now.Add(-time.Hour), nil)
		ctx := context.Background()
		if _, err := f.db.Pool().Exec(ctx, `INSERT INTO entry_state(principal_id,entry_id,kept,kept_rev,kept_occurred_at,kept_actor,kept_source,state_rev)
  VALUES($1,$2,true,1,$3,$1,'deliberate',1)`, p, kept, now.Add(-time.Hour)); err != nil {
			t.Fatal(err)
		}
		if _, err := f.db.Pool().Exec(ctx, `INSERT INTO entry_state(principal_id,entry_id,shelf,shelf_rev,shelf_occurred_at,shelf_actor,shelf_source,state_rev)
  VALUES($1,$2,'later',2,$3,$1,'deliberate',2)`, p, later, now.Add(-time.Hour)); err != nil {
			t.Fatal(err)
		}
		count, err := f.s.RetainEntries(ctx, now, 0)
		if err != nil {
			t.Fatal(err)
		}
		if count != 0 {
			t.Fatalf("kept/later/recent deleted: %d", count)
		}
		var n int64
		if err := f.db.Pool().QueryRow(ctx, "SELECT count(*) FROM entries WHERE id=ANY($1) AND deleted_at IS NULL", []int64{kept, later, recent}).Scan(&n); err != nil || n != 3 {
			t.Fatalf("protected entries lost: %d %v", n, err)
		}
	})
	t.Run("per_user_retention_days_shared_feed", func(t *testing.T) {
		f := newSyncTestFixture(t, template)
		userA := f.user(t, "syncTest-ent-sa")
		userB := f.user(t, "syncTest-ent-sb")
		f.principal(t, userA, PrincipalHuman, "a")
		f.principal(t, userB, PrincipalHuman, "b")
		feed := f.feed(t, "pers", 0)
		f.subscribe(t, userA, feed)
		f.subscribe(t, userB, feed)
		now := time.Now()
		e := f.entryAt(t, feed, 1, now.Add(-100*24*time.Hour), nil)
		ctx := context.Background()
		if _, err := f.db.Pool().Exec(ctx, "UPDATE users SET settings='{\"retention_days\":200}' WHERE id=$1", userB); err != nil {
			t.Fatal(err)
		}
		count, err := f.s.RetainEntries(ctx, now, 0)
		if err != nil {
			t.Fatal(err)
		}
		if count != 0 {
			t.Fatalf("entry deleted while subscriber still covers it: %d", count)
		}
		if _, err := f.db.Pool().Exec(ctx, "UPDATE users SET settings='{\"retention_days\":30}' WHERE id=$1", userB); err != nil {
			t.Fatal(err)
		}
		count, err = f.s.RetainEntries(ctx, now, 0)
		if err != nil {
			t.Fatal(err)
		}
		if count != 1 {
			t.Fatalf("per-user thresholds not applied: %d", count)
		}
		var tombstoned bool
		if err := f.db.Pool().QueryRow(ctx, "SELECT deleted_at IS NOT NULL FROM entries WHERE id=$1", e).Scan(&tombstoned); err != nil || !tombstoned {
			t.Fatalf("entry not tombstoned: %v", err)
		}
	})
	t.Run("first_seen_age_only_and_orphan", func(t *testing.T) {
		f := newSyncTestFixture(t, template)
		user := f.user(t, "syncTest-ent-fs")
		f.principal(t, user, PrincipalHuman, "h")
		feed := f.feed(t, "fs", 0)
		f.subscribe(t, user, feed)
		orphanFeed := f.feed(t, "orphan", 0)
		now := time.Now()
		recentPub := now.Add(-time.Hour)
		// first_seen age decides even when the publisher timestamp is fresh.
		e1 := f.entryAt(t, feed, 1, now.Add(-100*24*time.Hour), &recentPub)
		e2 := f.entryAt(t, orphanFeed, 2, now.Add(-100*24*time.Hour), nil)
		count, err := f.s.RetainEntries(context.Background(), now, 0)
		if err != nil {
			t.Fatal(err)
		}
		if count != 2 {
			t.Fatalf("first_seen retention: %d", count)
		}
		var n int64
		if err := f.db.Pool().QueryRow(context.Background(), "SELECT count(*) FROM entries WHERE id=ANY($1) AND deleted_at IS NOT NULL", []int64{e1, e2}).Scan(&n); err != nil || n != 2 {
			t.Fatalf("tombstones: %d %v", n, err)
		}
		// The orphan emits no user event; the subscribed one emits exactly one.
		if f.changeCount(t, user) != 1 {
			t.Fatalf("events: %d", f.changeCount(t, user))
		}
		// Rows remain: retention never deletes the identity row.
		var total int64
		if err := f.db.Pool().QueryRow(context.Background(), "SELECT count(*) FROM entries WHERE id=ANY($1)", []int64{e1, e2}).Scan(&total); err != nil || total != 2 {
			t.Fatalf("identity rows removed: %d", total)
		}
		if _, err := f.s.RetainEntries(context.Background(), now, -time.Hour); !errors.Is(err, ErrInvalid) {
			t.Fatalf("negative retention: %v", err)
		}
	})
}
