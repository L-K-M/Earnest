package web

import (
	"context"
	"crypto/hkdf"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/L-K-M/Earnest/internal/imageproxy"
	"github.com/L-K-M/Earnest/internal/service"
)

// contentLifetime bounds a signed article capability. It must outlast a
// reading session: a reader who keeps an article open, whose iframe reloads
// under memory pressure, or who reopens a cached article expects the URL to
// still work. The content handler re-checks subscription visibility on every
// request, so an unsubscription stops an outstanding ticket immediately; the
// residual risk of a leaked URL is the same one the image proxy accepts.
const contentLifetime = 12 * time.Hour

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
	// appOrigin is the only origin the article page reports to. Without
	// it the page carries no script and scrolls inside its own frame.
	appOrigin string
}

// NewContent prepares the isolated article handler and its signing key.
// appOrigin is the web app's origin, the frame's only message recipient.
func NewContent(svc *service.Service, auth *service.Auth, proxy *imageproxy.Proxy, origin, appOrigin string, secret []byte) (*Content, error) {
	if svc == nil || auth == nil || proxy == nil {
		return nil, fmt.Errorf("content: missing service, auth or proxy")
	}
	if appOrigin == "" || appOrigin == origin {
		return nil, fmt.Errorf("content: the app origin must be set and differ from the content origin")
	}
	signer, err := NewTicketSigner(secret, origin)
	if err != nil {
		return nil, err
	}
	return &Content{svc: svc, auth: auth, proxy: proxy, signer: signer, appOrigin: appOrigin}, nil
}

