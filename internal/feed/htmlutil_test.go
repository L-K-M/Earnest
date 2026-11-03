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
	}
	for _, tt := range tests {
		if got := plainTitle(tt.in); got != tt.want {
			t.Errorf("plainTitle(%q) = %q, want %q", tt.in, got, tt.want)
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
