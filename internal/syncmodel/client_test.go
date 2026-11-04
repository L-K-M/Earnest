package syncmodel

import "testing"

func TestAcknowledgeRangeGone(t *testing.T) {
	c := NewClient(Revision{Generation: "g1"})
	c.MergeEntry(Entry{ID: 1, Rev: 1})
	c.MergeEntry(Entry{ID: 2, Rev: 1})
	for _, principal := range []ID{1, 9} {
		state := InitialState(principal, 1)
		state.Kept = Register{Value: True, Rev: 1}
		state.StateRev = 1
		c.MergeState(state)
	}
	r := RangeResult{Results: []Result{
		{Status: Gone, State: InitialState(1, 1)},
		{Status: Applied, State: InitialState(1, 2)},
		{Status: Gone, State: InitialState(1, 3)},
	}}
	if err := c.AcknowledgeRange("range", "g1", r); err != nil {
		t.Fatal(err)
	}
	c.MergeEntry(Entry{ID: 1, Rev: 2})
	if e, ok := c.Entry(1); !ok || !e.Deleted {
		t.Fatal("range acknowledgement did not tombstone the gone entry")
	}
	if e, ok := c.Entry(2); !ok || e.Deleted {
		t.Fatal("range acknowledgement deleted a live entry")
	}
	if _, ok := c.Entry(3); ok {
		t.Fatal("range acknowledgement created an unknown entry")
	}
	for key := range c.states {
		if key.entry != 2 {
			t.Errorf("gone entry retained state for principal %d, entry %d", key.principal, key.entry)
		}
	}
	if _, ok := c.states[stateKey{1, 2}]; !ok {
		t.Fatal("range acknowledgement discarded a live entry's state")
	}
}
