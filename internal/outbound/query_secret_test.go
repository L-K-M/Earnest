package outbound

import (
	"net/http"
	"net/http/httptest"
	"net/netip"
	"net/url"
	"sync/atomic"
	"testing"
)

func TestCleartextQuerySecrets(t *testing.T) {
	const unlisted, listed = "feeds.test", "trusted.test"
	for _, tc := range []struct {
		name    string
		host    string
		query   string
		purpose Purpose
		refused bool
	}{
		{name: "token", query: "token=s", refused: true},
		{name: "access token", query: "access_token=s", refused: true},
		{name: "feed token", query: "feed_token=s", refused: true},
		{name: "API key in any case", query: "API-Key=s", refused: true},
		{name: "key", query: "user=ann&key=s", refused: true},
		{name: "passkey", query: "passkey=s", refused: true},
		{name: "underscored passkey", query: "torrent_pass=s", refused: true},
		{name: "password", query: "password=s", refused: true},
		{name: "escaped name", query: "api%5Fkey=s", refused: true},
		{name: "semicolon separator", query: "page=2;token=s", refused: true},
		{name: "beside a malformed pair", query: "x=%zz&token=s", refused: true},
		{name: "after an empty value", query: "token=&token=s", refused: true},
		{name: "discovery", query: "token=s", purpose: Discovery, refused: true},

		{name: "feed format", query: "feed=rss2&paged=2"},
		{name: "similar names", query: "keyword=go&author=ann&tokenized=1"},
		{name: "empty values", query: "token=&key"},
		{name: "publisher's image URL", query: "token=s", purpose: Image},
		{name: "publisher's article URL", query: "token=s", purpose: Extract},
		{name: "publisher's icon URL", query: "token=s", purpose: Favicon},
		{name: "allowlisted host", host: listed, query: "token=s"},
		{name: "loopback", host: "127.0.0.1", query: "token=s"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var hits atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { hits.Add(1) }))
			defer server.Close()
			u, err := url.Parse(server.URL)
			if err != nil {
				t.Fatal(err)
			}
			c := client(t, Config{
				Resolver:      &fakeResolver{addresses: [][]netip.Addr{{netip.MustParseAddr("127.0.0.1")}}},
				AllowHosts:    []string{listed + ":" + u.Port()},
				AllowPrefixes: []netip.Prefix{netip.MustParsePrefix("127.0.0.0/8")},
			})
			p := localPolicy(t, server.URL)
			if tc.purpose != "" {
				p.Purpose = tc.purpose
			}
			host := tc.host
			if host == "" {
				host = unlisted
			}

			_, err = c.Get(t.Context(), "http://"+host+":"+u.Port()+"/feed?"+tc.query, p)
			if tc.refused {
				if ErrorClass(err) != DeniedAddress || hits.Load() != 0 {
					t.Fatalf("secret travelled: hits %d, %v", hits.Load(), err)
				}
				return
			}
			if err != nil || hits.Load() != 1 {
				t.Fatalf("request refused: hits %d, %v", hits.Load(), err)
			}
		})
	}
}

func TestRedirectKeepsQuerySecretsOffHTTP(t *testing.T) {
	var hits atomic.Int32
	plain := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { hits.Add(1) }))
	defer plain.Close()
	plainURL, err := url.Parse(plain.URL)
	if err != nil {
		t.Fatal(err)
	}
	secure := httptest.NewTLSServer(http.RedirectHandler("http://feeds.test:"+plainURL.Port()+"/feed?token=s", http.StatusMovedPermanently))
	defer secure.Close()
	secureURL, err := url.Parse(secure.URL)
	if err != nil {
		t.Fatal(err)
	}
	c := client(t, Config{
		Resolver:      &fakeResolver{addresses: [][]netip.Addr{{netip.MustParseAddr("127.0.0.1")}}},
		AllowPrefixes: []netip.Prefix{netip.MustParsePrefix("127.0.0.0/8")},
	})
	tlsConfig := secure.Client().Transport.(*http.Transport).TLSClientConfig.Clone()
	tlsConfig.InsecureSkipVerify = true // the test certificate does not name feeds.test
	c.transports[HTTP1].TLSClientConfig = tlsConfig
	p := localPolicy(t, secure.URL)
	p.ExtraPorts = append(p.ExtraPorts, localPolicy(t, plain.URL).ExtraPorts...)

	if _, err := c.Get(t.Context(), "https://feeds.test:"+secureURL.Port()+"/feed?token=s", p); ErrorClass(err) != DeniedAddress || hits.Load() != 0 {
		t.Fatalf("redirect carried the secret over HTTP: hits %d, %v", hits.Load(), err)
	}
}

func TestCleartextWebhookSignature(t *testing.T) {
	var hits atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { hits.Add(1) }))
	defer server.Close()
	u, err := url.Parse(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	c := client(t, Config{
		Resolver:      &fakeResolver{addresses: [][]netip.Addr{{netip.MustParseAddr("127.0.0.1")}}},
		AllowPrefixes: []netip.Prefix{netip.MustParsePrefix("127.0.0.0/8")},
	})
	req, err := http.NewRequest(http.MethodPost, "http://hooks.test:"+u.Port()+"/hook", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("webhook-signature", "v1,signature")
	p := localPolicy(t, server.URL)
	p.Purpose, p.Redirect = Webhook, RedirectNone

	if _, err := c.Do(req, p); ErrorClass(err) != DeniedAddress || hits.Load() != 0 {
		t.Fatalf("signature travelled over HTTP: hits %d, %v", hits.Load(), err)
	}
}
