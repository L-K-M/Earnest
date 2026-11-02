package feed

import (
	"testing"
	"time"
)

func TestParseDate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		raw  string
		want string // RFC 3339; empty means nil
	}{
		{"rfc1123 gmt", "Mon, 02 Jan 2006 15:04:05 GMT", "2006-01-02T15:04:05Z"},
		{"rfc1123z", "Mon, 02 Jan 2006 15:04:05 -0700", "2006-01-02T22:04:05Z"},
		{"single digit day", "Tue, 3 Jan 2006 15:04:05 +0000", "2006-01-03T15:04:05Z"},
		{"no seconds", "Mon, 02 Jan 2006 15:04 -0700", "2006-01-02T22:04:00Z"},
		{"rfc822z two digit year", "02 Jan 06 15:04 -0700", "2006-01-02T22:04:00Z"},
		{"rfc822 named zone", "02 Jan 06 15:04 MST", "2006-01-02T22:04:00Z"},
		{"ansic", "Mon Jan  2 15:04:05 2006", "2006-01-02T15:04:05Z"},
		{"unix date", "Mon Jan  2 15:04:05 MST 2006", "2006-01-02T22:04:05Z"},
		{"ruby date", "Mon Jan 02 15:04:05 -0700 2006", "2006-01-02T22:04:05Z"},
		{"rfc3339 nano", "2026-01-02T03:04:05.123456789Z", "2026-01-02T03:04:05.123456789Z"},
		{"rfc3339 offset", "2026-01-02T03:04:05+02:00", "2026-01-02T01:04:05Z"},
		{"iso no zone", "2026-01-02T03:04:05", "2026-01-02T03:04:05Z"},
		{"iso space separator", "2026-01-02 03:04:05", "2026-01-02T03:04:05Z"},
		{"iso no seconds", "2026-01-02T03:04", "2026-01-02T03:04:00Z"},
		{"date only", "2026-01-02", "2026-01-02T00:00:00Z"},
		{"year month", "2026-01", "2026-01-01T00:00:00Z"},
		{"year only", "2026", "2026-01-01T00:00:00Z"},
		{"slash year first", "2026/01/02", "2026-01-02T00:00:00Z"},
		{"day month year words", "2 Jan 2006", "2006-01-02T00:00:00Z"},
		{"month day comma", "Jan 2, 2006", "2006-01-02T00:00:00Z"},
		{"full month day comma", "January 2, 2006", "2006-01-02T00:00:00Z"},
		{"full weekday month day", "Tuesday, January 2, 2006", "2006-01-02T00:00:00Z"},
		{"full weekday day month", "Monday, 2 January 2006", "2006-01-02T00:00:00Z"},
		{"named zone EST", "Fri, 02 Jan 2006 15:04:05 EST", "2006-01-02T20:04:05Z"},
		{"named zone PST", "Fri, 02 Jan 2006 15:04:05 PST", "2006-01-02T23:04:05Z"},
		{"named zone UT", "Fri, 02 Jan 2006 15:04:05 UT", "2006-01-02T15:04:05Z"},
		{"gmt with offset", "Fri, 02 Jan 2006 15:04:05 GMT+0100", "2006-01-02T14:04:05Z"},
		{"lowercase t and z", "2026-01-02t03:04:05z", "2026-01-02T03:04:05Z"},
		{"extra whitespace", "Mon,\n\t02   Jan 2006 15:04:05 GMT", "2006-01-02T15:04:05Z"},
		{"surrounding quotes", `"Mon, 02 Jan 2006 15:04:05 GMT"`, "2006-01-02T15:04:05Z"},
		{"parenthetical tail", "Mon, 02 Jan 2006 15:04:05 GMT (Greenwich Mean Time)", "2006-01-02T15:04:05Z"},
		{"fractional seconds", "Mon, 02 Jan 2006 15:04:05.123 GMT", "2006-01-02T15:04:05.123Z"},
		{"epoch seconds", "1700000000", "2023-11-14T22:13:20Z"},
		{"epoch milliseconds", "1700000000000", "2023-11-14T22:13:20Z"},

		{"empty", "", ""},
		{"garbage word", "not a date", ""},
		{"impossible month", "2026-13-45", ""},
		{"impossible day", "Sun, 99 Xyz 2026 99:99:99 +9999", ""},
		{"trailing junk", "Mon, 02 Jan 2006 15:04:05 GMT and then some", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := parseDate(tt.raw)
			if tt.want == "" {
				if got != nil {
					t.Fatalf("parseDate(%q) = %v, want nil", tt.raw, got)
				}
				return
			}
			if got == nil {
				t.Fatalf("parseDate(%q) = nil, want %s", tt.raw, tt.want)
			}
			want := tt.want
			if got.Format(time.RFC3339Nano) != want {
				t.Fatalf("parseDate(%q) = %s, want %s", tt.raw, got.Format(time.RFC3339Nano), want)
			}
			if got.Location() != time.UTC {
				t.Fatalf("parseDate(%q) location = %v, want UTC", tt.raw, got.Location())
			}
		})
	}
}
