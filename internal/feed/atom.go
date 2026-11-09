package feed

import (
	"encoding/base64"
	"net/url"
	"strconv"
	"strings"
)

// parseAtom reads both Atom versions. The root namespace tells them apart;
// untagged <feed> roots are treated as Atom 1.0.
func parseAtom(root *node, opts ParseOptions, limits Limits) (*Feed, error) {
	feed := &Feed{FeedURL: opts.BaseURL}
	is03 := root.name.Space == nsAtom03

	feed.Language = root.attr(nsXML, "lang")
	feed.Title = atomText(root.child("title"))
	feed.SiteURL = atomSiteURL(root, opts.BaseURL)
	feed.IconURL = atomIcon(root, feed.SiteURL, opts.BaseURL)

	for _, entry := range root.children("entry") {
		if len(feed.Items) >= limits.MaxItems {
			return nil, ErrTooManyItems
		}
		feed.Items = append(feed.Items, parseAtomEntry(entry, feed.Language, is03, feed.SiteURL, opts.BaseURL))
	}
	return feed, nil
}

// parseAtomEntry maps one entry. Atom ids are permanent identifiers by
// specification, so they need no stability heuristic. Relative URLs resolve
// against xml:base, then the feed link, then the feed URL.
func parseAtomEntry(n *node, feedLang string, is03 bool, feedLink, feedURL string) Item {
	it := Item{Language: feedLang}
	if lang := n.attr(nsXML, "lang"); lang != "" {
		it.Language = lang
	}

	links := n.children("link")
	idNode := n.child("id")
	it.GUID = atomID(idNode, feedLink, feedURL)
	it.URL = atomEntryURL(links, feedLink, feedURL)
	if it.URL == "" {
		if g := CleanURL(it.GUID); g != "" {
			it.URL = g
		}
	}

	it.Title = atomText(n.child("title"))
	it.Author = atomAuthor(n)

	if is03 {
		it.Published = parseDate(firstNonEmpty(childText(n, "issued"), childText(n, "created")))
		it.Updated = parseDate(childText(n, "modified"))
	} else {
		it.Published = parseDate(childText(n, "published"))
		it.Updated = parseDate(childText(n, "updated"))
	}

	summary := n.child("summary")
	it.SummaryHTML = atomMarkup(summary, effectiveBase(summary, feedLink, feedURL))
	content := n.child("content")
	it.ContentHTML = atomContent(content, effectiveBase(content, feedLink, feedURL), is03)

	for _, l := range links {
		if !strings.EqualFold(strings.TrimSpace(l.attrAny("rel")), "enclosure") {
			continue
		}
		href := cleanText(l.attrAny("href"))
		if href == "" {
			continue
		}
		length, _ := strconv.ParseInt(strings.TrimSpace(l.attrAny("length")), 10, 64)
		it.Enclosures = append(it.Enclosures, Enclosure{
			URL:    absURL(effectiveBase(l, feedLink, feedURL), href),
			Type:   strings.TrimSpace(l.attrAny("type")),
			Length: length,
		})
	}
	it.Image = itemImage(n, it.Enclosures, feedLink, feedURL)

	seen := make(map[string]bool)
	addCategory := func(s string) {
		s = cleanText(s)
		if s == "" || seen[s] {
			return
		}
		seen[s] = true
		it.Categories = append(it.Categories, s)
	}
	for _, c := range n.children("category") {
		addCategory(firstNonEmpty(c.attrAny("term"), c.attrAny("label")))
	}

	return it
}

// atomText renders a type-carrying Atom text construct as display text.
func atomText(n *node) string {
	if n == nil {
		return ""
	}
	typ := strings.ToLower(strings.TrimSpace(n.attrAny("type")))
	switch typ {
	case "xhtml", "application/xhtml+xml":
		return htmlText("<div>" + unwrapXHTMLDiv(n) + "</div>")
	default:
		return plainTitle(textContent(n))
	}
}

// atomID returns the entry id, resolving a relative IRI against the effective
// base. Atom ids are usually absolute URIs already.
func atomID(n *node, feedLink, feedURL string) string {
	id := cleanText(nodeText(n))
	if id == "" {
		return ""
	}
	if u, err := url.Parse(id); err == nil && u.IsAbs() {
		return id
	}
	return ResolveURL(effectiveBase(n, feedLink, feedURL), id)
}

