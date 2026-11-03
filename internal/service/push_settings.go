package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/L-K-M/Earnest/internal/push"
	"github.com/L-K-M/Earnest/internal/store"
	"github.com/jackc/pgx/v5"
)

const (
	// ntfyTokenPurpose binds the ntfy token envelope to this column.
	ntfyTokenPurpose = "earnest/ntfy-token/v1"

	maxNtfyServerLength = 2048
	maxNtfyTopicLength  = 64
	maxNtfyTokenLength  = 256
	maxTimezoneLength   = 64
)

// PushSettings is the user's notification configuration. The stored ntfy
// token is never returned; TokenSet reports whether one is held.
type PushSettings struct {
	NtfyServer string `json:"ntfy_server,omitempty"`
	NtfyTopic  string `json:"ntfy_topic,omitempty"`
	TokenSet   bool   `json:"token_set"`
	ShowText   bool   `json:"show_text"`
	// QuietStart and QuietEnd are "HH:MM" wall-clock times in Timezone.
	// Either both are set or both are empty.
	QuietStart string `json:"quiet_start,omitempty"`
	QuietEnd   string `json:"quiet_end,omitempty"`
	Timezone   string `json:"timezone"`
	Rev        uint64 `json:"rev"`
}

// pushSettings is the worker-facing view: the one place the decrypted ntfy
// token appears. Public reads return PushSettings only.
type pushSettings struct {
	PushSettings
	NtfyToken string
}

// PushSettingsInput replaces every setting on PUT. NtfyToken nil keeps the
// stored token; a non-nil value replaces it, and an empty string clears it.
// Clearing the ntfy server also clears the stored token.
type PushSettingsInput struct {
	NtfyServer string
	NtfyTopic  string
	NtfyToken  *string
	ShowText   bool
	QuietStart string
	QuietEnd   string
	Timezone   string
	OpID       string
}

// GetPushSettings returns the user's settings without any secrets.
func (s *Service) GetPushSettings(ctx context.Context, a Actor) (PushSettings, error) {
	if err := s.deviceActor(ctx, a); err != nil {
		return PushSettings{}, err
	}
	var out PushSettings
	err := s.store.WithTx(ctx, func(tx pgx.Tx) error {
		ps, err := s.pushSettingsTx(ctx, tx, a.UserID)
		if err != nil {
			return err
		}
		out = ps.PushSettings
		return nil
	})
	return out, err
}

// PutPushSettings validates and stores the whole settings document,
// deduplicated by operation ID.
func (s *Service) PutPushSettings(ctx context.Context, a Actor, in PushSettingsInput) (PushSettings, error) {
	if err := s.deviceActor(ctx, a); err != nil {
		return PushSettings{}, err
	}
	if !validOpID(in.OpID) {
		return PushSettings{}, fmt.Errorf("op_id: %w", ErrInvalid)
	}
	server, err := ntfyServerURL(in.NtfyServer)
	if err != nil {
		return PushSettings{}, err
	}
	if err := validNtfyTopic(in.NtfyTopic); err != nil {
		return PushSettings{}, err
	}
	if (server == "") != (in.NtfyTopic == "") {
		return PushSettings{}, fmt.Errorf("ntfy server and topic must be set together: %w", ErrInvalid)
	}
	if in.NtfyToken != nil && len(*in.NtfyToken) > maxNtfyTokenLength {
		return PushSettings{}, fmt.Errorf("ntfy token: %w", ErrInvalid)
	}
	tz, err := validTimezone(in.Timezone)
	if err != nil {
		return PushSettings{}, err
	}
	if (in.QuietStart == "") != (in.QuietEnd == "") || (in.QuietStart != "" && (!validClock(in.QuietStart) || !validClock(in.QuietEnd))) {
		return PushSettings{}, fmt.Errorf("quiet hours must be a pair of HH:MM times: %w", ErrInvalid)
	}
	out := PushSettings{NtfyServer: server, NtfyTopic: in.NtfyTopic, ShowText: in.ShowText, QuietStart: in.QuietStart, QuietEnd: in.QuietEnd, Timezone: tz}
	err = s.store.WithTx(ctx, func(tx pgx.Tx) error {
		if err := store.LockCore(ctx, tx); err != nil {
			return err
		}
		replay, err := replayOp(ctx, tx, a.PrincipalID, "PutPushSettings", in.OpID)
		if err != nil {
			return err
		}
		if replay != nil {
			return json.Unmarshal(replay, &out)
		}
		var envelope []byte
		switch {
		case server == "":
			// Disabling ntfy also drops the stored token.
		case in.NtfyToken == nil:
			if err := tx.QueryRow(ctx, "SELECT ntfy_token_encrypted FROM push_settings WHERE user_id=$1", a.UserID).Scan(&envelope); err != nil && !errors.Is(err, pgx.ErrNoRows) {
				return err
			}
		case *in.NtfyToken == "":
			// An explicit empty token clears the stored one.
		default:
			var err error
			if envelope, err = s.sealSecret([]byte(*in.NtfyToken), ntfyTokenPurpose); err != nil {
				return err
			}
		}
		err = tx.QueryRow(ctx, `INSERT INTO push_settings(user_id,ntfy_server,ntfy_topic,ntfy_token_encrypted,quiet_start,quiet_end,timezone,show_text)
			VALUES($1,$2,$3,$4,NULLIF($5,'')::time,NULLIF($6,'')::time,$7,$8)
			ON CONFLICT (user_id) DO UPDATE SET ntfy_server=$2,ntfy_topic=$3,ntfy_token_encrypted=$4,
				quiet_start=NULLIF($5,'')::time,quiet_end=NULLIF($6,'')::time,timezone=$7,show_text=$8,rev=push_settings.rev+1
			RETURNING rev`,
			a.UserID, nullString(server), nullString(in.NtfyTopic), envelope, in.QuietStart, in.QuietEnd, tz, in.ShowText).Scan(&out.Rev)
		if err != nil {
			return domainError(err)
		}
		out.TokenSet = envelope != nil
		return store.FinishOp(ctx, tx, a.PrincipalID, in.OpID, out)
	})
	return out, err
}

