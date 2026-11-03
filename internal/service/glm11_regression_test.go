package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"testing"
	"time"

	"github.com/L-K-M/Earnest/internal/llm"
	"github.com/L-K-M/Earnest/internal/pipeline"
	"github.com/L-K-M/Earnest/internal/store/storetest"
)

func TestCompileRuleRequiresWholeHumanIDs(t *testing.T) {
	f := newCoreFixture(t, storetest.NewTemplate(t))
	ctx := t.Context()
	label, err := f.s.CreateLabel(ctx, f.a, "Chosen label", "label")
	if err != nil {
		t.Fatal(err)
	}
	hook, err := f.s.CreateWebhook(ctx, f.a, WebhookInput{URL: "https://hook.example/match", Events: []string{"entry.matched"}}, "hook")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.Pool().Exec(ctx, `UPDATE feeds SET title='Chosen feed' WHERE id=$1`, f.feed); err != nil {
		t.Fatal(err)
	}
	p := &generatedProvider{outputs: map[string]string{}}
	ctx = generatedContext(t, f, p, []llm.Task{llm.TaskCompileRule}, 0)
	for _, action := range []struct {
		kind, field string
		id          int64
	}{
		{"add_label", "label_id", label.ID},
		{"webhook", "webhook_id", hook.ID},
	} {
		p.set("compiled_rule", fmt.Sprintf(`{"predicate":{"type":"and","children":[{"type":"feed_in","feed_names":["Chosen feed"]},{"type":"keyword","terms":["funding"]}]},"actions":[{"type":%q,%q:%d}]}`, action.kind, action.field, action.id))
		for _, tc := range []struct {
			name, token string
			valid       bool
		}{
			{"exact", fmt.Sprint(action.id), true},
			{"punctuation", fmt.Sprintf("#%d,", action.id), true},
			{"larger number", fmt.Sprintf("%d0", action.id), false},
			{"word", fmt.Sprintf("publisher%d", action.id), false},
			{"unicode word", fmt.Sprintf("記事%d", action.id), false},
		} {
			t.Run(action.kind+"/"+tc.name, func(t *testing.T) {
				_, err := f.s.CompileRule(ctx, f.a, "funding from Chosen feed with destination "+tc.token)
				if tc.valid && err != nil || !tc.valid && !errors.Is(err, ErrInvalid) {
					t.Fatalf("CompileRule destination %q: got %v, valid=%v", tc.token, err, tc.valid)
				}
			})
		}
	}
}

func TestEditionLoadFailurePreservesSnapshot(t *testing.T) {
	template := storetest.NewTemplate(t)
	for _, tc := range []struct {
		name    string
		attempt int
		cause   error
		status  string
	}{
		{"retry", 1, context.DeadlineExceeded, "pending"},
		{"exhausted", editionMaxAttempts, context.DeadlineExceeded, "failed"},
		{"paused", editionMaxAttempts, ErrPipelinePaused, "paused"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newCoreFixture(t, template)
			f.ingest(t, "Daily Beacon report.")
			if _, err := f.db.Pool().Exec(t.Context(), `UPDATE subscriptions SET cadence='week' WHERE id=$1`, f.subscription); err != nil {
				t.Fatal(err)
			}
			p := &generatedProvider{outputs: map[string]string{}}
			ctx := generatedContext(t, f, p, []llm.Task{llm.TaskEdition}, 0)
			edition, err := f.s.TriggerEdition(ctx, f.a, EditionDaily, time.Time{}, "edition")
			if err != nil || len(edition.Sections) == 0 {
				t.Fatalf("snapshot: %+v %v", edition, err)
			}
			job, err := f.s.claimEdition(ctx)
			if err != nil || job == nil {
				t.Fatalf("claim: %+v %v", job, err)
			}
			// Match the worker's empty result when loading fails before Scan.
			cancelled, cancel := context.WithCancel(ctx)
			cancel()
			unloaded, err := f.s.editionByID(cancelled, f.a.UserID, edition.ID)
			if err == nil {
				t.Fatal("cancelled load succeeded")
			}
			job.attempt = tc.attempt
			if _, err := f.db.Pool().Exec(ctx, `UPDATE jobs SET attempts=$2 WHERE id=$1`, job.id, job.attempt); err != nil {
				t.Fatal(err)
			}
			if err := f.s.finishEdition(ctx, job, &unloaded, tc.cause); err != nil {
				t.Fatal(err)
			}
			stored, err := f.s.editionByID(ctx, f.a.UserID, edition.ID)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(stored.Sections, edition.Sections) || stored.Model != edition.Model || stored.PromptVersion != edition.PromptVersion {
				t.Fatalf("snapshot overwritten after failed load: sections=%+v model=%q prompt=%q", stored.Sections, stored.Model, stored.PromptVersion)
			}
			if stored.Status != tc.status {
				t.Fatalf("status=%q, want %q", stored.Status, tc.status)
			}

			var jobs, attempts int
			var released bool
			if err := f.db.Pool().QueryRow(ctx, `SELECT count(*),coalesce(max(attempts),0),coalesce(bool_and(locked_until IS NULL AND run_at>$2),true) FROM jobs WHERE id=$1`, job.id, f.s.now()).Scan(&jobs, &attempts, &released); err != nil {
				t.Fatal(err)
			}
			if tc.status == "failed" {
				if jobs != 0 {
					t.Fatal("exhausted job was not removed")
				}
			} else {
				wantAttempts := tc.attempt
				if tc.status == "paused" {
					wantAttempts--
				}
				if jobs != 1 || attempts != wantAttempts || !released {
					t.Fatalf("retry job: jobs=%d attempts=%d released=%v", jobs, attempts, released)
				}
			}
		})
	}
}

