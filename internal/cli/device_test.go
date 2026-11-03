package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// stubSleeps replaces the polling wait with a recorder and freezes the clock.
func stubSleeps(t *testing.T) *[]time.Duration {
	t.Helper()
	durations := &[]time.Duration{}
	oldSleep, oldNow := sleepContext, nowFunc
	sleepContext = func(_ context.Context, d time.Duration) { *durations = append(*durations, d) }
	nowFunc = func() time.Time { return time.Date(2026, 11, 2, 12, 0, 0, 0, time.UTC) }
	t.Cleanup(func() { sleepContext, nowFunc = oldSleep, oldNow })
	return durations
}

func deviceProblem(w http.ResponseWriter, status int, code string) {
	w.Header().Set("Content-Type", "application/problem+json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(problem{
		Type:      "/problems/" + code,
		Title:     http.StatusText(status),
		Status:    status,
		ErrorCode: code,
		Detail:    code,
	})
}

func TestLoginDeviceFlowSlowDown(t *testing.T) {
	var mu sync.Mutex
	polls := 0
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/devices/start":
			_ = json.NewEncoder(w).Encode(deviceResult{
				DeviceCode:      "device-code",
				UserCode:        "ABCD-EFGH",
				VerificationURI: srv.URL + "/device",
				Interval:        0,
				ExpiresIn:       600,
			})
		case "/v1/devices/poll":
			mu.Lock()
			polls++
			pollsCopy := polls
			mu.Unlock()
			switch pollsCopy {
			case 1:
				deviceProblem(w, http.StatusBadRequest, devicePending)
			case 2:
				deviceProblem(w, http.StatusBadRequest, deviceSlowDown)
			default:
				_ = json.NewEncoder(w).Encode(deviceToken{AccessToken: "earnest_app_supersecret", TokenType: "Bearer"})
			}
		case "/v1/me":
			_ = json.NewEncoder(w).Encode(meResult{UserID: 7, PrincipalID: 9, Kind: "human"})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	durations := stubSleeps(t)
	code, out, errOut := runWithServer(t, srv, "", "login")
	if code != exitOK {
		t.Fatalf("exit = %d, want 0; stderr: %s", code, errOut)
	}
	if !bytes.Contains([]byte(out), []byte("ABCD-EFGH")) || !bytes.Contains([]byte(out), []byte("/device")) {
		t.Fatalf("user code or verification URL missing: %q", out)
	}
	if bytes.Contains([]byte(out), []byte("earnest_app_supersecret")) {
		t.Fatalf("login printed the token: %q", out)
	}
	wantDurations := []time.Duration{time.Second, time.Second, 6 * time.Second}
	if len(*durations) != len(wantDurations) {
		t.Fatalf("sleeps = %v, want %v", *durations, wantDurations)
	}
	for i := range wantDurations {
		if (*durations)[i] != wantDurations[i] {
			t.Fatalf("sleeps = %v, want %v", *durations, wantDurations)
		}
	}
	path := filepath.Join(os.Getenv("XDG_CONFIG_HOME"), "earnest", "config.json")
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if mode := info.Mode().Perm(); mode != 0o600 {
		t.Fatalf("config mode = %o, want 600", mode)
	}
	cfg, err := loadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Token != "earnest_app_supersecret" {
		t.Fatalf("stored token = %q", cfg.Token)
	}
	if cfg.URL != srv.URL {
		t.Fatalf("stored url = %q, want %q", cfg.URL, srv.URL)
	}
}

func TestLoginJSONOmitsToken(t *testing.T) {
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/devices/start":
			_ = json.NewEncoder(w).Encode(deviceResult{DeviceCode: "dc", UserCode: "ABCD-EFGH", VerificationURI: srv.URL + "/device", Interval: 0, ExpiresIn: 600})
		case "/v1/devices/poll":
			_ = json.NewEncoder(w).Encode(deviceToken{AccessToken: "earnest_app_jsonsecret", TokenType: "Bearer"})
		case "/v1/me":
			_ = json.NewEncoder(w).Encode(meResult{UserID: 7, PrincipalID: 9, Kind: "human"})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	stubSleeps(t)
	code, out, errOut := runWithServer(t, srv, "", "--json", "login")
	requireCode(t, code, exitOK, errOut)
	if strings.Contains(out, "earnest_app_jsonsecret") {
		t.Fatalf("login JSON leaked the token: %q", out)
	}
	var decoded struct {
		Server string `json:"server"`
		UserID int64  `json:"user_id"`
		Kind   string `json:"kind"`
	}
	if err := json.Unmarshal([]byte(out), &decoded); err != nil {
		t.Fatalf("not JSON: %v\n%s", err, out)
	}
	if decoded.Server != srv.URL || decoded.UserID != 7 || decoded.Kind != "human" {
		t.Fatalf("decoded = %+v", decoded)
	}
}

