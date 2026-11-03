package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"time"

	"github.com/L-K-M/Earnest/internal/store"
	"github.com/L-K-M/Earnest/internal/syncmodel"
	"github.com/jackc/pgx/v5"
)

var errRulePredicateChanged = errors.New("rule predicate changed during preparation")

// executeRuleJob isolates failures to one rule, so other rules can still run.
func (s *Service) executeRuleJob(ctx context.Context, userID, ruleID, feedID int64, ids []int64, generation ...string) error {
	if len(generation) > 0 {
		current, err := s.store.Generation(ctx)
		if err != nil {
			return err
		}
		if generation[0] != current {
			return nil
		}
	}
	var a Actor
	a.UserID, a.Kind, a.Via, a.Scopes = userID, PrincipalHuman, CredentialInternal, HumanScopes
	err := s.store.Pool().QueryRow(ctx, `SELECT id FROM principals WHERE user_id=$1 AND kind='human' ORDER BY id LIMIT 1`, userID).Scan(&a.PrincipalID)
	if err == pgx.ErrNoRows {
		return nil
	}
	if err != nil {
		return fmt.Errorf("rule owner: %w", err)
	}
	rules, err := s.Rules(ctx, a)
	if err != nil {
		return err
	}
	for _, rule := range rules {
		if rule.ID != ruleID || !rule.Enabled {
			continue
		}
		if err := ValidateRule(rule.Predicate, rule.Actions); err != nil {
			return err
		}
		entries, err := s.ruleEntries(ctx, userID, time.Time{}, ids)
		if err != nil {
			return err
		}
		for _, entry := range entries {
			var ready bool
			entry, ready, err = s.prepareModelPredicates(ctx, userID, rule.Predicate, entry)
			if err != nil {
				return err
			}
			if !ready {
				continue
			}
			if entry.FeedID == feedID && matchesRule(rule.Predicate, entry) {
				confirmed, err := s.confirmWatch(ctx, userID, rule, entry)
				if err != nil {
					return err
				}
				if !confirmed {
					continue
				}
				if err := s.applyRule(ctx, a, rule, entry, generation...); err != nil {
					return fmt.Errorf("rule %d: %w", rule.ID, err)
				}
			}
		}
		break
	}
	return nil
}

func (s *Service) applyRule(ctx context.Context, human Actor, rule Rule, entry ruleEntry, generation ...string) error {
	for {
		err := s.applyPreparedRule(ctx, human, &rule, entry, generation...)
		if !errors.Is(err, errRulePredicateChanged) {
			return err
		}

		var ready bool
		entry, ready, err = s.prepareModelPredicates(ctx, human.UserID, rule.Predicate, entry)
		if err != nil || !ready {
			return err
		}
		if !matchesRule(rule.Predicate, entry) {
			return nil
		}
		confirmed, err := s.confirmWatch(ctx, human.UserID, rule, entry)
		if err != nil || !confirmed {
			return err
		}
	}
}

