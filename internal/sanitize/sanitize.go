// Package sanitize reduces untrusted HTML to the subset Earnest stores and
// its clients render. The subset is versioned: Document carries the Version
// that produced it, so content written by an older version can be
// re-sanitised when the subset changes.
//
// Sanitising is tree-based. The input is parsed with x/net/html, an
// allowlisted copy is serialised, and the result is parsed and sanitised
// again until it stops changing. Output that does not reach a fixed point
// within maxPasses is rejected with ErrUnstable; callers must store nothing
// rather than fall back to the raw input.
//
// The rules are deliberately strict: script-capable, raw-text and foreign
// elements are dropped with their content; unknown elements are unwrapped;
// event handlers, style, class and id are always removed; and URL attributes
// are resolved against a base and restricted to http, https and (for links)
// mailto.
package sanitize

import (
	"bytes"
	"errors"
	"fmt"
	"net/url"
	"strings"

	"golang.org/x/net/html"
)

// Version identifies the sanitised HTML subset produced by this package.
// Bump it whenever the allowlist, attribute rules or URL policy change.
const Version uint8 = 1

// maxPasses caps the parse-sanitise-reparse loop. Sanitised output is
// normally a fixed point after one pass; the cap exists so a payload that
// keeps mutating fails closed instead of looping forever.
const maxPasses = 8

var (
	// ErrUnstable is returned when sanitising does not converge. Callers
	// must treat it as "no content" and never fall back to the raw input.
	ErrUnstable = errors.New("sanitize: output did not reach a fixed point")

	// ErrInvalidBase is returned when base has a scheme other than http or
	// https. Resolving against such a base could produce active URLs.
	ErrInvalidBase = errors.New("sanitize: base URL scheme must be http or https")
)

// Document is sanitised HTML plus the subset version that produced it.
type Document struct {
	Version uint8
	HTML    string
}

// Sanitize reduces input to the stored HTML subset. base resolves relative
// URLs; when base is nil, relative URLs are dropped. The result is a fixed
// point: Sanitize with the same base returns the same HTML.
func Sanitize(base *url.URL, input string) (Document, error) {
	if base != nil && base.Scheme != "" && base.Scheme != "http" && base.Scheme != "https" {
		return Document{}, fmt.Errorf("%w: %q", ErrInvalidBase, base.Scheme)
	}

	current := input
	for pass := 0; pass < maxPasses; pass++ {
		next, err := sanitizeOnce(base, current)
		if err != nil {
			return Document{}, err
		}
		if next == current {
			return Document{Version: Version, HTML: next}, nil
		}
		current = next
	}

	return Document{}, fmt.Errorf("%w after %d passes", ErrUnstable, maxPasses)
}

// sanitizeOnce parses input as a full document, copies the sanitised body
// children into a new tree and serialises them as a fragment.
func sanitizeOnce(base *url.URL, input string) (string, error) {
	doc, err := html.Parse(strings.NewReader(input))
	if err != nil {
		return "", fmt.Errorf("parse html: %w", err)
	}

	body := findElement(doc, "body")
	if body == nil {
		return "", nil
	}

	root := &html.Node{Type: html.ElementNode, Data: "div"}
	for child := body.FirstChild; child != nil; child = child.NextSibling {
		appendSanitized(root, child, base)
	}

	var buf bytes.Buffer
	for child := root.FirstChild; child != nil; child = child.NextSibling {
		if err := html.Render(&buf, child); err != nil {
			return "", fmt.Errorf("render html: %w", err)
		}
	}
	return buf.String(), nil
}

