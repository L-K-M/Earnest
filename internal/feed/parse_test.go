package feed

import (
	"errors"
	"os"
	"strings"
	"testing"
	"time"
)

const rss2Sample = `<?xml version="1.0" encoding="UTF-8"?>
<rss version="2.0" xmlns:content="http://purl.org/rss/1.0/modules/content/">
  <channel>
    <title>Example &amp; Co</title>
    <link>https://example.com/</link>
    <description>Examples</description>
    <language>en-us</language>
    <item>
      <title>First &lt;b&gt;post&lt;/b&gt;</title>
      <link>https://example.com/1</link>
      <guid isPermaLink="true">https://example.com/1</guid>
      <description>&lt;p&gt;Hello&lt;/p&gt;</description>
      <content:encoded><![CDATA[<p>Full <b>body</b> <img src="pic.png"></p>]]></content:encoded>
      <pubDate>Mon, 02 Jan 2006 15:04:05 GMT</pubDate>
      <category>News</category>
      <category>News</category>
      <enclosure url="https://example.com/audio.mp3" type="audio/mpeg" length="1234"/>
    </item>
  </channel>
</rss>`

func TestParseRSS2(t *testing.T) {
	t.Parallel()

	feed, err := Parse([]byte(rss2Sample), ParseOptions{BaseURL: "https://example.com/feed.xml"})
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if feed.Title != "Example & Co" {
		t.Fatalf("title = %q", feed.Title)
	}
	if feed.SiteURL != "https://example.com/" {
		t.Fatalf("site URL = %q", feed.SiteURL)
	}
	if feed.Language != "en-us" {
		t.Fatalf("language = %q", feed.Language)
	}
	if len(feed.Items) != 1 {
		t.Fatalf("items = %d, want 1", len(feed.Items))
	}

	it := feed.Items[0]
	if it.Title != "First post" {
		t.Fatalf("item title = %q, want %q", it.Title, "First post")
	}
	if it.URL != "https://example.com/1" {
		t.Fatalf("item URL = %q", it.URL)
	}
	if it.GUID != "https://example.com/1" {
		t.Fatalf("guid = %q", it.GUID)
	}
	if it.Published == nil || it.Published.Format(time.RFC3339) != "2006-01-02T15:04:05Z" {
		t.Fatalf("published = %v", it.Published)
	}
	if it.SummaryHTML != "<p>Hello</p>" {
		t.Fatalf("summary = %q", it.SummaryHTML)
	}
	if !strings.Contains(it.ContentHTML, "<p>Full <b>body</b> ") {
		t.Fatalf("content = %q", it.ContentHTML)
	}
	if !strings.Contains(it.ContentHTML, `src="https://example.com/pic.png"`) {
		t.Fatalf("relative image not resolved: %q", it.ContentHTML)
	}
	if len(it.Enclosures) != 1 || it.Enclosures[0].URL != "https://example.com/audio.mp3" || it.Enclosures[0].Length != 1234 {
		t.Fatalf("enclosures = %+v", it.Enclosures)
	}
	if len(it.Categories) != 1 || it.Categories[0] != "News" {
		t.Fatalf("categories = %v", it.Categories)
	}
	if it.Language != "en-us" {
		t.Fatalf("item language = %q", it.Language)
	}
}

const rss10Sample = `<?xml version="1.0" encoding="UTF-8"?>
<rdf:RDF xmlns:rdf="http://www.w3.org/1999/02/22-rdf-syntax-ns#"
         xmlns="http://purl.org/rss/1.0/"
         xmlns:dc="http://purl.org/dc/elements/1.1/">
  <channel rdf:about="https://example.com/">
    <title>Example RDF</title>
    <link>https://example.com/</link>
    <description>Examples</description>
    <items><rdf:Seq><rdf:li resource="https://example.com/1"/></rdf:Seq></items>
  </channel>
  <item rdf:about="https://example.com/1">
    <title>RDF item</title>
    <link>https://example.com/1</link>
    <description>RDF description</description>
    <dc:creator>Ada</dc:creator>
    <dc:date>2006-01-02T15:04:05Z</dc:date>
    <dc:subject>Tech</dc:subject>
  </item>
</rdf:RDF>`

