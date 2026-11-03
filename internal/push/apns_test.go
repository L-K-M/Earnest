package push

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// roundTripFunc adapts a function into an http.RoundTripper.
type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

// stubTransport records requests and answers from a function.
type stubTransport struct {
	requests chan capturedRequest
	respond  func(*http.Request) (*http.Response, error)
}

func (s *stubTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	body, _ := io.ReadAll(r.Body)
	s.requests <- capturedRequest{Method: r.Method, URL: r.URL.String(), Header: r.Header.Clone(), Body: body}
	return s.respond(r)
}

func okResponse(id string) *http.Response {
	header := make(http.Header)
	if id != "" {
		header.Set("apns-id", id)
	}
	return &http.Response{
		StatusCode: http.StatusOK,
		Status:     "200 OK",
		Header:     header,
		Body:       io.NopCloser(strings.NewReader("")),
	}
}

func statusResponse(status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Status:     fmt.Sprintf("%d %s", status, http.StatusText(status)),
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}

func TestAPNsSend(t *testing.T) {
	keyPEM := testAPNsKey(t)
	stub := &stubTransport{
		requests: make(chan capturedRequest, 8),
		respond:  func(*http.Request) (*http.Response, error) { return okResponse("apns-1"), nil },
	}
	transport, err := NewAPNs(APNsConfig{
		RoundTripper: stub,
		PrivateKey:   keyPEM,
		KeyID:        "KEYID",
		TeamID:       "TEAM",
	})
	if err != nil {
		t.Fatal(err)
	}
	receipt, err := transport.Send(context.Background(), Device{Kind: KindAPNs, Endpoint: "abcdef0123", ShowText: true}, Message{
		Title:          "Morning Agent: 1 alert",
		Body:           "One new alert",
		NotificationID: "n_551",
		Category:       CategoryAlert,
		AgentText:      "agent secret",
	})
	if err != nil {
		t.Fatal(err)
	}
	if receipt.ProviderID != "apns-1" {
		t.Fatalf("receipt = %q", receipt.ProviderID)
	}

	req := <-stub.requests
	if req.Method != http.MethodPost {
		t.Fatalf("method = %s", req.Method)
	}
	if got := req.Header.Get("apns-push-type"); got != apnsPushTypeAlert {
		t.Fatalf("apns-push-type = %q", got)
	}
	if got := req.Header.Get("apns-topic"); got != apnsTopic {
		t.Fatalf("apns-topic = %q", got)
	}
	if got := req.Header.Get("apns-priority"); got != apnsPriorityHigh {
		t.Fatalf("apns-priority = %q", got)
	}
	expiration, err := strconv.ParseInt(req.Header.Get("apns-expiration"), 10, 64)
	if err != nil || abs64(expiration-time.Now().Add(time.Duration(apnsExpirationSeconds)*time.Second).Unix()) > 5 {
		t.Fatalf("apns-expiration = %q", req.Header.Get("apns-expiration"))
	}
	authorization := req.Header.Get("authorization")
	if !strings.HasPrefix(authorization, "bearer ") {
		t.Fatalf("authorization = %q", authorization)
	}

	private, err := parseAPNsPrivateKey(keyPEM)
	if err != nil {
		t.Fatal(err)
	}
	public := make([]byte, 65)
	public[0] = 4
	private.PublicKey.X.FillBytes(public[1:33])
	private.PublicKey.Y.FillBytes(public[33:65])
	token := strings.TrimPrefix(authorization, "bearer ")
	header := jwtHeader(t, token)
	if header["kid"] != "KEYID" || header["alg"] != "ES256" {
		t.Fatalf("token header = %v", header)
	}
	claims := verifyES256JWT(t, token, public)
	if claims["iss"] != "TEAM" {
		t.Fatalf("iss = %v", claims["iss"])
	}
	if iat, ok := claims["iat"].(float64); !ok || abs64(int64(iat)-time.Now().Unix()) > 5 {
		t.Fatalf("iat = %v", claims["iat"])
	}

	var payload map[string]any
	if err := json.Unmarshal(req.Body, &payload); err != nil {
		t.Fatal(err)
	}
	if len(payload) != 2 {
		t.Fatalf("payload keys = %v", payload)
	}
	aps, ok := payload["aps"].(map[string]any)
	if !ok || len(aps) != 1 {
		t.Fatalf("aps = %v", aps)
	}
	alert, ok := aps["alert"].(map[string]any)
	if !ok || len(alert) != 2 {
		t.Fatalf("alert = %v", alert)
	}
	if alert["title"] != "Morning Agent: 1 alert" || alert["body"] != "One new alert" {
		t.Fatalf("alert = %v", alert)
	}
	if payload[apnsCustomKey] != "n_551" {
		t.Fatalf("notification id = %v", payload[apnsCustomKey])
	}
	if strings.Contains(string(req.Body), "agent secret") {
		t.Fatal("payload carries agent text")
	}
}

