package service

import (
	"context"
	"fmt"
	"testing"

	"github.com/L-K-M/Earnest/internal/feed"
	"github.com/L-K-M/Earnest/internal/store/storetest"
)

func TestImportKeptLabels(t *testing.T) {
	template := storetest.NewTemplate(t)
	labelsOf := func(t *testing.T, f *coreFixture, entry int64) string {
		t.Helper()
		var names string
		if err := f.db.Pool().QueryRow(context.Background(), `SELECT coalesce(string_agg(l.name, ',' ORDER BY l.name),'') FROM entry_labels m JOIN labels l ON l.id=m.label_id
 WHERE m.entry_id=$1 AND m.deleted_at IS NULL AND l.deleted_at IS NULL`, entry).Scan(&names); err != nil {
			t.Fatal(err)
		}
		return names
	}

	t.Run("adds_new_and_existing_labels_but_not_the_folder", func(t *testing.T) {
		f := newCoreFixture(t, template)
		ctx := context.Background()
		existing, err := f.s.CreateLabel(ctx, f.a, "Read later", "label-1")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := f.db.Pool().Exec(ctx, "WITH folder AS (INSERT INTO folders(user_id,name,rev) VALUES($1,'Tech',1) RETURNING id) UPDATE subscriptions SET folder_id=(SELECT id FROM folder) WHERE id=$2", f.a.UserID, f.subscription); err != nil {
			t.Fatal(err)
		}
		item := KeptImportItem{FeedURL: keptTestFeed, GUID: "tag:example.test,2026:labelled", Title: "Labelled", Labels: []string{"Read later", " Ideas ", "Tech", "Ideas", ""}}
		report, err := f.s.ImportKept(ctx, f.a, []KeptImportItem{item}, MissingFeedsSkip)
		if err != nil {
			t.Fatal(err)
		}
		wantStatuses(t, report, KeptImportCreated)
		entry := report.Results[0].EntryID
		if got := fmt.Sprint(report.Results[0].Labels); got != "[Ideas Read later]" {
			t.Fatalf("applied labels = %s", got)
		}
		if got := labelsOf(t, f, entry); got != "Ideas,Read later" {
			t.Fatalf("stored labels = %q; the folder name is not a label", got)
		}
		if f.count(t, "SELECT count(*) FROM labels WHERE user_id=$1 AND name='Read later'", f.a.UserID) != 1 || existing.ID == 0 {
			t.Fatal("an existing label must be reused, not duplicated")
		}
		again, err := f.s.ImportKept(ctx, f.a, []KeptImportItem{item}, MissingFeedsSkip)
		if err != nil {
			t.Fatal(err)
		}
		if len(again.Results[0].Labels) != 0 {
			t.Fatalf("a repeated import adds no labels: %v", again.Results[0].Labels)
		}
		coreCheckGapless(t, f.db, f.a.UserID)
	})

	t.Run("never_brings_back_a_deleted_label_or_a_removed_membership", func(t *testing.T) {
		f := newCoreFixture(t, template)
		ctx := context.Background()
		entry := f.ingestKept(t, feed.Item{GUID: "https://example.test/decided", URL: "https://example.test/decided"})
		gone, err := f.s.CreateLabel(ctx, f.a, "Old", "label-old")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := f.s.DeleteLabel(ctx, f.a, gone.ID, "delete-old"); err != nil {
			t.Fatal(err)
		}
		kept, err := f.s.CreateLabel(ctx, f.a, "Removed", "label-removed")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := f.s.AddLabel(ctx, f.a, kept.ID, entry, "add-removed"); err != nil {
			t.Fatal(err)
		}
		if _, err := f.s.RemoveLabel(ctx, f.a, kept.ID, entry, "remove-removed"); err != nil {
			t.Fatal(err)
		}
		report, err := f.s.ImportKept(ctx, f.a, []KeptImportItem{{FeedURL: keptTestFeed, GUID: "https://example.test/decided", Labels: []string{"Old", "Removed", "New"}}}, MissingFeedsSkip)
		if err != nil {
			t.Fatal(err)
		}
		wantStatuses(t, report, KeptImportMatched)
		if got := fmt.Sprint(report.Results[0].Labels); got != "[New]" {
			t.Fatalf("applied labels = %s", got)
		}
		if got := labelsOf(t, f, entry); got != "New" {
			t.Fatalf("stored labels = %q", got)
		}
		if f.count(t, "SELECT count(*) FROM labels WHERE user_id=$1 AND name='Old' AND deleted_at IS NULL", f.a.UserID) != 0 {
			t.Fatal("a deleted label must stay deleted")
		}
		coreCheckGapless(t, f.db, f.a.UserID)
	})
}
