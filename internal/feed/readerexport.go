package feed

import (
	"bytes"
	"encoding/json"
	"fmt"
	"html"
	"io"
	"regexp"
	"strings"
	"time"
)

// ReaderExport is a Google Reader stream-contents document: the JSON that
// FreshRSS, Inoreader, The Old Reader and Google Takeout write when you export
// starred articles. Only the fields Earnest can use are kept.
type ReaderExport struct {
	Title string
	Items []ReaderItem
}

// ReaderItem is one exported article, the feed it came from and what the
// exporting reader knew about it.
type ReaderItem struct {
	// Item carries the article as a feed would have. Its Language is empty
	// because the format has no language field.
	Item Item
	// FeedURL is the origin feed's address: origin.feedUrl (FreshRSS), or the
	// URL inside a "feed/<url>" stream ID (Google Reader, Inoreader). It is
	// empty when the export names neither as an absolute http(s) URL.
	FeedURL   string
	FeedTitle string
	SiteURL   string
	// Read reports a com.google/read state tag for any user.
	Read bool
	// Crawled is when the exporting reader first stored the article, from
	// crawlTimeMsec or timestampUsec. It is nil when neither is usable.
	Crawled *time.Time
	// Labels are the user's labels on the article, from "user/<id>/label/<name>"
	// tags, in export order without repeats. Inoreader also lists the feed's
	// folder this way; the importer tells the two apart.
	Labels []string
}

// readStateRE matches the read state tag for any user: "user/-/..." in
// FreshRSS and Google Reader, "user/<numeric id>/..." in Inoreader.
var readStateRE = regexp.MustCompile(`^user/[^/]+/state/com\.google/read$`)

// labelRE matches a user label tag and captures its name.
var labelRE = regexp.MustCompile(`^user/[^/]+/label/(.+)$`)

// ParseReaderExport reads a Google Reader stream-contents export. Items are
// streamed so the item cap and deadline apply during the walk, as for JSON
// Feed. A document without an "items" array is ErrUnsupportedFormat; wrong
// field types degrade to empty values rather than failing the whole export.
func ParseReaderExport(data []byte, limits Limits) (*ReaderExport, error) {
	limits = limits.withDefaults()
	if int64(len(data)) > limits.MaxBytes {
		return nil, fmt.Errorf("%w (%d bytes)", ErrTooLarge, len(data))
	}
	deadline := limits.deadline()
	data = trimLeadingSpace(trimBOM(data))
	if len(data) == 0 || data[0] != '{' {
		return nil, ErrUnsupportedFormat
	}

	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	tok, err := dec.Token()
	if err != nil {
		return nil, malformedJSON(err)
	}
	if err := expectDelim(tok, '{'); err != nil {
		return nil, err
	}

	export := &ReaderExport{Items: []ReaderItem{}}
	sawItems := false
	for dec.More() {
		if !deadline.IsZero() && time.Now().After(deadline) {
			return nil, ErrParseTimeout
		}
		keyTok, err := dec.Token()
		if err != nil {
			return nil, malformedJSON(err)
		}
		key, ok := keyTok.(string)
		if !ok {
			return nil, fmt.Errorf("%w: non-string object key", ErrMalformed)
		}
		switch key {
		case "items":
			if sawItems {
				return nil, fmt.Errorf("%w: duplicate items array", ErrMalformed)
			}
			sawItems = true
			if err := readReaderItems(dec, export, limits, deadline); err != nil {
				return nil, err
			}
		case "title":
			var title any
			if err := dec.Decode(&title); err != nil {
				return nil, malformedJSON(err)
			}
			export.Title = readerText(coerceString(title))
		default:
			var skip json.RawMessage
			if err := dec.Decode(&skip); err != nil {
				return nil, malformedJSON(err)
			}
		}
	}
	if !sawItems {
		return nil, ErrUnsupportedFormat
	}
	// An export is one object. Anything after it means the file was
	// concatenated or damaged, so nothing in it is trusted.
	if tok, err := dec.Token(); err != nil {
		return nil, malformedJSON(err)
	} else if err := expectDelim(tok, '}'); err != nil {
		return nil, err
	}
	if _, err := dec.Token(); err != io.EOF {
		return nil, fmt.Errorf("%w: data after the export object", ErrMalformed)
	}
	return export, nil
}

