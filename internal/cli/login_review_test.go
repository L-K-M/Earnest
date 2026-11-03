package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestJSONLoginEmitsApprovalBeforePolling(t *testing.T) {
	var out, errOut bytes.Buffer
	stubSleeps(t)
	sleepContext = func(_ context.Context, _ time.Duration) {
		var instructions map[string]any
		if err := json.Unmarshal(bytes.TrimSpace(out.Bytes()), &instructions); err != nil {
			t.Errorf("before polling, approval instructions missing: stdout=%q", out.String())
			return
		}
		if instructions["user_code"] != "ABCD-EFGH" || instructions["verification_uri"] != "https://reader.test/device" {
			t.Errorf("instructions=%v", instructions)
		}
		if strings.Contains(out.String(), "private-device-code") {
			t.Error("device credential leaked")
		}
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/devices/start" {
			writeJSON(w, deviceResult{DeviceCode: "private-device-code", UserCode: "ABCD-EFGH", VerificationURI: "https://reader.test/device", ExpiresIn: 600})
			return
		}
		deviceProblem(w, http.StatusBadRequest, deviceDenied)
	}))
	defer server.Close()
	t.Setenv("EARNEST_URL", server.URL)
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	requireCode(t, Run([]string{"login", "--json"}, &out, &errOut), exitFailure, errOut.String())
}
