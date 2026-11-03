package push

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
)

const (
	// ntfy priority levels: 3 is the server default, 4 marks high importance.
	ntfyPriorityDefault = "3"
	ntfyPriorityHigh    = "4"
)

// Ntfy publishes messages to an ntfy server. The request carries only the
// title, priority and credentials: ntfy clients fetch icon, attachment,
// click-action and email targets automatically, so Earnest never supplies a
// URL for them.
type Ntfy struct {
	egress egress
}

// NewNtfy returns a transport publishing to any ntfy server, self-hosted or
// not.
func NewNtfy(cfg Config) (*Ntfy, error) {
	if cfg.Client == nil {
		return nil, errors.New("push: missing outbound client")
	}
	return &Ntfy{egress: cfg.egress(false)}, nil
}

// Send posts msg to the device's topic. Agent text joins the body only when
// Device.TextAllowed reports true and the text carries no URL; see ntfyBody.
func (n *Ntfy) Send(ctx context.Context, dev Device, msg Message) (Receipt, error) {
	if dev.Kind != KindNtfy {
		return Receipt{}, wrongKind(KindNtfy, dev.Kind)
	}
	body := ntfyBody(dev, msg)
	if hasURL(body) {
		return Receipt{}, &Error{Kind: KindNtfy, Outcome: OutcomeNotDelivered, Err: errors.New("message body carries a URL")}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, dev.Endpoint, strings.NewReader(body))
	if err != nil {
		return Receipt{}, &Error{Kind: KindNtfy, Outcome: OutcomeNotDelivered, Err: fmt.Errorf("build request: %w", err)}
	}
	req.Header.Set("Title", msg.Title)
	req.Header.Set("Priority", ntfyPriority(msg.Category))
	req.Header.Set("Content-Type", "text/plain; charset=utf-8")
	if dev.Token != "" {
		req.Header.Set("Authorization", "Bearer "+dev.Token)
	} else if dev.Username != "" || dev.Password != "" {
		req.SetBasicAuth(dev.Username, dev.Password)
	}
	_, reply, err := n.egress.send(KindNtfy, req)
	if err != nil {
		return Receipt{}, err
	}
	return Receipt{ProviderID: ntfyMessageID(reply)}, nil
}

// ntfyBody composes the published message. Agent text joins the body only
// when the device opted in, the server is one you operate and the text holds
// no URL: ntfy clients auto-fetch URLs found in messages, which would leak
// that the notification was opened.
func ntfyBody(dev Device, msg Message) string {
	if dev.TextAllowed() && msg.AgentText != "" && !hasURL(msg.AgentText) {
		return msg.Body + "\n\n" + msg.AgentText
	}
	return msg.Body
}

// hasURL reports whether s contains something shaped like an absolute URI.
func hasURL(s string) bool {
	return strings.Contains(strings.ToLower(s), "://")
}

// ntfyPriority maps the closed category onto an ntfy priority: alerts are
// high, everything else keeps the server default.
func ntfyPriority(category Category) string {
	if category == CategoryAlert {
		return ntfyPriorityHigh
	}
	return ntfyPriorityDefault
}

// ntfyMessageID pulls the message ID out of an ntfy JSON response, tolerating
// servers that answer with anything else.
func ntfyMessageID(body []byte) string {
	var reply struct {
		ID string `json:"id"`
	}
	if json.Unmarshal(body, &reply) != nil {
		return ""
	}
	return reply.ID
}
