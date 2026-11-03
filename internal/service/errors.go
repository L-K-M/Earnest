package service

import "errors"

// Sentinel errors every surface maps to its own wire form: HTTP
// problem types, Google Reader status codes, MCP tool errors.
var (
	ErrNotFound     = errors.New("not found")
	ErrUnauthorized = errors.New("unauthorized")  // no or bad credentials
	ErrForbidden    = errors.New("forbidden")     // authenticated, lacks a scope
	ErrInvalid      = errors.New("invalid input") // wrap with the field and reason
	ErrConflict     = errors.New("conflict")      // e.g. duplicate name
	ErrGone         = errors.New("gone")          // cursor past retention or old generation
	ErrRateLimited  = errors.New("rate limited")  // budgets; wrap with details
	ErrUnavailable  = errors.New("unavailable")   // dependency down; retry later
)
