package feed

import (
	"strings"
	"testing"
)

func TestPlainTitle(t *testing.T) {
	t.Parallel()

	tests := []struct {
		in   string
		want string
	}{
		{"Plain", "Plain"},
		{"  spaced \n out  ", "spaced out"},
		{"<b>bold</b> title", "bold title"},
		{"AT&T and <b>tags</b>", "AT&T and tags"},
		{"5 < 6 and <3 you", "5 < 6 and <3 you"},
		{"", ""},
		// Character references survive one decoding when a publisher
		// escapes twice, as The Verge's Atom titles do. Text that only looks
		// like a reference without its semicolon stays as written.
		{"Pomera&#8217;s writing machine", "Pomera\u2019s writing machine"},
		{"Caf&eacute; &amp; bar &#x2014; open", "Caf\u00e9 & bar \u2014 open"},
		{"Q&A, M&Ms, Mom&nottoday, &copy2026", "Q&A, M&Ms, Mom&nottoday, &copy2026"},
		{"An &unknown; entity", "An &unknown; entity"},
	}
	for _, tt := range tests {
		if got := plainTitle(tt.in); got != tt.want {
			t.Errorf("plainTitle(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

// Regression: titles whose markup is only a character reference were shown
// with the reference, such as "Pomera&#8217;s". Each format's title path
// decodes it once.
func TestTitlesDecodeCharacterReferences(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, doc string
	}{
		{"atom html", `<feed xmlns="http://www.w3.org/2005/Atom"><title>f</title><entry><id>1</id><title type="html"><![CDATA[Pomera&#8217;s sleek machine]]></title></entry></feed>`},
		{"atom text", `<feed xmlns="http://www.w3.org/2005/Atom"><title>f</title><entry><id>1</id><title>Pomera&amp;#8217;s sleek machine</title></entry></feed>`},
		{"rss", `<rss version="2.0"><channel><title>f</title><item><guid>1</guid><title>Pomera&amp;#8217;s sleek machine</title></item></channel></rss>`},
		{"json feed", `{"version":"https://jsonfeed.org/version/1.1","title":"f","items":[{"id":"1","title":"Pomera&#8217;s sleek machine"}]}`},
	} {
		parsed, err := Parse([]byte(tc.doc), ParseOptions{BaseURL: "https://example.com/feed"})
		if err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		if len(parsed.Items) != 1 || parsed.Items[0].Title != "Pomera\u2019s sleek machine" {
			t.Errorf("%s: items=%+v", tc.name, parsed.Items)
		}
	}
}

func TestResolveHTML(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		in   string
		base string
		want string
	}{
		{
			name: "plain text is escaped",
			in:   "Tom & Jerry <3",
			base: "https://example.com/",
			want: "Tom &amp; Jerry &lt;3",
		},
		{
			name: "relative link resolved",
			in:   `<a href="page.html">x</a>`,
			base: "https://example.com/dir/",
			want: `<a href="https://example.com/dir/page.html">x</a>`,
		},
		{
			name: "fragment only untouched",
			in:   `<a href="#top">x</a>`,
			base: "https://example.com/",
			want: `<a href="#top">x</a>`,
		},
		{
			name: "mailto untouched",
			in:   `<a href="mailto:a@b.c">x</a>`,
			base: "https://example.com/",
			want: `<a href="mailto:a@b.c">x</a>`,
		},
		{
			name: "absolute untouched",
			in:   `<a href="https://other.example/x">x</a>`,
			base: "https://example.com/",
			want: `<a href="https://other.example/x">x</a>`,
		},
		{
			name: "javascript untouched",
			in:   `<a href="JavaScript:alert(1)">x</a>`,
			base: "https://example.com/",
			want: `<a href="JavaScript:alert(1)">x</a>`,
		},
		{
			name: "srcset resolved",
			in:   `<img srcset="a.png 1x, b.png 2x">`,
			base: "https://example.com/",
			want: `<img srcset="https://example.com/a.png 1x, https://example.com/b.png 2x"/>`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := resolveHTML(tt.in, tt.base); got != tt.want {
				t.Fatalf("resolveHTML = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestResolveHTMLBaseHref(t *testing.T) {
	t.Parallel()

	in := `<base href="https://cdn.example.com/"><img src="a.png">`
	got := resolveHTML(in, "https://example.com/")
	if !strings.Contains(got, `href="https://cdn.example.com/"`) {
		t.Fatalf("base href lost: %q", got)
	}
	if !strings.Contains(got, `src="https://cdn.example.com/a.png"`) {
		t.Fatalf("image not resolved against base href: %q", got)
	}
}

func TestResolveHTMLOversizedFieldStaysRaw(t *testing.T) {
	t.Parallel()

	in := strings.Repeat("<p>x</p>", maxHTMLFieldBytes)
	in = in[:maxHTMLFieldBytes+1]
	if got := resolveHTML(in, "https://example.com/"); got != in {
		t.Fatal("oversized HTML field was rewritten or escaped")
	}
}

func TestResolveHTMLDeepNestingDoesNotCrash(t *testing.T) {
	t.Parallel()

	in := strings.Repeat("<div>", 2000) + "x" + strings.Repeat("</div>", 2000)
	got := resolveHTML(in, "https://example.com/")
	if got != in {
		t.Fatal("deeply nested field was rewritten instead of left alone")
	}
}

// Items without a GUID or link are identified by title. Decoding must not
// change that identity, or the next fetch would store such an item twice.
func TestTitleDecodingKeepsIdentity(t *testing.T) {
	t.Parallel()
	doc := `<rss version="2.0"><channel><title>f</title><item><title>Pomera&amp;#8217;s</title><pubDate>Sun, 08 Nov 2026 10:00:00 GMT</pubDate></item></channel></rss>`
	parsed, err := Parse([]byte(doc), ParseOptions{BaseURL: "https://example.com/feed"})
	if err != nil || len(parsed.Items) != 1 {
		t.Fatalf("parse: %v %+v", err, parsed)
	}
	it := parsed.Items[0]
	if it.Title != "Pomera\u2019s" {
		t.Fatalf("title %q", it.Title)
	}
	want := hashHex("title:Pomera&#8217;s\npublished:2026-11-08T10:00:00Z")
	if got := IdentityHash(it); got != want {
		t.Fatalf("identity %s, want the pre-decoding %s", got, want)
	}
}
