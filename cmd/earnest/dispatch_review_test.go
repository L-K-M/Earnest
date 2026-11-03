package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestBinaryLeadingJSONFlag(t *testing.T) {
	binary := filepath.Join(t.TempDir(), "earnest")
	build := exec.Command("go", "build", "-o", binary, ".")
	build.Env = append(os.Environ(), "CGO_ENABLED=0", "GOFLAGS=-p=2")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build: %v\n%s", err, out)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `{"user_id":1,"principal_id":2,"kind":"human","scopes":[]}`)
	}))
	defer server.Close()
	for _, flag := range []string{"--json", "-json"} {
		t.Run(flag, func(t *testing.T) {
			cmd := exec.Command(binary, flag, "whoami")
			cmd.Env = append(os.Environ(), "EARNEST_URL="+server.URL, "EARNEST_TOKEN=test-token", "XDG_CONFIG_HOME="+t.TempDir())
			out, err := cmd.CombinedOutput()
			if err != nil {
				t.Fatalf("leading flag: %v\n%s", err, out)
			}
			var me map[string]any
			if err := json.Unmarshal(out, &me); err != nil || me["principal_id"] != float64(2) {
				t.Fatalf("response=%s error=%v", strings.TrimSpace(string(out)), err)
			}
		})
	}
}
