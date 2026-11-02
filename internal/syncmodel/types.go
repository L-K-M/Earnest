// Package syncmodel defines the executable, single-user sync protocol.
// A Model is transaction-owned: callers must serialize access and persist each
// mutation and its change together. It performs no I/O and acquires no locks.
package syncmodel

import (
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

type ID int64

type Revision struct {
	Generation string `json:"generation"`
	Rev        uint64 `json:"rev"`
}

type TokenKind string

const (
	TokenHuman  TokenKind = "human"
	TokenAgent  TokenKind = "agent"
	TokenRule   TokenKind = "rule"
	TokenCompat TokenKind = "compat"
)

type OperationKind string

const (
	Explicit OperationKind = "explicit"
	Passive  OperationKind = "passive"
)

type Source string

const (
	SourcePassive     Source = "passive"
	SourceRuleOrAgent Source = "rule_or_agent"
	SourceDeliberate  Source = "deliberate"
)

// DeriveSource accepts trusted authentication and operation classification only.
// No mutation accepts a caller-supplied Source.
func DeriveSource(token TokenKind, operation OperationKind) (Source, error) {
	if operation != Explicit && operation != Passive {
		return "", ErrInvalid
	}
	switch token {
	case TokenAgent, TokenRule:
		return SourceRuleOrAgent, nil
	case TokenHuman, TokenCompat:
		if operation == Passive {
			return SourcePassive, nil
		}
		return SourceDeliberate, nil
	default:
		return "", ErrInvalid
	}
}

type RegisterName string

const (
	Seen  RegisterName = "seen"
	Shelf RegisterName = "shelf"
	Kept  RegisterName = "kept"
)

type Value string

const (
	Unseen    Value = "unseen"
	SeenValue Value = "seen"
	None      Value = "none"
	Later     Value = "later"
	Done      Value = "done"
	False     Value = "false"
	True      Value = "true"
)

// MarshalJSON keeps the kept register boolean on the wire.
func (v Value) MarshalJSON() ([]byte, error) {
	switch v {
	case True:
		return []byte("true"), nil
	case False:
		return []byte("false"), nil
	}
	return json.Marshal(string(v))
}
func (v *Value) UnmarshalJSON(data []byte) error {
	switch string(data) {
	case "true":
		*v = True
		return nil
	case "false":
		*v = False
		return nil
	}
	var text string
	if err := json.Unmarshal(data, &text); err != nil {
		return fmt.Errorf("register value: %w", err)
	}
	if text == "true" || text == "false" || string(data) == "null" {
		return ErrInvalid
	}
	*v = Value(text)
	return nil
}

// InitialState represents an absent state row, including zero register revs.
func InitialState(principal, entry ID) State {
	return State{Principal: principal, EntryID: entry, Seen: Register{Value: Unseen}, Shelf: Register{Value: None}, Kept: Register{Value: False}}
}

type Register struct {
	Value      Value     `json:"value"`
	Rev        uint64    `json:"rev"`
	OccurredAt time.Time `json:"occurred_at"`
	Actor      ID        `json:"actor"`
	Source     Source    `json:"source"`
}

type State struct {
	Principal ID       `json:"principal"`
	EntryID   ID       `json:"entry_id"`
	Seen      Register `json:"seen"`
	Shelf     Register `json:"shelf"`
	Kept      Register `json:"kept"`
	StateRev  uint64   `json:"state_rev"`
}

func (s State) Unread() bool     { return s.Seen.Value == Unseen && s.Shelf.Value != Done }
func (s State) GoogleRead() bool { return s.Seen.Value == SeenValue || s.Shelf.Value == Done }
func (s State) Register(name RegisterName) (Register, error) {
	switch name {
	case Seen:
		return s.Seen, nil
	case Shelf:
		return s.Shelf, nil
	case Kept:
		return s.Kept, nil
	}
	return Register{}, ErrInvalid
}

type Entry struct {
	ID             ID        `json:"id"`
	SubscriptionID ID        `json:"subscription_id"`
	Seq            uint64    `json:"seq"`
	FirstSeenAt    time.Time `json:"first_seen_at"`
	PublishedAt    time.Time `json:"published_at"`
	Rev            uint64    `json:"rev"`
	Deleted        bool      `json:"deleted"`
}

// Age is independent of the untrusted publisher timestamp.
func (e Entry) Age(now time.Time) time.Duration {
	if now.Before(e.FirstSeenAt) {
		return 0
	}
	return now.Sub(e.FirstSeenAt)
}

type Context struct {
	Actor   ID        `json:"actor"`
	Token   TokenKind `json:"token"`
	Receipt time.Time `json:"receipt"`
}

type Write struct {
	OpID       string        `json:"op_id"`
	Principal  ID            `json:"principal"`
	EntryID    ID            `json:"entry_id"`
	Register   RegisterName  `json:"register"`
	Value      Value         `json:"value"`
	Base       Revision      `json:"base_rev"`
	OccurredAt time.Time     `json:"occurred_at"`
	Kind       OperationKind `json:"kind"`
}

type Status string

const (
	Applied    Status = "applied"
	Superseded Status = "superseded"
	Gone       Status = "gone"
	Expired    Status = "expired"
)

type Result struct {
	OpID   string `json:"op_id"`
	Status Status `json:"status"`
	State  State  `json:"state"`
	Replay bool   `json:"replay"`
}

type ScopeKind string

const (
	ScopeAll           ScopeKind = "all"
	ScopeSubscriptions ScopeKind = "subscriptions"
	ScopeFolder        ScopeKind = "folder"
)

type Scope struct {
	Kind ScopeKind `json:"kind"`
	IDs  []ID      `json:"ids,omitempty"`
}

type RangeWrite struct {
	Write      Write  `json:"write"`
	Scope      Scope  `json:"scope"`
	ThroughSeq uint64 `json:"through_seq"`
}

type RangeResult struct {
	Status        Status   `json:"status"`
	ThroughSeq    uint64   `json:"through_seq"`
	Subscriptions []ID     `json:"subscriptions"`
	Except        []ID     `json:"except"`
	Results       []Result `json:"results"`
	Replay        bool     `json:"replay"`
}

type ChangeType string

const (
	EntryCreated        ChangeType = "entry.created"
	EntryUpdated        ChangeType = "entry.updated"
	EntryDeleted        ChangeType = "entry.deleted"
	StateUpdated        ChangeType = "state.updated"
	StateRange          ChangeType = "state.range"
	LabelAdded          ChangeType = "label.added"
	LabelRemoved        ChangeType = "label.removed"
	PositionUpdated     ChangeType = "position.updated"
	SubscriptionCreated ChangeType = "subscription.created"
	SubscriptionUpdated ChangeType = "subscription.updated"
	SubscriptionDeleted ChangeType = "subscription.deleted"
	FolderUpdated       ChangeType = "folder.updated"
	FolderDeleted       ChangeType = "folder.deleted"
	AnnotationUpdated   ChangeType = "annotation.updated"
	AnnotationDeleted   ChangeType = "annotation.deleted"
	TokenRevoked        ChangeType = "token.revoked"
)

type Change struct {
	Rev      uint64       `json:"rev"`
	Type     ChangeType   `json:"type"`
	Entry    *Entry       `json:"entry,omitempty"`
	Results  []Result     `json:"results,omitempty"`
	Range    *RangeResult `json:"range,omitempty"`
	Object   *Object      `json:"object,omitempty"`
	Position *Position    `json:"position,omitempty"`
}

const MaxQueueAge = 30 * 24 * time.Hour
const MaxFutureSkew = 24 * time.Hour

var (
	ErrInvalid    = errors.New("invalid sync operation")
	ErrGeneration = errors.New("database generation changed: re-bootstrap required")
	ErrCursorGone = errors.New("cursor outside retained log: re-bootstrap required")
	ErrWithheld   = errors.New("notifications withheld after restore")
	ErrAttempted  = errors.New("notification attempt already recorded")
)

// OccurredAt clamps future clocks without promoting an old queued action.
func OccurredAt(client, receipt time.Time) time.Time {
	if client.Sub(receipt) > MaxFutureSkew {
		return receipt.UTC()
	}
	if client.After(receipt) {
		return receipt.UTC()
	}
	return client.UTC()
}
