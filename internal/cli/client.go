package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	defaultTimeout   = 30 * time.Second
	maxResponseBytes = 16 << 20
	userAgentPrefix  = "earnest-cli/"
)

// client is a thin REST client. It never touches the database and never logs
// credentials; the token travels only in the Authorization header.
type client struct {
	baseURL string
	token   string
	http    *http.Client
}

func newClient(baseURL, token string) *client {
	return &client{
		baseURL: strings.TrimRight(baseURL, "/"),
		token:   token,
		http:    &http.Client{Timeout: defaultTimeout},
	}
}

func userAgent() string { return userAgentPrefix + Version }

func (c *client) get(ctx context.Context, path string, query url.Values) ([]byte, error) {
	return c.do(ctx, http.MethodGet, path, query, "", nil)
}

func (c *client) send(ctx context.Context, method, path string, query url.Values, body any) ([]byte, error) {
	return c.do(ctx, method, path, query, "application/json", body)
}

// raw issues a request with caller-supplied content type and returns the body.
func (c *client) raw(ctx context.Context, method, path, contentType string, body []byte) ([]byte, error) {
	return c.do(ctx, method, path, nil, contentType, body)
}

func (c *client) do(ctx context.Context, method, path string, query url.Values, contentType string, body any) ([]byte, error) {
	target := c.baseURL + path
	if len(query) > 0 {
		target += "?" + query.Encode()
	}
	var payload io.Reader
	switch body := body.(type) {
	case nil:
	case []byte:
		payload = bytes.NewReader(body)
	default:
		data, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("encode request: %w", err)
		}
		payload = bytes.NewReader(data)
	}
	req, err := http.NewRequestWithContext(ctx, method, target, payload)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", userAgent())
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%s %s: %w", method, path, err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, problemError(resp.StatusCode, resp.Header.Get("Retry-After"), data)
	}
	return data, nil
}

// problemError decodes an RFC 9457 problem body, falling back to the status and
// raw body when the server sent something else.
func problemError(status int, retryAfter string, data []byte) error {
	apiErr := &apiError{status: status, retryAfter: retryAfter, raw: string(data)}
	var p problem
	if err := json.Unmarshal(bytes.TrimSpace(data), &p); err == nil && (p.Title != "" || p.Type != "" || p.Status != 0) {
		apiErr.problem = p
		if apiErr.problem.Title == "" {
			apiErr.problem.Title = http.StatusText(status)
		}
		if apiErr.problem.Status == 0 {
			apiErr.problem.Status = status
		}
		return apiErr
	}
	apiErr.problem = problem{
		Type:   "/problems/http-error",
		Title:  http.StatusText(status),
		Status: status,
		Detail: truncate(strings.TrimSpace(string(data)), maxProblemDetail),
	}
	return apiErr
}

const maxProblemDetail = 512

// decode unmarshals a JSON response into out, treating an empty body as absent.
func decode(raw []byte, out any) error {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || string(trimmed) == "null" {
		return nil
	}
	if err := json.Unmarshal(trimmed, out); err != nil {
		return fmt.Errorf("decode response: %w", err)
	}
	return nil
}

// bootstrapCursor reads the current generation and revision for write base_rev
// values and change cursors.
func (c *client) bootstrapCursor(ctx context.Context) (revision, error) {
	raw, err := c.get(ctx, "/v1/bootstrap", nil)
	if err != nil {
		return revision{}, err
	}
	var cursor revision
	if err := decode(raw, &cursor); err != nil {
		return revision{}, err
	}
	return cursor, nil
}

// principalID resolves the authenticated principal. State writes name their
// target principal explicitly.
func (c *client) principalID(ctx context.Context) (int64, error) {
	raw, err := c.get(ctx, "/v1/me", nil)
	if err != nil {
		return 0, err
	}
	var me meResult
	if err := decode(raw, &me); err != nil {
		return 0, err
	}
	if me.PrincipalID <= 0 {
		return 0, errors.New("server reported no principal")
	}
	return me.PrincipalID, nil
}
