package service

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"strings"
	"testing"

	"github.com/L-K-M/Earnest/internal/store"
	"github.com/L-K-M/Earnest/internal/store/storetest"
)

// The expected value below was computed independently of this code over the
// Standard Webhooks signed content "id.timestamp.body" using HMAC-SHA256.
func TestWebhookSignature(t *testing.T) {
	secret := "whsec_MDEyMzQ1Njc4OWFiY2RlZjAxMjM0NTY3ODlhYmNkZWY="
	body := []byte(`{"type":"entry.matched","timestamp":"2026-11-02T12:00:00Z","data":{"feed_id":7,"entry_id":42}}`)
	sig, err := signWebhook(secret, "msg_0123456789abcdef", 1700000000, body)
	if err != nil {
		t.Fatal(err)
	}
	if sig != "v1,GD5nuUG7J7z1ndTf3VRdP9KaIvhRf1xuDxSUlClwJS4=" {
		t.Fatalf("known answer: %s", sig)
	}
	// Independently re-derive from the decoded secret to cover key handling.
	key, err := decodeWebhookSecret(secret)
	if err != nil {
		t.Fatal(err)
	}
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte("msg_0123456789abcdef.1700000000."))
	mac.Write(body)
	want := "v1," + base64.StdEncoding.EncodeToString(mac.Sum(nil))
	if sig != want {
		t.Fatalf("independent signature: %s want %s", sig, want)
	}
	for _, bad := range []string{"", "plain", "whsec_", "whsec_!!!", "whsec_" + base64.StdEncoding.EncodeToString([]byte("short"))} {
		if _, err := signWebhook(bad, "m", 0, body); !errors.Is(err, ErrInvalid) {
			t.Fatalf("secret %q accepted: %v", bad, err)
		}
	}
}

