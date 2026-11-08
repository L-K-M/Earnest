package service

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/L-K-M/Earnest/internal/llm"
	"github.com/L-K-M/Earnest/internal/store"
	"github.com/L-K-M/Earnest/internal/store/storetest"
	"github.com/L-K-M/Earnest/internal/syncmodel"
)

func TestRuleCreateAndDryRun(t *testing.T) {
	f := newCoreFixture(t, storetest.NewTemplate(t))
	ctx := context.Background()
	id := f.ingest(t, "matching word")
	p := RulePredicate{Type: RuleKeyword, Terms: []string{"matching"}}
	actions := []RuleAction{{Type: RuleNotify, Budget: 1}}
	got, total, err := f.s.DryRunRule(ctx, f.a, p, actions)
	if err != nil || len(got) != 1 || got[0] != id || total != 1 {
		t.Fatalf("dry run: %v total=%d %v", got, total, err)
	}
	var before, after int
	if err := f.db.Pool().QueryRow(ctx, `SELECT (SELECT count(*) FROM activity) + (SELECT count(*) FROM changes) + (SELECT count(*) FROM entry_state) + (SELECT count(*) FROM op_ids) + (SELECT count(*) FROM entry_labels)`).Scan(&before); err != nil {
		t.Fatal(err)
	}
	if _, _, err := f.s.DryRunRule(ctx, f.a, p, actions); err != nil {
		t.Fatal(err)
	}
	if err := f.db.Pool().QueryRow(ctx, `SELECT (SELECT count(*) FROM activity) + (SELECT count(*) FROM changes) + (SELECT count(*) FROM entry_state) + (SELECT count(*) FROM op_ids) + (SELECT count(*) FROM entry_labels)`).Scan(&after); err != nil {
		t.Fatal(err)
	}
	if before != after {
		t.Fatal("dry run wrote activity")
	}
	rule, err := f.s.CreateRule(ctx, f.a, Rule{Name: "Example", Predicate: p, Actions: actions, Enabled: true}, "create-rule")
	if err != nil || rule.ID == 0 {
		t.Fatalf("create: %+v %v", rule, err)
	}
	agent := f.a
	agent.Kind = PrincipalAgent
	agent.Scopes = HumanScopes
	if _, err := f.s.CreateRule(ctx, agent, Rule{Name: "Bad", Predicate: p, Actions: actions}, "create-rule"); !errors.Is(err, ErrForbidden) {
		t.Fatalf("agent: %v", err)
	}
	f.clock.Add(int64(8 * 24 * time.Hour))
	got, total, err = f.s.DryRunRule(ctx, f.a, p, actions)
	if err != nil || len(got) != 0 || total != 0 {
		t.Fatalf("old entry: %v total=%d %v", got, total, err)
	}
}

func TestDryRunRuleCapsSample(t *testing.T) {
	f := newCoreFixture(t, storetest.NewTemplate(t))
	ctx := context.Background()
	items := make([]IngestItem, maxDryRunMatches+5)
	for i := range items {
		items[i] = coreItem(fmt.Sprintf("matching %d", i))
	}
	if _, err := f.s.RecordFetch(ctx, FetchOutcome{FeedID: f.feed, Status: FetchOK, Items: items}); err != nil {
		t.Fatal(err)
	}
	p := RulePredicate{Type: RuleKeyword, Terms: []string{"matching"}}
	ids, total, err := f.s.DryRunRule(ctx, f.a, p, []RuleAction{{Type: RuleNotify, Budget: 1}})
	if err != nil {
		t.Fatal(err)
	}
	if total != maxDryRunMatches+5 {
		t.Fatalf("total=%d want %d", total, maxDryRunMatches+5)
	}
	if len(ids) != maxDryRunMatches {
		t.Fatalf("sample=%d want %d", len(ids), maxDryRunMatches)
	}
}

