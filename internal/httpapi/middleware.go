package httpapi

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/bhcn/umaguessinggame/internal/httpapi/problem"
)

// ctxKey is the private context key namespace.
type ctxKey int

const (
	ctxRequestID ctxKey = iota
	ctxSession
	ctxRoute
)

// routeInfo records the matched operation for logging and metrics.
type routeInfo struct {
	OperationID string
	// Write reports whether the operation mutates state, which decides whether
	// CSRF and Origin checks apply.
	Write bool
	// RateClass selects the limiter bucket.
	RateClass string
}

// requestIDOf reads the request-scoped identifier.
func requestIDOf(r *http.Request) string {
	if v, ok := r.Context().Value(ctxRequestID).(string); ok {
		return v
	}
	return ""
}

// withRequestID assigns or adopts a request identifier.
//
// A client-supplied identifier is adopted only if it is short and printable, so a
// hostile value cannot inject newlines into logs or bloat them. A rejected value is
// removed from the incoming header as well, so no later code path can read it back.
func withRequestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := r.Header.Get("X-Request-Id")
		if validRequestID(id) {
			// Adopt it as-is.
		} else {
			if id != "" {
				r.Header.Del("X-Request-Id")
			}
			id = newRequestID()
		}
		w.Header().Set("X-Request-Id", id)
		// Date is set explicitly so a proxy cannot drop it and so tests can
		// assert its presence.
		w.Header().Set("Date", time.Now().UTC().Format(http.TimeFormat))
		ctx := context.WithValue(r.Context(), ctxRequestID, id)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// validRequestID accepts 8..128 printable ASCII characters.
func validRequestID(id string) bool {
	if len(id) < 8 || len(id) > 128 {
		return false
	}
	for i := 0; i < len(id); i++ {
		if id[i] < 0x21 || id[i] > 0x7e {
			return false
		}
	}
	return true
}

// newRequestID returns 128 bits of randomness, hex encoded.
func newRequestID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		// A clock-derived fallback still yields a unique, log-safe identifier.
		return "rid" + strconv.FormatInt(time.Now().UnixNano(), 36)
	}
	return hex.EncodeToString(b[:])
}

// recoverPanic converts a panic into a 500 Problem without leaking the stack.
func (s *Server) recoverPanic(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rec := recover(); rec != nil {
				s.log.Error("panic recovered",
					"requestId", requestIDOf(r),
					"operationId", operationOf(r),
					"path", r.URL.Path)
				s.metrics.Inc("http_panics_total")
				s.writeError(w, r, problem.Internal(nil))
			}
		}()
		next.ServeHTTP(w, r)
	})
}

// operationOf reads the matched operation id from the context.
func operationOf(r *http.Request) string {
	if v, ok := r.Context().Value(ctxRoute).(routeInfo); ok {
		return v.OperationID
	}
	return ""
}

// withRoute annotates the request with its operation identity.
func withRoute(info routeInfo, next http.HandlerFunc) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := context.WithValue(r.Context(), ctxRoute, info)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// statusRecorder captures the status code and byte count for logging.
type statusRecorder struct {
	http.ResponseWriter
	status int
	bytes  int
	wrote  bool
}

func (s *statusRecorder) WriteHeader(code int) {
	if !s.wrote {
		s.status = code
		s.wrote = true
	}
	s.ResponseWriter.WriteHeader(code)
}

func (s *statusRecorder) Write(p []byte) (int, error) {
	if !s.wrote {
		s.status = http.StatusOK
		s.wrote = true
	}
	n, err := s.ResponseWriter.Write(p)
	s.bytes += n
	return n, err
}

// Unwrap preserves ResponseController deadlines and WebSocket upgrades.
func (s *statusRecorder) Unwrap() http.ResponseWriter { return s.ResponseWriter }

// Flush forwards to the wrapped writer so SSE can stream.
func (s *statusRecorder) Flush() {
	if f, ok := s.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// withLogging records one structured line per request.
//
// The line deliberately contains no cookie, token, body or display name: only the
// operation, status, duration and the opaque request id .
func (s *Server) withLogging(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rec, r)
		dur := time.Since(start)
		s.metrics.Observe("http_request_duration", dur)
		s.metrics.Inc("http_requests_total")
		if rec.status >= 400 {
			s.metrics.Inc("http_errors_total")
		}
		op := operationOf(r)
		if op == "" {
			switch r.URL.Path {
			case "/health/live":
				op = "getLiveness"
			case "/health/ready":
				op = "getReadiness"
			default:
				op = "unmatched"
			}
		}
		s.log.Info("http request",
			"requestId", requestIDOf(r),
			"operationId", op,
			"method", r.Method,
			"path", r.URL.Path,
			"status", rec.status,
			"durationMs", dur.Milliseconds(),
		)
	})
}

// ---- rate limiting -------------------------------------------------------

