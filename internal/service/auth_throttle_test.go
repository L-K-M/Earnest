package service

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/L-K-M/Earnest/internal/store/storetest"
)

func TestAuthenticateDoesNotLockOutSuccessfulLogins(t *testing.T) {
	db := storetest.NewTemplate(t).New(t)
	ctx := context.Background()
	now := time.Now().UTC()
	a := NewAuth(db, func() time.Time { return now }, []byte(strings.Repeat("k", 32)))
	if _, err := a.CreateUser(ctx, "alice", "correct horse"); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < loginLimit+2; i++ {
		if _, err := a.Authenticate(ctx, "alice", "correct horse", "client-1"); err != nil {
			t.Fatalf("successful login %d locked out: %v", i+1, err)
		}
	}
	// A success forgives prior failures.
	for i := 0; i < loginLimit-1; i++ {
		if _, err := a.Authenticate(ctx, "alice", "wrong", "client-2"); !errors.Is(err, ErrUnauthorized) {
			t.Fatalf("failure %d: %v", i+1, err)
		}
	}
	if _, err := a.Authenticate(ctx, "alice", "correct horse", "client-2"); err != nil {
		t.Fatalf("success after failures: %v", err)
	}
	if _, err := a.Authenticate(ctx, "alice", "wrong", "client-2"); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("failure after success: %v", err)
	}
	// Repeated failures still lock out.
	for i := 0; i < loginLimit-1; i++ {
		if _, err := a.Authenticate(ctx, "alice", "wrong", "client-2"); !errors.Is(err, ErrUnauthorized) {
			t.Fatalf("lockout failure %d: %v", i+1, err)
		}
	}
	if _, err := a.Authenticate(ctx, "alice", "wrong", "client-2"); !errors.Is(err, ErrRateLimited) {
		t.Fatalf("lockout: %v", err)
	}
	// An unrelated user and client are unaffected.
	if _, err := a.CreateUser(ctx, "bob", "correct pony"); err != nil {
		t.Fatal(err)
	}
	if _, err := a.Authenticate(ctx, "bob", "correct pony", "client-3"); err != nil {
		t.Fatalf("unrelated login blocked: %v", err)
	}
	// Lockouts expire once the window elapses.
	now = now.Add(loginWindow + time.Second)
	if _, err := a.Authenticate(ctx, "alice", "correct horse", "client-2"); err != nil {
		t.Fatalf("lockout did not expire: %v", err)
	}
}
