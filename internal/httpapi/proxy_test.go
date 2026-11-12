package httpapi

import (
	"net/http/httptest"
	"testing"
)

func TestUntrustedForwardingCannotAuthorizeMetrics(t *testing.T) {
	s := New(Deps{Config: Config{TrustProxy: true, MetricsLocalOnly: true}})
	r := httptest.NewRequest("GET", "/metrics", nil)
	r.RemoteAddr = "203.0.113.20:12345"
	r.Header.Set("X-Forwarded-For", "127.0.0.1")
	if s.metricsAllowed(r) {
		t.Fatal("untrusted direct caller authorized metrics by forging a forwarded address")
	}
}

func TestUntrustedForwardingCannotSplitAddressBuckets(t *testing.T) {
	s := New(Deps{Config: Config{TrustProxy: true, Limiter: LimiterConfig{PublicPerIP: Rate{PerMinute: 1, BurstTokens: 1}}}})
	r := httptest.NewRequest("GET", "/", nil)
	r.RemoteAddr = "203.0.113.20:12345"
	r.Header.Set("X-Forwarded-For", "198.51.100.1")
	if err := s.limit(httptest.NewRecorder(), r, routeInfo{RateClass: "public"}); err != nil {
		t.Fatal(err)
	}
	r.Header.Set("X-Forwarded-For", "198.51.100.2")
	if err := s.limit(httptest.NewRecorder(), r, routeInfo{RateClass: "public"}); err == nil {
		t.Fatal("one direct caller bypassed its address limit by changing a forwarded header")
	}
}
