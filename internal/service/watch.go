package service

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/L-K-M/Earnest/internal/llm"
	"github.com/L-K-M/Earnest/internal/pipeline"
	"github.com/L-K-M/Earnest/internal/store"
	"github.com/jackc/pgx/v5"
)

// WatchRule is a notify-only semantic rule, optionally confirmed by a model.
// Budget limits notifications; CheckBudget separately limits daily model calls.
type WatchRule struct {
	Rule
	CheckLLM    bool   `json:"check_llm"`
	CheckBudget int    `json:"check_budget"`
	Status      string `json:"status"`
}

func validateWatch(rule Rule) error {
	if rule.Predicate.Type != RuleSemantic || len(rule.Predicate.Value) > 4096 || len(rule.Actions) != 1 || rule.Actions[0].Type != RuleNotify {
		return fmt.Errorf("watch requires one semantic predicate and notify action: %w", ErrInvalid)
	}
	return ValidateRule(rule.Predicate, rule.Actions)
}

// CreateWatch saves a semantic description and notify action atomically. Its
// description is embedded lazily once per model, never once per article.
// Repeating opID returns the original watch without granting another budget.
func (s *Service) CreateWatch(ctx context.Context, a Actor, w WatchRule, opID string) (WatchRule, error) {
	if err := s.ruleHuman(ctx, a); err != nil {
		return WatchRule{}, err
	}
	if !validName(w.Name) || !validOpID(opID) {
		return WatchRule{}, ErrInvalid
	}
	if err := validateWatch(w.Rule); err != nil {
		return WatchRule{}, err
	}
	if w.CheckBudget == 0 {
		w.CheckBudget = DefaultRuleNotificationBudget
	}
	if w.CheckBudget < 1 || w.CheckBudget > 1000 {
		return WatchRule{}, ErrInvalid
	}
	predicate, err := json.Marshal(w.Predicate)
	if err != nil {
		return WatchRule{}, fmt.Errorf("encode watch: %w", err)
	}
	actions, err := json.Marshal(struct {
		Actions []RuleAction `json:"actions"`
	}{w.Actions})
	if err != nil {
		return WatchRule{}, fmt.Errorf("encode watch actions: %w", err)
	}
	w.Status = watchStatus(ctx, w.CheckLLM)
	err = s.store.WithTx(ctx, func(tx pgx.Tx) error {
		if err := store.LockCore(ctx, tx); err != nil {
			return err
		}
		replay, err := replayOp(ctx, tx, a.PrincipalID, "CreateWatch", opID)
		if err != nil {
			return err
		}
		if replay != nil {
			return json.Unmarshal(replay, &w)
		}
		if err := tx.QueryRow(ctx, `INSERT INTO rules(user_id,source,ast,action,enabled,created_by) VALUES($1,$2,$3,$4,$5,$6) RETURNING id`, a.UserID, w.Name, predicate, actions, w.Enabled, a.PrincipalID).Scan(&w.ID); err != nil {
			return fmt.Errorf("create watch: %w", err)
		}
		_, err = tx.Exec(ctx, `INSERT INTO watch_rules(rule_id,check_llm,check_budget) VALUES($1,$2,$3)`, w.ID, w.CheckLLM, w.CheckBudget)
		if err != nil {
			return fmt.Errorf("save watch policy: %w", err)
		}
		return store.FinishOp(ctx, tx, a.PrincipalID, opID, w)
	})
	if err != nil {
		return WatchRule{}, err
	}
	return w, nil
}

func watchStatus(ctx context.Context, check bool) string {
	engine := pipelineEngine(ctx)
	if !engine.Enabled(llm.TaskEmbed) || (check && !engine.Enabled(llm.TaskClassify)) {
		return "disabled"
	}
	return "enabled"
}

