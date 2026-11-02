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
