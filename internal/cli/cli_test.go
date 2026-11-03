package cli

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestExtractJSONFlag(t *testing.T) {
	tests := []struct {
		name string
		in   []string
		out  []string
		json bool
	}{
		{name: "none", in: []string{"feeds", "list"}, out: []string{"feeds", "list"}},
		{name: "leading", in: []string{"--json", "feeds", "list"}, out: []string{"feeds", "list"}, json: true},
		{name: "trailing", in: []string{"feeds", "list", "--json"}, out: []string{"feeds", "list"}, json: true},
		{name: "single dash", in: []string{"-json", "whoami"}, out: []string{"whoami"}, json: true},
		{name: "repeated", in: []string{"--json", "read", "--json", "-json", "1"}, out: []string{"read", "1"}, json: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out, jsonOut := extractJSONFlag(tt.in)
			if jsonOut != tt.json {
				t.Fatalf("json = %v, want %v", jsonOut, tt.json)
			}
			if strings.Join(out, " ") != strings.Join(tt.out, " ") {
				t.Fatalf("args = %v, want %v", out, tt.out)
			}
		})
	}
}

func TestRunRepeatedJSONFlag(t *testing.T) {
	code, out, errOut, _ := runStub(t, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, meResult{UserID: 7, PrincipalID: 9, Kind: "human"})
	}, testToken, "--json", "whoami", "--json", "-json")
	requireCode(t, code, exitOK, errOut)
	if !json.Valid([]byte(out)) {
		t.Fatalf("invalid JSON: %q", out)
	}
}

func TestParseFlagsPreservesTerminator(t *testing.T) {
	for _, tc := range []struct {
		name  string
		args  []string
		want  []string
		limit int
	}{
		{"leading literal", []string{"--", "--limit"}, []string{"--limit"}, 10},
		{"leading value", []string{"--limit", "7", "--", "-publisher"}, []string{"-publisher"}, 7},
		{"intermixed", []string{"first", "--limit", "7", "--", "-second"}, []string{"first", "-second"}, 7},
		{"literal flags", []string{"--", "--limit", "7"}, []string{"--limit", "7"}, 10},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fs := newFlagSet("test")
			limit := fs.Int("limit", 10, "limit")
			if err := parseFlags(fs, tc.args, map[string]bool{"limit": true}); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(fs.Args(), tc.want) || *limit != tc.limit {
				t.Fatalf("args=%q limit=%d, want args=%q limit=%d", fs.Args(), *limit, tc.want, tc.limit)
			}
		})
	}
}

func TestSearchTerminator(t *testing.T) {
	for _, query := range []string{"-publisher", "--limit"} {
		t.Run(query, func(t *testing.T) {
			code, _, errOut, log := runStub(t, func(w http.ResponseWriter, r *http.Request) {
				writeJSON(w, searchPage{})
			}, testToken, "search", "--", query)
			requireCode(t, code, exitOK, errOut)
			requests := log.snapshot()
			if len(requests) != 1 || requests[0].query.Get("q") != query {
				t.Fatalf("requests=%+v, want query %q", requests, query)
			}
		})
	}
}

func TestNewOpID(t *testing.T) {
	seen := map[string]bool{}
	for range 100 {
		id, err := newOpID()
		if err != nil {
			t.Fatal(err)
		}
		if len(id) != 36 || strings.Count(id, "-") != 4 {
			t.Fatalf("op_id %q is not a canonical UUID", id)
		}
		if seen[id] {
			t.Fatalf("duplicate op_id %q", id)
		}
		seen[id] = true
		// Version 7 and RFC 4122 variant bits.
		if id[14] != '7' {
			t.Fatalf("op_id %q is not version 7", id)
		}
		if !strings.ContainsRune("89ab", rune(id[19])) {
			t.Fatalf("op_id %q has a non-RFC 4122 variant", id)
		}
	}
}

func TestViewFilter(t *testing.T) {
	tests := []struct {
		view viewFilter
		api  string
		ok   bool
	}{
		{viewToday, "river", true},
		{viewLater, "later", true},
		{viewKept, "kept", true},
		{viewDone, "done", true},
		{viewAll, "all", true},
		{viewFilter(""), "", false},
		{viewFilter("river"), "river", false},
		{viewFilter("bogus"), "bogus", false},
	}
	for _, tt := range tests {
		if got := tt.view.apiValue(); got != tt.api {
			t.Errorf("%q api value = %q, want %q", tt.view, got, tt.api)
		}
		if got := validView(tt.view); got != tt.ok {
			t.Errorf("%q valid = %v, want %v", tt.view, got, tt.ok)
		}
	}
}

func TestStateActionRegisterValue(t *testing.T) {
	tests := []struct {
		action stateAction
		reg    string
		value  any
	}{
		{actionDone, "shelf", "done"},
		{actionLater, "shelf", "later"},
		{actionKeep, "kept", true},
		{actionUnread, "seen", "unseen"},
		{stateAction("bogus"), "", nil},
	}
	for _, tt := range tests {
		reg, value := tt.action.registerValue()
		if reg != tt.reg || value != tt.value {
			t.Errorf("%q = (%q, %v), want (%q, %v)", tt.action, reg, value, tt.reg, tt.value)
		}
	}
}

