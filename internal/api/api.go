// Package api exposes the native REST API. Domain rules belong to service.
package api

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/L-K-M/Earnest/internal/syncmodel"
	"log/slog"
	"net/http"
	"net/url"
	"reflect"
	"strconv"
	"strings"

	"github.com/L-K-M/Earnest/internal/service"
	"github.com/L-K-M/Earnest/internal/web"
	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humago"
)

const sessionCookie = "earnest_session"
const csrfHeader = "X-CSRF-Token"

type routeAccess uint8

const (
	authenticatedAccess routeAccess = iota
	publicAccess
)

type actorKey struct{}
type requestKey struct{}
type responseKey struct{}
type server struct {
	svc            *service.Service
	auth           *service.Auth
	logger         *slog.Logger
	secure         bool
	baseURL        string
	api            huma.API
	signer         *web.TicketSigner
	vapidPublicKey string
}

// New returns the native API and generated OpenAPI handler. The optional
// content handler supplies the shared ticket signer for content URLs, and
// the optional VAPID public key enables browser push subscription setup.
func New(svc *service.Service, auth *service.Auth, logger *slog.Logger, baseURL string, content *web.Content, vapidPublicKey ...string) http.Handler {
	mux, _ := build(svc, auth, logger, baseURL, content, vapidPublicKey...)
	return SecurityHeaders(mux)
}

// OpenAPI generates the same deterministic document served at /openapi.json.
func OpenAPI() ([]byte, error) {
	_, api := build(nil, nil, slog.Default(), "", nil)
	data, err := json.MarshalIndent(api.OpenAPI(), "", "  ")
	if err != nil {
		return nil, err
	}
	return append(data, '\n'), nil
}

func build(svc *service.Service, auth *service.Auth, logger *slog.Logger, baseURL string, content *web.Content, vapidPublicKey ...string) (*http.ServeMux, huma.API) {
	if logger == nil {
		logger = slog.Default()
	}
	mux := http.NewServeMux()
	cfg := huma.DefaultConfig("Earnest native API", "1")
	cfg.OpenAPIPath = ""
	cfg.DocsPath = ""
	cfg.SchemasPath = ""
	cfg.CreateHooks = nil
	cfg.Components.Schemas.RegisterTypeAlias(reflect.TypeFor[syncmodel.Value](), reflect.TypeFor[registerValue]())
	cfg.Transformers = []huma.Transformer{func(ctx huma.Context, _ string, value any) (any, error) {
		if e, ok := value.(*huma.ErrorModel); ok {
			p := problemStatus(e.Status)
			p.Detail = e.Detail
			return p, nil
		}
		return value, nil
	}}
	cfg.Info.Description = "IDs are signed 64-bit JSON numbers. Mutations carry op_id where supported by the service. State operation paths contain literal colons. Entries use exclusive seq cursors; sync cursors carry generation and rev. SSE only hints: always read /v1/changes after connecting or receiving a hint."
	cfg.Components.SecuritySchemes = map[string]*huma.SecurityScheme{
		"bearer":  {Type: "http", Scheme: "bearer"},
		"session": {Type: "apiKey", In: "cookie", Name: sessionCookie},
		"csrf":    {Type: "apiKey", In: "header", Name: csrfHeader, Description: "Required with a session cookie on unsafe methods."},
	}
	h := humago.New(actionMux{mux}, cfg)
	var signer *web.TicketSigner
	if content != nil {
		signer = content.Signer()
	}
	s := &server{svc: svc, auth: auth, logger: logger, secure: strings.HasPrefix(baseURL, "https://"), baseURL: baseURL, api: h, signer: signer}
	if len(vapidPublicKey) > 0 {
		s.vapidPublicKey = vapidPublicKey[0]
	}
	h.UseMiddleware(s.authenticate)
	s.identityRoutes()
	s.readerRoutes()
	s.syncRoutes()
	s.opmlRoutes()
	s.annotationRoutes()
	s.notificationRoutes()
	s.pushRoutes()
	s.activityRoutes()
	s.webhookRoutes()
	s.searchRoutes()
	s.contentURLRoutes()
	s.ruleRoutes()
	s.settingsRoutes()
	s.embedRoutes()
	s.semanticRoutes()
	s.watchRoutes()
	s.generatedPipelineRoutes()
	s.documentProblems()
	mux.HandleFunc("/v1/", func(w http.ResponseWriter, r *http.Request) { writeProblem(w, problemStatus(http.StatusNotFound)) })
	mux.HandleFunc("GET /openapi.json", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		data, err := json.MarshalIndent(h.OpenAPI(), "", "  ")
		if err != nil {
			writeProblem(w, problemStatus(http.StatusInternalServerError))
			return
		}
		_, _ = w.Write(append(data, '\n'))
	})
	return mux, h
}

