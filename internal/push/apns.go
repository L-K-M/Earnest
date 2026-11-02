package push

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/L-K-M/Earnest/internal/outbound"
)

const (
	// apnsTopic is the app bundle that receives pushes.
	apnsTopic = "ch.lkmc.Earnest"

	apnsProductionHost = "api.push.apple.com"
	apnsSandboxHost    = "api.sandbox.apple.com"

	// apnsPushTypeAlert shows the notification immediately, with sound and a
	// badge if the payload asks for them.
	apnsPushTypeAlert = "alert"

	// APNs priorities: 10 is immediate, 5 lets the system group or delay.
	apnsPriorityHigh    = "10"
	apnsPriorityDefault = "5"

	// apnsTokenTTL is how long one provider token may be reused. Apple asks
	// for regeneration at least hourly; 50 minutes leaves margin.
	apnsTokenTTL = 50 * time.Minute

	// apnsExpirationSeconds keeps an undeliverable alert queued for a day.
	apnsExpirationSeconds = 24 * 60 * 60

	// apnsMaxPayload is the largest alert payload APNs accepts.
	apnsMaxPayload = 4096

	// apnsCustomKey carries the Earnest notification ID inside the payload so
	// the client can fetch the stored notification.
	apnsCustomKey = "earnest_id"
)

// APNsConfig configures the APNs transport.
type APNsConfig struct {
	// Client is the checked outbound client for production delivery.
	Client *outbound.Client
	// RoundTripper is a legacy injection seam; prefer Client.
	RoundTripper http.RoundTripper
	// PrivateKey is the .p8 signing key, PEM-encoded PKCS#8.
	PrivateKey []byte
	KeyID      string
	TeamID     string
	// Sandbox selects api.sandbox.apple.com instead of production.
	Sandbox bool
}

// APNs delivers alert notifications to Apple devices with token-based
// authentication. Payloads are not encrypted end to end, so they never carry
// agent text.
type APNs struct {
	client   *http.Client
	outbound *outbound.Client
	teamID   string
	keyID    string
	private  *ecdsa.PrivateKey
	sandbox  bool

	mu       sync.Mutex
	token    string
	tokenAt  time.Time
	now      func() time.Time
	tokenTTL time.Duration
}

// NewAPNs returns an APNs transport, or an error when the configuration is
// unusable.
func NewAPNs(cfg APNsConfig) (*APNs, error) {
	if cfg.Client == nil && cfg.RoundTripper == nil {
		return nil, errors.New("apns: missing round tripper")
	}
	private, err := parseAPNsPrivateKey(cfg.PrivateKey)
	if err != nil {
		return nil, err
	}
	if cfg.KeyID == "" {
		return nil, errors.New("apns: missing key ID")
	}
	if cfg.TeamID == "" {
		return nil, errors.New("apns: missing team ID")
	}
	return &APNs{
		client:   &http.Client{Transport: cfg.RoundTripper, Timeout: pushTimeout},
		outbound: cfg.Client,
		teamID:   cfg.TeamID,
		keyID:    cfg.KeyID,
		private:  private,
		sandbox:  cfg.Sandbox,
		now:      time.Now,
		tokenTTL: apnsTokenTTL,
	}, nil
}

// Send posts msg to the device token. A 410 response means the app was
// uninstalled or the token rotated, so the registration is unregistered.
func (a *APNs) Send(ctx context.Context, dev Device, msg Message) (Receipt, error) {
	if dev.Kind != KindAPNs {
		return Receipt{}, wrongKind(KindAPNs, dev.Kind)
	}
	if !validDeviceToken(dev.Endpoint) {
		return Receipt{}, &Error{Kind: KindAPNs, Outcome: OutcomeNotDelivered, Err: errors.New("invalid device token")}
	}
	payload, err := json.Marshal(apnsPayload{
		Aps:       apnsAps{Alert: apnsAlert{Title: msg.Title, Body: msg.Body}},
		EarnestID: msg.NotificationID,
	})
	if err != nil {
		return Receipt{}, &Error{Kind: KindAPNs, Outcome: OutcomeNotDelivered, Err: fmt.Errorf("marshal payload: %w", err)}
	}
	if len(payload) > apnsMaxPayload {
		return Receipt{}, &Error{Kind: KindAPNs, Outcome: OutcomeNotDelivered, Err: fmt.Errorf("payload exceeds %d octets", apnsMaxPayload)}
	}
	token, err := a.bearerToken()
	if err != nil {
		return Receipt{}, &Error{Kind: KindAPNs, Outcome: OutcomeNotDelivered, Err: err}
	}
	host := apnsProductionHost
	if a.sandbox {
		host = apnsSandboxHost
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://"+host+"/3/device/"+dev.Endpoint, bytes.NewReader(payload))
	if err != nil {
		return Receipt{}, &Error{Kind: KindAPNs, Outcome: OutcomeNotDelivered, Err: fmt.Errorf("build request: %w", err)}
	}
	req.Header.Set("authorization", "bearer "+token)
	req.Header.Set("apns-push-type", apnsPushTypeAlert)
	req.Header.Set("apns-topic", apnsTopic)
	req.Header.Set("apns-priority", apnsPriority(msg.Category))
	req.Header.Set("apns-expiration", strconv.FormatInt(a.now().Add(time.Duration(apnsExpirationSeconds)*time.Second).Unix(), 10))
	req.Header.Set("content-type", "application/json")

	var res *http.Response
	if a.outbound != nil {
		result, err := a.outbound.Do(req, outbound.Policy{Purpose: outbound.Push, Redirect: outbound.RedirectNone, Protocol: outbound.HTTP2, Timeout: pushTimeout, MaxBodySize: maxResponseBody})
		if result == nil {
			return Receipt{}, networkError(KindAPNs, err)
		}
		res = result.Response
	} else {
		var err error
		res, err = a.client.Do(req)
		if err != nil {
			return Receipt{}, &Error{Kind: KindAPNs, Outcome: classifyRoundTripError(err), Err: err}
		}
	}
	defer res.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(res.Body, maxResponseBody))
	switch {
	case res.StatusCode >= 200 && res.StatusCode < 300:
		return Receipt{ProviderID: res.Header.Get("apns-id")}, nil
	case res.StatusCode == http.StatusGone:
		return Receipt{}, &Error{Kind: KindAPNs, Outcome: OutcomeNotDelivered, Unregistered: true, Err: apnsStatus(res.Status, body)}
	case res.StatusCode >= 500 && res.StatusCode < 600:
		return Receipt{}, &Error{Kind: KindAPNs, Outcome: OutcomeUnknown, Err: apnsStatus(res.Status, body)}
	default:
		return Receipt{}, &Error{Kind: KindAPNs, Outcome: OutcomeNotDelivered, Err: apnsStatus(res.Status, body)}
	}
}

