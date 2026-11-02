package store_test

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/L-K-M/Earnest/internal/store"
	"github.com/L-K-M/Earnest/internal/store/storetest"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

func TestStore(t *testing.T) {
	template := storetest.NewTemplate(t)
	t.Run("migrations_empty_idempotent_concurrent", func(t *testing.T) {
		dsn := template.EmptyDatabaseURL(t)
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		var wg sync.WaitGroup
		for range 4 {
			wg.Add(1)
			go func() {
				defer wg.Done()
				db, err := store.Open(ctx, dsn)
				if err != nil {
					t.Error(err)
					return
				}
				defer db.Close()
				for range 2 {
					if err := db.Migrate(ctx); err != nil {
						t.Error(err)
						return
					}
				}
			}()
		}
		wg.Wait()
		db, err := store.Open(ctx, dsn)
		if err != nil {
			t.Fatal(err)
		}
		defer db.Close()
		var count int
		if err := db.Pool().QueryRow(ctx, "SELECT count(*) FROM schema_migrations").Scan(&count); err != nil || count != 1 {
			t.Fatalf("ledger count=%d error=%v", count, err)
		}
		var tables int
		if err := db.Pool().QueryRow(ctx, "SELECT count(*) FROM information_schema.tables WHERE table_schema='public' AND table_type='BASE TABLE'").Scan(&tables); err != nil || tables < 35 {
			t.Fatalf("table count=%d error=%v", tables, err)
		}
	})
	t.Run("counters", func(t *testing.T) { testCounters(t, template.New(t)) })
	t.Run("generation", func(t *testing.T) { testGeneration(t, template.New(t)) })
	t.Run("constraints_and_search", func(t *testing.T) { testSchema(t, template.New(t)) })
	t.Run("isolated_clones", func(t *testing.T) {
		a, b := template.New(t), template.New(t)
		createUser(t, a, "only-a")
		var n int
		if err := b.Pool().QueryRow(context.Background(), "SELECT count(*) FROM users").Scan(&n); err != nil || n != 0 {
			t.Fatalf("clone not isolated: count=%d error=%v", n, err)
		}
	})
}

func createUser(t *testing.T, db *store.Store, name string) int64 {
	t.Helper()
	var id int64
	if err := db.Pool().QueryRow(context.Background(), "INSERT INTO users (username,password_hash) VALUES ($1,'$argon2id$test') RETURNING id", name).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}

