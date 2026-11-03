package cli

import (
	"encoding/json"
	"net/http"
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
	if out != "hello world" {
		t.Fatalf("output = %q, want the stored text", out)
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
