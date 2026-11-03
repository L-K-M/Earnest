package cli

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"testing"
)

func TestAnnotateSendsOpID(t *testing.T) {
	code, out, errOut, log := runStub(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut || !strings.HasPrefix(r.URL.Path, "/v1/entries/") || !strings.HasSuffix(r.URL.Path, "/annotations/note") {
			http.NotFound(w, r)
			return
		}
		var body annotationBody
		decodeBody(t, r, &body)
		if body.OpID == "" || body.Value != "remember" {
			t.Errorf("body = %+v", body)
		}
		idText, _, _ := strings.Cut(strings.TrimPrefix(r.URL.Path, "/v1/entries/"), "/")
		id, err := strconv.ParseInt(idText, 10, 64)
		if err != nil {
			t.Fatalf("path = %q", r.URL.Path)
		}
		writeJSON(w, annotation{ID: id, Kind: "note", EntryIDs: []int64{id}, Value: body.Value, Rev: 2})
	}, testToken, "annotate", "1", "2", "--kind", "note", "--text", "remember")
	requireCode(t, code, exitOK, errOut)
	if !strings.Contains(out, "note recorded on entry 1 (id 1)") || !strings.Contains(out, "note recorded on entry 2 (id 2)") {
		t.Fatalf("output = %q", out)
	}
	requests := log.snapshot()
	var opIDs = make(map[string]bool)
	for _, req := range requests {
		var body map[string]any
		if err := json.Unmarshal(req.body, &body); err != nil {
			t.Fatal(err)
		}
		opID, _ := body["op_id"].(string)
		if opID == "" || opIDs[opID] {
			t.Fatalf("annotations need distinct op_ids: %v", body)
		}
		opIDs[opID] = true
	}
	if len(requests) != 2 || requests[0].method != http.MethodPut {
		t.Fatalf("requests = %+v", requests)
	}
	if requests[0].path != "/v1/entries/1/annotations/note" || requests[1].path != "/v1/entries/2/annotations/note" {
		t.Fatalf("paths = %s, %s", requests[0].path, requests[1].path)
	}
}

func TestAnnotateRejectsBadKindAndEmptyText(t *testing.T) {
	tests := []struct {
		name string
		args []string
	}{
		{"kind", []string{"annotate", "1", "--kind", "summary", "--text", "x"}},
		{"text", []string{"annotate", "1", "--kind", "note", "--text", ""}},
		{"ids", []string{"annotate", "--kind", "note", "--text", "x"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			code, _, errOut, _ := runStub(t, func(w http.ResponseWriter, r *http.Request) {
				t.Errorf("server should not be called")
			}, testToken, tt.args...)
			requireCode(t, code, exitUsage, errOut)
		})
	}
}

func TestBriefWritesBriefKind(t *testing.T) {
	code, out, errOut, log := runStub(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/briefs" || r.Method != http.MethodPost {
			http.NotFound(w, r)
			return
		}
		var body briefBody
		decodeBody(t, r, &body)
		if len(body.EntryIDs) != 3 || body.OpID == "" || body.Value != "morning summary" {
			t.Errorf("body = %+v", body)
		}
		writeJSON(w, annotation{ID: 8, Kind: "brief", EntryIDs: body.EntryIDs})
	}, testToken, "brief", "--text", "morning summary", "1", "2", "3")
	requireCode(t, code, exitOK, errOut)
	if !strings.Contains(out, "brief recorded on 3 entries (id 8)") {
		t.Fatalf("output = %q", out)
	}
	if requests := log.snapshot(); len(requests) != 1 || requests[0].path != "/v1/briefs" {
		t.Fatalf("requests = %+v", requests)
	}
}

func TestAnnotateBriefUsesBriefsEndpoint(t *testing.T) {
	code, _, errOut, log := runStub(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/briefs" || r.Method != http.MethodPost {
			http.NotFound(w, r)
			return
		}
		var body briefBody
		decodeBody(t, r, &body)
		if len(body.EntryIDs) != 1 || body.OpID == "" {
			t.Errorf("body = %+v", body)
		}
		writeJSON(w, annotation{ID: 9, Kind: "brief"})
	}, testToken, "annotate", "1", "--kind", "brief", "--text", "summary")
	requireCode(t, code, exitOK, errOut)
	if requests := log.snapshot(); len(requests) != 1 {
		t.Fatalf("requests = %+v", requests)
	}
}

func TestNotifySuccess(t *testing.T) {
	code, out, errOut, log := runStub(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/bootstrap":
			writeJSON(w, revision{Generation: "gen-9", Rev: 4})
			return
		case "/v1/notifications":
		default:
			http.NotFound(w, r)
			return
		}
		if r.Method != http.MethodPost {
			t.Errorf("method = %s", r.Method)
			return
		}
		var body notificationBody
		decodeBody(t, r, &body)
		if body.Generation != "gen-9" {
			t.Errorf("generation = %q", body.Generation)
		}
		if body.OpID == "" || body.Note != "CVE in rustls" || body.Category != "alert" {
			t.Errorf("body = %+v", body)
		}
		if len(body.EntryIDs) != 1 || body.EntryIDs[0] != 12 {
			t.Errorf("entry ids = %v", body.EntryIDs)
		}
		writeJSON(w, notification{
			ID:          551,
			PrincipalID: 9,
			Status:      "held",
			Category:    "alert",
			Budget:      notificationBudget{UsedToday: 1, LimitToday: 3},
			Why:         notificationWhy{Sender: "Morning Agent", Budget: notificationBudget{UsedToday: 1, LimitToday: 3}},
			HeldUntil:   timePtr("2026-11-02T19:00:00Z"),
		})
	}, testToken, "notify", "--note", "CVE in rustls", "--entry", "12", "--category", "alert")
	requireCode(t, code, exitOK, errOut)
	if !strings.Contains(out, "notification 551: held") || !strings.Contains(out, "used 1/3") {
		t.Fatalf("output = %q", out)
	}
	if !strings.Contains(out, "held until") {
		t.Fatalf("output = %q", out)
	}
	requests := log.snapshot()
	if len(requests) != 2 || requests[0].path != "/v1/bootstrap" {
		t.Fatalf("requests = %+v", requests)
	}
}

