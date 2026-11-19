package httpapi

import (
	"net/http/httptest"
	"net/netip"
	"strings"
	"testing"
)

func testPrefixes(t *testing.T, values ...string) []netip.Prefix {
	t.Helper()
	out := make([]netip.Prefix, 0, len(values))
	for _, value := range values {
		prefix, err := netip.ParsePrefix(value)
		if err != nil {
			t.Fatalf("parse %q: %v", value, err)
		}
		out = append(out, prefix)
	}
	return out
}

func TestClientAddressTrustWalk(t *testing.T) {
	for _, test := range []struct {
		name    string
		trust   bool
		cidrs   []string
		peer    string
		headers map[string][]string
		want    string
	}{
		{
			name: "trusted peer resolves rightmost untrusted hop", trust: true,
			cidrs: []string{"10.0.0.0/8"}, peer: "10.1.2.3:5555",
			headers: map[string][]string{"X-Forwarded-For": {"198.51.100.7, 10.0.0.9"}},
			want:    "198.51.100.7",
		},
		{
			name: "malicious leftmost value cannot move the boundary", trust: true,
			cidrs: []string{"10.0.0.0/8"}, peer: "10.1.2.3:5555",
			headers: map[string][]string{"X-Forwarded-For": {"1.2.3.4, 8.8.8.8"}},
			want:    "8.8.8.8",
		},
		{
			name: "untrusted direct peer ignores forwarded headers", trust: true,
			cidrs: []string{"10.0.0.0/8"}, peer: "203.0.113.20:5555",
			headers: map[string][]string{"X-Forwarded-For": {"198.51.100.1"}},
			want:    "203.0.113.20",
		},
		{
			name: "switch off ignores a listing that is present", trust: false,
			cidrs: []string{"10.0.0.0/8"}, peer: "10.1.2.3:5555",
			headers: map[string][]string{"X-Forwarded-For": {"198.51.100.7"}},
			want:    "10.1.2.3",
		},
		{
			name: "forwarded zone is refused", trust: true,
			cidrs: []string{"10.0.0.0/8"}, peer: "10.1.2.3:5555",
			headers: map[string][]string{"X-Forwarded-For": {"fe80::1%eth0"}},
			want:    "10.1.2.3",
		},
		{
			name: "transport zone is normalised so a LAN peer is not the unknown bucket", trust: true,
			cidrs: []string{"fe80::/10"}, peer: "[fe80::1%en0]:5555",
			headers: map[string][]string{"X-Forwarded-For": {"203.0.113.9"}},
			want:    "203.0.113.9",
		},
		{
			name: "multi-line forwarding joins in order", trust: true,
			cidrs: []string{"10.0.0.0/8"}, peer: "10.1.2.3:5555",
			headers: map[string][]string{"X-Forwarded-For": {"203.0.113.5, 10.0.0.4", "10.0.0.3"}},
			want:    "203.0.113.5",
		},
		{
			name: "IPv4-mapped client is unmapped", trust: true,
			cidrs: []string{"10.0.0.0/8"}, peer: "10.1.2.3:5555",
			headers: map[string][]string{"X-Forwarded-For": {"::ffff:203.0.113.9"}},
			want:    "203.0.113.9",
		},
		{
			name: "empty item fails closed", trust: true,
			cidrs: []string{"10.0.0.0/8"}, peer: "10.1.2.3:5555",
			headers: map[string][]string{"X-Forwarded-For": {"203.0.113.5,,"}},
			want:    "10.1.2.3",
		},
		{
			name: "port in forwarded value fails closed", trust: true,
			cidrs: []string{"10.0.0.0/8"}, peer: "10.1.2.3:5555",
			headers: map[string][]string{"X-Forwarded-For": {"203.0.113.5:443"}},
			want:    "10.1.2.3",
		},
		{
			name: "domain in forwarded value fails closed", trust: true,
			cidrs: []string{"10.0.0.0/8"}, peer: "10.1.2.3:5555",
			headers: map[string][]string{"X-Forwarded-For": {"client.example"}},
			want:    "10.1.2.3",
		},
		{
			name: "single valid X-Real-IP without X-Forwarded-For", trust: true,
			cidrs: []string{"10.0.0.0/8"}, peer: "10.1.2.3:5555",
			headers: map[string][]string{"X-Real-IP": {"203.0.113.44"}},
			want:    "203.0.113.44",
		},
		{
			name: "duplicate X-Real-IP is ignored", trust: true,
			cidrs: []string{"10.0.0.0/8"}, peer: "10.1.2.3:5555",
			headers: map[string][]string{"X-Real-IP": {"203.0.113.44", "203.0.113.45"}},
			want:    "10.1.2.3",
		},
		{
			name: "X-Forwarded-For wins over X-Real-IP", trust: true,
			cidrs: []string{"10.0.0.0/8"}, peer: "10.1.2.3:5555",
			headers: map[string][]string{"X-Forwarded-For": {"203.0.113.5"}, "X-Real-IP": {"203.0.113.44"}},
			want:    "203.0.113.5",
		},
		{
			name: "32 hops are the inclusive ceiling", trust: true,
			cidrs: []string{"10.0.0.0/8"}, peer: "10.1.2.3:5555",
			headers: map[string][]string{"X-Forwarded-For": {strings.Repeat("10.0.0.1, ", 31) + "10.0.0.1"}},
			want:    "10.0.0.1",
		},
		{
			name: "33 hops fail closed", trust: true,
			cidrs: []string{"10.0.0.0/8"}, peer: "10.1.2.3:5555",
			headers: map[string][]string{"X-Forwarded-For": {strings.Repeat("10.0.0.1, ", 32) + "10.0.0.1"}},
			want:    "10.1.2.3",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			s := New(Deps{Config: Config{TrustProxy: test.trust, TrustedProxies: testPrefixes(t, test.cidrs...)}})
			r := httptest.NewRequest("GET", "/", nil)
			r.RemoteAddr = test.peer
			for key, values := range test.headers {
				for _, value := range values {
					r.Header.Add(key, value)
				}
			}
			if got := s.clientIP(r); got != test.want {
				t.Fatalf("got %q want %q", got, test.want)
			}
		})
	}
}

