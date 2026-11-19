package httpapi

import (
	"context"
	"log/slog"
	"net/http"
	"net/netip"
	"time"

	"github.com/bhcn/umaguessinggame/internal/account"
	"github.com/bhcn/umaguessinggame/internal/admission"
	"github.com/bhcn/umaguessinggame/internal/challenge"
	"github.com/bhcn/umaguessinggame/internal/httpapi/problem"
	"github.com/bhcn/umaguessinggame/internal/observability"
	"github.com/bhcn/umaguessinggame/internal/realtime"
	"github.com/bhcn/umaguessinggame/internal/room"
	"github.com/bhcn/umaguessinggame/internal/session"
)

// Config is the HTTP layer configuration.
type Config struct {
	RealtimeConfig realtime.Config
	// APIVersion is reported by /config.
	APIVersion string
	// BuildVersion is reported by /config and /health.
	BuildVersion string
	// SiteName is reported by /config.
	SiteName string
	// DeploymentMode is "internet" or "lan" (PublicConfig enum).
	DeploymentMode string
	// AssetBaseURL prefixes public media references.
	AssetBaseURL string
	// DefaultTransport is "auto" or "polling" (PublicConfig enum).
	DefaultTransport string
	// MaxRoomMembers and MaxRoomSpectators are advertised lobby limits.
	MaxRoomMembers    int
	MaxRoomSpectators int
	// TrustProxy enables forwarding headers only from TrustedProxies.
	TrustProxy     bool
	TrustedProxies []netip.Prefix
	// MetricsLocalOnly restricts /metrics to loopback.
	MetricsLocalOnly bool
	// RequestTimeout bounds a single request's handler work.
	RequestTimeout time.Duration
	// MaxConcurrentRequests bounds REST work and session lookups.
	MaxConcurrentRequests int
	// Limiter configures the token buckets.
	Limiter LimiterConfig

	// Cookie and origin policy deliberately do NOT live here.
	//
	// They belong to session.Config, which is the component that writes cookies
	// and validates Origin. Duplicating them on this struct was a real defect:
	// the fields existed and were set by cmd/server, but nothing ever read them,
	// so a deployment could set SecureCookies or AllowedOrigins and see no
	// effect while believing it had. The single source of truth is the
	// session.Service passed in Deps; read its policy through
	// session.Service.Config() if a handler ever needs to report it.
	//
	// Likewise the idempotency receipt TTL and the per-room queue bound belong
	// to room.Config, and the session/display lifetimes to session.Config.
	// Keeping copies here would let them silently drift out of sync.
}

func (c Config) withDefaults() Config {
	c.TrustedProxies = append([]netip.Prefix(nil), c.TrustedProxies...)
	// Routes read rates from Config, while NewLimiter also resolves its own
	// allocation defaults. Resolve both from the same value; zero route rates
	// otherwise mean unlimited even when the limiter advertises defaults.
	c.Limiter = c.Limiter.withDefaults()
	if c.APIVersion == "" {
		c.APIVersion = "1"
	}
	if c.BuildVersion == "" {
		c.BuildVersion = "dev"
	}
	if c.SiteName == "" {
		c.SiteName = "UmaGuessingGame"
	}
	if c.DeploymentMode == "" {
		c.DeploymentMode = "lan"
	}
	if c.DefaultTransport == "" {
		c.DefaultTransport = "auto"
	}
	if c.MaxRoomMembers <= 0 {
		c.MaxRoomMembers = 200
	}
	if c.MaxRoomSpectators <= 0 {
		c.MaxRoomSpectators = 300
	}
	if c.MaxConcurrentRequests <= 0 {
		c.MaxConcurrentRequests = 128
	}
	if c.RequestTimeout <= 0 {
		c.RequestTimeout = 20 * time.Second
	}
	return c
}

// Server is the composed HTTP handler.
type Server struct {
	accounts     *account.Service
	admission    *admission.Service
	challenges   *challenge.Service
	cfg          Config
	log          *slog.Logger
	metrics      *observability.Metrics
	health       *observability.Health
	limiter      *Limiter
	requestSlots chan struct{}

	sessions *session.Service
	rooms    *room.Service
	// realtimeHub serves push-based streaming; streamHandler is its HTTP adapter.
	realtimeHub   *realtime.Hub
	streamHandler *realtime.Handler

	// ready reports whether migrations and content are usable.
	readyFn func(ctx context.Context) (bool, []string)

	handler http.Handler
}

