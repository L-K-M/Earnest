package service

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/L-K-M/Earnest/internal/store"
	"github.com/L-K-M/Earnest/internal/store/storetest"
	"github.com/L-K-M/Earnest/internal/syncmodel"
)

func TestTriageAndRevert(t *testing.T) {
	template := storetest.NewTemplate(t)
	t.Run("handle_bound_and_revert", func(t *testing.T) {
		f := newCoreFixture(t, template)
		ctx := context.Background()
		id := f.ingest(t, "article")
		a := surfaceAgent(t, f, "Morning", 3)
		b := surfaceAgent(t, f, "Evening", 3)
		generation, err := f.db.Generation(ctx)
		if err != nil {
			t.Fatal(err)
		}
		request := TriageRequest{OpID: "triage", Generation: generation, EntryIDs: []int64{id}, Action: TriageDone}
		result, err := f.s.Triage(ctx, a, request)
		if err != nil || len(result.Results) != 1 || result.Results[0].State.Shelf.Source != syncmodel.SourceRuleOrAgent {
			t.Fatal(result, err)
		}
		var tokenBytes []byte
		tokenBytes, err = json.Marshal(result)
		if err != nil || len(tokenBytes) == 0 {
			t.Fatal(err)
		}
		replay, err := f.s.Triage(ctx, a, request)
		if err != nil || replay.RevertHandle != result.RevertHandle {
			t.Fatal("triage replay", replay, err)
		}
		if err := f.s.RevertTriage(ctx, b, result.RevertHandle, "steal"); !errors.Is(err, ErrForbidden) {
			t.Fatal("handle crossed principal", err)
		}
		changes, err := f.s.AgentChangesToUnseen(ctx, f.a, time.Time{})
		if err != nil || len(changes) != 1 || changes[0].NextAction != "revert" {
			t.Fatal("unseen changes", changes, err)
		}
		if err := f.s.RevertTriage(ctx, a, result.RevertHandle, "undo"); err != nil {
			t.Fatal(err)
		}
		state, err := store.LoadState(ctx, f.db.Pool(), f.a.PrincipalID, id)
		if err != nil || state.Shelf.Value != syncmodel.None {
			t.Fatal(state, err)
		}
		if err := f.s.RevertTriage(ctx, a, result.RevertHandle, "undo"); err != nil {
			t.Fatal("replay undo", err)
		}
	})
	t.Run("human_edit_blocks_revert", func(t *testing.T) {
		f := newCoreFixture(t, template)
		ctx := context.Background()
		id := f.ingest(t, "article")
		a := surfaceAgent(t, f, "Morning", 3)
		gen, err := f.db.Generation(ctx)
		if err != nil {
			t.Fatal(err)
		}
		result, err := f.s.Triage(ctx, a, TriageRequest{OpID: "triage", Generation: gen, EntryIDs: []int64{id}, Action: TriageDone})
		if err != nil {
			t.Fatal(err)
		}
		edit := f.write(t, id, "human-later", syncmodel.Shelf, syncmodel.Later)
		edit.Base.Rev = result.Results[0].State.Shelf.Rev
		if _, err := f.s.Batch(ctx, f.a, []syncmodel.Write{edit}); err != nil {
			t.Fatal(err)
		}
		if err := f.s.RevertTriage(ctx, a, result.RevertHandle, "undo"); !errors.Is(err, ErrConflict) {
			t.Fatal("later human edit reverted", err)
		}
		rows, err := f.s.ListActivity(ctx, f.a, ActivityFilter{PrincipalID: a.PrincipalID})
		if err != nil || rows[0].NextAction != "nothing" || rows[0].Reason == "" {
			t.Fatal(rows, err)
		}
	})
	t.Run("expiry_and_human_revert", func(t *testing.T) {
		f := newCoreFixture(t, template)
		ctx := context.Background()
		id := f.ingest(t, "article")
		a := surfaceAgent(t, f, "Morning", 3)
		gen, err := f.db.Generation(ctx)
		if err != nil {
			t.Fatal(err)
		}
		result, err := f.s.Triage(ctx, a, TriageRequest{OpID: "triage", Generation: gen, EntryIDs: []int64{id}, Action: TriageSeen})
		if err != nil {
			t.Fatal(err)
		}
		f.clock.Add(int64(revertHandleLifetime))
		if err := f.s.RevertTriage(ctx, a, result.RevertHandle, "expired"); !errors.Is(err, ErrGone) {
			t.Fatal(err)
		}
		rows, err := f.s.ListActivity(ctx, f.a, ActivityFilter{PrincipalID: a.PrincipalID})
		if err != nil || len(rows) != 1 {
			t.Fatal(rows, err)
		}
		if err := f.s.Revert(ctx, f.a, rows[0].ID, "human-revert"); err != nil {
			t.Fatal(err)
		}
		if _, err := f.db.Pool().Exec(ctx, "UPDATE activity SET action='fake' WHERE id=$1", rows[0].ID); err == nil {
			t.Fatal("activity mutable")
		}
	})
}
func TestAnnotationRevert(t *testing.T) {
	template := storetest.NewTemplate(t)
	f := newCoreFixture(t, template)
	ctx := context.Background()
	id := f.ingest(t, "article")
	a := surfaceAgent(t, f, "Morning", 3)
	request := PutAnnotationRequest{OpID: "note", EntryIDs: []int64{id}, Kind: AnnotationNote, Value: "note"}
	n, err := f.s.PutAnnotation(ctx, a, request)
	if err != nil {
		t.Fatal(err)
	}
	rows, err := f.s.ListActivity(ctx, f.a, ActivityFilter{Action: "annotation.updated"})
	if err != nil || len(rows) != 1 {
		t.Fatal(rows, err)
	}
	if err := f.s.Revert(ctx, f.a, rows[0].ID, "revert-note"); err != nil {
		t.Fatal(err)
	}
	if _, err := f.s.GetAnnotation(ctx, f.a, a.PrincipalID, []int64{id}, AnnotationNote); !errors.Is(err, ErrNotFound) {
		t.Fatal("created annotation not removed", n, err)
	}
	request.OpID = "again"
	request.Value = "new note"
	if _, err := f.s.PutAnnotation(ctx, a, request); err != nil {
		t.Fatal(err)
	}
	if err := f.s.DeleteAnnotation(ctx, a, []int64{id}, AnnotationNote, "delete"); err != nil {
		t.Fatal(err)
	}
	rows, err = f.s.ListActivity(ctx, f.a, ActivityFilter{Action: "annotation.deleted"})
	if err != nil || len(rows) != 1 {
		t.Fatal(rows, err)
	}
	if err := f.s.Revert(ctx, a, rows[0].ID, "restore-note"); err != nil {
		t.Fatal(err)
	}
	restored, err := f.s.GetAnnotation(ctx, f.a, a.PrincipalID, []int64{id}, AnnotationNote)
	if err != nil || restored.Value != "new note" {
		t.Fatal(restored, err)
	}
}

