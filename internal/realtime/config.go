package realtime

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"
)

// Tunables from the v2 specification. The numbers are the documented baselines
// , not measurements of this implementation;
// requires them to be re-tuned against a real 100 player / 100 spectator load
// test before a public event, and this package exposes every one of them through
// Config so that tuning does not require a code change.
const (
	// DefaultCoalesceWindow is the merge window for ordinary state notifications
	// in one scope. gives a target of 100–200ms.
	DefaultCoalesceWindow = 150 * time.Millisecond
	// MinCoalesceWindow and MaxCoalesceWindow bound the documented range. A
	// window outside it is a configuration error, not a preference: below 100ms
	// the merge stops absorbing guess bursts, above 200ms phase feedback starts
	// to feel late.
	MinCoalesceWindow = 100 * time.Millisecond
	MaxCoalesceWindow = 200 * time.Millisecond

	// DefaultQueueMaxMessages and DefaultQueueMaxBytes bound one connection's
	// outbound queue .
	DefaultQueueMaxMessages = 64
	DefaultQueueMaxBytes    = 256 * 1024

	// DefaultWriteTimeout bounds one socket write. A client that cannot absorb a
	// single frame in this budget is treated as slow, not as present.
	DefaultWriteTimeout = 10 * time.Second

	// DefaultHeartbeatInterval and DefaultHeartbeatTimeout implement :
	// ping every 20s, declare offline after 60s without a response.
	DefaultHeartbeatInterval = 20 * time.Second
	DefaultHeartbeatTimeout  = 60 * time.Second

	// DefaultSSEKeepAlive is the comment-frame interval for the SSE fallback
	// .
	DefaultSSEKeepAlive = 15 * time.Second

	// DefaultHistoryMaxAge and DefaultHistoryMaxCount are the replay retention
	// limits (: last 10 minutes or 10,000 notifications per room, whichever
	// is reached first).
	DefaultHistoryMaxAge   = 10 * time.Minute
	DefaultHistoryMaxCount = 10_000

	// DefaultOutboxPollInterval is how often the dispatcher looks for newly
	// committed rows.
	DefaultOutboxPollInterval = 100 * time.Millisecond
	// DefaultOutboxBatch caps one dispatcher batch so a backlog cannot starve
	// the poll loop.
	DefaultOutboxBatch = 256

	// DefaultRevokeGrace bounds how long a revocation waits for the queued
	// session.revoked frame to reach the wire before the socket is torn down. It is
	// deliberately shorter than WriteTimeout: a peer that cannot accept one small
	// frame within this window is treated as gone, and the next handshake will be
	// refused anyway because the membership is no longer active. Waiting longer
	// would let a revoked session keep reading room notifications.
	DefaultRevokeGrace = 1 * time.Second

	// DefaultPresenceDebounce coalesces connect/disconnect churn into one
	// aggregated presence.changed (: presence is an aggregate, not a
	// per-person heartbeat relay).
	DefaultPresenceDebounce = 100 * time.Millisecond

	// DefaultMaxClientMessageBytes is the read limit for client frames. The only
	// accepted client messages are ping and ack, both tiny; a larger frame is a
	// protocol violation, not a big ping.
	DefaultMaxClientMessageBytes = 4096

	// DefaultRevalidateInterval bounds periodic checks of a live subscription's
	// session, membership binding, and role.
	DefaultRevalidateInterval = 30 * time.Second

	// DefaultOutboxRetention keeps dispatched outbox rows for the same window as
	// the in-memory replay buffer so an operator can still correlate, then drops
	// them .
	DefaultOutboxRetention = 10 * time.Minute

	// DefaultSessionCookieName is the same-origin HttpOnly session cookie
	// . The value is a name only; the token itself never appears in a URL
	// and never in a log line.
	DefaultSessionCookieName = "uma_session"
)

