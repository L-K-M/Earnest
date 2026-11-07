package api

import (
	"context"
	"net/http"
	"strconv"
	"strings"

	"github.com/L-K-M/Earnest/internal/service"
	"github.com/danielgtaylor/huma/v2"
)

type ruleBody struct {
	OpID      string                `json:"op_id"`
	Name      string                `json:"name"`
	Predicate service.RulePredicate `json:"predicate"`
	Actions   []service.RuleAction  `json:"actions"`
	Enabled   bool                  `json:"enabled"`
}

type rulePatch struct {
	Name      *string                `json:"name,omitempty"`
	Predicate *service.RulePredicate `json:"predicate,omitempty"`
	Actions   *[]service.RuleAction  `json:"actions,omitempty"`
	Enabled   *bool                  `json:"enabled,omitempty"`
}

type ruleDryRun struct {
	Predicate service.RulePredicate `json:"predicate"`
	Actions   []service.RuleAction  `json:"actions"`
}

func (s *server) rule(ctx context.Context, id int64) (service.Rule, error) {
	rules, err := s.svc.Rules(ctx, actor(ctx))
	if err != nil {
		return service.Rule{}, err
	}
	for _, rule := range rules {
		if rule.ID == id {
			return rule, nil
		}
	}
	return service.Rule{}, service.ErrNotFound
}

func (s *server) ruleRoutes() {
	route(s, http.MethodGet, "/v1/rules", "list-rules", func(ctx context.Context, _ *empty) ([]service.Rule, error) {
		return s.svc.Rules(ctx, actor(ctx))
	})
	route(s, http.MethodPost, "/v1/rules", "create-rule", func(ctx context.Context, in *bodyInput[ruleBody]) (service.Rule, error) {
		b := in.Body
		return s.svc.CreateRule(ctx, actor(ctx), service.Rule{Name: b.Name, Predicate: b.Predicate, Actions: b.Actions, Enabled: b.Enabled}, b.OpID)
	})
	route(s, http.MethodGet, "/v1/rules/{id}", "get-rule", func(ctx context.Context, in *idInput) (service.Rule, error) {
		return s.rule(ctx, in.ID)
	})
	route(s, http.MethodPatch, "/v1/rules/{id}", "patch-rule", func(ctx context.Context, in *idBody[rulePatch]) (service.Rule, error) {
		rule, err := s.rule(ctx, in.ID)
		if err != nil {
			return service.Rule{}, err
		}
		if in.Body.Name != nil {
			rule.Name = *in.Body.Name
		}
		if in.Body.Predicate != nil {
			rule.Predicate = *in.Body.Predicate
		}
		if in.Body.Actions != nil {
			rule.Actions = *in.Body.Actions
		}
		if in.Body.Enabled != nil {
			rule.Enabled = *in.Body.Enabled
		}
		if err := s.svc.UpdateRule(ctx, actor(ctx), rule); err != nil {
			return service.Rule{}, err
		}
		return s.rule(ctx, in.ID)
	})
	route(s, http.MethodDelete, "/v1/rules/{id}", "delete-rule", func(ctx context.Context, in *idInput) (empty, error) {
		return empty{}, s.svc.DeleteRule(ctx, actor(ctx), in.ID)
	})
	// ServeMux only permits a wildcard as a whole path segment. Dispatch the
	// suffix here, while documenting the two literal operation paths below.
	route(s, http.MethodPost, "/v1/rules/{id}", "set-rule-enabled", func(ctx context.Context, in *struct {
		ID string `path:"id"`
	}) (empty, error) {
		idText, action, ok := strings.Cut(in.ID, ":")
		id, err := strconv.ParseInt(idText, 10, 64)
		if !ok || err != nil || id <= 0 || (action != "enable" && action != "disable") {
			return empty{}, service.ErrNotFound
		}
		return empty{}, s.svc.SetRuleEnabled(ctx, actor(ctx), id, action == "enable")
	})
	paths := s.api.OpenAPI().Paths
	operation := paths["/v1/rules/{id}"].Post
	paths["/v1/rules/{id}"].Post = nil
	for _, action := range []string{"enable", "disable"} {
		op := *operation
		op.OperationID = action + "-rule"
		paths["/v1/rules/{id}:"+action] = &huma.PathItem{Post: &op}
	}
	route(s, http.MethodPost, "/v1/rules:dry-run", "dry-run-rule", func(ctx context.Context, in *bodyInput[ruleDryRun]) (entriesPage, error) {
		ids, total, err := s.svc.DryRunRule(ctx, actor(ctx), in.Body.Predicate, in.Body.Actions)
		if err != nil {
			return entriesPage{}, err
		}
		result := entriesPage{Entries: []entry{}, Total: total}
		const batchSize = 200
		for len(ids) > 0 {
			n := min(len(ids), batchSize)
			entries, err := s.svc.GetByIDs(ctx, actor(ctx), ids[:n])
			if err != nil {
				return entriesPage{}, err
			}
			result.Entries = append(result.Entries, toEntries(entries)...)
			ids = ids[n:]
		}
		return result, nil
	})
}
