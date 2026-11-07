package imageproxy

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/L-K-M/Earnest/internal/outbound"
)

func newTestProxy(t *testing.T, allow bool, servers ...*httptest.Server) *Proxy {
	t.Helper()
	cfg := outbound.Config{Spacing: time.Nanosecond}
	if allow {
		cfg.AllowHosts = []string{servers[0].Listener.Addr().String()}
	}
	c, err := outbound.New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	p, err := New([]byte(strings.Repeat("s", 32)), c, "https://reader.example")
	if err != nil {
		t.Fatal(err)
	}
	for _, server := range servers {
		u, _ := url.Parse(server.URL)
		n, _ := strconv.Atoi(u.Port())
		p.extraPorts = append(p.extraPorts, uint16(n))
	}
	return p
}
func request(t *testing.T, p *Proxy, source string) *httptest.ResponseRecorder {
	t.Helper()
	u, err := url.Parse(source)
	if err != nil {
		t.Fatal(err)
	}
	signed, err := p.URL(u)
	if err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	p.ServeHTTP(w, httptest.NewRequest(http.MethodGet, signed.String(), nil))
	return w
}
func TestFetchRestrictions(t *testing.T) {
	png := "\x89PNG\r\n\x1a\n" + strings.Repeat("x", 20)
	for _, tc := range []struct {
		name, body string
		want       int
	}{
		{"png", png, 200}, {"svg", `<svg xmlns="http://www.w3.org/2000/svg"></svg>`, 415}, {"html", "<html>bad</html>", 415}, {"large", png + strings.Repeat("x", maxImageSize), 502},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Header.Get("Cookie") != "" || r.Header.Get("Referer") != "" {
					t.Error("forwarded private headers")
				}
				w.Header().Set("Content-Type", "image/png")
				w.Header().Set("ETag", `"tag"`)
				fmt.Fprint(w, tc.body)
			}))
			defer s.Close()
			p := newTestProxy(t, true, s)
			w := request(t, p, s.URL)
			if w.Code != tc.want {
				t.Fatalf("status = %d, want %d", w.Code, tc.want)
			}
			if tc.want == 200 && (w.Header().Get("ETag") != `"tag"` || w.Header().Get("X-Content-Type-Options") != "nosniff") {
				t.Fatal(w.Header())
			}
		})
	}
}
func TestDeniedAndTampered(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { t.Error("private destination reached") }))
	defer s.Close()
	p := newTestProxy(t, false, s)
	if w := request(t, p, s.URL); w.Code != 502 {
		t.Fatal(w.Code)
	}
	u, _ := url.Parse(s.URL)
	signed, _ := p.URL(u)
	signed.Path += "x"
	w := httptest.NewRecorder()
	p.ServeHTTP(w, httptest.NewRequest("GET", signed.String(), nil))
	if w.Code != 404 {
		t.Fatal(w.Code)
	}
}
func TestRedirectPrivateDenied(t *testing.T) {
	dst := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { t.Error("private redirect reached") }))
	defer dst.Close()
	src := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, dst.URL, 302) }))
	defer src.Close()
	p := newTestProxy(t, true, src, dst)
	if w := request(t, p, src.URL); w.Code != 502 {
		t.Fatal(w.Code)
	}
}
func TestRewriteModes(t *testing.T) {
	c, _ := outbound.New(outbound.Config{})
	p, _ := New([]byte(strings.Repeat("s", 32)), c, "https://reader.example")
	input := `<img src="https://example.com/a" srcset="https://example.com/b 2x">`
	for _, tc := range []struct {
		mode               Mode
		contains, excludes string
	}{{Proxied, "<img", "https://example.com/"}, {ClickToLoad, "<a ", "<img"}} {
		out, err := p.Rewrite(input, tc.mode)
		if err != nil || !strings.Contains(out, tc.contains) || strings.Contains(out, tc.excludes) {
			t.Fatalf("%q %v", out, err)
		}
	}
}

func TestRewriteDropsUnsupportedImageSources(t *testing.T) {
	c, err := outbound.New(outbound.Config{})
	if err != nil {
		t.Fatal(err)
	}
	p, err := New([]byte(strings.Repeat("s", 32)), c, "https://reader.example")
	if err != nil {
		t.Fatal(err)
	}
	for _, mode := range []Mode{Proxied, ClickToLoad} {
		out, err := p.Rewrite(`<p>Article</p><img src="https://user:pass@example.com/a"><img src="https://example.com/b">`, mode)
		if err != nil || !strings.Contains(out, "Article") || strings.Contains(out, "user:pass") || !strings.Contains(out, "/img/") {
			t.Fatalf("mode %d: %q %v", mode, out, err)
		}
	}
}

