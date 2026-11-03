package cli

import (
	"encoding/json"
	"net/http"
	"reflect"
	"testing"
)

func TestExtractJSONFlagPreservesLiteralArguments(t *testing.T) {
	for _, tc := range []struct {
		name string
		args []string
		want []string
		json bool
	}{
		{"text value", []string{"--json", "annotate", "1", "--text", "--json"}, []string{"annotate", "1", "--text", "--json"}, true},
		{"single dash value", []string{"annotate", "1", "-text", "-json"}, []string{"annotate", "1", "-text", "-json"}, false},
		{"inline value", []string{"annotate", "1", "--text=--json", "--json"}, []string{"annotate", "1", "--text=--json"}, true},
		{"terminator", []string{"search", "--", "--json", "-json", "--json"}, []string{"search", "--", "--json", "-json", "--json"}, false},
		{"flag before terminator", []string{"--json", "search", "--", "--json"}, []string{"search", "--", "--json"}, true},
		{"terminator as value", []string{"annotate", "1", "--text", "--", "--json"}, []string{"annotate", "1", "--text", "--"}, true},
		{"boolean flag", []string{"entries", "list", "--unread", "--json"}, []string{"entries", "list", "--unread"}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, jsonOut := extractJSONFlag(tc.args)
			if !reflect.DeepEqual(got, tc.want) || jsonOut != tc.json {
				t.Fatalf("args=%q json=%v, want args=%q json=%v", got, jsonOut, tc.want, tc.json)
			}
		})
	}

	for _, name := range []string{"text", "kind", "note", "entry", "category", "dedupe-key", "view", "feed", "folder", "limit", "cursor", "action", "principal", "since"} {
		t.Run("value flag/"+name, func(t *testing.T) {
			args := []string{"--" + name, "--json", "--json"}
			got, jsonOut := extractJSONFlag(args)
			if !reflect.DeepEqual(got, args[:2]) || !jsonOut {
				t.Fatalf("args=%q json=%v, want args=%q json=true", got, jsonOut, args[:2])
			}
		})
	}
}

func TestAnnotateLiteralJSONFlag(t *testing.T) {
	code, out, errOut, log := runStub(t, func(w http.ResponseWriter, r *http.Request) {
		var body annotationBody
		decodeBody(t, r, &body)
		if body.Value != "--json" || body.OpID == "" {
			t.Errorf("body=%+v", body)
		}
		writeJSON(w, annotation{ID: 1, Kind: "note", EntryIDs: []int64{1}, Value: body.Value, Rev: 2})
	}, testToken, "--json", "annotate", "1", "--text", "--json")
	requireCode(t, code, exitOK, errOut)
	if !json.Valid([]byte(out)) || len(log.snapshot()) != 1 {
		t.Fatalf("output=%q requests=%+v", out, log.snapshot())
	}
}

func TestSearchLiteralJSONFlagAfterTerminator(t *testing.T) {
	code, _, errOut, log := runStub(t, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, searchPage{})
	}, testToken, "search", "--", "--json")
	requireCode(t, code, exitOK, errOut)
	requests := log.snapshot()
	if len(requests) != 1 || requests[0].query.Get("q") != "--json" {
		t.Fatalf("requests=%+v, want query --json", requests)
	}
}
