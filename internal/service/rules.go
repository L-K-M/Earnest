package service

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"time"
)

// Rule is a human-authored deterministic rule.
type Rule struct {
	ID            int64         `json:"id"`
	Name          string        `json:"name"`
	Predicate     RulePredicate `json:"predicate"`
	Actions       []RuleAction  `json:"actions"`
	Enabled       bool          `json:"enabled"`
	LastMatchAt   *time.Time    `json:"last_match_at,omitempty"`
	MatchCount    int64         `json:"match_count"`
	DisableReason string        `json:"disable_reason,omitempty"`
}

// NotificationSender delivers a rule notification within its configured budget.
type NotificationSender interface {
	SendRuleNotification(context.Context, int64, int64, int64, string, int) error
}

// WebhookEmitter emits an entry.matched event to a registered webhook.
type WebhookEmitter interface {
	EmitEntryMatched(context.Context, int64, int64, int64, int64) error
}

func (s *Service) ruleHuman(ctx context.Context, a Actor) error {
	if a.Kind != PrincipalHuman {
		return ErrForbidden
	}
	return s.checkActor(ctx, a, ScopeFeedsWrite)
}

// CreateRule validates and saves a human-authored rule.
func (s *Service) CreateRule(ctx context.Context, a Actor, rule Rule) (Rule, error) {
	if err := s.ruleHuman(ctx, a); err != nil {
		return Rule{}, err
	}
	if !validName(rule.Name) {
		return Rule{}, fmt.Errorf("rule name: %w", ErrInvalid)
	}
	if err := ValidateRule(rule.Predicate, rule.Actions); err != nil {
		return Rule{}, err
	}
	if err := s.checkRuleReferences(ctx, a.UserID, rule.Predicate, rule.Actions); err != nil {
		return Rule{}, err
	}
	predicate, err := json.Marshal(rule.Predicate)
	if err != nil {
		return Rule{}, fmt.Errorf("encode predicate: %w", err)
	}
	actions, err := json.Marshal(struct {
		Actions []RuleAction `json:"actions"`
	}{rule.Actions})
	if err != nil {
		return Rule{}, fmt.Errorf("encode actions: %w", err)
	}
	err = s.store.Pool().QueryRow(ctx, `INSERT INTO rules(user_id,source,ast,action,enabled,created_by) VALUES($1,$2,$3,$4,$5,$6) RETURNING id`, a.UserID, rule.Name, predicate, actions, rule.Enabled, a.PrincipalID).Scan(&rule.ID)
	if err != nil {
		return Rule{}, fmt.Errorf("create rule: %w", domainError(err))
	}
	rule.LastMatchAt = nil
	rule.MatchCount = 0
	return rule, nil
}

func (s *Service) checkRuleReferences(ctx context.Context, user int64, p RulePredicate, actions []RuleAction) error {
	var walk func(RulePredicate) error
	walk = func(p RulePredicate) error {
		for _, id := range p.IDs {
			var ok bool
			var q string
			switch p.Type {
			case RuleFeedIn:
				q = `SELECT EXISTS(SELECT 1 FROM subscriptions WHERE user_id=$1 AND feed_id=$2 AND deleted_at IS NULL)`
			case RuleFolderIn:
				q = `SELECT EXISTS(SELECT 1 FROM folders WHERE user_id=$1 AND id=$2 AND deleted_at IS NULL)`
			default:
				continue
			}
			if err := s.store.Pool().QueryRow(ctx, q, user, id).Scan(&ok); err != nil {
				return fmt.Errorf("check rule reference: %w", err)
			}
			if !ok {
				return fmt.Errorf("rule reference: %w", ErrInvalid)
			}
		}
		for _, child := range p.Children {
			if err := walk(child); err != nil {
				return err
			}
		}
		return nil
	}
	if err := walk(p); err != nil {
		return err
	}
	for _, action := range actions {
		var q string
		var id int64
		switch action.Type {
		case RuleAddLabel:
			q = `SELECT EXISTS(SELECT 1 FROM labels WHERE user_id=$1 AND id=$2 AND deleted_at IS NULL)`
			id = action.LabelID
		case RuleWebhook:
			q = `SELECT EXISTS(SELECT 1 FROM webhooks WHERE user_id=$1 AND id=$2)`
			id = action.WebhookID
		default:
			continue
		}
		var ok bool
		if err := s.store.Pool().QueryRow(ctx, q, user, id).Scan(&ok); err != nil {
			return fmt.Errorf("check rule action: %w", err)
		}
		if !ok {
			return fmt.Errorf("rule action reference: %w", ErrInvalid)
		}
	}
	return nil
}

