package realtime

import (
	"context"
	"database/sql"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/bhcn/umaguessinggame/internal/storage"
)

// fakeTransport is an in-memory transport for tests.
//
// It is the only place the tests touch the wire, and it deliberately records
// frames instead of parsing them into anything, so a test can assert on the exact
// JSON a browser would receive.
type fakeTransport struct {
	mu       sync.Mutex
	frames   []fakeFrame
	inbound  chan []byte
	closed   chan struct{}
	closeOne sync.Once
	// writeErr, when set, makes every write fail, simulating a dead socket.
	writeErr error
	// writeDelay slows every write, simulating a peer that cannot keep up.
	writeDelay time.Duration
	// gate, when non-nil, blocks every write until a token is received. It is the
	// deterministic way to stall the writer: no sleeps, no timing assumptions.
	gate chan struct{}
	// pingErr makes Ping fail, simulating a dead peer.
	pingErr error
	channel Channel
}

type fakeFrame struct {
	msg ServerMessage
	raw []byte
	at  time.Time
}

func newFakeTransport() *fakeTransport {
	return &fakeTransport{
		inbound: make(chan []byte, 64),
		closed:  make(chan struct{}),
		channel: ChannelWS,
	}
}

func newFakeSSETransport() *fakeTransport {
	t := newFakeTransport()
	t.channel = ChannelSSE
	return t
}

func (f *fakeTransport) Channel() Channel { return f.channel }

func (f *fakeTransport) WriteFrame(ctx context.Context, m ServerMessage, frame []byte) error {
	f.mu.Lock()
	err := f.writeErr
	delay := f.writeDelay
	gate := f.gate
	f.mu.Unlock()
	if gate != nil {
		select {
		case <-gate:
		case <-ctx.Done():
			return ctx.Err()
		case <-f.closed:
			return context.Canceled
		}
	}
	if delay > 0 {
		select {
		case <-time.After(delay):
		case <-ctx.Done():
			return ctx.Err()
		case <-f.closed:
			return context.Canceled
		}
	}
	if err != nil {
		return err
	}
	f.mu.Lock()
	// Copy: the caller may reuse the buffer.
	cp := make([]byte, len(frame))
	copy(cp, frame)
	f.frames = append(f.frames, fakeFrame{msg: m, raw: cp, at: time.Now()})
	f.mu.Unlock()
	return nil
}

func (f *fakeTransport) ReadFrame(ctx context.Context) ([]byte, error) {
	select {
	case b := <-f.inbound:
		return b, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-f.closed:
		return nil, context.Canceled
	}
}

func (f *fakeTransport) Ping(ctx context.Context) error {
	f.mu.Lock()
	err := f.pingErr
	f.mu.Unlock()
	if err != nil {
		return err
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-f.closed:
		return context.Canceled
	default:
		return nil
	}
}

func (f *fakeTransport) KeepAlive(context.Context) error { return nil }

func (f *fakeTransport) NeedsKeepAlive() bool { return f.channel == ChannelSSE }

func (f *fakeTransport) Close(int, string) error {
	f.closeOne.Do(func() { close(f.closed) })
	return nil
}

// setGate installs or clears the write gate.
func (f *fakeTransport) setGate(g chan struct{}) {
	f.mu.Lock()
	f.gate = g
	f.mu.Unlock()
}

// stallAfterHello returns a gate pre-filled with one token, so the hello frame is
// written and every subsequent write blocks. That makes "the peer stopped reading
// after it was connected" deterministic: no sleeps and no assumptions about which
// frame the writer happens to be on.
func stallAfterHello() chan struct{} {
	g := make(chan struct{}, 1)
	g <- struct{}{}
	return g
}

// release closes the gate so the blocked writer proceeds.
func release(g chan struct{}) {
	close(g)
}

// sent returns a copy of the recorded frames.
func (f *fakeTransport) sent() []fakeFrame {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]fakeFrame, len(f.frames))
	copy(out, f.frames)
	return out
}

// send queues one client frame.
func (f *fakeTransport) send(raw []byte) {
	f.inbound <- raw
}

// waitFrames waits until at least n frames have been recorded or the deadline
// passes, and returns whatever was recorded.
func (f *fakeTransport) waitFrames(t *testing.T, n int, timeout time.Duration) []fakeFrame {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for {
		got := f.sent()
		if len(got) >= n {
			return got
		}
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %d frames, got %d", n, len(got))
		}
		time.Sleep(time.Millisecond)
	}
}

// waitType waits until at least one frame of the given type has been recorded and
// returns it. It is the type-aware counterpart of waitFrames: presence.changed and
// pong can legitimately interleave with the frames a test cares about, so asserting
// on a positional index would make tests depend on unrelated timing.
func (f *fakeTransport) waitType(t *testing.T, typ MessageType, timeout time.Duration) fakeFrame {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for {
		for _, fr := range f.sent() {
			if fr.msg.Type == typ {
				return fr
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for a %s frame; got %v", typ, frameTypes(f.sent()))
		}
		time.Sleep(time.Millisecond)
	}
}

// ofType returns every recorded frame of one type.
func (f *fakeTransport) ofType(typ MessageType) []fakeFrame {
	var out []fakeFrame
	for _, fr := range f.sent() {
		if fr.msg.Type == typ {
			out = append(out, fr)
		}
	}
	return out
}

// waitSubscribed waits until the hub has registered a subscription, so a test that
// publishes immediately after subscribing does not race the registration.
func waitSubscribed(t *testing.T, hub *Hub, n int) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for hub.SubscriptionCount() != n {
		if time.Now().After(deadline) {
			t.Fatalf("subscription count = %d, want %d", hub.SubscriptionCount(), n)
		}
		time.Sleep(time.Millisecond)
	}
}

