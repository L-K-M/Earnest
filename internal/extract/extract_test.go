package extract

import (
	"net/url"
	"strings"
	"testing"

	"github.com/L-K-M/Earnest/internal/sanitize"
)

func testURL(t *testing.T) *url.URL {
	t.Helper()
	u, err := url.Parse("https://example.com/posts/test-article")
	if err != nil {
		t.Fatal(err)
	}
	return u
}

// articlePage is a small but realistic article page: navigation and a
// footer around the content, metadata in the head, and enough body text to
// clear MinTextChars.
const articlePage = `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8">
<title>Test Article - Example Site</title>
<meta name="author" content="Jane Doe">
<meta name="description" content="A test article about testing.">
</head>
<body>
<nav>Home | About | Contact</nav>
<article>
<h1>Test Article</h1>
<p class="byline">By <a href="/authors/jane">Jane Doe</a> on <time datetime="2026-01-02">January 2, 2026</time></p>
<p>Lorem ipsum dolor sit amet, consectetur adipiscing elit. Sed do eiusmod tempor incididunt ut labore et dolore magna aliqua. Ut enim ad minim veniam, quis nostrud exercitation ullamco laboris nisi ut aliquip ex ea commodo consequat. Duis aute irure dolor in reprehenderit in voluptate velit esse cillum dolore eu fugiat nulla pariatur. Excepteur sint occaecat cupidatat non proident, sunt in culpa qui officia deserunt mollit anim id est laborum.</p>
<p>Second paragraph with more text to push the article over the threshold. Readability should find this content and ignore the navigation and footer. The quick brown fox jumps over the lazy dog. Pack my box with five dozen liquor jugs. How vexingly quick daft zebras jump.</p>
<img src="/images/test.png" alt="A test image" onerror="alert(1)">
<a href="/related">Related</a>
</article>
<footer>Copyright Example Site</footer>
</body>
</html>`

func TestExtractArticle(t *testing.T) {
	result, err := Extract(testURL(t), []byte(articlePage))
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}

	if result.Status != StatusOK {
		t.Errorf("Status = %v, want %v", result.Status, StatusOK)
	}
	if result.Title != "Test Article - Example Site" {
		t.Errorf("Title = %q", result.Title)
	}
	if result.Byline != "Jane Doe" {
		t.Errorf("Byline = %q", result.Byline)
	}
	if result.Excerpt != "A test article about testing." {
		t.Errorf("Excerpt = %q", result.Excerpt)
	}
	if result.Language != "en" {
		t.Errorf("Language = %q", result.Language)
	}

	// The stored content is already in the subset: re-sanitising it is a
	// no-op, so a client can safely pass it through Sanitize again.
	resanitised, err := sanitize.Sanitize(testURL(t), result.Document.HTML)
	if err != nil {
		t.Fatalf("re-sanitise: %v", err)
	}
	if resanitised.HTML != result.Document.HTML {
		t.Errorf("extract output is not a sanitise fixed point:\n first  %q\n second %q", result.Document.HTML, resanitised.HTML)
	}

	html := result.Document.HTML
	for _, want := range []string{
		"<p>Lorem ipsum",
		"<img src=\"https://example.com/images/test.png\" alt=\"A test image\" loading=\"lazy\" referrerpolicy=\"no-referrer\"/>",
		"<a href=\"https://example.com/related\" target=\"_blank\" rel=\"noopener noreferrer\">Related</a>",
	} {
		if !strings.Contains(html, want) {
			t.Errorf("content missing %q:\n%s", want, html)
		}
	}
	for _, unwanted := range []string{
		"<nav>", "Home | About", "<footer>", "Copyright Example Site",
		"onerror", "alert(1)", "<script", "class=",
	} {
		if strings.Contains(html, unwanted) {
			t.Errorf("content contains %q:\n%s", unwanted, html)
		}
	}
}

func TestExtractStatus(t *testing.T) {
	tests := []struct {
		name  string
		page  string
		want  Status
		check func(*testing.T, Result)
	}{
		{
			name: "empty page",
			page: "<html><head><title>Empty</title></head><body></body></html>",
			want: StatusNotArticle,
		},
		{
			name: "no text content",
			page: `<html><body><img src="https://e.example/x.png" alt="x"></body></html>`,
			want: StatusNotArticle,
		},
		{
			name: "short page",
			page: "<html><body><article><p>Too short.</p></article></body></html>",
			want: StatusTooShort,
			check: func(t *testing.T, result Result) {
				if !strings.Contains(result.Document.HTML, "Too short.") {
					t.Errorf("short content missing: %q", result.Document.HTML)
				}
			},
		},
		{
			name: "links only",
			page: `<html><body><a href="https://e.example/1">one</a><a href="https://e.example/2">two</a><a href="https://e.example/3">three</a></body></html>`,
			want: StatusTooShort,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := Extract(testURL(t), []byte(tt.page))
			if err != nil {
				t.Fatalf("Extract: %v", err)
			}
			if result.Status != tt.want {
				t.Errorf("Status = %v, want %v (HTML %q)", result.Status, tt.want, result.Document.HTML)
			}
			if tt.check != nil {
				tt.check(t, result)
			}
		})
	}
}

