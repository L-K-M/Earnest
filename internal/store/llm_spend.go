package store

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

// Amounts in this file are micro-dollars, matching llm.USD. The schema stores
// numeric(14,6); the casts keep the conversion exact, with no floating point.

// LLMEnsureMonth returns the provider's spend row for month, creating it with
// limit when absent and re-asserting the configured limit so configuration
// stays the source of truth. month must fall on the first of a month.
func (s *Store) LLMEnsureMonth(ctx context.Context, provider string, month time.Time, limitUsdMicros int64) (spentUsdMicros int64, limitUsdMicrosOut int64, err error) {
	if err := s.expireLLMReservations(ctx, provider); err != nil {
		return 0, 0, err
	}
	err = s.pool.QueryRow(ctx, `INSERT INTO llm_spend (provider, month, spent_usd, limit_usd)
			VALUES ($1, $2::date, 0, $3::numeric / 1000000)
		ON CONFLICT (provider, month) DO UPDATE SET limit_usd = EXCLUDED.limit_usd
		RETURNING (spent_usd * 1000000)::bigint, (limit_usd * 1000000)::bigint`,
		provider, month, limitUsdMicros).Scan(&spentUsdMicros, &limitUsdMicrosOut)
	if err != nil {
		return 0, 0, fmt.Errorf("ensure llm spend: %w", err)
	}
	return spentUsdMicros, limitUsdMicrosOut, nil
}

// LLMAddSpend adds cost to the provider's month, creating the row when it is
// absent. This records already incurred costs; provider calls must reserve first.
func (s *Store) LLMAddSpend(ctx context.Context, provider string, month time.Time, costUsdMicros int64) error {
	if _, err := s.pool.Exec(ctx, `INSERT INTO llm_spend (provider, month, spent_usd, limit_usd)
			VALUES ($1, $2::date, $3::numeric / 1000000, 0)
		ON CONFLICT (provider, month) DO UPDATE
		SET spent_usd = llm_spend.spent_usd + EXCLUDED.spent_usd`,
		provider, month, costUsdMicros); err != nil {
		return fmt.Errorf("record llm spend: %w", err)
	}
	return nil
}

// LLMReserveSpend atomically holds estimate against the configured monthly cap.
// Spent includes pending estimates. A refusal returns no ID and changes no spend.
func (s *Store) LLMReserveSpend(ctx context.Context, provider string, month time.Time, limitMicros, estimateMicros int64, expiresAt time.Time) (id string, spentMicros int64, reserved bool, err error) {
	if estimateMicros < 0 || limitMicros < 0 {
		return "", 0, false, fmt.Errorf("reserve llm spend: negative amount")
	}
	if err := s.expireLLMReservations(ctx, provider); err != nil {
		return "", 0, false, err
	}
	id = rand.Text()
	err = s.WithTx(ctx, func(tx pgx.Tx) error {
		// The upsert locks the month's row until commit, serializing all callers
		// across pools and processes without holding a lock during network I/O.
		if err := tx.QueryRow(ctx, `INSERT INTO llm_spend(provider,month,spent_usd,limit_usd)
			VALUES($1,$2::date,0,$3::numeric/1000000)
			ON CONFLICT(provider,month) DO UPDATE SET limit_usd=EXCLUDED.limit_usd
			RETURNING (spent_usd*1000000)::bigint`, provider, month, limitMicros).Scan(&spentMicros); err != nil {
			return fmt.Errorf("lock llm budget: %w", err)
		}
		if limitMicros > 0 && (spentMicros >= limitMicros || estimateMicros > limitMicros-spentMicros) {
			return nil
		}
		if _, err := tx.Exec(ctx, `UPDATE llm_spend SET spent_usd=spent_usd+$3::numeric/1000000
			WHERE provider=$1 AND month=$2::date`, provider, month, estimateMicros); err != nil {
			return fmt.Errorf("hold llm spend: %w", err)
		}
		if _, err := tx.Exec(ctx, `INSERT INTO llm_spend_reservations(id,provider,month,estimate_micros,expires_at)
			VALUES($1,$2,$3::date,$4,$5)`, id, provider, month, estimateMicros, expiresAt); err != nil {
			return fmt.Errorf("save llm reservation: %w", err)
		}
		reserved = true
		return nil
	})
	if err != nil || !reserved {
		return "", spentMicros, false, err
	}
	return id, spentMicros, true, nil
}

// LLMSettleSpend replaces a reservation with actual cost once, even after expiry
// or a month rollover. Zero releases an unbilled call. Retries are idempotent.
func (s *Store) LLMSettleSpend(ctx context.Context, provider, id string, actualMicros int64) error {
	if actualMicros < 0 {
		return fmt.Errorf("settle llm spend: negative amount")
	}
	return s.WithTx(ctx, func(tx pgx.Tx) error {
		var month time.Time
		if err := tx.QueryRow(ctx, `SELECT month FROM llm_spend_reservations WHERE id=$1 AND provider=$2`, id, provider).Scan(&month); err != nil {
			return fmt.Errorf("find llm reservation: %w", err)
		}
		// Keep the same lock order as reservation: month, then receipt.
		if _, err := tx.Exec(ctx, `SELECT 1 FROM llm_spend WHERE provider=$1 AND month=$2::date FOR UPDATE`, provider, month); err != nil {
			return fmt.Errorf("lock llm settlement: %w", err)
		}
		var estimate int64
		err := tx.QueryRow(ctx, `UPDATE llm_spend_reservations SET actual_micros=$3,settled_at=clock_timestamp()
			WHERE id=$1 AND provider=$2 AND settled_at IS NULL RETURNING estimate_micros`, id, provider, actualMicros).Scan(&estimate)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("settle llm reservation: %w", err)
		}
		if _, err := tx.Exec(ctx, `UPDATE llm_spend SET spent_usd=spent_usd+($3::numeric-$4::numeric)/1000000
			WHERE provider=$1 AND month=$2::date`, provider, month, actualMicros, estimate); err != nil {
			return fmt.Errorf("reconcile llm spend: %w", err)
		}
		return nil
	})
}

func (s *Store) expireLLMReservations(ctx context.Context, provider string) error {
	// Unknown billing cannot safely be refunded. Close stale holds at their
	// estimate, retaining the receipt so a late response can still reconcile it.
	_, err := s.pool.Exec(ctx, `UPDATE llm_spend_reservations SET expired_at=clock_timestamp()
		WHERE provider=$1 AND settled_at IS NULL AND expired_at IS NULL AND expires_at<=clock_timestamp()`, provider)
	if err != nil {
		return fmt.Errorf("expire llm reservations: %w", err)
	}
	return nil
}
