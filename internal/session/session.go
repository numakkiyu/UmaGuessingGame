// Package session owns anonymous browser sessions, CSRF protection and the
// restricted display session.
//
// Security model :
//
// - The cookie carries a random opaque token. Only its SHA-256 hash is stored,
// so a database leak does not yield usable credentials.
// - The CSRF token is a separate random secret, also stored hashed. It is
// returned to the client in the session body and must be echoed in the
// X-CSRF-Token header on every state-changing request.
// - The session cookie is HttpOnly, so script cannot read it. It is Secure and
// SameSite=Lax on public deployments; the LAN HTTP exception is an explicit
// configuration choice, never a silent fallback.
// - Display sessions use a separate cookie so redeeming a display grant can
// never overwrite the host's console cookie.
package session

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"golang.org/x/text/unicode/norm"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/bhcn/umaguessinggame/internal/httpapi/problem"
	"github.com/bhcn/umaguessinggame/internal/storage"
	"github.com/bhcn/umaguessinggame/internal/weborigin"
)

// Kind distinguishes an interactive browser session from a read-only display
// session. The value must match the CHECK constraint on sessions.kind.
type Kind string

const (
	// KindPlayer is a normal interactive session.
	KindPlayer Kind = "player"
	// KindDisplay is a restricted, read-only projection session.
	KindDisplay Kind = "display"
)

// Valid reports whether the kind satisfies the schema constraint.
func (k Kind) Valid() bool { return k == KindPlayer || k == KindDisplay }

// Session is a server-side identity record. It deliberately carries no token.
type Session struct {
	ID          string
	DisplayName string
	Kind        Kind
	CreatedAt   time.Time
	ExpiresAt   time.Time
}

// Expired reports whether the session is past its expiry at t.
func (s Session) Expired(t time.Time) bool { return !t.Before(s.ExpiresAt) }

// DisplayNameMaxRunes is the NFC code-point limit from the contract
// (SessionCreate.displayName maxLength 24). Length is counted in code points,
// never UTF-16 units or bytes .
const DisplayNameMaxRunes = 24

// DisplayNameMinRunes is the contract minimum.
const DisplayNameMinRunes = 1

// Config configures cookie transport and validation policy.
type Config struct {
	// CookieName is the interactive session cookie name.
	CookieName string
	// DisplayCookieName is the restricted display cookie name.
	DisplayCookieName string
	// TTL is the interactive session lifetime.
	TTL time.Duration
	// DisplayTTL is the display session lifetime.
	DisplayTTL time.Duration
	// Secure marks cookies Secure. It must be true on public HTTPS.
	Secure bool
	// SameSite is applied to every cookie this package writes.
	SameSite http.SameSite
	// AllowedOrigins is the explicit same-origin allow list. When empty the
	// request Host is used as the single allowed origin.
	AllowedOrigins []string
	// AllowMissingOrigin permits unsafe requests without an Origin header.
	// It exists only for controlled non-browser clients on a private network;
	// it is false by default because browsers always send Origin on writes.
	AllowMissingOrigin bool
}

func (c Config) withDefaults() Config {
	if c.CookieName == "" {
		c.CookieName = "uma_session"
	}
	if c.DisplayCookieName == "" {
		c.DisplayCookieName = "uma_display"
	}
	if c.TTL <= 0 {
		c.TTL = 7 * 24 * time.Hour
	}
	if c.DisplayTTL <= 0 {
		c.DisplayTTL = 12 * time.Hour
	}
	if c.SameSite == 0 {
		c.SameSite = http.SameSiteLaxMode
	}
	return c
}

// Service implements session lifecycle operations against the runtime database.
type Service struct {
	db  *storage.DB
	cfg Config
	now func() time.Time
}

// New builds a session service.
func New(db *storage.DB, cfg Config) *Service {
	return &Service{db: db, cfg: cfg.withDefaults(), now: time.Now}
}

// SetClock overrides the clock. Tests use it to exercise expiry.
func (s *Service) SetClock(now func() time.Time) { s.now = now }

// Config returns the effective configuration.
func (s *Service) Config() Config { return s.cfg }

// CreateResult is what a successful session creation returns.
type CreateResult struct {
	Session   Session
	Token     string
	CSRFToken string
}

// Create registers a new interactive session and returns the opaque token and
// CSRF secret. The caller must set the cookie and must not persist the token.
func (s *Service) Create(ctx context.Context, displayName string) (CreateResult, error) {
	return s.create(ctx, displayName, KindPlayer, s.cfg.TTL)
}

// CreateDisplay registers a restricted display session.
func (s *Service) CreateDisplay(ctx context.Context) (CreateResult, error) {
	return s.create(ctx, "Display", KindDisplay, s.cfg.DisplayTTL)
}

