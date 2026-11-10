package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/L-K-M/Earnest/internal/store/storetest"
)

type subscriptionsProber struct {
	calls atomic.Int64
	probe func(context.Context, string, *FeedCredentials) (string, FeedMeta, []IngestItem, error)
}

func TestNormalizedFeedURLDefaultPorts(t *testing.T) {
	for _, tc := range []struct{ raw, want string }{
		{"http://EXAMPLE.test:80", "http://example.test/"},
		{"https://EXAMPLE.test:443/feed?q=1#fragment", "https://example.test/feed?q=1"},
		{"http://[2001:db8::1]:80/feed", "http://[2001:db8::1]/feed"},
		{"https://[2001:db8::1]:443/feed", "https://[2001:db8::1]/feed"},
		{"https://example.test:80/feed", "https://example.test:80/feed"},
		{"http://example.test:443/feed", "http://example.test:443/feed"},
		{"https://example.test:8443/feed", "https://example.test:8443/feed"},
	} {
		t.Run(tc.raw, func(t *testing.T) {
			got, err := normalizedFeedURL(tc.raw)
			if err != nil || got != tc.want {
				t.Fatalf("normalized URL = %q, want %q: %v", got, tc.want, err)
			}
		})
	}
}

func TestSubscribeDeduplicatesDefaultPorts(t *testing.T) {
	f := newCoreFixture(t, storetest.NewTemplate(t))
	f.s.prober = &subscriptionsProber{}
	ctx := t.Context()
	first, err := f.s.Subscribe(ctx, f.a, "https://feeds.test/rss", SubscribeOptions{OpID: "first"})
	if err != nil {
		t.Fatal(err)
	}
	second, err := f.s.Subscribe(ctx, f.a, "https://feeds.test:443/rss", SubscribeOptions{OpID: "second"})
	if err != nil || first.ID != second.ID || first.FeedID != second.FeedID {
		t.Fatalf("equivalent URL created another subscription: %+v %+v %v", first, second, err)
	}
}

// Resubscribing with credentials must not stack a second subscription on a
// parallel feed row: the existing subscription rebinds to the user's own
// credential-bearing feed, the same conversion UpdateSubscription performs.
func TestSubscribeSameURLWithCredentials(t *testing.T) {
	f := newCoreFixture(t, storetest.NewTemplate(t))
	f.s.prober = &subscriptionsProber{}
	ctx := context.Background()
	first, err := f.s.Subscribe(ctx, f.a, "https://feeds.test/dup", SubscribeOptions{OpID: "first"})
	if err != nil {
		t.Fatal(err)
	}
	creds := &FeedCredentials{Username: "reader", Password: "very-secret"}
	second, err := f.s.Subscribe(ctx, f.a, "https://feeds.test/dup", SubscribeOptions{OpID: "second", Credentials: creds})
	if err != nil {
		t.Fatal(err)
	}
	if second.ID != first.ID || !second.Private || second.FeedID == first.FeedID {
		t.Fatalf("credentials did not rebind the existing subscription: %+v -> %+v", first, second)
	}
	var subs int
	if err := f.db.Pool().QueryRow(ctx, `SELECT count(*) FROM subscriptions s JOIN feeds f ON f.id=s.feed_id WHERE s.user_id=$1 AND s.deleted_at IS NULL AND f.url='https://feeds.test/dup'`, f.a.UserID).Scan(&subs); err != nil || subs != 1 {
		t.Fatalf("subscriptions for URL: %d %v", subs, err)
	}
	var feeds int
	if err := f.db.Pool().QueryRow(ctx, "SELECT count(*) FROM feeds WHERE url='https://feeds.test/dup'").Scan(&feeds); err != nil || feeds != 2 {
		t.Fatalf("feed rows for URL: %d %v", feeds, err)
	}
	var envelope []byte
	if err := f.db.Pool().QueryRow(ctx, "SELECT credentials_encrypted FROM feeds WHERE id=$1", second.FeedID).Scan(&envelope); err != nil {
		t.Fatal(err)
	}
	decoded, err := f.s.openCredentials(envelope)
	if err != nil || decoded.Password != creds.Password {
		t.Fatal("credentials not bound to private feed", err)
	}
	// Rotated credentials on the bound feed keep the same subscription.
	rotated := &FeedCredentials{Username: "reader", Password: "rotated"}
	third, err := f.s.Subscribe(ctx, f.a, "https://feeds.test/dup", SubscribeOptions{OpID: "third", Credentials: rotated})
	if err != nil || third.ID != first.ID || third.FeedID != second.FeedID {
		t.Fatalf("rotated credentials changed binding: %+v %v", third, err)
	}
	if err := f.db.Pool().QueryRow(ctx, "SELECT credentials_encrypted FROM feeds WHERE id=$1", second.FeedID).Scan(&envelope); err != nil {
		t.Fatal(err)
	}
	decoded, err = f.s.openCredentials(envelope)
	if err != nil || decoded.Password != rotated.Password {
		t.Fatal("credentials not rotated", err)
	}
	coreCheckGapless(t, f.db, f.a.UserID)
}