// frameScript is the article page's only script; the page's CSP admits it
// by hash, so sanitised article HTML still cannot run any. It lets the web
// reader scroll the article in one place instead of a scrolling frame
// inside a scrolling pane. The page reports its height, including any
// horizontal scrollbar, so the reader can size the frame to the article. It
// forwards scrolling and single-key presses made while focus is in the
// frame, so Space and the reading shortcuts work after a click in the
// article; Space on a focused summary still opens its details, and Shift
// with an arrow, Home, End or a Page key still extends a selection. Messages go
// only to the app origin, and only when the page is framed; native clients
// run no script.
func frameScript(appOrigin string) string {
	target, _ := json.Marshal(appOrigin)
	return `(()=>{if(parent===self)return;const app=` + string(target) + `;let last=-1;` +
		`const send=()=>{const d=document.documentElement;const h=Math.ceil(d.getBoundingClientRect().height+innerHeight-d.clientHeight);if(h!==last){last=h;parent.postMessage({earnest:"height",height:h},app)}};` +
		`new ResizeObserver(send).observe(document.documentElement);addEventListener("load",send);addEventListener("resize",send);` +
		`const scroll=new Set([" ","PageDown","PageUp","Home","End","ArrowDown","ArrowUp"]);` +
		`addEventListener("keydown",e=>{if(e.defaultPrevented||e.ctrlKey||e.metaKey||e.altKey||e.isComposing)return;const k=e.key;` +
		`if(k===" "&&e.target instanceof Element&&e.target.closest("summary"))return;` +
		`if(e.shiftKey&&k!==" "&&scroll.has(k))return;` +
		`if(scroll.has(k))e.preventDefault();else if(k!=="Escape"&&k.length!==1)return;` +
		`parent.postMessage({earnest:"key",key:k,shift:e.shiftKey},app)})})()`
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

// framePolicy returns the CSP directive and element for frameScript, or
// nothing when no app origin is configured. Every page the frame can show
// carries it, so the reader never keeps an earlier article's height.
func (c *Content) framePolicy() (directive, element string) {
	if c.appOrigin == "" {
		return "", ""
	}
	js := frameScript(c.appOrigin)
	sum := sha256.Sum256([]byte(js))
	return "; script-src 'sha256-" + base64.StdEncoding.EncodeToString(sum[:]) + "'", "<script>" + js + "</script>"
}

// notice renders a short explanation in place of an article: an expired or
// invalid ticket, an article that is gone, or one that could not be shown.
// A bare error inside the reader's iframe looks like a broken article.
// The status stays honest for monitors and prefetchers; the browser still
// renders the body inside the frame. Its only script is frameScript, so it
// can tell the reader what to do but cannot refresh the ticket itself.
func (c *Content) notice(w http.ResponseWriter, status int, heading, text string) {
	directive, script := c.framePolicy()
	w.Header().Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'"+directive+"; base-uri 'none'; form-action 'none'")
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	_, _ = io.WriteString(w, `<!doctype html><html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><style>body{font:1rem/1.6 system-ui,sans-serif;color:#202020;background:#fff;max-width:34ch;margin:4rem auto;padding:0 1rem}h1{font-size:1.2rem}</style>`+script+`</head><body><h1>`+html.EscapeString(heading)+`</h1><p>`+html.EscapeString(text)+`</p></body></html>`)
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
		// Reopening the article fetches a fresh ticket from the app.
		c.notice(w, http.StatusGone, "This article link has expired", "Close the article and open it again in Earnest.")
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
		// A missing, hidden or no longer permitted article reads the same to
		// the reader and stays a 404; anything else is a server fault.
		if errors.Is(err, service.ErrNotFound) || errors.Is(err, service.ErrForbidden) || errors.Is(err, service.ErrUnauthorized) || errors.Is(err, service.ErrInvalid) {
			c.notice(w, http.StatusNotFound, "This article is not available", "It may have been removed, or you may no longer follow its feed.")
			return
		}
		slog.Error("content: load article", "entry", id, "error", err)
		c.notice(w, http.StatusInternalServerError, "This article could not be shown", "Close the article and try again.")
		return
	}
	mode := imageproxy.Proxied
	if t.Mode == ImageClickToLoad {
		mode = imageproxy.ClickToLoad
	}
	fragment, err := c.proxy.Rewrite(body.HTML, mode)
	if err != nil {
		slog.Error("content: rewrite article", "entry", id, "error", err)
		c.notice(w, http.StatusInternalServerError, "This article could not be shown", "Close the article and try again.")
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
	// Borders, captions and code blocks need theme-tuned colours so they do
	// not fall back to browser defaults that clash with the reading theme.
	bg, fg, muted, codeBg := "#fff", "#202020", "#6b675e", "#f3f1ea"
	switch r.URL.Query().Get("theme") {
	case "dark":
		bg, fg, muted, codeBg = "#242424", "#eee", "#a3a09a", "#2e2e2c"
	case "black":
		bg, fg, muted, codeBg = "#000", "#fff", "#9a9a9a", "#141414"
	case "eink":
		bg, fg, muted, codeBg = "#fff", "#000", "#000", "#fff"
	}
	css := fmt.Sprintf("body{max-width:%dch;margin:2rem auto;padding:0 1rem;font:1.1rem/1.65 %s;background:%s;color:%s;%soverflow-wrap:anywhere}"+
		"h1,h2,h3,h4{line-height:1.25;margin-top:1.6em;margin-bottom:.5em}h1{font-size:1.6em}h2{font-size:1.35em}h3{font-size:1.15em}h4{font-size:1em}"+
		"p,ul,ol,dl,blockquote,figure,table,pre{margin:.9em 0}"+
		"img{max-width:100%%;height:auto}"+
		"a{color:inherit;text-underline-offset:.15em}"+
		"code,pre{font-family:ui-monospace,SFMono-Regular,Menlo,Consolas,monospace;font-size:.88em}"+
		"pre{padding:.8em 1em;border-radius:6px;background:%s;overflow:auto}"+
		"pre code{font-size:1em}"+
		"blockquote{margin-inline:0;padding-inline-start:1em;border-inline-start:3px solid %s}"+
		"figure img{display:block;margin:0 auto}"+
		"figcaption{font-size:.85em;color:%s;margin-top:.4em;text-align:center}"+
		"table{border-collapse:collapse;display:block;overflow-x:auto;max-width:100%%}"+
		"th,td{border:1px solid %s;padding:.35em .6em;text-align:start;vertical-align:top}"+
		"hr{border:0;border-top:1px solid %s;margin:2em 0}",
		measure, font, bg, fg, spacing, codeBg, muted, muted, muted, muted)
	hash := sha256.Sum256([]byte(css))
	directive, script := c.framePolicy()
	w.Header().Set("Content-Security-Policy", "default-src 'none'; img-src "+c.signer.origin+"; style-src 'sha256-"+base64.StdEncoding.EncodeToString(hash[:])+"'"+directive+"; base-uri 'none'; form-action 'none'")
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	fmt.Fprintf(w, "<!doctype html><html lang=\"en\"><head><meta charset=\"utf-8\"><meta name=\"viewport\" content=\"width=device-width,initial-scale=1\"><style>%s</style>%s</head><body>%s</body></html>", css, script, fragment)
}
