package feed

import (
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"time"
)

// Feed is a parsed feed. SiteURL is the publisher's website, FeedURL the
// address the document was fetched from (ParseOptions.BaseURL), IconURL the
// feed image and Language the declared language. Items preserves document
// order.
type Feed struct {
	Title    string `json:"title"`
	SiteURL  string `json:"site_url"`
	FeedURL  string `json:"feed_url"`
	Language string `json:"language"`
	IconURL  string `json:"icon_url"`
	Items    []Item `json:"items"`
}

// Item is one entry. Published and Updated are nil when the source gave no
// usable date; they are never zero times. SummaryHTML and ContentHTML hold
// HTML markup (text sources are escaped into HTML). Language falls back to
// the feed language when the item does not declare one.
type Item struct {
	GUID        string      `json:"guid"`
	URL         string      `json:"url"`
	Title       string      `json:"title"`
	Author      string      `json:"author"`
	Published   *time.Time  `json:"published,omitempty"`
	Updated     *time.Time  `json:"updated,omitempty"`
	SummaryHTML string      `json:"summary_html"`
	ContentHTML string      `json:"content_html"`
	Enclosures  []Enclosure `json:"enclosures,omitempty"`
	Categories  []string    `json:"categories,omitempty"`
	Language    string      `json:"language"`

	// guidPermalink records an explicit RSS isPermaLink attribute. nil means
	// unspecified, which RSS treats as true. It is unexported because it only
	// informs IdentityHash; callers that construct Items by hand get the
	// permissive default.
	guidPermalink *bool
}

// Enclosure is a media attachment. Length is zero when the source omitted or
// mangled it.
type Enclosure struct {
	URL    string `json:"url"`
	Type   string `json:"type,omitempty"`
	Length int64  `json:"length,omitempty"`
}

// Limits bounds parsing. The zero value is replaced by DefaultLimits field
// by field, except Timeout, where a negative value disables the time bound.
type Limits struct {
	// MaxBytes is the largest accepted input. Larger documents fail with
	// ErrTooLarge rather than being truncated into a partial parse.
	MaxBytes int64
	// MaxItems bounds how many items one document may carry; a larger
	// document fails with ErrTooManyItems instead of being truncated.
	MaxItems int
	// Timeout bounds the XML and JSON walks. Zero takes DefaultLimits.
	Timeout time.Duration
}

// DefaultLimits is generous for real feeds and small enough to bound memory:
// a 20 MiB input cannot grow beyond roughly four times that after charset
// conversion, and 2000 items times a few dozen elements stays far below the
// parser's element cap.
var DefaultLimits = Limits{
	MaxBytes: 20 << 20,
	MaxItems: 2000,
	Timeout:  10 * time.Second,
}

// ParseOptions carries what the parser cannot know from the bytes alone.
type ParseOptions struct {
	// Charset is the encoding label from the HTTP Content-Type header, either
	// bare ("windows-1252") or the whole header value. It overrides the XML
	// declaration. Empty auto-detects from the BOM, the declaration and
	// content sniffing.
	Charset string
	// BaseURL is the URL the document was fetched from. Relative links
	// resolve against xml:base first, then the feed link, then this.
	BaseURL string
	// Limits bounds the parse; unset fields take DefaultLimits.
	Limits Limits
}

// Sentinel errors classify failures for feed health reporting.
var (
	ErrTooLarge          = errors.New("feed: document exceeds the size limit")
	ErrTooManyItems      = errors.New("feed: document exceeds the item limit")
	ErrParseTimeout      = errors.New("feed: parsing exceeded the time limit")
	ErrUnsupportedFormat = errors.New("feed: unsupported feed format")
	ErrMalformed         = errors.New("feed: malformed feed document")
	ErrDTD               = errors.New("feed: DTD declarations are not allowed")
)

// withDefaults fills zero fields from DefaultLimits.
func (l Limits) withDefaults() Limits {
	if l.MaxBytes <= 0 {
		l.MaxBytes = DefaultLimits.MaxBytes
	}
	if l.MaxItems <= 0 {
		l.MaxItems = DefaultLimits.MaxItems
	}
	if l.Timeout == 0 {
		l.Timeout = DefaultLimits.Timeout
	}
	return l
}

// deadline returns the absolute parse deadline, or the zero time when the
// caller disabled it.
func (l Limits) deadline() time.Time {
	if l.Timeout <= 0 {
		return time.Time{}
	}
	return time.Now().Add(l.Timeout)
}

// Parse parses one feed document. It never fetches anything.
func Parse(data []byte, opts ParseOptions) (*Feed, error) {
	limits := opts.Limits.withDefaults()
	if int64(len(data)) > limits.MaxBytes {
		return nil, fmt.Errorf("%w (%d bytes)", ErrTooLarge, len(data))
	}
	if len(data) == 0 {
		return nil, ErrUnsupportedFormat
	}
	if err := rejectDTD(data); err != nil {
		return nil, err
	}
	decoded, err := decodeToUTF8(data, opts.Charset, 4*limits.MaxBytes+1024)
	if err != nil {
		return nil, err
	}

	kind := detectFormat(decoded)
	switch kind {
	case formatRSS, formatRSS1, formatAtom:
		return parseXMLFeed(decoded, opts, limits, kind)
	case formatJSON:
		return parseJSONFeed(decoded, opts)
	default:
		return nil, ErrUnsupportedFormat
	}
}

// format identifies a detected document family. Detection is cheap and does
// not validate the whole document.
type format int

const (
	formatUnknown format = iota
	formatRSS
	formatRSS1
	formatAtom
	formatJSON
)

// detectFormat peeks at the first significant byte and, for XML, at the root
// element name. It returns formatUnknown for anything it cannot place.
func detectFormat(data []byte) format {
	data = trimLeadingSpace(trimBOM(data))
	if len(data) == 0 {
		return formatUnknown
	}
	switch data[0] {
	case '{':
		return formatJSON
	case '<':
		return detectXMLRoot(data)
	default:
		return formatUnknown
	}
}

// detectXMLRoot scans tokens until the root start element. Only encoding/xml
// can tell us whether the prolog is well formed enough to trust the name.
func detectXMLRoot(data []byte) format {
	dec := newXMLDecoder(data)
	for {
		tok, err := dec.Token()
		if err != nil {
			return formatUnknown
		}
		se, ok := tok.(xml.StartElement)
		if !ok {
			continue
		}
		switch se.Name.Local {
		case "rss":
			return formatRSS
		case "RDF":
			return formatRSS1
		case "feed":
			return formatAtom
		default:
			return formatUnknown
		}
	}
}

// trimBOM removes a leading UTF-8 BOM. Character set conversion may leave the
// BOM as U+FEFF-encoded bytes at the start of the stream.
func trimBOM(data []byte) []byte {
	for bytes.HasPrefix(data, []byte{0xEF, 0xBB, 0xBF}) {
		data = data[3:]
	}
	return data
}

// trimLeadingSpace removes ASCII whitespace before the document starts.
func trimLeadingSpace(data []byte) []byte {
	return bytes.TrimLeft(data, " \t\r\n")
}

// newXMLDecoder returns a decoder tolerant of the mismatched tags and missing
// end tags common in real-world feeds. Strict stays false so the token walk
// does not reject documents outright; structural problems still surface in
// buildTree.
func newXMLDecoder(data []byte) *xml.Decoder {
	dec := xml.NewDecoder(bytes.NewReader(data))
	dec.Strict = false
	return dec
}
