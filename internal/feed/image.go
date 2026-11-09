package feed

import (
	"net/url"
	"strconv"
	"strings"
)

// Media RSS and iTunes namespaces. The Media RSS URI is sometimes written
// without its trailing slash, so both spellings are accepted.
const (
	nsMediaRSS    = "http://search.yahoo.com/mrss/"
	nsMediaRSSAlt = "http://search.yahoo.com/mrss"
	nsITunes      = "http://www.itunes.com/dtds/podcast-1.0.dtd"
)

// imageExtensions identify a media:content element as an image when it
// declares neither medium nor type.
var imageExtensions = []string{".jpg", ".jpeg", ".png", ".gif", ".webp", ".avif"}

// itemImage returns the publisher's representative image for an XML item.
// Sources are tried in priority order and the first valid URL wins; images
// inside the body are never considered. Enclosures are the last source and
// are shared with JSON Feed.
func itemImage(n *node, enclosures []Enclosure, feedLink, feedURL string) string {
	if u := mediaThumbnailImage(n, feedLink, feedURL); u != "" {
		return u
	}
	if u := mediaContentImage(n, feedLink, feedURL); u != "" {
		return u
	}
	if u := itunesImage(n, feedLink, feedURL); u != "" {
		return u
	}
	return enclosureImage(enclosures)
}

// jsonFeedItemImage applies the JSON Feed sources: item image, then
// banner_image, then the shared enclosure fallback.
func jsonFeedItemImage(m map[string]any, base string, enclosures []Enclosure) string {
	for _, key := range []string{"image", "banner_image"} {
		if u := validImageURL(absURL(base, coerceString(m[key]))); u != "" {
			return u
		}
	}
	return enclosureImage(enclosures)
}

// mediaThumbnailImage picks the widest media:thumbnail among the item's own
// thumbnails, those in a media:group and those in a media:content.
func mediaThumbnailImage(n *node, feedLink, feedURL string) string {
	return pickWidest(mediaThumbnails(n), feedLink, feedURL)
}

// mediaContentImage picks the widest image-like media:content on the item or
// in a media:group.
func mediaContentImage(n *node, feedLink, feedURL string) string {
	var cands []*node
	for _, c := range mediaContentNodes(n) {
		if isImageMediaContent(c) {
			cands = append(cands, c)
		}
	}
	return pickWidest(cands, feedLink, feedURL)
}

// mediaThumbnails collects thumbnails in document order from the item's own
// children, media:group children and media:content children.
func mediaThumbnails(n *node) []*node {
	var out []*node
	for _, p := range n.parts {
		c := p.child
		if c == nil || !isMediaNamespace(c.name.Space) {
			continue
		}
		switch c.name.Local {
		case "thumbnail":
			out = append(out, c)
		case "group", "content":
			out = append(out, mediaNodes(c, "thumbnail")...)
		}
	}
	return out
}

// mediaContentNodes collects media:content elements in document order from
// the item's own children and media:group children.
func mediaContentNodes(n *node) []*node {
	var out []*node
	for _, p := range n.parts {
		c := p.child
		if c == nil || !isMediaNamespace(c.name.Space) {
			continue
		}
		switch c.name.Local {
		case "content":
			out = append(out, c)
		case "group":
			out = append(out, mediaNodes(c, "content")...)
		}
	}
	return out
}

// pickWidest selects the candidate with the largest numeric width; a missing
// or invalid width counts as zero and ties keep document order. The winner's
// url attribute is resolved against xml:base and the feed's own base.
func pickWidest(cands []*node, feedLink, feedURL string) string {
	best := -1
	bestWidth := 0
	for i, c := range cands {
		if strings.TrimSpace(c.attrAny("url")) == "" {
			continue
		}
		width, err := strconv.Atoi(strings.TrimSpace(c.attrAny("width")))
		if err != nil {
			width = 0
		}
		if best == -1 || width > bestWidth {
			best, bestWidth = i, width
		}
	}
	if best == -1 {
		return ""
	}
	return validImageURL(absURL(effectiveBase(cands[best], feedLink, feedURL), cands[best].attrAny("url")))
}

// itunesImage returns the item-level iTunes cover image.
func itunesImage(n *node, feedLink, feedURL string) string {
	for _, p := range n.parts {
		c := p.child
		if c == nil || c.name.Local != "image" || c.name.Space != nsITunes {
			continue
		}
		if u := validImageURL(absURL(effectiveBase(c, feedLink, feedURL), c.attrAny("href"))); u != "" {
			return u
		}
	}
	return ""
}

// enclosureImage returns the first image/* enclosure that is a usable URL.
func enclosureImage(enclosures []Enclosure) string {
	for _, enc := range enclosures {
		if !strings.HasPrefix(strings.ToLower(strings.TrimSpace(enc.Type)), "image/") {
			continue
		}
		if u := validImageURL(enc.URL); u != "" {
			return u
		}
	}
	return ""
}

// mediaNodes returns direct children of n in the Media RSS namespace with the
// given local name. Matching the namespace is deliberate: Atom's own
// <content> is the article body and must not be read as media:content.
func mediaNodes(n *node, local string) []*node {
	var out []*node
	for _, p := range n.parts {
		c := p.child
		if c == nil || c.name.Local != local || !isMediaNamespace(c.name.Space) {
			continue
		}
		out = append(out, c)
	}
	return out
}

func isMediaNamespace(space string) bool {
	return space == nsMediaRSS || space == nsMediaRSSAlt
}

// isImageMediaContent reports whether a media:content element declares an
// image: an explicit medium or type, or an image file extension when neither
// is present.
func isImageMediaContent(n *node) bool {
	medium := strings.ToLower(strings.TrimSpace(n.attrAny("medium")))
	typ := strings.ToLower(strings.TrimSpace(n.attrAny("type")))
	if medium == "image" || strings.HasPrefix(typ, "image/") {
		return true
	}
	if medium != "" || typ != "" {
		return false
	}
	u, err := url.Parse(strings.TrimSpace(n.attrAny("url")))
	if err != nil {
		return false
	}
	path := strings.ToLower(u.Path)
	for _, ext := range imageExtensions {
		if strings.HasSuffix(path, ext) {
			return true
		}
	}
	return false
}

// validImageURL accepts only absolute http(s) URLs with a host. SVG is
// rejected because the reader cannot rasterise it. The URL is returned
// unchanged; cleaning happens in absURL, as for enclosures.
func validImageURL(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	if !strings.EqualFold(u.Scheme, "http") && !strings.EqualFold(u.Scheme, "https") {
		return ""
	}
	if u.Host == "" {
		return ""
	}
	path := strings.ToLower(u.Path)
	if strings.HasSuffix(path, ".svg") || strings.HasSuffix(path, ".svgz") {
		return ""
	}
	return raw
}
