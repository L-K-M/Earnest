package llm

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/L-K-M/Earnest/internal/outbound"
	"github.com/L-K-M/Earnest/internal/store"
	"github.com/L-K-M/Earnest/internal/store/storetest"
)

func TestEstimateCompletion(t *testing.T) {
	schema := labelSchema(t)
	for _, test := range []struct {
		name    string
		req     CompleteRequest
		price   ModelInfo
		want    USD
		wantErr bool
	}{
		{"output maximum", CompleteRequest{MaxTokens: 60}, ModelInfo{OutputPer1M: 1000000}, 60, false},
		{"default maximum", CompleteRequest{}, ModelInfo{OutputPer1M: 1000000}, DefaultMaxTokens, false},
		{"negative maximum defaults", CompleteRequest{MaxTokens: -1}, ModelInfo{OutputPer1M: 1000000}, DefaultMaxTokens, false},
		{"input bytes and framing", CompleteRequest{System: "sys", Messages: []Message{{Role: RoleUser, Content: "界"}}}, ModelInfo{InputPer1M: 1000000}, USD(2*promptOverheadTokens + 3 + len("界") + len(RoleUser)), false},
		{"schema included", CompleteRequest{Schema: schema}, ModelInfo{InputPer1M: 1000000}, USD(promptOverheadTokens + len(schema.Definition) + len(schema.Name)), false},
		{"round up", CompleteRequest{MaxTokens: 1}, ModelInfo{OutputPer1M: 1}, 1, false},
		{"free model", CompleteRequest{MaxTokens: 1000}, ModelInfo{}, 0, false},
		{"negative price", CompleteRequest{MaxTokens: 1}, ModelInfo{OutputPer1M: -1}, 0, true},
		{"overflow", CompleteRequest{MaxTokens: math.MaxInt}, ModelInfo{OutputPer1M: math.MaxInt64}, 0, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			got, err := EstimateCompletion(test.req, test.price)
			if (err != nil) != test.wantErr || got != test.want {
				t.Fatalf("estimate=%d err=%v want=%d error=%v", got, err, test.want, test.wantErr)
			}
		})
	}
	if _, err := AddEstimates(math.MaxInt64, 1); err == nil {
		t.Fatal("overflowed batch sum accepted")
	}
}

func TestCompletionInputAndEmbeddingReserveBeforeCalling(t *testing.T) {
	for _, embedding := range []bool{false, true} {
		t.Run(fmt.Sprint("embedding=", embedding), func(t *testing.T) {
			var calls atomic.Int32
			provider := &fakeProvider{name: "paid", complete: func(CompleteRequest) (*Completion, error) {
				calls.Add(1)
				return &Completion{}, nil
			}, embed: func(EmbedRequest) (*EmbedResult, error) {
				calls.Add(1)
				return &EmbedResult{}, nil
			}}
			client, spend := newTestClient(t, provider, map[string]USD{"paid": 100}, map[string]ModelInfo{"test-model": {InputPer1M: 1000000, EmbeddingPer1M: 1000000}})
			var err error
			if embedding {
				_, _, err = client.Embed(t.Context(), TaskBlurb, EmbedRequest{Texts: []string{strings.Repeat("a", 101)}})
			} else {
				_, err = client.Complete(t.Context(), TaskBlurb, CompleteRequest{Messages: []Message{{Role: RoleUser, Content: strings.Repeat("a", 101)}}})
			}
			if !errors.Is(err, ErrBudgetExhausted) || calls.Load() != 0 || spend.spent != 0 {
				t.Fatalf("calls=%d spend=%d err=%v", calls.Load(), spend.spent, err)
			}
		})
	}
}

