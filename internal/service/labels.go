package service

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/L-K-M/Earnest/internal/store"
	"github.com/L-K-M/Earnest/internal/syncmodel"
	"github.com/jackc/pgx/v5"
)

// Label is one user-owned label definition. Deleted labels remain as
// revisioned tombstones so bootstrap can reconcile them.
type Label struct {
	ID      int64  `json:"id"`
	Name    string `json:"name"`
	Rev     uint64 `json:"rev"`
	Deleted bool   `json:"deleted"`
}

// LabelMembership is one entry's membership in a label, likewise tombstoned.
type LabelMembership struct {
	ID      int64  `json:"id"`
	LabelID int64  `json:"label_id"`
	EntryID int64  `json:"entry_id"`
	Rev     uint64 `json:"rev"`
	Deleted bool   `json:"deleted"`
}

// labelObject carries one stable membership identity in label.added/removed.
// Label definitions are not Objects: the model requires EntryID, so lifecycle
// events carry the definition in a "label" payload field matching the
// bootstrap projection.
func labelDefinition(l Label) map[string]any {
	return map[string]any{"id": l.ID, "name": l.Name, "rev": l.Rev, "deleted": l.Deleted}
}
func labelObject(a Actor, id, entry int64, name string, rev uint64, deleted bool) syncmodel.Object {
	return syncmodel.Object{
		Kind:      syncmodel.LabelObject,
		ID:        syncmodel.ID(id),
		Principal: syncmodel.ID(a.PrincipalID),
		EntryID:   syncmodel.ID(entry),
		Name:      name,
		Rev:       rev,
		Deleted:   deleted,
	}
}

// Labels returns all of the actor's labels including tombstones.
func (s *Service) Labels(ctx context.Context, a Actor) ([]Label, error) {
	if err := s.checkActor(ctx, a, ScopeEntriesRead); err != nil {
		return nil, err
	}
	rows, err := store.LoadLabels(ctx, s.store.Pool(), a.UserID)
	if err != nil {
		return nil, err
	}
	out := make([]Label, 0, len(rows))
	for _, r := range rows {
		out = append(out, Label{ID: r.ID, Name: r.Name, Rev: uint64(r.Rev), Deleted: r.Deleted})
	}
	return out, nil
}

func (s *Service) checkLabelWriter(ctx context.Context, a Actor, name, opID string) error {
	if err := s.checkActor(ctx, a, ScopeStateWrite); err != nil {
		return err
	}
	// Labels are a native user resource; annotation labels are the agent path.
	if a.Kind == PrincipalAgent {
		return ErrForbidden
	}
	if !validName(name) {
		return fmt.Errorf("name: %w", ErrInvalid)
	}
	if !validOpID(opID) {
		return fmt.Errorf("op_id: %w", ErrInvalid)
	}
	return nil
}

// CreateLabel adds a label definition and emits label.added.
func (s *Service) CreateLabel(ctx context.Context, a Actor, name, opID string) (Label, error) {
	if err := s.checkLabelWriter(ctx, a, name, opID); err != nil {
		return Label{}, err
	}
	var out Label
	err := s.withEvents(ctx, func(tx pgx.Tx) error {
		if err := store.LockCore(ctx, tx); err != nil {
			return err
		}
		replay, err := replayOp(ctx, tx, a.PrincipalID, "CreateLabel", opID)
		if err != nil {
			return err
		}
		if replay != nil {
			return json.Unmarshal(replay, &out)
		}
		rev, err := changeRev(ctx, tx, a.UserID)
		if err != nil {
			return err
		}
		id, err := store.InsertLabel(ctx, tx, a.UserID, name, rev)
		if err != nil {
			return domainError(err)
		}
		out = Label{ID: id, Name: name, Rev: uint64(rev)}
		if err := s.emitChange(ctx, tx, a.UserID, rev, syncmodel.LabelAdded, "label", labelDefinition(out)); err != nil {
			return err
		}
		return store.FinishOp(ctx, tx, a.PrincipalID, opID, out)
	})
	if err != nil {
		return Label{}, err
	}
	return out, nil
}

