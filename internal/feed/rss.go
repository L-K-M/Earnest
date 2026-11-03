package feed

import (
	"strconv"
	"strings"
)

// parseXMLFeed builds the shared tree and hands it to the right family parser.
func parseXMLFeed(data []byte, opts ParseOptions, limits Limits, kind format) (*Feed, error) {
	dec := newXMLDecoder(data)
	root, err := buildTree(dec, limits.deadline(), opts.BaseURL)
	if err != nil {
		return nil, err
	}
	doc := documentElement(root)
	if doc == nil {
		return nil, ErrMalformed
	}

	switch kind {
	case formatRSS:
		return parseRSS(doc, opts, limits, false)
	case formatRSS1:
		return parseRSS(doc, opts, limits, true)
	default:
		return parseAtom(doc, opts, limits)
	}
}

// parseRSS reads both RSS families. The RDF flag selects where items live
// (top level instead of inside channel) and adds the Dublin Core elements RSS
// 1.0 shares with its item nodes.
func parseRSS(root *node, opts ParseOptions, limits Limits, rdf bool) (*Feed, error) {
	feed := &Feed{FeedURL: opts.BaseURL}
	channel := root.child("channel")

	feed.Language = root.attr(nsXML, "lang")
	if channel != nil {
		if lang := channel.attr(nsXML, "lang"); lang != "" {
			feed.Language = lang
		}
		if lang := cleanText(childText(channel, "language")); lang != "" {
			feed.Language = lang
		}
		feed.Title = plainTitle(nodeText(channel.child("title")))
		feed.SiteURL = rssSiteURL(channel, opts.BaseURL)
		feed.IconURL = rssIcon(channel, feed.SiteURL, opts.BaseURL)
	}
	if feed.Language == "" && channel == nil {
		feed.Language = cleanText(childText(root, "language"))
	}

	// RSS 1.0 puts items beside the channel; RSS 2.0 normally puts them
	// inside it. Items directly under <rss> are tolerated even though they
	// violate the 2.0 layout.
	var itemNodes []*node
	switch {
	case rdf:
		itemNodes = root.children("item")
	case channel != nil:
		itemNodes = channel.children("item")
	default:
		itemNodes = root.children("item")
	}

	for _, n := range itemNodes {
		if len(feed.Items) >= limits.MaxItems {
			return nil, ErrTooManyItems
		}
		feed.Items = append(feed.Items, parseRSSItem(n, feed.Language, rdf, feed.SiteURL, opts.BaseURL))
	}
	return feed, nil
}

// parseRSSItem maps one RSS item. Unknown elements are ignored. URL fields
// resolve against xml:base first, then the feed link, then the feed URL.
func parseRSSItem(n *node, feedLang string, rdf bool, feedLink, feedURL string) Item {
	it := Item{Language: feedLang}
	if lang := n.attr(nsXML, "lang"); lang != "" {
		it.Language = lang
	} else if lang := cleanText(childText(n, "language")); lang != "" {
		it.Language = lang
	}

	it.Title = plainTitle(nodeText(n.child("title")))

	if guid := n.child("guid"); guid != nil {
		it.GUID = cleanText(textContent(guid))
		if strings.EqualFold(strings.TrimSpace(guidAttrFold(guid, "isPermaLink")), "false") {
			permalink := false
			it.guidPermalink = &permalink
		}
	}

	it.Author = plainTitle(firstNonEmpty(
		cleanText(childText(n, "creator")),
		cleanText(childText(n, "author")),
	))

	it.Published = parseDate(firstNonEmpty(childText(n, "pubDate"), childText(n, "date")))
	it.Updated = parseDate(childText(n, "updated"))

	it.URL = rssItemURL(n, it.GUID, it.guidPermalink, feedLink, feedURL)

	description := n.child("description")
	it.SummaryHTML = resolveHTML(nodeText(description), effectiveBase(description, feedLink, feedURL))
	content := n.child("encoded")
	it.ContentHTML = resolveHTML(nodeText(content), effectiveBase(content, feedLink, feedURL))

	for _, enc := range n.children("enclosure") {
		url := absURL(effectiveBase(enc, feedLink, feedURL), enc.attrAny("url"))
		if url == "" {
			continue
		}
		length, _ := strconv.ParseInt(strings.TrimSpace(enc.attrAny("length")), 10, 64)
		it.Enclosures = append(it.Enclosures, Enclosure{
			URL:    url,
			Type:   strings.TrimSpace(enc.attrAny("type")),
			Length: length,
		})
	}

	seen := make(map[string]bool)
	addCategory := func(s string) {
		s = plainTitle(s)
		if s == "" || seen[s] {
			return
		}
		seen[s] = true
		it.Categories = append(it.Categories, s)
	}
	for _, c := range n.children("category") {
		addCategory(textContent(c))
	}
	for _, c := range n.children("subject") {
		addCategory(textContent(c))
	}

	// RSS 1.0 carries the canonical identifier on rdf:about.
	if it.GUID == "" && rdf {
		it.GUID = cleanText(n.attr(nsRDF, "about"))
	}
	return it
}

