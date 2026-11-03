package cli

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"
)

const (
	clientName            = "earnest-cli"
	defaultPollInterval   = 5 * time.Second
	minPollInterval       = time.Second
	maxPollInterval       = 5 * time.Minute
	slowDownIncrement     = 5 * time.Second
	defaultDeviceLifetime = 10 * time.Minute
)

// RFC 8628 polling statuses the API returns as problem bodies.
const (
	devicePending  = "authorization_pending"
	deviceSlowDown = "slow_down"
	deviceDenied   = "access_denied"
	deviceExpired  = "expired_token"
)

func (a *app) login(ctx context.Context, args []string) error {
	fs := newFlagSet("login")
	if err := fs.Parse(args); err != nil {
		return usagef("usage: earnest login")
	}
	if fs.NArg() != 0 {
		return usagef("usage: earnest login")
	}
	cfg, err := loadConfig()
	if err != nil {
		return err
	}
	server := resolveServer(cfg)
	c := newClient(server, "")

	var started deviceResult
	raw, err := c.send(ctx, http.MethodPost, "/v1/devices/start", nil, deviceStart{ClientName: clientName + "/" + Version})
	if err != nil {
		return fmt.Errorf("start device login: %w", err)
	}
	if err := decode(raw, &started); err != nil {
		return fmt.Errorf("start device login: %w", err)
	}
	if a.json {
		if err := a.encodeJSON(struct {
			VerificationURI string `json:"verification_uri"`
			UserCode        string `json:"user_code"`
			ExpiresIn       int64  `json:"expires_in"`
			Interval        int64  `json:"interval"`
		}{started.VerificationURI, started.UserCode, started.ExpiresIn, started.Interval}); err != nil {
			return err
		}
	} else {
		fmt.Fprintf(a.out, "Open %s and enter code %s\n", started.VerificationURI, started.UserCode)
		fmt.Fprintln(a.out, "Waiting for approval…")
	}

	token, err := pollDevice(ctx, c, started)
	if err != nil {
		return err
	}
	cfg.URL = server
	cfg.Token = token
	if err := saveConfig(cfg); err != nil {
		return err
	}

	// Confirm the token works and show who it belongs to; a failure here does
	// not undo the stored login.
	c.token = token
	var me meResult
	if raw, err := c.get(ctx, "/v1/me", nil); err == nil {
		_ = decode(raw, &me)
	}
	if a.json {
		return a.encodeJSON(struct {
			Server      string `json:"server"`
			UserID      int64  `json:"user_id,omitempty"`
			PrincipalID int64  `json:"principal_id,omitempty"`
			Kind        string `json:"kind,omitempty"`
		}{Server: server, UserID: me.UserID, PrincipalID: me.PrincipalID, Kind: me.Kind})
	}
	fmt.Fprintln(a.out, "Logged in.")
	if me.Kind != "" {
		fmt.Fprintf(a.out, "user %d (%s), principal %d\n", me.UserID, me.Kind, me.PrincipalID)
	}
	return nil
}

// pollDevice waits with the server's interval, honours slow_down by backing
// off five seconds, and stops at denial, expiry or context cancellation.
func pollDevice(ctx context.Context, c *client, started deviceResult) (string, error) {
	interval := time.Duration(started.Interval) * time.Second
	if interval < minPollInterval {
		interval = minPollInterval
	}
	if interval > maxPollInterval {
		interval = maxPollInterval
	}
	lifetime := time.Duration(started.ExpiresIn) * time.Second
	if lifetime <= 0 {
		lifetime = defaultDeviceLifetime
	}
	deadline := nowFunc().Add(lifetime)

	for {
		sleepContext(ctx, interval)
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		if !nowFunc().Before(deadline) {
			return "", errors.New("device code expired before approval")
		}
		var tok deviceToken
		raw, err := c.send(ctx, http.MethodPost, "/v1/devices/poll", nil, devicePoll{DeviceCode: started.DeviceCode})
		if err == nil {
			if decodeErr := decode(raw, &tok); decodeErr != nil {
				return "", fmt.Errorf("decode device token: %w", decodeErr)
			}
			if tok.AccessToken == "" {
				return "", errors.New("server returned an empty access token")
			}
			return tok.AccessToken, nil
		}
		var apiErr *apiError
		if !errors.As(err, &apiErr) {
			return "", err
		}
		switch apiErr.problem.ErrorCode {
		case devicePending:
			continue
		case deviceSlowDown:
			interval += slowDownIncrement
			if interval > maxPollInterval {
				interval = maxPollInterval
			}
		case deviceDenied:
			return "", errors.New("device authorization was denied")
		case deviceExpired:
			return "", errors.New("device code expired")
		default:
			return "", err
		}
	}
}

func (a *app) logout(ctx context.Context, args []string) error {
	fs := newFlagSet("logout")
	if err := fs.Parse(args); err != nil || fs.NArg() != 0 {
		return usagef("usage: earnest logout")
	}
	path, err := configPath()
	if err != nil {
		return err
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("remove config: %w", err)
	}
	if a.json {
		return a.encodeJSON(map[string]bool{"logged_out": true})
	}
	fmt.Fprintln(a.out, "Logged out.")
	if os.Getenv("EARNEST_TOKEN") != "" {
		fmt.Fprintln(a.errOut, "note: EARNEST_TOKEN is still set; unset it to end the session")
	}
	return nil
}

func (a *app) whoami(ctx context.Context, args []string) error {
	fs := newFlagSet("whoami")
	if err := fs.Parse(args); err != nil || fs.NArg() != 0 {
		return usagef("usage: earnest whoami")
	}
	c, err := a.client()
	if err != nil {
		return err
	}
	raw, err := c.get(ctx, "/v1/me", nil)
	if err != nil {
		return err
	}
	var me meResult
	if err := decode(raw, &me); err != nil {
		return err
	}
	if a.json {
		return a.encodeJSON(struct {
			UserID      int64    `json:"user_id"`
			PrincipalID int64    `json:"principal_id"`
			Kind        string   `json:"kind"`
			Scopes      []string `json:"scopes"`
			Server      string   `json:"server"`
			Token       string   `json:"token"`
		}{
			UserID:      me.UserID,
			PrincipalID: me.PrincipalID,
			Kind:        me.Kind,
			Scopes:      me.Scopes,
			Server:      c.baseURL,
			Token:       maskToken(c.token),
		})
	}
	fmt.Fprintf(a.out, "user %d (%s), principal %d\n", me.UserID, me.Kind, me.PrincipalID)
	fmt.Fprintf(a.out, "server: %s\n", c.baseURL)
	fmt.Fprintf(a.out, "token: %s\n", maskToken(c.token))
	if len(me.Scopes) > 0 {
		fmt.Fprintf(a.out, "scopes: %s\n", strings.Join(me.Scopes, " "))
	}
	return nil
}
