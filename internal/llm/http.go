package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/L-K-M/Earnest/internal/outbound"
)

const (
	// defaultRequestTimeout bounds one provider call when the configuration
	// names none.
	defaultRequestTimeout = 60 * time.Second
	// maxErrorBody caps how much of a provider error response is kept for the
	// error text.
	maxErrorBody = 512
)

// callJSON sends payload as a JSON POST through outbound and decodes a
// successful JSON response into out. Failures become *Error, which carries
// the HTTP status and whether retrying could help.
func callJSON(ctx context.Context, client *outbound.Client, provider, endpoint string, headers http.Header, payload, out any, timeout time.Duration, extraPorts []uint16) error {
	var body io.Reader
	if payload != nil {
		encoded, err := json.Marshal(payload)
		if err != nil {
			return fmt.Errorf("llm: provider %s encode request: %w", provider, err)
		}
		body = bytes.NewReader(encoded)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, body)
	if err != nil {
		return fmt.Errorf("llm: provider %s build request: %w", provider, err)
	}
	req.Header.Set("Content-Type", "application/json")
	for key, values := range headers {
		for _, value := range values {
			req.Header.Add(key, value)
		}
	}
	result, err := client.Do(req, outbound.Policy{
		Purpose:    outbound.LLM,
		Timeout:    timeout,
		Redirect:   outbound.RedirectFollow,
		ExtraPorts: extraPorts,
	})
	if result == nil || result.Response == nil {
		return mapProviderError(provider, 0, nil, err)
	}
	defer result.Response.Body.Close()
	// outbound already bounds the body by its own limit; read all of it so a
	// long completion is never truncated.
	responseBody, readErr := io.ReadAll(result.Response.Body)
	if readErr != nil {
		return mapProviderError(provider, result.Response.StatusCode, nil, readErr)
	}
	if status := result.Response.StatusCode; status < 200 || status >= 300 {
		return mapProviderError(provider, status, responseBody, err)
	}
	if err != nil {
		return mapProviderError(provider, result.Response.StatusCode, responseBody, err)
	}
	if out != nil {
		if err := json.Unmarshal(responseBody, out); err != nil {
			return fmt.Errorf("llm: provider %s decode response: %w", provider, err)
		}
	}
	slog.DebugContext(ctx, "llm provider request", "provider", provider, "status", result.Response.StatusCode)
	return nil
}

// mapProviderError converts a transport or HTTP failure into *Error. The
// response body is included only to give the operator the provider's own
// message; request bodies and headers never appear.
func mapProviderError(provider string, status int, body []byte, err error) error {
	if err == nil {
		err = errors.New("provider request failed")
	}
	if len(body) > maxErrorBody {
		body = body[:maxErrorBody]
	}
	if detail := strings.TrimSpace(string(body)); detail != "" {
		err = fmt.Errorf("%w: %s", err, detail)
	}
	return &Error{Provider: provider, StatusCode: status, Retryable: retryableStatus(status, err), Err: err}
}

// retryableStatus reports whether repeating a failed call could succeed.
// Transport faults are retryable only when they look transient; a denied
// address or TLS failure will not improve.
func retryableStatus(status int, err error) bool {
	if status == 0 {
		switch outbound.ErrorClass(err) {
		case outbound.Timeout, outbound.Connect, outbound.DNS:
			return true
		default:
			return false
		}
	}
	switch status {
	case http.StatusRequestTimeout, http.StatusTooEarly, http.StatusTooManyRequests:
		return true
	default:
		return status >= 500 && status <= 599
	}
}
