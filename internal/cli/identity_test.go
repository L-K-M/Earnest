package cli

import (
	"bytes"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

func TestWhoamiHuman(t *testing.T) {
	code, out, errOut, _ := runStub(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/me" || r.Method != http.MethodGet {
			http.NotFound(w, r)
			return
		}
		writeJSON(w, meResult{UserID: 7, PrincipalID: 9, Kind: "human", Scopes: []string{"entries:read", "feeds:write"}})
	}, testToken, "whoami")
	requireCode(t, code, exitOK, errOut)
	if !strings.Contains(out, "user 7 (human), principal 9") {
		t.Fatalf("output = %q", out)
	}
	if strings.Contains(out, testToken) {
		t.Fatalf("whoami printed the token: %q", out)
	}
	if !strings.Contains(out, "entries:read") {
		t.Fatalf("output = %q", out)
	}
}

func TestWhoamiMasksToken(t *testing.T) {
	token := "earnest_agent_abcd1234"
	code, out, errOut, _ := runStub(t, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, meResult{UserID: 7, PrincipalID: 10, Kind: "agent"})
	}, token, "--json", "whoami")
	requireCode(t, code, exitOK, errOut)
	if strings.Contains(out, token) {
		t.Fatalf("token leaked into JSON: %q", out)
	}
	var decoded struct {
		Token  string `json:"token"`
		Kind   string `json:"kind"`
		Server string `json:"server"`
	}
	if err := json.Unmarshal([]byte(out), &decoded); err != nil {
		t.Fatalf("not JSON: %v\n%s", err, out)
	}
	if decoded.Kind != "agent" || decoded.Server == "" {
		t.Fatalf("decoded = %+v", decoded)
	}
	if !strings.Contains(decoded.Token, "…") || len(decoded.Token) >= len(token) {
		t.Fatalf("token not masked: %q", decoded.Token)
	}
}

func TestNotLoggedIn(t *testing.T) {
	t.Setenv("EARNEST_URL", "")
	t.Setenv("EARNEST_TOKEN", "")
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	var out, errOut bytes.Buffer
	code := Run([]string{"whoami"}, &out, &errOut)
	requireCode(t, code, exitFailure, errOut.String())
	if !strings.Contains(errOut.String(), "not logged in") {
		t.Fatalf("stderr = %q", errOut.String())
	}
}

func TestLogoutJSON(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("EARNEST_TOKEN", "")
	var out, errOut bytes.Buffer
	if code := Run([]string{"--json", "logout"}, &out, &errOut); code != exitOK {
		t.Fatalf("exit = %d, stderr %s", code, errOut.String())
	}
	var decoded map[string]bool
	if err := json.Unmarshal(out.Bytes(), &decoded); err != nil {
		t.Fatalf("not JSON: %v\n%s", err, out.String())
	}
	if !decoded["logged_out"] {
		t.Fatalf("decoded = %v", decoded)
	}
}

func TestLogoutWithoutConfig(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("EARNEST_TOKEN", "")
	var out, errOut bytes.Buffer
	if code := Run([]string{"logout"}, &out, &errOut); code != exitOK {
		t.Fatalf("exit = %d, stderr %s", code, errOut.String())
	}
	if !strings.Contains(out.String(), "Logged out") {
		t.Fatalf("output = %q", out.String())
	}
}
