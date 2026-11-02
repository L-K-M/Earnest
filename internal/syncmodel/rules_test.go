package syncmodel

import (
	"errors"
	"testing"
	"time"
)

func testTime() time.Time { return time.Date(2026, 11, 2, 12, 0, 0, 0, time.UTC) }
func testModel(t *testing.T) (*Model, Context) {
	t.Helper()
	m, err := New("g1")
	if err != nil {
		t.Fatal(err)
	}
	ctx := Context{Actor: 1, Token: TokenHuman, Receipt: testTime()}
	if _, err := m.Ingest(Entry{ID: 1, SubscriptionID: 1}, ctx.Receipt); err != nil {
		t.Fatal(err)
	}
	return m, ctx
}
func testWrite(id string, name RegisterName, value Value) Write {
	return Write{OpID: id, Principal: 1, EntryID: 1, Register: name, Value: value, Base: Revision{Generation: "g1"}, OccurredAt: testTime(), Kind: Explicit}
}
func apply(t *testing.T, m *Model, ctx Context, w Write) Result {
	t.Helper()
	r, err := m.Batch(ctx, []Write{w})
	if err != nil {
		t.Fatal(err)
	}
	return r[0]
}

func TestSourceDerivation(t *testing.T) {
	for _, tc := range []struct {
		token  TokenKind
		kind   OperationKind
		source Source
	}{
		{TokenHuman, Explicit, SourceDeliberate}, {TokenHuman, Passive, SourcePassive}, {TokenCompat, Explicit, SourceDeliberate}, {TokenCompat, Passive, SourcePassive}, {TokenAgent, Explicit, SourceRuleOrAgent}, {TokenAgent, Passive, SourceRuleOrAgent}, {TokenRule, Explicit, SourceRuleOrAgent}, {TokenRule, Passive, SourceRuleOrAgent},
	} {
		t.Run(string(tc.token)+"/"+string(tc.kind), func(t *testing.T) {
			got, err := DeriveSource(tc.token, tc.kind)
			if err != nil || got != tc.source {
				t.Fatalf("got %q %v", got, err)
			}
		})
	}
	if _, err := DeriveSource("forged", Explicit); !errors.Is(err, ErrInvalid) {
		t.Fatal(err)
	}
	if _, err := DeriveSource(TokenAgent, "forged"); !errors.Is(err, ErrInvalid) {
		t.Fatal(err)
	}
}

func TestConflictRules(t *testing.T) {
	now := testTime()
	for _, tc := range []struct {
		name          string
		current, next Register
		base          uint64
		win           bool
	}{
		{"current base bypasses rank", Register{Value: Unseen, Rev: 2, Source: SourceDeliberate}, Register{Value: SeenValue, Rev: 3, Source: SourcePassive}, 2, true},
		{"stale deliberate beats later agent", Register{Rev: 2, Source: SourceRuleOrAgent, OccurredAt: now}, Register{Rev: 3, Source: SourceDeliberate, OccurredAt: now.Add(-time.Hour)}, 0, true},
		{"stale passive loses", Register{Rev: 2, Source: SourceDeliberate}, Register{Rev: 3, Source: SourcePassive, OccurredAt: now}, 0, false},
		{"same source older loses", Register{Rev: 2, Source: SourceDeliberate, OccurredAt: now}, Register{Rev: 3, Source: SourceDeliberate, OccurredAt: now.Add(-time.Second)}, 0, false},
		{"same time higher revision", Register{Rev: 2, Source: SourceDeliberate, OccurredAt: now}, Register{Rev: 3, Source: SourceDeliberate, OccurredAt: now}, 0, true},
		{"passive seen despite old clock", Register{Value: Unseen, Rev: 2, Source: SourcePassive, OccurredAt: now}, Register{Value: SeenValue, Rev: 3, Source: SourcePassive, OccurredAt: now.Add(-time.Hour)}, 0, true},
		{"passive unseen cannot undo seen", Register{Value: SeenValue, Rev: 2, Source: SourcePassive}, Register{Value: Unseen, Rev: 3, Source: SourcePassive, OccurredAt: now}, 0, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := wins(Seen, tc.current, tc.next, tc.base); got != tc.win {
				t.Fatalf("got %v want %v", got, tc.win)
			}
		})
	}
}

func TestClockBoundaries(t *testing.T) {
	now := testTime()
	for _, offset := range []time.Duration{-MaxQueueAge - time.Second, -MaxQueueAge, -time.Hour, 0, time.Hour, MaxFutureSkew, MaxFutureSkew + time.Second} {
		t.Run(offset.String(), func(t *testing.T) {
			want := now.Add(offset)
			if offset > 0 {
				want = now
			}
			if got := OccurredAt(now.Add(offset), now); !got.Equal(want) {
				t.Fatalf("got %v want %v", got, want)
			}
		})
	}
}

