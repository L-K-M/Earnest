package fetcher

import (
	"compress/gzip"
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
)

type fakeService struct{ outcomes []service.FetchOutcome }

func (s *fakeService) ClaimDueFeeds(context.Context, time.Time, int, time.Duration) ([]service.FeedToFetch, error) {
	return nil, nil
}
func (s *fakeService) KnownIdentities(context.Context, int64, []string) (map[string]bool, error) {
	return map[string]bool{}, nil
}
func (s *fakeService) RecordFetch(_ context.Context, o service.FetchOutcome) (service.IngestSummary, error) {
	s.outcomes = append(s.outcomes, o)
	return service.IngestSummary{}, nil
}
func testFetcher(t *testing.T, server *httptest.Server) (*Fetcher, *fakeService) {
	t.Helper()
	u, _ := url.Parse(server.URL)
	_, p, _ := net.SplitHostPort(u.Host)
	port, _ := strconv.Atoi(p)
	c, err := outbound.New(outbound.Config{AllowHosts: []string{u.Host}, Spacing: time.Nanosecond})
	if err != nil {
		t.Fatal(err)
	}
	s := &fakeService{}
	f, err := New(s, c, Config{ExtraPorts: []uint16{uint16(port)}})
	if err != nil {
		t.Fatal(err)
	}
	return f, s
}
func TestFetchConditionalAndChanges(t *testing.T) {
	content := "first"
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls == 2 {
			if r.Header.Get("If-None-Match") != "etag" {
				t.Error("missing condition")
			}
			w.WriteHeader(304)
			return
		}
		fmt.Fprintf(w, `<rss version="2.0"><channel><title>Feed</title><item><guid>abc</guid><link>https://example.org/article</link><description>%s</description></item></channel></rss>`, content)
	}))
	defer server.Close()
	f, s := testFetcher(t, server)
	ctx := context.Background()
	claim := service.FeedToFetch{FeedID: 1, URL: server.URL}
	if err := f.Fetch(ctx, claim); err != nil {
		t.Fatal(err)
	}
	claim.ETag = "etag"
	if err := f.Fetch(ctx, claim); err != nil {
		t.Fatal(err)
	}
	content = "changed"
	if err := f.Fetch(ctx, claim); err != nil {
		t.Fatal(err)
	}
	if s.outcomes[1].Status != service.FetchNotModified {
		t.Fatalf("status %s", s.outcomes[1].Status)
	}
	a, b := s.outcomes[0].Items[0], s.outcomes[2].Items[0]
	if a.IdentityHash != b.IdentityHash || a.ContentHash == b.ContentHash {
		t.Fatal("identity or content hash wrong")
	}
}
func TestFetchFailures(t *testing.T) {
	tests := []struct {
		name               string
		status             int
		body, retry, class string
	}{
		{"rate", 429, "", "120", "rate_limit"}, {"malformed", 200, "<rss><channel>", "", "parse"}, {"gone", 410, "", "", "gone"},
		{"challenge", 403, "", "", "challenge"}, {"server", 500, "", "", "http"}, {"client", 418, "", "", "http"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if tt.retry != "" {
					w.Header().Set("Retry-After", tt.retry)
				}
				w.WriteHeader(tt.status)
				fmt.Fprint(w, tt.body)
			}))
			defer server.Close()
			f, s := testFetcher(t, server)
			if err := f.Fetch(context.Background(), service.FeedToFetch{URL: server.URL}); err != nil {
				t.Fatal(err)
			}
			o := s.outcomes[0]
			if o.Status != service.FetchFailed || o.ErrorClass != tt.class {
				t.Fatalf("outcome %+v", o)
			}
			if tt.retry != "" && o.RetryAfter != 120*time.Second {
				t.Fatalf("retry %v", o.RetryAfter)
			}
		})
	}
}
func TestProbeDiscovery(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/" {
			w.Header().Set("Content-Type", "text/html")
			fmt.Fprint(w, `<html><head><link rel="alternate" type="application/rss+xml" href="/rss.xml"></head></html>`)
			return
		}
		if r.URL.Path == "/rss.xml" {
			fmt.Fprint(w, `<rss version="2.0"><channel><title>News</title></channel></rss>`)
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()
	f, _ := testFetcher(t, server)
	found, meta, _, err := f.Probe(context.Background(), server.URL, nil)
	if err != nil || !strings.HasSuffix(found, "/rss.xml") || meta.Title != "News" {
		t.Fatalf("%s %+v %v", found, meta, err)
	}
}

