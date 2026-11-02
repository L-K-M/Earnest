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
	"encoding/base64"
	"encoding/json"
	"math/big"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"
)

// RFC 8291 section 5 worked example, with line wrapping removed.
const (
	rfcUAPrivate = "q1dXpw3UpT5VOmu_cf_v6ih07Aems3njxI-JWgLcM94"
	rfcUAPublic  = "BCVxsr7N_eNgVRqvHtD0zTZsEc6-VV-JvLexhqUzORcxaOzi6-AYWXvTBHm4bjyPjs7Vd8pZGH6SRpkNtoIAiw4"
	rfcASPrivate = "yfWPiYE-n46HLnH0KqZOF1fJJU3MYrct3AELtAQ-oRw"
	rfcASPublic  = "BP4z9KsN6nGRTbVYI_c7VJSPQTBtkgcy27mlmlMoZIIgDll6e3vCYLocInmYWAmS6TlzAC8wEqKK6PBru3jl7A8"
	rfcAuth      = "BTBZMqHH6r4Tts7J_aSIgg"
	rfcSalt      = "DGv6ra1nlYgDCS1FRnbzlw"
	rfcShared    = "kyrL1jIIOHEzg3sM2ZWRHDRB62YACZhhSlknJ672kSs"
	rfcCEK       = "oIhVW04MRdy2XN9CiKLxTg"
	rfcNonce     = "4h_95klXJ5E_qnoN"
	rfcBody      = "DGv6ra1nlYgDCS1FRnbzlwAAEABBBP4z9KsN6nGRTbVYI_c7VJSPQTBtkgcy27mlmlMoZIIgDll6e3vCYLocInmYWAmS6TlzAC8wEqKK6PBru3jl7A_yl95bQpu6cVPTpK4Mqgkf1CXztLVBSt2Ks3oZwbuwXPXLWyouBWLVWGNWQexSgSxsj_Qulcy4a-fN"
)

func TestEncryptRFC8291Example(t *testing.T) {
	asPrivate, err := ecdh.P256().NewPrivateKey(mustB64(t, rfcASPrivate))
	if err != nil {
		t.Fatal(err)
	}
	uaPublic := mustB64(t, rfcUAPublic)
	authSecret := mustB64(t, rfcAuth)
	salt := mustB64(t, rfcSalt)
	plaintext := []byte("When I grow up, I want to be a watermelon")

	// The ECDH step against the published intermediate value.
	shared, err := asPrivate.ECDH(mustPublicKey(t, uaPublic))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(shared, mustB64(t, rfcShared)) {
		t.Fatalf("shared secret\n got %x\nwant %x", shared, mustB64(t, rfcShared))
	}

	// Key derivation against the published CEK and nonce.
	cek, nonce, err := deriveKeys(shared, authSecret, salt, uaPublic, asPrivate.PublicKey().Bytes())
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(cek, mustB64(t, rfcCEK)) {
		t.Fatalf("CEK\n got %x\nwant %x", cek, mustB64(t, rfcCEK))
	}
	if !bytes.Equal(nonce, mustB64(t, rfcNonce)) {
		t.Fatalf("nonce\n got %x\nwant %x", nonce, mustB64(t, rfcNonce))
	}

	body, asPublic, err := encryptWithKey(asPrivate, plaintext, uaPublic, authSecret, salt)
	if err != nil {
		t.Fatal(err)
	}
	if want := mustB64(t, rfcBody); !bytes.Equal(body, want) {
		t.Fatalf("body\n got %s\nwant %s", base64.RawURLEncoding.EncodeToString(body), base64.RawURLEncoding.EncodeToString(want))
	}
	if want := mustB64(t, rfcASPublic); !bytes.Equal(asPublic, want) {
		t.Fatalf("sender public key\n got %x\nwant %x", asPublic, want)
	}
}