func TestBatchRejectsBeforeAnyMutation(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(*Write)
		want   error
	}{
		{"bad value", func(w *Write) { w.Value = "bogus" }, ErrInvalid},
		{"old generation", func(w *Write) { w.Base.Generation = "g0" }, ErrGeneration},
		{"future base", func(w *Write) { w.Base.Rev = 99 }, ErrInvalid},
		{"missing identity", func(w *Write) { w.OpID = "" }, ErrInvalid},
		{"invalid register", func(w *Write) { w.Register = "bogus" }, ErrInvalid},
		{"invalid classification", func(w *Write) { w.Kind = "bogus" }, ErrInvalid},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m, ctx := testModel(t)
			first := testWrite("a", Seen, SeenValue)
			second := testWrite("b", Kept, True)
			tc.change(&second)
			if _, err := m.Batch(ctx, []Write{first, second}); !errors.Is(err, tc.want) {
				t.Fatalf("got %v", err)
			}
			if m.Cursor().Rev != 1 || !m.State(1, 1).Unread() || len(m.operations) != 0 {
				t.Fatal("partial batch became visible")
			}
		})
	}
}

func TestDuplicateTargetsAndOperations(t *testing.T) {
	m, ctx := testModel(t)
	w := testWrite("a", Seen, SeenValue)
	other := w
	other.OpID = "b"
	if _, err := m.Batch(ctx, []Write{w, other}); !errors.Is(err, ErrInvalid) {
		t.Fatal(err)
	}
	other = testWrite("a", Kept, True)
	if _, err := m.Batch(ctx, []Write{w, other}); !errors.Is(err, ErrInvalid) {
		t.Fatal(err)
	}
	if m.rev != 1 {
		t.Fatal("invalid batch committed")
	}
}

func TestScopeResolution(t *testing.T) {
	for _, tc := range []struct {
		name         string
		scope        Scope
		seen1, seen2 bool
	}{
		{"subscription", Scope{Kind: ScopeSubscriptions, IDs: []ID{1}}, true, false},
		{"folder", Scope{Kind: ScopeFolder, IDs: []ID{9}}, false, true},
		{"all", Scope{Kind: ScopeAll}, true, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m, ctx := testModel(t)
			if _, err := m.Ingest(Entry{ID: 2, SubscriptionID: 2}, ctx.Receipt); err != nil {
				t.Fatal(err)
			}
			if _, err := m.PutObject(Object{Kind: SubscriptionObject, ID: 2, FolderID: 9}); err != nil {
				t.Fatal(err)
			}
			w := testWrite("range", Seen, SeenValue)
			r, err := m.Through(ctx, RangeWrite{Write: w, Scope: tc.scope, ThroughSeq: 2})
			if err != nil {
				t.Fatal(err)
			}
			if (m.State(1, 1).Seen.Value == SeenValue) != tc.seen1 || (m.State(1, 2).Seen.Value == SeenValue) != tc.seen2 {
				t.Fatalf("bad scope %+v", r)
			}
		})
	}
}

func TestChangesAndCopies(t *testing.T) {
	m, ctx := testModel(t)
	w := testWrite("range", Seen, SeenValue)
	r, err := m.Through(ctx, RangeWrite{Write: w, Scope: Scope{Kind: ScopeAll}, ThroughSeq: 1})
	if err != nil {
		t.Fatal(err)
	}
	r.Results[0].State.Seen.Value = Unseen
	r.Subscriptions[0] = 99
	changes, next, err := m.Changes(Revision{Generation: "g1"}, 1)
	if err != nil || next.Rev != 1 || len(changes) != 1 {
		t.Fatal(changes, next, err)
	}
	changes[0].Entry.Deleted = true
	changes, next, err = m.Changes(next, 1)
	if err != nil || next.Rev != 2 || changes[0].Range.Results[0].State.Seen.Value != SeenValue || changes[0].Range.Subscriptions[0] != 1 {
		t.Fatal(changes, next, err)
	}
	changes[0].Range.Results[0].State.Seen.Value = Unseen
	replay, err := m.Through(ctx, RangeWrite{Write: w, Scope: Scope{Kind: ScopeAll}, ThroughSeq: 1})
	if err != nil || !replay.Replay || replay.Results[0].State.Seen.Value != SeenValue {
		t.Fatal(replay, err)
	}
	copy := m.Clone()
	if err := copy.DeleteEntry(1); err != nil {
		t.Fatal(err)
	}
	e, _ := m.Entry(1)
	if e.Deleted {
		t.Fatal("snapshot aliases model")
	}
	if err := m.RetainAfter(1); err != nil {
		t.Fatal(err)
	}
	if _, _, err := m.Changes(Revision{Generation: "g1"}, 1); !errors.Is(err, ErrCursorGone) {
		t.Fatal(err)
	}
	if _, _, err := m.Changes(Revision{Generation: "g1", Rev: 1}, 1); err != nil {
		t.Fatal(err)
	}
}