func TestExtractSanitises(t *testing.T) {
	page := `<html><body><article>
<h1>Title</h1>
<p>` + strings.Repeat("Long enough body text. ", 40) + `</p>
<script>alert(1)</script>
<iframe src="https://evil.example"></iframe>
<form action="javascript:alert(1)"><input type="submit"><button>go</button></form>
<p>visible text</p>
<svg><script>alert(2)</script></svg>
<style>body { display: none }</style>
<img src="javascript:alert(3)" alt="bad">
<a href="javascript:alert(4)">bad link</a>
<a href="https://e.example/good">good link</a>
</article></body></html>`

	result, err := Extract(testURL(t), []byte(page))
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if result.Status != StatusOK {
		t.Fatalf("Status = %v, want %v", result.Status, StatusOK)
	}

	html := result.Document.HTML
	for _, unwanted := range []string{
		"<script", "alert(", "<iframe", "<form", "<input", "<button",
		"<svg", "<style", "javascript:", "<img", "display: none",
	} {
		if strings.Contains(html, unwanted) {
			t.Errorf("content contains %q:\n%s", unwanted, html)
		}
	}
	for _, want := range []string{
		"visible text",
		`<a href="https://e.example/good" target="_blank" rel="noopener noreferrer">good link</a>`,
		"bad link",
	} {
		if !strings.Contains(html, want) {
			t.Errorf("content missing %q:\n%s", want, html)
		}
	}
}

func TestExtractLanguageAndEncoding(t *testing.T) {
	t.Run("language from html", func(t *testing.T) {
		page := `<html lang="fr"><head><title>Bonjour</title></head><body><article><p>` +
			strings.Repeat("Ceci est un article. ", 40) + `</p></article></body></html>`
		result, err := Extract(testURL(t), []byte(page))
		if err != nil {
			t.Fatalf("Extract: %v", err)
		}
		if result.Language != "fr" {
			t.Errorf("Language = %q, want fr", result.Language)
		}
	})

	t.Run("legacy encoding declared in meta", func(t *testing.T) {
		// "café" in windows-1252, declared as iso-8859-1.
		page := "<html><head><meta charset=\"iso-8859-1\"><title>Caf\xe9</title></head>" +
			"<body><article><p>" + strings.Repeat("Caf\xe9 au lait. ", 40) + "</p></article></body></html>"
		result, err := Extract(testURL(t), []byte(page))
		if err != nil {
			t.Fatalf("Extract: %v", err)
		}
		if !strings.Contains(result.Document.HTML, "Café") {
			t.Errorf("decoded text missing: %q", result.Document.HTML)
		}
	})

	t.Run("utf8 without declaration kept", func(t *testing.T) {
		page := "<html><head><title>日本語</title></head><body><article><p>" +
			strings.Repeat("これは記事です。", 60) + "</p></article></body></html>"
		result, err := Extract(testURL(t), []byte(page))
		if err != nil {
			t.Fatalf("Extract: %v", err)
		}
		if !strings.Contains(result.Document.HTML, "これは記事です。") {
			t.Errorf("UTF-8 text mangled: %q", result.Document.HTML)
		}
	})
}

func TestExtractWithoutBaseURL(t *testing.T) {
	page := `<html><body><article><p>` + strings.Repeat("Body text without a base. ", 30) +
		`</p><a href="/relative">link</a><img src="/image.png" alt="i"></article></body></html>`

	result, err := Extract(nil, []byte(page))
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if result.Status != StatusOK {
		t.Fatalf("Status = %v, want %v", result.Status, StatusOK)
	}
	if strings.Contains(result.Document.HTML, "href=") {
		t.Errorf("relative href survived without a base URL: %q", result.Document.HTML)
	}
	if strings.Contains(result.Document.HTML, "<img") {
		t.Errorf("relative image survived without a base URL: %q", result.Document.HTML)
	}
}

func TestStatusString(t *testing.T) {
	tests := []struct {
		status Status
		want   string
	}{
		{StatusOK, "ok"},
		{StatusTooShort, "too_short"},
		{StatusNotArticle, "not_article"},
		{Status(99), "unknown"},
	}
	for _, tt := range tests {
		if got := tt.status.String(); got != tt.want {
			t.Errorf("Status(%d).String() = %q, want %q", tt.status, got, tt.want)
		}
	}
}
