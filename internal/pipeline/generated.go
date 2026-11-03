package pipeline

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/L-K-M/Earnest/internal/llm"
)

// Complete routes and budgets a structured completion, validating even custom providers.
func (e *Engine) Complete(ctx context.Context, task llm.Task, req llm.CompleteRequest) (*llm.Completion, error) {
	if !e.Enabled(task) {
		return nil, llm.ErrDisabled
	}
	if req.Schema == nil {
		return nil, fmt.Errorf("schema required: %w", llm.ErrInvalidOutput)
	}
	out, err := e.client.Complete(ctx, task, req)
	if err != nil {
		return nil, err
	}
	if out == nil {
		return nil, llm.ErrInvalidOutput
	}
	if err := req.Schema.Validate(out.Text); err != nil {
		return nil, err
	}
	if req.Schema.Name == "summary" || req.Schema.Name == "blurb" {
		if err := validateSummaryText(out.Text, req.Schema.Name); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// Reject text that cannot be stored before it can poison a content-shared cache.
func validateSummaryText(output, kind string) error {
	const summaryCharacters = 8192
	const blurbCharacters = 512
	var result struct {
		Claims []Claim `json:"claims"`
	}
	if err := json.Unmarshal([]byte(output), &result); err != nil {
		return fmt.Errorf("decode generated text: %w", err)
	}
	limit := summaryCharacters
	if kind == "blurb" {
		limit = blurbCharacters
	}
	length := max(0, len(result.Claims)-1)
	for _, claim := range result.Claims {
		length += utf8.RuneCountInString(claim.Text)
		for _, r := range claim.Text {
			if unicode.IsControl(r) && !unicode.IsSpace(r) {
				return fmt.Errorf("generated text contains control characters: %w", llm.ErrInvalidOutput)
			}
		}
		if strings.Contains(strings.ToLower(claim.Text), "<img") {
			return fmt.Errorf("generated text contains an image: %w", llm.ErrInvalidOutput)
		}
	}
	if length > limit {
		return fmt.Errorf("generated text exceeds %d characters: %w", limit, llm.ErrInvalidOutput)
	}
	return nil
}

type taskCompleter struct {
	engine *Engine
	task   llm.Task
}

func (c taskCompleter) Complete(ctx context.Context, _ llm.Task, req llm.CompleteRequest) (*llm.Completion, error) {
	return c.engine.Complete(ctx, c.task, req)
}

// Stripes bound memory while coalescing misses from interactive and edition callers.
var generatedLocks = func() [64]chan struct{} {
	var locks [64]chan struct{}
	for i := range locks {
		locks[i] = make(chan struct{}, 1)
	}
	return locks
}()

// Summarize returns content-shared claims. Private output is partitioned by owner.
// The source is the complete stored plain-text version, without annotations.
func (e *Engine) Summarize(ctx context.Context, owner *int64, source string, kind llm.CacheKind) ([]Claim, string, error) {
	task, version := llm.TaskSummary, SummaryPromptVersion
	if kind == llm.CacheBlurb {
		task, version = llm.TaskBlurb, BlurbPromptVersion
	} else if kind != llm.CacheSummary {
		return nil, "", llm.ErrInvalidOutput
	}
	model, err := e.Model(task)
	if err != nil {
		return nil, "", err
	}
	key := llm.ContentKey(owner, source, kind, version, model)
	lock := generatedLocks[int(key.ContentHash[0])%len(generatedLocks)]
	select {
	case lock <- struct{}{}:
		defer func() { <-lock }()
	case <-ctx.Done():
		return nil, "", ctx.Err()
	}
	cached, err := e.cache.Get(ctx, key)
	if err == nil {
		var claims []Claim
		if err := json.Unmarshal([]byte(cached.Output), &claims); err != nil {
			return nil, "", fmt.Errorf("cached summary: %w", err)
		}
		for i := range claims {
			claims[i].Verified = VerifyPassages(source, claims[i].Passages)
		}
		return claims, model, nil
	}
	if !errors.Is(err, llm.ErrMiss) {
		return nil, "", err
	}
	var claims []Claim
	if kind == llm.CacheBlurb {
		claim, err := GenerateBlurb(ctx, taskCompleter{e, task}, source)
		if err != nil {
			return nil, "", err
		}
		claims = []Claim{claim}
	} else {
		claims, err = GenerateSummary(ctx, e, source)
		if err != nil {
			return nil, "", err
		}
	}
	body, err := json.Marshal(claims)
	if err != nil {
		return nil, "", fmt.Errorf("encode summary: %w", err)
	}
	if err := e.cache.Put(ctx, key, string(body), nil); err != nil {
		return nil, "", err
	}
	return claims, model, nil
}
