package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestHumanTextControlsAndUnicode(t *testing.T) {
	for r := rune(0); r <= '\u009f'; r++ {
		if (r >= ' ' && r < '\u007f') || r == '\n' || r == '\t' {
			continue
		}
		if got := humanText("before" + string(r) + "after"); got != "beforeafter" {
			t.Fatalf("control %U remained: %q", r, got)
		}
	}
	text := "α 中文 🙂\nnext\tcolumn\u2028line separator"
	if got := humanText(text); got != text {
		t.Fatalf("changed ordinary text: %q", got)
	}
	if got := cell(strings.Repeat("\x1b", tableCellLimit) + "visible"); got != "visible" {
		t.Fatalf("controls consumed the cell's truncation budget: %q", got)
	}
}

type terminalErrorWriter struct{}

func (terminalErrorWriter) Write([]byte) (int, error) { return 0, io.ErrClosedPipe }

type terminalShortWriter struct{}

func (terminalShortWriter) Write(p []byte) (int, error) { return len(p) - 1, nil }

func TestHumanTextWriterCountsAndErrors(t *testing.T) {
	var out bytes.Buffer
	input := "before\x1b\x07\u009bafter\n"
	n, err := (humanTextWriter{out: &out}).Write([]byte(input))
	if err != nil || n != len(input) || out.String() != "beforeafter\n" {
		t.Fatalf("write: n=%d err=%v output=%q", n, err, out.String())
	}
	for _, tc := range []struct {
		name string
		out  io.Writer
		err  error
	}{
		{"failed", terminalErrorWriter{}, io.ErrClosedPipe},
		{"short", terminalShortWriter{}, io.ErrShortWrite},
	} {
		t.Run(tc.name, func(t *testing.T) {
			n, err := (humanTextWriter{out: tc.out}).Write([]byte(input))
			if n != 0 || !errors.Is(err, tc.err) {
				t.Fatalf("write: n=%d err=%v", n, err)
			}
		})
	}
}

func TestHumanCLIOutputPreservesTextAndMachineData(t *testing.T) {
	text := "中文 🙂\nnext\tcolumn\x1b]0;publisher\x07\u009bafter"
	t.Run("multiline text", func(t *testing.T) {
		code, out, errOut, _ := runStub(t, func(w http.ResponseWriter, r *http.Request) {
			writeJSON(w, content{EntryID: 12, Text: text})
		}, testToken, "read", "12")
		requireCode(t, code, exitOK, errOut)
		if out != "中文 🙂\nnext\tcolumn]0;publisherafter\n" {
			t.Fatalf("human rendering=%q", out)
		}
	})
	t.Run("JSON round trip", func(t *testing.T) {
		code, out, errOut, _ := runStub(t, func(w http.ResponseWriter, r *http.Request) {
			writeJSON(w, content{EntryID: 12, Text: text})
		}, testToken, "read", "12", "--json")
		requireCode(t, code, exitOK, errOut)
		var body content
		if err := json.Unmarshal([]byte(out), &body); err != nil || body.Text != text {
			t.Fatalf("machine data changed: text=%q error=%v", body.Text, err)
		}
	})

	for _, args := range [][]string{{"feeds", "export"}, {"--json", "feeds", "export"}} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			document := "<opml version=\"2.0\"><body><outline text=\"raw\u009btext\"/></body></opml>\n"
			code, out, errOut, _ := runStub(t, func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "text/x-opml")
				_, _ = io.WriteString(w, document)
			}, testToken, args...)
			requireCode(t, code, exitOK, errOut)
			if out != document {
				t.Fatalf("raw OPML changed: %q", out)
			}
		})
	}
}

func TestHumanCLIImportAndErrorsNeutralizeControls(t *testing.T) {
	text := "before\x1b]0;publisher\x07\u009bafter"
	t.Run("import subscription title", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "export.json")
		if err := os.WriteFile(path, []byte(`[]`), 0o600); err != nil {
			t.Fatal(err)
		}
		code, out, errOut, _ := runStub(t, func(w http.ResponseWriter, r *http.Request) {
			writeJSON(w, keptImportReport{Subscribed: []subscription{{ID: 1, Title: text}}})
		}, testToken, "entries", "import", path)
		requireCode(t, code, exitOK, errOut)
		if !strings.Contains(out, "subscribed: before]0;publisherafter") || strings.ContainsAny(out, "\x1b\x07\u009b") {
			t.Fatalf("import rendering=%q", out)
		}
	})
	t.Run("server problem", func(t *testing.T) {
		code, out, errOut, _ := runStub(t, func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusBadRequest)
			writeJSON(w, map[string]any{"status": http.StatusBadRequest, "title": text})
		}, testToken, "read", "12")
		requireCode(t, code, exitFailure, errOut)
		if out != "" || !strings.Contains(errOut, "before]0;publisherafter") || strings.ContainsAny(errOut, "\x1b\x07\u009b") {
			t.Fatalf("problem rendering: stdout=%q stderr=%q", out, errOut)
		}
	})
}