func TestRebindPreservesCanonicalFeedMetadata(t *testing.T) {
	template := storetest.NewTemplate(t)
	for _, private := range []bool{false, true} {
		t.Run(fmt.Sprintf("initial_private_%v", private), func(t *testing.T) {
			f := newCoreFixture(t, template)
			ctx := t.Context()
			f.s.prober = &subscriptionsProber{}
			opts := SubscribeOptions{Title: "Personal title", OpID: "subscribe"}
			creds := &FeedCredentials{Username: "reader", Password: "secret"}
			if private {
				opts.Credentials = creds
			}
			sub, err := f.s.Subscribe(ctx, f.a, "https://feeds.test/rebind", opts)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := f.db.Pool().Exec(ctx, "UPDATE feeds SET title='Publisher title',site_url='https://publisher.test/',language='fr' WHERE id=$1", sub.FeedID); err != nil {
				t.Fatal(err)
			}
			update := UpdateSubscriptionOptions{CredentialsSet: true, OpID: "rebind"}
			if !private {
				update.Credentials = creds
			}
			rebound, err := f.s.UpdateSubscription(ctx, f.a, sub.ID, update)
			if err != nil || rebound.Title != opts.Title || rebound.FeedID == sub.FeedID {
				t.Fatalf("rebind: %+v %v", rebound, err)
			}
			var title, site, language string
			if err := f.db.Pool().QueryRow(ctx, "SELECT title,site_url,language FROM feeds WHERE id=$1", rebound.FeedID).Scan(&title, &site, &language); err != nil {
				t.Fatal(err)
			}
			if title != "Publisher title" || site != "https://publisher.test/" || language != "fr" {
				t.Fatalf("canonical metadata polluted by subscription: title=%q site=%q language=%q", title, site, language)
			}
			empty := ""
			cleared, err := f.s.UpdateSubscription(ctx, f.a, sub.ID, UpdateSubscriptionOptions{Title: &empty, OpID: "clear-title"})
			if err != nil || cleared.Title != "Publisher title" {
				t.Fatalf("removing override did not reveal publisher title: %+v %v", cleared, err)
			}
		})
	}
}

func (p *subscriptionsProber) Probe(ctx context.Context, raw string, credentials *FeedCredentials) (string, FeedMeta, []IngestItem, error) {
	p.calls.Add(1)
	if p.probe != nil {
		return p.probe(ctx, raw, credentials)
	}
	return raw, FeedMeta{Title: "Feed", SiteURL: "https://feeds.test/"}, nil, nil
}
func subscriptionsOther(t *testing.T, f *coreFixture, name string) Actor {
	t.Helper()
	a := f.a
	if err := f.db.Pool().QueryRow(context.Background(), "INSERT INTO users(username,password_hash) VALUES($1,'$argon2id$test') RETURNING id", name).Scan(&a.UserID); err != nil {
		t.Fatal(err)
	}
	if err := f.db.Pool().QueryRow(context.Background(), "INSERT INTO principals(user_id,kind,name) VALUES($1,'human','Human') RETURNING id", a.UserID).Scan(&a.PrincipalID); err != nil {
		t.Fatal(err)
	}
	return a
}

