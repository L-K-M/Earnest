package service

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"math/big"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

var (
	ErrAuthorizationPending = errors.New("authorization_pending")
	ErrSlowDown             = errors.New("slow_down")
	ErrAccessDenied         = errors.New("access_denied")
	ErrExpiredToken         = errors.New("expired_token")
)

const deviceLifetime = 10 * time.Minute
const deviceInterval = 5
const userCodeAlphabet = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789"

// Device starts are public and unauthenticated, so they are capped per client
// key. The cap is generous for a household and small enough that a flood
// cannot grow the device_codes table without bound.
const deviceStartLimit = 10
const deviceStartWindow = 15 * time.Minute

// DeviceAuthorization gives a native client its codes and polling parameters.
type DeviceAuthorization struct {
	DeviceCode      string
	UserCode        string
	VerificationURI string
	Interval        time.Duration
	ExpiresAt       time.Time
}

// throttleDevice caps device authorization starts per client key. It shares
// the login attempt map so one abusive client is counted in one place.
func (a *Auth) throttleDevice(clientKey string) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	now := a.now()
	for key, v := range a.attempts {
		if !now.Before(v.until) {
			delete(a.attempts, key)
		}
	}
	key := "d:" + clientKey
	v := a.attempts[key]
	if v.until.IsZero() {
		v.until = now.Add(deviceStartWindow)
	}
	v.count++
	a.attempts[key] = v
	return v.count > deviceStartLimit
}

// StartDeviceAuth begins a device authorization request. clientKey is the
// caller's address (or another stable client identifier); an empty key skips
// the per-client cap, which only tests and in-process callers use.
func (a *Auth) StartDeviceAuth(ctx context.Context, clientName, clientKey string) (DeviceAuthorization, error) {
	if strings.TrimSpace(clientName) == "" {
		return DeviceAuthorization{}, fmt.Errorf("client name: %w", ErrInvalid)
	}
	if clientKey != "" && a.throttleDevice(clientKey) {
		return DeviceAuthorization{}, ErrRateLimited
	}
	code, err := makeSecret("")
	if err != nil {
		return DeviceAuthorization{}, err
	}
	var b strings.Builder
	for i := 0; i < 8; i++ {
		n, err := rand.Int(rand.Reader, big.NewInt(int64(len(userCodeAlphabet))))
		if err != nil {
			return DeviceAuthorization{}, fmt.Errorf("generate user code: %w", err)
		}
		b.WriteByte(userCodeAlphabet[n.Int64()])
	}
	raw := b.String()
	now := a.now()
	expiry := now.Add(deviceLifetime)
	_, err = a.db.Pool().Exec(ctx, "INSERT INTO device_codes (device_code_hash,user_code_hash,client_id,created_at,expires_at) VALUES ($1,$2,$3,$4,$5)", hashSecret(code), hashSecret(raw), clientName, now, expiry)
	if err != nil {
		return DeviceAuthorization{}, fmt.Errorf("start device authorization: %w", err)
	}
	return DeviceAuthorization{DeviceCode: code, UserCode: raw[:4] + "-" + raw[4:], VerificationURI: "/device", Interval: deviceInterval * time.Second, ExpiresAt: expiry}, nil
}

// ApproveDevice associates a pending code with a logged-in human.
func (a *Auth) ApproveDevice(ctx context.Context, actor Actor, userCode string) error {
	if actor.Kind != PrincipalHuman || actor.Via != CredentialSession {
		return ErrForbidden
	}
	code := strings.ToUpper(strings.ReplaceAll(userCode, "-", ""))
	if len(code) != 8 {
		return ErrInvalid
	}
	tag, err := a.db.Pool().Exec(ctx, "UPDATE device_codes SET status='approved',principal_id=$2 WHERE user_code_hash=$1 AND status='pending' AND expires_at>$3 AND EXISTS (SELECT 1 FROM principals WHERE id=$2 AND user_id=$4 AND kind='human')", hashSecret(code), actor.PrincipalID, a.now(), actor.UserID)
	if err != nil {
		return fmt.Errorf("approve device: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// PollDevice returns a token once, or the RFC 8628 polling status.
func (a *Auth) PollDevice(ctx context.Context, deviceCode string) (TokenSecret, error) {
	var result TokenSecret
	var statusErr error
	err := a.db.WithTx(ctx, func(tx pgx.Tx) error {
		var status, client string
		var principal *int64
		var expires time.Time
		var last *time.Time
		var interval int
		err := tx.QueryRow(ctx, "SELECT status,client_id,principal_id,expires_at,last_polled_at,poll_interval_seconds FROM device_codes WHERE device_code_hash=$1 FOR UPDATE", hashSecret(deviceCode)).Scan(&status, &client, &principal, &expires, &last, &interval)
		if errors.Is(err, pgx.ErrNoRows) {
			statusErr = ErrExpiredToken
			return nil
		}
		if err != nil {
			return fmt.Errorf("find device code: %w", err)
		}
		now := a.now()
		if !now.Before(expires) || status == "consumed" {
			statusErr = ErrExpiredToken
			return nil
		}
		if status == "denied" {
			statusErr = ErrAccessDenied
			return nil
		}
		if last != nil && now.Before(last.Add(time.Duration(interval)*time.Second)) {
			_, err = tx.Exec(ctx, "UPDATE device_codes SET last_polled_at=$2,poll_interval_seconds=poll_interval_seconds+5 WHERE device_code_hash=$1", hashSecret(deviceCode), now)
			if err != nil {
				return fmt.Errorf("slow polling: %w", err)
			}
			statusErr = ErrSlowDown
			return nil
		}
		if status == "pending" {
			_, err = tx.Exec(ctx, "UPDATE device_codes SET last_polled_at=$2 WHERE device_code_hash=$1", hashSecret(deviceCode), now)
			if err != nil {
				return fmt.Errorf("record polling: %w", err)
			}
			statusErr = ErrAuthorizationPending
			return nil
		}
		if status != "approved" || principal == nil {
			return fmt.Errorf("invalid device state: %w", ErrInvalid)
		}
		secret, err := makeSecret("earnest_app_")
		if err != nil {
			return err
		}
		tok := Token{PrincipalID: *principal, Name: client, Kind: TokenApp, Audience: AudienceAPI, Scopes: append([]Scope(nil), HumanScopes...), CreatedAt: now, DailyBudget: 3}
		err = tx.QueryRow(ctx, "INSERT INTO tokens (principal_id,name,token_hash,kind,audience,scopes,created_at) VALUES ($1,$2,$3,'app','api',$4,$5) RETURNING id", tok.PrincipalID, tok.Name, hashSecret(secret), scopeStrings(tok.Scopes), now).Scan(&tok.ID)
		if err != nil {
			return fmt.Errorf("issue device token: %w", err)
		}
		_, err = tx.Exec(ctx, "UPDATE device_codes SET status='consumed',last_polled_at=$2 WHERE device_code_hash=$1", hashSecret(deviceCode), now)
		if err != nil {
			return fmt.Errorf("consume device code: %w", err)
		}
		result = TokenSecret{Token: tok, Secret: secret}
		return nil
	})
	if err != nil {
		return TokenSecret{}, err
	}
	if statusErr != nil {
		return TokenSecret{}, statusErr
	}
	return result, nil
}
