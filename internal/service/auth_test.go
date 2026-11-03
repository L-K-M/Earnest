package service

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/L-K-M/Earnest/internal/store/storetest"
)

func TestIdentity(t *testing.T) {
	db := storetest.NewTemplate(t).New(t)
	ctx := context.Background()
	now := time.Now().UTC()
	a := NewAuth(db, func() time.Time { return now }, []byte(strings.Repeat("k", 32)))
	var logs bytes.Buffer
	old := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, nil)))
	defer slog.SetDefault(old)
	actor, err := a.CreateUser(ctx, "alice", "correct horse")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = a.Authenticate(ctx, "alice", "wrong", "client-1"); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("wrong password: %v", err)
	}
	if _, err = a.Authenticate(ctx, "missing", "wrong", "client-2"); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("unknown user: %v", err)
	}
	if _, err = a.Authenticate(ctx, "alice", "correct horse", "client-1"); err != nil {
		t.Fatal(err)
	}
	for _, scope := range HumanScopes {
		if !containsScope(AgentScopes, scope) {
			_, err := a.CreateToken(ctx, actor, TokenOptions{Kind: TokenAgent, Name: "agent", Audience: AudienceMCP, Scopes: []Scope{scope}})
			if !errors.Is(err, ErrInvalid) {
				t.Errorf("scope %s: %v", scope, err)
			}
		}
	}
	issued, err := a.CreateToken(ctx, actor, TokenOptions{Kind: TokenAgent, Name: "agent", Audience: AudienceMCP, Scopes: []Scope{ScopeEntriesRead}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = a.ResolveToken(ctx, issued.Secret, AudienceAPI); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("audience: %v", err)
	}
	agent, err := a.ResolveToken(ctx, issued.Secret, AudienceMCP)
	if err != nil || agent.Kind != PrincipalAgent {
		t.Fatalf("resolve: %+v %v", agent, err)
	}
	if _, err = a.CreateToken(ctx, agent, TokenOptions{Kind: TokenAgent}); !errors.Is(err, ErrForbidden) {
		t.Fatalf("agent creation: %v", err)
	}
	if err = a.RevokeToken(ctx, actor, issued.Token.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = a.ResolveToken(ctx, issued.Secret, AudienceMCP); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("revoked: %v", err)
	}
	expiry := now.Add(time.Minute)
	expiring, err := a.CreateToken(ctx, actor, TokenOptions{Kind: TokenApp, Name: "app", Audience: AudienceAPI, ExpiresAt: &expiry})
	if err != nil {
		t.Fatal(err)
	}
	now = expiry
	if _, err = a.ResolveToken(ctx, expiring.Secret, AudienceAPI); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("expired: %v", err)
	}
	password, err := a.SetCompatPassword(ctx, actor)
	if err != nil {
		t.Fatal(err)
	}
	compat, err := a.CompatLogin(ctx, "alice", password, "compat-client")
	if err != nil {
		t.Fatal(err)
	}
	resolved, err := a.ResolveCompatToken(ctx, compat.Secret)
	if err != nil || resolved.Via != CredentialCompat {
		t.Fatalf("compat: %+v %v", resolved, err)
	}
	for _, secret := range []string{"correct horse", password, issued.Secret, compat.Secret} {
		if strings.Contains(logs.String(), secret) {
			t.Error("secret appeared in log")
		}
	}
}

func containsScope(scopes []Scope, s Scope) bool {
	for _, v := range scopes {
		if v == s {
			return true
		}
	}
	return false
}

func TestPasswordHash(t *testing.T) {
	hash, err := passwordHash("example password")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(hash, "$argon2id$v=19$") || !checkPassword(hash, "example password") || checkPassword(hash, "wrong") {
		t.Fatal("password verification failed")
	}
	for _, bad := range []string{"", "$argon2id$v=19$m=1,t=1,p=1$bad$bad", hash + "$extra"} {
		if checkPassword(bad, "example password") {
			t.Errorf("accepted malformed hash %q", bad)
		}
	}
}

func TestSessionAndDevice(t *testing.T) {
	db := storetest.NewTemplate(t).New(t)
	ctx := context.Background()
	now := time.Now().UTC()
	a := NewAuth(db, func() time.Time { return now }, nil)
	actor, err := a.CreateUser(ctx, "web", "password")
	if err != nil {
		t.Fatal(err)
	}
	session, err := a.CreateSession(ctx, actor)
	if err != nil {
		t.Fatal(err)
	}
	if err = a.VerifyCSRF(ctx, session.ID, "wrong"); !errors.Is(err, ErrForbidden) {
		t.Fatalf("CSRF mismatch: %v", err)
	}
	if err = a.VerifyCSRF(ctx, session.ID, session.CSRF); err != nil {
		t.Fatal(err)
	}
	logged, err := a.ResolveSession(ctx, session.ID)
	if err != nil {
		t.Fatal(err)
	}
	device, err := a.StartDeviceAuth(ctx, "phone")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = a.PollDevice(ctx, device.DeviceCode); !errors.Is(err, ErrAuthorizationPending) {
		t.Fatalf("pending: %v", err)
	}
	if _, err = a.PollDevice(ctx, device.DeviceCode); !errors.Is(err, ErrSlowDown) {
		t.Fatalf("slow: %v", err)
	}
	if err = a.ApproveDevice(ctx, logged, device.UserCode); err != nil {
		t.Fatal(err)
	}
	now = now.Add(10 * time.Second)
	issued, err := a.PollDevice(ctx, device.DeviceCode)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = a.ResolveToken(ctx, issued.Secret, AudienceAPI); err != nil {
		t.Fatal(err)
	}
	if _, err = a.PollDevice(ctx, device.DeviceCode); !errors.Is(err, ErrExpiredToken) {
		t.Fatalf("consumed: %v", err)
	}
	denied, err := a.StartDeviceAuth(ctx, "denied")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.Pool().Exec(ctx, "UPDATE device_codes SET status='denied' WHERE device_code_hash=$1", hashSecret(denied.DeviceCode)); err != nil {
		t.Fatal(err)
	}
	if _, err = a.PollDevice(ctx, denied.DeviceCode); !errors.Is(err, ErrAccessDenied) {
		t.Fatalf("denied: %v", err)
	}
	sessions, err := a.ListSessions(ctx, logged)
	if err != nil || len(sessions) != 1 {
		t.Fatalf("sessions: %v %v", sessions, err)
	}
	if err := a.RevokeListedSession(ctx, logged, sessions[0].IDHash); err != nil {
		t.Fatal(err)
	}
	if _, err := a.ResolveSession(ctx, session.ID); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("revoked session: %v", err)
	}
	session, err = a.CreateSession(ctx, actor)
	if err != nil {
		t.Fatal(err)
	}
	device2, err := a.StartDeviceAuth(ctx, "expired")
	if err != nil {
		t.Fatal(err)
	}
	now = now.Add(deviceLifetime)
	if _, err = a.PollDevice(ctx, device2.DeviceCode); !errors.Is(err, ErrExpiredToken) {
		t.Fatalf("expired device: %v", err)
	}
	now = now.Add(sessionIdle)
	if _, err = a.ResolveSession(ctx, session.ID); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("expired session: %v", err)
	}
}
