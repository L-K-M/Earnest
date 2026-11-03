package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/L-K-M/Earnest/internal/store/storetest"
)

func TestAgentNotificationStatus(t *testing.T) {
	f := newCoreFixture(t, storetest.NewTemplate(t))
	ctx := context.Background()
	id := f.ingest(t, "status")
	agent := surfaceAgent(t, f, "Morning", 3)
	other := surfaceAgent(t, f, "Evening", 3)
	var foreignUser int64
	if err := f.db.Pool().QueryRow(ctx, "INSERT INTO users(username,password_hash) VALUES('other','$argon2id$test') RETURNING id").Scan(&foreignUser); err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.Pool().Exec(ctx, "INSERT INTO principals(user_id,kind,name) VALUES($1,'agent','Foreign')", foreignUser); err != nil {
		t.Fatal(err)
	}
	n, err := f.s.SendNotification(ctx, agent, surfaceRequest(t, f, id, "send-status"))
	if err != nil {
		t.Fatal(err)
	}
	if err := f.s.MuteAgent(ctx, f.a, agent.PrincipalID, NotificationsMuted, "mute-status"); err != nil {
		t.Fatal(err)
	}
	statuses, err := f.s.ListAgentNotificationStatus(ctx, f.a)
	if err != nil || len(statuses) != 2 || statuses[0] != (AgentNotificationStatus{PrincipalID: agent.PrincipalID, Name: "Morning", Muted: true, UsedToday: 1}) || statuses[1].PrincipalID != other.PrincipalID || statuses[1].UsedToday != 0 {
		t.Fatal(statuses, err)
	}
	detail, err := f.s.GetNotification(ctx, f.a, n.ID)
	if err != nil || !detail.Muted {
		t.Fatal(detail, err)
	}
	if _, err := f.s.ListAgentNotificationStatus(ctx, agent); !errors.Is(err, ErrForbidden) {
		t.Fatal(err)
	}
	if err := f.s.MuteAgent(ctx, f.a, agent.PrincipalID, NotificationsUnmuted, "unmute-status"); err != nil {
		t.Fatal(err)
	}
	detail, err = f.s.GetNotification(ctx, f.a, n.ID)
	if err != nil || detail.Muted {
		t.Fatal(detail, err)
	}
	f.clock.Add(int64(24 * time.Hour))
	statuses, err = f.s.ListAgentNotificationStatus(ctx, f.a)
	if err != nil || statuses[0].Muted || statuses[0].UsedToday != 0 {
		t.Fatal(statuses, err)
	}
}
