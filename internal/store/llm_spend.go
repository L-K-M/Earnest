package store

import (
	"context"
	"fmt"
	"time"
)

// Amounts in this file are micro-dollars, matching llm.USD. The schema stores
// numeric(14,6); the casts keep the conversion exact, with no floating point.

// LLMEnsureMonth returns the provider's spend row for month, creating it with
// limit when absent and re-asserting the configured limit so configuration
// stays the source of truth. month must fall on the first of a month.
func (s *Store) LLMEnsureMonth(ctx context.Context, provider string, month time.Time, limitUsdMicros int64) (spentUsdMicros int64, limitUsdMicrosOut int64, err error) {
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
// absent. Callers check the budget first with LLMEnsureMonth.
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