func TestWebhookSecretEnvelope(t *testing.T) {
	s, err := New(&store.Store{}, Deps{SecretKey: []byte(strings.Repeat("k", 32))})
	if err != nil {
		t.Fatal(err)
	}
	secret, err := newWebhookSecret()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(secret, "whsec_") {
		t.Fatal("secret lacks whsec_ prefix")
	}
	sealed, err := s.sealWebhookSecret(secret)
	if err != nil {
		t.Fatal(err)
	}
	again, err := s.sealWebhookSecret(secret)
	if err != nil {
		t.Fatal(err)
	}
	if string(sealed) == string(again) || strings.Contains(string(sealed), secret) {
		t.Fatal("webhook secrets are not randomly encrypted")
	}
	got, err := s.openWebhookSecret(sealed)
	if err != nil || got != secret {
		t.Fatal("webhook secret roundtrip", err)
	}
	sealed[len(sealed)-1] ^= 1
	if _, err := s.openWebhookSecret(sealed); !errors.Is(err, ErrUnavailable) {
		t.Fatal("tampered envelope accepted", err)
	}
	// The feed-credentials purpose must not open a webhook envelope either.
	feedSealed, err := s.sealCredentials(&FeedCredentials{Username: "u", Password: "p"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.openWebhookSecret(feedSealed); !errors.Is(err, ErrUnavailable) {
		t.Fatal("cross-purpose envelope accepted", err)
	}
}

func TestWebhookValidation(t *testing.T) {
	for _, tc := range []struct {
		name string
		ev   WebhookEvent
		ok   bool
	}{
		{"feed", WebhookEvent{Type: WebhookFeedFailing, FeedID: 3}, true},
		{"edition", WebhookEvent{Type: WebhookEditionReady, EditionID: 9}, true},
		{"entry", WebhookEvent{Type: WebhookEntryMatched, FeedID: 3, EntryID: 4}, true},
		{"unknown", WebhookEvent{Type: "entry.created", FeedID: 3}, false},
		{"feed_missing", WebhookEvent{Type: WebhookFeedFailing}, false},
		{"feed_extra", WebhookEvent{Type: WebhookFeedFailing, FeedID: 3, EntryID: 4}, false},
		{"entry_no_feed", WebhookEvent{Type: WebhookEntryMatched, EntryID: 4}, false},
		{"edition_extra", WebhookEvent{Type: WebhookEditionReady, EditionID: 9, FeedID: 3}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.ev.validate()
			if tc.ok != (err == nil) {
				t.Fatalf("validate: %v", err)
			}
		})
	}
	for _, tc := range []struct {
		name   string
		events []string
		ok     bool
	}{
		{"one", []string{"feed.failing"}, true},
		{"all", []string{"feed.failing", "edition.ready", "entry.matched"}, true},
		{"empty", nil, false},
		{"unknown", []string{"entry.created"}, false},
		{"dupe", []string{"feed.failing", "feed.failing"}, false},
	} {
		t.Run("events_"+tc.name, func(t *testing.T) {
			if validWebhookEvents(tc.events) != tc.ok {
				t.Fatal(tc.events)
			}
		})
	}
}

func webhookAgent(t *testing.T, f *coreFixture) Actor {
	t.Helper()
	agent := Actor{UserID: f.a.UserID, Kind: PrincipalAgent, Via: CredentialAgentToken, Scopes: []Scope{ScopeWebhooksWrite}}
	if err := f.db.Pool().QueryRow(context.Background(), "INSERT INTO principals(user_id,kind,name) VALUES($1,'agent','Bot') RETURNING id", f.a.UserID).Scan(&agent.PrincipalID); err != nil {
		t.Fatal(err)
	}
	return agent
}

func TestWebhookCRUD(t *testing.T) {
	template := storetest.NewTemplate(t)
	ctx := context.Background()
	t.Run("create_list_update_delete", func(t *testing.T) {
		f := newCoreFixture(t, template)
		in := WebhookInput{URL: "https://hooks.example.test/endpoint?token=abc", Events: []string{"feed.failing", "entry.matched"}}
		created, err := f.s.CreateWebhook(ctx, f.a, in, "create-1")
		if err != nil {
			t.Fatal(err)
		}
		if !strings.HasPrefix(created.Secret, "whsec_") || created.ID <= 0 || !created.Active || created.Rev != 1 {
			t.Fatalf("created: %+v", created)
		}
		var sealed []byte
		if err := f.db.Pool().QueryRow(ctx, "SELECT secret_encrypted FROM webhooks WHERE id=$1", created.ID).Scan(&sealed); err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(sealed), created.Secret) || strings.Contains(string(sealed), "whsec_") {
			t.Fatal("signing secret stored unencrypted")
		}
		replay, err := f.s.CreateWebhook(ctx, f.a, in, "create-1")
		if err != nil || replay.ID != created.ID || replay.Secret != "" {
			t.Fatal("op replay must not re-issue the secret", replay, err)
		}
		var count int
		if err := f.db.Pool().QueryRow(ctx, "SELECT count(*) FROM webhooks").Scan(&count); err != nil || count != 1 {
			t.Fatal("replay inserted a second webhook", count, err)
		}
		list, err := f.s.Webhooks(ctx, f.a)
		if err != nil || len(list) != 1 || list[0].ID != created.ID || list[0].URL != in.URL {
			t.Fatal("list", list, err)
		}
		updated, err := f.s.UpdateWebhook(ctx, f.a, created.ID, WebhookPatch{Events: []string{"edition.ready"}}, "patch-1")
		if err != nil || updated.Rev != 2 || len(updated.Events) != 1 || updated.Events[0] != "edition.ready" {
			t.Fatal("update", updated, err)
		}
		str := "https://hooks.example.test/other"
		updated, err = f.s.UpdateWebhook(ctx, f.a, created.ID, WebhookPatch{URL: &str}, "patch-2")
		if err != nil || updated.URL != str || updated.Events[0] != "edition.ready" {
			t.Fatal("partial update lost fields", updated, err)
		}
		replayed, err := f.s.UpdateWebhook(ctx, f.a, created.ID, WebhookPatch{URL: &str}, "patch-2")
		if err != nil || replayed.URL != str || replayed.Rev != updated.Rev {
			t.Fatal("update replay", replayed, err)
		}
		if err := f.s.DeleteWebhook(ctx, f.a, created.ID, "del-1"); err != nil {
			t.Fatal(err)
		}
		if err := f.db.Pool().QueryRow(ctx, "SELECT count(*) FROM webhooks").Scan(&count); err != nil || count != 0 {
			t.Fatal("delete", count, err)
		}
		if err := f.s.DeleteWebhook(ctx, f.a, created.ID, "del-1"); err != nil {
			t.Fatal("delete replay", err)
		}
		if err := f.s.DeleteWebhook(ctx, f.a, created.ID, "del-2"); !errors.Is(err, ErrNotFound) {
			t.Fatal("missing delete", err)
		}
	})
	t.Run("permissions", func(t *testing.T) {
		f := newCoreFixture(t, template)
		in := WebhookInput{URL: "https://hooks.example.test/", Events: []string{"feed.failing"}}
		agent := webhookAgent(t, f)
		if _, err := f.s.CreateWebhook(ctx, agent, in, "a1"); !errors.Is(err, ErrForbidden) {
			t.Fatal("agent created a webhook", err)
		}
		if _, err := f.s.Webhooks(ctx, agent); !errors.Is(err, ErrForbidden) {
			t.Fatal("agent listed webhooks", err)
		}
		scopeless := f.a
		scopeless.Scopes = []Scope{ScopeEntriesRead}
		if _, err := f.s.CreateWebhook(ctx, scopeless, in, "h1"); !errors.Is(err, ErrForbidden) {
			t.Fatal("scopeless human created a webhook", err)
		}
		ghost := Actor{UserID: f.a.UserID, PrincipalID: 999999, Kind: PrincipalHuman, Scopes: HumanScopes}
		if _, err := f.s.CreateWebhook(ctx, ghost, in, "g1"); !errors.Is(err, ErrUnauthorized) {
			t.Fatal("unknown principal", err)
		}
		// The database trigger independently rejects a non-human creator.
		created, err := f.s.CreateWebhook(ctx, f.a, in, "ok-1")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := f.s.UpdateWebhook(ctx, agent, created.ID, WebhookPatch{}, "a2"); !errors.Is(err, ErrForbidden) {
			t.Fatal("agent updated a webhook", err)
		}
		if err := f.s.DeleteWebhook(ctx, agent, created.ID, "a3"); !errors.Is(err, ErrForbidden) {
			t.Fatal("agent deleted a webhook", err)
		}
	})
	t.Run("invalid_inputs", func(t *testing.T) {
		f := newCoreFixture(t, template)
		for _, in := range []WebhookInput{
			{URL: "ftp://example.test/", Events: []string{"feed.failing"}},
			{URL: "https://user:pw@example.test/", Events: []string{"feed.failing"}},
			{URL: "https://example.test/#frag", Events: []string{"feed.failing"}},
			{URL: "", Events: []string{"feed.failing"}},
			{URL: "https://example.test/", Events: nil},
			{URL: "https://example.test/", Events: []string{"entry.created"}},
		} {
			if _, err := f.s.CreateWebhook(ctx, f.a, in, "bad-"+in.URL); !errors.Is(err, ErrInvalid) {
				t.Fatalf("input accepted: %+v %v", in, err)
			}
		}
		if _, err := f.s.CreateWebhook(ctx, f.a, WebhookInput{URL: "https://example.test/", Events: []string{"feed.failing"}}, ""); !errors.Is(err, ErrInvalid) {
			t.Fatal("empty op_id", err)
		}
	})
	t.Run("update_validation_and_isolation", func(t *testing.T) {
		f := newCoreFixture(t, template)
		created, err := f.s.CreateWebhook(ctx, f.a, WebhookInput{URL: "https://hooks.example.test/", Events: []string{"feed.failing"}}, "mk")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := f.s.UpdateWebhook(ctx, f.a, created.ID, WebhookPatch{}, "noop"); !errors.Is(err, ErrInvalid) {
			t.Fatal("empty patch", err)
		}
		bad := "notaurl"
		if _, err := f.s.UpdateWebhook(ctx, f.a, created.ID, WebhookPatch{URL: &bad}, "bad"); !errors.Is(err, ErrInvalid) {
			t.Fatal("bad url patch", err)
		}
		var other int64
		if err := f.db.Pool().QueryRow(ctx, "INSERT INTO users(username,password_hash) VALUES('other','$argon2id$test') RETURNING id").Scan(&other); err != nil {
			t.Fatal(err)
		}
		var principal int64
		if err := f.db.Pool().QueryRow(ctx, "INSERT INTO principals(user_id,kind,name) VALUES($1,'human','Other') RETURNING id", other).Scan(&principal); err != nil {
			t.Fatal(err)
		}
		stranger := Actor{UserID: other, PrincipalID: principal, Kind: PrincipalHuman, Via: CredentialSession, Scopes: HumanScopes}
		if _, err := f.s.UpdateWebhook(ctx, stranger, created.ID, WebhookPatch{}, "x"); !errors.Is(err, ErrInvalid) {
			t.Fatal("empty foreign patch", err)
		}
		off := false
		if _, err := f.s.UpdateWebhook(ctx, stranger, created.ID, WebhookPatch{Active: &off}, "x"); !errors.Is(err, ErrNotFound) {
			t.Fatal("foreign update", err)
		}
		if err := f.s.DeleteWebhook(ctx, stranger, created.ID, "x"); !errors.Is(err, ErrNotFound) {
			t.Fatal("foreign delete", err)
		}
		list, err := f.s.Webhooks(ctx, stranger)
		if err != nil || len(list) != 0 {
			t.Fatal("foreign list", list, err)
		}
	})
}