// atomEntryURL picks the alternate link: HTML when available, otherwise the
// first alternate. Links with other relations are not entry URLs.
func atomEntryURL(links []*node, feedLink, feedURL string) string {
	var alternate, html *node
	for _, l := range links {
		rel := strings.ToLower(strings.TrimSpace(l.attrAny("rel")))
		if rel == "" {
			rel = "alternate"
		}
		if rel != "alternate" {
			continue
		}
		href := cleanText(l.attrAny("href"))
		if href == "" {
			continue
		}
		typ := strings.ToLower(strings.TrimSpace(l.attrAny("type")))
		if alternate == nil {
			alternate = l
		}
		if html == nil && (typ == "" || typ == "text/html" || typ == "application/xhtml+xml") {
			html = l
		}
	}
	pick := html
	if pick == nil {
		pick = alternate
	}
	if pick == nil {
		return ""
	}
	return absURL(effectiveBase(pick, feedLink, feedURL), pick.attrAny("href"))
}

// atomSiteURL picks the feed-level alternate link.
func atomSiteURL(root *node, feedURL string) string {
	return atomEntryURL(root.children("link"), "", feedURL)
}

// atomIcon prefers <icon> and falls back to <logo>.
func atomIcon(root *node, feedLink, feedURL string) string {
	for _, name := range []string{"icon", "logo"} {
		n := root.child(name)
		if s := cleanText(nodeText(n)); s != "" {
			return absURL(effectiveBase(n, feedLink, feedURL), s)
		}
	}
	return ""
}

// atomAuthor returns the first author's display name.
func atomAuthor(n *node) string {
	for _, a := range n.children("author") {
		if name := plainTitle(cleanText(nodeText(a.child("name")))); name != "" {
			return name
		}
	}
	return ""
}

// atomMarkup converts a text construct to HTML according to its type.
func atomMarkup(n *node, base string) string {
	if n == nil {
		return ""
	}
	typ := strings.ToLower(strings.TrimSpace(n.attrAny("type")))
	switch typ {
	case "html", "text/html":
		return resolveHTML(textContent(n), base)
	case "xhtml", "application/xhtml+xml":
		return resolveHTMLFragment(unwrapXHTMLDiv(n), base)
	case "", "text", "text/plain":
		return escapeText(textContent(n))
	default:
		if strings.HasPrefix(typ, "text/") {
			return escapeText(textContent(n))
		}
		return ""
	}
}

// atomContent converts Atom 1.0 content. Content with a src attribute is
// external and is never fetched, so it contributes nothing here.
func atomContent(n *node, base string, is03 bool) string {
	if n == nil {
		return ""
	}
	if is03 {
		return atom03Content(n, base)
	}
	if cleanText(n.attrAny("src")) != "" {
		return ""
	}
	return atomMarkup(n, base)
}

// atom03Content honours the Atom 0.3 mode attribute: escaped text, base64
// payloads, or inline XML whose children are rendered as markup.
func atom03Content(n *node, base string) string {
	mode := strings.ToLower(strings.TrimSpace(n.attrAny("mode")))
	typ := strings.ToLower(strings.TrimSpace(n.attrAny("type")))
	raw := textContent(n)

	switch mode {
	case "base64":
		decoded, err := base64.StdEncoding.DecodeString(strings.TrimSpace(raw))
		if err != nil {
			return ""
		}
		return atom03Text(string(decoded), typ, base)
	case "escaped":
		return atom03Text(raw, typ, base)
	default:
		return atom03Markup(serializeChildren(n), typ, base)
	}
}

func atom03Text(raw, typ, base string) string {
	if strings.Contains(typ, "html") {
		return resolveHTML(raw, base)
	}
	return escapeText(raw)
}

func atom03Markup(markup, typ, base string) string {
	lower := strings.ToLower(typ)
	if strings.Contains(lower, "html") || strings.Contains(lower, "xml") {
		return resolveHTML(markup, base)
	}
	return escapeText(markup)
}

// unwrapXHTMLDiv removes the required xhtml div wrapper from an xhtml text
// construct, leaving the inner markup.
func unwrapXHTMLDiv(n *node) string {
	var elements []*node
	for _, p := range n.parts {
		if p.child != nil {
			elements = append(elements, p.child)
		}
	}
	if len(elements) == 1 && elements[0].name.Local == "div" && (elements[0].name.Space == nsXHTML || elements[0].name.Space == "") {
		return serializeChildren(elements[0])
	}
	return serializeChildren(n)
}
