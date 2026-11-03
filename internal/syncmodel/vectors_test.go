package syncmodel

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"
)

type vector struct {
	Version    int    `json:"version"`
	Name       string `json:"name"`
	Decision   string `json:"decision"`
	Setup      []step `json:"setup"`
	Operations []step `json:"operations"`
	Final      []step `json:"final"`
}

type step struct {
	Op         string          `json:"op"`
	Server     string          `json:"server,omitempty"`
	Device     string          `json:"device,omitempty"`
	Name       string          `json:"name,omitempty"`
	At         time.Time       `json:"at,omitempty"`
	Actor      ID              `json:"actor,omitempty"`
	Token      TokenKind       `json:"token,omitempty"`
	Entry      Entry           `json:"entry,omitempty"`
	State      State           `json:"state,omitempty"`
	Write      Write           `json:"write,omitempty"`
	Writes     []Write         `json:"writes,omitempty"`
	Scope      Scope           `json:"scope,omitempty"`
	Through    *uint64         `json:"through_seq,omitempty"`
	Timestamp  *time.Time      `json:"ts,omitempty"`
	Action     GoogleAction    `json:"action,omitempty"`
	Generation string          `json:"generation,omitempty"`
	Cursor     Revision        `json:"cursor,omitempty"`
	Rev        uint64          `json:"rev,omitempty"`
	Object     Object          `json:"object,omitempty"`
	Position   Position        `json:"position,omitempty"`
	Principal  ID              `json:"principal,omitempty"`
	EntryID    ID              `json:"entry_id,omitempty"`
	OpID       string          `json:"op_id,omitempty"`
	Count      *int            `json:"count,omitempty"`
	Repeat     int             `json:"repeat,omitempty"`
	Expect     json.RawMessage `json:"expect"`
	Error      string          `json:"error,omitempty"`
}

type captured struct {
	changes []Change
	next    Revision
}
type response struct {
	generation  string
	results     []Result
	rangeResult *RangeResult
	opID        string
}
type vectorRunner struct {
	models    map[string]*Model
	clients   map[string]*Client
	snapshots map[string]*Model
	prepared  map[string]Entry
	pages     map[string]captured
	responses map[string]response
}

var vectorFiles = []string{
	"01-range-exception.json", "02-queue-age.json", "03-retention-deleted.json", "04-source-ranks.json", "05-equal-time.json", "06-backdated-ingest.json", "07-publisher-clock.json", "08-expired-cursor.json", "09-restore-generation.json", "10-notification-response-lost.json", "11-shared-ingest.json", "12-offline-google-batch.json", "13-bootstrap-range-race.json", "14-ingest-commit-order.json", "15-repeated-operation.json", "16-google-unread-done.json", "17-independent-kept.json", "18-late-cluster-member.json", "19-notification-before-snapshot.json", "20-google-tradeoff.json", "21-bootstrap-newer-range-crash.json",
	"22-native-unread-done.json", "23-batch-atomicity.json", "24-label-tombstones.json", "25-position-live.json", "26-missing-ts.json", "27-client-coalescing.json", "28-clock-boundaries.json", "29-resource-tombstones.json", "30-principal-isolation.json", "31-queued-range-client.json", "32-coalesced-done-clear.json", "33-restore-release-safety.json", "34-integer-identity.json", "35-batch-causal-shelf.json", "36-ack-out-of-order-merge.json", "37-ack-stale-generation.json",
	"38-independent-register-merge.json",
}

func decodeVector(data []byte) (vector, error) {
	var v vector
	d := json.NewDecoder(bytes.NewReader(data))
	d.DisallowUnknownFields()
	if err := d.Decode(&v); err != nil {
		return v, err
	}
	if err := d.Decode(new(any)); err != io.EOF {
		return v, ErrInvalid
	}
	if v.Version != 1 || v.Name == "" || v.Decision == "" || len(v.Final) == 0 {
		return v, ErrInvalid
	}
	return v, nil
}

func knownVector(path string) bool { return slices.Contains(vectorFiles, filepath.Base(path)) }