// Deps are the collaborators the server needs.
type Deps struct {
	Accounts   *account.Service
	Admission  *admission.Service
	Challenges *challenge.Service
	Config     Config
	Logger     *slog.Logger
	Metrics    *observability.Metrics
	Health     *observability.Health
	Sessions   *session.Service
	Rooms      *room.Service
	// Realtime, when set, serves the stream and socket endpoints through the real
	// hub. When nil the server falls back to the polling implementation, which keeps
	// tests and single-file harnesses working without a hub.
	Realtime *realtime.Hub
	// Ready probes readiness. A nil probe reports ready, which is only correct
	// for a test harness.
	Ready func(ctx context.Context) (bool, []string)
}

// New builds the server and its route table.
func New(deps Deps) *Server {
	cfg := deps.Config.withDefaults()
	log := deps.Logger
	if log == nil {
		log = slog.Default()
	}
	metrics := deps.Metrics
	if metrics == nil {
		metrics = observability.NewMetrics()
	}
	s := &Server{
		accounts:   deps.Accounts,
		challenges: deps.Challenges,
		admission:  deps.Admission,
		cfg:        cfg,
		log:        log,
		metrics:    metrics,
		health:     deps.Health,
		sessions:   deps.Sessions,
		rooms:      deps.Rooms,
		readyFn:    deps.Ready,
	}
	s.limiter = NewLimiter(cfg.Limiter)
	s.requestSlots = make(chan struct{}, cfg.MaxConcurrentRequests)
	s.realtimeHub = deps.Realtime
	if deps.Realtime != nil {
		deps.Realtime.SetRevalidator(streamAuthorizer{srv: s})
		// A construction failure must not silently fall back to polling: the operator
		// asked for push, and a quiet downgrade would hide a misconfiguration behind
		// behaviour that still looks like it works. The hub is validated by its own
		// Config, so a failure here is a wiring bug, and routes() reports it.
		handler, err := realtime.NewHandler(deps.Realtime, streamAuthorizer{srv: s}, cfg.RealtimeConfig)
		if err != nil {
			s.log.Error("realtime handler unavailable; streaming will report an error",
				"error", err.Error())
		} else {
			s.streamHandler = handler
		}
	}
	s.handler = s.routes()
	return s
}

// Handler returns the composed handler.
func (s *Server) Handler() http.Handler { return s.handler }

// Metrics exposes the registry for tests and the /metrics handler.
func (s *Server) Metrics() *observability.Metrics { return s.metrics }

// route registers one operation.
//
// Every route declares its operationId, whether it writes, and its limiter class.
// Centralising this means a new endpoint cannot silently skip the CSRF, Origin,
// idempotency or rate-limit gates.
type route struct {
	operationID string
	method      string
	pattern     string
	write       bool
	rateClass   string
	// auth: "none" (public), "session" (cookie required), "session-or-display".
	auth string
	// idempotent requires an Idempotency-Key.
	idempotent bool
	// noCsrf exempts a write route from the CSRF check.
	//
	// It exists for exactly one case: the CSRF rotation endpoint. That endpoint
	// cannot require the token it is handing out, otherwise a reloaded page — the
	// situation it exists to fix — could never call it. Origin is still enforced,
	// and the session cookie is still required, so a cross-site request cannot use
	// it. Any other route setting this must justify it in review.
	noCsrf  bool
	handler http.HandlerFunc
}