// Watches lists watches with the actual task status rather than implying that
// saved configuration enables a provider.
func (s *Service) Watches(ctx context.Context, a Actor) ([]WatchRule, error) {
	rules, err := s.Rules(ctx, a)
	if err != nil {
		return nil, err
	}
	rows, err := s.store.Pool().Query(ctx, `SELECT w.rule_id,w.check_llm,w.check_budget FROM watch_rules w JOIN rules r ON r.id=w.rule_id AND r.user_id=$1 ORDER BY w.rule_id`, a.UserID)
	if err != nil {
		return nil, fmt.Errorf("list watches: %w", err)
	}
	defer rows.Close()
	byID := map[int64]Rule{}
	for _, r := range rules {
		byID[r.ID] = r
	}
	out := []WatchRule{}
	for rows.Next() {
		var w WatchRule
		var id int64
		if err := rows.Scan(&id, &w.CheckLLM, &w.CheckBudget); err != nil {
			return nil, fmt.Errorf("scan watch: %w", err)
		}
		w.Rule = byID[id]
		w.Status = watchStatus(ctx, w.CheckLLM)
		out = append(out, w)
	}
	return out, rows.Err()
}

// DeleteWatch removes only a watch owned by this human.
func (s *Service) DeleteWatch(ctx context.Context, a Actor, id int64) error {
	if err := s.ruleHuman(ctx, a); err != nil {
		return err
	}
	tag, err := s.store.Pool().Exec(ctx, `DELETE FROM rules r USING watch_rules w WHERE r.id=w.rule_id AND r.user_id=$1 AND r.id=$2`, a.UserID, id)
	if err != nil {
		return fmt.Errorf("delete watch: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Service) confirmWatch(ctx context.Context, user int64, rule Rule, entry ruleEntry) (bool, error) {
	var check bool
	var budget int
	err := s.store.Pool().QueryRow(ctx, `SELECT check_llm,check_budget FROM watch_rules WHERE rule_id=$1`, rule.ID).Scan(&check, &budget)
	if errors.Is(err, pgx.ErrNoRows) {
		return true, nil
	}
	if err != nil {
		return false, fmt.Errorf("watch policy: %w", err)
	}
	if err := validateWatch(rule); err != nil {
		return false, err
	}
	if !check {
		return true, nil
	}
	if watchStatus(ctx, true) == "disabled" {
		return false, nil
	}
	// Reload publisher-only text and consent. No annotations can enter this path.
	var hash []byte
	var owner *int64
	var consent bool
	if err := s.store.Pool().QueryRow(ctx, `SELECT e.content_hash,f.owner_user_id,(f.owner_user_id IS NULL OR f.llm_allowed) FROM entries e JOIN feeds f ON f.id=e.feed_id WHERE e.id=$1 AND e.content_hash=$2 AND e.title=$3`, entry.ID, entry.ContentHash, entry.Title).Scan(&hash, &owner, &consent); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return false, ErrPipelinePaused
		}
		return false, fmt.Errorf("watch source: %w", err)
	}
	if !consent {
		return false, nil
	}
	content := pipeline.Content{OwnerUserID: owner, Title: entry.Title, Text: entry.Body}
	// Changing a description or bounded title invalidates an earlier confirmation.
	key := sha256.Sum256([]byte("watch-v1\n" + rule.Predicate.Value + "\n" + content.Input()))
	hash = key[:]
	engine := pipelineEngine(ctx)
	model, _ := engine.Model(llm.TaskClassify)
	call, cached, err := s.store.WatchCheckReserve(ctx, rule.ID, entry.ID, hash, model, budget, s.now().UTC())
	if err != nil {
		return false, err
	}
	if cached != nil {
		return *cached, nil
	}
	if !call {
		return false, ErrPipelinePaused
	}
	matched, err := engine.CheckWatch(ctx, rule.Predicate.Value, content)
	if errors.Is(err, llm.ErrBudgetExhausted) || errors.Is(err, llm.ErrDisabled) {
		// The client refused before calling a provider, so this is safe to resume.
		if _, deleteErr := s.store.Pool().Exec(ctx, `DELETE FROM watch_checks WHERE rule_id=$1 AND entry_id=$2 AND content_hash=$3 AND model=$4 AND matched IS NULL`, rule.ID, entry.ID, hash, model); deleteErr != nil {
			return false, fmt.Errorf("pause watch: %w", deleteErr)
		}
		return false, pipelineError(err)
	}
	if err != nil {
		return false, err
	}
	if _, err := s.store.Pool().Exec(ctx, `UPDATE watch_checks SET matched=$5 WHERE rule_id=$1 AND entry_id=$2 AND content_hash=$3 AND model=$4`, rule.ID, entry.ID, hash, model, matched); err != nil {
		return false, fmt.Errorf("save watch confirmation: %w", err)
	}
	return matched, nil
}
