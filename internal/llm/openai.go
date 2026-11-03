package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"sort"
	"time"

	"github.com/L-K-M/Earnest/internal/outbound"
)

// OpenAI is a provider speaking the OpenAI chat-completions and embeddings
// dialect. The same dialect covers OpenAI itself and compatible servers such
// as Ollama and llama.cpp, which differ only in the base URL.
type OpenAI struct {
	name       string
	baseURL    string
	apiKey     string
	tokenField TokenField
	client     *outbound.Client
	timeout    time.Duration
	extraPorts []uint16
}

// Name returns the configured provider name.
func (p *OpenAI) Name() string { return p.name }

// openAIMessage is one chat turn on the wire.
type openAIMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// openAIResponseFormat asks for a JSON Schema-constrained completion.
type openAIResponseFormat struct {
	Type       string               `json:"type"`
	JSONSchema openAIJSONSchemaSpec `json:"json_schema"`
}

type openAIJSONSchemaSpec struct {
	Name   string          `json:"name"`
	Strict bool            `json:"strict"`
	Schema json.RawMessage `json:"schema"`
}

type openAIChatRequest struct {
	Model       string          `json:"model"`
	Messages    []openAIMessage `json:"messages"`
	Temperature float32         `json:"temperature,omitempty"`
	// Exactly one token field is set, chosen by the provider configuration.
	MaxCompletionTokens int                   `json:"max_completion_tokens,omitempty"`
	MaxTokens           int                   `json:"max_tokens,omitempty"`
	ResponseFormat      *openAIResponseFormat `json:"response_format,omitempty"`
}

type openAIChatResponse struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
	Usage struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
	} `json:"usage"`
}

// Complete performs a chat completion and validates structured output.
func (p *OpenAI) Complete(ctx context.Context, req CompleteRequest) (*Completion, error) {
	messages := make([]openAIMessage, 0, len(req.Messages)+1)
	if req.System != "" {
		messages = append(messages, openAIMessage{Role: string(RoleSystem), Content: req.System})
	}
	for _, message := range req.Messages {
		messages = append(messages, openAIMessage{Role: string(message.Role), Content: message.Content})
	}
	body := openAIChatRequest{Model: req.Model, Messages: messages, Temperature: req.Temperature}
	switch p.tokenField {
	case TokenFieldLegacy:
		body.MaxTokens = req.MaxTokens
	default:
		body.MaxCompletionTokens = req.MaxTokens
	}
	if req.Schema != nil {
		body.ResponseFormat = &openAIResponseFormat{
			Type: "json_schema",
			JSONSchema: openAIJSONSchemaSpec{
				Name:   req.Schema.Name,
				Strict: true,
				Schema: req.Schema.Definition,
			},
		}
	}
	var response openAIChatResponse
	if err := callJSON(ctx, p.client, p.name, p.baseURL+"/chat/completions", p.authHeader(), body, &response, p.timeout, p.extraPorts); err != nil {
		return nil, err
	}
	usage := Usage{Input: response.Usage.PromptTokens, Output: response.Usage.CompletionTokens}
	if len(response.Choices) == 0 {
		return &Completion{Usage: usage}, fmt.Errorf("llm: provider %s returned no choices", p.name)
	}
	text := response.Choices[0].Message.Content
	if req.Schema != nil {
		if err := req.Schema.Validate(text); err != nil {
			return &Completion{Usage: usage}, err
		}
	}
	slog.DebugContext(ctx, "llm completion", "provider", p.name, "model", req.Model,
		"input_tokens", response.Usage.PromptTokens, "output_tokens", response.Usage.CompletionTokens)
	return &Completion{
		Text:  text,
		Usage: usage,
	}, nil
}

type openAIEmbedRequest struct {
	Model      string   `json:"model"`
	Input      []string `json:"input"`
	Dimensions int      `json:"dimensions,omitempty"`
}

type openAIEmbedResponse struct {
	Data []struct {
		Index     int       `json:"index"`
		Embedding []float32 `json:"embedding"`
	} `json:"data"`
	Usage struct {
		PromptTokens int `json:"prompt_tokens"`
		TotalTokens  int `json:"total_tokens"`
	} `json:"usage"`
}

// Embed embeds every text, preserving input order.
func (p *OpenAI) Embed(ctx context.Context, req EmbedRequest) (*EmbedResult, error) {
	if len(req.Texts) == 0 {
		return &EmbedResult{}, nil
	}
	body := openAIEmbedRequest{Model: req.Model, Input: req.Texts, Dimensions: req.Dimensions}
	var response openAIEmbedResponse
	if err := callJSON(ctx, p.client, p.name, p.baseURL+"/embeddings", p.authHeader(), body, &response, p.timeout, p.extraPorts); err != nil {
		return nil, err
	}
	tokens := response.Usage.PromptTokens
	if tokens == 0 {
		tokens = response.Usage.TotalTokens
	}
	if len(response.Data) != len(req.Texts) {
		return &EmbedResult{Usage: Usage{Input: tokens}}, fmt.Errorf("llm: provider %s returned %d embeddings for %d texts", p.name, len(response.Data), len(req.Texts))
	}
	sort.SliceStable(response.Data, func(i, j int) bool { return response.Data[i].Index < response.Data[j].Index })
	vectors := make([][]float32, 0, len(response.Data))
	for _, item := range response.Data {
		vectors = append(vectors, item.Embedding)
	}
	slog.DebugContext(ctx, "llm embeddings", "provider", p.name, "model", req.Model, "texts", len(req.Texts), "tokens", tokens)
	return &EmbedResult{Vectors: vectors, Usage: Usage{Input: tokens}}, nil
}

// authHeader returns the bearer header, or an empty header for a local
// server that needs no key. The key never reaches a log.
func (p *OpenAI) authHeader() http.Header {
	header := http.Header{}
	if p.apiKey != "" {
		header.Set("Authorization", "Bearer "+p.apiKey)
	}
	return header
}