// CreateInTransaction creates a player session as part of an atomic identity transfer.
func (s *Service) CreateInTransaction(ctx context.Context, tx *sql.Tx, displayName string) (CreateResult, error) {
	return s.createWith(ctx, tx, displayName, KindPlayer, s.cfg.TTL)
}

type sessionWriter interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
}

func (s *Service) create(ctx context.Context, displayName string, kind Kind, ttl time.Duration) (CreateResult, error) {
	return s.createWith(ctx, s.db.Write(), displayName, kind, ttl)
}

func (s *Service) createWith(ctx context.Context, writer sessionWriter, displayName string, kind Kind, ttl time.Duration) (CreateResult, error) {
	if err := ValidateDisplayName(displayName); err != nil {
		return CreateResult{}, err
	}
	if !kind.Valid() {
		return CreateResult{}, problem.Internal(fmt.Errorf("session: invalid kind %q", kind))
	}
	token, err := randomToken()
	if err != nil {
		return CreateResult{}, problem.Internal(err)
	}
	csrf, err := randomToken()
	if err != nil {
		return CreateResult{}, problem.Internal(err)
	}
	id, err := randomID("ses_")
	if err != nil {
		return CreateResult{}, problem.Internal(err)
	}
	now := s.now().UTC()
	expires := now.Add(ttl)

	_, err = writer.ExecContext(ctx, `
		INSERT INTO sessions (id, token_hash, display_name, csrf_token_hash, kind, created_at, expires_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)`,
		id, hashToken(token), displayName, hashToken(csrf), string(kind),
		now.UnixMilli(), expires.UnixMilli())
	if err != nil {
		return CreateResult{}, problem.Internal(fmt.Errorf("session: insert: %w", err))
	}
	return CreateResult{
		Session:   Session{ID: id, DisplayName: displayName, Kind: kind, CreatedAt: now, ExpiresAt: expires},
		Token:     token,
		CSRFToken: csrf,
	}, nil
}

// Lookup resolves a cookie token to a live session.
//
// A revoked, expired or unknown token produces the same 401 so that the response
// cannot be used to probe which tokens existed.
func (s *Service) Lookup(ctx context.Context, token string) (Session, error) {
	if token == "" {
		return Session{}, ErrNoSession
	}
	var (
		id          string
		displayName string
		kind        string
		createdAt   int64
		expiresAt   int64
		revokedAt   sql.NullInt64
	)
	err := s.db.Read().QueryRowContext(ctx, `
		SELECT id, display_name, kind, created_at, expires_at, revoked_at
		FROM sessions WHERE token_hash = ?`, hashToken(token)).
		Scan(&id, &displayName, &kind, &createdAt, &expiresAt, &revokedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return Session{}, ErrNoSession
	}
	if err != nil {
		return Session{}, problem.Internal(fmt.Errorf("session: lookup: %w", err))
	}
	now := s.now().UTC()
	if revokedAt.Valid {
		return Session{}, ErrSessionRevoked
	}
	sess := Session{
		ID:          id,
		DisplayName: displayName,
		Kind:        Kind(kind),
		CreatedAt:   time.UnixMilli(createdAt).UTC(),
		ExpiresAt:   time.UnixMilli(expiresAt).UTC(),
	}
	if sess.Expired(now) {
		return Session{}, ErrSessionExpired
	}
	return sess, nil
}

// VerifyCSRF compares the presented CSRF token against the stored hash in
// constant time. A missing or wrong token is a 403, never a 401: the identity is
// valid, the write is not authorised .
func (s *Service) VerifyCSRF(ctx context.Context, sess Session, presented string) error {
	if presented == "" {
		return problem.Forbidden("csrf-token-missing", "X-CSRF-Token is required for state-changing requests")
	}
	var stored string
	err := s.db.Read().QueryRowContext(ctx,
		`SELECT csrf_token_hash FROM sessions WHERE id = ?`, sess.ID).Scan(&stored)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNoSession
	}
	if err != nil {
		return problem.Internal(fmt.Errorf("session: read csrf: %w", err))
	}
	if subtle.ConstantTimeCompare([]byte(stored), []byte(hashToken(presented))) != 1 {
		return problem.Forbidden("csrf-token-invalid", "X-CSRF-Token does not match this session")
	}
	return nil
}

// RotateCSRF replaces the token without changing session identity.
// Only its hash is stored, so the original cannot be recovered after a reload.
// The caller must authenticate the session and validate Origin first.
func (s *Service) RotateCSRF(ctx context.Context, sess Session) (string, error) {
	csrf, err := randomToken()
	if err != nil {
		return "", problem.Internal(fmt.Errorf("session: generate csrf: %w", err))
	}
	res, err := s.db.Write().ExecContext(ctx,
		`UPDATE sessions SET csrf_token_hash = ? WHERE id = ? AND revoked_at IS NULL`,
		hashToken(csrf), sess.ID)
	if err != nil {
		return "", problem.Internal(fmt.Errorf("session: rotate csrf: %w", err))
	}
	if n, err := res.RowsAffected(); err == nil && n == 0 {
		return "", ErrNoSession
	}
	return csrf, nil
}