func TestConfiguredExtraPort(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "\x89PNG\r\n\x1a\n"+strings.Repeat("x", 20))
	}))
	defer s.Close()
	c, err := outbound.New(outbound.Config{AllowHosts: []string{s.Listener.Addr().String()}})
	if err != nil {
		t.Fatal(err)
	}
	u, _ := url.Parse(s.URL)
	n, _ := strconv.Atoi(u.Port())
	without, err := New([]byte(strings.Repeat("s", 32)), c, "https://reader.example")
	if err != nil {
		t.Fatal(err)
	}
	if w := request(t, without, s.URL); w.Code != http.StatusBadGateway {
		t.Fatalf("without configured port: %d", w.Code)
	}
	p, err := New([]byte(strings.Repeat("s", 32)), c, "https://reader.example", uint16(n))
	if err != nil {
		t.Fatal(err)
	}
	if w := request(t, p, s.URL); w.Code != http.StatusOK {
		t.Fatalf("status = %d", w.Code)
	}
}

func TestProxyCachesBodiesAndAnswersRevalidation(t *testing.T) {
	png := "\x89PNG\r\n\x1a\n" + strings.Repeat("x", 20)
	var hits int
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		fmt.Fprint(w, png)
	}))
	defer s.Close()
	p := newTestProxy(t, true, s)
	u, _ := url.Parse(s.URL)
	signed, _ := p.URL(u)

	first := httptest.NewRecorder()
	p.ServeHTTP(first, httptest.NewRequest(http.MethodGet, signed.String(), nil))
	if first.Code != 200 || hits != 1 {
		t.Fatalf("first view: status %d upstream hits %d", first.Code, hits)
	}
	etag := first.Header().Get("ETag")
	if etag == "" {
		t.Fatal("a validator must be emitted so browsers can revalidate")
	}

	// A repeat view and a browser revalidation both answer from the cache.
	again := httptest.NewRecorder()
	p.ServeHTTP(again, httptest.NewRequest(http.MethodGet, signed.String(), nil))
	if again.Code != 200 || again.Body.String() != first.Body.String() || hits != 1 {
		t.Fatalf("cached view: status %d hits %d", again.Code, hits)
	}
	revalidated := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, signed.String(), nil)
	req.Header.Set("If-None-Match", etag)
	p.ServeHTTP(revalidated, req)
	if revalidated.Code != http.StatusNotModified || hits != 1 {
		t.Fatalf("revalidation: status %d hits %d", revalidated.Code, hits)
	}
}

func TestProxyRevalidatesUpstreamAfterTTL(t *testing.T) {
	png := "\x89PNG\r\n\x1a\n" + strings.Repeat("x", 20)
	var hits int
	var sawValidator string
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		sawValidator = r.Header.Get("If-None-Match")
		if hits == 1 {
			w.Header().Set("ETag", `"tag"`)
			fmt.Fprint(w, png)
			return
		}
		w.WriteHeader(http.StatusNotModified)
	}))
	defer s.Close()
	p := newTestProxy(t, true, s)
	p.ttl = time.Nanosecond // force revalidation on every view
	u, _ := url.Parse(s.URL)
	signed, _ := p.URL(u)

	first := httptest.NewRecorder()
	p.ServeHTTP(first, httptest.NewRequest(http.MethodGet, signed.String(), nil))
	second := httptest.NewRecorder()
	p.ServeHTTP(second, httptest.NewRequest(http.MethodGet, signed.String(), nil))
	if second.Code != 200 || second.Body.String() != first.Body.String() {
		t.Fatalf("304 upstream must keep the cached body: status %d", second.Code)
	}
	if hits != 2 || sawValidator != `"tag"` {
		t.Fatalf("upstream revalidation must be conditional: hits %d validator %q", hits, sawValidator)
	}
}

func TestProxyServesStaleWhenPublisherFails(t *testing.T) {
	png := "\x89PNG\r\n\x1a\n" + strings.Repeat("x", 20)
	var hits int
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		if hits == 1 {
			fmt.Fprint(w, png)
			return
		}
		http.Error(w, "down", http.StatusInternalServerError)
	}))
	defer s.Close()
	p := newTestProxy(t, true, s)
	p.ttl = time.Nanosecond
	u, _ := url.Parse(s.URL)
	signed, _ := p.URL(u)

	first := httptest.NewRecorder()
	p.ServeHTTP(first, httptest.NewRequest(http.MethodGet, signed.String(), nil))
	second := httptest.NewRecorder()
	p.ServeHTTP(second, httptest.NewRequest(http.MethodGet, signed.String(), nil))
	if second.Code != 200 || second.Body.String() != first.Body.String() {
		t.Fatalf("a cached body must survive publisher failure: status %d", second.Code)
	}
}

