package pipeline

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
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
	// Submit returns an opaque handle, persisted unchanged for Poll and Cancel.
	Submit(context.Context, []llm.CompleteRequest) (string, error)
	Poll(context.Context, string, []llm.CompleteRequest) ([]string, bool, error)
	Cancel(context.Context, string) error
}

type batchKey struct{}
type batchContext struct{ batch EditionBatch }

const openAIBatchFinishStop = "stop"

// WithEditionBatch installs an optional governed batch transport on a worker
// context. A nil batch clears any inherited transport.
func WithEditionBatch(ctx context.Context, batch EditionBatch) context.Context {
	return context.WithValue(ctx, batchKey{}, batchContext{batch: batch})
}

// EditionBatchFrom returns the configured transport; nil means synchronous fallback.
func EditionBatchFrom(ctx context.Context) EditionBatch {
	value, _ := ctx.Value(batchKey{}).(batchContext)
	return value.batch
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
	route, cfg, err := b.route()
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
	if result == nil || result.Response == nil {
		return nil, fmt.Errorf("batch transport: %w", err)
	}
	defer result.Response.Body.Close()
	if result.Response.StatusCode < 200 || result.Response.StatusCode >= 300 {
		return nil, &llm.Error{Provider: route.Provider.Name(), StatusCode: result.Response.StatusCode, Err: llm.ErrUnsupported}
	}
	if err != nil {
		return nil, fmt.Errorf("batch transport: %w", err)
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
	var lines bytes.Buffer
	price := b.models[route.Model]
	var reserved llm.USD
	for i, req := range requests {
		if req.Schema == nil {
			return "", llm.ErrInvalidOutput
		}
		if req.MaxTokens <= 0 {
			req.MaxTokens = llm.DefaultMaxTokens
		}
		messages := []map[string]string{{"role": "system", "content": req.System}}
		for _, m := range req.Messages {
			messages = append(messages, map[string]string{"role": string(m.Role), "content": m.Content})
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
		estimate, err := llm.EstimateCompletion(req, price)
		if err != nil {
			return "", err
		}
		reserved, err = llm.AddEstimates(reserved, estimate)
		if err != nil {
			return "", err
		}
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
	id, err := b.budget.Reserve(ctx, route.Provider.Name(), reserved)
	if err != nil {
		return "", err
	}
	raw, err := b.request(ctx, http.MethodPost, "/files", writer.FormDataContentType(), &upload)
	if err != nil {
		return "", errors.Join(err, b.budget.Reconcile(context.WithoutCancel(ctx), route.Provider.Name(), id, 0))
	}
	var file struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(raw, &file); err != nil {
		return "", errors.Join(fmt.Errorf("batch file response: %w", err), b.budget.Reconcile(context.WithoutCancel(ctx), route.Provider.Name(), id, 0))
	}
	if file.ID == "" {
		return "", errors.Join(llm.ErrInvalidOutput, b.budget.Reconcile(context.WithoutCancel(ctx), route.Provider.Name(), id, 0))
	}
	data, err := json.Marshal(map[string]string{"input_file_id": file.ID, "endpoint": "/v1/chat/completions", "completion_window": "24h"})
	if err != nil {
		return "", fmt.Errorf("batch submission: %w", err)
	}
	raw, err = b.request(ctx, http.MethodPost, "/batches", "application/json", bytes.NewReader(data))
	if err != nil {
		var providerErr *llm.Error
		if errors.As(err, &providerErr) && providerErr.StatusCode >= 400 && providerErr.StatusCode < 500 && providerErr.StatusCode != http.StatusRequestTimeout {
			return "", errors.Join(err, b.budget.Reconcile(context.WithoutCancel(ctx), route.Provider.Name(), id, 0))
		}
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
	// The reservation travels with the persisted job so another process can
	// settle it after polling, including when submission crossed a month end.
	return batch.ID + ":" + id, nil
}

func (b *openAIEditionBatch) Poll(ctx context.Context, id string, requests []llm.CompleteRequest) ([]string, bool, error) {
	id, reservation, _ := strings.Cut(id, ":")
	raw, err := b.request(ctx, http.MethodGet, "/batches/"+url.PathEscape(id), "application/json", nil)
	if err != nil {
		return nil, false, err
	}
	var batch struct {
		Status string `json:"status"`
		Output string `json:"output_file_id"`
		Errors string `json:"error_file_id"`
	}
	if err := json.Unmarshal(raw, &batch); err != nil {
		return nil, false, fmt.Errorf("batch status: %w", err)
	}
	switch batch.Status {
	case "failed", "expired", "cancelled", "completed":
	default:
		return nil, false, nil
	}
	var rows bytes.Buffer
	for _, file := range []string{batch.Output, batch.Errors} {
		if file == "" {
			continue
		}
		raw, err = b.request(ctx, http.MethodGet, "/files/"+url.PathEscape(file)+"/content", "application/json", nil)
		if err != nil {
			return nil, false, err
		}
		rows.Write(raw)
		rows.WriteByte('\n')
	}
	route, _, err := b.route()
	if err != nil {
		return nil, false, err
	}
	price := b.models[route.Model]
	output := make([]string, len(requests))
	seen := make(map[int]bool)
	var actual llm.USD
	var outputErr error
	unknown := false
	scanner := bufio.NewScanner(&rows)
	scanner.Buffer(make([]byte, 4096), 1<<20)
	for scanner.Scan() {
		if len(bytes.TrimSpace(scanner.Bytes())) == 0 {
			continue
		}
		var row struct {
			ID    string `json:"custom_id"`
			Error *struct {
				Code string `json:"code"`
			} `json:"error"`
			Response struct {
				Status int `json:"status_code"`
				Body   struct {
					Usage *struct {
						Input  int `json:"prompt_tokens"`
						Output int `json:"completion_tokens"`
					} `json:"usage"`
					Choices []struct {
						FinishReason string `json:"finish_reason"`
						Message      struct {
							Content string `json:"content"`
						} `json:"message"`
					} `json:"choices"`
				} `json:"body"`
			} `json:"response"`
		}
		if err := json.Unmarshal(scanner.Bytes(), &row); err != nil {
			unknown = true
			outputErr = fmt.Errorf("batch output line: %w", err)
			continue
		}
		i, err := strconv.Atoi(row.ID)
		if err != nil || i < 0 || i >= len(requests) || seen[i] {
			unknown = true
			outputErr = llm.ErrInvalidOutput
			continue
		}
		seen[i] = true
		if usage := row.Response.Body.Usage; usage != nil && usage.Input >= 0 && usage.Output >= 0 {
			cost, err := price.CompletionCost(llm.Usage{Input: usage.Input, Output: usage.Output})
			if err != nil {
				return nil, false, err
			}
			actual, err = llm.AddEstimates(actual, cost)
			if err != nil {
				return nil, false, err
			}
		} else if row.Error != nil && (row.Error.Code == "batch_expired" || row.Error.Code == "batch_cancelled") {
			// The provider did not execute these requests; partial successes
			// elsewhere in the same batch still contribute their billed usage.
		} else if row.Response.Status < 400 || row.Response.Status >= 500 || row.Response.Status == http.StatusRequestTimeout {
			unknown = true
		}
		if row.Response.Status != http.StatusOK || len(row.Response.Body.Choices) != 1 {
			outputErr = llm.ErrInvalidOutput
			continue
		}
		if reason := row.Response.Body.Choices[0].FinishReason; reason != "" && reason != openAIBatchFinishStop {
			outputErr = fmt.Errorf("%w: provider %s stopped early: finish_reason %s", llm.ErrInvalidOutput, route.Provider.Name(), reason)
			continue
		}
		text := row.Response.Body.Choices[0].Message.Content
		if err := requests[i].Schema.Validate(text); err != nil {
			outputErr = err
			continue
		}
		output[i] = text
	}
	if err := scanner.Err(); err != nil {
		return nil, false, fmt.Errorf("scan batch: %w", err)
	}
	// A failed batch without output never passed provider validation. Other
	// terminal batches may have billed partial work; refund only known usage.
	if reservation != "" && ((!unknown && len(seen) == len(requests)) || (batch.Status == "failed" && batch.Output == "" && batch.Errors == "")) {
		if err := b.budget.Reconcile(context.WithoutCancel(ctx), route.Provider.Name(), reservation, actual); err != nil {
			return nil, false, errors.Join(outputErr, err)
		}
	}
	if batch.Status != "completed" {
		return nil, false, errors.Join(outputErr, llm.ErrUnsupported)
	}
	if outputErr != nil {
		return nil, false, outputErr
	}
	for _, text := range output {
		if text == "" {
			return nil, false, llm.ErrInvalidOutput
		}
	}
	return output, true, nil
}

func (b *openAIEditionBatch) Cancel(ctx context.Context, id string) error {
	id, _, _ = strings.Cut(id, ":")
	// Cancellation is asynchronous and can include billed partial work. Its
	// estimate remains charged until terminal usage is known or the hold expires.
	_, err := b.request(ctx, http.MethodPost, "/batches/"+url.PathEscape(id)+"/cancel", "application/json", strings.NewReader("{}"))
	return err
}
