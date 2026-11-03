package api

import (
	"fmt"
	"net/http"
	"testing"

	"github.com/L-K-M/Earnest/internal/service"
	"github.com/L-K-M/Earnest/internal/store/storetest"
)

func TestHTTPAgentNotificationStatus(t *testing.T) {
	f := newAPIFixture(t, storetest.NewTemplate(t))
	agent, principal := surfaceAgent(t, f)
	requireStatus(t, f.do(t, http.MethodGet, "/v1/agents", nil, agent), http.StatusForbidden)
	mute := fmt.Sprintf("/v1/agents/%d/mute", principal)
	requireStatus(t, f.do(t, http.MethodPut, mute, opBody{OpID: "status-mute"}, f.session()), http.StatusOK)
	body := notificationBody{OpID: "status-send", Generation: generation(t, f), Note: "hello", EntryIDs: []int64{f.entryID}, Category: service.NotificationAlert}
	rec := f.do(t, http.MethodPost, "/v1/notifications", body, agent)
	requireStatus(t, rec, http.StatusAccepted)
	n := decodeBody[service.Notification](t, rec)
	rec = f.do(t, http.MethodGet, "/v1/agents", nil, f.session())
	requireStatus(t, rec, http.StatusOK)
	agents := decodeBody[[]service.AgentNotificationStatus](t, rec)
	if len(agents) != 1 || agents[0] != (service.AgentNotificationStatus{PrincipalID: principal, Name: "Morning Agent", Muted: true, UsedToday: 1}) {
		t.Fatal(agents)
	}
	path := fmt.Sprintf("/v1/notifications/%d", n.ID)
	rec = f.do(t, http.MethodGet, path, nil, f.session())
	requireStatus(t, rec, http.StatusOK)
	if !decodeBody[service.Notification](t, rec).Muted {
		t.Fatal(rec.Body)
	}
	requireStatus(t, f.do(t, http.MethodDelete, mute+"?op_id=status-unmute", nil, f.session()), http.StatusOK)
	rec = f.do(t, http.MethodGet, path, nil, f.session())
	requireStatus(t, rec, http.StatusOK)
	if decodeBody[service.Notification](t, rec).Muted {
		t.Fatal(rec.Body)
	}
}
