package realtime

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"
)

// Close codes used when this package terminates a connection.
//
// 1008 (policy violation) is the RFC 6455 code for "you broke the protocol", used
// when a client sends a frame that is not ping/ack. 4001 is in the
// application-private range (3000–4999) and means "your authorisation is gone".
const (
	ClosePolicyViolation = 1008
	CloseSessionRevoked  = 4001
	CloseServerError     = 1011
	CloseGoingAway       = 1001
	CloseTryAgainLater   = 1013
)

// Resync reasons. They are short, stable tokens because a client may switch on
// them to decide between "refetch silently" and "show a reconnect banner".
const (
	ResyncCursorExpired  = "cursor-expired"
	ResyncCursorScope    = "cursor-scope-mismatch"
	ResyncEpochChanged   = "server-epoch-changed"
	ResyncSlowConsumer   = "slow-consumer"
	ResyncBacklog        = "backlog-too-large"
	ResyncRoomVersionLag = "room-version-behind"
)

// Identity is the server-resolved caller identity. It is never derived from a
// request body or a query parameter; the HTTP layer resolves it from the session
// cookie and the membership table and passes the result in.
type Identity struct {
	// RoomID is the room being subscribed to.
	RoomID string
	// MembershipID identifies this caller's membership row.
	MembershipID string
	// SessionID identifies the anonymous session, used for revocation.
	SessionID string
	// Role is the membership role: player, team-captain, spectator, host, display.
	Role string
}

// Validate rejects an incomplete identity before any state is created.
func (i Identity) Validate() error {
	switch {
	case i.RoomID == "":
		return errors.New("realtime: identity has no roomId")
	case i.MembershipID == "":
		return errors.New("realtime: identity has no membershipId")
	case i.SessionID == "":
		return errors.New("realtime: identity has no sessionId")
	case i.Role == "":
		return errors.New("realtime: identity has no role")
	}
	return nil
}

// SubscribeRequest is what the HTTP layer hands to ServeWebSocket/ServeSSE after
// it has authorised the caller.
type SubscribeRequest struct {
	Identity Identity
	// Cursor resumes a previous position. Empty starts a fresh subscription.
	Cursor string
	// RoomVersion is the room version the client last saw. When it is behind the
	// server's current version, the caller may ask for a resync (see
	// Hub.SubscribeOptions.RoomVersion). Zero means "unknown, do not check".
	RoomVersion int64
}

// Revalidator optionally re-checks a live subscription's authorisation.
// requires every subscription to re-authorise rather than inherit a role
// forever, so when a Revalidator is configured the hub calls it periodically and
// on a positive "revoked" answer sends session.revoked and closes.
//
// Returning an error means "could not tell right now" (database busy, context
// cancelled) and must not disconnect anyone. Only a nil error with active=false
// revokes.
type Revalidator interface {
	MembershipActive(ctx context.Context, roomID, membershipID string) (active bool, err error)
}

// RoomVersions optionally supplies current versions for a hello. Without it the
// hub reports the last versions it saw published, which is correct for a client
// that is about to re-fetch anyway, but slightly stale for a brand new room.
type RoomVersions interface {
	CurrentVersions(ctx context.Context, roomID string) (roomVersion, phaseVersion int64, err error)
}

// Hub owns every subscription in the process.
//
// One Hub per process. It is safe for concurrent use. It never blocks on a socket:
// publishers append to per-connection bounded queues and return, so a stalled
// browser cannot slow down the dispatcher or another room.
type Hub struct {
	cfg     Config
	epoch   string
	metrics Metrics
	now     func() time.Time

	// revalidator and versions are optional integrations with the persistence
	// layer; nil means "not configured".
	revalidator Revalidator
	versions    RoomVersions

	mu      sync.RWMutex
	rooms   map[string]*roomState
	byID    map[string]*subscriber
	byMem   map[string]map[string]*subscriber
	bySess  map[string]map[string]*subscriber
	byRoom  map[string]map[string]*subscriber
	nextID  atomic.Uint64
	closed  bool
	closeCh chan struct{}

	wg sync.WaitGroup
}

// roomState is the per-room bookkeeping the hub needs.
type roomState struct {
	id   string
	hist *history

	mu           sync.Mutex
	lastSeq      int64
	roomVersion  int64
	phaseVersion int64
	online       int
	// presenceTimer debounces the aggregated presence.changed.
	presenceTimer *time.Timer
	// lastActive keeps an idle room around long enough to serve a replay.
	lastActive time.Time
}

// NewHub builds a hub. A zero Config is valid and uses the documented defaults.
func NewHub(cfg Config) (*Hub, error) {
	cfg = cfg.withDefaults()
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	epoch := cfg.ServerEpoch
	if epoch == "" {
		epoch = newEpoch()
	}
	h := &Hub{
		cfg:     cfg,
		epoch:   epoch,
		metrics: noopMetrics{},
		now:     time.Now,
		rooms:   make(map[string]*roomState),
		byID:    make(map[string]*subscriber),
		byMem:   make(map[string]map[string]*subscriber),
		bySess:  make(map[string]map[string]*subscriber),
		byRoom:  make(map[string]map[string]*subscriber),
		closeCh: make(chan struct{}),
	}
	h.wg.Add(1)
	go h.janitor()
	return h, nil
}

