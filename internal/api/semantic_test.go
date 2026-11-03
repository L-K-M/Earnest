package api

import (
	"fmt"
	"net/http"
	"testing"

	"github.com/L-K-M/Earnest/internal/service"
	"github.com/L-K-M/Earnest/internal/store/storetest"
)

func TestHTTPPipelineStatusAndSemantic(t *testing.T) {
	template := storetest.NewTemplate(t)
	t.Run("no engine reports disabled", func(t *testing.T) {
		f := newAPIFixture(t, template)
		rec := f.do(t, http.MethodGet, "/v1/pipeline", nil, f.session())
		requireStatus(t, rec, http.StatusOK)
		status := decodeBody[service.PipelineStatus](t, rec)
		for task, value := range map[string]string{"embed": status.Embed, "classify": status.Classify, "semantic": status.Semantic, "watch": status.Watch, "blurb": status.Blurb, "summary": status.Summary, "edition": status.Edition, "ask": status.Ask, "compile_rule": status.CompileRule} {
			if value != statusDisabled {
				t.Fatalf("%s status=%q", task, value)
			}
		}
	})
	t.Run("semantic paths answer in-band, never 500", func(t *testing.T) {
		f := newAPIFixture(t, template)
		for _, path := range []string{"/v1/search?q=stored&mode=hybrid", fmt.Sprintf("/v1/entries/%d/similar", f.entryID)} {
			rec := f.do(t, http.MethodGet, path, nil, f.session())
			requireStatus(t, rec, http.StatusOK)
			page := decodeBody[searchPage](t, rec)
			if page.Status != statusDisabled || page.Matches == nil || len(page.Matches) != 0 {
				t.Fatalf("%s: %+v", path, page)
			}
		}
		// Full-text search does not involve the pipeline and carries no status.
		rec := f.do(t, http.MethodGet, "/v1/search?q=stored", nil, f.session())
		requireStatus(t, rec, http.StatusOK)
		page := decodeBody[searchPage](t, rec)
		if page.Status != "" || len(page.Matches) != 1 {
			t.Fatal(page)
		}
		rec = f.do(t, http.MethodGet, "/v1/search?q=stored&mode=bogus", nil, f.session())
		if rec.Code != http.StatusBadRequest && rec.Code != http.StatusUnprocessableEntity {
			t.Fatalf("invalid mode status %d", rec.Code)
		}
	})
	t.Run("cluster groups annotate every entry", func(t *testing.T) {
		f := newAPIFixture(t, template)
		rec := f.do(t, http.MethodGet, "/v1/entries?view=all&group=cluster", nil, f.session())
		requireStatus(t, rec, http.StatusOK)
		page := decodeBody[entriesPage](t, rec)
		if len(page.Entries) != 1 || len(page.Groups) != 1 || page.Groups[0].Entry.ID != f.entryID || page.Groups[0].ClusterID != 0 || page.Groups[0].AlsoIn != 0 {
			t.Fatal(page)
		}
	})
}

func TestHTTPEntriesIncludeCachedBlurb(t *testing.T) {
	f := newAPIFixture(t, storetest.NewTemplate(t))
	var principal int64
	if err := f.db.Pool().QueryRow(t.Context(), `INSERT INTO principals(user_id,kind,name) VALUES($1,'pipeline','Summaries') RETURNING id`, f.owner.UserID).Scan(&principal); err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.Pool().Exec(t.Context(), `INSERT INTO annotations(user_id,principal_id,kind,value,entry_ids,rev,content_hash) SELECT $1,$2,'blurb','Stored plain text',ARRAY[id],1,content_hash FROM entries WHERE id=$3`, f.owner.UserID, principal, f.entryID); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"/v1/entries?view=all", "/v1/entries?view=all&group=cluster", fmt.Sprintf("/v1/entries?ids=%d", f.entryID)} {
		rec := f.do(t, http.MethodGet, path, nil, f.session())
		requireStatus(t, rec, http.StatusOK)
		page := decodeBody[entriesPage](t, rec)
		if len(page.Entries) != 1 || page.Entries[0].Blurb == nil || *page.Entries[0].Blurb != "Stored plain text" {
			t.Fatalf("%s: %+v", path, page)
		}
	}
}
