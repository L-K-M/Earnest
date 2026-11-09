package feed

import (
	"html"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"

	xhtml "golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

const (
	// maxHTMLFieldBytes caps per-field markup processing. Beyond this a field
	// is kept verbatim instead of being parsed, which keeps memory bounded
	// even though the input size limit already applies to the document.
	maxHTMLFieldBytes = 1 << 20
	// maxSafeHTMLDepth guards html.Render, which recurses per level. Real
	// article markup never comes close.
	maxSafeHTMLDepth = 1024
)

// tagStartRE finds the start of a real tag, so text like "5 < 6" or "<3 you"
// is not mistaken for markup.
var tagStartRE = regexp.MustCompile(`<[a-zA-Z/!]`)

// charRefRE matches a complete character reference. The semicolon is
// required so text such as "Mom&nottoday" or "&copy2026" is not read as
// HTML's legacy references.
var charRefRE = regexp.MustCompile(`&(?:#[0-9]{1,7}|#[xX][0-9a-fA-F]{1,6}|[a-zA-Z][a-zA-Z0-9]{1,31});`)

// decodeCharRefs decodes complete character references once. Publishers often
// escape titles twice, so after XML decoding a title still reads
// "Pomera&#8217;s". Unknown names stay as written.
func decodeCharRefs(s string) string {
	if !strings.Contains(s, "&") {
		return s
	}
	return charRefRE.ReplaceAllStringFunc(s, func(ref string) string {
		// html.UnescapeString also decodes a legacy name that merely starts
		// the reference, turning "&nottoday;" into "¬today;". A whole
		// reference expands to one or two characters.
		decoded := html.UnescapeString(ref)
		if decoded == ref || utf8.RuneCountInString(decoded) > 2 {
			return ref
		}
		// A title is one line of text: an encoded tab or line break becomes
		// a space, and other control characters stay as written.
		for _, r := range decoded {
			if unicode.IsSpace(r) {
				return " "
			}
			if unicode.IsControl(r) {
				return ref
			}
		}
		return decoded
	})
}

// looksLikeHTML reports whether s contains something that could be a tag.
func looksLikeHTML(s string) bool {
	return tagStartRE.MatchString(s)
}

// collapseWS trims and collapses all whitespace runs to single spaces.
func collapseWS(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

// escapeText makes plain text safe for storage in an HTML field.
func escapeText(s string) string {
	return html.EscapeString(s)
}

// plainTitle turns a title that may carry escaped or literal markup into
// display text.
func plainTitle(s string) string {
	display, _ := titleTexts(s)
	return display
}

// titleTexts returns a title's display text and the text the frozen identity
// rule hashes for items without a GUID or link. They differ only when the
// title holds character references outside markup, which earlier versions
// left undecoded; keeping that form keeps such items' identities stable.
func titleTexts(s string) (display, identity string) {
	s = strings.TrimSpace(s)
	if s == "" {
		return "", ""
	}
	if !looksLikeHTML(s) {
		return collapseWS(decodeCharRefs(s)), collapseWS(s)
	}
	text := htmlText(s)
	return text, text
}

// htmlText extracts the text content of an HTML fragment. Entities are
// decoded by the parser and whitespace is collapsed. Oversized or pathologically
// nested input falls back to treating the value as plain text.
func htmlText(s string) string {
	if strings.TrimSpace(s) == "" {
		return ""
	}
	if len(s) > maxHTMLFieldBytes {
		return collapseWS(s)
	}
	if !looksLikeHTML(s) {
		return collapseWS(s)
	}
	nodes, err := parseFragment(s)
	if err != nil {
		return collapseWS(s)
	}
	var b strings.Builder
	for _, n := range nodes {
		collectText(n, &b)
	}
	return collapseWS(b.String())
}

// parseFragment parses an HTML fragment without html/head/body wrappers.
func parseFragment(s string) ([]*xhtml.Node, error) {
	ctx := &xhtml.Node{Type: xhtml.ElementNode, Data: "div", DataAtom: atom.Div}
	return xhtml.ParseFragment(strings.NewReader(s), ctx)
}

// blockElements separate words when their text is extracted, so adjacent
// blocks do not run together.
var blockElements = map[string]bool{
	"address": true, "article": true, "aside": true, "blockquote": true,
	"br": true, "dd": true, "div": true, "dl": true, "dt": true,
	"fieldset": true, "figcaption": true, "figure": true, "footer": true,
	"form": true, "h1": true, "h2": true, "h3": true, "h4": true, "h5": true,
	"h6": true, "header": true, "hr": true, "li": true, "main": true,
	"nav": true, "ol": true, "p": true, "pre": true, "section": true,
	"table": true, "td": true, "th": true, "tr": true, "ul": true,
}

// collectText walks the tree iteratively so deep documents cannot exhaust the
// goroutine stack. Text nodes are appended in document order; block elements
// contribute a separating space on each side.
func collectText(root *xhtml.Node, b *strings.Builder) {
	type entry struct {
		n    *xhtml.Node
		exit bool
	}
	stack := []entry{{n: root}}
	for len(stack) > 0 {
		e := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		n := e.n

		if e.exit {
			if n.Type == xhtml.ElementNode && blockElements[n.Data] {
				b.WriteByte(' ')
			}
			continue
		}
		if n.Type == xhtml.TextNode {
			b.WriteString(n.Data)
			continue
		}
		if n.Type == xhtml.ElementNode && blockElements[n.Data] {
			b.WriteByte(' ')
		}
		stack = append(stack, entry{n: n, exit: true})
		for c := n.LastChild; c != nil; c = c.PrevSibling {
			stack = append(stack, entry{n: c})
		}
	}
}

// resolveHTML parses an HTML fragment, rewrites relative URLs against base and
// renders it back. Plain text is escaped rather than parsed. A document-level
// <base href> overrides the supplied base, matching browser behaviour.
func resolveHTML(s, base string) string {
	if strings.TrimSpace(s) == "" {
		return ""
	}
	if len(s) > maxHTMLFieldBytes {
		return s
	}
	if !looksLikeHTML(s) {
		return escapeText(s)
	}
	return resolveHTMLFragment(s, base)
}

// resolveHTMLFragment accepts markup that may contain only escaped text.
func resolveHTMLFragment(s, base string) string {
	if len(s) > maxHTMLFieldBytes {
		return s
	}
	nodes, err := parseFragment(s)
	if err != nil {
		return s
	}
	if fragmentDepth(nodes) > maxSafeHTMLDepth {
		return s
	}

	effectiveBase := base
	for _, n := range nodes {
		if href := findBaseHref(n); href != "" {
			if abs := ResolveURL(base, href); abs != "" {
				effectiveBase = abs
			}
			break
		}
	}

	var b strings.Builder
	for _, n := range nodes {
		rewriteURLs(n, effectiveBase)
		xhtml.Render(&b, n)
	}
	return b.String()
}

// fragmentDepth returns the deepest element nesting in the fragment.
func fragmentDepth(nodes []*xhtml.Node) int {
	type entry struct {
		n     *xhtml.Node
		depth int
	}
	maxDepth := 0
	var stack []entry
	for _, n := range nodes {
		stack = append(stack, entry{n: n, depth: 1})
	}
	for len(stack) > 0 {
		e := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if e.depth > maxDepth {
			maxDepth = e.depth
		}
		for c := e.n.FirstChild; c != nil; c = c.NextSibling {
			stack = append(stack, entry{n: c, depth: e.depth + 1})
		}
	}
	return maxDepth
}

// findBaseHref returns the href of the first <base> element in the subtree.
func findBaseHref(root *xhtml.Node) string {
	stack := []*xhtml.Node{root}
	for len(stack) > 0 {
		n := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if n.Type == xhtml.ElementNode && n.Data == "base" {
			if href := nodeAttr(n, "href"); href != "" {
				return href
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			stack = append(stack, c)
		}
	}
	return ""
}

// urlAttributes are attributes whose values are URLs that may be relative.
var urlAttributes = map[string]bool{
	"href": true, "src": true, "poster": true, "action": true,
	"cite": true, "data": true, "background": true, "longdesc": true,
}

// srcsetAttributes hold comma-separated candidate lists.
var srcsetAttributes = map[string]bool{
	"srcset": true, "imagesrcset": true,
}

// rewriteURLs walks the subtree iteratively, absolutising URL attribute values.
func rewriteURLs(root *xhtml.Node, base string) {
	stack := []*xhtml.Node{root}
	for len(stack) > 0 {
		n := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		for i := range n.Attr {
			attr := &n.Attr[i]
			switch {
			case urlAttributes[attr.Key]:
				attr.Val = absolutizeURL(attr.Val, base)
			case srcsetAttributes[attr.Key]:
				attr.Val = absolutizeSrcset(attr.Val, base)
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			stack = append(stack, c)
		}
	}
}

// nonWebSchemes are left alone: resolving them would corrupt them.
var nonWebSchemes = []string{
	"mailto:", "tel:", "javascript:", "vbscript:", "data:", "blob:",
	"file:", "about:", "chrome:", "ftp:", "ssh:",
}

// absolutizeURL resolves one attribute value against base. Fragment-only,
// empty and non-web schemes pass through unchanged.
func absolutizeURL(val, base string) string {
	val = strings.TrimSpace(val)
	if val == "" || strings.HasPrefix(val, "#") {
		return val
	}
	lower := strings.ToLower(val)
	for _, scheme := range nonWebSchemes {
		if strings.HasPrefix(lower, scheme) {
			return val
		}
	}
	return ResolveURL(base, val)
}

// absolutizeSrcset resolves every candidate in a srcset list, preserving the
// descriptors after each URL.
func absolutizeSrcset(val, base string) string {
	if !strings.Contains(val, ",") {
		return absolutizeURL(val, base)
	}
	parts := strings.Split(val, ",")
	for i, part := range parts {
		fields := strings.Fields(strings.TrimSpace(part))
		if fields == nil {
			continue
		}
		fields[0] = ResolveURL(base, fields[0])
		parts[i] = strings.Join(fields, " ")
	}
	return strings.Join(parts, ", ")
}

// nodeAttr returns an attribute value from an HTML node.
func nodeAttr(n *xhtml.Node, key string) string {
	for _, a := range n.Attr {
		if a.Key == key {
			return a.Val
		}
	}
	return ""
}