func TestSubscriptions(t *testing.T) {
	template := storetest.NewTemplate(t)
	t.Run("shared_public_private_encryption_and_updates", func(t *testing.T) {
		f := newCoreFixture(t, template)
		p := &subscriptionsProber{}
		f.s.prober = p
		ctx := context.Background()
		other := subscriptionsOther(t, f, "other")
		sub, err := f.s.Subscribe(ctx, f.a, "https://feeds.test/rss", SubscribeOptions{OpID: "public"})
		if err != nil {
			t.Fatal(err)
		}
		shared, err := f.s.Subscribe(ctx, other, "https://feeds.test/rss", SubscribeOptions{OpID: "shared"})
		if err != nil || sub.FeedID != shared.FeedID {
			t.Fatal("public feed not shared", sub, shared, err)
		}
		replay, err := f.s.Subscribe(ctx, f.a, "https://feeds.test/rss", SubscribeOptions{OpID: "public"})
		if err != nil || replay.ID != sub.ID || p.calls.Load() != 2 {
			t.Fatal("subscribe replay probed", replay, err, p.calls.Load())
		}
		credentials := &FeedCredentials{Username: "reader", Password: "very-secret", Headers: map[string]string{"X-Token": "hidden"}}
		private, err := f.s.Subscribe(ctx, f.a, "https://feeds.test/rss", SubscribeOptions{OpID: "private", Credentials: credentials, Extract: ExtractAlways, Cadence: CadenceWeek})
		if err != nil || !private.Private || private.FeedID == sub.FeedID {
			t.Fatal("private sharing", private, err)
		}
		otherPrivate, err := f.s.Subscribe(ctx, other, "https://feeds.test/rss", SubscribeOptions{OpID: "other-private", Credentials: credentials})
		if err != nil || otherPrivate.FeedID == private.FeedID {
			t.Fatal("private crosses users", err)
		}
		var envelope []byte
		if err := f.db.Pool().QueryRow(ctx, "SELECT credentials_encrypted FROM feeds WHERE id=$1", private.FeedID).Scan(&envelope); err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(envelope), credentials.Password) {
			t.Fatal("plaintext credentials")
		}
		decoded, err := f.s.openCredentials(envelope)
		if err != nil || decoded.Password != credentials.Password {
			t.Fatal("credential round trip", err)
		}
		title := "My title"
		cadence := CadenceNow
		extract := ExtractNone
		updated, err := f.s.UpdateSubscription(ctx, f.a, sub.ID, UpdateSubscriptionOptions{Title: &title, Cadence: &cadence, Extract: &extract, OpID: "update"})
		if err != nil || updated.Title != title || updated.Cadence != cadence || updated.Extract != extract {
			t.Fatal("subscription patch", updated, err)
		}
		got, err := f.s.Subscription(ctx, f.a, sub.ID)
		if err != nil || got.Rev != updated.Rev {
			t.Fatal("get subscription", got, err)
		}
		listed, err := f.s.Subscriptions(ctx, f.a)
		if err != nil || len(listed) != 2 {
			t.Fatal("list subscriptions", listed, err)
		}
		if _, err := f.s.Subscription(ctx, other, sub.ID); !errors.Is(err, ErrNotFound) {
			t.Fatal("cross user read", err)
		}
		if _, err := f.s.UpdateSubscription(ctx, other, sub.ID, UpdateSubscriptionOptions{OpID: "forbidden"}); !errors.Is(err, ErrNotFound) {
			t.Fatal("cross user update", err)
		}
		// A new URL can transition public -> private -> public without changing
		// ownership of a shared feed or disclosing credentials to another user.
		transition, err := f.s.Subscribe(ctx, f.a, "https://feeds.test/transition", SubscribeOptions{OpID: "transition"})
		if err != nil {
			t.Fatal(err)
		}
		publicFeed := transition.FeedID
		transition, err = f.s.UpdateSubscription(ctx, f.a, transition.ID, UpdateSubscriptionOptions{Credentials: credentials, OpID: "make-private"})
		if err != nil || !transition.Private || transition.FeedID == publicFeed {
			t.Fatal("privacy rebind", transition, err)
		}
		transition, err = f.s.UpdateSubscription(ctx, f.a, transition.ID, UpdateSubscriptionOptions{CredentialsSet: true, OpID: "make-public"})
		if err != nil || transition.Private || transition.FeedID != publicFeed {
			t.Fatal("public rebind", transition, err)
		}
		if err := f.s.Unsubscribe(ctx, f.a, sub.ID, "unsubscribe"); err != nil {
			t.Fatal(err)
		}
		if err := f.s.Unsubscribe(ctx, f.a, sub.ID, "unsubscribe"); err != nil {
			t.Fatal("unsubscribe replay", err)
		}
		if _, err := f.s.Subscription(ctx, f.a, sub.ID); !errors.Is(err, ErrNotFound) {
			t.Fatal("deleted visible", err)
		}
		if _, err := f.s.Subscription(ctx, other, shared.ID); err != nil {
			t.Fatal("unsubscribe affected other user", err)
		}
		coreCheckGapless(t, f.db, f.a.UserID)
		coreCheckGapless(t, f.db, other.UserID)
	})
	t.Run("folders_tombstones_and_scope", func(t *testing.T) {
		f := newCoreFixture(t, template)
		f.s.prober = &subscriptionsProber{}
		ctx := context.Background()
		folder, err := f.s.CreateFolder(ctx, f.a, "Tech", "create")
		if err != nil {
			t.Fatal(err)
		}
		replay, err := f.s.CreateFolder(ctx, f.a, "Tech", "create")
		if err != nil || replay.ID != folder.ID {
			t.Fatal("folder replay", err)
		}
		renamed, err := f.s.RenameFolder(ctx, f.a, folder.ID, "Reading", "rename")
		if err != nil || renamed.Name != "Reading" || renamed.Rev <= folder.Rev {
			t.Fatal("rename", renamed, err)
		}
		folders, err := f.s.Folders(ctx, f.a)
		if err != nil || len(folders) != 1 {
			t.Fatal(folders, err)
		}
		sub, err := f.s.Subscribe(ctx, f.a, "https://feeds.test/folder", SubscribeOptions{FolderID: folder.ID, OpID: "sub"})
		if err != nil {
			t.Fatal(err)
		}
		if err := f.s.DeleteFolder(ctx, f.a, folder.ID, "delete"); err != nil {
			t.Fatal(err)
		}
		if err := f.s.DeleteFolder(ctx, f.a, folder.ID, "delete"); err != nil {
			t.Fatal(err)
		}
		got, err := f.s.Subscription(ctx, f.a, sub.ID)
		if err != nil || got.FolderID != 0 || got.Rev <= sub.Rev {
			t.Fatal("folder move not revisioned", got, err)
		}
		page, err := f.s.Bootstrap(ctx, f.a, BootstrapRequest{Kind: BootstrapFolders})
		if err != nil || len(page.Items) != 1 || !strings.Contains(string(page.Items[0]), `"deleted": true`) {
			t.Fatal("folder tombstone", page, err)
		}
		agent := Actor{UserID: f.a.UserID, Kind: PrincipalAgent, Via: CredentialAgentToken, Scopes: []Scope{ScopeEntriesRead, ScopeFeedsWrite}}
		if err := f.db.Pool().QueryRow(ctx, "INSERT INTO principals(user_id,kind,name) VALUES($1,'agent','Bot') RETURNING id", f.a.UserID).Scan(&agent.PrincipalID); err != nil {
			t.Fatal(err)
		}
		p := f.s.prober.(*subscriptionsProber)
		before := p.calls.Load()
		if _, err := f.s.Subscribe(ctx, agent, "https://attacker.test/", SubscribeOptions{OpID: "agent"}); !errors.Is(err, ErrForbidden) {
			t.Fatal("agent caused fetch", err)
		}
		if p.calls.Load() != before {
			t.Fatal("agent reached prober")
		}
		read, err := f.s.Subscriptions(ctx, agent)
		if err != nil || len(read) != 2 {
			t.Fatal("agent cannot read all feeds", read, err)
		}
		coreCheckGapless(t, f.db, f.a.UserID)
	})
	t.Run("initial_history_and_later_backdated_arrival", func(t *testing.T) {
		f := newCoreFixture(t, template)
		now := f.s.now()
		oldTime := now.Add(-30 * 24 * time.Hour)
		recentTime := now.Add(-time.Hour)
		old := coreItem("old")
		old.PublishedAt = &oldTime
		recent := coreItem("recent")
		recent.PublishedAt = &recentTime
		f.s.prober = &subscriptionsProber{probe: func(context.Context, string, *FeedCredentials) (string, FeedMeta, []IngestItem, error) {
			return "https://feeds.test/history", FeedMeta{Title: "History"}, []IngestItem{old, recent}, nil
		}}
		sub, err := f.s.Subscribe(context.Background(), f.a, "https://feeds.test/history", SubscribeOptions{OpID: "history"})
		if err != nil {
			t.Fatal(err)
		}
		river, err := f.s.Entries(context.Background(), f.a, EntryFilter{SubscriptionID: sub.ID})
		if err != nil || len(river) != 1 || river[0].Title != "recent" {
			t.Fatal("historical import flooded river", river, err)
		}
		later := coreItem("new-backdated")
		later.PublishedAt = &oldTime
		if _, err := f.s.RecordFetch(context.Background(), FetchOutcome{FeedID: sub.FeedID, Status: FetchOK, Items: []IngestItem{old, recent, later}}); err != nil {
			t.Fatal(err)
		}
		river, err = f.s.Entries(context.Background(), f.a, EntryFilter{SubscriptionID: sub.ID})
		if err != nil || len(river) != 2 {
			t.Fatal("later backdated arrival hidden", river, err)
		}
		position, err := f.s.Position(context.Background(), f.a, "river:today")
		if err != nil || position.Seq != 0 {
			t.Fatal("initial position", position, err)
		}
		items := []IngestItem{}
		for i := range 21 {
			item := coreItem(fmt.Sprintf("firehose-%d", i))
			item.PublishedAt = &recentTime
			items = append(items, item)
		}
		if _, err := f.s.RecordFetch(context.Background(), FetchOutcome{FeedID: sub.FeedID, Status: FetchOK, Items: items}); err != nil {
			t.Fatal(err)
		}
		got, err := f.s.Subscription(context.Background(), f.a, sub.ID)
		if err != nil || !got.Health.Firehose {
			t.Fatal("firehose not reported", got, err)
		}
		coreCheckGapless(t, f.db, f.a.UserID)
	})
	t.Run("opml_deferred_nested_per_outline_and_export", func(t *testing.T) {
		f := newCoreFixture(t, template)
		p := &subscriptionsProber{}
		f.s.prober = p
		input := []OPMLOutline{{Title: "Tech", Outlines: []OPMLOutline{{Title: "One", XMLURL: "https://feeds.test/one", HTMLURL: "https://feeds.test/"}, {Title: "Nested", Outlines: []OPMLOutline{{Title: "Two", XMLURL: "https://feeds.test/two"}}}, {Title: "Bad", XMLURL: "file:///secret"}}}}
		results, err := f.s.ImportOPML(context.Background(), f.a, input)
		if err != nil || len(results) != 5 || results[1].Subscription == nil || !results[1].Deferred || results[4].Error == "" {
			t.Fatal("import results", results, err)
		}
		if p.calls.Load() != 0 {
			t.Fatal("import burst probes")
		}
		first, err := f.s.ExportOPML(context.Background(), f.a)
		if err != nil || len(first) != 3 {
			t.Fatal("export", first, err)
		}
		repeat, err := f.s.ImportOPML(context.Background(), f.a, input)
		if err != nil {
			t.Fatal(err)
		}
		for _, result := range repeat {
			if result.Subscription != nil && result.Error != "" {
				t.Fatal("repeat import errored:", result.Error)
			}
			if result.Subscription != nil && result.Deferred {
				t.Fatal("repeat import marked an existing feed deferred")
			}
		}
		second, err := f.s.ExportOPML(context.Background(), f.a)
		b1, _ := json.Marshal(first)
		b2, _ := json.Marshal(second)
		if err != nil || string(b1) != string(b2) {
			t.Fatal("repeat import duplicated resources", string(b1), string(b2), err)
		}
		due, err := f.s.ClaimDueFeeds(context.Background(), f.s.now().Add(time.Hour), 100, time.Minute)
		if err != nil || len(due) < 2 {
			t.Fatal("import feeds not due", due, err)
		}
		if err := f.s.DeleteFolder(context.Background(), f.a, results[0].Folder.ID, "delete-imported"); err != nil {
			t.Fatal(err)
		}
		if _, err := f.s.ImportOPML(context.Background(), f.a, input); err != nil {
			t.Fatal("import after folder deletion", err)
		}
		coreCheckGapless(t, f.db, f.a.UserID)
	})
	t.Run("opml_chunked_import_keeps_siblings_after_a_failure", func(t *testing.T) {
		f := newCoreFixture(t, template)
		input := make([]OPMLOutline, opmlChunkSize*2+10)
		for i := range input {
			input[i] = OPMLOutline{Title: fmt.Sprintf("Chunk feed %d", i), XMLURL: fmt.Sprintf("https://chunk.test/%d", i)}
		}
		input[opmlChunkSize].XMLURL = "file:///secret"
		results, err := f.s.ImportOPML(context.Background(), f.a, input)
		if err != nil {
			t.Fatal(err)
		}
		if len(results) != len(input) {
			t.Fatalf("results=%d want %d", len(results), len(input))
		}
		if results[opmlChunkSize].Error == "" {
			t.Fatal("bad outline did not report an error")
		}
		for i, result := range results {
			if i == opmlChunkSize {
				continue
			}
			if result.Subscription == nil || !result.Deferred {
				t.Fatalf("outline %d not subscribed: %+v", i, result)
			}
		}
		subs, err := f.s.Subscriptions(context.Background(), f.a)
		if err != nil {
			t.Fatal(err)
		}
		imported := 0
		for _, sub := range subs {
			if sub.ID != f.subscription {
				imported++
			}
		}
		if imported != len(input)-1 {
			t.Fatalf("imported=%d want %d", imported, len(input)-1)
		}
		coreCheckGapless(t, f.db, f.a.UserID)
	})
	t.Run("opml_folder_at_chunk_boundary_parents_next_chunk_children", func(t *testing.T) {
		f := newCoreFixture(t, template)
		input := make([]OPMLOutline, opmlChunkSize-1)
		for i := range input {
			input[i] = OPMLOutline{Title: fmt.Sprintf("Boundary feed %d", i), XMLURL: fmt.Sprintf("https://boundary.test/%d", i)}
		}
		folder := OPMLOutline{Title: "Boundary folder"}
		for i := 0; i < 3; i++ {
			folder.Outlines = append(folder.Outlines, OPMLOutline{Title: fmt.Sprintf("Child %d", i), XMLURL: fmt.Sprintf("https://boundary.test/child/%d", i)})
		}
		input = append(input, folder)
		results, err := f.s.ImportOPML(context.Background(), f.a, input)
		if err != nil {
			t.Fatal(err)
		}
		folderIdx := opmlChunkSize - 1
		if results[folderIdx].Folder == nil {
			t.Fatal("boundary folder not created")
		}
		for i := folderIdx + 1; i < folderIdx+4; i++ {
			if results[i].Subscription == nil || results[i].Subscription.FolderID != results[folderIdx].Folder.ID {
				t.Fatalf("child %d not placed in boundary folder: %+v", i, results[i])
			}
		}
		coreCheckGapless(t, f.db, f.a.UserID)
	})
}

