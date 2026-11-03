package service

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"testing"

	"github.com/L-K-M/Earnest/internal/store/storetest"
)

func entriesTestChanges(t *testing.T, f *entriesTestFixture, kind string) []map[string]any {
	t.Helper()
	rows, err := f.db.Pool().Query(context.Background(), "SELECT payload FROM changes WHERE user_id=$1 AND type=$2 ORDER BY rev", f.a.UserID, kind)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		var body []byte
		if err := rows.Scan(&body); err != nil {
			t.Fatal(err)
		}
		var payload map[string]any
		if err := json.Unmarshal(body, &payload); err != nil {
			t.Fatal(err)
		}
		out = append(out, payload)
	}
	return out
}

func TestLabelsService(t *testing.T) {
	template := storetest.NewTemplate(t)
	f := entriesTestNew(t, template)
	ctx := context.Background()
	user := f.a.UserID
	feed := entriesTestFeed(t, f.db, "https://entriesTest-l.test/rss", 0)
	entriesTestSub(t, f.db, user, feed, "today")
	e1 := f.ingest(t, feed, "first", f.now())
	e2 := f.ingest(t, feed, "second", f.now())
	f.setClock(entriesTestBase)

	t.Run("create_rename_events_and_replay", func(t *testing.T) {
		label, err := f.s.CreateLabel(ctx, f.a, "entriesTest tech", "op-create")
		if err != nil || label.ID == 0 || label.Rev == 0 || label.Deleted {
			t.Fatalf("create: %+v %v", label, err)
		}
		added := entriesTestChanges(t, f, "label.added")
		if len(added) != 1 {
			t.Fatalf("events: %v", added)
		}
		def, _ := added[0]["label"].(map[string]any)
		if def["name"] != "entriesTest tech" || def["deleted"] != false {
			t.Fatalf("definition object: %v", def)
		}
		if uint64(def["rev"].(float64)) != label.Rev {
			t.Fatal("event does not carry the resource revision")
		}
		replay, err := f.s.CreateLabel(ctx, f.a, "entriesTest tech", "op-create")
		if err != nil || replay != label {
			t.Fatalf("replay: %+v %v", replay, err)
		}
		if n := len(entriesTestChanges(t, f, "label.added")); n != 1 {
			t.Fatalf("replay emitted %d events", n)
		}
		if _, err := f.s.CreateLabel(ctx, f.a, "entriesTest tech", "op-dup-name"); !errors.Is(err, ErrConflict) {
			t.Fatal("duplicate name accepted", err)
		}
		renamed, err := f.s.RenameLabel(ctx, f.a, label.ID, "entriesTest renamed", "op-rename")
		if err != nil || renamed.Name != "entriesTest renamed" || renamed.Rev <= label.Rev {
			t.Fatalf("rename: %+v %v", renamed, err)
		}
		if _, err := f.s.RenameLabel(ctx, f.a, label.ID+99, "x", "op-missing"); !errors.Is(err, ErrNotFound) {
			t.Fatal("missing label renamed", err)
		}
		second, err := f.s.CreateLabel(ctx, f.a, "entriesTest other", "op-second")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := f.s.RenameLabel(ctx, f.a, second.ID, "entriesTest renamed", "op-clash"); !errors.Is(err, ErrConflict) {
			t.Fatal("rename onto taken name accepted", err)
		}
	})
	t.Run("membership_lifecycle_and_tombstones", func(t *testing.T) {
		label, err := f.s.CreateLabel(ctx, f.a, "entriesTest members", "m-create")
		if err != nil {
			t.Fatal(err)
		}
		m, err := f.s.AddLabel(ctx, f.a, label.ID, e1, "m-add-1")
		if err != nil || m.LabelID != label.ID || m.EntryID != e1 || m.Rev == 0 || m.Deleted {
			t.Fatalf("add: %+v %v", m, err)
		}
		again, err := f.s.AddLabel(ctx, f.a, label.ID, e1, "m-add-again")
		if err != nil || again.Rev != m.Rev {
			t.Fatalf("repeat add changed membership: %+v", again)
		}
		replay, err := f.s.AddLabel(ctx, f.a, label.ID, e1, "m-add-1")
		if err != nil || replay != m {
			t.Fatalf("add replay: %+v %v", replay, err)
		}
		removed, err := f.s.RemoveLabel(ctx, f.a, label.ID, e1, "m-remove")
		if err != nil || !removed.Deleted || removed.Rev <= m.Rev {
			t.Fatalf("remove: %+v %v", removed, err)
		}
		if _, err := f.s.RemoveLabel(ctx, f.a, label.ID, e1, "m-remove-again"); !errors.Is(err, ErrNotFound) {
			t.Fatal("removed membership removed again", err)
		}
		restored, err := f.s.AddLabel(ctx, f.a, label.ID, e1, "m-restore")
		if err != nil || restored.Deleted || restored.Rev <= removed.Rev {
			t.Fatalf("resurrect: %+v %v", restored, err)
		}
		// Deleting the definition tombstones every membership with its own event.
		if _, err := f.s.AddLabel(ctx, f.a, label.ID, e2, "m-add-2"); err != nil {
			t.Fatal(err)
		}
		deleted, err := f.s.DeleteLabel(ctx, f.a, label.ID, "m-delete")
		if err != nil || !deleted.Deleted || deleted.Name != "entriesTest members" {
			t.Fatalf("delete: %+v %v", deleted, err)
		}
		var memberships int
		if err := f.db.Pool().QueryRow(ctx, "SELECT count(*) FROM entry_labels WHERE user_id=$1 AND label_id=$2 AND deleted_at IS NOT NULL", user, label.ID).Scan(&memberships); err != nil || memberships != 2 {
			t.Fatalf("membership tombstones: %d %v", memberships, err)
		}
		// One earlier direct removal plus two cascade memberships and the definition.
		removedEvents := entriesTestChanges(t, f, "label.removed")
		if len(removedEvents) != 4 {
			t.Fatalf("tombstone events: %v", removedEvents)
		}
		membershipEvents, definitionEvents := 0, 0
		for _, payload := range removedEvents {
			if obj, ok := payload["object"].(map[string]any); ok {
				if obj["deleted"] != true || obj["kind"] != "label" || obj["entry_id"] == nil || obj["label_id"] != nil {
					t.Fatalf("membership removal object: %v", obj)
				}
				membershipEvents++
				continue
			}
			def, _ := payload["label"].(map[string]any)
			if def["deleted"] != true {
				t.Fatalf("definition removal is not a tombstone: %v", def)
			}
			definitionEvents++
		}
		if membershipEvents != 3 || definitionEvents != 1 {
			t.Fatalf("tombstone split %d/%d", membershipEvents, definitionEvents)
		}
		if _, err := f.s.AddLabel(ctx, f.a, label.ID, e1, "m-dead-add"); !errors.Is(err, ErrNotFound) {
			t.Fatal("deleted label still assignable", err)
		}
		labels, err := f.s.Labels(ctx, f.a)
		if err != nil {
			t.Fatal(err)
		}
		var found bool
		for _, l := range labels {
			if l.ID == label.ID && l.Deleted && l.Rev == deleted.Rev {
				found = true
			}
		}
		if !found {
			t.Fatal("tombstone missing from bootstrap list")
		}
		// A freed name can be taken again.
		if _, err := f.s.CreateLabel(ctx, f.a, "entriesTest members", "m-recreate"); err != nil {
			t.Fatal("tombstoned name not reusable", err)
		}
	})
	t.Run("validation_scopes_and_cross_method", func(t *testing.T) {
		label, err := f.s.CreateLabel(ctx, f.a, "entriesTest guard", "g-create")
		if err != nil {
			t.Fatal(err)
		}
		other := entriesTestOtherUser(t, f.db, "entriesTestOtherL")
		if _, err := f.s.AddLabel(ctx, other, label.ID, e1, "g-foreign-label"); !errors.Is(err, ErrNotFound) {
			t.Fatal("foreign label assignable", err)
		}
		foreign, err := f.s.CreateLabel(ctx, other, "entriesTest foreign", "g-foreign-create")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := f.s.AddLabel(ctx, other, foreign.ID, e1, "g-foreign-entry"); !errors.Is(err, ErrNotFound) {
			t.Fatal("foreign entry assignable", err)
		}
		agent := entriesTestAgent(t, f.db, user, "labelbot", ScopeEntriesRead, ScopeAnnotationsWrite, ScopePositionOwn)
		if _, err := f.s.CreateLabel(ctx, agent, "agent label", "g-agent"); !errors.Is(err, ErrForbidden) {
			t.Fatal("agent wrote a native label", err)
		}
		reader := f.a
		reader.Scopes = []Scope{ScopeEntriesRead}
		if _, err := f.s.CreateLabel(ctx, reader, "x", "g-reader"); !errors.Is(err, ErrForbidden) {
			t.Fatal("scopeless writer accepted", err)
		}
		// The same op_id under a different method is a collision, not a replay.
		if _, err := f.s.PutPosition(ctx, f.a, "river", 1, "g-create"); !errors.Is(err, ErrInvalid) {
			t.Fatal("cross-method op_id accepted", err)
		}
		for _, tc := range []struct {
			name string
			call func() error
		}{
			{"empty name", func() error { _, err := f.s.CreateLabel(ctx, f.a, "  ", "g-badname"); return err }},
			{"empty op", func() error { _, err := f.s.CreateLabel(ctx, f.a, "x", ""); return err }},
			{"bad label id", func() error { _, err := f.s.AddLabel(ctx, f.a, 0, e1, "g-badid"); return err }},
			{"bad entry id", func() error { _, err := f.s.AddLabel(ctx, f.a, label.ID, 0, "g-badid2"); return err }},
			{"missing label", func() error { _, err := f.s.AddLabel(ctx, f.a, label.ID+99, e1, "g-missing"); return err }},
		} {
			if err := tc.call(); !errors.Is(err, ErrInvalid) && !errors.Is(err, ErrNotFound) {
				t.Fatalf("%s: %v", tc.name, err)
			}
		}
	})
}