func TestParseRSS10(t *testing.T) {
	t.Parallel()

	feed, err := Parse([]byte(rss10Sample), ParseOptions{BaseURL: "https://example.com/feed.rdf"})
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if feed.Title != "Example RDF" {
		t.Fatalf("title = %q", feed.Title)
	}
	if len(feed.Items) != 1 {
		t.Fatalf("items = %d", len(feed.Items))
	}
	it := feed.Items[0]
	if it.GUID != "https://example.com/1" {
		t.Fatalf("guid = %q", it.GUID)
	}
	if it.Author != "Ada" {
		t.Fatalf("author = %q", it.Author)
	}
	if it.Published == nil || it.Published.Format(time.RFC3339) != "2006-01-02T15:04:05Z" {
		t.Fatalf("published = %v", it.Published)
	}
	if len(it.Categories) != 1 || it.Categories[0] != "Tech" {
		t.Fatalf("categories = %v", it.Categories)
	}
}

const atom10Sample = `<?xml version="1.0" encoding="utf-8"?>
<feed xmlns="http://www.w3.org/2005/Atom" xml:lang="en">
  <title>Atom Example</title>
  <link rel="self" href="https://example.com/atom.xml"/>
  <link rel="alternate" type="text/html" href="https://example.com/"/>
  <icon>/icon.png</icon>
  <entry>
    <title>Atom entry</title>
    <id>tag:example.com,2006:1</id>
    <link rel="alternate" type="text/html" href="/posts/1"/>
    <link rel="enclosure" href="https://example.com/audio.mp3" type="audio/mpeg" length="42"/>
    <author><name>Ada</name><email>ada@example.com</email></author>
    <published>2006-01-02T15:04:05Z</published>
    <updated>2006-01-03T15:04:05+02:00</updated>
    <summary>Short</summary>
    <content type="html">&lt;p&gt;Body&lt;/p&gt;</content>
    <category term="tech"/>
  </entry>
</feed>`

func TestParseAtom10(t *testing.T) {
	t.Parallel()

	feed, err := Parse([]byte(atom10Sample), ParseOptions{BaseURL: "https://example.com/atom.xml"})
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if feed.Title != "Atom Example" {
		t.Fatalf("title = %q", feed.Title)
	}
	if feed.SiteURL != "https://example.com/" {
		t.Fatalf("site URL = %q", feed.SiteURL)
	}
	if feed.Language != "en" {
		t.Fatalf("language = %q", feed.Language)
	}
	if feed.IconURL != "https://example.com/icon.png" {
		t.Fatalf("icon = %q", feed.IconURL)
	}
	if len(feed.Items) != 1 {
		t.Fatalf("items = %d", len(feed.Items))
	}

	it := feed.Items[0]
	if it.GUID != "tag:example.com,2006:1" {
		t.Fatalf("guid = %q", it.GUID)
	}
	if it.URL != "https://example.com/posts/1" {
		t.Fatalf("url = %q", it.URL)
	}
	if it.Author != "Ada" {
		t.Fatalf("author = %q", it.Author)
	}
	if it.Published == nil || it.Published.Format(time.RFC3339) != "2006-01-02T15:04:05Z" {
		t.Fatalf("published = %v", it.Published)
	}
	if it.Updated == nil || it.Updated.Format(time.RFC3339) != "2006-01-03T13:04:05Z" {
		t.Fatalf("updated = %v", it.Updated)
	}
	if it.SummaryHTML != "Short" {
		t.Fatalf("summary = %q", it.SummaryHTML)
	}
	if it.ContentHTML != "<p>Body</p>" {
		t.Fatalf("content = %q", it.ContentHTML)
	}
	if len(it.Enclosures) != 1 || it.Enclosures[0].Length != 42 {
		t.Fatalf("enclosures = %+v", it.Enclosures)
	}
	if len(it.Categories) != 1 || it.Categories[0] != "tech" {
		t.Fatalf("categories = %v", it.Categories)
	}
}