func TestClientReservationSettlement(t *testing.T) {
	for _, test := range []struct {
		name       string
		completion *Completion
		err        error
		want       USD
	}{
		{"successful usage", &Completion{Text: "ok", Usage: Usage{Output: 20}}, nil, 20},
		{"rejected output billed", &Completion{Usage: Usage{Output: 20}}, ErrInvalidOutput, 20},
		{"unbilled rejection", nil, &Error{StatusCode: 400, Err: errors.New("invalid request")}, 0},
		{"unbilled rate limit", nil, &Error{StatusCode: 429, Err: errors.New("rate limited")}, 0},
		{"unsupported", nil, ErrUnsupported, 0},
		{"DNS never sent", nil, &Error{Err: &outbound.Error{Class: outbound.DNS, Err: errors.New("no host")}}, 0},
		{"timeout billing unknown", nil, context.DeadlineExceeded, 60},
		{"server fault billing unknown", nil, &Error{StatusCode: 500, Err: errors.New("server fault")}, 60},
		{"usage missing", &Completion{Text: "ok"}, nil, 60},
	} {
		t.Run(test.name, func(t *testing.T) {
			provider := &fakeProvider{name: "paid", complete: func(req CompleteRequest) (*Completion, error) {
				return test.completion, test.err
			}}
			client, spend := newTestClient(t, provider, map[string]USD{"paid": 100}, map[string]ModelInfo{"test-model": {OutputPer1M: 1000000}})
			_, err := client.Complete(t.Context(), TaskBlurb, CompleteRequest{MaxTokens: 60})
			if (err != nil) != (test.err != nil) || spend.spent != int64(test.want) {
				t.Fatalf("spend=%d want=%d err=%v", spend.spent, test.want, err)
			}
		})
	}
}

func TestClientSendsReservedDefaultOutputBound(t *testing.T) {
	for _, kind := range []ProviderKind{KindOpenAI, KindAnthropic, KindGemini} {
		t.Run(string(kind), func(t *testing.T) {
			server, capture := newCapture(t, func(w http.ResponseWriter, r *http.Request) {
				switch kind {
				case KindOpenAI:
					io.WriteString(w, `{"choices":[{"message":{"content":"ok"}}],"usage":{"prompt_tokens":1,"completion_tokens":1}}`)
				case KindAnthropic:
					io.WriteString(w, `{"content":[{"type":"text","text":"ok"}],"usage":{"input_tokens":1,"output_tokens":1}}`)
				case KindGemini:
					io.WriteString(w, `{"candidates":[{"content":{"parts":[{"text":"ok"}]}}],"usageMetadata":{"promptTokenCount":1,"candidatesTokenCount":1}}`)
				}
			})
			client, spend := newTestClient(t, newTestProvider(t, kind, server.URL), map[string]USD{"test": DefaultMaxTokens}, map[string]ModelInfo{"test-model": {OutputPer1M: 1000000}})
			if _, err := client.Complete(t.Context(), TaskBlurb, CompleteRequest{}); err != nil {
				t.Fatal(err)
			}
			body := decodeBody(t, capture.one(t))
			var bound any
			switch kind {
			case KindOpenAI:
				bound = body["max_completion_tokens"]
			case KindAnthropic:
				bound = body["max_tokens"]
			case KindGemini:
				bound = body["generationConfig"].(map[string]any)["maxOutputTokens"]
			}
			if bound != float64(DefaultMaxTokens) || spend.spent != 1 {
				t.Fatalf("bound=%v spend=%d", bound, spend.spent)
			}
		})
	}
}

