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

// ImageMode selects how the content origin loads remote images.
type ImageMode string

const (
	ImageProxied     ImageMode = "proxied"
	ImageClickToLoad ImageMode = "click"
)

// Ticket is the signed capability a content URL carries. The content origin
// rebuilds the actor from it, so every field is authenticated.
type Ticket struct {
	UserID      int64                 `json:"u"`
	PrincipalID int64                 `json:"p"`
	Kind        service.PrincipalKind `json:"k"`
	EntryID     int64                 `json:"e"`
	Expires     int64                 `json:"x"`
	Mode        ImageMode             `json:"m"`
}

// TicketSigner derives the content key and signs and verifies tickets. The
// web content handler and the native API share it, so a URL either issues is
// accepted by the other.
type TicketSigner struct {
	key    []byte
	origin string
	now    func() time.Time
}

// NewTicketSigner derives the content key from secret and binds issued URLs
// to origin.
func NewTicketSigner(secret []byte, origin string) (*TicketSigner, error) {
	if len(secret) < 32 {
		return nil, fmt.Errorf("content: secret must be at least 32 bytes")
	}
	key, err := hkdf.Key(sha256.New, secret, nil, "earnest/content-url/v1", 32)
	if err != nil {
		return nil, fmt.Errorf("derive content key: %w", err)
	}
	return &TicketSigner{key: key, origin: origin, now: time.Now}, nil
}

// Issue signs a fresh ticket for the actor and entry, and returns its URL
// and expiry.
func (s *TicketSigner) Issue(a service.Actor, entryID int64, mode ImageMode) (string, time.Time) {
	t := Ticket{UserID: a.UserID, PrincipalID: a.PrincipalID, Kind: a.Kind, EntryID: entryID, Expires: s.now().Add(contentLifetime).Unix(), Mode: mode}
	return s.URL(t), time.Unix(t.Expires, 0).UTC()
}

// URL renders the content origin URL for t.
func (s *TicketSigner) URL(t Ticket) string {
	return s.origin + "/content/" + strconv.FormatInt(t.EntryID, 10) + "?sig=" + url.QueryEscape(s.Sign(t))
}

// Sign returns the signed ticket token carried in a content URL.
func (s *TicketSigner) Sign(t Ticket) string {
	data, _ := json.Marshal(t)
	payload := base64.RawURLEncoding.EncodeToString(data)
	mac := hmac.New(sha256.New, s.key)
	mac.Write([]byte(payload))
	return payload + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

// Verify checks the signature, expiry and capability fields of a token.
func (s *TicketSigner) Verify(raw string) (Ticket, bool) {
	var t Ticket
	payload, signature, ok := strings.Cut(raw, ".")
	if !ok {
		return t, false
	}
	sig, err := base64.RawURLEncoding.DecodeString(signature)
	if err != nil {
		return t, false
	}
	mac := hmac.New(sha256.New, s.key)
	mac.Write([]byte(payload))
	if !hmac.Equal(sig, mac.Sum(nil)) {
		return t, false
	}
	data, err := base64.RawURLEncoding.DecodeString(payload)
	if err != nil || json.Unmarshal(data, &t) != nil || t.Expires <= s.now().Unix() || t.UserID <= 0 || t.PrincipalID <= 0 || t.EntryID <= 0 || (t.Kind != service.PrincipalHuman && t.Kind != service.PrincipalAgent) || (t.Mode != ImageProxied && t.Mode != ImageClickToLoad) {
		return Ticket{}, false
	}
	return t, true
}

type contentService interface {
	Content(context.Context, service.Actor, int64) (service.EntryContent, error)
}

// Content serves signed article capabilities and issues them to authenticated sessions.
type Content struct {
	svc    contentService
	auth   *service.Auth
	proxy  *imageproxy.Proxy
	signer *TicketSigner
}

// NewContent prepares the isolated article handler and its signing key.
func NewContent(svc *service.Service, auth *service.Auth, proxy *imageproxy.Proxy, origin string, secret []byte) (*Content, error) {
	if svc == nil || auth == nil || proxy == nil {
		return nil, fmt.Errorf("content: missing service, auth or proxy")
	}
	signer, err := NewTicketSigner(secret, origin)
	if err != nil {
		return nil, err
	}
	return &Content{svc: svc, auth: auth, proxy: proxy, signer: signer}, nil
}

// Signer exposes the signing half so another surface, such as the native API,
// can issue URLs this handler accepts.
func (c *Content) Signer() *TicketSigner { return c.signer }

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
	mode := ImageMode(r.URL.Query().Get("mode"))
	if mode == "" {
		mode = ImageProxied
	}
	if mode != ImageProxied && mode != ImageClickToLoad {
		http.Error(w, "invalid image mode", http.StatusBadRequest)
		return
	}
	u, _ := c.signer.Issue(actor, id, mode)
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
	t, ok := c.signer.Verify(r.URL.Query().Get("sig"))
	id, err := strconv.ParseInt(strings.TrimPrefix(r.URL.Path, "/content/"), 10, 64)
	if !ok || err != nil || id != t.EntryID {
		http.NotFound(w, r)
		return
	}
	// The content origin only reads stored bodies, so an agent ticket gets
	// exactly the read scope and never the human's wider scopes.
	scopes := service.HumanScopes
	if t.Kind == service.PrincipalAgent {
		scopes = []service.Scope{service.ScopeEntriesRead}
	}
	actor := service.Actor{UserID: t.UserID, PrincipalID: t.PrincipalID, Kind: t.Kind, Via: service.CredentialSession, Scopes: scopes}
	body, err := c.svc.Content(r.Context(), actor, id)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	mode := imageproxy.Proxied
	if t.Mode == ImageClickToLoad {
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
	if r.URL.Query().Get("font") == "sans" {
		font = "system-ui,sans-serif"
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
	css := fmt.Sprintf("body{max-width:%dch;margin:2rem auto;padding:0 1rem;font:1.1rem/1.65 %s;background:%s;color:%s;overflow-wrap:anywhere}img{max-width:100%%;height:auto}a{color:inherit}pre{overflow:auto}", measure, font, bg, fg)
	hash := sha256.Sum256([]byte(css))
	w.Header().Set("Content-Security-Policy", "default-src 'none'; img-src "+c.signer.origin+"; style-src 'sha256-"+base64.StdEncoding.EncodeToString(hash[:])+"'; base-uri 'none'; form-action 'none'")
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	fmt.Fprintf(w, "<!doctype html><html lang=\"en\"><head><meta charset=\"utf-8\"><meta name=\"viewport\" content=\"width=device-width,initial-scale=1\"><style>%s</style></head><body>%s</body></html>", css, fragment)
}
