package web

import (
	"context"
	"crypto/hkdf"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/L-K-M/Earnest/internal/imageproxy"
	"github.com/L-K-M/Earnest/internal/service"
)

const contentLifetime = 5 * time.Minute

type ticket struct {
	UserID      int64  `json:"u"`
	PrincipalID int64  `json:"p"`
	EntryID     int64  `json:"e"`
	Expires     int64  `json:"x"`
	Mode        string `json:"m"`
}

type contentService interface {
	Content(context.Context, service.Actor, int64) (service.EntryContent, error)
}

// Content serves signed article capabilities and issues them to authenticated sessions.
type Content struct {
	svc    contentService
	auth   *service.Auth
	proxy  *imageproxy.Proxy
	origin string
	key    []byte
	now    func() time.Time
}

// NewContent prepares the isolated article handler and its signing key.
func NewContent(svc *service.Service, auth *service.Auth, proxy *imageproxy.Proxy, origin string, secret []byte) (*Content, error) {
	if svc == nil || auth == nil || proxy == nil || len(secret) < 32 {
		return nil, fmt.Errorf("content: missing service, auth, proxy or secret")
	}
	key, err := hkdf.Key(sha256.New, secret, nil, "earnest/content-url/v1", 32)
	if err != nil {
		return nil, fmt.Errorf("derive content key: %w", err)
	}
	return &Content{svc: svc, auth: auth, proxy: proxy, origin: origin, key: key, now: time.Now}, nil
}

func (c *Content) sign(t ticket) string {
	data, _ := json.Marshal(t)
	payload := base64.RawURLEncoding.EncodeToString(data)
	mac := hmac.New(sha256.New, c.key)
	mac.Write([]byte(payload))
	return payload + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

func (c *Content) verify(s string) (ticket, bool) {
	var t ticket
	payload, signature, ok := strings.Cut(s, ".")
	if !ok {
		return t, false
	}
	sig, err := base64.RawURLEncoding.DecodeString(signature)
	if err != nil {
		return t, false
	}
	mac := hmac.New(sha256.New, c.key)
	mac.Write([]byte(payload))
	if !hmac.Equal(sig, mac.Sum(nil)) {
		return t, false
	}
	data, err := base64.RawURLEncoding.DecodeString(payload)
	if err != nil || json.Unmarshal(data, &t) != nil || t.Expires <= c.now().Unix() || t.UserID <= 0 || t.PrincipalID <= 0 || t.EntryID <= 0 || (t.Mode != "proxied" && t.Mode != "click") {
		return ticket{}, false
	}
	return t, true
}

// URLHandler issues a short-lived content URL after checking the session and entry.
func (c *Content) URLHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	cookie, err := r.Cookie("earnest_session")
	if err != nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	actor, err := c.auth.ResolveSession(r.Context(), cookie.Value)
	if err != nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	id, err := strconv.ParseInt(r.PathValue("entry"), 10, 64)
	if err != nil || id <= 0 {
		http.NotFound(w, r)
		return
	}
	if _, err := c.svc.Content(r.Context(), actor, id); err != nil {
		http.NotFound(w, r)
		return
	}
	mode := r.URL.Query().Get("mode")
	if mode == "" {
		mode = "proxied"
	}
	if mode != "proxied" && mode != "click" {
		http.Error(w, "invalid image mode", http.StatusBadRequest)
		return
	}
	sig := c.sign(ticket{actor.UserID, actor.PrincipalID, id, c.now().Add(contentLifetime).Unix(), mode})
	u := c.origin + "/content/" + strconv.FormatInt(id, 10) + "?sig=" + url.QueryEscape(sig)
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"url": u})
}

// ServeHTTP serves a signed article without depending on browser cookies.
func (c *Content) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	t, ok := c.verify(r.URL.Query().Get("sig"))
	id, err := strconv.ParseInt(strings.TrimPrefix(r.URL.Path, "/content/"), 10, 64)
	if !ok || err != nil || id != t.EntryID {
		http.NotFound(w, r)
		return
	}
	actor := service.Actor{UserID: t.UserID, PrincipalID: t.PrincipalID, Kind: service.PrincipalHuman, Via: service.CredentialSession, Scopes: service.HumanScopes}
	body, err := c.svc.Content(r.Context(), actor, id)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	mode := imageproxy.Proxied
	if t.Mode == "click" {
		mode = imageproxy.ClickToLoad
	}
	fragment, err := c.proxy.Rewrite(body.HTML, mode)
	if err != nil {
		http.Error(w, "content unavailable", http.StatusInternalServerError)
		return
	}
	measure := 68
	if n, err := strconv.Atoi(r.URL.Query().Get("measure")); err == nil && n >= 62 && n <= 72 {
		measure = n
	}
	font := "Georgia,serif"
	spacing := ""
	switch r.URL.Query().Get("font") {
	case "sans":
		font = "system-ui,sans-serif"
	case "dyslexic":
		font = "Verdana,Arial,sans-serif"
		spacing = "letter-spacing:.05em;word-spacing:.12em;"
	}
	bg, fg := "#fff", "#202020"
	switch r.URL.Query().Get("theme") {
	case "dark":
		bg, fg = "#242424", "#eee"
	case "black":
		bg, fg = "#000", "#fff"
	case "eink":
		bg, fg = "#fff", "#000"
	}
	css := fmt.Sprintf("body{max-width:%dch;margin:2rem auto;padding:0 1rem;font:1.1rem/1.65 %s;background:%s;color:%s;%soverflow-wrap:anywhere}img{max-width:100%%;height:auto}a{color:inherit}pre{overflow:auto}", measure, font, bg, fg, spacing)
	hash := sha256.Sum256([]byte(css))
	w.Header().Set("Content-Security-Policy", "default-src 'none'; img-src "+c.origin+"; style-src 'sha256-"+base64.StdEncoding.EncodeToString(hash[:])+"'; base-uri 'none'; form-action 'none'")
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	fmt.Fprintf(w, "<!doctype html><html lang=\"en\"><head><meta charset=\"utf-8\"><meta name=\"viewport\" content=\"width=device-width,initial-scale=1\"><style>%s</style></head><body>%s</body></html>", css, fragment)
}
