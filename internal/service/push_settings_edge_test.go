package service

import (
	"encoding/base64"
	"errors"
	"testing"
	"time"
)

func TestQuietHoursTransitionBoundary(t *testing.T) {
	for _, tc := range []struct{ name, now, end, want string }{
		{"spring_gap", "2026-03-08T06:45:00Z", "02:30", "2026-03-08T07:00:00Z"},
		{"second_autumn_hour", "2026-11-01T06:15:00Z", "01:30", "2026-11-01T06:30:00Z"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			now, err := time.Parse(time.RFC3339, tc.now)
			if err != nil {
				t.Fatal(err)
			}
			got := quietUntil(pushSettings{PushSettings: PushSettings{Timezone: "America/New_York", QuietStart: "22:00", QuietEnd: tc.end}}, now)
			if got.UTC().Format(time.RFC3339) != tc.want {
				t.Fatalf("quiet boundary got %s want %s", got, tc.want)
			}
		})
	}
}
func TestDeviceRejectsInvalidCurvePoint(t *testing.T) {
	invalid := base64.RawURLEncoding.EncodeToString(append([]byte{4}, make([]byte, 64)...))
	if err := subscriptionMaterial(invalid, testAuth); !errors.Is(err, ErrInvalid) {
		t.Fatal("invalid point accepted", err)
	}
}
func TestDeviceRejectsLocalHostnames(t *testing.T) {
	for _, host := range []string{"localhost", "localhost.", "relay.localhost", "relay.local", "router.home.arpa"} {
		if _, err := pushEndpoint("https://" + host + "/endpoint"); !errors.Is(err, ErrInvalid) {
			t.Fatalf("local hostname %s accepted: %v", host, err)
		}
	}
}

func TestPublicNtfyTextBoundary(t *testing.T) {
	for _, server := range []string{"https://ntfy.sh", "https://NTFY.SH.", "https://relay.ntfy.sh.", ""} {
		if selfHostedNtfy(server) {
			t.Fatalf("public ntfy classified as user-operated: %s", server)
		}
	}
}
