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
	"sync"
	"testing"
	"time"

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

// The two article fetches must overlap: each handler only releases once both
// requests have arrived, so a serial fetcher times out and the items fall back
// to feed content. The articles use different hostnames because the outbound
// client spaces requests per hostname.
func TestFullTextExtractionOverlaps(t *testing.T) {
	var mu sync.Mutex
	arrived := 0
	release := make(chan struct{})
	articleHandler := func(w http.ResponseWriter, _ *http.Request) {
		mu.Lock()
		arrived++
		if arrived == 2 {
			close(release)
		}
		mu.Unlock()
		select {
		case <-release:
		case <-time.After(5 * time.Second):
			http.Error(w, "timeout", http.StatusInternalServerError)
			return
		}
		_, _ = fmt.Fprintf(w, `<html><body><article><p>%s</p></article></body></html>`, strings.Repeat("word ", 200))
	}
	articleA := httptest.NewServer(http.HandlerFunc(articleHandler))
	defer articleA.Close()
	articleB := httptest.NewServer(http.HandlerFunc(articleHandler))
	defer articleB.Close()
	articleAURL := strings.Replace(articleA.URL, "127.0.0.1", "localhost", 1)
	feed := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = fmt.Fprintf(w, `<?xml version="1.0"?><rss version="2.0"><channel><title>t</title>`+
			`<item><title>a</title><link>%s</link><description>short</description></item>`+
			`<item><title>b</title><link>%s</link><description>short</description></item>`+
			`</channel></rss>`, articleAURL, articleB.URL)
	}))
	defer feed.Close()

	hosts := make([]string, 0, 3)
	ports := make([]uint16, 0, 3)
	for _, raw := range []string{feed.URL, articleAURL, articleB.URL} {
		u, err := url.Parse(raw)
		if err != nil {
			t.Fatal(err)
		}
		hosts = append(hosts, u.Host)
		_, p, _ := net.SplitHostPort(u.Host)
		n, _ := strconv.Atoi(p)
		ports = append(ports, uint16(n))
	}
	c, err := outbound.New(outbound.Config{AllowHosts: hosts, Spacing: time.Nanosecond})
	if err != nil {
		t.Fatal(err)
	}
	s := &fakeService{}
	f, err := New(s, c, Config{ExtraPorts: ports})
	if err != nil {
		t.Fatal(err)
	}
	if err := f.Fetch(context.Background(), service.FeedToFetch{FeedID: 1, URL: feed.URL, Extract: service.ExtractAlways}); err != nil {
		t.Fatal(err)
	}
	if len(s.outcomes) != 1 {
		t.Fatalf("outcomes: %d", len(s.outcomes))
	}
	items := s.outcomes[0].Items
	if len(items) != 2 {
		t.Fatalf("items: %d", len(items))
	}
	for i, item := range items {
		if item.ContentSource != service.ContentFromExtracted {
			t.Fatalf("item %d source = %s, want extracted (title %q)", i, item.ContentSource, item.Title)
		}
	}
}

func testFetcherForServers(t *testing.T, servers ...*httptest.Server) (*Fetcher, *fakeService) {
	t.Helper()
	hosts := make([]string, 0, len(servers))
	ports := make([]uint16, 0, len(servers))
	for _, server := range servers {
		u, err := url.Parse(server.URL)
		if err != nil {
			t.Fatal(err)
		}
		hosts = append(hosts, u.Host)
		_, p, _ := net.SplitHostPort(u.Host)
		n, _ := strconv.Atoi(p)
		ports = append(ports, uint16(n))
	}
	c, err := outbound.New(outbound.Config{AllowHosts: hosts, Spacing: time.Nanosecond})
	if err != nil {
		t.Fatal(err)
	}
	s := &fakeService{}
	f, err := New(s, c, Config{ExtraPorts: ports})
	if err != nil {
		t.Fatal(err)
	}
	return f, s
}

// Cancelling the context must not leave the producer blocked on a job channel
// whose workers have all exited.
func TestFullTextExtractionCancellationDoesNotHang(t *testing.T) {
	var once sync.Once
	arrived := make(chan struct{})
	release := make(chan struct{})
	article := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		once.Do(func() { close(arrived) })
		<-release
	}))
	defer article.Close()
	feedServed := make(chan struct{})
	feed := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		close(feedServed)
		_, _ = fmt.Fprintf(w, `<?xml version="1.0"?><rss version="2.0"><channel><title>t</title>%s</channel></rss>`,
			strings.Repeat(`<item><title>a</title><link>`+article.URL+`</link><description>short</description></item>`, 5))
	}))
	defer feed.Close()
	f, _ := testFetcherForServers(t, feed, article)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- f.Fetch(ctx, service.FeedToFetch{FeedID: 1, URL: feed.URL, Extract: service.ExtractAlways})
	}()
	<-feedServed
	select {
	case <-arrived:
	case <-time.After(5 * time.Second):
		cancel()
		t.Fatal("extraction never started")
	}
	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Fetch deadlocked after cancellation")
	}
	close(release)
}