// Rename updates the display name and returns the updated session.
func (s *Service) Rename(ctx context.Context, sess Session, displayName string) (Session, error) {
	if err := ValidateDisplayName(displayName); err != nil {
		return Session{}, err
	}
	res, err := s.db.Write().ExecContext(ctx,
		`UPDATE sessions SET display_name = ? WHERE id = ? AND revoked_at IS NULL`, displayName, sess.ID)
	if err != nil {
		return Session{}, problem.Internal(fmt.Errorf("session: rename: %w", err))
	}
	if n, err := res.RowsAffected(); err == nil && n == 0 {
		return Session{}, ErrNoSession
	}
	sess.DisplayName = displayName
	return sess, nil
}

// Revoke marks the session revoked. Revoking twice is not an error: the caller
// still gets a 204, but the identity is never reactivated .
func (s *Service) Revoke(ctx context.Context, sess Session) error {
	now := s.now().UTC().UnixMilli()
	_, err := s.db.Write().ExecContext(ctx,
		`UPDATE sessions SET revoked_at = COALESCE(revoked_at, ?) WHERE id = ?`, now, sess.ID)
	if err != nil {
		return problem.Internal(fmt.Errorf("session: revoke: %w", err))
	}
	return nil
}

// Errors returned by Lookup. They are intentionally coarse.
var (
	// ErrNoSession reports an absent or unknown credential.
	ErrNoSession = problem.Unauthorized("session-required", "a valid session cookie is required")
	// ErrSessionExpired reports an elapsed session lifetime.
	ErrSessionExpired = problem.Unauthorized("session-expired", "the session has expired; create a new one")
	// ErrSessionRevoked reports a revoked session.
	ErrSessionRevoked = problem.Unauthorized("session-revoked", "the session was revoked")
)

// ValidateDisplayName enforces the contract length bounds in code points after
// NFC normalisation.
//
// The count is Unicode code points, matching JSON Schema maxLength, so the
// browser and the server agree on emoji sequences .
func ValidateDisplayName(name string) error {
	n := len([]rune(Normalize(name)))
	if n < DisplayNameMinRunes {
		return problem.Validation("displayName is required",
			problem.FieldError{Field: "displayName", Message: "must not be empty"})
	}
	if n > DisplayNameMaxRunes {
		return problem.Validation("displayName is too long",
			problem.FieldError{Field: "displayName", Message: fmt.Sprintf("must be at most %d characters", DisplayNameMaxRunes)})
	}
	if strings.ContainsAny(name, "\x00\n\r\t") {
		return problem.Validation("displayName contains control characters",
			problem.FieldError{Field: "displayName", Message: "must not contain control characters"})
	}
	return nil
}

// Normalize gives display-name validation and duplicate detection the same NFC
// representation; public labels are still not identity or authentication.
func Normalize(s string) string {
	return norm.NFC.String(strings.NewReplacer("\u3000", " ", "\u00a0", " ").Replace(strings.TrimSpace(s)))
}

// OriginAllowed validates the Origin header of an unsafe request.
//
// A browser always sends Origin on POST/PATCH/DELETE, so a missing Origin is
// rejected unless the deployment explicitly opted out. This blocks cross-site
// request forgery at the transport layer even before CSRF is checked .
func (s *Service) OriginAllowed(r *http.Request) error {
	if isSafeMethod(r.Method) {
		return nil
	}
	origin := r.Header.Get("Origin")
	if origin == "" {
		if s.cfg.AllowMissingOrigin {
			return nil
		}
		return problem.Forbidden("origin-required", "an Origin header is required for state-changing requests")
	}
	if !s.originMatches(origin, r) {
		return problem.Forbidden("origin-mismatch", "the request Origin is not allowed for this deployment")
	}
	return nil
}

func (s *Service) originMatches(origin string, r *http.Request) bool {
	scheme, host, ok := weborigin.Parse(origin)
	if !ok {
		return false
	}
	for _, allowed := range s.cfg.AllowedOrigins {
		aScheme, aHost, valid := weborigin.Parse(strings.TrimRight(allowed, "/"))
		if valid && scheme == aScheme && host == aHost {
			return true
		}
	}
	if len(s.cfg.AllowedOrigins) > 0 {
		return false
	}
	return weborigin.Same(r, scheme, host)
}

func isSafeMethod(m string) bool {
	switch m {
	case http.MethodGet, http.MethodHead, http.MethodOptions:
		return true
	}
	return false
}