// pushSettingsTx loads the row inside the caller's transaction and decrypts
// the ntfy token for the delivery worker. A user without a row gets the
// defaults, which keep notifications unheld.
func (s *Service) pushSettingsTx(ctx context.Context, tx pgx.Tx, user int64) (pushSettings, error) {
	var ps pushSettings
	var server, topic, quietStart, quietEnd *string
	var token []byte
	err := tx.QueryRow(ctx, `SELECT ntfy_server,ntfy_topic,ntfy_token_encrypted,
		to_char(quiet_start,'HH24:MI'),to_char(quiet_end,'HH24:MI'),timezone,show_text,rev
		FROM push_settings WHERE user_id=$1`, user).
		Scan(&server, &topic, &token, &quietStart, &quietEnd, &ps.Timezone, &ps.ShowText, &ps.Rev)
	if errors.Is(err, pgx.ErrNoRows) {
		return pushSettings{PushSettings: PushSettings{Timezone: "UTC"}}, nil
	}
	if err != nil {
		return pushSettings{}, fmt.Errorf("load push settings: %w", err)
	}
	ps.NtfyServer = value(server)
	ps.NtfyTopic = value(topic)
	ps.QuietStart = value(quietStart)
	ps.QuietEnd = value(quietEnd)
	if len(token) > 0 {
		plain, err := s.openSecret(token, ntfyTokenPurpose)
		if err != nil {
			return pushSettings{}, err
		}
		ps.NtfyToken = string(plain)
		ps.TokenSet = true
	}
	return ps, nil
}

// notificationDevicesTx loads every push destination for the user inside the
// caller's transaction: registered devices plus the ntfy endpoint from push
// settings. Subscription keys and the ntfy token are decrypted here, inside
// the delivery transaction, not on the way out.
func (s *Service) notificationDevicesTx(ctx context.Context, tx pgx.Tx, user int64) ([]push.Device, error) {
	out := []push.Device{}
	rows, err := tx.Query(ctx, "SELECT transport,endpoint,keys,show_text FROM devices WHERE user_id=$1 ORDER BY id", user)
	if err != nil {
		return nil, fmt.Errorf("list devices: %w", err)
	}
	for rows.Next() {
		var kind, endpoint string
		var keys []byte
		var d push.Device
		if err := rows.Scan(&kind, &endpoint, &keys, &d.ShowText); err != nil {
			rows.Close()
			return nil, err
		}
		d.Kind = push.Kind(kind)
		d.Endpoint = endpoint
		if len(keys) > 2 {
			p256dh, auth, err := s.openDeviceKeys(keys)
			if err != nil {
				rows.Close()
				return nil, err
			}
			d.P256DH, d.AuthSecret = p256dh, auth
		}
		out = append(out, d)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	ps, err := s.pushSettingsTx(ctx, tx, user)
	if err != nil {
		return nil, err
	}
	if ps.NtfyServer != "" && ps.NtfyTopic != "" {
		out = append(out, push.Device{
			Kind:       push.KindNtfy,
			Endpoint:   ps.NtfyServer + "/" + ps.NtfyTopic,
			Token:      ps.NtfyToken,
			ShowText:   ps.ShowText,
			SelfHosted: selfHostedNtfy(ps.NtfyServer),
		})
	}
	return out, nil
}

// quietUntil reports until when a notification accepted now is held. The
// zero time means send immediately: no quiet hours, outside the window, or
// an unresolvable timezone, which writes never let in. An overnight window
// (start after end) crosses midnight; equal endpoints hold nothing.
// Wall-clock ends are converted through the zone, so DST transitions land on
// the intended local time or the closest instant that exists.
func quietUntil(ps pushSettings, now time.Time) time.Time {
	if ps.QuietStart == "" || ps.QuietEnd == "" || ps.QuietStart == ps.QuietEnd {
		return time.Time{}
	}
	loc, err := time.LoadLocation(ps.Timezone)
	if err != nil {
		return time.Time{}
	}
	start := clockMinutes(ps.QuietStart)
	end := clockMinutes(ps.QuietEnd)
	local := now.In(loc)
	cur := local.Hour()*60 + local.Minute()
	day := time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, loc)
	if start < end {
		if cur >= start && cur < end {
			return quietEndAfter(day, end, now)
		}
		return time.Time{}
	}
	if cur >= start {
		return quietEndAfter(day.AddDate(0, 0, 1), end, now)
	}
	if cur < end {
		return quietEndAfter(day, end, now)
	}
	return time.Time{}
}