// SetMetrics installs a metrics sink.
func (h *Hub) SetMetrics(m Metrics) {
	if m == nil {
		m = noopMetrics{}
	}
	h.metrics = m
}

// SetRevalidator installs the optional authorisation re-check.
func (h *Hub) SetRevalidator(r Revalidator) { h.revalidator = r }

// SetRoomVersions installs the optional version source for hello.
func (h *Hub) SetRoomVersions(v RoomVersions) { h.versions = v }

// Epoch is this process's server epoch. It appears in every message and every
// cursor, so a cursor from a previous process is rejected .
func (h *Hub) Epoch() string { return h.epoch }

// Config returns the effective configuration, with defaults applied.
func (h *Hub) Config() Config { return h.cfg }

// room returns (creating if needed) the state for one room.
func (h *Hub) room(roomID string) *roomState {
	h.mu.Lock()
	defer h.mu.Unlock()
	rs, ok := h.rooms[roomID]
	if !ok {
		rs = &roomState{
			id:   roomID,
			hist: newHistory(h.cfg.HistoryMaxAge, h.cfg.HistoryMaxCount),
		}
		h.rooms[roomID] = rs
	}
	// lastActive is guarded by rs.mu, not h.mu, because the janitor reads it while
	// holding only the room lock.
	rs.mu.Lock()
	rs.lastActive = h.now()
	rs.mu.Unlock()
	return rs
}

// existingRoom returns the state for one room without creating it.
func (h *Hub) existingRoom(roomID string) (*roomState, bool) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	rs, ok := h.rooms[roomID]
	return rs, ok
}

// Close shuts the hub down and terminates every subscription.
func (h *Hub) Close() error {
	h.mu.Lock()
	if h.closed {
		h.mu.Unlock()
		return nil
	}
	h.closed = true
	close(h.closeCh)
	subs := make([]*subscriber, 0, len(h.byID))
	for _, s := range h.byID {
		subs = append(subs, s)
	}
	h.mu.Unlock()

	for _, s := range subs {
		s.terminate(CloseGoingAway, "server shutting down")
	}
	h.wg.Wait()
	return nil
}

// janitor prunes idle rooms so a long-lived process does not retain history for
// every room ever opened.
func (h *Hub) janitor() {
	defer h.wg.Done()
	interval := h.cfg.HistoryMaxAge / 4
	if interval < time.Second {
		interval = time.Second
	}
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-h.closeCh:
			return
		case <-t.C:
			cutoff := h.now().Add(-h.cfg.HistoryMaxAge)
			h.mu.Lock()
			for id, rs := range h.rooms {
				rs.mu.Lock()
				idle := rs.online == 0 && rs.lastActive.Before(cutoff)
				rs.mu.Unlock()
				if idle {
					delete(h.rooms, id)
				}
			}
			h.mu.Unlock()
		}
	}
}

// publication is one notification ready to be fanned out.
//
// The template has no cursor: cursor is per-recipient because it is scope-bound.
// Keeping cursor out of the shared template is what makes fan-out cheap and
// replay possible without re-deriving the message.
type publication struct {
	tmpl     ServerMessage
	seq      int64
	audience Audience
}

// publish fans one notification out to every matching subscriber in its room.
//
// Publishing never blocks on a socket. Each subscriber's queue applies its own
// merge and backpressure policy, so one slow peer degrades only itself.
func (h *Hub) publish(p publication) {
	rs, ok := h.existingRoom(p.tmpl.RoomID)
	if !ok {
		// No room state means nobody ever subscribed here; nothing to do.
		return
	}
	h.noteVersions(rs, p.tmpl)

	// Fan-out is per room, not per process. Scanning every subscriber would make a
	// guess in one room cost time proportional to every connection in the process,
	// which is exactly the broadcast amplification asks to avoid.
	h.mu.RLock()
	subs := make([]*subscriber, 0, 8)
	for _, s := range h.byRoom[p.tmpl.RoomID] {
		subs = append(subs, s)
	}
	h.mu.RUnlock()

	for _, s := range subs {
		if !p.audience.allows(s.role, s.membershipID) {
			// Filtered out privately. This is the legitimate source of sequence
			// gaps that warns clients about, and it is recorded in history so
			// a later replay applies the same filter.
			continue
		}
		s.deliverTemplate(p.tmpl, p.seq)
	}
}

// noteVersions records the versions a published message carries.
func (h *Hub) noteVersions(rs *roomState, m ServerMessage) {
	rs.mu.Lock()
	defer rs.mu.Unlock()
	if m.RoomVersion > rs.roomVersion {
		rs.roomVersion = m.RoomVersion
	}
	if m.PhaseVer != nil && *m.PhaseVer > rs.phaseVersion {
		rs.phaseVersion = *m.PhaseVer
	}
}

