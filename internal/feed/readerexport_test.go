package feed

import (
	"errors"
	"strings"
	"testing"
	"time"
)

// freshRSSExport has the shape FreshRSS writes for "Export starred articles":
// a numeric stream ID, an explicit feedUrl and guid, HTML-escaped enclosure
// links and titles that keep &amp; escaped.
const freshRSSExport = `{
    "id": "user/admin/state/org.freshrss/starred",
    "title": "List of favourite articles",
    "author": "admin",
    "items": [
{
    "frss:id": "1763571081411160",
    "id": "tag:google.com,2005:reader/item/000643f563d7fa58",
    "crawlTimeMsec": "1763571081411",
    "timestampUsec": "1763571081411160",
    "published": 1763570786,
    "title": "Tom &amp; Jerry &lt;3",
    "canonical": [{"href": "https://example.com/2025/11/tom-and-jerry?utm_source=rss"}],
    "alternate": [{"href": "https://example.com/2025/11/tom-and-jerry", "type": "text/html"}],
    "categories": ["user/-/state/com.google/reading-list", "user/-/state/com.google/read", "user/-/state/com.google/starred", "Cartoons"],
    "origin": {"streamId": "feed/16", "htmlUrl": "https://example.com/", "title": "Example &amp; Co", "feedUrl": "https://example.com/feed/"},
    "content": {"content": "<p>Hello <img src=\"/img/a.png\"></p>"},
    "guid": "https://example.com/?p=42",
    "author": "Ann Author",
    "enclosure": [
        {"href": "https://images.example.com/a.jpg?crop=entropy&amp;w=2000&amp;q=80", "type": "image"},
        {"href": "javascript:alert(1)", "type": "image"},
        {"href": "/media/b.mp3", "type": "audio/mpeg", "length": "1234"}
    ]
}
    ]
}`

// inoreaderExport uses a "feed/<url>" stream ID, a numeric user in state
// tags, a summary instead of content and no publisher GUID.
const inoreaderExport = `{
  "direction": "ltr",
  "id": "user/1005921515/state/com.google/starred",
  "title": "Starred items",
  "updated": 1700000000,
  "items": [
    {
      "crawlTimeMsec": 1700000001000,
      "timestampUsec": "1700000001000000",
      "id": "tag:google.com,2005:reader/item/0000000a1b2c3d4e",
      "categories": ["user/1005921515/state/com.google/reading-list", "user/1005921515/state/com.google/starred", "user/1005921515/label/Tech"],
      "title": "No GUID here",
      "published": 1699990000,
      "updated": 1699995000,
      "alternate": [{"href": "https://blog.example.org/feed.xml", "type": "application/rss+xml"}, {"href": "https://blog.example.org/posts/1"}],
      "summary": {"direction": "ltr", "content": "A <b>short</b> summary"},
      "author": "",
      "origin": {"streamId": "feed/https://blog.example.org/feed.xml", "title": "Example Blog", "htmlUrl": "https://blog.example.org/"}
    }
  ]
}`

func TestParseReaderExportFreshRSS(t *testing.T) {
	export, err := ParseReaderExport([]byte(freshRSSExport), Limits{})
	if err != nil {
		t.Fatal(err)
	}
	if export.Title != "List of favourite articles" || len(export.Items) != 1 {
		t.Fatalf("export = %q with %d items", export.Title, len(export.Items))
	}
	got := export.Items[0]
	if got.FeedURL != "https://example.com/feed/" || got.FeedTitle != "Example & Co" || got.SiteURL != "https://example.com/" {
		t.Fatalf("origin = %q %q %q", got.FeedURL, got.FeedTitle, got.SiteURL)
	}
	if !got.Read {
		t.Fatal("a read state tag must mark the article read")
	}
	if want := time.UnixMilli(1763571081411).UTC(); got.Crawled == nil || !got.Crawled.Equal(want) {
		t.Fatalf("crawled = %v, want %v", got.Crawled, want)
	}
	it := got.Item
	if it.GUID != "https://example.com/?p=42" {
		t.Fatalf("guid = %q; the reader's own item ID must never become the GUID", it.GUID)
	}
	// The canonical link wins over the alternate and is cleaned like a feed link.
	if it.URL != "https://example.com/2025/11/tom-and-jerry" {
		t.Fatalf("url = %q", it.URL)
	}
	if it.Title != "Tom & Jerry <3" || it.Author != "Ann Author" {
		t.Fatalf("title/author = %q / %q", it.Title, it.Author)
	}
	if it.Published == nil || !it.Published.Equal(time.Unix(1763570786, 0)) || it.Updated != nil {
		t.Fatalf("published/updated = %v / %v", it.Published, it.Updated)
	}
	if !strings.Contains(it.ContentHTML, `src="https://example.com/img/a.png"`) || it.SummaryHTML != "" {
		t.Fatalf("content = %q, summary = %q; relative URLs resolve against the article link", it.ContentHTML, it.SummaryHTML)
	}
	want := []Enclosure{
		{URL: "https://images.example.com/a.jpg?crop=entropy&q=80&w=2000", Type: "image"},
		{URL: "https://example.com/media/b.mp3", Type: "audio/mpeg", Length: 1234},
	}
	if len(it.Enclosures) != len(want) {
		t.Fatalf("enclosures = %+v, want %+v", it.Enclosures, want)
	}
	for i := range want {
		if it.Enclosures[i] != want[i] {
			t.Fatalf("enclosure %d = %+v, want %+v", i, it.Enclosures[i], want[i])
		}
	}
}

