package sanitize

import (
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/net/html"
)

func testBase(t testing.TB) *url.URL {
	t.Helper()
	base, err := url.Parse("https://example.com/article/")
	if err != nil {
		t.Fatal(err)
	}
	return base
}

func TestSanitize(t *testing.T) {
	tests := []struct {
		name   string
		noBase bool
		input  string
		want   string
	}{
		{
			name:  "plain text",
			input: "hello world",
			want:  "hello world",
		},
		{
			name:  "allowed elements",
			input: "<p>Hello <b>world</b> and <em>friends</em></p>",
			want:  "<p>Hello <b>world</b> and <em>friends</em></p>",
		},
		{
			name:  "unknown element unwrapped",
			input: "<unknown>keep <b>this</b></unknown>",
			want:  "keep <b>this</b>",
		},
		{
			name:  "script dropped with content",
			input: "<div><script>alert(1)</script><p>x</p></div>",
			want:  "<div><p>x</p></div>",
		},
		{
			name:  "style dropped with content",
			input: "<style>body { display: none }</style><p>x</p>",
			want:  "<p>x</p>",
		},
		{
			name:  "svg dropped with content",
			input: "<svg><script>alert(1)</script><text>x</text></svg><p>y</p>",
			want:  "<p>y</p>",
		},
		{
			name:  "math dropped with content",
			input: "<math><mtext><img src=x onerror=alert(1)></mtext></math><p>y</p>",
			want:  "<p>y</p>",
		},
		{
			name:  "template dropped with content",
			input: "<template><img src=x onerror=alert(1)></template><p>y</p>",
			want:  "<p>y</p>",
		},
		{
			name:  "noscript dropped with content",
			input: "<noscript><img src=x onerror=alert(1)></noscript><p>y</p>",
			want:  "<p>y</p>",
		},
		{
			name:  "iframe object embed dropped",
			input: `<iframe src="https://e.example"></iframe><object data="x"></object><embed src="y"><p>z</p>`,
			want:  "<p>z</p>",
		},
		{
			name:  "form unwrapped controls dropped",
			input: `<form action="javascript:alert(1)"><input type="submit"><button>x</button>text</form>`,
			want:  "text",
		},
		{
			name:  "event handlers style class id stripped",
			input: `<p class="x" id="y" style="color:red" onclick="alert(1)">x</p>`,
			want:  "<p>x</p>",
		},
		{
			name:  "javascript href dropped",
			input: `<a href="javascript:alert(1)">x</a>`,
			want:  `<a target="_blank" rel="noopener noreferrer">x</a>`,
		},
		{
			name:  "mixed case javascript href dropped",
			input: `<a href="JaVaScRiPt:alert(1)">x</a>`,
			want:  `<a target="_blank" rel="noopener noreferrer">x</a>`,
		},
		{
			name:  "entity encoded javascript href dropped",
			input: `<a href="&#106;avascript:alert(1)">x</a>`,
			want:  `<a target="_blank" rel="noopener noreferrer">x</a>`,
		},
		{
			name:  "tab in javascript href dropped",
			input: "<a href=\"java\tscript:alert(1)\">x</a>",
			want:  `<a target="_blank" rel="noopener noreferrer">x</a>`,
		},
		{
			name:  "data href dropped",
			input: `<a href="data:text/html,<script>alert(1)</script>">x</a>`,
			want:  `<a target="_blank" rel="noopener noreferrer">x</a>`,
		},
		{
			name:  "tel href dropped",
			input: `<a href="tel:+123">x</a>`,
			want:  `<a target="_blank" rel="noopener noreferrer">x</a>`,
		},
		{
			name:  "mailto href kept",
			input: `<a href="mailto:x@example.com">m</a>`,
			want:  `<a href="mailto:x@example.com" target="_blank" rel="noopener noreferrer">m</a>`,
		},
		{
			name:  "relative href resolved",
			input: `<a href="/path">x</a>`,
			want:  `<a href="https://example.com/path" target="_blank" rel="noopener noreferrer">x</a>`,
		},
		{
			name:  "protocol relative href resolved",
			input: `<a href="//other.example/x">x</a>`,
			want:  `<a href="https://other.example/x" target="_blank" rel="noopener noreferrer">x</a>`,
		},
		{
			name:  "target and rel forced",
			input: `<a href="https://e.example" target="_self" rel="opener">x</a>`,
			want:  `<a href="https://e.example" target="_blank" rel="noopener noreferrer">x</a>`,
		},
		{
			name:  "img kept with forced attributes",
			input: `<img src="/i.png" alt="a" width="10" height="20">`,
			want:  `<img src="https://example.com/i.png" alt="a" width="10" height="20" loading="lazy" referrerpolicy="no-referrer"/>`,
		},
		{
			name:  "img javascript src dropped",
			input: `<img src="javascript:alert(1)" alt="a">`,
			want:  "",
		},
		{
			name:  "img data src dropped",
			input: `<img src="data:image/png;base64,AAAA">`,
			want:  "",
		},
		{
			name:  "img without src dropped",
			input: `<img alt="a">`,
			want:  "",
		},
		{
			name:  "img invalid width dropped",
			input: `<img src="/i.png" width="expression(alert(1))">`,
			want:  `<img src="https://example.com/i.png" loading="lazy" referrerpolicy="no-referrer"/>`,
		},
		{
			name:  "srcset all candidates valid",
			input: `<img src="/a.png" srcset="/a.png 1x, /b.png 2x">`,
			want:  `<img src="https://example.com/a.png" srcset="https://example.com/a.png 1x, https://example.com/b.png 2x" loading="lazy" referrerpolicy="no-referrer"/>`,
		},
		{
			name:  "srcset one bad candidate drops attribute",
			input: `<img src="/a.png" srcset="/a.png 1x, javascript:alert(1) 2x">`,
			want:  `<img src="https://example.com/a.png" loading="lazy" referrerpolicy="no-referrer"/>`,
		},
		{
			name:  "srcset data candidate drops attribute",
			input: `<img srcset="data:image/svg+xml,<svg onload=alert(1)> 1x">`,
			want:  "",
		},
		{
			name:  "srcset invalid descriptor drops attribute",
			input: `<img src="/a.png" srcset="/a.png 1x 2x">`,
			want:  `<img src="https://example.com/a.png" loading="lazy" referrerpolicy="no-referrer"/>`,
		},
		{
			name:  "srcset trailing comma",
			input: `<img srcset="/a.png 1x,">`,
			want:  `<img srcset="https://example.com/a.png 1x" loading="lazy" referrerpolicy="no-referrer"/>`,
		},
		{
			name:  "srcset double comma",
			input: `<img srcset="/a.png 1x,, /b.png 2x">`,
			want:  `<img srcset="https://example.com/a.png 1x, https://example.com/b.png 2x" loading="lazy" referrerpolicy="no-referrer"/>`,
		},
		{
			name:  "srcset with comma inside url",
			input: `<img srcset="/a,b.png 1x, /c.png 2x">`,
			want:  `<img srcset="https://example.com/a,b.png 1x, https://example.com/c.png 2x" loading="lazy" referrerpolicy="no-referrer"/>`,
		},
		{
			name:  "title and datetime escaped",
			input: `<abbr title="<script>alert(1)</script>">x</abbr><time datetime="<img src=x onerror=alert(1)>">y</time>`,
			want:  `<abbr title="&lt;script&gt;alert(1)&lt;/script&gt;">x</abbr><time datetime="&lt;img src=x onerror=alert(1)&gt;">y</time>`,
		},
		{
			name:  "table attributes kept",
			input: `<table><tr><th scope="col" colspan="2">h</th></tr><tr><td rowspan="1">c</td></tr></table>`,
			want:  `<table><tbody><tr><th colspan="2" scope="col">h</th></tr><tr><td rowspan="1">c</td></tr></tbody></table>`,
		},
		{
			name:  "list attributes kept",
			input: `<ol start="3"><li value="2">x</li></ol>`,
			want:  `<ol start="3"><li value="2">x</li></ol>`,
		},
		{
			name:  "language tag kept",
			input: `<p dir="auto" lang="en-US">x</p>`,
			want:  `<p dir="auto" lang="en-US">x</p>`,
		},
		{
			name:  "dir and lang kept",
			input: `<p dir="rtl" lang="ar">x</p>`,
			want:  `<p dir="rtl" lang="ar">x</p>`,
		},
		{
			name:  "invalid dir and lang dropped",
			input: `<p dir="bogus" lang="!!">x</p>`,
			want:  `<p>x</p>`,
		},
		{
			name:  "details and summary kept",
			input: `<details open><summary>s</summary><p>p</p></details>`,
			want:  `<details open=""><summary>s</summary><p>p</p></details>`,
		},
		{
			name:  "table structure kept",
			input: `<table><tr><td>c</td></tr></table>`,
			want:  `<table><tbody><tr><td>c</td></tr></tbody></table>`,
		},
		{
			name:  "comment dropped",
			input: `<!-- c --><p>x</p>`,
			want:  `<p>x</p>`,
		},
		{
			name:  "doctype dropped",
			input: `<!DOCTYPE html><p>x</p>`,
			want:  `<p>x</p>`,
		},
		{
			name:  "head metadata dropped",
			input: `<html><head><title>t</title><meta charset="utf-8"><link rel="stylesheet" href="x"></head><body><p>x</p></body></html>`,
			want:  `<p>x</p>`,
		},
		{
			name:  "nested p normalised",
			input: `<p>one<p>two`,
			want:  `<p>one</p><p>two</p>`,
		},
		{
			name:   "relative url without base dropped",
			noBase: true,
			input:  `<a href="/path">x</a><img src="/i.png">`,
			want:   `<a target="_blank" rel="noopener noreferrer">x</a>`,
		},
		{
			name:  "empty input",
			input: "",
			want:  "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			base := testBase(t)
			if tt.noBase {
				base = nil
			}
			doc, err := Sanitize(base, tt.input)
			if err != nil {
				t.Fatalf("Sanitize: %v", err)
			}
			if doc.HTML != tt.want {
				t.Errorf("Sanitize(%q)\n got  %q\n want %q", tt.input, doc.HTML, tt.want)
			}
			if doc.Version != Version {
				t.Errorf("Version = %d, want %d", doc.Version, Version)
			}
		})
	}
}