// recordHistory appends one published notification to the room's replay buffer.
func (h *Hub) recordHistory(rs *roomState, p publication) {
	now := h.now()
	rs.mu.Lock()
	if p.seq > rs.lastSeq {
		rs.lastSeq = p.seq
	}
	rs.mu.Unlock()

	rs.hist.append(historyEntry{
		seq:      p.seq,
		at:       now,
		tmpl:     p.tmpl,
		audience: p.audience,
	}, now)
}

// Publish is the entry point the dispatcher uses for one committed outbox event.
//
// It is exported so that an in-process publisher (tests, or a component that has
// already committed and does not want to round-trip through the outbox) can reuse
// the same fan-out path. Everything that matters for correctness — ordering,
// history, cursor minting, audience filtering — is identical to the dispatcher
// path.
func (h *Hub) Publish(p Publication) error {
	if p.RoomID == "" {
		return errors.New("realtime: publication has no roomId")
	}
	switch p.Type {
	case TypeStateChanged, TypePhaseChanged, TypePresenceChange:
	default:
		return fmt.Errorf("realtime: connection message %q cannot be published to a room", p.Type)
	}
	tmpl := ServerMessage{
		SchemaVersion: SchemaVersion,
		EventID:       p.EventID,
		Type:          p.Type,
		RoomID:        p.RoomID,
		RoomVersion:   p.RoomVersion,
		ServerEpoch:   h.epoch,
		ServerTime:    h.now().UTC().Format(time.RFC3339Nano),
		Resource:      p.Resource,
		RoundID:       p.RoundID,
		Reason:        p.Reason,
	}
	if p.PhaseVersion != 0 {
		v := p.PhaseVersion
		tmpl.PhaseVer = &v
	}
	if p.OnlineCount != nil {
		v := *p.OnlineCount
		tmpl.Online = &v
	}
	if err := tmpl.Validate(); err != nil {
		return err
	}

	rs := h.room(p.RoomID)
	pub := publication{tmpl: tmpl, seq: p.Seq, audience: p.Audience}
	h.recordHistory(rs, pub)
	h.publish(pub)
	return nil
}

// Publication is the exported description of one notification.
type Publication struct {
	EventID      string
	Type         MessageType
	RoomID       string
	RoomVersion  int64
	PhaseVersion int64
	Seq          int64
	Resource     Resource
	RoundID      string
	Reason       string
	Audience     Audience
	OnlineCount  *int64
}

// subscriber is one live connection.
type subscriber struct {
	hub          *Hub
	id           string
	roomID       string
	membershipID string
	sessionID    string
	role         string
	channel      Channel
	tr           transport
	out          *outbound

	// lastSeq is the last storage sequence this peer has been told about. It is
	// the position its next cursor will encode. It is written by the publishing
	// goroutine and read by the reader/heartbeat goroutines, so it is atomic.
	lastSeq atomic.Int64

	// stop is closed exactly once when the connection starts terminating. It
	// wakes every loop (writer, reader, heartbeat, revalidation) and the Serve
	// select. It is deliberately separate from "the transport is closed": stop
	// means "no new work", the transport close means "the socket is gone".
	stop     chan struct{}
	stopOnce sync.Once
	// termReason records why the connection ended, for logs and tests.
	termReason atomic.Value
	closeCode  atomic.Int32

	// writeMu serialises close so that a close frame is not interleaved with a
	// data frame from the writer goroutine.
	closeOnce sync.Once
}

// newSubscriberID returns a process-unique connection id.
func (h *Hub) newSubscriberID() string {
	return fmt.Sprintf("sub-%d", h.nextID.Add(1))
}

// SubscribeOptions controls one subscription.
type SubscribeOptions struct {
	Identity Identity
	Channel  Channel
	// Cursor resumes; empty starts fresh.
	Cursor string
	// RoomVersion is the client's last seen room version, or 0 for unknown.
	RoomVersion int64
	// Transport is the already-upgraded connection.
	Transport transport
}