func TestLoginDenied(t *testing.T) {
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/devices/start":
			_ = json.NewEncoder(w).Encode(deviceResult{DeviceCode: "dc", UserCode: "ABCD-EFGH", VerificationURI: srv.URL + "/device", Interval: 0, ExpiresIn: 600})
		case "/v1/devices/poll":
			deviceProblem(w, http.StatusBadRequest, deviceDenied)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	stubSleeps(t)
	code, _, errOut := runWithServer(t, srv, "", "login")
	if code != exitFailure {
		t.Fatalf("exit = %d, want %d", code, exitFailure)
	}
	if !bytes.Contains([]byte(errOut), []byte("denied")) {
		t.Fatalf("stderr = %q, want denial", errOut)
	}
	if _, err := os.Stat(filepath.Join(os.Getenv("XDG_CONFIG_HOME"), "earnest", "config.json")); !os.IsNotExist(err) {
		t.Fatalf("config written despite denial: %v", err)
	}
}

func TestLoginExpired(t *testing.T) {
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/devices/start":
			_ = json.NewEncoder(w).Encode(deviceResult{DeviceCode: "dc", UserCode: "ABCD-EFGH", VerificationURI: srv.URL + "/device", Interval: 0, ExpiresIn: 600})
		case "/v1/devices/poll":
			deviceProblem(w, http.StatusBadRequest, deviceExpired)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	stubSleeps(t)
	code, _, errOut := runWithServer(t, srv, "", "login")
	if code != exitFailure {
		t.Fatalf("exit = %d, want %d", code, exitFailure)
	}
	if !bytes.Contains([]byte(errOut), []byte("expired")) {
		t.Fatalf("stderr = %q, want expiry", errOut)
	}
}

func TestLoginUnexpectedProblem(t *testing.T) {
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/devices/start":
			_ = json.NewEncoder(w).Encode(deviceResult{DeviceCode: "dc", UserCode: "ABCD-EFGH", VerificationURI: srv.URL + "/device", Interval: 0, ExpiresIn: 600})
		case "/v1/devices/poll":
			deviceProblem(w, http.StatusInternalServerError, "internal-error")
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	stubSleeps(t)
	code, _, errOut := runWithServer(t, srv, "", "login")
	if code != exitFailure {
		t.Fatalf("exit = %d, want %d", code, exitFailure)
	}
	if !bytes.Contains([]byte(errOut), []byte("500")) {
		t.Fatalf("stderr = %q, want server status", errOut)
	}
}

func TestLogoutRemovesConfig(t *testing.T) {
	home := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", home)
	t.Setenv("EARNEST_TOKEN", "")
	if err := saveConfig(config{URL: "http://localhost:8080", Token: "secret"}); err != nil {
		t.Fatal(err)
	}
	var out, errOut bytes.Buffer
	if code := Run([]string{"logout"}, &out, &errOut); code != exitOK {
		t.Fatalf("exit = %d, want 0; stderr %s", code, errOut.String())
	}
	if _, err := os.Stat(filepath.Join(home, "earnest", "config.json")); !os.IsNotExist(err) {
		t.Fatalf("config still present: %v", err)
	}
}

func runWithServer(t *testing.T, srv *httptest.Server, token string, args ...string) (int, string, string) {
	t.Helper()
	t.Setenv("EARNEST_URL", srv.URL)
	t.Setenv("EARNEST_TOKEN", token)
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	var out, errOut bytes.Buffer
	code := Run(args, &out, &errOut)
	return code, out.String(), errOut.String()
}