func TestMaskToken(t *testing.T) {
	tests := []struct {
		name  string
		token string
		want  string
	}{
		{"empty", "", "********"},
		{"short", "abc", "********"},
		{"long", "earnest_app_0123456789", "earn…6789"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := maskToken(tt.token)
			if got == tt.token && tt.token != "" {
				t.Fatalf("mask %q returned the token unchanged", tt.token)
			}
			if tt.want != "" && got != tt.want {
				t.Fatalf("mask = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestBudgetText(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want string
	}{
		{"absent", "", ""},
		{"fields", `{"used_today":3,"limit_today":5}`, "used 3/5"},
		{"partial", `{"limit_today":2}`, "used 0/2"},
		{"other shape", `{"reason":"rate_limit"}`, `{"reason":"rate_limit"}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := budgetText(json.RawMessage(tt.raw)); got != tt.want {
				t.Fatalf("budgetText(%s) = %q, want %q", tt.raw, got, tt.want)
			}
		})
	}
}

func TestAPIErrorMessage(t *testing.T) {
	budget, _ := json.Marshal(map[string]int{"used_today": 3, "limit_today": 3})
	tests := []struct {
		name    string
		err     *apiError
		want    string
		contain []string
	}{
		{
			name: "not found with detail",
			err:  &apiError{status: 404, problem: problem{Title: "Not Found", Detail: "subscription 9"}},
			want: "404 Not Found: subscription 9",
		},
		{
			name:    "rate limit shows budget",
			err:     &apiError{status: 429, problem: problem{Title: "Too Many Requests", Budget: budget}},
			contain: []string{"429 Too Many Requests", "budget used 3/3"},
		},
		{
			name:    "retry after",
			err:     &apiError{status: 429, retryAfter: "42", problem: problem{Title: "Too Many Requests"}},
			contain: []string{"retry after 42"},
		},
		{
			name: "fallback without problem",
			err:  &apiError{status: 500, problem: problem{Title: "Internal Server Error"}},
			want: "500 Internal Server Error",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.err.Error()
			if tt.want != "" && got != tt.want {
				t.Fatalf("message = %q, want %q", got, tt.want)
			}
			for _, part := range tt.contain {
				if !strings.Contains(got, part) {
					t.Fatalf("message %q does not contain %q", got, part)
				}
			}
		})
	}
}

func TestProblemErrorWithoutTitle(t *testing.T) {
	for _, tc := range []struct {
		name     string
		body     string
		wantType string
	}{
		{"type", `{"type":"/problems/budget","detail":"daily limit","budget":{"used_today":3,"limit_today":3}}`, "/problems/budget"},
		{"status", `{"status":429,"detail":"daily limit","budget":{"used_today":3,"limit_today":3}}`, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := problemError(http.StatusTooManyRequests, "42", []byte(tc.body)).(*apiError)
			if err.problem.Type != tc.wantType || err.problem.Detail != "daily limit" || err.problem.Status != http.StatusTooManyRequests {
				t.Fatalf("problem fields lost: %+v", err.problem)
			}
			for _, part := range []string{"429 Too Many Requests", "daily limit", "budget used 3/3", "retry after 42"} {
				if !strings.Contains(err.Error(), part) {
					t.Errorf("message=%q, want %q", err.Error(), part)
				}
			}
		})
	}
}

func TestResolveServerAndTokenPrecedence(t *testing.T) {
	home := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", home)
	cfg := config{URL: "http://file:8080", Token: "file-token"}
	if err := saveConfig(cfg); err != nil {
		t.Fatal(err)
	}
	if got := resolveServer(cfg); got != "http://file:8080" {
		t.Fatalf("file url = %q", got)
	}
	if got := resolveToken(cfg); got != "file-token" {
		t.Fatalf("file token = %q", got)
	}
	t.Setenv("EARNEST_URL", "http://env:9090/")
	t.Setenv("EARNEST_TOKEN", "env-token")
	if got := resolveServer(cfg); got != "http://env:9090" {
		t.Fatalf("env url = %q", got)
	}
	if got := resolveToken(cfg); got != "env-token" {
		t.Fatalf("env token = %q", got)
	}
}

func TestSaveConfigPermissions(t *testing.T) {
	home := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", home)
	if err := saveConfig(config{URL: "http://localhost:8080", Token: "secret"}); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(home, "earnest", "config.json")
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if mode := info.Mode().Perm(); mode != 0o600 {
		t.Fatalf("config mode = %o, want 600", mode)
	}
	dirInfo, err := os.Stat(filepath.Dir(path))
	if err != nil {
		t.Fatal(err)
	}
	if mode := dirInfo.Mode().Perm(); mode != 0o700 {
		t.Fatalf("config dir mode = %o, want 700", mode)
	}
}

func TestConfigPathFallsBackToHome(t *testing.T) {
	home := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("HOME", home)
	path, err := configPath()
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(home, ".config", "earnest", "config.json")
	if path != want {
		t.Fatalf("config path = %q, want %q", path, want)
	}
}

func TestRunUsageErrors(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("EARNEST_TOKEN", "")
	t.Setenv("EARNEST_URL", "")
	var out, errOut strings.Builder
	if code := Run(nil, &out, &errOut); code != exitUsage {
		t.Fatalf("no args exit = %d, want %d", code, exitUsage)
	}
	if !strings.Contains(errOut.String(), "usage: earnest") {
		t.Fatalf("usage not printed: %q", errOut.String())
	}
	out.Reset()
	errOut.Reset()
	if code := Run([]string{"bogus"}, &out, &errOut); code != exitUsage {
		t.Fatalf("unknown command exit = %d, want %d", code, exitUsage)
	}
}