func testCounters(t *testing.T, db *store.Store) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	ids := []int64{createUser(t, db, "a"), createUser(t, db, "b"), createUser(t, db, "c")}
	var wg sync.WaitGroup
	var mu sync.Mutex
	values := map[int64][]int64{}
	var seqs []int64
	for worker := range 20 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for round := range 10 {
				// Overlapping subsets exercise both sorted locks and first counter creation.
				counts := map[int64]int64{ids[worker%3]: 2, ids[(worker+round+1)%3]: 2}
				var allocated store.Allocation
				err := db.WithTx(ctx, func(tx pgx.Tx) error {
					var err error
					allocated, err = store.AllocateCounters(ctx, tx, 2, counts)
					if err != nil {
						return err
					}
					// Commit change rows with their revisions, not merely counters in isolation.
					for id, r := range allocated.Revs {
						for rev := r.First; rev <= r.Last; rev++ {
							if _, err := tx.Exec(ctx, "INSERT INTO changes (user_id,rev,type,payload) VALUES ($1,$2,'test','{}')", id, rev); err != nil {
								return err
							}
						}
					}
					return nil
				})
				if err != nil {
					t.Error(err)
					return
				}
				mu.Lock()
				seqs = append(seqs, allocated.Seq.First, allocated.Seq.Last)
				for id, r := range allocated.Revs {
					values[id] = append(values[id], r.First, r.Last)
				}
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	checkGapless := func(name string, values []int64) {
		t.Helper()
		sort.Slice(values, func(i, j int) bool { return values[i] < values[j] })
		for i, v := range values {
			if v != int64(i+1) {
				t.Fatalf("%s has gap/duplicate at %d: %d", name, i, v)
			}
		}
	}
	if len(seqs) != 400 {
		t.Fatalf("got %d seq values", len(seqs))
	}
	checkGapless("seq", seqs)
	for _, id := range ids {
		checkGapless(fmt.Sprint(id), values[id])
	}
	sentinel := errors.New("rollback mutation")
	if err := db.WithTx(ctx, func(tx pgx.Tx) error {
		_, err := store.AllocateCounters(ctx, tx, 10, map[int64]int64{ids[0]: 10})
		if err != nil {
			return err
		}
		return sentinel
	}); !errors.Is(err, sentinel) {
		t.Fatalf("rollback error: %v", err)
	}
	if err := db.WithTx(ctx, func(tx pgx.Tx) error {
		next, err := store.AllocateCounters(ctx, tx, 1, map[int64]int64{ids[0]: 1})
		if err != nil {
			return err
		}
		if next.Seq.First != 401 || next.Revs[ids[0]].First != int64(len(values[ids[0]])+1) {
			t.Fatalf("rollback consumed counters: %+v", next)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		seq   int64
		users map[int64]int64
	}{{-1, nil}, {0, map[int64]int64{0: 1}}, {0, map[int64]int64{ids[0]: 0}}} {
		if err := db.WithTx(ctx, func(tx pgx.Tx) error { _, err := store.AllocateCounters(ctx, tx, test.seq, test.users); return err }); err == nil {
			t.Fatal("invalid allocation accepted")
		}
	}
	// Panics must also release transaction locks and roll back allocations.
	func() {
		defer func() {
			if recover() == nil {
				t.Error("expected panic")
			}
		}()
		_ = db.WithTx(ctx, func(tx pgx.Tx) error { _, _ = store.AllocateCounters(ctx, tx, 1, nil); panic("abort") })
	}()
	var seq int64
	if err := db.Pool().QueryRow(ctx, "SELECT value FROM seq_counter").Scan(&seq); err != nil || seq != 401 {
		t.Fatalf("panic consumed counter: %d %v", seq, err)
	}
}

func testGeneration(t *testing.T, db *store.Store) {
	ctx := context.Background()
	user := createUser(t, db, "generation")
	var principal int64
	if err := db.Pool().QueryRow(ctx, "INSERT INTO principals (user_id,kind,name) VALUES ($1,'agent','test') RETURNING id", user).Scan(&principal); err != nil {
		t.Fatal(err)
	}
	before, err := db.Generation(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, status := range []string{"held", "queued", "attempting", "delivered", "cancelled", "failed", "withheld", "uncertain"} {
		_, err := db.Pool().Exec(ctx, `INSERT INTO notifications(sender_principal_id,recipient_user_id,op_id,note,entry_ids,category,status,held_until,generation,attempted_at,delivered_at)
   VALUES($1,$2,$3,'test',ARRAY[1]::bigint[],'alert',$3,now(),$4,
   CASE WHEN $3 IN ('attempting','delivered','uncertain') THEN now() END,CASE WHEN $3='delivered' THEN now() END)`, principal, user, status, before)
		if err != nil {
			t.Fatal(err)
		}
	}
	after, err := db.RotateGeneration(ctx)
	if err != nil || after == before || after == "" {
		t.Fatalf("rotate: %q -> %q: %v", before, after, err)
	}
	current, err := db.Generation(ctx)
	if err != nil || current != after {
		t.Fatalf("generation not persisted: %q %v", current, err)
	}
	rows, err := db.Pool().Query(ctx, "SELECT op_id,status,generation::text FROM notifications ORDER BY id")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
		var op, status, generation string
		if err := rows.Scan(&op, &status, &generation); err != nil {
			t.Fatal(err)
		}
		want := op
		if op == "held" || op == "queued" {
			want = "withheld"
		}
		if op == "attempting" {
			want = "uncertain"
		}
		if status != want || generation != before {
			t.Fatalf("%s: got %s %s", op, status, generation)
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
}

func testSchema(t *testing.T, db *store.Store) {
	ctx := context.Background()
	a, b := createUser(t, db, "schema-a"), createUser(t, db, "schema-b")
	var principal, agent, feed, private, entry, folder, label int64
	queries := []struct {
		sql  string
		args []any
		dest *int64
	}{
		{"INSERT INTO principals(user_id,kind,name) VALUES($1,'human','me') RETURNING id", []any{a}, &principal},
		{"INSERT INTO principals(user_id,kind,name) VALUES($1,'agent','agent') RETURNING id", []any{a}, &agent},
		{"INSERT INTO feeds(url) VALUES('https://example.com/rss') RETURNING id", nil, &feed},
		{"INSERT INTO feeds(url,owner_user_id,credentials_encrypted) VALUES('https://example.com/rss',$1,'encrypted') RETURNING id", []any{a}, &private},
		{"INSERT INTO entries(feed_id,seq,identity_hash,title,language,content_hash) VALUES($1,1,decode(repeat('ab',32),'hex'),'Running title','en-US',decode(repeat('cd',32),'hex')) RETURNING id", []any{&feed}, &entry},
		{"INSERT INTO folders(user_id,name) VALUES($1,'mine') RETURNING id", []any{a}, &folder},
		{"INSERT INTO labels(user_id,name,rev) VALUES($1,'label',1) RETURNING id", []any{a}, &label},
	}
	for _, q := range queries {
		if err := db.Pool().QueryRow(ctx, q.sql, q.args...).Scan(q.dest); err != nil {
			t.Fatal(err)
		}
	}
	tests := []struct {
		name, sql string
		args      []any
	}{
		{"public_duplicate", "INSERT INTO feeds(url) VALUES('https://example.com/rss')", nil},
		{"private_subscriber", "INSERT INTO subscriptions(user_id,feed_id) VALUES($1,$2)", []any{b, private}},
		{"owner_immutable", "UPDATE feeds SET owner_user_id=$1 WHERE id=$2", []any{b, private}},
		{"public_credentials", "UPDATE feeds SET credentials_encrypted='secret' WHERE id=$1", []any{&feed}},
		{"foreign_folder", "INSERT INTO subscriptions(user_id,feed_id,folder_id) VALUES($1,$2,$3)", []any{b, feed, folder}},
		{"foreign_label", "INSERT INTO entry_labels(user_id,entry_id,label_id,rev) VALUES($1,$2,$3,1)", []any{b, entry, label}},
		{"bad_state", "INSERT INTO entry_state(principal_id,entry_id,shelf) VALUES($1,$2,'invalid')", []any{principal, entry}},
		{"incomplete_register", "INSERT INTO entry_state(principal_id,entry_id,seen_rev,state_rev) VALUES($1,$2,1,1)", []any{principal, entry}},
		{"agent_webhook", "INSERT INTO webhooks(user_id,created_by,url,secret_encrypted,events) VALUES($1,$2,'https://example.com','secret',ARRAY['entry.matched'])", []any{a, agent}},
		{"token_hash_length", "INSERT INTO tokens(principal_id,name,token_hash,kind,audience) VALUES($1,'bad','short','agent','mcp')", []any{agent}},
		{"duplicate_identity", "INSERT INTO entries(feed_id,seq,identity_hash) VALUES($1,2,decode(repeat('ab',32),'hex'))", []any{&feed}},
		{"invalid_embedding_dims", "INSERT INTO embeddings(entry_id,model,dims,embedding) VALUES($1,'model',1025,array_fill(1::real,ARRAY[1024])::halfvec)", []any{entry}},
		{"unpadded_embedding", "INSERT INTO embeddings(entry_id,model,dims,embedding) VALUES($1,'model',2,array_fill(1::real,ARRAY[1024])::halfvec)", []any{entry}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := db.Pool().Exec(ctx, test.sql, test.args...)
			var pgerr *pgconn.PgError
			if !errors.As(err, &pgerr) || !(pgerr.Code == "23514" || pgerr.Code == "23505" || pgerr.Code == "23503") {
				t.Fatalf("expected constraint violation: %v", err)
			}
		})
	}
	if _, err := db.Pool().Exec(ctx, "INSERT INTO contents(entry_id,content_hash,sanitizer_version,source,html,text) VALUES($1,decode(repeat('cd',32),'hex'),1,'feed','<p>Body walking</p>','Body walking')", entry); err != nil {
		t.Fatal(err)
	}
	var vector string
	if err := db.Pool().QueryRow(ctx, "SELECT search_vector::text FROM entries WHERE id=$1", entry).Scan(&vector); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(vector, "'run':1A") || !strings.Contains(vector, "'walk':4B") {
		t.Fatalf("weighted stemmed search: %s", vector)
	}
	if _, err := db.Pool().Exec(ctx, "UPDATE entries SET language='unknown' WHERE id=$1", entry); err != nil {
		t.Fatal(err)
	}
	if err := db.Pool().QueryRow(ctx, "SELECT search_vector::text FROM entries WHERE id=$1", entry).Scan(&vector); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(vector, "'running':1A") {
		t.Fatalf("fallback search: %s", vector)
	}

	if _, err := db.Pool().Exec(ctx, "INSERT INTO embeddings(entry_id,model,dims,embedding) VALUES($1,'model',2,(ARRAY[1::real,2::real] || array_fill(0::real,ARRAY[1022]))::halfvec)", entry); err != nil {
		t.Fatalf("zero-padded embedding: %v", err)
	}
	// Locked queue rows must not block another worker's independent claim.
	if _, err := db.Pool().Exec(ctx, "INSERT INTO jobs(kind,payload) VALUES('first','{}'),('second','{}')"); err != nil {
		t.Fatal(err)
	}
	first, err := db.Pool().Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer first.Rollback(ctx)
	second, err := db.Pool().Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer second.Rollback(ctx)
	claim := `SELECT id FROM jobs WHERE run_at<=now() AND (locked_until IS NULL OR locked_until<=now()) ORDER BY run_at,id FOR UPDATE SKIP LOCKED LIMIT 1`
	var firstID, secondID int64
	if err := first.QueryRow(ctx, claim).Scan(&firstID); err != nil {
		t.Fatal(err)
	}
	if err := second.QueryRow(ctx, claim).Scan(&secondID); err != nil {
		t.Fatal(err)
	}
	if firstID == secondID {
		t.Fatal("workers claimed the same job")
	}
	// Missing state is unread; a default state row remains unseen.
	if _, err := db.Pool().Exec(ctx, "INSERT INTO entry_state(principal_id,entry_id) VALUES($1,$2)", principal, entry); err != nil {
		t.Fatal(err)
	}
	var seen, shelf string
	var kept bool
	if err := db.Pool().QueryRow(ctx, "SELECT seen,shelf,kept FROM entry_state").Scan(&seen, &shelf, &kept); err != nil || seen != "unseen" || shelf != "none" || kept {
		t.Fatalf("default state: %s %s %v %v", seen, shelf, kept, err)
	}
	if _, err := db.Pool().Exec(ctx, "INSERT INTO activity(user_id,principal_id,action,target) VALUES($1,$2,'test','{}')", a, principal); err != nil {
		t.Fatal(err)
	}
	for _, query := range []string{"UPDATE activity SET action='overwrite'", "DELETE FROM activity"} {
		if _, err := db.Pool().Exec(ctx, query); err == nil {
			t.Fatal("activity changed")
		}
	}
	// Tombstones retain identity while dropping search text.
	if _, err := db.Pool().Exec(ctx, "UPDATE entries SET deleted_at=now() WHERE id=$1", entry); err != nil {
		t.Fatal(err)
	}
	if err := db.Pool().QueryRow(ctx, "SELECT search_vector::text FROM entries WHERE id=$1", entry).Scan(&vector); err != nil || vector != "" {
		t.Fatalf("tombstone search: %s %v", vector, err)
	}
}

func TestOpenRedactsErrors(t *testing.T) {
	_, err := store.Open(context.Background(), "postgres://user:super-secret@invalid:bad/database")
	if err == nil || strings.Contains(err.Error(), "super-secret") {
		t.Fatalf("unsafe error: %v", err)
	}
	if reflect.TypeOf(errors.Unwrap(err)) == nil {
		t.Fatal("error lost cause")
	}
}
