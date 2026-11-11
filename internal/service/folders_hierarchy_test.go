package service

import (
	"encoding/json"
	"errors"
	"fmt"
	"testing"

	"github.com/L-K-M/Earnest/internal/store/storetest"
)

func TestFolderHierarchy(t *testing.T) {
	f := newCoreFixture(t, storetest.NewTemplate(t))
	ctx := t.Context()
	create := func(name string) Folder {
		t.Helper()
		folder, err := f.s.CreateFolder(ctx, f.a, name, "create-"+name)
		if err != nil {
			t.Fatal(err)
		}
		return folder
	}
	move := func(id, parent int64, op string) (Folder, error) {
		return f.s.UpdateFolder(ctx, f.a, id, UpdateFolderOptions{ParentID: &parent, OpID: op})
	}
	parent, child, grandchild := create("Parent"), create("Child"), create("Grandchild")
	moved, err := move(child.ID, parent.ID, "move")
	if err != nil || moved.ParentID != parent.ID || moved.Name != child.Name {
		t.Fatal(moved, err)
	}
	replay, err := move(child.ID, parent.ID, "move")
	if err != nil || replay != moved {
		t.Fatal("replay", replay, err)
	}
	renamed, err := f.s.RenameFolder(ctx, f.a, child.ID, "Renamed", "rename")
	if err != nil || renamed.ParentID != parent.ID {
		t.Fatal("rename lost parent", renamed, err)
	}
	if _, err := move(grandchild.ID, child.ID, "nest"); err != nil {
		t.Fatal(err)
	}
	for i, destination := range []int64{parent.ID, child.ID, grandchild.ID, -1} {
		if _, err := move(parent.ID, destination, fmt.Sprintf("cycle-%d", i)); !errors.Is(err, ErrInvalid) {
			t.Fatalf("accepted cycle/invalid parent %d: %v", destination, err)
		}
	}
	if _, err := move(child.ID, 999999, "missing"); !errors.Is(err, ErrNotFound) {
		t.Fatal("accepted missing parent", err)
	}
	var foreign int64
	if err := f.db.Pool().QueryRow(ctx, `WITH u AS (INSERT INTO users(username,password_hash) VALUES('other','$argon2id$test') RETURNING id) INSERT INTO folders(user_id,name) SELECT id,'Foreign' FROM u RETURNING id`).Scan(&foreign); err != nil {
		t.Fatal(err)
	}
	if _, err := move(child.ID, foreign, "foreign"); !errors.Is(err, ErrNotFound) {
		t.Fatal("accepted foreign parent", err)
	}
	gone := create("Gone")
	if err := f.s.DeleteFolder(ctx, f.a, gone.ID, "delete-gone"); err != nil {
		t.Fatal(err)
	}
	if _, err := move(child.ID, gone.ID, "gone"); !errors.Is(err, ErrNotFound) {
		t.Fatal("accepted deleted parent", err)
	}
	page, err := f.s.Bootstrap(ctx, f.a, BootstrapRequest{Kind: BootstrapFolders})
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, raw := range page.Items {
		var folder Folder
		if err := json.Unmarshal(raw, &folder); err != nil {
			t.Fatal(err)
		}
		if folder.ID == child.ID {
			found = true
			if folder.ParentID != parent.ID {
				t.Fatal("bootstrap lost parent", folder)
			}
		}
	}
	if !found {
		t.Fatal("bootstrap lost child")
	}
	root, err := move(child.ID, 0, "root")
	if err != nil || root.ParentID != 0 {
		t.Fatal(root, err)
	}
	if _, err := move(child.ID, parent.ID, "back"); err != nil {
		t.Fatal(err)
	}
	if _, err := f.s.UpdateSubscription(ctx, f.a, f.subscription, UpdateSubscriptionOptions{FolderID: &parent.ID, OpID: "file"}); err != nil {
		t.Fatal(err)
	}
	if err := f.s.DeleteFolder(ctx, f.a, parent.ID, "delete-parent"); err != nil {
		t.Fatal(err)
	}
	folders, err := f.s.Folders(ctx, f.a)
	if err != nil {
		t.Fatal(err)
	}
	for _, folder := range folders {
		if folder.ID == child.ID && (folder.ParentID != 0 || folder.Rev <= renamed.Rev) {
			t.Fatal("child not reparented", folder)
		}
		if folder.ID == grandchild.ID && folder.ParentID != child.ID {
			t.Fatal("grandchild moved", folder)
		}
	}
	sub, err := f.s.Subscription(ctx, f.a, f.subscription)
	if err != nil || sub.FolderID != 0 {
		t.Fatal("direct feed not unfiled", sub, err)
	}
	var events int
	if err := f.db.Pool().QueryRow(ctx, `SELECT count(*) FROM changes WHERE user_id=$1 AND payload->'folder'->>'id'=$2 AND payload->'folder'->>'parent_id'='0'`, f.a.UserID, fmt.Sprint(child.ID)).Scan(&events); err != nil {
		t.Fatal(err)
	}
	if events < 3 {
		t.Fatal("root moves missing revisioned events", events)
	}
	coreCheckGapless(t, f.db, f.a.UserID)
}

