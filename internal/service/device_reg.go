package service

import (
	"context"
	"crypto/ecdh"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/L-K-M/Earnest/internal/push"
	"github.com/L-K-M/Earnest/internal/store"
	"github.com/jackc/pgx/v5"
)

// Device is one registered push destination. Subscription keys are write-only:
// they are stored encrypted and never returned by reads.
type Device struct {
	ID        int64     `json:"id"`
	Kind      push.Kind `json:"kind"`
	Endpoint  string    `json:"endpoint"`
	ShowText  bool      `json:"show_text"`
	Rev       uint64    `json:"rev"`
	CreatedAt time.Time `json:"created_at"`
}

// DeviceRegistration supplies one Web Push or UnifiedPush subscription.
// ntfy is configured through push settings instead; APNs registers through
// the native client.
type DeviceRegistration struct {
	Kind       push.Kind
	Endpoint   string
	P256DH     string
	AuthSecret string
	ShowText   bool
	OpID       string
}

// deviceActor enforces human-only device management. A registration is a
// destination the server posts to, so it stays yours only, like webhooks.
func (s *Service) deviceActor(ctx context.Context, a Actor) error {
	if a.Kind != PrincipalHuman {
		return ErrForbidden
	}
	return s.checkActor(ctx, a, ScopeDevicesWrite)
}

// RegisterDevice stores a subscription's endpoint and keys, deduplicated by
// the caller's operation ID. Re-registering the same endpoint refreshes its
// keys and opt-in flag, since push subscriptions rotate.
func (s *Service) RegisterDevice(ctx context.Context, a Actor, r DeviceRegistration) (Device, error) {
	if err := s.deviceActor(ctx, a); err != nil {
		return Device{}, err
	}
	if r.Kind != push.KindWebPush && r.Kind != push.KindUnifiedPush {
		return Device{}, fmt.Errorf("transport: %w", ErrInvalid)
	}
	endpoint, err := pushEndpoint(r.Endpoint)
	if err != nil {
		return Device{}, err
	}
	if !validOpID(r.OpID) {
		return Device{}, fmt.Errorf("op_id: %w", ErrInvalid)
	}
	keys, err := s.sealDeviceKeys(r.P256DH, r.AuthSecret)
	if err != nil {
		return Device{}, err
	}
	out := Device{Kind: r.Kind, Endpoint: endpoint, ShowText: r.ShowText}
	err = s.store.WithTx(ctx, func(tx pgx.Tx) error {
		if err := store.LockCore(ctx, tx); err != nil {
			return err
		}
		replay, err := replayOp(ctx, tx, a.PrincipalID, "RegisterDevice", r.OpID)
		if err != nil {
			return err
		}
		if replay != nil {
			return json.Unmarshal(replay, &out)
		}
		err = tx.QueryRow(ctx, `INSERT INTO devices(user_id,transport,endpoint,keys,show_text) VALUES($1,$2,$3,$4,$5)
			ON CONFLICT (user_id,transport,endpoint) DO UPDATE SET keys=$4,show_text=$5,rev=devices.rev+1
			RETURNING id,rev,created_at`,
			a.UserID, string(r.Kind), endpoint, keys, r.ShowText).Scan(&out.ID, &out.Rev, &out.CreatedAt)
		if err != nil {
			return domainError(err)
		}
		return store.FinishOp(ctx, tx, a.PrincipalID, r.OpID, out)
	})
	return out, err
}

// ListDevices returns the user's registrations without their keys.
func (s *Service) ListDevices(ctx context.Context, a Actor) ([]Device, error) {
	if err := s.deviceActor(ctx, a); err != nil {
		return nil, err
	}
	rows, err := s.store.Pool().Query(ctx, "SELECT id,transport,endpoint,show_text,rev,created_at FROM devices WHERE user_id=$1 ORDER BY id", a.UserID)
	if err != nil {
		return nil, fmt.Errorf("list devices: %w", err)
	}
	defer rows.Close()
	out := []Device{}
	for rows.Next() {
		var d Device
		var kind string
		if err := rows.Scan(&d.ID, &kind, &d.Endpoint, &d.ShowText, &d.Rev, &d.CreatedAt); err != nil {
			return nil, err
		}
		d.Kind = push.Kind(kind)
		out = append(out, d)
	}
	return out, rows.Err()
}

// DeleteDevice removes one registration, deduplicated by operation ID.
func (s *Service) DeleteDevice(ctx context.Context, a Actor, id int64, opID string) error {
	if err := s.deviceActor(ctx, a); err != nil {
		return err
	}
	if id <= 0 {
		return fmt.Errorf("device id: %w", ErrInvalid)
	}
	if !validOpID(opID) {
		return fmt.Errorf("op_id: %w", ErrInvalid)
	}
	return s.store.WithTx(ctx, func(tx pgx.Tx) error {
		if err := store.LockCore(ctx, tx); err != nil {
			return err
		}
		replay, err := replayOp(ctx, tx, a.PrincipalID, "DeleteDevice", opID)
		if err != nil {
			return err
		}
		if replay != nil {
			return nil
		}
		tag, err := tx.Exec(ctx, "DELETE FROM devices WHERE user_id=$1 AND id=$2", a.UserID, id)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return ErrNotFound
		}
		return store.FinishOp(ctx, tx, a.PrincipalID, opID, map[string]any{"id": id})
	})
}