func TestForwardedChainLimitsAreInclusive(t *testing.T) {
	// Exactly 2048 bytes of joined header value is accepted; 2049 is not.
	if _, ok := forwardedChain([]string{strings.Repeat("a", 2048)}); !ok {
		t.Fatal("2048-byte chain was rejected")
	}
	if _, ok := forwardedChain([]string{strings.Repeat("a", 2049)}); ok {
		t.Fatal("2049-byte chain was accepted")
	}
	// Two lines: the separating comma counts once towards the joined length.
	if chain, ok := forwardedChain([]string{strings.Repeat("a", 1023), strings.Repeat("b", 1024)}); !ok || len(chain) != 2 {
		t.Fatalf("2048 bytes across two lines: ok=%v chain=%d", ok, len(chain))
	}
	if _, ok := forwardedChain([]string{strings.Repeat("a", 1024), strings.Repeat("b", 1024)}); ok {
		t.Fatal("2049 bytes across two lines were accepted")
	}
	// Hop ceiling: 32 entries pass, 33 fail.
	hops := make([]string, 32)
	for i := range hops {
		hops[i] = "10.0.0.1"
	}
	if chain, ok := forwardedChain(hops); !ok || len(chain) != 32 {
		t.Fatalf("32 hops: ok=%v len=%d", ok, len(chain))
	}
	if _, ok := forwardedChain(append(hops, "10.0.0.1")); ok {
		t.Fatal("33 hops were accepted")
	}
	// Multi-line order is preserved as if it were one comma list.
	if chain, _ := forwardedChain([]string{"1.1.1.1, 2.2.2.2", "3.3.3.3"}); strings.Join(chain, "|") != "1.1.1.1| 2.2.2.2|3.3.3.3" {
		t.Fatalf("unexpected join order: %v", chain)
	}
}

func TestMetricsRequireLoopbackPeerAndClient(t *testing.T) {
	s := New(Deps{Config: Config{
		TrustProxy:       true,
		TrustedProxies:   testPrefixes(t, "127.0.0.0/8", "::1/128"),
		MetricsLocalOnly: true,
	}})
	remote := httptest.NewRequest("GET", "/metrics", nil)
	remote.RemoteAddr = "127.0.0.1:12345"
	remote.Header.Set("X-Forwarded-For", "203.0.113.9")
	if s.metricsAllowed(remote) {
		t.Fatal("trusted loopback proxy forwarded a remote client and metrics were allowed")
	}
	local := httptest.NewRequest("GET", "/metrics", nil)
	local.RemoteAddr = "127.0.0.1:12345"
	local.Header.Set("X-Forwarded-For", "127.0.0.2")
	if !s.metricsAllowed(local) {
		t.Fatal("loopback peer with a loopback client should read metrics")
	}
	direct := httptest.NewRequest("GET", "/metrics", nil)
	direct.RemoteAddr = "[::1]:12345"
	if !s.metricsAllowed(direct) {
		t.Fatal("direct IPv6 loopback should read metrics")
	}
}