// apnsStatus renders the provider's response for the error string, capped
// and stripped of control characters so a hostile server cannot bloat or
// forge log lines.
func apnsStatus(status string, body []byte) error {
	detail := strings.Map(func(r rune) rune {
		if r == '\n' || r == '\r' || r == '\t' {
			return ' '
		}
		if r < ' ' {
			return -1
		}
		return r
	}, strings.TrimSpace(string(body)))
	if len(detail) > 200 {
		detail = detail[:200]
	}
	if detail == "" {
		return errors.New(status)
	}
	return fmt.Errorf("%s: %s", status, detail)
}

// apnsPriority maps the closed category onto an APNs priority: alerts are
// immediate, everything else may be grouped or delayed by the system.
func apnsPriority(category Category) string {
	if category == CategoryAlert {
		return apnsPriorityHigh
	}
	return apnsPriorityDefault
}

// bearerToken returns a cached provider token, regenerating it once it has
// lived past tokenTTL.
func (a *APNs) bearerToken() (string, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	now := a.now()
	if a.token == "" || now.Sub(a.tokenAt) >= a.tokenTTL {
		token, err := a.signToken(now)
		if err != nil {
			return "", err
		}
		a.token, a.tokenAt = token, now
	}
	return a.token, nil
}

// signToken builds the provider authentication JWT: ES256 with the key ID in
// the header and the team ID as issuer.
func (a *APNs) signToken(now time.Time) (string, error) {
	header, err := json.Marshal(map[string]string{"alg": "ES256", "kid": a.keyID})
	if err != nil {
		return "", fmt.Errorf("marshal APNs header: %w", err)
	}
	claims, err := json.Marshal(map[string]any{"iss": a.teamID, "iat": now.Unix()})
	if err != nil {
		return "", fmt.Errorf("marshal APNs claims: %w", err)
	}
	signingInput := base64.RawURLEncoding.EncodeToString(header) + "." + base64.RawURLEncoding.EncodeToString(claims)
	digest := sha256.Sum256([]byte(signingInput))
	r, s, err := ecdsa.Sign(rand.Reader, a.private, digest[:])
	if err != nil {
		return "", fmt.Errorf("sign APNs token: %w", err)
	}
	signature := make([]byte, 64)
	r.FillBytes(signature[:32])
	s.FillBytes(signature[32:])
	return signingInput + "." + base64.RawURLEncoding.EncodeToString(signature), nil
}

// parseAPNsPrivateKey decodes a .p8 token signing key. Apple issues PKCS#8
// PEM; SEC1 is accepted for completeness.
func parseAPNsPrivateKey(keyPEM []byte) (*ecdsa.PrivateKey, error) {
	block, _ := pem.Decode(keyPEM)
	if block == nil {
		return nil, errors.New("apns: private key is not PEM")
	}
	if parsed, err := x509.ParsePKCS8PrivateKey(block.Bytes); err == nil {
		key, ok := parsed.(*ecdsa.PrivateKey)
		if !ok {
			return nil, fmt.Errorf("apns: key is %T, want ECDSA P-256", parsed)
		}
		if key.Curve != elliptic.P256() {
			return nil, errors.New("apns: key is not P-256")
		}
		return key, nil
	}
	key, err := x509.ParseECPrivateKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("apns: parse private key: %w", err)
	}
	if key.Curve != elliptic.P256() {
		return nil, errors.New("apns: key is not P-256")
	}
	return key, nil
}

// validDeviceToken rejects tokens that are empty, oversized or carry
// characters that would change the shape of the request URL.
func validDeviceToken(token string) bool {
	if token == "" || len(token) > 256 {
		return false
	}
	for _, r := range token {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
		case r == '-', r == '_', r == '.', r == '~', r == ':':
		default:
			return false
		}
	}
	return true
}

// apnsPayload is the notification body: the alert text plus the Earnest
// notification ID. APNs payloads reach the provider in the clear, so no agent
// text ever appears here.
type apnsPayload struct {
	Aps       apnsAps `json:"aps"`
	EarnestID string  `json:"earnest_id"`
}

type apnsAps struct {
	Alert apnsAlert `json:"alert"`
}

type apnsAlert struct {
	Title string `json:"title"`
	Body  string `json:"body"`
}