// pushEndpoint validates a subscription URL at registration time. Only https
// destinations are accepted, and an IP literal must be public; a DNS name
// cannot be classified without resolving it, so private addresses behind a
// name are left to the outbound client's dial-time checks. Nothing is
// fetched here.
func pushEndpoint(raw string) (string, error) {
	if len(raw) > 8192 {
		return "", fmt.Errorf("device endpoint: %w", ErrInvalid)
	}
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u == nil || u.Scheme != "https" || u.Host == "" || u.Hostname() == "" || u.User != nil || u.Fragment != "" {
		return "", fmt.Errorf("device endpoint: %w", ErrInvalid)
	}
	host := strings.ToLower(strings.TrimSuffix(u.Hostname(), "."))
	if host == "localhost" || strings.HasSuffix(host, ".localhost") || strings.HasSuffix(host, ".local") || strings.HasSuffix(host, ".home.arpa") {
		return "", fmt.Errorf("device endpoint is a local hostname: %w", ErrInvalid)
	}
	if ip, err := netip.ParseAddr(host); err == nil && !publicPushAddr(ip) {
		return "", fmt.Errorf("device endpoint is not a public address: %w", ErrInvalid)
	}
	if port := u.Port(); port != "" {
		if n, err := strconv.Atoi(port); err != nil || n < 1 || n > 65535 {
			return "", fmt.Errorf("device endpoint port: %w", ErrInvalid)
		}
	}
	return u.String(), nil
}

// privatePushPrefixes mirrors the outbound client's denied list so an IP
// literal endpoint is rejected at registration instead of at first send.
var privatePushPrefixes = func() []netip.Prefix {
	var out []netip.Prefix
	for _, s := range []string{"0.0.0.0/8", "10.0.0.0/8", "100.64.0.0/10", "127.0.0.0/8", "169.254.0.0/16", "172.16.0.0/12", "192.168.0.0/16", "198.18.0.0/15", "240.0.0.0/4", "::/128", "::1/128", "::/96", "fc00::/7", "fe80::/10", "::ffff:0:0/96", "64:ff9b::/96"} {
		out = append(out, netip.MustParsePrefix(s))
	}
	return out
}()

func publicPushAddr(ip netip.Addr) bool {
	if !ip.IsValid() || ip.Zone() != "" || ip.IsUnspecified() || ip.IsMulticast() {
		return false
	}
	for _, p := range privatePushPrefixes {
		if p.Contains(ip) {
			return false
		}
	}
	return true
}

const deviceKeysPurpose = "earnest/device-keys/v1"

// deviceKeys is the subscription key pair. The whole document is sealed
// before it reaches the keys column.
type deviceKeys struct {
	P256DH string `json:"p256dh"`
	Auth   string `json:"auth"`
}

type sealedKeys struct {
	Sealed string `json:"sealed"`
}

// subscriptionMaterial checks the RFC 8291 key shapes the push transports
// require, so an unusable registration is rejected at write time.
func subscriptionMaterial(p256dh, auth string) error {
	pub, err := base64.RawURLEncoding.DecodeString(p256dh)
	if err != nil || len(pub) != 65 || pub[0] != 4 {
		return fmt.Errorf("p256dh: %w", ErrInvalid)
	}
	if _, err := ecdh.P256().NewPublicKey(pub); err != nil {
		return fmt.Errorf("p256dh point: %w", ErrInvalid)
	}
	secret, err := base64.RawURLEncoding.DecodeString(auth)
	if err != nil || len(secret) != 16 {
		return fmt.Errorf("auth secret: %w", ErrInvalid)
	}
	return nil
}

func (s *Service) sealDeviceKeys(p256dh, auth string) ([]byte, error) {
	if err := subscriptionMaterial(p256dh, auth); err != nil {
		return nil, err
	}
	plain, err := json.Marshal(deviceKeys{P256DH: p256dh, Auth: auth})
	if err != nil {
		return nil, err
	}
	sealed, err := s.sealSecret(plain, deviceKeysPurpose)
	if err != nil {
		return nil, err
	}
	body, err := json.Marshal(sealedKeys{Sealed: base64.StdEncoding.EncodeToString(sealed)})
	if err != nil {
		return nil, fmt.Errorf("encode device keys: %w", err)
	}
	return body, nil
}

func (s *Service) openDeviceKeys(body []byte) (p256dh, auth string, err error) {
	var wrapper sealedKeys
	if err := json.Unmarshal(body, &wrapper); err != nil || wrapper.Sealed == "" {
		return "", "", fmt.Errorf("decode device keys: %w", ErrUnavailable)
	}
	sealed, err := base64.StdEncoding.DecodeString(wrapper.Sealed)
	if err != nil {
		return "", "", fmt.Errorf("device key envelope: %w", err)
	}
	plain, err := s.openSecret(sealed, deviceKeysPurpose)
	if err != nil {
		return "", "", err
	}
	var keys deviceKeys
	if err := json.Unmarshal(plain, &keys); err != nil {
		return "", "", fmt.Errorf("decode device keys: %w", err)
	}
	return keys.P256DH, keys.Auth, nil
}

// sealSecret encrypts a small secret with the service AEAD. The purpose is
// authenticated as additional data, so an envelope copied from another
// column cannot be opened here.
func (s *Service) sealSecret(plaintext []byte, purpose string) ([]byte, error) {
	nonce := make([]byte, s.credentials.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, fmt.Errorf("secret nonce: %w", err)
	}
	return s.credentials.Seal(nonce, nonce, plaintext, []byte(purpose)), nil
}

func (s *Service) openSecret(envelope []byte, purpose string) ([]byte, error) {
	n := s.credentials.NonceSize()
	if len(envelope) < n {
		return nil, fmt.Errorf("secret envelope: %w", ErrInvalid)
	}
	plain, err := s.credentials.Open(nil, envelope[:n], envelope[n:], []byte(purpose))
	if err != nil {
		return nil, fmt.Errorf("decrypt secret: %w", ErrUnavailable)
	}
	return plain, nil
}
