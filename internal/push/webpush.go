package push

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/ecdh"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"golang.org/x/crypto/hkdf"
)

const (
	// RFC 8188/8291 content coding parameters.
	contentEncodingAES128GCM = "aes128gcm"
	saltLength               = 16
	keyLength                = 16
	nonceLength              = 12
	tagLength                = 16
	paddingDelimiter         = 0x02
	defaultRecordSize        = 4096

	// maxPlaintext is what fits a 4096-octet push body after the 86-octet
	// header, the padding delimiter and the GCM tag (RFC 8291 section 4).
	// Larger messages would be rejected by push services.
	maxPlaintext = 4096 - 86 - 1 - 16

	// webPushTTLSeconds is how long the push service may hold the message for
	// an offline device.
	webPushTTLSeconds = 24 * 60 * 60

	// vapidExpiry bounds the VAPID token lifetime; RFC 8292 allows at most 24
	// hours.
	vapidExpiry = 12 * time.Hour

	webPushInfoLabel = "WebPush: info"
	cekInfoLabel     = "Content-Encoding: aes128gcm\x00"
	nonceInfoLabel   = "Content-Encoding: nonce\x00"
)

// VAPIDKeys is the server's VAPID signing key pair (RFC 8292).
type VAPIDKeys struct {
	// PrivateKey is the PEM-encoded PKCS#8 EC P-256 key.
	PrivateKey []byte
	// PublicKey is the uncompressed public point, base64url without padding.
	PublicKey string
	// Subject is the optional "sub" claim: a mailto: or https: contact URI.
	Subject string
}

// GenerateVAPIDKeys creates a new P-256 key pair for VAPID signing, in the
// form the transports expect.
func GenerateVAPIDKeys(subject string) (VAPIDKeys, error) {
	private, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return VAPIDKeys{}, fmt.Errorf("generate VAPID key: %w", err)
	}
	der, err := x509.MarshalPKCS8PrivateKey(private)
	if err != nil {
		return VAPIDKeys{}, fmt.Errorf("marshal VAPID key: %w", err)
	}
	public := make([]byte, 65)
	public[0] = 4
	private.X.FillBytes(public[1:33])
	private.Y.FillBytes(public[33:65])
	return VAPIDKeys{
		PrivateKey: pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der}),
		PublicKey:  base64.RawURLEncoding.EncodeToString(public),
		Subject:    subject,
	}, nil
}

// WebPush sends RFC 8030 push messages to browsers with RFC 8291 encryption
// and RFC 8292 VAPID identification.
type WebPush struct {
	egress egress
	vapid  *vapidAuth
}

// NewWebPush returns a browser push transport. keys are required because push
// services reject unidentifiable senders.
func NewWebPush(cfg Config, keys VAPIDKeys) (*WebPush, error) {
	if cfg.Client == nil {
		return nil, errors.New("push: missing outbound client")
	}
	vapid, err := parseVAPIDKeys(keys)
	if err != nil {
		return nil, err
	}
	return &WebPush{egress: cfg.egress(true), vapid: vapid}, nil
}

func (w *WebPush) Send(ctx context.Context, dev Device, msg Message) (Receipt, error) {
	if dev.Kind != KindWebPush {
		return Receipt{}, wrongKind(KindWebPush, dev.Kind)
	}
	return sendEncrypted(ctx, w.egress, KindWebPush, w.vapid, dev, msg)
}

// UnifiedPush sends RFC 8030 push messages to UnifiedPush distributors,
// which use the same encryption as browsers. Endpoints on private addresses
// are rejected by the shared outbound client and redirects are never
// followed, per UnifiedPush's server guidance.
type UnifiedPush struct {
	egress egress
	vapid  *vapidAuth
}

// NewUnifiedPush returns a UnifiedPush transport. VAPID is optional because
// most distributors do not validate it; pass nil to omit it.
func NewUnifiedPush(cfg Config, keys *VAPIDKeys) (*UnifiedPush, error) {
	if cfg.Client == nil {
		return nil, errors.New("push: missing outbound client")
	}
	var vapid *vapidAuth
	if keys != nil {
		parsed, err := parseVAPIDKeys(*keys)
		if err != nil {
			return nil, err
		}
		vapid = parsed
	}
	return &UnifiedPush{egress: cfg.egress(true), vapid: vapid}, nil
}

