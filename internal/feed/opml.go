package feed

import (
	"encoding/xml"
	"fmt"
)

// OPML is a subscription document. Outlines is the forest under <body>;
// folders nest through Outline.Children.
type OPML struct {
	Version  string    `json:"version"`
	Title    string    `json:"title"`
	Outlines []Outline `json:"outlines"`
}

// Outline is one node. XMLURL is set for subscriptions; Children makes the
// node a folder. Title falls back from the title attribute to text on import.
type Outline struct {
	Title    string    `json:"title"`
	XMLURL   string    `json:"xml_url,omitempty"`
	HTMLURL  string    `json:"html_url,omitempty"`
	Children []Outline `json:"children,omitempty"`
}

// Wire shapes for encoding/xml. Attribute names follow the OPML 2.0
// conventions; elements outside these shapes are ignored.
type xmlOPML struct {
	XMLName xml.Name    `xml:"opml"`
	Version string      `xml:"version,attr"`
	Head    xmlOPMLHead `xml:"head"`
	Body    xmlOPMLBody `xml:"body"`
}

type xmlOPMLHead struct {
	Title string `xml:"title"`
}

type xmlOPMLBody struct {
	Outlines []xmlOutline `xml:"outline"`
}

type xmlOutline struct {
	Text     string       `xml:"text,attr"`
	Title    string       `xml:"title,attr"`
	XMLURL   string       `xml:"xmlUrl,attr,omitempty"`
	HTMLURL  string       `xml:"htmlUrl,attr,omitempty"`
	Outlines []xmlOutline `xml:"outline"`
}

// ParseOPML reads an OPML document. A missing version is tolerated, empty
// outlines are dropped, and either text or title may carry the label. Nested
// outlines become nested folders. Like feed parsing it never fetches: the
// caller follows xmlUrl values.
func ParseOPML(data []byte) (*OPML, error) {
	if int64(len(data)) > DefaultLimits.MaxBytes {
		return nil, ErrTooLarge
	}
	if err := rejectDTD(data); err != nil {
		return nil, err
	}
	decoded, err := decodeToUTF8(data, "", 4*DefaultLimits.MaxBytes+1024)
	if err != nil {
		return nil, err
	}

	var doc xmlOPML
	if err := newXMLDecoder(decoded).Decode(&doc); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrMalformed, err)
	}

	out := &OPML{
		Version: doc.Version,
		Title:   cleanText(doc.Head.Title),
	}
	for _, o := range doc.Body.Outlines {
		if converted := convertOutline(o); !converted.empty() {
			out.Outlines = append(out.Outlines, converted)
		}
	}
	return out, nil
}

// GenerateOPML writes an OPML 2.0 document. Output is deterministic: two calls
// with equal documents produce identical bytes.
func GenerateOPML(doc *OPML) ([]byte, error) {
	if doc == nil {
		return nil, fmt.Errorf("feed: nil OPML document")
	}
	version := doc.Version
	if version == "" {
		version = "2.0"
	}
	wire := xmlOPML{
		Version: version,
		Head:    xmlOPMLHead{Title: doc.Title},
	}
	for _, o := range doc.Outlines {
		wire.Body.Outlines = append(wire.Body.Outlines, convertOutlineBack(o))
	}

	body, err := xml.MarshalIndent(wire, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("feed: marshal OPML: %w", err)
	}
	return append([]byte(xml.Header), body...), nil
}

func convertOutline(o xmlOutline) Outline {
	title := cleanText(o.Title)
	if title == "" {
		title = cleanText(o.Text)
	}
	out := Outline{
		Title:   title,
		XMLURL:  cleanText(o.XMLURL),
		HTMLURL: cleanText(o.HTMLURL),
	}
	for _, child := range o.Outlines {
		if converted := convertOutline(child); !converted.empty() {
			out.Children = append(out.Children, converted)
		}
	}
	return out
}

func (o Outline) empty() bool {
	return o.Title == "" && o.XMLURL == "" && o.HTMLURL == "" && len(o.Children) == 0
}

func convertOutlineBack(o Outline) xmlOutline {
	wire := xmlOutline{
		Text:    o.Title,
		Title:   o.Title,
		XMLURL:  o.XMLURL,
		HTMLURL: o.HTMLURL,
	}
	for _, child := range o.Children {
		wire.Outlines = append(wire.Outlines, convertOutlineBack(child))
	}
	return wire
}
