// Package llm is Earnest's model provider layer: chat completions,
// embeddings, per-task routing, spend budgets and the output cache.
//
// Earnest's own models read feed content written by strangers. They never
// receive tools, and their output is validated against a caller-supplied JSON
// schema before it is used, so a prompt-injected article cannot make the
// pipeline act or pass forged structured output through. See
// docs/research/llm.md, threat model invariant 1.
package llm

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// Role is the author of one message.
type Role string

const (
	RoleSystem    Role = "system"
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
)

// Message is one conversation turn. Content is plain text; feed content is
// untrusted and belongs in Messages, never in the system instruction.
type Message struct {
	Role    Role
	Content string
}

// Usage counts the tokens a provider billed for one call.
type Usage struct {
	Input  int
	Output int
}

// Completion is one chat completion.
type Completion struct {
	Text  string
	Usage Usage
}

// EmbedResult is one provider embedding call, before normalisation.
type EmbedResult struct {
	// Vectors holds one raw vector per input text, in input order.
	Vectors [][]float32
	Usage   Usage
}

// CompleteRequest is one chat completion.
//
// It deliberately has no field that could carry a tool definition: a model
// that reads feed content must not be able to act. The type system enforces
// this as far as it can and TestNoTools guards the rest.
type CompleteRequest struct {
	// Model is the model to call. The router fills it in from configuration.
	Model string
	// System is the instruction prefix. It must frame feed content as data.
	System string
	// Messages are the conversation turns after the system message.
	Messages []Message
	// Schema requests structured output. When set, the provider validates the
	// model's text against it and rejects invalid output.
	Schema *Schema
	// MaxTokens bounds the completion. The governed Client uses DefaultMaxTokens
	// when unset, and sends that same bound to every provider.
	MaxTokens int
	// Temperature controls sampling. Zero uses the provider default.
	Temperature float32
}

// EmbedRequest is one embedding call.
type EmbedRequest struct {
	// Model is the model name, recorded with every stored vector.
	Model string
	// Texts are the inputs, embedded in order.
	Texts []string
	// Dimensions requests a vector width from providers that support it.
	// Zero uses the provider default.
	Dimensions int
}

// Provider is one model API. Every request leaves Earnest through
// internal/outbound with Purpose llm, so the shared address checks, host
// spacing and private-address allowlist apply.
type Provider interface {
	// Name is the configured provider name, used for budgets and logs.
	Name() string
	// Complete performs a chat completion. On rejected output, a non-nil
	// result retains billed usage with no usable text.
	Complete(ctx context.Context, req CompleteRequest) (*Completion, error)
	// Embed performs an embedding call, retaining billed partial usage on error.
	// A provider without an embeddings API
	// returns ErrUnsupported.
	Embed(ctx context.Context, req EmbedRequest) (*EmbedResult, error)
}

// Sentinel errors. Callers classify failures with errors.Is.
var (
	// ErrDisabled reports a task with no configured provider and model.
	ErrDisabled = errors.New("llm: task is not configured")
	// ErrUnsupported reports an operation the provider cannot perform.
	ErrUnsupported = errors.New("llm: operation is not supported by this provider")
	// ErrMiss reports an absent cache entry.
	ErrMiss = errors.New("llm: cache entry not found")
	// ErrInvalidOutput reports model output that does not match the
	// requested schema.
	ErrInvalidOutput = errors.New("llm: model output is invalid")
	// ErrDimensions reports an embedding vector that cannot be normalised.
	ErrDimensions = errors.New("llm: embedding dimensions cannot be normalised")
)

// ErrBudgetExhausted reports that a provider's monthly spend limit is
// reached. The pipeline pauses rather than failing the operation.
var ErrBudgetExhausted = errors.New("llm: provider budget exhausted")

// BudgetError describes a refused call because a provider reached its
// monthly limit. It matches ErrBudgetExhausted with errors.Is.
type BudgetError struct {
	Provider string
	Month    time.Time
	Spent    USD
	Limit    USD
}

func (e *BudgetError) Error() string {
	return fmt.Sprintf("llm: provider %s spent %s of %s in %s",
		e.Provider, e.Spent, e.Limit, e.Month.Format("2006-01"))
}

// Is lets errors.Is(err, ErrBudgetExhausted) succeed.
func (e *BudgetError) Is(target error) bool { return target == ErrBudgetExhausted }

// Unwrap keeps the sentinel in the chain.
func (e *BudgetError) Unwrap() error { return ErrBudgetExhausted }

// Error describes a provider request failure.
type Error struct {
	Provider string
	// StatusCode is the HTTP status, or zero for a transport failure.
	StatusCode int
	// Retryable reports whether repeating the call could succeed.
	Retryable bool
	Err       error
}

func (e *Error) Error() string {
	if e.StatusCode != 0 {
		return fmt.Sprintf("llm: provider %s returned HTTP %d: %v", e.Provider, e.StatusCode, e.Err)
	}
	return fmt.Sprintf("llm: provider %s: %v", e.Provider, e.Err)
}

func (e *Error) Unwrap() error { return e.Err }
