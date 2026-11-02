package service

import (
	"context"
	"strings"
	"testing"

	"github.com/L-K-M/Earnest/internal/store/storetest"
)

func TestSearchAndSavedSearch(t *testing.T) {
	f := entriesTestNew(t, storetest.NewTemplate(t))
	ctx := context.Background()
	feed := entriesTestFeed(t, f.db, "https://search.example/feed", 0)
	entriesTestSub(t, f.db, f.a.UserID, feed, "now")
	items := []IngestItem{entriesTestItem("first"), entriesTestItem("second")}
	items[0].Title = "Beacon title"
	items[0].ContentText = "a plain body"
	items[1].Title = "Other title"
	items[1].ContentText = "a beacon body"
	if _, err := f.s.RecordFetch(ctx, FetchOutcome{FeedID: feed, Status: FetchOK, Items: items}); err != nil {
		t.Fatal(err)
	}
	var agent int64
	if err := f.db.Pool().QueryRow(ctx, `INSERT INTO principals(user_id,kind,name) VALUES($1,'agent','Scout') RETURNING id`, f.a.UserID).Scan(&agent); err != nil {
		t.Fatal(err)
	}
	var entry int64
	if err := f.db.Pool().QueryRow(ctx, `SELECT id FROM entries WHERE feed_id=$1 ORDER BY seq LIMIT 1`, feed).Scan(&entry); err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.Pool().Exec(ctx, `INSERT INTO annotations(user_id,principal_id,kind,value,entry_ids,rev) VALUES($1,$2,'note','secret observation',ARRAY[$3::bigint],1)`, f.a.UserID, agent, entry); err != nil {
		t.Fatal(err)
	}
	var otherUser, otherPrincipal int64
	if err := f.db.Pool().QueryRow(ctx, `INSERT INTO users(username,password_hash) VALUES('search-other','$argon2id$test') RETURNING id`).Scan(&otherUser); err != nil {
		t.Fatal(err)
	}
	if err := f.db.Pool().QueryRow(ctx, `INSERT INTO principals(user_id,kind,name) VALUES($1,'human','Other') RETURNING id`, otherUser).Scan(&otherPrincipal); err != nil {
		t.Fatal(err)
	}
	private := entriesTestFeed(t, f.db, "https://search.example/private", otherUser)
	entriesTestSub(t, f.db, otherUser, private, "now")
	privateItem := entriesTestItem("private")
	privateItem.Title = "beacon private"
	if _, err := f.s.RecordFetch(ctx, FetchOutcome{FeedID: private, Status: FetchOK, Items: []IngestItem{privateItem}}); err != nil {
		t.Fatal(err)
	}
	other := Actor{UserID: otherUser, PrincipalID: otherPrincipal, Kind: PrincipalHuman, Via: CredentialSession, Scopes: HumanScopes}
	if result, err := f.s.Search(ctx, other, "observation", SearchFilter{}, nil); err != nil || len(result) != 0 {
		t.Fatalf("other user's note: %+v %v", result, err)
	}
	got, err := f.s.Search(ctx, f.a, "beacon", SearchFilter{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].EntryID != entry || got[0].Rank <= got[1].Rank {
		t.Fatalf("rank: %+v", got)
	}
	if !strings.Contains(got[0].Snippet, "Beacon") || strings.ContainsAny(got[0].Snippet, "<>") {
		t.Fatalf("snippet is not plain: %q", got[0].Snippet)
	}
	note, err := f.s.Search(ctx, f.a, "observation", SearchFilter{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(note) != 1 || note[0].AgentName != "Scout" || note[0].AnnotationID == 0 || !strings.Contains(note[0].Snippet, "observation") {
		t.Fatalf("note: %+v", note)
	}
	first, err := f.s.Search(ctx, f.a, "beacon", SearchFilter{Limit: 1}, nil)
	if err != nil {
		t.Fatal(err)
	}
	next, err := f.s.Search(ctx, f.a, "beacon", SearchFilter{Limit: 1}, &SearchCursor{Rank: first[0].Rank, Seq: first[0].Seq, AnnotationID: first[0].AnnotationID})
	if err != nil || len(next) != 1 || next[0].EntryID == first[0].EntryID {
		t.Fatalf("cursor: %+v %v", next, err)
	}
	saved, err := f.s.CreateSavedSearch(ctx, f.a, "My search", "beacon", SearchFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if got, err := f.s.ListSavedSearch(ctx, f.a, saved.ID, nil); err != nil || len(got) != 2 {
		t.Fatalf("saved view: %+v %v", got, err)
	}
	if err := f.s.RenameSavedSearch(ctx, f.a, saved.ID, "Renamed"); err != nil {
		t.Fatal(err)
	}
	if list, err := f.s.SavedSearches(ctx, f.a); err != nil || len(list) != 1 || list[0].Name != "Renamed" {
		t.Fatalf("saved list: %+v %v", list, err)
	}
	if err := f.s.DeleteSavedSearch(ctx, f.a, saved.ID); err != nil {
		t.Fatal(err)
	}
}

func TestSearchFeedLanguage(t *testing.T) {
	f := entriesTestNew(t, storetest.NewTemplate(t))
	ctx := context.Background()
	feed := entriesTestFeed(t, f.db, "https://language.example/feed", 0)
	entriesTestSub(t, f.db, f.a.UserID, feed, "now")
	item := entriesTestItem("language")
	item.Title = "running quickly"
	if _, err := f.s.RecordFetch(ctx, FetchOutcome{FeedID: feed, Status: FetchOK, Items: []IngestItem{item}}); err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		language, query string
		count           int
	}{
		{"en", "run", 1},
		{"unknown", "run", 0},
		{"unknown", "running", 1},
	}
	for _, tc := range cases {
		t.Run(tc.language+tc.query, func(t *testing.T) {
			if _, err := f.db.Pool().Exec(ctx, `UPDATE feeds SET language=$2 WHERE id=$1`, feed, tc.language); err != nil {
				t.Fatal(err)
			}
			got, err := f.s.Search(ctx, f.a, tc.query, SearchFilter{}, nil)
			if err != nil || len(got) != tc.count {
				t.Fatalf("got %d results, want %d: %v", len(got), tc.count, err)
			}
		})
	}
}
