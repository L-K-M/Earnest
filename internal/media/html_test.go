package media_test

import (
	"net/url"
	"reflect"
	"testing"

	"github.com/L-K-M/Earnest/internal/media"
)

func parseTestURL(t *testing.T, raw string) *url.URL {
	t.Helper()
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatalf("parse %q: %v", raw, err)
	}
	return u
}

func TestThumbnailCandidates(t *testing.T) {
	tests := []struct {
		name        string
		explicit    string
		contentHTML string
		want        []string
	}{
		{
			name:     "explicit first then content images",
			explicit: "https://cdn.example.com/explicit.png",
			contentHTML: `<img src="https://example.com/one.png">` +
				`<img src="https://example.com/two.png">`,
			want: []string{
				"https://cdn.example.com/explicit.png",
				"https://example.com/one.png",
				"https://example.com/two.png",
			},
		},
		{
			name:        "invalid explicit falls through to content",
			explicit:    "data:image/png;base64,AAAA",
			contentHTML: `<img src="https://example.com/one.png">`,
			want:        []string{"https://example.com/one.png"},
		},
		{
			name:        "explicit is trimmed",
			explicit:    "  https://example.com/explicit.png  ",
			contentHTML: "",
			want:        []string{"https://example.com/explicit.png"},
		},
		{
			name:        "empty input",
			contentHTML: "",
			want:        nil,
		},
		{
			name:        "no images",
			contentHTML: `<p>no images here</p>`,
			want:        nil,
		},
		{
			name:        "srcset width descriptors",
			contentHTML: `<img srcset="https://example.com/small.png 100w, https://example.com/large.png 400w">`,
			want:        []string{"https://example.com/large.png"},
		},
		{
			name:        "srcset density descriptors",
			contentHTML: `<img srcset="https://example.com/one.png 1x, https://example.com/two.png 2x">`,
			want:        []string{"https://example.com/two.png"},
		},
		{
			name:        "srcset width beats density",
			contentHTML: `<img srcset="https://example.com/tiny.png 2x, https://example.com/wide.png 100w">`,
			want:        []string{"https://example.com/wide.png"},
		},
		{
			name:        "srcset without descriptors uses first",
			contentHTML: `<img srcset="https://example.com/first.png, https://example.com/second.png">`,
			want:        []string{"https://example.com/first.png"},
		},
		{
			name:        "empty src falls back to srcset",
			contentHTML: `<img src="" srcset="https://example.com/a.png 2x, https://example.com/b.png 1x">`,
			want:        []string{"https://example.com/a.png"},
		},
		{
			name:        "invalid src does not fall back to srcset",
			contentHTML: `<img src="data:image/png;base64,AA" srcset="https://example.com/a.png">`,
			want:        nil,
		},
		{
			name:        "width and height below minimum",
			contentHTML: `<img src="https://example.com/a.png" width="1" height="1">`,
			want:        nil,
		},
		{
			name:        "width below minimum",
			contentHTML: `<img src="https://example.com/a.png" width="48">`,
			want:        nil,
		},
		{
			name:        "height below minimum",
			contentHTML: `<img src="https://example.com/a.png" height="63">`,
			want:        nil,
		},
		{
			name:        "width at minimum",
			contentHTML: `<img src="https://example.com/a.png" width="64">`,
			want:        []string{"https://example.com/a.png"},
		},
		{
			name:        "non-numeric size ignored",
			contentHTML: `<img src="https://example.com/a.png" width="full" height="auto">`,
			want:        []string{"https://example.com/a.png"},
		},
		{
			name:        "whitespace padded size is not digits",
			contentHTML: `<img src="https://example.com/a.png" width=" 48 " height=" 48 ">`,
			want:        []string{"https://example.com/a.png"},
		},
		{
			name:        "whitespace trimmed",
			contentHTML: `<img src="  https://example.com/a.png  ">`,
			want:        []string{"https://example.com/a.png"},
		},
		{
			name:        "dedupe explicit and content",
			explicit:    "https://example.com/a.png",
			contentHTML: `<img src="https://example.com/a.png"><img src="https://example.com/a.png">`,
			want:        []string{"https://example.com/a.png"},
		},
		{
			name:        "candidate cap",
			contentHTML: `<img src="https://example.com/1.png"><img src="https://example.com/2.png"><img src="https://example.com/3.png"><img src="https://example.com/4.png"><img src="https://example.com/5.png">`,
			want: []string{
				"https://example.com/1.png",
				"https://example.com/2.png",
				"https://example.com/3.png",
			},
		},
		{
			name:        "cap includes explicit",
			explicit:    "https://example.com/explicit.png",
			contentHTML: `<img src="https://example.com/1.png"><img src="https://example.com/2.png"><img src="https://example.com/3.png">`,
			want: []string{
				"https://example.com/explicit.png",
				"https://example.com/1.png",
				"https://example.com/2.png",
			},
		},
		{
			name:        "malformed markup keeps earlier images",
			contentHTML: `<img src="https://example.com/ok.png"><p><b>bold<img src="https://example.com/next.png">`,
			want:        []string{"https://example.com/ok.png", "https://example.com/next.png"},
		},
		{
			name:        "truncated markup keeps earlier image",
			contentHTML: `<img src="https://example.com/ok.png"><p><img src="https://example.com/later.png"`,
			want:        []string{"https://example.com/ok.png"},
		},
		{
			name:        "scheme filtering",
			contentHTML: `<img src="data:image/png;base64,AA"><img src="javascript:alert(1)"><img src="/relative.png"><img src="ftp://example.com/a.png"><img src="https://example.com/ok.png">`,
			want:        []string{"https://example.com/ok.png"},
		},
		{
			name:        "userinfo and missing host",
			contentHTML: `<img src="https://user:pass@example.com/a.png"><img src="https:///b.png"><img src="https://example.com/ok.png">`,
			want:        []string{"https://example.com/ok.png"},
		},
		{
			name:        "svg filtering",
			contentHTML: `<img src="https://example.com/a.svg"><img src="https://example.com/b.svgz"><img src="https://example.com/c.SVG"><img src="https://example.com/ok.png">`,
			want:        []string{"https://example.com/ok.png"},
		},
		{
			name:        "explicit svg skipped",
			explicit:    "https://example.com/explicit.svg",
			contentHTML: `<img src="https://example.com/ok.png">`,
			want:        []string{"https://example.com/ok.png"},
		},
		{
			name:        "explicit tracker allowed",
			explicit:    "https://pixel.wp.com/tracker.gif",
			contentHTML: "",
			want:        []string{"https://pixel.wp.com/tracker.gif"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := media.ThumbnailCandidates(tt.explicit, tt.contentHTML)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("ThumbnailCandidates(%q, %q) = %#v, want %#v",
					tt.explicit, tt.contentHTML, got, tt.want)
			}
		})
	}
}