// routes builds the mux from the route table.
func (s *Server) routes() http.Handler {
	mux := http.NewServeMux()

	for _, rt := range s.table() {
		info := routeInfo{OperationID: rt.operationID, Write: rt.write, RateClass: rt.rateClass}
		h := s.wrap(rt, info)
		mux.Handle(rt.method+" "+rt.pattern, h)
	}

	// /metrics is deliberately not part of the contract's operation set; it is an
	// operational endpoint restricted to loopback by default .
	mux.Handle("GET /metrics", http.HandlerFunc(s.handleMetrics))

	// A path that matches no route still returns a Problem, never an empty body.
	mux.Handle("/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.writeError(w, r, problem.NotFound("route-not-found", "no API operation matches this request"))
	}))

	// Middleware order matters: the request id must exist before anything logs,
	// and panic recovery must wrap the whole chain.
	var h http.Handler = mux
	h = s.withLogging(h)
	h = s.withIngress(h)
	h = s.recoverPanic(h)
	h = withRequestID(h)
	h = s.withSecurityHeaders(h)
	return h
}

// wrap applies the per-route gates in the required order.
//
// The order is normative :
//
//	identity → rate limit → body limit → contract parse → CSRF/Origin →
//	idempotency lookup → queue → transaction
//
// Body size is bounded by the decoder inside the handler, which is where the
// contract shape is known; the header-level checks come first so an unauthorised
// caller cannot make the server parse anything.
func (s *Server) wrap(rt route, info routeInfo) http.Handler {
	inner := withRoute(info, func(w http.ResponseWriter, r *http.Request) {
		if rt.operationID == "getLiveness" {
			rt.handler(w, r)
			return
		}
		release, ok := s.admitRequest(w, r)
		if !ok {
			return
		}
		defer release()
		// Identity-independent flood gates run before this route wrapper.
		if rt.auth == "none" {
			if err := s.limit(w, r, info); err != nil {
				s.writeError(w, r, err)
				return
			}
			rt.handler(w, r)
			return
		}
		// Authentication keeps the ordinary short request budget even when the
		// successful stream will outlive it. Do not carry this cancelled child
		// context into the stream itself.
		authCtx, cancelAuth := context.WithTimeout(r.Context(), s.cfg.RequestTimeout)
		sess, err := s.authenticate(w, r.WithContext(authCtx), rt.auth)
		cancelAuth()
		if err != nil {
			s.writeError(w, r, err)
			return
		}
		ctx := context.WithValue(r.Context(), ctxSession, sessionValue{
			ID: sess.Session.ID, Kind: sess.Session.Kind, DisplayName: sess.Session.DisplayName,
			ExpiresAt: sess.Session.ExpiresAt,
		})
		r = r.WithContext(ctx)

		if err := s.limit(w, r, info); err != nil {
			s.writeError(w, r, err)
			return
		}
		// Origin then CSRF: both are required for a cookie write .
		if rt.write {
			if err := s.sessions.OriginAllowed(r); err != nil {
				s.writeError(w, r, err)
				return
			}
			if !rt.noCsrf {
				if err := s.sessions.VerifyCSRF(r.Context(), sess.Session, r.Header.Get("X-CSRF-Token")); err != nil {
					s.writeError(w, r, err)
					return
				}
			}
			// The key is enforced here rather than in each handler, so a route
			// declared idempotent cannot silently skip the requirement. Operations
			// the contract exempts (session creation, single-use grant redemption,
			// invitation resolution) are simply not flagged.
			if rt.idempotent {
				if _, err := requireIdempotencyKey(r); err != nil {
					s.writeError(w, r, err)
					return
				}
			}
		}
		if !s.checkAccountAccess(w, r, rt.operationID) {
			return
		}
		if rt.write {
			switch rt.operationID {
			case "verifyAdmission", "rotateSessionCsrf", "deleteCurrentSession", "cancelMatchmakingTicket", "createSurrender":
				// Verification and departure must remain possible under adaptive risk.
			case "leaveOrRemoveMembership", "respondToMatch":
				// These handlers distinguish departure from removing or accepting others.
			default:
				if s.blockRiskAdmission(w, r) {
					return
				}
			}
		}
		if s.admission.Required(rt.operationID) && !s.admission.Allowed(r, sess.Session.ID) {
			s.writeError(w, r, problem.Forbidden("admission-required", "开始前请先完成入场确认"))
			return
		}
		if rt.operationID == "streamRoomEvents" || rt.operationID == "upgradeRoomSocket" {
			release()
		}
		rt.handler(w, r)
	})
	if rt.operationID == "streamRoomEvents" || rt.operationID == "upgradeRoomSocket" {
		// Streaming owns heartbeat/write deadlines and admission limits. A normal
		// REST deadline used to tear down every healthy subscription after 20s.
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx, cancel := context.WithTimeout(r.Context(), 2*time.Hour)
			defer cancel()
			inner.ServeHTTP(w, r.WithContext(ctx))
		})
	}
	return s.withTimeout(inner)
}