func TestPositionsService(t *testing.T) {
	template := storetest.NewTemplate(t)
	f := entriesTestNew(t, template)
	ctx := context.Background()
	user := f.a.UserID
	feed := entriesTestFeed(t, f.db, "https://entriesTest-pos.test/rss", 0)
	entriesTestSub(t, f.db, user, feed, "today")
	ids := f.ingestMany(t, feed, "anchor", 8, f.now())
	f.setClock(entriesTestBase)
	var maxSeq uint64
	for _, id := range ids {
		e, err := f.s.Entry(ctx, f.a, id)
		if err != nil {
			t.Fatal(err)
		}
		if e.Seq > maxSeq {
			maxSeq = e.Seq
		}
	}

	t.Run("put_get_lww_and_replay", func(t *testing.T) {
		pos, err := f.s.Position(ctx, f.a, "river")
		if err != nil || pos.Seq != 0 || pos.Rev != 0 || pos.View != "river" {
			t.Fatalf("unwritten position: %+v %v", pos, err)
		}
		pos, err = f.s.PutPosition(ctx, f.a, "river", 5, "p-1")
		if err != nil || pos.Seq != 5 || pos.Rev == 0 || pos.View != "river" || int64(pos.Principal) != f.a.PrincipalID {
			t.Fatalf("put: %+v %v", pos, err)
		}
		events := entriesTestChanges(t, f, "position.updated")
		if len(events) != 1 {
			t.Fatalf("events: %v", events)
		}
		body, _ := events[0]["position"].(map[string]any)
		if uint64(body["seq"].(float64)) != 5 || uint64(body["rev"].(float64)) != pos.Rev || body["view"] != "river" {
			t.Fatalf("position payload: %v", body)
		}
		// Backward moves are ordinary last-writer-wins commits.
		pos, err = f.s.PutPosition(ctx, f.a, "river", 3, "p-2")
		if err != nil || pos.Seq != 3 {
			t.Fatalf("backward move: %+v %v", pos, err)
		}
		got, err := f.s.Position(ctx, f.a, "river")
		if err != nil || got.Seq != 3 {
			t.Fatalf("read back: %+v %v", got, err)
		}
		replay, err := f.s.PutPosition(ctx, f.a, "river", 3, "p-2")
		if err != nil || replay != pos {
			t.Fatalf("replay: %+v %v", replay, err)
		}
		if n := len(entriesTestChanges(t, f, "position.updated")); n != 2 {
			t.Fatalf("replay emitted %d events", n)
		}
		if _, err := f.s.PutPosition(ctx, f.a, "week", maxSeq, "p-3"); err != nil {
			t.Fatal(err)
		}
		all, err := f.s.Positions(ctx, f.a)
		if err != nil || len(all) != 2 {
			t.Fatalf("positions: %+v %v", all, err)
		}
	})
	t.Run("scopes_validation_and_agent_own", func(t *testing.T) {
		agent := entriesTestAgent(t, f.db, user, "posbot", ScopeEntriesRead, ScopePositionOwn)
		pos, err := f.s.PutPosition(ctx, agent, "briefing", 2, "pa-1")
		if err != nil || int64(pos.Principal) != agent.PrincipalID {
			t.Fatalf("agent position: %+v %v", pos, err)
		}
		got, err := f.s.Position(ctx, agent, "river")
		if err != nil || got.Seq != 0 {
			t.Fatal("agent read the human position", got, err)
		}
		if _, err := f.s.PutPosition(ctx, agent, "briefing", 4, "pa-2"); err != nil {
			t.Fatal(err)
		}
		narrow := entriesTestAgent(t, f.db, user, "narrowbot", ScopeEntriesRead)
		if _, err := f.s.PutPosition(ctx, narrow, "briefing", 1, "pa-narrow"); !errors.Is(err, ErrForbidden) {
			t.Fatal("agent wrote position without scope", err)
		}
		reader := f.a
		reader.Scopes = []Scope{ScopeEntriesRead}
		if _, err := f.s.PutPosition(ctx, reader, "river", 1, "pa-reader"); !errors.Is(err, ErrForbidden) {
			t.Fatal("human wrote position without state scope", err)
		}
		if _, err := f.s.Position(ctx, narrow, "river"); err != nil {
			t.Fatal("entries:read cannot read positions", err)
		}
		for _, tc := range []struct {
			name string
			view string
			seq  uint64
			op   string
		}{
			{"empty view", "", 1, "pv-1"},
			{"seq overflow", "river", math.MaxInt64 + 1, "pv-2"},
			{"empty op", "river", 1, ""},
		} {
			if _, err := f.s.PutPosition(ctx, f.a, tc.view, tc.seq, tc.op); !errors.Is(err, ErrInvalid) {
				t.Fatalf("%s accepted: %v", tc.name, err)
			}
		}
	})
}