// Serve runs one subscription until the connection ends.
//
// Ordering on connect is deliberate:
//
// 1. hello (critical) — carries epoch, time, versions, cursor and heartbeat.
// 2. replay or resync.required — only after hello, so the client always has the
// epoch before it is told its cursor is unusable.
//
// The function returns when the peer disconnects, the hub closes, or the peer
// misbehaves. It never returns while a write is in flight on another goroutine.
func (h *Hub) Serve(ctx context.Context, opts SubscribeOptions) error {
	if err := opts.Identity.Validate(); err != nil {
		return err
	}
	if opts.Transport == nil {
		return errors.New("realtime: no transport")
	}
	h.mu.RLock()
	closed := h.closed
	h.mu.RUnlock()
	if closed {
		return errors.New("realtime: hub is closed")
	}

	s := &subscriber{
		hub:          h,
		id:           h.newSubscriberID(),
		roomID:       opts.Identity.RoomID,
		membershipID: opts.Identity.MembershipID,
		sessionID:    opts.Identity.SessionID,
		role:         opts.Identity.Role,
		channel:      opts.Transport.Channel(),
		tr:           opts.Transport,
		out:          newOutbound(h.cfg.CoalesceWindow, h.cfg.QueueMaxMessages, h.cfg.QueueMaxBytes),
		stop:         make(chan struct{}),
	}

	h.register(s)

	// Resolve the resume position *before* hello, because hello carries the cursor
	// and the cursor must describe where this subscriber actually is.
	//
	// Getting this order wrong is a real bug: if hello advertised the room's newest
	// sequence and the replay then started from an older one, the client's stored
	// cursor would jump forward and it would permanently skip the events in between
	// on its next reconnect. The position is therefore derived first, hello reports
	// it, and only then are the retained entries delivered.
	rs := h.room(s.roomID)
	roomVersion, phaseVersion := h.currentVersions(ctx, rs)
	startSeq, resumePlan := h.planResume(s, opts.Cursor, opts.RoomVersion)

	s.lastSeq.Store(startSeq)
	s.sendHello(roomVersion, phaseVersion)
	h.applyResume(s, resumePlan)

	// Writer runs on this goroutine; reader and heartbeat run alongside.
	writerDone := make(chan struct{})
	go func() {
		defer close(writerDone)
		h.writeLoop(s)
	}()
	readDone := make(chan struct{})
	go func() {
		defer close(readDone)
		h.readLoop(s)
	}()
	beatDone := make(chan struct{})
	go func() {
		defer close(beatDone)
		h.heartbeatLoop(s)
	}()
	revalidateDone := make(chan struct{})
	if h.cfg.RevalidateInterval > 0 && h.revalidator != nil {
		// This goroutine is tracked by revalidateDone rather than h.wg: adding to
		// h.wg here would race with Hub.Close's Wait, because Close can run between
		// the Add and the Wait.
		go func() {
			defer close(revalidateDone)
			h.revalidateLoop(s)
		}()
	} else {
		close(revalidateDone)
	}

	select {
	case <-s.stop:
	case <-ctx.Done():
		s.terminate(CloseGoingAway, "request context done")
	case <-h.closeCh:
		s.terminate(CloseGoingAway, "hub closed")
	}

	// stop is already closed by terminate (or is about to be), so the writer's pop
	// returns false. Close the queue and wait for every goroutine to stop touching
	// the transport before Serve returns.
	s.out.close()
	<-writerDone
	<-readDone
	<-beatDone
	<-revalidateDone

	h.unregister(s)
	return s.err()
}

// err converts the termination reason into an error for the HTTP layer.
func (s *subscriber) err() error {
	if v, ok := s.termReason.Load().(string); ok && v != "" {
		return fmt.Errorf("realtime: subscription %s ended: %s", s.id, v)
	}
	return nil
}

// register adds the subscriber to every index and bumps the online count.
func (h *Hub) register(s *subscriber) {
	h.mu.Lock()
	h.byID[s.id] = s
	addIndex(h.byMem, s.membershipID, s)
	addIndex(h.bySess, s.sessionID, s)
	addIndex(h.byRoom, s.roomID, s)
	h.mu.Unlock()

	rs := h.room(s.roomID)
	rs.mu.Lock()
	rs.online++
	online := rs.online
	rs.mu.Unlock()

	h.metrics.Gauge(MetricOnline, int64(online))
	h.metrics.Counter(MetricSubscriptions, 1)
	h.schedulePresence(rs, online)
}

// unregister removes the subscriber and schedules the aggregated presence update.
func (h *Hub) unregister(s *subscriber) {
	h.mu.Lock()
	delete(h.byID, s.id)
	removeIndex(h.byMem, s.membershipID, s)
	removeIndex(h.bySess, s.sessionID, s)
	removeIndex(h.byRoom, s.roomID, s)
	h.mu.Unlock()

	rs, ok := h.existingRoom(s.roomID)
	if !ok {
		return
	}
	rs.mu.Lock()
	if rs.online > 0 {
		rs.online--
	}
	online := rs.online
	rs.mu.Unlock()

	h.metrics.Gauge(MetricOnline, int64(online))
	h.metrics.Counter(MetricSubscriptions, -1)
	h.schedulePresence(rs, online)
}

func addIndex(m map[string]map[string]*subscriber, key string, s *subscriber) {
	set, ok := m[key]
	if !ok {
		set = make(map[string]*subscriber, 2)
		m[key] = set
	}
	set[s.id] = s
}

func removeIndex(m map[string]map[string]*subscriber, key string, s *subscriber) {
	set, ok := m[key]
	if !ok {
		return
	}
	delete(set, s.id)
	if len(set) == 0 {
		delete(m, key)
	}
}

