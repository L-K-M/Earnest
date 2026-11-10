package llm

import (
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"

	"github.com/L-K-M/Earnest/internal/outbound"
	"github.com/L-K-M/Earnest/internal/store/storetest"
)

func TestClientNetworkFailureSettlement(t *testing.T) {
	for _, embedding := range []bool{false, true} {
		for _, tc := range []struct {
			name    string
			class   outbound.Class
			err     error
			charged bool
		}{
			{"dial_refused", outbound.Connect, &net.OpError{Op: "dial", Err: syscall.ECONNREFUSED}, false},
			{"read_reset", outbound.Connect, &net.OpError{Op: "read", Err: syscall.ECONNRESET}, true},
			{"write_reset", outbound.Connect, &net.OpError{Op: "write", Err: syscall.ECONNRESET}, true},
			{"unexpected_eof", outbound.Connect, io.ErrUnexpectedEOF, true},
			{"dns", outbound.DNS, &net.DNSError{Err: "no host"}, false},
			{"tls", outbound.TLS, errors.New("certificate rejected"), false},
			{"denied", outbound.DeniedAddress, errors.New("private address"), false},
		} {
			t.Run(fmt.Sprintf("embedding=%t/%s", embedding, tc.name), func(t *testing.T) {
				failure := &Error{Provider: "paid", Err: &outbound.Error{Class: tc.class, Err: &url.Error{Op: "Post", URL: "https://provider.test", Err: tc.err}}}
				var calls int
				provider := &fakeProvider{name: "paid",
					complete: func(CompleteRequest) (*Completion, error) { calls++; return nil, failure },
					embed:    func(EmbedRequest) (*EmbedResult, error) { calls++; return nil, failure },
				}
				price := ModelInfo{OutputPer1M: 1000000, EmbeddingPer1M: 1000000}
				completion := CompleteRequest{MaxTokens: 60}
				embed := EmbedRequest{Texts: []string{strings.Repeat("a", 60)}}
				estimate, err := EstimateCompletion(completion, price)
				if embedding {
					estimate, err = estimateEmbedding(embed, price)
				}
				if err != nil {
					t.Fatal(err)
				}
				client, spend := newTestClient(t, provider, map[string]USD{"paid": estimate}, map[string]ModelInfo{"test-model": price})
				call := func() error {
					if embedding {
						_, _, err := client.Embed(t.Context(), TaskBlurb, embed)
						return err
					}
					_, err := client.Complete(t.Context(), TaskBlurb, completion)
					return err
				}
				if err := call(); !errors.Is(err, failure) {
					t.Fatalf("error = %v, want provider failure", err)
				}
				want := int64(0)
				if tc.charged {
					want = int64(estimate)
				}
				if spend.spent != want || len(spend.reservations) != 0 {
					t.Fatalf("settled spend = %d, want %d; holds = %v", spend.spent, want, spend.reservations)
				}
				err = call()
				if tc.charged {
					if !errors.Is(err, ErrBudgetExhausted) || calls != 1 {
						t.Fatalf("uncertain call must exhaust the cap: calls=%d, error=%v", calls, err)
					}
				} else if !errors.Is(err, failure) || calls != 2 {
					t.Fatalf("unsent call must refund the cap: calls=%d, error=%v", calls, err)
				}
			})
		}
	}
}

func TestPostgresLostProviderResponseRetainsEstimate(t *testing.T) {
	db := storetest.NewTemplate(t).New(t)
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		conn, _, err := w.(http.Hijacker).Hijack()
		if err != nil {
			t.Error(err)
			return
		}
		// The provider received the request but its response is lost. No
		// real provider is contacted or billed by this regression test.
		conn.Close()
	}))
	defer server.Close()
	client, _ := newTestClient(t, newTestProvider(t, KindOpenAI, server.URL), nil, map[string]ModelInfo{"test-model": {OutputPer1M: 1000000}})
	client.budget = NewBudget(db, map[string]USD{"test": 60})
	if _, err := client.Complete(t.Context(), TaskBlurb, CompleteRequest{MaxTokens: 60}); outbound.ErrorClass(err) != outbound.Connect {
		t.Fatalf("error = %v, want outbound connection failure", err)
	}
	spend, err := client.budget.Spend(t.Context(), "test")
	if err != nil || spend.Spent != 60 {
		t.Fatalf("spend = %+v, error = %v, want retained estimate of 60", spend, err)
	}
	_, err = client.Complete(t.Context(), TaskBlurb, CompleteRequest{MaxTokens: 60})
	if !errors.Is(err, ErrBudgetExhausted) || calls.Load() != 1 {
		t.Fatalf("uncertain request must not be refunded: calls=%d, error=%v", calls.Load(), err)
	}
}