func readReaderItems(dec *json.Decoder, export *ReaderExport, limits Limits, deadline time.Time) error {
	tok, err := dec.Token()
	if err != nil {
		return malformedJSON(err)
	}
	if err := expectDelim(tok, '['); err != nil {
		return err
	}
	for dec.More() {
		if len(export.Items) >= limits.MaxItems {
			return ErrTooManyItems
		}
		if !deadline.IsZero() && time.Now().After(deadline) {
			return ErrParseTimeout
		}
		var raw json.RawMessage
		if err := dec.Decode(&raw); err != nil {
			return malformedJSON(err)
		}
		m, err := decodeJSONObject(raw)
		if err != nil {
			return fmt.Errorf("%w: item %d: %v", ErrMalformed, len(export.Items), err)
		}
		export.Items = append(export.Items, readerItem(m))
	}
	tok, err = dec.Token()
	if err != nil {
		return malformedJSON(err)
	}
	return expectDelim(tok, ']')
}

// readerItem maps one stream item. The reader's own item ID
// ("tag:google.com,2005:reader/item/...") names the reader's copy, not the
// publisher's article, so it never becomes the GUID; FreshRSS adds the
// publisher's GUID as "guid".
func readerItem(m map[string]any) ReaderItem {
	origin, _ := m["origin"].(map[string]any)
	out := ReaderItem{
		FeedURL:   readerFeedURL(origin),
		FeedTitle: readerText(coerceString(origin["title"])),
		SiteURL:   httpURL(coerceString(origin["htmlUrl"])),
		Crawled:   readerCrawled(m),
	}

	it := Item{
		GUID:      strings.TrimSpace(coerceString(m["guid"])),
		URL:       readerLink(m),
		Title:     readerText(coerceString(m["title"])),
		Author:    readerText(coerceString(m["author"])),
		Published: unixSeconds(m["published"]),
		Updated:   unixSeconds(m["updated"]),
	}
	base := firstNonEmpty(it.URL, out.SiteURL)
	if content := coerceString(nested(m, "content", "content")); strings.TrimSpace(content) != "" {
		it.ContentHTML = resolveHTML(content, base)
	}
	if summary := coerceString(nested(m, "summary", "content")); strings.TrimSpace(summary) != "" {
		it.SummaryHTML = resolveHTML(summary, base)
	}
	for _, e := range coerceSlice(m["enclosure"]) {
		obj, ok := e.(map[string]any)
		if !ok {
			continue
		}
		// FreshRSS writes enclosure links HTML-escaped. Only &amp; is undone:
		// a general unescape would also rewrite legacy entity names such as
		// "&copy" inside an ordinary query string.
		href := strings.ReplaceAll(coerceString(obj["href"]), "&amp;", "&")
		link := httpURL(ResolveURL(base, strings.TrimSpace(href)))
		if link == "" {
			continue
		}
		it.Enclosures = append(it.Enclosures, Enclosure{URL: link, Type: cleanText(coerceString(obj["type"])), Length: max(coerceInt(numberish(obj["length"])), 0)})
	}
	seenLabels := map[string]bool{}
	for _, c := range coerceSlice(m["categories"]) {
		tag := strings.TrimSpace(coerceString(c))
		if readStateRE.MatchString(tag) {
			out.Read = true
		}
		if match := labelRE.FindStringSubmatch(tag); match != nil {
			if name := strings.TrimSpace(match[1]); name != "" && !seenLabels[name] {
				seenLabels[name] = true
				out.Labels = append(out.Labels, name)
			}
		}
	}
	out.Item = it
	return out
}

