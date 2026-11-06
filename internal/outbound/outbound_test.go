package outbound

import (
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"net/url"
	"strconv"
	"sync"
	"testing"
	"time"
)

type fakeResolver struct {
	mu        sync.Mutex
	addresses [][]netip.Addr
	calls     int
}

func (r *fakeResolver) LookupNetIP(_ context.Context, _, _ string) ([]netip.Addr, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	i := r.calls
	r.calls++
	if i >= len(r.addresses) {
		i = len(r.addresses) - 1
	}
	return r.addresses[i], nil
}
func client(t *testing.T, cfg Config) *Client {
	t.Helper()
	if cfg.Spacing == 0 {
		cfg.Spacing = time.Nanosecond
	}
	c, e := New(cfg)
	if e != nil {
		t.Fatal(e)
	}
	return c
}
func localPolicy(t *testing.T, raw string) Policy {
	t.Helper()
	u, e := url.Parse(raw)
	if e != nil {
		t.Fatal(e)
	}
	p, e := strconv.Atoi(u.Port())
	if e != nil {
		t.Fatal(e)
	}
	return Policy{Purpose: Feed, ExtraPorts: []uint16{uint16(p)}}
}
func TestDenyRanges(t *testing.T) {
	c := client(t, Config{})
	cases := []string{"0.1.2.3", "10.1.2.3", "100.64.0.1", "127.0.0.1", "169.254.1.1", "172.16.0.1", "192.168.1.1", "198.18.0.1", "240.0.0.1", "::", "::1", "::2", "fc00::1", "fe80::1", "::ffff:8.8.8.8", "64:ff9b::808:808", "fe80::1%eth0"}
	for _, s := range cases {
		t.Run(s, func(t *testing.T) {
			if c.allowed(netip.MustParseAddr(s), "test:80") {
				t.Fatalf("allowed %s", s)
			}
		})
	}
	if !c.allowed(netip.MustParseAddr("8.8.8.8"), "test:80") {
		t.Fatal("public denied")
	}
}
func TestLocalAndLimits(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("If-None-Match") == "tag" {
			w.WriteHeader(304)
			return
		}
		fmt.Fprint(w, "123456")
	}))
	defer s.Close()
	p := localPolicy(t, s.URL)
	c := client(t, Config{})
	_, e := c.Get(context.Background(), s.URL, p)
	if ErrorClass(e) != DeniedAddress {
		t.Fatalf("denied: %v", e)
	}
	c = client(t, Config{AllowPrefixes: []netip.Prefix{netip.MustParsePrefix("127.0.0.0/8")}})
	p.MaxBodySize = 5
	_, e = c.Get(context.Background(), s.URL, p)
	if ErrorClass(e) != TooLarge {
		t.Fatalf("size: %v", e)
	}
	p.ETag = "tag"
	r, e := c.Get(context.Background(), s.URL, p)
	if e != nil || r.Response.StatusCode != 304 {
		t.Fatalf("304: %v %v", r, e)
	}
}
func TestRedirectCredentials(t *testing.T) {
	var got http.Header
	dst := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { got = r.Header.Clone(); fmt.Fprint(w, "ok") }))
	defer dst.Close()
	src := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, dst.URL, 301) }))
	defer src.Close()
	p := localPolicy(t, src.URL)
	u, _ := url.Parse(dst.URL)
	port, _ := strconv.Atoi(u.Port())
	p.ExtraPorts = append(p.ExtraPorts, uint16(port))
	p.Credentials = Credentials{Username: "a", Password: "b", Headers: http.Header{"X-Secret": {"secret"}}}
	c := client(t, Config{AllowPrefixes: []netip.Prefix{netip.MustParsePrefix("127.0.0.0/8")}})
	result, e := c.Get(context.Background(), src.URL, p)
	if e != nil {
		t.Fatal(e)
	}
	if !result.PermanentRedirect || got.Get("Authorization") != "" || got.Get("X-Secret") != "" {
		t.Fatalf("redirect: %+v %v", result, got)
	}
}
func TestRedirectDenied(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, "http://10.0.0.1/", 302) }))
	defer s.Close()
	u, _ := url.Parse(s.URL)
	c := client(t, Config{AllowHosts: []string{u.Host}})
	_, e := c.Get(context.Background(), s.URL, localPolicy(t, s.URL))
	if ErrorClass(e) != DeniedAddress {
		t.Fatalf("got %v", e)
	}
}
func TestGzipAndStatus(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/429" {
			w.Header().Set("Retry-After", "42")
			w.WriteHeader(429)
			return
		}
		w.Header().Set("Content-Encoding", "gzip")
		z := gzip.NewWriter(w)
		_, _ = z.Write(bytes.Repeat([]byte("a"), 50000))
		_ = z.Close()
	}))
	defer s.Close()
	c := client(t, Config{AllowPrefixes: []netip.Prefix{netip.MustParsePrefix("127.0.0.0/8")}})
	p := localPolicy(t, s.URL)
	_, e := c.Get(context.Background(), s.URL, p)
	if ErrorClass(e) != TooLarge {
		t.Fatalf("gzip: %v", e)
	}
	_, e = c.Get(context.Background(), s.URL+"/429", p)
	var ce *Error
	if !errors.As(e, &ce) || ce.Class != HTTP429 || ce.RetryAfter != 42*time.Second {
		t.Fatalf("429: %v", e)
	}
}
func TestRebinding(t *testing.T) {
	r := &fakeResolver{addresses: [][]netip.Addr{{netip.MustParseAddr("8.8.8.8")}, {netip.MustParseAddr("127.0.0.1")}}}
	c := client(t, Config{Resolver: r})
	p := Policy{Purpose: Feed, Timeout: 50 * time.Millisecond}
	_, _ = c.Get(context.Background(), "http://example.test/", p)
	_, e := c.Get(context.Background(), "http://example.test/", p)
	if ErrorClass(e) != DeniedAddress {
		t.Fatalf("rebinding: %v", e)
	}
}
func TestURLValidation(t *testing.T) {
	for _, s := range []string{"http://[::]/", "http://[fe80::1%25eth0]/", "http://2130706433/", "http://0177.0.0.1/"} {
		t.Run(s, func(t *testing.T) {
			c := client(t, Config{})
			_, e := c.Get(context.Background(), s, Policy{Purpose: Feed, Timeout: time.Second})
			if ErrorClass(e) != DeniedAddress {
				t.Fatalf("expected denied address: %v", e)
			}
		})
	}
}
func TestSpacing(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = io.WriteString(w, "ok") }))
	defer s.Close()
	c := client(t, Config{Spacing: 40 * time.Millisecond, AllowPrefixes: []netip.Prefix{netip.MustParsePrefix("127.0.0.0/8")}})
	p := localPolicy(t, s.URL)
	start := time.Now()
	for i := 0; i < 2; i++ {
		if _, e := c.Get(context.Background(), s.URL, p); e != nil {
			t.Fatal(e)
		}
	}
	if time.Since(start) < 35*time.Millisecond {
		t.Fatal("spacing not enforced")
	}
}
func TestParsing(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		s    string
		want time.Duration
		ok   bool
	}{{"12", 12 * time.Second, true}, {now.Add(time.Minute).Format(http.TimeFormat), time.Minute, true}, {"-1", 0, false}, {"junk", 0, false}} {
		got, ok := ParseRetryAfter(tc.s, now)
		if got != tc.want || ok != tc.ok {
			t.Fatalf("retry %q: %v %v", tc.s, got, ok)
		}
	}
	for _, tc := range []struct {
		s    string
		want time.Duration
		ok   bool
	}{{"public, max-age=60", time.Minute, true}, {"MAX-AGE=\"4\"", 4 * time.Second, true}, {"max-age=-1", 0, false}} {
		got, ok := ParseMaxAge(tc.s)
		if got != tc.want || ok != tc.ok {
			t.Fatalf("max-age %q: %v %v", tc.s, got, ok)
		}
	}
}

