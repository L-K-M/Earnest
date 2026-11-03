package llm

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"
)

// fakeSpendStore records budget operations in memory.
type fakeSpendStore struct {
	mu           sync.Mutex
	reservations map[string]int64
	next         int
	spent        int64
	limit        int64
	ensure       int
	adds         []int64
	provider     string
	months       []time.Time
}

func (f *fakeSpendStore) LLMEnsureMonth(ctx context.Context, provider string, month time.Time, limitUsdMicros int64) (int64, int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.ensure++
	f.provider = provider
	f.limit = limitUsdMicros
	f.months = append(f.months, month)
	return f.spent, limitUsdMicros, nil
}

func (f *fakeSpendStore) LLMAddSpend(ctx context.Context, provider string, month time.Time, costUsdMicros int64) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.provider = provider
	f.adds = append(f.adds, costUsdMicros)
	f.spent += costUsdMicros
	return nil
}

func (f *fakeSpendStore) LLMReserveSpend(_ context.Context, provider string, _ time.Time, limit, estimate int64, _ time.Time) (string, int64, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if limit > 0 && (f.spent >= limit || estimate > limit-f.spent) {
		return "", f.spent, false, nil
	}
	if f.reservations == nil {
		f.reservations = make(map[string]int64)
	}
	f.next++
	id := fmt.Sprint(f.next)
	f.reservations[id] = estimate
	f.spent += estimate
	f.provider = provider
	return id, f.spent - estimate, true, nil
}

func (f *fakeSpendStore) LLMSettleSpend(_ context.Context, provider, id string, actual int64) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	estimate, ok := f.reservations[id]
	if !ok {
		return nil
	}
	delete(f.reservations, id)
	f.spent += actual - estimate
	f.provider = provider
	if actual > 0 {
		f.adds = append(f.adds, actual)
	}
	return nil
}

func TestBudgetAuthorize(t *testing.T) {
	tests := []struct {
		name       string
		spent      int64
		limit      USD
		configured bool
		wantErr    bool
	}{
		{name: "under limit", spent: 50, limit: 100, configured: true},
		{name: "at limit", spent: 100, limit: 100, configured: true, wantErr: true},
		{name: "over limit", spent: 150, limit: 100, configured: true, wantErr: true},
		{name: "unlimited when absent", spent: 100, configured: false},
		{name: "unlimited when zero", spent: 100, limit: 0, configured: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			limits := map[string]USD{}
			if test.configured {
				limits["openai"] = test.limit
			}
			budget := NewBudget(&fakeSpendStore{spent: test.spent}, limits)
			err := budget.Authorize(context.Background(), "openai")
			if test.wantErr {
				var budgetErr *BudgetError
				if !errors.As(err, &budgetErr) {
					t.Fatalf("error = %v, want *BudgetError", err)
				}
				if !errors.Is(err, ErrBudgetExhausted) {
					t.Error("error does not match ErrBudgetExhausted")
				}
				if budgetErr.Spent != USD(test.spent) || budgetErr.Limit != test.limit {
					t.Errorf("budget error = %+v", budgetErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("error = %v", err)
			}
		})
	}
}

func TestBudgetRecord(t *testing.T) {
	store := &fakeSpendStore{}
	budget := NewBudget(store, nil)
	if err := budget.Record(context.Background(), "openai", 2500); err != nil {
		t.Fatal(err)
	}
	if err := budget.Record(context.Background(), "openai", 0); err != nil {
		t.Fatal(err)
	}
	if len(store.adds) != 1 || store.adds[0] != 2500 {
		t.Errorf("adds = %v", store.adds)
	}
	if store.provider != "openai" {
		t.Errorf("provider = %q", store.provider)
	}
}

func TestBudgetMonthTruncation(t *testing.T) {
	store := &fakeSpendStore{}
	budget := NewBudget(store, nil)
	budget.now = func() time.Time { return time.Date(2026, 11, 15, 10, 30, 0, 0, time.FixedZone("x", 3600)) }
	if _, err := budget.Spend(context.Background(), "openai"); err != nil {
		t.Fatal(err)
	}
	if got := store.provider; got != "openai" {
		t.Errorf("provider = %q", got)
	}
	if len(store.months) != 1 {
		t.Fatalf("months = %v", store.months)
	}
	want := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)
	if !store.months[0].Equal(want) {
		t.Errorf("month = %v, want %v", store.months[0], want)
	}
}

func TestBudgetSpend(t *testing.T) {
	budget := NewBudget(&fakeSpendStore{spent: 42}, map[string]USD{"openai": 100})
	spend, err := budget.Spend(context.Background(), "openai")
	if err != nil {
		t.Fatal(err)
	}
	if spend.Spent != 42 || spend.Limit != 100 {
		t.Errorf("spend = %+v", spend)
	}
	if spend.Unlimited() {
		t.Error("spend should be limited")
	}
	if got := spend.Month; got.Day() != 1 || got.Location() != time.UTC {
		t.Errorf("month = %v", got)
	}
}