// RFC 8292 Figure 1 example JWT and JWK, used to pin the ES256 JOSE form.
func TestVerifyRFC8292JWT(t *testing.T) {
	const token = "eyJ0eXAiOiJKV1QiLCJhbGciOiJFUzI1NiJ9." +
		"eyJhdWQiOiJodHRwczovL3B1c2guZXhhbXBsZS5uZXQiLCJleHAiOjE0NTM1MjM3NjgsInN1YiI6Im1haWx0bzpwdXNoQGV4YW1wbGUuY29tIn0." +
		"i3CYb7t4xfxCDquptFOepC9GAu_HLGkMlMuCGSK2rpiUfnK9ojFwDXb1JrErtmysazNjjvW2L9OkSSHzvoD1oA"
	public := append([]byte{4}, append(mustB64(t, "DUfHPKLVFQzVvnCPGyfucbECzPDa7rWbXriLcysAjEc"), mustB64(t, "F6YK5h4SDYic-dRuU_RCPCfA5aq9ojSwk5Y2EmClBPs")...)...)
	claims := verifyES256JWT(t, token, public)
	if claims["aud"] != "https://push.example.net" {
		t.Fatalf("aud = %v", claims["aud"])
	}
	if claims["sub"] != "mailto:push@example.com" {
		t.Fatalf("sub = %v", claims["sub"])
	}
	if exp := int64(claims["exp"].(float64)); exp != 1453523768 {
		t.Fatalf("exp = %d", exp)
	}
}

func TestWebPushSend(t *testing.T) {
	subPrivate := newSubscriber(t)
	subAuth := randomSecret(t)
	srv, cap := newCapture(t, http.StatusCreated, func(h http.Header) { h.Set("Location", "/message/abc") })

	vapid, err := GenerateVAPIDKeys("mailto:admin@example.com")
	if err != nil {
		t.Fatal(err)
	}
	transport, err := NewWebPush(testConfig(t, srv.URL), vapid)
	if err != nil {
		t.Fatal(err)
	}
	msg := Message{
		Title:          "Morning Agent: 1 alert",
		Body:           "One new alert",
		NotificationID: "n_551",
		Category:       CategoryAlert,
		AgentText:      "CVE in rustls",
	}
	receipt, err := transport.Send(context.Background(), Device{
		Kind:       KindWebPush,
		Endpoint:   srv.URL + "/push/subscription",
		P256DH:     base64.RawURLEncoding.EncodeToString(subPrivate.PublicKey().Bytes()),
		AuthSecret: base64.RawURLEncoding.EncodeToString(subAuth),
		ShowText:   true,
	}, msg)
	if err != nil {
		t.Fatal(err)
	}
	if receipt.ProviderID != "/message/abc" {
		t.Fatalf("receipt = %q", receipt.ProviderID)
	}

	req := cap.one(t)
	if req.Method != http.MethodPost {
		t.Fatalf("method = %s", req.Method)
	}
	if got := req.Header.Get("Content-Encoding"); got != contentEncodingAES128GCM {
		t.Fatalf("content encoding = %q", got)
	}
	if got := req.Header.Get("TTL"); got != strconv.Itoa(webPushTTLSeconds) {
		t.Fatalf("ttl = %q", got)
	}
	authorization := req.Header.Get("Authorization")
	if !strings.HasPrefix(authorization, "vapid t=") {
		t.Fatalf("authorization = %q", authorization)
	}
	cryptoKey := req.Header.Get("Crypto-Key")
	if !strings.Contains(cryptoKey, "p256ecdsa="+vapid.PublicKey) {
		t.Fatalf("crypto key = %q", cryptoKey)
	}
	if !strings.Contains(cryptoKey, "dh=") {
		t.Fatalf("crypto key misses the ephemeral key: %q", cryptoKey)
	}

	origin := mustOrigin(t, srv.URL)
	token := strings.TrimPrefix(strings.SplitN(authorization, ",", 2)[0], "vapid t=")
	claims := verifyES256JWT(t, token, mustB64(t, vapid.PublicKey))
	if claims["aud"] != origin {
		t.Fatalf("aud = %v, want %s", claims["aud"], origin)
	}
	if claims["sub"] != "mailto:admin@example.com" {
		t.Fatalf("sub = %v", claims["sub"])
	}
	if exp, ok := claims["exp"].(float64); !ok || abs64(int64(exp)-time.Now().Add(vapidExpiry).Unix()) > 5 {
		t.Fatalf("exp = %v, want about now + %s", claims["exp"], vapidExpiry)
	}

	payload := decryptPayload(t, req.Body, subPrivate, subAuth)
	var got pushPayload
	if err := json.Unmarshal(payload, &got); err != nil {
		t.Fatal(err)
	}
	if got.Title != msg.Title || got.Body != msg.Body || got.NotificationID != msg.NotificationID || got.Category != string(msg.Category) {
		t.Fatalf("payload = %+v", got)
	}
	if got.Text != msg.AgentText {
		t.Fatalf("agent text = %q, want %q", got.Text, msg.AgentText)
	}
}