type ruleEntry struct {
	ID, FeedID, FolderID int64
	Title, Body, Author  string
	FirstSeen            time.Time
	ClassifierLabels     []string
	SemanticMatches      map[string]bool
	ContentHash          []byte
}

func matchesRule(p RulePredicate, e ruleEntry) bool {
	switch p.Type {
	case RuleFeedIn:
		return slices.Contains(p.IDs, e.FeedID)
	case RuleFolderIn:
		return slices.Contains(p.IDs, e.FolderID)
	case RuleKeyword:
		for _, term := range p.Terms {
			if strings.Contains(strings.ToLower(e.Title), strings.ToLower(term)) || strings.Contains(strings.ToLower(e.Body), strings.ToLower(term)) {
				return true
			}
		}
		return false
	case RuleAuthor:
		return strings.EqualFold(e.Author, p.Value)
	case RuleClassifierIn:
		return classifierMatches(p.Terms, e.ClassifierLabels)
	case RuleSemantic:
		return e.SemanticMatches[p.Value]
	case RuleAnd:
		for _, c := range p.Children {
			if !matchesRule(c, e) {
				return false
			}
		}
		return true
	case RuleOr:
		for _, c := range p.Children {
			if matchesRule(c, e) {
				return true
			}
		}
		return false
	case RuleNot:
		return !matchesRule(p.Children[0], e)
	}
	return false
}

func (s *Service) ruleEntries(ctx context.Context, user int64, since time.Time, ids []int64) ([]ruleEntry, error) {
	rows, err := s.store.Pool().Query(ctx, `SELECT e.id,e.feed_id,coalesce(s.folder_id,0),e.title,e.author,e.first_seen_at,coalesce(c.text,''),e.content_hash FROM entries e JOIN subscriptions s ON s.feed_id=e.feed_id AND s.user_id=$1 AND s.deleted_at IS NULL LEFT JOIN LATERAL (SELECT text FROM contents WHERE entry_id=e.id AND content_hash=e.content_hash ORDER BY (source='extracted') DESC,sanitizer_version DESC LIMIT 1) c ON true WHERE e.deleted_at IS NULL AND e.first_seen_at >= $2 AND ($3::bigint[] IS NULL OR e.id=ANY($3)) ORDER BY e.seq DESC`, user, since, ids)
	if err != nil {
		return nil, fmt.Errorf("load rule entries: %w", err)
	}
	defer rows.Close()
	result := []ruleEntry{}
	for rows.Next() {
		var e ruleEntry
		if err := rows.Scan(&e.ID, &e.FeedID, &e.FolderID, &e.Title, &e.Author, &e.FirstSeen, &e.Body, &e.ContentHash); err != nil {
			return nil, fmt.Errorf("scan rule entry: %w", err)
		}
		result = append(result, e)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read rule entries: %w", err)
	}
	return result, nil
}

// DryRunRule returns matching entry IDs from the past seven days without writing.
func (s *Service) DryRunRule(ctx context.Context, a Actor, p RulePredicate, actions []RuleAction) ([]int64, error) {
	if err := s.ruleHuman(ctx, a); err != nil {
		return nil, err
	}
	if err := ValidateRule(p, actions); err != nil {
		return nil, err
	}
	if err := s.checkRuleReferences(ctx, a.UserID, p, actions); err != nil {
		return nil, err
	}
	entries, err := s.ruleEntries(ctx, a.UserID, s.now().UTC().Add(-7*24*time.Hour), nil)
	if err != nil {
		return nil, err
	}
	ids := []int64{}
	for _, e := range entries {
		var ready bool
		e, ready, err = s.prepareModelPredicates(ctx, a.UserID, p, e)
		if err != nil {
			if pipelineWaiting(err) {
				continue
			}
			return nil, err
		}
		if !ready {
			continue
		}
		if matchesRule(p, e) {
			ids = append(ids, e.ID)
		}
	}
	return ids, nil
}

