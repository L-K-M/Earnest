package service

import (
	"context"
	"errors"
	"testing"

	"github.com/L-K-M/Earnest/internal/store/storetest"
)

func TestMutes(t *testing.T) {
	template := storetest.NewTemplate(t)
	f := entriesTestNew(t, template)
	ctx := context.Background()
	feed := entriesTestFeed(t, f.db, "https://mutes.test/rss", 0)
	entriesTestSub(t, f.db, f.a.UserID, feed, "now")
	first := f.ingest(t, feed, "A noisy headline", entriesTestBase)
	second := f.ingest(t, feed, "Other headline", entriesTestBase)
	if _, err := f.db.Pool().Exec(ctx, `UPDATE entries SET author='Ada' WHERE id=$1`, second); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		kind  MuteKind
		value string
	}{{"bad", "word"}, {MuteKeyword, "  "}} {
		if _, err := f.s.CreateMute(ctx, f.a, tc.kind, tc.value, "invalid"); !errors.Is(err, ErrInvalid) {
			t.Fatalf("invalid mute: %v", err)
		}
	}
	keyword, err := f.s.CreateMute(ctx, f.a, MuteKeyword, " NoISy ", "keyword")
	if err != nil {
		t.Fatal(err)
	}
	replay, err := f.s.CreateMute(ctx, f.a, MuteKeyword, " NoISy ", "keyword")
	if err != nil || replay != keyword {
		t.Fatalf("replay: %+v %v", replay, err)
	}
	author, err := f.s.CreateMute(ctx, f.a, MuteAuthor, "ada", "author")
	if err != nil {
		t.Fatal(err)
	}
	list, err := f.s.Mutes(ctx, f.a)
	if err != nil || len(list) != 2 {
		t.Fatalf("list: %+v %v", list, err)
	}
	river, err := f.s.Entries(ctx, f.a, EntryFilter{})
	if err != nil || len(river) != 0 {
		t.Fatalf("muted river: %+v %v", river, err)
	}
	all, err := f.s.Entries(ctx, f.a, EntryFilter{View: ViewAll})
	if err != nil || len(all) != 2 {
		t.Fatalf("all entries: %+v %v", all, err)
	}
	if err := f.s.DeleteMute(ctx, f.a, keyword.ID, "delete"); err != nil {
		t.Fatal(err)
	}
	if err := f.s.DeleteMute(ctx, f.a, keyword.ID, "delete"); err != nil {
		t.Fatal(err)
	}
	river, err = f.s.Entries(ctx, f.a, EntryFilter{})
	if err != nil || len(river) != 1 || river[0].ID != first {
		t.Fatalf("unmuted river: %+v %v", river, err)
	}
	restored, err := f.s.CreateMute(ctx, f.a, MuteKeyword, "NoISy", "restore")
	if err != nil || restored.ID != keyword.ID || restored.Deleted {
		t.Fatalf("restore: %+v %v", restored, err)
	}
	other := entriesTestOtherUser(t, f.db, "muteOther")
	if err := f.s.DeleteMute(ctx, other, author.ID, "foreign"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("foreign deletion: %v", err)
	}
	agent := entriesTestAgent(t, f.db, f.a.UserID, "mutebot", ScopeEntriesRead, ScopeFeedsWrite)
	if _, err := f.s.CreateMute(ctx, agent, MuteAuthor, "Ada", "agent"); !errors.Is(err, ErrForbidden) {
		t.Fatalf("agent: %v", err)
	}
}
