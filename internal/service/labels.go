package service

import (
	"context"
	"encoding/json"
	"errors"
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

// dedupeIDs keeps the first occurrence of each id in input order. A batched
// upsert cannot touch one row twice, and duplicate ids carry no new intent.
func dedupeIDs(ids []int64) []int64 {
	seen := make(map[int64]bool, len(ids))
	out := make([]int64, 0, len(ids))
	for _, id := range ids {
		if !seen[id] {
			seen[id] = true
			out = append(out, id)
		}
	}
	return out
}

// labelSetResult is the committed outcome of a bulk membership mutation.
// Entries apply in input order and the first failure stops the set, so the
// memberships cover the applied prefix only; the record lets a replayed
// op_id return the same partial result and error. Failed is the input index
// that stopped the set, -1 when every entry was processed.
type labelSetResult struct {
	Memberships []LabelMembership `json:"memberships"`
	Failed      int               `json:"failed"`
	NotFound    bool              `json:"not_found,omitempty"`
}

func (r labelSetResult) err() error {
	switch {
	case r.Failed < 0:
		return nil
	case r.NotFound:
		return ErrNotFound
	default:
		return fmt.Errorf("label or entry id: %w", ErrInvalid)
	}
}

// AddLabels assigns a live label to each entry inside one locked mutation and
// one op record, emitting the same per-entry label.added events a sequential
// AddLabel loop emits. Entries apply in order; the first entry that would
// fail AddLabel stops the set with the earlier changes still committed, so a
// returned error accompanies a committed prefix and reports that first
// failure. Memberships holds one row per processed entry, including
// already-member no-ops. A replayed op_id returns the recorded outcome.
func (s *Service) AddLabels(ctx context.Context, a Actor, labelID int64, entryIDs []int64, opID string) ([]LabelMembership, error) {
	if err := s.checkLabelWriter(ctx, a, "x", opID); err != nil {
		return nil, err
	}
	if labelID <= 0 {
		return nil, fmt.Errorf("label id: %w", ErrInvalid)
	}
	entryIDs = dedupeIDs(entryIDs)
	if len(entryIDs) == 0 {
		return []LabelMembership{}, nil
	}
	var result labelSetResult
	err := s.withEvents(ctx, func(tx pgx.Tx) error {
		if err := store.LockCore(ctx, tx); err != nil {
			return err
		}
		replay, err := replayOp(ctx, tx, a.PrincipalID, "AddLabels", opID)
		if err != nil {
			return err
		}
		if replay != nil {
			return json.Unmarshal(replay, &result)
		}
		result, err = s.addLabelsTx(ctx, tx, a, labelID, entryIDs)
		if err != nil {
			return err
		}
		return store.FinishOp(ctx, tx, a.PrincipalID, opID, result)
	})
	if err != nil {
		return nil, err
	}
	return result.Memberships, result.err()
}

// RemoveLabels tombstones every listed membership inside one locked mutation
// and one op record, emitting the same per-entry label.removed events a
// sequential RemoveLabel loop emits for a caller ignoring ErrNotFound: a
// missing label and missing or tombstoned memberships are skipped. A
// nonpositive entry id still stops the set with earlier removals committed,
// matching that loop. A replayed op_id returns the recorded outcome.
func (s *Service) RemoveLabels(ctx context.Context, a Actor, labelID int64, entryIDs []int64, opID string) ([]LabelMembership, error) {
	if err := s.checkLabelWriter(ctx, a, "x", opID); err != nil {
		return nil, err
	}
	if labelID <= 0 {
		return nil, fmt.Errorf("label id: %w", ErrInvalid)
	}
	entryIDs = dedupeIDs(entryIDs)
	if len(entryIDs) == 0 {
		return []LabelMembership{}, nil
	}
	var result labelSetResult
	err := s.withEvents(ctx, func(tx pgx.Tx) error {
		if err := store.LockCore(ctx, tx); err != nil {
			return err
		}
		replay, err := replayOp(ctx, tx, a.PrincipalID, "RemoveLabels", opID)
		if err != nil {
			return err
		}
		if replay != nil {
			return json.Unmarshal(replay, &result)
		}
		result, err = s.removeLabelsTx(ctx, tx, a, labelID, entryIDs)
		if err != nil {
			return err
		}
		return store.FinishOp(ctx, tx, a.PrincipalID, opID, result)
	})
	if err != nil {
		return nil, err
	}
	return result.Memberships, result.err()
}

// addLabelsTx runs the AddLabels mutation inside the locked transaction.
// Within one entry the id check precedes the label lookup and the visibility
// check, matching AddLabel's order, so the first failing entry decides both
// the applied prefix and the reported error.
func (s *Service) addLabelsTx(ctx context.Context, tx pgx.Tx, a Actor, labelID int64, entryIDs []int64) (labelSetResult, error) {
	result := labelSetResult{Memberships: []LabelMembership{}, Failed: -1}
	limit := len(entryIDs)
	for i, id := range entryIDs {
		if id <= 0 {
			limit = i
			break
		}
	}
	label, err := store.LoadLabel(ctx, tx, a.UserID, labelID)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return result, domainError(err)
	}
	if err != nil || label.Deleted {
		result.Failed = 0
		result.NotFound = limit > 0
		return result, nil
	}
	ids := entryIDs[:limit]
	visible, err := store.VisibleEntries(ctx, tx, a.UserID, ids)
	if err != nil {
		return result, err
	}
	members, err := store.LoadMemberships(ctx, tx, a.UserID, labelID, ids)
	if err != nil {
		return result, err
	}
	resolved := map[int64]LabelMembership{}
	var changed []int64
	processed := limit
	for i, id := range ids {
		if !visible[id] {
			processed = i
			result.Failed = i
			result.NotFound = true
			break
		}
		if _, dup := resolved[id]; dup {
			continue
		}
		if m, found := members[id]; found && !m.Deleted {
			// Already a member: idempotent no-op, no new event.
			resolved[id] = LabelMembership{ID: m.ID, LabelID: labelID, EntryID: id, Rev: uint64(m.Rev)}
			continue
		}
		resolved[id] = LabelMembership{LabelID: labelID, EntryID: id}
		changed = append(changed, id)
	}
	if result.Failed < 0 && limit < len(entryIDs) {
		result.Failed = limit
	}
	if len(changed) > 0 {
		alloc, err := store.AllocateCounters(ctx, tx, 0, map[int64]int64{a.UserID: int64(len(changed))})
		if err != nil {
			return result, err
		}
		base := alloc.Revs[a.UserID].First
		revs := make([]int64, len(changed))
		for i := range revs {
			revs[i] = base + int64(i)
		}
		identities, err := store.UpsertMemberships(ctx, tx, a.UserID, labelID, changed, revs)
		if err != nil {
			return result, domainError(err)
		}
		payloads := make([]json.RawMessage, len(changed))
		for i, entry := range changed {
			mID := identities[entry]
			resolved[entry] = LabelMembership{ID: mID, LabelID: labelID, EntryID: entry, Rev: uint64(revs[i])}
			obj := labelObject(a, mID, entry, label.Name, uint64(revs[i]), false)
			payloads[i], err = json.Marshal(map[string]any{"object": obj, "label_id": labelID})
			if err != nil {
				return result, err
			}
		}
		if err := insertEvents(ctx, tx, a.UserID, revs, string(syncmodel.LabelAdded), payloads, s.now().UTC()); err != nil {
			return result, err
		}
	}
	for i := 0; i < processed; i++ {
		result.Memberships = append(result.Memberships, resolved[ids[i]])
	}
	return result, nil
}