// appendSanitized copies n into parent when n is allowed. Dropped elements
// disappear with their subtree; unknown elements are unwrapped, keeping
// their sanitised children. Comments and doctype nodes are discarded.
func appendSanitized(parent, n *html.Node, base *url.URL) {
	switch n.Type {
	case html.TextNode:
		parent.AppendChild(&html.Node{Type: html.TextNode, Data: n.Data})

	case html.ElementNode:
		if _, drop := dropElements[n.Data]; drop {
			return
		}
		if _, ok := allowedElements[n.Data]; !ok {
			for child := n.FirstChild; child != nil; child = child.NextSibling {
				appendSanitized(parent, child, base)
			}
			return
		}

		attrs := sanitizeAttrs(n, base)
		if n.Data == "img" && !hasImageSource(attrs) {
			// Neither src nor srcset survived validation, so the element
			// would render as a broken image and can only carry alt text.
			return
		}

		elem := &html.Node{
			Type:      html.ElementNode,
			DataAtom:  n.DataAtom,
			Namespace: n.Namespace,
			Data:      n.Data,
			Attr:      attrs,
		}
		for child := n.FirstChild; child != nil; child = child.NextSibling {
			appendSanitized(elem, child, base)
		}
		parent.AppendChild(elem)
	}
}

// sanitizeAttrs builds the attribute list for an allowed element. Attributes
// are emitted in a fixed order so the output is canonical and the stability
// check is meaningful.
func sanitizeAttrs(n *html.Node, base *url.URL) []html.Attribute {
	var attrs []html.Attribute

	if v, ok := attributeValue(n, "dir"); ok {
		if dir, ok := cleanDir(v); ok {
			attrs = append(attrs, html.Attribute{Key: "dir", Val: dir})
		}
	}
	if v, ok := attributeValue(n, "lang"); ok {
		if lang, ok := cleanLang(v); ok {
			attrs = append(attrs, html.Attribute{Key: "lang", Val: lang})
		}
	}

	for _, name := range elementAttrs[n.Data] {
		v, ok := attributeValue(n, name)
		if !ok {
			continue
		}
		if v, ok = cleanAttr(base, name, v); ok {
			attrs = append(attrs, html.Attribute{Key: name, Val: v})
		}
	}

	switch n.Data {
	case "a":
		// The renderers sandbox content, but a link must not be able to
		// reach the parent document or leak the referrer.
		attrs = append(attrs,
			html.Attribute{Key: "target", Val: "_blank"},
			html.Attribute{Key: "rel", Val: "noopener noreferrer"},
		)
	case "img":
		attrs = append(attrs,
			html.Attribute{Key: "loading", Val: "lazy"},
			html.Attribute{Key: "referrerpolicy", Val: "no-referrer"},
		)
	}
	return attrs
}

// cleanAttr validates one attribute value. It returns false when the
// attribute must be dropped.
func cleanAttr(base *url.URL, name, val string) (string, bool) {
	switch name {
	case "href":
		return resolveURL(base, val, urlKindLink)
	case "src":
		return resolveURL(base, val, urlKindImage)
	case "srcset":
		return cleanSrcset(base, val)
	case "alt", "title", "datetime":
		return val, true
	case "width", "height", "colspan", "rowspan", "span":
		return cleanDigits(val)
	case "start", "value":
		return cleanInteger(val)
	case "open", "reversed":
		// Boolean attributes: the value is irrelevant, the name is enough.
		return "", true
	case "scope":
		return cleanScope(val)
	default:
		return "", false
	}
}

// hasImageSource reports whether an img kept at least one source.
func hasImageSource(attrs []html.Attribute) bool {
	for _, attr := range attrs {
		if attr.Key == "src" || attr.Key == "srcset" {
			return true
		}
	}
	return false
}

func attributeValue(n *html.Node, key string) (string, bool) {
	for _, attr := range n.Attr {
		if attr.Key == key {
			return attr.Val, true
		}
	}
	return "", false
}

func findElement(n *html.Node, tag string) *html.Node {
	if n.Type == html.ElementNode && n.Data == tag {
		return n
	}
	for child := n.FirstChild; child != nil; child = child.NextSibling {
		if found := findElement(child, tag); found != nil {
			return found
		}
	}
	return nil
}