func (u *UnifiedPush) Send(ctx context.Context, dev Device, msg Message) (Receipt, error) {
	if dev.Kind != KindUnifiedPush {
		return Receipt{}, wrongKind(KindUnifiedPush, dev.Kind)
	}
	return sendEncrypted(ctx, u.egress, KindUnifiedPush, u.vapid, dev, msg)
}

// sendEncrypted encrypts msg for dev and posts it to the subscription
// endpoint. Both transport families share RFC 8291; they differ only in
// VAPID support.
func sendEncrypted(ctx context.Context, e egress, kind Kind, vapid *vapidAuth, dev Device, msg Message) (Receipt, error) {
	payload, err := marshalPayload(msg, dev.TextAllowed())
	if err != nil {
		return Receipt{}, &Error{Kind: kind, Outcome: OutcomeNotDelivered, Err: err}
	}
	if len(payload) > maxPlaintext {
		return Receipt{}, &Error{Kind: kind, Outcome: OutcomeNotDelivered, Err: fmt.Errorf("payload exceeds %d octets", maxPlaintext)}
	}
	uaPublic, authSecret, err := subscriptionKeys(dev)
	if err != nil {
		return Receipt{}, &Error{Kind: kind, Outcome: OutcomeNotDelivered, Err: err}
	}
	salt := make([]byte, saltLength)
	if _, err := rand.Read(salt); err != nil {
		return Receipt{}, &Error{Kind: kind, Outcome: OutcomeNotDelivered, Err: fmt.Errorf("generate salt: %w", err)}
	}
	body, asPublic, err := encryptPayload(payload, uaPublic, authSecret, salt)
	if err != nil {
		return Receipt{}, &Error{Kind: kind, Outcome: OutcomeNotDelivered, Err: err}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, dev.Endpoint, bytes.NewReader(body))
	if err != nil {
		return Receipt{}, &Error{Kind: kind, Outcome: OutcomeNotDelivered, Err: fmt.Errorf("build request: %w", err)}
	}
	req.Header.Set("Content-Encoding", contentEncodingAES128GCM)
	req.Header.Set("Content-Type", "application/octet-stream")
	req.Header.Set("TTL", strconv.Itoa(webPushTTLSeconds))
	if vapid != nil {
		origin, err := endpointOrigin(dev.Endpoint)
		if err != nil {
			return Receipt{}, &Error{Kind: kind, Outcome: OutcomeNotDelivered, Err: err}
		}
		authorization, err := vapid.authorization(origin, time.Now())
		if err != nil {
			return Receipt{}, &Error{Kind: kind, Outcome: OutcomeNotDelivered, Err: err}
		}
		req.Header.Set("Authorization", authorization)
		req.Header.Set("Crypto-Key", "dh="+base64.RawURLEncoding.EncodeToString(asPublic)+"; p256ecdsa="+base64.RawURLEncoding.EncodeToString(vapid.public))
	}
	res, _, err := e.send(kind, req)
	if err != nil {
		return Receipt{}, err
	}
	return Receipt{ProviderID: res.Header.Get("Location")}, nil
}

// pushPayload is the encrypted body for Web Push and UnifiedPush. A client
// opens the stored notification by its ID; the app fetches the rest over the
// tailnet. Text carries the agent's own words and appears only when the
// device is allowed to see them.
type pushPayload struct {
	Title          string `json:"title"`
	Body           string `json:"body"`
	NotificationID string `json:"notification_id,omitempty"`
	Category       string `json:"category,omitempty"`
	Text           string `json:"text,omitempty"`
}

func marshalPayload(msg Message, allowText bool) ([]byte, error) {
	p := pushPayload{
		Title:          msg.Title,
		Body:           msg.Body,
		NotificationID: msg.NotificationID,
		Category:       string(msg.Category),
	}
	if allowText {
		p.Text = msg.AgentText
	}
	data, err := json.Marshal(p)
	if err != nil {
		return nil, fmt.Errorf("marshal payload: %w", err)
	}
	return data, nil
}

