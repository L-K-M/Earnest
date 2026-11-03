package cli

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

func TestStateBatchSendsDistinctOpIDs(t *testing.T) {
	code, out, errOut, log := runStub(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/bootstrap":
			writeJSON(w, revision{Generation: "gen-1", Rev: 9})
		case "/v1/me":
			writeJSON(w, meResult{UserID: 7, PrincipalID: 9, Kind: "human"})
		case "/v1/state:batch":
			var writes []stateWrite
			decodeBody(t, r, &writes)
			if len(writes) != 2 {
				t.Fatalf("writes = %d, want 2", len(writes))
			}
			seen := map[string]bool{}
			for _, write := range writes {
				if write.OpID == "" || seen[write.OpID] {
					t.Errorf("missing or duplicate op_id %+v", write)
				}
				seen[write.OpID] = true
				if write.Base.Generation != "gen-1" {
					t.Errorf("base generation = %q", write.Base.Generation)
				}
				if write.Principal != 9 {
					t.Errorf("principal = %d", write.Principal)
				}
				if write.Register != "shelf" || write.Value != "done" {
					t.Errorf("register/value = %s/%v", write.Register, write.Value)
				}
				if write.Kind != "explicit" {
					t.Errorf("kind = %q", write.Kind)
				}
				if write.OccurredAt.IsZero() {
					t.Error("occurred_at is zero")
				}
			}
			writeJSON(w, []stateResult{
				{OpID: writes[0].OpID, Status: "applied", State: testEntry().State},
				{OpID: writes[1].OpID, Status: "superseded", State: testEntry().State},
			})
		default:
			http.NotFound(w, r)
		}
	}, testToken, "done", "10", "11")
	requireCode(t, code, exitOK, errOut)
	if !strings.Contains(out, "applied") || !strings.Contains(out, "superseded") {
		t.Fatalf("output = %q", out)
	}
	requests := log.snapshot()
	if len(requests) != 3 {
		t.Fatalf("requests = %d, want 3", len(requests))
	}
}

func TestUnreadWriteShape(t *testing.T) {
	code, _, errOut, _ := runStub(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/bootstrap":
			writeJSON(w, revision{Generation: "g", Rev: 1})
		case "/v1/me":
			writeJSON(w, meResult{PrincipalID: 9, Kind: "agent"})
		case "/v1/state:batch":
			var writes []stateWrite
			decodeBody(t, r, &writes)
			if len(writes) != 1 || writes[0].Register != "seen" || writes[0].Value != "unseen" {
				t.Fatalf("writes = %+v", writes)
			}
			writeJSON(w, []stateResult{})
		default:
			http.NotFound(w, r)
		}
	}, testToken, "unread", "10")
	requireCode(t, code, exitOK, errOut)
}

func TestKeepWriteShape(t *testing.T) {
	code, _, errOut, _ := runStub(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/bootstrap":
			writeJSON(w, revision{Generation: "g", Rev: 1})
		case "/v1/me":
			writeJSON(w, meResult{PrincipalID: 9})
		case "/v1/state:batch":
			var writes []stateWrite
			decodeBody(t, r, &writes)
			if len(writes) != 1 || writes[0].Register != "kept" || writes[0].Value != true {
				t.Fatalf("writes = %+v", writes)
			}
			writeJSON(w, []stateResult{})
		default:
			http.NotFound(w, r)
		}
	}, testToken, "keep", "10")
	requireCode(t, code, exitOK, errOut)
}

func TestStateBatchRequiresIDs(t *testing.T) {
	code, _, errOut, _ := runStub(t, func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("server should not be called")
	}, testToken, "done")
	requireCode(t, code, exitUsage, errOut)
}

func TestStateBatchProblemExitsNonZero(t *testing.T) {
	code, _, errOut, _ := runStub(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/bootstrap":
			writeJSON(w, revision{Generation: "g", Rev: 1})
		case "/v1/me":
			writeJSON(w, meResult{PrincipalID: 9})
		case "/v1/state:batch":
			writeProblem(w, http.StatusBadRequest, "invalid-input", "entry 10 is gone")
		default:
			http.NotFound(w, r)
		}
	}, testToken, "later", "10")
	requireCode(t, code, exitFailure, errOut)
	if !strings.Contains(errOut, "entry 10 is gone") {
		t.Fatalf("stderr = %q", errOut)
	}
}

func TestStateBatchJSON(t *testing.T) {
	code, out, errOut, _ := runStub(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/bootstrap":
			writeJSON(w, revision{Generation: "g", Rev: 1})
		case "/v1/me":
			writeJSON(w, meResult{PrincipalID: 9})
		case "/v1/state:batch":
			writeJSON(w, []stateResult{{OpID: "op", Status: "applied"}})
		default:
			http.NotFound(w, r)
		}
	}, testToken, "--json", "keep", "10")
	requireCode(t, code, exitOK, errOut)
	var decoded []map[string]any
	if err := json.Unmarshal([]byte(out), &decoded); err != nil {
		t.Fatalf("not JSON: %v\n%s", err, out)
	}
	if len(decoded) != 1 || decoded[0]["status"] != "applied" {
		t.Fatalf("decoded = %v", decoded)
	}
}