// currentVersions asks the optional RoomVersions source, falling back to what the
// hub last saw published.
func (h *Hub) currentVersions(ctx context.Context, rs *roomState) (roomVersion, phaseVersion int64) {
	if h.versions != nil {
		// A failing lookup must not prevent the connection: the client will fetch a
		// snapshot immediately after hello anyway, and a wrong version there is
		// corrected by the version in that response.
		if rv, pv, err := h.versions.CurrentVersions(ctx, rs.id); err == nil {
			rs.mu.Lock()
			if rv > rs.roomVersion {
				rs.roomVersion = rv
			}
			if pv > rs.phaseVersion {
				rs.phaseVersion = pv
			}
			rs.mu.Unlock()
		}
	}
	rs.mu.Lock()
	defer rs.mu.Unlock()
	return rs.roomVersion, rs.phaseVersion
}

// lastSeqOf reads the room's current sequence.
func (h *Hub) lastSeqOf(rs *roomState) int64 {
	rs.mu.Lock()
	defer rs.mu.Unlock()
	return rs.lastSeq
}

// sendHello emits the mandatory first message.
func (s *subscriber) sendHello(roomVersion, phaseVersion int64) {
	beat := hbMillis(s.hub.cfg.HeartbeatInterval)
	m := ServerMessage{
		SchemaVersion: SchemaVersion,
		EventID:       "hello-" + s.id,
		Type:          TypeHello,
		RoomID:        s.roomID,
		RoomVersion:   roomVersion,
		ServerEpoch:   s.hub.epoch,
		ServerTime:    s.hub.now().UTC().Format(time.RFC3339Nano),
		PhaseVer:      &phaseVersion,
		HeartbeatM:    &beat,
	}
	s.deliverWithCursor(m, s.lastSeq.Load())
}

func hbMillis(d time.Duration) int64 {
	ms := d.Milliseconds()
	if ms < 1000 {
		// The schema minimum is 1000. A smaller configured interval is clamped rather
		// than allowed to produce an invalid frame.
		ms = 1000
	}
	return ms
}

// resumePlan is a decided resume outcome. Deciding and acting are separate so that
// hello can be sent with the correct cursor before any replayed entry is queued.
type resumePlan struct {
	// resyncReason, when non-empty, means continuity could not be proven and the
	// client must rebuild from a snapshot.
	resyncReason string
	// entries are the retained notifications to deliver, oldest first.
	entries []historyEntry
}

// planResume decides how a reconnect is served.
//
// It is pure with respect to the connection: it reads the history and returns a
// plan, without touching the outbound queue.
func (h *Hub) planResume(s *subscriber, token string, clientRoomVersion int64) (int64, resumePlan) {
	rs := h.room(s.roomID)
	current := h.lastSeqOf(rs)

	if token == "" {
		// Fresh subscription with no position. The hello cursor is the current
		// position, so the client starts from "now" and re-fetches a snapshot, which
		// is exactly what a first connect does.
		return current, resumePlan{}
	}

	cur, err := parseCursor(token, h.epoch, s.roomID, s.membershipID)
	if err != nil {
		// Either a restart (epoch changed) or a forged/garbled token. Both mean the
		// client's position cannot be trusted, so it starts from "now" and is told to
		// rebuild.
		h.metrics.Counter(MetricReplayMiss, 1)
		return current, resumePlan{resyncReason: ResyncCursorExpired}
	}

	rs.mu.Lock()
	roomVersion := rs.roomVersion
	rs.mu.Unlock()
	if clientRoomVersion > 0 && clientRoomVersion < roomVersion {
		// The client is behind on room state. Replaying notifications would leave it
		// with a correct cursor but a stale snapshot, so a rebuild is required.
		h.metrics.Counter(MetricReplayMiss, 1)
		return current, resumePlan{resyncReason: ResyncRoomVersionLag}
	}

	// Leave headroom in the queue: hello already occupies a slot and a replay must
	// not fill the queue so completely that the first post-reconnect notification
	// overflows. A longer backlog is better served by a snapshot rebuild.
	maxReplay := h.cfg.QueueMaxMessages / 2
	if maxReplay < 1 {
		maxReplay = 1
	}
	res := rs.hist.replay(cur.seqOf(), maxReplay, func(e historyEntry) bool {
		return e.audience.allows(s.role, s.membershipID)
	})
	if res.gap {
		h.metrics.Counter(MetricReplayMiss, 1)
		return current, resumePlan{resyncReason: ResyncBacklog}
	}
	h.metrics.Counter(MetricReplayHit, 1)
	return cur.seqOf(), resumePlan{entries: res.entries}
}

// applyResume delivers a decided plan.
func (h *Hub) applyResume(s *subscriber, plan resumePlan) {
	if plan.resyncReason != "" {
		s.sendResync(plan.resyncReason)
		return
	}
	for _, e := range plan.entries {
		s.deliverTemplate(e.tmpl, e.seq)
	}
}

// deliverTemplate mints this subscriber's cursor and queues the message.
func (s *subscriber) deliverTemplate(tmpl ServerMessage, seq int64) {
	// advance is monotonic: a replayed older entry must never move the position
	// backwards, or the next cursor would ask for events already delivered.
	for {
		cur := s.lastSeq.Load()
		if seq <= cur {
			break
		}
		if s.lastSeq.CompareAndSwap(cur, seq) {
			break
		}
	}
	s.deliverWithCursor(tmpl, seq)
}

