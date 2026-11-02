package fetcher

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/L-K-M/Earnest/internal/outbound"
	"github.com/L-K-M/Earnest/internal/service"
	"github.com/L-K-M/Earnest/internal/store"
	"github.com/L-K-M/Earnest/internal/store/storetest"
)

// These tests bind the fetcher to a real service so a permissive fake cannot
// mask the persisted contract: RecordFetch only accepts the service's error
// vocabulary, and KnownIdentities decides whether extraction runs.

func testClient(t *testing.T, hosts ...string) *outbound.Client {
	t.Helper()
	c, err := outbound.New(outbound.Config{AllowHosts: hosts, Spacing: time.Nanosecond})
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func serverHostPort(t *testing.T, server *httptest.Server) (string, uint16) {
	t.Helper()
	u, _ := url.Parse(server.URL)
	_, p, err := net.SplitHostPort(u.Host)
	if err != nil {
		t.Fatal(err)
	}
	n, err := strconv.Atoi(p)
	if err != nil {
		t.Fatal(err)
	}
	return u.Host, uint16(n)
}

func integrationFetcher(t *testing.T, db *store.Store, client *outbound.Client, ports ...uint16) (*Fetcher, *service.Service) {
	t.Helper()
	svc, err := service.New(db, service.Deps{SecretKey: []byte(strings.Repeat("k", 32))})
	if err != nil {
		t.Fatal(err)
	}
	f, err := New(svc, client, Config{ExtraPorts: ports})
	if err != nil {
		t.Fatal(err)
	}
	return f, svc
}

func insertFeed(t *testing.T, db *store.Store, rawURL string) int64 {
	t.Helper()
	var id int64
	if err := db.Pool().QueryRow(context.Background(), "INSERT INTO feeds(url) VALUES($1) RETURNING id", rawURL).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}

func persistedClass(t *testing.T, db *store.Store, feed int64) (string, time.Time) {
	t.Helper()
	var class string
	var next time.Time
	if err := db.Pool().QueryRow(context.Background(), "SELECT coalesce(error_class,''),next_fetch_at FROM feeds WHERE id=$1", feed).Scan(&class, &next); err != nil {
		t.Fatal(err)
	}
	return class, next
}

func TestFetchOutcomePersisted(t *testing.T) {
	template := storetest.NewTemplate(t)
	ctx := context.Background()
	for _, tt := range []struct {
		name   string
		status int
		retry  string
		class  string
	}{
		{"rate_limited", 429, "7200", "rate_limit"},
		{"gone", 410, "", "gone"},
		{"challenge", 403, "", "challenge"},
		{"server_error", 500, "", "http"},
		{"other_client_error", 418, "", "http"},
		{"malformed_feed", 200, "", "parse"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if tt.retry != "" {
					w.Header().Set("Retry-After", tt.retry)
				}
				w.WriteHeader(tt.status)
				fmt.Fprint(w, "<rss><channel>")
			}))
			defer server.Close()
			db := template.New(t)
			host, port := serverHostPort(t, server)
			f, _ := integrationFetcher(t, db, testClient(t, host), port)
			feed := insertFeed(t, db, server.URL)
			if err := f.Fetch(ctx, service.FeedToFetch{FeedID: feed, URL: server.URL}); err != nil {
				t.Fatalf("record fetch rejected the outcome: %v", err)
			}
			class, next := persistedClass(t, db, feed)
			if class != tt.class {
				t.Fatalf("persisted class %q, want %q", class, tt.class)
			}
			if tt.retry != "" && next.Before(time.Now().Add(119*time.Minute)) {
				t.Fatalf("retry-after not honoured: next %v", next)
			}
		})
	}
	t.Run("denied_address", func(t *testing.T) {
		db := template.New(t)
		f, _ := integrationFetcher(t, db, testClient(t))
		feed := insertFeed(t, db, "http://10.0.0.1/")
		if err := f.Fetch(ctx, service.FeedToFetch{FeedID: feed, URL: "http://10.0.0.1/"}); err != nil {
			t.Fatalf("record fetch rejected the outcome: %v", err)
		}
		if class, _ := persistedClass(t, db, feed); class != "policy" {
			t.Fatalf("persisted class %q, want policy", class)
		}
	})
	t.Run("connect_refused", func(t *testing.T) {
		db := template.New(t)
		f, _ := integrationFetcher(t, db, testClient(t, "127.0.0.1:1"), 1)
		feed := insertFeed(t, db, "http://127.0.0.1:1/")
		if err := f.Fetch(ctx, service.FeedToFetch{FeedID: feed, URL: "http://127.0.0.1:1/"}); err != nil {
			t.Fatalf("record fetch rejected the outcome: %v", err)
		}
		if class, _ := persistedClass(t, db, feed); class != "network" {
			t.Fatalf("persisted class %q, want network", class)
		}
	})
	t.Run("too_large", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Length", strconv.Itoa(50<<20))
			data := make([]byte, 32<<10)
			for i := 0; i < 1600; i++ {
				if _, err := w.Write(data); err != nil {
					return
				}
			}
		}))
		defer server.Close()
		db := template.New(t)
		host, port := serverHostPort(t, server)
		f, _ := integrationFetcher(t, db, testClient(t, host), port)
		feed := insertFeed(t, db, server.URL)
		if err := f.Fetch(ctx, service.FeedToFetch{FeedID: feed, URL: server.URL}); err != nil {
			t.Fatalf("record fetch rejected the outcome: %v", err)
		}
		if class, _ := persistedClass(t, db, feed); class != "size" {
			t.Fatalf("persisted class %q, want size", class)
		}
	})
}

