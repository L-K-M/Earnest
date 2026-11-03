package greader

import (
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"

	"github.com/L-K-M/Earnest/internal/feed"
	"github.com/L-K-M/Earnest/internal/service"
)

func (h *handler) tags(w http.ResponseWriter, r *http.Request, a service.Actor) error {
	folders, err := h.svc.Folders(r.Context(), a)
	if err != nil {
		return err
	}
	labels, err := h.svc.Labels(r.Context(), a)
	if err != nil {
		return err
	}
	names := map[string]bool{}
	tags := []any{}
	for _, s := range []string{"read", "starred", "kept-unread"} {
		tags = append(tags, map[string]any{"id": statePrefix + s, "type": "system"})
	}
	for _, f := range folders {
		names[f.Name] = true
	}
	for _, l := range labels {
		if !l.Deleted {
			names[l.Name] = true
		}
	} // Sort for stable ETags.
	keys := make([]string, 0, len(names))
	for name := range names {
		keys = append(keys, name)
	}
	sort.Strings(keys)
	for _, name := range keys {
		tags = append(tags, map[string]any{"id": labelPrefix + name, "type": "folder"})
	}
	conditionalJSON(w, r, map[string]any{"tags": tags})
	return nil
}
func (h *handler) subscriptions(w http.ResponseWriter, r *http.Request, a service.Actor) error {
	subs, err := h.svc.Subscriptions(r.Context(), a)
	if err != nil {
		return err
	}
	folders, err := h.svc.Folders(r.Context(), a)
	if err != nil {
		return err
	}
	names := map[int64]string{}
	for _, f := range folders {
		names[f.ID] = f.Name
	}
	out := []any{}
	for _, s := range subs {
		if s.Deleted {
			continue
		}
		cats := []any{}
		if name := names[s.FolderID]; name != "" {
			cats = append(cats, map[string]any{"id": labelPrefix + name, "label": name})
		}
		out = append(out, map[string]any{"id": "feed/" + s.URL, "title": s.Title, "url": s.URL, "htmlUrl": s.SiteURL, "categories": cats, "sortid": fmt.Sprintf("%08x", s.ID), "firstitemmsec": "0"})
	}
	conditionalJSON(w, r, map[string]any{"subscriptions": out})
	return nil
}
func (h *handler) findSub(r *http.Request, a service.Actor, id string) (service.Subscription, error) {
	if !strings.HasPrefix(id, "feed/") {
		return service.Subscription{}, service.ErrInvalid
	}
	subs, err := h.svc.Subscriptions(r.Context(), a)
	if err != nil {
		return service.Subscription{}, err
	}
	for _, s := range subs {
		if !s.Deleted && s.URL == strings.TrimPrefix(id, "feed/") {
			return s, nil
		}
	}
	return service.Subscription{}, service.ErrNotFound
}
func (h *handler) folder(r *http.Request, a service.Actor, name string, create bool) (int64, error) {
	folders, err := h.svc.Folders(r.Context(), a)
	if err != nil {
		return 0, err
	}
	for _, f := range folders {
		if f.Name == name {
			return f.ID, nil
		}
	}
	if !create {
		return 0, service.ErrNotFound
	}
	f, err := h.svc.CreateFolder(r.Context(), a, name, opID())
	return f.ID, err
}
func (h *handler) subscriptionEdit(w http.ResponseWriter, r *http.Request, a service.Actor) error {
	id := r.Form.Get("s")
	action := r.Form.Get("ac")
	if action == "subscribe" {
		if !strings.HasPrefix(id, "feed/") {
			return service.ErrInvalid
		}
		opts := service.SubscribeOptions{OpID: opID(), Title: r.Form.Get("t")}
		for _, v := range r.Form["a"] {
			name, ok := labelName(v)
			if !ok {
				return service.ErrInvalid
			}
			folder, err := h.folder(r, a, name, true)
			if err != nil {
				return err
			}
			opts.FolderID = folder
		}
		_, err := h.svc.Subscribe(r.Context(), a, strings.TrimPrefix(id, "feed/"), opts)
		if err != nil {
			return err
		}
		fmt.Fprint(w, "OK")
		return nil
	}
	sub, err := h.findSub(r, a, id)
	if err != nil {
		return err
	}
	if action == "unsubscribe" {
		if err = h.svc.Unsubscribe(r.Context(), a, sub.ID, opID()); err != nil {
			return err
		}
		fmt.Fprint(w, "OK")
		return nil
	}
	if action != "edit" {
		return service.ErrInvalid
	}
	opts := service.UpdateSubscriptionOptions{OpID: opID()}
	if _, ok := r.Form["t"]; ok {
		v := r.Form.Get("t")
		opts.Title = &v
	}
	folder := sub.FolderID
	for _, v := range r.Form["r"] {
		name, ok := labelName(v)
		if !ok {
			return service.ErrInvalid
		}
		f, e := h.folder(r, a, name, false)
		if e != nil {
			return e
		}
		if f == folder {
			folder = 0
		}
	}
	for _, v := range r.Form["a"] {
		name, ok := labelName(v)
		if !ok {
			return service.ErrInvalid
		}
		folder, err = h.folder(r, a, name, true)
		if err != nil {
			return err
		}
	}
	if folder != sub.FolderID {
		opts.FolderID = &folder
	}
	if _, err = h.svc.UpdateSubscription(r.Context(), a, sub.ID, opts); err != nil {
		return err
	}
	fmt.Fprint(w, "OK")
	return nil
}
func (h *handler) quickadd(w http.ResponseWriter, r *http.Request, a service.Actor) error {
	s, err := h.svc.Subscribe(r.Context(), a, r.Form.Get("quickadd"), service.SubscribeOptions{OpID: opID()})
	if err != nil {
		return err
	}
	writeJSON(w, map[string]any{"numResults": 1, "query": r.Form.Get("quickadd"), "streamId": "feed/" + s.URL, "streamName": s.Title})
	return nil
}
func (h *handler) importOPML(w http.ResponseWriter, r *http.Request, a service.Actor) error {
	b, err := io.ReadAll(io.LimitReader(r.Body, 8<<20))
	if err != nil {
		return err
	}
	doc, err := feed.ParseOPML(b)
	if err != nil {
		return service.ErrInvalid
	}
	var convert func([]feed.Outline) []service.OPMLOutline
	convert = func(items []feed.Outline) []service.OPMLOutline {
		out := make([]service.OPMLOutline, 0, len(items))
		for _, x := range items {
			out = append(out, service.OPMLOutline{Title: x.Title, XMLURL: x.XMLURL, HTMLURL: x.HTMLURL, Outlines: convert(x.Children)})
		}
		return out
	}
	results, err := h.svc.ImportOPML(r.Context(), a, convert(doc.Outlines))
	if err != nil {
		return err
	}
	for _, result := range results {
		if result.Error != "" {
			return fmt.Errorf("import outline %v: %w", result.Path, service.ErrInvalid)
		}
	}
	fmt.Fprint(w, "OK")
	return nil
}
func (h *handler) renameTag(w http.ResponseWriter, r *http.Request, a service.Actor) error {
	old, ok := labelName(r.Form.Get("s"))
	if !ok {
		return service.ErrInvalid
	}
	name, ok := labelName(r.Form.Get("dest"))
	if !ok {
		return service.ErrInvalid
	}
	folders, err := h.svc.Folders(r.Context(), a)
	if err != nil {
		return err
	}
	labels, err := h.svc.Labels(r.Context(), a)
	if err != nil {
		return err
	}
	found := false
	for _, f := range folders {
		if f.Name == old {
			found = true
			if _, err = h.svc.RenameFolder(r.Context(), a, f.ID, name, opID()); err != nil {
				return err
			}
		}
	}
	for _, l := range labels {
		if !l.Deleted && l.Name == old {
			found = true
			if _, err = h.svc.RenameLabel(r.Context(), a, l.ID, name, opID()); err != nil {
				return err
			}
		}
	}
	if !found {
		return service.ErrNotFound
	}
	fmt.Fprint(w, "OK")
	return nil
}
func (h *handler) disableTag(w http.ResponseWriter, r *http.Request, a service.Actor) error {
	name, ok := labelName(r.Form.Get("s"))
	if !ok {
		return service.ErrInvalid
	}
	folders, err := h.svc.Folders(r.Context(), a)
	if err != nil {
		return err
	}
	labels, err := h.svc.Labels(r.Context(), a)
	if err != nil {
		return err
	}
	found := false
	for _, f := range folders {
		if f.Name == name {
			found = true
			if err = h.svc.DeleteFolder(r.Context(), a, f.ID, opID()); err != nil {
				return err
			}
		}
	}
	for _, l := range labels {
		if !l.Deleted && l.Name == name {
			found = true
			if _, err = h.svc.DeleteLabel(r.Context(), a, l.ID, opID()); err != nil {
				return err
			}
		}
	}
	if !found {
		return service.ErrNotFound
	}
	fmt.Fprint(w, "OK")
	return nil
}
