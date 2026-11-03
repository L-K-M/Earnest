package push

import (
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/L-K-M/Earnest/internal/outbound"
)

// testConfig returns a Config whose outbound client may reach a loopback
// httptest server on that server's own port.
func testConfig(t *testing.T, rawURL string) Config {
	t.Helper()
	u, err := url.Parse(rawURL)
	if err != nil {
		t.Fatal(err)
	}
	port, err := strconv.Atoi(u.Port())
	if err != nil {
		t.Fatalf("port of %q: %v", rawURL, err)
	}
	client, err := outbound.New(outbound.Config{
		Version:       "test",
		AllowPrefixes: []netip.Prefix{netip.MustParsePrefix("127.0.0.0/8")},
		Spacing:       time.Nanosecond,
	})
	if err != nil {
		t.Fatal(err)
	}
	return Config{Client: client, ExtraPorts: []uint16{uint16(port)}}
}

// testOutboundClient returns a standalone outbound client for transports
// that do not need a local server.
func testOutboundClient(t *testing.T) *outbound.Client {
	t.Helper()
	client, err := outbound.New(outbound.Config{Version: "test", Spacing: time.Nanosecond})
	if err != nil {
		t.Fatal(err)
	}
	return client
}

// capturedRequest records what a test server received.
type capturedRequest struct {
	Method string
	URL    string
	Header http.Header
	Body   []byte
}

// capture collects requests that reached a test server.
type capture struct {
	requests chan capturedRequest
	count    atomic.Int64
}

func newCapture(t *testing.T, status int, setHeader func(http.Header)) (*httptest.Server, *capture) {
	t.Helper()
	cap := &capture{requests: make(chan capturedRequest, 16)}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		cap.count.Add(1)
		cap.requests <- capturedRequest{Method: r.Method, URL: r.URL.String(), Header: r.Header.Clone(), Body: body}
		if setHeader != nil {
			setHeader(w.Header())
		}
		w.WriteHeader(status)
	}))
	t.Cleanup(srv.Close)
	return srv, cap
}

// one waits for the next request the transport made.
func (c *capture) one(t *testing.T) capturedRequest {
	t.Helper()
	select {
	case req := <-c.requests:
		return req
	case <-time.After(5 * time.Second):
		t.Fatal("server received no request")
		return capturedRequest{}
	}
}

// requireCount asserts how many requests arrived in total.
func (c *capture) requireCount(t *testing.T, want int64) {
	t.Helper()
	if got := c.count.Load(); got != want {
		t.Fatalf("server received %d requests, want %d", got, want)
	}
}

// mustB64 decodes base64url without padding, ignoring line-wrapping spaces.
func mustB64(t *testing.T, s string) []byte {
	t.Helper()
	clean := strings.Map(func(r rune) rune {
		switch r {
		case ' ', '\n', '\r', '\t':
			return -1
		}
		return r
	}, s)
	out, err := base64.RawURLEncoding.DecodeString(clean)
	if err != nil {
		t.Fatalf("decode %q: %v", s, err)
	}
	return out
}

// jwtHeader decodes the JOSE header of a compact JWT.
func jwtHeader(t *testing.T, token string) map[string]any {
	t.Helper()
	raw, err := base64.RawURLEncoding.DecodeString(strings.Split(token, ".")[0])
	if err != nil {
		t.Fatal(err)
	}
	var header map[string]any
	if err := json.Unmarshal(raw, &header); err != nil {
		t.Fatal(err)
	}
	return header
}
