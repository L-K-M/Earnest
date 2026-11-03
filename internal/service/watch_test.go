package service

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/L-K-M/Earnest/internal/llm"
	"github.com/L-K-M/Earnest/internal/store/storetest"
)

func TestWatchPausesThenNotifiesWithinBudget(t *testing.T) {
	f := newCoreFixture(t, storetest.NewTemplate(t))
	p := &pipelineProvider{classifier: `{"labels":["news"]}`}
	ctx := pipelineTestContext(t, f.db, p, []llm.Task{llm.TaskEmbed, llm.TaskClassify}, 0)
	watch, err := f.s.CreateWatch(ctx, f.a, WatchRule{Rule: Rule{Name: "Space watch", Predicate: RulePredicate{Type: RuleSemantic, Value: "beacon topic"}, Actions: []RuleAction{{Type: RuleNotify, Budget: 1}}, Enabled: true}, CheckLLM: true, CheckBudget: 1})
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