func TestWebPushTextOptIn(t *testing.T) {
	tests := []struct {
		name     string
		showText bool
		wantText string
	}{
		{"opted out omits agent text", false, ""},
		{"opted in includes agent text", true, "the agent's words"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			subPrivate := newSubscriber(t)
			subAuth := randomSecret(t)
			srv, cap := newCapture(t, http.StatusCreated, nil)
			vapid, err := GenerateVAPIDKeys("")
			if err != nil {
				t.Fatal(err)
			}
			transport, err := NewWebPush(testConfig(t, srv.URL), vapid)
			if err != nil {
				t.Fatal(err)
			}
			_, err = transport.Send(context.Background(), Device{
				Kind:       KindWebPush,
				Endpoint:   srv.URL + "/push/subscription",
				P256DH:     base64.RawURLEncoding.EncodeToString(subPrivate.PublicKey().Bytes()),
				AuthSecret: base64.RawURLEncoding.EncodeToString(subAuth),
				ShowText:   tt.showText,
			}, Message{Title: "t", Body: "b", AgentText: "the agent's words"})
			if err != nil {
				t.Fatal(err)
			}
			req := cap.one(t)
			payload := decryptPayload(t, req.Body, subPrivate, subAuth)
			var got pushPayload
			if err := json.Unmarshal(payload, &got); err != nil {
				t.Fatal(err)
			}
			if got.Text != tt.wantText {
				t.Fatalf("text = %q, want %q", got.Text, tt.wantText)
			}
		})
	}
}

func TestEncryptedGone(t *testing.T) {
	tests := []struct {
		name string
		new  func(Config) (Transport, error)
	}{
		{"webpush", func(cfg Config) (Transport, error) {
			keys, err := GenerateVAPIDKeys("")
			if err != nil {
				return nil, err
			}
			return NewWebPush(cfg, keys)
		}},
		{"unifiedpush", func(cfg Config) (Transport, error) { return NewUnifiedPush(cfg, nil) }},
	}
	for _, tt := range tests {
		for _, code := range []int{http.StatusNotFound, http.StatusGone} {
			t.Run(tt.name+" "+http.StatusText(code), func(t *testing.T) {
				srv, captured := newCapture(t, code, nil)
				transport, err := tt.new(testConfig(t, srv.URL))
				if err != nil {
					t.Fatal(err)
				}
				dev := Device{Kind: KindUnifiedPush, Endpoint: srv.URL + "/push/x"}
				sub := newSubscriber(t)
				dev.P256DH = base64.RawURLEncoding.EncodeToString(sub.PublicKey().Bytes())
				dev.AuthSecret = base64.RawURLEncoding.EncodeToString(randomSecret(t))
				if tt.name == "webpush" {
					dev.Kind = KindWebPush
				}
				_, err = transport.Send(context.Background(), dev, Message{Title: "t", Body: "b"})
				if err == nil {
					t.Fatal("expected an error")
				}
				if OutcomeOf(err) != OutcomeNotDelivered {
					t.Fatalf("outcome = %v, want not delivered", OutcomeOf(err))
				}
				if !IsUnregistered(err) {
					t.Fatal("device should be marked unregistered")
				}
				captured.one(t)
			})
		}
	}
}

