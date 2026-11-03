package store

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"
)

// EmbeddingPut stores one normalised vector. dims is the model's own width and
// must be between 1 and len(vector); a shorter vector must be zero-padded to
// len(vector), which the schema checks.
func (s *Store) EmbeddingPut(ctx context.Context, entryID int64, model string, dims int, vector []float32) error {
	if dims < 1 || dims > len(vector) {
		return fmt.Errorf("embedding dims %d out of range 1..%d", dims, len(vector))
	}
	literal, err := formatHalfvec(vector)
	if err != nil {
		return err
	}
	if _, err := s.pool.Exec(ctx, `INSERT INTO embeddings (entry_id, model, dims, embedding)
			VALUES ($1, $2, $3, $4::halfvec)
		ON CONFLICT (entry_id, model) DO UPDATE
		SET dims = EXCLUDED.dims, embedding = EXCLUDED.embedding`,
		entryID, model, dims, literal); err != nil {
		return fmt.Errorf("write embedding: %w", err)
	}
	return nil
}

// EmbeddingGet returns the stored vector and its model dimensions. found is
// false when the entry has no vector for model.
func (s *Store) EmbeddingGet(ctx context.Context, entryID int64, model string) (dims int, vector []float32, found bool, err error) {
	var literal string
	err = s.pool.QueryRow(ctx, `SELECT dims, embedding::text FROM embeddings WHERE entry_id = $1 AND model = $2`,
		entryID, model).Scan(&dims, &literal)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, nil, false, nil
	}
	if err != nil {
		return 0, nil, false, fmt.Errorf("read embedding: %w", err)
	}
	vector, err = parseHalfvec(literal)
	if err != nil {
		return 0, nil, false, err
	}
	return dims, vector, true, nil
}

// formatHalfvec renders a vector as a pgvector halfvec literal.
func formatHalfvec(vector []float32) (string, error) {
	var builder strings.Builder
	builder.WriteByte('[')
	for i, value := range vector {
		if i > 0 {
			builder.WriteByte(',')
		}
		if math.IsNaN(float64(value)) || math.IsInf(float64(value), 0) {
			return "", fmt.Errorf("embedding component %d is not finite", i)
		}
		builder.WriteString(strconv.FormatFloat(float64(value), 'g', -1, 32))
	}
	builder.WriteByte(']')
	return builder.String(), nil
}

// parseHalfvec reads a pgvector halfvec literal.
func parseHalfvec(literal string) ([]float32, error) {
	text := strings.TrimSpace(literal)
	if len(text) < 2 || text[0] != '[' || text[len(text)-1] != ']' {
		return nil, fmt.Errorf("invalid halfvec literal")
	}
	body := strings.TrimSpace(text[1 : len(text)-1])
	if body == "" {
		return nil, fmt.Errorf("empty halfvec literal")
	}
	parts := strings.Split(body, ",")
	vector := make([]float32, 0, len(parts))
	for _, part := range parts {
		value, err := strconv.ParseFloat(strings.TrimSpace(part), 32)
		if err != nil {
			return nil, fmt.Errorf("parse halfvec component: %w", err)
		}
		vector = append(vector, float32(value))
	}
	return vector, nil
}
