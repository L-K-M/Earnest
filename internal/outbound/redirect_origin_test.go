package outbound

import (
	"net/http"
	"net/http/httptest"
	"net/netip"
	"net/url"
	"testing"
)

func TestHTTPSRedirectsKeepCredentialsOnTheirOrigin(t *testing.T) {
	for _, tc := range []struct {
		name, from, to string
		credentials    bool
	}{
		{name: "ASCII case", from: "wiki.test", to: "WIKI.Test", credentials: true},
		{name: "same Unicode name", from: "bücher.test", to: "bücher.TEST", credentials: true},
		{name: "another host", from: "wiki.test", to: "other.test"},
		// Unicode lowercasing equates each pair, but IDNA dials two hosts.
		{name: "dotted capital I", from: "wiki.test", to: "wİki.test"},
		{name: "dotted capital I, escaped", from: "wiki.test", to: "w%C4%B0ki.test"},
		{name: "original is the look-alike", from: "wİki.test", to: "wiki.test"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var location string
			var next []http.Header
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/start" {
					w.Header().Set("Location", location)
					w.WriteHeader(http.StatusFound)
					return
				}
				next = append(next, r.Header.Clone())
			}))
			defer server.Close()
			u, err := url.Parse(server.URL)
			if err != nil {
				t.Fatal(err)
			}
			location = "https://" + tc.to + ":" + u.Port() + "/next"
			c := client(t, Config{
				Resolver:      &fakeResolver{addresses: [][]netip.Addr{{netip.MustParseAddr("127.0.0.1")}}},
				AllowPrefixes: []netip.Prefix{netip.MustParsePrefix("127.0.0.0/8")},
			})
			tlsConfig := server.Client().Transport.(*http.Transport).TLSClientConfig.Clone()
			tlsConfig.InsecureSkipVerify = true // the test certificate names none of these hosts
			c.transports[HTTP1].TLSClientConfig = tlsConfig
			req, err := http.NewRequest(http.MethodGet, "https://"+tc.from+":"+u.Port()+"/start", nil)
			if err != nil {
				t.Fatal(err)
			}
			p := localPolicy(t, server.URL)
			p.Credentials = Credentials{Username: "reader", Password: "private", Headers: http.Header{"X-Secret": {"private"}}}

			if _, err := c.Do(req, p); err != nil || len(next) != 1 {
				t.Fatalf("redirect: hits %d, %v", len(next), err)
			}
			for _, name := range []string{"Authorization", "X-Secret"} {
				if sent := next[0].Get(name) != ""; sent != tc.credentials {
					t.Errorf("%s sent = %t, want %t", name, sent, tc.credentials)
				}
			}
		})
	}
}
