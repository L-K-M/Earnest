package service

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/L-K-M/Earnest/internal/pipeline"
	"github.com/L-K-M/Earnest/internal/store"
	"github.com/jackc/pgx/v5"
)

const classifierMaxAttempts = 3
const classifierRetryDelay = time.Minute

func classifierRetryKey(entry int64) string { return fmt.Sprintf("classify/%d", entry) }

// Failure records share the durable jobs table. Exhausted inputs stay deferred
// until their content, title, model or prompt changes; other entries keep moving.
func (s *Service) deferClassifier(ctx context.Context, entry store.EmbedEntry, model string) error {
	payload, err := json.Marshal(map[string]string{
		"model": model, "prompt_version": pipeline.ClassifierPromptVersion,
		"content_hash": fmt.Sprintf("%x", entry.ContentHash), "input_title": entry.Title,
	})
	if err != nil {
		return fmt.Errorf("encode classifier retry: %w", err)
	}
	var attempts int
	err = s.store.WithTx(ctx, func(tx pgx.Tx) error {
		if err := tx.QueryRow(ctx, `INSERT INTO jobs(kind,unique_key,payload,attempts,last_error)
 VALUES('classify',$1,$2,1,'Classifier call or output failed')
 ON CONFLICT(unique_key) DO UPDATE SET payload=EXCLUDED.payload,
 attempts=CASE WHEN jobs.payload=EXCLUDED.payload THEN jobs.attempts+1 ELSE 1 END,
 last_error=EXCLUDED.last_error RETURNING attempts`, classifierRetryKey(entry.ID), payload).Scan(&attempts); err != nil {
			return fmt.Errorf("record classifier failure: %w", err)
		}
		if attempts >= classifierMaxAttempts {
			_, err := tx.Exec(ctx, `UPDATE jobs SET run_at='infinity' WHERE unique_key=$1`, classifierRetryKey(entry.ID))
			return err
		}
		delay := classifierRetryDelay * time.Duration(1<<(attempts-1))
		_, err := tx.Exec(ctx, `UPDATE jobs SET run_at=now()+$2*interval '1 second' WHERE unique_key=$1`, classifierRetryKey(entry.ID), delay.Seconds())
		return err
	})
	if err != nil {
		return fmt.Errorf("defer classifier: %w", err)
	}
	slog.WarnContext(ctx, "classifier input deferred", "entry_id", entry.ID, "attempts", attempts)
	return nil
}
