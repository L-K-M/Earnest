package feed

import (
	"errors"
	"strings"
	"testing"
)

const sampleOPML = `<?xml version="1.0" encoding="UTF-8"?>
<opml version="2.0">
  <head><title>My feeds</title></head>
  <body>
    <outline text="News">
      <outline type="rss" text="Example Times" title="Example Times" xmlUrl="https://example.com/feed.xml" htmlUrl="https://example.com/"/>
      <outline type="rss" title="Other Feed" xmlUrl="https://other.example/rss"/>
    </outline>
    <outline text="Solo" xmlUrl="https://solo.example/atom.xml" htmlUrl="https://solo.example/"/>
    <outline text="Empty folder"></outline>
  </body>
</opml>`

func TestParseOPML(t *testing.T) {
	t.Parallel()

	doc, err := ParseOPML([]byte(sampleOPML))
	if err != nil {
		t.Fatalf("ParseOPML: %v", err)
	}
	if doc.Title != "My feeds" {
		t.Fatalf("title = %q, want %q", doc.Title, "My feeds")
	}
	if len(doc.Outlines) != 3 {
		t.Fatalf("top-level outlines = %d, want 3", len(doc.Outlines))
	}

	folder := doc.Outlines[0]
	if folder.Title != "News" || folder.XMLURL != "" {
		t.Fatalf("folder = %+v", folder)
	}
	if len(folder.Children) != 2 {
		t.Fatalf("folder children = %d, want 2", len(folder.Children))
	}
	first := folder.Children[0]
	if first.Title != "Example Times" || first.XMLURL != "https://example.com/feed.xml" || first.HTMLURL != "https://example.com/" {
		t.Fatalf("first child = %+v", first)
	}
	second := folder.Children[1]
	if second.Title != "Other Feed" || second.HTMLURL != "" {
		t.Fatalf("second child = %+v", second)
	}
	if doc.Outlines[1].Title != "Solo" {
		t.Fatalf("second top-level = %+v", doc.Outlines[1])
	}
	if doc.Outlines[2].Title != "Empty folder" || doc.Outlines[2].XMLURL != "" {
		t.Fatalf("third top-level = %+v", doc.Outlines[2])
	}
}

func TestParseOPMLToleratesMissingPieces(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		doc  string
		want Outline
	}{
		{
			name: "no version and text only",
			doc:  `<opml><body><outline text="Feed" xmlUrl="https://example.com/rss"/></body></opml>`,
			want: Outline{Title: "Feed", XMLURL: "https://example.com/rss"},
		},
		{
			name: "empty body",
			doc:  `<opml version="2.0"><head><title>x</title></head><body></body></opml>`,
			want: Outline{},
		},
		{
			name: "unclosed outline",
			doc:  `<opml><body><outline text="Feed" xmlUrl="https://example.com/rss"></body></opml>`,
			want: Outline{Title: "Feed", XMLURL: "https://example.com/rss"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			doc, err := ParseOPML([]byte(tt.doc))
			if err != nil {
				t.Fatalf("ParseOPML: %v", err)
			}
			if tt.want.Title == "" && tt.want.XMLURL == "" {
				if len(doc.Outlines) != 0 {
					t.Fatalf("outlines = %+v, want none", doc.Outlines)
				}
				return
			}
			if len(doc.Outlines) != 1 || !outlinesEqual(doc.Outlines[0], tt.want) {
				t.Fatalf("outlines = %+v, want %+v", doc.Outlines, tt.want)
			}
		})
	}
}

func TestParseOPMLRejectsDangerAndDamage(t *testing.T) {
	t.Parallel()

	if _, err := ParseOPML([]byte(`<?xml version="1.0"?><!DOCTYPE opml [<!ENTITY x "y">]><opml/>`)); !errors.Is(err, ErrDTD) {
		t.Fatalf("DTD error = %v, want ErrDTD", err)
	}
	if _, err := ParseOPML([]byte(`not xml at all`)); !errors.Is(err, ErrMalformed) {
		t.Fatalf("malformed error = %v, want ErrMalformed", err)
	}
	if _, err := ParseOPML(nil); !errors.Is(err, ErrMalformed) {
		t.Fatalf("empty error = %v, want ErrMalformed", err)
	}
}

