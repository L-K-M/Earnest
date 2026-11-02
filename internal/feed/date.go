package feed

import (
	"regexp"
	"strconv"
	"strings"
	"time"
)

// dateLayouts covers the RSS and Atom families plus the common drift seen in
// the wild: missing zone, missing seconds, full weekday names, single-digit
// days and year-only stamps. Layouts are tried in order; the first success
// wins. All results are normalised to UTC so hashes and comparisons do not
// depend on the host time zone.
var dateLayouts = []string{
	time.RFC3339Nano,
	time.RFC3339,
	time.RFC1123Z,
	time.RFC1123,
	time.RFC822Z,
	time.RFC822,
	time.ANSIC,
	time.UnixDate,
	time.RubyDate,
	"Mon Jan 02 15:04:05 -0700 2006",
	"Mon Jan _2 15:04:05 -0700 2006",
	"Mon, 02 Jan 2006 15:04:05 -0700",
	"Mon, 2 Jan 2006 15:04:05 -0700",
	"Mon, 02 Jan 2006 15:04:05 MST",
	"Mon, 2 Jan 2006 15:04:05 MST",
	"Mon, 02 Jan 2006 15:04:05",
	"Mon, 2 Jan 2006 15:04:05",
	"Mon, 2 Jan 2006 15:04 -0700",
	"Mon, 2 Jan 2006 15:04 MST",
	"Mon, 2 Jan 2006 15:04",
	"2 Jan 2006 15:04:05 -0700",
	"2 Jan 2006 15:04:05 MST",
	"2 Jan 2006 15:04 -0700",
	"2 Jan 2006 15:04",
	"02 Jan 2006 15:04:05 MST",
	"02 Jan 2006 15:04 MST",
	"Jan 2, 2006",
	"January 2, 2006",
	"Monday, January 2, 2006",
	"Monday, 2 January 2006",
	"2 Jan 2006",
	"02 January 2006",
	"2006-01-02T15:04:05-0700",
	"2006-01-02T15:04:05",
	"2006-01-02T15:04",
	"2006-01-02 15:04:05 -0700",
	"2006-01-02 15:04:05",
	"2006-01-02 15:04",
	"2006-01-02",
	"2006-01",
	"2006",
	"2006/01/02",
}

// namedZones maps the abbreviations real feeds use to fixed offsets. Without
// this, Go resolves an unknown abbreviation against the host zone, which
// makes parsed times machine-dependent.
var namedZones = map[string]string{
	"GMT": "+0000",
	"UTC": "+0000",
	"UT":  "+0000",
	"EST": "-0500",
	"EDT": "-0400",
	"CST": "-0600",
	"CDT": "-0500",
	"MST": "-0700",
	"MDT": "-0600",
	"PST": "-0800",
	"PDT": "-0700",
}

var (
	namedZoneRE   = regexp.MustCompile(`(?i)\b(GMT|UTC|UT|EST|EDT|CST|CDT|MST|MDT|PST|PDT)\b`)
	gmtOffsetRE   = regexp.MustCompile(`(?i)\b(?:GMT|UTC)\s*([+-][0-9]{2}:?[0-9]{2})\b`)
	parenTailRE   = regexp.MustCompile(`\s*\([^()]*\)\s*$`)
	epochSecsRE   = regexp.MustCompile(`^[0-9]{10}$`)
	epochMillisRE = regexp.MustCompile(`^[0-9]{13}$`)
)

// parseDate returns the parsed instant or nil when raw is empty or matches no
// known layout. Unparseable dates are absent rather than zero, because a zero
// time would sort before everything and masquerade as a real publication
// date. Future dates are kept; they are display-only.
func parseDate(raw string) *time.Time {
	s := normalizeDateInput(raw)
	if s == "" {
		return nil
	}
	if epochSecsRE.MatchString(s) {
		sec, err := strconv.ParseInt(s, 10, 64)
		if err == nil {
			t := time.Unix(sec, 0).UTC()
			return &t
		}
	}
	if epochMillisRE.MatchString(s) {
		ms, err := strconv.ParseInt(s, 10, 64)
		if err == nil {
			t := time.UnixMilli(ms).UTC()
			return &t
		}
	}
	for _, layout := range dateLayouts {
		t, err := time.Parse(layout, s)
		if err == nil {
			t = t.UTC()
			return &t
		}
	}
	return nil
}

// normalizeDateInput trims, unquotes, collapses whitespace, folds the ISO T
// and Z separators to upper case, drops parenthetical annotations and rewrites
// named zones to numeric offsets.
func normalizeDateInput(raw string) string {
	s := strings.TrimSpace(raw)
	if s == "" {
		return ""
	}
	if len(s) >= 2 {
		first, last := s[0], s[len(s)-1]
		if (first == '"' && last == '"') || (first == '\'' && last == '\'') {
			s = s[1 : len(s)-1]
		}
	}
	s = collapseWS(s)
	s = parenTailRE.ReplaceAllString(s, "")

	// ISO allows lower-case t and z; Go's layouts are case-sensitive.
	if len(s) > 10 && s[10] == 't' {
		s = s[:10] + "T" + s[11:]
	}
	if strings.HasSuffix(s, "z") {
		s = s[:len(s)-1] + "Z"
	}

	s = gmtOffsetRE.ReplaceAllStringFunc(s, func(m string) string {
		sub := gmtOffsetRE.FindStringSubmatch(m)
		return strings.ReplaceAll(sub[1], ":", "")
	})
	s = namedZoneRE.ReplaceAllStringFunc(s, func(m string) string {
		return namedZones[strings.ToUpper(m)]
	})
	return s
}