// RenameLabel renames a live definition. The rename is a definition lifecycle
// event, so it re-emits label.added carrying the renamed object.
func (s *Service) RenameLabel(ctx context.Context, a Actor, id int64, name, opID string) (Label, error) {
	if err := s.checkLabelWriter(ctx, a, name, opID); err != nil {
		return Label{}, err
	}
	if id <= 0 {
		return Label{}, fmt.Errorf("label id: %w", ErrInvalid)
	}
	var out Label
	err := s.withEvents(ctx, func(tx pgx.Tx) error {
		if err := store.LockCore(ctx, tx); err != nil {
			return err
		}
		replay, err := replayOp(ctx, tx, a.PrincipalID, "RenameLabel", opID)
		if err != nil {
			return err
		}
		if replay != nil {
			return json.Unmarshal(replay, &out)
		}
		label, err := store.LoadLabel(ctx, tx, a.UserID, id)
		if err != nil {
			return domainError(err)
		}
		if label.Deleted {
			return ErrNotFound
		}
		rev, err := changeRev(ctx, tx, a.UserID)
		if err != nil {
			return err
		}
		if err := store.SetLabelName(ctx, tx, id, name, rev); err != nil {
			return domainError(err)
		}
		out = Label{ID: id, Name: name, Rev: uint64(rev)}
		if err := s.emitChange(ctx, tx, a.UserID, rev, syncmodel.LabelAdded, "label", labelDefinition(out)); err != nil {
			return err
		}
		return store.FinishOp(ctx, tx, a.PrincipalID, opID, out)
	})
	if err != nil {
		return Label{}, err
	}
	return out, nil
}

// DeleteLabel tombstones the definition and every active membership. Each
// membership removal is its own label.removed event, so replaying clients
// tombstone the same rows instead of inferring the cascade.
func (s *Service) DeleteLabel(ctx context.Context, a Actor, id int64, opID string) (Label, error) {
	if err := s.checkLabelWriter(ctx, a, "x", opID); err != nil {
		return Label{}, err
	}
	if id <= 0 {
		return Label{}, fmt.Errorf("label id: %w", ErrInvalid)
	}
	var out Label
	err := s.withEvents(ctx, func(tx pgx.Tx) error {
		if err := store.LockCore(ctx, tx); err != nil {
			return err
		}
		replay, err := replayOp(ctx, tx, a.PrincipalID, "DeleteLabel", opID)
		if err != nil {
			return err
		}
		if replay != nil {
			return json.Unmarshal(replay, &out)
		}
		label, err := store.LoadLabel(ctx, tx, a.UserID, id)
		if err != nil {
			return domainError(err)
		}
		if label.Deleted {
			return ErrNotFound
		}
		members, err := store.ActiveMembershipEntries(ctx, tx, a.UserID, id)
		if err != nil {
			return err
		}
		alloc, err := store.AllocateCounters(ctx, tx, 0, map[int64]int64{a.UserID: int64(len(members) + 1)})
		if err != nil {
			return err
		}
		rev := alloc.Revs[a.UserID].First
		now := s.now().UTC()
		for _, entry := range members {
			membershipID, err := store.MembershipID(ctx, tx, a.UserID, entry, id)
			if err != nil {
				return err
			}
			if err := store.TombstoneMembership(ctx, tx, a.UserID, entry, id, rev, now); err != nil {
				return err
			}
			obj := labelObject(a, membershipID, entry, label.Name, uint64(rev), true)
			if err := insertEvent(ctx, tx, a.UserID, rev, string(syncmodel.LabelRemoved), map[string]any{"object": obj, "label_id": id}, s.now().UTC()); err != nil {
				return err
			}
			rev++
		}
		if err := store.TombstoneLabel(ctx, tx, id, rev, now); err != nil {
			return err
		}
		out = Label{ID: id, Name: label.Name, Rev: uint64(rev), Deleted: true}
		if err := s.emitChange(ctx, tx, a.UserID, rev, syncmodel.LabelRemoved, "label", labelDefinition(out)); err != nil {
			return err
		}
		return store.FinishOp(ctx, tx, a.PrincipalID, opID, out)
	})
	if err != nil {
		return Label{}, err
	}
	return out, nil
}

