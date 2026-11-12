package service

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/L-K-M/Earnest/internal/outbound"
	"github.com/L-K-M/Earnest/internal/store/storetest"
)

func TestArticleIntegrationValidation(t *testing.T) {
	for _, raw := range []string{"javascript:alert(1)", "https://user:password@example.test", "https://example.test?token=secret", "https://example.test?", "https://example.test/#fragment", "https:///path"} {
		if _, err := integrationURL(raw); !errors.Is(err, ErrInvalid) {
			t.Errorf("accepted URL %q: %v", raw, err)
		}
	}
	if got, err := integrationURL(" http://hister.local:8080/base/ "); err != nil || got != "http://hister.local:8080/base" {
		t.Fatalf("URL = %q, %v", got, err)
	}
	for _, id := range []string{"", "../other", "id/path", "id?x", strings.Repeat("a", 257)} {
		if integrationID(id) {
			t.Errorf("accepted ID %q", id)
		}
	}
}

func TestArticleIntegrationSettings(t *testing.T) {
	f := newCoreFixture(t, storetest.NewTemplate(t))
	ctx := t.Context()
	token := "secret-karakeep-token"
	in := IntegrationSettingsInput{URL: "https://keep.example/base/", DefaultListID: "reading", Token: &token, OpID: "save"}
	out, err := f.s.PutArticleIntegration(ctx, f.a, IntegrationKarakeep, in)
	if err != nil || !out.TokenSet || out.URL != "https://keep.example/base" || out.DefaultListID != "reading" {
		t.Fatalf("save: %+v %v", out, err)
	}
	// Both the public read and operation replay are redacted; settings own
	// only their fragment, so ordinary reader saves cannot erase the secret.
	if _, err := f.s.PutArticleIntegration(ctx, f.a, IntegrationKarakeep, in); err != nil {
		t.Fatal(err)
	}
	settings := DefaultReaderSettings()
	if _, err := f.s.PutSettings(ctx, f.a, ReaderSettingsInput{ReadingMode: settings.ReadingMode, TodayDecayDays: settings.TodayDecayDays, SingleKeyShortcuts: true, Theme: settings.Theme, Measure: settings.Measure, Font: settings.Font, OpID: "reader"}); err != nil {
		t.Fatal(err)
	}
	read, err := f.s.GetArticleIntegrations(ctx, f.a)
	if err != nil || read.Karakeep != out || read.Hister.URL != "" {
		t.Fatalf("read: %+v %v", read, err)
	}
	var raw, ops string
	if err := f.db.Pool().QueryRow(ctx, "SELECT settings::text FROM users WHERE id=$1", f.a.UserID).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	if err := f.db.Pool().QueryRow(ctx, "SELECT result::text FROM op_ids WHERE principal_id=$1 AND op_id='save'", f.a.PrincipalID).Scan(&ops); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(raw, token) || strings.Contains(ops, token) || strings.Contains(ops, "token_encrypted") {
		t.Fatal("secret leaked")
	}
	in.OpID, in.URL, in.Token = "preserve", out.URL, nil
	if out, err = f.s.PutArticleIntegration(ctx, f.a, IntegrationKarakeep, in); err != nil || !out.TokenSet {
		t.Fatalf("preserve: %+v %v", out, err)
	}
	in.OpID, in.URL = "change-host", "https://other.example"
	if _, err := f.s.PutArticleIntegration(ctx, f.a, IntegrationKarakeep, in); !errors.Is(err, ErrInvalid) {
		t.Fatalf("sent old token to changed host: %v", err)
	}
	in.OpID, in.URL = "disable", ""
	if out, err = f.s.PutArticleIntegration(ctx, f.a, IntegrationKarakeep, in); err != nil || out.TokenSet || out.DefaultListID != "" {
		t.Fatalf("disable: %+v %v", out, err)
	}
	token = "hister-key"
	in = IntegrationSettingsInput{URL: "http://hister.local", DefaultLabel: "Reading queue", Token: &token, OpID: "hister"}
	if _, err := f.s.PutArticleIntegration(ctx, f.a, IntegrationHister, in); err != nil {
		t.Fatal(err)
	}
	in.URL, in.Token, in.OpID = "http://other.local", nil, "hister-new-host"
	if out, err = f.s.PutArticleIntegration(ctx, f.a, IntegrationHister, in); err != nil || out.TokenSet || out.DefaultLabel != "Reading queue" {
		t.Fatalf("new host must lose token: %+v %v", out, err)
	}
	in.DefaultListID, in.OpID = "not-supported", "bad-folder"
	if _, err := f.s.PutArticleIntegration(ctx, f.a, IntegrationHister, in); !errors.Is(err, ErrInvalid) {
		t.Fatalf("Hister folder accepted: %v", err)
	}
	agent := f.a
	agent.Kind = PrincipalAgent
	if _, err := f.s.GetArticleIntegrations(ctx, agent); !errors.Is(err, ErrForbidden) {
		t.Fatalf("agent read: %v", err)
	}
	if _, err := f.s.PutArticleIntegration(ctx, agent, IntegrationHister, in); !errors.Is(err, ErrForbidden) {
		t.Fatalf("agent write: %v", err)
	}
}

