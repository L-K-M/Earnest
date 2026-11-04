package service

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/L-K-M/Earnest/internal/store/storetest"
)

// The native clients execute these same files against their cached river rule.
func TestSharedRiverFixtures(t *testing.T) {
	template := storetest.NewTemplate(t)
	files, err := filepath.Glob("../../testdata/river/*.json")
	if err != nil || len(files) == 0 {
		t.Fatalf("river fixtures: %v (%d files)", err, len(files))
	}
	for _, file := range files {
		t.Run(filepath.Base(file), func(t *testing.T) {
			var fixture struct {
				Now           time.Time
				DecayDays     int `json:"today_decay_days"`
				Subscriptions []struct {
					ID             int64
					Cadence        string
					Muted, Deleted bool
					Since          time.Time `json:"initial_since"`
					Through        int64     `json:"initial_through_seq"`
				}
				Entries []struct {
					ID, Seq                    int64
					Subscription               int64      `json:"subscription_id"`
					FirstSeen                  time.Time  `json:"first_seen_at"`
					Published                  *time.Time `json:"published_at"`
					Author, Title, Seen, Shelf string
					Deleted                    bool
				}
				Mutes []struct {
					ID          int64
					Kind, Value string
					Rev         int64
					Deleted     bool
				}
				Expected []int64 `json:"expected_ids"`
			}
			data, err := os.ReadFile(file)
			if err != nil {
				t.Fatal(err)
			}
			if err := json.Unmarshal(data, &fixture); err != nil {
				t.Fatal(err)
			}
			f := entriesTestNew(t, template)
			f.setClock(fixture.Now)
			ctx := context.Background()
			exec := func(sql string, args ...any) {
				t.Helper()
				if _, err := f.db.Pool().Exec(ctx, sql, args...); err != nil {
					t.Fatal(err)
				}
			}
			if fixture.DecayDays != 0 {
				exec(`UPDATE users SET settings=jsonb_build_object('today_decay_days',$2::int) WHERE id=$1`, f.a.UserID, fixture.DecayDays)
			}
			for _, sub := range fixture.Subscriptions {
				feed := entriesTestFeed(t, f.db, fmt.Sprintf("https://river.test/%d", sub.ID), 0)
				exec(`INSERT INTO subscriptions(id,user_id,feed_id,cadence,muted,initial_since,initial_through_seq,deleted_at) VALUES($1,$2,$3,$4,$5,$6,$7,CASE WHEN $8 THEN now() END)`, sub.ID, f.a.UserID, feed, sub.Cadence, sub.Muted, sub.Since, sub.Through, sub.Deleted)
				for _, e := range fixture.Entries {
					if e.Subscription != sub.ID {
						continue
					}
					exec(`INSERT INTO entries(id,feed_id,seq,identity_hash,first_seen_at,published_at,author,title,deleted_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,CASE WHEN $9 THEN now() END)`, e.ID, feed, e.Seq, entriesTestHashB(fmt.Sprint(e.ID)), e.FirstSeen, e.Published, e.Author, e.Title, e.Deleted)
					exec(`INSERT INTO entry_state(principal_id,entry_id,seen,seen_rev,seen_occurred_at,seen_actor,seen_source,shelf,shelf_rev,shelf_occurred_at,shelf_actor,shelf_source,state_rev) VALUES($1,$2,$3,1,$5,$1,'deliberate',$4,1,$5,$1,'deliberate',1)`, f.a.PrincipalID, e.ID, e.Seen, e.Shelf, fixture.Now)
				}
			}
			for _, m := range fixture.Mutes {
				exec(`INSERT INTO mutes(id,user_id,kind,value,rev,deleted_at) VALUES($1,$2,$3,$4,$5,CASE WHEN $6 THEN now() END)`, m.ID, f.a.UserID, m.Kind, m.Value, m.Rev, m.Deleted)
			}
			entries, err := f.s.Entries(ctx, f.a, EntryFilter{View: ViewRiver, Limit: 200})
			if err != nil {
				t.Fatal(err)
			}
			ids := make([]int64, 0, len(entries))
			for _, entry := range entries {
				ids = append(ids, entry.ID)
			}
			if !reflect.DeepEqual(ids, fixture.Expected) {
				t.Fatalf("river IDs = %v, want %v", ids, fixture.Expected)
			}
		})
	}
}
