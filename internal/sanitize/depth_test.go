package sanitize

import (
	"net/url"
	"strings"
	"testing"
)

// deepHTML nests count levels of <div> around a text node, the smallest
// well-formed payload that reaches the walkers at an arbitrary depth.
func deepHTML(count int) string {
	var b strings.Builder
	for i := 0; i < count; i++ {
		b.WriteString("<div>")
	}
	b.WriteString("hello")
	for i := 0; i < count; i++ {
		b.WriteString("</div>")
	}
	return b.String()
}

// Hostile publishers control item HTML depth. x/net/html rejects trees with
// more than 512 open elements (parse.go panics and parser.parse recovers it
// into an error), so the recursive walks in this package and html.Render only
// ever see bounded trees. These tests pin that boundary: ordinary nesting
// works, and over-deep input must fail or return empty output gracefully,
// never panic or overflow the goroutine stack. The fetcher keeps such items
// without content instead of failing the feed (fetcher_test.go).

func TestSanitizeDeepNestingBounded(t *testing.T) {
	base, err := url.Parse("https://example.com/")
	if err != nil {
		t.Fatal(err)
	}
	doc, err := Sanitize(base, deepHTML(256))
	if err != nil {
		t.Fatalf("ordinary nesting must sanitise: %v", err)
	}
	if !strings.Contains(doc.HTML, "hello") {
		t.Fatalf("ordinary nesting must keep its text: %q", doc.HTML)
	}
	if doc, err := Sanitize(base, deepHTML(1<<20)); err == nil {
		// If a future parser accepts deep input, the result must still be a
		// bounded fixed point rather than a stack-overflowing walk.
		again, err := Sanitize(base, doc.HTML)
		if err != nil || again.HTML != doc.HTML {
			t.Fatalf("deep sanitised output must be a fixed point: err %v, equal %v", err, again.HTML == doc.HTML)
		}
	}
}

func TestTextDeepNestingBounded(t *testing.T) {
	// Text also sees stored content written before any depth bound.
	Text(deepHTML(1 << 20))
}

func TestImageURLsDeepNestingBounded(t *testing.T) {
	input := strings.Replace(deepHTML(1<<20), "hello", `<img src="https://example.com/a.png">`, 1)
	_, _ = ImageURLs(input)
}

func TestRewriteImagesDeepNestingBounded(t *testing.T) {
	input := strings.Replace(deepHTML(1<<20), "hello", `<img src="https://example.com/a.png">`, 1)
	_, _ = RewriteImages(input, func(u *url.URL) (*url.URL, error) { return u, nil })
}