func TestVectors(t *testing.T) {
	files, err := filepath.Glob("../../testdata/vectors/*.json")
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != len(vectorFiles) {
		t.Fatalf("found %d vectors, expected %d; register every new vector", len(files), len(vectorFiles))
	}
	for _, path := range files {
		t.Run(filepath.Base(path), func(t *testing.T) {
			if !knownVector(path) {
				t.Fatal("unknown vector file")
			}
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			v, err := decodeVector(data)
			if err != nil {
				t.Fatal(err)
			}
			r := vectorRunner{models: map[string]*Model{}, clients: map[string]*Client{}, snapshots: map[string]*Model{}, prepared: map[string]Entry{}, pages: map[string]captured{}, responses: map[string]response{}}
			steps := append(append(v.Setup, v.Operations...), v.Final...)
			for i, s := range steps {
				result, err := r.execute(s)
				if got := errorName(err); got != s.Error {
					t.Fatalf("step %d %s: error %q, want %q (%v)", i, s.Op, got, s.Error, err)
				}
				if len(s.Expect) == 0 {
					t.Fatalf("step %d: every step must declare expect", i)
				}
				actual, _ := json.Marshal(result)
				var got, want any
				if err := decodeNumberJSON(actual, &got); err != nil {
					t.Fatal(err)
				}
				if err := decodeNumberJSON(s.Expect, &want); err != nil {
					t.Fatal(err)
				}
				if err := subset(want, got, ""); err != nil {
					t.Fatalf("step %d %s: %v\ngot %s", i, s.Op, err, actual)
				}
			}
		})
	}
}

func decodeNumberJSON(data []byte, out *any) error {
	d := json.NewDecoder(bytes.NewReader(data))
	d.UseNumber()
	return d.Decode(out)
}

func TestUnknownVectorRejected(t *testing.T) {
	if knownVector("99-unknown.json") {
		t.Fatal("unknown file accepted")
	}
	r := vectorRunner{models: map[string]*Model{}}
	if _, err := r.execute(step{Op: "unrecognized"}); !errors.Is(err, ErrInvalid) {
		t.Fatal("unknown step accepted")
	}
	if _, err := decodeVector([]byte(`{"version":99,"name":"unknown","decision":"x","final":[{}]}`)); err == nil {
		t.Fatal("unknown version accepted")
	}
	if _, err := decodeVector([]byte(`{"version":1,"unexpected":true}`)); err == nil {
		t.Fatal("unknown field accepted")
	}
}

// Expected objects are subsets, arrays are exact and ordered. No unknown
// assertion key can pass accidentally, since every requested key must exist.
func subset(want, got any, path string) error {
	switch w := want.(type) {
	case map[string]any:
		g, ok := got.(map[string]any)
		if !ok {
			return fmt.Errorf("%s: expected object, got %v", path, got)
		}
		for key, value := range w {
			next, ok := g[key]
			if !ok {
				return fmt.Errorf("%s.%s: missing key", path, key)
			}
			if err := subset(value, next, path+"."+key); err != nil {
				return err
			}
		}
	case []any:
		g, ok := got.([]any)
		if !ok || len(g) != len(w) {
			return fmt.Errorf("%s: array mismatch: want %v, got %v", path, w, got)
		}
		for i := range w {
			if err := subset(w[i], g[i], fmt.Sprintf("%s[%d]", path, i)); err != nil {
				return err
			}
		}
	default:
		if !reflect.DeepEqual(want, got) {
			return fmt.Errorf("%s: want %v, got %v", path, want, got)
		}
	}
	return nil
}

func errorName(err error) string {
	switch {
	case err == nil:
		return ""
	case errors.Is(err, ErrGeneration):
		return "generation"
	case errors.Is(err, ErrCursorGone):
		return "cursor_gone"
	case errors.Is(err, ErrWithheld):
		return "withheld"
	case errors.Is(err, ErrAttempted):
		return "attempted"
	case errors.Is(err, ErrInvalid):
		return "invalid"
	}
	return err.Error()
}

