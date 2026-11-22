package outbound

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"net/url"
	"sync/atomic"
	"testing"
	"time"
)

type offlineResolver struct{}

func (offlineResolver) LookupNetIP(context.Context, string, string) ([]netip.Addr, error) {
	return nil, errors.New("offline")
}

// The offline resolver separates the two outcomes without any traffic: a DNS
// failure means the credential gate let the request through to the dialer.
func TestCleartextCredentialsNeedLoopbackOrExactHost(t *testing.T) {
	c := client(t, Config{
		Resolver:      offlineResolver{},
		AllowHosts:    []string{"hister:8080", "Keep.Example:80", "192.168.1.10:8080", "[fd00::1]:8080", "wiki:8080"},
		AllowPrefixes: []netip.Prefix{netip.MustParsePrefix("10.0.0.0/8")},
	})
	for _, tc := range []struct {
		url  string
		want Class
	}{
		{"http://127.0.0.1:8080/", DNS},
		{"http://127.9.8.7:8080/", DNS},
		{"http://[::1]:8080/", DNS},
		{"http://localhost:8080/", DNS},
		{"http://LOCALHOST:8080/", DNS},
		{"http://hister:8080/", DNS},
		{"http://HISTER:8080/", DNS},
		{"http://keep.example/", DNS},
		{"http://keep.example:80/", DNS},
		{"http://192.168.1.10:8080/", DNS},
		{"http://[fd00::1]:8080/", DNS},
		{"https://anywhere.example/", DNS},

		{"http://models.example/", DeniedAddress},
		{"http://hister/", DeniedAddress},
		{"http://hister:8081/", DeniedAddress},
		{"http://other:8080/", DeniedAddress},
		{"http://sub.hister:8080/", DeniedAddress},
		{"http://hister.:8080/", DeniedAddress},
		{"http://keep.example:0080/", DeniedAddress},
		{"http://192.168.1.11:8080/", DeniedAddress},
		{"http://10.0.0.5:8080/", DeniedAddress}, // a prefix is not an exact host
		{"http://localhost.:8080/", DeniedAddress},
		{"http://localhost.example:8080/", DeniedAddress},
		{"http://127.0.0.1.example:8080/", DeniedAddress},
		// Case folding and IDNA disagree on these, so they never match by name.
		{"http://localhoſt:8080/", DeniedAddress},
		{"http://Keep.example/", DeniedAddress},
		{"http://wİki:8080/", DeniedAddress},
	} {
		for name, attach := range map[string]func(*http.Request, *Policy){
			"password":      func(_ *http.Request, p *Policy) { p.Credentials.Password = "private" },
			"username":      func(_ *http.Request, p *Policy) { p.Credentials.Username = "private" },
			"custom header": func(_ *http.Request, p *Policy) { p.Credentials.Headers = http.Header{"X-Secret": {"private"}} },
			"authorization": func(r *http.Request, _ *Policy) { r.Header.Set("Authorization", "Bearer private") },
			"cookie":        func(r *http.Request, _ *Policy) { r.Header.Set("Cookie", "session=private") },
			"api key":       func(r *http.Request, _ *Policy) { r.Header.Set("x-api-key", "private") },
			"google key":    func(r *http.Request, _ *Policy) { r.Header.Set("x-goog-api-key", "private") },
		} {
			t.Run(tc.url+" "+name, func(t *testing.T) {
				req, err := http.NewRequest(http.MethodGet, tc.url, nil)
				if err != nil {
					t.Fatal(err)
				}
				p := Policy{Purpose: Feed, ExtraPorts: []uint16{8080, 8081}}
				attach(req, &p)
				if _, err := c.Do(req, p); ErrorClass(err) != tc.want {
					t.Fatalf("got %v, want %s", err, tc.want)
				}
			})
		}
	}
}

func TestLocalhostMustResolveToLoopback(t *testing.T) {
	// 192.0.2.0/24 is reserved for documentation, so a regression only times out.
	c := client(t, Config{
		Resolver:   &fakeResolver{addresses: [][]netip.Addr{{netip.MustParseAddr("192.0.2.1")}}},
		AllowHosts: []string{"localhost:8080"},
	})
	req, err := http.NewRequest(http.MethodGet, "http://localhost:8080/", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer private")
	p := Policy{Purpose: Feed, ExtraPorts: []uint16{8080}, Timeout: 200 * time.Millisecond}
	if _, err := c.Do(req, p); ErrorClass(err) != DeniedAddress {
		t.Fatalf("localhost left the machine: %v", err)
	}
}

func TestCleartextRedirectsKeepCredentialsOnTheirOrigin(t *testing.T) {
	for _, tc := range []struct {
		name, host  string
		credentials bool // whether the redirect target may receive them
	}{
		{name: "same origin", host: "wiki.test", credentials: true},
		{name: "another allowlisted origin", host: "other.test"},
		{name: "loopback", host: "127.0.0.1"},
		// Unicode case folding equates this name with wiki.test; IDNA dials another host.
		{name: "look-alike origin", host: "wİki.test"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var hits atomic.Int32
			var got http.Header
			var location string
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/start" {
					w.Header().Set("Location", location)
					w.WriteHeader(http.StatusFound)
					return
				}
				got = r.Header.Clone()
				hits.Add(1)
			}))
			defer server.Close()
			u, err := url.Parse(server.URL)
			if err != nil {
				t.Fatal(err)
			}
			port := ":" + u.Port()
			location = "http://" + tc.host + port + "/next"
			c := client(t, Config{
				Resolver:      &fakeResolver{addresses: [][]netip.Addr{{netip.MustParseAddr("127.0.0.1")}}},
				AllowHosts:    []string{"wiki.test" + port, "other.test" + port},
				AllowPrefixes: []netip.Prefix{netip.MustParsePrefix("127.0.0.0/8")},
			})
			req, err := http.NewRequest(http.MethodGet, "http://wiki.test"+port+"/start", nil)
			if err != nil {
				t.Fatal(err)
			}
			req.Header.Set("X-Api-Key", "private")
			req.Header.Set("X-Goog-Api-Key", "private")
			p := localPolicy(t, server.URL)
			p.Credentials = Credentials{Username: "reader", Password: "private", Headers: http.Header{"X-Secret": {"private"}}}

			_, err = c.Do(req, p)
			if err != nil || hits.Load() != 1 {
				t.Fatalf("redirect: hits %d, %v", hits.Load(), err)
			}
			for _, name := range []string{"Authorization", "X-Secret", "X-Api-Key", "X-Goog-Api-Key"} {
				if sent := got.Get(name) != ""; sent != tc.credentials {
					t.Errorf("%s sent = %t, want %t", name, sent, tc.credentials)
				}
			}
		})
	}
}
