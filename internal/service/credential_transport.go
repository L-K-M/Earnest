package service

import (
	"fmt"
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

// validateQuerySecretTransport applies the same rule to a feed URL's query.
func validateQuerySecretTransport(client *outbound.Client, rawURL string) error {
	u, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("feed URL: %w", ErrInvalid)
	}
	if outbound.HasQuerySecret(u) && !client.AllowsCredentials(u) {
		return fmt.Errorf("secrets in feed URLs %s: %w", credentialTransportRule, ErrInvalid)
	}

	return nil
}