// LimiterConfig configures the token-bucket limiters.
//
// Several independent buckets are required: limiting only by public IP would make
// one classroom NAT look like a single abusive client and lock out every player
// behind it .
type LimiterConfig struct {
	// MaxBuckets caps attacker-controlled key cardinality. Existing clients keep
	// their allowance when unknown keys are refused at capacity.
	MaxBuckets int
	// RequestPerIP bounds work before credential lookups, including invalid cookies.
	RequestPerIP Rate
	// AnonymousPerIP bounds public catalogue reads.
	AnonymousPerIP Rate
	// PublicPerIP bounds public invite lookups and grant redemption per address.
	PublicPerIP Rate
	// SessionCreatePerIP allows coordinated first arrivals behind a venue NAT,
	// without relaxing invite/grant lookup limits or trusting a client fingerprint.
	SessionCreatePerIP Rate
	// SessionPerMinute bounds authenticated operations per session.
	SessionPerMinute Rate
	// RoomPerMinute bounds commands per room.
	RoomPerMinute Rate
	// ActionPerMinute bounds a single action class per session, so a guess loop
	// cannot exhaust the room bucket.
	ActionPerMinute Rate
	// Burst sizes buckets whose rate has no explicit BurstTokens capacity.
	Burst float64
}

// Rate is a sustained per-minute allowance.
type Rate struct {
	PerMinute int
	// BurstTokens overrides the refill-derived capacity for coordinated arrivals.
	// Zero preserves the default used by ordinary request classes.
	BurstTokens int
}

func (c LimiterConfig) withDefaults() LimiterConfig {
	if c.RequestPerIP.PerMinute <= 0 {
		c.RequestPerIP.PerMinute = 6000
	}
	if c.RequestPerIP.BurstTokens <= 0 {
		c.RequestPerIP.BurstTokens = 1200
	}
	if c.AnonymousPerIP.PerMinute <= 0 {
		c.AnonymousPerIP.PerMinute = 600
	}
	if c.AnonymousPerIP.BurstTokens <= 0 {
		c.AnonymousPerIP.BurstTokens = 120
	}
	if c.MaxBuckets <= 0 {
		c.MaxBuckets = 20000
	}
	if c.PublicPerIP.PerMinute <= 0 {
		c.PublicPerIP.PerMinute = 30
	}
	if c.PublicPerIP.BurstTokens <= 0 {
		c.PublicPerIP.BurstTokens = 30
	}
	if c.SessionCreatePerIP.PerMinute <= 0 {
		c.SessionCreatePerIP.PerMinute = 300
	}
	if c.SessionCreatePerIP.BurstTokens <= 0 {
		c.SessionCreatePerIP.BurstTokens = 100
	}
	if c.SessionPerMinute.PerMinute <= 0 {
		c.SessionPerMinute.PerMinute = 240
	}
	if c.SessionPerMinute.BurstTokens <= 0 {
		c.SessionPerMinute.BurstTokens = 120
	}
	if c.RoomPerMinute.PerMinute <= 0 {
		c.RoomPerMinute.PerMinute = 600
	}
	if c.RoomPerMinute.BurstTokens <= 0 {
		c.RoomPerMinute.BurstTokens = 300
	}
	if c.ActionPerMinute.PerMinute <= 0 {
		c.ActionPerMinute.PerMinute = 120
	}
	if c.ActionPerMinute.BurstTokens <= 0 {
		c.ActionPerMinute.BurstTokens = 60
	}
	if c.Burst <= 0 {
		c.Burst = 1.5
	}
	return c
}

// Limiter is a set of independent token buckets.
type Limiter struct {
	cfg LimiterConfig

	mu      sync.Mutex
	buckets map[string]*bucket
	// Stale cleanup reclaims capacity; MaxBuckets is the hard allocation bound.
	lastSweep time.Time
	now       func() time.Time
}

type bucket struct {
	tokens   float64
	capacity float64
	perSec   float64
	last     time.Time
}

// NewLimiter builds a limiter with the configured allowances.
func NewLimiter(cfg LimiterConfig) *Limiter {
	return &Limiter{
		cfg:       cfg.withDefaults(),
		buckets:   make(map[string]*bucket),
		lastSweep: time.Now(),
		now:       time.Now,
	}
}

