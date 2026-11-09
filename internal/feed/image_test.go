package feed

import (
	"testing"
)

// parseImageItem parses a one-item RSS document and returns that item.
func parseImageItem(t *testing.T, itemBody string) Item {
	t.Helper()
	doc := `<rss version="2.0" xmlns:media="http://search.yahoo.com/mrss/" xmlns:itunes="http://www.itunes.com/dtds/podcast-1.0.dtd">
  <channel><title>t</title><link>https://example.com/</link>
    <item><title>i</title><guid>g</guid>` + itemBody + `</item>
  </channel></rss>`
	feed, err := Parse([]byte(doc), ParseOptions{BaseURL: "https://example.com/feed.xml"})
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(feed.Items) != 1 {
		t.Fatalf("items = %d, want 1", len(feed.Items))
	}
	return feed.Items[0]
}

func TestItemImagePriority(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		body string
		want string
	}{
		{
			name: "media thumbnail beats media content, itunes and enclosure",
			body: `<media:thumbnail url="https://cdn.example.com/thumb.jpg" width="100"/>
			       <media:content url="https://cdn.example.com/content.jpg" medium="image" width="9999"/>
			       <itunes:image href="https://cdn.example.com/itunes.jpg"/>
			       <enclosure url="https://cdn.example.com/enclosure.jpg" type="image/jpeg"/>`,
			want: "https://cdn.example.com/thumb.jpg",
		},
		{
			name: "media content beats itunes and enclosure",
			body: `<media:content url="https://cdn.example.com/content.jpg" medium="image"/>
			       <itunes:image href="https://cdn.example.com/itunes.jpg"/>
			       <enclosure url="https://cdn.example.com/enclosure.jpg" type="image/jpeg"/>`,
			want: "https://cdn.example.com/content.jpg",
		},
		{
			name: "itunes beats enclosure",
			body: `<itunes:image href="https://cdn.example.com/itunes.jpg"/>
			       <enclosure url="https://cdn.example.com/enclosure.jpg" type="image/jpeg"/>`,
			want: "https://cdn.example.com/itunes.jpg",
		},
		{
			name: "enclosure is the fallback",
			body: `<enclosure url="https://cdn.example.com/enclosure.jpg" type="image/jpeg"/>`,
			want: "https://cdn.example.com/enclosure.jpg",
		},
		{
			name: "non-image enclosure is ignored",
			body: `<enclosure url="https://cdn.example.com/audio.mp3" type="audio/mpeg"/>`,
			want: "",
		},
		{
			name: "invalid media thumbnail falls through to itunes",
			body: `<media:thumbnail url="javascript:alert(1)" width="9999"/>
			       <itunes:image href="https://cdn.example.com/itunes.jpg"/>`,
			want: "https://cdn.example.com/itunes.jpg",
		},
		{
			name: "thumbnail without url is skipped",
			body: `<media:thumbnail width="9999"/>
			       <media:thumbnail url="https://cdn.example.com/next.jpg" width="1"/>`,
			want: "https://cdn.example.com/next.jpg",
		},
		{
			name: "media content without url is skipped",
			body: `<media:content medium="image" width="9999"/>
			       <media:content url="https://cdn.example.com/next.jpg" medium="image" width="1"/>`,
			want: "https://cdn.example.com/next.jpg",
		},
		{
			name: "url whitespace is trimmed",
			body: `<media:thumbnail url="  https://cdn.example.com/spaced.jpg  " width="1"/>`,
			want: "https://cdn.example.com/spaced.jpg",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := parseImageItem(t, tc.body).Image; got != tc.want {
				t.Fatalf("image = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestMediaThumbnailWidthSelection(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		body string
		want string
	}{
		{
			name: "largest wins",
			body: `<media:thumbnail url="https://cdn.example.com/a.jpg" width="120"/>
			       <media:thumbnail url="https://cdn.example.com/b.jpg" width="640"/>
			       <media:thumbnail url="https://cdn.example.com/c.jpg" width="300"/>`,
			want: "https://cdn.example.com/b.jpg",
		},
		{
			name: "tie keeps document order",
			body: `<media:thumbnail url="https://cdn.example.com/a.jpg" width="640"/>
			       <media:thumbnail url="https://cdn.example.com/b.jpg" width="640"/>`,
			want: "https://cdn.example.com/a.jpg",
		},
		{
			name: "missing width is zero",
			body: `<media:thumbnail url="https://cdn.example.com/a.jpg" width="0"/>
			       <media:thumbnail url="https://cdn.example.com/b.jpg"/>`,
			want: "https://cdn.example.com/a.jpg",
		},
		{
			name: "invalid width is zero",
			body: `<media:thumbnail url="https://cdn.example.com/a.jpg" width="wide"/>
			       <media:thumbnail url="https://cdn.example.com/b.jpg" width="10"/>`,
			want: "https://cdn.example.com/b.jpg",
		},
		{
			name: "negative width loses to missing",
			body: `<media:thumbnail url="https://cdn.example.com/a.jpg" width="-5"/>
			       <media:thumbnail url="https://cdn.example.com/b.jpg"/>`,
			want: "https://cdn.example.com/b.jpg",
		},
		{
			name: "overflowing width is invalid",
			body: `<media:thumbnail url="https://cdn.example.com/a.jpg" width="99999999999999999999"/>
			       <media:thumbnail url="https://cdn.example.com/b.jpg" width="10"/>`,
			want: "https://cdn.example.com/b.jpg",
		},
		{
			name: "group and direct thumbnails keep document order",
			body: `<media:group><media:thumbnail url="https://cdn.example.com/group.jpg" width="200"/></media:group>
			       <media:thumbnail url="https://cdn.example.com/direct.jpg" width="200"/>`,
			want: "https://cdn.example.com/group.jpg",
		},
		{
			name: "content child thumbnail wins on width",
			body: `<media:thumbnail url="https://cdn.example.com/direct.jpg" width="100"/>
			       <media:content url="https://cdn.example.com/video.mp4" type="video/mp4">
			         <media:thumbnail url="https://cdn.example.com/content.jpg" width="500"/>
			       </media:content>`,
			want: "https://cdn.example.com/content.jpg",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := parseImageItem(t, tc.body).Image; got != tc.want {
				t.Fatalf("image = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestMediaContentImageFilter(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name  string
		attrs string
		want  bool
	}{
		{"medium image", `url="https://cdn.example.com/a.jpg" medium="image"`, true},
		{"medium image beats video type", `url="https://cdn.example.com/a.mp4" medium="image" type="video/mp4"`, true},
		{"image type", `url="https://cdn.example.com/a.mp4" type="image/jpeg"`, true},
		{"image type is case insensitive", `url="https://cdn.example.com/a.mp4" type="IMAGE/PNG"`, true},
		{"extension only", `url="https://cdn.example.com/photo.jpg"`, true},
		{"extension is case insensitive", `url="https://cdn.example.com/photo.JPEG"`, true},
		{"extension with query", `url="https://cdn.example.com/photo.webp?w=1"`, true},
		{"relative extension", `url="img/photo.avif"`, true},
		{"video is not an image", `url="https://cdn.example.com/a.jpg" medium="video" type="video/mp4"`, false},
		{"declared type blocks extension", `url="https://cdn.example.com/photo.jpg" type="application/octet-stream"`, false},
		{"declared medium blocks extension", `url="https://cdn.example.com/photo.jpg" medium="audio"`, false},
		{"non-image extension", `url="https://cdn.example.com/clip.mp4"`, false},
		{"svg extension is not listed", `url="https://cdn.example.com/cover.svg"`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := parseImageItem(t, `<media:content `+tc.attrs+`/>`).Image
			if (got != "") != tc.want {
				t.Fatalf("image = %q, want present=%v", got, tc.want)
			}
		})
	}
}

func TestMediaNamespaceMatching(t *testing.T) {
	t.Parallel()
	doc := `<rss version="2.0" xmlns:media="http://search.yahoo.com/mrss/" xmlns:alt="http://search.yahoo.com/mrss" xmlns:wrong="http://example.com/not-mrss" xmlns:itunes="http://www.itunes.com/dtds/podcast-1.0.dtd">
  <channel><title>t</title><link>https://example.com/</link>
    <item><title>a</title><guid>a</guid>
      <wrong:thumbnail url="https://cdn.example.com/wrong.jpg" width="9999"/>
      <wrong:content url="https://cdn.example.com/wrong-content.jpg" medium="image" width="9999"/>
      <alt:thumbnail url="https://cdn.example.com/alt.jpg" width="100"/>
    </item>
    <item><title>b</title><guid>b</guid>
      <wrong:group><media:thumbnail url="https://cdn.example.com/wrong-group.jpg" width="9999"/></wrong:group>
      <itunes:image href="https://cdn.example.com/itunes.jpg"/>
    </item>
  </channel></rss>`
	feed, err := Parse([]byte(doc), ParseOptions{BaseURL: "https://example.com/feed.xml"})
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(feed.Items) != 2 {
		t.Fatalf("items = %d, want 2", len(feed.Items))
	}
	if got := feed.Items[0].Image; got != "https://cdn.example.com/alt.jpg" {
		t.Fatalf("first image = %q; the no-slash namespace is still Media RSS", got)
	}
	if got := feed.Items[1].Image; got != "https://cdn.example.com/itunes.jpg" {
		t.Fatalf("second image = %q; a foreign namespace must not contribute media elements", got)
	}
}

func TestAtomContentIsNotMediaContent(t *testing.T) {
	t.Parallel()
	doc := `<feed xmlns="http://www.w3.org/2005/Atom" xmlns:media="http://search.yahoo.com/mrss/">
  <title>t</title>
  <entry>
    <title>i</title><id>tag:example.com,2026:1</id>
    <content type="html">&lt;p&gt;Article body&lt;/p&gt;</content>
    <media:content url="https://cdn.example.com/cover.jpg" medium="image"/>
  </entry>
</feed>`
	feed, err := Parse([]byte(doc), ParseOptions{BaseURL: "https://example.com/atom.xml"})
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	it := feed.Items[0]
	if it.Image != "https://cdn.example.com/cover.jpg" {
		t.Fatalf("image = %q", it.Image)
	}
	if it.ContentHTML != "<p>Article body</p>" {
		t.Fatalf("Atom content was disturbed: %q", it.ContentHTML)
	}
}

func TestItemImageSkipsSVG(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ name, url string }{
		{"svg", "https://cdn.example.com/cover.svg"},
		{"svg uppercase", "https://cdn.example.com/cover.SVG"},
		{"svgz", "https://cdn.example.com/cover.svgz"},
		{"svg with query", "https://cdn.example.com/cover.svg?w=1"},
		{"svg with fragment", "https://cdn.example.com/cover.svg#x"},
		{"percent encoded", "https://cdn.example.com/cover%2Esvg"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			body := `<media:thumbnail url="` + tc.url + `" width="9999"/>
			         <itunes:image href="https://cdn.example.com/itunes.jpg"/>`
			if got := parseImageItem(t, body).Image; got != "https://cdn.example.com/itunes.jpg" {
				t.Fatalf("image = %q, want the iTunes fallback", got)
			}
		})
	}
}

func TestItemImageRelativeResolution(t *testing.T) {
	t.Parallel()
	doc := `<rss version="2.0" xmlns:media="http://search.yahoo.com/mrss/" xmlns:itunes="http://www.itunes.com/dtds/podcast-1.0.dtd">
  <channel><title>t</title><link>https://example.com/blog/</link>
    <item xml:base="https://cdn.example.com/img/"><title>a</title><guid>a</guid>
      <media:content url="photo.jpg" medium="image"/>
    </item>
    <item><title>b</title><guid>b</guid>
      <media:thumbnail url="../thumb.png"/>
    </item>
    <item><title>c</title><guid>c</guid>
      <media:content url="//cdn.example.com/proto.jpg" type="image/jpeg"/>
    </item>
    <item><title>d</title><guid>d</guid>
      <itunes:image href="cover.jpg"/>
    </item>
  </channel></rss>`
	feed, err := Parse([]byte(doc), ParseOptions{BaseURL: "https://example.com/feed.xml"})
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	want := []string{
		"https://cdn.example.com/img/photo.jpg",
		"https://example.com/thumb.png",
		"https://cdn.example.com/proto.jpg",
		"https://example.com/blog/cover.jpg",
	}
	for i, w := range want {
		if got := feed.Items[i].Image; got != w {
			t.Fatalf("item %d image = %q, want %q", i, got, w)
		}
	}
}

func TestJSONFeedItemImage(t *testing.T) {
	t.Parallel()
	doc := `{"version":"https://jsonfeed.org/version/1.1","home_page_url":"https://example.com/","items":[
	  {"id":"1","image":"one.jpg","banner_image":"banner.jpg"},
	  {"id":"2","banner_image":"/banner.png"},
	  {"id":"3","image":"javascript:alert(1)","banner_image":"https://cdn.example.com/b.jpg"},
	  {"id":"4","attachments":[{"url":"https://cdn.example.com/a.jpg","mime_type":"image/jpeg"}]},
	  {"id":"5","image":"https://cdn.example.com/i.svg","banner_image":"https://cdn.example.com/b.webp"},
	  {"id":"6","attachments":[{"url":"https://cdn.example.com/a.png","mime_type":"IMAGE/PNG"}]}
	]}`
	feed, err := Parse([]byte(doc), ParseOptions{BaseURL: "https://example.com/feed.json"})
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	want := []string{
		"https://example.com/one.jpg",
		"https://example.com/banner.png",
		"https://cdn.example.com/b.jpg",
		"https://cdn.example.com/a.jpg",
		"https://cdn.example.com/b.webp",
		"https://cdn.example.com/a.png",
	}
	if len(feed.Items) != len(want) {
		t.Fatalf("items = %d, want %d", len(feed.Items), len(want))
	}
	for i, w := range want {
		if got := feed.Items[i].Image; got != w {
			t.Fatalf("item %d image = %q, want %q", i, got, w)
		}
	}
}

func TestEnclosureImageFallbackByFormat(t *testing.T) {
	t.Parallel()
	rss := `<rss version="2.0"><channel><title>t</title><link>https://example.com/</link>
	  <item><title>a</title><guid>a</guid><enclosure url="https://cdn.example.com/a.jpg" type="image/jpeg"/></item>
	</channel></rss>`
	atom := `<feed xmlns="http://www.w3.org/2005/Atom"><title>t</title>
	  <entry><title>a</title><id>tag:example.com,1</id>
	    <link rel="enclosure" href="https://cdn.example.com/b.jpg" type="image/jpeg"/>
	  </entry>
	</feed>`
	json := `{"version":"https://jsonfeed.org/version/1.1","items":[
	  {"id":"1","attachments":[{"url":"https://cdn.example.com/c.jpg","mime_type":"image/jpeg"}]}]}`
	for _, tc := range []struct{ name, doc string }{{"rss", rss}, {"atom", atom}, {"json", json}} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			feed, err := Parse([]byte(tc.doc), ParseOptions{BaseURL: "https://example.com/feed"})
			if err != nil {
				t.Fatalf("Parse: %v", err)
			}
			if len(feed.Items) != 1 || feed.Items[0].Image == "" {
				t.Fatalf("items = %+v, want one item with an image", feed.Items)
			}
		})
	}
}