// readerFeedURL prefers FreshRSS's explicit feedUrl, then a Google Reader
// "feed/<url>" stream ID. FreshRSS stream IDs are "feed/<number>" and name
// nothing outside that server.
func readerFeedURL(origin map[string]any) string {
	if u := httpURL(coerceString(origin["feedUrl"])); u != "" {
		return u
	}
	stream := strings.TrimSpace(coerceString(origin["streamId"]))
	if rest, ok := strings.CutPrefix(stream, "feed/"); ok {
		return httpURL(rest)
	}
	return ""
}

// readerLink takes the canonical link, then an HTML alternate, then any
// alternate, keeping only absolute http(s) URLs.
func readerLink(m map[string]any) string {
	for _, c := range coerceSlice(m["canonical"]) {
		if obj, ok := c.(map[string]any); ok {
			if u := httpURL(coerceString(obj["href"])); u != "" {
				return u
			}
		}
	}
	var fallback string
	for _, a := range coerceSlice(m["alternate"]) {
		obj, ok := a.(map[string]any)
		if !ok {
			continue
		}
		u := httpURL(coerceString(obj["href"]))
		if u == "" {
			continue
		}
		kind := strings.ToLower(strings.TrimSpace(coerceString(obj["type"])))
		if kind == "" || kind == "text/html" {
			return u
		}
		if fallback == "" {
			fallback = u
		}
	}
	return fallback
}

// readerCrawled reads crawlTimeMsec, falling back to timestampUsec. Both are
// decimal strings in practice; numbers are accepted too.
func readerCrawled(m map[string]any) *time.Time {
	if ms := coerceInt(numberish(m["crawlTimeMsec"])); ms > 0 {
		return plausibleTime(time.UnixMilli(ms))
	}
	if us := coerceInt(numberish(m["timestampUsec"])); us > 0 {
		return plausibleTime(time.UnixMicro(us))
	}
	return nil
}

func unixSeconds(v any) *time.Time {
	s := coerceInt(numberish(v))
	if s <= 0 {
		return nil
	}
	return plausibleTime(time.Unix(s, 0))
}

// latestPlausibleYear bounds exported timestamps. Later values are corrupt,
// and past the year 294276 a database cannot store them at all.
const latestPlausibleYear = 9999

// plausibleTime drops a timestamp no feed could have meant, field by field
// like other malformed values.
func plausibleTime(t time.Time) *time.Time {
	t = t.UTC()
	if t.Year() > latestPlausibleYear {
		return nil
	}
	return &t
}

// readerText decodes a text field. Google Reader text is HTML, and FreshRSS
// keeps &amp;, &lt; and &gt; escaped in titles, so entities are decoded once:
// by the HTML parser when the value has markup, otherwise directly.
func readerText(s string) string {
	s = strings.TrimSpace(s)
	if looksLikeHTML(s) {
		return htmlText(s)
	}
	return collapseWS(html.UnescapeString(s))
}

// numberish lets coerceInt read a decimal string the way it reads a number.
func numberish(v any) any {
	if s, ok := v.(string); ok {
		return json.Number(strings.TrimSpace(s))
	}
	return v
}

func nested(m map[string]any, outer, inner string) any {
	obj, _ := m[outer].(map[string]any)
	return obj[inner]
}

// httpURL canonicalises an absolute http(s) URL and returns "" for anything
// else, including relative references and active schemes.
func httpURL(raw string) string {
	return CleanURL(strings.TrimSpace(raw))
}

// IdentityCandidates returns every identity hash an entry for it may already
// be stored under, IdentityHash's own choice first. An exported copy no longer
// records whether the feed marked its GUID isPermaLink="false", or whether the
// exporting reader made the GUID up, so a fetch may have stored the same
// article under its link identity instead.
func IdentityCandidates(it Item) []string {
	primary := IdentityHash(it)
	out := []string{primary}
	if strings.TrimSpace(it.GUID) == "" {
		return out
	}
	withoutGUID := it
	withoutGUID.GUID = ""
	if alt := IdentityHash(withoutGUID); alt != primary {
		out = append(out, alt)
	}
	return out
}
