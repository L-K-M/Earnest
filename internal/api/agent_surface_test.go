package api

import (
	"crypto/ecdh"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/L-K-M/Earnest/internal/push"
	"github.com/L-K-M/Earnest/internal/service"
	"github.com/L-K-M/Earnest/internal/store/storetest"
	"github.com/L-K-M/Earnest/internal/syncmodel"
)

func decodeBody[T any](t *testing.T, rec *httptest.ResponseRecorder) T {
	t.Helper()
	var out T
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	return out
}
func surfaceAgent(t *testing.T, f *apiFixture) (requestCredentials, int64) {
	t.Helper()
	token, err := f.auth.CreateToken(t.Context(), f.owner, service.TokenOptions{Name: "Morning Agent", Kind: service.TokenAgent, Audience: service.AudienceAPI, DailyBudget: 1, Scopes: []service.Scope{service.ScopeEntriesRead, service.ScopeAnnotationsWrite, service.ScopeNotifySend, service.ScopeStateWriteHuman}})
	if err != nil {
		t.Fatal(err)
	}
	return requestCredentials{bearer: token.Secret}, token.Token.PrincipalID
}
func generation(t *testing.T, f *apiFixture) string {
	t.Helper()
	cursor, err := f.svc.BootstrapCursor(t.Context(), f.owner)
	if err != nil {
		t.Fatal(err)
	}
	return cursor.Generation
}