// SecurityHeaders applies the application's response policy to every route.
func SecurityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self'; img-src 'self'; connect-src 'self'; object-src 'none'; base-uri 'none'; form-action 'self'; frame-ancestors 'none'")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		next.ServeHTTP(w, r)
	})
}

func actor(ctx context.Context) service.Actor {
	a, _ := ctx.Value(actorKey{}).(service.Actor)
	return a
}
func request(ctx context.Context) *http.Request {
	r, _ := ctx.Value(requestKey{}).(*http.Request)
	return r
}
func (s *server) authenticate(ctx huma.Context, next func(huma.Context)) {
	r, w := humago.Unwrap(ctx)
	public := ctx.Operation().Metadata["public"] == true
	var a service.Actor
	var err error
	if authorization := r.Header.Get("Authorization"); authorization != "" {
		kind, token, ok := strings.Cut(authorization, " ")
		if !ok || !strings.EqualFold(kind, "Bearer") || token == "" {
			err = service.ErrUnauthorized
		} else {
			a, err = s.auth.ResolveToken(r.Context(), token, service.AudienceAPI)
		}
	} else if cookie, cookieErr := r.Cookie(sessionCookie); cookieErr == nil && ctx.Operation().OperationID != "session-login" {
		a, err = s.auth.ResolveSession(r.Context(), cookie.Value)
		if err == nil && r.Method != http.MethodGet && r.Method != http.MethodHead && r.Method != http.MethodOptions {
			err = s.auth.VerifyCSRF(r.Context(), cookie.Value, r.Header.Get(csrfHeader))
		}
		if err != nil && public {
			// Device pairing is also available before establishing a session.
			a, err = service.Actor{}, nil
		}
	} else if !public {
		err = service.ErrUnauthorized
	}
	if err != nil {
		writeProblem(w, s.problem(err))
		return
	}
	if r.Header.Get("Authorization") == "" && r.Method != http.MethodGet && r.Method != http.MethodHead && r.Method != http.MethodOptions {
		if origin := r.Header.Get("Origin"); origin != "" && !sameOrigin(origin, s.baseURL) {
			writeProblem(w, problemStatus(http.StatusForbidden))
			return
		}
	}
	c := context.WithValue(r.Context(), actorKey{}, a)
	c = context.WithValue(c, requestKey{}, r)
	c = context.WithValue(c, responseKey{}, w)
	next(huma.WithContext(ctx, c))
}

type problem struct {
	Type      string `json:"type"`
	Title     string `json:"title"`
	Status    int    `json:"status"`
	Detail    string `json:"detail,omitempty"`
	Budget    any    `json:"budget,omitempty"`
	ErrorCode string `json:"error,omitempty"`
}

func (p *problem) Error() string             { return p.Title }
func (p *problem) GetStatus() int            { return p.Status }
func (p *problem) ContentType(string) string { return "application/problem+json" }
func problemStatus(status int) *problem {
	names := map[int]string{http.StatusBadRequest: "invalid-input", http.StatusUnauthorized: "unauthorized", http.StatusForbidden: "forbidden", http.StatusNotFound: "not-found", http.StatusMethodNotAllowed: "method-not-allowed", http.StatusConflict: "conflict", http.StatusGone: "gone", http.StatusRequestEntityTooLarge: "content-too-large", http.StatusUnsupportedMediaType: "unsupported-media-type", http.StatusUnprocessableEntity: "invalid-input", http.StatusTooManyRequests: "rate-limited", http.StatusInternalServerError: "internal-error", http.StatusServiceUnavailable: "unavailable"}
	name, ok := names[status]
	if !ok {
		name = "http-error"
	}
	return &problem{Type: "/problems/" + name, Title: http.StatusText(status), Status: status}
}
func (s *server) problem(err error) *problem {
	status := http.StatusInternalServerError
	for _, mapping := range []struct {
		err    error
		status int
	}{
		{service.ErrUnauthorized, http.StatusUnauthorized}, {service.ErrForbidden, http.StatusForbidden}, {service.ErrNotFound, http.StatusNotFound}, {service.ErrInvalid, http.StatusBadRequest}, {service.ErrConflict, http.StatusConflict}, {service.ErrGone, http.StatusGone}, {service.ErrRateLimited, http.StatusTooManyRequests}, {service.ErrUnavailable, http.StatusServiceUnavailable},
	} {
		if errors.Is(err, mapping.err) {
			status = mapping.status
			break
		}
	}
	p := problemStatus(status)
	if status == http.StatusInternalServerError {
		s.logger.Error("API operation failed", "error", err)
	} else {
		p.Detail = err.Error()
	}
	if status == http.StatusTooManyRequests {
		// Budget-bearing service errors may supply structured details without HTTP dependencies.
		var notificationBudget *service.NotificationBudgetError
		var budget interface{ BudgetDetails() any }
		if errors.As(err, &notificationBudget) {
			p.Budget = notificationBudget.Budget
		} else if errors.As(err, &budget) {
			p.Budget = budget.BudgetDetails()
		} else {
			p.Budget = map[string]any{"reason": "rate_limit"}
		}
	}
	return p
}
func writeProblem(w http.ResponseWriter, p *problem) {
	w.Header().Set("Content-Type", "application/problem+json")
	if p.Status == http.StatusUnauthorized {
		w.Header().Set("WWW-Authenticate", "Bearer")
	}
	w.WriteHeader(p.Status)
	_ = json.NewEncoder(w).Encode(p)
}