// deliverWithCursor finalises the cursor and applies the queue policy.
func (s *subscriber) deliverWithCursor(tmpl ServerMessage, seq int64) {
	m := tmpl
	m.Cursor = mintCursor(s.hub.epoch, s.roomID, s.membershipID, seq)
	if s.hub.cfg.StrictMessages {
		if err := m.Validate(); err != nil {
			// A contract violation is our bug, not the peer's. Dropping the frame is
			// better than sending an invalid one, and the counter makes it visible.
			s.hub.metrics.Counter(MetricQueueDropped, 1)
			return
		}
	}
	frame, err := json.Marshal(m)
	if err != nil {
		s.hub.metrics.Counter(MetricQueueDropped, 1)
		return
	}
	s.apply(m, frame)
}

// apply enqueues one encoded frame and handles the escalation the queue asks for.
func (s *subscriber) apply(m ServerMessage, frame []byte) {
	switch s.out.enqueue(m, frame) {
	case enqueueOK:
		return
	case enqueueNeedResync:
		// The peer is too far behind for ordinary notifications. Tell it to rebuild;
		// this is not silent loss because the client is explicitly instructed.
		s.hub.metrics.Counter(MetricQueueOverflow, 1)
		s.sendResyncNow(ResyncSlowConsumer)
	case enqueueNeedClose:
		s.hub.metrics.Counter(MetricQueueOverflow, 1)
		s.terminate(CloseTryAgainLater, "outbound queue overflow")
	}
}

// sendResync queues a resync.required notification (the non-escalating form used
// during resume, where the queue is known to be empty).
func (s *subscriber) sendResync(reason string) {
	s.hub.metrics.Counter(MetricResyncSent, 1)
	s.deliverWithCursor(ServerMessage{
		SchemaVersion: SchemaVersion,
		EventID:       "resync-" + s.id + "-" + reason,
		Type:          TypeResyncRequired,
		RoomID:        s.roomID,
		RoomVersion:   s.roomVersionNow(),
		ServerEpoch:   s.hub.epoch,
		ServerTime:    s.hub.now().UTC().Format(time.RFC3339Nano),
		Reason:        reason,
	}, s.lastSeq.Load())
}

// sendResyncNow is sendResync for the escalation path. It must not recurse into
// apply() through deliverWithCursor's normal queue policy, because the queue is
// exactly the thing that is full.
func (s *subscriber) sendResyncNow(reason string) {
	s.hub.metrics.Counter(MetricResyncSent, 1)
	m := ServerMessage{
		SchemaVersion: SchemaVersion,
		EventID:       "resync-" + s.id + "-" + reason + "-" + fmt.Sprint(s.hub.now().UnixNano()),
		Type:          TypeResyncRequired,
		RoomID:        s.roomID,
		RoomVersion:   s.roomVersionNow(),
		ServerEpoch:   s.hub.epoch,
		ServerTime:    s.hub.now().UTC().Format(time.RFC3339Nano),
		Reason:        reason,
		Cursor:        mintCursor(s.hub.epoch, s.roomID, s.membershipID, s.lastSeq.Load()),
	}
	frame, err := json.Marshal(m)
	if err != nil {
		return
	}
	// Bypass the substitutable backlog entirely: drop every windowed frame, then
	// place this critical one. If even that does not fit, the connection dies and
	// the client rebuilds on reconnect.
	s.out.evictSubstitutable()
	if s.out.enqueue(m, frame) == enqueueNeedClose {
		s.terminate(CloseTryAgainLater, "cannot deliver resync.required")
	}
}

// roomVersionNow reads the room's current version.
func (s *subscriber) roomVersionNow() int64 {
	rs, ok := s.hub.existingRoom(s.roomID)
	if !ok {
		return 0
	}
	rs.mu.Lock()
	defer rs.mu.Unlock()
	return rs.roomVersion
}

// terminate ends the subscription. It is idempotent.
//
// The close is attempted on the writer goroutine's behalf: terminate only records
// the reason, closes stop, and closes the transport. Serve then waits for the
// goroutines to exit before returning, so the transport is never used after
// Close.
func (s *subscriber) terminate(code int, reason string) {
	s.stopOnce.Do(func() {
		s.termReason.Store(reason)
		s.closeCode.Store(int32(code))
		// Signal every loop first: stop means "no new work", and closing it before
		// the transport keeps the writer from starting a frame it cannot finish.
		close(s.stop)
		// Closing the transport unblocks a read that is already in progress, which
		// is the only reliable way to interrupt it.
		s.closeOnce.Do(func() {
			_ = s.tr.Close(code, reason)
		})
	})
}

// writeLoop pumps the outbound queue onto the transport.
func (h *Hub) writeLoop(s *subscriber) {
	for {
		q, ok := s.out.pop(s.stop)
		if !ok {
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), h.cfg.WriteTimeout)
		err := s.tr.WriteFrame(ctx, q.msg, q.frame)
		cancel()
		s.out.finishWrite()
		if err != nil {
			s.terminate(CloseServerError, "write failed: "+err.Error())
			return
		}
	}
}

