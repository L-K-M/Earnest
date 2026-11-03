package api

import (
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/L-K-M/Earnest/internal/service"
	"github.com/L-K-M/Earnest/internal/store/storetest"
)

func TestHTTPSearch(t *testing.T) {
	template := storetest.NewTemplate(t)
	f := newAPIFixture(t, template)
	agent, _ := surfaceAgent(t, f)
	requireStatus(t, f.do(t, http.MethodPut, fmt.Sprintf("/v1/entries/%d/annotations/note", f.entryID), annotationBody{Value: "Stored content: agent observation", OpID: "search-note"}, agent), http.StatusOK)
	rec := f.do(t, http.MethodGet, "/v1/search?q=stored&limit=1", nil, agent)
	requireStatus(t, rec, http.StatusOK)
	page := decodeBody[searchPage](t, rec)
	seen := map[searchMatchKind]bool{}
	for len(page.Matches) > 0 {
		if len(page.Matches) != 1 || page.NextCursor == "" {
			t.Fatal(page)
		}
		match := page.Matches[0]
		if match.EntryID != f.entryID {
			t.Fatal(match)
		}
		if seen[match.Kind] {
			t.Fatal("pagination repeated match", page)
		}
		seen[match.Kind] = true
		if match.Kind == searchAgentNote && (match.AgentName != "Morning Agent" || match.AnnotationID == 0 || !match.Untrusted) {
			t.Fatal("unlabelled agent note", match)
		}
		rec = f.do(t, http.MethodGet, "/v1/search?q=stored&limit=1&cursor="+page.NextCursor, nil, agent)
		requireStatus(t, rec, http.StatusOK)
		page = decodeBody[searchPage](t, rec)
	}
	if !seen[searchEntry] || !seen[searchAgentNote] {
		t.Fatal(seen)
	}
	for _, query := range []string{"?q=stored&subscription_id=999999", "?q=stored&folder_id=999999", "?q=nonexistent"} {
		rec = f.do(t, http.MethodGet, "/v1/search"+query, nil, f.session())
		requireStatus(t, rec, http.StatusOK)
		if page := decodeBody[searchPage](t, rec); len(page.Matches) != 0 {
			t.Fatal(page)
		}
	}
	for _, query := range []string{"?q=stored&cursor=invalid", "?q=stored&cursor=bnVsbA", "?q=stored&limit=201", "?q=stored&subscription_id=-1"} {
		rec = f.do(t, http.MethodGet, "/v1/search"+query, nil, agent)
		if rec.Code != http.StatusBadRequest && rec.Code != http.StatusUnprocessableEntity {
			t.Fatalf("invalid query status %d: %s", rec.Code, rec.Body)
		}
	}
}

