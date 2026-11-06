package store

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

// LockCore takes the global counter lock before discovering exact mutation counts.
// Hold it through commit; then AllocateCounters can allocate only used numbers.
// This also serialises subscription membership with fan-out ingest.
func LockCore(ctx context.Context, tx pgx.Tx) error {
	if _, err := tx.Exec(ctx, "SELECT value FROM seq_counter WHERE singleton FOR UPDATE"); err != nil {
		return fmt.Errorf("lock domain commit: %w", err)
	}
	return nil
}

// InsertChange persists a payload using an already allocated user revision.
func InsertChange(ctx context.Context, tx pgx.Tx, user, rev int64, kind string, payload any, at time.Time) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("encode change: %w", err)
	}
	if _, err := tx.Exec(ctx, "INSERT INTO changes(user_id,rev,type,payload,created_at) VALUES($1,$2,$3,$4,$5)", user, rev, kind, body, at); err != nil {
		return fmt.Errorf("insert change: %w", err)
	}
	return nil
}

// InsertChanges persists one payload per allocated user revision in a single
// statement, pairing each revision with the payload at the same index.
func InsertChanges(ctx context.Context, tx pgx.Tx, user int64, revs []int64, kind string, payloads []json.RawMessage, at time.Time) error {
	if len(revs) != len(payloads) {
		return fmt.Errorf("revisions and payloads must pair")
	}
	if len(revs) == 0 {
		return nil
	}
	type row struct {
		Rev     int64           `json:"rev"`
		Payload json.RawMessage `json:"payload"`
	}
	batch := make([]row, len(revs))
	for i := range revs {
		batch[i] = row{Rev: revs[i], Payload: payloads[i]}
	}
	body, err := json.Marshal(batch)
	if err != nil {
		return fmt.Errorf("encode changes: %w", err)
	}
	if _, err := tx.Exec(ctx, `INSERT INTO changes(user_id,rev,type,payload,created_at)
		SELECT $1, (r->>'rev')::bigint, $2, r->'payload', $3
		FROM jsonb_array_elements($4::jsonb) AS r`, user, kind, at, string(body)); err != nil {
		return fmt.Errorf("insert changes: %w", err)
	}
	return nil
}
