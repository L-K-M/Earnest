package llm

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/L-K-M/Earnest/internal/outbound"
)

// Gemini is a provider for Google's generateContent and embedContent APIs.
// The base URL includes the API version, so an operator can move to a newer
// one by changing configuration.
type Gemini struct {
	name       string
	baseURL    string
	apiKey     string
	client     *outbound.Client
	timeout    time.Duration
	extraPorts []uint16
}

// Name returns the configured provider name.
func (p *Gemini) Name() string { return p.name }

// geminiPart is one piece of a prompt or a response.
type geminiPart struct {
	Text string `json:"text"`
}

// geminiContent is one prompt role's content.
type geminiContent struct {
	Role  string       `json:"role,omitempty"`
	Parts []geminiPart `json:"parts"`
}

type geminiGenerationConfig struct {
	MaxOutputTokens  int     `json:"maxOutputTokens,omitempty"`
	Temperature      float32 `json:"temperature,omitempty"`
	ResponseMimeType string  `json:"responseMimeType,omitempty"`
}

type geminiRequest struct {
	SystemInstruction *geminiContent         `json:"systemInstruction,omitempty"`
	Contents          []geminiContent        `json:"contents"`
	GenerationConfig  geminiGenerationConfig `json:"generationConfig,omitempty"`
}

type geminiResponse struct {
	Candidates []struct {
		Content struct {
			Parts []geminiPart `json:"parts"`
		} `json:"content"`
		FinishReason string `json:"finishReason"`
	} `json:"candidates"`
	UsageMetadata struct {
		PromptTokenCount     int `json:"promptTokenCount"`
		CandidatesTokenCount int `json:"candidatesTokenCount"`
		TotalTokenCount      int `json:"totalTokenCount"`
	} `json:"usageMetadata"`
}

// Complete performs a generateContent call and validates structured output.
// The schema is enforced by Validate; responseMimeType asks the model for
// JSON so the common case needs no repair.
func (p *Gemini) Complete(ctx context.Context, req CompleteRequest) (*Completion, error) {
	body := geminiRequest{
		Contents: geminiContents(req.Messages),
		GenerationConfig: geminiGenerationConfig{
			MaxOutputTokens:  req.MaxTokens,
			Temperature:      req.Temperature,
			ResponseMimeType: jsonMimeType(req),
		},
	}
	if req.System != "" {
		body.SystemInstruction = &geminiContent{Parts: []geminiPart{{Text: req.System}}}
	}
	var response geminiResponse
	endpoint := fmt.Sprintf("%s/models/%s:generateContent", p.baseURL, url.PathEscape(modelName(req.Model)))
	if err := callJSON(ctx, p.client, p.name, endpoint, p.authHeader(), body, &response, p.timeout, p.extraPorts); err != nil {
		return nil, err
	}
	usage := Usage{Input: response.UsageMetadata.PromptTokenCount, Output: response.UsageMetadata.CandidatesTokenCount}
	if len(response.Candidates) == 0 {
		return &Completion{Usage: usage}, fmt.Errorf("llm: provider %s returned no candidates", p.name)
	}
	var text strings.Builder
	for _, part := range response.Candidates[0].Content.Parts {
		text.WriteString(part.Text)
	}
	if req.Schema != nil {
		if err := req.Schema.Validate(text.String()); err != nil {
			return &Completion{Usage: usage}, err
		}
	}
	slog.DebugContext(ctx, "llm completion", "provider", p.name, "model", req.Model,
		"input_tokens", response.UsageMetadata.PromptTokenCount, "output_tokens", response.UsageMetadata.CandidatesTokenCount)
	return &Completion{
		Text:  text.String(),
		Usage: usage,
	}, nil
}

type geminiEmbedRequest struct {
	Content struct {
		Parts []geminiPart `json:"parts"`
	} `json:"content"`
	OutputDimensionality int `json:"outputDimensionality,omitempty"`
}

type geminiEmbedResponse struct {
	Embedding struct {
		Values []float32 `json:"values"`
	} `json:"embedding"`
}

// Embed embeds every text. Gemini's embedContent takes one text per call, so
// a batch is a sequence of requests sharing one provider and budget. The
// response carries no token count, so Usage stays zero and embedding spend
// for Gemini is not metered.
func (p *Gemini) Embed(ctx context.Context, req EmbedRequest) (*EmbedResult, error) {
	if len(req.Texts) == 0 {
		return &EmbedResult{}, nil
	}
	vectors := make([][]float32, 0, len(req.Texts))
	for _, text := range req.Texts {
		var body geminiEmbedRequest
		body.Content.Parts = []geminiPart{{Text: text}}
		body.OutputDimensionality = req.Dimensions
		var response geminiEmbedResponse
		endpoint := fmt.Sprintf("%s/models/%s:embedContent", p.baseURL, url.PathEscape(modelName(req.Model)))
		if err := callJSON(ctx, p.client, p.name, endpoint, p.authHeader(), body, &response, p.timeout, p.extraPorts); err != nil {
			return nil, err
		}
		vectors = append(vectors, response.Embedding.Values)
	}
	slog.DebugContext(ctx, "llm embeddings", "provider", p.name, "model", req.Model, "texts", len(req.Texts))
	return &EmbedResult{Vectors: vectors, Usage: Usage{}}, nil
}

// authHeader returns the API key header. The key never reaches a log.
func (p *Gemini) authHeader() http.Header {
	header := http.Header{}
	if p.apiKey != "" {
		header.Set("x-goog-api-key", p.apiKey)
	}
	return header
}

// geminiContents maps the conversation to Gemini's role names. Gemini
// calls the assistant role "model".
func geminiContents(messages []Message) []geminiContent {
	contents := make([]geminiContent, 0, len(messages))
	for _, message := range messages {
		role := message.Role
		if role == RoleAssistant {
			role = "model"
		}
		contents = append(contents, geminiContent{Role: string(role), Parts: []geminiPart{{Text: message.Content}}})
	}
	return contents
}

// modelName strips a leading models/ path so an operator can paste either
// form.
func modelName(model string) string {
	return strings.TrimPrefix(model, "models/")
}

// jsonMimeType asks for JSON when a schema is set, and leaves the provider
// default otherwise.
func jsonMimeType(req CompleteRequest) string {
	if req.Schema != nil {
		return "application/json"
	}
	return ""
}
