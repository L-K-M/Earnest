package pipeline

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/L-K-M/Earnest/internal/llm"
)

func TestEditionBatchRejectsIncompleteOutputAndKeepsUsage(t *testing.T) {
	for _, tc := range []struct {
		name     string
		reason   string
		complete bool
		usage    bool
	}{
		{"omitted", "", true, true},
		{"stop", "stop", true, true},
		{"length", "length", false, true},
		{"content filter", "content_filter", false, true},
		{"tool calls", "tool_calls", false, true},
		{"unknown", "other", false, true},
		{"length without usage", "length", false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			spend := &batchSpend{}
			batch, _, newBatch := reservationBatch(t, spend, 200, func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/files":
					io.WriteString(w, `{"id":"file-one"}`)
				case "/batches":
					io.WriteString(w, `{"id":"batch-one"}`)
				case "/batches/batch-one":
					io.WriteString(w, `{"status":"completed","output_file_id":"output"}`)
				case "/files/output/content":
					for i := range 2 {
						choice := map[string]any{"message": map[string]string{"content": `"ok"`}}
						if i == 1 {
							choice["finish_reason"] = "stop"
						} else if tc.reason != "" {
							choice["finish_reason"] = tc.reason
						}
						body := map[string]any{"choices": []any{choice}}
						if tc.usage {
							body["usage"] = map[string]int{"prompt_tokens": 1, "completion_tokens": 20 + i*10}
						}
						_ = json.NewEncoder(w).Encode(map[string]any{"custom_id": fmt.Sprint(i), "response": map[string]any{"status_code": http.StatusOK, "body": body}})
					}
				default:
					t.Error("unexpected request", r.URL.Path)
				}
			})
			requests := []llm.CompleteRequest{reservedBatchRequest(t, 60), reservedBatchRequest(t, 60)}
			id, err := batch.Submit(t.Context(), requests)
			if err != nil || spend.spent != 120 {
				t.Fatalf("submit spend=%d err=%v", spend.spent, err)
			}
			// Polling after a restart and repeated polling must preserve the charge.
			for range 2 {
				outputs, done, err := newBatch().Poll(t.Context(), id, requests)
				if tc.complete {
					if err != nil || !done || len(outputs) != 2 {
						t.Fatalf("outputs=%q done=%v err=%v", outputs, done, err)
					}
				} else if !errors.Is(err, llm.ErrInvalidOutput) || !strings.Contains(err.Error(), tc.reason) || done || outputs != nil {
					t.Errorf("incomplete output accepted: outputs=%q done=%v err=%v", outputs, done, err)
				}
				want := int64(50)
				if !tc.usage {
					want = 120
				}
				if spend.spent != want {
					t.Fatalf("spend=%d want=%d", spend.spent, want)
				}
			}
		})
	}
}