func TestNestedFolderOPML(t *testing.T) {
	f := newCoreFixture(t, storetest.NewTemplate(t))
	ctx := t.Context()
	input := []OPMLOutline{{Title: "Parent", Outlines: []OPMLOutline{{Title: "Child", Outlines: []OPMLOutline{{Title: "Feed", XMLURL: "https://nested.test/rss"}}}}}}
	result, err := f.s.ImportOPML(ctx, f.a, input)
	if err != nil || len(result) != 3 {
		t.Fatal(result, err)
	}
	if result[0].Folder == nil || result[1].Folder == nil || result[2].Subscription == nil {
		t.Fatal(result)
	}
	if result[1].Folder.ParentID != result[0].Folder.ID {
		t.Fatal("import flattened child", result[1])
	}
	exported, err := f.s.ExportOPML(ctx, f.a)
	if err != nil || len(exported) != 2 || exported[0].Title != "Parent" || len(exported[0].Outlines) != 1 || exported[0].Outlines[0].Title != "Child" || len(exported[0].Outlines[0].Outlines) != 1 {
		t.Fatal(exported, err)
	}
	// Importing an existing folder under its descendant must not silently move it.
	repeated, err := f.s.ImportOPML(ctx, f.a, []OPMLOutline{{Title: "Child", Outlines: []OPMLOutline{{Title: "Parent"}}}})
	if err != nil || repeated[1].Folder.ParentID != 0 {
		t.Fatal("existing folder moved", repeated, err)
	}
	coreCheckGapless(t, f.db, f.a.UserID)
}

func TestConcurrentFolderMovesCannotCycle(t *testing.T) {
	f := newCoreFixture(t, storetest.NewTemplate(t))
	ctx := t.Context()
	a, err := f.s.CreateFolder(ctx, f.a, "A", "a")
	if err != nil {
		t.Fatal(err)
	}
	b, err := f.s.CreateFolder(ctx, f.a, "B", "b")
	if err != nil {
		t.Fatal(err)
	}
	start := make(chan struct{})
	results := make(chan error, 2)
	for _, pair := range [][2]int64{{a.ID, b.ID}, {b.ID, a.ID}} {
		go func() {
			<-start
			_, err := f.s.UpdateFolder(ctx, f.a, pair[0], UpdateFolderOptions{ParentID: &pair[1], OpID: fmt.Sprintf("move-%d", pair[0])})
			results <- err
		}()
	}
	close(start)
	successes := 0
	for range 2 {
		err := <-results
		if err == nil {
			successes++
		} else if !errors.Is(err, ErrInvalid) {
			t.Fatal(err)
		}
	}
	if successes != 1 {
		t.Fatal("competing moves did not serialize", successes)
	}
	coreCheckGapless(t, f.db, f.a.UserID)
}
