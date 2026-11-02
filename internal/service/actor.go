// Package service holds every domain rule. HTTP, Google Reader, MCP and
// the CLI call it; it alone calls the store and the outbound client.
package service

import (
	"slices"

	"github.com/L-K-M/Earnest/internal/syncmodel"
)

// PrincipalKind says who acts: you, an agent, or the internal pipeline.
type PrincipalKind string

const (
	PrincipalHuman    PrincipalKind = "human"
	PrincipalAgent    PrincipalKind = "agent"
	PrincipalPipeline PrincipalKind = "pipeline"
)

// Credential says how the caller authenticated. It decides the
// conflict-policy source of state writes, never the caller.
type Credential string

const (
	CredentialSession    Credential = "session"     // web cookie
	CredentialAppToken   Credential = "app_token"   // Mac, Android, CLI (device flow)
	CredentialAgentToken Credential = "agent_token" // REST and MCP agents
	CredentialCompat     Credential = "compat"      // Google Reader clients
	CredentialInternal   Credential = "internal"    // rules, pipeline, fetcher
)

// Scope is one permission. Humans hold all of them; agents hold a
// subset of AgentScopes chosen when the token is created.
type Scope string

const (
	ScopeEntriesRead      Scope = "entries:read"
	ScopeAnnotationsWrite Scope = "annotations:write"
	ScopePositionOwn      Scope = "position:own"
	ScopeNotifySend       Scope = "notify:send"
	ScopeNotifyUrgent     Scope = "notify:urgent"
	ScopeStateWriteHuman  Scope = "state:write-human"

	// Never grantable to an agent: each can cause a fetch of a URL an
	// agent influenced, or reach a destination outside the tailnet.
	ScopeStateWrite    Scope = "state:write"
	ScopeFeedsWrite    Scope = "feeds:write"
	ScopeWebhooksWrite Scope = "webhooks:write"
	ScopeDevicesWrite  Scope = "devices:write"
	ScopeTokensWrite   Scope = "tokens:write"
	ScopeAdmin         Scope = "admin"
)

// AgentScopes are the only scopes an agent token may hold.
var AgentScopes = []Scope{
	ScopeEntriesRead, ScopeAnnotationsWrite, ScopePositionOwn,
	ScopeNotifySend, ScopeNotifyUrgent, ScopeStateWriteHuman,
}

// HumanScopes are what your own sessions and app tokens hold.
var HumanScopes = []Scope{
	ScopeEntriesRead, ScopeAnnotationsWrite, ScopePositionOwn,
	ScopeNotifySend, ScopeNotifyUrgent, ScopeStateWrite,
	ScopeFeedsWrite, ScopeWebhooksWrite, ScopeDevicesWrite,
	ScopeTokensWrite, ScopeAdmin,
}

// CompatScopes are what a Google Reader client may do: read, change
// state and edit subscriptions.
var CompatScopes = []Scope{ScopeEntriesRead, ScopeStateWrite, ScopeFeedsWrite}

// Actor is the authenticated caller of a service operation. Every
// service method takes one, so scopes are enforced in one place.
type Actor struct {
	UserID      int64
	PrincipalID int64
	Kind        PrincipalKind
	Via         Credential
	TokenID     int64 // 0 for sessions and internal callers
	Scopes      []Scope
}

// Has reports whether the actor holds scope s.
func (a Actor) Has(s Scope) bool {
	return slices.Contains(a.Scopes, s)
}

// SyncTokenKind maps the credential to the sync model's token kind,
// from which the conflict-policy source is derived.
func (a Actor) SyncTokenKind() syncmodel.TokenKind {
	switch {
	case a.Kind == PrincipalAgent:
		return syncmodel.TokenAgent
	case a.Via == CredentialInternal:
		return syncmodel.TokenRule
	case a.Via == CredentialCompat:
		return syncmodel.TokenCompat
	default:
		return syncmodel.TokenHuman
	}
}
