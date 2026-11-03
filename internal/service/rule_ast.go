package service

import (
	"fmt"
	"github.com/L-K-M/Earnest/internal/pipeline"
	"strings"
)

// RulePredicateType names a node in the closed rule language.
type RulePredicateType string

const (
	RuleFeedIn       RulePredicateType = "feed_in"
	RuleFolderIn     RulePredicateType = "folder_in"
	RuleKeyword      RulePredicateType = "keyword"
	RuleAuthor       RulePredicateType = "author"
	RuleClassifierIn RulePredicateType = "classifier_in"
	RuleSemantic     RulePredicateType = "semantic"
	RuleAnd          RulePredicateType = "and"
	RuleOr           RulePredicateType = "or"
	RuleNot          RulePredicateType = "not"
)

// RulePredicate is one node. Only the fields belonging to Type may be set.
type RulePredicate struct {
	Type     RulePredicateType `json:"type"`
	IDs      []int64           `json:"ids,omitempty"`
	Terms    []string          `json:"terms,omitempty"`
	Value    string            `json:"value,omitempty"`
	Children []RulePredicate   `json:"children,omitempty"`
}

// ClassifierPressRelease remains the original rule vocabulary spelling.
// The pipeline owns the full closed vocabulary.
const ClassifierPressRelease = pipeline.LabelPressRelease

// RuleActionType identifies a permitted effect of a rule.
type RuleActionType string

const (
	RuleAddLabel RuleActionType = "add_label"
	RuleSetShelf RuleActionType = "set_shelf"
	RuleNotify   RuleActionType = "notify"
	RuleWebhook  RuleActionType = "webhook"
)

// RuleAction holds one effect. LabelID references a native user label.
type RuleAction struct {
	Type      RuleActionType `json:"type"`
	LabelID   int64          `json:"label_id,omitempty"`
	Shelf     string         `json:"shelf,omitempty"`
	Budget    int            `json:"budget,omitempty"`
	WebhookID int64          `json:"webhook_id,omitempty"`
}

const maxRuleNodes = 256

// DefaultRuleNotificationBudget limits notifications per rule per day.
const DefaultRuleNotificationBudget = 3

// ValidateRule checks the closed AST and the allowed predicates per action.
// Ownership of referenced feeds, folders, labels and webhooks is checked by
// the service when a rule is saved.
func ValidateRule(predicate RulePredicate, actions []RuleAction) error {
	nodes := 0
	scoped, webhookSafe, err := validateRulePredicate(predicate, &nodes)
	if err != nil {
		return err
	}
	if len(actions) == 0 || len(actions) > maxRuleNodes {
		return fmt.Errorf("rule actions: %w", ErrInvalid)
	}
	for _, action := range actions {
		switch action.Type {
		case RuleAddLabel:
			if action.LabelID <= 0 || action.Shelf != "" || action.Budget != 0 || action.WebhookID != 0 {
				return fmt.Errorf("label action: %w", ErrInvalid)
			}
		case RuleSetShelf:
			if !scoped || (action.Shelf != "later" && action.Shelf != "done") || action.LabelID != 0 || action.Budget != 0 || action.WebhookID != 0 {
				return fmt.Errorf("shelf action requires selected feeds and a valid shelf: %w", ErrInvalid)
			}
		case RuleNotify:
			if action.Budget < 0 || action.LabelID != 0 || action.Shelf != "" || action.WebhookID != 0 {
				return fmt.Errorf("invalid notification action: %w", ErrInvalid)
			}
		case RuleWebhook:
			if !webhookSafe || action.WebhookID <= 0 || action.LabelID != 0 || action.Shelf != "" || action.Budget != 0 {
				return fmt.Errorf("webhook action requires feed or keyword predicates: %w", ErrInvalid)
			}
		default:
			return fmt.Errorf("unknown rule action: %w", ErrInvalid)
		}
	}
	return nil
}

func validateRulePredicate(p RulePredicate, nodes *int) (scoped, webhookSafe bool, err error) {
	*nodes++
	if *nodes > maxRuleNodes {
		return false, false, fmt.Errorf("rule too large: %w", ErrInvalid)
	}
	invalid := func() (bool, bool, error) {
		return false, false, fmt.Errorf("invalid %q predicate: %w", p.Type, ErrInvalid)
	}
	switch p.Type {
	case RuleFeedIn, RuleFolderIn:
		if len(p.IDs) == 0 || len(p.Terms) != 0 || p.Value != "" || len(p.Children) != 0 {
			return invalid()
		}
		for _, id := range p.IDs {
			if id <= 0 {
				return invalid()
			}
		}
		return p.Type == RuleFeedIn, p.Type == RuleFeedIn, nil
	case RuleKeyword, RuleClassifierIn:
		if len(p.Terms) == 0 || len(p.IDs) != 0 || p.Value != "" || len(p.Children) != 0 {
			return invalid()
		}
		for _, term := range p.Terms {
			if strings.TrimSpace(term) == "" || (p.Type == RuleClassifierIn && !pipeline.ValidLabel(term)) {
				return invalid()
			}
		}
		return false, p.Type == RuleKeyword, nil
	case RuleAuthor, RuleSemantic:
		if strings.TrimSpace(p.Value) == "" || len(p.IDs) != 0 || len(p.Terms) != 0 || len(p.Children) != 0 {
			return invalid()
		}
		return false, false, nil
	case RuleAnd, RuleOr, RuleNot:
		if len(p.IDs) != 0 || len(p.Terms) != 0 || p.Value != "" || (p.Type == RuleNot && len(p.Children) != 1) || (p.Type != RuleNot && len(p.Children) < 2) {
			return invalid()
		}
		scoped = p.Type == RuleOr
		webhookSafe = true
		for _, child := range p.Children {
			childScoped, childSafe, childErr := validateRulePredicate(child, nodes)
			if childErr != nil {
				return false, false, childErr
			}
			if p.Type == RuleAnd {
				scoped = scoped || childScoped
			}
			if p.Type == RuleOr {
				scoped = scoped && childScoped
			}
			webhookSafe = webhookSafe && childSafe
		}
		return scoped, webhookSafe, nil
	default:
		return invalid()
	}
}