func TestSanitizeInvalidBase(t *testing.T) {
	base, err := url.Parse("javascript:alert(1)")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Sanitize(base, "<p>x</p>"); err == nil {
		t.Fatal("Sanitize accepted a javascript: base URL")
	}
}

func TestSanitizeIdempotent(t *testing.T) {
	base := testBase(t)
	inputs := []string{
		`<p>Hello <b>world</b></p>`,
		`<noscript><p title="</noscript><img src=x onerror=alert(1)>">`,
		`<svg><style><img src=x onerror=alert(1)></style></svg>`,
		`<math><mtext><table><mglyph><style><!--</style><img title="--><img src=1 onerror=alert(1)>">`,
		`<form><math><mtext></form><form><mglyph><style></math><img src onerror=alert(1)>`,
		`<a href="&#106;avascript:alert(1)">x</a>`,
		`<img srcset="javascript:alert(1) 1x, https://e.example/a.png 2x">`,
		`<img srcset="avascriptalert(") 1x, https://e.example/a.png 2x">`,
		`<table>foo<tr><td>bar</td></tr></table>`,
		`<template><img src=x onerror=alert(1)></template>`,
		`<p>one<p>two`,
		`<a href="https://a.example">one<a href="https://b.example">two</a></a>`,
	}
	for _, input := range inputs {
		first, err := Sanitize(base, input)
		if err != nil {
			t.Fatalf("Sanitize(%q): %v", input, err)
		}
		second, err := Sanitize(base, first.HTML)
		if err != nil {
			t.Fatalf("re-sanitize(%q): %v", first.HTML, err)
		}
		if first.HTML != second.HTML {
			t.Errorf("not idempotent for %q:\n first  %q\n second %q", input, first.HTML, second.HTML)
		}
	}
}