func TestEditionRequestUsesPeriod(t *testing.T) {
	template := storetest.NewTemplate(t)
	for _, tc := range []struct {
		period   EditionPeriod
		question string
	}{
		{EditionDaily, "Today"},
		{EditionWeekly, "This week"},
	} {
		t.Run(string(tc.period), func(t *testing.T) {
			f := newCoreFixture(t, template)
			f.ingest(t, "Beacon report.")
			if _, err := f.db.Pool().Exec(t.Context(), `UPDATE subscriptions SET cadence='week' WHERE id=$1`, f.subscription); err != nil {
				t.Fatal(err)
			}
			p := &generatedProvider{outputs: map[string]string{}}
			ctx := generatedContext(t, f, p, []llm.Task{llm.TaskEdition}, 0)
			batch := &testEditionBatch{}
			ctx = pipeline.WithEditionBatch(ctx, batch)
			if _, err := f.s.TriggerEdition(ctx, f.a, tc.period, f.s.now().Add(time.Hour), "edition"); err != nil {
				t.Fatal(err)
			}
			if _, err := f.s.RunEditionJobs(ctx, 1); err != nil {
				t.Fatal(err)
			}
			if len(batch.requests) != 1 {
				t.Fatalf("requests=%d", len(batch.requests))
			}
			var input struct {
				Question string `json:"question"`
			}
			if err := json.Unmarshal([]byte(batch.requests[0].Messages[0].Content), &input); err != nil {
				t.Fatal(err)
			}
			if input.Question != tc.question {
				t.Fatalf("edition question=%q, want %q", input.Question, tc.question)
			}
		})
	}
}

func TestWatchReplayRestoresZeroBudget(t *testing.T) {
	f := newCoreFixture(t, storetest.NewTemplate(t))
	watch := WatchRule{Rule: Rule{
		Name: "Watch", Predicate: RulePredicate{Type: RuleSemantic, Value: "beacon"},
		Actions: []RuleAction{{Type: RuleNotify}}, Enabled: true,
	}}
	original, err := f.s.CreateWatch(t.Context(), f.a, watch, "watch")
	if err != nil {
		t.Fatal(err)
	}
	watch.Actions = []RuleAction{{Type: RuleNotify, Budget: 10}}
	replayed, err := f.s.CreateWatch(t.Context(), f.a, watch, "watch")
	if err != nil || !reflect.DeepEqual(replayed, original) {
		t.Fatalf("replayed actions=%+v, want %+v: %v", replayed.Actions, original.Actions, err)
	}
}
