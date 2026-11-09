package cli

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestEntriesListQueryMapping(t *testing.T) {
	item := testEntry()
	code, out, errOut, _ := runStub(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/entries" || r.Method != http.MethodGet {
			http.NotFound(w, r)
			return
		}
		query := r.URL.Query()
		if query.Get("view") != "river" {
			t.Errorf("view = %q, want river for today", query.Get("view"))
		}
		if query.Get("unread_only") != "true" {
			t.Errorf("unread_only = %q", query.Get("unread_only"))
		}
		if query.Get("subscription_id") != "5" {
			t.Errorf("subscription_id = %q", query.Get("subscription_id"))
		}
		if query.Get("limit") != "20" || query.Get("cursor") != "7" {
			t.Errorf("limit/cursor = %q/%q", query.Get("limit"), query.Get("cursor"))
		}
		writeJSON(w, entriesPage{Entries: []entry{item}, NextCursor: "12"})
	}, testToken, "entries", "list", "--view", "today", "--unread", "--feed", "5", "--limit", "20", "--cursor", "7")
	requireCode(t, code, exitOK, errOut)
	if !strings.Contains(out, "A headline") || !strings.Contains(out, "unread") {
		t.Fatalf("table = %q", out)
	}
	if !strings.Contains(out, "--cursor 12") {
		t.Fatalf("continuation hint missing: %q", out)
	}
}

func TestEntriesListViewValues(t *testing.T) {
	tests := []struct {
		view string
		want string
	}{
		{"today", "river"},
		{"later", "later"},
		{"kept", "kept"},
		{"done", "done"},
		{"all", "all"},
	}
	for _, tt := range tests {
		t.Run(tt.view, func(t *testing.T) {
			code, _, errOut, log := runStub(t, func(w http.ResponseWriter, r *http.Request) {
				if got := r.URL.Query().Get("view"); got != tt.want {
					t.Errorf("view = %q, want %q", got, tt.want)
				}
				writeJSON(w, entriesPage{})
			}, testToken, "entries", "list", "--view", tt.view)
			requireCode(t, code, exitOK, errOut)
			if len(log.snapshot()) != 1 {
				t.Fatalf("requests = %d", len(log.snapshot()))
			}
		})
	}
}

func TestEntriesListRejectsBadView(t *testing.T) {
	code, _, errOut, _ := runStub(t, func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("server should not be called")
	}, testToken, "entries", "list", "--view", "river")
	requireCode(t, code, exitUsage, errOut)
}

func TestReadPrintsStoredText(t *testing.T) {
	code, out, errOut, _ := runStub(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/entries/12/content" || r.Method != http.MethodGet {
			http.NotFound(w, r)
			return
		}
		writeJSON(w, content{
			EntryID:          12,
			HTML:             "<p>hello world</p>",
			Text:             "hello world",
			SanitizerVersion: 1,
			Source:           "stored",
		})
	}, testToken, "read", "12")
	requireCode(t, code, exitOK, errOut)
	if out != "hello world\n" {
		t.Fatalf("output = %q, want the stored text with a trailing newline", out)
	}
}

func TestReadJSON(t *testing.T) {
	code, out, errOut, _ := runStub(t, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, content{EntryID: 12, Text: "hello", SanitizerVersion: 1, Source: "stored"})
	}, testToken, "--json", "read", "12")
	requireCode(t, code, exitOK, errOut)
	var decoded map[string]any
	if err := json.Unmarshal([]byte(out), &decoded); err != nil {
		t.Fatalf("not JSON: %v\n%s", err, out)
	}
	if decoded["text"] != "hello" {
		t.Fatalf("decoded = %v", decoded)
	}
}

func TestSearchTable(t *testing.T) {
	code, out, errOut, _ := runStub(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/search" {
			http.NotFound(w, r)
			return
		}
		if r.URL.Query().Get("q") != "rustls" {
			t.Errorf("q = %q", r.URL.Query().Get("q"))
		}
		if r.URL.Query().Get("limit") != "20" {
			t.Errorf("limit = %q", r.URL.Query().Get("limit"))
		}
		writeJSON(w, searchPage{Matches: []searchMatch{{Kind: "entry", EntryID: 3, Title: "Rustls advisory", Snippet: "a CVE"}}})
	}, testToken, "search", "--limit", "20", "rustls")
	requireCode(t, code, exitOK, errOut)
	if !strings.Contains(out, "Rustls advisory") || !strings.Contains(out, "a CVE") {
		t.Fatalf("table = %q", out)
	}
}

