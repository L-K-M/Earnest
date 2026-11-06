// Package greader implements the Google Reader compatibility API.
package greader

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/L-K-M/Earnest/internal/service"
	"github.com/L-K-M/Earnest/internal/syncmodel"
)

const (
	prefix      = "/api/greader/"
	api         = "reader/api/0/"
	labelPrefix = "user/-/label/"
	statePrefix = "user/-/state/com.google/"
	itemPrefix  = "tag:google.com,2005:reader/item/"
)

type handler struct {
	svc    *service.Service
	auth   *service.Auth
	logger *slog.Logger
}

// New constructs a Google Reader handler mounted at /api/greader.
func New(svc *service.Service, auth *service.Auth, logger *slog.Logger) http.Handler {
	if logger == nil {
		logger = slog.Default()
	}
	return &handler{svc: svc, auth: auth, logger: logger}
}
func opID() string           { return rand.Text() }
func itemID(id int64) string { return fmt.Sprintf("%s%016x", itemPrefix, uint64(id)) }
func parseItemID(s string) (int64, error) {
	if strings.HasPrefix(s, itemPrefix) {
		n, err := strconv.ParseUint(strings.TrimPrefix(s, itemPrefix), 16, 64)
		if err != nil || n == 0 || n > uint64(^uint64(0)>>1) {
			return 0, service.ErrInvalid
		}
		return int64(n), nil
	}
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil || n <= 0 {
		return 0, service.ErrInvalid
	}
	return n, nil
}
func labelName(s string) (string, bool) {
	name, ok := strings.CutPrefix(s, labelPrefix)
	return name, ok && name != ""
}
func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(v)
}
func conditionalJSON(w http.ResponseWriter, r *http.Request, v any) {
	b, err := json.Marshal(v)
	if err != nil {
		http.Error(w, "Internal Server Error", 500)
		return
	}
	sum := sha256.Sum256(b)
	tag := `"` + hex.EncodeToString(sum[:]) + `"`
	w.Header().Set("ETag", tag)
	w.Header().Set("Cache-Control", "private, no-cache")
	for _, t := range strings.Split(r.Header.Get("If-None-Match"), ",") {
		t = strings.TrimSpace(t)
		if t == tag || t == "W/"+tag || t == "*" {
			w.WriteHeader(http.StatusNotModified)
			return
		}
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_, _ = w.Write(b)
}
func csrf(token string) string {
	s := sha256.Sum256([]byte("greader csrf\x00" + token))
	return hex.EncodeToString(s[:])
}
func (h *handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, prefix)
	if path == r.URL.Path {
		path = strings.TrimPrefix(r.URL.Path, "/")
	}
	if path == "accounts/ClientLogin" {
		if r.Method != http.MethodPost {
			http.Error(w, "Method Not Allowed", 405)
			return
		}
		if err := r.ParseForm(); err != nil {
			http.Error(w, "Bad Request", 400)
			return
		}
		client := r.RemoteAddr
		if host, _, err := net.SplitHostPort(client); err == nil {
			client = host
		}
		token, err := h.auth.CompatLogin(r.Context(), r.Form.Get("Email"), r.Form.Get("Passwd"), client)
		if err != nil {
			h.fail(w, err)
			return
		}
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		fmt.Fprintf(w, "SID=%s\nLSID=%s\nAuth=%s\n", token.Secret, token.Secret, token.Secret)
		return
	}
	token, ok := strings.CutPrefix(r.Header.Get("Authorization"), "GoogleLogin auth=")
	if !ok || token == "" {
		http.Error(w, "Unauthorized", 401)
		return
	}
	actor, err := h.auth.ResolveCompatToken(r.Context(), token)
	if err != nil {
		h.fail(w, err)
		return
	}
	if path == api+"token" {
		if r.Method != http.MethodGet {
			http.Error(w, "Method Not Allowed", 405)
			return
		}
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		fmt.Fprint(w, csrf(token))
		return
	}
	write := map[string]bool{"rename-tag": true, "disable-tag": true, "subscription/edit": true, "subscription/quickadd": true, "subscription/import": true, "edit-tag": true, "mark-all-as-read": true}
	name := strings.TrimPrefix(path, api)
	if path == name {
		http.NotFound(w, r)
		return
	}
	if write[name] {
		if r.Method != http.MethodPost {
			http.Error(w, "Method Not Allowed", 405)
			return
		}
		t := r.URL.Query().Get("T")
		if name != "subscription/import" {
			if err = r.ParseForm(); err != nil {
				h.fail(w, service.ErrInvalid)
				return
			}
			t = r.Form.Get("T")
		}
		expected := csrf(token)
		if subtle.ConstantTimeCompare([]byte(t), []byte(expected)) != 1 {
			http.Error(w, "Unauthorized", 401)
			return
		}
	} else if name == "stream/items/contents" {
		if r.Method != http.MethodPost {
			http.Error(w, "Method Not Allowed", 405)
			return
		}
		if err = r.ParseForm(); err != nil {
			h.fail(w, service.ErrInvalid)
			return
		}
	} else if r.Method != http.MethodGet {
		http.Error(w, "Method Not Allowed", 405)
		return
	}
	switch name {
	case "user-info":
		writeJSON(w, map[string]any{"userId": strconv.FormatInt(actor.UserID, 10), "userName": strconv.FormatInt(actor.UserID, 10), "userProfileId": strconv.FormatInt(actor.UserID, 10), "isBloggerUser": false, "signupTimeSec": 0})
	case "tag/list":
		err = h.tags(w, r, actor)
	case "rename-tag":
		err = h.renameTag(w, r, actor)
	case "disable-tag":
		err = h.disableTag(w, r, actor)
	case "subscription/list":
		err = h.subscriptions(w, r, actor)
	case "subscription/edit":
		err = h.subscriptionEdit(w, r, actor)
	case "subscription/quickadd":
		err = h.quickadd(w, r, actor)
	case "subscription/import":
		err = h.importOPML(w, r, actor)
	case "stream/items/ids":
		err = h.ids(w, r, actor)
	case "stream/items/contents":
		err = h.contents(w, r, actor)
	case "edit-tag":
		err = h.editTag(w, r, actor)
	case "mark-all-as-read":
		err = h.markAll(w, r, actor)
	case "unread-count":
		err = h.unreadCount(w, r, actor)
	default:
		http.NotFound(w, r)
		return
	}
	if err != nil {
		h.fail(w, err)
	}
}
func (h *handler) fail(w http.ResponseWriter, err error) {
	status := 500
	switch {
	case errors.Is(err, service.ErrUnauthorized):
		status = 401
	case errors.Is(err, service.ErrForbidden):
		status = 403
	case errors.Is(err, service.ErrInvalid):
		status = 400
	case errors.Is(err, service.ErrNotFound):
		status = 404
	case errors.Is(err, service.ErrConflict):
		status = 409
	case errors.Is(err, service.ErrRateLimited):
		status = 429
	case errors.Is(err, service.ErrUnavailable):
		status = 503
	}
	if status == 500 {
		h.logger.Error("google reader request failed", "error", err)
	}
	http.Error(w, http.StatusText(status), status)
}
func (h *handler) editTag(w http.ResponseWriter, r *http.Request, a service.Actor) error {
	raw := r.Form["i"]
	if len(raw) == 0 || len(raw) > 10000 {
		return service.ErrInvalid
	}
	ids := make([]int64, 0, len(raw))
	seen := map[int64]bool{}
	for _, s := range raw {
		id, err := parseItemID(s)
		if err != nil {
			return err
		}
		if !seen[id] {
			ids = append(ids, id)
			seen[id] = true
		}
	}
	type action struct {
		kind  syncmodel.GoogleAction
		label string
		add   bool
	}
	var actions []action
	for _, p := range []struct {
		key string
		add bool
	}{{"a", true}, {"r", false}} {
		for _, tag := range r.Form[p.key] {
			var act syncmodel.GoogleAction
			switch tag {
			case statePrefix + "read":
				if p.add {
					act = syncmodel.GoogleRead
				} else {
					act = syncmodel.GoogleUnread
				}
			case statePrefix + "kept-unread":
				if p.add {
					act = syncmodel.GoogleKeptUnread
				}
			case statePrefix + "starred":
				if p.add {
					act = syncmodel.GoogleStar
				} else {
					act = syncmodel.GoogleUnstar
				}
			default:
				name, ok := labelName(tag)
				if !ok {
					return service.ErrInvalid
				}
				actions = append(actions, action{label: name, add: p.add})
			}
			if act != "" {
				actions = append(actions, action{kind: act})
			}
		}
	}
	if len(actions) == 0 {
		return service.ErrInvalid
	}
	for _, act := range actions {
		if act.kind != "" {
			if _, err := h.svc.GoogleWrite(r.Context(), a, act.kind, ids, opID()); err != nil {
				return err
			}
			continue
		}
		labels, err := h.svc.Labels(r.Context(), a)
		if err != nil {
			return err
		}
		var id int64
		for _, l := range labels {
			if !l.Deleted && l.Name == act.label {
				id = l.ID
				break
			}
		}
		if id == 0 && act.add {
			l, err := h.svc.CreateLabel(r.Context(), a, act.label, opID())
			if err != nil {
				return err
			}
			id = l.ID
		}
		if id == 0 {
			continue
		}
		if act.add {
			_, err = h.svc.AddLabels(r.Context(), a, id, ids, opID())
		} else {
			_, err = h.svc.RemoveLabels(r.Context(), a, id, ids, opID())
		}
		if err != nil && (act.add || !errors.Is(err, service.ErrNotFound)) {
			return err
		}
	}
	_, _ = fmt.Fprint(w, "OK")
	return nil
}
func (h *handler) markAll(w http.ResponseWriter, r *http.Request, a service.Actor) error {
	scope := syncmodel.Scope{Kind: syncmodel.ScopeAll}
	s := r.Form.Get("s")
	var streamLabel string
	if s != "" && s != statePrefix+"reading-list" {
		if strings.HasPrefix(s, "feed/") {
			subs, err := h.svc.Subscriptions(r.Context(), a)
			if err != nil {
				return err
			}
			for _, sub := range subs {
				if sub.URL == strings.TrimPrefix(s, "feed/") {
					scope = syncmodel.Scope{Kind: syncmodel.ScopeSubscriptions, IDs: []syncmodel.ID{syncmodel.ID(sub.ID)}}
					break
				}
			}
			if scope.Kind == syncmodel.ScopeAll {
				return service.ErrNotFound
			}
		} else if name, ok := labelName(s); ok {
			streamLabel = name
		} else {
			return service.ErrInvalid
		}
	}
	cursor, err := h.svc.BootstrapCursor(r.Context(), a)
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	req := service.ThroughRequest{Scope: scope, Write: syncmodel.Write{OpID: opID(), Principal: syncmodel.ID(a.PrincipalID), Base: syncmodel.Revision{Generation: cursor.Generation}, OccurredAt: now, Kind: syncmodel.Passive, Register: syncmodel.Seen, Value: syncmodel.SeenValue}}
	if ts := r.Form.Get("ts"); ts != "" {
		n, e := strconv.ParseInt(ts, 10, 64)
		if e != nil || n < 0 {
			return service.ErrInvalid
		}
		t := time.UnixMicro(n).UTC()
		req.At = &t
	}
	if streamLabel != "" {
		_, err = h.svc.GoogleLabelThrough(r.Context(), a, req, streamLabel)
	} else {
		_, err = h.svc.Through(r.Context(), a, req)
	}
	if err != nil {
		return err
	}
	_, _ = fmt.Fprint(w, "OK")
	return nil
}
func (h *handler) unreadCount(w http.ResponseWriter, r *http.Request, a service.Actor) error {
	// One grouped query replaces paging every unread entry to count it.
	counts, err := h.svc.UnreadCounts(r.Context(), a)
	if err != nil {
		return err
	}
	writeJSON(w, map[string]any{"unreadcounts": []any{map[string]any{"id": statePrefix + "reading-list", "count": counts.Total, "newestItemTimestampUsec": "0"}}})
	return nil
}
