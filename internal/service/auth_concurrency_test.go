package service

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/L-K-M/Earnest/internal/store/storetest"
)

func TestLoginReservationOutcomes(t *testing.T) {
	auth := NewAuth(nil, nil, nil)
	for _, outcome := range []loginOutcome{loginFailed, loginSucceeded, loginAborted} {
		if !auth.startLogin("alice", "client") {
			t.Fatal("unexpected rate limit")
		}
		auth.finishLogin("alice", "client", outcome)
		if len(auth.pendingLogins) != 0 {
			t.Fatalf("reservation leaked after %d: %v", outcome, auth.pendingLogins)
		}
	}
	if auth.attempts["u:alice"].count != 0 || auth.attempts["c:client"].count != 1 {
		t.Fatalf("success must forgive only the account; abort must not count: %v", auth.attempts)
	}
}

func TestLoginReservationsSurviveWindowExpiry(t *testing.T) {
	now := time.Now()
	auth := NewAuth(nil, func() time.Time { return now }, nil)
	for range loginLimit {
		if !auth.startLogin("alice", "client") {
			t.Fatal("unexpected rate limit")
		}
	}
	now = now.Add(loginWindow + time.Second)
	if auth.startLogin("alice", "other-client") || auth.startLogin("bob", "client") {
		t.Fatal("window expiry must not discard in-flight username or client slots")
	}
	if !auth.startLogin("bob", "other-client") {
		t.Fatal("unrelated logins must not wait for another account's checks")
	}
	auth.finishLogin("bob", "other-client", loginAborted)
	for range loginLimit {
		auth.finishLogin("alice", "client", loginAborted)
	}
	if len(auth.pendingLogins) != 0 || !auth.startLogin("alice", "client") {
		t.Fatal("released slots must become available")
	}
	auth.finishLogin("alice", "client", loginAborted)
}

func TestCanceledLoginsReleaseReservations(t *testing.T) {
	db := storetest.NewTemplate(t).New(t)
	auth := NewAuth(db, nil, nil)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	for range loginLimit + 2 {
		if _, err := auth.Authenticate(ctx, "missing", "wrong", "client"); !errors.Is(err, context.Canceled) {
			t.Fatalf("aborted web login: %v", err)
		}
		if _, err := auth.CompatLogin(ctx, "missing", "wrong", "client"); !errors.Is(err, context.Canceled) {
			t.Fatalf("aborted compatibility login: %v", err)
		}
	}
	if len(auth.pendingLogins) != 0 || len(auth.attempts) != 0 {
		t.Fatalf("database errors must release slots without counting failures: pending=%v, failures=%v", auth.pendingLogins, auth.attempts)
	}
}

func TestConcurrentLoginsRespectFailureLimit(t *testing.T) {
	template := storetest.NewTemplate(t)
	for _, scope := range []string{"username", "client"} {
		t.Run(scope, func(t *testing.T) {
			db := template.New(t)
			auth := NewAuth(db, nil, nil)
			ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
			defer cancel()
			tx, err := db.Pool().Begin(ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer tx.Rollback(context.Background())
			// Block both login queries before hashing. Every attempt must
			// either reserve a slot and wait here, or be rate limited. This
			// avoids depending on Argon2 speed or goroutine scheduling.
			if _, err := tx.Exec(ctx, "LOCK TABLE users IN ACCESS EXCLUSIVE MODE"); err != nil {
				t.Fatal(err)
			}
			const attempts = loginLimit + 2
			results := make(chan error, attempts)
			for i := range attempts {
				go func() {
					username, client := "missing", "same-client"
					if scope == "username" {
						client = fmt.Sprintf("client-%d", i)
					} else {
						username = fmt.Sprintf("missing-%d", i)
					}
					// The web and Google Reader endpoints must share both
					// the in-flight allowance and the recorded failures.
					if i%2 == 0 {
						_, err := auth.Authenticate(ctx, username, "wrong", client)
						results <- err
					} else {
						_, err := auth.CompatLogin(ctx, username, "wrong", client)
						results <- err
					}
				}()
			}
			poll := time.NewTicker(5 * time.Millisecond)
			defer poll.Stop()
			for {
				var blocked int
				err := db.Pool().QueryRow(ctx, `SELECT count(*) FROM pg_stat_activity
 WHERE datname=current_database() AND wait_event_type='Lock'
 AND query LIKE 'SELECT u.id,p.id,%'`).Scan(&blocked)
				if err != nil {
					t.Fatal(err)
				}
				if blocked+len(results) == attempts {
					break
				}
				select {
				case <-ctx.Done():
					t.Fatal("login attempts did not reach the query or rate limiter")
				case <-poll.C:
				}
			}
			if err := tx.Rollback(ctx); err != nil {
				t.Fatal(err)
			}
			var checked, limited int
			for range attempts {
				select {
				case err := <-results:
					switch {
					case errors.Is(err, ErrUnauthorized):
						checked++
					case errors.Is(err, ErrRateLimited):
						limited++
					default:
						t.Errorf("unexpected login result: %v", err)
					}
				case <-ctx.Done():
					t.Fatal("login attempt did not finish")
				}
			}
			if checked != loginLimit || limited != attempts-loginLimit {
				t.Fatalf("checked %d passwords, limited %d; want %d checks and %d refusals", checked, limited, loginLimit, attempts-loginLimit)
			}
		})
	}
}