func TestSearchFiltersAndCursor(t *testing.T) {
	code, _, errOut, log := runStub(t, func(w http.ResponseWriter, r *http.Request) {
		query := r.URL.Query()
		if query.Get("subscription_id") != "5" || query.Get("folder_id") != "2" || query.Get("cursor") != "next" {
			t.Errorf("query = %v", query)
		}
		writeJSON(w, searchPage{Matches: []searchMatch{{Kind: "agent_note", EntryID: 3, Title: "Entry", Snippet: "a note", AgentName: "Morning Agent"}, {Kind: "entry", EntryID: 4, Title: "Other"}}, NextCursor: "next-2"})
	}, testToken, "search", "--feed", "5", "--folder", "2", "--cursor", "next", "note")
	requireCode(t, code, exitOK, errOut)
	if requests := log.snapshot(); len(requests) != 1 {
		t.Fatalf("requests = %d", len(requests))
	}
}

func TestSearchJSON(t *testing.T) {
	code, out, errOut, _ := runStub(t, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, searchPage{Matches: []searchMatch{{Kind: "agent_note", EntryID: 3, Title: "Entry", Snippet: "a note", AgentName: "Morning Agent"}}})
	}, testToken, "--json", "search", "note")
	requireCode(t, code, exitOK, errOut)
	var decoded struct {
		Matches []map[string]any `json:"matches"`
	}
	if err := json.Unmarshal([]byte(out), &decoded); err != nil {
		t.Fatalf("not JSON: %v\n%s", err, out)
	}
	if len(decoded.Matches) != 1 || decoded.Matches[0]["agent_name"] != "Morning Agent" {
		t.Fatalf("decoded = %v", decoded)
	}
}

func TestBriefingUsesPosition(t *testing.T) {
	item := testEntry()
	item.Seq = 101
	code, out, errOut, log := runStub(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/positions/river":
			writeJSON(w, position{Principal: 9, View: "river", Seq: 42, Rev: 7})
		case "/v1/entries":
			if r.URL.Query().Get("after_seq") != "42" {
				t.Errorf("after_seq = %q", r.URL.Query().Get("after_seq"))
			}
			writeJSON(w, entriesPage{Entries: []entry{item}})
		default:
			http.NotFound(w, r)
		}
	}, testToken, "briefing")
	requireCode(t, code, exitOK, errOut)
	if !strings.Contains(out, "new since seq 42") || !strings.Contains(out, "A headline") {
		t.Fatalf("output = %q", out)
	}
	requests := log.snapshot()
	if len(requests) != 2 || requests[0].path != "/v1/positions/river" {
		t.Fatalf("requests = %+v", requests)
	}
}

func TestBriefingCursor(t *testing.T) {
	item := testEntry()
	item.Seq = 102
	code, _, errOut, log := runStub(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/positions/river":
			writeJSON(w, position{Principal: 9, View: "river", Seq: 42, Rev: 7})
		case "/v1/entries":
			if r.URL.Query().Get("cursor") != "101" {
				t.Errorf("cursor = %q", r.URL.Query().Get("cursor"))
			}
			if r.URL.Query().Get("after_seq") != "" {
				t.Errorf("after_seq = %q", r.URL.Query().Get("after_seq"))
			}
			writeJSON(w, entriesPage{Entries: []entry{item}})
		default:
			http.NotFound(w, r)
		}
	}, testToken, "briefing", "--cursor", "101")
	requireCode(t, code, exitOK, errOut)
	if requests := log.snapshot(); len(requests) != 2 {
		t.Fatalf("requests = %+v", requests)
	}
}

// writeExport writes a small export file and returns its path.
func writeExport(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "starred.json")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// requireLines asserts the output lines, ignoring table padding.
func requireLines(t *testing.T, out string, want []string) {
	t.Helper()
	lines := strings.Split(strings.TrimSuffix(out, "\n"), "\n")
	if len(lines) != len(want) {
		t.Fatalf("output has %d lines, want %d:\n%s", len(lines), len(want), out)
	}
	for i, line := range lines {
		if got := strings.Join(strings.Fields(line), " "); got != want[i] {
			t.Fatalf("line %d = %q, want %q", i, got, want[i])
		}
	}
}

