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
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
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

func TestAnnotationRevertRejectsReplacement(t *testing.T) {
	f := newCoreFixture(t, storetest.NewTemplate(t))
	ctx := context.Background()
	a := surfaceAgent(t, f, "Writer", 3)
	id := f.ingest(t, "article")
	req := PutAnnotationRequest{OpID: "old", EntryIDs: []int64{id}, Kind: AnnotationNote, Value: "old"}
	if _, err := f.s.PutAnnotation(ctx, a, req); err != nil {
		t.Fatal(err)
	}
	if err := f.s.DeleteAnnotation(ctx, a, req.EntryIDs, req.Kind, "delete-old"); err != nil {
		t.Fatal(err)
	}
	rows, err := f.s.ListActivity(ctx, f.a, ActivityFilter{Action: "annotation.deleted"})
	if err != nil || len(rows) != 1 {
		t.Fatal(rows, err)
	}
	req.OpID, req.Value = "new", "new"
	if _, err := f.s.PutAnnotation(ctx, a, req); err != nil {
		t.Fatal(err)
	}
	if err := f.s.Revert(ctx, f.a, rows[0].ID, "undo-delete"); !errors.Is(err, ErrConflict) {
		t.Fatalf("revert = %v, want conflict", err)
	}
}

func TestRevertUsesTransactionForAuthorization(t *testing.T) {
	f := newCoreFixture(t, storetest.NewTemplate(t))
	ctx := context.Background()
	id := f.ingest(t, "article")
	a := surfaceAgent(t, f, "Agent", 3)
	gen, err := f.db.Generation(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.s.Triage(ctx, a, TriageRequest{OpID: "triage", Generation: gen, EntryIDs: []int64{id}, Action: TriageDone}); err != nil {
		t.Fatal(err)
	}
	rows, err := f.s.ListActivity(ctx, f.a, ActivityFilter{PrincipalID: a.PrincipalID})
	if err != nil || len(rows) != 1 {
		t.Fatal(rows, err)
	}
	var held []*pgxpool.Conn
	for i := 0; i < 9; i++ {
		c, err := f.db.Pool().Acquire(ctx)
		if err != nil {
			t.Fatal(err)
		}
		held = append(held, c)
	}
	defer func() {
		for _, c := range held {
			c.Release()
		}
	}()
	timeout, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	err = f.db.WithTx(timeout, func(tx pgx.Tx) error {
		if err := store.LockCore(timeout, tx); err != nil {
			return err
		}
		return f.s.revertTx(timeout, tx, f.a, rows[0].ID, "undo")
	})
	if err != nil {
		t.Fatalf("revert with pool occupied: %v", err)
	}
}

func TestAgentChangesToUnseenFiltersBeforeLimit(t *testing.T) {
	f := newCoreFixture(t, storetest.NewTemplate(t))
	ctx := context.Background()
	id := f.ingest(t, "article")
	a := surfaceAgent(t, f, "Agent", 3)
	own := syncmodel.State{Principal: syncmodel.ID(a.PrincipalID), EntryID: syncmodel.ID(id), Seen: syncmodel.Register{Value: syncmodel.Unseen}, Shelf: syncmodel.Register{Value: syncmodel.None}, Kept: syncmodel.Register{Value: syncmodel.False}}
	body, err := json.Marshal(own)
	if err != nil {
		t.Fatal(err)
	}
	_, err = f.db.Pool().Exec(ctx, `INSERT INTO activity(user_id,principal_id,action,target,before,after,created_at) SELECT $1,$2,'state.updated','{}',$3::jsonb,$3::jsonb,$4 FROM generate_series(1,1000)`, f.a.UserID, a.PrincipalID, body, f.s.now())
	if err != nil {
		t.Fatal(err)
	}
	gen, err := f.db.Generation(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.s.Triage(ctx, a, TriageRequest{OpID: "triage", Generation: gen, EntryIDs: []int64{id}, Action: TriageDone}); err != nil {
		t.Fatal(err)
	}
	rows, err := f.s.AgentChangesToUnseen(ctx, f.a, time.Time{})
	if err != nil || len(rows) != 1 {
		t.Fatalf("unseen changes: %d %v", len(rows), err)
	}
	id2 := f.ingest(t, "another article")
	if _, err := f.s.Triage(ctx, a, TriageRequest{OpID: "triage-2", Generation: gen, EntryIDs: []int64{id2}, Action: TriageDone}); err != nil {
		t.Fatal(err)
	}
	page, err := f.s.AgentChangesToUnseen(ctx, f.a, time.Time{}, ActivityFilter{AfterID: rows[0].ID, Limit: 1})
	if err != nil || len(page) != 1 || page[0].ID <= rows[0].ID {
		t.Fatalf("second page: %v %v", page, err)
	}
}

func TestRevertPublishesChangeHint(t *testing.T) {
	f := newCoreFixture(t, storetest.NewTemplate(t))
	ctx := context.Background()
	id := f.ingest(t, "article")
	a := surfaceAgent(t, f, "Morning", 3)
	gen, err := f.db.Generation(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.s.Triage(ctx, a, TriageRequest{OpID: "triage", Generation: gen, EntryIDs: []int64{id}, Action: TriageDone}); err != nil {
		t.Fatal(err)
	}
	rows, err := f.s.ListActivity(ctx, f.a, ActivityFilter{PrincipalID: a.PrincipalID})
	if err != nil || len(rows) != 1 {
		t.Fatal(rows, err)
	}
	hints, cancel, err := f.s.WatchChanges(ctx, f.a)
	if err != nil {
		t.Fatal(err)
	}
	defer cancel()
	if err := f.s.Revert(ctx, f.a, rows[0].ID, "human-revert"); err != nil {
		t.Fatal(err)
	}
	select {
	case <-hints:
	case <-time.After(5 * time.Second):
		t.Fatal("revert did not publish a change hint")
	}
}

func TestAnnotationRevertEmitsFullPayload(t *testing.T) {
	f := newCoreFixture(t, storetest.NewTemplate(t))
	ctx := context.Background()
	id := f.ingest(t, "article")
	a := surfaceAgent(t, f, "Morning", 3)
	request := PutAnnotationRequest{OpID: "note", EntryIDs: []int64{id}, Kind: AnnotationNote, Value: "note"}
	if _, err := f.s.PutAnnotation(ctx, a, request); err != nil {
		t.Fatal(err)
	}
	rows, err := f.s.ListActivity(ctx, f.a, ActivityFilter{Action: "annotation.updated"})
	if err != nil || len(rows) != 1 {
		t.Fatal(rows, err)
	}
	hints, cancel, err := f.s.WatchChanges(ctx, f.a)
	if err != nil {
		t.Fatal(err)
	}
	defer cancel()
	if err := f.s.Revert(ctx, f.a, rows[0].ID, "revert-note"); err != nil {
		t.Fatal(err)
	}
	select {
	case <-hints:
	case <-time.After(5 * time.Second):
		t.Fatal("annotation revert did not publish a change hint")
	}
	var kind string
	var payload []byte
	if err := f.db.Pool().QueryRow(ctx, "SELECT type,payload FROM changes WHERE user_id=$1 ORDER BY rev DESC LIMIT 1", f.a.UserID).Scan(&kind, &payload); err != nil {
		t.Fatal(err)
	}
	if kind != "annotation.deleted" {
		t.Fatalf("change type %q", kind)
	}
	var body struct {
		Annotation Annotation `json:"annotation"`
	}
	if err := json.Unmarshal(payload, &body); err != nil {
		t.Fatal(err)
	}
	if body.Annotation.Value != "note" || body.Annotation.ID == 0 {
		t.Fatalf("payload missing restored annotation: %s", payload)
	}
}

func TestMutePublishesChangeHint(t *testing.T) {
	f := entriesTestNew(t, storetest.NewTemplate(t))
	ctx := context.Background()
	hints, cancel, err := f.s.WatchChanges(ctx, f.a)
	if err != nil {
		t.Fatal(err)
	}
	defer cancel()
	mute, err := f.s.CreateMute(ctx, f.a, MuteKeyword, "noisy", "mute")
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-hints:
	case <-time.After(5 * time.Second):
		t.Fatal("mute create did not publish a change hint")
	}
	if err := f.s.DeleteMute(ctx, f.a, mute.ID, "delete"); err != nil {
		t.Fatal(err)
	}
	select {
	case <-hints:
	case <-time.After(5 * time.Second):
		t.Fatal("mute delete did not publish a change hint")
	}
}