func TestAPNsProductionHost(t *testing.T) {
	tests := []struct {
		name     string
		sandbox  bool
		wantHost string
	}{
		{"production", false, "https://api.push.apple.com"},
		{"sandbox", true, "https://api.sandbox.push.apple.com"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stub := &stubTransport{
				requests: make(chan capturedRequest, 1),
				respond:  func(*http.Request) (*http.Response, error) { return okResponse(""), nil },
			}
			transport, err := NewAPNs(APNsConfig{RoundTripper: stub, PrivateKey: testAPNsKey(t), KeyID: "K", TeamID: "T", Sandbox: tt.sandbox})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := transport.Send(context.Background(), Device{Kind: KindAPNs, Endpoint: "token"}, Message{Title: "t", Body: "b"}); err != nil {
				t.Fatal(err)
			}
			req := <-stub.requests
			if req.URL != tt.wantHost+"/3/device/token" {
				t.Fatalf("url = %q", req.URL)
			}
		})
	}
}

func TestAPNsStatusOutcomes(t *testing.T) {
	keyPEM := testAPNsKey(t)
	tests := []struct {
		name    string
		status  int
		outcome Outcome
		dropped bool
	}{
		{"gone unregisters", http.StatusGone, OutcomeNotDelivered, true},
		{"server error is unknown", http.StatusInternalServerError, OutcomeUnknown, false},
		{"bad request is known failure", http.StatusBadRequest, OutcomeNotDelivered, false},
		{"rate limited is known failure", http.StatusTooManyRequests, OutcomeNotDelivered, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stub := &stubTransport{
				requests: make(chan capturedRequest, 1),
				respond: func(*http.Request) (*http.Response, error) {
					return statusResponse(tt.status, `{"reason":"test"}`), nil
				},
			}
			transport, err := NewAPNs(APNsConfig{RoundTripper: stub, PrivateKey: keyPEM, KeyID: "K", TeamID: "T"})
			if err != nil {
				t.Fatal(err)
			}
			_, err = transport.Send(context.Background(), Device{Kind: KindAPNs, Endpoint: "token"}, Message{Title: "t", Body: "b"})
			if err == nil {
				t.Fatal("expected an error")
			}
			if OutcomeOf(err) != tt.outcome {
				t.Fatalf("outcome = %v, want %v (%v)", OutcomeOf(err), tt.outcome, err)
			}
			if IsUnregistered(err) != tt.dropped {
				t.Fatalf("unregistered = %v, want %v", IsUnregistered(err), tt.dropped)
			}
			<-stub.requests
		})
	}
}

func TestAPNsRoundTripOutcomes(t *testing.T) {
	keyPEM := testAPNsKey(t)
	tests := []struct {
		name    string
		err     error
		outcome Outcome
	}{
		{"connection reset", &net.OpError{Op: "read", Err: syscall.ECONNRESET}, OutcomeUnknown},
		{"unexpected eof", io.ErrUnexpectedEOF, OutcomeUnknown},
		{"deadline exceeded", context.DeadlineExceeded, OutcomeUnknown},
		{"cancelled", context.Canceled, OutcomeUnknown},
		{"connection refused", &net.OpError{Op: "dial", Err: syscall.ECONNREFUSED}, OutcomeNotDelivered},
		{"dns failure", &net.DNSError{Err: "no such host", IsNotFound: true}, OutcomeNotDelivered},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			transport, err := NewAPNs(APNsConfig{
				RoundTripper: roundTripFunc(func(*http.Request) (*http.Response, error) { return nil, tt.err }),
				PrivateKey:   keyPEM,
				KeyID:        "K",
				TeamID:       "T",
			})
			if err != nil {
				t.Fatal(err)
			}
			_, err = transport.Send(context.Background(), Device{Kind: KindAPNs, Endpoint: "token"}, Message{Title: "t", Body: "b"})
			if err == nil {
				t.Fatal("expected an error")
			}
			if OutcomeOf(err) != tt.outcome {
				t.Fatalf("outcome = %v, want %v (%v)", OutcomeOf(err), tt.outcome, err)
			}
		})
	}
}

