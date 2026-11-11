package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/L-K-M/Earnest/internal/store/storetest"
)

func readerSettingsInput(op string) ReaderSettingsInput {
	return ReaderSettingsInput{
		ReadingMode:        ReadingModeUnreadCounts,
		ShowOnlyUnread:     new(true),
		TodayDecayDays:     14,
		SingleKeyShortcuts: false,
		Theme:              ReaderThemeEink,
		Measure:            70,
		Font:               ReaderFontDyslexic,
		FeedOverrides: map[string]ReaderOverride{
			"12": {Theme: themePtr(ReaderThemeBlack)},
			"34": {Measure: intPtr(62), Font: fontPtr(ReaderFontSans)},
		},
		OpID: op,
	}
}

func themePtr(t ReaderTheme) *ReaderTheme { return &t }
func fontPtr(f ReaderFont) *ReaderFont    { return &f }
func intPtr(n int) *int                   { return &n }

func TestValidateReaderSettings(t *testing.T) {
	base := readerSettingsInput("v")
	cases := []struct {
		name    string
		mutate  func(*ReaderSettingsInput)
		wantErr bool
	}{
		{"valid", func(*ReaderSettingsInput) {}, false},
		{"valid_min", func(in *ReaderSettingsInput) {
			*in = ReaderSettingsInput{ReadingMode: ReadingModeRiver, TodayDecayDays: 1, Theme: ReaderThemeLight, Measure: 62, Font: ReaderFontSerif}
		}, false},
		{"valid_max", func(in *ReaderSettingsInput) {
			in.TodayDecayDays = 365
			in.Measure = 72
		}, false},
		{"empty_overrides", func(in *ReaderSettingsInput) { in.FeedOverrides = nil }, false},
		{"empty_override_entry", func(in *ReaderSettingsInput) {
			in.FeedOverrides = map[string]ReaderOverride{"9": {}}
		}, false},
		{"reading_mode", func(in *ReaderSettingsInput) { in.ReadingMode = "list" }, true},
		{"reading_mode_empty", func(in *ReaderSettingsInput) { in.ReadingMode = "" }, true},
		{"decay_zero", func(in *ReaderSettingsInput) { in.TodayDecayDays = 0 }, true},
		{"decay_negative", func(in *ReaderSettingsInput) { in.TodayDecayDays = -3 }, true},
		{"decay_over", func(in *ReaderSettingsInput) { in.TodayDecayDays = 366 }, true},
		{"theme", func(in *ReaderSettingsInput) { in.Theme = "sepia" }, true},
		{"measure_low", func(in *ReaderSettingsInput) { in.Measure = 61 }, true},
		{"measure_high", func(in *ReaderSettingsInput) { in.Measure = 73 }, true},
		{"font", func(in *ReaderSettingsInput) { in.Font = "comic-sans" }, true},
		{"override_key_empty", func(in *ReaderSettingsInput) {
			in.FeedOverrides = map[string]ReaderOverride{"": {Measure: intPtr(66)}}
		}, true},
		{"override_key_blank", func(in *ReaderSettingsInput) {
			in.FeedOverrides = map[string]ReaderOverride{"  ": {Measure: intPtr(66)}}
		}, true},
		{"override_key_long", func(in *ReaderSettingsInput) {
			in.FeedOverrides = map[string]ReaderOverride{strings.Repeat("k", 129): {Measure: intPtr(66)}}
		}, true},
		{"override_key_zero", func(in *ReaderSettingsInput) {
			in.FeedOverrides = map[string]ReaderOverride{"0": {Measure: intPtr(66)}}
		}, true},
		{"override_key_negative", func(in *ReaderSettingsInput) {
			in.FeedOverrides = map[string]ReaderOverride{"-1": {Measure: intPtr(66)}}
		}, true},
		{"override_key_not_id", func(in *ReaderSettingsInput) {
			in.FeedOverrides = map[string]ReaderOverride{"feed": {Measure: intPtr(66)}}
		}, true},
		{"override_key_alias", func(in *ReaderSettingsInput) {
			in.FeedOverrides = map[string]ReaderOverride{"01": {Measure: intPtr(66)}}
		}, true},
		{"override_key_overflow", func(in *ReaderSettingsInput) {
			in.FeedOverrides = map[string]ReaderOverride{"9223372036854775808": {Measure: intPtr(66)}}
		}, true},
		{"override_theme", func(in *ReaderSettingsInput) {
			in.FeedOverrides = map[string]ReaderOverride{"1": {Theme: themePtr("neon")}}
		}, true},
		{"override_measure", func(in *ReaderSettingsInput) {
			in.FeedOverrides = map[string]ReaderOverride{"1": {Measure: intPtr(80)}}
		}, true},
		{"override_font", func(in *ReaderSettingsInput) {
			in.FeedOverrides = map[string]ReaderOverride{"1": {Font: fontPtr("mono")}}
		}, true},
		{"too_many_overrides", func(in *ReaderSettingsInput) {
			in.FeedOverrides = map[string]ReaderOverride{}
			for i := 0; i <= 512; i++ {
				in.FeedOverrides[fmt.Sprintf("f%d", i)] = ReaderOverride{Measure: intPtr(66)}
			}
		}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			in := base
			tc.mutate(&in)
			err := validateReaderSettings(in)
			if tc.wantErr && !errors.Is(err, ErrInvalid) {
				t.Fatalf("want ErrInvalid, got %v", err)
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("want valid, got %v", err)
			}
		})
	}
}