func TestRedirectAndRetryDate(t *testing.T) {
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/redirect":
			http.Redirect(w, r, "/feed", http.StatusMovedPermanently)
		case "/private":
			http.Redirect(w, r, "http://10.0.0.1/", http.StatusMovedPermanently)
		case "/rate":
			w.Header().Set("Retry-After", time.Now().Add(2*time.Hour).UTC().Format(http.TimeFormat))
			w.WriteHeader(503)
		default:
			fmt.Fprint(w, `<rss version="2.0"><channel><title>News</title></channel></rss>`)
		}
	}))
	defer server.Close()
	f, s := testFetcher(t, server)
	for _, path := range []string{"/redirect", "/private", "/rate"} {
		if err := f.Fetch(context.Background(), service.FeedToFetch{URL: server.URL + path}); err != nil {
			t.Fatal(err)
		}
	}
	if s.outcomes[0].PermanentRedirectURL != server.URL+"/feed" {
		t.Fatalf("redirect %+v", s.outcomes[0])
	}
	if s.outcomes[1].ErrorClass != "policy" {
		t.Fatalf("private %+v", s.outcomes[1])
	}
	if s.outcomes[2].RetryAfter < 119*time.Minute || s.outcomes[2].NextFetchAt.Sub(s.outcomes[2].AttemptedAt) < s.outcomes[2].RetryAfter {
		t.Fatalf("retry %+v", s.outcomes[2])
	}
}

func TestBodyLimit(t *testing.T) {
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
	f, s := testFetcher(t, server)
	if err := f.Fetch(context.Background(), service.FeedToFetch{URL: server.URL}); err != nil {
		t.Fatal(err)
	}
	if s.outcomes[0].ErrorClass != "size" {
		t.Fatalf("%+v", s.outcomes[0])
	}
}

func TestPersistedErrorClasses(t *testing.T) {
	// Every outbound class has an explicit service-vocabulary mapping; feed
	// health rejects anything else.
	for _, tc := range []struct {
		class outbound.Class
		want  string
	}{
		{outbound.DNS, "dns"},
		{outbound.Connect, "network"},
		{outbound.TLS, "tls"},
		{outbound.Timeout, "timeout"},
		{outbound.DeniedAddress, "policy"},
		{outbound.TooLarge, "size"},
		{outbound.HTTPGone, "gone"},
		{outbound.HTTP429, "rate_limit"},
		{outbound.HTTP403, "challenge"},
		{outbound.HTTP5xx, "http"},
		{outbound.RedirectLoop, "http"},
		{outbound.Decode, "parse"},
		{classHTTP4xx, "http"},
		{classParse, "parse"},
		{"something_new", "parse"},
	} {
		if got := persistedErrorClass(tc.class); got != tc.want {
			t.Errorf("persistedErrorClass(%q) = %q, want %q", tc.class, got, tc.want)
		}
	}
}

func TestConnectRefusedMapsToNetwork(t *testing.T) {
	c, err := outbound.New(outbound.Config{AllowHosts: []string{"127.0.0.1:1"}, Spacing: time.Nanosecond})
	if err != nil {
		t.Fatal(err)
	}
	s := &fakeService{}
	f, err := New(s, c, Config{ExtraPorts: []uint16{1}})
	if err != nil {
		t.Fatal(err)
	}
	if err := f.Fetch(context.Background(), service.FeedToFetch{URL: "http://127.0.0.1:1/"}); err != nil {
		t.Fatal(err)
	}
	if s.outcomes[0].ErrorClass != "network" {
		t.Fatalf("%+v", s.outcomes[0])
	}
}

