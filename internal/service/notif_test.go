package service

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/L-K-M/Earnest/internal/push"
	"github.com/L-K-M/Earnest/internal/store/storetest"
)

func surfaceAgent(t *testing.T, f *coreFixture, name string, budget int) Actor {
	t.Helper()
	ctx := context.Background()
	a := Actor{UserID: f.a.UserID, Kind: PrincipalAgent, Via: CredentialAgentToken, Scopes: []Scope{ScopeEntriesRead, ScopeAnnotationsWrite, ScopeNotifySend, ScopeStateWriteHuman, ScopePositionOwn}}
	if err := f.db.Pool().QueryRow(ctx, "INSERT INTO principals(user_id,kind,name) VALUES($1,'agent',$2) RETURNING id", a.UserID, name).Scan(&a.PrincipalID); err != nil {
		t.Fatal(err)
	}
	if err := f.db.Pool().QueryRow(ctx, "INSERT INTO tokens(principal_id,name,token_hash,kind,audience,scopes,daily_budget) VALUES($1,$2,$3,'agent','api',$4,$5) RETURNING id", a.PrincipalID, name, hashSecret(name), scopeStrings(a.Scopes), budget).Scan(&a.TokenID); err != nil {
		t.Fatal(err)
	}
	return a
}
func surfaceRequest(t *testing.T, f *coreFixture, id int64, op string) SendNotificationRequest {
	t.Helper()
	gen, err := f.db.Generation(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return SendNotificationRequest{OpID: op, Generation: gen, Note: "agent secret https://secret.example", EntryIDs: []int64{id}, Category: NotificationAlert}
}

func TestMuteAgentHoldsPendingNotification(t *testing.T) {
	f := newCoreFixture(t, storetest.NewTemplate(t))
	ctx := context.Background()
	a := surfaceAgent(t, f, "Morning", 3)
	id := f.ingest(t, "article")
	n, err := f.s.SendNotification(ctx, a, surfaceRequest(t, f, id, "queued"))
	if err != nil || n.Status != NotificationQueued {
		t.Fatal(n, err)
	}
	if err := f.s.MuteAgent(ctx, f.a, a.PrincipalID, NotificationsMuted, "mute"); err != nil {
		t.Fatal(err)
	}
	n, err = f.s.GetNotification(ctx, f.a, n.ID)
	if err != nil || n.Status != NotificationHeld {
		t.Fatalf("muted pending notification: status=%s err=%v", n.Status, err)
	}
	if err := f.s.CancelNotification(ctx, f.a, n.ID, "cancel"); err != nil {
		t.Fatalf("muted pending notification cannot be cancelled: %v", err)
	}
	if err := f.s.MuteAgent(ctx, f.a, a.PrincipalID, NotificationsUnmuted, "unmute"); err != nil {
		t.Fatal(err)
	}
	n, err = f.s.GetNotification(ctx, f.a, n.ID)
	if err != nil || n.Status != NotificationCancelled {
		t.Fatalf("unmute resurrected cancellation: %+v %v", n, err)
	}
}

type surfaceTransport struct {
	send func(push.Device, push.Message) error
}

func (t surfaceTransport) Send(_ context.Context, d push.Device, m push.Message) (push.Receipt, error) {
	return push.Receipt{}, t.send(d, m)
}

func TestNotificationRules(t *testing.T) {
	template := storetest.NewTemplate(t)
	t.Run("quiet_urgent_budget_and_dedupe", func(t *testing.T) {
		f := newCoreFixture(t, template)
		ctx := context.Background()
		id := f.ingest(t, "article")
		a := surfaceAgent(t, f, "Morning", 2)
		if _, err := f.db.Pool().Exec(ctx, "INSERT INTO push_settings(user_id,quiet_start,quiet_end,timezone) VALUES($1,'11:00','13:00','UTC')", f.a.UserID); err != nil {
			t.Fatal(err)
		}
		r := surfaceRequest(t, f, id, "one")
		r.DedupeKey = "same"
		n, err := f.s.SendNotification(ctx, a, r)
		if err != nil || n.Status != NotificationHeld || n.Budget.UsedToday != 1 || n.Why.Sender != "Morning" {
			t.Fatal(n, err)
		}
		r.OpID = "two"
		dupe, err := f.s.SendNotification(ctx, a, r)
		if err != nil || dupe.ID != n.ID || !dupe.Replay {
			t.Fatal("dedupe", dupe, err)
		}
		other := surfaceAgent(t, f, "Evening", 2)
		otherN, err := f.s.SendNotification(ctx, other, r)
		if err != nil || otherN.ID == n.ID {
			t.Fatal("dedupe swallowed other principal", otherN, err)
		}
		a.Scopes = append(a.Scopes, ScopeNotifyUrgent)
		if _, err := f.db.Pool().Exec(ctx, "UPDATE tokens SET scopes=array_append(scopes,'notify:urgent') WHERE id=$1", a.TokenID); err != nil {
			t.Fatal(err)
		}
		r.OpID = "urgent"
		r.DedupeKey = ""
		urgent, err := f.s.SendNotification(ctx, a, r)
		if err != nil || urgent.Status != NotificationQueued || urgent.Budget.UsedToday != 2 {
			t.Fatal("urgent", urgent, err)
		}
		r.OpID = "over"
		_, err = f.s.SendNotification(ctx, a, r)
		var budgetError *NotificationBudgetError
		if !errors.Is(err, ErrRateLimited) || !errors.As(err, &budgetError) || budgetError.Budget.UsedToday != 2 {
			t.Fatal("budget bypass", err)
		}
		if err := f.s.CancelNotification(ctx, a, n.ID, "cancel"); err != nil {
			t.Fatal(err)
		}
		if err := f.s.CancelNotification(ctx, a, urgent.ID, "cancel-queued"); !errors.Is(err, ErrConflict) {
			t.Fatal("cancelled queued", err)
		}
		if _, err := f.s.GetNotification(ctx, a, n.ID); !errors.Is(err, ErrForbidden) {
			t.Fatal("agent listed notifications", err)
		}
	})
	t.Run("concurrent_budget", func(t *testing.T) {
		f := newCoreFixture(t, template)
		id := f.ingest(t, "article")
		a := surfaceAgent(t, f, "Morning", 1)
		var wg sync.WaitGroup
		errs := make(chan error, 8)
		for i := range 8 {
			wg.Add(1)
			go func() {
				defer wg.Done()
				_, err := f.s.SendNotification(context.Background(), a, surfaceRequest(t, f, id, fmt.Sprint(i)))
				errs <- err
			}()
		}
		wg.Wait()
		close(errs)
		accepted := 0
		for err := range errs {
			if err == nil {
				accepted++
			} else if !errors.Is(err, ErrRateLimited) {
				t.Fatal(err)
			}
		}
		if accepted != 1 {
			t.Fatal("budget race", accepted)
		}
	})
	t.Run("ownership_and_category", func(t *testing.T) {
		f := newCoreFixture(t, template)
		id := f.ingest(t, "article")
		a := surfaceAgent(t, f, "Morning", 3)
		r := surfaceRequest(t, f, id, "bad")
		r.EntryIDs = []int64{999999}
		if _, err := f.s.SendNotification(context.Background(), a, r); !errors.Is(err, ErrNotFound) {
			t.Fatal(err)
		}
		r.EntryIDs = []int64{id}
		r.Category = "watch"
		if _, err := f.s.SendNotification(context.Background(), a, r); !errors.Is(err, ErrInvalid) {
			t.Fatal(err)
		}
	})
}
func TestNotificationDelivery(t *testing.T) {
	template := storetest.NewTemplate(t)
	for _, tc := range []struct {
		name      string
		show      bool
		sendError error
		status    NotificationStatus
	}{
		{"default", false, nil, NotificationDelivered},
		{"opt_in", true, nil, NotificationDelivered},
		{"unknown", false, errors.New("connection lost after send"), NotificationUncertain},
		{"definite_failure", false, &push.Error{Outcome: push.OutcomeNotDelivered, Err: errors.New("rejected")}, NotificationFailed},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newCoreFixture(t, template)
			ctx := context.Background()
			id := f.ingest(t, "article")
			a := surfaceAgent(t, f, "Morning", 3)
			if _, err := f.db.Pool().Exec(ctx, "INSERT INTO devices(user_id,transport,endpoint,show_text) VALUES($1,'webpush','https://push.example/endpoint',$2)", f.a.UserID, tc.show); err != nil {
				t.Fatal(err)
			}
			calls := 0
			registry := push.NewRegistry()
			if err := registry.Register(push.KindWebPush, surfaceTransport{send: func(d push.Device, m push.Message) error {
				calls++
				var status string
				var attempted *time.Time
				if err := f.db.Pool().QueryRow(ctx, "SELECT status,attempted_at FROM notifications WHERE id=$1", m.NotificationID).Scan(&status, &attempted); err != nil || status != "attempting" || attempted == nil {
					t.Fatal("attempt not durable before send", status, attempted, err)
				}
				if (m.AgentText != "") != tc.show || m.Title != "Earnest" || m.Body != "Agent notification: 1 alert" {
					t.Fatal("unsafe push payload", m)
				}
				return tc.sendError
			}}); err != nil {
				t.Fatal(err)
			}
			f.s.pushRegistry = registry
			n, err := f.s.SendNotification(ctx, a, surfaceRequest(t, f, id, "send"))
			if err != nil {
				t.Fatal(err)
			}
			count, err := f.s.DeliverNotifications(ctx, 10)
			if err != nil || count != 1 {
				t.Fatal(count, err)
			}
			got, err := f.s.GetNotification(ctx, f.a, n.ID)
			if err != nil || got.Status != tc.status {
				t.Fatal(got, err)
			}
			count, err = f.s.DeliverNotifications(ctx, 10)
			if err != nil || count != 0 || calls != 1 {
				t.Fatal("attempt retried", count, calls, err)
			}
		})
	}
}
func TestNotificationDeliverySkipsMuted(t *testing.T) {
	f := newCoreFixture(t, storetest.NewTemplate(t))
	ctx := context.Background()
	id := f.ingest(t, "article")
	muted := surfaceAgent(t, f, "Muted", 3)
	clear := surfaceAgent(t, f, "Clear", 3)
	if _, err := f.db.Pool().Exec(ctx, "INSERT INTO devices(user_id,transport,endpoint) VALUES($1,'webpush','https://push.example/endpoint')", f.a.UserID); err != nil {
		t.Fatal(err)
	}
	var sent []string
	registry := push.NewRegistry()
	if err := registry.Register(push.KindWebPush, surfaceTransport{send: func(_ push.Device, m push.Message) error {
		sent = append(sent, m.NotificationID)
		return nil
	}}); err != nil {
		t.Fatal(err)
	}
	f.s.pushRegistry = registry
	blocked, err := f.s.SendNotification(ctx, muted, surfaceRequest(t, f, id, "muted"))
	if err != nil || blocked.Status != NotificationQueued {
		t.Fatal(blocked, err)
	}
	allowed, err := f.s.SendNotification(ctx, clear, surfaceRequest(t, f, id, "clear"))
	if err != nil || allowed.Status != NotificationQueued {
		t.Fatal(allowed, err)
	}
	// A mute committed after acceptance leaves a queued row; the claim
	// filters it instead of claiming it.
	if _, err := f.db.Pool().Exec(ctx, "INSERT INTO notification_mutes(user_id,principal_id) VALUES($1,$2)", f.a.UserID, muted.PrincipalID); err != nil {
		t.Fatal(err)
	}
	count, err := f.s.DeliverNotifications(ctx, 10)
	if err != nil || count != 1 || len(sent) != 1 || sent[0] != fmt.Sprint(allowed.ID) {
		t.Fatal("muted candidate delivered", count, sent, err)
	}
	got, err := f.s.GetNotification(ctx, f.a, blocked.ID)
	if err != nil || got.Status != NotificationQueued {
		t.Fatal("muted row touched", got, err)
	}
	if _, err := f.db.Pool().Exec(ctx, "DELETE FROM notification_mutes WHERE user_id=$1 AND principal_id=$2", f.a.UserID, muted.PrincipalID); err != nil {
		t.Fatal(err)
	}
	count, err = f.s.DeliverNotifications(ctx, 10)
	if err != nil || count != 1 || len(sent) != 2 || sent[1] != fmt.Sprint(blocked.ID) {
		t.Fatal("unmuted row lost", count, sent, err)
	}
}
func TestNotificationDeliveryHoldsQuietThenContinues(t *testing.T) {
	f := newCoreFixture(t, storetest.NewTemplate(t))
	ctx := context.Background()
	id := f.ingest(t, "article")
	a := surfaceAgent(t, f, "Morning", 3)
	if _, err := f.db.Pool().Exec(ctx, "INSERT INTO devices(user_id,transport,endpoint) VALUES($1,'webpush','https://push.example/endpoint')", f.a.UserID); err != nil {
		t.Fatal(err)
	}
	var sent []string
	registry := push.NewRegistry()
	if err := registry.Register(push.KindWebPush, surfaceTransport{send: func(_ push.Device, m push.Message) error {
		sent = append(sent, m.NotificationID)
		return nil
	}}); err != nil {
		t.Fatal(err)
	}
	f.s.pushRegistry = registry
	quiet, err := f.s.SendNotification(ctx, a, surfaceRequest(t, f, id, "quiet"))
	if err != nil || quiet.Status != NotificationQueued {
		t.Fatal(quiet, err)
	}
	a.Scopes = append(a.Scopes, ScopeNotifyUrgent)
	if _, err := f.db.Pool().Exec(ctx, "UPDATE tokens SET scopes=array_append(scopes,'notify:urgent') WHERE id=$1", a.TokenID); err != nil {
		t.Fatal(err)
	}
	urgent, err := f.s.SendNotification(ctx, a, surfaceRequest(t, f, id, "urgent"))
	if err != nil || urgent.Status != NotificationQueued {
		t.Fatal(urgent, err)
	}
	// Quiet hours enabled after acceptance: the claim holds the first
	// candidate and still delivers the urgent one behind it.
	if _, err := f.db.Pool().Exec(ctx, "INSERT INTO push_settings(user_id,quiet_start,quiet_end,timezone) VALUES($1,'11:00','13:00','UTC')", f.a.UserID); err != nil {
		t.Fatal(err)
	}
	count, err := f.s.DeliverNotifications(ctx, 10)
	if err != nil || count != 1 || len(sent) != 1 || sent[0] != fmt.Sprint(urgent.ID) {
		t.Fatal("claim stopped at quiet row", count, sent, err)
	}
	got, err := f.s.GetNotification(ctx, f.a, quiet.ID)
	if err != nil || got.Status != NotificationHeld || got.HeldUntil == nil || !got.HeldUntil.Equal(time.Date(2026, 11, 2, 13, 0, 0, 0, time.UTC)) {
		t.Fatal("quiet row not held until quiet end", got, err)
	}
	f.clock.Store(time.Date(2026, 11, 2, 13, 0, 0, 0, time.UTC).UnixNano())
	count, err = f.s.DeliverNotifications(ctx, 10)
	if err != nil || count != 1 || len(sent) != 2 || sent[1] != fmt.Sprint(quiet.ID) {
		t.Fatal("held row lost after quiet end", count, sent, err)
	}
}
func TestNotificationRestoreAndMute(t *testing.T) {
	template := storetest.NewTemplate(t)
	f := newCoreFixture(t, template)
	ctx := context.Background()
	id := f.ingest(t, "article")
	a := surfaceAgent(t, f, "Morning", 10)
	r := surfaceRequest(t, f, id, "not-accepted-before-restore")
	if _, err := f.db.RotateGeneration(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := f.s.SendNotification(ctx, a, r); !errors.Is(err, ErrGone) {
		t.Fatal("vector 19 replay accepted", err)
	}
	current := surfaceRequest(t, f, id, "accepted")
	n, err := f.s.SendNotification(ctx, a, current)
	if err != nil {
		t.Fatal(err)
	}
	f.s.pushRegistry = push.NewRegistry()
	count, err := f.s.DeliverNotifications(ctx, 1)
	if err != nil || count != 0 {
		t.Fatal("restore gate open", count, err)
	}
	if err := f.s.ReleaseNotifications(ctx, f.a, current.Generation, "release"); err != nil {
		t.Fatal(err)
	}
	// Restore pending acceptance and release only the next generation.
	next, err := f.db.RotateGeneration(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.s.ReleaseNotifications(ctx, f.a, next, "release-next"); err != nil {
		t.Fatal(err)
	}
	got, err := f.s.GetNotification(ctx, f.a, n.ID)
	if err != nil || got.Status != NotificationWithheld {
		t.Fatal("old generation released", got, err)
	}
	if _, err := f.s.SendNotification(ctx, a, current); !errors.Is(err, ErrGone) {
		t.Fatal("old op dedupe bypassed generation", err)
	}
	if err := f.s.MuteAgent(ctx, f.a, a.PrincipalID, NotificationsMuted, "mute"); err != nil {
		t.Fatal(err)
	}
	muted, err := f.s.SendNotification(ctx, a, surfaceRequest(t, f, id, "muted"))
	if err != nil || muted.Status != NotificationHeld {
		t.Fatal(muted, err)
	}
	count, err = f.s.DeliverNotifications(ctx, 10)
	if err != nil || count != 0 {
		t.Fatal("muted sent", count, err)
	}
}

func TestNotificationLocalDayAndInterruptedAttempt(t *testing.T) {
	template := storetest.NewTemplate(t)
	f := newCoreFixture(t, template)
	ctx := context.Background()
	id := f.ingest(t, "article")
	a := surfaceAgent(t, f, "Morning", 1)
	if _, err := f.db.Pool().Exec(ctx, "INSERT INTO push_settings(user_id,timezone) VALUES($1,'America/New_York')", f.a.UserID); err != nil {
		t.Fatal(err)
	}
	f.clock.Store(time.Date(2026, 11, 3, 4, 59, 59, 0, time.UTC).UnixNano())
	first, err := f.s.SendNotification(ctx, a, surfaceRequest(t, f, id, "before-midnight"))
	if err != nil {
		t.Fatal(err)
	}
	f.clock.Add(int64(2 * time.Second))
	second, err := f.s.SendNotification(ctx, a, surfaceRequest(t, f, id, "after-midnight"))
	if err != nil || second.Budget.UsedToday != 1 {
		t.Fatal("local day did not reset budget", second, err)
	}
	if _, err := f.db.Pool().Exec(ctx, "UPDATE notifications SET status='attempting',attempted_at=$2 WHERE id=$1", first.ID, f.s.now()); err != nil {
		t.Fatal(err)
	}
	recovered, err := f.s.RecoverNotificationAttempts(ctx)
	if err != nil || recovered != 1 {
		t.Fatal(recovered, err)
	}
	got, err := f.s.GetNotification(ctx, f.a, first.ID)
	if err != nil || got.Status != NotificationUncertain {
		t.Fatal(got, err)
	}
	recovered, err = f.s.RecoverNotificationAttempts(ctx)
	if err != nil || recovered != 0 {
		t.Fatal("recovery not idempotent", recovered, err)
	}
}

func TestNotificationPartlyDeliveredIsNotFailed(t *testing.T) {
	template := storetest.NewTemplate(t)
	f := newCoreFixture(t, template)
	ctx := context.Background()
	id := f.ingest(t, "article")
	a := surfaceAgent(t, f, "Morning", 3)
	for _, endpoint := range []string{"https://push.example/accepted", "https://push.example/rejected"} {
		if _, err := f.db.Pool().Exec(ctx, "INSERT INTO devices(user_id,transport,endpoint) VALUES($1,'webpush',$2)", f.a.UserID, endpoint); err != nil {
			t.Fatal(err)
		}
	}
	registry := push.NewRegistry()
	if err := registry.Register(push.KindWebPush, surfaceTransport{send: func(d push.Device, _ push.Message) error {
		if d.Endpoint == "https://push.example/accepted" {
			return nil
		}
		return &push.Error{Outcome: push.OutcomeNotDelivered, Err: errors.New("rejected")}
	}}); err != nil {
		t.Fatal(err)
	}
	f.s.pushRegistry = registry
	n, err := f.s.SendNotification(ctx, a, surfaceRequest(t, f, id, "send"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.s.DeliverNotifications(ctx, 1); err != nil {
		t.Fatal(err)
	}
	got, err := f.s.GetNotification(ctx, f.a, n.ID)
	if err != nil || got.Status != NotificationDelivered {
		t.Fatal("accepted device incorrectly reported failed", got.Status, err)
	}
}
