package cli

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

func TestChangesPassesCursor(t *testing.T) {
	code, out, errOut, log := runStub(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/changes" || r.Method != http.MethodGet {
			http.NotFound(w, r)
			return
		}
		if got := r.URL.Query().Get("cursor"); got != "gen-1:5" {
			t.Errorf("cursor = %q", got)
		}
		if got := r.URL.Query().Get("limit"); got != "50" {
			t.Errorf("limit = %q", got)
		}
		writeJSON(w, changesPage{
			Changes: []changeRecord{{
				Rev:     6,
				Type:    "state.updated",
				Payload: json.RawMessage(`{"entry_id":12,"register":"shelf","value":"done"}`),
			}},
			Next:       revision{Generation: "gen-1", Rev: 6},
			NextCursor: "gen-1:6",
		})
	}, testToken, "changes", "--cursor", "gen-1:5", "--limit", "50")
	requireCode(t, code, exitOK, errOut)
	if !strings.Contains(out, "state.updated") || !strings.Contains(out, `"entry_id":12`) {
		t.Fatalf("table = %q", out)
	}
	if !strings.Contains(out, "next cursor: gen-1:6") {
		t.Fatalf("output = %q", out)
	}
	requests := log.snapshot()
	if len(requests) != 1 {
		t.Fatalf("requests = %d, want only the change page", len(requests))
	}
}

func TestChangesWithoutCursorUsesHead(t *testing.T) {
	code, out, errOut, log := runStub(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/bootstrap":
			writeJSON(w, revision{Generation: "gen-1", Rev: 41})
		case "/v1/changes":
			if got := r.URL.Query().Get("cursor"); got != "gen-1:41" {
				t.Errorf("cursor = %q", got)
			}
			writeJSON(w, changesPage{Next: revision{Generation: "gen-1", Rev: 41}})
		default:
			http.NotFound(w, r)
		}
	}, testToken, "changes")
	requireCode(t, code, exitOK, errOut)
	if !strings.Contains(out, "current cursor: gen-1:41") {
		t.Fatalf("output = %q", out)
	}
	if !strings.Contains(out, "no changes") {
		t.Fatalf("output = %q", out)
	}
	requests := log.snapshot()
	if len(requests) != 2 || requests[0].path != "/v1/bootstrap" {
		t.Fatalf("requests = %+v", requests)
	}
}

func TestChangesWithoutCursorJSONStaysClean(t *testing.T) {
	code, out, errOut, _ := runStub(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/bootstrap":
			writeJSON(w, revision{Generation: "gen-1", Rev: 41})
		case "/v1/changes":
			writeJSON(w, changesPage{Next: revision{Generation: "gen-1", Rev: 41}})
		default:
			http.NotFound(w, r)
		}
	}, testToken, "--json", "changes")
	requireCode(t, code, exitOK, errOut)
	if strings.Contains(out, "current cursor") {
		t.Fatalf("human hint polluted JSON: %q", out)
	}
	var decoded changesPage
	if err := json.Unmarshal([]byte(out), &decoded); err != nil {
		t.Fatalf("not JSON: %v\n%s", err, out)
	}
}

func TestChangesJSON(t *testing.T) {
	code, out, errOut, _ := runStub(t, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, changesPage{
			Changes:    []changeRecord{{Rev: 6, Type: "entry.created"}},
			Next:       revision{Generation: "gen-1", Rev: 6},
			NextCursor: "gen-1:6",
		})
	}, testToken, "--json", "changes", "--cursor", "gen-1:5")
	requireCode(t, code, exitOK, errOut)
	var decoded struct {
		NextCursor string           `json:"next_cursor"`
		Changes    []map[string]any `json:"changes"`
	}
	if err := json.Unmarshal([]byte(out), &decoded); err != nil {
		t.Fatalf("not JSON: %v\n%s", err, out)
	}
	if decoded.NextCursor != "gen-1:6" || len(decoded.Changes) != 1 {
		t.Fatalf("decoded = %v", decoded)
	}
}