func TestImageDoesNotAffectHashes(t *testing.T) {
	t.Parallel()
	base := Item{GUID: "https://example.com/1", URL: "https://example.com/1", Title: "Title", ContentHTML: "<p>Body</p>"}
	withImage := base
	withImage.Image = "https://cdn.example.com/title.jpg"
	if IdentityHash(base) != IdentityHash(withImage) {
		t.Fatal("IdentityHash changed with Image")
	}
	if LegacyIdentityHash(base) != LegacyIdentityHash(withImage) {
		t.Fatal("LegacyIdentityHash changed with Image")
	}
	if ContentHash(base) != ContentHash(withImage) {
		t.Fatal("ContentHash changed with Image")
	}
}

// Regression: an SVG declared only by its media type, at a URL without an
// .svg extension, is skipped like one named by extension.
func TestItemImageSkipsSVGMediaType(t *testing.T) {
	t.Parallel()
	got := parseImageItem(t, `<media:content url="https://cdn.example.com/logo" type="image/svg+xml"/>
	       <enclosure url="https://cdn.example.com/badge" type="image/svg+xml; charset=utf-8"/>
	       <enclosure url="https://cdn.example.com/photo" type="image/jpeg"/>`).Image
	if got != "https://cdn.example.com/photo" {
		t.Fatalf("Image = %q, want the JPEG enclosure", got)
	}
}