// removeLabelsTx runs the RemoveLabels mutation inside the locked transaction.
func (s *Service) removeLabelsTx(ctx context.Context, tx pgx.Tx, a Actor, labelID int64, entryIDs []int64) (labelSetResult, error) {
	result := labelSetResult{Memberships: []LabelMembership{}, Failed: -1}
	limit := len(entryIDs)
	for i, id := range entryIDs {
		if id <= 0 {
			limit = i
			break
		}
	}
	label, err := store.LoadLabel(ctx, tx, a.UserID, labelID)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return result, domainError(err)
	}
	if err == nil && !label.Deleted {
		ids := entryIDs[:limit]
		members, err := store.LoadMemberships(ctx, tx, a.UserID, labelID, ids)
		if err != nil {
			return result, err
		}
		seen := map[int64]bool{}
		var changed []int64
		for _, id := range ids {
			if seen[id] {
				continue
			}
			seen[id] = true
			if m, found := members[id]; found && !m.Deleted {
				changed = append(changed, id)
			}
		}
		if len(changed) > 0 {
			alloc, err := store.AllocateCounters(ctx, tx, 0, map[int64]int64{a.UserID: int64(len(changed))})
			if err != nil {
				return result, err
			}
			base := alloc.Revs[a.UserID].First
			revs := make([]int64, len(changed))
			for i := range revs {
				revs[i] = base + int64(i)
			}
			at := s.now().UTC()
			if err := store.TombstoneMemberships(ctx, tx, a.UserID, labelID, changed, revs, at); err != nil {
				return result, err
			}
			payloads := make([]json.RawMessage, len(changed))
			for i, entry := range changed {
				m := members[entry]
				result.Memberships = append(result.Memberships, LabelMembership{ID: m.ID, LabelID: labelID, EntryID: entry, Rev: uint64(revs[i]), Deleted: true})
				obj := labelObject(a, m.ID, entry, label.Name, uint64(revs[i]), true)
				payloads[i], err = json.Marshal(map[string]any{"object": obj, "label_id": labelID})
				if err != nil {
					return result, err
				}
			}
			if err := insertEvents(ctx, tx, a.UserID, revs, string(syncmodel.LabelRemoved), payloads, at); err != nil {
				return result, err
			}
		}
	}
	if limit < len(entryIDs) {
		result.Failed = limit
	}
	return result, nil
}
