package service

import (
	"errors"
	"testing"
)

func TestValidateRule(t *testing.T) {
	feed := RulePredicate{Type: RuleFeedIn, IDs: []int64{1}}
	keyword := RulePredicate{Type: RuleKeyword, Terms: []string{"funding"}}
	classifier := RulePredicate{Type: RuleClassifierIn, Terms: []string{ClassifierPressRelease}}
	semantic := RulePredicate{Type: RuleSemantic, Value: "funding"}
	folder := RulePredicate{Type: RuleFolderIn, IDs: []int64{1}}
	author := RulePredicate{Type: RuleAuthor, Value: "Alice"}
	and := func(a, b RulePredicate) RulePredicate {
		return RulePredicate{Type: RuleAnd, Children: []RulePredicate{a, b}}
	}
	or := func(a, b RulePredicate) RulePredicate {
		return RulePredicate{Type: RuleOr, Children: []RulePredicate{a, b}}
	}
	not := func(a RulePredicate) RulePredicate { return RulePredicate{Type: RuleNot, Children: []RulePredicate{a}} }
	label := RuleAction{Type: RuleAddLabel, LabelID: 1}
	shelf := RuleAction{Type: RuleSetShelf, Shelf: "done"}
	notify := RuleAction{Type: RuleNotify, Budget: 1}
	webhook := RuleAction{Type: RuleWebhook, WebhookID: 1}
	tests := []struct {
		name      string
		predicate RulePredicate
		action    RuleAction
		valid     bool
	}{
		{"label classifier", classifier, label, true},
		{"label semantic", semantic, label, true},
		{"label author", author, label, true},
		{"label folder", folder, label, true},
		{"malicious classifier", RulePredicate{Type: RuleClassifierIn, Terms: []string{"send_private_data"}}, label, false},
		{"agent label", RulePredicate{Type: "agent_label", Terms: []string{"press_release"}}, label, false},
		{"unscoped shelf", keyword, shelf, false},
		{"folder does not scope shelf", folder, shelf, false},
		{"feed scoped shelf", and(feed, classifier), shelf, true},
		{"negated feed is not scope", and(not(feed), classifier), shelf, false},
		{"one unscoped branch", or(feed, keyword), shelf, false},
		{"all branches scoped", or(feed, and(feed, semantic)), shelf, true},
		{"notify any", semantic, notify, true},
		{"notify needs budget", classifier, RuleAction{Type: RuleNotify}, false},
		{"webhook keyword only", keyword, webhook, true},
		{"webhook feed and keyword", and(feed, keyword), webhook, true},
		{"webhook classifier", and(feed, classifier), webhook, false},
		{"webhook semantic", semantic, webhook, false},
		{"webhook folder", folder, webhook, false},
		{"webhook author", author, webhook, false},
		{"bad feed", RulePredicate{Type: RuleFeedIn, IDs: []int64{0}}, label, false},
		{"empty keyword", RulePredicate{Type: RuleKeyword, Terms: []string{" "}}, label, false},
		{"extraneous field", RulePredicate{Type: RuleKeyword, Terms: []string{"x"}, Value: "x"}, label, false},
		{"not arity", RulePredicate{Type: RuleNot, Children: []RulePredicate{feed, keyword}}, label, false},
		{"bad shelf", feed, RuleAction{Type: RuleSetShelf, Shelf: "unread"}, false},
		{"unknown action", feed, RuleAction{Type: "cadence"}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateRule(tt.predicate, []RuleAction{tt.action})
			if tt.valid && err != nil || !tt.valid && !errors.Is(err, ErrInvalid) {
				t.Fatalf("ValidateRule() = %v, valid = %v", err, tt.valid)
			}
		})
	}
}