func TestMappedResolverAndRedirectPolicy(t *testing.T) {
	r := &fakeResolver{addresses: [][]netip.Addr{{netip.MustParseAddr("::ffff:127.0.0.1")}}}
	c := client(t, Config{Resolver: r})
	_, err := c.Get(context.Background(), "http://example.test/", Policy{Purpose: Feed})
	if ErrorClass(err) != DeniedAddress {
		t.Fatalf("mapped resolver address: %v", err)
	}
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "http://10.0.0.1/", http.StatusFound)
	}))
	defer s.Close()
	u, _ := url.Parse(s.URL)
	c = client(t, Config{AllowHosts: []string{u.Host}})
	p := localPolicy(t, s.URL)
	p.Purpose = Webhook
	p.Redirect = RedirectNone
	result, err := c.Get(context.Background(), s.URL, p)
	if err != nil || result.Response.StatusCode != http.StatusFound {
		t.Fatalf("redirect disabled: %v %v", result, err)
	}
}

func TestLimiterCancellation(t *testing.T) {
	c := client(t, Config{Spacing: time.Second})
	release, err := c.acquire(context.Background(), "example.test")
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	_, err = c.acquire(ctx, "example.test")
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("waiting: %v", err)
	}
}

func TestHTTP2CheckedDialer(t *testing.T) {
	s := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.ProtoMajor != 2 {
			t.Errorf("protocol = %s", r.Proto)
		}
		fmt.Fprint(w, "ok")
	}))
	s.EnableHTTP2 = true
	s.StartTLS()
	defer s.Close()
	p := localPolicy(t, s.URL)
	p.Protocol = HTTP2
	c := client(t, Config{})
	if _, err := c.Get(context.Background(), s.URL, p); ErrorClass(err) != DeniedAddress {
		t.Fatalf("private destination: %v", err)
	}
	c = client(t, Config{AllowPrefixes: []netip.Prefix{netip.MustParsePrefix("127.0.0.0/8")}})
	u, _ := url.Parse(s.URL)
	tr := c.transport(u, HTTP2)
	tr.TLSClientConfig = s.Client().Transport.(*http.Transport).TLSClientConfig.Clone()
	defer tr.CloseIdleConnections()
	resp, err := tr.RoundTrip(httptest.NewRequest("GET", s.URL, nil))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.ProtoMajor != 2 {
		t.Fatalf("protocol = %s", resp.Proto)
	}
}

func TestTransportReuse(t *testing.T) {
	var mu sync.Mutex
	opened := 0
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("ok"))
	}))
	srv.Config.ConnState = func(_ net.Conn, state http.ConnState) {
		if state == http.StateNew {
			mu.Lock()
			opened++
			mu.Unlock()
		}
	}
	srv.Start()
	defer srv.Close()
	c := client(t, Config{AllowPrefixes: []netip.Prefix{netip.MustParsePrefix("127.0.0.0/8")}})
	_, port, err := net.SplitHostPort(srv.Listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	portNumber, err := strconv.Atoi(port)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		result, err := c.Get(context.Background(), srv.URL, Policy{Purpose: Feed, ExtraPorts: []uint16{uint16(portNumber)}})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := io.Copy(io.Discard, result.Response.Body); err != nil {
			t.Fatal(err)
		}
		result.Response.Body.Close()
	}
	mu.Lock()
	defer mu.Unlock()
	if opened != 1 {
		t.Fatalf("opened %d connections, want 1 (keep-alive)", opened)
	}
}