// subscriptionKeys decodes the base64url subscription material.
func subscriptionKeys(dev Device) (public, authSecret []byte, err error) {
	public, err = base64.RawURLEncoding.DecodeString(dev.P256DH)
	if err != nil || len(public) != 65 || public[0] != 4 {
		return nil, nil, errors.New("invalid p256dh key")
	}
	authSecret, err = base64.RawURLEncoding.DecodeString(dev.AuthSecret)
	if err != nil || len(authSecret) != 16 {
		return nil, nil, errors.New("invalid auth secret")
	}
	return public, authSecret, nil
}

// endpointOrigin returns the RFC 6454 origin of the subscription URL, which
// is the VAPID audience.
func endpointOrigin(raw string) (string, error) {
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" {
		return "", fmt.Errorf("invalid endpoint %q", raw)
	}
	scheme := strings.ToLower(u.Scheme)
	port := u.Port()
	if port != "" && !((scheme == "https" && port == "443") || (scheme == "http" && port == "80")) {
		return scheme + "://" + net.JoinHostPort(strings.ToLower(u.Hostname()), port), nil
	}
	return scheme + "://" + strings.ToLower(u.Hostname()), nil
}

// encryptPayload encrypts plaintext per RFC 8291 with a fresh ephemeral key.
// It returns the request body and the ephemeral public key, which callers
// need for the Crypto-Key header.
func encryptPayload(plaintext, uaPublic, authSecret, salt []byte) (body, asPublic []byte, err error) {
	asPrivate, err := ecdh.P256().GenerateKey(rand.Reader)
	if err != nil {
		return nil, nil, fmt.Errorf("ephemeral key: %w", err)
	}
	return encryptWithKey(asPrivate, plaintext, uaPublic, authSecret, salt)
}