func integrationFixture(t *testing.T, template *storetest.Template, handler http.HandlerFunc) (*coreFixture, string, int64) {
	t.Helper()
	f := newCoreFixture(t, template)
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	u, _ := url.Parse(server.URL)
	port, _ := strconv.Atoi(u.Port())
	client, err := outbound.New(outbound.Config{Version: "test", AllowHosts: []string{u.Host}, Spacing: time.Nanosecond})
	if err != nil {
		t.Fatal(err)
	}
	f.s.outbound, f.s.mediaPorts = client, []uint16{uint16(port)}
	item := coreItem("send")
	item.URL = "https://example.test/send"
	if _, err := f.s.RecordFetch(t.Context(), FetchOutcome{FeedID: f.feed, Status: FetchOK, Items: []IngestItem{item}}); err != nil {
		t.Fatal(err)
	}
	entries, err := f.s.Entries(t.Context(), f.a, EntryFilter{View: ViewAll})
	if err != nil || len(entries) != 1 {
		t.Fatalf("entries: %v %v", entries, err)
	}
	return f, server.URL, entries[0].ID
}

func TestSendArticle(t *testing.T) {
	template := storetest.NewTemplate(t)
	t.Run("Karakeep create list and idempotent replay", func(t *testing.T) {
		var creates, lists atomic.Int32
		f, server, id := integrationFixture(t, template, func(w http.ResponseWriter, r *http.Request) {
			if r.Header.Get("Authorization") != "Bearer keep-key" {
				t.Error("missing auth")
			}
			switch r.URL.Path {
			case "/base/api/v1/bookmarks":
				creates.Add(1)
				var body map[string]string
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body["type"] != "link" || body["url"] != "https://example.test/send" || r.Method != http.MethodPost {
					t.Errorf("bookmark request: %v %v", body, err)
				}
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusCreated)
				fmt.Fprint(w, `{"id":"bookmark-1"}`)
			case "/base/api/v1/lists/reading/bookmarks/bookmark-1":
				lists.Add(1)
				if r.Method != http.MethodPut {
					t.Error("not PUT")
				}
				w.WriteHeader(http.StatusNoContent)
			default:
				t.Errorf("unexpected path %s", r.URL.Path)
				w.WriteHeader(404)
			}
		})
		token := "keep-key"
		if _, err := f.s.PutArticleIntegration(t.Context(), f.a, IntegrationKarakeep, IntegrationSettingsInput{URL: server + "/base", Token: &token, DefaultListID: "reading", OpID: "config"}); err != nil {
			t.Fatal(err)
		}
		for range 2 {
			out, err := f.s.SendArticle(t.Context(), f.a, id, IntegrationKarakeep, "send")
			if err != nil || out.Message != "Sent to Karakeep." {
				t.Fatalf("send: %+v %v", out, err)
			}
		}
		if creates.Load() != 1 || lists.Load() != 1 {
			t.Fatalf("replayed network calls: %d %d", creates.Load(), lists.Load())
		}
		agent := f.a
		agent.Kind = PrincipalAgent
		if _, err := f.s.SendArticle(t.Context(), agent, id, IntegrationKarakeep, "agent"); !errors.Is(err, ErrForbidden) {
			t.Fatalf("agent send: %v", err)
		}
		if _, err := f.s.SendArticle(t.Context(), f.a, id+1000, IntegrationKarakeep, "missing"); !errors.Is(err, ErrNotFound) {
			t.Fatalf("invisible entry: %v", err)
		}
	})
	t.Run("Hister sends stored text with default label", func(t *testing.T) {
		var received map[string]string
		f, server, id := integrationFixture(t, template, func(w http.ResponseWriter, r *http.Request) {
			// Hister's non-browser API still enforces its documented Origin
			// convention, even when a Bearer token authenticates the request.
			if r.Header.Get("Origin") != "hister://" {
				http.Error(w, "CSRF token mismatch", http.StatusForbidden)
				return
			}
			if r.URL.Path != "/api/add" || r.Method != http.MethodPost || r.Header.Get("Authorization") != "Bearer hister-key" {
				t.Errorf("bad Hister request: %s %s", r.Method, r.URL.Path)
			}
			if err := json.NewDecoder(r.Body).Decode(&received); err != nil {
				t.Error(err)
			}
			w.WriteHeader(http.StatusCreated)
		})
		token := "hister-key"
		if _, err := f.s.PutArticleIntegration(t.Context(), f.a, IntegrationHister, IntegrationSettingsInput{URL: server, Token: &token, DefaultLabel: "RSS reading", OpID: "config"}); err != nil {
			t.Fatal(err)
		}
		out, err := f.s.SendArticle(t.Context(), f.a, id, IntegrationHister, "send")
		if err != nil || out.Message != "Sent to Hister." {
			t.Fatalf("send: %+v %v", out, err)
		}
		if received["label"] != "RSS reading" || received["text"] != coreItem("send").ContentText || received["title"] != coreItem("send").Title || received["url"] != "https://example.test/send" {
			t.Fatalf("payload: %v", received)
		}
	})
	t.Run("partial list failure is not reported as success", func(t *testing.T) {
		f, server, id := integrationFixture(t, template, func(w http.ResponseWriter, r *http.Request) {
			if r.Method == http.MethodPost {
				w.Header().Set("Content-Type", "application/json")
				fmt.Fprint(w, `{"id":"existing"}`)
				return
			}
			w.WriteHeader(http.StatusNotFound)
			fmt.Fprint(w, "secret-key must never appear in the error")
		})
		token := "secret-key"
		if _, err := f.s.PutArticleIntegration(t.Context(), f.a, IntegrationKarakeep, IntegrationSettingsInput{URL: server, Token: &token, DefaultListID: "missing", OpID: "config"}); err != nil {
			t.Fatal(err)
		}
		out, err := f.s.SendArticle(t.Context(), f.a, id, IntegrationKarakeep, "send")
		if !errors.Is(err, ErrUnavailable) || !strings.Contains(err.Error(), "saved to Karakeep, but") || strings.Contains(err.Error(), token) || out.Message != "" {
			t.Fatalf("partial outcome: %+v %v", out, err)
		}
	})
	t.Run("redirects are rejected without forwarding a credential", func(t *testing.T) {
		var calls atomic.Int32
		f, server, id := integrationFixture(t, template, func(w http.ResponseWriter, r *http.Request) {
			calls.Add(1)
			w.Header().Set("Location", "/login")
			w.WriteHeader(http.StatusFound)
		})
		token := "secret-key"
		if _, err := f.s.PutArticleIntegration(t.Context(), f.a, IntegrationHister, IntegrationSettingsInput{URL: server, Token: &token, OpID: "config"}); err != nil {
			t.Fatal(err)
		}
		if _, err := f.s.SendArticle(t.Context(), f.a, id, IntegrationHister, "send"); !errors.Is(err, ErrUnavailable) {
			t.Fatalf("redirect: %v", err)
		}
		if calls.Load() != 1 {
			t.Fatal("followed redirect")
		}
	})
	t.Run("private destinations still require explicit allowlisting", func(t *testing.T) {
		f, server, id := integrationFixture(t, template, func(w http.ResponseWriter, r *http.Request) { t.Error("unallowlisted server reached") })
		client, _ := outbound.New(outbound.Config{Version: "test"})
		f.s.outbound = client
		if _, err := f.s.PutArticleIntegration(t.Context(), f.a, IntegrationHister, IntegrationSettingsInput{URL: server, OpID: "config"}); err != nil {
			t.Fatal(err)
		}
		if _, err := f.s.SendArticle(t.Context(), f.a, id, IntegrationHister, "send"); !errors.Is(err, ErrUnavailable) || !strings.Contains(err.Error(), "EARNEST_OUTBOUND_ALLOW_PRIVATE") {
			t.Fatalf("private send: %v", err)
		}
	})
}