// activeElements are elements that must never appear in sanitised output.
// The list is independent of the package's own tables so a mistake in them
// cannot hide behind the assertion.
var activeElements = map[string]bool{
	"script": true, "style": true, "template": true, "noscript": true,
	"svg": true, "math": true,
	"iframe": true, "object": true, "embed": true, "applet": true,
	"frame": true, "frameset": true, "noframes": true, "noembed": true,
	"fencedframe": true, "portal": true,
	"base": true, "meta": true, "link": true, "title": true,
	"basefont": true, "bgsound": true,
	"form": true, "input": true, "button": true, "select": true,
	"textarea": true, "option": true, "optgroup": true, "datalist": true,
	"keygen": true, "param": true, "source": true, "track": true,
}

// urlAttributes are attributes whose values are URLs and must not carry an
// active scheme.
var urlAttributes = map[string]bool{
	"href": true, "src": true, "srcset": true, "cite": true,
	"longdesc": true, "usemap": true, "formaction": true, "action": true,
	"poster": true, "background": true, "dynsrc": true, "lowsrc": true,
	"ping": true, "manifest": true, "data": true, "codebase": true,
	"archive": true, "classid": true, "profile": true, "icon": true,
}

// forbiddenAttributes are dropped by policy even on allowed elements.
var forbiddenAttributes = map[string]bool{
	"style": true, "class": true, "id": true, "srcdoc": true,
	"formaction": true, "xlink:href": true, "action": true,
	"poster": true, "background": true, "dynsrc": true, "lowsrc": true,
	"ping": true, "manifest": true, "data": true, "codebase": true,
	"archive": true, "classid": true, "http-equiv": true, "charset": true,
}