// encryptWithKey runs the RFC 8291 single-record encryption with an explicit
// application-server key, so tests can replay a specification vector.
func encryptWithKey(asPrivate *ecdh.PrivateKey, plaintext, uaPublic, authSecret, salt []byte) (body, asPublic []byte, err error) {
	uaKey, err := ecdh.P256().NewPublicKey(uaPublic)
	if err != nil {
		return nil, nil, fmt.Errorf("parse p256dh: %w", err)
	}
	shared, err := asPrivate.ECDH(uaKey)
	if err != nil {
		return nil, nil, fmt.Errorf("ecdh: %w", err)
	}
	asPublic = asPrivate.PublicKey().Bytes()
	cek, nonce, err := deriveKeys(shared, authSecret, salt, uaPublic, asPublic)
	if err != nil {
		return nil, nil, err
	}

	// The single record ends with the padding delimiter; RFC 8291 section 4
	// requires the record size to exceed the padded plaintext plus the tag.
	padded := make([]byte, 0, len(plaintext)+1)
	padded = append(padded, plaintext...)
	padded = append(padded, paddingDelimiter)
	block, err := aes.NewCipher(cek)
	if err != nil {
		return nil, nil, fmt.Errorf("cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, nil, fmt.Errorf("gcm: %w", err)
	}
	ciphertext := gcm.Seal(nil, nonce, padded, nil)

	header := make([]byte, 0, saltLength+4+1+len(asPublic))
	header = append(header, salt...)
	var rs [4]byte
	binary.BigEndian.PutUint32(rs[:], recordSize(len(padded)))
	header = append(header, rs[:]...)
	header = append(header, byte(len(asPublic)))
	header = append(header, asPublic...)
	return append(header, ciphertext...), asPublic, nil
}

// deriveKeys implements RFC 8291 sections 3.3 and 3.4: it combines the ECDH
// shared secret with the authentication secret, then derives the content
// encryption key and nonce from the message salt.
func deriveKeys(sharedSecret, authSecret, salt, uaPublic, asPublic []byte) (cek, nonce []byte, err error) {
	keyInfo := make([]byte, 0, len(webPushInfoLabel)+1+len(uaPublic)+len(asPublic))
	keyInfo = append(keyInfo, webPushInfoLabel...)
	keyInfo = append(keyInfo, 0)
	keyInfo = append(keyInfo, uaPublic...)
	keyInfo = append(keyInfo, asPublic...)
	prkKey := hkdf.Extract(sha256.New, sharedSecret, authSecret)
	ikm, err := expandKey(prkKey, keyInfo, 32)
	if err != nil {
		return nil, nil, err
	}
	prk := hkdf.Extract(sha256.New, ikm, salt)
	cek, err = expandKey(prk, []byte(cekInfoLabel), keyLength)
	if err != nil {
		return nil, nil, err
	}
	nonce, err = expandKey(prk, []byte(nonceInfoLabel), nonceLength)
	if err != nil {
		return nil, nil, err
	}
	return cek, nonce, nil
}

// expandKey reads length octets of HKDF output.
func expandKey(prk, info []byte, length int) ([]byte, error) {
	out := make([]byte, length)
	if _, err := io.ReadFull(hkdf.Expand(sha256.New, prk, info), out); err != nil {
		return nil, fmt.Errorf("hkdf expand: %w", err)
	}
	return out, nil
}

// recordSize returns the RFC 8188 record size, which RFC 8291 requires to
// exceed the padded plaintext plus the authentication tag.
func recordSize(padded int) uint32 {
	size := uint32(padded + tagLength + 1)
	if size < defaultRecordSize {
		size = defaultRecordSize
	}
	return size
}

// vapidAuth is a parsed VAPID signing key.
type vapidAuth struct {
	private *ecdsa.PrivateKey
	public  []byte
	subject string
}

// parseVAPIDKeys accepts PKCS#8 or SEC1 PEM and a base64url uncompressed
// public point.
func parseVAPIDKeys(keys VAPIDKeys) (*vapidAuth, error) {
	block, _ := pem.Decode(keys.PrivateKey)
	if block == nil {
		return nil, errors.New("VAPID private key is not PEM")
	}
	var private *ecdsa.PrivateKey
	if parsed, err := x509.ParsePKCS8PrivateKey(block.Bytes); err == nil {
		key, ok := parsed.(*ecdsa.PrivateKey)
		if !ok {
			return nil, fmt.Errorf("VAPID key is %T, want ECDSA P-256", parsed)
		}
		private = key
	} else if parsed, err := x509.ParseECPrivateKey(block.Bytes); err == nil {
		private = parsed
	} else {
		return nil, fmt.Errorf("parse VAPID key: %w", err)
	}
	if private.Curve != elliptic.P256() {
		return nil, errors.New("VAPID key is not P-256")
	}
	public, err := base64.RawURLEncoding.DecodeString(keys.PublicKey)
	if err != nil || len(public) != 65 || public[0] != 4 {
		return nil, errors.New("invalid VAPID public key")
	}
	return &vapidAuth{private: private, public: public, subject: keys.Subject}, nil
}

// vapidClaims is the RFC 8292 JWT body.
type vapidClaims struct {
	Audience string `json:"aud"`
	Expires  int64  `json:"exp"`
	Subject  string `json:"sub,omitempty"`
}

// authorization builds the RFC 8292 "vapid" Authorization value bound to
// origin.
func (v *vapidAuth) authorization(origin string, now time.Time) (string, error) {
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"typ":"JWT","alg":"ES256"}`))
	claims, err := json.Marshal(vapidClaims{
		Audience: origin,
		Expires:  now.Add(vapidExpiry).Unix(),
		Subject:  v.subject,
	})
	if err != nil {
		return "", fmt.Errorf("marshal VAPID claims: %w", err)
	}
	signingInput := header + "." + base64.RawURLEncoding.EncodeToString(claims)
	digest := sha256.Sum256([]byte(signingInput))
	r, s, err := ecdsa.Sign(rand.Reader, v.private, digest[:])
	if err != nil {
		return "", fmt.Errorf("sign VAPID token: %w", err)
	}
	// JOSE ES256 signatures are the raw R||S pair, not ASN.1.
	signature := make([]byte, 64)
	r.FillBytes(signature[:32])
	s.FillBytes(signature[32:])
	token := signingInput + "." + base64.RawURLEncoding.EncodeToString(signature)
	return "vapid t=" + token + ", k=" + base64.RawURLEncoding.EncodeToString(v.public), nil
}