func TestHTTPSavedSearchesAndMutes(t *testing.T) {
	template := storetest.NewTemplate(t)
	t.Run("saved searches", func(t *testing.T) {
		f := newAPIFixture(t, template)
		agent, _ := surfaceAgent(t, f)
		body := savedSearchBody{Name: "Stored entries", Query: "stored", Filters: service.SearchFilter{SubscriptionID: f.subID, Limit: 1}}
		rec := f.do(t, http.MethodPost, "/v1/saved-searches", body, f.session())
		requireStatus(t, rec, http.StatusOK)
		created := decodeBody[savedSearch](t, rec)
		if created.ID == 0 || created.Name != body.Name || created.Filters.SubscriptionID != f.subID {
			t.Fatal(created)
		}
		rec = f.do(t, http.MethodGet, "/v1/saved-searches", nil, agent)
		requireStatus(t, rec, http.StatusOK)
		if rows := decodeBody[[]savedSearch](t, rec); len(rows) != 1 || rows[0].ID != created.ID {
			t.Fatal(rows)
		}
		path := fmt.Sprintf("/v1/saved-searches/%d", created.ID)
		requireStatus(t, f.do(t, http.MethodGet, path, nil, agent), http.StatusOK)
		rec = f.do(t, http.MethodGet, path+"/entries", nil, agent)
		requireStatus(t, rec, http.StatusOK)
		page := decodeBody[searchPage](t, rec)
		if len(page.Matches) != 1 || page.Matches[0].EntryID != f.entryID {
			t.Fatal(page)
		}
		rec = f.do(t, http.MethodGet, path+"/entries?cursor="+page.NextCursor, nil, agent)
		requireStatus(t, rec, http.StatusOK)
		if len(decodeBody[searchPage](t, rec).Matches) != 0 {
			t.Fatal("saved cursor repeated results")
		}
		requireStatus(t, f.do(t, http.MethodPost, "/v1/saved-searches", body, agent), http.StatusForbidden)
		requireStatus(t, f.do(t, http.MethodPatch, path, savedSearchRename{Name: "Renamed"}, agent), http.StatusForbidden)
		requireStatus(t, f.do(t, http.MethodDelete, path, nil, agent), http.StatusForbidden)
		requireStatus(t, f.do(t, http.MethodPatch, path, savedSearchRename{Name: "Renamed"}, f.session()), http.StatusOK)
		requireStatus(t, f.do(t, http.MethodDelete, path, nil, f.session()), http.StatusOK)
		requireStatus(t, f.do(t, http.MethodGet, path+"/entries", nil, f.session()), http.StatusNotFound)
	})
	t.Run("mutes hide river without changing state", func(t *testing.T) {
		f := newAPIFixture(t, template)
		agent, _ := surfaceAgent(t, f)
		body := muteBody{Kind: service.MuteKeyword, Value: "Test", OpID: "mute"}
		rec := f.do(t, http.MethodPost, "/v1/mutes", body, f.session())
		requireStatus(t, rec, http.StatusOK)
		mute := decodeBody[service.Mute](t, rec)
		if mute.ID == 0 || mute.Value != "Test" {
			t.Fatal(mute)
		}
		rec = f.do(t, http.MethodPost, "/v1/mutes", body, f.session())
		requireStatus(t, rec, http.StatusOK)
		if decodeBody[service.Mute](t, rec).ID != mute.ID {
			t.Fatal("mute replay changed ID")
		}
		rec = f.do(t, http.MethodGet, "/v1/mutes", nil, agent)
		requireStatus(t, rec, http.StatusOK)
		if rows := decodeBody[[]service.Mute](t, rec); len(rows) != 1 {
			t.Fatal(rows)
		}
		rec = f.do(t, http.MethodGet, "/v1/entries?view=river", nil, f.session())
		requireStatus(t, rec, http.StatusOK)
		if len(decodeBody[entriesPage](t, rec).Entries) != 0 {
			t.Fatal("mute did not hide river entry")
		}
		requireStatus(t, f.do(t, http.MethodPost, "/v1/mutes", body, agent), http.StatusForbidden)
		requireStatus(t, f.do(t, http.MethodGet, fmt.Sprintf("/v1/mutes/%d", mute.ID), nil, agent), http.StatusOK)
		path := fmt.Sprintf("/v1/mutes/%d?op_id=unmute", mute.ID)
		requireStatus(t, f.do(t, http.MethodDelete, path, nil, agent), http.StatusForbidden)
		requireStatus(t, f.do(t, http.MethodDelete, path, nil, f.session()), http.StatusOK)
		rec = f.do(t, http.MethodGet, "/v1/entries?view=river", nil, f.session())
		requireStatus(t, rec, http.StatusOK)
		entries := decodeBody[entriesPage](t, rec).Entries
		if len(entries) != 1 || entries[0].State.Seen.Value != "unseen" {
			t.Fatal(entries)
		}
	})
}

func TestSearchCursorEncoding(t *testing.T) {
	for _, raw := range []string{"!", strings.Repeat("A", 1025), "bm90IGpzb24"} {
		if _, err := decodeSearchCursor(raw); err == nil {
			t.Fatalf("accepted %q", raw)
		}
	}
	original := []service.SearchResult{{EntryID: 1, Seq: 42, Rank: 0.125, AnnotationID: 17, AgentName: "Bot"}}
	page, err := toSearchPage(original, nil)
	if err != nil {
		t.Fatal(err)
	}
	cursor, err := decodeSearchCursor(page.NextCursor)
	if err != nil || cursor.Rank != 0.125 || cursor.Seq != 42 || cursor.AnnotationID != 17 {
		t.Fatal(cursor, err)
	}
}
