package outbound

import (
	"net/url"
	"slices"
	"strings"
)

// querySecretNames are the query parameters treated as secrets in feed URLs,
// in lower case without '-' or '_'. A query can't be refused as a whole: many
// public feeds need one, as in ?feed=rss2.
var querySecretNames = []string{
	"token", "accesstoken", "authtoken", "apitoken", "feedtoken", "rsstoken", "privatetoken", "wltoken",
	"key", "apikey", "authkey", "feedkey", "rsskey",
	"auth", "secret", "password", "passwd", "passkey", "torrentpass",
}

var querySecretSeparators = strings.NewReplacer("-", "", "_", "")

// HasQuerySecret reports whether u's query gives a value to a parameter that
// feeds use for secrets. It can't recognise secrets in paths or other names.
func HasQuerySecret(u *url.URL) bool {
	if u == nil {
		return false
	}

	// url.Query drops malformed pairs and pairs holding semicolons, which a
	// server may still read.
	pairs := strings.FieldsFunc(u.RawQuery, func(r rune) bool { return r == '&' || r == ';' })

	return slices.ContainsFunc(pairs, func(pair string) bool {
		name, value, _ := strings.Cut(pair, "=")
		if value == "" {
			return false
		}
		if unescaped, err := url.QueryUnescape(name); err == nil {
			name = unescaped
		}

		name = querySecretSeparators.Replace(strings.ToLower(strings.TrimSpace(name)))

		return slices.Contains(querySecretNames, name)
	})
}