// Allow consumes one token from the named bucket.
//
// It returns the retry hint in seconds when the bucket is empty.
func (l *Limiter) Allow(name string, rate Rate) (bool, int) {
	if len(name) == 0 || len(name) > 256 {
		return false, 60
	}
	perMinute := rate.PerMinute
	if perMinute <= 0 {
		return true, 0
	}
	perSec := float64(perMinute) / 60
	capacity := perSec * l.cfg.Burst
	if rate.BurstTokens > 0 {
		capacity = float64(rate.BurstTokens)
	}
	if capacity < 1 {
		capacity = 1
	}
	now := l.now()

	l.mu.Lock()
	defer l.mu.Unlock()
	l.sweepLocked(now)

	b, ok := l.buckets[name]
	if !ok {
		// Never evict a live bucket to admit a new one: cycling identities or
		// room paths must not reset an existing client's exhausted allowance.
		if len(l.buckets) >= l.cfg.MaxBuckets {
			return false, 60
		}
		b = &bucket{tokens: capacity, capacity: capacity, perSec: perSec, last: now}
		l.buckets[name] = b
	}
	// Refill based on elapsed time.
	elapsed := now.Sub(b.last).Seconds()
	if elapsed > 0 {
		b.tokens += elapsed * b.perSec
		if b.tokens > b.capacity {
			b.tokens = b.capacity
		}
		b.last = now
	}
	if b.tokens < 1 {
		wait := (1 - b.tokens) / b.perSec
		retry := int(wait + 0.999)
		if retry < 1 {
			retry = 1
		}
		return false, retry
	}
	b.tokens--
	return true, 0
}

// sweepLocked drops idle buckets that have fully refilled.
func (l *Limiter) sweepLocked(now time.Time) {
	if now.Sub(l.lastSweep) < time.Minute {
		return
	}
	l.lastSweep = now
	for k, b := range l.buckets {
		if now.Sub(b.last) > 5*time.Minute && b.tokens+now.Sub(b.last).Seconds()*b.perSec >= b.capacity {
			delete(l.buckets, k)
		}
	}
}

