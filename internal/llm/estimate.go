package llm

import (
	"fmt"
	"math"
	"math/bits"
)

// DefaultMaxTokens bounds output when the caller supplies no maximum.
const DefaultMaxTokens = 4096

// promptOverheadTokens covers message framing and provider-added schema text.
// Each UTF-8 input byte is also reserved as one token, including schema bytes.
const promptOverheadTokens = 256

// EstimateCompletion conservatively prices input bytes, message framing, schema
// and maximum output tokens. Nonpositive MaxTokens uses DefaultMaxTokens, which
// callers must also send to the provider. Fractional micro-dollars round up.
func EstimateCompletion(req CompleteRequest, price ModelInfo) (USD, error) {
	input := promptOverheadTokens + len(req.System)
	for _, message := range req.Messages {
		input += promptOverheadTokens + len(message.Content) + len(message.Role)
	}
	if req.Schema != nil {
		input += len(req.Schema.Definition) + len(req.Schema.Name)
	}
	output := req.MaxTokens
	if output <= 0 {
		output = DefaultMaxTokens
	}
	return price.CompletionCost(Usage{Input: input, Output: output})
}

// CompletionCost prices billed completion usage, rounding each charge up to a
// micro-dollar so repeated fractional charges cannot evade the monthly cap.
func (price ModelInfo) CompletionCost(usage Usage) (USD, error) {
	in, err := estimateTokens(price.InputPer1M, usage.Input)
	if err != nil {
		return 0, err
	}
	out, err := estimateTokens(price.OutputPer1M, usage.Output)
	if err != nil {
		return 0, err
	}
	return AddEstimates(in, out)
}

// EmbeddingCost prices billed embedding usage, rounding up to a micro-dollar.
func (price ModelInfo) EmbeddingCost(usage Usage) (USD, error) {
	return estimateTokens(price.EmbeddingPer1M, usage.Input)
}

func estimateEmbedding(req EmbedRequest, price ModelInfo) (USD, error) {
	var tokens int
	for _, text := range req.Texts {
		tokens += len(text) + promptOverheadTokens
	}
	return price.EmbeddingCost(Usage{Input: tokens})
}

func estimateTokens(price USD, tokens int) (USD, error) {
	if price < 0 || tokens < 0 {
		return 0, fmt.Errorf("llm: negative price or token estimate")
	}
	hi, lo := bits.Mul64(uint64(price), uint64(tokens))
	if hi >= 1000000 {
		return 0, fmt.Errorf("llm: cost estimate overflow")
	}
	quotient, remainder := bits.Div64(hi, lo, 1000000)
	if quotient > math.MaxInt64 || (quotient == math.MaxInt64 && remainder != 0) {
		return 0, fmt.Errorf("llm: cost estimate overflow")
	}
	if remainder != 0 {
		quotient++
	}
	return USD(quotient), nil
}

// AddEstimates totals individually bounded requests without money overflow.
func AddEstimates(a, b USD) (USD, error) {
	if a < 0 || b < 0 || a > math.MaxInt64-b {
		return 0, fmt.Errorf("llm: cost estimate overflow")
	}
	return a + b, nil
}