func (s *Service) applyPreparedRule(ctx context.Context, human Actor, rule *Rule, entry ruleEntry, generation ...string) error {
	if err := ValidateRule(rule.Predicate, rule.Actions); err != nil {
		return err
	}
	preparedPredicate := rule.Predicate
	return s.withEvents(ctx, func(tx pgx.Tx) error {
		if err := store.LockCore(ctx, tx); err != nil {
			return err
		}
		if len(generation) > 0 {
			current, err := stateRevision(ctx, tx, human.UserID)
			if err != nil {
				return err
			}
			if generation[0] != current.Generation {
				return nil
			}
		}
		var enabled bool
		var predicate, actions []byte
		err := tx.QueryRow(ctx, `SELECT enabled,source,ast,action FROM rules WHERE id=$1 AND user_id=$2 FOR UPDATE`, rule.ID, human.UserID).Scan(&enabled, &rule.Name, &predicate, &actions)
		if err == pgx.ErrNoRows {
			return nil
		}
		if err != nil {
			return fmt.Errorf("load rule: %w", err)
		}
		if !enabled {
			return nil
		}
		rule.Predicate = RulePredicate{}
		if err := json.Unmarshal(predicate, &rule.Predicate); err != nil {
			return fmt.Errorf("decode rule predicate: %w", err)
		}
		var wrapper struct {
			Actions []RuleAction `json:"actions"`
		}
		if err := json.Unmarshal(actions, &wrapper); err != nil {
			return fmt.Errorf("decode rule actions: %w", err)
		}
		rule.Actions = wrapper.Actions
		if err := ValidateRule(rule.Predicate, rule.Actions); err != nil {
			return err
		}
		// Model data belongs to the predicate prepared outside this transaction.
		if !reflect.DeepEqual(preparedPredicate, rule.Predicate) {
			return errRulePredicateChanged
		}
		if !matchesRule(rule.Predicate, entry) {
			return nil
		}
		var principal int64
		err = tx.QueryRow(ctx, `INSERT INTO principals(user_id,kind,name) VALUES($1,'pipeline','Rules') ON CONFLICT (user_id,name) WHERE kind='pipeline' AND name='Rules' DO UPDATE SET name=EXCLUDED.name RETURNING id`, human.UserID).Scan(&principal)
		if err != nil {
			return fmt.Errorf("rule actor: %w", err)
		}
		actor := Actor{UserID: human.UserID, PrincipalID: principal, Kind: PrincipalPipeline, Via: CredentialInternal, Scopes: []Scope{ScopeStateWrite}}
		matchOp := fmt.Sprintf("rule/%d/%d/match", rule.ID, entry.ID)
		replay, err := replayOp(ctx, tx, principal, "ApplyRule", matchOp)
		if err != nil {
			return err
		}
		if replay != nil {
			return nil
		}
		now := s.now().UTC()
		for i, action := range rule.Actions {
			op := fmt.Sprintf("rule/%d/%d/%d", rule.ID, entry.ID, i)
			switch action.Type {
			case RuleSetShelf:
				current, err := stateRevision(ctx, tx, human.UserID)
				if err != nil {
					return err
				}
				state, err := store.LoadState(ctx, tx, human.PrincipalID, entry.ID)
				if err != nil {
					return err
				}
				// A deliberate unread made after ingestion wins over a delayed rule.
				if state.Seen.Value == syncmodel.Unseen && state.Seen.Source == syncmodel.SourceDeliberate && !state.Seen.OccurredAt.Before(entry.FirstSeen) {
					continue
				}
				write := syncmodel.Write{OpID: op, Principal: syncmodel.ID(human.PrincipalID), EntryID: syncmodel.ID(entry.ID), Register: syncmodel.Shelf, Value: syncmodel.Value(action.Shelf), Base: syncmodel.Revision{Generation: current.Generation}, OccurredAt: entry.FirstSeen, Kind: syncmodel.Explicit}
				results, err := s.batchTx(ctx, tx, actor, []syncmodel.Write{write}, current, now)
				if err != nil {
					return err
				}
				if len(results) > 0 && results[0].Status == syncmodel.Applied && !results[0].Replay {
					target, err := json.Marshal(map[string]any{"rule_id": rule.ID, "rule_name": rule.Name, "entry_id": entry.ID, "register": syncmodel.Shelf, "op_id": op})
					if err != nil {
						return err
					}
					before, err := json.Marshal(state)
					if err != nil {
						return err
					}
					after, err := json.Marshal(results[0].State)
					if err != nil {
						return err
					}
					if _, err = tx.Exec(ctx, `INSERT INTO activity(user_id,principal_id,action,target,before,after,revert_state,created_at) VALUES($1,$2,'state.updated',$3,$4,$5,$5,$6)`, human.UserID, principal, target, before, after, now); err != nil {
						return fmt.Errorf("record rule activity: %w", err)
					}
				}
			case RuleAddLabel:
				label, err := store.LoadLabel(ctx, tx, human.UserID, action.LabelID)
				if err != nil {
					return domainError(err)
				}
				if label.Deleted {
					return ErrNotFound
				}
				_, deleted, found, err := store.LoadMembership(ctx, tx, human.UserID, entry.ID, action.LabelID)
				if err != nil {
					return err
				}
				if found && !deleted {
					continue
				}
				rev, err := changeRev(ctx, tx, human.UserID)
				if err != nil {
					return err
				}
				if err := store.UpsertMembership(ctx, tx, human.UserID, entry.ID, action.LabelID, rev); err != nil {
					return err
				}
				membership, err := store.MembershipID(ctx, tx, human.UserID, entry.ID, action.LabelID)
				if err != nil {
					return err
				}
				obj := labelObject(actor, membership, entry.ID, label.Name, uint64(rev), false)
				if err := insertEvent(ctx, tx, human.UserID, rev, string(syncmodel.LabelAdded), map[string]any{"object": obj, "label_id": action.LabelID}, now); err != nil {
					return err
				}
				if _, err = tx.Exec(ctx, `INSERT INTO activity(user_id,principal_id,action,target,created_at) VALUES($1,$2,'label.added',$3,$4)`, human.UserID, principal, map[string]any{"rule_id": rule.ID, "rule_name": rule.Name, "entry_id": entry.ID, "label_id": action.LabelID}, now); err != nil {
					return fmt.Errorf("record rule label activity: %w", err)
				}
			case RuleNotify:
				if err := s.ruleNotificationTx(ctx, tx, actor, *rule, entry, action.Budget); err != nil {
					return err
				}
			case RuleWebhook:
				if err := s.ruleWebhookTx(ctx, tx, human.UserID, action.WebhookID, entry.FeedID, entry.ID); err != nil {
					return err
				}
			default:
				return fmt.Errorf("unknown rule action: %w", ErrInvalid)
			}
		}
		_, err = tx.Exec(ctx, `UPDATE rules SET last_match_at=$3,match_count=match_count+1 WHERE id=$1 AND user_id=$2`, rule.ID, human.UserID, now)
		if err != nil {
			return fmt.Errorf("record rule match: %w", err)
		}
		return store.FinishOp(ctx, tx, principal, matchOp, map[string]any{"rule_id": rule.ID, "entry_id": entry.ID})
	})
}
