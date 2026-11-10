package web

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"

	"github.com/L-K-M/Earnest/internal/imageproxy"
	"github.com/L-K-M/Earnest/internal/outbound"
	"github.com/L-K-M/Earnest/internal/service"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
)

func TestTicketValidation(t *testing.T) {
	now := time.Unix(100, 0)
	s := &TicketSigner{key: []byte("01234567890123456789012345678901"), now: func() time.Time { return now }}
	valid := Ticket{UserID: 1, PrincipalID: 2, Kind: service.PrincipalHuman, EntryID: 3, Expires: 101, Mode: ImageClickToLoad}
	sig := s.Sign(valid)
	for _, tc := range []struct {
		name, sig string
		want      bool
	}{
		{"valid", sig, true},
		{"tampered", sig + "x", false},
		{"expired", s.Sign(Ticket{UserID: 1, PrincipalID: 2, Kind: service.PrincipalHuman, EntryID: 3, Expires: 100, Mode: ImageClickToLoad}), false},
		{"invalid mode", s.Sign(Ticket{UserID: 1, PrincipalID: 2, Kind: service.PrincipalHuman, EntryID: 3, Expires: 101, Mode: "other"}), false},
		{"invalid kind", s.Sign(Ticket{UserID: 1, PrincipalID: 2, Kind: "other", EntryID: 3, Expires: 101, Mode: ImageClickToLoad}), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, ok := s.Verify(tc.sig)
			if ok != tc.want {
				t.Fatalf("verify = %v", ok)
			}
		})
	}
}

func TestAppPolicyAndFallback(t *testing.T) {
	w := httptest.NewRecorder()
	New("https://content.example").ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/later", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d", w.Code)
	}
	if !strings.Contains(w.Header().Get("Content-Security-Policy"), "frame-src https://content.example") {
		t.Fatalf("CSP = %q", w.Header().Get("Content-Security-Policy"))
	}
}

type fakeContent struct{}

func (fakeContent) Content(_ context.Context, _ service.Actor, id int64) (service.EntryContent, error) {
	if id == 404 {
		return service.EntryContent{}, service.ErrNotFound
	}
	if id == 403 {
		return service.EntryContent{}, fmt.Errorf("entry %d: %w", id, service.ErrForbidden)
	}
	if id == 500 {
		return service.EntryContent{}, errors.New("database unavailable")
	}
	return service.EntryContent{HTML: "<p>Article</p>"}, nil
}

func newContentRecorder(t *testing.T, query string) *httptest.ResponseRecorder {
	t.Helper()
	egress, err := outbound.New(outbound.Config{Version: "test"})
	if err != nil {
		t.Fatal(err)
	}
	proxy, err := imageproxy.New([]byte(strings.Repeat("x", 32)), egress, "https://content.example")
	if err != nil {
		t.Fatal(err)
	}
	c := &Content{svc: fakeContent{}, proxy: proxy, appOrigin: "https://app.example", signer: &TicketSigner{key: []byte(strings.Repeat("x", 32)), origin: "https://content.example", now: func() time.Time { return time.Unix(100, 0) }}}
	sig := c.signer.Sign(Ticket{UserID: 1, PrincipalID: 2, Kind: service.PrincipalHuman, EntryID: 3, Expires: 101, Mode: ImageProxied})
	w := httptest.NewRecorder()
	c.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/content/3?sig="+sig+query, nil))
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d", w.Code)
	}
	return w
}

func TestContentStyleHashMatchesCSS(t *testing.T) {
	w := newContentRecorder(t, "")
	body := w.Body.String()
	start := strings.Index(body, "<style>")
	end := strings.Index(body, "</style>")
	if start < 0 || end < 0 || end < start {
		t.Fatalf("style element not found in %q", body)
	}
	css := body[start+len("<style>") : end]
	sum := sha256.Sum256([]byte(css))
	want := "style-src 'sha256-" + base64.StdEncoding.EncodeToString(sum[:]) + "'"
	if policy := w.Header().Get("Content-Security-Policy"); !strings.Contains(policy, want) {
		t.Fatalf("CSP = %q, want %q", policy, want)
	}
}

func TestContentDarkThemeStyles(t *testing.T) {
	w := newContentRecorder(t, "&theme=dark")
	body := w.Body.String()
	for _, want := range []string{
		"border-inline-start:3px solid #a3a09a",
		"border:1px solid #a3a09a",
		"background:#2e2e2c",
		"blockquote{",
		"table{",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("body missing %q", want)
		}
	}
}

