package api

import (
	"fmt"
	"net/http"
	"testing"

	"github.com/L-K-M/Earnest/internal/service"
	"github.com/L-K-M/Earnest/internal/store/storetest"
)

func TestHTTPWatchRules(t *testing.T) {
	template := storetest.NewTemplate(t)
	f := newAPIFixture(t, template)

	// With no engine the watch is stored and reports disabled, not a 500.
	body := watchBody{Name: "Funding watch", Description: "funding rounds", Enabled: true, CheckLLM: true, Budget: 2, CheckBudget: 5}
	rec := f.do(t, http.MethodPost, "/v1/watch-rules", body, f.session())
	requireStatus(t, rec, http.StatusOK)
	watch := decodeBody[service.WatchRule](t, rec)
	if watch.ID == 0 || watch.Name != body.Name || watch.Predicate.Type != service.RuleSemantic || watch.Predicate.Value != body.Description || len(watch.Actions) != 1 || watch.Actions[0].Type != service.RuleNotify || watch.Actions[0].Budget != body.Budget || !watch.CheckLLM || watch.CheckBudget != body.CheckBudget || watch.Status != statusDisabled {
		t.Fatalf("watch=%+v", watch)
	}

	rec = f.do(t, http.MethodGet, "/v1/watch-rules", nil, f.session())
	requireStatus(t, rec, http.StatusOK)
	watches := decodeBody[[]service.WatchRule](t, rec)
	if len(watches) != 1 || watches[0].ID != watch.ID || watches[0].Status != statusDisabled {
		t.Fatal(watches)
	}

	requireStatus(t, f.do(t, http.MethodDelete, fmt.Sprintf("/v1/watch-rules/%d", watch.ID), nil, f.session()), http.StatusOK)
	rec = f.do(t, http.MethodGet, "/v1/watch-rules", nil, f.session())
	requireStatus(t, rec, http.StatusOK)
	if watches = decodeBody[[]service.WatchRule](t, rec); len(watches) != 0 {
		t.Fatal(watches)
	}
}