// TestInitialCohortExtraction covers the reported regression: a feed ingested
// through Subscribe's probe holds only feed-sourced content, so the first
// scheduled fetch with extraction enabled must retry extraction once, reuse
// the cached version afterwards, and never resurrect tombstones.
func TestInitialCohortExtraction(t *testing.T) {
	template := storetest.NewTemplate(t)
	db := template.New(t)
	ctx := context.Background()
	var articleHits atomic.Int64
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/article" {
			articleHits.Add(1)
			fmt.Fprint(w, `<html><head><title>Article</title></head><body><article><p>`+strings.Repeat("The full article body. ", 40)+`</p></article></body></html>`)
			return
		}
		fmt.Fprintf(w, `<rss version="2.0"><channel><title>News</title><item><guid>one</guid><link>%s/article</link><title>Item</title><description>Feed summary.</description></item></channel></rss>`, server.URL)
	}))
	defer server.Close()
	host, port := serverHostPort(t, server)
	f, svc := integrationFetcher(t, db, testClient(t, host), port)
	probing, err := service.New(db, service.Deps{SecretKey: []byte(strings.Repeat("k", 32)), FeedProber: f})
	if err != nil {
		t.Fatal(err)
	}
	actor := service.Actor{Kind: service.PrincipalHuman, Via: service.CredentialSession, Scopes: service.HumanScopes}
	if err := db.Pool().QueryRow(ctx, "INSERT INTO users(username,password_hash) VALUES('reader','$argon2id$test') RETURNING id").Scan(&actor.UserID); err != nil {
		t.Fatal(err)
	}
	if err := db.Pool().QueryRow(ctx, "INSERT INTO principals(user_id,kind,name) VALUES($1,'human','Reader') RETURNING id", actor.UserID).Scan(&actor.PrincipalID); err != nil {
		t.Fatal(err)
	}
	sub, err := probing.Subscribe(ctx, actor, server.URL+"/feed", service.SubscribeOptions{OpID: "subscribe", Extract: service.ExtractAlways})
	if err != nil {
		t.Fatal(err)
	}
	if got := articleHits.Load(); got != 0 {
		t.Fatalf("probe fetched the article %d times", got)
	}
	var entry int64
	if err := db.Pool().QueryRow(ctx, "SELECT id FROM entries WHERE feed_id=$1", sub.FeedID).Scan(&entry); err != nil {
		t.Fatal(err)
	}
	due, err := svc.ClaimDueFeeds(ctx, time.Now().Add(time.Hour), 10, time.Minute)
	if err != nil || len(due) != 1 || due[0].Extract != service.ExtractAlways {
		t.Fatalf("claim: %+v %v", due, err)
	}
	if err := f.Fetch(ctx, due[0]); err != nil {
		t.Fatal(err)
	}
	if got := articleHits.Load(); got != 1 {
		t.Fatalf("initial cohort never extracted: %d article hits", got)
	}
	_, _, _, source, err := store.LoadContent(ctx, db.Pool(), entry)
	if err != nil || source != "extracted" {
		t.Fatalf("content source %q %v", source, err)
	}
	claim := service.FeedToFetch{FeedID: sub.FeedID, URL: server.URL + "/feed", Extract: service.ExtractAlways}
	if err := f.Fetch(ctx, claim); err != nil {
		t.Fatal(err)
	}
	if got := articleHits.Load(); got != 1 {
		t.Fatalf("cached extraction not reused: %d article hits", got)
	}
	if _, err := db.Pool().Exec(ctx, "UPDATE entries SET deleted_at=now() WHERE id=$1", entry); err != nil {
		t.Fatal(err)
	}
	if err := f.Fetch(ctx, claim); err != nil {
		t.Fatal(err)
	}
	if got := articleHits.Load(); got != 1 {
		t.Fatalf("tombstone re-extracted: %d article hits", got)
	}
	var live int
	if err := db.Pool().QueryRow(ctx, "SELECT count(*) FROM entries WHERE feed_id=$1 AND deleted_at IS NULL", sub.FeedID).Scan(&live); err != nil || live != 0 {
		t.Fatalf("tombstone resurrected: %d %v", live, err)
	}
}
