// Package web serves the embedded web client and, later, the sandboxed
// content origin. The React client has not been built yet, so New returns a
// placeholder that answers the root route instead of a 404.
package web

import "net/http"

const index = `<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>Earnest</title>
</head>
<body>
<p>Earnest is running. The web client is not built yet; use the API under /v1/ or the OpenAPI document at /openapi.json.</p>
</body>
</html>
`

// New returns the web client handler mounted at /. Until web/ ships a built
// bundle it serves a placeholder page.
func New() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(index))
	})
}
