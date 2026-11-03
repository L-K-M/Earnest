package store_test

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/L-K-M/Earnest/internal/store"
	"github.com/L-K-M/Earnest/internal/store/storetest"
)

func TestLLMReservationsAcrossPools(t *testing.T) {
	template := storetest.NewTemplate(t)
	dsn := template.DatabaseURL(t)
	ctx := context.Background()
	var pools []*store.Store
	for range 2 {
		db, err := store.Open(ctx, dsn)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(db.Close)
		pools = append(pools, db)
	}
	month := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)
	start := make(chan struct{})
	ids := make(chan string, 32)
	var wg sync.WaitGroup
	var accepted atomic.Int32
	for i := range 32 {
		wg.Go(func() {
			<-start
			id, _, ok, err := pools[i%len(pools)].LLMReserveSpend(ctx, "paid", month, 100, 60, time.Now().Add(time.Hour))
			if err != nil {
				t.Error(err)
				return
			}
			if ok {
				accepted.Add(1)
				ids <- id
			}
		})
	}
	close(start)
	wg.Wait()
	close(ids)
	if accepted.Load() != 1 {
		t.Fatalf("accepted %d calls, want one", accepted.Load())
	}
	db := pools[0]
	assertLLMSpend(t, db, "paid", month, 60)
	for id := range ids {
		// Simulate concurrent workers replaying settlement after a lost commit reply.
		for i := range 16 {
			wg.Go(func() {
				if err := pools[i%len(pools)].LLMSettleSpend(ctx, "paid", id, 20); err != nil {
					t.Error(err)
				}
			})
		}
	}
	wg.Wait()
	assertLLMSpend(t, db, "paid", month, 20)
	if _, _, ok, err := db.LLMReserveSpend(ctx, "paid", month, 100, 80, time.Now().Add(time.Hour)); err != nil || !ok {
		t.Fatalf("refunded remainder unavailable: ok=%v err=%v", ok, err)
	}
	assertLLMSpend(t, db, "paid", month, 100)
}

func TestLLMReservationLifecycle(t *testing.T) {
	template := storetest.NewTemplate(t)
	month := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)
	for _, test := range []struct {
		name    string
		expired bool
		actual  int64
	}{
		{"billed", false, 30},
		{"unbilled", false, 0},
		{"expired then billed", true, 30},
		{"expired then unbilled", true, 0},
	} {
		t.Run(test.name, func(t *testing.T) {
			db := template.New(t)
			ctx := context.Background()
			expires := time.Now().Add(time.Hour)
			if test.expired {
				expires = time.Now().Add(-time.Hour)
			}
			id, _, ok, err := db.LLMReserveSpend(ctx, "paid", month, 100, 60, expires)
			if err != nil || !ok || id == "" {
				t.Fatalf("reserve: id=%q ok=%v err=%v", id, ok, err)
			}
			if refusedID, spent, ok, err := db.LLMReserveSpend(ctx, "paid", month, 100, 41, time.Now().Add(time.Hour)); err != nil || ok || refusedID != "" || spent != 60 {
				t.Fatalf("refusal: id=%q spent=%d ok=%v err=%v", refusedID, spent, ok, err)
			}
			var expired bool
			if err := db.Pool().QueryRow(ctx, `SELECT expired_at IS NOT NULL FROM llm_spend_reservations WHERE id=$1`, id).Scan(&expired); err != nil || expired != test.expired {
				t.Fatalf("expiry=%v err=%v", expired, err)
			}
			// Expiry cannot restore funds that a crashed process may have spent.
			assertLLMSpend(t, db, "paid", month, 60)
			december := month.AddDate(0, 1, 0)
			if _, _, err := db.LLMEnsureMonth(ctx, "paid", december, 100); err != nil {
				t.Fatal(err)
			}
			if err := db.LLMSettleSpend(ctx, "other", id, test.actual); err == nil {
				t.Fatal("another provider settled the reservation")
			}
			if err := db.LLMSettleSpend(ctx, "paid", id, test.actual); err != nil {
				t.Fatal(err)
			}
			if err := db.LLMSettleSpend(ctx, "paid", id, test.actual); err != nil {
				t.Fatal(err)
			}
			assertLLMSpend(t, db, "paid", month, test.actual)
			assertLLMSpend(t, db, "paid", december, 0)
		})
	}
}

func TestLLMReservationLimits(t *testing.T) {
	template := storetest.NewTemplate(t)
	for _, test := range []struct {
		name                   string
		spent, limit, estimate int64
		want                   bool
	}{
		{"exact fit", 40, 100, 60, true},
		{"one micro over", 40, 100, 61, false},
		{"exhausted zero cost", 100, 100, 0, false},
		{"already over", 110, 100, 1, false},
		{"unlimited", 100, 0, 1000, true},
		{"zero cost", 0, 100, 0, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			db := template.New(t)
			ctx := context.Background()
			month := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)
			if err := db.LLMAddSpend(ctx, "paid", month, test.spent); err != nil {
				t.Fatal(err)
			}
			_, _, ok, err := db.LLMReserveSpend(ctx, "paid", month, test.limit, test.estimate, time.Now().Add(time.Hour))
			if err != nil || ok != test.want {
				t.Fatalf("reserved=%v want=%v err=%v", ok, test.want, err)
			}
			want := test.spent
			if test.want {
				want += test.estimate
			}
			assertLLMSpend(t, db, "paid", month, want)
			// Providers have independent caps.
			if _, _, ok, err := db.LLMReserveSpend(ctx, "other", month, 100, 100, time.Now().Add(time.Hour)); err != nil || !ok {
				t.Fatalf("other provider: reserved=%v err=%v", ok, err)
			}
		})
	}
}

func assertLLMSpend(t *testing.T, db *store.Store, provider string, month time.Time, want int64) {
	t.Helper()
	var got int64
	if err := db.Pool().QueryRow(context.Background(), `SELECT (spent_usd*1000000)::bigint FROM llm_spend WHERE provider=$1 AND month=$2::date`, provider, month).Scan(&got); err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("%s %s spend=%d want=%d", provider, month.Format("2006-01"), got, want)
	}
}
