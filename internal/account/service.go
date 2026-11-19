// Package account authenticates external identities without replacing game session IDs.
package account

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/bhcn/umaguessinggame/internal/deployment"
	"github.com/bhcn/umaguessinggame/internal/httpapi/problem"
	"github.com/bhcn/umaguessinggame/internal/storage"
	"github.com/google/uuid"
)

const cookieName = "__Host-uma_account"
const transactionCookie = "__Host-uma_login"

var ErrRequired = problem.Unauthorized("account-required", "此玩法需要登录")
var ErrLogin = errors.New("account: external sign-in failed")

type PublicProvider struct {
	ID           string            `json:"id"`
	Available    bool              `json:"available"`
	DisplayNames map[string]string `json:"displayNames,omitempty"`
	IconPath     string            `json:"iconPath,omitempty"`
}
type PublicConfig struct {
	Enabled      bool             `json:"enabled"`
	RequireLogin map[string]bool  `json:"requireLogin"`
	Providers    []PublicProvider `json:"providers"`
}
type Status struct {
	Authenticated bool   `json:"authenticated"`
	UserID        string `json:"userId,omitempty"`
	ExpiresAt     string `json:"expiresAt,omitempty"`
	CSRFToken     string `json:"csrfToken,omitempty"`
	ID            string `json:"-"`
	absolute      time.Time
	renewed       time.Time
}
type loginTransaction struct {
	provider, state, verifier, nonce, browserHash, returnTo string
	expires                                                 time.Time
}
type Service struct {
	db           *storage.DB
	cfg          deployment.AccountAccess
	providers    map[string]*provider
	client       *http.Client
	now          func() time.Time
	csrfKey      []byte
	mu           sync.Mutex
	transactions map[string]loginTransaction
}

// New validates configured providers once. Provider tokens are never persisted.
func New(ctx context.Context, db *storage.DB, cfg deployment.AccountAccess) (*Service, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return newService(ctx, db, cfg, externalClient())
}
func newService(ctx context.Context, db *storage.DB, cfg deployment.AccountAccess, client *http.Client) (*Service, error) {
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		return nil, err
	}
	s := &Service{db: db, cfg: cfg.Resolved(), providers: map[string]*provider{}, client: client, now: time.Now, csrfKey: key, transactions: map[string]loginTransaction{}}
	if !cfg.Enabled {
		return s, nil
	}
	for _, config := range cfg.Providers {
		if !config.Enabled {
			continue
		}
		p, err := loadProvider(ctx, config, strings.TrimRight(cfg.PublicURL, "/")+"/api/v1/account/callback/"+config.ID, client)
		if err != nil {
			return nil, fmt.Errorf("accounts: provider %s unavailable (check configuration and connectivity)", config.ID)
		}
		s.providers[config.ID] = p
	}
	return s, nil
}
func (s *Service) Public() PublicConfig {
	out := PublicConfig{RequireLogin: map[string]bool{}, Providers: []PublicProvider{}}
	if s == nil || !s.cfg.Enabled {
		return out
	}
	out.Enabled = true
	for _, f := range []string{"friend", "event", "matchmaking"} {
		out.RequireLogin[f] = s.cfg.Required(f)
	}
	for _, configured := range s.cfg.Providers {
		if !configured.Enabled || s.providers[configured.ID] == nil {
			continue
		}
		out.Providers = append(out.Providers, PublicProvider{ID: configured.ID, Available: true, DisplayNames: configured.DisplayNames, IconPath: configured.IconPath})
	}
	return out
}
func (s *Service) Required(form string) bool { return s != nil && s.cfg.Required(form) }
func (s *Service) Enabled() bool             { return s != nil && s.cfg.Enabled }
func secret() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}
func digest(v string) string { h := sha256.Sum256([]byte(v)); return hex.EncodeToString(h[:]) }
func cookie(r *http.Request, name string) string {
	c, err := r.Cookie(name)
	if err != nil || len(c.Value) > 128 {
		return ""
	}
	return c.Value
}
func setCookie(w http.ResponseWriter, name, value string, until, timeNow time.Time) {
	maxAge := int(until.Sub(timeNow).Seconds())
	if value == "" {
		maxAge = -1
	}
	http.SetCookie(w, &http.Cookie{Name: name, Value: value, Path: "/", Expires: until, MaxAge: maxAge, Secure: true, HttpOnly: true, SameSite: http.SameSiteLaxMode})
}
func (s *Service) csrf(id string) string {
	mac := hmac.New(sha256.New, s.csrfKey)
	mac.Write([]byte(id))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}