func TestContentPolicy(t *testing.T) {
	egress, err := outbound.New(outbound.Config{Version: "test"})
	if err != nil {
		t.Fatal(err)
	}
	proxy, err := imageproxy.New([]byte(strings.Repeat("x", 32)), egress, "https://content.example")
	if err != nil {
		t.Fatal(err)
	}
	c := &Content{svc: fakeContent{}, proxy: proxy, appOrigin: "https://app.example", signer: &TicketSigner{key: []byte(strings.Repeat("x", 32)), origin: "https://content.example", now: func() time.Time { return time.Unix(100, 0) }}}
	sig := c.signer.Sign(Ticket{UserID: 1, PrincipalID: 2, Kind: service.PrincipalHuman, EntryID: 3, Expires: 101, Mode: ImageProxied})
	w := httptest.NewRecorder()
	c.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/content/3?sig="+sig, nil))
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d", w.Code)
	}
	policy := w.Header().Get("Content-Security-Policy")
	if !strings.Contains(policy, "default-src 'none'") || !strings.Contains(policy, "img-src https://content.example") || !strings.Contains(policy, "style-src 'sha256-") {
		t.Fatalf("CSP = %q", policy)
	}
	// The page's own script is the only one allowed: one hash, no
	// 'unsafe-inline', no host or scheme sources, no handler attributes.
	var scriptSrc string
	for _, directive := range strings.Split(policy, ";") {
		if fields := strings.Fields(directive); len(fields) > 0 && fields[0] == "script-src" {
			scriptSrc = strings.TrimSpace(directive)
		}
	}
	if fields := strings.Fields(scriptSrc); len(fields) != 2 || !strings.HasPrefix(fields[1], "'sha256-") {
		t.Fatalf("script-src = %q, want exactly one hash", scriptSrc)
	}
}

// The article page's script must be admitted by its hash and nothing else,
// and it reports only to the configured app origin.
func TestContentScriptHashMatchesScript(t *testing.T) {
	w := newContentRecorder(t, "")
	body := w.Body.String()
	start := strings.Index(body, "<script>")
	end := strings.Index(body, "</script>")
	if start < 0 || end < start {
		t.Fatalf("script element not found in %q", body)
	}
	if head := strings.Index(body, "</head>"); head < end {
		t.Fatal("the script must sit in the head, before any article markup")
	}
	script := body[start+len("<script>") : end]
	sum := sha256.Sum256([]byte(script))
	want := "script-src 'sha256-" + base64.StdEncoding.EncodeToString(sum[:]) + "'"
	if policy := w.Header().Get("Content-Security-Policy"); !strings.Contains(policy, want) {
		t.Fatalf("CSP = %q, want %q", policy, want)
	}
	if !strings.Contains(script, `const app="https://app.example"`) || strings.Count(script, "postMessage(") != 2 || strings.Count(script, ",app)") != 2 {
		t.Fatalf("script must post only to the app origin: %q", script)
	}
	// Space on a focused summary must still open its details.
	if !strings.Contains(script, `closest("summary")`) {
		t.Fatalf("script must leave Space on a summary alone: %q", script)
	}
	// Shift with arrows, Home, End or Page keys must still extend a selection.
	if !strings.Contains(script, `if(e.shiftKey&&k!==" "&&scroll.has(k))return;`) {
		t.Fatalf("script must leave selection keys alone: %q", script)
	}
}

// Without an app origin the page stays script-free and scrolls in its frame.
func TestContentWithoutAppOriginHasNoScript(t *testing.T) {
	egress, err := outbound.New(outbound.Config{Version: "test"})
	if err != nil {
		t.Fatal(err)
	}
	proxy, err := imageproxy.New([]byte(strings.Repeat("x", 32)), egress, "https://content.example")
	if err != nil {
		t.Fatal(err)
	}
	c := &Content{svc: fakeContent{}, proxy: proxy, signer: &TicketSigner{key: []byte(strings.Repeat("x", 32)), origin: "https://content.example", now: func() time.Time { return time.Unix(100, 0) }}}
	sig := c.signer.Sign(Ticket{UserID: 1, PrincipalID: 2, Kind: service.PrincipalHuman, EntryID: 3, Expires: 101, Mode: ImageProxied})
	w := httptest.NewRecorder()
	c.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/content/3?sig="+sig, nil))
	if strings.Contains(w.Header().Get("Content-Security-Policy"), "script-src") || strings.Contains(w.Body.String(), "<script") {
		t.Fatalf("CSP = %q, body = %q", w.Header().Get("Content-Security-Policy"), w.Body.String())
	}
}

// The frame may run the page's hashed script, but it must never share an
// origin with anything: allow-same-origin would hand the article the
// content origin's storage.
func TestReaderIframeSandbox(t *testing.T) {
	source, err := os.ReadFile("../../web/src/ArticleFrame.tsx")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(source), `sandbox="allow-scripts allow-popups allow-popups-to-escape-sandbox"`) || strings.Contains(string(source), "allow-same-origin") {
		t.Fatal("reader iframe sandbox changed")
	}
}

