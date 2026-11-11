package main

import (
	"context"
	"net/http"

	"github.com/L-K-M/Earnest/internal/service"
)

// Pin routing at the surface boundary, not inside individual model calls, so
// all calls in one operation agree even when Settings is saved concurrently.
func modelHandler(svc *service.Service, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx, err := svc.ModelContext(r.Context())
		if err != nil {
			w.Header().Set("Content-Type", "application/problem+json")
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte(`{"type":"/problems/unavailable","title":"Service Unavailable","status":503,"detail":"LLM settings could not be loaded."}`))
			return
		}
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func modelJob(svc *service.Service, run func(context.Context, int) (int, error)) func(context.Context, int) (int, error) {
	return func(ctx context.Context, limit int) (int, error) {
		ctx, err := svc.ModelContext(ctx)
		if err != nil {
			return 0, err
		}
		return run(ctx, limit)
	}
}