// Config configures a Hub, a Dispatcher and the HTTP handler.
//
// Zero values are replaced by the documented defaults in withDefaults; Validate
// then rejects combinations that would violate the specification instead of
// silently clamping them.
type Config struct {
	// HandshakeTimeout bounds membership lookup before a stream is admitted.
	HandshakeTimeout time.Duration
	// Admission limits bound long-lived allocations independently of HTTP rate.
	MaxConnections           int
	MaxConnectionsPerRoom    int
	MaxConnectionsPerSession int

	// ServerEpoch identifies this process lifetime. Empty means "generate a
	// random one at startup". requires it to change on every process start
	// so a cursor from a previous process is never trusted.
	ServerEpoch string

	// CoalesceWindow is the merge window for ordinary notifications. It must be
	// inside [MinCoalesceWindow, MaxCoalesceWindow].
	CoalesceWindow time.Duration

	// QueueMaxMessages and QueueMaxBytes bound one connection's outbound queue.
	QueueMaxMessages int
	QueueMaxBytes    int

	// WriteTimeout bounds a single socket write.
	WriteTimeout time.Duration

	// HeartbeatInterval is the WebSocket ping period; HeartbeatTimeout is how
	// long a ping may stay unanswered before the peer is considered offline.
	HeartbeatInterval time.Duration
	HeartbeatTimeout  time.Duration

	// SSEKeepAlive is the SSE comment keep-alive interval.
	SSEKeepAlive time.Duration

	// HistoryMaxAge and HistoryMaxCount bound the per-room replay buffer.
	HistoryMaxAge   time.Duration
	HistoryMaxCount int

	// OutboxPollInterval and OutboxBatch tune the dispatcher.
	OutboxPollInterval time.Duration
	OutboxBatch        int
	// OutboxRetention is how long a dispatched outbox row is kept before pruning.
	OutboxRetention time.Duration

	// RevokeGrace bounds the wait for a revocation frame to be written.
	RevokeGrace time.Duration

	// PresenceDebounce coalesces presence changes.
	PresenceDebounce time.Duration

	// MaxClientMessageBytes is the WebSocket read limit for client frames.
	MaxClientMessageBytes int64

	// RevalidateInterval controls live identity checks when a Revalidator is
	// configured. Zero uses the default interval.
	RevalidateInterval time.Duration

	// SessionCookieName is the cookie holding the opaque session token.
	SessionCookieName string

	// AllowedOrigins lists additional accepted Origin values for the WebSocket
	// handshake and the SSE stream. The request's own host is always accepted
	// (same-origin SPA). Entries are either a bare host ("localhost:5173") or a
	// full "scheme://host". A wildcard is not supported on purpose.
	AllowedOrigins []string

	// RequireOrigin rejects a handshake with no Origin header at all. It should
	// stay true in production: browsers always send Origin for a WebSocket
	// handshake, so a missing header means a non-browser client. It may be
	// lowered only for an explicitly configured LAN/tooling mode, and the reason
	// belongs in the deployment notes .
	RequireOrigin bool

	// StrictMessages validates every outgoing message against the schema
	// constraints before it is enqueued. It costs a little CPU per message and is
	// meant for tests and staging, where a contract violation should fail loudly
	// instead of reaching a browser.
	StrictMessages bool
}

// withDefaults fills every zero value with the documented baseline.
func (c Config) withDefaults() Config {
	if c.HandshakeTimeout <= 0 {
		c.HandshakeTimeout = 10 * time.Second
	}
	if c.RevalidateInterval <= 0 {
		c.RevalidateInterval = DefaultRevalidateInterval
	}
	if c.CoalesceWindow <= 0 {
		c.CoalesceWindow = DefaultCoalesceWindow
	}
	if c.QueueMaxMessages <= 0 {
		c.QueueMaxMessages = DefaultQueueMaxMessages
	}
	if c.QueueMaxBytes <= 0 {
		c.QueueMaxBytes = DefaultQueueMaxBytes
	}
	if c.WriteTimeout <= 0 {
		c.WriteTimeout = DefaultWriteTimeout
	}
	if c.HeartbeatInterval <= 0 {
		c.HeartbeatInterval = DefaultHeartbeatInterval
	}
	if c.HeartbeatTimeout <= 0 {
		c.HeartbeatTimeout = DefaultHeartbeatTimeout
	}
	if c.SSEKeepAlive <= 0 {
		c.SSEKeepAlive = DefaultSSEKeepAlive
	}
	if c.HistoryMaxAge <= 0 {
		c.HistoryMaxAge = DefaultHistoryMaxAge
	}
	if c.HistoryMaxCount <= 0 {
		c.HistoryMaxCount = DefaultHistoryMaxCount
	}
	if c.OutboxPollInterval <= 0 {
		c.OutboxPollInterval = DefaultOutboxPollInterval
	}
	if c.OutboxBatch <= 0 {
		c.OutboxBatch = DefaultOutboxBatch
	}
	if c.OutboxRetention <= 0 {
		c.OutboxRetention = DefaultOutboxRetention
	}
	if c.RevokeGrace <= 0 {
		c.RevokeGrace = DefaultRevokeGrace
	}
	if c.PresenceDebounce <= 0 {
		c.PresenceDebounce = DefaultPresenceDebounce
	}
	if c.MaxClientMessageBytes <= 0 {
		c.MaxClientMessageBytes = DefaultMaxClientMessageBytes
	}
	if c.MaxConnections <= 0 {
		c.MaxConnections = 2000
	}
	if c.MaxConnectionsPerRoom <= 0 {
		c.MaxConnectionsPerRoom = 200
	}
	if c.MaxConnectionsPerSession <= 0 {
		c.MaxConnectionsPerSession = 2
	}
	if c.SessionCookieName == "" {
		c.SessionCookieName = DefaultSessionCookieName
	}
	return c
}

