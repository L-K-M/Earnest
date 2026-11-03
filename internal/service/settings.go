package service

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/L-K-M/Earnest/internal/store"
	"github.com/L-K-M/Earnest/internal/syncmodel"
	"github.com/jackc/pgx/v5"
)

// ReadingMode picks how the river presents unread work: an ending river or
// per-feed unread counts, offered at onboarding per docs/research/ux.md.
type ReadingMode string

const (
	ReadingModeRiver        ReadingMode = "river"
	ReadingModeUnreadCounts ReadingMode = "unread_counts"
)

// ReaderTheme is the reading-view palette. "black" is true black for OLED and
// "eink" is the high-contrast e-ink variant.
type ReaderTheme string

const (
	ReaderThemeSystem ReaderTheme = "system"
	ReaderThemeLight  ReaderTheme = "light"
	ReaderThemeDark   ReaderTheme = "dark"
	ReaderThemeBlack  ReaderTheme = "black"
	ReaderThemeEink   ReaderTheme = "eink"
)

// ReaderFont is the reading-view face; dyslexic is the dyslexia-friendly cut.
type ReaderFont string

const (
	ReaderFontSerif    ReaderFont = "serif"
	ReaderFontSans     ReaderFont = "sans"
	ReaderFontDyslexic ReaderFont = "dyslexic"
)

const (
	minMeasure = 62
	maxMeasure = 72

	// Match the existing river fallback before a user saves preferences.
	defaultReaderDecayDays = defaultTodayDecayDays

	maxFeedOverrides        = 512
	maxFeedOverrideKeyBytes = 128
)

// settingsUpdated carries the whole validated document on the change feed.
const settingsUpdated = syncmodel.ChangeType("settings.updated")

// settingsRevKey holds the revision of the last settings change inside
// users.settings, alongside the flat reader keys.
const settingsRevKey = "reader_rev"

// ReaderOverride overrides the reading view for one feed. Every member is
// optional; a nil member inherits the account-wide setting.
type ReaderOverride struct {
	Theme   *ReaderTheme `json:"theme,omitempty"`
	Measure *int         `json:"measure,omitempty"`
	Font    *ReaderFont  `json:"font,omitempty"`
}

// ReaderSettings is the validated slice of users.settings the clients render.
// The fields sit at the document's top level because todayDecay already reads
// settings->>'today_decay_days' there. Rev is the change-log revision of the
// last write, so a client can order it against settings.updated events.
type ReaderSettings struct {
	ReadingMode        ReadingMode               `json:"reading_mode"`
	TodayDecayDays     int                       `json:"today_decay_days"`
	SingleKeyShortcuts bool                      `json:"single_key_shortcuts"`
	Theme              ReaderTheme               `json:"theme"`
	Measure            int                       `json:"measure"`
	Font               ReaderFont                `json:"font"`
	FeedOverrides      map[string]ReaderOverride `json:"feed_overrides"`
	Rev                uint64                    `json:"rev"`
}

// ReaderSettingsInput replaces every reader setting on PUT.
type ReaderSettingsInput struct {
	ReadingMode        ReadingMode
	TodayDecayDays     int
	SingleKeyShortcuts bool
	Theme              ReaderTheme
	Measure            int
	Font               ReaderFont
	FeedOverrides      map[string]ReaderOverride
	OpID               string
}

// DefaultReaderSettings is what a user who never saved settings reads.
func DefaultReaderSettings() ReaderSettings {
	return ReaderSettings{
		ReadingMode:        ReadingModeRiver,
		TodayDecayDays:     defaultReaderDecayDays,
		SingleKeyShortcuts: true,
		Theme:              ReaderThemeSystem,
		Measure:            66,
		Font:               ReaderFontSerif,
		FeedOverrides:      map[string]ReaderOverride{},
	}
}

// GetSettings returns the user's reader settings. Absent or corrupt keys fall
// back to the defaults field by field; the service owns these keys, so only a
// hand edit or an out-of-band writer can produce a value that fails to decode.
func (s *Service) GetSettings(ctx context.Context, a Actor) (ReaderSettings, error) {
	if err := s.deviceActor(ctx, a); err != nil {
		return ReaderSettings{}, err
	}
	var raw []byte
	if err := s.store.Pool().QueryRow(ctx, "SELECT settings FROM users WHERE id=$1", a.UserID).Scan(&raw); err != nil {
		return ReaderSettings{}, domainError(err)
	}
	return decodeReaderSettings(raw)
}

