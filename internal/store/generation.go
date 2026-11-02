package store

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
)

// Generation reads the database identity embedded in cursors and base revisions.
func (s *Store) Generation(ctx context.Context) (string, error) {
	var generation string
	if err := s.pool.QueryRow(ctx, "SELECT generation::text FROM system WHERE singleton").Scan(&generation); err != nil {
		return "", fmt.Errorf("read generation: %w", err)
	}
	return generation, nil
}

// RotateGeneration must run after restoring a backup, before serving requests.
// Pending sends are withheld and previously attempted sends become uncertain.
// Delivery workers must be stopped before rotation, just as during a restore.
func (s *Store) RotateGeneration(ctx context.Context) (string, error) {
	var generation string
	err := s.WithTx(ctx, func(tx pgx.Tx) error {
		if _, err := AllocateCounters(ctx, tx, 0, nil); err != nil {
			return err
		}
		if err := tx.QueryRow(ctx, "UPDATE system SET generation=gen_random_uuid() WHERE singleton RETURNING generation::text").Scan(&generation); err != nil {
			return fmt.Errorf("rotate generation: %w", err)
		}
		if _, err := tx.Exec(ctx, `UPDATE notifications SET status=CASE WHEN attempted_at IS NULL THEN 'withheld' ELSE 'uncertain' END
   WHERE status IN ('held','queued','attempting')`); err != nil {
			return fmt.Errorf("withhold restored notifications: %w", err)
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	return generation, nil
}
