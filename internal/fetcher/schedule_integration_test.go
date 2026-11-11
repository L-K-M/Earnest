package fetcher

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/L-K-M/Earnest/internal/service"
	"github.com/L-K-M/Earnest/internal/store/storetest"
)

func TestCacheAgeDoesNotReplaceFailureBackoff(t *testing.T) {
	db := storetest.NewTemplate(t).New(t)
	var phase atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "max-age=864000")
		switch phase.Load() {
		case 1:
			w.Header().Set("Retry-After", "259200")
			w.WriteHeader(http.StatusTooManyRequests)
		case 2:
			w.WriteHeader(http.StatusNotModified)
		default:
			fmt.Fprint(w, `<rss version="2.0"><channel><title>Comments</title>`)
			for i := range 6 {
				at := time.Now().UTC().Truncate(time.Second).Add(-time.Minute - time.Duration(i)*time.Hour)
				fmt.Fprintf(w, `<item><guid>comment-%d</guid><pubDate>%s</pubDate></item>`, i, at.Format(time.RFC1123Z))
			}
			fmt.Fprint(w, `</channel></rss>`)
		}
	}))
	defer server.Close()
	host, port := serverHostPort(t, server)
	f, _ := integrationFetcher(t, db, testClient(t, host), port)
	feedID := insertFeed(t, db, server.URL)
	for stage, want := range []time.Duration{2 * time.Hour, 72 * time.Hour, 2 * time.Hour} {
		phase.Store(int32(stage))
		if err := f.Fetch(t.Context(), service.FeedToFetch{FeedID: feedID, URL: server.URL}); err != nil {
			t.Fatal(err)
		}
		var attempt, next time.Time
		var cadence float64
		var failures int
		if err := db.Pool().QueryRow(t.Context(), `SELECT last_attempt_at,next_fetch_at,extract(epoch FROM posting_interval)::double precision,consecutive_failures FROM feeds WHERE id=$1`, feedID).Scan(&attempt, &next, &cadence, &failures); err != nil {
			t.Fatal(err)
		}
		if wait := next.Sub(attempt); wait < want || wait > want+time.Second {
			t.Fatalf("stage %d: wait %v, want %v", stage, wait, want)
		}
		wantFailures := 0
		if stage == 1 {
			wantFailures = 1
		}
		if cadence != time.Hour.Seconds() || failures != wantFailures {
			t.Fatalf("stage %d: cadence=%v failures=%d", stage, cadence, failures)
		}
	}
}

func TestCacheAgeFollowsFeedCadenceAcrossUnchangedFetches(t *testing.T) {
	template := storetest.NewTemplate(t)
	for _, tc := range []struct {
		name     string
		cadence  time.Duration
		maxAge   string
		wantWait time.Duration
	}{
		{"hourly_comments", time.Hour, "172800", 2 * time.Hour},
		{"frequent_comments", 10 * time.Minute, "172800", 20 * time.Minute},
		{"monthly_feed", 30 * 24 * time.Hour, "864000", 48 * time.Hour},
		{"no_dates", 0, "172800", time.Hour},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db := template.New(t)
			var doc strings.Builder
			doc.WriteString(`<rss version="2.0"><channel><title>Feed</title>`)
			for i := range 6 {
				fmt.Fprintf(&doc, `<item><guid>item-%d</guid><title>Item %d</title>`, i, i)
				if tc.cadence > 0 {
					at := time.Now().UTC().Truncate(time.Second).Add(-time.Minute - time.Duration(i)*tc.cadence)
					fmt.Fprintf(&doc, `<pubDate>%s</pubDate>`, at.Format(time.RFC1123Z))
				}
				doc.WriteString(`</item>`)
			}
			doc.WriteString(`</channel></rss>`)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Cache-Control", "max-age="+tc.maxAge)
				w.Header().Set("ETag", `"v1"`)
				if r.Header.Get("If-None-Match") == `"v1"` {
					w.WriteHeader(http.StatusNotModified)
					return
				}
				fmt.Fprint(w, doc.String())
			}))
			defer server.Close()
			host, port := serverHostPort(t, server)
			feedID := insertFeed(t, db, server.URL)
			claim := service.FeedToFetch{FeedID: feedID, URL: server.URL}
			// Reconstruct both objects each pass, like a process restart. A 304
			// must reuse durable cadence, not lose it or repeatedly double it.
			for pass := range 4 {
				f, _ := integrationFetcher(t, db, testClient(t, host), port)
				if err := f.Fetch(context.Background(), claim); err != nil {
					t.Fatal(err)
				}
				var attempt, next time.Time
				var status, failures int
				if err := db.Pool().QueryRow(t.Context(), `SELECT last_attempt_at,next_fetch_at,last_http_status,consecutive_failures FROM feeds WHERE id=$1`, feedID).Scan(&attempt, &next, &status, &failures); err != nil {
					t.Fatal(err)
				}
				wantStatus := http.StatusNotModified
				if pass == 0 {
					wantStatus = http.StatusOK
				}
				if status != wantStatus || failures != 0 {
					t.Fatalf("pass %d: status=%d failures=%d", pass, status, failures)
				}
				if wait := next.Sub(attempt); wait < tc.wantWait || wait > tc.wantWait+time.Second {
					t.Fatalf("pass %d: cache scheduled %v, want %v", pass, wait, tc.wantWait)
				}
				claim.ETag = `"v1"`
			}
		})
	}
}
