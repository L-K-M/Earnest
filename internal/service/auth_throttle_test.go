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
	for i := 0; i < loginLimit; i++ {
		if _, err := a.Authenticate(ctx, "alice", "wrong", "client-2"); !errors.Is(err, ErrUnauthorized) {
			t.Fatalf("failure %d: %v", i+1, err)
		}
	}
	if _, err := a.Authenticate(ctx, "alice", "wrong", "client-2"); !errors.Is(err, ErrRateLimited) {
		t.Fatalf("lockout: %v", err)
	}
}