func TestAgentOwnStateAndPositionActivity(t *testing.T) {
	template := storetest.NewTemplate(t)
	f := newCoreFixture(t, template)
	ctx := context.Background()
	id := f.ingest(t, "article")
	a := surfaceAgent(t, f, "Morning", 3)
	w := f.write(t, id, "own", syncmodel.Seen, syncmodel.SeenValue)
	w.Principal = syncmodel.ID(a.PrincipalID)
	if _, err := f.s.Batch(ctx, a, []syncmodel.Write{w}); err != nil {
		t.Fatal(err)
	}
	rows, err := f.s.ListActivity(ctx, f.a, ActivityFilter{PrincipalID: a.PrincipalID})
	if err != nil || len(rows) != 1 {
		t.Fatal("own state not logged", rows, err)
	}
	if err := f.s.Revert(ctx, f.a, rows[0].ID, "human-undo-own"); err != nil {
		t.Fatal(err)
	}
	state, err := store.LoadState(ctx, f.db.Pool(), a.PrincipalID, id)
	if err != nil || state.Seen.Value != syncmodel.Unseen {
		t.Fatal(state, err)
	}
	if _, err := f.s.PutPosition(ctx, a, "today", 1, "position"); err != nil {
		t.Fatal(err)
	}
	rows, err = f.s.ListActivity(ctx, f.a, ActivityFilter{PrincipalID: a.PrincipalID, Action: "position.updated"})
	if err != nil || len(rows) != 1 || rows[0].NextAction != "revert" {
		t.Fatal("position not logged", rows, err)
	}
	if err := f.s.Revert(ctx, a, rows[0].ID, "undo-position"); err != nil {
		t.Fatal(err)
	}
	position, err := f.s.Position(ctx, a, "today")
	if err != nil || position.Seq != 0 {
		t.Fatal(position, err)
	}
}

func TestRevertPreservesUnrelatedRegister(t *testing.T) {
	template := storetest.NewTemplate(t)
	f := newCoreFixture(t, template)
	ctx := context.Background()
	id := f.ingest(t, "article")
	a := surfaceAgent(t, f, "Morning", 3)
	gen, err := f.db.Generation(ctx)
	if err != nil {
		t.Fatal(err)
	}
	result, err := f.s.Triage(ctx, a, TriageRequest{OpID: "done", Generation: gen, EntryIDs: []int64{id}, Action: TriageDone})
	if err != nil {
		t.Fatal(err)
	}
	kept := f.write(t, id, "keep", syncmodel.Kept, syncmodel.True)
	if _, err := f.s.Batch(ctx, f.a, []syncmodel.Write{kept}); err != nil {
		t.Fatal(err)
	}
	if err := f.s.RevertTriage(ctx, a, result.RevertHandle, "undo"); err != nil {
		t.Fatal("unrelated register blocked revert", err)
	}
	state, err := store.LoadState(ctx, f.db.Pool(), f.a.PrincipalID, id)
	if err != nil || state.Shelf.Value != syncmodel.None || state.Kept.Value != syncmodel.True {
		t.Fatal(state, err)
	}
}