func assertSafeHTML(t *testing.T, out string) {
	t.Helper()

	doc, err := html.Parse(strings.NewReader(out))
	if err != nil {
		t.Fatalf("output does not parse: %v\n%s", err, out)
	}

	var walk func(*html.Node)
	walk = func(n *html.Node) {
		switch n.Type {
		case html.ElementNode:
			assertSafeElement(t, n, out)
		case html.CommentNode:
			t.Errorf("comment survived: %q in %q", n.Data, out)
		}
		for child := n.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(doc)
}

func assertSafeElement(t *testing.T, n *html.Node, out string) {
	t.Helper()

	if n.Namespace != "" {
		t.Errorf("namespaced element %q (namespace %q) survived: %s", n.Data, n.Namespace, out)
	}
	if activeElements[n.Data] {
		t.Errorf("active element %q survived: %s", n.Data, out)
	}

	for _, attr := range n.Attr {
		if strings.HasPrefix(strings.ToLower(attr.Key), "on") {
			t.Errorf("event handler %q survived: %s", attr.Key, out)
		}
		if forbiddenAttributes[attr.Key] {
			t.Errorf("attribute %q survived: %s", attr.Key, out)
		}
		if urlAttributes[attr.Key] {
			checkSafeURL(t, attr.Key, attr.Val, out)
		}
	}

	if n.Data == "a" {
		if target, _ := attributeValue(n, "target"); target != "_blank" {
			t.Errorf("a target = %q, want _blank: %s", target, out)
		}
		rel, _ := attributeValue(n, "rel")
		if !strings.Contains(rel, "noopener") || !strings.Contains(rel, "noreferrer") {
			t.Errorf("a rel = %q, want noopener noreferrer: %s", rel, out)
		}
	}

	if n.Data == "img" {
		if loading, _ := attributeValue(n, "loading"); loading != "lazy" {
			t.Errorf("img loading = %q, want lazy: %s", loading, out)
		}
		if referrerpolicy, _ := attributeValue(n, "referrerpolicy"); referrerpolicy != "no-referrer" {
			t.Errorf("img referrerpolicy = %q, want no-referrer: %s", referrerpolicy, out)
		}
		src, hasSrc := attributeValue(n, "src")
		_, hasSrcset := attributeValue(n, "srcset")
		if !hasSrc && !hasSrcset {
			t.Errorf("img without a source survived: %s", out)
			return
		}
		if hasSrc {
			u, err := url.Parse(normalizeURL(src))
			if err != nil || (u.Scheme != "http" && u.Scheme != "https") {
				t.Errorf("img src %q is not http(s): %s", src, out)
			}
		}
	}
}

func checkSafeURL(t *testing.T, key, val, out string) {
	t.Helper()

	if key == "srcset" {
		for _, candidate := range srcsetURLs(val) {
			checkURLScheme(t, key, candidate, out)
		}
		return
	}
	checkURLScheme(t, key, val, out)
}

func checkURLScheme(t *testing.T, key, val, out string) {
	t.Helper()

	u, err := url.Parse(normalizeURL(val))
	if err != nil {
		t.Errorf("attribute %q is not a parseable URL %q: %s", key, val, out)
		return
	}
	switch u.Scheme {
	case "http", "https", "mailto":
	default:
		t.Errorf("attribute %q has scheme %q: %s", key, u.Scheme, out)
	}
}

// corpusDir is the repository-level payload corpus, relative to this
// package's test working directory.
const corpusDir = "../../testdata/sanitize"

func TestCorpus(t *testing.T) {
	base := testBase(t)

	files, err := filepath.Glob(corpusDir + "/*.html")
	if err != nil {
		t.Fatal(err)
	}
	if len(files) == 0 {
		t.Fatal("no corpus files found")
	}

	for _, file := range files {
		t.Run(filepath.Base(file), func(t *testing.T) {
			input, err := os.ReadFile(file)
			if err != nil {
				t.Fatal(err)
			}

			doc, err := Sanitize(base, string(input))
			if err != nil {
				t.Fatalf("Sanitize: %v", err)
			}
			assertSafeHTML(t, doc.HTML)

			again, err := Sanitize(base, doc.HTML)
			if err != nil {
				t.Fatalf("re-sanitize: %v", err)
			}
			if again.HTML != doc.HTML {
				t.Errorf("not idempotent:\n first  %q\n second %q", doc.HTML, again.HTML)
			}
			if doc.Version != Version {
				t.Errorf("Version = %d, want %d", doc.Version, Version)
			}
		})
	}
}

func FuzzSanitize(f *testing.F) {
	base := testBase(f)

	seeds := []string{
		"<p>hello world</p>",
		"<script>alert(1)</script>",
		`<noscript><p title="</noscript><img src=x onerror=alert(1)>">`,
		"<svg><style><img src=x onerror=alert(1)></style></svg>",
		`<a href="javascript:alert(1)">x</a>`,
		`<img srcset="javascript:alert(1) 1x, https://e.example/a.png 2x">`,
		`<img srcset="avascriptalert(") 1x, https://e.example/a.png 2x">`,
		"<table>foo<tr><td>bar</td></tr></table>",
		"<template><img src=x onerror=alert(1)></template>",
		"<math><mtext><table><mglyph><style><!--</style><img title=\"--><img src=1 onerror=alert(1)>\">",
	}
	for _, seed := range seeds {
		f.Add(seed)
	}
	if files, err := filepath.Glob(corpusDir + "/*.html"); err == nil {
		for _, file := range files {
			if content, err := os.ReadFile(file); err == nil {
				f.Add(string(content))
			}
		}
	}

	f.Fuzz(func(t *testing.T, input string) {
		doc, err := Sanitize(base, input)
		if err != nil {
			return // fail closed is a valid outcome
		}
		assertSafeHTML(t, doc.HTML)

		again, err := Sanitize(base, doc.HTML)
		if err != nil {
			t.Fatalf("re-sanitize(%q): %v", doc.HTML, err)
		}
		if again.HTML != doc.HTML {
			t.Errorf("not idempotent:\n first  %q\n second %q", doc.HTML, again.HTML)
		}
	})
}
