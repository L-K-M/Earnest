package llm

import (
	"errors"
	"math"
	"testing"
)

func TestNormaliseEmbedding(t *testing.T) {
	t.Run("exact width", func(t *testing.T) {
		vector := make([]float32, StorageDimensions)
		for i := range vector {
			vector[i] = float32(i) / 1000
		}
		got, err := NormaliseEmbedding(vector, false)
		if err != nil {
			t.Fatal(err)
		}
		if len(got.Values) != StorageDimensions || got.SourceDimensions != StorageDimensions {
			t.Fatalf("vector = %d components, source %d", len(got.Values), got.SourceDimensions)
		}
		if got.Values[10] != 0.01 {
			t.Errorf("component 10 = %v", got.Values[10])
		}
	})

	t.Run("padded", func(t *testing.T) {
		got, err := NormaliseEmbedding([]float32{0.5, -0.25}, false)
		if err != nil {
			t.Fatal(err)
		}
		if len(got.Values) != StorageDimensions {
			t.Fatalf("components = %d", len(got.Values))
		}
		if got.SourceDimensions != 2 {
			t.Errorf("source = %d, want 2", got.SourceDimensions)
		}
		if got.Values[0] != 0.5 || got.Values[1] != -0.25 {
			t.Errorf("head = %v", got.Values[:2])
		}
		for i, value := range got.Values[2:] {
			if value != 0 {
				t.Fatalf("component %d = %v, want zero padding", i+2, value)
			}
		}
	})

	t.Run("truncated when allowed", func(t *testing.T) {
		vector := make([]float32, 1536)
		for i := range vector {
			vector[i] = 1
		}
		got, err := NormaliseEmbedding(vector, true)
		if err != nil {
			t.Fatal(err)
		}
		if len(got.Values) != StorageDimensions {
			t.Fatalf("components = %d", len(got.Values))
		}
		if got.SourceDimensions != StorageDimensions {
			t.Errorf("source = %d, want %d", got.SourceDimensions, StorageDimensions)
		}
	})

	t.Run("rejected when not truncatable", func(t *testing.T) {
		vector := make([]float32, 1536)
		_, err := NormaliseEmbedding(vector, false)
		if !errors.Is(err, ErrDimensions) {
			t.Fatalf("error = %v, want ErrDimensions", err)
		}
	})

	t.Run("empty", func(t *testing.T) {
		_, err := NormaliseEmbedding(nil, true)
		if !errors.Is(err, ErrDimensions) {
			t.Fatalf("error = %v, want ErrDimensions", err)
		}
	})

	t.Run("non-finite", func(t *testing.T) {
		_, err := NormaliseEmbedding([]float32{float32(math.NaN())}, true)
		if !errors.Is(err, ErrDimensions) {
			t.Fatalf("error = %v, want ErrDimensions", err)
		}
		_, err = NormaliseEmbedding([]float32{float32(math.Inf(1))}, true)
		if !errors.Is(err, ErrDimensions) {
			t.Fatalf("error = %v, want ErrDimensions", err)
		}
	})
}
