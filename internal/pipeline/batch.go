package pipeline

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/L-K-M/Earnest/internal/llm"
	"github.com/L-K-M/Earnest/internal/outbound"
)

// EditionBatch groups structured requests submitted ahead of an edition's deadline.
// Implementations must route and budget submissions and expose no tools.
type EditionBatch interface {
	Submit(context.Context, []llm.CompleteRequest) (string, error)
	Poll(context.Context, string, []llm.CompleteRequest) ([]string, bool, error)
	Cancel(context.Context, string) error
}

type batchKey struct{}

// WithEditionBatch installs an optional governed batch transport on a worker context.
func WithEditionBatch(ctx context.Context, batch EditionBatch) context.Context {
	return context.WithValue(ctx, batchKey{}, batch)
}

// EditionBatchFrom returns the configured transport; nil means synchronous fallback.
func EditionBatchFrom(ctx context.Context) EditionBatch {
	batch, _ := ctx.Value(batchKey{}).(EditionBatch)
	return batch
}

type openAIEditionBatch struct {
	router   *llm.Router
	budget   *llm.Budget
	models   map[string]llm.ModelInfo
	config   llm.Config
	outbound *outbound.Client
}

// NewEditionBatch enables the OpenAI-compatible batch protocol. Other dialects
// return nil and use synchronous generation through the ordinary client.
func NewEditionBatch(router *llm.Router, budget *llm.Budget, config llm.Config, client *outbound.Client) EditionBatch {
	if router == nil || budget == nil || client == nil || !router.Enabled(llm.TaskEdition) {
		return nil
	}
	task := config.Tasks[llm.TaskEdition]
	if config.Providers[task.Provider].Kind != llm.KindOpenAI {
		return nil
	}
	return &openAIEditionBatch{router: router, budget: budget, models: config.Models, config: config, outbound: client}
}

func (b *openAIEditionBatch) route() (llm.Route, llm.ProviderConfig, error) {
	route, err := b.router.Task(llm.TaskEdition)
	if err != nil {
		return llm.Route{}, llm.ProviderConfig{}, err
	}
	return route, b.config.Providers[route.Provider.Name()], nil
}

func (b *openAIEditionBatch) request(ctx context.Context, method, path, contentType string, body io.Reader) ([]byte, error) {
	_, cfg, err := b.route()
	if err != nil {
		return nil, err
	}
	u, err := url.Parse(strings.TrimRight(cfg.BaseURL, "/") + path)
	if err != nil {
		return nil, fmt.Errorf("batch URL: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, method, u.String(), body)
	if err != nil {
		return nil, fmt.Errorf("batch request: %w", err)
	}
	req.Header.Set("Content-Type", contentType)
	if cfg.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+cfg.APIKey)
	}
	var ports []uint16
	if port, err := strconv.Atoi(u.Port()); err == nil && port > 0 && port <= 65535 {
		ports = []uint16{uint16(port)}
	}
	result, err := b.outbound.Do(req, outbound.Policy{Purpose: outbound.LLM, Redirect: outbound.RedirectNone, ExtraPorts: ports, MaxBodySize: 16 << 20, Timeout: 2 * time.Minute})
	if err != nil {
		return nil, fmt.Errorf("batch transport: %w", err)
	}
	defer result.Response.Body.Close()
	if result.Response.StatusCode < 200 || result.Response.StatusCode >= 300 {
		return nil, fmt.Errorf("batch HTTP %d: %w", result.Response.StatusCode, llm.ErrUnsupported)
	}
	data, err := io.ReadAll(result.Response.Body)
	if err != nil {
		return nil, fmt.Errorf("read batch: %w", err)
	}
	return data, nil
}