func TestEntriesImportRequestAndOutput(t *testing.T) {
	export := []byte(`{"subscriptions":[],"items":[{"title":"An article"}]}` + "\n")
	file := writeExport(t, string(export))
	report := keptImportReport{
		Results: []keptImportResult{
			{Index: 0, Status: "created", EntryID: 12, SubscriptionID: 3, FeedURL: "https://a.example/feed", Title: "An article", Labels: []string{"Tech"}},
			{Index: 1, Status: "matched", EntryID: 13, Title: "Another"},
			{Index: 2, Status: "not_subscribed", FeedURL: "https://b.example/rss", Title: "Third"},
			{Index: 3, Status: "failed", Title: "Broken", Error: "the article has no ID, link or title"},
		},
		Subscribed: []subscription{testSubscription(), {ID: 6, URL: "https://example.com/other.xml"}},
	}
	code, out, errOut, log := runStub(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/kept:import" || r.Method != http.MethodPost {
			http.NotFound(w, r)
			return
		}
		if ct := r.Header.Get("Content-Type"); ct != "application/json" {
			t.Errorf("content type = %q", ct)
		}
		if got := r.URL.Query().Get("missing_feeds"); got != "" {
			t.Errorf("missing_feeds = %q, want empty by default", got)
		}
		writeJSON(w, report)
	}, testToken, "entries", "import", file)
	requireCode(t, code, exitOK, errOut)
	requests := log.snapshot()
	if len(requests) != 1 {
		t.Fatalf("requests = %d", len(requests))
	}
	if string(requests[0].body) != string(export) {
		t.Fatalf("body = %q, want %q", requests[0].body, export)
	}
	requireLines(t, out, []string{
		"ARTICLE RESULT TITLE",
		"0 created An article",
		"1 matched Another",
		"2 not subscribed: https://b.example/rss Third",
		"3 failed: the article has no ID, link or title Broken",
		"subscribed: Example Feed",
		"subscribed: https://example.com/other.xml",
		"summary: 1 created, 1 matched, 1 not subscribed, 1 failed",
		"labels: added the export's labels to 1 article",
		"1 skipped because you don't follow their feed; run again with --subscribe-missing to subscribe and import them",
	})
}

func TestEntriesImportSubscribeMissing(t *testing.T) {
	file := writeExport(t, `{}`)
	report := keptImportReport{
		Results: []keptImportResult{
			{Index: 0, Status: "not_subscribed", FeedURL: "https://b.example/rss", Title: "Third"},
		},
	}
	code, out, errOut, log := runStub(t, func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query().Get("missing_feeds"); got != "subscribe" {
			t.Errorf("missing_feeds = %q, want subscribe", got)
		}
		writeJSON(w, report)
	}, testToken, "entries", "import", "--subscribe-missing", file)
	requireCode(t, code, exitOK, errOut)
	if strings.Contains(out, "skipped because you don't follow") {
		t.Fatalf("hint printed with --subscribe-missing: %q", out)
	}
	if requests := log.snapshot(); len(requests) != 1 {
		t.Fatalf("requests = %d", len(requests))
	}
}

func TestEntriesImportStatusLabels(t *testing.T) {
	file := writeExport(t, `{}`)
	report := keptImportReport{
		Results: []keptImportResult{
			{Index: 0, Status: "unchanged", Title: "Kept already"},
			{Index: 1, Status: "unkept", Title: "Removed"},
			{Index: 2, Status: "gone", Title: "Retained out"},
			{Index: 3, Status: "not_subscribed"},
		},
	}
	code, out, errOut, _ := runStub(t, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, report)
	}, testToken, "entries", "import", file)
	requireCode(t, code, exitOK, errOut)
	requireLines(t, out, []string{
		"ARTICLE RESULT TITLE",
		"0 unchanged Kept already",
		"1 unkept Removed",
		"2 gone Retained out",
		"3 not subscribed -",
		"summary: 1 unchanged, 1 unkept, 1 not subscribed, 1 gone",
		"1 skipped because you don't follow their feed; run again with --subscribe-missing to subscribe and import them",
	})
}