func TestSubscribeBackfillBoundsLargeFeed(t *testing.T) {
	f := newCoreFixture(t, storetest.NewTemplate(t))
	f.s.prober = &subscriptionsProber{}
	ctx := context.Background()
	feed := entriesTestFeed(t, f.db, "https://feeds.test/large", 0)
	recent := f.s.now().Add(-time.Hour)
	old := f.s.now().Add(-(bootstrapWindow + 24*time.Hour))
	// Five entries older than the bootstrap window, then more in-window
	// entries than the backfill bound: the covered set must be the newest
	// in-window seqs, with the oldest seqs of each group left unstamped.
	const total = subscriptionBackfillLimit + 10
	if _, err := f.db.Pool().Exec(ctx, `INSERT INTO entries(feed_id,seq,identity_hash,first_seen_at,published_at,title)
SELECT $1,base+g,decode(md5('bf-'||g)||md5(g||'-bf'),'hex'),CASE WHEN g<=5 THEN $3::timestamptz ELSE $2::timestamptz END,CASE WHEN g>$4-3 THEN $2::timestamptz END,'entry-'||g
FROM (SELECT coalesce(max(seq),0) AS base FROM entries) b,generate_series(1,$4) g`, feed, recent, old, total); err != nil {
		t.Fatal(err)
	}
	sub, err := f.s.Subscribe(ctx, f.a, "https://feeds.test/large", SubscribeOptions{OpID: "large-feed"})
	if err != nil {
		t.Fatal(err)
	}
	var stamped, events, minSeq, maxSeq int64
	if err := f.db.Pool().QueryRow(ctx, `SELECT count(*),coalesce(min(e.seq),0),coalesce(max(e.seq),0) FROM entry_revisions er JOIN entries e ON e.id=er.entry_id WHERE er.user_id=$1`, f.a.UserID).Scan(&stamped, &minSeq, &maxSeq); err != nil {
		t.Fatal(err)
	}
	var base int64
	if err := f.db.Pool().QueryRow(ctx, "SELECT min(seq) FROM entries WHERE feed_id=$1", feed).Scan(&base); err != nil {
		t.Fatal(err)
	}
	if stamped != subscriptionBackfillLimit || minSeq != base+10 || maxSeq != base+total-1 {
		t.Fatalf("backfill stamps %d over seq %d..%d, want %d over %d..%d", stamped, minSeq, maxSeq, subscriptionBackfillLimit, base+10, base+total-1)
	}
	if err := f.db.Pool().QueryRow(ctx, "SELECT count(*) FROM changes WHERE user_id=$1 AND type='entry.created'", f.a.UserID).Scan(&events); err != nil {
		t.Fatal(err)
	}
	if events != subscriptionBackfillLimit {
		t.Fatalf("entry.created events = %d, want %d", events, subscriptionBackfillLimit)
	}
	// The river still surfaces the whole in-window backlog; the five the
	// bound skipped appear among the stamped ones at rev 0.
	river, err := f.s.Entries(ctx, f.a, EntryFilter{View: ViewRiver, SubscriptionID: sub.ID, Limit: 100})
	if err != nil || len(river) != 100 {
		t.Fatalf("river: %d entries %v", len(river), err)
	}
	var unstamped, stampedRiver int
	for _, e := range river {
		if e.Rev == 0 {
			unstamped++
		} else {
			stampedRiver++
		}
	}
	if unstamped != 5 || stampedRiver != 95 {
		t.Fatalf("river revs: %d unstamped %d stamped, want 5 and 95", unstamped, stampedRiver)
	}
	// Entries the bound skipped still list through the history view at rev 0.
	history, err := f.s.Entries(ctx, f.a, EntryFilter{View: ViewAll, SubscriptionID: sub.ID, BeforeSeq: uint64(base + 10)})
	if err != nil || len(history) != 10 {
		t.Fatalf("history: %+v %v", history, err)
	}
	for _, e := range history {
		if e.Rev != 0 {
			t.Fatalf("unstamped entry %d rev = %d", e.ID, e.Rev)
		}
	}
	// Bootstrap still pages every in-window entry, unstamped ones at rev 0.
	var items, zeroRev int
	cursor := BootstrapRequest{Kind: BootstrapEntries, Limit: 1000}
	for {
		page, err := f.s.Bootstrap(ctx, f.a, cursor)
		if err != nil {
			t.Fatal(err)
		}
		for _, body := range page.Items {
			var entry struct {
				Rev uint64 `json:"rev"`
			}
			if err := json.Unmarshal(body, &entry); err != nil {
				t.Fatal(err)
			}
			items++
			if entry.Rev == 0 {
				zeroRev++
			}
		}
		if !page.HasMore {
			break
		}
		cursor.AfterID = page.NextID
	}
	if items != total-5 || zeroRev != 5 {
		t.Fatalf("bootstrap: %d items %d unstamped, want %d items 5 unstamped", items, zeroRev, total-5)
	}
	coreCheckGapless(t, f.db, f.a.UserID)
}

