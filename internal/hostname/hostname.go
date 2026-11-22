// Package hostname compares host names where a match decides who may receive
// a secret.
//
// strings.ToLower and strings.EqualFold equate names that IDNA dials as
// different servers, such as wİki.test and wiki.test. Folding only ASCII
// letters cannot: two spellings of one international name may compare
// different, which withholds the secret rather than sharing it.
package hostname

// Lower returns host with its ASCII letters in lower case and every other
// byte unchanged.
func Lower(host string) string {
	b := []byte(host)
	for i, c := range b {
		if 'A' <= c && c <= 'Z' {
			b[i] = c + ('a' - 'A')
		}
	}

	return string(b)
}

// Equal reports whether a and b differ at most in ASCII case.
func Equal(a, b string) bool { return Lower(a) == Lower(b) }