func (s *Server) blockRiskAdmission(w http.ResponseWriter, r *http.Request) bool {
	sess, ok := sessionOf(r)
	if ok && s.admission.RiskRequired(sess.ID) {
		s.writeError(w, r, problem.Forbidden("admission-risk-required", "请完成入场确认后继续"))
		return true
	}
	return false
}

// sessionValue is the request-scoped identity.
type sessionValue struct {
	ID          string
	Kind        session.Kind
	DisplayName string
	ExpiresAt   time.Time
}

// authed is the resolved session plus its token.
type authed struct {
	Session session.Session
	Token   string
}

// authenticate resolves the caller's session.
//
// A display session is accepted only for routes that allow it: the display
// cookie must never reach a gameplay write .
func (s *Server) authenticate(w http.ResponseWriter, r *http.Request, mode string) (authed, error) {
	if s.sessions == nil {
		return authed{}, problem.NotImplemented("sessions-unavailable",
			"session support is not configured on this server")
	}
	displayOnly := mode == "session-or-display" && r.URL.Query().Get("view") == "display"
	var sess session.Session
	var err error
	var token string
	if !displayOnly {
		token = s.sessions.CookieToken(r)
		sess, err = s.sessions.Lookup(r.Context(), token)
		if err == nil && sess.Kind == session.KindPlayer {
			return authed{Session: sess, Token: token}, nil
		}
	}
	if mode == "session-or-display" {
		dtoken := s.sessions.DisplayCookieToken(r)
		if dtoken != "" {
			dsess, derr := s.sessions.Lookup(r.Context(), dtoken)
			if derr == nil && dsess.Kind == session.KindDisplay {
				return authed{Session: dsess, Token: dtoken}, nil
			}
		}
	}
	if displayOnly {
		return authed{}, session.ErrNoSession
	}
	if err != nil {
		return authed{}, err
	}
	// A display session is not an interactive identity for this route.
	return authed{}, problem.Forbidden("display-session-not-allowed",
		"this operation requires an interactive session, not a display session")
}

// withSecurityHeaders sets defensive response headers.
//
// These are cheap and protect the SPA even though the API returns JSON: a
// misconfigured content type must not become script execution.
func (s *Server) withSecurityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Referrer-Policy", "same-origin")
		h.Set("X-Frame-Options", "DENY")
		next.ServeHTTP(w, r)
	})
}

