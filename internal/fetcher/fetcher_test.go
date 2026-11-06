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

func TestSameOriginScoping(t *testing.T) {
	cases := []struct {
		a, b string
		same bool
	}{
		{"https://example.test/feed", "https://example.test/other", true},
		{"https://example.test/feed", "https://example.test:443/other", true},
		{"http://example.test/feed", "http://example.test:80/other", true},
		{"http://[::1]:8080/a", "http://[::1]:8080/b", true},
		{"https://example.test/feed", "https://other.test/feed", false},
		{"https://example.test/feed", "http://example.test/feed", false},
		{"https://example.test:8443/feed", "https://example.test/feed", false},
		{"/feed", "/other", false},
		{"https://example.test/feed", "not a url", false},
	}
	for _, c := range cases {
		if sameOrigin(c.a, c.b) != c.same || sameOrigin(c.b, c.a) != c.same {
			t.Fatalf("sameOrigin(%q,%q) != %v", c.a, c.b, c.same)
		}
	}
}

func TestFetchScopesCredentialsToCanonicalOrigin(t *testing.T) {
	body := `<rss version="2.0"><channel><title>Feed</title><item><guid>abc</guid><link>https://example.org/article</link><description>d</description></item></channel></rss>`
	var movedAuth, homeAuth string
	var movedHits, homeHits atomic.Int32
	moved := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		movedHits.Add(1)
		movedAuth = r.Header.Get("Authorization")
		fmt.Fprint(w, body)
	}))
	defer moved.Close()
	home := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		homeHits.Add(1)
		homeAuth = r.Header.Get("Authorization")
		fmt.Fprint(w, body)
	}))
	defer home.Close()
	ports := make([]uint16, 0, 2)
	for _, server := range []*httptest.Server{home, moved} {
		u, _ := url.Parse(server.URL)
		_, p, _ := net.SplitHostPort(u.Host)
		port, _ := strconv.Atoi(p)
		ports = append(ports, uint16(port))
	}
	c, err := outbound.New(outbound.Config{AllowHosts: []string{mustHost(t, home), mustHost(t, moved)}, Spacing: time.Nanosecond})
	if err != nil {
		t.Fatal(err)
	}
	f, err := New(&fakeService{}, c, Config{ExtraPorts: ports})
	if err != nil {
		t.Fatal(err)
	}
	creds := &service.FeedCredentials{Username: "u", Password: "p"}
	ctx := context.Background()
	// A recorded permanent redirect to another origin must not carry the
	// feed's credentials to the new host.
	if err := f.Fetch(ctx, service.FeedToFetch{FeedID: 1, URL: home.URL + "/feed", FetchURL: moved.URL + "/feed", Credentials: creds}); err != nil {
		t.Fatal(err)
	}
	// The fetch must target the recorded redirect, or the leak check below
	// would pass vacuously.
	if movedHits.Load() != 1 || homeHits.Load() != 0 {
		t.Fatalf("fetch hit moved=%d home=%d, want the redirect target only", movedHits.Load(), homeHits.Load())
	}
	if movedAuth != "" {
		t.Fatalf("credentials leaked to the redirect target: %q", movedAuth)
	}
	// The feed's own origin still receives them, including on another path.
	if err := f.Fetch(ctx, service.FeedToFetch{FeedID: 1, URL: home.URL + "/feed", FetchURL: home.URL + "/other", Credentials: creds}); err != nil {
		t.Fatal(err)
	}
	if homeHits.Load() != 1 || homeAuth == "" {
		t.Fatal("credentials missing on the feed's own origin")
	}
}

func mustHost(t *testing.T, server *httptest.Server) string {
	t.Helper()
	u, err := url.Parse(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	return u.Host
}