// Rules lists the user's rules in creation order.
func (s *Service) Rules(ctx context.Context, a Actor) ([]Rule, error) {
	if err := s.ruleHuman(ctx, a); err != nil {
		return nil, err
	}
	rows, err := s.store.Pool().Query(ctx, `SELECT id,source,ast,action,enabled,last_match_at,match_count FROM rules WHERE user_id=$1 ORDER BY id`, a.UserID)
	if err != nil {
		return nil, fmt.Errorf("list rules: %w", err)
	}
	defer rows.Close()
	out := []Rule{}
	for rows.Next() {
		var rule Rule
		var predicate, actions []byte
		if err := rows.Scan(&rule.ID, &rule.Name, &predicate, &actions, &rule.Enabled, &rule.LastMatchAt, &rule.MatchCount); err != nil {
			return nil, fmt.Errorf("scan rule: %w", err)
		}
		if err := json.Unmarshal(predicate, &rule.Predicate); err != nil {
			return nil, fmt.Errorf("decode predicate: %w", err)
		}
		var wrapper struct {
			Actions       []RuleAction `json:"actions"`
			DisableReason string       `json:"disable_reason"`
		}
		if err := json.Unmarshal(actions, &wrapper); err != nil {
			return nil, fmt.Errorf("decode actions: %w", err)
		}
		rule.Actions = wrapper.Actions
		rule.DisableReason = wrapper.DisableReason
		out = append(out, rule)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list rules: %w", err)
	}
	return out, nil
}

// UpdateRule replaces a rule's name, predicate, actions and enabled flag.
func (s *Service) UpdateRule(ctx context.Context, a Actor, rule Rule) error {
	if err := s.ruleHuman(ctx, a); err != nil {
		return err
	}
	if rule.ID <= 0 || !validName(rule.Name) {
		return ErrInvalid
	}
	if err := ValidateRule(rule.Predicate, rule.Actions); err != nil {
		return err
	}
	if err := s.checkRuleReferences(ctx, a.UserID, rule.Predicate, rule.Actions); err != nil {
		return err
	}
	var watch bool
	if err := s.store.Pool().QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM watch_rules w JOIN rules r ON r.id=w.rule_id WHERE r.user_id=$1 AND r.id=$2)`, a.UserID, rule.ID).Scan(&watch); err != nil {
		return err
	}
	if watch {
		if err := validateWatch(rule); err != nil {
			return err
		}
	}
	predicate, err := json.Marshal(rule.Predicate)
	if err != nil {
		return fmt.Errorf("encode predicate: %w", err)
	}
	actions, err := json.Marshal(struct {
		Actions []RuleAction `json:"actions"`
	}{rule.Actions})
	if err != nil {
		return fmt.Errorf("encode actions: %w", err)
	}
	tag, err := s.store.Pool().Exec(ctx, `UPDATE rules SET source=$3,ast=$4,action=$5,enabled=$6 WHERE user_id=$1 AND id=$2`, a.UserID, rule.ID, rule.Name, predicate, actions, rule.Enabled)
	if err != nil {
		return fmt.Errorf("update rule: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// SetRuleEnabled enables or disables a rule.
func (s *Service) SetRuleEnabled(ctx context.Context, a Actor, id int64, enabled bool) error {
	if err := s.ruleHuman(ctx, a); err != nil {
		return err
	}
	if id <= 0 {
		return ErrInvalid
	}
	tag, err := s.store.Pool().Exec(ctx, `UPDATE rules SET enabled=$3, action=CASE WHEN $3 THEN action - 'disable_reason' ELSE action END WHERE user_id=$1 AND id=$2`, a.UserID, id, enabled)
	if err != nil {
		return fmt.Errorf("set rule enabled: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// DeleteRule removes a rule.
func (s *Service) DeleteRule(ctx context.Context, a Actor, id int64) error {
	if err := s.ruleHuman(ctx, a); err != nil {
		return err
	}
	if id <= 0 {
		return ErrInvalid
	}
	tag, err := s.store.Pool().Exec(ctx, `DELETE FROM rules WHERE user_id=$1 AND id=$2`, a.UserID, id)
	if err != nil {
		return fmt.Errorf("delete rule: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}