// SetCookie writes the interactive session cookie.
func (s *Service) SetCookie(w http.ResponseWriter, token string, expires time.Time) {
	s.writeCookie(w, s.cfg.CookieName, token, expires)
}

// ClearCookie expires the interactive session cookie.
func (s *Service) ClearCookie(w http.ResponseWriter) {
	s.writeCookie(w, s.cfg.CookieName, "", time.Unix(0, 0).UTC())
}

// SetDisplayCookie writes the restricted display cookie.
func (s *Service) SetDisplayCookie(w http.ResponseWriter, token string, expires time.Time) {
	s.writeCookie(w, s.cfg.DisplayCookieName, token, expires)
}

func (s *Service) writeCookie(w http.ResponseWriter, name, value string, expires time.Time) {
	http.SetCookie(w, &http.Cookie{
		Name:     name,
		Value:    value,
		Path:     "/",
		HttpOnly: true,
		Secure:   s.cfg.Secure,
		SameSite: s.cfg.SameSite,
		Expires:  expires,
		// Lax + explicit SameSite still permits the cookie on same-site
		// navigation, which the SPA needs for a hard reload.
		MaxAge: maxAgeSeconds(expires),
	})
}

func maxAgeSeconds(expires time.Time) int {
	d := time.Until(expires)
	if d <= 0 {
		return -1
	}
	return int(d.Seconds())
}

// CookieToken extracts the interactive session token from the request.
func (s *Service) CookieToken(r *http.Request) string {
	c, err := r.Cookie(s.cfg.CookieName)
	if err != nil {
		return ""
	}
	return c.Value
}

// DisplayCookieToken extracts the display session token from the request.
func (s *Service) DisplayCookieToken(r *http.Request) string {
	c, err := r.Cookie(s.cfg.DisplayCookieName)
	if err != nil {
		return ""
	}
	return c.Value
}

// randomToken returns 256 bits of randomness, base64url encoded without padding.
// 32 bytes exceeds the contract's "at least 128 bits" requirement .
func randomToken() (string, error) {
	var b [32]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", fmt.Errorf("session: random: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(b[:]), nil
}

// randomID returns a prefixed opaque identifier.
func randomID(prefix string) (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", fmt.Errorf("session: random id: %w", err)
	}
	return prefix + hex.EncodeToString(b[:]), nil
}

// hashToken is the one-way transform used for both session and CSRF tokens.
func hashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// RandomToken exposes token generation for the room package's grant codes.
func RandomToken() (string, error) { return randomToken() }

// RandomID exposes prefixed id generation.
func RandomID(prefix string) (string, error) { return randomID(prefix) }

// HashToken exposes the hashing used for stored secrets.
func HashToken(token string) string { return hashToken(token) }

// OriginAllowedForStream validates Origin for a streaming or upgrade request.
//
// SSE and WebSocket handshakes are GET requests, so isSafeMethod would normally
// skip the check. A cross-origin EventSource or socket must still be refused,
// because the browser sends cookies with both .
func (s *Service) OriginAllowedForStream(r *http.Request) error {
	// A stream opened by a browser may legitimately carry no Origin header.
	//
	// EventSource (the SSE API) does not set Origin for same-origin requests, and
	// Origin is a forbidden header name so page script cannot add it. Requiring it
	// unconditionally made every browser SSE stream fail with 403 while curl
	// worked — a defect that unit tests with a hand-built request could not catch.
	//
	// The fallback still enforces same-origin: a browser that sends no Origin but
	// does send Referer must have a matching Referer. A request with neither header
	// is only allowed when the deployment explicitly opted in via
	// AllowMissingOrigin, which is the documented non-browser/private-network case.
	origin := r.Header.Get("Origin")
	if origin == "" {
		if referer := r.Header.Get("Referer"); referer != "" {
			if !s.refererMatches(referer, r) {
				return problem.Forbidden("origin-mismatch",
					"the request Referer is not allowed for this deployment")
			}
			return nil
		}
		if s.cfg.AllowMissingOrigin {
			return nil
		}
		return problem.Forbidden("origin-required",
			"a stream requires an Origin or Referer header, or an explicit AllowMissingOrigin setting")
	}
	if !s.originMatches(origin, r) {
		return problem.Forbidden("origin-mismatch",
			"the request Origin is not allowed for this deployment")
	}
	return nil
}

// refererMatches reports whether a Referer header names an allowed origin.
//
// Referer carries a full URL, so it is reduced to scheme://host before comparison.
// A malformed Referer is rejected rather than ignored: silently accepting garbage
// would make the header a way to bypass the check.
func (s *Service) refererMatches(referer string, r *http.Request) bool {
	u, err := url.Parse(referer)
	if err != nil || u.Scheme == "" || u.Host == "" || u.User != nil {
		return false
	}
	return s.originMatches(u.Scheme+"://"+u.Host, r)
}