// PutSettings validates and stores the whole settings document, deduplicated
// by operation ID. The write merges into users.settings, preserving keys the
// reader settings do not own, and commits one settings.updated event.
func (s *Service) PutSettings(ctx context.Context, a Actor, in ReaderSettingsInput) (ReaderSettings, error) {
	if err := s.deviceActor(ctx, a); err != nil {
		return ReaderSettings{}, err
	}
	if !validOpID(in.OpID) {
		return ReaderSettings{}, fmt.Errorf("op_id: %w", ErrInvalid)
	}
	if err := validateReaderSettings(in); err != nil {
		return ReaderSettings{}, err
	}
	out := ReaderSettings{
		ReadingMode:        in.ReadingMode,
		TodayDecayDays:     in.TodayDecayDays,
		SingleKeyShortcuts: in.SingleKeyShortcuts,
		Theme:              in.Theme,
		Measure:            in.Measure,
		Font:               in.Font,
		FeedOverrides:      in.FeedOverrides,
	}
	if out.FeedOverrides == nil {
		out.FeedOverrides = map[string]ReaderOverride{}
	}
	err := s.withEvents(ctx, func(tx pgx.Tx) error {
		if err := store.LockCore(ctx, tx); err != nil {
			return err
		}
		replay, err := replayOp(ctx, tx, a.PrincipalID, "PutSettings", in.OpID)
		if err != nil {
			return err
		}
		if replay != nil {
			return json.Unmarshal(replay, &out)
		}
		rev, err := changeRev(ctx, tx, a.UserID)
		if err != nil {
			return err
		}
		out.Rev = uint64(rev)
		// The fragment replaces the owned keys only; unrelated settings such
		// as retention_days survive the merge.
		fragment, err := json.Marshal(map[string]any{
			"reading_mode":         out.ReadingMode,
			"today_decay_days":     out.TodayDecayDays,
			"single_key_shortcuts": out.SingleKeyShortcuts,
			"theme":                out.Theme,
			"measure":              out.Measure,
			"font":                 out.Font,
			"feed_overrides":       out.FeedOverrides,
			settingsRevKey:         rev,
		})
		if err != nil {
			return fmt.Errorf("encode settings: %w", err)
		}
		if _, err := tx.Exec(ctx, "UPDATE users SET settings = settings || $2::jsonb WHERE id=$1", a.UserID, string(fragment)); err != nil {
			return domainError(err)
		}
		if err := s.emitChange(ctx, tx, a.UserID, rev, settingsUpdated, "settings", out); err != nil {
			return err
		}
		return store.FinishOp(ctx, tx, a.PrincipalID, in.OpID, out)
	})
	if err != nil {
		return ReaderSettings{}, err
	}
	return out, nil
}

func validateReaderSettings(in ReaderSettingsInput) error {
	switch in.ReadingMode {
	case ReadingModeRiver, ReadingModeUnreadCounts:
	default:
		return fmt.Errorf("reading_mode: %w", ErrInvalid)
	}
	if in.TodayDecayDays < 1 || in.TodayDecayDays > maxTodayDecayDays {
		return fmt.Errorf("today_decay_days: %w", ErrInvalid)
	}
	if !validReaderTheme(in.Theme) {
		return fmt.Errorf("theme: %w", ErrInvalid)
	}
	if in.Measure < minMeasure || in.Measure > maxMeasure {
		return fmt.Errorf("measure: %w", ErrInvalid)
	}
	if !validReaderFont(in.Font) {
		return fmt.Errorf("font: %w", ErrInvalid)
	}
	if len(in.FeedOverrides) > maxFeedOverrides {
		return fmt.Errorf("feed_overrides: %w", ErrInvalid)
	}
	for key, override := range in.FeedOverrides {
		if !validOverrideFeedID(key) {
			return fmt.Errorf("feed_overrides key: %w", ErrInvalid)
		}
		if err := validReaderOverride(override); err != nil {
			return fmt.Errorf("feed_overrides[%s]: %w", key, err)
		}
	}
	return nil
}

