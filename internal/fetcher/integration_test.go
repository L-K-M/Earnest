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

	"github.com/L-K-M/Earnest/internal/feed"
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

func TestFetchCorrectsStoredUTF8(t *testing.T) {
	template := storetest.NewTemplate(t)
	db := template.New(t)
	ctx := context.Background()
	const title = "Let’s Talk About Why all This OT Stuff is Connected"
	const text = "It’s connected."
	doc := `<?xml version="1.0" encoding="UTF-8"?><rss version="2.0"><channel><title>Lobsters: Newest Comments</title>` + strings.Repeat(" ", 2048) +
		`<item><guid>https://lobste.rs/c/example</guid><title>` + title + `</title><description>` + text + `</description></item></channel></rss>`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/rss+xml")
		fmt.Fprint(w, doc)
	}))
	defer server.Close()
	host, port := serverHostPort(t, server)
	f, svc := integrationFetcher(t, db, testClient(t, host), port)
	feedID := insertFeed(t, db, server.URL)
	claim := service.FeedToFetch{FeedID: feedID, URL: server.URL}

	// Reproduce the old decoder's windows-1252 interpretation in storage.
	corrupted, err := feed.Parse([]byte(doc), feed.ParseOptions{Charset: "windows-1252"})
	if err != nil {
		t.Fatal(err)
	}
	items, err := f.items(ctx, claim, corrupted.Items)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.RecordFetch(ctx, service.FetchOutcome{FeedID: feedID, Status: service.FetchOK, Items: items}); err != nil {
		t.Fatal(err)
	}
	var beforeID, beforeSeq int64
	var beforeTitle string
	if err := db.Pool().QueryRow(ctx, "SELECT id,seq,title FROM entries WHERE feed_id=$1", feedID).Scan(&beforeID, &beforeSeq, &beforeTitle); err != nil {
		t.Fatal(err)
	}
	if beforeTitle == title {
		t.Fatal("seed did not reproduce the corrupted title")
	}

	if err := f.Fetch(ctx, claim); err != nil {
		t.Fatal(err)
	}
	var afterID, afterSeq int64
	var afterTitle string
	var count int
	if err := db.Pool().QueryRow(ctx, "SELECT count(*) FROM entries WHERE feed_id=$1", feedID).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if err := db.Pool().QueryRow(ctx, "SELECT id,seq,title FROM entries WHERE feed_id=$1", feedID).Scan(&afterID, &afterSeq, &afterTitle); err != nil {
		t.Fatal(err)
	}
	if count != 1 || afterID != beforeID || afterSeq != beforeSeq || afterTitle != title {
		t.Fatalf("refresh changed entry identity or failed to correct title: count=%d id=%d seq=%d title=%q", count, afterID, afterSeq, afterTitle)
	}
	_, gotText, _, _, err := store.LoadContent(ctx, db.Pool(), afterID)
	if err != nil {
		t.Fatal(err)
	}
	if gotText != text {
		t.Fatalf("stored body = %q, want %q", gotText, text)
	}
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

// Seq stays the cross-batch first-seen order: a backdated article arriving in
// a later fetch gets a later seq than an earlier fetch's newer article, even
// though its batch sorts oldest to newest.
func TestFetchBatchOrderKeepsCrossBatchSeq(t *testing.T) {
	db := storetest.NewTemplate(t).New(t)
	ctx := context.Background()
	doc := `<?xml version="1.0"?><rss version="2.0"><channel><title>News</title>` +
		`<item><guid>first</guid><title>First</title><pubDate>Tue, 02 Feb 2021 00:00:00 GMT</pubDate><description>1</description></item>` +
		`</channel></rss>`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, doc)
	}))
	defer server.Close()
	host, port := serverHostPort(t, server)
	f, _ := integrationFetcher(t, db, testClient(t, host), port)
	feedID := insertFeed(t, db, server.URL)
	fetch := func() {
		t.Helper()
		if err := f.Fetch(ctx, service.FeedToFetch{FeedID: feedID, URL: server.URL}); err != nil {
			t.Fatal(err)
		}
	}
	fetch()
	// The second fetch lists its newest item first, but one of them is
	// backdated: within the batch it must still sort oldest to newest.
	doc = `<?xml version="1.0"?><rss version="2.0"><channel><title>News</title>` +
		`<item><guid>newer</guid><title>Newer</title><pubDate>Wed, 03 Feb 2021 00:00:00 GMT</pubDate><description>2</description></item>` +
		`<item><guid>backdated</guid><title>Backdated</title><pubDate>Mon, 01 Feb 2021 00:00:00 GMT</pubDate><description>3</description></item>` +
		`</channel></rss>`
	fetch()
	rows, err := db.Pool().Query(ctx, "SELECT title FROM entries WHERE feed_id=$1 ORDER BY seq", feedID)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var got []string
	for rows.Next() {
		var title string
		if err := rows.Scan(&title); err != nil {
			t.Fatal(err)
		}
		got = append(got, title)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if want := "First,Backdated,Newer"; strings.Join(got, ",") != want {
		t.Fatalf("seq order = %q, want %q", strings.Join(got, ","), want)
	}
}

// The batch order the fetcher reports must become the entries' seq order, so
// a newest-first page (ORDER BY seq DESC) shows the newest article first.
func TestFetchBatchOrderPersistsAsSeq(t *testing.T) {
	db := storetest.NewTemplate(t).New(t)
	ctx := context.Background()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, `<?xml version="1.0"?><rss version="2.0"><channel><title>News</title>`+
			`<item><guid>newest</guid><title>Newest</title><pubDate>Wed, 03 Feb 2021 00:00:00 GMT</pubDate><description>n</description></item>`+
			`<item><guid>middle</guid><title>Middle</title><pubDate>Tue, 02 Feb 2021 00:00:00 GMT</pubDate><description>m</description></item>`+
			`<item><guid>oldest</guid><title>Oldest</title><pubDate>Mon, 01 Feb 2021 00:00:00 GMT</pubDate><description>o</description></item>`+
			`</channel></rss>`)
	}))
	defer server.Close()
	host, port := serverHostPort(t, server)
	f, _ := integrationFetcher(t, db, testClient(t, host), port)
	feedID := insertFeed(t, db, server.URL)
	if err := f.Fetch(ctx, service.FeedToFetch{FeedID: feedID, URL: server.URL}); err != nil {
		t.Fatal(err)
	}
	rows, err := db.Pool().Query(ctx, "SELECT title,seq FROM entries WHERE feed_id=$1 ORDER BY seq", feedID)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	type row struct {
		title string
		seq   int64
	}
	var got []row
	for rows.Next() {
		var r row
		if err := rows.Scan(&r.title, &r.seq); err != nil {
			t.Fatal(err)
		}
		got = append(got, r)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	want := []string{"Oldest", "Middle", "Newest"}
	if len(got) != len(want) {
		t.Fatalf("entries = %+v, want %d", got, len(want))
	}
	for i, title := range want {
		if got[i].title != title {
			t.Fatalf("seq %d holds %q, want %q (order %+v)", got[i].seq, got[i].title, title, got)
		}
		if i > 0 && got[i].seq <= got[i-1].seq {
			t.Fatalf("seq not increasing with publication date: %+v", got)
		}
	}
}