func (s *Service) VerifyCSRF(r *http.Request, st Status) bool {
	return st.Authenticated && subtle.ConstantTimeCompare([]byte(r.Header.Get("X-Account-CSRF")), []byte(s.csrf(st.ID))) == 1
}

// Current checks both lifetimes and revocation. Expired and unknown cookies look identical.
func (s *Service) Current(ctx context.Context, r *http.Request) (Status, error) {
	if !s.Enabled() {
		return Status{}, nil
	}
	token := cookie(r, cookieName)
	if token == "" {
		return Status{}, nil
	}
	var st Status
	var idle, absolute, renewed int64
	now := s.now().UnixMilli()
	err := s.db.Read().QueryRowContext(ctx, `SELECT id,account_id,idle_expires_at,absolute_expires_at,renewed_at FROM account_sessions
 WHERE (token_hash=? OR (previous_hash=? AND previous_until>?)) AND revoked_at IS NULL AND idle_expires_at>? AND absolute_expires_at>?`, digest(token), digest(token), now, now, now).Scan(&st.ID, &st.UserID, &idle, &absolute, &renewed)
	if errors.Is(err, sql.ErrNoRows) {
		return Status{}, nil
	}
	if err != nil {
		return Status{}, err
	}
	st.Authenticated = true
	st.ExpiresAt = time.UnixMilli(min(idle, absolute)).UTC().Format(time.RFC3339)
	st.absolute = time.UnixMilli(absolute)
	st.renewed = time.UnixMilli(renewed)
	st.CSRFToken = s.csrf(st.ID)
	return st, nil
}

// Renew rotates the browser secret, with a short grace period for requests already in flight.
func (s *Service) Renew(ctx context.Context, w http.ResponseWriter, r *http.Request, st Status) error {
	if !st.Authenticated {
		return ErrRequired
	}
	now := s.now()
	until := minTime(now.Add(time.Duration(s.cfg.IdleSeconds)*time.Second), st.absolute)
	token := cookie(r, cookieName)
	// The compare-and-swap prevents concurrent tabs from overwriting a newer rotation.
	if now.Sub(st.renewed) >= 30*time.Minute {
		next, err := secret()
		if err != nil {
			return err
		}
		res, err := s.db.Write().ExecContext(ctx, `UPDATE account_sessions SET previous_hash=token_hash,previous_until=?,token_hash=?,renewed_at=?,idle_expires_at=? WHERE id=? AND token_hash=? AND revoked_at IS NULL AND idle_expires_at>? AND absolute_expires_at>?`, now.Add(30*time.Second).UnixMilli(), digest(next), now.UnixMilli(), until.UnixMilli(), st.ID, digest(token), now.UnixMilli(), now.UnixMilli())
		if err != nil {
			return err
		}
		n, _ := res.RowsAffected()
		if n == 0 {
			return nil
		}
		token = next
	} else {
		result, err := s.db.Write().ExecContext(ctx, `UPDATE account_sessions SET idle_expires_at=? WHERE id=? AND token_hash=? AND revoked_at IS NULL AND idle_expires_at>? AND absolute_expires_at>?`, until.UnixMilli(), st.ID, digest(token), now.UnixMilli(), now.UnixMilli())
		if err != nil {
			return err
		}
		if n, _ := result.RowsAffected(); n == 0 {
			return nil
		}
	}
	setCookie(w, cookieName, token, until, now)
	return nil
}
func minTime(a, b time.Time) time.Time {
	if a.Before(b) {
		return a
	}
	return b
}