func TestReaderSettings(t *testing.T) {
	template := storetest.NewTemplate(t)

	t.Run("defaults_for_fresh_user", func(t *testing.T) {
		f := newCoreFixture(t, template)
		got, err := f.s.GetSettings(context.Background(), f.a)
		if err != nil {
			t.Fatal(err)
		}
		want := DefaultReaderSettings()
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("defaults: %+v", got)
		}
	})

	t.Run("put_get_replay_and_event", func(t *testing.T) {
		f := newCoreFixture(t, template)
		ctx := context.Background()
		hints, cancel, err := f.s.WatchChanges(ctx, f.a)
		if err != nil {
			t.Fatal(err)
		}
		defer cancel()
		want := readerSettingsInput("s1")
		put, err := f.s.PutSettings(ctx, f.a, want)
		if err != nil {
			t.Fatal(err)
		}
		if put.ReadingMode != want.ReadingMode || put.TodayDecayDays != 14 || put.Theme != ReaderThemeEink ||
			put.Measure != 70 || put.Font != ReaderFontDyslexic || put.SingleKeyShortcuts || !put.ShowOnlyUnread || put.Rev == 0 {
			t.Fatalf("put: %+v", put)
		}
		if got := put.FeedOverrides["12"]; got.Theme == nil || *got.Theme != ReaderThemeBlack {
			t.Fatalf("override: %+v", put.FeedOverrides)
		}
		replay, err := f.s.PutSettings(ctx, f.a, want)
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(replay, put) {
			t.Fatalf("replay differs: %+v vs %+v", replay, put)
		}
		got, err := f.s.GetSettings(ctx, f.a)
		if err != nil || !reflect.DeepEqual(got, put) {
			t.Fatalf("get: %+v %v", got, err)
		}
		var kind string
		var payload []byte
		if err := f.db.Pool().QueryRow(ctx, "SELECT type,payload FROM changes WHERE user_id=$1 AND rev=$2", f.a.UserID, put.Rev).Scan(&kind, &payload); err != nil {
			t.Fatal(err)
		}
		if kind != "settings.updated" {
			t.Fatalf("change type %q", kind)
		}
		var body struct {
			Settings ReaderSettings `json:"settings"`
		}
		if err := json.Unmarshal(payload, &body); err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(body.Settings, put) {
			t.Fatalf("event payload: %+v", body.Settings)
		}
		select {
		case <-hints:
		case <-time.After(5 * time.Second):
			t.Fatal("no change notification")
		}
		coreCheckGapless(t, f.db, f.a.UserID)
	})

	t.Run("legacy_saves_preserve_unread_preference", func(t *testing.T) {
		f := newCoreFixture(t, template)
		ctx := context.Background()
		if _, err := f.s.PutSettings(ctx, f.a, readerSettingsInput("unread-on")); err != nil {
			t.Fatal(err)
		}
		legacy := readerSettingsInput("legacy-save")
		legacy.ShowOnlyUnread = nil
		got, err := f.s.PutSettings(ctx, f.a, legacy)
		if err != nil || !got.ShowOnlyUnread {
			t.Fatalf("legacy save cleared unread preference: %+v %v", got, err)
		}
		legacy.OpID = "unread-off"
		legacy.ShowOnlyUnread = new(false)
		got, err = f.s.PutSettings(ctx, f.a, legacy)
		if err != nil || got.ShowOnlyUnread {
			t.Fatalf("explicit false did not clear unread preference: %+v %v", got, err)
		}
	})

	t.Run("unrelated_keys_preserved", func(t *testing.T) {
		f := newCoreFixture(t, template)
		ctx := context.Background()
		if _, err := f.db.Pool().Exec(ctx, `UPDATE users SET settings='{"retention_days":200,"custom":{"x":1},"today_decay_days":"4"}' WHERE id=$1`, f.a.UserID); err != nil {
			t.Fatal(err)
		}
		got, err := f.s.GetSettings(ctx, f.a)
		if err != nil || got.TodayDecayDays != 4 {
			t.Fatalf("string days not honored: %+v %v", got, err)
		}
		in := readerSettingsInput("s1")
		in.TodayDecayDays = 9
		if _, err := f.s.PutSettings(ctx, f.a, in); err != nil {
			t.Fatal(err)
		}
		var doc map[string]json.RawMessage
		if err := f.db.Pool().QueryRow(ctx, "SELECT settings FROM users WHERE id=$1", f.a.UserID).Scan(&doc); err != nil {
			t.Fatal(err)
		}
		var retention int
		var custom map[string]int
		if err := json.Unmarshal(doc["retention_days"], &retention); err != nil || retention != 200 {
			t.Fatalf("retention_days lost: %s", doc["retention_days"])
		}
		if err := json.Unmarshal(doc["custom"], &custom); err != nil || custom["x"] != 1 {
			t.Fatalf("custom key lost: %s", doc["custom"])
		}
		if got, err := f.s.todayDecay(ctx, f.a.UserID); err != nil || got != 9*24*time.Hour {
			t.Fatalf("river decay: %v %v", got, err)
		}
	})

	t.Run("rev_increments_and_overrides_clear", func(t *testing.T) {
		f := newCoreFixture(t, template)
		ctx := context.Background()
		one, err := f.s.PutSettings(ctx, f.a, readerSettingsInput("r1"))
		if err != nil {
			t.Fatal(err)
		}
		in := readerSettingsInput("r2")
		in.FeedOverrides = nil
		two, err := f.s.PutSettings(ctx, f.a, in)
		if err != nil || two.Rev <= one.Rev {
			t.Fatalf("rev: %d -> %d %v", one.Rev, two.Rev, err)
		}
		got, err := f.s.GetSettings(ctx, f.a)
		if err != nil || len(got.FeedOverrides) != 0 {
			t.Fatalf("overrides not cleared: %+v", got)
		}
	})

	t.Run("op_id_reuse_across_methods_rejected", func(t *testing.T) {
		f := newCoreFixture(t, template)
		ctx := context.Background()
		if _, err := f.s.PutSettings(ctx, f.a, readerSettingsInput("dup")); err != nil {
			t.Fatal(err)
		}
		if _, err := f.s.PutPosition(ctx, f.a, "river", 3, "dup"); !errors.Is(err, ErrInvalid) {
			t.Fatalf("cross-method op_id accepted: %v", err)
		}
	})

	t.Run("service_validation", func(t *testing.T) {
		f := newCoreFixture(t, template)
		ctx := context.Background()
		in := readerSettingsInput("")
		if _, err := f.s.PutSettings(ctx, f.a, in); !errors.Is(err, ErrInvalid) {
			t.Fatalf("empty op_id: %v", err)
		}
		in = readerSettingsInput("bad")
		in.Theme = "neon"
		if _, err := f.s.PutSettings(ctx, f.a, in); !errors.Is(err, ErrInvalid) {
			t.Fatalf("bad theme: %v", err)
		}
	})

	t.Run("agent_forbidden", func(t *testing.T) {
		f := newCoreFixture(t, template)
		ctx := context.Background()
		agent := Actor{UserID: f.a.UserID, Kind: PrincipalAgent, Via: CredentialAgentToken, Scopes: AgentScopes}
		if err := f.db.Pool().QueryRow(ctx, "INSERT INTO principals(user_id,kind,name) VALUES($1,'agent','Bot') RETURNING id", f.a.UserID).Scan(&agent.PrincipalID); err != nil {
			t.Fatal(err)
		}
		if _, err := f.s.GetSettings(ctx, agent); !errors.Is(err, ErrForbidden) {
			t.Fatalf("agent get: %v", err)
		}
		if _, err := f.s.PutSettings(ctx, agent, readerSettingsInput("a1")); !errors.Is(err, ErrForbidden) {
			t.Fatalf("agent put: %v", err)
		}
	})

	t.Run("corrupt_keys_fall_back", func(t *testing.T) {
		f := newCoreFixture(t, template)
		ctx := context.Background()
		if _, err := f.db.Pool().Exec(ctx, `UPDATE users SET settings='{"theme":42,"measure":900,"feed_overrides":{"1":{"measure":64},"2":{"measure":999}},"reader_rev":-1}' WHERE id=$1`, f.a.UserID); err != nil {
			t.Fatal(err)
		}
		got, err := f.s.GetSettings(ctx, f.a)
		if err != nil {
			t.Fatal(err)
		}
		want := DefaultReaderSettings()
		want.FeedOverrides = map[string]ReaderOverride{"1": {Measure: intPtr(64)}}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("lenient decode: %+v", got)
		}
	})
}
