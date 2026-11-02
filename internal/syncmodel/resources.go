package syncmodel

import (
	"cmp"
	"slices"
)

// Resources is an isolated bootstrap page source, ordered by stable identity.
// Production services page their database rows instead of loading a Model.
type Resources struct {
	Entries   []Entry    `json:"entries"`
	States    []State    `json:"states"`
	Objects   []Object   `json:"objects"`
	Positions []Position `json:"positions"`
}

func (m *Model) Resources() Resources {
	r := Resources{Entries: []Entry{}, States: []State{}, Objects: []Object{}, Positions: []Position{}}
	for _, e := range m.entries {
		r.Entries = append(r.Entries, e)
	}
	for _, s := range m.states {
		r.States = append(r.States, s)
	}
	for _, o := range m.objects {
		r.Objects = append(r.Objects, o)
	}
	for _, p := range m.positions {
		r.Positions = append(r.Positions, p)
	}
	slices.SortFunc(r.Entries, func(a, b Entry) int { return cmp.Compare(a.ID, b.ID) })
	slices.SortFunc(r.States, func(a, b State) int {
		if a.Principal != b.Principal {
			return cmp.Compare(a.Principal, b.Principal)
		}
		return cmp.Compare(a.EntryID, b.EntryID)
	})
	slices.SortFunc(r.Objects, func(a, b Object) int {
		if a.Kind != b.Kind {
			return cmp.Compare(a.Kind, b.Kind)
		}
		return cmp.Compare(a.ID, b.ID)
	})
	slices.SortFunc(r.Positions, func(a, b Position) int {
		if a.Principal != b.Principal {
			return cmp.Compare(a.Principal, b.Principal)
		}
		return cmp.Compare(a.View, b.View)
	})
	return r
}
