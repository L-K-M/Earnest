package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/L-K-M/Earnest/internal/service"
	"github.com/L-K-M/Earnest/internal/syncmodel"
	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humago"
)

const (
	sseWriteTimeout      = 10 * time.Second
	sseHeartbeatInterval = 20 * time.Second
)

type changesInput struct {
	Generation string `query:"generation"`
	Cursor     string `query:"cursor" doc:"generation:rev cursor returned by bootstrap or the preceding change page."`
	Rev        uint64 `query:"rev"`
	Limit      int    `query:"limit" minimum:"0" maximum:"1000"`
}
type bootstrapInput struct {
	Kind           service.BootstrapKind `path:"kind"`
	AfterID        int64                 `query:"after_id"`
	AfterPrincipal int64                 `query:"after_principal"`
	AfterView      string                `query:"after_view"`
	Limit          int                   `query:"limit" minimum:"0" maximum:"1000"`
	Since          time.Time             `query:"since"`
}
type eventsInput struct {
	LastEventID string `header:"Last-Event-ID" doc:"generation:rev; old generation or expired retention returns 410 before opening the stream."`
}

func (s *server) syncRoutes() {
	route(s, http.MethodGet, "/v1/changes", "get-changes", func(ctx context.Context, in *changesInput) (changesPage, error) {
		cursor := syncmodel.Revision{Generation: in.Generation, Rev: in.Rev}
		if in.Cursor != "" {
			parsed, err := parseCursor(in.Cursor)
			if err != nil {
				return changesPage{}, err
			}
			cursor = parsed
		}
		page, err := s.svc.Changes(ctx, actor(ctx), cursor, in.Limit)
		return changesPage{Changes: page.Changes, Next: page.Next, NextCursor: formatCursor(page.Next)}, err
	})
	route(s, http.MethodGet, "/v1/bootstrap", "bootstrap-cursor", func(ctx context.Context, in *empty) (syncmodel.Revision, error) {
		return s.svc.BootstrapCursor(ctx, actor(ctx))
	})
	route(s, http.MethodGet, "/v1/bootstrap/{kind}", "bootstrap-page", func(ctx context.Context, in *bootstrapInput) (service.BootstrapPage, error) {
		return s.svc.Bootstrap(ctx, actor(ctx), service.BootstrapRequest{Kind: in.Kind, AfterID: in.AfterID, AfterPrincipal: in.AfterPrincipal, AfterView: in.AfterView, Limit: in.Limit, Since: in.Since})
	})
	huma.Register(s.api, huma.Operation{Method: http.MethodGet, Path: "/v1/events", OperationID: "change-events", Security: []map[string][]string{{"bearer": {}}, {"session": {}}}, Errors: []int{http.StatusBadRequest, http.StatusUnauthorized, http.StatusForbidden, http.StatusGone, http.StatusServiceUnavailable}, Responses: map[string]*huma.Response{"200": {Description: "Coalesced change hints. Clients must query /v1/changes; no change payloads are streamed.", Content: map[string]*huma.MediaType{"text/event-stream": {Schema: &huma.Schema{Type: "string"}}}}}}, func(ctx context.Context, in *eventsInput) (*huma.StreamResponse, error) {
		// Subscribe before reading the cursor, closing the commit/connection race.
		hints, cancel, err := s.svc.WatchChanges(ctx, actor(ctx))
		if err != nil {
			return nil, s.problem(err)
		}
		authHints, cancelAuth, err := s.auth.WatchChanges(ctx, actor(ctx))
		if err != nil {
			cancel()
			return nil, s.problem(err)
		}
		cleanup := func() { cancel(); cancelAuth() }
		if in.LastEventID != "" {
			generation, revText, ok := strings.Cut(in.LastEventID, ":")
			rev, err := strconv.ParseUint(revText, 10, 64)
			if !ok || err != nil {
				cleanup()
				return nil, s.problem(service.ErrInvalid)
			}
			if _, err := s.svc.Changes(ctx, actor(ctx), syncmodel.Revision{Generation: generation, Rev: rev}, 1); err != nil {
				cleanup()
				return nil, s.problem(err)
			}
		}
		cursor, err := s.svc.BootstrapCursor(ctx, actor(ctx))
		if err != nil {
			cleanup()
			return nil, s.problem(err)
		}
		return &huma.StreamResponse{Body: func(hctx huma.Context) {
			defer cleanup()
			_, w := humago.Unwrap(hctx)
			w.Header().Set("Content-Type", "text/event-stream")
			w.Header().Set("Cache-Control", "no-store")
			w.Header().Set("X-Accel-Buffering", "no")
			controller := http.NewResponseController(w)
			send := func(c syncmodel.Revision) error {
				// Bound slow clients while allowing the stream itself to live indefinitely.
				_ = controller.SetWriteDeadline(time.Now().Add(sseWriteTimeout))
				defer controller.SetWriteDeadline(time.Time{})
				data, err := json.Marshal(c)
				if err != nil {
					return err
				}
				if _, err := fmt.Fprintf(w, "id: %s:%d\nevent: changes\ndata: %s\n\n", c.Generation, c.Rev, data); err != nil {
					return err
				}
				return controller.Flush()
			}
			if err := send(cursor); err != nil {
				return
			}
			heartbeat := time.NewTicker(sseHeartbeatInterval)
			defer heartbeat.Stop()
			for {
				select {
				case <-ctx.Done():
					return
				case <-hints:
				case <-authHints:
				case <-heartbeat.C:
					// Re-resolve credentials so revocation and session expiry close idle streams.
					r := request(ctx)
					var err error
					if a := actor(ctx); a.Via == service.CredentialSession {
						cookie, cookieErr := r.Cookie(sessionCookie)
						if cookieErr != nil {
							return
						}
						_, err = s.auth.ResolveSession(ctx, cookie.Value)
					} else {
						_, secret, _ := strings.Cut(r.Header.Get("Authorization"), " ")
						_, err = s.auth.ResolveToken(ctx, secret, service.AudienceAPI)
					}
					if err != nil {
						return
					}
				}
				cursor, err := s.svc.BootstrapCursor(ctx, actor(ctx))
				if err != nil {
					return
				}
				if err := send(cursor); err != nil {
					return
				}
			}
		}}, nil
	})
}

type changesPage struct {
	Changes    []service.ChangeRecord `json:"changes"`
	Next       syncmodel.Revision     `json:"next"`
	NextCursor string                 `json:"next_cursor"`
}

func parseCursor(text string) (syncmodel.Revision, error) {
	generation, revText, ok := strings.Cut(text, ":")
	rev, err := strconv.ParseUint(revText, 10, 64)
	if !ok || generation == "" || err != nil {
		return syncmodel.Revision{}, service.ErrInvalid
	}
	return syncmodel.Revision{Generation: generation, Rev: rev}, nil
}
func formatCursor(cursor syncmodel.Revision) string {
	return fmt.Sprintf("%s:%d", cursor.Generation, cursor.Rev)
}
