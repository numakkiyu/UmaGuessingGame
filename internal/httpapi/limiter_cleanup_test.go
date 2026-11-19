package httpapi

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

func TestLimiterVenueBurstAndIdleCleanup(t *testing.T) {
	l := NewLimiter(LimiterConfig{})
	now := time.Now()
	l.now = func() time.Time { return now }
	rate := Rate{PerMinute: 300, BurstTokens: 200}
	for i := 0; i < 200; i++ {
		if ok, _ := l.Allow("shared-venue", rate); !ok {
			t.Fatalf("shared NAT refused attendee %d", i+1)
		}
	}
	if ok, _ := l.Allow("shared-venue", rate); ok {
		t.Fatal("burst limit not enforced")
	}
	now = now.Add(6 * time.Minute)
	l.Allow("next", Rate{PerMinute: 60})
	if _, ok := l.buckets["shared-venue"]; ok {
		t.Fatal("idle depleted bucket never expired after refill")
	}
}

// Distinct hostile keys cannot consume unbounded memory or reset live limits.
func TestLimiterCardinalityDoesNotEvictExistingClients(t *testing.T) {
	l := NewLimiter(LimiterConfig{MaxBuckets: 3})
	now := time.Now()
	l.now = func() time.Time { return now }
	rate := Rate{PerMinute: 60, BurstTokens: 2}
	for _, key := range []string{"active", "exhausted", "third"} {
		if ok, _ := l.Allow(key, rate); !ok {
			t.Fatal("initial client refused")
		}
	}
	l.Allow("exhausted", rate)
	for i := 0; i < 1000; i++ {
		if ok, retry := l.Allow(fmt.Sprintf("hostile-%d", i), rate); ok || retry < 1 {
			t.Fatal("unbounded key accepted")
		}
	}
	if len(l.buckets) != 3 {
		t.Fatalf("buckets = %d", len(l.buckets))
	}
	if ok, _ := l.Allow("active", rate); !ok {
		t.Fatal("existing allowance lost at capacity")
	}
	if ok, _ := l.Allow("exhausted", rate); ok {
		t.Fatal("flood reset exhausted bucket")
	}
	now = now.Add(6 * time.Minute)
	if ok, _ := l.Allow("new-client", rate); !ok {
		t.Fatal("idle capacity was not reclaimed")
	}
	if len(l.buckets) != 1 {
		t.Fatalf("idle buckets remain: %d", len(l.buckets))
	}
	if ok, _ := l.Allow(strings.Repeat("x", 257), rate); ok {
		t.Fatal("oversized key accepted")
	}
}