func TestHTTPAgentSurface(t *testing.T) {
	template := storetest.NewTemplate(t)
	t.Run("annotations and briefs", func(t *testing.T) {
		f := newAPIFixture(t, template)
		agent, principal := surfaceAgent(t, f)
		path := fmt.Sprintf("/v1/entries/%d/annotations/note", f.entryID)
		rec := f.do(t, http.MethodPut, path, annotationBody{Value: "My plain note", OpID: "note"}, agent)
		requireStatus(t, rec, http.StatusOK)
		note := decodeBody[service.Annotation](t, rec)
		if note.PrincipalID != principal || note.Value != "My plain note" {
			t.Fatal(note)
		}
		requireStatus(t, f.do(t, http.MethodGet, path, nil, agent), http.StatusOK)
		requireStatus(t, f.do(t, http.MethodGet, fmt.Sprintf("%s?principal_id=%d", path, principal), nil, f.session()), http.StatusOK)
		rec = f.do(t, http.MethodGet, fmt.Sprintf("/v1/annotations?entry_ids=%d", f.entryID), nil, f.session())
		requireStatus(t, rec, http.StatusOK)
		if notes := decodeBody[[]service.Annotation](t, rec); len(notes) != 1 {
			t.Fatal(notes)
		}
		for i, value := range []string{"First brief", "Latest brief"} {
			requireStatus(t, f.do(t, http.MethodPost, "/v1/briefs", briefBody{EntryIDs: []int64{f.entryID}, Value: value, OpID: fmt.Sprintf("brief%d", i)}, agent), http.StatusOK)
		}
		rec = f.do(t, http.MethodGet, "/v1/briefs/today", nil, f.session())
		requireStatus(t, rec, http.StatusOK)
		briefs := decodeBody[[]service.Annotation](t, rec)
		if len(briefs) != 1 || briefs[0].Value != "Latest brief" {
			t.Fatal(briefs)
		}
		requireStatus(t, f.do(t, http.MethodDelete, path+"?op_id=delete-note", nil, agent), http.StatusOK)
		requireStatus(t, f.do(t, http.MethodGet, path, nil, agent), http.StatusNotFound)
		requireStatus(t, f.do(t, http.MethodPut, fmt.Sprintf("/v1/entries/%d/annotations/summary", f.entryID), annotationBody{Value: "forbidden", OpID: "pipeline-only"}, agent), http.StatusForbidden)
	})
	t.Run("notifications budget provenance and cancellation", func(t *testing.T) {
		f := newAPIFixture(t, template)
		agent, principal := surfaceAgent(t, f)
		mutePath := fmt.Sprintf("/v1/agents/%d/mute", principal)
		requireStatus(t, f.do(t, http.MethodPut, mutePath, opBody{OpID: "mute"}, f.session()), http.StatusOK)
		body := notificationBody{OpID: "send", Generation: generation(t, f), Note: "The agent's words", EntryIDs: []int64{f.entryID}, Category: service.NotificationAlert}
		rec := f.do(t, http.MethodPost, "/v1/notifications", body, agent)
		requireStatus(t, rec, http.StatusAccepted)
		notification := decodeBody[service.Notification](t, rec)
		if notification.Status != service.NotificationHeld || notification.Budget.UsedToday != 1 {
			t.Fatal(notification)
		}
		requireStatus(t, f.do(t, http.MethodPost, "/v1/notifications", body, agent), http.StatusAccepted)
		body.OpID = "over-budget"
		rec = f.do(t, http.MethodPost, "/v1/notifications", body, agent)
		requireStatus(t, rec, http.StatusTooManyRequests)
		assertProblem(t, rec, "/problems/rate-limited")
		var p struct {
			Budget service.NotificationBudget `json:"budget"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &p); err != nil {
			t.Fatal(err)
		}
		if p.Budget.UsedToday != 1 || p.Budget.LimitToday != 1 {
			t.Fatalf("budget missing: %s", rec.Body)
		}
		path := fmt.Sprintf("/v1/notifications/%d", notification.ID)
		rec = f.do(t, http.MethodGet, path, nil, f.session())
		requireStatus(t, rec, http.StatusOK)
		detail := decodeBody[service.Notification](t, rec)
		why, _ := json.Marshal(detail.Why)
		if detail.Note != body.Note || detail.Why.Sender != "Morning Agent" || strings.Contains(string(why), body.Note) {
			t.Fatal(detail)
		}
		requireStatus(t, f.do(t, http.MethodGet, "/v1/notifications", nil, f.session()), http.StatusOK)
		requireStatus(t, f.do(t, http.MethodGet, path, nil, agent), http.StatusForbidden)
		requireStatus(t, f.do(t, http.MethodPost, path+":cancel", opBody{OpID: "cancel"}, agent), http.StatusOK)
		requireStatus(t, f.do(t, http.MethodDelete, mutePath+"?op_id=unmute", nil, f.session()), http.StatusOK)
		requireStatus(t, f.do(t, http.MethodPost, "/v1/notifications:release", releaseBody{Generation: body.Generation, OpID: "release"}, f.session()), http.StatusOK)
		requireStatus(t, f.do(t, http.MethodPost, "/v1/notifications:release", releaseBody{Generation: "old", OpID: "stale-release"}, f.session()), http.StatusGone)
	})
	t.Run("devices and settings", func(t *testing.T) {
		f := newAPIFixture(t, template)
		key, err := ecdh.P256().NewPrivateKey(append(make([]byte, 31), 1))
		if err != nil {
			t.Fatal(err)
		}
		body := deviceBody{Kind: push.KindWebPush, Endpoint: "https://push.example/sub", P256DH: base64.RawURLEncoding.EncodeToString(key.PublicKey().Bytes()), AuthSecret: base64.RawURLEncoding.EncodeToString(make([]byte, 16)), ShowText: true, OpID: "device"}
		rec := f.do(t, http.MethodPost, "/v1/devices", body, f.session())
		requireStatus(t, rec, http.StatusOK)
		device := decodeBody[service.Device](t, rec)
		rec = f.do(t, http.MethodGet, "/v1/devices", nil, f.session())
		requireStatus(t, rec, http.StatusOK)
		if strings.Contains(rec.Body.String(), body.P256DH) || strings.Contains(rec.Body.String(), body.AuthSecret) {
			t.Fatal("keys exposed")
		}
		requireStatus(t, f.do(t, http.MethodDelete, fmt.Sprintf("/v1/devices/%d?op_id=delete-device", device.ID), nil, f.session()), http.StatusOK)
		token := "secret-ntfy-token"
		settings := pushSettingsBody{NtfyServer: "https://ntfy.example", NtfyTopic: "reader", NtfyToken: &token, Timezone: "UTC", QuietStart: "22:00", QuietEnd: "07:00", OpID: "settings"}
		rec = f.do(t, http.MethodPut, "/v1/push-settings", settings, f.session())
		requireStatus(t, rec, http.StatusOK)
		if strings.Contains(rec.Body.String(), token) {
			t.Fatal("ntfy token exposed")
		}
		rec = f.do(t, http.MethodGet, "/v1/push-settings", nil, f.session())
		requireStatus(t, rec, http.StatusOK)
		if settings := decodeBody[service.PushSettings](t, rec); !settings.TokenSet || settings.QuietStart != "22:00" {
			t.Fatal(settings)
		}
		f.handler = New(f.svc, f.auth, nil, "https://reader.example", f.content, "test-public-key")
		rec = f.do(t, http.MethodGet, "/v1/push/vapid-public-key", nil, f.session())
		requireStatus(t, rec, http.StatusOK)
		if decodeBody[vapidKey](t, rec).PublicKey != "test-public-key" {
			t.Fatal(rec.Body)
		}
	})
	t.Run("human-only boundaries", func(t *testing.T) {
		f := newAPIFixture(t, template)
		agent, _ := surfaceAgent(t, f)
		for _, tc := range []struct {
			method, path string
			body         any
		}{
			{http.MethodGet, "/v1/devices", nil},
			{http.MethodPost, "/v1/devices", deviceBody{}},
			{http.MethodDelete, "/v1/devices/1?op_id=denied", nil},
			{http.MethodGet, "/v1/push-settings", nil},
			{http.MethodPut, "/v1/push-settings", pushSettingsBody{}},
			{http.MethodGet, "/v1/push/vapid-public-key", nil},
			{http.MethodGet, "/v1/webhooks", nil},
			{http.MethodPost, "/v1/webhooks", webhookBody{}},
			{http.MethodPatch, "/v1/webhooks/1", webhookPatch{}},
			{http.MethodDelete, "/v1/webhooks/1?op_id=denied", nil},
		} {
			t.Run(tc.method+tc.path, func(t *testing.T) {
				rec := f.do(t, tc.method, tc.path, tc.body, agent)
				requireStatus(t, rec, http.StatusForbidden)
				assertProblem(t, rec, "/problems/forbidden")
			})
		}
	})
	t.Run("webhook secret shown once", func(t *testing.T) {
		f := newAPIFixture(t, template)
		body := webhookBody{URL: "https://hooks.example/receive", Events: []string{"entry.matched"}, OpID: "webhook"}
		rec := f.do(t, http.MethodPost, "/v1/webhooks", body, f.session())
		requireStatus(t, rec, http.StatusOK)
		created := decodeBody[service.WebhookSecret](t, rec)
		if !strings.HasPrefix(created.Secret, "whsec_") {
			t.Fatal(created)
		}
		rec = f.do(t, http.MethodPost, "/v1/webhooks", body, f.session())
		requireStatus(t, rec, http.StatusOK)
		if decodeBody[service.WebhookSecret](t, rec).Secret != "" {
			t.Fatal("secret replayed")
		}
		rec = f.do(t, http.MethodGet, "/v1/webhooks", nil, f.session())
		requireStatus(t, rec, http.StatusOK)
		if strings.Contains(rec.Body.String(), created.Secret) {
			t.Fatal("secret exposed")
		}
		active := false
		path := fmt.Sprintf("/v1/webhooks/%d", created.ID)
		rec = f.do(t, http.MethodPatch, path, webhookPatch{Active: &active, OpID: "disable"}, f.session())
		requireStatus(t, rec, http.StatusOK)
		if decodeBody[service.Webhook](t, rec).Active {
			t.Fatal("still active")
		}
		requireStatus(t, f.do(t, http.MethodDelete, path+"?op_id=delete-webhook", nil, f.session()), http.StatusOK)
	})
	t.Run("triage activity and stale revert", func(t *testing.T) {
		f := newAPIFixture(t, template)
		agent, principal := surfaceAgent(t, f)
		since := time.Now().Add(-time.Minute).UTC().Format(time.RFC3339)
		rec := f.do(t, http.MethodPost, "/v1/triage", triageBody{OpID: "triage", Generation: generation(t, f), EntryIDs: []int64{f.entryID}, Action: service.TriageDone}, agent)
		requireStatus(t, rec, http.StatusOK)
		triage := decodeBody[service.TriageResult](t, rec)
		if triage.RevertHandle == "" || len(triage.Results) != 1 || triage.Results[0].Status != syncmodel.Applied {
			t.Fatal(triage)
		}
		rec = f.do(t, http.MethodGet, "/v1/activity/agent-changes?since="+since, nil, f.session())
		requireStatus(t, rec, http.StatusOK)
		if changes := decodeBody[[]service.Activity](t, rec); len(changes) != 1 {
			t.Fatal(changes)
		}
		rec = f.do(t, http.MethodGet, fmt.Sprintf("/v1/activity?principal_id=%d&action=state.updated&since=%s", principal, since), nil, f.session())
		requireStatus(t, rec, http.StatusOK)
		activities := decodeBody[activityPage](t, rec).Activities
		if len(activities) != 1 || activities[0].NextAction != service.ActivityRevert {
			t.Fatal(activities)
		}
		rec = f.do(t, http.MethodGet, "/v1/activity?grouped=true", nil, f.session())
		requireStatus(t, rec, http.StatusOK)
		if groups := decodeBody[activityPage](t, rec).Groups; len(groups) != 1 || groups[0].PrincipalID != principal {
			t.Fatal(groups)
		}
		write := f.write(t, "human-later", syncmodel.Shelf, syncmodel.Later)
		write.Base.Rev = triage.Results[0].State.Shelf.Rev
		requireStatus(t, f.do(t, http.MethodPost, "/v1/state:batch", []syncmodel.Write{write}, f.session()), http.StatusOK)
		rec = f.do(t, http.MethodPost, "/v1/triage:revert", triageRevertBody{RevertHandle: triage.RevertHandle, OpID: "stale-revert"}, agent)
		requireStatus(t, rec, http.StatusConflict)
		assertProblem(t, rec, "/problems/conflict")
		requireStatus(t, f.do(t, http.MethodPost, fmt.Sprintf("/v1/activity/%d:revert", activities[0].ID), opBody{OpID: "stale-activity-revert"}, f.session()), http.StatusConflict)
	})
	t.Run("triage revert succeeds", func(t *testing.T) {
		f := newAPIFixture(t, template)
		agent, _ := surfaceAgent(t, f)
		rec := f.do(t, http.MethodPost, "/v1/triage", triageBody{OpID: "triage", Generation: generation(t, f), EntryIDs: []int64{f.entryID}, Action: service.TriageSeen}, agent)
		requireStatus(t, rec, http.StatusOK)
		result := decodeBody[service.TriageResult](t, rec)
		requireStatus(t, f.do(t, http.MethodPost, "/v1/triage:revert", triageRevertBody{RevertHandle: result.RevertHandle, OpID: "revert"}, agent), http.StatusOK)
		rec = f.do(t, http.MethodGet, fmt.Sprintf("/v1/entries/%d", f.entryID), nil, f.session())
		requireStatus(t, rec, http.StatusOK)
		if decodeBody[entry](t, rec).State.Seen.Value != syncmodel.Unseen {
			t.Fatal("not reverted")
		}
	})
}
