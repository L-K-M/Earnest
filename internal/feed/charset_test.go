package feed

import "testing"

func TestCharsetLabel(t *testing.T) {
	t.Parallel()

	tests := []struct {
		in   string
		want string
	}{
		{"windows-1252", "windows-1252"},
		{"ISO-8859-1", "ISO-8859-1"},
		{"utf-8", "utf-8"},
		{"text/html; charset=windows-1252", "windows-1252"},
		{`text/xml; charset="utf-8"`, "utf-8"},
		{"application/rss+xml; charset='iso-8859-1'; profile=x", "iso-8859-1"},
		{"", ""},
	}
	for _, tt := range tests {
		if got := charsetLabel(tt.in); got != tt.want {
			t.Errorf("charsetLabel(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}
