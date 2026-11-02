package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

const (
	envPushVAPIDPrivateKey = "EARNEST_PUSH_VAPID_PRIVATE_KEY"
	envPushVAPIDPublicKey  = "EARNEST_PUSH_VAPID_PUBLIC_KEY"
	envPushVAPIDSubject    = "EARNEST_PUSH_VAPID_SUBJECT"
	envPushVAPIDKeyFile    = "EARNEST_PUSH_VAPID_KEY_FILE"
	envAPNsPrivateKey      = "EARNEST_APNS_PRIVATE_KEY"
	envAPNsKeyID           = "EARNEST_APNS_KEY_ID"
	envAPNsTeamID          = "EARNEST_APNS_TEAM_ID"
	envAPNsSandbox         = "EARNEST_APNS_SANDBOX"
)

func (c *Config) loadPush() error {
	if key := os.Getenv(envPushVAPIDPrivateKey); key != "" {
		c.PushVAPIDPrivateKey = []byte(key)
	}
	c.PushVAPIDPublicKey = envOr(envPushVAPIDPublicKey, "")
	c.PushVAPIDSubject = envOr(envPushVAPIDSubject, "")
	c.PushVAPIDKeyFile = envOr(envPushVAPIDKeyFile, "")
	if (len(c.PushVAPIDPrivateKey) == 0) != (c.PushVAPIDPublicKey == "") {
		return fmt.Errorf("%s and %s must be set together", envPushVAPIDPrivateKey, envPushVAPIDPublicKey)
	}
	if len(c.PushVAPIDPrivateKey) > 0 && c.PushVAPIDKeyFile != "" {
		return fmt.Errorf("%s cannot be combined with explicit VAPID keys", envPushVAPIDKeyFile)
	}
	if key := os.Getenv(envAPNsPrivateKey); key != "" {
		c.APNsPrivateKey = []byte(key)
	}
	c.APNsKeyID = envOr(envAPNsKeyID, "")
	c.APNsTeamID = envOr(envAPNsTeamID, "")
	configured := 0
	for _, set := range []bool{len(c.APNsPrivateKey) > 0, c.APNsKeyID != "", c.APNsTeamID != ""} {
		if set {
			configured++
		}
	}
	if configured != 0 && configured != 3 {
		return fmt.Errorf("%s, %s and %s must be set together", envAPNsPrivateKey, envAPNsKeyID, envAPNsTeamID)
	}
	raw := strings.TrimSpace(os.Getenv(envAPNsSandbox))
	if raw == "" {
		return nil
	}
	sandbox, err := strconv.ParseBool(raw)
	if err != nil {
		return fmt.Errorf("%s must be a boolean", envAPNsSandbox)
	}
	if sandbox && configured == 0 {
		return fmt.Errorf("%s requires APNs key settings", envAPNsSandbox)
	}
	c.APNsSandbox = sandbox
	return nil
}
