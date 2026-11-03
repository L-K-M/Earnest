package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/L-K-M/Earnest/internal/store"
	"github.com/jackc/pgx/v5"
)

// SendRuleNotification accepts a notification on behalf of a rule's pipeline.
func (s *Service) SendRuleNotification(ctx context.Context, user, ruleID, entryID int64, _ string, budget int) error {
	return s.store.WithTx(ctx, func(tx pgx.Tx) error {
		if err := store.LockCore(ctx, tx); err != nil {
			return err
		}
		var rule Rule
		var predicate []byte
		if err := tx.QueryRow(ctx, `SELECT source,ast FROM rules WHERE user_id=$1 AND id=$2`, user, ruleID).Scan(&rule.Name, &predicate); err != nil {
			return domainError(err)
		}
		rule.ID = ruleID
		if err := json.Unmarshal(predicate, &rule.Predicate); err != nil {
			return fmt.Errorf("decode rule: %w", err)
		}
		entries, err := s.ruleEntries(ctx, user, time.Time{}, []int64{entryID})
		if err != nil {
			return err
		}
		if len(entries) == 0 {
			return ErrNotFound
		}
		var principal int64
		if err := tx.QueryRow(ctx, `INSERT INTO principals(user_id,kind,name) VALUES($1,'pipeline','Rules') ON CONFLICT (user_id,name) WHERE kind='pipeline' AND name='Rules' DO UPDATE SET name=EXCLUDED.name RETURNING id`, user).Scan(&principal); err != nil {
			return fmt.Errorf("rule actor: %w", err)
		}
		return s.ruleNotificationTx(ctx, tx, Actor{UserID: user, PrincipalID: principal, Kind: PrincipalPipeline, Via: CredentialInternal}, rule, entries[0], budget)
	})
}

// EmitEntryMatched enqueues an IDs-only event for the selected webhook.
func (s *Service) EmitEntryMatched(ctx context.Context, user, webhookID, feedID, entryID int64) error {
	return s.store.WithTx(ctx, func(tx pgx.Tx) error { return s.ruleWebhookTx(ctx, tx, user, webhookID, feedID, entryID) })
}

func ruleMatchedText(p RulePredicate, e ruleEntry) string {
	if p.Type == RuleSemantic && e.SemanticMatches[p.Value] {
		return passage(e.Body)
	}
	if p.Type == RuleKeyword {
		for _, term := range p.Terms {
			if strings.Contains(strings.ToLower(e.Title), strings.ToLower(term)) || strings.Contains(strings.ToLower(e.Body), strings.ToLower(term)) {
				return term
			}
		}
	}
	for _, child := range p.Children {
		if matchesRule(child, e) {
			if text := ruleMatchedText(child, e); text != "" {
				return text
			}
		}
	}
	if p.Type == RuleAuthor {
		return e.Author
	}
	return ""
}

