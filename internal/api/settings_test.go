package api

import (
	"io"
	"log/slog"
	"net/http"
	"testing"

	"github.com/L-K-M/Earnest/internal/service"
	"github.com/L-K-M/Earnest/internal/store/storetest"
	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humago"
)

func intPtr(n int) *int { return &n }

// settingsHandler mounts only the settings routes, mirroring build() until
// api.go registers them.
func settingsHandler(f *apiFixture) http.Handler {
	mux := http.NewServeMux()
	cfg := huma.DefaultConfig("Earnest native API", "1")
	cfg.OpenAPIPath = ""
	cfg.DocsPath = ""
	cfg.SchemasPath = ""
	cfg.CreateHooks = nil
	h := humago.New(actionMux{mux}, cfg)
	s := &server{svc: f.svc, auth: f.auth, logger: slog.New(slog.NewTextHandler(io.Discard, nil)), api: h}
	h.UseMiddleware(s.authenticate)
	s.settingsRoutes()
	return mux
}

func settingsBodyFor(op string) settingsBody {
	return settingsBody{
		ReadingMode:        service.ReadingModeUnreadCounts,
		TodayDecayDays:     14,
		SingleKeyShortcuts: false,
		Theme:              service.ReaderThemeEink,
		Measure:            70,
		Font:               service.ReaderFontDyslexic,
		FeedOverrides:      map[string]service.ReaderOverride{"12": {Measure: intPtr(64)}},
		OpID:               op,
	}
}

func TestHTTPSettings(t *testing.T) {
	template := storetest.NewTemplate(t)

	t.Run("get put roundtrip and replay", func(t *testing.T) {
		f := newAPIFixture(t, template)
		f.handler = settingsHandler(f)
		rec := f.do(t, http.MethodGet, "/v1/settings", nil, f.session())
		requireStatus(t, rec, http.StatusOK)
		got := decodeBody[service.ReaderSettings](t, rec)
		if got.ReadingMode != service.ReadingModeRiver || got.TodayDecayDays != 2 || !got.SingleKeyShortcuts ||
			got.Theme != service.ReaderThemeSystem || got.Measure != 66 || got.Font != service.ReaderFontSerif || got.Rev != 0 {
			t.Fatalf("defaults: %+v", got)
		}
		rec = f.do(t, http.MethodPut, "/v1/settings", settingsBodyFor("s1"), f.session())
		requireStatus(t, rec, http.StatusOK)
		put := decodeBody[service.ReaderSettings](t, rec)
		if put.Theme != service.ReaderThemeEink || put.Rev == 0 {
			t.Fatalf("put: %+v", put)
		}
		rec = f.do(t, http.MethodPut, "/v1/settings", settingsBodyFor("s1"), f.session())
		requireStatus(t, rec, http.StatusOK)
		replay := decodeBody[service.ReaderSettings](t, rec)
		if replay.Rev != put.Rev {
			t.Fatalf("replay allocated a new rev: %+v vs %+v", replay, put)
		}
		rec = f.do(t, http.MethodGet, "/v1/settings", nil, f.session())
		requireStatus(t, rec, http.StatusOK)
		if got = decodeBody[service.ReaderSettings](t, rec); got.Rev != put.Rev || got.Theme != service.ReaderThemeEink || got.FeedOverrides["12"].Measure == nil {
			t.Fatalf("get: %+v", got)
		}
	})

	t.Run("validation and auth", func(t *testing.T) {
		f := newAPIFixture(t, template)
		f.handler = settingsHandler(f)
		for _, tc := range []struct {
			name string
			body settingsBody
			want int
		}{
			{"missing op_id", settingsBody{ReadingMode: service.ReadingModeRiver, TodayDecayDays: 7, SingleKeyShortcuts: true, Theme: service.ReaderThemeSystem, Measure: 66, Font: service.ReaderFontSerif}, http.StatusBadRequest},
			{"bad theme", settingsBody{ReadingMode: service.ReadingModeRiver, TodayDecayDays: 7, Theme: "neon", Measure: 66, Font: service.ReaderFontSerif, OpID: "bad-theme"}, http.StatusBadRequest},
			{"bad measure", settingsBody{ReadingMode: service.ReadingModeRiver, TodayDecayDays: 7, Theme: service.ReaderThemeDark, Measure: 100, Font: service.ReaderFontSerif, OpID: "bad-measure"}, http.StatusBadRequest},
		} {
			rec := f.do(t, http.MethodPut, "/v1/settings", tc.body, f.session())
			requireStatus(t, rec, tc.want)
			assertProblem(t, rec, "/problems/invalid-input")
		}
		rec := f.do(t, http.MethodGet, "/v1/settings", nil, requestCredentials{})
		requireStatus(t, rec, http.StatusUnauthorized)
		assertProblem(t, rec, "/problems/unauthorized")
		agent := requestCredentials{bearer: f.token(t, service.AudienceAPI)}
		rec = f.do(t, http.MethodGet, "/v1/settings", nil, agent)
		requireStatus(t, rec, http.StatusForbidden)
		assertProblem(t, rec, "/problems/forbidden")
		rec = f.do(t, http.MethodPut, "/v1/settings", settingsBodyFor("denied"), agent)
		requireStatus(t, rec, http.StatusForbidden)
		// Session writes still need the CSRF token.
		rec = f.do(t, http.MethodPut, "/v1/settings", settingsBodyFor("csrf"), requestCredentials{cookie: f.cookie})
		requireStatus(t, rec, http.StatusForbidden)
	})
}

// The production mount lives outside this task's assigned paths. With the
// reviewed route patch applied (or a Go overlay), exercise the real router.
func TestHTTPSettingsProductionMount(t *testing.T) {
	_, document := build(nil, nil, nil, "")
	if document.OpenAPI().Paths["/v1/settings"] == nil {
		t.Skip("settings route mount awaits integration in internal/api/api.go")
	}

	template := storetest.NewTemplate(t)
	f := newAPIFixture(t, template)
	rec := f.do(t, http.MethodGet, "/v1/settings", nil, f.session())
	requireStatus(t, rec, http.StatusOK)
	rec = f.do(t, http.MethodPut, "/v1/settings", settingsBodyFor("production-settings"), f.session())
	requireStatus(t, rec, http.StatusOK)
	got := decodeBody[service.ReaderSettings](t, rec)
	if got.Rev == 0 || got.ReadingMode != service.ReadingModeUnreadCounts {
		t.Fatalf("production settings: %+v", got)
	}
}