// waitClosed waits for the transport to be closed.
func (f *fakeTransport) waitClosed(t *testing.T, timeout time.Duration) {
	t.Helper()
	select {
	case <-f.closed:
	case <-time.After(timeout):
		t.Fatal("timed out waiting for the transport to close")
	}
}

// newTestHub builds a hub with short timings so tests stay fast without changing
// the semantics under test.
func newTestHub(t *testing.T, tweak func(*Config)) *Hub {
	t.Helper()
	cfg := Config{
		CoalesceWindow:     MinCoalesceWindow,
		QueueMaxMessages:   DefaultQueueMaxMessages,
		QueueMaxBytes:      DefaultQueueMaxBytes,
		WriteTimeout:       2 * time.Second,
		HeartbeatInterval:  50 * time.Millisecond,
		HeartbeatTimeout:   200 * time.Millisecond,
		SSEKeepAlive:       50 * time.Millisecond,
		HistoryMaxAge:      10 * time.Minute,
		HistoryMaxCount:    DefaultHistoryMaxCount,
		PresenceDebounce:   20 * time.Millisecond,
		OutboxPollInterval: 10 * time.Millisecond,
		OutboxBatch:        64,
		RevalidateInterval: 0,
		RequireOrigin:      true,
		StrictMessages:     true,
	}
	if tweak != nil {
		tweak(&cfg)
	}
	hub, err := NewHub(cfg)
	if err != nil {
		t.Fatalf("NewHub: %v", err)
	}
	t.Cleanup(func() { hub.Close() })
	return hub
}

// newTestDB opens a real SQLite database in a temporary directory.
//
// The dispatcher tests use the real schema, not a stub: the "no broadcast before
// commit" property depends on WAL visibility semantics, which a fake would not
// reproduce.
func newTestDB(t *testing.T) *storage.DB {
	t.Helper()
	db, err := storage.Open(context.Background(), storage.Config{Dir: t.TempDir()})
	if err != nil {
		t.Fatalf("storage.Open: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

// seedRoom inserts the minimum rows a room needs: a session, a room and a
// membership, so outbox rows can satisfy their foreign keys.
func seedRoom(t *testing.T, db *storage.DB, roomID, sessionID, membershipID, role string) {
	t.Helper()
	ctx := context.Background()
	now := time.Now().UTC().UnixMilli()
	if _, err := db.Write().ExecContext(ctx,
		`INSERT INTO sessions (id, token_hash, display_name, csrf_token_hash, kind, created_at, expires_at)
		 VALUES (?, ?, ?, ?, 'player', ?, ?)`,
		sessionID, "hash-"+sessionID, "Tester", "csrf-"+sessionID, now, now+3600_000); err != nil {
		t.Fatalf("seed session: %v", err)
	}
	if _, err := db.Write().ExecContext(ctx,
		`INSERT INTO rooms (id, owner_session_id, name, status, room_version, created_at, updated_at)
		 VALUES (?, ?, 'Room', 'playing', 1, ?, ?)`,
		roomID, sessionID, now, now); err != nil {
		t.Fatalf("seed room: %v", err)
	}
	if _, err := db.Write().ExecContext(ctx,
		`INSERT INTO memberships (id, room_id, session_id, role, joined_at)
		 VALUES (?, ?, ?, ?, ?)`,
		membershipID, roomID, sessionID, role, now); err != nil {
		t.Fatalf("seed membership: %v", err)
	}
}

// appendOutbox writes one outbox row inside a transaction and commits it.
func appendOutbox(t *testing.T, db *storage.DB, roomID, eventID string, seq int64, typ MessageType, payload OutboxPayload) {
	t.Helper()
	ctx := context.Background()
	tx, err := db.Write().BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	if err := AppendEvent(ctx, tx, eventID, roomID, seq, typ, payload); err != nil {
		tx.Rollback()
		t.Fatalf("AppendEvent: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("commit: %v", err)
	}
}

// subscribe starts one subscription and returns its transport plus a cancel.
func subscribe(t *testing.T, hub *Hub, ident Identity, cursor string, tr *fakeTransport) (cancel func()) {
	t.Helper()
	ctx, cancelFn := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = hub.Serve(ctx, SubscribeOptions{
			Identity:  ident,
			Channel:   tr.Channel(),
			Cursor:    cursor,
			Transport: tr,
		})
	}()
	var once sync.Once
	stop := func() {
		once.Do(func() {
			cancelFn()
			tr.Close(1000, "test done")
			select {
			case <-done:
			case <-time.After(2 * time.Second):
				t.Error("subscription did not stop")
			}
		})
	}
	t.Cleanup(stop)
	return stop
}

// mustJSON decodes a recorded frame.
func mustJSON(t *testing.T, raw []byte) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatalf("frame is not JSON: %v (%s)", err, raw)
	}
	return m
}

// identity builds a valid identity for tests.
func identity(roomID, membershipID, sessionID, role string) Identity {
	return Identity{
		RoomID:       roomID,
		MembershipID: membershipID,
		SessionID:    sessionID,
		Role:         role,
	}
}

// ensure sql import is used even if a future refactor drops a helper.
var _ = sql.ErrNoRows
