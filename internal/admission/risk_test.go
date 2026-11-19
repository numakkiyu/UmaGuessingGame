package admission

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/bhcn/umaguessinggame/internal/deployment"
)

func grantedRequest(s *Service, id string) *http.Request {
	w := httptest.NewRecorder()
	s.Grant(w, id, s.now().Add(time.Hour))
	r := httptest.NewRequest("GET", "https://uma.example/api/v1/sessions/current/admission", nil)
	for _, c := range w.Result().Cookies() {
		r.AddCookie(c)
	}
	return r
}
func TestAdaptiveRevokesOnlyTheFloodingSession(t *testing.T) {
	s := enabled(t)
	now := time.Now()
	s.now = func() time.Time { return now }
	first, other := grantedRequest(s, "first"), grantedRequest(s, "other")
	for i := 0; i < 5; i++ {
		s.ObserveLimit("first")
	}
	if s.RiskRequired("first") || !s.Allowed(first, "first") {
		t.Fatal("isolated refusals caused a challenge")
	}
	s.ObserveLimit("first")
	if !s.RiskRequired("first") || s.Allowed(first, "first") {
		t.Fatal("old grant survived repeated limits")
	}
	if s.RiskRequired("other") || !s.Allowed(other, "other") {
		t.Fatal("neighbour affected")
	}
	fresh := grantedRequest(s, "first")
	if s.RiskRequired("first") || !s.Allowed(fresh, "first") || s.Allowed(first, "first") {
		t.Fatal("new grant did not restore only its own revision")
	}
	for i := 0; i < 6; i++ {
		s.ObserveLimit("first")
	}
	if s.Allowed(fresh, "first") {
		t.Fatal("second flood reused a healthy grant")
	}
	now = now.Add(time.Hour)
	if s.RiskRequired("first") {
		t.Fatal("risk did not expire")
	}
}
func TestAdaptiveWindowAndDisabledModes(t *testing.T) {
	s := enabled(t)
	now := time.Now()
	s.now = func() time.Time { return now }
	for i := 0; i < 5; i++ {
		s.ObserveLimit("a")
	}
	now = now.Add(61 * time.Second)
	s.ObserveLimit("a")
	if s.RiskRequired("a") {
		t.Fatal("old refusals leaked into a new window")
	}
	off := false
	for _, cfg := range []deployment.Turnstile{{}, {Enabled: true, SiteKey: "site", Hostnames: []string{"uma.example"}, Adaptive: &off}} {
		disabled, err := New(cfg, "secret", false)
		if err != nil {
			t.Fatal(err)
		}
		for i := 0; i < 10; i++ {
			disabled.ObserveLimit("a")
		}
		if disabled.RiskRequired("a") || len(disabled.risks) != 0 {
			t.Fatal("disabled gate kept risk state")
		}
	}
}
func TestRiskMemoryBoundAndConcurrency(t *testing.T) {
	s := enabled(t)
	var wg sync.WaitGroup
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for n := 0; n < 20; n++ {
				s.ObserveLimit("same")
				_ = s.Status(httptest.NewRequest("GET", "/", nil), "same")
			}
		}()
	}
	wg.Wait()
	if !s.RiskRequired("same") {
		t.Fatal("concurrent flood lost")
	}
	for i := 0; i < maxRiskEntries+20; i++ {
		s.ObserveLimit(fmt.Sprint(i))
	}
	if len(s.risks) > maxRiskEntries {
		t.Fatal("unbounded risk memory")
	}
}
func TestTokenReplayAndUncertainRetryRemainSessionBound(t *testing.T) {
	s := enabled(t)
	calls := 0
	var keys []string
	s.client = &http.Client{Transport: transportFunc(func(r *http.Request) (*http.Response, error) {
		calls++
		_ = r.ParseForm()
		keys = append(keys, r.Form.Get("idempotency_key"))
		if calls == 1 {
			return nil, fmt.Errorf("provider response lost")
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(fmt.Sprintf(`{"success":true,"hostname":"uma.example","action":"game_entry","challenge_ts":%q}`, s.now().UTC().Format(time.RFC3339)))), Header: make(http.Header)}, nil
	})}
	if s.VerifyForSession(context.Background(), "one-token", "a") == nil {
		t.Fatal("lost response treated as success")
	}
	if s.VerifyForSession(context.Background(), "one-token", "b") == nil || calls != 1 {
		t.Fatal("uncertain token crossed identity")
	}
	if err := s.VerifyForSession(context.Background(), "one-token", "a"); err != nil {
		t.Fatal(err)
	}
	if len(keys) != 2 || len(keys[0]) != 36 || keys[0] != keys[1] {
		t.Fatal("provider retry changed idempotency key")
	}
	if s.VerifyForSession(context.Background(), "one-token", "a") == nil || s.VerifyForSession(context.Background(), "one-token", "b") == nil || calls != 2 {
		t.Fatal("consumed token replayed")
	}
}

func TestRequiredRiskRejectsEvenCurrentRevisionSignature(t *testing.T) {
	s := enabled(t)
	for i := 0; i < 6; i++ {
		s.ObserveLimit("flood")
	}
	stamp := fmt.Sprint(s.now().Add(time.Minute).Unix())
	r := httptest.NewRequest("GET", "/api/v1/sessions/current/admission", nil)
	r.AddCookie(&http.Cookie{Name: cookieName, Value: stamp + "." + s.sign("flood|"+stamp+"|1")})
	if s.Allowed(r, "flood") {
		t.Fatal("active risk accepted a signed grant before being cleared")
	}
}
