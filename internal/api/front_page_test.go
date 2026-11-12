package api

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/L-K-M/Earnest/internal/store/storetest"
)

func TestFrontPageContractAndAuthentication(t *testing.T) {
	f := newAPIFixture(t, storetest.NewTemplate(t))
	requireStatus(t, f.do(t, http.MethodGet, "/v1/front-page", nil, requestCredentials{}), http.StatusUnauthorized)
	requireStatus(t, f.do(t, http.MethodGet, "/v1/front-page?period=monthly", nil, f.session()), http.StatusUnprocessableEntity)
	for _, period := range []string{"daily", "weekly"} {
		rec := f.do(t, http.MethodGet, "/v1/front-page?period="+period, nil, f.session())
		requireStatus(t, rec, http.StatusOK)
		var page frontPage
		if err := json.Unmarshal(rec.Body.Bytes(), &page); err != nil {
			t.Fatal(err)
		}
		if string(page.Period) != period || page.Revision == "" || page.Sections == nil || page.SelectedAt.IsZero() {
			t.Fatalf("contract %s", rec.Body.String())
		}
		for _, section := range page.Sections {
			for _, story := range section.Stories {
				if story.Entry.ID == 0 || story.Coverage == nil || story.FeedCount < 1 {
					t.Fatalf("story %s", rec.Body.String())
				}
			}
		}
	}
}
