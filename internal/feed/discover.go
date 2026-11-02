package feed

import (
	"bytes"
	"fmt"
	"net/url"
	"strings"

	xhtml "golang.org/x/net/html"
)

// DiscoveredFeed is one candidate feed URL. Title and Type come from the
// declaring link element when present.
type DiscoveredFeed struct {
	URL   string `json:"url"`
	Title string `json:"title,omitempty"`
	Type  string `json:"type,omitempty"`
}

// feedLinkTypes are the MIME types treated as feed declarations.
var feedLinkTypes = map[string]bool{
	"application/rss+xml":   true,
	"application/atom+xml":  true,
	"application/feed+json": true,
	"application/json":      true,
}

// fallbackFeedPaths are probed root-relative when a page declares nothing.
// They are always appended after declared links so callers can try them in
// order; duplication is removed.
var fallbackFeedPaths = []string{
	"/feed",
	"/rss",
	"/atom.xml",
	"/index.xml",
	"/feed.json",
}

// Discover returns candidate feed URLs for an HTML page. Candidates appear in
// preference order: a host-specific mapping when the URL identifies a
// YouTube channel or Reddit community or user, then explicit
// <link rel="alternate"> declarations in document order, then common fallback
// paths. Only absolute http(s) URLs are returned.
//
// The page itself is never fetched; pageURL supplies the base for relative
// links and a page-level <base href> overrides it, as in a browser.
func Discover(page []byte, pageURL string) ([]DiscoveredFeed, error) {
	if int64(len(page)) > DefaultLimits.MaxBytes {
		return nil, ErrTooLarge
	}
	u, err := url.Parse(strings.TrimSpace(pageURL))
	if err != nil || !u.IsAbs() || u.Host == "" {
		return nil, fmt.Errorf("feed: invalid page URL %q", pageURL)
	}

	doc, err := xhtml.Parse(bytes.NewReader(page))
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrMalformed, err)
	}

	var out []DiscoveredFeed
	seen := make(map[string]bool)
	add := func(f DiscoveredFeed) {
		f.URL = CleanURL(f.URL)
		if f.URL == "" || seen[f.URL] {
			return
		}
		seen[f.URL] = true
		out = append(out, f)
	}

	add(DiscoveredFeed{URL: youtubeFeedURL(u), Type: "application/atom+xml"})
	add(DiscoveredFeed{URL: redditFeedURL(u), Type: "application/rss+xml"})

	pageBase := pageURL
	if href := findBaseHref(doc); href != "" {
		if abs := ResolveURL(pageURL, href); abs != "" {
			pageBase = abs
		}
	}
	for _, link := range findLinkAlternates(doc) {
		href := cleanText(nodeAttr(link, "href"))
		if href == "" {
			continue
		}
		typ := strings.ToLower(strings.TrimSpace(nodeAttr(link, "type")))
		if i := strings.IndexByte(typ, ';'); i >= 0 {
			typ = strings.TrimSpace(typ[:i])
		}
		if !feedLinkTypes[typ] {
			continue
		}
		add(DiscoveredFeed{
			URL:   ResolveURL(pageBase, href),
			Title: cleanText(nodeAttr(link, "title")),
			Type:  typ,
		})
	}

	origin := u.Scheme + "://" + u.Host
	for _, path := range fallbackFeedPaths {
		add(DiscoveredFeed{URL: origin + path})
	}
	return out, nil
}

// findLinkAlternates collects <link> elements whose rel attribute contains the
// alternate token. Traversal is iterative to bound stack use on deep markup.
func findLinkAlternates(root *xhtml.Node) []*xhtml.Node {
	var out []*xhtml.Node
	stack := []*xhtml.Node{root}
	for len(stack) > 0 {
		n := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if n.Type == xhtml.ElementNode && n.Data == "link" {
			for _, token := range strings.Fields(strings.ToLower(nodeAttr(n, "rel"))) {
				if token == "alternate" {
					out = append(out, n)
					break
				}
			}
		}
		for c := n.LastChild; c != nil; c = c.PrevSibling {
			stack = append(stack, c)
		}
	}
	return out
}

// youtubeFeedURL maps /channel/<id> and /user/<name> pages to their video
// feeds. Handle-style URLs (/@name, /c/name) cannot be mapped without a
// network lookup and are deliberately left alone rather than guessed.
func youtubeFeedURL(u *url.URL) string {
	host := strings.ToLower(u.Hostname())
	if host != "youtube.com" && host != "www.youtube.com" && host != "m.youtube.com" {
		return ""
	}
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	if len(parts) < 2 || parts[1] == "" {
		return ""
	}
	switch parts[0] {
	case "channel":
		if parts[1] != "" {
			return "https://www.youtube.com/feeds/videos.xml?channel_id=" + url.QueryEscape(parts[1])
		}
	case "user":
		if parts[1] != "" {
			return "https://www.youtube.com/feeds/videos.xml?user=" + url.QueryEscape(parts[1])
		}
	}
	return ""
}

// redditFeedURL maps community and user pages to their .rss endpoints. Feed
// URLs themselves are ignored, since they are not HTML pages to search.
func redditFeedURL(u *url.URL) string {
	host := strings.ToLower(u.Hostname())
	switch host {
	case "reddit.com", "www.reddit.com", "old.reddit.com", "np.reddit.com":
	default:
		return ""
	}
	path := strings.TrimSuffix(u.Path, "/")
	lower := strings.ToLower(path)
	if strings.HasSuffix(lower, ".rss") || strings.HasSuffix(lower, ".json") {
		return ""
	}
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) < 2 || parts[1] == "" {
		return ""
	}
	switch parts[0] {
	case "r":
		return "https://www.reddit.com/r/" + url.PathEscape(parts[1]) + "/.rss"
	case "user":
		return "https://www.reddit.com/user/" + url.PathEscape(parts[1]) + "/.rss"
	}
	return ""
}
