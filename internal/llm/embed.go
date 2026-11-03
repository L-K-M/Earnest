package llm

import (
	"fmt"
	"math"
)

// StorageDimensions is the width of the embeddings.halfvec column. Every
// stored vector is exactly this long.
const StorageDimensions = 1024

// Vector is one embedding, normalised to StorageDimensions.
type Vector struct {
	// Values holds exactly StorageDimensions components.
	Values []float32
	// SourceDimensions is the model's own width before padding or truncation.
	SourceDimensions int
}

// NormaliseEmbedding pads or truncates vec to StorageDimensions and reports
// the model's own width.
//
// Shorter vectors are zero-padded, which the schema's CHECK constraint
// requires. Longer vectors are truncated only when truncatable is set: cutting
// a model that is not Matryoshka-style destroys its meaning, so a model that
// cannot be truncated is rejected instead.
func NormaliseEmbedding(vec []float32, truncatable bool) (Vector, error) {
	if len(vec) == 0 {
		return Vector{}, fmt.Errorf("%w: empty vector", ErrDimensions)
	}
	for i, v := range vec {
		if math.IsNaN(float64(v)) || math.IsInf(float64(v), 0) {
			return Vector{}, fmt.Errorf("%w: component %d is not finite", ErrDimensions, i)
		}
	}
	switch {
	case len(vec) == StorageDimensions:
		return Vector{Values: append([]float32(nil), vec...), SourceDimensions: StorageDimensions}, nil
	case len(vec) < StorageDimensions:
		padded := make([]float32, StorageDimensions)
		copy(padded, vec)
		return Vector{Values: padded, SourceDimensions: len(vec)}, nil
	default:
		if !truncatable {
			return Vector{}, fmt.Errorf("%w: %d components exceed %d and the model is not truncatable",
				ErrDimensions, len(vec), StorageDimensions)
		}
		return Vector{Values: append([]float32(nil), vec[:StorageDimensions]...), SourceDimensions: StorageDimensions}, nil
	}
}