// Validate rejects a configuration that cannot satisfy the specification.
func (c Config) Validate() error {
	switch {
	case c.CoalesceWindow < MinCoalesceWindow || c.CoalesceWindow > MaxCoalesceWindow:
		return fmt.Errorf("realtime: CoalesceWindow %s outside the specified %s–%s range",
			c.CoalesceWindow, MinCoalesceWindow, MaxCoalesceWindow)
	case c.QueueMaxMessages < 1:
		return errors.New("realtime: QueueMaxMessages must be positive")
	case c.QueueMaxBytes < 1024:
		return errors.New("realtime: QueueMaxBytes must be at least 1024")
	case c.WriteTimeout <= 0:
		return errors.New("realtime: WriteTimeout must be positive")
	case c.HeartbeatTimeout <= c.HeartbeatInterval:
		return fmt.Errorf("realtime: HeartbeatTimeout %s must exceed HeartbeatInterval %s",
			c.HeartbeatTimeout, c.HeartbeatInterval)
	case c.HistoryMaxAge <= 0:
		return errors.New("realtime: HistoryMaxAge must be positive")
	case c.HistoryMaxCount < 1:
		return errors.New("realtime: HistoryMaxCount must be positive")
	case c.OutboxBatch < 1:
		return errors.New("realtime: OutboxBatch must be positive")
	case c.RevokeGrace <= 0:
		return errors.New("realtime: RevokeGrace must be positive")
	case c.MaxClientMessageBytes < 64:
		return errors.New("realtime: MaxClientMessageBytes is too small for a ping")
	}
	for _, o := range c.AllowedOrigins {
		if strings.ContainsAny(o, "*?") {
			return errors.New("realtime: AllowedOrigins must not contain a wildcard")
		}
	}
	return nil
}

// newEpoch returns a fresh random epoch identifier (32 hex characters).
func newEpoch() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		// crypto/rand failing at startup is not recoverable in a useful way; a
		// panic here is preferable to silently reusing a fixed epoch, which would
		// let a cursor from a previous process lifetime be accepted.
		panic("realtime: crypto/rand unavailable: " + err.Error())
	}
	return hex.EncodeToString(b[:])
}

// Metrics receives counters and gauges. The default is a no-op so the package is
// usable without an observability stack; internal/observability plugs in through
// Hub.SetMetrics.
type Metrics interface {
	Counter(name string, delta int64)
	Gauge(name string, value int64)
}

// Metric names. They follow the outbox lag and queue pressure items asks
// operators to record.
const (
	MetricPublished         = "realtime.outbox.published"
	MetricDispatchErrors    = "realtime.outbox.dispatch_errors"
	MetricDispatchLagMillis = "realtime.outbox.lag_ms"
	MetricCoalesced         = "realtime.notify.coalesced"
	MetricQueueOverflow     = "realtime.conn.overflow"
	MetricQueueDropped      = "realtime.conn.dropped"
	MetricReplayHit         = "realtime.replay.hit"
	MetricReplayMiss        = "realtime.replay.miss"
	MetricResyncSent        = "realtime.resync.sent"
	MetricRevoked           = "realtime.session.revoked"
	MetricOnline            = "realtime.presence.online"
	MetricSubscriptions     = "realtime.presence.subscriptions"
)

type noopMetrics struct{}

func (noopMetrics) Counter(string, int64) {}
func (noopMetrics) Gauge(string, int64)   {}
