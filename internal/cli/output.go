package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"text/tabwriter"
	"time"
	"unicode"
)

const (
	tableCellLimit = 60
	compactLimit   = 80
)

// humanText preserves text layout without letting untrusted text issue
// terminal commands. Machine formats bypass this rendering boundary.
func humanText(value string) string {
	return strings.Map(func(r rune) rune {
		if r != '\n' && r != '\t' && unicode.IsControl(r) {
			return -1
		}
		return r
	}, value)
}

type humanTextWriter struct{ out io.Writer }

func (w humanTextWriter) Write(p []byte) (int, error) {
	text := humanText(string(p))
	n, err := io.WriteString(w.out, text)
	if err != nil {
		return 0, err
	}
	if n != len(text) {
		return 0, io.ErrShortWrite
	}
	// The whole input was consumed, including controls omitted from its rendering.
	return len(p), nil
}

// writeTable renders rows as an aligned plain-text table.
func writeTable(w io.Writer, header []string, rows [][]string) {
	tw := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
	fmt.Fprintln(tw, strings.Join(header, "\t"))
	for _, row := range rows {
		fmt.Fprintln(tw, strings.Join(row, "\t"))
	}
	_ = tw.Flush()
}

func tableRows(count int) [][]string { return make([][]string, 0, count) }

// cell sanitises one value for the fixed-width table.
func cell(value string) string {
	value = humanText(value)
	value = strings.ReplaceAll(value, "\t", " ")
	value = strings.ReplaceAll(value, "\n", " ")
	return truncate(value, tableCellLimit)
}

// truncate shortens to n runes with an ellipsis, keeping tables readable.
func truncate(value string, n int) string {
	if n <= 0 {
		return ""
	}
	runes := []rune(value)
	if len(runes) <= n {
		return value
	}
	return string(runes[:n-1]) + "…"
}

// formatTime renders an instant for humans in their local zone.
func formatTime(t time.Time) string {
	if t.IsZero() {
		return "-"
	}
	return t.Local().Format("2006-01-02 15:04")
}

// compactJSON flattens a raw JSON value into one table cell.
func compactJSON(raw json.RawMessage) string {
	if len(raw) == 0 || string(raw) == "null" {
		return "-"
	}
	var buf bytes.Buffer
	if err := json.Compact(&buf, raw); err != nil {
		return truncate(string(raw), compactLimit)
	}
	return truncate(buf.String(), compactLimit)
}
