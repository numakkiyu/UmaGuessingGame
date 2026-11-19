package httpapi

import (
	"net"
	"net/http"
	"net/netip"
	"strings"
)

const (
	maxForwardedBytes = 2048
	maxForwardedHops  = 32
)

func parseClientAddress(raw string) netip.Addr {
	addr, err := netip.ParseAddr(strings.TrimSpace(raw))
	if err != nil || addr.Zone() != "" {
		return netip.Addr{}
	}
	return addr.Unmap()
}

// RemoteAddr is assigned by net/http, never by a forwarded header.
//
// A real TCP link-local IPv6 peer may carry a zone (fe80::1%en0). The zone
// names a local interface, not the client, so it is stripped here; forwarded
// header values keep rejecting zones because there the text is attacker input.
func peerAddress(r *http.Request) netip.Addr {
	raw := r.RemoteAddr
	if host, _, err := net.SplitHostPort(raw); err == nil {
		raw = host
	}
	raw = strings.TrimSpace(raw)
	if i := strings.LastIndexByte(raw, '%'); i >= 0 {
		raw = raw[:i]
	}
	return parseClientAddress(raw)
}

func (s *Server) trustedProxy(addr netip.Addr) bool {
	if !s.cfg.TrustProxy || !addr.IsValid() {
		return false
	}
	for _, prefix := range s.cfg.TrustedProxies {
		if prefix.Contains(addr) {
			return true
		}
	}
	return false
}

// clientAddress walks from the transport peer towards the client. Untrusted
// hops form the boundary; values further left cannot influence address gates.
// Malformed or excessive relevant chains fail closed to the transport peer.
func (s *Server) clientAddress(r *http.Request) netip.Addr {
	peer := peerAddress(r)
	if !s.trustedProxy(peer) {
		return peer
	}
	values := r.Header.Values("X-Forwarded-For")
	if len(values) == 0 {
		// Compatibility only: a trusted proxy must replace this header itself.
		real := r.Header.Values("X-Real-IP")
		if len(real) == 1 && len(real[0]) <= maxForwardedBytes {
			if addr := parseClientAddress(real[0]); addr.IsValid() {
				return addr
			}
		}
		return peer
	}
	chain, ok := forwardedChain(values)
	if !ok {
		return peer
	}
	current := peer
	for i := len(chain) - 1; i >= 0; i-- {
		if !s.trustedProxy(current) {
			return current
		}
		current = parseClientAddress(chain[i])
		if !current.IsValid() {
			return peer
		}
	}
	return current
}

// forwardedChain bounds and splits X-Forwarded-For values. The byte limit is
// inclusive over the exact joined length (2048 accepted, 2049 rejected) and the
// hop limit counts comma-separated entries, so a 33rd hop fails closed.
func forwardedChain(values []string) ([]string, bool) {
	length, hops := 0, 0
	for i, value := range values {
		if i > 0 {
			length++ // separating comma, matching the join below
		}
		length += len(value)
		hops += strings.Count(value, ",") + 1
		if length > maxForwardedBytes || hops > maxForwardedHops {
			return nil, false
		}
	}
	return strings.Split(strings.Join(values, ","), ","), true
}

func (s *Server) clientIP(r *http.Request) string {
	if addr := s.clientAddress(r); addr.IsValid() {
		return addr.String()
	}
	// Invalid transport addresses do not produce attacker-chosen limiter keys.
	return "unknown"
}
