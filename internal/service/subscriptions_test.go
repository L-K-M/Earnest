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
		if err != nil || len(listed) != 3 {
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
