package service

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/L-K-M/Earnest/internal/store/storetest"
)

func TestRuleDeliveries(t *testing.T) {
	f := newCoreFixture(t, storetest.NewTemplate(t))
	ctx := context.Background()
	first := f.ingest(t, "urgent funding")
	second := f.ingest(t, "more funding")
	hook, err := f.s.CreateWebhook(ctx, f.a, WebhookInput{URL: "https://example.test/hook", Events: []string{"entry.matched"}}, "rule-hook")
	if err != nil {
		t.Fatal(err)
	}
	other, err := f.s.CreateWebhook(ctx, f.a, WebhookInput{URL: "https://other.test/hook", Events: []string{"entry.matched"}}, "other-hook")
	if err != nil {
		t.Fatal(err)
	}
	rule, err := f.s.CreateRule(ctx, f.a, Rule{Name: "Funding watch", Enabled: true, Predicate: RulePredicate{Type: RuleKeyword, Terms: []string{"funding"}}, Actions: []RuleAction{{Type: RuleNotify, Budget: 1}, {Type: RuleWebhook, WebhookID: hook.ID}}}, "create-rule")
	if err != nil {
		t.Fatal(err)
	}
	if err := f.s.executeRuleJob(ctx, f.a.UserID, rule.ID, f.feed, []int64{first, second}); err != nil {
		t.Fatal(err)
	}
	if err := f.s.executeRuleJob(ctx, f.a.UserID, rule.ID, f.feed, []int64{first, second}); err != nil {
		t.Fatal(err)
	}
	var matches int
	if err := f.db.Pool().QueryRow(ctx, `SELECT match_count FROM rules WHERE id=$1`, rule.ID).Scan(&matches); err != nil {
		t.Fatal(err)
	}
	if matches != 2 {
		t.Fatalf("match count after retry = %d", matches)
	}
	var count int
	var why, category string
	if err := f.db.Pool().QueryRow(ctx, `SELECT count(*),min(why::text),min(category) FROM notifications WHERE recipient_user_id=$1`, f.a.UserID).Scan(&count, &why, &category); err != nil {
		t.Fatal(err)
	}
	if count != 1 || !strings.Contains(why, "Funding watch") || !strings.Contains(why, "funding") || category != "digest" {
		t.Fatalf("notifications: count=%d why=%s category=%s", count, why, category)
	}
	rows, err := f.db.Pool().Query(ctx, `SELECT payload->>'body', (payload->>'webhook_id')::bigint FROM jobs WHERE kind='webhook'`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	count = 0
	for rows.Next() {
		var body string
		var id int64
		if err := rows.Scan(&body, &id); err != nil {
			t.Fatal(err)
		}
		var event struct {
			Type string         `json:"type"`
			Data map[string]any `json:"data"`
		}
		if err := json.Unmarshal([]byte(body), &event); err != nil {
			t.Fatal(err)
		}
		if id != hook.ID || id == other.ID || event.Type != "entry.matched" || len(event.Data) != 2 || event.Data["feed_id"] != float64(f.feed) || event.Data["entry_id"] == nil {
			t.Fatalf("webhook: %d %s", id, body)
		}
		count++
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if count != 2 {
		t.Fatalf("webhook jobs = %d", count)
	}
}