func TestParseAtomXHTMLContent(t *testing.T) {
	t.Parallel()

	doc := `<?xml version="1.0"?>
<feed xmlns="http://www.w3.org/2005/Atom">
  <title>x</title>
  <entry>
    <title type="xhtml"><div xmlns="http://www.w3.org/1999/xhtml">A <b>bold</b> title</div></title>
    <id>urn:uuid:1</id>
    <content type="xhtml"><div xmlns="http://www.w3.org/1999/xhtml"><p>Body <a href="page.html">link</a></p></div></content>
  </entry>
</feed>`
	feed, err := Parse([]byte(doc), ParseOptions{BaseURL: "https://example.com/atom.xml"})
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	it := feed.Items[0]
	if it.Title != "A bold title" {
		t.Fatalf("title = %q", it.Title)
	}
	if !strings.Contains(it.ContentHTML, `href="https://example.com/page.html"`) {
		t.Fatalf("relative link not resolved: %q", it.ContentHTML)
	}
	if strings.Contains(it.ContentHTML, "<div") {
		t.Fatalf("wrapper div leaked into content: %q", it.ContentHTML)
	}
}

func TestParseAtomXHTMLWithoutDiv(t *testing.T) {
	t.Parallel()

	// The xhtml div wrapper is required by the spec, but feeds omit it or
	// carry plain text; the parser must not panic on either.
	doc := `<?xml version="1.0"?>
<feed xmlns="http://www.w3.org/2005/Atom">
  <title type="xhtml"/>
  <entry>
    <title type="xhtml">Text without a div</title>
    <id>urn:uuid:2</id>
    <content type="xhtml"/>
  </entry>
  <entry>
    <id>urn:uuid:3</id>
    <content type="xhtml">Text without a div</content>
  </entry>
</feed>`
	feed, err := Parse([]byte(doc), ParseOptions{BaseURL: "https://example.com/atom.xml"})
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if feed.Title != "" {
		t.Fatalf("feed title = %q", feed.Title)
	}
	if len(feed.Items) != 2 {
		t.Fatalf("items = %d", len(feed.Items))
	}
	it := feed.Items[0]
	if it.Title != "Text without a div" {
		t.Fatalf("title = %q", it.Title)
	}
	if it.ContentHTML != "" {
		t.Fatalf("content = %q", it.ContentHTML)
	}
	if got := feed.Items[1].ContentHTML; got != "Text without a div" {
		t.Fatalf("text-only content = %q", got)
	}
}

const atom03Sample = `<?xml version="1.0" encoding="utf-8"?>
<feed version="0.3" xmlns="http://purl.org/atom/ns#">
  <title>Old Atom</title>
  <tagline>Legacy</tagline>
  <entry>
    <title>Old entry</title>
    <id>tag:example.com,2006:2</id>
    <link rel="alternate" type="text/html" href="https://example.com/2"/>
    <issued>2006-01-02T15:04:05Z</issued>
    <modified>2006-01-03T15:04:05Z</modified>
    <author><name>Grace</name></author>
    <content type="text/html" mode="escaped">&lt;p&gt;Old body&lt;/p&gt;</content>
  </entry>
</feed>`

func TestParseAtom03(t *testing.T) {
	t.Parallel()

	feed, err := Parse([]byte(atom03Sample), ParseOptions{})
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if feed.Title != "Old Atom" {
		t.Fatalf("title = %q", feed.Title)
	}
	it := feed.Items[0]
	if it.Author != "Grace" {
		t.Fatalf("author = %q", it.Author)
	}
	if it.Published == nil || it.Updated == nil {
		t.Fatalf("dates missing: %+v", it)
	}
	if it.ContentHTML != "<p>Old body</p>" {
		t.Fatalf("content = %q", it.ContentHTML)
	}
}