func TestSubscriptionValidation(t *testing.T) {
	for _, raw := range []string{"ftp://feeds.test/rss", "https://user:secret@feeds.test/", "not a URL", "https:///missing"} {
		if _, err := normalizedFeedURL(raw); !errors.Is(err, ErrInvalid) {
			t.Fatalf("accepted %q: %v", raw, err)
		}
	}
	for _, options := range []SubscribeOptions{{OpID: ""}, {OpID: "op", Cadence: "year"}, {OpID: "op", Extract: "random"}, {OpID: "op", FolderID: -1}} {
		if err := validateSubscribe(options); !errors.Is(err, ErrInvalid) {
			t.Fatal("invalid options accepted", options, err)
		}
	}
}

// The REST projection reports a muted subscription so clients can leave it
// out of Today-wide range marks, as the sync projection already lets the
// native clients do.
func TestSubscriptionsReportMuted(t *testing.T) {
	f := newCoreFixture(t, storetest.NewTemplate(t))
	f.s.prober = &subscriptionsProber{}
	ctx := t.Context()
	sub, err := f.s.Subscribe(ctx, f.a, "https://feeds.test/muted", SubscribeOptions{OpID: "muted"})
	if err != nil {
		t.Fatal(err)
	}
	if sub.Muted {
		t.Fatalf("new subscription reported muted: %+v", sub)
	}
	if _, err := f.db.Pool().Exec(ctx, `UPDATE subscriptions SET muted=true WHERE id=$1`, sub.ID); err != nil {
		t.Fatal(err)
	}
	subs, err := f.s.Subscriptions(ctx, f.a)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, got := range subs {
		if got.ID != sub.ID {
			continue
		}
		found = true
		if !got.Muted {
			t.Fatalf("muted subscription listed as unmuted: %+v", got)
		}
	}
	if !found {
		t.Fatalf("subscription %d missing from the list", sub.ID)
	}
}
