package sanitize

import (
	"net/url"
	"strings"
)

// urlKind selects which schemes a URL attribute may use.
type urlKind int

const (
	// urlKindLink allows http, https and mailto.
	urlKindLink urlKind = iota
	// urlKindImage allows http and https only.
	urlKindImage
)

// urlWhitespace is removed from URLs before parsing. Browsers strip these
// characters, so a scheme check that did not would be bypassable with
// entities such as java&#x09;script:.
var urlWhitespace = strings.NewReplacer("\t", "", "\n", "", "\r", "")

// resolveURL normalises raw, resolves it against base when it is relative,
// and checks that the resulting scheme is allowed for kind. It returns false
// when the attribute must be dropped.
func resolveURL(base *url.URL, raw string, kind urlKind) (string, bool) {
	raw = normalizeURL(raw)
	if raw == "" {
		return "", false
	}

	ref, err := url.Parse(raw)
	if err != nil {
		return "", false
	}
	if ref.Scheme == "" {
		if base == nil || (base.Scheme != "http" && base.Scheme != "https") {
			return "", false
		}
		ref = base.ResolveReference(ref)
	}

	switch kind {
	case urlKindLink:
		if ref.Scheme != "http" && ref.Scheme != "https" && ref.Scheme != "mailto" {
			return "", false
		}
	case urlKindImage:
		if ref.Scheme != "http" && ref.Scheme != "https" {
			return "", false
		}
	default:
		return "", false
	}
	return ref.String(), true
}

// normalizeURL prepares a URL attribute the way browsers do: leading and
// trailing C0 controls and spaces are stripped, and tab, LF and CR are
// removed wherever they appear.
func normalizeURL(s string) string {
	return urlWhitespace.Replace(strings.TrimFunc(s, func(r rune) bool {
		return r <= 0x20
	}))
}

// srcsetCandidate is one URL plus its optional width or pixel-density
// descriptor.
type srcsetCandidate struct {
	url        string
	descriptor string
}

// parseSrcset splits a srcset attribute following the HTML algorithm:
// candidates are separated by commas, but a comma inside a non-empty URL is
// part of that URL. A trailing comma terminates the URL and is not part of
// it. The boolean is false when a candidate is malformed (for example more
// than one descriptor), which makes the whole attribute unusable.
func parseSrcset(raw string) (candidates []srcsetCandidate, ok bool) {
	ok = true
	i, n := 0, len(raw)
	for {
		for i < n && (isSpaceByte(raw[i]) || raw[i] == ',') {
			i++
		}
		if i >= n {
			return candidates, ok
		}

		start := i
		for i < n && !isSpaceByte(raw[i]) {
			i++
		}
		src := raw[start:i]

		// A URL ending in a comma has no descriptors; the comma separates
		// it from the next candidate.
		if strings.HasSuffix(src, ",") {
			src = strings.TrimRight(src, ",")
			if src == "" {
				return candidates, false
			}
			candidates = append(candidates, srcsetCandidate{url: src})
			continue
		}

		var descriptor string
		for {
			for i < n && isSpaceByte(raw[i]) {
				i++
			}
			if i >= n {
				break
			}
			if raw[i] == ',' {
				i++
				break
			}
			start := i
			for i < n && !isSpaceByte(raw[i]) && raw[i] != ',' {
				i++
			}
			if descriptor != "" {
				ok = false
			} else {
				descriptor = raw[start:i]
			}
		}
		candidates = append(candidates, srcsetCandidate{url: src, descriptor: descriptor})
	}
}

// cleanSrcset resolves every candidate and returns the canonical srcset, or
// false if any candidate is invalid. A single bad candidate drops the whole
// attribute rather than keeping the safe part, so a reader cannot be nudged
// into loading an unverified URL.
func cleanSrcset(base *url.URL, raw string) (string, bool) {
	candidates, ok := parseSrcset(raw)
	if !ok || len(candidates) == 0 {
		return "", false
	}

	out := make([]string, 0, len(candidates))
	for _, candidate := range candidates {
		resolved, ok := resolveURL(base, candidate.url, urlKindImage)
		if !ok {
			return "", false
		}
		if candidate.descriptor != "" {
			if !validDescriptor(candidate.descriptor) {
				return "", false
			}
			resolved += " " + candidate.descriptor
		}
		out = append(out, resolved)
	}
	return strings.Join(out, ", "), true
}

// validDescriptor reports whether s is a width ("123w") or pixel-density
// ("1.5x") descriptor.
func validDescriptor(s string) bool {
	i := 0
	for i < len(s) && isDigit(s[i]) {
		i++
	}
	if i == 0 {
		return false
	}
	if i < len(s) && s[i] == '.' {
		i++
		start := i
		for i < len(s) && isDigit(s[i]) {
			i++
		}
		if i == start {
			return false
		}
	}
	return i == len(s)-1 && (s[i] == 'w' || s[i] == 'x')
}

// srcsetURLs returns the URL of each candidate in a srcset attribute, in
// order, without validating or resolving them. It is used on HTML that
// Sanitize already checked.
func srcsetURLs(raw string) []string {
	candidates, _ := parseSrcset(raw)
	urls := make([]string, 0, len(candidates))
	for _, candidate := range candidates {
		if candidate.url != "" {
			urls = append(urls, candidate.url)
		}
	}
	return urls
}

func isSpaceByte(b byte) bool {
	return b == ' ' || b == '\t' || b == '\n' || b == '\f' || b == '\r'
}

func isDigit(b byte) bool {
	return b >= '0' && b <= '9'
}

// cleanDigits accepts an optional run of digits, as used by width, height,
// colspan, rowspan and span.
func cleanDigits(val string) (string, bool) {
	val = strings.TrimSpace(val)
	if val == "" {
		return "", false
	}
	for i := 0; i < len(val); i++ {
		if !isDigit(val[i]) {
			return "", false
		}
	}
	return val, true
}

// cleanInteger accepts an optional minus sign followed by digits, as used
// by ol start and li value. A plus sign is not valid HTML.
func cleanInteger(val string) (string, bool) {
	val = strings.TrimSpace(val)
	if val == "" {
		return "", false
	}
	if val[0] == '-' {
		val = val[1:]
	}
	if val == "" {
		return "", false
	}
	for i := 0; i < len(val); i++ {
		if !isDigit(val[i]) {
			return "", false
		}
	}
	return val, true
}

// cleanDir accepts the three HTML direction values.
func cleanDir(val string) (string, bool) {
	switch dir := strings.ToLower(strings.TrimSpace(val)); dir {
	case "ltr", "rtl", "auto":
		return dir, true
	default:
		return "", false
	}
}

// cleanLang accepts a BCP 47-shaped language tag: hyphen-separated
// alphanumeric subtags of at most eight characters. The value is inert, but
// a bounded shape keeps stored content from carrying arbitrary attribute
// payloads.
func cleanLang(val string) (string, bool) {
	val = strings.TrimSpace(val)
	if val == "" || len(val) > 35 {
		return "", false
	}
	for _, part := range strings.Split(val, "-") {
		if part == "" || len(part) > 8 {
			return "", false
		}
		for i := 0; i < len(part); i++ {
			c := part[i]
			alphanumeric := c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9'
			if !alphanumeric {
				return "", false
			}
		}
	}
	return val, true
}

// cleanScope accepts the four th scope values.
func cleanScope(val string) (string, bool) {
	switch scope := strings.ToLower(strings.TrimSpace(val)); scope {
	case "row", "col", "rowgroup", "colgroup":
		return scope, true
	default:
		return "", false
	}
}
