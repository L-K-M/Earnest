package llm

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/L-K-M/Earnest/internal/outbound"
)

// anthropicVersion is the Messages API version this client targets.
const anthropicVersion = "2023-06-01"

// defaultMaxTokens is the completion budget sent when the caller names none.
// Anthropic requires max_tokens, so there is always a value.
const defaultMaxTokens = DefaultMaxTokens

// Anthropic is a provider for the Anthropic Messages API. Anthropic exposes
// no embeddings API, so Embed returns ErrUnsupported.
type Anthropic struct {
	name       string
	baseURL    string
	apiKey     string
	client     *outbound.Client
	timeout    time.Duration
	extraPorts []uint16
}

// Name returns the configured provider name.
func (p *Anthropic) Name() string { return p.name }

type anthropicMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type anthropicRequest struct {
	Model       string             `json:"model"`
	System      string             `json:"system,omitempty"`
	Messages    []anthropicMessage `json:"messages"`
	MaxTokens   int                `json:"max_tokens"`
	Temperature float32            `json:"temperature,omitempty"`
}

type anthropicResponse struct {
	Content []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"content"`
	Usage struct {
		InputTokens  int `json:"input_tokens"`
		OutputTokens int `json:"output_tokens"`
	} `json:"usage"`
}

// Complete performs a Messages completion and validates structured output.
//
// Anthropic has no tool-free JSON-schema mode, so the schema travels in the
// system instruction and Validate enforces it on the returned text. Tools are
// never used.
func (p *Anthropic) Complete(ctx context.Context, req CompleteRequest) (*Completion, error) {
	system := req.System
	if req.Schema != nil {
		system = system + "\n\nRespond with a single JSON value matching this schema:\n" + string(req.Schema.Definition)
	}
	messages := make([]anthropicMessage, 0, len(req.Messages))
	for _, message := range req.Messages {
		messages = append(messages, anthropicMessage{Role: string(message.Role), Content: message.Content})
	}
	maxTokens := req.MaxTokens
	if maxTokens <= 0 {
		maxTokens = defaultMaxTokens
	}
	body := anthropicRequest{
		Model:       req.Model,
		System:      system,
		Messages:    messages,
		MaxTokens:   maxTokens,
		Temperature: req.Temperature,
	}
	var response anthropicResponse
	if err := callJSON(ctx, p.client, p.name, p.baseURL+"/v1/messages", p.authHeader(), body, &response, p.timeout, p.extraPorts); err != nil {
		return nil, err
	}
	var text strings.Builder
	for _, block := range response.Content {
		if block.Type == "text" {
			text.WriteString(block.Text)
		}
	}
	usage := Usage{Input: response.Usage.InputTokens, Output: response.Usage.OutputTokens}
	if req.Schema != nil {
		if err := req.Schema.Validate(text.String()); err != nil {
			return &Completion{Usage: usage}, err
		}
	}
	slog.DebugContext(ctx, "llm completion", "provider", p.name, "model", req.Model,
		"input_tokens", response.Usage.InputTokens, "output_tokens", response.Usage.OutputTokens)
	return &Completion{
		Text:  text.String(),
		Usage: usage,
	}, nil
}

// Embed reports that Anthropic has no embeddings API.
func (p *Anthropic) Embed(ctx context.Context, req EmbedRequest) (*EmbedResult, error) {
	return nil, fmt.Errorf("%w: provider %s has no embeddings API", ErrUnsupported, p.name)
}

// authHeader returns the API key and version headers. The key never reaches a
// log.
func (p *Anthropic) authHeader() http.Header {
	header := http.Header{}
	if p.apiKey != "" {
		header.Set("x-api-key", p.apiKey)
	}
	header.Set("anthropic-version", anthropicVersion)
	return header
}