func (r *vectorRunner) execute(s step) (any, error) {
	server := s.Server
	if server == "" {
		server = "main"
	}
	m := r.models[server]
	if m == nil {
		m, _ = New("g1")
		r.models[server] = m
	}
	ctx := Context{Actor: s.Actor, Token: s.Token, Receipt: s.At}
	if ctx.Actor == 0 {
		ctx.Actor = 1
	}
	if ctx.Token == "" {
		ctx.Token = TokenHuman
	}
	if ctx.Receipt.IsZero() {
		ctx.Receipt = time.Date(2026, 11, 2, 12, 0, 0, 0, time.UTC)
	}
	principal := s.Principal
	if principal == 0 {
		principal = 1
	}
	defaults := func(w Write) Write {
		if w.Principal == 0 {
			w.Principal = principal
		}
		if w.EntryID == 0 {
			w.EntryID = s.EntryID
		}
		if w.Base.Generation == "" {
			w.Base.Generation = m.generation
		}
		if w.OccurredAt.IsZero() {
			w.OccurredAt = ctx.Receipt
		}
		if w.Kind == "" {
			w.Kind = Explicit
		}
		return w
	}
	c := r.clients[s.Device]
	switch s.Op {
	case "ingest":
		return m.Ingest(s.Entry, ctx.Receipt)
	case "prepare_ingest":
		r.prepared[s.Name] = s.Entry
		return map[string]any{"seq": m.HighestSeq(), "rev": m.Cursor().Rev}, nil
	case "commit_ingest":
		e, ok := r.prepared[s.Name]
		if !ok {
			return nil, ErrInvalid
		}
		delete(r.prepared, s.Name)
		return m.Ingest(e, ctx.Receipt)
	case "delete":
		return map[string]any{}, m.DeleteEntry(s.EntryID)
	case "update_entry":
		return map[string]any{}, m.UpdateEntry(s.EntryID, s.Entry.PublishedAt)
	case "write":
		results, err := m.Batch(ctx, []Write{defaults(s.Write)})
		if err != nil {
			return nil, err
		}
		return results[0], nil
	case "batch", "cluster":
		writes := append([]Write{}, s.Writes...)
		for i := range writes {
			writes[i] = defaults(writes[i])
		}
		if s.Op == "cluster" {
			return m.Cluster(ctx, writes)
		}
		return m.Batch(ctx, writes)
	case "google":
		ctx.Token = TokenCompat
		w, err := GoogleWrite(m.generation, s.OpID, principal, s.EntryID, s.Action, ctx)
		if err != nil {
			return nil, err
		}
		results, err := m.Batch(ctx, []Write{w})
		if err != nil {
			return nil, err
		}
		return results[0], nil
	case "repeat_ingest":
		for i := 0; i < s.Repeat; i++ {
			e := s.Entry
			e.ID += ID(i)
			if _, err := m.Ingest(e, ctx.Receipt); err != nil {
				return nil, err
			}
		}
		return map[string]any{"rev": m.Cursor().Rev, "seq": m.HighestSeq()}, nil
	case "google_batch":
		ctx.Token = TokenCompat
		writes := []Write{}
		for i := 0; i < s.Repeat; i++ {
			w, err := GoogleWrite(m.generation, fmt.Sprintf("%s-%d", s.OpID, i), principal, s.EntryID+ID(i), s.Action, ctx)
			if err != nil {
				return nil, err
			}
			writes = append(writes, w)
		}
		results, err := m.Batch(ctx, writes)
		counts := map[Status]int{Applied: 0, Superseded: 0, Gone: 0, Expired: 0}
		for _, v := range results {
			counts[v.Status]++
		}
		return counts, err
	case "range":
		through := m.ThroughAt(s.Timestamp)
		if s.Through != nil {
			through = *s.Through
		}
		w := defaults(s.Write)
		if w.Register == "" {
			w.Register = Seen
		}
		if w.Value == "" {
			w.Value = SeenValue
		}
		return m.Through(ctx, RangeWrite{Write: w, Scope: s.Scope, ThroughSeq: through})
	case "retain":
		return map[string]any{}, m.RetainAfter(s.Rev)
	case "changes", "capture_changes":
		cursor := s.Cursor
		if cursor.Generation == "" {
			cursor.Generation = m.generation
		}
		changes, next, err := m.Changes(cursor, 100000)
		if s.Op == "capture_changes" && err == nil {
			r.pages[s.Name] = captured{changes, next}
		}
		types := []ChangeType{}
		revs := []uint64{}
		for _, c := range changes {
			types = append(types, c.Type)
			revs = append(revs, c.Rev)
		}
		return map[string]any{"types": types, "revs": revs, "next": next, "changes": changes}, err
	case "checkpoint":
		r.snapshots[s.Name] = m.Clone()
		return map[string]any{"rev": m.rev}, nil
	case "restore":
		restored, err := Restore(r.snapshots[s.Name], s.Generation)
		if err == nil {
			r.models[server] = restored
		}
		return map[string]any{}, err
	case "bootstrap_begin":
		if c == nil {
			c = NewClient(m.Cursor())
			r.clients[s.Device] = c
		}
		c.BeginBootstrap(m.Cursor())
		return c.Cursor(), nil
	case "bootstrap_page":
		if c == nil {
			return nil, ErrInvalid
		}
		resources := m.Resources()
		for _, e := range resources.Entries {
			c.MergeEntry(e)
		}
		for _, state := range resources.States {
			c.MergeState(state)
		}
		for _, o := range resources.Objects {
			c.MergeObject(o)
		}
		for _, p := range resources.Positions {
			c.MergePosition(p)
		}
		return map[string]any{}, nil
	case "client_pull":
		if c == nil {
			return nil, ErrInvalid
		}
		changes, next, err := m.Changes(c.Cursor(), 100000)
		if err != nil {
			return nil, err
		}
		err = c.ApplyPage(changes, next)
		return c.Cursor(), err
	case "client_apply":
		if c == nil {
			return nil, ErrInvalid
		}
		page, ok := r.pages[s.Name]
		if !ok {
			return nil, ErrInvalid
		}
		changes := page.changes
		if s.Count != nil {
			if *s.Count < 0 || *s.Count > len(changes) {
				return nil, ErrInvalid
			}
			changes = changes[:*s.Count]
		}
		err := c.ApplyPage(changes, page.next)
		return c.Cursor(), err
	case "client_queue":
		if c == nil {
			return nil, ErrInvalid
		}
		return map[string]any{}, c.Queue(defaults(s.Write))

	case "client_queue_range":
		if c == nil {
			return nil, ErrInvalid
		}
		through := m.ThroughAt(s.Timestamp)
		if s.Through != nil {
			through = *s.Through
		}
		w := defaults(s.Write)
		if w.Register == "" {
			w.Register = Seen
		}
		if w.Value == "" {
			w.Value = SeenValue
		}
		return map[string]any{}, c.QueueRange(RangeWrite{Write: w, Scope: s.Scope, ThroughSeq: through})
	case "client_merge_state":
		if c == nil {
			return nil, ErrInvalid
		}
		c.MergeState(s.State)
		return map[string]any{}, nil
	case "client_submit":
		if c == nil {
			return nil, ErrInvalid
		}
		pending := c.Outbox()
		if len(pending) == 0 {
			return nil, ErrInvalid
		}
		generation := c.Cursor().Generation
		if pending[0].Range != nil {
			request := *pending[0].Range
			result, err := m.Through(ctx, request)
			if err != nil {
				return nil, err
			}
			r.responses[s.Name] = response{generation: generation, rangeResult: &result, opID: request.Write.OpID}
			return result, nil
		}
		results, err := m.Batch(ctx, []Write{*pending[0].Write})
		if err != nil {
			return nil, err
		}
		r.responses[s.Name] = response{generation: generation, results: results}
		return results[0], nil
	case "client_ack":
		if c == nil {
			return nil, ErrInvalid
		}
		saved, ok := r.responses[s.Name]
		if !ok {
			return nil, ErrInvalid
		}
		if saved.rangeResult != nil {
			return map[string]any{}, c.AcknowledgeRange(saved.opID, saved.generation, *saved.rangeResult)
		}
		return map[string]any{}, c.Acknowledge(saved.generation, saved.results)
	case "client_flush":
		if c == nil {
			return nil, ErrInvalid
		}
		out := []any{}
		pending := c.Outbox()
		for len(pending) > 0 {
			if pending[0].Range != nil {
				request := *pending[0].Range
				result, err := m.Through(ctx, request)
				if err != nil {
					return nil, err
				}
				if err := c.AcknowledgeRange(request.Write.OpID, request.Write.Base.Generation, result); err != nil {
					return nil, err
				}
				out = append(out, result)
				pending = pending[1:]
				continue
			}
			writes := []Write{}
			for len(pending) > 0 && pending[0].Write != nil {
				writes = append(writes, *pending[0].Write)
				pending = pending[1:]
			}
			results, err := m.Batch(ctx, writes)
			if err != nil {
				return nil, err
			}
			if err := c.Acknowledge(writes[0].Base.Generation, results); err != nil {
				return nil, err
			}
			for _, result := range results {
				out = append(out, result)
			}
		}
		return out, nil

	case "object":
		return m.PutObject(s.Object)
	case "position":
		return m.PutPosition(s.Position)
	case "accept_notification":
		generation := s.Generation
		if generation == "" {
			generation = m.generation
		}
		return m.AcceptNotification(ctx.Actor, s.OpID, generation)
	case "attempt_notification":
		return m.AttemptNotification(ctx.Actor, s.OpID, ctx.Receipt)
	case "complete_notification":
		return map[string]any{}, m.CompleteNotification(ctx.Actor, s.OpID)
	case "release_notifications":
		m.ReleaseNotifications()
		return map[string]any{}, nil
	case "assert_state":
		state := m.State(principal, s.EntryID)
		if s.Device != "" {
			if c == nil {
				return nil, ErrInvalid
			}
			state = c.State(principal, s.EntryID)
		}
		return map[string]any{"state": state, "unread": state.Unread(), "google_read": state.GoogleRead()}, nil
	case "assert_entry":
		e, ok := m.Entry(s.EntryID)
		if s.Device != "" {
			if c == nil {
				return nil, ErrInvalid
			}
			e, ok = c.Entry(s.EntryID)
		}
		return map[string]any{"entry": e, "exists": ok, "age_seconds": e.Age(ctx.Receipt).Seconds()}, nil
	case "assert_object":
		o, ok := m.Object(s.Object.Kind, s.Object.ID)
		if s.Device != "" {
			if c == nil {
				return nil, ErrInvalid
			}
			o, ok = c.Object(s.Object.Kind, s.Object.ID)
		}
		return map[string]any{"object": o, "exists": ok}, nil
	case "assert_position":
		p, ok := m.Position(principal, s.Position.View)
		if s.Device != "" {
			if c == nil {
				return nil, ErrInvalid
			}
			p, ok = c.Position(principal, s.Position.View)
		}
		return map[string]any{"position": p, "exists": ok}, nil
	case "assert_client":
		if c == nil {
			return nil, ErrInvalid
		}
		return map[string]any{"outbox_size": len(c.Outbox()), "cursor": c.Cursor()}, nil
	case "assert_log":
		revs := []uint64{}
		types := []ChangeType{}
		gapless := true
		last := m.floor
		for _, change := range m.changes {
			revs = append(revs, change.Rev)
			types = append(types, change.Type)
			if change.Rev != last+1 {
				gapless = false
			}
			last = change.Rev
		}
		return map[string]any{"rev": m.rev, "seq": m.seq, "generation": m.generation, "revs": revs, "types": types, "gapless": gapless, "count": len(m.changes)}, nil
	default:
		return nil, fmt.Errorf("unknown step %q: %w", s.Op, ErrInvalid)
	}
}

func TestNoIOOrWallClock(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range files {
		if strings.HasSuffix(path, "_test.go") {
			continue
		}
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		for _, forbidden := range []string{"time.Now(", `"os"`, `"net/http"`, `"database/sql"`} {
			if bytes.Contains(data, []byte(forbidden)) {
				t.Errorf("%s contains %s", path, forbidden)
			}
		}
	}
}
