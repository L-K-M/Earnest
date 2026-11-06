package greader

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/L-K-M/Earnest/internal/service"
	"github.com/L-K-M/Earnest/internal/syncmodel"
)

type membership struct {
	LabelID int64  `json:"label_id"`
	EntryID int64  `json:"entry_id"`
	Name    string `json:"name"`
	Deleted bool   `json:"deleted"`
}

func (h *handler) memberships(r *http.Request, a service.Actor) (map[int64][]string, error) {
	out := map[int64][]string{}
	req := service.BootstrapRequest{Kind: service.BootstrapLabelMemberships, Limit: 200}
	for {
		p, err := h.svc.Bootstrap(r.Context(), a, req)
		if err != nil {
			return nil, err
		}
		for _, b := range p.Items {
			var m membership
			if err = json.Unmarshal(b, &m); err != nil {
				return nil, err
			}
			if !m.Deleted {
				out[m.EntryID] = append(out[m.EntryID], m.Name)
			}
		}
		if !p.HasMore {
			break
		}
		req.AfterID = p.NextID
		req.AfterPrincipal = p.NextPrincipal
	}
	return out, nil
}
func entryTime(e service.Entry) time.Time {
	if e.PublishedAt != nil {
		return *e.PublishedAt
	}
	return e.FirstSeenAt
}
func (h *handler) ids(w http.ResponseWriter, r *http.Request, a service.Actor) error {
	q := r.URL.Query()
	n := 1000
	if q.Get("n") != "" {
		v, err := strconv.Atoi(q.Get("n"))
		if err != nil || v < 1 || v > 10000 {
			return service.ErrInvalid
		}
		n = v
	}
	var before uint64
	if q.Get("c") != "" {
		var err error
		before, err = strconv.ParseUint(q.Get("c"), 10, 64)
		if err != nil || before == 0 {
			return service.ErrInvalid
		}
	}
	var ot, nt int64
	var err error
	if q.Get("ot") != "" {
		ot, err = strconv.ParseInt(q.Get("ot"), 10, 64)
		if err != nil {
			return service.ErrInvalid
		}
	}
	if q.Get("nt") != "" {
		nt, err = strconv.ParseInt(q.Get("nt"), 10, 64)
		if err != nil {
			return service.ErrInvalid
		}
	}
	subs, err := h.svc.Subscriptions(r.Context(), a)
	if err != nil {
		return err
	}
	folders, err := h.svc.Folders(r.Context(), a)
	if err != nil {
		return err
	}
	members, err := h.memberships(r, a)
	if err != nil {
		return err
	}
	urls := map[int64]string{}
	subfolders := map[int64]int64{}
	for _, s := range subs {
		urls[s.ID] = s.URL
		subfolders[s.ID] = s.FolderID
	}
	foldernames := map[int64]string{}
	for _, f := range folders {
		foldernames[f.ID] = f.Name
	}
	matches := func(tag string, e service.Entry) bool {
		switch tag {
		case "", statePrefix + "reading-list":
			return true
		case statePrefix + "read":
			return e.State.GoogleRead()
		case statePrefix + "starred":
			return e.State.Kept.Value == syncmodel.True
		case statePrefix + "kept-unread":
			return e.State.Unread()
		}
		if strings.HasPrefix(tag, "feed/") {
			return urls[e.SubscriptionID] == strings.TrimPrefix(tag, "feed/")
		}
		if name, ok := labelName(tag); ok {
			if foldernames[subfolders[e.SubscriptionID]] == name {
				return true
			}
			for _, m := range members[e.ID] {
				if m == name {
					return true
				}
			}
		}
		return false
	}
	valid := func(tag string) bool {
		if tag == "" || tag == statePrefix+"reading-list" || tag == statePrefix+"read" || tag == statePrefix+"starred" || tag == statePrefix+"kept-unread" || strings.HasPrefix(tag, "feed/") {
			return true
		}
		_, ok := labelName(tag)
		return ok
	}
	for _, key := range []string{"s", "xt", "it"} {
		for _, v := range q[key] {
			if !valid(v) {
				return service.ErrInvalid
			}
		}
	}
	list := []any{}
	last := uint64(0)
	more := false
	f := service.EntryFilter{View: service.ViewAll, Limit: 200, Direction: service.PageBackward, BeforeSeq: before}
	for {
		entries, err := h.svc.Entries(r.Context(), a, f)
		if err != nil {
			return err
		}
		for _, e := range entries {
			pass := true
			for _, s := range q["s"] {
				if !matches(s, e) {
					pass = false
				}
			}
			for _, s := range q["xt"] {
				if matches(s, e) {
					pass = false
				}
			}
			for _, s := range q["it"] {
				if !matches(s, e) {
					pass = false
				}
			}
			t := entryTime(e).Unix()
			if ot != 0 && t <= ot {
				pass = false
			}
			if nt != 0 && t >= nt {
				pass = false
			}
			if !pass {
				continue
			}
			if len(list) == n {
				more = true
				break
			}
			list = append(list, map[string]string{"id": strconv.FormatInt(e.ID, 10)})
			last = e.Seq
		}
		if more || len(entries) < 200 {
			break
		}
		f.BeforeSeq = entries[len(entries)-1].Seq
	}
	out := map[string]any{"itemRefs": list}
	if more {
		out["continuation"] = strconv.FormatUint(last, 10)
	}
	writeJSON(w, out)
	return nil
}
func (h *handler) contents(w http.ResponseWriter, r *http.Request, a service.Actor) error {
	ids := []int64{}
	for _, v := range r.Form["i"] {
		id, err := parseItemID(v)
		if err != nil {
			return err
		}
		ids = append(ids, id)
	}
	if len(ids) == 0 || len(ids) > 10000 {
		return service.ErrInvalid
	}
	subs, err := h.svc.Subscriptions(r.Context(), a)
	if err != nil {
		return err
	}
	folders, err := h.svc.Folders(r.Context(), a)
	if err != nil {
		return err
	}
	members, err := h.memberships(r, a)
	if err != nil {
		return err
	}
	subMap := map[int64]service.Subscription{}
	folderMap := map[int64]string{}
	for _, s := range subs {
		subMap[s.ID] = s
	}
	for _, f := range folders {
		folderMap[f.ID] = f.Name
	}
	items := []any{}
	for start := 0; start < len(ids); start += 200 {
		end := start + 200
		if end > len(ids) {
			end = len(ids)
		}
		entries, err := h.svc.GetByIDsWithContent(r.Context(), a, ids[start:end])
		if err != nil {
			return err
		}
		for _, e := range entries {
			if e.Deleted {
				continue
			}
			s := subMap[e.SubscriptionID]
			categories := []string{statePrefix + "reading-list"}
			if e.State.GoogleRead() {
				categories = append(categories, statePrefix+"read")
			}
			if e.State.Kept.Value == syncmodel.True {
				categories = append(categories, statePrefix+"starred")
			}
			seen := map[string]bool{}
			if name := folderMap[s.FolderID]; name != "" {
				seen[name] = true
				categories = append(categories, labelPrefix+name)
			}
			for _, name := range members[e.ID] {
				if !seen[name] {
					categories = append(categories, labelPrefix+name)
					seen[name] = true
				}
			}
			content := ""
			if e.Content != nil {
				content = e.Content.HTML
			}
			ts := entryTime(e).Unix()
			items = append(items, map[string]any{"id": itemID(e.ID), "crawlTimeMsec": strconv.FormatInt(e.FirstSeenAt.UnixMilli(), 10), "timestampUsec": strconv.FormatInt(e.FirstSeenAt.UnixMicro(), 10), "published": ts, "updated": ts, "title": e.Title, "author": e.Author, "categories": categories, "origin": map[string]string{"streamId": "feed/" + s.URL, "title": s.Title, "htmlUrl": s.SiteURL}, "alternate": []any{map[string]string{"href": e.URL, "type": "text/html"}}, "content": map[string]string{"direction": "ltr", "content": content}})
		}
	}
	writeJSON(w, map[string]any{"id": statePrefix + "reading-list", "items": items, "updated": time.Now().Unix()})
	return nil
}