// readLoop consumes client frames. Only ping and ack are accepted.
//
// A business-looking frame is a protocol violation: the connection is closed with
// 1008 and the reason names the rule, so a developer integrating against the wrong
// plane gets a clear signal instead of a silent no-op.
func (h *Hub) readLoop(s *subscriber) {
	if s.channel != ChannelWS {
		// SSE is server-to-client only.
		return
	}
	for {
		raw, err := s.tr.ReadFrame(context.Background())
		if err != nil {
			s.terminate(CloseGoingAway, "peer closed")
			return
		}
		msg, err := DecodeClientMessage(raw)
		if err != nil {
			h.metrics.Counter("realtime.client.rejected", 1)
			s.terminate(ClosePolicyViolation, "only ping and ack are accepted; business actions use REST")
			return
		}
		switch msg.Type {
		case ClientPing:
			s.sendPong()
		case ClientAck:
			// The cursor is opaque to us too: we do not use it to prune anything,
			// because the outbox is the source of truth and at-least-once delivery
			// means an ack can never be treated as a proof of receipt.
			h.metrics.Counter("realtime.client.ack", 1)
		}
	}
}

// sendPong answers a client ping with a protocol-level pong notification.
func (s *subscriber) sendPong() {
	s.deliverWithCursor(ServerMessage{
		SchemaVersion: SchemaVersion,
		EventID:       "pong-" + s.id + "-" + fmt.Sprint(s.hub.now().UnixNano()),
		Type:          TypePong,
		RoomID:        s.roomID,
		RoomVersion:   s.roomVersionNow(),
		ServerEpoch:   s.hub.epoch,
		ServerTime:    s.hub.now().UTC().Format(time.RFC3339Nano),
	}, s.lastSeq.Load())
}

// heartbeatLoop keeps the connection alive and detects a dead peer.
//
// WS uses protocol ping/pong . SSE uses a
// comment frame every 15s. Neither writes to the database: presence is an
// in-process estimate, not a persisted heartbeat .
func (h *Hub) heartbeatLoop(s *subscriber) {
	if s.tr.NeedsKeepAlive() {
		t := time.NewTicker(h.cfg.SSEKeepAlive)
		defer t.Stop()
		for {
			select {
			case <-s.stop:
				return
			case <-t.C:
				ctx, cancel := context.WithTimeout(context.Background(), h.cfg.WriteTimeout)
				err := s.tr.KeepAlive(ctx)
				cancel()
				if err != nil {
					s.terminate(CloseServerError, "keep-alive failed")
					return
				}
			}
		}
	}

	t := time.NewTicker(h.cfg.HeartbeatInterval)
	defer t.Stop()
	for {
		select {
		case <-s.stop:
			return
		case <-t.C:
			ctx, cancel := context.WithTimeout(context.Background(), h.cfg.HeartbeatTimeout)
			err := s.tr.Ping(ctx)
			cancel()
			if err != nil {
				if errors.Is(err, ErrPingUnsupported) {
					return
				}
				s.terminate(CloseGoingAway, "heartbeat timeout")
				return
			}
		}
	}
}

// revalidateLoop periodically re-checks authorisation .
func (h *Hub) revalidateLoop(s *subscriber) {
	t := time.NewTicker(h.cfg.RevalidateInterval)
	defer t.Stop()
	for {
		select {
		case <-s.stop:
			return
		case <-t.C:
			active, err := h.revalidator.MembershipActive(context.Background(), s.roomID, s.membershipID)
			if err != nil {
				// Transient: never disconnect on a failed check.
				continue
			}
			if !active {
				h.revokeSubscriber(s, "membership is no longer active")
				return
			}
		}
	}
}

// schedulePresence debounces one aggregated presence.changed for a room.
//
// requires presence to be an aggregate: no per-person heartbeats, no
// database write every few seconds. This coalesces a burst of joins into one
// message carrying only the count.
func (h *Hub) schedulePresence(rs *roomState, online int) {
	rs.mu.Lock()
	if rs.presenceTimer != nil {
		// A timer is already running; it will read the current count when it fires,
		// so nothing else is needed. This is the whole coalescing mechanism: N joins
		// inside the window produce one presence.changed.
		rs.mu.Unlock()
		return
	}
	rs.presenceTimer = time.AfterFunc(h.cfg.PresenceDebounce, func() {
		rs.mu.Lock()
		count := rs.online
		rs.presenceTimer = nil
		rs.mu.Unlock()
		h.emitPresence(rs.id, count)
	})
	rs.mu.Unlock()
}

// emitPresence publishes one presence.changed for a room.
func (h *Hub) emitPresence(roomID string, online int) {
	tmpl := ServerMessage{
		SchemaVersion: SchemaVersion,
		EventID:       "presence-" + roomID + "-" + fmt.Sprint(h.now().UnixNano()),
		Type:          TypePresenceChange,
		RoomID:        roomID,
		ServerEpoch:   h.epoch,
		ServerTime:    h.now().UTC().Format(time.RFC3339Nano),
	}
	tmpl.RoomVersion = h.roomVersionOf(roomID)
	count := int64(online)
	tmpl.Online = &count

	rs, ok := h.existingRoom(roomID)
	if !ok {
		return
	}
	h.publish(publication{tmpl: tmpl, seq: h.lastSeqOf(rs)})
}