// guidAttrFold reads an attribute whose case varies in the wild, such as
// isPermaLink versus ispermalink.
func guidAttrFold(n *node, want string) string {
	for _, a := range n.attrs {
		if strings.EqualFold(a.Name.Local, want) {
			return a.Value
		}
	}
	return ""
}

// rssItemURL prefers a plain link, falls back to an atom:link alternate and
// finally to a GUID that is itself a permalink URL.
func rssItemURL(n *node, guid string, permalink *bool, feedLink, feedURL string) string {
	if ln := n.child("link"); ln != nil {
		if text := cleanText(textContent(ln)); text != "" {
			return absURL(effectiveBase(ln, feedLink, feedURL), text)
		}
	}
	for _, ln := range n.children("link") {
		rel := strings.ToLower(strings.TrimSpace(ln.attrAny("rel")))
		if rel == "" || rel == "alternate" {
			if href := cleanText(ln.attrAny("href")); href != "" {
				return absURL(effectiveBase(ln, feedLink, feedURL), href)
			}
		}
	}
	if CleanURL(guid) != "" && (permalink == nil || *permalink) {
		return CleanURL(guid)
	}
	return ""
}

// rssSiteURL finds the channel's HTML link, falling back to an atom:link
// alternate. rel=self is the feed address, not the site, so it is skipped.
func rssSiteURL(channel *node, feedURL string) string {
	if ln := channel.child("link"); ln != nil {
		if text := cleanText(textContent(ln)); text != "" {
			return absURL(effectiveBase(ln, "", feedURL), text)
		}
	}
	for _, ln := range channel.children("link") {
		rel := strings.ToLower(strings.TrimSpace(ln.attrAny("rel")))
		if rel != "" && rel != "alternate" {
			continue
		}
		if href := cleanText(ln.attrAny("href")); href != "" {
			return absURL(effectiveBase(ln, "", feedURL), href)
		}
	}
	return ""
}

// rssIcon finds the channel image: RSS <image><url>, RDF image
// rdf:resource, or iTunes <itunes:image href>.
func rssIcon(channel *node, feedLink, feedURL string) string {
	for _, img := range channel.children("image") {
		base := effectiveBase(img, feedLink, feedURL)
		if u := cleanText(childText(img, "url")); u != "" {
			return absURL(base, u)
		}
		if r := cleanText(img.attrAny("resource")); r != "" {
			return absURL(base, r)
		}
		if h := cleanText(img.attrAny("href")); h != "" {
			return absURL(base, h)
		}
	}
	return ""
}

// childText returns the concatenated text of the first matching child.
func childText(n *node, local string) string {
	c := n.child(local)
	if c == nil {
		return ""
	}
	return textContent(c)
}

// cleanText trims and collapses whitespace.
func cleanText(s string) string {
	return collapseWS(s)
}

// firstNonEmpty returns the first argument that survives trimming.
func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}