type output[T any] struct{ Body T }
type empty struct{}
type idInput struct {
	ID int64 `path:"id" minimum:"1"`
}
type deleteInput struct {
	ID   int64  `path:"id" minimum:"1"`
	OpID string `query:"op_id" required:"true"`
}
type bodyInput[T any] struct{ Body T }
type idBody[T any] struct {
	ID   int64 `path:"id" minimum:"1"`
	Body T
}

func route[I, O any](s *server, method, path, id string, fn func(context.Context, *I) (O, error)) {
	routeStatus(s, method, path, id, http.StatusOK, fn)
}
func routeStatus[I, O any](s *server, method, path, id string, status int, fn func(context.Context, *I) (O, error)) {
	registerStatus(s, method, path, id, authenticatedAccess, status, fn)
}
func register[I, O any](s *server, method, path, id string, access routeAccess, fn func(context.Context, *I) (O, error)) {
	registerStatus(s, method, path, id, access, http.StatusOK, fn)
}
func registerStatus[I, O any](s *server, method, path, id string, access routeAccess, status int, fn func(context.Context, *I) (O, error)) {
	security := []map[string][]string{{"bearer": {}}, {"session": {}}}
	if method != http.MethodGet && method != http.MethodHead && method != http.MethodOptions {
		security[1]["csrf"] = []string{}
	}
	if access == publicAccess {
		security = []map[string][]string{}
	}
	huma.Register(s.api, huma.Operation{Method: method, Path: path, OperationID: id, DefaultStatus: status, Metadata: map[string]any{"public": access == publicAccess}, Security: security, Errors: []int{http.StatusBadRequest, http.StatusUnauthorized, http.StatusForbidden, http.StatusNotFound, http.StatusConflict, http.StatusGone, http.StatusTooManyRequests, http.StatusServiceUnavailable}, MaxBodyBytes: 8 << 20}, func(ctx context.Context, in *I) (*output[O], error) {
		result, err := fn(ctx, in)
		if err != nil {
			var p *problem
			if errors.As(err, &p) {
				return nil, p
			}
			return nil, s.problem(err)
		}
		return &output[O]{Body: result}, nil
	})
}

// registerValue describes the sync model's custom JSON string-or-boolean encoding.
type registerValue string

func (registerValue) Schema(huma.Registry) *huma.Schema {
	return &huma.Schema{OneOf: []*huma.Schema{{Type: "boolean"}, {Type: "string", Enum: []any{"seen", "unseen", "none", "later", "done"}}}}
}

func (s *server) documentProblems() {
	schema := s.api.OpenAPI().Components.Schemas.Schema(reflect.TypeFor[problem](), true, "Problem")
	for _, item := range s.api.OpenAPI().Paths {
		for _, op := range []*huma.Operation{item.Get, item.Post, item.Put, item.Patch, item.Delete} {
			if op == nil {
				continue
			}
			for status, response := range op.Responses {
				if len(status) == 3 && status[0] >= '4' {
					response.Content = map[string]*huma.MediaType{"application/problem+json": {Schema: schema}}
				}
			}
		}
	}
}

func sameOrigin(provided, base string) bool {
	a, err := url.Parse(provided)
	if err != nil {
		return false
	}
	b, err := url.Parse(base)
	if err != nil {
		return false
	}
	if a.User != nil || a.RawQuery != "" || a.Fragment != "" || (a.Path != "" && a.Path != "/") {
		return false
	}
	port := func(u *url.URL) string {
		if p := u.Port(); p != "" {
			n, err := strconv.Atoi(p)
			if err != nil {
				return "invalid"
			}
			return strconv.Itoa(n)
		}
		if u.Scheme == "https" {
			return "443"
		}
		return "80"
	}
	return strings.EqualFold(a.Scheme, b.Scheme) && strings.EqualFold(a.Hostname(), b.Hostname()) && port(a) == port(b)
}
