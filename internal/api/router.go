package api

import (
	"net/http"
	"strings"
)

// actionMux adapts terminal ID actions to ServeMux's whole-segment wildcards.
// Huma keeps the public colon path for validation and OpenAPI.
type actionMux struct{ *http.ServeMux }

func (m actionMux) HandleFunc(pattern string, handler func(http.ResponseWriter, *http.Request)) {
	end := strings.Index(pattern, "}:")
	if end < 0 {
		m.ServeMux.HandleFunc(pattern, handler)
		return
	}

	start := strings.LastIndex(pattern[:end], "{")
	name, suffix := pattern[start+1:end], pattern[end+1:]
	m.ServeMux.HandleFunc(pattern[:end+1], func(w http.ResponseWriter, r *http.Request) {
		value := r.PathValue(name)
		id, ok := strings.CutSuffix(value, suffix)
		if !ok || id == "" {
			writeProblem(w, problemStatus(http.StatusNotFound))
			return
		}

		r.SetPathValue(name, id)
		handler(w, r)
	})
}
