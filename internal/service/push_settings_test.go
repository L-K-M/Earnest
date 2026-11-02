package service

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/L-K-M/Earnest/internal/push"
	"github.com/L-K-M/Earnest/internal/store/storetest"
	"github.com/jackc/pgx/v5"
)

func ntfySettings(op string) PushSettingsInput {
	token := "tk_test_secret"
	return PushSettingsInput{
		NtfyServer: "https://ntfy.example.net",
		NtfyTopic:  "alerts_1",
		NtfyToken:  &token,
		QuietStart: "22:00",
		QuietEnd:   "07:00",
		Timezone:   "Europe/Berlin",
		OpID:       op,
	}
}

func TestPushSettings(t *testing.T) {
	template := storetest.NewTemplate(t)

	t.Run("defaults_without_row", func(t *testing.T) {
		f := newCoreFixture(t, template)
		got, err := f.s.GetPushSettings(context.Background(), f.a)
		if err != nil {
			t.Fatal(err)
		}
		if got.Timezone != "UTC" || got.TokenSet || got.NtfyServer != "" || got.QuietStart != "" {
			t.Fatalf("defaults: %+v", got)
		}
	})

	t.Run("put_get_roundtrip_and_replay", func(t *testing.T) {
		f := newCoreFixture(t, template)
		ctx := context.Background()
		want := ntfySettings("p1")
		put, err := f.s.PutPushSettings(ctx, f.a, want)
		if err != nil {
			t.Fatal(err)
		}
		if !put.TokenSet || put.NtfyServer != want.NtfyServer || put.Timezone != want.Timezone || put.QuietStart != "22:00" {
			t.Fatalf("put: %+v", put)
		}
		replay, err := f.s.PutPushSettings(ctx, f.a, want)
		if err != nil || replay != put {
			t.Fatalf("replay: %+v %v", replay, err)
		}
		got, err := f.s.GetPushSettings(ctx, f.a)
		if err != nil {
			t.Fatal(err)
		}
		if !got.TokenSet || got.QuietEnd != "07:00" || got.ShowText {
			t.Fatalf("get: %+v", got)
		}
	})

	t.Run("token_lifecycle", func(t *testing.T) {
		f := newCoreFixture(t, template)
		ctx := context.Background()
		in := ntfySettings("t1")
		if _, err := f.s.PutPushSettings(ctx, f.a, in); err != nil {
			t.Fatal(err)
		}
		// A sealed envelope, never the token itself.
		var envelope []byte
		if err := f.db.Pool().QueryRow(ctx, "SELECT ntfy_token_encrypted FROM push_settings WHERE user_id=$1", f.a.UserID).Scan(&envelope); err != nil {
			t.Fatal(err)
		}
		if len(envelope) == 0 || strings.Contains(string(envelope), "tk_test_secret") {
			t.Fatalf("token not sealed: %x", envelope)
		}
		var worker pushSettings
		if err := f.s.store.WithTx(ctx, func(tx pgx.Tx) error {
			var err error
			worker, err = f.s.pushSettingsTx(ctx, tx, f.a.UserID)
			return err
		}); err != nil {
			t.Fatal(err)
		}
		if worker.NtfyToken != "tk_test_secret" || !worker.TokenSet {
			t.Fatalf("worker token: %+v", worker)
		}
		// Nil keeps the stored token; a fresh PUT without it must not lose it.
		keep := ntfySettings("t2")
		keep.NtfyToken = nil
		if _, err := f.s.PutPushSettings(ctx, f.a, keep); err != nil {
			t.Fatal(err)
		}
		if err := f.s.store.WithTx(ctx, func(tx pgx.Tx) error {
			var err error
			worker, err = f.s.pushSettingsTx(ctx, tx, f.a.UserID)
			return err
		}); err != nil || worker.NtfyToken != "tk_test_secret" {
			t.Fatalf("token not kept: %+v %v", worker, err)
		}
		// Empty clears it.
		empty := ""
		clear := ntfySettings("t3")
		clear.NtfyToken = &empty
		got, err := f.s.PutPushSettings(ctx, f.a, clear)
		if err != nil || got.TokenSet {
			t.Fatalf("clear: %+v %v", got, err)
		}
		var stored []byte
		if err := f.db.Pool().QueryRow(ctx, "SELECT ntfy_token_encrypted FROM push_settings WHERE user_id=$1", f.a.UserID).Scan(&stored); err != nil || stored != nil {
			t.Fatalf("token not cleared: %v %v", stored, err)
		}
		// Disabling ntfy drops the token too.
		if _, err := f.s.PutPushSettings(ctx, f.a, ntfySettings("t4")); err != nil {
			t.Fatal(err)
		}
		off := PushSettingsInput{OpID: "t5", Timezone: "UTC"}
		got, err = f.s.PutPushSettings(ctx, f.a, off)
		if err != nil || got.NtfyServer != "" || got.TokenSet {
			t.Fatalf("disable: %+v %v", got, err)
		}
		if err := f.db.Pool().QueryRow(ctx, "SELECT ntfy_token_encrypted FROM push_settings WHERE user_id=$1", f.a.UserID).Scan(&stored); err != nil || stored != nil {
			t.Fatalf("disabled token kept: %v %v", stored, err)
		}
	})

	t.Run("validation", func(t *testing.T) {
		f := newCoreFixture(t, template)
		ctx := context.Background()
		token := "tk"
		bad := []PushSettingsInput{
			{NtfyTopic: "only-topic", OpID: "v1"},                                     // topic without server
			{NtfyServer: "https://ntfy.example.net", OpID: "v2"},                      // server without topic
			{NtfyServer: "ftp://ntfy.example.net", NtfyTopic: "t", OpID: "v3"},        // scheme
			{NtfyServer: "https://u:p@ntfy.example.net", NtfyTopic: "t", OpID: "v4"},  // userinfo
			{NtfyServer: "https://ntfy.example.net/?q=1", NtfyTopic: "t", OpID: "v5"}, // query
			{NtfyServer: "https://ntfy.example.net/#f", NtfyTopic: "t", OpID: "v6"},   // fragment
			{NtfyServer: "https://ntfy.example.net", NtfyTopic: "a/b", OpID: "v7"},    // path char
			{NtfyServer: "https://ntfy.example.net", NtfyTopic: "a?b", OpID: "v8"},    // query char
			{NtfyServer: "https://ntfy.example.net", NtfyTopic: strings.Repeat("t", 65), OpID: "v9"},
			{NtfyServer: "https://ntfy.example.net", NtfyTopic: "t", NtfyToken: &token, Timezone: "Mars/Olympus", OpID: "v10"},
			{QuietStart: "22:00", OpID: "v11"},                    // unpaired quiet hour
			{QuietStart: "25:00", QuietEnd: "07:00", OpID: "v12"}, // bad clock
			{QuietStart: "2200", QuietEnd: "07:00", OpID: "v13"},  // not HH:MM
			{OpID: ""}, // missing op
		}
		for i, in := range bad {
			if _, err := f.s.PutPushSettings(ctx, f.a, in); !errors.Is(err, ErrInvalid) {
				t.Fatalf("case %d accepted: %+v", i, in)
			}
		}
		good := []PushSettingsInput{
			{OpID: "g1"}, // everything cleared
			{QuietStart: "08:00", QuietEnd: "12:30", Timezone: "UTC", OpID: "g2"},
			{NtfyServer: "http://nas.local:8090/", NtfyTopic: "t.t-1_~", OpID: "g3"}, // self-hosted http, trailing slash
			{NtfyServer: "https://ntfy.example.net", NtfyTopic: "t", NtfyToken: &token, ShowText: true, OpID: "g4"},
		}
		for i, in := range good {
			if _, err := f.s.PutPushSettings(ctx, f.a, in); err != nil {
				t.Fatalf("case %d rejected: %v", i, err)
			}
		}
	})

	t.Run("rev_increments", func(t *testing.T) {
		f := newCoreFixture(t, template)
		ctx := context.Background()
		one, err := f.s.PutPushSettings(ctx, f.a, PushSettingsInput{OpID: "r1", Timezone: "UTC"})
		if err != nil {
			t.Fatal(err)
		}
		two, err := f.s.PutPushSettings(ctx, f.a, PushSettingsInput{OpID: "r2", QuietStart: "01:00", QuietEnd: "02:00", Timezone: "UTC"})
		if err != nil || two.Rev != one.Rev+1 {
			t.Fatalf("rev: %d -> %d %v", one.Rev, two.Rev, err)
		}
	})

	t.Run("notification_devices_include_ntfy", func(t *testing.T) {
		f := newCoreFixture(t, template)
		ctx := context.Background()
		if _, err := f.s.RegisterDevice(ctx, f.a, webpushReg("n1")); err != nil {
			t.Fatal(err)
		}
		if _, err := f.s.PutPushSettings(ctx, f.a, ntfySettings("n2")); err != nil {
			t.Fatal(err)
		}
		var devices []push.Device
		if err := f.s.store.WithTx(ctx, func(tx pgx.Tx) error {
			var err error
			devices, err = f.s.notificationDevicesTx(ctx, tx, f.a.UserID)
			return err
		}); err != nil {
			t.Fatal(err)
		}
		if len(devices) != 2 {
			t.Fatalf("devices: %+v", devices)
		}
		var ntfy *push.Device
		for i := range devices {
			if devices[i].Kind == push.KindNtfy {
				ntfy = &devices[i]
			}
		}
		if ntfy == nil {
			t.Fatalf("no ntfy device: %+v", devices)
		}
		if ntfy.Endpoint != "https://ntfy.example.net/alerts_1" || ntfy.Token != "tk_test_secret" || !ntfy.SelfHosted || ntfy.ShowText {
			t.Fatalf("ntfy device: %+v", ntfy)
		}
	})

	t.Run("ntfy_text_opt_in_rules", func(t *testing.T) {
		// A device only carries agent text when it opted in and the path can
		// protect it; the public ntfy.sh service never counts as self-hosted.
		for _, tc := range []struct {
			name     string
			settings PushSettingsInput
			wantText bool
		}{
			{"self-hosted opted in", PushSettingsInput{NtfyServer: "https://ntfy.example.net", NtfyTopic: "t", ShowText: true, OpID: "x1"}, true},
			{"self-hosted opted out", PushSettingsInput{NtfyServer: "https://ntfy.example.net", NtfyTopic: "t", OpID: "x2"}, false},
			{"public ntfy.sh opted in", PushSettingsInput{NtfyServer: "https://ntfy.sh", NtfyTopic: "t", ShowText: true, OpID: "x3"}, false},
			{"ntfy.sh subdomain", PushSettingsInput{NtfyServer: "https://a.ntfy.sh", NtfyTopic: "t", ShowText: true, OpID: "x4"}, false},
		} {
			f := newCoreFixture(t, template)
			ctx := context.Background()
			if _, err := f.s.PutPushSettings(ctx, f.a, tc.settings); err != nil {
				t.Fatalf("%s: %v", tc.name, err)
			}
			var devices []push.Device
			if err := f.s.store.WithTx(ctx, func(tx pgx.Tx) error {
				var err error
				devices, err = f.s.notificationDevicesTx(ctx, tx, f.a.UserID)
				return err
			}); err != nil {
				t.Fatalf("%s: %v", tc.name, err)
			}
			if len(devices) != 1 || devices[0].Kind != push.KindNtfy || devices[0].TextAllowed() != tc.wantText {
				t.Fatalf("%s: %+v", tc.name, devices)
			}
		}
	})
}

