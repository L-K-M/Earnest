package llm

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// USD is an amount in millionths of a US dollar. Money stays an integer so
// budgets are exact; the schema stores numeric(14,6) with the same scale.
type USD int64

// usdScale is the number of decimal places USD keeps.
const usdScale = 6

// ParseUSD parses a non-negative decimal amount into USD. Fractions beyond
// six places are rejected rather than rounded, so a configured price can
// never silently differ from the stored one.
func ParseUSD(text string) (USD, error) {
	s := strings.TrimSpace(text)
	if s == "" {
		return 0, fmt.Errorf("empty amount")
	}
	if strings.HasPrefix(s, "-") {
		return 0, fmt.Errorf("amount must not be negative: %q", text)
	}
	s = strings.TrimPrefix(s, "+")
	whole, fraction, _ := strings.Cut(s, ".")
	if whole == "" && fraction == "" {
		return 0, fmt.Errorf("invalid amount %q", text)
	}
	if len(fraction) > usdScale {
		return 0, fmt.Errorf("amount %q has more than %d decimal places", text, usdScale)
	}
	for _, r := range whole + fraction {
		if r < '0' || r > '9' {
			return 0, fmt.Errorf("invalid amount %q", text)
		}
	}
	for len(fraction) < usdScale {
		fraction += "0"
	}
	micros, err := strconv.ParseInt(whole+fraction, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid amount %q", text)
	}
	return USD(micros), nil
}

// String renders the amount with six decimal places.
func (u USD) String() string {
	negative := u < 0
	if negative {
		u = -u
	}
	whole, fraction := u/1e6, u%1e6
	s := strconv.FormatInt(int64(whole), 10) + "." + fmt.Sprintf("%06d", fraction)
	if negative {
		return "-" + s
	}
	return s
}

// MulTokens returns the cost of a token count priced at u micro-dollars per
// one million tokens, rounded to the nearest micro-dollar.
func (u USD) MulTokens(tokens int) USD {
	if u <= 0 || tokens <= 0 {
		return 0
	}
	return USD((int64(u)*int64(tokens) + 500000) / 1e6)
}

// SpendStore reads and writes provider spend. *store.Store implements it.
// Amounts are micro-dollars, matching USD.
type SpendStore interface {
	// LLMEnsureMonth returns the provider's spend row for month, creating it
	// with limit when absent and re-asserting the configured limit.
	LLMEnsureMonth(ctx context.Context, provider string, month time.Time, limitUsdMicros int64) (spentUsdMicros int64, limitUsdMicrosOut int64, err error)
	// LLMAddSpend adds cost to the provider's month.
	LLMAddSpend(ctx context.Context, provider string, month time.Time, costUsdMicros int64) error
	// LLMReserveSpend atomically includes estimate in spend if it fits the cap.
	LLMReserveSpend(ctx context.Context, provider string, month time.Time, limitMicros, estimateMicros int64, expiresAt time.Time) (id string, spentMicros int64, reserved bool, err error)
	// LLMSettleSpend replaces a hold with actual cost, idempotently.
	LLMSettleSpend(ctx context.Context, provider, id string, actualMicros int64) error
}

// Spend is a provider's recorded spend and limit for one month. Spent includes
// conservative estimates for calls whose billed usage is not yet known.
type Spend struct {
	Provider string
	Month    time.Time
	Spent    USD
	Limit    USD
}

// Unlimited reports whether the provider has no cap. A zero limit means
// unlimited, so a provider can be configured without a budget.
func (s Spend) Unlimited() bool { return s.Limit <= 0 }

// Budget enforces per-provider monthly spend limits.
type Budget struct {
	store  SpendStore
	limits map[string]USD
	now    func() time.Time
}

// NewBudget returns a budget backed by store. limits maps a provider name to
// its monthly cap; a provider absent from the map, or capped at zero, is
// unlimited.
func NewBudget(store SpendStore, limits map[string]USD) *Budget {
	return &Budget{store: store, limits: limits, now: time.Now}
}

// Authorize is an advisory check, not permission to call a provider; use Reserve.
// It reports ErrBudgetExhausted when the provider's recorded spend has
// reached its limit. A provider with no configured limit is always
// authorized. The month's row is created on first use.
func (b *Budget) Authorize(ctx context.Context, provider string) error {
	limit, ok := b.limits[provider]
	if !ok || limit <= 0 {
		return nil
	}
	spend, err := b.Spend(ctx, provider)
	if err != nil {
		return err
	}
	if spend.Spent >= limit {
		return &BudgetError{Provider: provider, Month: spend.Month, Spent: spend.Spent, Limit: limit}
	}
	return nil
}

// reservationLifetime accommodates the provider's 24-hour batch window and
// polling delays. Crashed calls expire at their estimate, never as free spend.
const reservationLifetime = 48 * time.Hour

// Reserve holds a conservative cost estimate before a provider call. It returns
// ErrBudgetExhausted without a hold when the estimate cannot fit this month.
func (b *Budget) Reserve(ctx context.Context, provider string, estimate USD) (string, error) {
	month := b.month()
	limit := b.limits[provider]
	id, spent, ok, err := b.store.LLMReserveSpend(ctx, provider, month, int64(limit), int64(estimate), time.Now().Add(reservationLifetime))
	if err != nil {
		return "", fmt.Errorf("reserve spend: %w", err)
	}
	if !ok {
		return "", &BudgetError{Provider: provider, Month: month, Spent: USD(spent), Limit: limit}
	}
	return id, nil
}

// Reconcile replaces a reservation with billed cost, including rejected output.
// A zero cost releases an unbilled call. The receipt retains its original month.
func (b *Budget) Reconcile(ctx context.Context, provider, id string, actual USD) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := b.store.LLMSettleSpend(ctx, provider, id, int64(actual)); err != nil {
		return fmt.Errorf("reconcile spend: %w", err)
	}
	return nil
}

// Record adds cost to the provider's month. Zero and negative costs are
// ignored.
func (b *Budget) Record(ctx context.Context, provider string, cost USD) error {
	if cost <= 0 {
		return nil
	}
	if err := b.store.LLMAddSpend(ctx, provider, b.month(), int64(cost)); err != nil {
		return fmt.Errorf("record spend: %w", err)
	}
	return nil
}

// Spend returns the provider's recorded spend and configured limit for the
// current month, creating the row when absent.
func (b *Budget) Spend(ctx context.Context, provider string) (Spend, error) {
	month := b.month()
	limit := b.limits[provider]
	spentMicros, _, err := b.store.LLMEnsureMonth(ctx, provider, month, int64(limit))
	if err != nil {
		return Spend{}, fmt.Errorf("read spend: %w", err)
	}
	return Spend{Provider: provider, Month: month, Spent: USD(spentMicros), Limit: limit}, nil
}

// month truncates now to the first day of the month in UTC, matching the
// schema's month column.
func (b *Budget) month() time.Time {
	now := b.now().UTC()
	return time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
}