func (b *openAIEditionBatch) Submit(ctx context.Context, requests []llm.CompleteRequest) (string, error) {
	route, cfg, err := b.route()
	if err != nil {
		return "", err
	}
	if err := b.budget.Authorize(ctx, route.Provider.Name()); err != nil {
		return "", err
	}
	var lines bytes.Buffer
	price := b.models[route.Model]
	var reserved llm.USD
	for i, req := range requests {
		if req.Schema == nil {
			return "", llm.ErrInvalidOutput
		}
		messages := []map[string]string{{"role": "system", "content": req.System}}
		inputBytes := len(req.System) + len(req.Schema.Definition)
		for _, m := range req.Messages {
			messages = append(messages, map[string]string{"role": string(m.Role), "content": m.Content})
			inputBytes += len(m.Content)
		}
		body := map[string]any{"model": route.Model, "messages": messages, "response_format": map[string]any{"type": "json_schema", "json_schema": map[string]any{"name": req.Schema.Name, "strict": true, "schema": req.Schema.Definition}}}
		tokenField := "max_completion_tokens"
		if cfg.TokenField != "" {
			tokenField = string(cfg.TokenField)
		}
		body[tokenField] = req.MaxTokens
		if err := json.NewEncoder(&lines).Encode(map[string]any{"custom_id": fmt.Sprint(i), "method": "POST", "url": "/v1/chat/completions", "body": body}); err != nil {
			return "", fmt.Errorf("batch JSONL: %w", err)
		}
		// Reserve a conservative full-price byte-fallback upper bound before submission.
		// Batch usage has no refund primitive in the shared spend ledger.
		reserved += price.InputPer1M.MulTokens(inputBytes) + price.OutputPer1M.MulTokens(req.MaxTokens)
	}
	spend, err := b.budget.Spend(ctx, route.Provider.Name())
	if err != nil {
		return "", err
	}
	if !spend.Unlimited() && reserved > spend.Limit-spend.Spent {
		return "", &llm.BudgetError{Provider: spend.Provider, Month: spend.Month, Spent: spend.Spent, Limit: spend.Limit}
	}
	var upload bytes.Buffer
	writer := multipart.NewWriter(&upload)
	if err := writer.WriteField("purpose", "batch"); err != nil {
		return "", fmt.Errorf("batch purpose: %w", err)
	}
	part, err := writer.CreateFormFile("file", "edition.jsonl")
	if err != nil {
		return "", fmt.Errorf("batch file: %w", err)
	}
	if _, err := part.Write(lines.Bytes()); err != nil {
		return "", fmt.Errorf("batch content: %w", err)
	}
	if err := writer.Close(); err != nil {
		return "", fmt.Errorf("batch multipart: %w", err)
	}
	raw, err := b.request(ctx, http.MethodPost, "/files", writer.FormDataContentType(), &upload)
	if err != nil {
		return "", err
	}
	var file struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(raw, &file); err != nil {
		return "", fmt.Errorf("batch file response: %w", err)
	}
	if file.ID == "" {
		return "", llm.ErrInvalidOutput
	}
	if err := b.budget.Record(ctx, route.Provider.Name(), reserved); err != nil {
		return "", err
	}
	data, err := json.Marshal(map[string]string{"input_file_id": file.ID, "endpoint": "/v1/chat/completions", "completion_window": "24h"})
	if err != nil {
		return "", fmt.Errorf("batch submission: %w", err)
	}
	raw, err = b.request(ctx, http.MethodPost, "/batches", "application/json", bytes.NewReader(data))
	if err != nil {
		return "", err
	}
	var batch struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(raw, &batch); err != nil {
		return "", fmt.Errorf("batch response: %w", err)
	}
	if batch.ID == "" {
		return "", llm.ErrInvalidOutput
	}
	return batch.ID, nil
}

func (b *openAIEditionBatch) Poll(ctx context.Context, id string, requests []llm.CompleteRequest) ([]string, bool, error) {
	raw, err := b.request(ctx, http.MethodGet, "/batches/"+url.PathEscape(id), "application/json", nil)
	if err != nil {
		return nil, false, err
	}
	var batch struct {
		Status string `json:"status"`
		Output string `json:"output_file_id"`
	}
	if err := json.Unmarshal(raw, &batch); err != nil {
		return nil, false, fmt.Errorf("batch status: %w", err)
	}
	switch batch.Status {
	case "failed", "expired", "cancelled":
		return nil, false, llm.ErrUnsupported
	case "completed":
	default:
		return nil, false, nil
	}
	if batch.Output == "" {
		return nil, false, llm.ErrInvalidOutput
	}
	raw, err = b.request(ctx, http.MethodGet, "/files/"+url.PathEscape(batch.Output)+"/content", "application/json", nil)
	if err != nil {
		return nil, false, err
	}
	output := make([]string, len(requests))
	scanner := bufio.NewScanner(bytes.NewReader(raw))
	scanner.Buffer(make([]byte, 4096), 1<<20)
	for scanner.Scan() {
		var row struct {
			ID       string `json:"custom_id"`
			Response struct {
				Status int `json:"status_code"`
				Body   struct {
					Choices []struct {
						Message struct {
							Content string `json:"content"`
						} `json:"message"`
					} `json:"choices"`
				} `json:"body"`
			} `json:"response"`
		}
		if err := json.Unmarshal(scanner.Bytes(), &row); err != nil {
			return nil, false, fmt.Errorf("batch output line: %w", err)
		}
		i, err := strconv.Atoi(row.ID)
		if err != nil || i < 0 || i >= len(requests) || output[i] != "" || row.Response.Status != http.StatusOK || len(row.Response.Body.Choices) != 1 {
			return nil, false, llm.ErrInvalidOutput
		}
		text := row.Response.Body.Choices[0].Message.Content
		if err := requests[i].Schema.Validate(text); err != nil {
			return nil, false, err
		}
		output[i] = text
	}
	if err := scanner.Err(); err != nil {
		return nil, false, fmt.Errorf("scan batch: %w", err)
	}
	for _, text := range output {
		if text == "" {
			return nil, false, llm.ErrInvalidOutput
		}
	}
	return output, true, nil
}

func (b *openAIEditionBatch) Cancel(ctx context.Context, id string) error {
	_, err := b.request(ctx, http.MethodPost, "/batches/"+url.PathEscape(id)+"/cancel", "application/json", strings.NewReader("{}"))
	return err
}
