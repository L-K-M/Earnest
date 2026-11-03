package api

import (
	"context"
	"net/http"

	"github.com/L-K-M/Earnest/internal/push"
	"github.com/L-K-M/Earnest/internal/service"
)

type notificationBody struct {
	OpID       string                       `json:"op_id"`
	Generation string                       `json:"generation"`
	Note       string                       `json:"note"`
	EntryIDs   []int64                      `json:"entry_ids"`
	Category   service.NotificationCategory `json:"category" enum:"alert,digest,brief"`
	DedupeKey  string                       `json:"dedupe_key,omitempty"`
}
type notificationsInput struct {
	BeforeID int64 `query:"before_id" minimum:"0"`
	Limit    int   `query:"limit" default:"100" minimum:"1" maximum:"1000"`
}
type releaseBody struct {
	Generation string `json:"generation"`
	OpID       string `json:"op_id"`
}
type deviceBody struct {
	Kind       push.Kind `json:"kind"`
	Endpoint   string    `json:"endpoint"`
	P256DH     string    `json:"p256dh"`
	AuthSecret string    `json:"auth_secret"`
	ShowText   bool      `json:"show_text"`
	OpID       string    `json:"op_id"`
}
type pushSettingsBody struct {
	NtfyServer string  `json:"ntfy_server"`
	NtfyTopic  string  `json:"ntfy_topic"`
	NtfyToken  *string `json:"ntfy_token,omitempty"`
	ShowText   bool    `json:"show_text"`
	QuietStart string  `json:"quiet_start"`
	QuietEnd   string  `json:"quiet_end"`
	Timezone   string  `json:"timezone"`
	OpID       string  `json:"op_id"`
}
type vapidKey struct {
	PublicKey string `json:"public_key"`
}

func (s *server) notificationRoutes() {
	route(s, http.MethodGet, "/v1/agents", "list-agents", func(ctx context.Context, in *empty) ([]service.AgentNotificationStatus, error) {
		return s.svc.ListAgentNotificationStatus(ctx, actor(ctx))
	})
	routeStatus(s, http.MethodPost, "/v1/notifications", "send-notification", http.StatusAccepted, func(ctx context.Context, in *bodyInput[notificationBody]) (service.Notification, error) {
		b := in.Body
		return s.svc.SendNotification(ctx, actor(ctx), service.SendNotificationRequest{OpID: b.OpID, Generation: b.Generation, Note: b.Note, EntryIDs: b.EntryIDs, Category: b.Category, DedupeKey: b.DedupeKey})
	})
	route(s, http.MethodGet, "/v1/notifications", "list-notifications", func(ctx context.Context, in *notificationsInput) ([]service.Notification, error) {
		return s.svc.ListNotifications(ctx, actor(ctx), in.BeforeID, in.Limit)
	})
	route(s, http.MethodGet, "/v1/notifications/{id}", "get-notification", func(ctx context.Context, in *idInput) (service.Notification, error) {
		return s.svc.GetNotification(ctx, actor(ctx), in.ID)
	})
	route(s, http.MethodPost, "/v1/notifications/{id}:cancel", "cancel-notification", func(ctx context.Context, in *idBody[opBody]) (empty, error) {
		return empty{}, s.svc.CancelNotification(ctx, actor(ctx), in.ID, in.Body.OpID)
	})
	route(s, http.MethodPost, "/v1/notifications:release", "release-notifications", func(ctx context.Context, in *bodyInput[releaseBody]) (empty, error) {
		return empty{}, s.svc.ReleaseNotifications(ctx, actor(ctx), in.Body.Generation, in.Body.OpID)
	})
	route(s, http.MethodPut, "/v1/agents/{id}/mute", "mute-agent", func(ctx context.Context, in *idBody[opBody]) (empty, error) {
		return empty{}, s.svc.MuteAgent(ctx, actor(ctx), in.ID, service.NotificationsMuted, in.Body.OpID)
	})
	route(s, http.MethodDelete, "/v1/agents/{id}/mute", "unmute-agent", func(ctx context.Context, in *deleteInput) (empty, error) {
		return empty{}, s.svc.MuteAgent(ctx, actor(ctx), in.ID, service.NotificationsUnmuted, in.OpID)
	})
}

func (s *server) pushRoutes() {
	route(s, http.MethodGet, "/v1/devices", "list-devices", func(ctx context.Context, in *empty) ([]service.Device, error) {
		return s.svc.ListDevices(ctx, actor(ctx))
	})
	route(s, http.MethodPost, "/v1/devices", "register-device", func(ctx context.Context, in *bodyInput[deviceBody]) (service.Device, error) {
		b := in.Body
		return s.svc.RegisterDevice(ctx, actor(ctx), service.DeviceRegistration{Kind: b.Kind, Endpoint: b.Endpoint, P256DH: b.P256DH, AuthSecret: b.AuthSecret, ShowText: b.ShowText, OpID: b.OpID})
	})
	route(s, http.MethodDelete, "/v1/devices/{id}", "delete-device", func(ctx context.Context, in *deleteInput) (empty, error) {
		return empty{}, s.svc.DeleteDevice(ctx, actor(ctx), in.ID, in.OpID)
	})
	route(s, http.MethodGet, "/v1/push-settings", "get-push-settings", func(ctx context.Context, in *empty) (service.PushSettings, error) {
		return s.svc.GetPushSettings(ctx, actor(ctx))
	})
	route(s, http.MethodPut, "/v1/push-settings", "put-push-settings", func(ctx context.Context, in *bodyInput[pushSettingsBody]) (service.PushSettings, error) {
		b := in.Body
		return s.svc.PutPushSettings(ctx, actor(ctx), service.PushSettingsInput{NtfyServer: b.NtfyServer, NtfyTopic: b.NtfyTopic, NtfyToken: b.NtfyToken, ShowText: b.ShowText, QuietStart: b.QuietStart, QuietEnd: b.QuietEnd, Timezone: b.Timezone, OpID: b.OpID})
	})
	route(s, http.MethodGet, "/v1/push/vapid-public-key", "vapid-public-key", func(ctx context.Context, in *empty) (vapidKey, error) {
		// Settings authorisation stays in the service, including the human-only check.
		if _, err := s.svc.GetPushSettings(ctx, actor(ctx)); err != nil {
			return vapidKey{}, err
		}
		if s.vapidPublicKey == "" {
			return vapidKey{}, service.ErrUnavailable
		}
		return vapidKey{PublicKey: s.vapidPublicKey}, nil
	})
}