func TestUnifiedPushSendWithoutVAPID(t *testing.T) {
	subPrivate := newSubscriber(t)
	subAuth := randomSecret(t)
	srv, cap := newCapture(t, http.StatusCreated, nil)
	transport, err := NewUnifiedPush(testConfig(t, srv.URL), nil)
	if err != nil {
		t.Fatal(err)
	}
	_, err = transport.Send(context.Background(), Device{
		Kind:       KindUnifiedPush,
		Endpoint:   srv.URL + "/push/subscription",
		P256DH:     base64.RawURLEncoding.EncodeToString(subPrivate.PublicKey().Bytes()),
		AuthSecret: base64.RawURLEncoding.EncodeToString(subAuth),
		ShowText:   true,
	}, Message{Title: "t", Body: "b", AgentText: "words"})
	if err != nil {
		t.Fatal(err)
	}
	req := cap.one(t)
	if req.Header.Get("Authorization") != "" || req.Header.Get("Crypto-Key") != "" {
		t.Fatalf("unexpected identification headers: %v", req.Header)
	}
	payload := decryptPayload(t, req.Body, subPrivate, subAuth)
	var got pushPayload
	if err := json.Unmarshal(payload, &got); err != nil {
		t.Fatal(err)
	}
	if got.Text != "words" {
		t.Fatalf("text = %q", got.Text)
	}
}

func TestWebPushPayloadTooLarge(t *testing.T) {
	srv, cap := newCapture(t, http.StatusCreated, nil)
	vapid, err := GenerateVAPIDKeys("")
	if err != nil {
		t.Fatal(err)
	}
	transport, err := NewWebPush(testConfig(t, srv.URL), vapid)
	if err != nil {
		t.Fatal(err)
	}
	sub := newSubscriber(t)
	_, err = transport.Send(context.Background(), Device{
		Kind:       KindWebPush,
		Endpoint:   srv.URL,
		P256DH:     base64.RawURLEncoding.EncodeToString(sub.PublicKey().Bytes()),
		AuthSecret: base64.RawURLEncoding.EncodeToString(randomSecret(t)),
	}, Message{Title: "t", Body: strings.Repeat("a", maxPlaintext)})
	if err == nil {
		t.Fatal("expected an error")
	}
	if OutcomeOf(err) != OutcomeNotDelivered {
		t.Fatalf("outcome = %v", OutcomeOf(err))
	}
	select {
	case <-cap.requests:
		t.Fatal("oversized payload was sent anyway")
	default:
	}
}

func TestEndpointOrigin(t *testing.T) {
	tests := []struct {
		raw  string
		want string
	}{
		{"https://push.example.net/push/x", "https://push.example.net"},
		{"https://push.example.net:8443/push/x", "https://push.example.net:8443"},
		{"http://ntfy.local:8080/topic", "http://ntfy.local:8080"},
		{"http://example.net/topic", "http://example.net"},
	}
	for _, tt := range tests {
		got, err := endpointOrigin(tt.raw)
		if err != nil {
			t.Errorf("%s: %v", tt.raw, err)
			continue
		}
		if got != tt.want {
			t.Errorf("%s: got %q, want %q", tt.raw, got, tt.want)
		}
	}
	for _, raw := range []string{"", "ftp://example.net/x", "https:///path"} {
		if _, err := endpointOrigin(raw); err == nil {
			t.Errorf("%q: expected an error", raw)
		}
	}
}

func TestGenerateVAPIDKeys(t *testing.T) {
	keys, err := GenerateVAPIDKeys("mailto:a@b.c")
	if err != nil {
		t.Fatal(err)
	}
	auth, err := parseVAPIDKeys(keys)
	if err != nil {
		t.Fatal(err)
	}
	if auth.subject != "mailto:a@b.c" {
		t.Fatalf("subject = %q", auth.subject)
	}
	if !bytes.Equal(auth.public, mustB64(t, keys.PublicKey)) {
		t.Fatal("public key mismatch")
	}
	if _, err := ecdh.P256().NewPublicKey(auth.public); err != nil {
		t.Fatalf("public key does not parse: %v", err)
	}
}