func TestCompressedBombAndSlowServer(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/slow" {
			<-r.Context().Done()
			return
		}
		w.Header().Set("Content-Encoding", "gzip")
		gz := gzip.NewWriter(w)
		_, _ = gz.Write([]byte(strings.Repeat("a", 1<<20)))
		_ = gz.Close()
	}))
	defer server.Close()
	f, s := testFetcher(t, server)
	if err := f.Fetch(context.Background(), service.FeedToFetch{URL: server.URL}); err != nil {
		t.Fatal(err)
	}
	if s.outcomes[0].ErrorClass != "size" {
		t.Fatalf("gzip: %+v", s.outcomes[0])
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if err := f.Fetch(ctx, service.FeedToFetch{URL: server.URL + "/slow"}); err != nil {
		t.Fatal(err)
	}
	if s.outcomes[1].ErrorClass != "timeout" {
		t.Fatalf("slow: %+v", s.outcomes[1])
	}
}
func TestHostSpacing(t *testing.T) {
	var hits []time.Time
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits = append(hits, time.Now())
		fmt.Fprint(w, `<rss version="2.0"><channel><title>News</title></channel></rss>`)
	}))
	defer server.Close()
	f, _ := testFetcher(t, server)
	u, _ := url.Parse(server.URL)
	_, p, _ := net.SplitHostPort(u.Host)
	port, _ := strconv.Atoi(p)
	c, err := outbound.New(outbound.Config{AllowHosts: []string{u.Host}, Spacing: 50 * time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	f.client = c
	f.config.ExtraPorts = []uint16{uint16(port)}
	for _, path := range []string{"/one", "/two"} {
		if err := f.Fetch(context.Background(), service.FeedToFetch{URL: server.URL + path}); err != nil {
			t.Fatal(err)
		}
	}
	if len(hits) != 2 || hits[1].Sub(hits[0]) < 40*time.Millisecond {
		t.Fatalf("spacing: %v", hits)
	}
}

func TestFetchIsolatesUnsalvageableItems(t *testing.T) {
	deep := strings.Repeat("<div>", 2000) + "deep" + strings.Repeat("</div>", 2000)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, `<rss version="2.0"><channel><title>Feed</title>
<item><guid>good</guid><link>https://example.org/ok</link><description>fine</description></item>
<item><guid>deep</guid><link>https://example.org/deep</link><description><![CDATA[%s]]></description></item>
</channel></rss>`, deep)
	}))
	defer server.Close()
	f, s := testFetcher(t, server)
	if err := f.Fetch(context.Background(), service.FeedToFetch{FeedID: 1, URL: server.URL}); err != nil {
		t.Fatal(err)
	}
	if s.outcomes[0].Status != service.FetchOK {
		t.Fatalf("one unsalvageable item must not fail the fetch, got %s", s.outcomes[0].Status)
	}
	if len(s.outcomes[0].Items) != 2 {
		t.Fatalf("both items must reach ingest, got %d", len(s.outcomes[0].Items))
	}
	for _, item := range s.outcomes[0].Items {
		switch item.GUID {
		case "good":
			if item.ContentHTML != "fine" {
				t.Fatalf("good item lost its content: %q", item.ContentHTML)
			}
		case "deep":
			if item.ContentHTML != "" || item.ContentText != "" {
				t.Fatalf("unsalvageable item must store no content, got %q", item.ContentHTML)
			}
		default:
			t.Fatalf("unexpected guid %q", item.GUID)
		}
	}
}

