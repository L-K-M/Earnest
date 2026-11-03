package push

import (
	"context"
	"encoding/base64"
	"errors"
	"net/http"
	"testing"
)

func TestSendRejectsInvalidSubscriptionKeys(t *testing.T) {
	tests := []struct {
		name   string
		device Device
	}{
		{"missing p256dh", Device{Kind: KindWebPush, Endpoint: "https://push.example.net/x"}},
		{"short p256dh", Device{Kind: KindWebPush, Endpoint: "https://push.example.net/x", P256DH: "AAAA"}},
		{"missing auth secret", Device{Kind: KindWebPush, Endpoint: "https://push.example.net/x", P256DH: validPublic(t)}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			vapid, err := GenerateVAPIDKeys("")
			if err != nil {
				t.Fatal(err)
			}
			transport, err := NewWebPush(Config{Client: testOutboundClient(t)}, vapid)
			if err != nil {
				t.Fatal(err)
			}
			_, err = transport.Send(context.Background(), tt.device, Message{Title: "t", Body: "b"})
			if err == nil || OutcomeOf(err) != OutcomeNotDelivered {
				t.Fatalf("outcome = %v (%v)", OutcomeOf(err), err)
			}
		})
	}
}

func TestSendRejectsNonHTTPEndpoint(t *testing.T) {
	srv, captured := newCapture(t, http.StatusCreated, nil)
	vapid, err := GenerateVAPIDKeys("")
	if err != nil {
		t.Fatal(err)
	}
	transport, err := NewWebPush(testConfig(t, srv.URL), vapid)
	if err != nil {
		t.Fatal(err)
	}
	dev := Device{
		Kind:       KindWebPush,
		Endpoint:   "ftp://push.example.net/x",
		P256DH:     validPublic(t),
		AuthSecret: base64.RawURLEncoding.EncodeToString(randomSecret(t)),
	}
	_, err = transport.Send(context.Background(), dev, Message{Title: "t", Body: "b"})
	if err == nil || OutcomeOf(err) != OutcomeNotDelivered {
		t.Fatalf("outcome = %v (%v)", OutcomeOf(err), err)
	}
	select {
	case <-captured.requests:
		t.Fatal("request was sent anyway")
	default:
	}
}

func TestConstructorValidation(t *testing.T) {
	if _, err := NewNtfy(Config{}); err == nil {
		t.Error("ntfy without a client accepted")
	}
	if _, err := NewWebPush(Config{}, VAPIDKeys{}); err == nil {
		t.Error("webpush without a client or keys accepted")
	}
	if _, err := NewUnifiedPush(Config{}, &VAPIDKeys{}); err == nil {
		t.Error("unifiedpush without a client accepted")
	}
	if _, err := NewUnifiedPush(Config{Client: testOutboundClient(t)}, &VAPIDKeys{PrivateKey: []byte("nope")}); err == nil {
		t.Error("unifiedpush with an unusable VAPID key accepted")
	}
	if _, err := NewAPNs(APNsConfig{RoundTripper: roundTripFunc(nil), PrivateKey: testAPNsKey(t)}); err == nil {
		t.Error("apns without key and team IDs accepted")
	}
}

func TestErrorUnwrap(t *testing.T) {
	cause := errors.New("cause")
	err := &Error{Kind: KindNtfy, Outcome: OutcomeUnknown, Err: cause}
	if !errors.Is(err, cause) {
		t.Fatal("errors.Is does not reach the cause")
	}
	if got := errors.Unwrap(err); got != cause {
		t.Fatalf("unwrap = %v", got)
	}
}

// validPublic returns a syntactically valid uncompressed P-256 point.
func validPublic(t *testing.T) string {
	t.Helper()
	return base64.RawURLEncoding.EncodeToString(newSubscriber(t).PublicKey().Bytes())
}
