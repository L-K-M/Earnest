package service

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/L-K-M/Earnest/internal/llm"
	"github.com/L-K-M/Earnest/internal/store/storetest"
)

func TestCompileRuleClosedASTDryRunAndNoArticlePayload(t *testing.T) {
	f := newCoreFixture(t, storetest.NewTemplate(t))
	id := f.ingest(t, "funding ARTICLE_CONTENT_SENTINEL")
	if _, err := f.db.Pool().Exec(context.Background(), `UPDATE feeds SET title='Chosen feed' WHERE id=$1`, f.feed); err != nil {
		t.Fatal(err)
	}
	valid := `{"predicate":{"type":"and","children":[{"type":"feed_in","feed_names":["Chosen feed"]},{"type":"keyword","terms":["funding"]}]},"actions":[{"type":"set_shelf","shelf":"done"}]}`
	p := &generatedProvider{outputs: map[string]string{"compiled_rule": valid}}
	ctx := generatedContext(t, f, p, []llm.Task{llm.TaskCompileRule}, 0)
	compiled, err := f.s.CompileRule(ctx, f.a, "Move funding from Chosen feed to done")
	if err != nil || compiled.Matched != 1 || len(compiled.EntryIDs) != 1 || compiled.EntryIDs[0] != id || len(compiled.FeedsToConfirm) != 1 || compiled.FeedsToConfirm[0].ID != f.feed {
		t.Fatalf("compile %+v %v", compiled, err)
	}
	var count int
	if err := f.db.Pool().QueryRow(ctx, `SELECT count(*) FROM rules WHERE user_id=$1`, f.a.UserID).Scan(&count); err != nil || count != 0 {
		t.Fatalf("compiler saved rule %d %v", count, err)
	}
	payload := p.payloads()[0]
	if strings.Contains(payload, "ARTICLE_CONTENT_SENTINEL") || strings.Contains(payload, `"tools"`) || !strings.Contains(payload, "untrusted_feed_names") {
		t.Fatal("compiler unsafe payload", payload)
	}
	for _, test := range []struct {
		name, output string
		want         error
	}{
		{"unknown AST", `{"predicate":{"type":"eval","value":"true"},"actions":[{"type":"notify","budget":3}]}`, llm.ErrInvalidOutput},
		{"extra field", `{"predicate":{"type":"keyword","terms":["funding"],"script":"go"},"actions":[{"type":"notify","budget":3}]}`, llm.ErrInvalidOutput},
		{"unscoped shelf", `{"predicate":{"type":"keyword","terms":["funding"]},"actions":[{"type":"set_shelf","shelf":"done"}]}`, ErrInvalid},
		{"broad OR", `{"predicate":{"type":"or","children":[{"type":"feed_in","feed_names":["Chosen feed"]},{"type":"keyword","terms":["funding"]}]},"actions":[{"type":"set_shelf","shelf":"done"}]}`, ErrInvalid},
		{"model webhook", `{"predicate":{"type":"classifier_in","terms":["news"]},"actions":[{"type":"webhook","webhook_id":1}]}`, ErrInvalid},
		{"unknown feed", `{"predicate":{"type":"feed_in","feed_names":["Other feed"]},"actions":[{"type":"set_shelf","shelf":"done"}]}`, ErrInvalid},
		{"agent label", `{"predicate":{"type":"classifier_in","terms":["agent:urgent"]},"actions":[{"type":"notify","budget":3}]}`, ErrInvalid},
	} {
		t.Run(test.name, func(t *testing.T) {
			p.set("compiled_rule", test.output)
			if _, err := f.s.CompileRule(ctx, f.a, "Move funding from Chosen feed to done"); !errors.Is(err, test.want) {
				t.Fatalf("want %v got %v", test.want, err)
			}
		})
	}
	old := len(p.payloads())
	agent := f.a
	agent.Kind = PrincipalAgent
	if _, err := f.s.CompileRule(ctx, agent, "Notify funding"); !errors.Is(err, ErrForbidden) || len(p.payloads()) != old {
		t.Fatal("agent compiler access", err)
	}
}

func TestCompiledWebhookKeywordsAllComeFromHuman(t *testing.T) {
	for _, test := range []struct {
		name      string
		predicate RulePredicate
		want      bool
	}{
		{"literal", RulePredicate{Type: RuleKeyword, Terms: []string{"funding"}}, true},
		{"extra AND term", RulePredicate{Type: RuleAnd, Children: []RulePredicate{{Type: RuleKeyword, Terms: []string{"funding"}}, {Type: RuleKeyword, Terms: []string{"publisher invented"}}}}, false},
		{"negative term", RulePredicate{Type: RuleNot, Children: []RulePredicate{{Type: RuleKeyword, Terms: []string{"publisher invented"}}}}, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := compiledLiteralKeywords(test.predicate, "Notify funding from chosen feed"); got != test.want {
				t.Fatalf("got %v want %v", got, test.want)
			}
		})
	}
}