func TestParseReaderExportInoreader(t *testing.T) {
	export, err := ParseReaderExport([]byte(inoreaderExport), Limits{})
	if err != nil {
		t.Fatal(err)
	}
	got := export.Items[0]
	if got.FeedURL != "https://blog.example.org/feed.xml" {
		t.Fatalf("feed URL = %q; a feed/<url> stream ID names the feed", got.FeedURL)
	}
	if got.Read {
		t.Fatal("an article without a read tag stays unread")
	}
	if want := time.UnixMilli(1700000001000).UTC(); got.Crawled == nil || !got.Crawled.Equal(want) {
		t.Fatalf("numeric crawlTimeMsec = %v, want %v", got.Crawled, want)
	}
	it := got.Item
	// The HTML alternate is preferred over the feed's own alternate link.
	if it.GUID != "" || it.URL != "https://blog.example.org/posts/1" {
		t.Fatalf("guid/url = %q / %q", it.GUID, it.URL)
	}
	if it.ContentHTML != "" || !strings.Contains(it.SummaryHTML, "<b>short</b>") {
		t.Fatalf("content = %q, summary = %q", it.ContentHTML, it.SummaryHTML)
	}
	if it.Updated == nil || !it.Updated.Equal(time.Unix(1699995000, 0)) {
		t.Fatalf("updated = %v", it.Updated)
	}
}

func TestParseReaderExportDegradesFieldByField(t *testing.T) {
	doc := `{"items": [
		{"title": 7, "origin": {"streamId": "feed/16"}, "categories": "user/-/state/com.google/read", "canonical": [{"href": "javascript:alert(1)"}], "alternate": "x", "published": -5, "timestampUsec": "1700000000000000"},
		{"origin": "nope", "content": "not an object", "crawlTimeMsec": "soon"}
	]}`
	export, err := ParseReaderExport([]byte(doc), Limits{})
	if err != nil {
		t.Fatal(err)
	}
	first, second := export.Items[0], export.Items[1]
	if first.FeedURL != "" {
		t.Fatalf("a FreshRSS numeric stream ID names no feed, got %q", first.FeedURL)
	}
	if first.Read || first.Item.URL != "" || first.Item.Title != "7" || first.Item.Published != nil {
		t.Fatalf("first = %+v", first)
	}
	if want := time.UnixMicro(1700000000000000).UTC(); first.Crawled == nil || !first.Crawled.Equal(want) {
		t.Fatalf("timestampUsec fallback = %v", first.Crawled)
	}
	if second.FeedURL != "" || second.Crawled != nil || second.Item.ContentHTML != "" {
		t.Fatalf("second = %+v", second)
	}
}

func TestParseReaderExportRejectsOtherDocuments(t *testing.T) {
	for _, tc := range []struct {
		name string
		doc  string
		want error
	}{
		{"empty", "", ErrUnsupportedFormat},
		{"array", `[{"items": []}]`, ErrUnsupportedFormat},
		{"no items", `{"title": "x"}`, ErrUnsupportedFormat},
		{"xml", `<rss></rss>`, ErrUnsupportedFormat},
		{"items not array", `{"items": {}}`, ErrMalformed},
		{"item not object", `{"items": [1]}`, ErrMalformed},
		{"duplicate items", `{"items": [], "items": []}`, ErrMalformed},
		{"truncated", `{"items": [{"title": "x"}`, ErrMalformed},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := ParseReaderExport([]byte(tc.doc), Limits{}); !errors.Is(err, tc.want) {
				t.Fatalf("err = %v, want %v", err, tc.want)
			}
		})
	}
	if _, err := ParseReaderExport([]byte(`{"items": [{}, {}, {}]}`), Limits{MaxItems: 2}); !errors.Is(err, ErrTooManyItems) {
		t.Fatalf("item cap: %v", err)
	}
	if _, err := ParseReaderExport([]byte(freshRSSExport), Limits{MaxBytes: 64}); !errors.Is(err, ErrTooLarge) {
		t.Fatalf("size cap: %v", err)
	}
	// An empty export is valid and imports nothing.
	export, err := ParseReaderExport([]byte("\xef\xbb\xbf "+`{"items": []}`), Limits{})
	if err != nil || len(export.Items) != 0 {
		t.Fatalf("empty export = %+v, %v", export, err)
	}
}

func TestIdentityCandidates(t *testing.T) {
	published := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	withGUID := Item{GUID: "6915a1b2c3d4e5f6a7b8c9d0", URL: "https://example.com/post", Title: "Post", Published: &published}
	got := IdentityCandidates(withGUID)
	byLink := IdentityHash(Item{URL: withGUID.URL, Title: withGUID.Title, Published: &published})
	if len(got) != 2 || got[0] != IdentityHash(withGUID) || got[1] != byLink {
		t.Fatalf("candidates = %v; want the GUID identity, then the link identity a fetch uses for isPermaLink=false", got)
	}
	noGUID := Item{URL: "https://example.com/post"}
	if got := IdentityCandidates(noGUID); len(got) != 1 || got[0] != IdentityHash(noGUID) {
		t.Fatalf("no GUID = %v", got)
	}
}
