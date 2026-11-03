package api

import (
	"fmt"
	"net/http"
	"testing"

	"github.com/L-K-M/Earnest/internal/service"
	"github.com/L-K-M/Earnest/internal/store/storetest"
)

func TestGeneratedPipelineDisabledProblemsAndEdition(t *testing.T) {
	f := newAPIFixture(t, storetest.NewTemplate(t))
	for _, test := range []struct {
		path string
		body any
	}{
		{fmt.Sprintf("/v1/entries/%d/summary", f.entryID), nil},
		{"/v1/rules:compile", compileBody{Text: "Notify news"}},
		{"/v1/ask", askBody{Question: "What is new?"}},
	} {
		rec := f.do(t, http.MethodPost, test.path, test.body, f.session())
		requireStatus(t, rec, http.StatusConflict)
		if rec.Header().Get("Content-Type") != "application/problem+json" {
			t.Fatal("disabled problem content type", rec.Header())
		}
	}
	if _, err := f.db.Pool().Exec(t.Context(), `UPDATE subscriptions SET cadence='week' WHERE id=$1`, f.subID); err != nil {
		t.Fatal(err)
	}
	rec := f.do(t, http.MethodPost, "/v1/editions", editionTriggerBody{Period: service.EditionWeekly, OpID: "api-edition"}, f.session())
	requireStatus(t, rec, http.StatusOK)
	requireStatus(t, f.do(t, http.MethodGet, "/v1/editions/latest", nil, f.session()), http.StatusOK)
	requireStatus(t, f.do(t, http.MethodPost, "/v1/ask", askBody{Question: "news"}, requestCredentials{}), http.StatusUnauthorized)
}
