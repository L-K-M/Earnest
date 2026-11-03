package sanitize

import (
	"regexp"
	"strings"

	"golang.org/x/net/html"
)

// Text markers separate structure from source whitespace while the text is
// being assembled.
const (
	paraMark = "\x00"
	lineMark = "\x01"
)

var (
	textSentinels = strings.NewReplacer(paraMark, "", lineMark, "")
	textSpace     = regexp.MustCompile("[ \t\n\r\f\v]+")
	textParaPad   = regexp.MustCompile(" *" + paraMark + " *")
	textLinePad   = regexp.MustCompile(" *" + lineMark + " *")
	textBlank     = regexp.MustCompile("\n{3,}")
)

// Text returns the visible text of sanitised HTML with paragraph breaks:
// block elements start a new paragraph, br starts a new line within one,
// and runs of whitespace collapse to single spaces. Preformatted whitespace
// inside pre collapses too; the HTML remains the source of truth for
// rendering. Text is meant for search indexing and summaries, never for
// rendering.
func Text(htmlStr string) string {
	doc, err := html.Parse(strings.NewReader(htmlStr))
	if err != nil {
		return ""
	}

	var b strings.Builder
	writeText(&b, doc)
	return collapseText(b.String())
}

// writeText walks the tree, emitting text and structural markers.
func writeText(b *strings.Builder, n *html.Node) {
	switch n.Type {
	case html.TextNode:
		b.WriteString(textSentinels.Replace(n.Data))

	case html.ElementNode:
		if _, skip := invisibleElements[n.Data]; skip {
			return
		}
		if n.Data == "br" {
			b.WriteString(lineMark)
			return
		}
		block := isBlockElement(n.Data)
		if block {
			b.WriteString(paraMark)
		}
		for child := n.FirstChild; child != nil; child = child.NextSibling {
			writeText(b, child)
		}
		if block {
			b.WriteString(paraMark)
		}

	default:
		for child := n.FirstChild; child != nil; child = child.NextSibling {
			writeText(b, child)
		}
	}
}

func isBlockElement(tag string) bool {
	_, ok := blockElements[tag]
	return ok
}

// collapseText turns the markers into newlines and normalises whitespace.
func collapseText(s string) string {
	s = textSpace.ReplaceAllString(s, " ")
	s = textParaPad.ReplaceAllString(s, paraMark)
	s = textLinePad.ReplaceAllString(s, lineMark)
	s = strings.ReplaceAll(s, lineMark, "\n")
	s = strings.ReplaceAll(s, paraMark, "\n\n")
	s = textBlank.ReplaceAllString(s, "\n\n")
	return strings.TrimSpace(s)
}