// ResolveIdentity is atomic across concurrent callbacks and never merges by nickname or email.
func (s *Service) ResolveIdentity(ctx context.Context, issuer, clientID, subject string) (string, error) {
	if issuer == "" || clientID == "" || subject == "" || len(subject) > 1024 {
		return "", ErrLogin
	}
	tx, err := s.db.Write().BeginTx(ctx, nil)
	if err != nil {
		return "", err
	}
	defer tx.Rollback()
	var id string
	err = tx.QueryRowContext(ctx, `SELECT account_id FROM account_identities WHERE issuer=? AND client_id=? AND subject=?`, issuer, clientID, subject).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		id = uuid.NewString()
		now := s.now().UnixMilli()
		if _, err = tx.ExecContext(ctx, `INSERT INTO accounts(id,created_at) VALUES(?,?)`, id, now); err != nil {
			return "", err
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO account_identities(issuer,client_id,subject,account_id,created_at) VALUES(?,?,?,?,?)`, issuer, clientID, subject, id, now); err != nil {
			return "", err
		}
	} else if err != nil {
		return "", err
	}
	return id, tx.Commit()
}
func (s *Service) issue(ctx context.Context, w http.ResponseWriter, userID string) error {
	token, err := secret()
	if err != nil {
		return err
	}
	now := s.now()
	absolute := now.Add(time.Duration(s.cfg.AbsoluteSeconds) * time.Second)
	idle := minTime(now.Add(time.Duration(s.cfg.IdleSeconds)*time.Second), absolute)
	_, err = s.db.Write().ExecContext(ctx, `INSERT INTO account_sessions(id,account_id,token_hash,created_at,idle_expires_at,absolute_expires_at,renewed_at) VALUES(?,?,?,?,?,?,?)`, uuid.NewString(), userID, digest(token), now.UnixMilli(), idle.UnixMilli(), absolute.UnixMilli(), now.UnixMilli())
	if err != nil {
		return err
	}
	setCookie(w, cookieName, token, idle, now)
	return nil
}
func (s *Service) Logout(ctx context.Context, w http.ResponseWriter, st Status) error {
	if st.Authenticated {
		if _, err := s.db.Write().ExecContext(ctx, `UPDATE account_sessions SET revoked_at=? WHERE id=?`, s.now().UnixMilli(), st.ID); err != nil {
			return err
		}
	}
	setCookie(w, cookieName, "", time.Unix(1, 0), s.now())
	return nil
}
func (s *Service) Active(ctx context.Context, id string) (bool, error) {
	if !s.Enabled() {
		return false, nil
	}
	var n int
	now := s.now().UnixMilli()
	err := s.db.Read().QueryRowContext(ctx, `SELECT COUNT(*) FROM account_sessions WHERE id=? AND revoked_at IS NULL AND idle_expires_at>? AND absolute_expires_at>?`, id, now, now).Scan(&n)
	return n == 1, err
}

// Require binds a game session once; another account cannot inherit its protected seats.
func (s *Service) Require(ctx context.Context, r *http.Request, form, gameID string) (Status, error) {
	if !s.Required(form) {
		return Status{}, nil
	}
	st, err := s.Current(ctx, r)
	if err != nil {
		return Status{}, err
	}
	if !st.Authenticated {
		return Status{}, ErrRequired
	}

	var bound, authorized string
	err = s.db.Read().QueryRowContext(ctx, `SELECT account_id,COALESCE(authorized_session_id,'') FROM account_game_bindings WHERE session_id=?`, gameID).Scan(&bound, &authorized)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return Status{}, err
	}
	if bound != "" && bound != st.UserID {
		return Status{}, problem.Forbidden("account-changed", "登录的账户变了，请回到大厅重新进入")
	}
	if bound == st.UserID && authorized == st.ID {
		return st, nil
	}
	_, err = s.db.Write().ExecContext(ctx, `INSERT INTO account_game_bindings(session_id,account_id,authorized_session_id,created_at) VALUES(?,?,?,?) ON CONFLICT(session_id) DO UPDATE SET authorized_session_id=excluded.authorized_session_id WHERE account_game_bindings.account_id=excluded.account_id`, gameID, st.UserID, st.ID, s.now().UnixMilli())
	if err != nil {
		return Status{}, err
	}
	var actual string
	if err = s.db.Read().QueryRowContext(ctx, `SELECT account_id FROM account_game_bindings WHERE session_id=?`, gameID).Scan(&actual); err != nil {
		return Status{}, err
	}
	if actual != st.UserID {
		return Status{}, problem.Forbidden("account-changed", "登录的账户变了，请回到大厅重新进入")
	}

	return st, nil
}

// GameActive revalidates a captured streaming authorization without accepting a cookie substitute.
func (s *Service) GameActive(ctx context.Context, gameID string) (bool, error) {
	if !s.Enabled() {
		return false, nil
	}
	now := s.now().UnixMilli()
	var n int
	err := s.db.Read().QueryRowContext(ctx, `SELECT COUNT(*) FROM account_game_bindings b JOIN account_sessions a ON a.id=b.authorized_session_id AND a.account_id=b.account_id WHERE b.session_id=? AND a.revoked_at IS NULL AND a.idle_expires_at>? AND a.absolute_expires_at>?`, gameID, now, now).Scan(&n)
	return n == 1, err
}
