package feed

import (
	"reflect"
	"strings"
	"testing"
)

func discoveredURLs(feeds []DiscoveredFeed) []string {
	urls := make([]string, 0, len(feeds))
	for _, f := range feeds {
		urls = append(urls, f.URL)
	}
	return urls
}

func TestDiscoverExplicitLinks(t *testing.T) {
	t.Parallel()

	page := `<!doctype html><html><head>
	<base href="https://example.com/blog/">
	<link rel="alternate" type="application/rss+xml" title="Posts RSS" href="/feed.xml">
	<link rel="alternate" type="application/atom+xml" href="atom.xml">
	<link rel="stylesheet" type="text/css" href="style.css">
	<link rel="ALTERNATE home" type="application/feed+json" href="feed.json">
	<link rel="alternate" type="application/rss+xml" href="javascript:alert(1)">
	</head><body></body></html>`

	got, err := Discover([]byte(page), "https://example.com/blog/index.html")
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	want := []string{
		"https://example.com/feed.xml",
		"https://example.com/blog/atom.xml",
		"https://example.com/blog/feed.json",
		"https://example.com/feed",
		"https://example.com/rss",
		"https://example.com/atom.xml",
		"https://example.com/index.xml",
		"https://example.com/feed.json",
	}
	if !reflect.DeepEqual(discoveredURLs(got), want) {
		t.Fatalf("candidates = %v, want %v", discoveredURLs(got), want)
	}
	if got[0].Title != "Posts RSS" || got[0].Type != "application/rss+xml" {
		t.Fatalf("first candidate metadata = %+v", got[0])
	}
}

func TestDiscoverFallbackPathsOnly(t *testing.T) {
	t.Parallel()

	got, err := Discover([]byte("<html><body>No feeds here.</body></html>"), "https://example.com/")
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	want := []string{
		"https://example.com/feed",
		"https://example.com/rss",
		"https://example.com/atom.xml",
		"https://example.com/index.xml",
		"https://example.com/feed.json",
	}
	if !reflect.DeepEqual(discoveredURLs(got), want) {
		t.Fatalf("candidates = %v, want %v", discoveredURLs(got), want)
	}
}

func TestDiscoverDeduplicatesFallbacks(t *testing.T) {
	t.Parallel()

	page := `<html><head><link rel="alternate" type="application/rss+xml" href="/feed"></head></html>`
	got, err := Discover([]byte(page), "https://example.com/")
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	want := []string{
		"https://example.com/feed",
		"https://example.com/rss",
		"https://example.com/atom.xml",
		"https://example.com/index.xml",
		"https://example.com/feed.json",
	}
	if !reflect.DeepEqual(discoveredURLs(got), want) {
		t.Fatalf("candidates = %v, want %v", discoveredURLs(got), want)
	}
}

func TestDiscoverYouTube(t *testing.T) {
	t.Parallel()

	tests := []struct {
		pageURL string
		want    string
	}{
		{"https://www.youtube.com/channel/UCabc123DEF", "https://www.youtube.com/feeds/videos.xml?channel_id=UCabc123DEF"},
		{"https://youtube.com/channel/UCabc123DEF/about", "https://www.youtube.com/feeds/videos.xml?channel_id=UCabc123DEF"},
		{"https://m.youtube.com/user/legacyname", "https://www.youtube.com/feeds/videos.xml?user=legacyname"},
		{"https://www.youtube.com/c/customname", ""},
		{"https://www.youtube.com/@handle", ""},
		{"https://youtu.be/dQw4w9WgXcQ", ""},
	}
	for _, tt := range tests {
		got, err := Discover([]byte("<html></html>"), tt.pageURL)
		if err != nil {
			t.Fatalf("Discover(%q): %v", tt.pageURL, err)
		}
		if tt.want == "" {
			for _, f := range got {
				if strings.Contains(f.URL, "youtube.com/feeds/") {
					t.Errorf("Discover(%q) mapped to %q, want none", tt.pageURL, f.URL)
				}
			}
			continue
		}
		found := false
		for _, f := range got {
			if f.URL == tt.want {
				found = true
			}
		}
		if !found {
			t.Errorf("Discover(%q) candidates = %v, want %q", tt.pageURL, discoveredURLs(got), tt.want)
		}
	}
}

func TestDiscoverReddit(t *testing.T) {
	t.Parallel()

	tests := []struct {
		pageURL string
		want    string
	}{
		{"https://www.reddit.com/r/golang/", "https://www.reddit.com/r/golang/.rss"},
		{"https://old.reddit.com/r/golang/top/", "https://www.reddit.com/r/golang/.rss"},
		{"https://www.reddit.com/user/someone", "https://www.reddit.com/user/someone/.rss"},
		{"https://www.reddit.com/r/golang/.rss", ""},
	}
	for _, tt := range tests {
		got, err := Discover([]byte("<html></html>"), tt.pageURL)
		if err != nil {
			t.Fatalf("Discover(%q): %v", tt.pageURL, err)
		}
		if tt.want == "" {
			for _, f := range got {
				if strings.Contains(f.URL, "/.rss") {
					t.Errorf("Discover(%q) mapped to %q, want none", tt.pageURL, f.URL)
				}
			}
			continue
		}
		found := false
		for _, f := range got {
			if f.URL == tt.want {
				found = true
			}
		}
		if !found {
			t.Errorf("Discover(%q) candidates = %v, want %q", tt.pageURL, discoveredURLs(got), tt.want)
		}
	}
}

func TestDiscoverRejectsBadPageURL(t *testing.T) {
	t.Parallel()

	if _, err := Discover([]byte("<html></html>"), "not a url"); err == nil {
		t.Fatal("expected an error for a relative page URL")
	}
}

func TestDiscoverSkipsAlternateWithoutType(t *testing.T) {
	t.Parallel()

	page := `<html><head><link rel="alternate" href="/untyped"></head></html>`
	got, err := Discover([]byte(page), "https://example.com/")
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	for _, f := range got {
		if f.URL == "https://example.com/untyped" {
			t.Fatalf("alternate without a type was accepted: %+v", got)
		}
	}
	if len(got) != len(fallbackFeedPaths) {
		t.Fatalf("candidates = %v, want just the fallback paths", discoveredURLs(got))
	}
}