func TestClientPostgresConcurrentReservations(t *testing.T) {
	dsn := storetest.NewTemplate(t).DatabaseURL(t)
	firstStarted := make(chan struct{})
	releaseFirst := make(chan struct{})
	var calls atomic.Int32
	provider := &fakeProvider{name: "paid", complete: func(req CompleteRequest) (*Completion, error) {
		if calls.Add(1) == 1 {
			close(firstStarted)
			<-releaseFirst
		}
		return &Completion{Text: "ok", Usage: Usage{Output: 60}}, nil
	}}
	var clients []*Client
	for range 2 {
		db, err := store.Open(t.Context(), dsn)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(db.Close)
		router := &Router{providers: map[string]Provider{"paid": provider}, tasks: map[Task]TaskConfig{TaskBlurb: {Provider: "paid", Model: "model"}}}
		client, err := NewClient(router, NewBudget(db, map[string]USD{"paid": 100}), map[string]ModelInfo{"model": {OutputPer1M: 1000000}})
		if err != nil {
			t.Fatal(err)
		}
		clients = append(clients, client)
	}
	firstDone := make(chan error, 1)
	go func() {
		_, err := clients[0].Complete(t.Context(), TaskBlurb, CompleteRequest{MaxTokens: 60})
		firstDone <- err
	}()
	select {
	case <-firstStarted:
	case err := <-firstDone:
		t.Fatalf("first call failed before provider: %v", err)
	case <-time.After(10 * time.Second):
		t.Fatal("first call did not start")
	}
	_, secondErr := clients[1].Complete(t.Context(), TaskBlurb, CompleteRequest{MaxTokens: 60})
	close(releaseFirst)
	if err := <-firstDone; err != nil {
		t.Fatal(err)
	}
	spend, err := clients[1].budget.Spend(t.Context(), "paid")
	if err != nil || !errors.Is(secondErr, ErrBudgetExhausted) || calls.Load() != 1 || spend.Spent != 60 {
		t.Fatalf("calls=%d spend=%v first=%v second=%v", calls.Load(), spend, err, secondErr)
	}
}

func TestClientPostgresReconcilesAfterCancellationAndMonthRollover(t *testing.T) {
	db := storetest.NewTemplate(t).New(t)
	budget := NewBudget(db, map[string]USD{"paid": 100})
	november := time.Date(2026, 11, 30, 23, 59, 0, 0, time.UTC)
	budget.now = func() time.Time { return november }
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	provider := &fakeProvider{name: "paid", complete: func(CompleteRequest) (*Completion, error) {
		cancel()
		budget.now = func() time.Time { return november.Add(time.Hour) }
		return &Completion{Usage: Usage{Output: 20}}, ErrInvalidOutput
	}}
	router := &Router{providers: map[string]Provider{"paid": provider}, tasks: map[Task]TaskConfig{TaskBlurb: {Provider: "paid", Model: "model"}}}
	client, err := NewClient(router, budget, map[string]ModelInfo{"model": {OutputPer1M: 1000000}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.Complete(ctx, TaskBlurb, CompleteRequest{MaxTokens: 60}); !errors.Is(err, ErrInvalidOutput) {
		t.Fatalf("completion: %v", err)
	}
	var spent int64
	if err := db.Pool().QueryRow(t.Context(), `SELECT (spent_usd*1000000)::bigint FROM llm_spend WHERE provider='paid' AND month='2026-11-01'`).Scan(&spent); err != nil || spent != 20 {
		t.Fatalf("original month spend=%d err=%v", spent, err)
	}
	spend, err := budget.Spend(t.Context(), "paid")
	if err != nil || spend.Spent != 0 {
		t.Fatalf("new month spend=%v err=%v", spend, err)
	}
}

func TestEmbeddingEstimateIncludesEveryInput(t *testing.T) {
	got, err := estimateEmbedding(EmbedRequest{Texts: []string{"a", "界"}}, ModelInfo{EmbeddingPer1M: 1000000})
	if want := USD(2*promptOverheadTokens + len("a界")); err != nil || got != want {
		t.Fatalf("estimate=%d want=%d err=%v", got, want, err)
	}
}

func TestFractionalBilledUsageCannotEvadeCap(t *testing.T) {
	var calls int
	provider := &fakeProvider{name: "paid", complete: func(CompleteRequest) (*Completion, error) {
		calls++
		return &Completion{Text: "ok", Usage: Usage{Output: 1}}, nil
	}}
	client, spend := newTestClient(t, provider, map[string]USD{"paid": 1}, map[string]ModelInfo{"test-model": {OutputPer1M: 100000}})
	if _, err := client.Complete(t.Context(), TaskBlurb, CompleteRequest{MaxTokens: 1}); err != nil {
		t.Fatal(err)
	}
	if _, err := client.Complete(t.Context(), TaskBlurb, CompleteRequest{MaxTokens: 1}); !errors.Is(err, ErrBudgetExhausted) || calls != 1 || spend.spent != 1 {
		t.Fatalf("calls=%d spend=%d err=%v", calls, spend.spent, err)
	}
}
