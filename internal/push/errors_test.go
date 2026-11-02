package push

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strconv"
	"testing"
	"time"

	"github.com/L-K-M/Earnest/internal/outbound"
)

func TestOutcomeOf(t *testing.T) {
	if OutcomeOf(nil) != OutcomeDelivered {
		t.Fatal("nil error should mean delivered")
	}
	if OutcomeOf(errors.New("plain failure")) != OutcomeUnknown {
		t.Fatal("unclassified errors must be unknown")
	}
	if OutcomeOf(&Error{Outcome: OutcomeNotDelivered}) != OutcomeNotDelivered {
		t.Fatal("typed error lost its outcome")
	}
	wrapped := fmt.Errorf("send: %w", &Error{Kind: KindNtfy, Outcome: OutcomeUnknown})
	if OutcomeOf(wrapped) != OutcomeUnknown {
		t.Fatalf("wrapped outcome = %v", OutcomeOf(wrapped))
	}
}

func TestIsUnregistered(t *testing.T) {
	if IsUnregistered(nil) {
		t.Fatal("nil error is not unregistered")
	}
	var target *Error
	err := fmt.Errorf("wrap: %w", &Error{Kind: KindAPNs, Unregistered: true})
	if !errors.As(err, &target) || !IsUnregistered(err) {
		t.Fatal("unregistered flag lost")
	}
	if IsUnregistered(&Error{Outcome: OutcomeNotDelivered}) {
		t.Fatal("ordinary failure marked unregistered")
	}
}

// TestHTTPStatusClassification covers the outbound path shared by ntfy,
// Web Push and UnifiedPush.
func TestHTTPStatusClassification(t *testing.T) {
	tests := []struct {
		name    string
		status  int
		outcome Outcome
	}{
		{"accepted", http.StatusAccepted, OutcomeDelivered},
		{"server error is unknown", http.StatusServiceUnavailable, OutcomeUnknown},
		{"not found is known", http.StatusNotFound, OutcomeNotDelivered},
		{"rate limited is known", http.StatusTooManyRequests, OutcomeNotDelivered},
		{"forbidden is known", http.StatusForbidden, OutcomeNotDelivered},
		{"redirect is known", http.StatusFound, OutcomeNotDelivered},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv, captured := newCapture(t, tt.status, nil)
			transport, err := NewNtfy(testConfig(t, srv.URL))
			if err != nil {
				t.Fatal(err)
			}
			_, err = transport.Send(context.Background(), Device{Kind: KindNtfy, Endpoint: srv.URL + "/topic"}, Message{Title: "t", Body: "b"})
			if tt.outcome == OutcomeDelivered && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if tt.outcome != OutcomeDelivered && err == nil {
				t.Fatal("expected an error")
			}
			if OutcomeOf(err) != tt.outcome {
				t.Fatalf("outcome = %v (%v), want %v", OutcomeOf(err), err, tt.outcome)
			}
			captured.one(t)
		})
	}
}

func TestConnectionRefusedIsKnownNotDelivered(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	address := listener.Addr().String()
	_, portText, err := net.SplitHostPort(address)
	if err != nil {
		t.Fatal(err)
	}
	port, err := strconv.Atoi(portText)
	if err != nil {
		t.Fatal(err)
	}
	listener.Close()

	client, err := outbound.New(outbound.Config{
		Version:       "test",
		AllowPrefixes: []netip.Prefix{netip.MustParsePrefix("127.0.0.0/8")},
		Spacing:       time.Nanosecond,
	})
	if err != nil {
		t.Fatal(err)
	}
	transport, err := NewNtfy(Config{Client: client, ExtraPorts: []uint16{uint16(port)}})
	if err != nil {
		t.Fatal(err)
	}
	_, err = transport.Send(context.Background(), Device{Kind: KindNtfy, Endpoint: "http://" + address + "/topic"}, Message{Title: "t", Body: "b"})
	if err == nil {
		t.Fatal("expected an error")
	}
	if OutcomeOf(err) != OutcomeNotDelivered {
		t.Fatalf("outcome = %v (%v), want not delivered", OutcomeOf(err), err)
	}
}

func TestTimeoutAfterWriteIsUnknown(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(500 * time.Millisecond)
		w.WriteHeader(http.StatusCreated)
	}))
	defer srv.Close()
	transport, err := NewNtfy(testConfig(t, srv.URL))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	_, err = transport.Send(ctx, Device{Kind: KindNtfy, Endpoint: srv.URL + "/topic"}, Message{Title: "t", Body: "b"})
	if err == nil {
		t.Fatal("expected an error")
	}
	if OutcomeOf(err) != OutcomeUnknown {
		t.Fatalf("outcome = %v (%v), want unknown", OutcomeOf(err), err)
	}
}

func TestConnectionResetIsUnknown(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, _, err := w.(http.Hijacker).Hijack()
		if err != nil {
			return
		}
		conn.Close()
	}))
	defer srv.Close()
	transport, err := NewNtfy(testConfig(t, srv.URL))
	if err != nil {
		t.Fatal(err)
	}
	_, err = transport.Send(context.Background(), Device{Kind: KindNtfy, Endpoint: srv.URL + "/topic"}, Message{Title: "t", Body: "b"})
	if err == nil {
		t.Fatal("expected an error")
	}
	if OutcomeOf(err) != OutcomeUnknown {
		t.Fatalf("outcome = %v (%v), want unknown", OutcomeOf(err), err)
	}
}

func TestWrongDeviceKind(t *testing.T) {
	vapid, err := GenerateVAPIDKeys("")
	if err != nil {
		t.Fatal(err)
	}
	webpush, err := NewWebPush(Config{Client: testOutboundClient(t)}, vapid)
	if err != nil {
		t.Fatal(err)
	}
	_, err = webpush.Send(context.Background(), Device{Kind: KindNtfy, Endpoint: "https://example.net"}, Message{})
	if OutcomeOf(err) != OutcomeNotDelivered {
		t.Fatalf("outcome = %v", OutcomeOf(err))
	}
	if err.Error() != "push webpush not delivered: device is ntfy, not webpush" {
		t.Fatalf("error = %q", err)
	}
}
