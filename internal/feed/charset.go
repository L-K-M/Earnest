package feed

import (
	"bytes"
	"fmt"
	"io"
	"regexp"
	"strings"

	"golang.org/x/net/html/charset"
)

// decodeToUTF8 converts data to UTF-8. An explicit HTTP charset wins over the
// XML declaration; without one the WHATWG sniffing rules in x/net/html/charset
// inspect the BOM, the declaration and finally fall back to windows-1252,
// which is what browsers do for mislabelled documents. The output is capped
// because some legacy encodings expand up to four bytes per input byte.
func decodeToUTF8(data []byte, httpCharset string, maxDecoded int64) ([]byte, error) {
	r, err := utf8Reader(data, httpCharset)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrMalformed, err)
	}
	out, err := io.ReadAll(io.LimitReader(r, maxDecoded+1))
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrMalformed, err)
	}
	if int64(len(out)) > maxDecoded {
		return nil, ErrTooLarge
	}
	out = trimBOM(out)
	return neutraliseXMLDeclaration(out), nil
}

// utf8Reader picks the explicit charset first and falls back to sniffing when
// the label is unknown, so a bogus header cannot make the parse fail.
func utf8Reader(data []byte, httpCharset string) (io.Reader, error) {
	label := charsetLabel(httpCharset)
	if label == "" {
		return charset.NewReader(bytes.NewReader(data), "")
	}
	r, err := charset.NewReaderLabel(label, bytes.NewReader(data))
	if err == nil {
		return r, nil
	}
	return charset.NewReader(bytes.NewReader(data), "")
}

// charsetLabel extracts the encoding label from a Content-Type charset
// parameter or bare label, dropping any parameters.
func charsetLabel(contentType string) string {
	if contentType == "" {
		return ""
	}
	label := strings.TrimSpace(contentType)
	lower := strings.ToLower(label)
	if i := strings.Index(lower, "charset="); i >= 0 {
		label = strings.TrimSpace(label[i+len("charset="):])
	}
	if i := strings.IndexByte(label, ';'); i >= 0 {
		label = strings.TrimSpace(label[:i])
	}
	if len(label) >= 2 && ((label[0] == '"' && label[len(label)-1] == '"') || (label[0] == '\'' && label[len(label)-1] == '\'')) {
		label = label[1 : len(label)-1]
	}
	return label
}

// xmlEncodingAttr matches the encoding pseudo-attribute in an XML declaration
// so the already-converted bytes can be declared as UTF-8. Without this the
// decoder would hand the declaration to CharsetReader and try to re-decode
// UTF-8 bytes as windows-1252.
var xmlEncodingAttr = regexp.MustCompile(`(?i)(<\?xml[^>]*?)\s+encoding\s*=\s*(?:"[^"]*"|'[^']*')`)

// neutraliseXMLDeclaration rewrites the declaration's encoding to UTF-8.
func neutraliseXMLDeclaration(data []byte) []byte {
	loc := xmlEncodingAttr.FindSubmatchIndex(data)
	if loc == nil {
		return data
	}
	out := make([]byte, 0, len(data))
	out = append(out, data[:loc[3]]...)
	out = append(out, ` encoding="utf-8"`...)
	out = append(out, data[loc[1]:]...)
	return out
}

// rejectDTD refuses documents containing a DTD or entity declarations.
// encoding/xml never expands custom entities, but rejecting the constructs
// outright removes any dependence on that behaviour and keeps external entity
// references from being interpreted by future readers of the code.
func rejectDTD(data []byte) error {
	if containsFold(data, "<!DOCTYPE") || containsFold(data, "<!ENTITY") {
		return ErrDTD
	}
	return nil
}

// containsFold reports whether needle occurs in haystack case-insensitively.
// It only folds ASCII letters, which is enough for XML keywords.
func containsFold(haystack []byte, needle string) bool {
	if len(needle) == 0 || len(haystack) < len(needle) {
		return false
	}
	lower := make([]byte, len(needle))
	for j := range needle {
		c := needle[j]
		if 'A' <= c && c <= 'Z' {
			c += 'a' - 'A'
		}
		lower[j] = c
	}
	for i := 0; i+len(lower) <= len(haystack); i++ {
		same := true
		for j, n := range lower {
			h := haystack[i+j]
			if 'A' <= h && h <= 'Z' {
				h += 'a' - 'A'
			}
			if h != n {
				same = false
				break
			}
		}
		if same {
			return true
		}
	}
	return false
}
