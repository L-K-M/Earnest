// Package extract finds the main article content in a fetched HTML page and
// returns it in the sanitised subset Earnest stores.
//
// Fetching is the caller's job (internal/outbound); Extract takes the bytes
// and the page URL, runs codeberg.org/readeck/go-readability over them, and
// passes the result through internal/sanitize. A page that yields no
// readable content is reported with a Status, not an error, so the caller
// can store the metadata and show an honest failure.
package extract

import (
	"bytes"
	"fmt"
	"net/url"
	"unicode/utf8"

	readability "codeberg.org/readeck/go-readability/v2"
	"golang.org/x/net/html"
	"golang.org/x/net/html/charset"

	"github.com/L-K-M/Earnest/internal/sanitize"
)

// Status says what Extract found on the page.
type Status uint8

const (
	// StatusOK means the page yielded an article at or above MinTextChars.
	StatusOK Status = iota
	// StatusTooShort means readable content was found, but below
	// MinTextChars.
	StatusTooShort
	// StatusNotArticle means the page had no readable article content.
	StatusNotArticle
)

// String returns the status as it appears in logs and API responses.
func (s Status) String() string {
	switch s {
	case StatusOK:
		return "ok"
	case StatusTooShort:
		return "too_short"
	case StatusNotArticle:
		return "not_article"
	default:
		return "unknown"
	}
}

// MinTextChars is the minimum visible text length for a page to count as an
// article. It matches readability's own default threshold.
const MinTextChars = 500

// Result is the extracted article. Document is populated for StatusOK and
// StatusTooShort, and empty for StatusNotArticle.
type Result struct {
	Document sanitize.Document
	Title    string
	Byline   string
	Excerpt  string
	Language string
	Status   Status
}

// Extract returns the main article content of body as sanitised HTML.
// pageURL resolves relative URLs in the page; it may be nil, in which case
// relative URLs are dropped by the sanitiser. A <base> element in the page
// is ignored: relative URLs always resolve against pageURL, so a publisher
// cannot retarget them at another origin.
func Extract(pageURL *url.URL, body []byte) (Result, error) {
	parser := readability.NewParser()
	article, err := parser.Parse(bytes.NewReader(decodeUTF8(body)), pageURL)
	if err != nil {
		return Result{}, fmt.Errorf("extract: parse article: %w", err)
	}

	result := Result{
		Title:    article.Title(),
		Byline:   article.Byline(),
		Excerpt:  article.Excerpt(),
		Language: article.Language(),
	}

	if article.Node == nil {
		result.Status = StatusNotArticle
		return result, nil
	}

	content := renderNode(article.Node)
	text := sanitize.Text(content)
	if text == "" {
		result.Status = StatusNotArticle
		return result, nil
	}

	doc, err := sanitize.Sanitize(pageURL, content)
	if err != nil {
		return Result{}, fmt.Errorf("extract: sanitise article: %w", err)
	}
	result.Document = doc

	if utf8.RuneCountInString(text) < MinTextChars {
		result.Status = StatusTooShort
		return result, nil
	}
	result.Status = StatusOK
	return result, nil
}

// renderNode serialises a readability node as an HTML fragment.
func renderNode(n *html.Node) string {
	var buf bytes.Buffer
	if err := html.Render(&buf, n); err != nil {
		return ""
	}
	return buf.String()
}

// decodeUTF8 converts a legacy-encoded page to UTF-8 when the bytes are not
// already valid UTF-8. Pages without an encoding declaration are assumed to
// be UTF-8, the common case; DetermineEncoding's windows-1252 default would
// otherwise mangle them.
func decodeUTF8(body []byte) []byte {
	if utf8.Valid(body) {
		return body
	}

	enc, _, _ := charset.DetermineEncoding(body, "")
	if enc == nil {
		return body
	}
	decoded, err := enc.NewDecoder().Bytes(body)
	if err != nil {
		return body
	}
	return decoded
}
