package api

import (
	"context"
	"net/http"

	"github.com/L-K-M/Earnest/internal/service"
)

type settingsBody struct {
	ReadingMode        service.ReadingMode               `json:"reading_mode"`
	ShowOnlyUnread     *bool                             `json:"show_only_unread,omitempty" doc:"Show only unread articles on intentional list loads; omission preserves the saved preference."`
	TodayDecayDays     int                               `json:"today_decay_days"`
	SingleKeyShortcuts bool                              `json:"single_key_shortcuts"`
	Theme              service.ReaderTheme               `json:"theme"`
	Measure            int                               `json:"measure"`
	Font               service.ReaderFont                `json:"font"`
	FeedOverrides      map[string]service.ReaderOverride `json:"feed_overrides,omitempty"`
	OpID               string                            `json:"op_id"`
}

func (s *server) settingsRoutes() {
	route(s, http.MethodGet, "/v1/settings", "get-settings", func(ctx context.Context, in *empty) (service.ReaderSettings, error) {
		return s.svc.GetSettings(ctx, actor(ctx))
	})
	route(s, http.MethodPut, "/v1/settings", "put-settings", func(ctx context.Context, in *bodyInput[settingsBody]) (service.ReaderSettings, error) {
		b := in.Body
		return s.svc.PutSettings(ctx, actor(ctx), service.ReaderSettingsInput{
			ReadingMode:        b.ReadingMode,
			ShowOnlyUnread:     b.ShowOnlyUnread,
			TodayDecayDays:     b.TodayDecayDays,
			SingleKeyShortcuts: b.SingleKeyShortcuts,
			Theme:              b.Theme,
			Measure:            b.Measure,
			Font:               b.Font,
			FeedOverrides:      b.FeedOverrides,
			OpID:               b.OpID,
		})
	})
}
