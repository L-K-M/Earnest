package web

import (
	"context"
	"crypto/sha256"
	"encoding/base64"

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

func (fakeContent) Content(_ context.Context, _ service.Actor, _ int64) (service.EntryContent, error) {
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
	c := &Content{svc: fakeContent{}, proxy: proxy, signer: &TicketSigner{key: []byte(strings.Repeat("x", 32)), origin: "https://content.example", now: func() time.Time { return time.Unix(100, 0) }}}
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
	c := &Content{svc: fakeContent{}, proxy: proxy, signer: &TicketSigner{key: []byte(strings.Repeat("x", 32)), origin: "https://content.example", now: func() time.Time { return time.Unix(100, 0) }}}
	sig := c.signer.Sign(Ticket{UserID: 1, PrincipalID: 2, Kind: service.PrincipalHuman, EntryID: 3, Expires: 101, Mode: ImageProxied})
	w := httptest.NewRecorder()
	c.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/content/3?sig="+sig, nil))
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d", w.Code)
	}
	policy := w.Header().Get("Content-Security-Policy")
	if !strings.Contains(policy, "default-src 'none'") || !strings.Contains(policy, "img-src https://content.example") || !strings.Contains(policy, "style-src 'sha256-") || strings.Contains(policy, "script-src") {
		t.Fatalf("CSP = %q", policy)
	}
}

func TestReaderIframeSandbox(t *testing.T) {
	source, err := os.ReadFile("../../web/src/App.tsx")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(source), `sandbox="allow-popups allow-popups-to-escape-sandbox"`) {
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
