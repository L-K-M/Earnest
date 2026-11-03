package feed

import "testing"

func TestCleanURL(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		raw  string
		want string
	}{
		{"canonical stays", "https://example.com/a", "https://example.com/a"},
		{"upper scheme", "HTTPS://example.com/A", "https://example.com/A"},
		{"http default port", "http://EXAMPLE.com:80/a", "http://example.com/a"},
		{"https default port", "https://example.com:443/a", "https://example.com/a"},
		{"nondefault port kept", "http://example.com:8080/a", "http://example.com:8080/a"},
		{"fragment dropped", "https://example.com/a#frag", "https://example.com/a"},
		{"userinfo dropped", "https://user:pass@example.com/a", "https://example.com/a"},
		{"path case kept", "https://example.com/CamelCase", "https://example.com/CamelCase"},
		{"trailing slash kept", "https://example.com/a/", "https://example.com/a/"},
		{"dot segments", "https://example.com/a/./b/../c", "https://example.com/a/c"},
		{"leading dot dot", "https://example.com/../a", "https://example.com/a"},
		{"trailing dot", "https://example.com/a/.", "https://example.com/a/"},
		{"trailing dot dot", "https://example.com/a/b/..", "https://example.com/a/"},
		{"double slash kept", "https://example.com/a//b", "https://example.com/a//b"},
		{"query params sorted", "https://example.com/?b=2&a=1", "https://example.com/?a=1&b=2"},
		{"utm stripped", "https://example.com/?utm_source=x&keep=1", "https://example.com/?keep=1"},
		{"all utm stripped", "https://example.com/?utm_source=a&utm_medium=b&utm_campaign=c&utm_term=d&utm_content=e", "https://example.com/"},
		{"click ids stripped", "https://example.com/p?fbclid=1&gclid=2&dclid=3&gclsrc=4&mc_cid=5&mc_eid=6&igshid=7&si=8&yclid=9", "https://example.com/p"},
		{"prefixed families stripped", "https://example.com/?pk_campaign=a&matomo_campaign=b&sc_channel=c&hmb_medium=d&elqtrack=e&trk=f", "https://example.com/"},
		{"percent encoding normalised", "https://example.com/%7euser", "https://example.com/~user"},
		{"mail scheme rejected", "mailto:someone@example.com", ""},
		{"ftp rejected", "ftp://example.com/file", ""},
		{"relative rejected", "/posts/1", ""},
		{"protocol relative rejected", "//example.com/x", ""},
		{"garbage rejected", "not a url", ""},
		{"empty rejected", "", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := CleanURL(tt.raw); got != tt.want {
				t.Fatalf("CleanURL(%q) = %q, want %q", tt.raw, got, tt.want)
			}
		})
	}
}

func TestIsTrackingParam(t *testing.T) {
	t.Parallel()

	tracking := []string{
		"utm_source", "UTM_MEDIUM", "utm_campaign_id", "fbclid", "gclid",
		"dclid", "gclsrc", "mc_cid", "mc_eid", "igshid", "_openstat",
		"_hsenc", "_hsmi", "hsCtaTracking", "vero_id", "wt_zmc",
		"pk_campaign", "pk_kwd", "matomo_campaign", "piwik_kwd",
		"sc_channel", "hmb_medium", "elqTrackId", "trkContact", "si",
		"cmpid", "spm", "ref_src", "ref_url", "gs_l",
	}
	for _, name := range tracking {
		if !IsTrackingParam(name) {
			t.Errorf("IsTrackingParam(%q) = false, want true", name)
		}
	}

	kept := []string{"page", "id", "q", "redirect", "next", "ref", "source", "medium", "cid", "t"}
	for _, name := range kept {
		if IsTrackingParam(name) {
			t.Errorf("IsTrackingParam(%q) = true, want false", name)
		}
	}
}

func TestResolveURL(t *testing.T) {
	t.Parallel()

	const base = "https://example.com/dir/page.html"
	tests := []struct {
		ref  string
		want string
	}{
		{"other.html", "https://example.com/dir/other.html"},
		{"/root.html", "https://example.com/root.html"},
		{"../up.html", "https://example.com/up.html"},
		{"./same.html", "https://example.com/dir/same.html"},
		{"?q=1", "https://example.com/dir/page.html?q=1"},
		{"//cdn.example.com/a.js", "https://cdn.example.com/a.js"},
		{"https://other.example/x", "https://other.example/x"},
		{"", base},
		{"#frag", base},
		{"/a/./b/../c", "https://example.com/a/c"},
	}

	for _, tt := range tests {
		if got := ResolveURL(base, tt.ref); got != tt.want {
			t.Errorf("ResolveURL(%q, %q) = %q, want %q", base, tt.ref, got, tt.want)
		}
	}

	if got := ResolveURL("/relative/base", "x"); got != "x" {
		t.Errorf("ResolveURL with relative base = %q, want %q", got, "x")
	}
}
