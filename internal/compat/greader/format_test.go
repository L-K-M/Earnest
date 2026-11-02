package greader

import (
	"net/http/httptest"
	"strings"
	"testing"
)

func TestItemID(t *testing.T) {
	for _, tc := range []struct {
		input string
		id    int64
		valid bool
	}{
		{"1", 1, true}, {"tag:google.com,2005:reader/item/0000000000000001", 1, true}, {"9223372036854775807", 9223372036854775807, true}, {"0", 0, false}, {"-1", 0, false}, {"tag:google.com,2005:reader/item/ffffffffffffffff", 0, false}, {"not-an-id", 0, false},
	} {
		t.Run(tc.input, func(t *testing.T) {
			got, err := parseItemID(tc.input)
			if (err == nil) != tc.valid || (tc.valid && got != tc.id) {
				t.Fatalf("got %d, %v", got, err)
			}
			if tc.valid {
				round, err := parseItemID(itemID(got))
				if err != nil || round != got {
					t.Fatalf("round trip %d: %v", round, err)
				}
			}
		})
	}
}
func TestConditionalJSON(t *testing.T) {
	first := httptest.NewRecorder()
	conditionalJSON(first, httptest.NewRequest("GET", "/", nil), map[string]string{"a": "b"})
	tag := first.Header().Get("ETag")
	if tag == "" {
		t.Fatal("missing ETag")
	}
	r := httptest.NewRequest("GET", "/", nil)
	r.Header.Set("If-None-Match", tag)
	second := httptest.NewRecorder()
	conditionalJSON(second, r, map[string]string{"a": "b"})
	if second.Code != 304 || strings.TrimSpace(second.Body.String()) != "" {
		t.Fatalf("status %d body %q", second.Code, second.Body.String())
	}
}