func TestNotifyBudgetProblemShowsBudget(t *testing.T) {
	code, out, errOut, _ := runStub(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/bootstrap" {
			writeJSON(w, revision{Generation: "gen-9", Rev: 4})
			return
		}
		w.Header().Set("Content-Type", "application/problem+json")
		w.WriteHeader(http.StatusTooManyRequests)
		_ = json.NewEncoder(w).Encode(problem{
			Type:   "/problems/rate-limited",
			Title:  "Too Many Requests",
			Status: http.StatusTooManyRequests,
			Detail: "notification budget 3/3",
			Budget: json.RawMessage(`{"used_today":3,"limit_today":3}`),
		})
	}, testToken, "notify", "--note", "again", "--entry", "12")
	requireCode(t, code, exitFailure, errOut)
	if strings.Contains(out, "notification") {
		t.Fatalf("reported success: %q", out)
	}
	for _, part := range []string{"429", "Too Many Requests", "used 3/3"} {
		if !strings.Contains(errOut, part) {
			t.Fatalf("stderr %q missing %q", errOut, part)
		}
	}
}

func TestNotifyRequiresNote(t *testing.T) {
	code, _, errOut, _ := runStub(t, func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("server should not be called")
	}, testToken, "notify", "--note", "")
	requireCode(t, code, exitUsage, errOut)
}

func TestNotifyRequiresEntry(t *testing.T) {
	code, _, errOut, _ := runStub(t, func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("server should not be called")
	}, testToken, "notify", "--note", "no entry")
	requireCode(t, code, exitUsage, errOut)
	if !strings.Contains(errOut, "--entry") {
		t.Fatalf("stderr = %q", errOut)
	}
}

func TestTriageSendsGenerationAndAction(t *testing.T) {
	code, out, errOut, log := runStub(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/bootstrap":
			writeJSON(w, revision{Generation: "gen-7", Rev: 21})
		case "/v1/triage":
			var body triageBody
			decodeBody(t, r, &body)
			if body.OpID == "" || body.Generation != "gen-7" || len(body.EntryIDs) != 2 || body.Action != "done" {
				t.Errorf("body = %+v", body)
			}
			writeJSON(w, triageResult{
				Results:      []stateResult{{Status: "applied"}, {Status: "superseded"}},
				RevertHandle: "revert-handle-123",
				ExpiresAt:    parseTestTime("2026-11-03T12:00:00Z"),
			})
		default:
			http.NotFound(w, r)
		}
	}, testToken, "triage", "done", "10", "11")
	requireCode(t, code, exitOK, errOut)
	if !strings.Contains(out, "revert handle: revert-handle-123") {
		t.Fatalf("output = %q", out)
	}
	if !strings.Contains(out, "earnest revert revert-handle-123") {
		t.Fatalf("output = %q", out)
	}
	if !strings.Contains(out, "expires") {
		t.Fatalf("output = %q", out)
	}
	requests := log.snapshot()
	if len(requests) != 2 || requests[1].path != "/v1/triage" {
		t.Fatalf("requests = %+v", requests)
	}
}

func TestTriageRejectsBadAction(t *testing.T) {
	code, _, errOut, _ := runStub(t, func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("server should not be called")
	}, testToken, "triage", "maybe", "1")
	requireCode(t, code, exitUsage, errOut)
}

func TestRevertPostsHandle(t *testing.T) {
	code, out, errOut, log := runStub(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/triage:revert" || r.Method != http.MethodPost {
			http.NotFound(w, r)
			return
		}
		var body map[string]any
		decodeBody(t, r, &body)
		if body["revert_handle"] != "handle-9" || body["op_id"] == "" || body["handle"] != nil {
			t.Errorf("body = %+v", body)
		}
		writeJSON(w, struct{}{})
	}, testToken, "revert", "handle-9")
	requireCode(t, code, exitOK, errOut)
	if !strings.Contains(out, "reverted handle-9") {
		t.Fatalf("output = %q", out)
	}
	requests := log.snapshot()
	if len(requests) != 1 {
		t.Fatalf("requests = %d", len(requests))
	}
}

func TestActivityTable(t *testing.T) {
	code, out, errOut, log := runStub(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/activity" {
			http.NotFound(w, r)
			return
		}
		if r.URL.Query().Get("limit") != "25" {
			t.Errorf("limit = %q", r.URL.Query().Get("limit"))
		}
		writeJSON(w, activityPage{Activities: []activity{{
			ID:            42,
			PrincipalName: "Morning Agent",
			Action:        "annotation.updated",
			Target:        json.RawMessage(`{"id":3}`),
			NextAction:    "revert",
			CreatedAt:     parseTestTime("2026-11-02T07:00:00Z"),
		}}})
	}, testToken, "activity", "--limit", "25")
	requireCode(t, code, exitOK, errOut)
	if !strings.Contains(out, "Morning Agent") || !strings.Contains(out, "annotation.updated") {
		t.Fatalf("table = %q", out)
	}
	requests := log.snapshot()
	if len(requests) != 1 {
		t.Fatalf("requests = %d", len(requests))
	}
}
