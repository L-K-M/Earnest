package api

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/L-K-M/Earnest/internal/clientip"
	"github.com/L-K-M/Earnest/internal/service"
)

type loginBody struct {
	Username string `json:"username"`
	Password string `json:"password"`
}
type loginResult struct {
	CSRFToken string    `json:"csrf_token"`
	ExpiresAt time.Time `json:"expires_at"`
}
type meResult struct {
	UserID      int64                 `json:"user_id"`
	PrincipalID int64                 `json:"principal_id"`
	Kind        service.PrincipalKind `json:"kind"`
	Scopes      []service.Scope       `json:"scopes"`
}
type tokenBody struct {
	Kind        service.TokenKind `json:"kind"`
	Name        string            `json:"name"`
	Scopes      []service.Scope   `json:"scopes,omitempty"`
	Audience    service.Audience  `json:"audience"`
	DailyBudget int               `json:"daily_budget"`
	ExpiresAt   *time.Time        `json:"expires_at,omitempty"`
}
type deviceStart struct {
	ClientName string `json:"client_name"`
}
type devicePoll struct {
	DeviceCode string `json:"device_code"`
}
type deviceApprove struct {
	UserCode string `json:"user_code"`
}
type deviceResult struct {
	DeviceCode      string `json:"device_code"`
	UserCode        string `json:"user_code"`
	VerificationURI string `json:"verification_uri"`
	Interval        int64  `json:"interval"`
	ExpiresIn       int64  `json:"expires_in"`
}
type deviceToken struct {
	AccessToken string `json:"access_token"`
	TokenType   string `json:"token_type"`
}

func (s *server) cookie(value string, expires time.Time) *http.Cookie {
	return &http.Cookie{Name: sessionCookie, Value: value, Path: "/", HttpOnly: true, Secure: s.secure, SameSite: http.SameSiteLaxMode, Expires: expires}
}

func (s *server) identityRoutes() {
	register(s, http.MethodPost, "/v1/session/login", "session-login", publicAccess, func(ctx context.Context, in *bodyInput[loginBody]) (loginResult, error) {
		// Behind a same-host proxy the socket address is the proxy's; use the
		// forwarded client or a per-credential bucket, never one shared bucket.
		client, ok := clientip.Key(request(ctx))
		if !ok {
			client = "user:" + in.Body.Username
		}
		a, err := s.auth.Authenticate(ctx, in.Body.Username, in.Body.Password, client)
		if err != nil {
			return loginResult{}, err
		}
		session, err := s.auth.CreateSession(ctx, a)
		if err != nil {
			return loginResult{}, err
		}
		w := ctx.Value(responseKey{}).(http.ResponseWriter)
		w.Header().Set("Cache-Control", "no-store")
		http.SetCookie(w, s.cookie(session.ID, session.ExpiresAt))
		return loginResult{CSRFToken: session.CSRF, ExpiresAt: session.ExpiresAt}, nil
	})
	route(s, http.MethodPost, "/v1/session/logout", "session-logout", func(ctx context.Context, in *empty) (empty, error) {
		cookie, err := request(ctx).Cookie(sessionCookie)
		if err != nil {
			return empty{}, service.ErrUnauthorized
		}
		if err := s.auth.RevokeSession(ctx, actor(ctx), cookie.Value); err != nil {
			return empty{}, err
		}
		cleared := s.cookie("", time.Unix(1, 0))
		cleared.MaxAge = -1
		http.SetCookie(ctx.Value(responseKey{}).(http.ResponseWriter), cleared)
		return empty{}, nil
	})
	route(s, http.MethodGet, "/v1/me", "get-me", func(ctx context.Context, in *empty) (meResult, error) {
		a := actor(ctx)
		return meResult{UserID: a.UserID, PrincipalID: a.PrincipalID, Kind: a.Kind, Scopes: a.Scopes}, nil
	})
	route(s, http.MethodGet, "/v1/tokens", "list-tokens", func(ctx context.Context, in *empty) ([]token, error) {
		result, err := s.auth.ListTokens(ctx, actor(ctx))
		out := make([]token, 0, len(result))
		for _, item := range result {
			out = append(out, token(item))
		}
		return out, err
	})
	route(s, http.MethodPost, "/v1/tokens", "create-token", func(ctx context.Context, in *bodyInput[tokenBody]) (tokenSecret, error) {
		b := in.Body
		ctx.Value(responseKey{}).(http.ResponseWriter).Header().Set("Cache-Control", "no-store")
		result, err := s.auth.CreateToken(ctx, actor(ctx), service.TokenOptions{Kind: b.Kind, Name: b.Name, Scopes: b.Scopes, Audience: b.Audience, DailyBudget: b.DailyBudget, ExpiresAt: b.ExpiresAt})
		return tokenSecret{Token: token(result.Token), Secret: result.Secret}, err
	})
	route(s, http.MethodDelete, "/v1/tokens/{id}", "revoke-token", func(ctx context.Context, in *idInput) (empty, error) {
		return empty{}, s.auth.RevokeToken(ctx, actor(ctx), in.ID)
	})
	register(s, http.MethodPost, "/v1/devices/start", "device-start", publicAccess, func(ctx context.Context, in *bodyInput[deviceStart]) (deviceResult, error) {
		// Authenticated requests still pass the service's human-only token gate.
		if actor(ctx).PrincipalID != 0 {
			if _, err := s.auth.ListTokens(ctx, actor(ctx)); err != nil {
				return deviceResult{}, err
			}
		}
		result, err := s.auth.StartDeviceAuth(ctx, in.Body.ClientName)
		if err != nil {
			return deviceResult{}, err
		}
		ctx.Value(responseKey{}).(http.ResponseWriter).Header().Set("Cache-Control", "no-store")
		return deviceResult{DeviceCode: result.DeviceCode, UserCode: result.UserCode, VerificationURI: s.baseURL + result.VerificationURI, Interval: int64(result.Interval / time.Second), ExpiresIn: int64(time.Until(result.ExpiresAt) / time.Second)}, nil
	})
	register(s, http.MethodPost, "/v1/devices/poll", "device-poll", publicAccess, func(ctx context.Context, in *bodyInput[devicePoll]) (deviceToken, error) {
		if actor(ctx).PrincipalID != 0 {
			if _, err := s.auth.ListTokens(ctx, actor(ctx)); err != nil {
				return deviceToken{}, err
			}
		}
		tok, err := s.auth.PollDevice(ctx, in.Body.DeviceCode)
		if err != nil {
			for _, code := range []error{service.ErrAuthorizationPending, service.ErrSlowDown, service.ErrAccessDenied, service.ErrExpiredToken} {
				if errors.Is(err, code) {
					p := problemStatus(http.StatusBadRequest)
					p.ErrorCode = code.Error()
					p.Detail = code.Error()
					return deviceToken{}, p
				}
			}
			return deviceToken{}, err
		}
		ctx.Value(responseKey{}).(http.ResponseWriter).Header().Set("Cache-Control", "no-store")
		return deviceToken{AccessToken: tok.Secret, TokenType: "Bearer"}, nil
	})
	route(s, http.MethodPost, "/v1/devices/approve", "device-approve", func(ctx context.Context, in *bodyInput[deviceApprove]) (empty, error) {
		return empty{}, s.auth.ApproveDevice(ctx, actor(ctx), in.Body.UserCode)
	})
}