// roomVersionOf reads one room's version without creating it.
func (h *Hub) roomVersionOf(roomID string) int64 {
	rs, ok := h.existingRoom(roomID)
	if !ok {
		return 0
	}
	rs.mu.Lock()
	defer rs.mu.Unlock()
	return rs.roomVersion
}

// OnlineCount reports the aggregated online count for a room.
func (h *Hub) OnlineCount(roomID string) int {
	rs, ok := h.existingRoom(roomID)
	if !ok {
		return 0
	}
	rs.mu.Lock()
	defer rs.mu.Unlock()
	return rs.online
}

// SubscriptionCount reports how many live subscriptions exist.
func (h *Hub) SubscriptionCount() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.byID)
}

// RevokeMembership disconnects every subscription of one membership and sends
// session.revoked first.
//
// This is the "角色撤销立即断开订阅" requirement . The notification is
// critical: it is never merged away and never silently dropped. If it cannot be
// queued the connection is closed anyway, and the next handshake will be refused
// because the membership is no longer active.
func (h *Hub) RevokeMembership(roomID, membershipID, reason string) int {
	if reason == "" {
		reason = "membership revoked"
	}
	h.mu.RLock()
	var subs []*subscriber
	if roomID == "" {
		if set, ok := h.byMem[membershipID]; ok {
			for _, s := range set {
				subs = append(subs, s)
			}
		}
	} else if set, ok := h.byMem[membershipID]; ok {
		for _, s := range set {
			if s.roomID == roomID {
				subs = append(subs, s)
			}
		}
	}
	h.mu.RUnlock()

	for _, s := range subs {
		h.revokeSubscriber(s, reason)
	}
	return len(subs)
}

// RevokeSession disconnects every subscription of one session.
func (h *Hub) RevokeSession(sessionID, reason string) int {
	if reason == "" {
		reason = "session revoked"
	}
	h.mu.RLock()
	var subs []*subscriber
	if set, ok := h.bySess[sessionID]; ok {
		for _, s := range set {
			subs = append(subs, s)
		}
	}
	h.mu.RUnlock()

	for _, s := range subs {
		h.revokeSubscriber(s, reason)
	}
	return len(subs)
}

// revokeSubscriber sends session.revoked and then terminates.
//
// The send is synchronous with respect to the queue: the frame is placed before
// done is closed, so the writer goroutine still drains it. That ordering is what
// makes "revocation is never silently lost" true rather than aspirational.
func (h *Hub) revokeSubscriber(s *subscriber, reason string) {
	h.metrics.Counter(MetricRevoked, 1)
	m := ServerMessage{
		SchemaVersion: SchemaVersion,
		EventID:       "revoked-" + s.id,
		Type:          TypeSessionRevoked,
		RoomID:        s.roomID,
		RoomVersion:   s.roomVersionNow(),
		ServerEpoch:   h.epoch,
		ServerTime:    h.now().UTC().Format(time.RFC3339Nano),
		Reason:        reason,
		Cursor:        mintCursor(h.epoch, s.roomID, s.membershipID, s.lastSeq.Load()),
	}
	frame, err := json.Marshal(m)
	if err == nil {
		// Revocation outranks everything: clear substitutable traffic, then place it.
		s.out.evictSubstitutable()
		_ = s.out.enqueue(m, frame)
	}
	// Give the writer a moment to flush the frame before the socket is torn down.
	// Without this the client would see a bare close and have to guess whether it
	// was revoked or merely disconnected, which is exactly the ambiguity
	// wants to avoid.
	s.flushAndTerminate(CloseSessionRevoked, reason)
}

// flushAndTerminate lets the writer deliver what is already queued, then
// terminates.
//
// Closing the queue first is what makes this work: pop() drains what is already
// there and only then reports "nothing more", so no new frame can be accepted
// while the revocation is in flight. The wait is bounded by RevokeGrace because a
// peer that cannot accept one small frame is gone, and keeping a revoked session
// subscribed is worse than an unacknowledged close.
func (s *subscriber) flushAndTerminate(code int, reason string) {
	s.out.close()

	deadline := time.NewTimer(s.hub.cfg.RevokeGrace)
	defer deadline.Stop()
	tick := time.NewTicker(2 * time.Millisecond)
	defer tick.Stop()
	for !s.out.drained() {
		select {
		case <-deadline.C:
			// The peer could not accept the frame within the grace window. Closing is
			// the correct outcome: the membership is already gone, so the next
			// handshake is refused regardless.
			s.terminate(code, reason)
			return
		case <-tick.C:
		}
	}
	s.terminate(code, reason)
}

// evictSubstitutable exposes the queue's eviction step for the escalation path.
func (o *outbound) evictSubstitutable() {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.evictSubstitutableLocked()
}