func TestIndependentRegisterMerge(t *testing.T) {
	c := NewClient(Revision{Generation: "g1"})
	c.MergeState(State{Principal: 1, EntryID: 1, StateRev: 9, Seen: Register{Value: Unseen, Rev: 8}, Kept: Register{Value: True, Rev: 9}})
	c.MergeState(State{Principal: 1, EntryID: 1, StateRev: 7, Seen: Register{Value: SeenValue, Rev: 7}, Shelf: Register{Value: Later, Rev: 6}})
	s := c.State(1, 1)
	if s.Seen.Value != Unseen || s.Shelf.Value != Later || s.Kept.Value != True || s.StateRev != 9 {
		t.Fatal(s)
	}
}

func TestOutboxGenerationAndLateAcknowledgement(t *testing.T) {
	c := NewClient(Revision{Generation: "g1"})
	c.MergeEntry(Entry{ID: 1, Rev: 1})
	w := testWrite("a", Seen, SeenValue)
	if err := c.Queue(w); err != nil {
		t.Fatal(err)
	}
	w.OpID = "b"
	w.Value = Unseen
	if err := c.Queue(w); err != nil {
		t.Fatal(err)
	}
	c.Acknowledge([]Result{{OpID: "a", Status: Applied, State: State{Principal: 1, EntryID: 1, Seen: Register{Value: SeenValue, Rev: 2}}}})
	if len(c.Outbox()) != 1 || c.State(1, 1).Seen.Value != Unseen {
		t.Fatal("late ack erased pending replacement")
	}
	c.BeginBootstrap(Revision{Generation: "g2", Rev: 1})
	if c.Outbox()[0].Write.Base != (Revision{Generation: "g2"}) {
		t.Fatal("generation did not make intent stale")
	}
}

func TestPassiveKeptUsesClock(t *testing.T) {
	m, ctx := testModel(t)
	first := testWrite("first", Kept, True)
	first.Kind = Passive
	if apply(t, m, ctx, first).Status != Applied {
		t.Fatal("first write rejected")
	}
	next := testWrite("next", Kept, False)
	next.Kind = Passive
	ctx.Receipt = ctx.Receipt.Add(time.Minute)
	next.OccurredAt = ctx.Receipt
	if got := apply(t, m, ctx, next); got.Status != Applied {
		t.Fatal("stale passive kept write must use same-source clock", got)
	}
}

func TestRestoreCannotRetryAnOldPendingNotification(t *testing.T) {
	m, _ := New("g1")
	if _, err := m.AcceptNotification(1, "n", "g1"); err != nil {
		t.Fatal(err)
	}
	backup := m.Clone()
	if _, err := m.AttemptNotification(1, "n", testTime()); err != nil {
		t.Fatal(err)
	}
	restored, err := Restore(backup, "g2")
	if err != nil {
		t.Fatal(err)
	}
	restored.ReleaseNotifications()
	if _, err := restored.AttemptNotification(1, "n", testTime()); err == nil {
		t.Fatal("restore plus release granted a duplicate send")
	}
}

func TestDoneClearSurvivesSeenCoalescing(t *testing.T) {
	m, ctx := testModel(t)
	apply(t, m, ctx, testWrite("done", Shelf, Done))
	c := NewClient(m.Cursor())
	e, _ := m.Entry(1)
	c.MergeEntry(e)
	c.MergeState(m.State(1, 1))
	unseen := testWrite("unread", Seen, Unseen)
	if err := c.Queue(unseen); err != nil {
		t.Fatal(err)
	}
	seen := testWrite("seen-again", Seen, SeenValue)
	if err := c.Queue(seen); err != nil {
		t.Fatal(err)
	}
	if c.State(1, 1).Shelf.Value == Done {
		t.Fatal("coalescing resurrected Done after unread cleared it")
	}
}

