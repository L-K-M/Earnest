package service

import (
	"context"
	"crypto/ecdh"
	"encoding/base64"
	"errors"
	"strings"
	"testing"

	"github.com/L-K-M/Earnest/internal/push"
	"github.com/L-K-M/Earnest/internal/store/storetest"
	"github.com/jackc/pgx/v5"
)

var (
	// A 65-byte uncompressed P-256 point and a 16-byte auth secret,
	// base64url as the transports require.
	testP256DH = func() string {
		key, err := ecdh.P256().NewPrivateKey(append(make([]byte, 31), 1))
		if err != nil {
			panic(err)
		}
		return base64.RawURLEncoding.EncodeToString(key.PublicKey().Bytes())
	}()
	testAuth = base64.RawURLEncoding.EncodeToString(make([]byte, 16))
)

func webpushReg(op string) DeviceRegistration {
	return DeviceRegistration{
		Kind:       push.KindWebPush,
		Endpoint:   "https://push.example.net/sub/" + op,
		P256DH:     testP256DH,
		AuthSecret: testAuth,
		OpID:       op,
	}
}

func agentActor(t *testing.T, f *coreFixture, scopes []Scope) Actor {
	t.Helper()
	a := Actor{UserID: f.a.UserID, Kind: PrincipalAgent, Via: CredentialAgentToken, Scopes: scopes}
	if err := f.db.Pool().QueryRow(context.Background(), "INSERT INTO principals(user_id,kind,name) VALUES($1,'agent','Bot') RETURNING id", f.a.UserID).Scan(&a.PrincipalID); err != nil {
		t.Fatal(err)
	}
	return a
}

