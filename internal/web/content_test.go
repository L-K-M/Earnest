package web

import (
	"context"

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
	source, err := os.ReadFile("../../web/src/main.tsx")
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
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), "This article link has expired") {
		t.Fatalf("body = %q", w.Body.String())
	}
	if policy := w.Header().Get("Content-Security-Policy"); !strings.Contains(policy, "default-src 'none'") {
		t.Fatalf("CSP = %q", policy)
	}
}
