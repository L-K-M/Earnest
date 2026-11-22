package sanitize

import (
	"errors"
	"io"
	"slices"
	"strconv"
	"strings"

	"golang.org/x/net/html"
)

const escapedSpace = "%20"

// MapSrcsetURLs replaces each URL in a srcset and keeps its descriptor. A
// comma inside a URL does not separate candidates. A malformed srcset is
// returned unchanged; Sanitize drops it.
func MapSrcsetURLs(raw string, replace func(string) string) string {
	candidates, ok := parseSrcset(raw)
	if !ok || len(candidates) == 0 {
		return raw
	}

	out := make([]string, len(candidates))
	for i, candidate := range candidates {
		out[i] = replace(candidate.url)
		if candidate.descriptor != "" {
			out[i] += " " + candidate.descriptor
		}
	}

	return strings.Join(out, ", ")
}

// DropUnusableSrcsets removes a srcset no browser can choose from reliably
// when its img has a src to show instead, and leaves all other bytes of the
// sanitised HTML alone.
//
// Earnest used to split srcset at the commas inside URLs. The bodies it
// stored then cannot be reconstructed, but those lists break the rules a
// valid one follows, and their src is intact.
func DropUnusableSrcsets(input string) string {
	if !strings.Contains(input, "srcset=") {
		return input
	}

	var out strings.Builder
	z := html.NewTokenizer(strings.NewReader(input))
	for {
		kind := z.Next()
		if kind == html.ErrorToken {
			if errors.Is(z.Err(), io.EOF) {
				out.Write(z.Raw())
				return out.String()
			}
			return input
		}

		raw := string(z.Raw()) // Token may rewrite the buffer Raw points into.
		if kind == html.StartTagToken || kind == html.SelfClosingTagToken {
			token := z.Token()
			if attrs, changed := withoutUnusableSrcset(token); changed {
				token.Attr = attrs
				raw = token.String()
			}
		}
		out.WriteString(raw)
	}
}

func withoutUnusableSrcset(img html.Token) ([]html.Attribute, bool) {
	if img.Data != "img" {
		return nil, false
	}

	var src, srcset string
	for _, attr := range img.Attr {
		switch attr.Key {
		case "src":
			src = attr.Val
		case "srcset":
			srcset = attr.Val
		}
	}
	if src == "" || srcset == "" || usableSrcset(srcset) {
		return nil, false
	}

	return slices.DeleteFunc(slices.Clone(img.Attr), func(attr html.Attribute) bool {
		return attr.Key == "srcset"
	}), true
}

// usableSrcset reports whether the candidates either all declare a width or
// all differ in density, where no descriptor means 1x.
func usableSrcset(raw string) bool {
	candidates, ok := parseSrcset(raw)
	if !ok {
		return false
	}

	widths := 0
	densities := map[float64]bool{}
	for _, candidate := range candidates {
		if strings.HasSuffix(candidate.descriptor, "w") {
			widths++
			continue
		}

		density := 1.0
		if candidate.descriptor == "" {
			if len(candidates) == 1 && absorbedDescriptor(candidate.url) {
				return false
			}
		} else {
			value, err := strconv.ParseFloat(strings.TrimSuffix(candidate.descriptor, "x"), 64)
			if err != nil {
				return false
			}
			density = value
		}
		if densities[density] {
			return false
		}
		densities[density] = true
	}

	return widths == 0 || widths == len(candidates)
}

// absorbedDescriptor reports whether resolving "a.png 2x" as one URL left
// "a.png%202x". That only happened to a srcset without commas, so in a
// longer list the ending is part of the name.
func absorbedDescriptor(rawURL string) bool {
	i := strings.LastIndex(rawURL, escapedSpace)
	return i >= 0 && validDescriptor(rawURL[i+len(escapedSpace):])
}
