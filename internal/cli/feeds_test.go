package cli

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

func TestFeedsList(t *testing.T) {
	sub := testSubscription()
	code, out, errOut, log := runStub(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/subscriptions" || r.Method != http.MethodGet {
			http.NotFound(w, r)
			return
		}
		writeJSON(w, []subscription{sub})
	}, testToken, "feeds", "list")
	requireCode(t, code, exitOK, errOut)
	if !strings.Contains(out, "Example Feed") || !strings.Contains(out, "today") {
		t.Fatalf("table missing subscription: %q", out)
	}
	requests := log.snapshot()
	if len(requests) != 1 || requests[0].auth != "Bearer "+testToken {
		t.Fatalf("requests = %+v", requests)
	}
}

func TestFeedsListJSON(t *testing.T) {
	sub := testSubscription()
	code, out, errOut, _ := runStub(t, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, []subscription{sub})
	}, testToken, "--json", "feeds", "list")
	requireCode(t, code, exitOK, errOut)
	var decoded []map[string]any
	if err := json.Unmarshal([]byte(out), &decoded); err != nil {
		t.Fatalf("--json output is not JSON: %v\n%s", err, out)
	}
	if len(decoded) != 1 || decoded[0]["title"] != "Example Feed" {
		t.Fatalf("decoded = %v", decoded)
	}
}

func TestFeedsAddSendsOpID(t *testing.T) {
	sub := testSubscription()
	code, out, errOut, _ := runStub(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/subscriptions" || r.Method != http.MethodPost {
			http.NotFound(w, r)
			return
		}
		var body subscribeBody
		decodeBody(t, r, &body)
		if body.OpID == "" {
			t.Errorf("subscribe body has no op_id: %+v", body)
		}
		if body.URL != "https://example.com/feed.xml" {
			t.Errorf("subscribe url = %q", body.URL)
		}
		writeJSON(w, sub)
	}, testToken, "feeds", "add", "https://example.com/feed.xml")
	requireCode(t, code, exitOK, errOut)
	if !strings.Contains(out, "subscribed Example Feed (id 5)") {
		t.Fatalf("output = %q", out)
	}
}

func TestFeedsAddRejectsNonURL(t *testing.T) {
	code, _, errOut, _ := runStub(t, func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("server should not be called")
	}, testToken, "feeds", "add", "not-a-url")
	requireCode(t, code, exitUsage, errOut)
	if !strings.Contains(errOut, "http://") {
		t.Fatalf("stderr = %q", errOut)
	}
}

func TestFeedsRemoveSendsOpID(t *testing.T) {
	code, out, errOut, _ := runStub(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/subscriptions/5" || r.Method != http.MethodDelete {
			http.NotFound(w, r)
			return
		}
		if r.URL.Query().Get("op_id") == "" {
			t.Errorf("delete query has no op_id: %v", r.URL.Query())
		}
		writeJSON(w, struct{}{})
	}, testToken, "feeds", "remove", "5")
	requireCode(t, code, exitOK, errOut)
	if !strings.Contains(out, "removed subscription 5") {
		t.Fatalf("output = %q", out)
	}
}

func TestFeedsImport(t *testing.T) {
	sub := testSubscription()
	results := []opmlResult{
		{Path: []int{0, 1}, Deferred: true, Subscription: &sub},
		{Path: []int{1}, Error: "invalid feed URL"},
	}
	code, out, errOut, log := runStub(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/opml:import" || r.Method != http.MethodPost {
			http.NotFound(w, r)
			return
		}
		if ct := r.Header.Get("Content-Type"); ct != "application/xml" {
			t.Errorf("content type = %q", ct)
		}
		writeJSON(w, results)
	}, testToken, "feeds", "import", "testdata/import.opml")
	requireCode(t, code, exitOK, errOut)
	if !strings.Contains(out, "subscribed (fetching): Example Feed") {
		t.Fatalf("output = %q", out)
	}
	if !strings.Contains(out, "error: invalid feed URL") {
		t.Fatalf("output = %q", out)
	}
	requests := log.snapshot()
	if len(requests) != 1 {
		t.Fatalf("requests = %d", len(requests))
	}
}

func TestFeedsExportWritesOPML(t *testing.T) {
	opml := `<?xml version="1.0"?><opml version="2.0"><body><outline xmlUrl="https://example.com/feed.xml"/></body></opml>` + "\n"
	code, out, errOut, _ := runStub(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/opml" || r.Method != http.MethodGet {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/x-opml; charset=utf-8")
		_, _ = w.Write([]byte(opml))
	}, testToken, "feeds", "export")
	requireCode(t, code, exitOK, errOut)
	if out != opml {
		t.Fatalf("output = %q, want the OPML document", out)
	}
}

func TestSubscribeProblemExitsNonZero(t *testing.T) {
	code, out, errOut, _ := runStub(t, func(w http.ResponseWriter, r *http.Request) {
		writeProblem(w, http.StatusNotFound, "not-found", "subscription 9")
	}, testToken, "feeds", "add", "https://example.com/missing.xml")
	requireCode(t, code, exitFailure, errOut)
	if strings.Contains(out, "subscribed") {
		t.Fatalf("reported success on failure: %q", out)
	}
	for _, part := range []string{"404", "Not Found", "subscription 9"} {
		if !strings.Contains(errOut, part) {
			t.Fatalf("stderr %q missing %q", errOut, part)
		}
	}
}