func TestThumbnailCandidatesTrackerURLs(t *testing.T) {
	urls := []string{
		"https://feeds.feedburner.com/tracker.gif",
		"https://feeds.wordpress.com/tracker.gif",
		"https://pixel.wp.com/tracker.gif",
		"https://stats.wordpress.com/tracker.gif",
		"https://www.gravatar.com/avatar/abc.jpg",
		"https://gravatar.com/avatar/abc.jpg",
		"https://secure.gravatar.com/avatar/abc.jpg",
		"https://PIXEL.WP.COM/tracker.gif",
		"https://example.com/emoji/one.png",
		"https://example.com/smilies/icon.png",
		"https://example.com/wp-includes/images/loading.gif",
	}
	for _, raw := range urls {
		t.Run(raw, func(t *testing.T) {
			got := media.ThumbnailCandidates("", `<img src="`+raw+`">`)
			if got != nil {
				t.Errorf("ThumbnailCandidates for tracker %q = %#v, want nil", raw, got)
			}
		})
	}
}

func TestIconLinks(t *testing.T) {
	pageURL := parseTestURL(t, "https://example.com/")

	tests := []struct {
		name     string
		document string
		base     *url.URL
		want     []string
	}{
		{
			name: "realistic head ranking",
			document: `<!doctype html><html><head>
<title>Example</title>
<link rel="shortcut icon" href="/favicon.ico">
<link rel="icon" type="image/png" sizes="32x32" href="/icon-32x32.png">
<link rel="icon" type="image/png" sizes="192x192" href="/icon-192x192.png">
<link rel="apple-touch-icon" sizes="180x180" href="/apple-touch-icon-180x180.png">
<link rel="mask-icon" color="#000000" href="/mask-icon.svg">
<link rel="icon" type="image/svg+xml" sizes="any" href="/icon.svg">
</head><body><link rel="icon" href="/after-body.png"></body></html>`,
			base: pageURL,
			want: []string{
				"https://example.com/icon-192x192.png",
				"https://example.com/icon-32x32.png",
				"https://example.com/apple-touch-icon-180x180.png",
				"https://example.com/favicon.ico",
			},
		},
		{
			name: "base href relative to page url",
			document: `<html><head><base href="/icons/">` +
				`<link rel="icon" sizes="32x32" href="icon-32.png">` +
				`<link rel="icon" sizes="16x16" href="icon-16.png">` +
				`</head></html>`,
			base: pageURL,
			want: []string{
				"https://example.com/icons/icon-32.png",
				"https://example.com/icons/icon-16.png",
			},
		},
		{
			name: "base href relative path",
			document: `<html><head><base href="assets/">` +
				`<link rel="icon" sizes="32x32" href="icon.png">` +
				`</head></html>`,
			base: parseTestURL(t, "https://example.com/blog/"),
			want: []string{"https://example.com/blog/assets/icon.png"},
		},
		{
			name: "absolute base href",
			document: `<html><head><base href="https://cdn.example.com/icons/">` +
				`<link rel="icon" sizes="32x32" href="icon.png">` +
				`</head></html>`,
			base: pageURL,
			want: []string{"https://cdn.example.com/icons/icon.png"},
		},
		{
			name: "links after body ignored",
			document: `<html><head><link rel="icon" sizes="32x32" href="/head.png"></head>` +
				`<body><link rel="icon" sizes="64x64" href="/body.png"></body></html>`,
			base: pageURL,
			want: []string{"https://example.com/head.png"},
		},
		{
			name: "uppercase rel sizes and href",
			document: `<html><head>` +
				`<link REL="ICON" SIZES="32X32" HREF="/Upper.png">` +
				`<link REL="APPLE-TOUCH-ICON" SIZES="180X180" HREF="/Apple.png">` +
				`</head></html>`,
			base: pageURL,
			want: []string{
				"https://example.com/Upper.png",
				"https://example.com/Apple.png",
			},
		},
		{
			name: "nil base keeps absolute only",
			document: `<html><head>` +
				`<link rel="icon" sizes="32x32" href="/relative.png">` +
				`<link rel="icon" sizes="64x64" href="https://cdn.example.com/absolute.png">` +
				`</head></html>`,
			base: nil,
			want: []string{"https://cdn.example.com/absolute.png"},
		},
		{
			name: "nil base with absolute base href",
			document: `<html><head><base href="https://cdn.example.com/">` +
				`<link rel="icon" sizes="32x32" href="icon.png">` +
				`</head></html>`,
			base: nil,
			want: []string{"https://cdn.example.com/icon.png"},
		},
		{
			name: "nil base with relative base href",
			document: `<html><head><base href="/icons/">` +
				`<link rel="icon" sizes="32x32" href="icon.png">` +
				`</head></html>`,
			base: nil,
			want: nil,
		},
		{
			name: "four result cap",
			document: `<html><head>` +
				`<link rel="icon" sizes="16x16" href="/16.png">` +
				`<link rel="icon" sizes="24x24" href="/24.png">` +
				`<link rel="icon" sizes="32x32" href="/32.png">` +
				`<link rel="icon" sizes="48x48" href="/48.png">` +
				`<link rel="icon" sizes="64x64" href="/64.png">` +
				`</head></html>`,
			base: pageURL,
			want: []string{
				"https://example.com/64.png",
				"https://example.com/48.png",
				"https://example.com/32.png",
				"https://example.com/24.png",
			},
		},
		{
			name: "duplicate hrefs deduplicated",
			document: `<html><head>` +
				`<link rel="icon" sizes="16x16" href="/same.png">` +
				`<link rel="icon" sizes="32x32" href="/same.png">` +
				`<link rel="apple-touch-icon" sizes="180x180" href="/same.png">` +
				`</head></html>`,
			base: pageURL,
			want: []string{"https://example.com/same.png"},
		},
		{
			name: "unknown size ranks before small",
			document: `<html><head>` +
				`<link rel="icon" sizes="16x16" href="/small.png">` +
				`<link rel="shortcut icon" href="/shortcut.ico">` +
				`</head></html>`,
			base: pageURL,
			want: []string{
				"https://example.com/shortcut.ico",
				"https://example.com/small.png",
			},
		},
		{
			name: "apple touch precomposed",
			document: `<html><head>` +
				`<link rel="apple-touch-icon-precomposed" sizes="180x180" href="/apple.png">` +
				`<link rel="shortcut icon" href="/favicon.ico">` +
				`</head></html>`,
			base: pageURL,
			want: []string{
				"https://example.com/apple.png",
				"https://example.com/favicon.ico",
			},
		},
		{
			name: "multiple sizes picks largest side",
			document: `<html><head>` +
				`<link rel="icon" sizes="16x16 32x32" href="/multi.png">` +
				`</head></html>`,
			base: pageURL,
			want: []string{"https://example.com/multi.png"},
		},
		{
			name: "skip unusable links",
			document: `<html><head>` +
				`<link rel="icon" href="data:image/png;base64,AA">` +
				`<link rel="icon" href="javascript:alert(1)">` +
				`<link rel="icon" href="">` +
				`<link rel="icon" href="/icon.svg">` +
				`<link rel="icon" href="/icon.svgz">` +
				`<link rel="icon" href="https://user@example.com/icon.png">` +
				`<link rel="mask-icon" href="/mask.png">` +
				`<link rel="icon" href="/good.png" sizes="32x32">` +
				`</head></html>`,
			base: pageURL,
			want: []string{"https://example.com/good.png"},
		},
		{
			name:     "self closing link",
			document: `<html><head><link rel="icon" sizes="32x32" href="/a.png" /></head></html>`,
			base:     pageURL,
			want:     []string{"https://example.com/a.png"},
		},
		{
			name:     "empty document",
			document: ``,
			base:     pageURL,
			want:     nil,
		},
		{
			name:     "malformed document",
			document: `<html><head><link rel="icon" sizes="32x32" href="/ok.png"><body`,
			base:     pageURL,
			want:     []string{"https://example.com/ok.png"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := media.IconLinks([]byte(tt.document), tt.base)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("IconLinks() = %#v, want %#v", got, tt.want)
			}
		})
	}
}