func TestEntriesImportEmptyReport(t *testing.T) {
	file := writeExport(t, `{}`)
	code, out, errOut, _ := runStub(t, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, keptImportReport{})
	}, testToken, "entries", "import", file)
	requireCode(t, code, exitOK, errOut)
	requireLines(t, out, []string{"ARTICLE RESULT TITLE", "summary: nothing to import"})
}

func TestEntriesImportJSON(t *testing.T) {
	file := writeExport(t, `{}`)
	report := keptImportReport{
		Results: []keptImportResult{
			{Index: 0, Status: "created", EntryID: 12, Title: "An article"},
			{Index: 1, Status: "failed", Title: "Broken", Error: "no id"},
		},
		Subscribed: []subscription{testSubscription()},
	}
	code, out, errOut, _ := runStub(t, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, report)
	}, testToken, "--json", "entries", "import", file)
	requireCode(t, code, exitOK, errOut)
	var decoded keptImportReport
	if err := json.Unmarshal([]byte(out), &decoded); err != nil {
		t.Fatalf("not JSON: %v\n%s", err, out)
	}
	if len(decoded.Results) != 2 || decoded.Results[0].Status != "created" || decoded.Results[1].Error != "no id" {
		t.Fatalf("decoded = %+v", decoded)
	}
	if len(decoded.Subscribed) != 1 || decoded.Subscribed[0].Title != "Example Feed" {
		t.Fatalf("decoded = %+v", decoded)
	}
	if strings.Contains(out, "summary:") {
		t.Fatalf("--json added text: %q", out)
	}
}

func TestEntriesImportRequiresFile(t *testing.T) {
	code, _, errOut, log := runStub(t, func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("server should not be called")
	}, testToken, "entries", "import")
	requireCode(t, code, exitUsage, errOut)
	if !strings.Contains(errOut, "usage: earnest entries import") {
		t.Fatalf("stderr = %q", errOut)
	}
	if requests := log.snapshot(); len(requests) != 0 {
		t.Fatalf("requests = %d", len(requests))
	}
}

func TestEntriesImportMissingFile(t *testing.T) {
	code, _, errOut, _ := runStub(t, func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("server should not be called")
	}, testToken, "entries", "import", filepath.Join(t.TempDir(), "absent.json"))
	requireCode(t, code, exitFailure, errOut)
	if !strings.Contains(errOut, "read export:") {
		t.Fatalf("stderr = %q", errOut)
	}
}

func TestBriefingJSON(t *testing.T) {
	item := testEntry()
	code, out, errOut, _ := runStub(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/positions/river":
			writeJSON(w, position{Principal: 9, View: "river", Seq: 42, Rev: 7})
		case "/v1/entries":
			writeJSON(w, entriesPage{Entries: []entry{item}})
		default:
			http.NotFound(w, r)
		}
	}, testToken, "--json", "briefing")
	requireCode(t, code, exitOK, errOut)
	var decoded struct {
		Position struct {
			Seq uint64 `json:"seq"`
		} `json:"position"`
		Entries []map[string]any `json:"entries"`
	}
	if err := json.Unmarshal([]byte(out), &decoded); err != nil {
		t.Fatalf("not JSON: %v\n%s", err, out)
	}
	if decoded.Position.Seq != 42 || len(decoded.Entries) != 1 {
		t.Fatalf("decoded = %v", decoded)
	}
}

func TestKeptResultCell(t *testing.T) {
	for _, tc := range []struct {
		result keptImportResult
		want   string
	}{
		{keptImportResult{Status: "failed", Error: "no link"}, "failed: no link"},
		{keptImportResult{Status: "failed"}, "failed"},
		{keptImportResult{Status: "not_subscribed", FeedURL: "https://b.example/rss"}, "not subscribed: https://b.example/rss"},
		{keptImportResult{Status: "not_subscribed"}, "not subscribed"},
		{keptImportResult{Status: "unkept"}, "unkept"},
	} {
		if got := keptResultCell(tc.result); got != tc.want {
			t.Errorf("keptResultCell(%+v) = %q, want %q", tc.result, got, tc.want)
		}
	}
}
