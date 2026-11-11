package store

import (
	"io/fs"
	"testing"
	"testing/fstest"
	"time"
)

func TestFeedCadenceUpgradeReconsidersOnlyHealthyWaits(t *testing.T) {
	db := migrationDatabase(t)
	ctx := t.Context()
	files := fstest.MapFS{}
	names, err := fs.Glob(migrations, "migrations/*.sql")
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range names {
		if name >= "migrations/0012_" {
			continue
		}
		body, err := migrations.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		files[name] = &fstest.MapFile{Data: body}
	}
	if err := db.migrate(ctx, files); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Microsecond)
	for id, failures := range []int{0, 1} {
		if _, err := db.pool.Exec(ctx, `INSERT INTO feeds(id,url,last_attempt_at,next_fetch_at,consecutive_failures,lease_until)
 VALUES($1,$2,$3,$4,$5,$6)`, id+1, "https://feed.test/"+string(rune('a'+id)), now, now.Add(72*time.Hour), failures, now.Add(10*time.Minute)); err != nil {
			t.Fatal(err)
		}
	}
	if err := db.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	for id := 1; id <= 2; id++ {
		var next, lease time.Time
		var unknown bool
		if err := db.pool.QueryRow(ctx, `SELECT next_fetch_at,lease_until,posting_interval IS NULL AND posting_sample_at IS NULL FROM feeds WHERE id=$1`, id).Scan(&next, &lease, &unknown); err != nil {
			t.Fatal(err)
		}
		if !unknown || !lease.Equal(now.Add(10*time.Minute)) {
			t.Fatalf("upgrade invented cadence or changed lease: id=%d unknown=%v lease=%v", id, unknown, lease)
		}
		if id == 1 && (next.Before(now.Add(time.Hour)) || next.After(time.Now().Add(time.Hour))) {
			t.Fatalf("healthy wait was not reconsidered in one hour: %v", next)
		}
		if id == 2 && !next.Equal(now.Add(72*time.Hour)) {
			t.Fatalf("failed feed's retry was shortened: %v", next)
		}
	}
}