func TestExpiredTicketPage(t *testing.T) {
	egress, err := outbound.New(outbound.Config{Version: "test"})
	if err != nil {
		t.Fatal(err)
	}
	proxy, err := imageproxy.New([]byte(strings.Repeat("x", 32)), egress, "https://content.example")
	if err != nil {
		t.Fatal(err)
	}
	c := &Content{svc: fakeContent{}, proxy: proxy, signer: &TicketSigner{key: []byte(strings.Repeat("x", 32)), origin: "https://content.example", now: func() time.Time { return time.Unix(200, 0) }}}
	sig := c.signer.Sign(Ticket{UserID: 1, PrincipalID: 2, Kind: service.PrincipalHuman, EntryID: 3, Expires: 100, Mode: ImageProxied})
	w := httptest.NewRecorder()
	c.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/content/3?sig="+sig, nil))
	if w.Code != http.StatusGone {
		t.Fatalf("status = %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), "This article link has expired") {
		t.Fatalf("body = %q", w.Body.String())
	}
	if policy := w.Header().Get("Content-Security-Policy"); !strings.Contains(policy, "default-src 'none'") {
		t.Fatalf("CSP = %q", policy)
	}
	// Invalid signature and entry-ID mismatch take the same branch.
	w2 := httptest.NewRecorder()
	c.ServeHTTP(w2, httptest.NewRequest(http.MethodGet, "/content/999?sig="+sig, nil))
	if !strings.Contains(w2.Body.String(), "This article link has expired") {
		t.Fatalf("mismatch body = %q", w2.Body.String())
	}
	w3 := httptest.NewRecorder()
	c.ServeHTTP(w3, httptest.NewRequest(http.MethodGet, "/content/3?sig=garbage", nil))
	if !strings.Contains(w3.Body.String(), "This article link has expired") {
		t.Fatalf("bad sig body = %q", w3.Body.String())
	}
}

// Every page the reader's frame can show reports its height, so a notice
// never inherits the previous article's height.
func TestContentNoticesCarryTheFrameScript(t *testing.T) {
	egress, err := outbound.New(outbound.Config{Version: "test"})
	if err != nil {
		t.Fatal(err)
	}
	proxy, err := imageproxy.New([]byte(strings.Repeat("x", 32)), egress, "https://content.example")
	if err != nil {
		t.Fatal(err)
	}
	c := &Content{svc: fakeContent{}, proxy: proxy, appOrigin: "https://app.example", signer: &TicketSigner{key: []byte(strings.Repeat("x", 32)), origin: "https://content.example", now: func() time.Time { return time.Unix(100, 0) }}}
	for _, tc := range []struct {
		name, path string
		status     int
		heading    string
	}{
		{"expired", "/content/3?sig=bogus", http.StatusGone, "This article link has expired"},
		{"gone", "/content/404?sig=" + c.signer.Sign(Ticket{UserID: 1, PrincipalID: 2, Kind: service.PrincipalHuman, EntryID: 404, Expires: 101, Mode: ImageProxied}), http.StatusNotFound, "This article is not available"},
		{"hidden", "/content/403?sig=" + c.signer.Sign(Ticket{UserID: 1, PrincipalID: 2, Kind: service.PrincipalHuman, EntryID: 403, Expires: 101, Mode: ImageProxied}), http.StatusNotFound, "This article is not available"},
		{"broken", "/content/500?sig=" + c.signer.Sign(Ticket{UserID: 1, PrincipalID: 2, Kind: service.PrincipalHuman, EntryID: 500, Expires: 101, Mode: ImageProxied}), http.StatusInternalServerError, "This article could not be shown"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			c.ServeHTTP(w, httptest.NewRequest(http.MethodGet, tc.path, nil))
			body := w.Body.String()
			if w.Code != tc.status || !strings.Contains(body, tc.heading) {
				t.Fatalf("status = %d, body = %q", w.Code, body)
			}
			start, end := strings.Index(body, "<script>"), strings.Index(body, "</script>")
			if start < 0 || end < start {
				t.Fatalf("no frame script in %q", body)
			}
			sum := sha256.Sum256([]byte(body[start+len("<script>") : end]))
			if want := "script-src 'sha256-" + base64.StdEncoding.EncodeToString(sum[:]) + "'"; !strings.Contains(w.Header().Get("Content-Security-Policy"), want) {
				t.Fatalf("CSP = %q, want %q", w.Header().Get("Content-Security-Policy"), want)
			}
		})
	}
}

func TestFrameScriptBindsMessagesToTheHostNonce(t *testing.T) {
	js := frameScript("https://app.example", "abc_123")
	for _, want := range []string{
		`const n="abc_123"`,
		// Every message echoes it, and keys typed into the article's own
		// form controls never leave the frame.
		`input,textarea,select,[contenteditable]`,
	} {
		if !strings.Contains(js, want) {
			t.Fatalf("frameScript() = %q, want it to contain %q", js, want)
		}
	}
	if got := strings.Count(js, "nonce:n"); got != 2 {
		t.Fatalf("frameScript() echoes the nonce %d times, want 2: %q", got, js)
	}
}

func TestFrameNonceAcceptsOnlyBoundedTokens(t *testing.T) {
	for _, tc := range []struct{ query, want string }{
		{"frame_nonce=abc_123-XYZ", "abc_123-XYZ"},
		{"", ""},
		{"frame_nonce=", ""},
		{"frame_nonce=" + strings.Repeat("a", 65), ""},
		{"frame_nonce=with%20space", ""},
		{"frame_nonce=%3Cscript%3E", ""},
	} {
		r := httptest.NewRequest(http.MethodGet, "/content/1?"+tc.query, nil)
		if got := frameNonce(r); got != tc.want {
			t.Fatalf("frameNonce(%q) = %q, want %q", tc.query, got, tc.want)
		}
	}
}
