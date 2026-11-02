package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"reflect"
	"testing"

	"github.com/L-K-M/Earnest/internal/service"
	"github.com/L-K-M/Earnest/internal/store/storetest"
)

func TestHTTPRules(t *testing.T) {
	template := storetest.NewTemplate(t)
	f := newAPIFixture(t, template)
	sub, err := f.svc.Subscription(t.Context(), f.owner, f.subID)
	if err != nil {
		t.Fatal(err)
	}
	body := ruleBody{
		Name: "Test rule", Enabled: true,
		Predicate: service.RulePredicate{Type: service.RuleFeedIn, IDs: []int64{sub.FeedID}},
		Actions:   []service.RuleAction{{Type: service.RuleSetShelf, Shelf: "done"}},
	}
	rec := f.do(t, http.MethodPost, "/v1/rules", body, f.session())
	requireStatus(t, rec, http.StatusOK)
	var created service.Rule
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	if created.ID <= 0 || created.Name != body.Name || !created.Enabled || !reflect.DeepEqual(created.Predicate, body.Predicate) || !reflect.DeepEqual(created.Actions, body.Actions) {
		t.Fatalf("created rule=%+v", created)
	}
	path := fmt.Sprintf("/v1/rules/%d", created.ID)
	rec = f.do(t, http.MethodGet, "/v1/rules", nil, f.session())
	requireStatus(t, rec, http.StatusOK)
	var listed []service.Rule
	if err := json.Unmarshal(rec.Body.Bytes(), &listed); err != nil {
		t.Fatal(err)
	}
	if len(listed) != 1 || !reflect.DeepEqual(listed[0], created) {
		t.Fatalf("listed rules=%+v, want %+v", listed, created)
	}
	getRule := func() service.Rule {
		t.Helper()
		rec := f.do(t, http.MethodGet, path, nil, f.session())
		requireStatus(t, rec, http.StatusOK)
		var rule service.Rule
		if err := json.Unmarshal(rec.Body.Bytes(), &rule); err != nil {
			t.Fatal(err)
		}
		return rule
	}
	if got := getRule(); !reflect.DeepEqual(got, created) {
		t.Fatalf("get rule=%+v, want %+v", got, created)
	}
	name := "Renamed rule"
	rec = f.do(t, http.MethodPatch, path, rulePatch{Name: &name}, f.session())
	requireStatus(t, rec, http.StatusOK)
	created.Name = name
	var patched service.Rule
	if err := json.Unmarshal(rec.Body.Bytes(), &patched); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(patched, created) || !reflect.DeepEqual(getRule(), created) {
		t.Fatalf("patched rule=%+v, want %+v", patched, created)
	}
	for _, tc := range []struct {
		action  string
		enabled bool
	}{{"disable", false}, {"enable", true}} {
		requireStatus(t, f.do(t, http.MethodPost, path+":"+tc.action, nil, f.session()), http.StatusOK)
		created.Enabled = tc.enabled
		if got := getRule(); !reflect.DeepEqual(got, created) {
			t.Fatalf("after %s: %+v, want %+v", tc.action, got, created)
		}
	}

	// Compare persisted rule and effect tables as well as the entry state.
	snapshot := func() string {
		t.Helper()
		var result string
		err := f.db.Pool().QueryRow(t.Context(), `SELECT jsonb_build_array(
			(SELECT coalesce(jsonb_agg(to_jsonb(r) ORDER BY r.id), '[]'::jsonb) FROM rules r),
			(SELECT coalesce(jsonb_agg(to_jsonb(s) ORDER BY s.entry_id), '[]'::jsonb) FROM entry_state s),
			(SELECT count(*) FROM activity), (SELECT count(*) FROM changes),
			(SELECT count(*) FROM op_ids), (SELECT count(*) FROM entry_labels),
			(SELECT count(*) FROM notifications))::text`).Scan(&result)
		if err != nil {
			t.Fatal(err)
		}
		return result
	}
	before := snapshot()
	rec = f.do(t, http.MethodPost, "/v1/rules:dry-run", ruleDryRun{Predicate: body.Predicate, Actions: body.Actions}, f.session())
	requireStatus(t, rec, http.StatusOK)
	var matches entriesPage
	if err := json.Unmarshal(rec.Body.Bytes(), &matches); err != nil {
		t.Fatal(err)
	}
	if len(matches.Entries) != 1 || matches.Entries[0].ID != f.entryID {
		t.Fatalf("dry run matches=%+v", matches.Entries)
	}
	if after := snapshot(); before != after {
		t.Fatalf("dry run changed persisted state: before=%s after=%s", before, after)
	}

	credentials := requestCredentials{bearer: f.token(t, service.AudienceAPI)}
	for _, tc := range []struct {
		method, path string
		body         any
	}{
		{http.MethodGet, "/v1/rules", nil},
		{http.MethodPost, "/v1/rules", body},
		{http.MethodGet, path, nil},
		{http.MethodPatch, path, rulePatch{Name: &name}},
		{http.MethodPost, path + ":enable", nil},
		{http.MethodPost, path + ":disable", nil},
		{http.MethodPost, "/v1/rules:dry-run", ruleDryRun{Predicate: body.Predicate, Actions: body.Actions}},
		{http.MethodDelete, path, nil},
	} {
		t.Run(tc.method+tc.path, func(t *testing.T) {
			rec := f.do(t, tc.method, tc.path, tc.body, credentials)
			requireStatus(t, rec, http.StatusForbidden)
			assertProblem(t, rec, "/problems/forbidden")
		})
	}
	requireStatus(t, f.do(t, http.MethodDelete, path, nil, f.session()), http.StatusOK)
	requireStatus(t, f.do(t, http.MethodGet, path, nil, f.session()), http.StatusNotFound)
	rec = f.do(t, http.MethodGet, "/v1/rules", nil, f.session())
	requireStatus(t, rec, http.StatusOK)
	if err := json.Unmarshal(rec.Body.Bytes(), &listed); err != nil || len(listed) != 0 {
		t.Fatalf("rules after delete=%+v: %v", listed, err)
	}
}