func TestParseJSONFeed(t *testing.T) {
	t.Parallel()

	doc := `{
	  "version": "https://jsonfeed.org/version/1.1",
	  "title": "JSON Example",
	  "home_page_url": "/",
	  "feed_url": "https://example.com/feed.json",
	  "icon": "https://example.com/icon.png",
	  "favicon": "https://example.com/favicon.ico",
	  "language": "en",
	  "items": [
	    {
	      "id": 42,
	      "url": "/posts/42",
	      "title": "JSON item",
	      "content_html": "<p>Body</p>",
	      "summary": "A summary",
	      "date_published": "2026-01-02T03:04:05Z",
	      "date_modified": "2026-01-03T03:04:05Z",
	      "authors": [{"name": "Ada"}, {"name": "Grace"}],
	      "tags": ["tech", "go"],
	      "attachments": [{"url": "https://example.com/file.pdf", "mime_type": "application/pdf", "size_in_bytes": 2048}]
	    }
	  ]
	}`
	feed, err := Parse([]byte(doc), ParseOptions{BaseURL: "https://example.com/feed.json"})
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if feed.Title != "JSON Example" || feed.SiteURL != "https://example.com/" || feed.Language != "en" {
		t.Fatalf("feed = %+v", feed)
	}
	if feed.IconURL != "https://example.com/icon.png" {
		t.Fatalf("icon = %q", feed.IconURL)
	}
	it := feed.Items[0]
	if it.GUID != "42" {
		t.Fatalf("guid = %q", it.GUID)
	}
	if it.URL != "https://example.com/posts/42" {
		t.Fatalf("url = %q", it.URL)
	}
	if it.Author != "Ada, Grace" {
		t.Fatalf("author = %q", it.Author)
	}
	if it.SummaryHTML != "A summary" {
		t.Fatalf("summary = %q", it.SummaryHTML)
	}
	if len(it.Enclosures) != 1 || it.Enclosures[0].Length != 2048 {
		t.Fatalf("enclosures = %+v", it.Enclosures)
	}
	if len(it.Categories) != 2 {
		t.Fatalf("categories = %v", it.Categories)
	}
}

func TestParseRelativeURLsResolveAgainstFeedLink(t *testing.T) {
	t.Parallel()

	doc := `<rss version="2.0"><channel>
	  <title>t</title><link>https://example.com/blog/</link>
	  <item><title>a</title><link>post/1</link></item>
	</channel></rss>`
	feed, err := Parse([]byte(doc), ParseOptions{})
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if feed.Items[0].URL != "https://example.com/blog/post/1" {
		t.Fatalf("url = %q", feed.Items[0].URL)
	}
}

func TestParseXMLBaseOverrides(t *testing.T) {
	t.Parallel()

	doc := `<rss version="2.0"><channel xml:base="https://cdn.example.com/">
	  <title>t</title><link>https://example.com/</link>
	  <item><title>a</title><link>post/1</link></item>
	</channel></rss>`
	feed, err := Parse([]byte(doc), ParseOptions{})
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if feed.Items[0].URL != "https://cdn.example.com/post/1" {
		t.Fatalf("url = %q", feed.Items[0].URL)
	}
}

func TestParseMissingDatesAreAbsent(t *testing.T) {
	t.Parallel()

	doc := `<rss version="2.0"><channel><title>t</title>
	  <item><title>a</title><pubDate>yesterday maybe</pubDate></item>
	  <item><title>b</title></item>
	</channel></rss>`
	feed, err := Parse([]byte(doc), ParseOptions{})
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if feed.Items[0].Published != nil || feed.Items[0].Updated != nil {
		t.Fatalf("garbage date produced a value: %+v", feed.Items[0])
	}
	if feed.Items[1].Published != nil {
		t.Fatalf("missing date produced a value")
	}
}

func TestParseFutureDateKept(t *testing.T) {
	t.Parallel()

	doc := `<rss version="2.0"><channel><title>t</title>
	  <item><title>a</title><pubDate>Mon, 02 Jan 2035 15:04:05 GMT</pubDate></item>
	</channel></rss>`
	feed, err := Parse([]byte(doc), ParseOptions{})
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if feed.Items[0].Published == nil || feed.Items[0].Published.Year() != 2035 {
		t.Fatalf("future date dropped: %+v", feed.Items[0].Published)
	}
}

