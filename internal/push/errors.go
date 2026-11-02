package push

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"syscall"
	"time"

	"github.com/L-K-M/Earnest/internal/outbound"
)

const (
	// pushTimeout bounds one delivery attempt, connection setup included.
	pushTimeout = 15 * time.Second
	// maxResponseBody caps how much of a provider response is buffered.
	maxResponseBody = 64 << 10
)

// Outcome classifies what a caller may conclude about a delivery attempt.
// Notifications are at most once, so the distinction decides both the
// reported status and whether a retry could duplicate the message.
type Outcome uint8

const (
	// OutcomeDelivered means the provider accepted the message.
	OutcomeDelivered Outcome = iota
	// OutcomeNotDelivered means the provider answered definitively, or the
	// request never left Earnest. Safe to report as failed.
	OutcomeNotDelivered
	// OutcomeUnknown means the message may have been delivered. Never
	// retried: the notification stays marked uncertain instead.
	OutcomeUnknown
)

// Error describes a failed delivery attempt.
type Error struct {
	Kind    Kind
	Outcome Outcome
	// Unregistered means the provider said the device no longer exists, so
	// the registration can be dropped. Only set by transport families whose
	// providers signal this.
	Unregistered bool
	Err          error
}

func (e *Error) Error() string {
	switch e.Outcome {
	case OutcomeNotDelivered:
		return fmt.Sprintf("push %s not delivered: %v", e.Kind, e.Err)
	case OutcomeUnknown:
		return fmt.Sprintf("push %s outcome unknown: %v", e.Kind, e.Err)
	default:
		return fmt.Sprintf("push %s: %v", e.Kind, e.Err)
	}
}

func (e *Error) Unwrap() error { return e.Err }

// OutcomeOf reports the outcome carried by err. A nil error is
// OutcomeDelivered. An error the transports did not classify is
// OutcomeUnknown: assuming non-delivery could justify a duplicate send, which
// at most once delivery forbids.
func OutcomeOf(err error) Outcome {
	if err == nil {
		return OutcomeDelivered
	}
	var e *Error
	if errors.As(err, &e) {
		return e.Outcome
	}
	return OutcomeUnknown
}

// IsUnregistered reports whether the provider said the device no longer
// exists.
func IsUnregistered(err error) bool {
	var e *Error
	return errors.As(err, &e) && e.Unregistered
}

// wrongKind rejects a device registered for another transport.
func wrongKind(want, got Kind) error {
	return &Error{Kind: want, Outcome: OutcomeNotDelivered, Err: fmt.Errorf("device is %s, not %s", got, want)}
}

// egress bundles the shared outbound client with the policy knobs every
// transport needs. Push requests never follow redirects: a redirect could
// deliver the payload, including agent text, to a different destination.
type egress struct {
	client             *outbound.Client
	extraPorts         []uint16
	unregisteredOnGone bool
}

// send performs one delivery request and classifies the result. The response
// is nil when the request never completed. A response that arrived carries
// the outcome in its status code alone, so an outbound status error is
// ignored here.
func (e egress) send(kind Kind, req *http.Request) (*http.Response, []byte, error) {
	res, err := e.client.Do(req, outbound.Policy{
		Purpose:    outbound.Push,
		Redirect:   outbound.RedirectNone,
		Timeout:    pushTimeout,
		ExtraPorts: e.extraPorts,
	})
	if res == nil {
		return nil, nil, networkError(kind, err)
	}
	defer res.Response.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(res.Response.Body, maxResponseBody))
	return res.Response, body, statusError(kind, res.Response.StatusCode, e.unregisteredOnGone)
}

// networkError classifies a failure before any response was received. The
// outbound client collapses connection problems into a few classes, so the
// dial phase is inspected to tell "nothing was sent" apart from "the
// connection broke mid-request".
func networkError(kind Kind, err error) error {
	var oe *outbound.Error
	if !errors.As(err, &oe) {
		// Rejected locally (bad URL, policy violation); nothing reached the
		// wire.
		return &Error{Kind: kind, Outcome: OutcomeNotDelivered, Err: err}
	}
	switch oe.Class {
	case outbound.DeniedAddress, outbound.DNS, outbound.TLS,
		outbound.HTTPGone, outbound.HTTP429, outbound.HTTP403:
		return &Error{Kind: kind, Outcome: OutcomeNotDelivered, Err: oe}
	case outbound.Connect:
		if dialPhase(oe.Err) {
			return &Error{Kind: kind, Outcome: OutcomeNotDelivered, Err: oe}
		}
	}
	return &Error{Kind: kind, Outcome: OutcomeUnknown, Err: oe}
}

// statusError classifies a completed HTTP response. 5xx is unknown: the
// provider saw the request and may still have queued the message.
func statusError(kind Kind, code int, unregisteredOnGone bool) error {
	switch {
	case code >= 200 && code < 300:
		return nil
	case (code == http.StatusNotFound || code == http.StatusGone) && unregisteredOnGone:
		return &Error{Kind: kind, Outcome: OutcomeNotDelivered, Unregistered: true, Err: fmt.Errorf("HTTP %d", code)}
	case code >= 500 && code < 600:
		return &Error{Kind: kind, Outcome: OutcomeUnknown, Err: fmt.Errorf("HTTP %d", code)}
	default:
		return &Error{Kind: kind, Outcome: OutcomeNotDelivered, Err: fmt.Errorf("HTTP %d", code)}
	}
}

// classifyRoundTripError classifies failures from an injected
// http.RoundTripper, which carries no outbound classification. Anything that
// proves the connection was never established is definitely not delivered;
// everything else is unknown.
func classifyRoundTripError(err error) Outcome {
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return OutcomeUnknown
	}
	if dialPhase(err) {
		return OutcomeNotDelivered
	}
	var dns *net.DNSError
	if errors.As(err, &dns) {
		return OutcomeNotDelivered
	}
	var record tls.RecordHeaderError
	var cert *tls.CertificateVerificationError
	if errors.As(err, &record) || errors.As(err, &cert) {
		return OutcomeNotDelivered
	}
	return OutcomeUnknown
}

// dialPhase reports whether the failure happened while establishing the
// connection, which proves the request was never transmitted. Connection
// resets surface as read or write errors instead, so they stay unknown.
func dialPhase(err error) bool {
	var op *net.OpError
	if !errors.As(err, &op) {
		return false
	}
	if op.Op == "dial" {
		return true
	}
	return errors.Is(err, syscall.ECONNREFUSED) ||
		errors.Is(err, syscall.EHOSTUNREACH) ||
		errors.Is(err, syscall.ENETUNREACH)
}