func TestRowResolverMatchesModel(t *testing.T) {
	for _, tc := range []struct {
		name     string
		register RegisterName
		value    Value
		token    TokenKind
		kind     OperationKind
	}{
		{"deliberate unread", Seen, Unseen, TokenHuman, Explicit},
		{"passive read", Seen, SeenValue, TokenHuman, Passive},
		{"agent shelf", Shelf, Later, TokenAgent, Explicit},
		{"kept", Kept, True, TokenHuman, Explicit},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m, ctx := testModel(t)
			apply(t, m, ctx, testWrite("done", Shelf, Done))
			ctx.Token = tc.token
			w := testWrite("row", tc.register, tc.value)
			w.Kind = tc.kind
			e, _ := m.Entry(1)
			got, err := ResolveWrite(ctx, w, e, m.State(1, 1), Revision{Generation: "g1", Rev: m.rev + 1})
			if err != nil {
				t.Fatal(err)
			}
			want := apply(t, m, ctx, w)
			if got != want {
				t.Fatalf("row resolver %+v != model %+v", got, want)
			}
		})
	}
}

func TestUnknownMissingEntryIsGone(t *testing.T) {
	ctx := Context{Actor: 1, Token: TokenHuman, Receipt: testTime()}
	w := testWrite("missing", Seen, SeenValue)
	r, err := ResolveWrite(ctx, w, Entry{}, InitialState(1, 1), Revision{Generation: "g1", Rev: 1})
	if err != nil || r.Status != Gone {
		t.Fatal(r, err)
	}
}

func TestClientTombstoneCannotResurrect(t *testing.T) {
	c := NewClient(Revision{Generation: "g1"})
	c.MergeEntry(Entry{ID: 1, Rev: 1})
	c.Acknowledge([]Result{{OpID: "gone", Status: Gone, State: InitialState(1, 1)}})
	c.MergeEntry(Entry{ID: 1, Rev: 2})
	e, _ := c.Entry(1)
	if !e.Deleted {
		t.Fatal("delayed page resurrected a body known gone")
	}
}

func TestResourceSnapshotIsolationAndOrder(t *testing.T) {
	m, ctx := testModel(t)
	if _, err := m.Ingest(Entry{ID: 9, SubscriptionID: 2}, ctx.Receipt); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Ingest(Entry{ID: 3, SubscriptionID: 1}, ctx.Receipt); err != nil {
		t.Fatal(err)
	}
	r := m.Resources()
	if len(r.Entries) != 3 || r.Entries[0].ID != 1 || r.Entries[1].ID != 3 || r.Entries[2].ID != 9 {
		t.Fatal(r.Entries)
	}
	r.Entries[0].Deleted = true
	e, _ := m.Entry(1)
	if e.Deleted {
		t.Fatal("resource snapshot aliases model")
	}
}

func TestJSONValuesAndIntegerAssertions(t *testing.T) {
	for _, tc := range []struct {
		json  string
		value Value
	}{{`true`, True}, {`false`, False}, {`"seen"`, SeenValue}, {`"later"`, Later}} {
		var v Value
		if err := v.UnmarshalJSON([]byte(tc.json)); err != nil || v != tc.value {
			t.Fatal(v, err)
		}
		data, err := v.MarshalJSON()
		if err != nil || string(data) != tc.json {
			t.Fatal(string(data), err)
		}
	}
	for _, input := range []string{`"true"`, `"false"`, `null`, `123`, `{}`} {
		var v Value
		if err := v.UnmarshalJSON([]byte(input)); err == nil {
			t.Fatalf("accepted %s", input)
		}
	}
	var a, b any
	if err := decodeNumberJSON([]byte(`9007199254740992`), &a); err != nil {
		t.Fatal(err)
	}
	if err := decodeNumberJSON([]byte(`9007199254740993`), &b); err != nil {
		t.Fatal(err)
	}
	if subset(a, b, "") == nil {
		t.Fatal("assertion rounded distinct IDs")
	}
}

func TestTwoStalePassiveUnseensResolveToSeen(t *testing.T) {
	m, ctx := testModel(t)
	first := testWrite("first", Seen, Unseen)
	first.Kind = Passive
	apply(t, m, ctx, first)
	second := testWrite("second", Seen, Unseen)
	second.Kind = Passive
	result := apply(t, m, ctx, second)
	if result.State.Seen.Value != SeenValue {
		t.Fatal("two stale passive writes did not resolve to seen", result)
	}
}

func TestLaterBatchShelfSupersedesOwnUnreadSideEffect(t *testing.T) {
	m, ctx := testModel(t)
	apply(t, m, ctx, testWrite("done", Shelf, Done))
	unread := testWrite("unread", Seen, Unseen)
	shelf := testWrite("done-again", Shelf, Done)
	shelf.Base.Rev = 2
	results, err := m.Batch(ctx, []Write{unread, shelf})
	if err != nil {
		t.Fatal(err)
	}
	if results[1].Status != Applied || m.State(1, 1).Shelf.Value != Done {
		t.Fatal("later explicit shelf conflicted with own batch side effect", results)
	}
}