func (s *Service) ruleNotificationTx(ctx context.Context, tx pgx.Tx, actor Actor, rule Rule, entry ruleEntry, limit int) error {
	if limit == 0 {
		limit = DefaultRuleNotificationBudget
	}
	op := fmt.Sprintf("rule/%d/%d/notify", rule.ID, entry.ID)
	var exists bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM notifications WHERE sender_principal_id=$1 AND op_id=$2)`, actor.PrincipalID, op).Scan(&exists); err != nil {
		return fmt.Errorf("check rule notification: %w", err)
	}
	if exists {
		return nil
	}
	settings, err := s.pushSettingsTx(ctx, tx, actor.UserID)
	if err != nil {
		return err
	}
	loc, err := time.LoadLocation(settings.Timezone)
	if err != nil {
		return fmt.Errorf("push timezone: %w", err)
	}
	now := s.now().UTC()
	local := now.In(loc)
	start := time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, loc)
	budget := NotificationBudget{LimitToday: limit}
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM notifications WHERE recipient_user_id=$1 AND sender_principal_id=$2 AND why->>'matching_rule_id'=$3 AND created_at >= $4 AND created_at < $5`, actor.UserID, actor.PrincipalID, fmt.Sprint(rule.ID), start, start.AddDate(0, 0, 1)).Scan(&budget.UsedToday); err != nil {
		return fmt.Errorf("rule notification budget: %w", err)
	}
	if budget.UsedToday >= budget.LimitToday {
		return nil
	}
	budget.UsedToday++
	var cadence string
	if err := tx.QueryRow(ctx, `SELECT cadence FROM subscriptions WHERE user_id=$1 AND feed_id=$2 AND deleted_at IS NULL`, actor.UserID, entry.FeedID).Scan(&cadence); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		return fmt.Errorf("rule subscription: %w", err)
	}
	category := NotificationDigest
	if cadence == "now" {
		category = NotificationAlert
	}
	matched := ruleMatchedText(rule.Predicate, entry)
	if matched == "" {
		matched = entry.Title
	}
	runes := []rune(matched)
	if len(runes) > 200 {
		matched = string(runes[:200])
	}
	why := NotificationWhy{Sender: fmt.Sprintf("Rules: %s; matched: %s", rule.Name, matched), Budget: budget, MatchingRuleID: rule.ID}
	encoded, err := json.Marshal(why)
	if err != nil {
		return fmt.Errorf("encode notification provenance: %w", err)
	}
	current, err := stateRevision(ctx, tx, actor.UserID)
	if err != nil {
		return err
	}
	status := NotificationQueued
	var held *time.Time
	until := quietUntil(settings, now)
	var muted bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM notification_mutes WHERE user_id=$1 AND principal_id=$2)`, actor.UserID, actor.PrincipalID).Scan(&muted); err != nil {
		return fmt.Errorf("check notification mute: %w", err)
	}
	if muted {
		until = time.Date(9999, 1, 1, 0, 0, 0, 0, time.UTC)
	}
	if !until.IsZero() {
		status = NotificationHeld
		held = &until
	}
	var id int64
	err = tx.QueryRow(ctx, `INSERT INTO notifications(sender_principal_id,recipient_user_id,op_id,note,entry_ids,category,dedupe_key,status,held_until,generation,why,budget_used,budget_limit,created_at,urgent) VALUES($1,$2,$3,'',$4,$5,$3,$6,$7,$8,$9,$10,$11,$12,false) RETURNING id`, actor.PrincipalID, actor.UserID, op, []int64{entry.ID}, category, status, held, current.Generation, encoded, budget.UsedToday, budget.LimitToday, now).Scan(&id)
	if err != nil {
		return fmt.Errorf("accept rule notification: %w", err)
	}
	return s.recordActivity(ctx, tx, actor, "notification.created", map[string]any{"id": id, "rule_id": rule.ID}, nil, map[string]any{"id": id, "why": why})
}

func (s *Service) ruleWebhookTx(ctx context.Context, tx pgx.Tx, user, webhookID, feedID, entryID int64) error {
	ev := WebhookEvent{Type: WebhookEntryMatched, FeedID: feedID, EntryID: entryID}
	if err := ev.validate(); err != nil {
		return err
	}
	body, err := ev.body(s.now())
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `INSERT INTO jobs(kind,payload,unique_key,run_at)
 SELECT $1,jsonb_build_object('generation',(SELECT generation::text FROM system WHERE singleton),'webhook_id',w.id,'message_id','msg_'||replace(gen_random_uuid()::text,'-',''),'event',$2::text,'body',$3::text),'wh:'||w.id||':'||$4,$7
 FROM webhooks w WHERE w.user_id=$5 AND w.id=$6 AND w.active AND $2=ANY(w.events) ON CONFLICT(unique_key) DO NOTHING`, webhookJobKind, string(ev.Type), body, ev.dedupeKey(), user, webhookID, s.now().UTC())
	if err != nil {
		return fmt.Errorf("enqueue rule webhook: %w", err)
	}
	return nil
}

var _ NotificationSender = (*Service)(nil)
var _ WebhookEmitter = (*Service)(nil)
