package service

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/L-K-M/Earnest/internal/llm"
	"github.com/L-K-M/Earnest/internal/store/storetest"
)

func TestWatchPausesThenNotifiesWithinBudget(t *testing.T) {
	f := newCoreFixture(t, storetest.NewTemplate(t))
	p := &pipelineProvider{classifier: `{"labels":["news"]}`}
	ctx := pipelineTestContext(t, f.db, p, []llm.Task{llm.TaskEmbed, llm.TaskClassify}, 0)
	watch, err := f.s.CreateWatch(ctx, f.a, WatchRule{Rule: Rule{Name: "Space watch", Predicate: RulePredicate{Type: RuleSemantic, Value: "beacon topic"}, Actions: []RuleAction{{Type: RuleNotify, Budget: 1}}, Enabled: true}, CheckLLM: true, CheckBudget: 1}, "create-watch")
	if err != nil {
		t.Fatal(err)
	}
	f.ingest(t, "beacon first matched passage")
	f.ingest(t, "beacon second matched passage")
	if _, err := f.s.RunRuleJobs(ctx, 10); err != nil {
		t.Fatal(err)
	}
	var attempts int
	var count int
	if err := f.db.Pool().QueryRow(ctx, `SELECT count(*),coalesce(max(attempts),0) FROM jobs WHERE kind='rule'`).Scan(&count, &attempts); err != nil || count != 2 || attempts != 0 {
		t.Fatalf("waiting rules %d attempts %d %v", count, attempts, err)
	}
	if _, err := f.s.RunPipeline(ctx, 10); err != nil {
		t.Fatal(err)
	}
	f.clock.Add(int64(2 * time.Minute))
	if _, err := f.s.RunRuleJobs(ctx, 10); err != nil {
		t.Fatal(err)
	}
	var why string
	if err := f.db.Pool().QueryRow(ctx, `SELECT count(*) FROM notifications WHERE why->>'matching_rule_id'=$1`, fmt.Sprint(watch.ID)).Scan(&count); err != nil || count != 1 {
		t.Fatalf("watch notifications %d %v", count, err)
	}
	if err := f.db.Pool().QueryRow(ctx, `SELECT why->>'sender' FROM notifications LIMIT 1`).Scan(&why); err != nil || !strings.Contains(why, "Space watch") || !strings.Contains(why, "matched passage") {
		t.Fatalf("watch provenance %q %v", why, err)
	}
	if err := f.db.Pool().QueryRow(ctx, `SELECT count(*) FROM watch_checks WHERE rule_id=$1`, watch.ID).Scan(&count); err != nil || count != 1 {
		t.Fatalf("check budget %d %v", count, err)
	}
	if err := f.s.UpdateRule(ctx, f.a, Rule{ID: watch.ID, Name: "changed", Predicate: watch.Predicate, Actions: []RuleAction{{Type: RuleAddLabel, LabelID: 1}}, Enabled: true}); err == nil {
		t.Fatal("watch changed to non-notify action")
	}
	disabled, err := f.s.Watches(context.Background(), f.a)
	if err != nil || len(disabled) != 1 || disabled[0].Status != "disabled" {
		t.Fatalf("disabled watch %+v %v", disabled, err)
	}
}

func TestWatchesSkipRulesCreatedBetweenListQueries(t *testing.T) {
	f := newCoreFixture(t, storetest.NewTemplate(t))
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	initial, err := f.s.CreateWatch(ctx, f.a, WatchRule{Rule: Rule{
		Name: "Initial watch", Predicate: RulePredicate{Type: RuleSemantic, Value: "beacon"},
		Actions: []RuleAction{{Type: RuleNotify, Budget: 1}}, Enabled: true,
	}}, "initial-watch")
	if err != nil {
		t.Fatal(err)
	}
	tx, err := f.db.Pool().Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(context.Background())
	if _, err := tx.Exec(ctx, `LOCK TABLE watch_rules IN ACCESS EXCLUSIVE MODE`); err != nil {
		t.Fatal(err)
	}
	type result struct {
		watches []WatchRule
		err     error
	}
	done := make(chan result, 1)
	go func() {
		watches, err := f.s.Watches(ctx, f.a)
		done <- result{watches, err}
	}()
	// Wait until Rules has returned and the watch-policy query is blocked.
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		var waiting bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM pg_locks WHERE relation='watch_rules'::regclass AND NOT granted)`).Scan(&waiting); err != nil {
			t.Fatal(err)
		}
		if waiting {
			break
		}
		select {
		case <-ctx.Done():
			t.Fatal("watch-policy query never waited for the table lock")
		case <-ticker.C:
		}
	}
	var added int64
	if err := tx.QueryRow(ctx, `INSERT INTO rules(user_id,source,ast,action,enabled,created_by)
		SELECT user_id,'Concurrent watch',ast,action,enabled,created_by FROM rules WHERE id=$1 RETURNING id`, initial.ID).Scan(&added); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx, `INSERT INTO watch_rules(rule_id,check_llm,check_budget) VALUES($1,false,3)`, added); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	listed := <-done
	if listed.err != nil {
		t.Fatal(listed.err)
	}
	if len(listed.watches) != 1 || listed.watches[0].ID != initial.ID {
		t.Fatalf("list included a watch without a loaded rule: %+v", listed.watches)
	}
	// The concurrent watch is complete and visible on the next listing.
	if watches, err := f.s.Watches(ctx, f.a); err != nil || len(watches) != 2 || watches[1].ID != added {
		t.Fatalf("next watch listing: %+v %v", watches, err)
	}
}

func TestWatchCreationConcurrentReplay(t *testing.T) {
	f := newCoreFixture(t, storetest.NewTemplate(t))
	ctx := context.Background()
	watch := WatchRule{Rule: Rule{Name: "Watch", Predicate: RulePredicate{Type: RuleSemantic, Value: "beacon"}, Actions: []RuleAction{{Type: RuleNotify, Budget: 2}}, Enabled: true}, CheckLLM: true, CheckBudget: 3}
	if _, err := f.s.CreateWatch(ctx, f.a, watch, ""); !errors.Is(err, ErrInvalid) {
		t.Fatalf("missing op_id: %v", err)
	}
	const workers = 8
	results := make(chan WatchRule, workers)
	errs := make(chan error, workers)
	var wg sync.WaitGroup
	for range workers {
		wg.Go(func() {
			result, err := f.s.CreateWatch(ctx, f.a, watch, "concurrent-watch")
			results <- result
			errs <- err
		})
	}
	wg.Wait()
	close(results)
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	var original WatchRule
	for result := range results {
		if original.ID == 0 {
			original = result
		}
		if !reflect.DeepEqual(result, original) {
			t.Fatalf("replay differs: %+v vs %+v", result, original)
		}
	}
	watches, err := f.s.Watches(ctx, f.a)
	if err != nil || len(watches) != 1 {
		t.Fatalf("watches=%v error=%v", watches, err)
	}
	watch.Name = "Changed retry payload"
	replay, err := f.s.CreateWatch(ctx, f.a, watch, "concurrent-watch")
	if err != nil || !reflect.DeepEqual(replay, original) {
		t.Fatalf("original receipt lost: %+v %v", replay, err)
	}
	if _, err := f.s.CreateLabel(ctx, f.a, "Label", "label-operation"); err != nil {
		t.Fatal(err)
	}
	if _, err := f.s.CreateWatch(ctx, f.a, watch, "label-operation"); !errors.Is(err, ErrInvalid) {
		t.Fatalf("cross-method op reused: %v", err)
	}
}
