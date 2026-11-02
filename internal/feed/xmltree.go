package feed

import (
	"encoding/xml"
	"fmt"
	"html"
	"io"
	"strings"
	"time"
)

// Namespace URIs used to qualify attributes and to tell Atom versions apart.
const (
	nsXML    = "http://www.w3.org/XML/1998/namespace"
	nsRDF    = "http://www.w3.org/1999/02/22-rdf-syntax-ns#"
	nsAtom   = "http://www.w3.org/2005/Atom"
	nsAtom03 = "http://purl.org/atom/ns#"
	nsXHTML  = "http://www.w3.org/1999/xhtml"
)

const (
	// maxXMLDepth is far above any real feed but stops nesting bombs.
	maxXMLDepth = 200
	// maxXMLElements bounds total nodes. Real feeds with 2000 items stay an
	// order of magnitude below this.
	maxXMLElements = 200000
	// parseCheckInterval is how often the token walk looks at the deadline.
	parseCheckInterval = 64
)

// node is one XML element with its ordered content.
type node struct {
	name    xml.Name
	attrs   []xml.Attr
	parts   []nodePart
	base    string
	baseSet bool
}

// nodePart keeps character data and child elements in document order, which
// matters when serialising mixed XHTML content back to HTML.
type nodePart struct {
	text  string
	child *node
}

// buildTree consumes the decoder to EOF and returns the document root. The
// decoder runs in non-strict mode so real-world junk does not abort the walk;
// structural damage still surfaces as an error from the decoder or as an
// unclosed element at EOF.
func buildTree(dec *xml.Decoder, deadline time.Time, base string) (*node, error) {
	root := &node{name: xml.Name{Local: "#document"}, base: base}
	stack := []*node{root}
	elements := 0
	tokens := 0
	closed := false

	for {
		if tokens%parseCheckInterval == 0 && !deadline.IsZero() && time.Now().After(deadline) {
			return nil, ErrParseTimeout
		}
		tok, err := dec.Token()
		tokens++

		if err == io.EOF {
			if !closed {
				return nil, fmt.Errorf("%w: unclosed element <%s>", ErrMalformed, stack[len(stack)-1].name.Local)
			}
			return root, nil
		}
		if err != nil {
			return nil, fmt.Errorf("%w: %v", ErrMalformed, err)
		}

		switch t := tok.(type) {
		case xml.StartElement:
			if closed {
				return nil, fmt.Errorf("%w: content after the root element", ErrMalformed)
			}
			elements++
			if elements > maxXMLElements {
				return nil, fmt.Errorf("%w: more than %d elements", ErrMalformed, maxXMLElements)
			}
			if len(stack) > maxXMLDepth {
				return nil, fmt.Errorf("%w: nesting deeper than %d levels", ErrMalformed, maxXMLDepth)
			}
			parent := stack[len(stack)-1]
			n := &node{
				name:    t.Name,
				attrs:   append([]xml.Attr(nil), t.Attr...),
				base:    parent.base,
				baseSet: parent.baseSet,
			}
			if declared := n.attr(nsXML, "base"); declared != "" {
				n.base = ResolveURL(parent.base, declared)
				n.baseSet = true
			}
			parent.parts = append(parent.parts, nodePart{child: n})
			stack = append(stack, n)

		case xml.EndElement:
			if len(stack) <= 1 || closed {
				return nil, fmt.Errorf("%w: unexpected end element </%s>", ErrMalformed, t.Name.Local)
			}
			stack = stack[:len(stack)-1]
			if len(stack) == 1 {
				closed = true
			}

		case xml.CharData:
			if closed && len(strings.TrimSpace(string(t))) > 0 {
				return nil, fmt.Errorf("%w: content after the root element", ErrMalformed)
			}
			if !closed {
				cur := stack[len(stack)-1]
				cur.parts = append(cur.parts, nodePart{text: string(t)})
			}
		}
	}
}

// attr returns the value of a namespace-qualified attribute.
func (n *node) attr(space, local string) string {
	for _, a := range n.attrs {
		if a.Name.Space == space && a.Name.Local == local {
			return a.Value
		}
	}
	return ""
}

// attrAny returns the first attribute with the given local name, whatever its
// namespace. Feed extensions reuse local names such as "href" liberally.
func (n *node) attrAny(local string) string {
	for _, a := range n.attrs {
		if a.Name.Local == local {
			return a.Value
		}
	}
	return ""
}

// child returns the first direct child with the given local name.
func (n *node) child(local string) *node {
	for _, p := range n.parts {
		if p.child != nil && p.child.name.Local == local {
			return p.child
		}
	}
	return nil
}

// children returns every direct child with the given local name.
func (n *node) children(local string) []*node {
	var out []*node
	for _, p := range n.parts {
		if p.child != nil && p.child.name.Local == local {
			out = append(out, p.child)
		}
	}
	return out
}

// textContent returns all descendant character data in document order.
func textContent(n *node) string {
	var b strings.Builder
	writeText(n, &b)
	return b.String()
}

// documentElement returns the root element below the synthetic document node.
func documentElement(root *node) *node {
	for _, p := range root.parts {
		if p.child != nil {
			return p.child
		}
	}
	return nil
}

// effectiveBase applies the documented resolution order: a declared
// xml:base anywhere on the ancestor chain wins, then the feed link, then the
// URL the feed was fetched from.
func effectiveBase(n *node, feedLink, feedURL string) string {
	if n != nil && n.baseSet {
		return n.base
	}
	if feedLink != "" {
		return feedLink
	}
	return feedURL
}

// nodeText is the nil-safe form used by extraction code.
func nodeText(n *node) string {
	if n == nil {
		return ""
	}
	return textContent(n)
}

func writeText(n *node, b *strings.Builder) {
	for _, p := range n.parts {
		b.WriteString(p.text)
		if p.child != nil {
			writeText(p.child, b)
		}
	}
}

// serializeChildren renders the node's content as HTML, preserving child
// markup. Used for Atom xhtml content and for titles that carry xhtml.
func serializeChildren(n *node) string {
	var b strings.Builder
	for _, p := range n.parts {
		serializePart(p, &b)
	}
	return b.String()
}

func serializePart(p nodePart, b *strings.Builder) {
	if p.child == nil {
		b.WriteString(html.EscapeString(p.text))
		return
	}
	serializeNode(p.child, b)
}

func serializeNode(n *node, b *strings.Builder) {
	name := n.name.Local
	b.WriteByte('<')
	b.WriteString(name)
	for _, a := range n.attrs {
		// Namespace declarations are an XML artefact; HTML output does not
		// need them and they would leak into stored content.
		if a.Name.Space == "http://www.w3.org/2000/xmlns/" || a.Name.Local == "xmlns" {
			continue
		}
		attrName := a.Name.Local
		if a.Name.Space == nsXML {
			attrName = "xml:" + attrName
		}
		b.WriteByte(' ')
		b.WriteString(attrName)
		b.WriteString(`="`)
		b.WriteString(html.EscapeString(a.Value))
		b.WriteString(`"`)
	}
	if isVoidElement(name) {
		b.WriteString("/>")
		return
	}
	b.WriteByte('>')
	for _, p := range n.parts {
		serializePart(p, b)
	}
	b.WriteString("</")
	b.WriteString(name)
	b.WriteByte('>')
}

// voidElements never take children or an end tag in HTML.
var voidElements = map[string]bool{
	"area": true, "base": true, "br": true, "col": true, "embed": true,
	"hr": true, "img": true, "input": true, "link": true, "meta": true,
	"param": true, "source": true, "track": true, "wbr": true,
}

func isVoidElement(name string) bool { return voidElements[name] }
