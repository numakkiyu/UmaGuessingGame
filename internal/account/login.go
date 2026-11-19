package account

import (
	"context"
	"crypto/subtle"
	"net/http"
	"net/url"
	"strings"
	"time"

	"golang.org/x/oauth2"
)

// SafeReturn only allows known same-origin SPA routes; no protocol-relative or encoded paths.
func SafeReturn(raw string) string {
	if len(raw) > 256 || strings.ContainsAny(raw, "\\%?#\r\n") {
		return "/"
	}
	switch raw {
	case "/", "/friends", "/party", "/matchmaking":
		return raw
	}
	return "/"
}
func (s *Service) Start(w http.ResponseWriter, r *http.Request, id, returnTo string) (string, error) {
	if !s.Enabled() {
		return "", ErrLogin
	}
	p := s.providers[id]
	if p == nil {
		return "", ErrLogin
	}
	browser, err := secret()
	if err != nil {
		return "", err
	}
	state, err := secret()
	if err != nil {
		return "", err
	}
	nonce, err := secret()
	if err != nil {
		return "", err
	}
	verifier := oauth2.GenerateVerifier()
	now := s.now()
	s.mu.Lock()
	defer s.mu.Unlock()
	for key, t := range s.transactions {
		if !t.expires.After(now) {
			delete(s.transactions, key)
		}
	}
	if len(s.transactions) >= 1024 {
		return "", ErrLogin
	}
	// One transaction per browser: starting a second login cancels the first.
	old := digest(cookie(r, transactionCookie))
	for key, t := range s.transactions {
		if t.browserHash == old {
			delete(s.transactions, key)
		}
	}
	s.transactions[digest(state)] = loginTransaction{provider: id, state: state, verifier: verifier, nonce: nonce, browserHash: digest(browser), returnTo: SafeReturn(returnTo), expires: now.Add(5 * time.Minute)}
	setCookie(w, transactionCookie, browser, now.Add(5*time.Minute), now)
	return p.oauth.AuthCodeURL(state, oauth2.S256ChallengeOption(verifier), oauth2.SetAuthURLParam("nonce", nonce)), nil
}

// Callback consumes a browser-bound transaction before contacting a provider.
func (s *Service) Callback(ctx context.Context, w http.ResponseWriter, r *http.Request, id string, beforeIssue ...func() error) (string, error) {
	if !s.Enabled() {
		return "/", ErrLogin
	}
	q := r.URL.Query()
	state := q.Get("state")
	if len(state) != 43 || len(q["state"]) != 1 {
		return "/", ErrLogin
	}
	s.mu.Lock()
	t, ok := s.transactions[digest(state)]
	if !ok || t.provider != id || !t.expires.After(s.now()) || subtle.ConstantTimeCompare([]byte(t.browserHash), []byte(digest(cookie(r, transactionCookie)))) != 1 {
		s.mu.Unlock()
		return "/", ErrLogin
	}
	delete(s.transactions, digest(state))
	s.mu.Unlock()
	setCookie(w, transactionCookie, "", time.Unix(1, 0), s.now())
	p := s.providers[id]
	if p == nil {
		return t.returnTo, ErrLogin
	}
	if q.Get("error") != "" || len(q["code"]) != 1 || q.Get("code") == "" || len(q.Get("code")) > 4096 || (q.Get("iss") != "" && q.Get("iss") != p.issuer) {
		return t.returnTo, ErrLogin
	}
	sub, err := p.subject(ctx, q.Get("code"), t.verifier, t.nonce, s.client)
	if err != nil {
		return t.returnTo, ErrLogin
	}
	userID, err := s.ResolveIdentity(ctx, p.issuer, p.config.ClientID, sub)
	if err != nil {
		return t.returnTo, err
	}
	for _, prepare := range beforeIssue {
		if err = prepare(); err != nil {
			return t.returnTo, err
		}
	}
	if err = s.issue(ctx, w, userID); err != nil {
		return t.returnTo, err
	}
	return t.returnTo, nil
}
func ResultURL(returnTo string, ok bool) string {
	result := "failed"
	if ok {
		result = "success"
	}
	return SafeReturn(returnTo) + "?" + url.Values{"signin": {result}}.Encode()
}