func TestParseVAPIDKeysRejectsBadInput(t *testing.T) {
	good, err := GenerateVAPIDKeys("")
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name string
		keys VAPIDKeys
	}{
		{"not pem", VAPIDKeys{PrivateKey: []byte("nope"), PublicKey: good.PublicKey}},
		{"bad public", VAPIDKeys{PrivateKey: good.PrivateKey, PublicKey: "!!!"}},
		{"short public", VAPIDKeys{PrivateKey: good.PrivateKey, PublicKey: "AAAA"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := parseVAPIDKeys(tt.keys); err == nil {
				t.Fatal("expected an error")
			}
		})
	}
}

// verifyES256JWT checks a compact JOSE ES256 token against an uncompressed
// P-256 public point and returns its claims.
func verifyES256JWT(t *testing.T, token string, public []byte) map[string]any {
	t.Helper()
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		t.Fatalf("token has %d segments", len(parts))
	}
	headerJSON, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		t.Fatal(err)
	}
	var header struct {
		Alg string `json:"alg"`
	}
	if err := json.Unmarshal(headerJSON, &header); err != nil {
		t.Fatal(err)
	}
	if header.Alg != "ES256" {
		t.Fatalf("alg = %q", header.Alg)
	}
	signature, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil || len(signature) != 64 {
		t.Fatalf("signature length = %d, err %v", len(signature), err)
	}
	pub := &ecdsa.PublicKey{Curve: elliptic.P256()}
	pub.X = new(big.Int).SetBytes(public[1:33])
	pub.Y = new(big.Int).SetBytes(public[33:65])
	digest := sha256.Sum256([]byte(parts[0] + "." + parts[1]))
	if !ecdsa.Verify(pub, digest[:], new(big.Int).SetBytes(signature[:32]), new(big.Int).SetBytes(signature[32:])) {
		t.Fatal("signature does not verify")
	}
	claimsJSON, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		t.Fatal(err)
	}
	var claims map[string]any
	if err := json.Unmarshal(claimsJSON, &claims); err != nil {
		t.Fatal(err)
	}
	return claims
}

// decryptPayload undoes RFC 8291 with the subscriber's private key and strips
// the padding delimiter.
func decryptPayload(t *testing.T, body []byte, private *ecdh.PrivateKey, authSecret []byte) []byte {
	t.Helper()
	if len(body) < saltLength+4+1+65 {
		t.Fatalf("body too short: %d", len(body))
	}
	salt := body[:saltLength]
	asPublic := body[21 : 21+65]
	ciphertext := body[21+65:]
	shared, err := private.ECDH(mustPublicKey(t, asPublic))
	if err != nil {
		t.Fatal(err)
	}
	cek, nonce, err := deriveKeys(shared, authSecret, salt, private.PublicKey().Bytes(), asPublic)
	if err != nil {
		t.Fatal(err)
	}
	block, err := aes.NewCipher(cek)
	if err != nil {
		t.Fatal(err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		t.Fatal(err)
	}
	padded, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		t.Fatalf("decrypt: %v", err)
	}
	i := len(padded) - 1
	for i >= 0 && padded[i] == 0 {
		i--
	}
	if i < 0 || padded[i] != paddingDelimiter {
		t.Fatalf("missing padding delimiter")
	}
	return padded[:i]
}

func mustPublicKey(t *testing.T, raw []byte) *ecdh.PublicKey {
	t.Helper()
	key, err := ecdh.P256().NewPublicKey(raw)
	if err != nil {
		t.Fatal(err)
	}
	return key
}

func newSubscriber(t *testing.T) *ecdh.PrivateKey {
	t.Helper()
	key, err := ecdh.P256().GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	return key
}

func randomSecret(t *testing.T) []byte {
	t.Helper()
	secret := make([]byte, saltLength)
	if _, err := rand.Read(secret); err != nil {
		t.Fatal(err)
	}
	return secret
}

func mustOrigin(t *testing.T, raw string) string {
	t.Helper()
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	return u.Scheme + "://" + u.Host
}

func abs64(n int64) int64 {
	if n < 0 {
		return -n
	}
	return n
}
