package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/L-K-M/Earnest/internal/store/storetest"
)

func TestRuleJobUnicodeFailure(t *testing.T) {
	template := storetest.NewTemplate(t)
	for _, attempt := range []int{1, ruleJobMaxAttempts} {
		t.Run(fmt.Sprint(attempt), func(t *testing.T) {
			f := newCoreFixture(t, template)
			ctx := context.Background()
			rule, err := f.s.CreateRule(ctx, f.a, Rule{Name: "Rule", Enabled: true, Predicate: RulePredicate{Type: RuleKeyword, Terms: []string{"word"}}, Actions: []RuleAction{{Type: RuleNotify}}}, "rule")
			if err != nil {
				t.Fatal(err)
			}
			f.ingest(t, "word")
			if _, err := f.db.Pool().Exec(ctx, "UPDATE jobs SET attempts=$1 WHERE kind=$2", attempt-1, ruleJobKind); err != nil {
				t.Fatal(err)
			}
			job, err := f.s.claimRuleJob(ctx)
			if err != nil || job == nil {
				t.Fatalf("claim: %v %v", job, err)
			}
			if err := f.s.finishRuleJob(ctx, job, errors.New(strings.Repeat("界", 1100))); err != nil {
				t.Fatalf("finish Unicode failure: %v", err)
			}
			var reason string
			if attempt == ruleJobMaxAttempts {
				err = f.db.Pool().QueryRow(ctx, "SELECT action->>'disable_reason' FROM rules WHERE id=$1 AND NOT enabled", rule.ID).Scan(&reason)
			} else {
				err = f.db.Pool().QueryRow(ctx, "SELECT last_error FROM jobs WHERE id=$1 AND locked_until IS NULL", job.id).Scan(&reason)
			}
			if err != nil || !utf8.ValidString(reason) || utf8.RuneCountInString(reason) != 1024 {
				t.Fatalf("stored reason: runes=%d valid=%v err=%v", utf8.RuneCountInString(reason), utf8.ValidString(reason), err)
			}
		})
	}
}
