package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/L-K-M/Earnest/internal/store/storetest"
	"github.com/L-K-M/Earnest/internal/syncmodel"
)

func TestRuleCreateAndDryRun(t *testing.T) {
	f := newCoreFixture(t, storetest.NewTemplate(t))
	ctx := context.Background()
	id := f.ingest(t, "matching word")
	p := RulePredicate{Type: RuleKeyword, Terms: []string{"matching"}}
	actions := []RuleAction{{Type: RuleNotify, Budget: 1}}
	got, err := f.s.DryRunRule(ctx, f.a, p, actions)
	if err != nil || len(got) != 1 || got[0] != id {
		t.Fatalf("dry run: %v %v", got, err)
	}
	var before, after int
	if err := f.db.Pool().QueryRow(ctx, `SELECT (SELECT count(*) FROM activity) + (SELECT count(*) FROM changes) + (SELECT count(*) FROM entry_state) + (SELECT count(*) FROM op_ids) + (SELECT count(*) FROM entry_labels)`).Scan(&before); err != nil {
		t.Fatal(err)
	}
	if _, err := f.s.DryRunRule(ctx, f.a, p, actions); err != nil {
		t.Fatal(err)
	}
	if err := f.db.Pool().QueryRow(ctx, `SELECT (SELECT count(*) FROM activity) + (SELECT count(*) FROM changes) + (SELECT count(*) FROM entry_state) + (SELECT count(*) FROM op_ids) + (SELECT count(*) FROM entry_labels)`).Scan(&after); err != nil {
		t.Fatal(err)
	}
	if before != after {
		t.Fatal("dry run wrote activity")
	}
	rule, err := f.s.CreateRule(ctx, f.a, Rule{Name: "Example", Predicate: p, Actions: actions, Enabled: true})
	if err != nil || rule.ID == 0 {
		t.Fatalf("create: %+v %v", rule, err)
	}
	agent := f.a
	agent.Kind = PrincipalAgent
	agent.Scopes = HumanScopes
	if _, err := f.s.CreateRule(ctx, agent, Rule{Name: "Bad", Predicate: p, Actions: actions}); !errors.Is(err, ErrForbidden) {
		t.Fatalf("agent: %v", err)
	}
	f.clock.Add(int64(8 * 24 * time.Hour))
	got, err = f.s.DryRunRule(ctx, f.a, p, actions)
	if err != nil || len(got) != 0 {
		t.Fatalf("old entry: %v %v", got, err)
	}
}

func TestRuleExecutionAndManagement(t *testing.T) {
	f := newCoreFixture(t, storetest.NewTemplate(t))
	ctx := context.Background()
	label, err := f.s.CreateLabel(ctx, f.a, "Important", "label-create")
	if err != nil {
		t.Fatal(err)
	}
	rule, err := f.s.CreateRule(ctx, f.a, Rule{Name: "Feed rule", Enabled: true, Predicate: RulePredicate{Type: RuleFeedIn, IDs: []int64{f.feed}}, Actions: []RuleAction{{Type: RuleAddLabel, LabelID: label.ID}, {Type: RuleSetShelf, Shelf: "done"}}})
	if err != nil {
		t.Fatal(err)
	}
	id := f.ingest(t, "matched")
	var shelf, source string
	if err := f.db.Pool().QueryRow(ctx, `SELECT shelf,shelf_source FROM entry_state WHERE principal_id=$1 AND entry_id=$2`, f.a.PrincipalID, id).Scan(&shelf, &source); err != nil {
		t.Fatal(err)
	}
	if shelf != "done" || source != "rule_or_agent" {
		t.Fatalf("state: %s %s", shelf, source)
	}
	var labels, activity int
	if err := f.db.Pool().QueryRow(ctx, `SELECT (SELECT count(*) FROM entry_labels WHERE entry_id=$1 AND label_id=$2 AND deleted_at IS NULL),(SELECT count(*) FROM activity WHERE target->>'rule_name'='Feed rule')`, id, label.ID).Scan(&labels, &activity); err != nil {
		t.Fatal(err)
	}
	if labels != 1 || activity != 2 {
		t.Fatalf("labels=%d activity=%d", labels, activity)
	}
	rules, err := f.s.Rules(ctx, f.a)
	if err != nil || len(rules) != 1 || rules[0].MatchCount != 1 || rules[0].LastMatchAt == nil {
		t.Fatalf("rules: %+v %v", rules, err)
	}
	rule.Name = "Changed"
	rule.Enabled = false
	if err := f.s.UpdateRule(ctx, f.a, rule); err != nil {
		t.Fatal(err)
	}
	if err := f.s.SetRuleEnabled(ctx, f.a, rule.ID, true); err != nil {
		t.Fatal(err)
	}
	if err := f.s.DeleteRule(ctx, f.a, rule.ID); err != nil {
		t.Fatal(err)
	}
	if err := f.s.DeleteRule(ctx, f.a, rule.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("delete missing: %v", err)
	}
}

func TestRuleDelayedWriteLosesToUnread(t *testing.T) {
	f := newCoreFixture(t, storetest.NewTemplate(t))
	ctx := context.Background()
	id := f.ingest(t, "delayed")
	f.clock.Add(int64(time.Second))
	write := f.write(t, id, "unread", syncmodel.Seen, syncmodel.Unseen)
	if _, err := f.s.Batch(ctx, f.a, []syncmodel.Write{write}); err != nil {
		t.Fatal(err)
	}
	rule, err := f.s.CreateRule(ctx, f.a, Rule{Name: "Delayed", Enabled: true, Predicate: RulePredicate{Type: RuleFeedIn, IDs: []int64{f.feed}}, Actions: []RuleAction{{Type: RuleSetShelf, Shelf: "done"}}})
	if err != nil {
		t.Fatal(err)
	}
	entries, err := f.s.ruleEntries(ctx, f.a.UserID, time.Time{}, []int64{id})
	if err != nil || len(entries) != 1 {
		t.Fatalf("entries: %v %v", entries, err)
	}
	if err := f.s.applyRule(ctx, f.a, rule, entries[0]); err != nil {
		t.Fatal(err)
	}
	var shelf string
	if err := f.db.Pool().QueryRow(ctx, `SELECT shelf FROM entry_state WHERE principal_id=$1 AND entry_id=$2`, f.a.PrincipalID, id).Scan(&shelf); err != nil {
		t.Fatal(err)
	}
	if shelf != "none" {
		t.Fatalf("shelf = %s", shelf)
	}
}
