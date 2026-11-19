package deployment

import (
	"fmt"
	"net/netip"
	"strings"
)

// Network is private deployment policy, never part of the player configuration.
type Network struct {
	TrustProxy        bool     `json:"trustProxy,omitempty"`
	TrustedProxyCIDRs []string `json:"trustedProxyCIDRs,omitempty"`
}

// ParseTrustedProxyCIDRs bounds parsing and refuses implicit trust of the internet.
// Empty lists are allowed here so environment/flags may supply file defaults;
// startup checks that the final enabled policy has at least one trusted prefix.
func ParseTrustedProxyCIDRs(values []string) ([]netip.Prefix, error) {
	if len(values) > 64 {
		return nil, fmt.Errorf("trusted proxy list must contain at most 64 CIDRs")
	}
	var prefixes []netip.Prefix
	seen := make(map[netip.Prefix]bool)
	for _, value := range values {
		prefix, err := netip.ParsePrefix(strings.TrimSpace(value))
		if err != nil || prefix.Addr().Is4In6() || prefix.Bits() == 0 {
			return nil, fmt.Errorf("trusted proxy entry %q must be an IPv4 or IPv6 CIDR narrower than /0; use plain IPv4 for mapped addresses", value)
		}
		prefix = prefix.Masked()
		if !seen[prefix] {
			prefixes = append(prefixes, prefix)
			seen[prefix] = true
		}
	}
	return prefixes, nil
}