func TestGenerateOPMLRoundTrip(t *testing.T) {
	t.Parallel()

	doc := &OPML{
		Version: "2.0",
		Title:   "Feeds & more",
		Outlines: []Outline{
			{
				Title:   "News",
				XMLURL:  "",
				HTMLURL: "",
				Children: []Outline{
					{Title: "Example & Sons", XMLURL: "https://example.com/feed?a=1&b=2", HTMLURL: "https://example.com/"},
				},
			},
			{Title: "Solo", XMLURL: "https://solo.example/rss"},
		},
	}

	data, err := GenerateOPML(doc)
	if err != nil {
		t.Fatalf("GenerateOPML: %v", err)
	}
	out, err := ParseOPML(data)
	if err != nil {
		t.Fatalf("ParseOPML(GenerateOPML): %v", err)
	}
	if !outlineTreesEqual(doc, out) {
		t.Fatalf("round trip mismatch:\n got %+v\nwant %+v", out, doc)
	}
}

func TestGenerateOPMLEscapesAndIsDeterministic(t *testing.T) {
	t.Parallel()

	doc := &OPML{
		Title:    "A & B <feeds>",
		Outlines: []Outline{{Title: `He said "hi"`, XMLURL: "https://example.com/a?x=1&y=2"}},
	}
	first, err := GenerateOPML(doc)
	if err != nil {
		t.Fatalf("GenerateOPML: %v", err)
	}
	second, err := GenerateOPML(doc)
	if err != nil {
		t.Fatalf("GenerateOPML: %v", err)
	}
	if string(first) != string(second) {
		t.Fatal("export is not deterministic")
	}
	for _, want := range []string{
		`<?xml version="1.0" encoding="UTF-8"?>`,
		`<opml version="2.0">`,
		`<title>A &amp; B &lt;feeds&gt;</title>`,
		`<outline text="He said &#34;hi&#34;" title="He said &#34;hi&#34;" xmlUrl="https://example.com/a?x=1&amp;y=2"></outline>`,
	} {
		if !strings.Contains(string(first), want) {
			t.Errorf("export missing %q:\n%s", want, first)
		}
	}
}

func TestGenerateOPMLNilDocument(t *testing.T) {
	t.Parallel()

	if _, err := GenerateOPML(nil); err == nil {
		t.Fatal("expected an error for a nil document")
	}
}

func outlineTreesEqual(a, b *OPML) bool {
	if a.Version != b.Version || a.Title != b.Title || len(a.Outlines) != len(b.Outlines) {
		return false
	}
	for i := range a.Outlines {
		if !outlinesEqual(a.Outlines[i], b.Outlines[i]) {
			return false
		}
	}
	return true
}

func outlinesEqual(a, b Outline) bool {
	if a.Title != b.Title || a.XMLURL != b.XMLURL || a.HTMLURL != b.HTMLURL || len(a.Children) != len(b.Children) {
		return false
	}
	for i := range a.Children {
		if !outlinesEqual(a.Children[i], b.Children[i]) {
			return false
		}
	}
	return true
}

func TestParseOPMLCharsetHandling(t *testing.T) {
	t.Parallel()

	data := []byte("<?xml version=\"1.0\" encoding=\"iso-8859-1\"?><opml><head><title>caf\xe9</title></head><body><outline text=\"Feed\" xmlUrl=\"https://example.com/rss\"/></body></opml>")
	doc, err := ParseOPML(data)
	if err != nil {
		t.Fatalf("ParseOPML: %v", err)
	}
	if doc.Title != "café" {
		t.Fatalf("title = %q, want %q", doc.Title, "café")
	}
}

func TestParseOPMLUTF8AfterASCIIPrefix(t *testing.T) {
	t.Parallel()

	const title = "Reader’s feeds 日本語"
	data := []byte(`<?xml version="1.0" encoding="UTF-8"?><opml><head>` + strings.Repeat(" ", 2048) +
		`<title>` + title + `</title></head><body><outline text="` + title + `" xmlUrl="https://example.com/rss"/></body></opml>`)
	doc, err := ParseOPML(data)
	if err != nil {
		t.Fatal(err)
	}
	if doc.Title != title || len(doc.Outlines) != 1 || doc.Outlines[0].Title != title {
		t.Fatalf("OPML text changed: %+v", doc)
	}
}

func TestParseOPMLBOMAndUTF8(t *testing.T) {
	t.Parallel()

	data := append([]byte{0xEF, 0xBB, 0xBF}, []byte("<opml><head><title>café</title></head><body/></opml>")...)
	if _, err := ParseOPML(data); err != nil {
		t.Fatalf("ParseOPML: %v", err)
	}
}
