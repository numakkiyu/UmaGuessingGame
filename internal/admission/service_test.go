package admission

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/bhcn/umaguessinggame/internal/deployment"
)

type transportFunc func(*http.Request) (*http.Response, error)

func (f transportFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func enabled(t *testing.T) *Service {
	t.Helper()
	s, err := New(deployment.Turnstile{Enabled: true, SiteKey: "test-site", Hostnames: []string{"uma.example"}}, "test-secret", true)
	if err != nil {
		t.Fatal(err)
	}
	return s
}
func TestVerifyValidatesAllBoundaries(t *testing.T) {
	for _, kind := range []string{"valid", "failure", "hostname", "action", "expired", "future", "missing-time", "bad-json", "huge", "status"} {
		t.Run(kind, func(t *testing.T) {
			s := enabled(t)
			now := time.Now().UTC()
			s.now = func() time.Time { return now }
			calls := 0
			s.client = &http.Client{Transport: transportFunc(func(r *http.Request) (*http.Response, error) {
				calls++
				if r.URL.String() != siteverifyURL {
					t.Fatal("non-fixed endpoint")
				}
				if err := r.ParseForm(); err != nil {
					t.Fatal(err)
				}
				if r.Form.Get("secret") != "test-secret" || r.Form.Get("response") != "challenge" {
					t.Fatal("incorrect siteverify request")
				}
				result := map[string]any{"success": true, "hostname": "uma.example", "action": action, "challenge_ts": now}
				switch kind {
				case "failure":
					result["success"] = false
				case "hostname":
					result["hostname"] = "other.example"
				case "action":
					result["action"] = "login"
				case "expired":
					result["challenge_ts"] = now.Add(-301 * time.Second)
				case "future":
					result["challenge_ts"] = now.Add(time.Minute)
				case "missing-time":
					delete(result, "challenge_ts")
				}
				b, _ := json.Marshal(result)
				raw := string(b)
				status := 200
				if kind == "bad-json" {
					raw = "not json"
				}
				if kind == "huge" {
					raw = strings.Repeat("a", 16385)
				}
				if kind == "status" {
					status = 503
				}
				return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(raw)), Header: make(http.Header)}, nil
			})}
			err := s.Verify(context.Background(), "challenge")
			if (err == nil) != (kind == "valid") {
				t.Fatalf("validation err=%v", err)
			}
			if calls != 1 {
				t.Fatalf("requests=%d", calls)
			}
		})
	}
}
func TestGrantSessionBindingTamperExpiryAndRestart(t *testing.T) {
	s := enabled(t)
	now := time.Now().UTC()
	s.now = func() time.Time { return now }
	w := httptest.NewRecorder()
	s.Grant(w, "ses_A", now.Add(time.Hour))
	cookie := w.Result().Cookies()[0]
	if !cookie.HttpOnly || !cookie.Secure || cookie.SameSite != http.SameSiteLaxMode {
		t.Fatal("weak cookie policy")
	}
	req := httptest.NewRequest("POST", "https://uma.example/api/v1/rooms", nil)
	req.AddCookie(cookie)
	if !s.Allowed(req, "ses_A") || s.Allowed(req, "ses_B") {
		t.Fatal("incorrect binding")
	}
	restart := enabled(t)
	if restart.Allowed(req, "ses_A") {
		t.Fatal("restart accepted old process grant")
	}
	s.now = func() time.Time { return now.Add(901 * time.Second) }
	if s.Allowed(req, "ses_A") {
		t.Fatal("accepted expired grant")
	}
	s.now = func() time.Time { return now }
	cookie.Value += "x"
	bad := httptest.NewRequest("POST", "https://uma.example/api/v1/rooms", nil)
	bad.AddCookie(cookie)
	if s.Allowed(bad, "ses_A") {
		t.Fatal("accepted tampering")
	}
	w = httptest.NewRecorder()
	s.Grant(w, "ses_A", now.Add(time.Second))
	if w.Result().Cookies()[0].Expires.After(now.Add(2 * time.Second)) {
		t.Fatal("outlived session")
	}
}
func TestDisabledDoesNotCallExternalService(t *testing.T) {
	s, err := New(deployment.Turnstile{}, "", false)
	if err != nil {
		t.Fatal(err)
	}
	s.client = &http.Client{Transport: transportFunc(func(*http.Request) (*http.Response, error) { t.Fatal("disabled network call"); return nil, nil })}
	if err := s.Verify(context.Background(), ""); err != nil {
		t.Fatal(err)
	}
	if s.Required("createRoom") || s.Public().Enabled {
		t.Fatal("disabled gate advertised")
	}
}
func TestFloodBoundAndTokenLength(t *testing.T) {
	s := enabled(t)
	s.client = &http.Client{Transport: transportFunc(func(r *http.Request) (*http.Response, error) { <-r.Context().Done(); return nil, r.Context().Err() })}
	for i := 0; i < cap(s.slots); i++ {
		s.slots <- struct{}{}
	}
	before := time.Now()
	if err := s.Verify(context.Background(), "x"); err == nil {
		t.Fatal("admitted saturation")
	}
	if time.Since(before) > time.Second {
		t.Fatal("unbounded wait")
	}
	for _, token := range []string{"", strings.Repeat("x", 2049)} {
		if s.Verify(context.Background(), token) == nil {
			t.Fatal("accepted invalid length")
		}
	}
}
func TestSignalIsOptionalAndScoped(t *testing.T) {
	s := enabled(t)
	if key, _, _ := s.SignalBucket(strings.Repeat("a", 64)); key != "" {
		t.Fatal("collected when disabled")
	}
	s.cfg.BrowserSignals = true
	a, _, _ := s.SignalBucket(strings.Repeat("a", 64))
	other := enabled(t)
	other.cfg.BrowserSignals = true
	b, _, _ := other.SignalBucket(strings.Repeat("a", 64))
	if a == "" || a == strings.Repeat("a", 64) || a == b {
		t.Fatal("unscoped raw signal")
	}
	for _, invalid := range []string{"", strings.Repeat("A", 64), strings.Repeat("f", 65)} {
		if key, _, _ := s.SignalBucket(invalid); key != "" {
			t.Fatal("accepted malformed signal")
		}
	}
}
