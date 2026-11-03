package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/L-K-M/Earnest/internal/store/storetest"
)

func TestEvalCommandValidation(t *testing.T) {
	for _, args := range [][]string{{"--task", "unknown"}, {"--task", "summary", "extra"}} {
		if err := evalCommand(context.Background(), args, &bytes.Buffer{}); err == nil || !strings.Contains(err.Error(), "usage:") {
			t.Fatalf("args=%v err=%v", args, err)
		}
	}
}

func TestEvalCommandConfiguredFakeProvider(t *testing.T) {
	template := storetest.NewTemplate(t)
	t.Setenv("EARNEST_DATABASE_URL", template.DatabaseURL(t))
	t.Setenv("EARNEST_SECRET_KEY", strings.Repeat("s", 32))
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": map[string]string{"content": `{"claims":[{"text":"Beacon trial.","passages":["Beacon trial."]},{"text":"Beacon trial.","passages":["Beacon trial."]},{"text":"Beacon trial.","passages":["Beacon trial."]}]}`}}}, "usage": map[string]int{"prompt_tokens": 10, "completion_tokens": 5}})
	}))
	defer server.Close()
	u, _ := url.Parse(server.URL)
	t.Setenv("EARNEST_OUTBOUND_ALLOW_PRIVATE", u.Host)
	t.Setenv("EARNEST_LLM_PROVIDER_FAKE_KIND", "openai")
	t.Setenv("EARNEST_LLM_PROVIDER_FAKE_BASE_URL", server.URL)
	t.Setenv("EARNEST_LLM_TASK_SUMMARY_PROVIDER", "fake")
	t.Setenv("EARNEST_LLM_TASK_SUMMARY_MODEL", "fake-model")
	t.Setenv("EARNEST_LLM_MODELS", `{"fake-model":{"input_per_1m":0,"output_per_1m":0}}`)
	corpus := filepath.Join(t.TempDir(), "golden.json")
	if err := os.WriteFile(corpus, []byte(`[{"id":"one","title":"Beacon","text":"Beacon trial.","excerpt_only":false,"rubric":{"must_mention":["Beacon"],"must_not_mention":["sold"],"labels":["news"]}}]`), 0600); err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	if err := evalCommand(context.Background(), []string{"--task", "summary", "--corpus", corpus}, &output); err != nil {
		t.Fatal(err)
	}
	var report struct {
		Total, Passed int
		Rate          float64 `json:"citation_rate"`
	}
	if err := json.Unmarshal(output.Bytes(), &report); err != nil || report.Total != 1 || report.Passed != 1 || report.Rate != 1 {
		t.Fatalf("report %s %v", output.String(), err)
	}
}
