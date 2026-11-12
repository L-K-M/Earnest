package service

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/L-K-M/Earnest/internal/outbound"
	"github.com/L-K-M/Earnest/internal/store/storetest"
)

func TestDiscoverLLMModelsDraftAndSavedKeys(t *testing.T) {
	f := newCoreFixture(t, storetest.NewTemplate(t))
	var calls atomic.Int64
	var expected atomic.Value
	expected.Store("Bearer sk-test-private")
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.Method != http.MethodGet || r.URL.Path != "/v1/models" || r.Header.Get("Authorization") != expected.Load().(string) {
			t.Error("wrong discovery request or credentials")
		}
		io.WriteString(w, `{"data":[{"id":"available"}]}`)
	}))
	defer provider.Close()
	u, _ := url.Parse(provider.URL)
	client, err := outbound.New(outbound.Config{AllowHosts: []string{u.Host}, Spacing: time.Nanosecond})
	if err != nil {
		t.Fatal(err)
	}
	f.s.outbound = client
	settings := llmSettingsTestInput()
	settings.Providers[0].BaseURL = provider.URL + "/v1"
	input := LLMModelsInput{Provider: settings.Providers[0]}
	check := func(in LLMModelsInput) {
		t.Helper()
		got, err := f.s.DiscoverLLMModels(t.Context(), f.a, in)
		if err != nil || len(got.Models) != 1 || got.Models[0].ID != "available" {
			t.Fatalf("discovery: %+v %v", got, err)
		}
		raw, _ := json.Marshal(got)
		if strings.Contains(string(raw), "sk-test-private") {
			t.Fatal("leaked key")
		}
	}
	check(input)
	loaded, err := f.s.GetLLMSettings(t.Context(), f.a)
	if err != nil || loaded.Revision != 0 || len(loaded.Providers) != 0 {
		t.Fatal("discovery persisted draft")
	}
	var ops int
	if err := f.db.Pool().QueryRow(t.Context(), "SELECT count(*) FROM op_ids").Scan(&ops); err != nil || ops != 0 {
		t.Fatalf("discovery entered operation log: %d %v", ops, err)
	}
	saved, err := f.s.PutLLMSettings(t.Context(), f.a, settings)
	if err != nil {
		t.Fatal(err)
	}
	input.Revision = saved.Revision
	input.Provider.APIKey = ""
	check(input)
	before := calls.Load()
	stale := input
	stale.Revision = 0
	if _, err := f.s.DiscoverLLMModels(t.Context(), f.a, stale); !errors.Is(err, ErrConflict) {
		t.Fatalf("stale revision: %v", err)
	}
	moved := input
	moved.Provider.BaseURL = provider.URL + "/other"
	if _, err := f.s.DiscoverLLMModels(t.Context(), f.a, moved); !errors.Is(err, ErrInvalid) {
		t.Fatalf("credentials moved to new URL: %v", err)
	}
	changed := input
	changed.Provider.Kind = "anthropic"
	if _, err := f.s.DiscoverLLMModels(t.Context(), f.a, changed); !errors.Is(err, ErrInvalid) {
		t.Fatalf("credentials moved to new kind: %v", err)
	}
	if calls.Load() != before {
		t.Fatal("invalid input contacted provider")
	}
	input.Provider.Name = "renamed"
	expected.Store("")
	check(input) // Renaming cannot inherit the old name's secret.
	input.Provider.Name = settings.Providers[0].Name
	input.Provider.ClearAPIKey = true
	check(input)
	input.Provider.ClearAPIKey = false
	input.Provider.APIKey = "replacement"
	expected.Store("Bearer replacement")
	check(input)
	loaded, err = f.s.GetLLMSettings(t.Context(), f.a)
	if err != nil || loaded.Revision != saved.Revision || !loaded.Providers[0].HasAPIKey {
		t.Fatal("discovery changed saved configuration")
	}
	input.Provider.APIKey = ""
	expected.Store("Bearer sk-test-private")
	check(input) // Previewing replacement/clear did not modify the saved key.
}
