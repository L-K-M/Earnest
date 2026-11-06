package greader

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/L-K-M/Earnest/internal/service"
	"github.com/L-K-M/Earnest/internal/syncmodel"
)

// streamTag maps one Google Reader stream or tag parameter to the service's
// entry predicate. Callers must check it with validStreamTag first.
func streamTag(tag string) service.EntryTag {
	switch tag {
	case "", statePrefix + "reading-list":
		return service.EntryTag{Kind: service.TagAll}
	case statePrefix + "read":
		return service.EntryTag{Kind: service.TagRead}
	case statePrefix + "starred":
		return service.EntryTag{Kind: service.TagKept}
	case statePrefix + "kept-unread":
		return service.EntryTag{Kind: service.TagUnread}
	}
	if strings.HasPrefix(tag, "feed/") {
		return service.EntryTag{Kind: service.TagFeed, Name: strings.TrimPrefix(tag, "feed/")}
	}
	name, _ := labelName(tag)
	return service.EntryTag{Kind: service.TagLabel, Name: name}
}
func validStreamTag(tag string) bool {
	if tag == "" || tag == statePrefix+"reading-list" || tag == statePrefix+"read" || tag == statePrefix+"starred" || tag == statePrefix+"kept-unread" || strings.HasPrefix(tag, "feed/") {
		return true
	}
	_, ok := labelName(tag)
	return ok
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
	for _, key := range []string{"s", "xt", "it"} {
		for _, v := range q[key] {
			if !validStreamTag(v) {
				return service.ErrInvalid
			}
		}
	}
	var require, exclude []service.EntryTag
	var feedTag, labelTag bool
	add := func(v string, out *[]service.EntryTag) {
		t := streamTag(v)
		feedTag = feedTag || t.Kind == service.TagFeed
		labelTag = labelTag || t.Kind == service.TagLabel
		*out = append(*out, t)
	}
	for _, v := range q["s"] {
		add(v, &require)
	}
	for _, v := range q["it"] {
		add(v, &require)
	}
	for _, v := range q["xt"] {
		add(v, &exclude)
	}
	// The page query carries every predicate; the maps below exist only to
	// recheck it in memory. Feed tags need subscription URLs, label tags also
	// need folder names and each page's memberships — fetched for the page
	// rather than bootstrapping the whole membership set.
	var urls, foldernames map[int64]string
	var subfolders map[int64]int64
	if feedTag || labelTag {
		subs, err := h.svc.Subscriptions(r.Context(), a)
		if err != nil {
			return err
		}
		urls = map[int64]string{}
		subfolders = map[int64]int64{}
		for _, s := range subs {
			urls[s.ID] = s.URL
			subfolders[s.ID] = s.FolderID
		}
	}
	if labelTag {
		folders, err := h.svc.Folders(r.Context(), a)
		if err != nil {
			return err
		}
		foldernames = map[int64]string{}
		for _, f := range folders {
			foldernames[f.ID] = f.Name
		}
	}
	var members map[int64][]string
	matches := func(t service.EntryTag, e service.Entry) bool {
		switch t.Kind {
		case service.TagAll:
			return true
		case service.TagRead:
			return e.State.GoogleRead()
		case service.TagKept:
			return e.State.Kept.Value == syncmodel.True
		case service.TagUnread:
			return e.State.Unread()
		case service.TagFeed:
			return urls[e.SubscriptionID] == t.Name
		case service.TagLabel:
			if foldernames[subfolders[e.SubscriptionID]] == t.Name {
				return true
			}
			for _, m := range members[e.ID] {
				if m == t.Name {
					return true
				}
			}
		}
		return false
	}
	list := []any{}
	last := uint64(0)
	more := false
	f := service.EntryFilter{View: service.ViewAll, Limit: 200, Direction: service.PageBackward, BeforeSeq: before, RequireTags: require, ExcludeTags: exclude, AfterTime: ot, BeforeTime: nt}
	for {
		entries, err := h.svc.Entries(r.Context(), a, f)
		if err != nil {
			return err
		}
		if labelTag && len(entries) > 0 {
			entryIDs := make([]int64, len(entries))
			for i, e := range entries {
				entryIDs[i] = e.ID
			}
			members, err = h.svc.EntryLabelNames(r.Context(), a, entryIDs)
			if err != nil {
				return err
			}
		}
		for _, e := range entries {
			pass := true
			for _, t := range require {
				if !matches(t, e) {
					pass = false
				}
			}
			for _, t := range exclude {
				if matches(t, e) {
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
		entries, err := h.svc.GetByIDs(r.Context(), a, ids[start:end])
		if err != nil {
			return err
		}
		members, err := h.svc.EntryLabelNames(r.Context(), a, ids[start:end])
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
