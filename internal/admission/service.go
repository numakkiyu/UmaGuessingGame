// Package admission verifies entry once and checks a short-lived, session-bound
// permission locally. It never changes game rules or treats a challenge as login.
package admission

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/bhcn/umaguessinggame/internal/deployment"
	"github.com/bhcn/umaguessinggame/internal/httpapi/problem"
)

const cookieName = "uma_entry"
const action = "game_entry"
const siteverifyURL = "https://challenges.cloudflare.com/turnstile/v0/siteverify"

type PublicConfig struct {
	BrowserSignalsEnabled bool   `json:"browserSignalsEnabled,omitempty"`
	Enabled               bool   `json:"enabled"`
	SiteKey               string `json:"siteKey,omitempty"`
}
type Service struct {
	cfg        deployment.Turnstile
	secret     string
	secure     bool
	signingKey [32]byte
	client     *http.Client
	slots      chan struct{}
	now        func() time.Time
}

func New(cfg deployment.Turnstile, secret string, secure bool) (*Service, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	cfg = cfg.Resolved()
	if cfg.Enabled && strings.TrimSpace(secret) == "" {
		return nil, fmt.Errorf("turnstile secret environment variable is empty")
	}
	s := &Service{cfg: cfg, secret: secret, secure: secure, client: &http.Client{Timeout: 4 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}, slots: make(chan struct{}, cfg.MaxConcurrent), now: time.Now}
	if _, err := rand.Read(s.signingKey[:]); err != nil {
		return nil, err
	}
	return s, nil
}
func (s *Service) Public() PublicConfig {
	if s == nil || !s.cfg.Enabled {
		return PublicConfig{}
	}
	return PublicConfig{Enabled: true, SiteKey: s.cfg.SiteKey, BrowserSignalsEnabled: s.cfg.BrowserSignals}
}
func (s *Service) Required(op string) bool {
	if s == nil || !s.cfg.Enabled {
		return false
	}
	for _, v := range s.cfg.Operations {
		if v == op {
			return true
		}
	}
	return false
}
func (s *Service) sign(payload string) string {
	mac := hmac.New(sha256.New, s.signingKey[:])
	mac.Write([]byte(payload))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}
func (s *Service) Allowed(r *http.Request, sessionID string) bool {
	if s == nil || !s.cfg.Enabled {
		return true
	}
	c, err := r.Cookie(cookieName)
	if err != nil || len(c.Value) > 256 {
		return false
	}
	p := strings.Split(c.Value, ".")
	if len(p) != 2 {
		return false
	}
	exp, err := strconv.ParseInt(p[0], 10, 64)
	now := s.now().Unix()
	if err != nil || exp <= now || exp > now+int64(s.cfg.GrantSeconds) {
		return false
	}
	return hmac.Equal([]byte(p[1]), []byte(s.sign(sessionID+"|"+p[0])))
}
func (s *Service) Grant(w http.ResponseWriter, sessionID string, sessionExpiry time.Time) time.Time {
	expires := s.now().Add(time.Duration(s.cfg.GrantSeconds) * time.Second)
	if sessionExpiry.Before(expires) {
		expires = sessionExpiry
	}
	stamp := strconv.FormatInt(expires.Unix(), 10)
	http.SetCookie(w, &http.Cookie{Name: cookieName, Value: stamp + "." + s.sign(sessionID+"|"+stamp), Path: "/api/v1", HttpOnly: true, Secure: s.secure, SameSite: http.SameSiteLaxMode, Expires: expires, MaxAge: max(1, int(expires.Sub(s.now()).Seconds()))})
	return expires
}
func (s *Service) Verify(ctx context.Context, token string) error {
	if s == nil || !s.cfg.Enabled {
		return nil
	}
	if len(token) == 0 || len(token) > 2048 {
		return problem.Forbidden("admission-invalid", "请重新完成入场确认")
	}
	select {
	case s.slots <- struct{}{}:
		defer func() { <-s.slots }()
	default:
		return problem.RateLimited(2)
	}
	ctx, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()
	body := url.Values{"secret": {s.secret}, "response": {token}}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, siteverifyURL, strings.NewReader(body.Encode()))
	if err != nil {
		return problem.Internal(err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := s.client.Do(req)
	if err != nil {
		return unavailable("admission-unavailable", "入场确认暂时没有回应，请稍后再试")
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 16385))
	if err != nil || len(raw) > 16384 || resp.StatusCode != 200 {
		return unavailable("admission-unavailable", "入场确认暂时没有回应，请稍后再试")
	}
	var result struct {
		Success     bool      `json:"success"`
		Hostname    string    `json:"hostname"`
		Action      string    `json:"action"`
		ChallengeTS time.Time `json:"challenge_ts"`
	}
	if json.Unmarshal(raw, &result) != nil {
		return unavailable("admission-unavailable", "入场确认暂时没有回应，请稍后再试")
	}
	allowedHost := false
	for _, host := range s.cfg.Hostnames {
		if result.Hostname == host {
			allowedHost = true
			break
		}
	}
	age := s.now().Sub(result.ChallengeTS)
	if !result.Success || !allowedHost || result.Action != action || result.ChallengeTS.IsZero() || age > 5*time.Minute || age < -30*time.Second {
		return problem.Forbidden("admission-invalid", "请重新完成入场确认")
	}
	return nil
}

func unavailable(code, detail string) *problem.Error {
	e := problem.New(http.StatusServiceUnavailable, code, detail)
	e.Retryable = true
	e.RetryAfter = 2
	return e
}

// SignalBucket never accepts a signal as proof of identity. An absent/spoofed
// value still faces every session/IP gate and the actual challenge verification.
func (s *Service) SignalBucket(signal string) (string, int, int) {
	if s == nil || !s.cfg.Enabled || !s.cfg.BrowserSignals || len(signal) != 64 {
		return "", 0, 0
	}
	for _, c := range signal {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')) {
			return "", 0, 0
		}
	}
	return "entry-browser:" + s.sign("signal|"+signal), s.cfg.SignalPerMinute, s.cfg.SignalBurst
}
