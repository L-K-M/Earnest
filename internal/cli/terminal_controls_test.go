package cli

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/L-K-M/Earnest/internal/feed"
	"github.com/L-K-M/Earnest/internal/sanitize"
)

func TestPublisherTerminalControlsAreInert(t *testing.T) {
	// Capture payloads in buffers, never in a live terminal.
	payload := "\x1b]52;c;dGVzdA==\x07"
	raw, err := json.Marshal(map[string]any{
		"version": "https://jsonfeed.org/version/1.1",
		"title":   "Publisher",
		"items": []map[string]any{{
			"id": "terminal-controls", "title": "before" + payload + "after",
			"content_html": "<p>before" + payload + "after</p>",
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := feed.Parse(raw, feed.ParseOptions{BaseURL: "https://feed.test/rss"})
	if err != nil || len(parsed.Items) != 1 {
		t.Fatalf("parse error=%v", err)
	}
	doc, err := sanitize.Sanitize(nil, parsed.Items[0].ContentHTML)
	if err != nil {
		t.Fatal(err)
	}
	text := sanitize.Text(doc.HTML)
	if !strings.Contains(parsed.Items[0].Title, payload) || !strings.Contains(text, payload) {
		t.Fatal("publisher controls were already neutralized upstream")
	}
	t.Run("entry table", func(t *testing.T) {
		item := testEntry()
		item.Title = parsed.Items[0].Title
		code, out, errOut, _ := runStub(t, func(w http.ResponseWriter, r *http.Request) {
			writeJSON(w, entriesPage{Entries: []entry{item}})
		}, testToken, "entries", "list")
		requireCode(t, code, exitOK, errOut)
		if strings.ContainsAny(out, "\x1b\x07") {
			t.Fatalf("terminal controls reached the human table: %q", out)
		}
	})
	t.Run("read", func(t *testing.T) {
		code, out, errOut, _ := runStub(t, func(w http.ResponseWriter, r *http.Request) {
			writeJSON(w, content{EntryID: 12, Text: text, HTML: doc.HTML, SanitizerVersion: 1, Source: "stored"})
		}, testToken, "read", "12")
		requireCode(t, code, exitOK, errOut)
		if strings.ContainsAny(out, "\x1b\x07") {
			t.Fatalf("terminal controls reached human reading output: %q", out)
		}
	})
}
