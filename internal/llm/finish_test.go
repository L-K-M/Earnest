package llm

import (
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
)

// A completion cut short by the token limit or a filter is not an answer:
// every provider must reject it while still reporting the billed usage.
func TestFinishReasonRejectsIncompleteOutput(t *testing.T) {
	cases := []struct {
		kind     ProviderKind
		reason   string
		complete bool
		body     func(reason string) string
	}{
		{KindOpenAI, "", true, openAIBody},
		{KindOpenAI, "stop", true, openAIBody},
		{KindOpenAI, "length", false, openAIBody},
		{KindOpenAI, "content_filter", false, openAIBody},
		{KindAnthropic, "", true, anthropicBody},
		{KindAnthropic, "end_turn", true, anthropicBody},
		{KindAnthropic, "stop_sequence", true, anthropicBody},
		{KindAnthropic, "max_tokens", false, anthropicBody},
		{KindAnthropic, "refusal", false, anthropicBody},
	}
	for _, tc := range cases {
		t.Run(string(tc.kind)+"/"+tc.reason, func(t *testing.T) {
			server, _ := newCapture(t, func(w http.ResponseWriter, r *http.Request) {
				io.WriteString(w, tc.body(tc.reason))
			})
			provider := newTestProvider(t, tc.kind, server.URL)
			result, err := provider.Complete(t.Context(), CompleteRequest{Model: "test-model", Schema: labelSchema(t)})
			if tc.complete {
				if err != nil || result == nil || result.Text != `{"label":"news"}` {
					t.Fatalf("result=%+v error=%v", result, err)
				}
				return
			}
			if !errors.Is(err, ErrInvalidOutput) || !strings.Contains(err.Error(), tc.reason) {
				t.Fatalf("%s accepted: error=%v", tc.reason, err)
			}
			if result == nil || result.Text != "" || result.Usage != (Usage{Input: 12, Output: 6}) {
				t.Fatalf("rejected result must retain usage only: %+v", result)
			}
		})
	}
}

func openAIBody(reason string) string {
	return `{"choices":[{"finish_reason":` + quoteJSON(reason) + `,"message":{"content":"{\"label\":\"news\"}"}}],"usage":{"prompt_tokens":12,"completion_tokens":6}}`
}

func anthropicBody(reason string) string {
	return `{"stop_reason":` + quoteJSON(reason) + `,"content":[{"type":"text","text":"{\"label\":\"news\"}"}],"usage":{"input_tokens":12,"output_tokens":6}}`
}