func TestQuietUntil(t *testing.T) {
	berlin := func(start, end string) pushSettings {
		return pushSettings{PushSettings: PushSettings{QuietStart: start, QuietEnd: end, Timezone: "Europe/Berlin"}}
	}
	at := func(y int, m time.Month, d, h, min int) time.Time {
		return time.Date(y, m, d, h, min, 0, 0, time.UTC)
	}
	for _, tc := range []struct {
		name string
		ps   pushSettings
		now  time.Time
		// want is nil for "send now"; otherwise the expected hold end.
		want *time.Time
	}{
		{"no quiet hours", berlin("", ""), at(2026, 11, 2, 12, 0), nil},
		{"equal ends disabled", berlin("22:00", "22:00"), at(2026, 11, 2, 21, 0), nil},
		{"same-day inside", berlin("09:00", "17:00"), at(2026, 11, 2, 9, 0), ptrTime(at(2026, 11, 2, 16, 0))}, // 10:00 CET -> 17:00 CET = 16:00 UTC
		{"same-day before", berlin("09:00", "17:00"), at(2026, 11, 2, 6, 0), nil},
		{"same-day after", berlin("09:00", "17:00"), at(2026, 11, 2, 17, 0), nil},
		{"overnight evening", berlin("22:00", "07:00"), at(2026, 11, 2, 21, 30), ptrTime(at(2026, 11, 3, 6, 0))}, // 22:30 CET -> tomorrow 07:00 CET = 06:00 UTC
		{"overnight morning", berlin("22:00", "07:00"), at(2026, 11, 3, 5, 0), ptrTime(at(2026, 11, 3, 6, 0))},   // 06:00 CET -> today 07:00 CET = 06:00 UTC
		{"overnight midday", berlin("22:00", "07:00"), at(2026, 11, 3, 12, 0), nil},
		{"overnight boundary start", berlin("22:00", "07:00"), at(2026, 11, 2, 21, 0), ptrTime(at(2026, 11, 3, 6, 0))},
		{"overnight boundary end", berlin("22:00", "07:00"), at(2026, 11, 3, 6, 0), nil}, // exactly 07:00 CET is outside
		{"utc timezone", pushSettings{PushSettings: PushSettings{QuietStart: "22:00", QuietEnd: "07:00", Timezone: "UTC"}}, at(2026, 11, 2, 22, 30), ptrTime(at(2026, 11, 3, 7, 0))},
		{"broken timezone", pushSettings{PushSettings: PushSettings{QuietStart: "22:00", QuietEnd: "07:00", Timezone: "Mars/Olympus"}}, at(2026, 11, 2, 22, 30), nil},
		// DST: 2026-03-29 Europe/Berlin springs forward at 02:00. 23:30 UTC is
		// 00:30 CEST, inside 22-07; the hold ends at 07:00 CEST = 05:00 UTC.
		{"dst spring", berlin("22:00", "07:00"), at(2026, 3, 28, 23, 30), ptrTime(at(2026, 3, 29, 5, 0))},
		// 2026-10-25 falls back. 23:30 UTC is 00:30 CET (UTC+1 before... actually
		// 2026-10-25 00:30 UTC is 01:30 CET? Berlin is UTC+2 in summer: at
		// 23:30 UTC Oct 24 it is 01:30 CEST Oct 25, inside quiet; end 07:00 CET
		// = 06:00 UTC after the fall-back.
		{"dst fall", berlin("22:00", "07:00"), at(2026, 10, 24, 23, 30), ptrTime(at(2026, 10, 25, 6, 0))},
	} {
		got := quietUntil(tc.ps, tc.now)
		if tc.want == nil {
			if !got.IsZero() {
				t.Fatalf("%s: held until %v", tc.name, got)
			}
			continue
		}
		if !got.Equal(*tc.want) {
			t.Fatalf("%s: got %v want %v", tc.name, got, *tc.want)
		}
	}
	// A quiet end inside a DST gap normalises instead of failing: on
	// 2026-03-29, 02:30 does not exist in Berlin.
	if got := quietUntil(berlin("22:00", "02:30"), at(2026, 3, 28, 23, 30)); got.IsZero() {
		t.Fatal("DST gap end produced no hold")
	}
}

func ptrTime(t time.Time) *time.Time { return &t }