func TestRuleExecutionAndManagement(t *testing.T) {
	f := newCoreFixture(t, storetest.NewTemplate(t))
	ctx := context.Background()
	label, err := f.s.CreateLabel(ctx, f.a, "Important", "label-create")
	if err != nil {
		t.Fatal(err)
	}
	rule, err := f.s.CreateRule(ctx, f.a, Rule{Name: "Feed rule", Enabled: true, Predicate: RulePredicate{Type: RuleFeedIn, IDs: []int64{f.feed}}, Actions: []RuleAction{{Type: RuleAddLabel, LabelID: label.ID}, {Type: RuleSetShelf, Shelf: "done"}}}, "create-rule")
	if err != nil {
		t.Fatal(err)
	}
	id := f.ingest(t, "matched")
	if n, err := f.s.RunRuleJobs(ctx, 10); err != nil || n != 1 {
		t.Fatalf("run rule job: %d %v", n, err)
	}
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
	rule, err := f.s.CreateRule(ctx, f.a, Rule{Name: "Delayed", Enabled: true, Predicate: RulePredicate{Type: RuleFeedIn, IDs: []int64{f.feed}}, Actions: []RuleAction{{Type: RuleSetShelf, Shelf: "done"}}}, "create-rule")
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

func TestRuleJobRetriesAndDisables(t *testing.T) {
	f := newCoreFixture(t, storetest.NewTemplate(t))
	ctx := context.Background()
	label, err := f.s.CreateLabel(ctx, f.a, "Temporary", "temporary-label")
	if err != nil {
		t.Fatal(err)
	}
	rule, err := f.s.CreateRule(ctx, f.a, Rule{Name: "Broken", Enabled: true, Predicate: RulePredicate{Type: RuleFeedIn, IDs: []int64{f.feed}}, Actions: []RuleAction{{Type: RuleAddLabel, LabelID: label.ID}}}, "create-rule")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.Pool().Exec(ctx, `UPDATE labels SET deleted_at=now() WHERE id=$1`, label.ID); err != nil {
		t.Fatal(err)
	}
	id := f.ingest(t, "committed despite rule failure")
	for attempt := 1; attempt <= ruleJobMaxAttempts; attempt++ {
		n, err := f.s.RunRuleJobs(ctx, 10)
		if err != nil || n != 1 {
			t.Fatalf("attempt %d: %d %v", attempt, n, err)
		}
		if attempt < ruleJobMaxAttempts {
			n, err := f.s.RunRuleJobs(ctx, 10)
			if err != nil || n != 0 {
				t.Fatalf("retry ran before backoff: %d %v", n, err)
			}
			f.clock.Add(int64(ruleJobRetryDelay * time.Duration(1<<(attempt-1))))
		}
	}
	var enabled bool
	var reason string
	if err := f.db.Pool().QueryRow(ctx, `SELECT enabled,action->>'disable_reason' FROM rules WHERE id=$1`, rule.ID).Scan(&enabled, &reason); err != nil {
		t.Fatal(err)
	}
	if enabled || reason == "" {
		t.Fatalf("rule enabled=%v reason=%q", enabled, reason)
	}
	var exists bool
	if err := f.db.Pool().QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM entries WHERE id=$1)`, id).Scan(&exists); err != nil || !exists {
		t.Fatalf("entry missing: %v", err)
	}
}

func TestHumanCanRevertRuleShelfChange(t *testing.T) {
	f := newCoreFixture(t, storetest.NewTemplate(t))
	ctx := context.Background()
	_, err := f.s.CreateRule(ctx, f.a, Rule{Name: "Rule", Enabled: true, Predicate: RulePredicate{Type: RuleFeedIn, IDs: []int64{f.feed}}, Actions: []RuleAction{{Type: RuleSetShelf, Shelf: "done"}}}, "create-rule")
	if err != nil {
		t.Fatal(err)
	}
	id := f.ingest(t, "article")
	if _, err := f.s.RunRuleJobs(ctx, 10); err != nil {
		t.Fatal(err)
	}
	rows, err := f.s.ListActivity(ctx, f.a, ActivityFilter{Action: "state.updated"})
	if err != nil || len(rows) == 0 {
		t.Fatal(rows, err)
	}
	if err := f.s.Revert(ctx, f.a, rows[0].ID, "undo-rule"); err != nil {
		t.Fatalf("revert rule: %v", err)
	}
	state, err := store.LoadState(ctx, f.db.Pool(), f.a.PrincipalID, id)
	if err != nil || state.Shelf.Value != syncmodel.None {
		t.Fatal(state, err)
	}
}

func TestRuleJobFromOldGenerationDoesNotDeliver(t *testing.T) {
	f := newCoreFixture(t, storetest.NewTemplate(t))
	ctx := context.Background()
	hook, err := f.s.CreateWebhook(ctx, f.a, WebhookInput{URL: "https://example.test/hook", Events: []string{"entry.matched"}}, "hook")
	if err != nil {
		t.Fatal(err)
	}
	_, err = f.s.CreateRule(ctx, f.a, Rule{Name: "Rule", Enabled: true, Predicate: RulePredicate{Type: RuleKeyword, Terms: []string{"article"}}, Actions: []RuleAction{{Type: RuleNotify, Budget: 1}, {Type: RuleWebhook, WebhookID: hook.ID}}}, "create-rule")
	if err != nil {
		t.Fatal(err)
	}
	f.ingest(t, "article")
	if _, err := f.db.RotateGeneration(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := f.s.RunRuleJobs(ctx, 10); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := f.db.Pool().QueryRow(ctx, `SELECT (SELECT count(*) FROM notifications)+(SELECT count(*) FROM jobs WHERE kind='webhook')`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("old job created %d deliveries", count)
	}
}

func TestRuleExecutionUsesEditedPredicate(t *testing.T) {
	f := newCoreFixture(t, storetest.NewTemplate(t))
	ctx := context.Background()
	id := f.ingest(t, "article")
	old, err := f.s.CreateRule(ctx, f.a, Rule{Name: "Rule", Enabled: true, Predicate: RulePredicate{Type: RuleFeedIn, IDs: []int64{f.feed}}, Actions: []RuleAction{{Type: RuleSetShelf, Shelf: "done"}}}, "create-rule")
	if err != nil {
		t.Fatal(err)
	}
	entries, err := f.s.ruleEntries(ctx, f.a.UserID, time.Time{}, []int64{id})
	if err != nil || len(entries) != 1 {
		t.Fatal(entries, err)
	}
	edited := old
	edited.Predicate = RulePredicate{Type: RuleAnd, Children: []RulePredicate{{Type: RuleFeedIn, IDs: []int64{f.feed}}, {Type: RuleKeyword, Terms: []string{"absent"}}}}
	if err := f.s.UpdateRule(ctx, f.a, edited); err != nil {
		t.Fatal(err)
	}
	if err := f.s.applyRule(ctx, f.a, old, entries[0]); err != nil {
		t.Fatal(err)
	}
	state, err := store.LoadState(ctx, f.db.Pool(), f.a.PrincipalID, id)
	if err != nil || state.Shelf.Value != syncmodel.None {
		t.Fatalf("state %v: %v", state, err)
	}
	edited.Predicate = old.Predicate
	edited.Actions = []RuleAction{{Type: RuleSetShelf, Shelf: "later"}}
	if err := f.s.UpdateRule(ctx, f.a, edited); err != nil {
		t.Fatal(err)
	}
	if err := f.s.applyRule(ctx, f.a, old, entries[0]); err != nil {
		t.Fatal(err)
	}
	state, err = store.LoadState(ctx, f.db.Pool(), f.a.PrincipalID, id)
	if err != nil || state.Shelf.Value != syncmodel.Later {
		t.Fatalf("edited action state %v: %v", state, err)
	}
}

func TestRuleCreationReplay(t *testing.T) {
	f := newCoreFixture(t, storetest.NewTemplate(t))
	ctx := context.Background()
	rule := Rule{Name: "Rule", Enabled: true, Predicate: RulePredicate{Type: RuleKeyword, Terms: []string{"news"}}, Actions: []RuleAction{{Type: RuleNotify, Budget: 1}}}
	first, err := f.s.CreateRule(ctx, f.a, rule, "create-rule")
	if err != nil {
		t.Fatal(err)
	}
	second, err := f.s.CreateRule(ctx, f.a, rule, "create-rule")
	if err != nil {
		t.Fatal(err)
	}
	if first.ID != second.ID {
		t.Fatalf("replay created rules %d and %d", first.ID, second.ID)
	}
}

func TestGoogleLabelThroughIncludesFolderAndLabel(t *testing.T) {
	f := newCoreFixture(t, storetest.NewTemplate(t))
	ctx := context.Background()
	var folder int64
	if err := f.db.Pool().QueryRow(ctx, `INSERT INTO folders(user_id,name) VALUES($1,'Topic') RETURNING id`, f.a.UserID).Scan(&folder); err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.Pool().Exec(ctx, `UPDATE subscriptions SET folder_id=$1 WHERE id=$2`, folder, f.subscription); err != nil {
		t.Fatal(err)
	}
	first := f.ingest(t, "folder article")
	var feed int64
	if err := f.db.Pool().QueryRow(ctx, `INSERT INTO feeds(url) VALUES('https://other.test/feed') RETURNING id`).Scan(&feed); err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.Pool().Exec(ctx, `INSERT INTO subscriptions(user_id,feed_id) VALUES($1,$2)`, f.a.UserID, feed); err != nil {
		t.Fatal(err)
	}
	if _, err := f.s.RecordFetch(ctx, FetchOutcome{FeedID: feed, Status: FetchOK, Items: []IngestItem{coreItem("label article")}}); err != nil {
		t.Fatal(err)
	}
	var second int64
	if err := f.db.Pool().QueryRow(ctx, `SELECT id FROM entries WHERE feed_id=$1`, feed).Scan(&second); err != nil {
		t.Fatal(err)
	}
	label, err := f.s.CreateLabel(ctx, f.a, "Topic", "label")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.s.AddLabel(ctx, f.a, label.ID, second, "membership"); err != nil {
		t.Fatal(err)
	}
	gen, err := f.db.Generation(ctx)
	if err != nil {
		t.Fatal(err)
	}
	compat := f.a
	compat.Via = CredentialCompat
	req := ThroughRequest{Scope: syncmodel.Scope{Kind: syncmodel.ScopeAll}, Write: syncmodel.Write{OpID: "mark", Principal: syncmodel.ID(f.a.PrincipalID), Base: syncmodel.Revision{Generation: gen}, OccurredAt: f.s.now(), Kind: syncmodel.Passive, Register: syncmodel.Seen, Value: syncmodel.SeenValue}}
	if _, err := f.s.GoogleLabelThrough(ctx, compat, req, "Topic"); err != nil {
		t.Fatal(err)
	}
	for _, id := range []int64{first, second} {
		state, err := store.LoadState(ctx, f.db.Pool(), f.a.PrincipalID, id)
		if err != nil || state.Seen.Value != syncmodel.SeenValue {
			t.Fatalf("entry %d: %v %v", id, state, err)
		}
	}
}

func TestEditedRulePreparesNewModelPredicates(t *testing.T) {
	template := storetest.NewTemplate(t)
	for _, tc := range []struct {
		name      string
		predicate RulePredicate
		task      llm.Task
	}{
		{"classifier disabled", RulePredicate{Type: RuleClassifierIn, Terms: []string{"news"}}, ""},
		{"semantic disabled", RulePredicate{Type: RuleSemantic, Value: "news"}, ""},
		{"classifier pending", RulePredicate{Type: RuleClassifierIn, Terms: []string{"news"}}, llm.TaskClassify},
		{"semantic pending", RulePredicate{Type: RuleSemantic, Value: "news"}, llm.TaskEmbed},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newCoreFixture(t, template)
			ctx := t.Context()
			if tc.task != "" {
				ctx = generatedContext(t, f, &generatedProvider{outputs: map[string]string{}}, []llm.Task{tc.task}, 0)
			}
			id := f.ingest(t, "article")
			old, err := f.s.CreateRule(ctx, f.a, Rule{Name: "Edited", Enabled: true, Predicate: RulePredicate{Type: RuleFeedIn, IDs: []int64{f.feed}}, Actions: []RuleAction{{Type: RuleSetShelf, Shelf: "done"}}}, "edited-model-rule")
			if err != nil {
				t.Fatal(err)
			}
			entries, err := f.s.ruleEntries(ctx, f.a.UserID, time.Time{}, []int64{id})
			if err != nil || len(entries) != 1 {
				t.Fatal(entries, err)
			}
			prepared, ready, err := f.s.prepareModelPredicates(ctx, f.a.UserID, old.Predicate, entries[0])
			if err != nil || !ready {
				t.Fatal(ready, err)
			}
			// Interleave the edit after preparation and before the locked application.
			edited := old
			edited.Predicate = RulePredicate{Type: RuleAnd, Children: []RulePredicate{old.Predicate, {Type: RuleNot, Children: []RulePredicate{tc.predicate}}}}
			if err := f.s.UpdateRule(ctx, f.a, edited); err != nil {
				t.Fatal(err)
			}
			err = f.s.applyRule(ctx, f.a, old, prepared)
			if tc.task == "" && err != nil || tc.task != "" && !errors.Is(err, ErrPipelinePaused) {
				t.Fatalf("edited predicate preparation: %v", err)
			}
			state, err := store.LoadState(ctx, f.db.Pool(), f.a.PrincipalID, id)
			if err != nil || state.Shelf.Value != syncmodel.None {
				t.Fatalf("disabled model changed shelf: %v err=%v", state.Shelf.Value, err)
			}
		})
	}
}

func TestLegacyRuleJobMigratesAndExecutes(t *testing.T) {
	f := newCoreFixture(t, storetest.NewTemplate(t))
	ctx := t.Context()
	_, err := f.s.CreateRule(ctx, f.a, Rule{Name: "Legacy", Enabled: true, Predicate: RulePredicate{Type: RuleFeedIn, IDs: []int64{f.feed}}, Actions: []RuleAction{{Type: RuleNotify, Budget: 3}}}, "legacy-rule")
	if err != nil {
		t.Fatal(err)
	}
	f.ingest(t, "legacy pending article")
	if _, err := f.db.Pool().Exec(ctx, `UPDATE jobs SET payload=payload-'generation' WHERE kind='rule'`); err != nil {
		t.Fatal(err)
	}
	// Recreate the upgrade boundary with a pending pre-generation job. Later
	// migrations are idempotent and rerun with it.
	if _, err := f.db.Pool().Exec(ctx, `DELETE FROM schema_migrations WHERE name>='0009_'`); err != nil {
		t.Fatal(err)
	}
	if err := f.db.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	if n, err := f.s.RunRuleJobs(ctx, 10); err != nil || n != 1 {
		t.Fatalf("jobs=%d err=%v", n, err)
	}
	var notifications, pending int
	if err := f.db.Pool().QueryRow(ctx, `SELECT (SELECT count(*) FROM notifications),(SELECT count(*) FROM jobs WHERE kind='rule')`).Scan(&notifications, &pending); err != nil {
		t.Fatal(err)
	}
	if notifications != 1 || pending != 0 {
		t.Fatalf("notifications=%d pending=%d", notifications, pending)
	}
}