func TestAPNsTokenRefresh(t *testing.T) {
	keyPEM := testAPNsKey(t)
	stub := &stubTransport{
		requests: make(chan capturedRequest, 8),
		respond:  func(*http.Request) (*http.Response, error) { return okResponse(""), nil },
	}
	transport, err := NewAPNs(APNsConfig{RoundTripper: stub, PrivateKey: keyPEM, KeyID: "K", TeamID: "T"})
	if err != nil {
		t.Fatal(err)
	}
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	current := base
	transport.now = func() time.Time { return current }
	transport.tokenTTL = apnsTokenTTL
	dev := Device{Kind: KindAPNs, Endpoint: "token"}
	msg := Message{Title: "t", Body: "b"}

	send := func() string {
		t.Helper()
		if _, err := transport.Send(context.Background(), dev, msg); err != nil {
			t.Fatal(err)
		}
		return strings.TrimPrefix((<-stub.requests).Header.Get("authorization"), "bearer ")
	}

	first := send()
	current = base.Add(49 * time.Minute)
	if again := send(); again != first {
		t.Fatal("token changed inside its lifetime")
	}
	current = base.Add(51 * time.Minute)
	refreshed := send()
	if refreshed == first {
		t.Fatal("token was not regenerated after 50 minutes")
	}
	private, err := parseAPNsPrivateKey(keyPEM)
	if err != nil {
		t.Fatal(err)
	}
	public := make([]byte, 65)
	public[0] = 4
	private.PublicKey.X.FillBytes(public[1:33])
	private.PublicKey.Y.FillBytes(public[33:65])
	claims := verifyES256JWT(t, refreshed, public)
	if iat := int64(claims["iat"].(float64)); iat != current.Unix() {
		t.Fatalf("iat = %d, want %d", iat, current.Unix())
	}
	if reused := send(); reused != refreshed {
		t.Fatal("token changed too early")
	}
}

func TestAPNsRejectsBadConfiguration(t *testing.T) {
	keyPEM := testAPNsKey(t)
	if _, err := NewAPNs(APNsConfig{PrivateKey: keyPEM, KeyID: "K", TeamID: "T"}); err == nil {
		t.Fatal("expected an error without a round tripper")
	}
	if _, err := NewAPNs(APNsConfig{
		RoundTripper: roundTripFunc(func(*http.Request) (*http.Response, error) { return nil, errors.New("unused") }),
		PrivateKey:   []byte("nope"),
		KeyID:        "K",
		TeamID:       "T",
	}); err == nil {
		t.Fatal("expected an error with a bad key")
	}
	transport, err := NewAPNs(APNsConfig{
		RoundTripper: roundTripFunc(func(*http.Request) (*http.Response, error) { return okResponse(""), nil }),
		PrivateKey:   keyPEM,
		KeyID:        "K",
		TeamID:       "T",
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, token := range []string{"", "has space", "has/slash", "query?x=1", "frag#ment", strings.Repeat("x", 300)} {
		_, err := transport.Send(context.Background(), Device{Kind: KindAPNs, Endpoint: token}, Message{})
		if OutcomeOf(err) != OutcomeNotDelivered {
			t.Errorf("%q: outcome = %v", token, OutcomeOf(err))
		}
	}
}

func TestParseAPNsPrivateKeyAcceptsSEC1(t *testing.T) {
	private, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	der, err := x509.MarshalECPrivateKey(private)
	if err != nil {
		t.Fatal(err)
	}
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: der})
	parsed, err := parseAPNsPrivateKey(keyPEM)
	if err != nil {
		t.Fatal(err)
	}
	if parsed.D.Cmp(private.D) != 0 {
		t.Fatal("key mismatch")
	}
}

func testAPNsKey(t *testing.T) []byte {
	t.Helper()
	private, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	der, err := x509.MarshalPKCS8PrivateKey(private)
	if err != nil {
		t.Fatal(err)
	}
	return pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der})
}