func TestDeviceRegistration(t *testing.T) {
	template := storetest.NewTemplate(t)

	t.Run("human_only", func(t *testing.T) {
		f := newCoreFixture(t, template)
		ctx := context.Background()
		// Even a misscoped agent token carrying devices:write is refused:
		// the kind check precedes the scope check.
		agent := agentActor(t, f, []Scope{ScopeEntriesRead, ScopeDevicesWrite})
		for _, call := range []func() error{
			func() error { _, err := f.s.RegisterDevice(ctx, agent, webpushReg("a1")); return err },
			func() error { _, err := f.s.ListDevices(ctx, agent); return err },
			func() error { return f.s.DeleteDevice(ctx, agent, 1, "a2") },
			func() error { _, err := f.s.GetPushSettings(ctx, agent); return err },
			func() error { _, err := f.s.PutPushSettings(ctx, agent, PushSettingsInput{OpID: "a3"}); return err },
		} {
			if err := call(); !errors.Is(err, ErrForbidden) {
				t.Fatalf("agent call accepted: %v", err)
			}
		}
		pipe := Actor{UserID: f.a.UserID, PrincipalID: f.a.PrincipalID, Kind: PrincipalPipeline, Via: CredentialInternal, Scopes: HumanScopes}
		if _, err := f.s.RegisterDevice(ctx, pipe, webpushReg("p1")); !errors.Is(err, ErrForbidden) {
			t.Fatalf("pipeline call accepted: %v", err)
		}
		compat := f.a
		compat.Via = CredentialCompat
		compat.Scopes = CompatScopes
		if _, err := f.s.RegisterDevice(ctx, compat, webpushReg("c1")); !errors.Is(err, ErrForbidden) {
			t.Fatalf("compat call accepted: %v", err)
		}
		humanNoScope := f.a
		humanNoScope.Scopes = []Scope{ScopeEntriesRead}
		if _, err := f.s.RegisterDevice(ctx, humanNoScope, webpushReg("h1")); !errors.Is(err, ErrForbidden) {
			t.Fatalf("unscoped call accepted: %v", err)
		}
		if _, err := f.s.RegisterDevice(ctx, Actor{Kind: PrincipalHuman, Scopes: HumanScopes}, webpushReg("z1")); !errors.Is(err, ErrUnauthorized) {
			t.Fatalf("anonymous call accepted: %v", err)
		}
	})

	t.Run("endpoint_validation", func(t *testing.T) {
		f := newCoreFixture(t, template)
		ctx := context.Background()
		cases := []struct {
			endpoint string
			want     error
		}{
			{"https://push.example.net/abc", nil},
			{"https://push.example.net:443/abc?token=x", nil},
			{"https://8.8.8.8/sub", nil},
			{"https://[2606:4700:4700::1111]/sub", nil},
			{"http://push.example.net/abc", ErrInvalid},
			{"ftp://push.example.net/abc", ErrInvalid},
			{"https://", ErrInvalid},
			{"", ErrInvalid},
			{"https://user:pass@push.example.net/abc", ErrInvalid},
			{"https://push.example.net/abc#frag", ErrInvalid},
			{"https://10.0.0.1/abc", ErrInvalid},
			{"https://127.0.0.1/abc", ErrInvalid},
			{"https://172.16.0.5/abc", ErrInvalid},
			{"https://192.168.1.1/abc", ErrInvalid},
			{"https://169.254.169.254/latest", ErrInvalid},
			{"https://100.64.0.1/abc", ErrInvalid},
			{"https://[::1]/abc", ErrInvalid},
			{"https://[fe80::1]/abc", ErrInvalid},
			{"https://[fc00::1]/abc", ErrInvalid},
			{"https://[::ffff:10.0.0.1]/abc", ErrInvalid},
			{"https://push.example.net:notaport/abc", ErrInvalid},
			{"https://push.example.net:70000/abc", ErrInvalid},
		}
		for i, tc := range cases {
			reg := webpushReg("ep" + string(rune('a'+i)))
			reg.Endpoint = tc.endpoint
			_, err := f.s.RegisterDevice(ctx, f.a, reg)
			if !errors.Is(err, tc.want) {
				t.Fatalf("endpoint %q: got %v want %v", tc.endpoint, err, tc.want)
			}
		}
	})

	t.Run("kind_and_keys_validation", func(t *testing.T) {
		f := newCoreFixture(t, template)
		ctx := context.Background()
		for _, kind := range []push.Kind{push.KindNtfy, push.KindAPNs, push.Kind("carrier-pigeon"), push.Kind("")} {
			reg := webpushReg("k1")
			reg.Kind = kind
			if _, err := f.s.RegisterDevice(ctx, f.a, reg); !errors.Is(err, ErrInvalid) {
				t.Fatalf("kind %q accepted: %v", kind, err)
			}
		}
		short := make([]byte, 64)
		wrongPrefix := append([]byte{5}, short[:63]...)
		badKeys := []DeviceRegistration{
			{Kind: push.KindWebPush, Endpoint: "https://push.example.net/a", P256DH: "not-base64!!!", AuthSecret: testAuth, OpID: "b1"},
			{Kind: push.KindWebPush, Endpoint: "https://push.example.net/a", P256DH: base64.RawURLEncoding.EncodeToString(short), AuthSecret: testAuth, OpID: "b2"},
			{Kind: push.KindWebPush, Endpoint: "https://push.example.net/a", P256DH: base64.RawURLEncoding.EncodeToString(wrongPrefix), AuthSecret: testAuth, OpID: "b3"},
			{Kind: push.KindWebPush, Endpoint: "https://push.example.net/a", P256DH: testP256DH, AuthSecret: base64.RawURLEncoding.EncodeToString(short[:15]), OpID: "b4"},
			{Kind: push.KindWebPush, Endpoint: "https://push.example.net/a", P256DH: testP256DH, AuthSecret: "", OpID: "b5"},
		}
		for _, reg := range badKeys {
			if _, err := f.s.RegisterDevice(ctx, f.a, reg); !errors.Is(err, ErrInvalid) {
				t.Fatalf("bad keys accepted: %+v", reg)
			}
		}
		reg := webpushReg("ok")
		reg.Kind = push.KindUnifiedPush
		if _, err := f.s.RegisterDevice(ctx, f.a, reg); err != nil {
			t.Fatalf("unifiedpush rejected: %v", err)
		}
	})

	t.Run("register_list_delete_and_replay", func(t *testing.T) {
		f := newCoreFixture(t, template)
		ctx := context.Background()
		d, err := f.s.RegisterDevice(ctx, f.a, webpushReg("r1"))
		if err != nil {
			t.Fatal(err)
		}
		if d.ID == 0 || d.Kind != push.KindWebPush || d.ShowText {
			t.Fatalf("registration: %+v", d)
		}
		again, err := f.s.RegisterDevice(ctx, f.a, webpushReg("r1"))
		if err != nil || again.ID != d.ID {
			t.Fatalf("op replay: %+v %v", again, err)
		}
		var count int
		if err := f.db.Pool().QueryRow(ctx, "SELECT count(*) FROM devices").Scan(&count); err != nil || count != 1 {
			t.Fatalf("replay duplicated: %d %v", count, err)
		}
		// Re-registering the same endpoint with a fresh op refreshes, not duplicates.
		fresher := webpushReg("r2")
		fresher.Endpoint = "https://push.example.net/sub/r1"
		fresher.ShowText = true
		updated, err := f.s.RegisterDevice(ctx, f.a, fresher)
		if err != nil || updated.ID != d.ID || !updated.ShowText || updated.Rev != d.Rev+1 {
			t.Fatalf("refresh: %+v want id=%d rev=%d", updated, d.ID, d.Rev+1)
		}
		list, err := f.s.ListDevices(ctx, f.a)
		if err != nil || len(list) != 1 || list[0].Endpoint != fresher.Endpoint || !list[0].ShowText {
			t.Fatalf("list: %+v %v", list, err)
		}
		if err := f.s.DeleteDevice(ctx, f.a, d.ID, "d1"); err != nil {
			t.Fatal(err)
		}
		if err := f.s.DeleteDevice(ctx, f.a, d.ID, "d1"); err != nil {
			t.Fatalf("delete replay: %v", err)
		}
		if err := f.s.DeleteDevice(ctx, f.a, d.ID, "d2"); !errors.Is(err, ErrNotFound) {
			t.Fatalf("second delete: %v", err)
		}
		if err := f.s.DeleteDevice(ctx, f.a, 0, "d3"); !errors.Is(err, ErrInvalid) {
			t.Fatalf("bad id: %v", err)
		}
	})

	t.Run("keys_stay_secret", func(t *testing.T) {
		f := newCoreFixture(t, template)
		ctx := context.Background()
		if _, err := f.s.RegisterDevice(ctx, f.a, webpushReg("s1")); err != nil {
			t.Fatal(err)
		}
		var keys string
		if err := f.db.Pool().QueryRow(ctx, "SELECT keys::text FROM devices LIMIT 1").Scan(&keys); err != nil {
			t.Fatal(err)
		}
		if strings.Contains(keys, testP256DH) || strings.Contains(keys, testAuth) {
			t.Fatalf("plaintext key material stored: %s", keys)
		}
		var devices []push.Device
		err := f.s.store.WithTx(ctx, func(tx pgx.Tx) error {
			var err error
			devices, err = f.s.notificationDevicesTx(ctx, tx, f.a.UserID)
			return err
		})
		if err != nil || len(devices) != 1 {
			t.Fatalf("notification devices: %v %v", devices, err)
		}
		if devices[0].P256DH != testP256DH || devices[0].AuthSecret != testAuth {
			t.Fatalf("keys did not round-trip: %+v", devices[0])
		}
	})
}