func TestFetchDropsActiveSchemeLinks(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `<rss version="2.0"><channel><title>Feed</title>
<item><guid>js</guid><link>javascript:alert(1)</link><description>body</description></item>
<item><guid>data</guid><link>data:text/html,hi</link><description>body</description></item>
<item><guid>ok</guid><link>https://example.org/ok</link><description>body</description></item>
</channel></rss>`)
	}))
	defer server.Close()
	f, s := testFetcher(t, server)
	if err := f.Fetch(context.Background(), service.FeedToFetch{FeedID: 1, URL: server.URL}); err != nil {
		t.Fatal(err)
	}
	if s.outcomes[0].Status != service.FetchOK {
		t.Fatalf("status %s", s.outcomes[0].Status)
	}
	for _, item := range s.outcomes[0].Items {
		switch item.GUID {
		case "js", "data":
			if item.URL != "" {
				t.Fatalf("active-scheme link must not be stored, got %q", item.URL)
			}
			if item.ContentHTML != "body" {
				t.Fatalf("item should keep its content: %q", item.ContentHTML)
			}
		case "ok":
			if item.URL != "https://example.org/ok" {
				t.Fatalf("http(s) links must be kept, got %q", item.URL)
			}
		default:
			t.Fatalf("unexpected guid %q", item.GUID)
		}
	}
}

func TestIntervalLetsSlowFeedsBackOffToMaxInterval(t *testing.T) {
	f, _ := testFetcher(t, httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {})))
	now := time.Now()
	day := 24 * time.Hour
	parsed := &feed.Feed{Items: []feed.Item{
		{Published: func() *time.Time { v := now.Add(-14 * day); return &v }()},
		{Published: func() *time.Time { v := now.Add(-7 * day); return &v }()},
		{Published: func() *time.Time { v := now; return &v }()},
	}}
	got := f.interval(service.FeedToFetch{}, parsed, service.FetchOK, "")
	if got != f.config.MaxInterval {
		t.Fatalf("a weekly feed must poll at MaxInterval (%v), got %v", f.config.MaxInterval, got)
	}
}

func TestFetchBoundsExtractionWork(t *testing.T) {
	var pageHits atomic.Int32
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/page") {
			pageHits.Add(1)
			fmt.Fprint(w, "<html><body><article><h1>Story</h1><p>a readable article body with enough words</p></article></body></html>")
			return
		}
		var b strings.Builder
		b.WriteString(`<rss version="2.0"><channel><title>Feed</title>`)
		for i := 0; i < 20; i++ {
			fmt.Fprintf(&b, `<item><guid>g%d</guid><link>%s/page%d</link><description>body %d</description></item>`, i, server.URL, i, i)
		}
		b.WriteString(`</channel></rss>`)
		fmt.Fprint(w, b.String())
	}))
	defer server.Close()
	f, s := testFetcher(t, server)
	claim := service.FeedToFetch{FeedID: 1, URL: server.URL, Extract: service.ExtractAlways, LeaseUntil: time.Now().Add(10 * time.Minute)}
	if err := f.Fetch(context.Background(), claim); err != nil {
		t.Fatal(err)
	}
	if s.outcomes[0].Status != service.FetchOK || len(s.outcomes[0].Items) != 20 {
		t.Fatalf("all items must be ingested, got %s with %d items", s.outcomes[0].Status, len(s.outcomes[0].Items))
	}
	if got := pageHits.Load(); got != maxExtractsPerFetch {
		t.Fatalf("one pass must extract at most %d pages, got %d", maxExtractsPerFetch, got)
	}
}

func TestFetchStopsExtractingNearTheLeaseDeadline(t *testing.T) {
	var pageHits atomic.Int32
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/page") {
			pageHits.Add(1)
			fmt.Fprint(w, "<html><body><article><h1>Story</h1><p>a readable article body with enough words</p></article></body></html>")
			return
		}
		fmt.Fprintf(w, `<rss version="2.0"><channel><title>Feed</title><item><guid>g</guid><link>%s/page0</link><description>body</description></item></channel></rss>`, server.URL)
	}))
	defer server.Close()
	f, _ := testFetcher(t, server)
	claim := service.FeedToFetch{FeedID: 1, URL: server.URL, Extract: service.ExtractAlways, LeaseUntil: time.Now().Add(time.Second)}
	if err := f.Fetch(context.Background(), claim); err != nil {
		t.Fatal(err)
	}
	if got := pageHits.Load(); got != 0 {
		t.Fatalf("a lease about to expire must defer extraction, got %d page fetches", got)
	}
}