func validOverrideFeedID(key string) bool {
	if len(key) > maxFeedOverrideKeyBytes {
		return false
	}
	id, err := strconv.ParseInt(key, 10, 64)
	return err == nil && id > 0 && strconv.FormatInt(id, 10) == key
}

func validReaderOverride(o ReaderOverride) error {
	if o.Theme != nil && !validReaderTheme(*o.Theme) {
		return fmt.Errorf("theme: %w", ErrInvalid)
	}
	if o.Measure != nil && (*o.Measure < minMeasure || *o.Measure > maxMeasure) {
		return fmt.Errorf("measure: %w", ErrInvalid)
	}
	if o.Font != nil && !validReaderFont(*o.Font) {
		return fmt.Errorf("font: %w", ErrInvalid)
	}
	return nil
}

func validReaderTheme(t ReaderTheme) bool {
	switch t {
	case ReaderThemeSystem, ReaderThemeLight, ReaderThemeDark, ReaderThemeBlack, ReaderThemeEink:
		return true
	}
	return false
}

func validReaderFont(f ReaderFont) bool {
	switch f {
	case ReaderFontSerif, ReaderFontSans, ReaderFontDyslexic:
		return true
	}
	return false
}

// decodeReaderSettings merges stored keys over the defaults. Each field is
// independent: a stale or hand-edited value loses to the default for that
// field rather than failing the read, matching todayDecay's leniency.
func decodeReaderSettings(raw []byte) (ReaderSettings, error) {
	out := DefaultReaderSettings()
	var doc map[string]json.RawMessage
	if err := json.Unmarshal(raw, &doc); err != nil {
		return ReaderSettings{}, fmt.Errorf("decode settings: %w", err)
	}
	if text, ok := settingsString(doc, "reading_mode"); ok {
		if mode := ReadingMode(text); mode == ReadingModeRiver || mode == ReadingModeUnreadCounts {
			out.ReadingMode = mode
		}
	}
	if n, ok := settingsNumber(doc, "today_decay_days"); ok && n >= 1 && n <= maxTodayDecayDays {
		out.TodayDecayDays = n
	}
	var on bool
	if raw, ok := doc["single_key_shortcuts"]; ok && json.Unmarshal(raw, &on) == nil {
		out.SingleKeyShortcuts = on
	}
	if text, ok := settingsString(doc, "theme"); ok {
		if theme := ReaderTheme(text); validReaderTheme(theme) {
			out.Theme = theme
		}
	}
	if n, ok := settingsNumber(doc, "measure"); ok && n >= minMeasure && n <= maxMeasure {
		out.Measure = n
	}
	if text, ok := settingsString(doc, "font"); ok {
		if font := ReaderFont(text); validReaderFont(font) {
			out.Font = font
		}
	}
	if raw, ok := doc["feed_overrides"]; ok {
		var overrides map[string]ReaderOverride
		if json.Unmarshal(raw, &overrides) == nil {
			for key, override := range overrides {
				if !validOverrideFeedID(key) || validReaderOverride(override) != nil {
					continue
				}
				out.FeedOverrides[key] = override
			}
		}
	}
	if n, ok := settingsNumber(doc, settingsRevKey); ok && n > 0 {
		out.Rev = uint64(n)
	}
	return out, nil
}

// settingsString and settingsNumber accept the stored key as written;
// numbers also tolerate a decimal string so a hand edit that todayDecay would
// honour reads the same here.
func settingsString(doc map[string]json.RawMessage, key string) (string, bool) {
	raw, ok := doc[key]
	if !ok {
		return "", false
	}
	var s string
	if err := json.Unmarshal(raw, &s); err != nil {
		return "", false
	}
	return s, true
}

func settingsNumber(doc map[string]json.RawMessage, key string) (int, bool) {
	raw, ok := doc[key]
	if !ok {
		return 0, false
	}
	var n int
	if err := json.Unmarshal(raw, &n); err == nil {
		return n, true
	}
	var text string
	if err := json.Unmarshal(raw, &text); err != nil {
		return 0, false
	}
	n, err := strconv.Atoi(strings.TrimSpace(text))
	if err != nil {
		return 0, false
	}
	return n, true
}