// withTimeout bounds handler work.
//
// The bound must exceed the SQLite busy_timeout so a busy database produces its
// own error rather than being cut off by the HTTP timeout .
func (s *Server) withTimeout(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), s.cfg.RequestTimeout)
		defer cancel()
		// Context cancellation alone cannot interrupt a slow request body or write.
		controller := http.NewResponseController(w)
		deadline, _ := ctx.Deadline()
		_ = controller.SetReadDeadline(deadline)
		_ = controller.SetWriteDeadline(deadline.Add(5 * time.Second))
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// table is the complete route table for the 48 contract operations.
//
// Every operationId appears exactly once; the test suite asserts the set matches
// the contract so a missing or renamed operation fails the build.
func (s *Server) table() []route {
	return []route{
		{operationID: "getAccount", method: "GET", pattern: "/api/v1/account", auth: "none", rateClass: "public", handler: s.handleGetAccount},
		{operationID: "renewAccount", method: "POST", pattern: "/api/v1/account/renew", auth: "none", rateClass: "public", handler: s.handleRenewAccount},
		{operationID: "deleteAccountSession", method: "DELETE", pattern: "/api/v1/account/session", auth: "none", rateClass: "public", handler: s.handleDeleteAccount},
		{operationID: "startAccountLogin", method: "POST", pattern: "/api/v1/account/logins/{providerId}", auth: "none", rateClass: "account-login", handler: s.handleStartAccount},
		{operationID: "accountCallback", method: "GET", pattern: "/api/v1/account/callback/{providerId}", auth: "none", rateClass: "account-login", handler: s.handleAccountCallback},
		// ---- sessions ----
		{operationID: "getAdmissionStatus", method: "GET", pattern: "/api/v1/sessions/current/admission", auth: "none", rateClass: "admission-status", handler: s.handleAdmissionStatus},
		{operationID: "verifyAdmission", method: "POST", pattern: "/api/v1/sessions/current/admission", write: true, auth: "session", rateClass: "admission", handler: s.handleVerifyAdmission},
		{operationID: "createSession", method: "POST", pattern: "/api/v1/sessions",
			write: false, rateClass: "session-create", auth: "none", handler: s.handleCreateSession},
		{operationID: "getCurrentSession", method: "GET", pattern: "/api/v1/sessions/current",
			auth: "session", rateClass: "identity-recovery", handler: s.handleGetCurrentSession},
		{operationID: "updateCurrentSession", method: "PATCH", pattern: "/api/v1/sessions/current",
			write: true, rateClass: "session", auth: "session", idempotent: true, handler: s.handleUpdateCurrentSession},
		{operationID: "deleteCurrentSession", method: "DELETE", pattern: "/api/v1/sessions/current",
			write: true, rateClass: "session", auth: "session", idempotent: true, handler: s.handleDeleteCurrentSession},
		// Rotate CSRF without changing session identity. Each call issues a new token and takes no Idempotency-Key.
		{operationID: "rotateSessionCsrf", method: "POST", pattern: "/api/v1/sessions/current/csrf",
			write: true, rateClass: "identity-recovery", auth: "session", noCsrf: true, handler: s.handleRotateSessionCsrf},

		// ---- config, modes, characters ----
		{operationID: "getPublicConfig", method: "GET", pattern: "/api/v1/config",
			auth: "none", handler: s.handleGetPublicConfig},
		{operationID: "listModes", method: "GET", pattern: "/api/v1/modes",
			auth: "none", handler: s.handleListModes},
		{operationID: "getAptitudePracticePack", method: "GET", pattern: "/api/v1/practice-packs/aptitude-grid", auth: "none", rateClass: "public", handler: s.handleGetAptitudePracticePack},
		{operationID: "getRacePracticePack", method: "GET", pattern: "/api/v1/practice-packs/race-rebuild", auth: "none", rateClass: "public", handler: s.handleGetRacePracticePack},
		{operationID: "getPathPracticePack", method: "GET", pattern: "/api/v1/practice-packs/co-race-path", auth: "none", rateClass: "public", handler: s.handleGetPathPracticePack},
		{operationID: "getAudioPracticePack", method: "GET", pattern: "/api/v1/practice-packs/audio", auth: "none", rateClass: "public", handler: s.handleGetAudioPracticePack},
		{operationID: "getPedigreePracticePack", method: "GET", pattern: "/api/v1/practice-packs/pedigree", auth: "none", rateClass: "public", handler: s.handleGetPedigreePracticePack},
		{operationID: "getStrategyPracticePack", method: "GET", pattern: "/api/v1/practice-packs/strategy-race", auth: "none", rateClass: "public", handler: s.handleGetStrategyPracticePack},
		{operationID: "getDraftPracticePack", method: "GET", pattern: "/api/v1/practice-packs/draft", auth: "none", rateClass: "public", handler: s.handleGetDraftPracticePack},
		{operationID: "getMemoryPracticePack", method: "GET", pattern: "/api/v1/practice-packs/memory", auth: "none", rateClass: "public", handler: s.handleGetMemoryPracticePack},
		{operationID: "getSortPracticePack", method: "GET", pattern: "/api/v1/practice-packs/sort",
			auth: "none", rateClass: "public", handler: s.handleGetSortPracticePack},
		{operationID: "getClassicPracticePack", method: "GET", pattern: "/api/v1/practice-packs/classic",
			auth: "none", rateClass: "public", handler: s.handleGetClassicPracticePack},
		{operationID: "createChallenge", method: "POST", pattern: "/api/v1/challenges",
			write: true, auth: "session", rateClass: "challenge-create", idempotent: true, handler: s.handleCreateChallenge},
		{operationID: "getChallenge", method: "GET", pattern: "/api/v1/challenges/{challengeId}",
			auth: "session", rateClass: "session", handler: s.handleGetChallenge},
		{operationID: "guessChallenge", method: "POST", pattern: "/api/v1/challenges/{challengeId}/guesses",
			write: true, auth: "session", rateClass: "challenge-action", idempotent: true, handler: s.handleGuessChallenge},
		{operationID: "finishChallenge", method: "POST", pattern: "/api/v1/challenges/{challengeId}/finish",
			write: true, auth: "session", rateClass: "challenge-action", idempotent: true, handler: s.handleFinishChallenge},
		{operationID: "listCharacters", method: "GET", pattern: "/api/v1/characters",
			auth: "none", handler: s.handleListCharacters},
		{operationID: "getCharacter", method: "GET", pattern: "/api/v1/characters/{characterId}",
			auth: "none", handler: s.handleGetCharacter},

		// ---- rooms ----
		{operationID: "createRoom", method: "POST", pattern: "/api/v1/rooms",
			write: true, rateClass: "session", auth: "session", idempotent: true, handler: s.handleCreateRoom},
		// Paired displays may read their room through display/observe capabilities, but may not act as players.
		{operationID: "getRoom", method: "GET", pattern: "/api/v1/rooms/{roomId}",
			auth: "session-or-display", handler: s.handleGetRoom},
		{operationID: "updateRoom", method: "PATCH", pattern: "/api/v1/rooms/{roomId}",
			write: true, rateClass: "room", auth: "session", idempotent: true, handler: s.handleUpdateRoom},
		{operationID: "transferRoomHost", method: "POST", pattern: "/api/v1/rooms/{roomId}/host-transfers",
			write: true, rateClass: "room", auth: "session", idempotent: true, handler: s.handleTransferRoomHost},
		{operationID: "joinRoom", method: "POST", pattern: "/api/v1/rooms/{roomId}/memberships",
			write: true, rateClass: "room", auth: "session", idempotent: true, handler: s.handleJoinRoom},
		{operationID: "updateMembership", method: "PATCH", pattern: "/api/v1/rooms/{roomId}/memberships/{membershipId}",
			write: true, rateClass: "room", auth: "session", idempotent: true, handler: s.handleUpdateMembership},
		{operationID: "leaveOrRemoveMembership", method: "DELETE", pattern: "/api/v1/rooms/{roomId}/memberships/{membershipId}",
			write: true, rateClass: "room", auth: "session", idempotent: true, handler: s.handleLeaveOrRemoveMembership},
		{operationID: "createTeam", method: "POST", pattern: "/api/v1/rooms/{roomId}/teams",
			write: true, rateClass: "room", auth: "session", idempotent: true, handler: s.handleCreateTeam},
		{operationID: "issueControlLease", method: "POST", pattern: "/api/v1/rooms/{roomId}/control-leases",
			write: true, rateClass: "room", auth: "session", idempotent: true, handler: s.handleIssueControlLease},
		{operationID: "createRound", method: "POST", pattern: "/api/v1/rooms/{roomId}/rounds",
			write: true, rateClass: "room", auth: "session", idempotent: true, handler: s.handleCreateRound},

		// ---- rounds ----
		{operationID: "getRound", method: "GET", pattern: "/api/v1/rounds/{roundId}",
			auth: "session-or-display", handler: s.handleGetRound},
		{operationID: "createTransition", method: "POST", pattern: "/api/v1/rounds/{roundId}/transitions",
			write: true, rateClass: "room", auth: "session", idempotent: true, handler: s.handleCreateTransition},
		{operationID: "createSubmission", method: "POST", pattern: "/api/v1/rounds/{roundId}/submissions",
			write: true, rateClass: "room", auth: "session", idempotent: true, handler: s.handleCreateSubmission},
		{operationID: "createSurrender", method: "POST", pattern: "/api/v1/rounds/{roundId}/surrenders",
			write: true, rateClass: "room", auth: "session", idempotent: true, handler: s.handleCreateSurrender},
		{operationID: "createPauseRequest", method: "POST", pattern: "/api/v1/rounds/{roundId}/pause-requests",
			write: true, rateClass: "room", auth: "session", idempotent: true, handler: s.handleCreatePauseRequest},
		{operationID: "respondToPause", method: "POST", pattern: "/api/v1/rounds/{roundId}/pause-requests/{requestId}/responses",
			write: true, rateClass: "room", auth: "session", idempotent: true, handler: s.handleRespondToPause},
		{operationID: "requestRematch", method: "POST", pattern: "/api/v1/rounds/{roundId}/rematch-requests",
			write: true, rateClass: "room", auth: "session", idempotent: true, handler: s.handleRequestRematch},
		{operationID: "createTrade", method: "POST", pattern: "/api/v1/rounds/{roundId}/trades",
			write: true, rateClass: "room", auth: "session", idempotent: true, handler: s.handleCreateTrade},
		{operationID: "respondToTrade", method: "POST", pattern: "/api/v1/rounds/{roundId}/trades/{tradeId}/responses",
			write: true, rateClass: "room", auth: "session", idempotent: true, handler: s.handleRespondToTrade},
		{operationID: "getPathNeighbors", method: "GET", pattern: "/api/v1/rounds/{roundId}/path-neighbors",
			auth: "session", handler: s.handleGetPathNeighbors},

		// ---- matchmaking ----
		{operationID: "getMatchmakingLobby", method: "GET", pattern: "/api/v1/matchmaking-lobby", auth: "none", rateClass: "matchmaking-lobby", handler: s.handleMatchmakingLobby},
		{operationID: "createMatchmakingProof", method: "POST", pattern: "/api/v1/matchmaking-proofs", write: true, auth: "session", rateClass: "admission", handler: s.handleMatchmakingProof},
		{operationID: "getActiveMatchmakingTicket", method: "GET", pattern: "/api/v1/matchmaking-tickets/current", auth: "session", rateClass: "session", handler: s.handleActiveMatchmakingTicket},
		{operationID: "heartbeatMatchmakingTicket", method: "POST", pattern: "/api/v1/matchmaking-tickets/{ticketId}/heartbeat", write: true, auth: "session", rateClass: "session", handler: s.handleMatchmakingHeartbeat},
		{operationID: "createMatchmakingTicket", method: "POST", pattern: "/api/v1/matchmaking-tickets",
			write: true, rateClass: "session", auth: "session", idempotent: true, handler: s.handleCreateMatchmakingTicket},
		{operationID: "getMatchmakingTicket", method: "GET", pattern: "/api/v1/matchmaking-tickets/{ticketId}",
			auth: "session", rateClass: "session", handler: s.handleGetMatchmakingTicket},
		{operationID: "cancelMatchmakingTicket", method: "DELETE", pattern: "/api/v1/matchmaking-tickets/{ticketId}",
			write: true, rateClass: "session", auth: "session", idempotent: true, handler: s.handleCancelMatchmakingTicket},
		{operationID: "respondToMatch", method: "POST", pattern: "/api/v1/matchmaking-tickets/{ticketId}/responses",
			write: true, rateClass: "session", auth: "session", idempotent: true, handler: s.handleRespondToMatch},

		// ---- events ----
		{operationID: "createEvent", method: "POST", pattern: "/api/v1/events",
			write: true, rateClass: "session", auth: "session", idempotent: true, handler: s.handleCreateEvent},
		{operationID: "getEvent", method: "GET", pattern: "/api/v1/events/{eventId}",
			auth: "session-or-display", handler: s.handleGetEvent},
		{operationID: "createEventTransition", method: "POST", pattern: "/api/v1/events/{eventId}/transitions",
			write: true, rateClass: "session", auth: "session", idempotent: true, handler: s.handleCreateEventTransition},
		{operationID: "createSegment", method: "POST", pattern: "/api/v1/events/{eventId}/segments",
			write: true, rateClass: "session", auth: "session", idempotent: true, handler: s.handleCreateSegment},
		{operationID: "updateSegment", method: "PATCH", pattern: "/api/v1/events/{eventId}/segments/{segmentId}",
			write: true, rateClass: "session", auth: "session", idempotent: true, handler: s.handleUpdateSegment},
		{operationID: "getStandings", method: "GET", pattern: "/api/v1/events/{eventId}/standings",
			auth: "session-or-display", handler: s.handleGetStandings},
		{operationID: "createScoreCorrection", method: "POST", pattern: "/api/v1/events/{eventId}/score-corrections",
			write: true, rateClass: "session", auth: "session", idempotent: true, handler: s.handleCreateScoreCorrection},
		{operationID: "createDisplayGrant", method: "POST", pattern: "/api/v1/events/{eventId}/display-grants",
			write: true, rateClass: "session", auth: "session", idempotent: true, handler: s.handleCreateDisplayGrant},

		// ---- grants and recovery ----
		// Redeeming a pairing code WRITES (it consumes the grant and creates a
		// display session), so it is flagged write to get the Origin check. It is
		// exempt from CSRF because the display screen is a brand-new browser with no
		// session and therefore no token to present — the pairing code itself is the
		// credential. Marking it write:false would have skipped Origin too, which is
		// needlessly permissive for an endpoint that mints an identity.
		{operationID: "redeemDisplayGrant", method: "POST", pattern: "/api/v1/display-sessions",
			write: true, noCsrf: true, rateClass: "public", auth: "none", handler: s.handleRedeemDisplayGrant},
		{operationID: "createRecoveryGrant", method: "POST", pattern: "/api/v1/memberships/{membershipId}/recovery-grants",
			write: true, rateClass: "session", auth: "session", idempotent: true, handler: s.handleCreateRecoveryGrant},
		{operationID: "redeemRecoveryGrant", method: "POST", pattern: "/api/v1/session-recoveries",
			write: false, rateClass: "public", auth: "none", handler: s.handleRedeemRecoveryGrant},

		// ---- invitations ----
		{operationID: "resolveRoomInvitation", method: "POST", pattern: "/api/v1/room-invitations/resolutions",
			write: false, rateClass: "public", auth: "none", handler: s.handleResolveRoomInvitation},

		// ---- room event stream ----
		{operationID: "listRoomEvents", method: "GET", pattern: "/api/v1/rooms/{roomId}/events",
			rateClass: "session", auth: "session-or-display", handler: s.handleListRoomEvents},
		{operationID: "streamRoomEvents", method: "GET", pattern: "/api/v1/rooms/{roomId}/stream",
			rateClass: "stream", auth: "session-or-display", handler: s.handleStreamRoomEvents},
		{operationID: "upgradeRoomSocket", method: "GET", pattern: "/api/v1/rooms/{roomId}/socket",
			rateClass: "stream", auth: "session-or-display", handler: s.handleUpgradeRoomSocket},

		// ---- health ----
		{operationID: "getLiveness", method: "GET", pattern: "/health/live",
			auth: "none", handler: s.handleLiveness},
		{operationID: "getReadiness", method: "GET", pattern: "/health/ready",
			auth: "none", handler: s.handleReadiness},
	}
}

// OperationIDs returns the registered operation ids, used by the contract test.
func (s *Server) OperationIDs() []string {
	rt := s.table()
	out := make([]string, 0, len(rt))
	for _, r := range rt {
		out = append(out, r.operationID)
	}
	return out
}

// WriteOperations returns the ids that require CSRF and Origin validation.
func (s *Server) WriteOperations() []string {
	var out []string
	for _, r := range s.table() {
		if r.write {
			out = append(out, r.operationID)
		}
	}
	return out
}

// IdempotentOperations returns the ids that require an Idempotency-Key.
func (s *Server) IdempotentOperations() []string {
	var out []string
	for _, r := range s.table() {
		if r.idempotent {
			out = append(out, r.operationID)
		}
	}
	return out
}

// metricsAllowed reports whether the caller may read /metrics.
func (s *Server) metricsAllowed(r *http.Request) bool {
	if !s.cfg.MetricsLocalOnly {
		return true
	}
	return peerAddress(r).IsLoopback() && s.clientAddress(r).IsLoopback()
}