// AddLabel assigns an entry a live label. Adding an existing membership is a
// no-op returning the current row; a tombstoned one resurrects.
func (s *Service) AddLabel(ctx context.Context, a Actor, labelID, entryID int64, opID string) (LabelMembership, error) {
	if err := s.checkLabelWriter(ctx, a, "x", opID); err != nil {
		return LabelMembership{}, err
	}
	if labelID <= 0 || entryID <= 0 {
		return LabelMembership{}, fmt.Errorf("label or entry id: %w", ErrInvalid)
	}
	var out LabelMembership
	err := s.withEvents(ctx, func(tx pgx.Tx) error {
		if err := store.LockCore(ctx, tx); err != nil {
			return err
		}
		replay, err := replayOp(ctx, tx, a.PrincipalID, "AddLabel", opID)
		if err != nil {
			return err
		}
		if replay != nil {
			return json.Unmarshal(replay, &out)
		}
		label, err := store.LoadLabel(ctx, tx, a.UserID, labelID)
		if err != nil {
			return domainError(err)
		}
		if label.Deleted {
			return ErrNotFound
		}
		visible, err := store.EntryVisible(ctx, tx, a.UserID, entryID)
		if err != nil {
			return err
		}
		if !visible {
			return ErrNotFound
		}
		rev, deleted, found, err := store.LoadMembership(ctx, tx, a.UserID, entryID, labelID)
		if err != nil {
			return err
		}
		if found && !deleted {
			membershipID, err := store.MembershipID(ctx, tx, a.UserID, entryID, labelID)
			if err != nil {
				return err
			}
			// Already a member: idempotent no-op, no new event.
			out = LabelMembership{ID: membershipID, LabelID: labelID, EntryID: entryID, Rev: uint64(rev)}
			return store.FinishOp(ctx, tx, a.PrincipalID, opID, out)
		}
		rev, err = changeRev(ctx, tx, a.UserID)
		if err != nil {
			return err
		}
		if err := store.UpsertMembership(ctx, tx, a.UserID, entryID, labelID, rev); err != nil {
			return domainError(err)
		}
		membershipID, err := store.MembershipID(ctx, tx, a.UserID, entryID, labelID)
		if err != nil {
			return err
		}
		obj := labelObject(a, membershipID, entryID, label.Name, uint64(rev), false)
		if err := insertEvent(ctx, tx, a.UserID, rev, string(syncmodel.LabelAdded), map[string]any{"object": obj, "label_id": labelID}, s.now().UTC()); err != nil {
			return err
		}
		out = LabelMembership{ID: membershipID, LabelID: labelID, EntryID: entryID, Rev: uint64(rev)}
		return store.FinishOp(ctx, tx, a.PrincipalID, opID, out)
	})
	if err != nil {
		return LabelMembership{}, err
	}
	return out, nil
}

// RemoveLabel tombstones an active membership and emits label.removed.
func (s *Service) RemoveLabel(ctx context.Context, a Actor, labelID, entryID int64, opID string) (LabelMembership, error) {
	if err := s.checkLabelWriter(ctx, a, "x", opID); err != nil {
		return LabelMembership{}, err
	}
	if labelID <= 0 || entryID <= 0 {
		return LabelMembership{}, fmt.Errorf("label or entry id: %w", ErrInvalid)
	}
	var out LabelMembership
	err := s.withEvents(ctx, func(tx pgx.Tx) error {
		if err := store.LockCore(ctx, tx); err != nil {
			return err
		}
		replay, err := replayOp(ctx, tx, a.PrincipalID, "RemoveLabel", opID)
		if err != nil {
			return err
		}
		if replay != nil {
			return json.Unmarshal(replay, &out)
		}
		label, err := store.LoadLabel(ctx, tx, a.UserID, labelID)
		if err != nil {
			return domainError(err)
		}
		if label.Deleted {
			return ErrNotFound
		}
		_, deleted, found, err := store.LoadMembership(ctx, tx, a.UserID, entryID, labelID)
		if err != nil {
			return err
		}
		if !found || deleted {
			return ErrNotFound
		}
		rev, err := changeRev(ctx, tx, a.UserID)
		if err != nil {
			return err
		}
		if err := store.TombstoneMembership(ctx, tx, a.UserID, entryID, labelID, rev, s.now().UTC()); err != nil {
			return err
		}
		membershipID, err := store.MembershipID(ctx, tx, a.UserID, entryID, labelID)
		if err != nil {
			return err
		}
		obj := labelObject(a, membershipID, entryID, label.Name, uint64(rev), true)
		if err := insertEvent(ctx, tx, a.UserID, rev, string(syncmodel.LabelRemoved), map[string]any{"object": obj, "label_id": labelID}, s.now().UTC()); err != nil {
			return err
		}
		out = LabelMembership{ID: membershipID, LabelID: labelID, EntryID: entryID, Rev: uint64(rev), Deleted: true}
		return store.FinishOp(ctx, tx, a.PrincipalID, opID, out)
	})
	if err != nil {
		return LabelMembership{}, err
	}
	return out, nil
}
