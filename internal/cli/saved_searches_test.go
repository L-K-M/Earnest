package cli

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

func TestSavedSearchesList(t *testing.T) {
	code, out, errOut, log := runStub(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/saved-searches" || r.Method != http.MethodGet {
			http.NotFound(w, r)
			return
		}
		writeJSON(w, []savedSearch{{
			ID:      7,
			Name:    "Rust",
			Query:   "rustls",
			Filters: searchFilter{SubscriptionID: 5, Limit: 20},
		}})
	}, testToken, "saved-searches", "list")
	requireCode(t, code, exitOK, errOut)
	if !strings.Contains(out, "Rust") || !strings.Contains(out, "rustls") || !strings.Contains(out, "feed 5") {
		t.Fatalf("table = %q", out)
	}
	if requests := log.snapshot(); len(requests) != 1 {
		t.Fatalf("requests = %d", len(requests))
	}
}

func TestSavedSearchAdd(t *testing.T) {
	code, out, errOut, log := runStub(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/saved-searches" || r.Method != http.MethodPost {
			http.NotFound(w, r)
			return
		}
		var body savedSearchBody
		decodeBody(t, r, &body)
		if body.Name != "Rust" || body.Query != "rustls" || body.Filters.SubscriptionID != 5 || body.Filters.Limit != 20 {
			t.Errorf("body = %+v", body)
		}
		writeJSON(w, savedSearch{ID: 7, Name: body.Name, Query: body.Query, Filters: body.Filters})
	}, testToken, "saved-searches", "add", "Rust", "rustls", "--feed", "5", "--limit", "20")
	requireCode(t, code, exitOK, errOut)
	if !strings.Contains(out, `saved search "Rust" recorded (id 7)`) {
		t.Fatalf("output = %q", out)
	}
	if requests := log.snapshot(); len(requests) != 1 {
		t.Fatalf("requests = %d", len(requests))
	}
}

func TestSavedSearchShow(t *testing.T) {
	code, out, errOut, _ := runStub(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/saved-searches/7":
			writeJSON(w, savedSearch{ID: 7, Name: "Rust", Query: "rustls", Filters: searchFilter{Limit: 20}})
		case "/v1/saved-searches/7/entries":
			writeJSON(w, searchPage{Matches: []searchMatch{{Kind: "entry", EntryID: 3, Title: "Rustls advisory", Snippet: "a CVE"}}, NextCursor: "next"})
		default:
			http.NotFound(w, r)
		}
	}, testToken, "saved-searches", "show", "7")
	requireCode(t, code, exitOK, errOut)
	for _, part := range []string{"Rust (id 7): rustls", "Rustls advisory", "--cursor next"} {
		if !strings.Contains(out, part) {
			t.Fatalf("output = %q missing %q", out, part)
		}
	}
}

func TestSavedSearchShowJSON(t *testing.T) {
	code, out, errOut, _ := runStub(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/saved-searches/7":
			writeJSON(w, savedSearch{ID: 7, Name: "Rust", Query: "rustls"})
		case "/v1/saved-searches/7/entries":
			writeJSON(w, searchPage{Matches: []searchMatch{{Kind: "entry", EntryID: 3, Title: "Rustls advisory"}}})
		default:
			http.NotFound(w, r)
		}
	}, testToken, "--json", "saved-searches", "show", "7")
	requireCode(t, code, exitOK, errOut)
	var decoded struct {
		Search  savedSearch   `json:"search"`
		Matches []searchMatch `json:"matches"`
	}
	if err := json.Unmarshal([]byte(out), &decoded); err != nil {
		t.Fatalf("not JSON: %v\n%s", err, out)
	}
	if decoded.Search.ID != 7 || len(decoded.Matches) != 1 || decoded.Matches[0].EntryID != 3 {
		t.Fatalf("decoded = %+v", decoded)
	}
}

func TestSavedSearchRenameAndRemove(t *testing.T) {
	code, _, errOut, log := runStub(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/v1/saved-searches/7" && r.Method == http.MethodPatch:
			var body savedSearchRename
			decodeBody(t, r, &body)
			if body.Name != "Rustls" {
				t.Errorf("body = %+v", body)
			}
			writeJSON(w, struct{}{})
		case r.URL.Path == "/v1/saved-searches/7" && r.Method == http.MethodDelete:
			writeJSON(w, struct{}{})
		default:
			http.NotFound(w, r)
		}
	}, testToken, "saved-searches", "rename", "7", "Rustls")
	requireCode(t, code, exitOK, errOut)

	code, _, errOut, log = runStub(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/saved-searches/7" || r.Method != http.MethodDelete {
			http.NotFound(w, r)
			return
		}
		writeJSON(w, struct{}{})
	}, testToken, "saved-searches", "remove", "7")
	requireCode(t, code, exitOK, errOut)
	if requests := log.snapshot(); len(requests) != 1 {
		t.Fatalf("requests = %d", len(requests))
	}
}