func clockMinutes(hhmm string) int {
	h, _ := strconv.Atoi(hhmm[:2])
	m, _ := strconv.Atoi(hhmm[3:])
	return h*60 + m
}

// A skipped wall-clock end means the first valid time after the gap.
// During a repeated hour choose an end still ahead of the receipt time.
func quietEndAfter(day time.Time, minutes int, now time.Time) time.Time {
	end := wallClock(day, minutes)
	if end.After(now) {
		return end
	}
	for candidate := end.Add(time.Minute); candidate.Before(end.Add(24 * time.Hour)); candidate = candidate.Add(time.Minute) {
		local := candidate.In(day.Location())
		if candidate.After(now) && sameLocalDay(local, day) && local.Hour()*60+local.Minute() >= minutes {
			return candidate
		}
	}
	return end
}
func sameLocalDay(a, b time.Time) bool {
	return a.Year() == b.Year() && a.Month() == b.Month() && a.Day() == b.Day()
}
func wallClock(day time.Time, minutes int) time.Time {
	loc := day.Location()
	nominal := time.Date(day.Year(), day.Month(), day.Day(), minutes/60, minutes%60, 0, 0, loc)
	if sameLocalDay(nominal, day) && nominal.Hour()*60+nominal.Minute() == minutes {
		return nominal
	}
	// time.Date may normalize a gap either forward or backward. Find the
	// earliest valid local minute after the requested wall-clock boundary.
	for candidate := nominal.Add(-24 * time.Hour); !candidate.After(nominal.Add(24 * time.Hour)); candidate = candidate.Add(time.Minute) {
		local := candidate.In(loc)
		if sameLocalDay(local, day) && local.Hour()*60+local.Minute() >= minutes {
			return candidate
		}
	}
	return nominal
}

// ntfyServerURL validates the configured server. http is allowed because a
// self-hosted ntfy commonly sits on the tailnet without TLS; the outbound
// client's address and port rules still apply at publish time.
func ntfyServerURL(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", nil
	}
	if len(raw) > maxNtfyServerLength {
		return "", fmt.Errorf("ntfy server: %w", ErrInvalid)
	}
	u, err := url.Parse(raw)
	if err != nil || u == nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return "", fmt.Errorf("ntfy server: %w", ErrInvalid)
	}
	u.Path = strings.TrimRight(u.Path, "/")
	return u.String(), nil
}

// validNtfyTopic restricts a topic to RFC 3986 unreserved characters so it
// always interpolates into the publish URL unchanged.
func validNtfyTopic(topic string) error {
	if topic == "" {
		return nil
	}
	if len(topic) > maxNtfyTopicLength {
		return fmt.Errorf("ntfy topic: %w", ErrInvalid)
	}
	for _, r := range topic {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
		case r == '-', r == '_', r == '.', r == '~':
		default:
			return fmt.Errorf("ntfy topic: %w", ErrInvalid)
		}
	}
	return nil
}

func validTimezone(tz string) (string, error) {
	if tz == "" {
		return "UTC", nil
	}
	if len(tz) > maxTimezoneLength {
		return "", fmt.Errorf("timezone: %w", ErrInvalid)
	}
	loc, err := time.LoadLocation(tz)
	if err != nil {
		return "", fmt.Errorf("timezone: %w", ErrInvalid)
	}
	return loc.String(), nil
}

// validClock accepts only strict "HH:MM" within the day.
func validClock(s string) bool {
	if len(s) != len("HH:MM") {
		return false
	}
	_, err := time.Parse("15:04", s)
	return err == nil
}

// selfHostedNtfy treats any configured server other than the public ntfy.sh
// service as one the user operates. The distinction decides whether agent
// text may travel this plaintext path; see push.Device.TextAllowed.
func selfHostedNtfy(server string) bool {
	u, err := url.Parse(server)
	if err != nil {
		return false
	}
	host := strings.ToLower(strings.TrimSuffix(u.Hostname(), "."))
	return host != "" && host != "ntfy.sh" && !strings.HasSuffix(host, ".ntfy.sh")
}

func nullString(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func value(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