func TestParseEncodings(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		data    []byte
		charset string
		want    string
	}{
		{
			name:    "windows-1252 from header",
			data:    []byte("<rss version=\"2.0\"><channel><title>caf\xe9 \x93quoted\x94</title></channel></rss>"),
			charset: "windows-1252",
			want:    "café “quoted”",
		},
		{
			name: "iso-8859-1 declaration",
			data: []byte("<?xml version=\"1.0\" encoding=\"iso-8859-1\"?><rss version=\"2.0\"><channel><title>caf\xe9</title></channel></rss>"),
			want: "café",
		},
		{
			name: "utf-8 bom",
			data: append([]byte{0xEF, 0xBB, 0xBF}, []byte("<rss version=\"2.0\"><channel><title>café</title></channel></rss>")...),
			want: "café",
		},
		{
			name:    "unknown charset falls back to sniffing",
			data:    []byte("<rss version=\"2.0\"><channel><title>caf\xe9</title></channel></rss>"),
			charset: "bogus-charset",
			want:    "café",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			feed, err := Parse(tt.data, ParseOptions{Charset: tt.charset})
			if err != nil {
				t.Fatalf("Parse: %v", err)
			}
			if feed.Title != tt.want {
				t.Fatalf("title = %q, want %q", feed.Title, tt.want)
			}
		})
	}
}

func TestParseRejectsDangerousAndBrokenDocuments(t *testing.T) {
	t.Parallel()

	laughs := `<?xml version="1.0"?><!DOCTYPE rss [<!ENTITY lol "lol"><!ENTITY lol2 "&lol;&lol;&lol;">]><rss version="2.0"><channel><title>&lol2;</title></channel></rss>`
	if _, err := Parse([]byte(laughs), ParseOptions{}); !errors.Is(err, ErrDTD) {
		t.Fatalf("billion laughs error = %v, want ErrDTD", err)
	}

	xxe := `<?xml version="1.0"?><!DOCTYPE rss [<!ENTITY xxe SYSTEM "file:///etc/passwd">]><rss version="2.0"><channel><title>&xxe;</title></channel></rss>`
	if _, err := Parse([]byte(xxe), ParseOptions{}); !errors.Is(err, ErrDTD) {
		t.Fatalf("external entity error = %v, want ErrDTD", err)
	}

	tests := []struct {
		name string
		doc  string
		want error
	}{
		{"empty", "", ErrUnsupportedFormat},
		{"plain text", "hello world", ErrUnsupportedFormat},
		{"wrong root", "<opml></opml>", ErrUnsupportedFormat},
		{"broken xml", "<rss><channel><title>x</wrong></channel></rss>", ErrMalformed},
		{"truncated", "<rss><channel><title>x", ErrMalformed},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if _, err := Parse([]byte(tt.doc), ParseOptions{}); !errors.Is(err, tt.want) {
				t.Fatalf("error = %v, want %v", err, tt.want)
			}
		})
	}
}

func TestParseLimits(t *testing.T) {
	t.Parallel()

	items := strings.Repeat("<item><title>x</title></item>", 5)
	doc := "<rss version=\"2.0\"><channel><title>t</title>" + items + "</channel></rss>"

	if _, err := Parse([]byte(doc), ParseOptions{Limits: Limits{MaxItems: 2}}); !errors.Is(err, ErrTooManyItems) {
		t.Fatalf("item limit error = %v, want ErrTooManyItems", err)
	}
	if _, err := Parse([]byte(doc), ParseOptions{Limits: Limits{MaxBytes: 10}}); !errors.Is(err, ErrTooLarge) {
		t.Fatalf("size limit error = %v, want ErrTooLarge", err)
	}
	if _, err := Parse([]byte(doc), ParseOptions{Limits: Limits{Timeout: time.Nanosecond}}); !errors.Is(err, ErrParseTimeout) {
		t.Fatalf("timeout error = %v, want ErrParseTimeout", err)
	}
}

