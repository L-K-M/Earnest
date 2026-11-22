package service

import (
	"net/url"

	"github.com/L-K-M/Earnest/internal/outbound"
)

// Loopback needs no mention: the dial-time address check requires the same entry.
const credentialTransportRule = "require HTTPS, or an exact host:port in EARNEST_OUTBOUND_ALLOW_PRIVATE for HTTP"

// credentialsMayTravel applies the outbound rule before credentials are saved
// or probed, so a refusal is reported where it can be fixed.
func credentialsMayTravel(client *outbound.Client, rawURL string) bool {
	u, err := url.Parse(rawURL)
	return err == nil && client.AllowsCredentials(u)
}