func TestProxyStaleFallbackClassification(t *testing.T) {
	png := "\x89PNG\r\n\x1a\n" + strings.Repeat("x", 20)
	for _, tc := range []struct {
		name       string
		status     int
		wantSecond int
	}{
		{"404 evicts", http.StatusNotFound, http.StatusBadGateway},
		{"410 evicts", http.StatusGone, http.StatusBadGateway},
		{"401 evicts", http.StatusUnauthorized, http.StatusBadGateway},
		{"500 serves stale", http.StatusInternalServerError, http.StatusOK},
		{"429 serves stale", http.StatusTooManyRequests, http.StatusOK},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var hits int
			s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				hits++
				if hits == 1 {
					fmt.Fprint(w, png)
					return
				}
				w.WriteHeader(tc.status)
			}))
			defer s.Close()
			p := newTestProxy(t, true, s)
			p.ttl = time.Nanosecond
			u, _ := url.Parse(s.URL)
			signed, _ := p.URL(u)

			first := httptest.NewRecorder()
			p.ServeHTTP(first, httptest.NewRequest(http.MethodGet, signed.String(), nil))
			if first.Code != http.StatusOK {
				t.Fatalf("first view: status %d", first.Code)
			}
			second := httptest.NewRecorder()
			p.ServeHTTP(second, httptest.NewRequest(http.MethodGet, signed.String(), nil))
			if second.Code != tc.wantSecond {
				t.Fatalf("second view: status %d, want %d", second.Code, tc.wantSecond)
			}
			if tc.wantSecond == http.StatusOK && second.Body.String() != first.Body.String() {
				t.Fatal("transient failure must serve the cached body")
			}
			if tc.wantSecond == http.StatusBadGateway {
				third := httptest.NewRecorder()
				p.ServeHTTP(third, httptest.NewRequest(http.MethodGet, signed.String(), nil))
				if third.Code != http.StatusBadGateway || hits != 3 {
					t.Fatalf("definitive failure must evict and refetch upstream: status %d hits %d", third.Code, hits)
				}
			}
		})
	}
}

func TestProxyStaleTouchKeepsNewerEntry(t *testing.T) {
	png := "\x89PNG\r\n\x1a\n" + strings.Repeat("x", 20)
	var p *Proxy
	var source string
	var hits int
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		if hits == 1 {
			fmt.Fprint(w, png)
			return
		}
		// Simulate a concurrent request storing a newer body while this
		// request's revalidation is in flight.
		p.cache.put(source, cachedImage{body: []byte("newer"), kind: "image/png", etag: `"newer"`, added: time.Now()})
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer s.Close()
	p = newTestProxy(t, true, s)
	p.ttl = time.Nanosecond
	source = s.URL
	u, _ := url.Parse(s.URL)
	signed, _ := p.URL(u)

	first := httptest.NewRecorder()
	p.ServeHTTP(first, httptest.NewRequest(http.MethodGet, signed.String(), nil))
	if first.Code != http.StatusOK {
		t.Fatalf("first view: status %d", first.Code)
	}
	second := httptest.NewRecorder()
	p.ServeHTTP(second, httptest.NewRequest(http.MethodGet, signed.String(), nil))
	if second.Code != http.StatusOK || second.Body.String() != png {
		t.Fatalf("transient failure must serve the cached snapshot: status %d", second.Code)
	}
	got, ok := p.cache.get(source)
	if !ok || string(got.body) != "newer" {
		t.Fatalf("stale fallback must not overwrite a newer cached body: %q", got.body)
	}
}

func TestProxyIfNoneMatchRevalidation(t *testing.T) {
	png := "\x89PNG\r\n\x1a\n" + strings.Repeat("x", 20)
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("ETag", `"v1"`)
		fmt.Fprint(w, png)
	}))
	defer s.Close()
	p := newTestProxy(t, true, s)
	u, _ := url.Parse(s.URL)
	signed, _ := p.URL(u)

	first := httptest.NewRecorder()
	p.ServeHTTP(first, httptest.NewRequest(http.MethodGet, signed.String(), nil))
	if first.Code != http.StatusOK {
		t.Fatalf("first view: status %d", first.Code)
	}
	for _, tc := range []struct {
		name, header string
		want         int
	}{
		{"exact", `"v1"`, http.StatusNotModified},
		{"list", `"other", "v1"`, http.StatusNotModified},
		{"weak", `W/"v1"`, http.StatusNotModified},
		{"weak list", `W/"other", W/"v1"`, http.StatusNotModified},
		{"star", `*`, http.StatusNotModified},
		{"miss", `"v2"`, http.StatusOK},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, signed.String(), nil)
			req.Header.Set("If-None-Match", tc.header)
			w := httptest.NewRecorder()
			p.ServeHTTP(w, req)
			if w.Code != tc.want {
				t.Fatalf("If-None-Match %q: status %d, want %d", tc.header, w.Code, tc.want)
			}
		})
	}
}

func TestEtagMatches(t *testing.T) {
	for _, tc := range []struct {
		name, header, etag string
		want               bool
	}{
		{"exact", `"v1"`, `"v1"`, true},
		{"list", `"a", "v1"`, `"v1"`, true},
		{"weak client", `W/"v1"`, `"v1"`, true},
		{"weak cached", `"v1"`, `W/"v1"`, true},
		{"weak both", `W/"v1"`, `W/"v1"`, true},
		{"star", `*`, `"v1"`, true},
		{"miss", `"v2"`, `"v1"`, false},
		{"weak miss", `W/"v2"`, `"v1"`, false},
		{"empty", ``, `"v1"`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := etagMatches(tc.header, tc.etag); got != tc.want {
				t.Fatalf("etagMatches(%q, %q) = %v, want %v", tc.header, tc.etag, got, tc.want)
			}
		})
	}
}
