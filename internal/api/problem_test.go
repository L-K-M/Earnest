package api

import (
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/L-K-M/Earnest/internal/service"
)

type budgetError struct{}

func (budgetError) Error() string      { return "daily notification limit" }
func (budgetError) Unwrap() error      { return service.ErrRateLimited }
func (budgetError) BudgetDetails() any { return map[string]int{"used_today": 3, "limit_today": 3} }

func TestProblems(t *testing.T) {
	s := &server{logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
	for _, tc := range []struct {
		err    error
		status int
		name   string
	}{
		{service.ErrUnauthorized, 401, "unauthorized"},
		{service.ErrForbidden, 403, "forbidden"},
		{service.ErrNotFound, 404, "not-found"},
		{service.ErrInvalid, 400, "invalid-input"},
		{service.ErrConflict, 409, "conflict"},
		{service.ErrGone, 410, "gone"},
		{service.ErrRateLimited, 429, "rate-limited"},
		{service.ErrUnavailable, 503, "unavailable"},
		{errors.New("database secret"), 500, "internal-error"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := s.problem(fmt.Errorf("operation: %w", tc.err))
			if p.Status != tc.status || p.Type != "/problems/"+tc.name {
				t.Fatalf("problem = %+v", p)
			}
			if tc.status == 500 && p.Detail != "" {
				t.Fatal("internal error leaked")
			}
			rec := httptest.NewRecorder()
			writeProblem(rec, p)
			if rec.Header().Get("Content-Type") != "application/problem+json" {
				t.Fatal("problem media type missing")
			}
		})
	}
	p := s.problem(budgetError{})
	if p.Budget.(map[string]int)["limit_today"] != 3 {
		t.Fatal("budget details lost")
	}
}

func TestSecurityHeaders(t *testing.T) {
	handler := SecurityHeaders(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(503) }))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest("GET", "/missing", nil))
	if rec.Header().Get("X-Content-Type-Options") != "nosniff" || rec.Header().Get("Referrer-Policy") != "no-referrer" {
		t.Fatal("response policy missing")
	}
	if rec.Header().Get("Content-Security-Policy") == "" {
		t.Fatal("CSP missing")
	}
}

func TestOrigins(t *testing.T) {
	for _, tc := range []struct {
		provided, base string
		want           bool
	}{
		{"https://reader.example", "https://reader.example:443", true},
		{"http://reader.example:80", "http://reader.example", true},
		{"https://READER.example", "https://reader.example", true},
		{"https://other.example", "https://reader.example", false},
		{"http://reader.example", "https://reader.example", false},
		{"https://reader.example:444", "https://reader.example", false},
		{"null", "https://reader.example", false},
		{"https://user@reader.example", "https://reader.example", false},
		{"https://reader.example/other", "https://reader.example", false},
		{"https:reader.example", "https://reader.example", false},
		{"about:blank", "https://reader.example", false},
		{"data:text/plain,hello", "https://reader.example", false},
		{"about:blank", "about:blank", false},
		{"data:text/plain,hello", "data:text/plain,hello", false},
	} {
		if got := sameOrigin(tc.provided, tc.base); got != tc.want {
			t.Errorf("sameOrigin(%q,%q)=%v", tc.provided, tc.base, got)
		}
	}
}
