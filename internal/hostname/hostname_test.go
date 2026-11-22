package hostname

import "testing"

func TestEqualFoldsOnlyASCII(t *testing.T) {
	for _, tc := range []struct {
		name, a, b string
		want       bool
	}{
		{"ASCII case", "WIKI.Test", "wiki.test", true},
		{"same Unicode name", "bücher.TEST", "bücher.test", true},
		{"IPv6", "FD00::A", "fd00::a", true},
		{"dotted capital I", "wİki.test", "wiki.test", false},
		{"final sigma", "wσki.test", "wςki.test", false},
		{"long s", "wiſh.test", "wish.test", false},
		{"Kelvin sign", "wiKi.test", "wiki.test", false},
		{"Unicode case", "BÜCHER.test", "bücher.test", false},
		// strings.ToLower turns every invalid byte into U+FFFD.
		{"invalid bytes", "w\xffki.test", "w\xfeki.test", false},
		{"trailing dot", "wiki.test.", "wiki.test", false},
	} {
		if got := Equal(tc.a, tc.b); got != tc.want {
			t.Errorf("%s: Equal(%q, %q) = %t, want %t", tc.name, tc.a, tc.b, got, tc.want)
		}
	}
}