// limit applies the limiter for a route.
func (s *Server) limit(w http.ResponseWriter, r *http.Request, info routeInfo) error {
	sess, hasSession := sessionOf(r)
	switch info.RateClass {
	case "account-login":
		if ok, retry := s.limiter.Allow("account-login:"+s.clientIP(r), Rate{PerMinute: 30, BurstTokens: 10}); !ok {
			return problem.RateLimited(retry)
		}
	case "session-create":
		if ok, retry := s.limiter.Allow("session-create-ip:"+s.clientIP(r), s.cfg.Limiter.SessionCreatePerIP); !ok {
			return problem.RateLimited(retry)
		}
	case "matchmaking-lobby":
		if ok, retry := s.limiter.Allow("match-lobby-ip:"+s.clientIP(r), Rate{PerMinute: 600, BurstTokens: 200}); !ok {
			return problem.RateLimited(retry)
		}
		if hasSession {
			if ok, retry := s.limiter.Allow("match-lobby-session:"+sess.ID, Rate{PerMinute: 60, BurstTokens: 10}); !ok {
				return s.sessionRateLimited(sess.ID, retry)
			}
		}
	case "admission-status":
		// Status polling at an event must not consume invitation redemption's
		// much smaller shared-IP budget.
		if ok, retry := s.limiter.Allow("entry-state-ip:"+s.clientIP(r), Rate{PerMinute: 600, BurstTokens: 200}); !ok {
			return problem.RateLimited(retry)
		}
	case "identity-recovery":
		// A flooded game-action budget must not lock the player out of the
		// identity/CSRF recovery needed to complete verification after a reload.
		for _, gate := range []struct {
			key  string
			rate Rate
		}{
			{"identity-session:" + sess.ID, Rate{PerMinute: 24, BurstTokens: 8}},
			{"identity-ip:" + s.clientIP(r), Rate{PerMinute: 600, BurstTokens: 200}},
		} {
			if ok, retry := s.limiter.Allow(gate.key, gate.rate); !ok {
				return problem.RateLimited(retry)
			}
		}
	case "admission":
		for _, gate := range []struct {
			key  string
			rate Rate
		}{{"entry-session:" + sess.ID, Rate{PerMinute: 12, BurstTokens: 3}}, {"entry-ip:" + s.clientIP(r), Rate{PerMinute: 600, BurstTokens: 200}}} {
			if ok, retry := s.limiter.Allow(gate.key, gate.rate); !ok {
				return problem.RateLimited(retry)
			}
		}

	case "challenge-create", "challenge-action":
		if !hasSession {
			return nil
		}
		rate := Rate{PerMinute: 60, BurstTokens: 4}
		if info.RateClass == "challenge-create" {
			rate = Rate{PerMinute: 12, BurstTokens: 3}
		}
		for _, gate := range []struct {
			key  string
			rate Rate
		}{
			{"verified-session:" + sess.ID + ":" + info.RateClass, rate},
			{"verified-ip:" + s.clientIP(r), Rate{PerMinute: 600, BurstTokens: 200}},
		} {
			if ok, retry := s.limiter.Allow(gate.key, gate.rate); !ok {
				if strings.HasPrefix(gate.key, "verified-session:") {
					return s.sessionRateLimited(sess.ID, retry)
				}
				return problem.RateLimited(retry)
			}
		}
	case "public":
		// Public lookup/redemption operations share a conservative address gate.
		// First arrivals use the separate session-create class above.
		ok, retry := s.limiter.Allow("ip:"+s.clientIP(r), s.cfg.Limiter.PublicPerIP)
		if !ok {
			return problem.RateLimited(retry)
		}
		if hasSession {
			ok, retry = s.limiter.Allow("ses:"+sess.ID, s.cfg.Limiter.SessionPerMinute)
			if !ok {
				return s.sessionRateLimited(sess.ID, retry)
			}
		}
	case "stream":
		// Shared-IP venues need room for a whole audience; the per-session bucket
		// limits reconnect storms without banning everyone behind the same AP.
		for _, gate := range []struct {
			key  string
			rate Rate
		}{
			{"stream-ip:" + s.clientIP(r), Rate{PerMinute: 300, BurstTokens: 200}},
			{"stream-session:" + sess.ID, Rate{PerMinute: 12, BurstTokens: 3}},
		} {
			if ok, retry := s.limiter.Allow(gate.key, gate.rate); !ok {
				return problem.RateLimited(retry)
			}
		}
	case "", "session":
		// Catalogue reads have a separate address budget for shared-network venues.
		if !hasSession {
			if ok, retry := s.limiter.Allow("catalogue-ip:"+s.clientIP(r), s.cfg.Limiter.AnonymousPerIP); !ok {
				return problem.RateLimited(retry)
			}
			return nil
		}
		ok, retry := s.limiter.Allow("ses:"+sess.ID, s.cfg.Limiter.SessionPerMinute)
		if !ok {
			return s.sessionRateLimited(sess.ID, retry)
		}
	case "room":
		if !hasSession {
			return nil
		}
		// A room command is limited by session, by room and by action class. The
		// three buckets mean a single hot room cannot starve other rooms, and one
		// guess loop cannot starve that room's other commands.
		ok, retry := s.limiter.Allow("ses:"+sess.ID, s.cfg.Limiter.SessionPerMinute)
		if !ok {
			return s.sessionRateLimited(sess.ID, retry)
		}
		ok, retry = s.limiter.Allow("act:"+sess.ID+":"+info.OperationID, s.cfg.Limiter.ActionPerMinute)
		if !ok {
			return s.sessionRateLimited(sess.ID, retry)
		}
		resource := r.PathValue("roomId")
		if resource == "" {
			resource = r.PathValue("roundId")
		}
		if resource == "" {
			resource = r.PathValue("eventId")
		}
		if resource != "" && s.rooms != nil {
			kind := "room"
			if r.PathValue("roundId") != "" {
				kind = "round"
			} else if r.PathValue("eventId") != "" {
				kind = "event"
			}
			// Unauthorised requests retain their session/action limits, but cannot
			// consume a victim room's allowance just by knowing its URL.
			roomID, err := s.rooms.CommandBudgetRoom(r.Context(), sess.ID, kind, resource)
			if err != nil {
				return problem.Internal(err)
			}
			resource = roomID
		}
		if resource != "" {
			ok, retry = s.limiter.Allow("resource:"+resource+":"+info.OperationID, s.cfg.Limiter.RoomPerMinute)
			if !ok {
				return problem.RateLimited(retry)
			}
		}
	}
	return nil
}

// sessionOf reads the authenticated session from the context.
func sessionOf(r *http.Request) (sessionValue, bool) {
	v, ok := r.Context().Value(ctxSession).(sessionValue)
	return v, ok
}

// observeProblem records the stable code for metrics without logging details.
func (s *Server) observeProblem(r *http.Request, e *problem.Error) {
	if e == nil {
		return
	}
	s.metrics.Inc("problem_" + e.Code)
	if e.Status >= 500 {
		// 500 的 cause 必须写进日志。problem.Error 明确保留 cause 供服务端排查
		// （problem.go:56「retained for server-side logging only」），但这里原本只打
		// code/status，于是线上出现 500 时日志里只有「internal-error」，真实原因
		// 完全查不到——2026-11-11 排查 race-rebuild 练习包 500 时就卡在这。
		// cause 只进日志，不进响应体，不会泄露路径或 SQL。
		fields := []any{
			"requestId", requestIDOf(r),
			"operationId", operationOf(r),
			"code", e.Code,
			"status", e.Status,
		}
		if cause := e.Unwrap(); cause != nil {
			fields = append(fields, "cause", cause.Error())
		}
		s.log.Error("request failed", fields...)
		return
	}
	s.log.Info("request rejected",
		"requestId", requestIDOf(r),
		"operationId", operationOf(r),
		"code", e.Code,
		"status", e.Status)
}

func (s *Server) sessionRateLimited(id string, retry int) error {
	s.admission.ObserveLimit(id)
	return problem.RateLimited(retry)
}