func TestJSONFeedMappingDeadline(t *testing.T) {
	// The single raw item is cheap to scan, but mapping its HTML is expensive.
	// No later top-level key or raw item can supply a deadline check.
	doc := `{"items":[{"id":"1","content_html":"` + strings.Repeat("<p>x</p>", 100000) + `"}]}`
	if _, err := Parse([]byte(doc), ParseOptions{Limits: Limits{Timeout: time.Millisecond}}); !errors.Is(err, ErrParseTimeout) {
		t.Fatalf("JSON item mapping error = %v, want ErrParseTimeout", err)
	}
}

func TestParseTrailingGarbageIsRejected(t *testing.T) {
	t.Parallel()

	doc := `<rss version="2.0"><channel><title>t</title></channel></rss>and then some junk`
	if _, err := Parse([]byte(doc), ParseOptions{}); !errors.Is(err, ErrMalformed) {
		t.Fatalf("error = %v, want ErrMalformed", err)
	}

	// Whitespace and comments after the root are harmless.
	benign := "<rss version=\"2.0\"><channel><title>t</title></channel></rss>\n<!-- done -->\n"
	feed, err := Parse([]byte(benign), ParseOptions{})
	if err != nil {
		t.Fatalf("Parse benign suffix: %v", err)
	}
	if feed.Title != "t" {
		t.Fatalf("title = %q", feed.Title)
	}
}

func TestParseJSONFeedRejectsUnknownVersion(t *testing.T) {
	t.Parallel()

	doc := `{"version":"https://jsonfeed.org/version/2","title":"x","items":[]}`
	if _, err := Parse([]byte(doc), ParseOptions{}); !errors.Is(err, ErrUnsupportedFormat) {
		t.Fatalf("error = %v, want ErrUnsupportedFormat", err)
	}
}

func TestParseJSONFeedToleratesMissingVersion(t *testing.T) {
	t.Parallel()

	doc := `{"title":"x","items":[{"id":"1","title":"a"}]}`
	feed, err := Parse([]byte(doc), ParseOptions{})
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(feed.Items) != 1 {
		t.Fatalf("items = %d", len(feed.Items))
	}
}

func TestParseHTTPCharsetOverridesDeclaration(t *testing.T) {
	t.Parallel()

	// The declaration lies about the bytes; the HTTP charset wins.
	doc := []byte("<?xml version=\"1.0\" encoding=\"UTF-8\"?><rss version=\"2.0\"><channel><title>caf\xe9</title></channel></rss>")
	feed, err := Parse(doc, ParseOptions{Charset: "text/html; charset=windows-1252"})
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if feed.Title != "café" {
		t.Fatalf("title = %q, want %q", feed.Title, "café")
	}
}

func TestParseAtomXHTMLEntities(t *testing.T) {
	data, err := os.ReadFile("../../testdata/feeds/atom-xhtml-entities.xml")
	if err != nil {
		t.Fatal(err)
	}
	feed, err := Parse(data, ParseOptions{BaseURL: "https://example.test/feed.xml"})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := feed.Title, "Feed & <b> text"; got != want {
		t.Errorf("feed title = %q, want %q", got, want)
	}
	it := feed.Items[0]
	if got, want := it.Title, "Title & <b> text bold"; got != want {
		t.Errorf("title = %q, want %q", got, want)
	}
	if got, want := it.SummaryHTML, "Summary &amp; &lt;b&gt; text"; got != want {
		t.Errorf("summary = %q, want %q", got, want)
	}
	if got, want := it.ContentHTML, "Content &amp; &lt;b&gt; text <b>bold</b>"; got != want {
		t.Errorf("content = %q, want %q", got, want)
	}
	if got, want := feed.Items[1].ContentHTML, "Content &amp; &lt;b&gt; text"; got != want {
		t.Errorf("text-only content = %q, want %q", got, want)
	}
}
