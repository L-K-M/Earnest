package feed

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// parseJSONFeed reads a JSON Feed. Items are streamed through RawMessage so
// the item cap and deadline apply during the walk rather than after a full
// decode. Content after the top-level object is ignored.
func parseJSONFeed(data []byte, opts ParseOptions) (*Feed, error) {
	limits := opts.Limits.withDefaults()
	deadline := limits.deadline()

	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()

	tok, err := dec.Token()
	if err != nil {
		return nil, malformedJSON(err)
	}
	if err := expectDelim(tok, '{'); err != nil {
		return nil, err
	}

	values := map[string]any{}
	var itemRaws []json.RawMessage
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

		if key == "items" {
			sawItems = true
			startTok, err := dec.Token()
			if err != nil {
				return nil, malformedJSON(err)
			}
			if err := expectDelim(startTok, '['); err != nil {
				return nil, err
			}
			for dec.More() {
				if len(itemRaws) >= limits.MaxItems {
					return nil, ErrTooManyItems
				}
				if !deadline.IsZero() && time.Now().After(deadline) {
					return nil, ErrParseTimeout
				}
				var raw json.RawMessage
				if err := dec.Decode(&raw); err != nil {
					return nil, malformedJSON(err)
				}
				itemRaws = append(itemRaws, raw)
			}
			endTok, err := dec.Token()
			if err != nil {
				return nil, malformedJSON(err)
			}
			if err := expectDelim(endTok, ']'); err != nil {
				return nil, err
			}
			continue
		}

		var v any
		if err := dec.Decode(&v); err != nil {
			return nil, malformedJSON(err)
		}
		values[key] = v
	}

	endTok, err := dec.Token()
	if err != nil {
		return nil, malformedJSON(err)
	}
	if err := expectDelim(endTok, '}'); err != nil {
		return nil, err
	}

	version := coerceString(values["version"])
	if version != "" && !supportedJSONFeedVersion(version) {
		return nil, ErrUnsupportedFormat
	}
	if !sawItems && version == "" {
		return nil, ErrUnsupportedFormat
	}

	feed := &Feed{
		Title:    cleanText(coerceString(values["title"])),
		FeedURL:  opts.BaseURL,
		SiteURL:  absURL(opts.BaseURL, coerceString(values["home_page_url"])),
		Language: cleanText(coerceString(values["language"])),
		IconURL:  absURL(opts.BaseURL, firstNonEmpty(coerceString(values["icon"]), coerceString(values["favicon"]))),
	}

	base := feed.SiteURL
	if base == "" {
		base = opts.BaseURL
	}

	for _, raw := range itemRaws {
		m, err := decodeJSONObject(raw)
		if err != nil {
			return nil, fmt.Errorf("%w: %v", ErrMalformed, err)
		}
		feed.Items = append(feed.Items, jsonFeedItem(m, base, feed.Language))
	}
	return feed, nil
}

// jsonFeedItem maps one JSON Feed item. Unknown keys are ignored; wrong types
// degrade to empty values rather than failing the whole import.
func jsonFeedItem(m map[string]any, base, feedLang string) Item {
	it := Item{Language: feedLang}
	if lang := cleanText(coerceString(m["language"])); lang != "" {
		it.Language = lang
	}

	it.GUID = coerceString(m["id"])
	it.URL = absURL(base, coerceString(m["url"]))
	it.Title = cleanText(coerceString(m["title"]))
	it.Author = jsonFeedAuthor(m)
	it.Published = parseDate(coerceString(m["date_published"]))
	it.Updated = parseDate(coerceString(m["date_modified"]))

	contentHTML := coerceString(m["content_html"])
	switch {
	case strings.TrimSpace(contentHTML) != "":
		it.ContentHTML = resolveHTML(contentHTML, base)
	default:
		it.ContentHTML = escapeText(coerceString(m["content_text"]))
	}

	// Summary is plain text in the specification, so it is escaped into HTML
	// rather than parsed as markup.
	it.SummaryHTML = escapeText(coerceString(m["summary"]))

	for _, a := range coerceSlice(m["attachments"]) {
		obj, ok := a.(map[string]any)
		if !ok {
			continue
		}
		url := absURL(base, coerceString(obj["url"]))
		if url == "" {
			continue
		}
		it.Enclosures = append(it.Enclosures, Enclosure{
			URL:    url,
			Type:   cleanText(coerceString(obj["mime_type"])),
			Length: coerceInt(obj["size_in_bytes"]),
		})
	}

	seen := make(map[string]bool)
	for _, t := range coerceSlice(m["tags"]) {
		tag := cleanText(coerceString(t))
		if tag == "" || seen[tag] {
			continue
		}
		seen[tag] = true
		it.Categories = append(it.Categories, tag)
	}

	return it
}

// jsonFeedAuthor prefers the 1.1 authors array, joining multiple names, and
// falls back to the 1.0 author object.
func jsonFeedAuthor(m map[string]any) string {
	var names []string
	for _, a := range coerceSlice(m["authors"]) {
		if obj, ok := a.(map[string]any); ok {
			if name := cleanText(coerceString(obj["name"])); name != "" {
				names = append(names, name)
			}
		}
	}
	if len(names) > 0 {
		return strings.Join(names, ", ")
	}
	if obj, ok := m["author"].(map[string]any); ok {
		return cleanText(coerceString(obj["name"]))
	}
	return ""
}

// supportedJSONFeedVersion accepts both published version strings and bare
// numbers, but rejects other versions rather than guessing at their shape.
func supportedJSONFeedVersion(v string) bool {
	v = strings.ToLower(strings.TrimSpace(v))
	if v == "1.0" || v == "1.1" {
		return true
	}
	return strings.Contains(v, "jsonfeed.org/version/1")
}

// expectDelim checks a JSON delimiter token.
func expectDelim(tok json.Token, want json.Delim) error {
	d, ok := tok.(json.Delim)
	if !ok || d != want {
		return fmt.Errorf("%w: unexpected token %v", ErrMalformed, tok)
	}
	return nil
}

func malformedJSON(err error) error {
	return fmt.Errorf("%w: %v", ErrMalformed, err)
}

// decodeJSONObject decodes one item with numbers preserved.
func decodeJSONObject(raw json.RawMessage) (map[string]any, error) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var m map[string]any
	if err := dec.Decode(&m); err != nil {
		return nil, err
	}
	if m == nil {
		m = map[string]any{}
	}
	return m, nil
}

// coerceString flattens the scalar types JSON can carry. Arrays and objects
// become empty strings; callers that want them use coerceSlice.
func coerceString(v any) string {
	switch t := v.(type) {
	case nil:
		return ""
	case string:
		return t
	case json.Number:
		return t.String()
	case bool:
		return strconv.FormatBool(t)
	default:
		return ""
	}
}

func coerceSlice(v any) []any {
	s, _ := v.([]any)
	return s
}

func coerceInt(v any) int64 {
	switch t := v.(type) {
	case json.Number:
		if n, err := t.Int64(); err == nil {
			return n
		}
		if f, err := t.Float64(); err == nil {
			return int64(f)
		}
	case float64:
		return int64(t)
	case int64:
		return t
	}
	return 0
}
