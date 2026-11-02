package config_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/L-K-M/Earnest/internal/config"
)

func TestPushConfig(t *testing.T) {
	for _, tc := range []struct {
		name    string
		env     map[string]string
		invalid bool
	}{
		{"defaults", nil, false},
		{"explicit VAPID", map[string]string{"EARNEST_PUSH_VAPID_PRIVATE_KEY": "private", "EARNEST_PUSH_VAPID_PUBLIC_KEY": "public", "EARNEST_PUSH_VAPID_SUBJECT": "mailto:reader@example.com"}, false},
		{"persisted VAPID", map[string]string{"EARNEST_PUSH_VAPID_KEY_FILE": "/data/vapid.json"}, false},
		{"partial VAPID", map[string]string{"EARNEST_PUSH_VAPID_PRIVATE_KEY": "private"}, true},
		{"conflicting VAPID sources", map[string]string{"EARNEST_PUSH_VAPID_PRIVATE_KEY": "private", "EARNEST_PUSH_VAPID_PUBLIC_KEY": "public", "EARNEST_PUSH_VAPID_KEY_FILE": "/data/vapid.json"}, true},
		{"partial APNs", map[string]string{"EARNEST_APNS_KEY_ID": "key"}, true},
		{"APNs", map[string]string{"EARNEST_APNS_PRIVATE_KEY": "private", "EARNEST_APNS_KEY_ID": "key", "EARNEST_APNS_TEAM_ID": "team", "EARNEST_APNS_SANDBOX": "true"}, false},
		{"invalid boolean", map[string]string{"EARNEST_APNS_SANDBOX": "maybe"}, true},
		{"sandbox requires APNs", map[string]string{"EARNEST_APNS_SANDBOX": "true"}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			setEnv(t, tc.env)
			cfg, err := config.Load()
			if (err != nil) != tc.invalid {
				t.Fatalf("Load error=%v invalid=%v", err, tc.invalid)
			}
			if err != nil {
				return
			}
			if cfg.PushVAPIDPublicKey != tc.env["EARNEST_PUSH_VAPID_PUBLIC_KEY"] || string(cfg.PushVAPIDPrivateKey) != tc.env["EARNEST_PUSH_VAPID_PRIVATE_KEY"] || string(cfg.APNsPrivateKey) != tc.env["EARNEST_APNS_PRIVATE_KEY"] {
				t.Fatal("push configuration not retained")
			}
		})
	}
}
func TestPushSecretsRedacted(t *testing.T) {
	setEnv(t, map[string]string{"EARNEST_PUSH_VAPID_PRIVATE_KEY": "secret-vapid", "EARNEST_PUSH_VAPID_PUBLIC_KEY": "public", "EARNEST_APNS_PRIVATE_KEY": "secret-apns", "EARNEST_APNS_KEY_ID": "key", "EARNEST_APNS_TEAM_ID": "team"})
	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	for _, logged := range []string{cfg.String(), cfg.GoString(), cfg.LogValue().String(), fmt.Sprintf("%+v", cfg), fmt.Sprintf("%#v", cfg)} {
		if strings.Contains(logged, "secret-vapid") || strings.Contains(logged, "secret-apns") {
			t.Fatal("push key exposed")
		}
	}
}
