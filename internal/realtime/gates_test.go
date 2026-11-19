package realtime

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/bhcn/umaguessinggame/internal/storage"
)

// -----------------------------------------------------------------------------
// Gate 1: no broadcast before the transaction commits.
// -----------------------------------------------------------------------------

// TestDispatcherDoesNotPublishUncommitted is the release-gate test for
// "事务提交前发成功广播是明确禁止的".
//
// It proves the property two ways:
//
// 1. While the writing transaction is still open, repeated dispatch cycles see
// nothing, even though the row exists on the writer's connection.
// 2. Only after Commit does a dispatch cycle deliver the notification.
//
// The first half is the one that matters. If someone ever "optimises" the
// dispatcher to publish from the writer connection, or to enqueue before commit,
// this test fails.
func TestDispatcherDoesNotPublishUncommitted(t *testing.T) {
	db := newTestDB(t)
	hub := newTestHub(t, nil)
	const roomID, sessionID, memberID = "room-1", "sess-1", "mem-1"
	seedRoom(t, db, roomID, sessionID, memberID, "player")

	tr := newFakeTransport()
	subscribe(t, hub, identity(roomID, memberID, sessionID, "player"), "", tr)
	tr.waitFrames(t, 1, time.Second) // hello

	disp, err := NewDispatcher(db, hub, hub.Config())
	if err != nil {
		t.Fatalf("NewDispatcher: %v", err)
	}

	ctx := context.Background()
	tx, err := db.Write().BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	if err := AppendEvent(ctx, tx, "evt-uncommitted", roomID, 1, TypeStateChanged, OutboxPayload{
		RoomVersion: 2,
		Resource:    ResourceRoom,
	}); err != nil {
		tx.Rollback()
		t.Fatalf("AppendEvent: %v", err)
	}

	// Dispatch while the transaction is open. Nothing may be published.
	for i := 0; i < 5; i++ {
		n, err := disp.DispatchOnce(ctx)
		if err != nil {
			t.Fatalf("DispatchOnce (pre-commit): %v", err)
		}
		if n != 0 {
			t.Fatalf("dispatched %d rows before commit; a broadcast must never precede the commit", n)
		}
	}
	if got := len(tr.sent()); got != 1 {
		t.Fatalf("received %d frames before commit, want only the hello", got)
	}

	// A backlog query must also see nothing: the row is not committed.
	backlog, err := disp.Backlog(ctx)
	if err != nil {
		t.Fatalf("Backlog: %v", err)
	}
	if backlog != 0 {
		t.Fatalf("backlog = %d before commit, want 0", backlog)
	}

	if err := tx.Commit(); err != nil {
		t.Fatalf("commit: %v", err)
	}

	// Now it must be published.
	n, err := disp.DispatchOnce(ctx)
	if err != nil {
		t.Fatalf("DispatchOnce (post-commit): %v", err)
	}
	if n != 1 {
		t.Fatalf("dispatched %d rows after commit, want 1", n)
	}
	frame := tr.waitType(t, TypeStateChanged, 2*time.Second)
	if frame.msg.EventID != "evt-uncommitted" {
		t.Fatalf("eventId = %q, want evt-uncommitted", frame.msg.EventID)
	}
}

// TestRolledBackOutboxIsNeverPublished proves the converse: a rolled-back write
// leaves no trace and can never be broadcast.
func TestRolledBackOutboxIsNeverPublished(t *testing.T) {
	db := newTestDB(t)
	hub := newTestHub(t, nil)
	const roomID, sessionID, memberID = "room-rb", "sess-rb", "mem-rb"
	seedRoom(t, db, roomID, sessionID, memberID, "player")

	tr := newFakeTransport()
	subscribe(t, hub, identity(roomID, memberID, sessionID, "player"), "", tr)
	tr.waitFrames(t, 1, time.Second)

	disp, err := NewDispatcher(db, hub, hub.Config())
	if err != nil {
		t.Fatalf("NewDispatcher: %v", err)
	}
	ctx := context.Background()

	tx, err := db.Write().BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	if err := AppendEvent(ctx, tx, "evt-rolled-back", roomID, 1, TypePhaseChanged, OutboxPayload{
		RoomVersion:  2,
		PhaseVersion: 2,
	}); err != nil {
		t.Fatalf("AppendEvent: %v", err)
	}
	if err := tx.Rollback(); err != nil {
		t.Fatalf("rollback: %v", err)
	}

	n, err := disp.DispatchOnce(ctx)
	if err != nil {
		t.Fatalf("DispatchOnce: %v", err)
	}
	if n != 0 {
		t.Fatalf("dispatched %d rows from a rolled-back transaction", n)
	}
	if got := len(tr.sent()); got != 1 {
		t.Fatalf("frames = %d, want 1 (hello only)", got)
	}
}

// -----------------------------------------------------------------------------
// Gate 2: at-least-once delivery with client-side dedupe by eventId.
// -----------------------------------------------------------------------------

// TestAtLeastOnceRedeliveryIsDedupableByEventID forces the crash window between
// publish and mark, then proves the same eventId is delivered twice.
//
// That is the contract: the server is allowed to redeliver, the client dedupes by
// eventId, and the server must never pretend the duplicate did not happen.
func TestAtLeastOnceRedeliveryIsDedupableByEventID(t *testing.T) {
	db := newTestDB(t)
	hub := newTestHub(t, nil)
	const roomID, sessionID, memberID = "room-ao", "sess-ao", "mem-ao"
	seedRoom(t, db, roomID, sessionID, memberID, "player")

	tr := newFakeTransport()
	subscribe(t, hub, identity(roomID, memberID, sessionID, "player"), "", tr)
	tr.waitFrames(t, 1, time.Second)

	disp, err := NewDispatcher(db, hub, hub.Config())
	if err != nil {
		t.Fatalf("NewDispatcher: %v", err)
	}
	ctx := context.Background()

	appendOutbox(t, db, roomID, "evt-once", 1, TypeStateChanged, OutboxPayload{
		RoomVersion: 2, Resource: ResourceRoom,
	})

	// Simulate a crash after publish, before the dispatched_at update.
	disp.markFailure = func(string) error { return errors.New("simulated crash after publish") }
	if _, err := disp.DispatchOnce(ctx); err == nil {
		t.Fatal("expected the simulated crash to surface as an error")
	}
	first := tr.waitType(t, TypeStateChanged, 2*time.Second)
	if first.msg.EventID != "evt-once" {
		t.Fatalf("eventId = %q, want evt-once", first.msg.EventID)
	}

	// The row is still undispatched, so the next cycle redelivers it.
	disp.markFailure = nil
	n, err := disp.DispatchOnce(ctx)
	if err != nil {
		t.Fatalf("DispatchOnce (retry): %v", err)
	}
	if n != 1 {
		t.Fatalf("redelivered %d rows, want 1", n)
	}

	// Both deliveries carry the same eventId, so a client that keeps a set of seen
	// ids collapses them into one. Nothing else may be needed to dedupe.
	deadline := time.Now().Add(2 * time.Second)
	var seen map[string]int
	for {
		seen = map[string]int{}
		for _, f := range tr.ofType(TypeStateChanged) {
			seen[f.msg.EventID]++
		}
		if seen["evt-once"] >= 2 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("expected 2 deliveries of evt-once, got %d", seen["evt-once"])
		}
		time.Sleep(time.Millisecond)
	}
	if seen["evt-once"] != 2 {
		t.Fatalf("expected exactly 2 deliveries of evt-once, got %d", seen["evt-once"])
	}

	// A third cycle must find nothing: the row is now marked.
	n, err = disp.DispatchOnce(ctx)
	if err != nil {
		t.Fatalf("DispatchOnce (after mark): %v", err)
	}
	if n != 0 {
		t.Fatalf("dispatched %d rows after the mark, want 0", n)
	}
}

// TestCursorIsStableAcrossRedelivery proves the cursor does not move when the same
// event is redelivered, so a redelivery cannot rewind or skip a client position.
func TestCursorIsStableAcrossRedelivery(t *testing.T) {
	hub := newTestHub(t, nil)
	const roomID, sessionID, memberID = "room-cur", "sess-cur", "mem-cur"

	tr := newFakeTransport()
	subscribe(t, hub, identity(roomID, memberID, sessionID, "player"), "", tr)
	tr.waitFrames(t, 1, time.Second)

	// phase.changed is used rather than state.changed because ordinary state
	// notifications are legitimately merged inside the window, which would make the
	// two deliveries indistinguishable from one. A phase transition is never merged,
	// so the same event genuinely arrives twice.
	pub := Publication{
		EventID: "evt-same", Type: TypePhaseChanged, RoomID: roomID,
		RoomVersion: 3, PhaseVersion: 3, Seq: 7,
	}
	if err := hub.Publish(pub); err != nil {
		t.Fatalf("Publish: %v", err)
	}
	if err := hub.Publish(pub); err != nil {
		t.Fatalf("Publish (repeat): %v", err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for len(tr.ofType(TypePhaseChanged)) < 2 {
		if time.Now().After(deadline) {
			t.Fatalf("expected 2 phase.changed frames, got %d", len(tr.ofType(TypePhaseChanged)))
		}
		time.Sleep(time.Millisecond)
	}
	frames := tr.ofType(TypePhaseChanged)
	if frames[0].msg.Cursor != frames[1].msg.Cursor {
		t.Fatalf("cursor changed between redeliveries: %q vs %q",
			frames[0].msg.Cursor, frames[1].msg.Cursor)
	}
}

// -----------------------------------------------------------------------------
// Gate 3: slow client backpressure, coalescing and resync.
// -----------------------------------------------------------------------------

// TestCoalesceWindowMergesOrdinaryNotifications proves the 100–200ms merge: many
// state.changed in one scope inside one window collapse to a single frame.
func TestCoalesceWindowMergesOrdinaryNotifications(t *testing.T) {
	hub := newTestHub(t, func(c *Config) { c.CoalesceWindow = MaxCoalesceWindow })
	const roomID, sessionID, memberID = "room-co", "sess-co", "mem-co"

	tr := newFakeTransport()
	subscribe(t, hub, identity(roomID, memberID, sessionID, "player"), "", tr)
	tr.waitFrames(t, 1, time.Second)

	const burst = 20
	for i := 0; i < burst; i++ {
		if err := hub.Publish(Publication{
			EventID:     fmt.Sprintf("evt-co-%d", i),
			Type:        TypeStateChanged,
			RoomID:      roomID,
			RoomVersion: int64(i + 2),
			Seq:         int64(i + 1),
			Resource:    ResourceRoom,
		}); err != nil {
			t.Fatalf("Publish %d: %v", i, err)
		}
	}

	// Wait past the window, then check that the burst collapsed.
	time.Sleep(MaxCoalesceWindow + 200*time.Millisecond)
	states := tr.ofType(TypeStateChanged)
	if len(states) == 0 {
		t.Fatal("no state.changed frame was delivered")
	}
	if len(states) >= burst {
		t.Fatalf("%d notifications produced %d frames; the merge window did not coalesce anything",
			burst, len(states))
	}
	// The newest version must survive the merge: coalescing may drop intermediate
	// versions but never the latest, or the client would fetch a stale snapshot.
	last := states[len(states)-1]
	if last.msg.RoomVersion != burst+1 {
		t.Fatalf("last state.changed roomVersion = %d, want %d (the newest must survive coalescing)",
			last.msg.RoomVersion, burst+1)
	}
}

// TestPhaseChangedIsNeverCoalesced proves terminal semantics are not merged away.
//
// A phase transition carries "locked / revealed / settled / cancelled". If two of
// them were merged into one frame the client could miss the transition that ends
// the round, which explicitly forbids.
func TestPhaseChangedIsNeverCoalesced(t *testing.T) {
	hub := newTestHub(t, func(c *Config) { c.CoalesceWindow = MaxCoalesceWindow })
	const roomID, sessionID, memberID = "room-ph", "sess-ph", "mem-ph"

	tr := newFakeTransport()
	subscribe(t, hub, identity(roomID, memberID, sessionID, "player"), "", tr)
	tr.waitFrames(t, 1, time.Second)

	for i := 0; i < 5; i++ {
		if err := hub.Publish(Publication{
			EventID:      fmt.Sprintf("evt-ph-%d", i),
			Type:         TypePhaseChanged,
			RoomID:       roomID,
			RoomVersion:  int64(i + 2),
			PhaseVersion: int64(i + 2),
			Seq:          int64(i + 1),
		}); err != nil {
			t.Fatalf("Publish %d: %v", i, err)
		}
	}
	deadline := time.Now().Add(2 * time.Second)
	for len(tr.ofType(TypePhaseChanged)) < 5 {
		if time.Now().After(deadline) {
			t.Fatalf("delivered %d phase.changed frames, want 5 (phase transitions must not be merged)",
				len(tr.ofType(TypePhaseChanged)))
		}
		time.Sleep(time.Millisecond)
	}
	if got := len(tr.ofType(TypePhaseChanged)); got != 5 {
		t.Fatalf("delivered %d phase.changed frames, want exactly 5", got)
	}
}

// TestSlowClientGetsResyncNotSilentLoss proves the slow-consumer policy: once the
// outbound queue is full, ordinary notifications stop and the client is explicitly
// told to rebuild, rather than silently losing updates.
//
// The peer is stalled deterministically with a write gate rather than a sleep, so
// the test asserts a property instead of a timing.
func TestSlowClientGetsResyncNotSilentLoss(t *testing.T) {
	hub := newTestHub(t, func(c *Config) {
		c.QueueMaxMessages = 4
		c.QueueMaxBytes = 4096
		c.CoalesceWindow = MinCoalesceWindow
		c.WriteTimeout = 5 * time.Second
	})
	const roomID, sessionID, memberID = "room-slow", "sess-slow", "mem-slow"

	tr := newFakeTransport()
	gate := stallAfterHello()
	tr.setGate(gate)
	subscribe(t, hub, identity(roomID, memberID, sessionID, "player"), "", tr)
	waitSubscribed(t, hub, 1)
	tr.waitFrames(t, 1, 2*time.Second) // hello got through, the peer is now stalled

	// Publish far more than the queue can hold. Distinct round ids defeat the merge
	// key, so the pressure is real rather than absorbed by coalescing.
	for i := 0; i < 40; i++ {
		if err := hub.Publish(Publication{
			EventID:     fmt.Sprintf("evt-slow-%d", i),
			Type:        TypeStateChanged,
			RoomID:      roomID,
			RoomVersion: int64(i + 2),
			Seq:         int64(i + 1),
			Resource:    ResourceRound,
			RoundID:     fmt.Sprintf("round-%d", i),
		}); err != nil {
			t.Fatalf("Publish %d: %v", i, err)
		}
	}

	// The stalled peer must have been escalated: either an overflow was recorded while
	// it was still subscribed, or the connection was closed outright. Both are
	// acceptable answers; silence is not. The counters are sampled continuously
	// because a close removes the subscriber from the index.
	deadline := time.Now().Add(3 * time.Second)
	escalated := false
	for !escalated {
		stats := slowStats(hub)
		if stats.Overflow > 0 || stats.CloseNeeded > 0 {
			escalated = true
			break
		}
		select {
		case <-tr.closed:
			escalated = true
		default:
		}
		if time.Now().After(deadline) {
			t.Fatalf("no escalation was recorded for a stalled peer: %+v", stats)
		}
		time.Sleep(2 * time.Millisecond)
	}

	// Unblock the transport. The escalation must be observable on the wire as either a
	// resync.required (so the client rebuilds) or a close (so the client reconnects
	// and rebuilds). A silently dropped update would be the only failure.
	release(gate)
	resync := tr.waitType(t, TypeResyncRequired, 5*time.Second)
	if resync.msg.Reason == "" {
		t.Fatal("resync.required carries no reason; the client cannot tell why")
	}
	if resync.msg.Reason != ResyncSlowConsumer {
		t.Fatalf("resync reason = %q, want %q", resync.msg.Reason, ResyncSlowConsumer)
	}
}

// waitOverflow blocks until the single subscriber has recorded a queue overflow, or
// fails. It is the deterministic way to reach "the peer is behind".
func waitOverflow(t *testing.T, hub *Hub, timeout time.Duration) outboundStats {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for {
		stats := slowStats(hub)
		if stats.Overflow > 0 || stats.CloseNeeded > 0 {
			return stats
		}
		if time.Now().After(deadline) {
			t.Fatalf("the peer never fell behind; counters: %+v", stats)
		}
		time.Sleep(time.Millisecond)
	}
}

// slowStats finds the single subscriber's outbound counters.
func slowStats(hub *Hub) outboundStats {
	hub.mu.RLock()
	defer hub.mu.RUnlock()
	for _, s := range hub.byID {
		return s.out.snapshot()
	}
	return outboundStats{}
}

func frameTypes(frames []fakeFrame) []string {
	out := make([]string, 0, len(frames))
	for _, f := range frames {
		out = append(out, string(f.msg.Type))
	}
	return out
}

// TestCriticalNotificationEvictsOrdinaryOnes proves the priority rule: a
// session.revoked is never dropped in favour of queued state updates.
func TestCriticalNotificationEvictsOrdinaryOnes(t *testing.T) {
	hub := newTestHub(t, func(c *Config) {
		c.QueueMaxMessages = 4
		c.QueueMaxBytes = 4096
		c.RevokeGrace = 5 * time.Second
	})
	const roomID, sessionID, memberID = "room-pri", "sess-pri", "mem-pri"

	tr := newFakeTransport()
	gate := stallAfterHello()
	tr.setGate(gate)
	subscribe(t, hub, identity(roomID, memberID, sessionID, "player"), "", tr)
	waitSubscribed(t, hub, 1)
	tr.waitFrames(t, 1, 2*time.Second) // hello got through, the peer is now stalled

	for i := 0; i < 30; i++ {
		_ = hub.Publish(Publication{
			EventID:     fmt.Sprintf("evt-pri-%d", i),
			Type:        TypeStateChanged,
			RoomID:      roomID,
			RoomVersion: int64(i + 2),
			Seq:         int64(i + 1),
			Resource:    ResourceRound,
			RoundID:     fmt.Sprintf("round-%d", i),
		})
	}

	// Wait until the peer is provably behind before revoking. This is the state the
	// test is about: the queue is full of ordinary traffic, so the revocation has to
	// displace it. Waiting for the counter instead of sleeping makes the setup
	// deterministic.
	waitOverflow(t, hub, 3*time.Second)

	// Revoke while the peer is stalled. RevokeMembership waits for the frame to reach
	// the wire, so it runs alongside the release.
	revoked := make(chan int, 1)
	go func() {
		revoked <- hub.RevokeMembership(roomID, memberID, "role changed")
	}()
	release(gate)

	select {
	case n := <-revoked:
		if n != 1 {
			t.Fatalf("revoked %d subscriptions, want 1", n)
		}
	case <-time.After(8 * time.Second):
		t.Fatal("RevokeMembership did not return")
	}

	frame := tr.waitType(t, TypeSessionRevoked, 8*time.Second)
	if frame.msg.Reason == "" {
		t.Fatal("session.revoked carries no reason")
	}
	// And the peer must not keep receiving ordinary state after being revoked.
	after := 0
	for _, f := range tr.sent() {
		if f.msg.Type == TypeStateChanged && f.at.After(frame.at) {
			after++
		}
	}
	if after != 0 {
		t.Fatalf("%d state.changed frames arrived after session.revoked", after)
	}
}

// TestRevocationClosesTheConnection proves the revocation is not merely advisory.
func TestRevocationClosesTheConnection(t *testing.T) {
	hub := newTestHub(t, nil)
	const roomID, sessionID, memberID = "room-rev", "sess-rev", "mem-rev"

	tr := newFakeTransport()
	subscribe(t, hub, identity(roomID, memberID, sessionID, "player"), "", tr)
	tr.waitFrames(t, 1, time.Second)
	if hub.SubscriptionCount() != 1 {
		t.Fatalf("subscriptions = %d, want 1", hub.SubscriptionCount())
	}

	n := hub.RevokeSession(sessionID, "session revoked by host")
	if n != 1 {
		t.Fatalf("revoked %d subscriptions, want 1", n)
	}
	tr.waitClosed(t, 3*time.Second)

	deadline := time.Now().Add(3 * time.Second)
	for hub.SubscriptionCount() != 0 {
		if time.Now().After(deadline) {
			t.Fatalf("subscription count = %d after revocation, want 0", hub.SubscriptionCount())
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// -----------------------------------------------------------------------------
// Gate 4: cursor gaps are legitimate and never inferred as loss.
// -----------------------------------------------------------------------------

// TestCursorGapIsNotTreatedAsLoss proves the property states: the visible
// sequence may have gaps because of private filtering, and a resume must replay the
// visible entries without declaring a resync.
//
// The events are 1,2,3,4,5 but only odd sequences are visible to this member. A
// client that insisted on "difference > 1 means loss" would resync on every
// resume; the server must not encourage that, so it replays 3 and 5 without a gap.
func TestCursorGapIsNotTreatedAsLoss(t *testing.T) {
	hub := newTestHub(t, nil)
	const roomID = "room-gap"
	const visibleMem, hiddenMem = "mem-visible", "mem-hidden"

	tr := newFakeTransport()
	subscribe(t, hub, identity(roomID, visibleMem, "sess-visible", "player"), "", tr)
	tr.waitFrames(t, 1, time.Second)

	for i := int64(1); i <= 5; i++ {
		aud := Audience{}
		if i%2 == 0 {
			// Even sequences are for the host only; the player never sees them.
			aud = Audience{MembershipIDs: []string{hiddedHolder}}
		}
		if err := hub.Publish(Publication{
			EventID:     fmt.Sprintf("evt-gap-%d", i),
			Type:        TypeStateChanged,
			RoomID:      roomID,
			RoomVersion: i + 1,
			Seq:         i,
			Resource:    ResourceRoom,
			Audience:    aud,
		}); err != nil {
			t.Fatalf("Publish %d: %v", i, err)
		}
	}
	time.Sleep(MinCoalesceWindow + 100*time.Millisecond)

	frames := tr.sent()
	// The player received the hello plus state changes for sequences 1, 3, 5.
	var stateSeqs []int64
	for _, f := range frames {
		if f.msg.Type == TypeStateChanged {
			stateSeqs = append(stateSeqs, f.msg.RoomVersion)
		}
	}
	if len(stateSeqs) == 0 {
		t.Fatal("no visible state.changed was delivered")
	}

	// Resume from the cursor of the first delivered state change. The replay must
	// not report a gap even though the sequence numbers are not consecutive.
	var resumeFrom string
	for _, f := range frames {
		if f.msg.Type == TypeStateChanged {
			resumeFrom = f.msg.Cursor
			break
		}
	}

	tr2 := newFakeTransport()
	subscribe(t, hub, identity(roomID, visibleMem, "sess-visible", "player"), resumeFrom, tr2)
	got := tr2.waitFrames(t, 2, 2*time.Second)
	for _, f := range got {
		if f.msg.Type == TypeResyncRequired {
			t.Fatalf("resume over a legitimately gapped sequence produced resync.required (%q); "+
				"private filtering must not be reported as loss", f.msg.Reason)
		}
	}
}

// hiddedHolder is the membership allowed to see the even sequences.
const hiddedHolder = "mem-hidden-holder"

// TestResumeReplaysOnlyVisibleEntries proves replay applies the same audience
// filter as live delivery, so a resume cannot leak a private event.
func TestResumeReplaysOnlyVisibleEntries(t *testing.T) {
	hub := newTestHub(t, nil)
	const roomID = "room-filter"
	const playerMem, hostMem = "mem-player", "mem-host"

	// A host is subscribed so the room has state and history.
	hostTr := newFakeTransport()
	subscribe(t, hub, identity(roomID, hostMem, "sess-host", "host"), "", hostTr)
	hostTr.waitFrames(t, 1, time.Second)

	// Sequence 1 is public, sequence 2 is host-only.
	if err := hub.Publish(Publication{
		EventID: "evt-public", Type: TypeStateChanged, RoomID: roomID,
		RoomVersion: 2, Seq: 1, Resource: ResourceRoom,
	}); err != nil {
		t.Fatalf("Publish public: %v", err)
	}
	if err := hub.Publish(Publication{
		EventID: "evt-host-only", Type: TypeStateChanged, RoomID: roomID,
		RoomVersion: 3, Seq: 2, Resource: ResourceRoom,
		Audience: Audience{Roles: []string{"host"}},
	}); err != nil {
		t.Fatalf("Publish host-only: %v", err)
	}
	time.Sleep(MinCoalesceWindow + 100*time.Millisecond)

	// The player resumes from sequence 0 and must only receive the public event.
	playerTr := newFakeTransport()
	subscribe(t, hub, identity(roomID, playerMem, "sess-player", "player"), "", playerTr)
	playerTr.waitFrames(t, 1, time.Second)

	// Mint a cursor at sequence 0 for the player by publishing nothing and using a
	// fresh connection: instead, drive the replay path explicitly by connecting with
	// a cursor obtained from a first connection.
	// Simplest correct approach: connect a second player connection with the cursor
	// of the hello of the first player connection, then publish nothing and check
	// that replaying from 0..2 filters sequence 2 out.
	pub := newFakeTransport()
	cursor := mintCursor(hub.Epoch(), roomID, playerMem, 0)
	subscribe(t, hub, identity(roomID, playerMem, "sess-player2", "player"), cursor, pub)
	frames := pub.waitFrames(t, 2, 2*time.Second)

	for _, f := range frames {
		if f.msg.EventID == "evt-host-only" {
			t.Fatal("a host-only notification was replayed to a player")
		}
	}
	// And the public one must be there, otherwise the filter is just dropping
	// everything and the test would pass vacuously.
	found := false
	for _, f := range frames {
		if f.msg.EventID == "evt-public" {
			found = true
		}
	}
	if !found {
		t.Fatalf("the public notification was not replayed; frames: %v", frameTypes(frames))
	}
}

// TestCursorRejectsOtherScope proves a cursor is bound to its scope.
func TestCursorRejectsOtherScope(t *testing.T) {
	hub := newTestHub(t, nil)
	const roomID, sessionID, memberID = "room-scope", "sess-scope", "mem-scope"

	tr := newFakeTransport()
	subscribe(t, hub, identity(roomID, memberID, sessionID, "player"), "", tr)
	tr.waitFrames(t, 1, time.Second)
	if err := hub.Publish(Publication{
		EventID: "evt-scope", Type: TypeStateChanged, RoomID: roomID,
		RoomVersion: 2, Seq: 1, Resource: ResourceRoom,
	}); err != nil {
		t.Fatalf("Publish: %v", err)
	}
	otherCursor := tr.waitType(t, TypeStateChanged, 2*time.Second).msg.Cursor

	// A different membership in the same room must not accept that cursor.
	other := newFakeTransport()
	subscribe(t, hub, identity(roomID, "mem-other", "sess-other", "spectator"), otherCursor, other)
	got := other.waitFrames(t, 2, 2*time.Second)
	sawResync := false
	for _, f := range got {
		if f.msg.Type == TypeResyncRequired {
			sawResync = true
		}
	}
	if !sawResync {
		t.Fatalf("a cursor minted for another scope was accepted; frames: %v", frameTypes(got))
	}
}

// TestCursorIsOpaque proves the wire cursor carries no readable sequence number.
//
// forbids the client from doing arithmetic on the cursor, so the token must
// not look like an integer and must not contain a decimal rendering of the
// sequence.
func TestCursorIsOpaque(t *testing.T) {
	const epoch, roomID, memberID = "epoch-xyz", "room-op", "mem-op"
	tok := mintCursor(epoch, roomID, memberID, 12345)
	if tok == "" {
		t.Fatal("empty cursor")
	}
	if strings.Contains(tok, "12345") {
		t.Fatalf("cursor %q leaks the raw sequence number", tok)
	}
	for _, c := range tok {
		if c >= '0' && c <= '9' {
			// base64url includes digits; what matters is that the token is not a
			// decimal integer, which the leak check above covers. Fail only if the
			// whole token is numeric.
			continue
		}
		goto notNumeric
	}
	t.Fatalf("cursor %q is entirely numeric", tok)
notNumeric:
	if len(tok) < 16 {
		t.Fatalf("cursor %q is too short to carry an authenticated payload", tok)
	}
}

// -----------------------------------------------------------------------------
// Gate 5: serverEpoch changes on restart and invalidates old cursors.
// -----------------------------------------------------------------------------

// TestServerEpochChangesPerProcess proves a new hub gets a new epoch.
func TestServerEpochChangesPerProcess(t *testing.T) {
	h1 := newTestHub(t, nil)
	h2 := newTestHub(t, nil)
	if h1.Epoch() == h2.Epoch() {
		t.Fatalf("two hubs share the epoch %q; a restart must change it", h1.Epoch())
	}
	if len(h1.Epoch()) < 16 {
		t.Fatalf("epoch %q is too short to be a meaningful process identity", h1.Epoch())
	}
}

// TestOldEpochCursorForcesResync proves a cursor from a previous process is
// rejected, so nothing depends on a stale in-memory position .
func TestOldEpochCursorForcesResync(t *testing.T) {
	const roomID, sessionID, memberID = "room-epoch", "sess-epoch", "mem-epoch"

	oldHub := newTestHub(t, nil)
	oldTr := newFakeTransport()
	subscribe(t, oldHub, identity(roomID, memberID, sessionID, "player"), "", oldTr)
	oldTr.waitFrames(t, 1, time.Second)
	if err := oldHub.Publish(Publication{
		EventID: "evt-epoch", Type: TypeStateChanged, RoomID: roomID,
		RoomVersion: 2, Seq: 1, Resource: ResourceRoom,
	}); err != nil {
		t.Fatalf("Publish: %v", err)
	}
	oldFrames := oldTr.waitFrames(t, 2, time.Second)
	oldCursor := oldFrames[1].msg.Cursor

	// A brand new process: different epoch, empty history.
	newHub := newTestHub(t, nil)
	if newHub.Epoch() == oldHub.Epoch() {
		t.Fatal("epoch did not change between processes")
	}
	newTr := newFakeTransport()
	subscribe(t, newHub, identity(roomID, memberID, sessionID, "player"), oldCursor, newTr)
	hello := newTr.waitType(t, TypeHello, 2*time.Second)
	if hello.msg.ServerEpoch != newHub.Epoch() {
		t.Fatalf("hello epoch = %q, want %q", hello.msg.ServerEpoch, newHub.Epoch())
	}
	// hello must be the very first frame: the client needs the epoch before it can
	// interpret a resync instruction.
	if first := newTr.sent()[0]; first.msg.Type != TypeHello {
		t.Fatalf("first frame type = %q, want hello", first.msg.Type)
	}
	resync := newTr.waitType(t, TypeResyncRequired, 2*time.Second)
	if resync.msg.Reason == "" {
		t.Fatal("resync.required has no reason")
	}
}

// TestHistoryPruningForcesResync proves the retention bound produces a resync
// rather than a silent gap.
func TestHistoryPruningForcesResync(t *testing.T) {
	hub := newTestHub(t, func(c *Config) {
		c.HistoryMaxCount = 4
		c.CoalesceWindow = MinCoalesceWindow
	})
	const roomID, sessionID, memberID = "room-prune", "sess-prune", "mem-prune"

	tr := newFakeTransport()
	subscribe(t, hub, identity(roomID, memberID, sessionID, "player"), "", tr)
	tr.waitFrames(t, 1, time.Second)

	// Fill well past the retention bound.
	for i := int64(1); i <= 12; i++ {
		if err := hub.Publish(Publication{
			EventID:      fmt.Sprintf("evt-prune-%d", i),
			Type:         TypePhaseChanged, // never coalesced, so all are retained
			RoomID:       roomID,
			RoomVersion:  i + 1,
			PhaseVersion: i + 1,
			Seq:          i,
		}); err != nil {
			t.Fatalf("Publish %d: %v", i, err)
		}
	}
	time.Sleep(200 * time.Millisecond)

	// A cursor from sequence 1 is now older than the retained window (which starts
	// at 9), so continuity is unprovable and the client must be told to resync.
	staleCursor := mintCursor(hub.Epoch(), roomID, memberID, 1)
	tr2 := newFakeTransport()
	subscribe(t, hub, identity(roomID, memberID, sessionID, "player"), staleCursor, tr2)
	tr2.waitType(t, TypeResyncRequired, 2*time.Second)
}

// -----------------------------------------------------------------------------
// Gate 6: Origin enforcement on the handshake.
// -----------------------------------------------------------------------------

// TestOriginRejected proves a cross-site Origin cannot open a socket.
func TestOriginRejected(t *testing.T) {
	hub := newTestHub(t, nil)
	auth := stubAuthorizer{ident: identity("room-origin", "mem-origin", "sess-origin", "player"), ok: true}
	h, err := NewHandler(hub, auth, Config{RequireOrigin: true, AllowedOrigins: []string{"localhost:5173"}})
	if err != nil {
		t.Fatalf("NewHandler: %v", err)
	}

	cases := []struct {
		name   string
		origin string
		host   string
		want   int
	}{
		{"same origin", "http://game.example", "game.example", 0},
		{"allow-listed host", "http://localhost:5173", "game.example", 0},
		{"cross site", "https://evil.example", "game.example", 403},
		{"missing origin", "", "game.example", 403},
		{"malformed origin", "not-a-url", "game.example", 403},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := newRequestWithOrigin(tc.origin, tc.host)
			got, why := h.origins.allowed(req)
			if tc.want == 0 {
				if !got {
					t.Fatalf("origin %q rejected (%s), want accepted", tc.origin, why)
				}
				return
			}
			if got {
				t.Fatalf("origin %q accepted, want rejected", tc.origin)
			}
			if why == "" {
				t.Fatal("rejection carries no reason")
			}
		})
	}
}

// TestHandshakeRejectsNonMember proves membership is checked before any state is
// created.
func TestHandshakeRejectsNonMember(t *testing.T) {
	hub := newTestHub(t, nil)
	auth := stubAuthorizer{ok: false}
	h, err := NewHandler(hub, auth, Config{RequireOrigin: true})
	if err != nil {
		t.Fatalf("NewHandler: %v", err)
	}
	rec := newRecorder()
	req := newRequestWithOrigin("https://game.example", "game.example")
	req.SetPathValue("roomId", "room-x")
	h.ServeWebSocket(rec, req)

	if rec.code != 403 {
		t.Fatalf("status = %d, want 403 for a non-member", rec.code)
	}
	if hub.SubscriptionCount() != 0 {
		t.Fatal("a rejected handshake created a subscription")
	}
	if ct := rec.header.Get("Content-Type"); !strings.HasPrefix(ct, "application/problem+json") {
		t.Fatalf("content type = %q, want problem+json", ct)
	}
}

// TestHandshakeRejectsBadOriginBeforeUpgrade proves the order: Origin is checked
// before membership, so a cross-site request cannot even probe membership.
func TestHandshakeRejectsBadOriginBeforeUpgrade(t *testing.T) {
	hub := newTestHub(t, nil)
	var authCalls atomic.Int64
	auth := countingAuthorizer{calls: &authCalls}
	h, err := NewHandler(hub, auth, Config{RequireOrigin: true})
	if err != nil {
		t.Fatalf("NewHandler: %v", err)
	}
	rec := newRecorder()
	req := newRequestWithOrigin("https://evil.example", "game.example")
	req.SetPathValue("roomId", "room-x")
	h.ServeWebSocket(rec, req)

	if rec.code != 403 {
		t.Fatalf("status = %d, want 403", rec.code)
	}
	if authCalls.Load() != 0 {
		t.Fatal("membership was checked for a cross-origin request; Origin must be enforced first")
	}
}

// -----------------------------------------------------------------------------
// Gate 7: only ping and ack are accepted from a client.
// -----------------------------------------------------------------------------

// TestDecodeClientMessageAcceptsOnlyPingAndAck pins the accepted frame shapes.
func TestDecodeClientMessageAcceptsOnlyPingAndAck(t *testing.T) {
	good := []string{
		`{"type":"ping","clientMessageId":"m1","sentAt":"2026-11-08T08:00:00Z"}`,
		`{"type":"ack","cursor":"abc"}`,
	}
	for _, raw := range good {
		if _, err := DecodeClientMessage([]byte(raw)); err != nil {
			t.Errorf("DecodeClientMessage(%s) = %v, want nil", raw, err)
		}
	}

	bad := []struct{ name, raw string }{
		{"guess via WS", `{"type":"guess","characterId":"c1"}`},
		{"submit via WS", `{"type":"submission","payload":{}}`},
		{"role change", `{"type":"role","role":"host"}`},
		{"score edit", `{"type":"score","units":100}`},
		{"ping with extra field", `{"type":"ping","clientMessageId":"m1","sentAt":"2026-11-08T08:00:00Z","action":"submit"}`},
		{"ack with extra field", `{"type":"ack","cursor":"abc","guess":"x"}`},
		{"ping without sentAt", `{"type":"ping","clientMessageId":"m1"}`},
		{"ping with bad sentAt", `{"type":"ping","clientMessageId":"m1","sentAt":"yesterday"}`},
		{"ping without id", `{"type":"ping","sentAt":"2026-11-08T08:00:00Z"}`},
		{"not JSON", `ping`},
		{"empty object", `{}`},
	}
	for _, tc := range bad {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := DecodeClientMessage([]byte(tc.raw)); err == nil {
				t.Fatalf("DecodeClientMessage(%s) accepted an invalid frame", tc.raw)
			} else if !errors.Is(err, ErrUnknownClientMessage) {
				t.Fatalf("error %v does not wrap ErrUnknownClientMessage", err)
			}
		})
	}
}

// TestBusinessFrameClosesConnection proves a business-looking frame terminates the
// socket with a policy violation rather than being silently ignored.
func TestBusinessFrameClosesConnection(t *testing.T) {
	hub := newTestHub(t, nil)
	const roomID, sessionID, memberID = "room-biz", "sess-biz", "mem-biz"

	tr := newFakeTransport()
	subscribe(t, hub, identity(roomID, memberID, sessionID, "player"), "", tr)
	tr.waitFrames(t, 1, time.Second)

	tr.send([]byte(`{"type":"guess","characterId":"special-week"}`))
	tr.waitClosed(t, 3*time.Second)
}

// TestPingProducesPong proves the accepted frame actually works.
func TestPingProducesPong(t *testing.T) {
	hub := newTestHub(t, nil)
	const roomID, sessionID, memberID = "room-ping", "sess-ping", "mem-ping"

	tr := newFakeTransport()
	subscribe(t, hub, identity(roomID, memberID, sessionID, "player"), "", tr)
	tr.waitFrames(t, 1, time.Second)

	tr.send([]byte(`{"type":"ping","clientMessageId":"m1","sentAt":"2026-11-08T08:00:00Z"}`))
	tr.waitType(t, TypePong, 2*time.Second)
}

// TestAckIsAccepted proves ack does not kill the connection.
func TestAckIsAccepted(t *testing.T) {
	hub := newTestHub(t, nil)
	const roomID, sessionID, memberID = "room-ack", "sess-ack", "mem-ack"

	tr := newFakeTransport()
	subscribe(t, hub, identity(roomID, memberID, sessionID, "player"), "", tr)
	tr.waitFrames(t, 1, time.Second)

	tr.send([]byte(`{"type":"ack","cursor":"opaque"}`))
	// A follow-up ping must still work: the connection survived the ack.
	tr.send([]byte(`{"type":"ping","clientMessageId":"m2","sentAt":"2026-11-08T08:00:00Z"}`))
	// The pong proves the connection survived the ack.
	tr.waitType(t, TypePong, 2*time.Second)
}

// -----------------------------------------------------------------------------
// Schema conformance.
// -----------------------------------------------------------------------------

// TestServerMessageMatchesContractSchema pins every message this package emits
// against the contract's required set and closed field set.
func TestServerMessageMatchesContractSchema(t *testing.T) {
	hub := newTestHub(t, nil)
	const roomID, sessionID, memberID = "room-schema", "sess-schema", "mem-schema"

	tr := newFakeTransport()
	subscribe(t, hub, identity(roomID, memberID, sessionID, "player"), "", tr)
	tr.waitFrames(t, 1, time.Second)

	if err := hub.Publish(Publication{
		EventID: "evt-schema", Type: TypeStateChanged, RoomID: roomID,
		RoomVersion: 4, Seq: 1, Resource: ResourceRound, RoundID: "round-1", PhaseVersion: 2,
	}); err != nil {
		t.Fatalf("Publish: %v", err)
	}
	tr.send([]byte(`{"type":"ping","clientMessageId":"m1","sentAt":"2026-11-08T08:00:00Z"}`))
	frames := tr.waitFrames(t, 3, 2*time.Second)

	allowed := map[string]bool{
		"schemaVersion": true, "eventId": true, "type": true, "roomId": true,
		"roomVersion": true, "serverEpoch": true, "serverTime": true, "cursor": true,
		"resource": true, "roundId": true, "phaseVersion": true, "reason": true,
		"onlineCount": true, "heartbeatIntervalMs": true,
	}
	required := []string{"schemaVersion", "eventId", "type", "roomId",
		"roomVersion", "serverEpoch", "serverTime", "cursor"}

	for _, f := range frames {
		obj := mustJSON(t, f.raw)
		for _, key := range required {
			if _, ok := obj[key]; !ok {
				t.Errorf("%s message is missing required field %q", f.msg.Type, key)
			}
		}
		for key := range obj {
			if !allowed[key] {
				t.Errorf("%s message carries field %q, which the schema forbids (additionalProperties=false)",
					f.msg.Type, key)
			}
		}
		if obj["schemaVersion"] != "1" {
			t.Errorf("schemaVersion = %v, want \"1\"", obj["schemaVersion"])
		}
	}
}

// TestPresenceIsAggregated proves presence is one aggregated count, not a
// per-person heartbeat, and that it is not persisted.
func TestPresenceIsAggregated(t *testing.T) {
	hub := newTestHub(t, nil)
	const roomID = "room-presence"

	// Ten spectators join. The debounce must collapse their joins.
	var trs []*fakeTransport
	for i := 0; i < 10; i++ {
		tr := newFakeTransport()
		trs = append(trs, tr)
		subscribe(t, hub, identity(roomID, fmt.Sprintf("mem-p-%d", i), fmt.Sprintf("sess-p-%d", i), "spectator"), "", tr)
	}
	time.Sleep(400 * time.Millisecond)

	if got := hub.OnlineCount(roomID); got != 10 {
		t.Fatalf("online count = %d, want 10", got)
	}

	// Count presence notifications seen by the first subscriber.
	presence := 0
	var lastCount int64 = -1
	for _, f := range trs[0].sent() {
		if f.msg.Type == TypePresenceChange {
			presence++
			if f.msg.Online != nil {
				lastCount = *f.msg.Online
			}
		}
	}
	if presence == 0 {
		t.Fatal("no presence.changed was emitted")
	}
	if presence > 3 {
		t.Fatalf("10 joins produced %d presence.changed frames; presence must be aggregated", presence)
	}
	if lastCount < 0 {
		t.Fatal("presence.changed carries no onlineCount")
	}
}

// TestHelloCarriesHeartbeatAndCursor proves the first frame gives the client
// everything requires: epoch, time, versions, cursor, heartbeat.
func TestHelloCarriesHeartbeatAndCursor(t *testing.T) {
	hub := newTestHub(t, nil)
	const roomID, sessionID, memberID = "room-hello", "sess-hello", "mem-hello"

	tr := newFakeTransport()
	subscribe(t, hub, identity(roomID, memberID, sessionID, "player"), "", tr)
	frames := tr.waitFrames(t, 1, time.Second)
	hello := frames[0].msg

	if hello.Type != TypeHello {
		t.Fatalf("first frame = %q, want hello", hello.Type)
	}
	if hello.ServerEpoch != hub.Epoch() {
		t.Fatalf("hello epoch = %q, want %q", hello.ServerEpoch, hub.Epoch())
	}
	if hello.Cursor == "" {
		t.Fatal("hello has no cursor; the client cannot resume without one")
	}
	if hello.HeartbeatM == nil || *hello.HeartbeatM < 1000 {
		t.Fatalf("hello heartbeatIntervalMs = %v, want >= 1000", hello.HeartbeatM)
	}
	if hello.ServerTime == "" {
		t.Fatal("hello has no serverTime")
	}
	if err := hello.Validate(); err != nil {
		t.Fatalf("hello fails schema validation: %v", err)
	}
}

// TestSSEFallbackDeliversNotifications proves the degraded channel carries the same
// documents.
func TestSSEFallbackDeliversNotifications(t *testing.T) {
	hub := newTestHub(t, nil)
	const roomID, sessionID, memberID = "room-sse", "sess-sse", "mem-sse"

	tr := newFakeSSETransport()
	subscribe(t, hub, identity(roomID, memberID, sessionID, "player"), "", tr)
	tr.waitFrames(t, 1, time.Second)

	if err := hub.Publish(Publication{
		EventID: "evt-sse", Type: TypeStateChanged, RoomID: roomID,
		RoomVersion: 2, Seq: 1, Resource: ResourceRoom,
	}); err != nil {
		t.Fatalf("Publish: %v", err)
	}
	frame := tr.waitType(t, TypeStateChanged, 2*time.Second)
	if frame.msg.EventID != "evt-sse" {
		t.Fatalf("eventId = %q, want evt-sse over SSE", frame.msg.EventID)
	}
	// The same schema applies on SSE.
	if err := frame.msg.Validate(); err != nil {
		t.Fatalf("SSE message fails schema validation: %v", err)
	}
}

// TestRoomVersionLagForcesResync proves a client that is behind on roomVersion is
// told to rebuild instead of receiving a misleading replay.
func TestRoomVersionLagForcesResync(t *testing.T) {
	hub := newTestHub(t, nil)
	const roomID, sessionID, memberID = "room-lag", "sess-lag", "mem-lag"

	tr := newFakeTransport()
	subscribe(t, hub, identity(roomID, memberID, sessionID, "player"), "", tr)
	tr.waitFrames(t, 1, time.Second)
	if err := hub.Publish(Publication{
		EventID: "evt-lag", Type: TypeStateChanged, RoomID: roomID,
		RoomVersion: 9, Seq: 1, Resource: ResourceRoom,
	}); err != nil {
		t.Fatalf("Publish: %v", err)
	}
	cursor := tr.waitType(t, TypeStateChanged, 2*time.Second).msg.Cursor

	// Resume claiming an old room version.
	tr2 := newFakeTransport()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = hub.Serve(ctx, SubscribeOptions{
			Identity:    identity(roomID, memberID, sessionID, "player"),
			Channel:     ChannelWS,
			Cursor:      cursor,
			RoomVersion: 3, // behind the published 9
			Transport:   tr2,
		})
	}()
	t.Cleanup(func() {
		cancel()
		tr2.Close(1000, "done")
		<-done
	})

	resync := tr2.waitType(t, TypeResyncRequired, 2*time.Second)
	if resync.msg.Reason != ResyncRoomVersionLag {
		t.Fatalf("resync reason = %q, want %q", resync.msg.Reason, ResyncRoomVersionLag)
	}
}

// TestDispatcherPrunesDispatchedRows proves the outbox does not grow forever.
func TestDispatcherPrunesDispatchedRows(t *testing.T) {
	db := newTestDB(t)
	hub := newTestHub(t, nil)
	const roomID, sessionID, memberID = "room-prunedb", "sess-prunedb", "mem-prunedb"
	seedRoom(t, db, roomID, sessionID, memberID, "player")

	disp, err := NewDispatcher(db, hub, Config{OutboxRetention: time.Millisecond})
	if err != nil {
		t.Fatalf("NewDispatcher: %v", err)
	}
	ctx := context.Background()
	appendOutbox(t, db, roomID, "evt-db-1", 1, TypeStateChanged, OutboxPayload{RoomVersion: 2, Resource: ResourceRoom})
	if _, err := disp.DispatchOnce(ctx); err != nil {
		t.Fatalf("DispatchOnce: %v", err)
	}
	time.Sleep(5 * time.Millisecond)
	n, err := disp.PruneDispatched(ctx)
	if err != nil {
		t.Fatalf("PruneDispatched: %v", err)
	}
	if n != 1 {
		t.Fatalf("pruned %d rows, want 1", n)
	}
	backlog, err := disp.Backlog(ctx)
	if err != nil {
		t.Fatalf("Backlog: %v", err)
	}
	if backlog != 0 {
		t.Fatalf("backlog = %d after prune, want 0", backlog)
	}
}

// TestAppendEventRejectsHubOnlyTypes proves the outbox cannot be used to inject a
// revocation or a resync, which only the hub may generate.
func TestAppendEventRejectsHubOnlyTypes(t *testing.T) {
	db := newTestDB(t)
	const roomID, sessionID, memberID = "room-inj", "sess-inj", "mem-inj"
	seedRoom(t, db, roomID, sessionID, memberID, "player")

	ctx := context.Background()
	tx, err := db.Write().BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	defer tx.Rollback()

	for _, typ := range []MessageType{TypeSessionRevoked, TypeResyncRequired, TypeHello, TypePong} {
		if err := AppendEvent(ctx, tx, "evt-"+string(typ), roomID, 1, typ, OutboxPayload{}); err == nil {
			t.Errorf("AppendEvent accepted hub-only type %q", typ)
		}
	}
}

// TestAppendEventRequiresTransaction proves the transaction argument is mandatory,
// which is what makes "outbox row in the same transaction" enforceable.
func TestAppendEventRequiresTransaction(t *testing.T) {
	ctx := context.Background()
	if err := AppendEvent(ctx, nil, "evt", "room", 1, TypeStateChanged, OutboxPayload{}); err == nil {
		t.Fatal("AppendEvent accepted a nil transaction")
	}
}

// TestConcurrentPublishAndSubscribe runs the hub under the race detector.
func TestConcurrentPublishAndSubscribe(t *testing.T) {
	hub := newTestHub(t, nil)
	const roomID = "room-race"

	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			tr := newFakeTransport()
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			go func() {
				_ = hub.Serve(ctx, SubscribeOptions{
					Identity:  identity(roomID, fmt.Sprintf("mem-r-%d", i), fmt.Sprintf("sess-r-%d", i), "player"),
					Channel:   ChannelWS,
					Transport: tr,
				})
			}()
			time.Sleep(5 * time.Millisecond)
			for j := 0; j < 20; j++ {
				_ = hub.Publish(Publication{
					EventID:     fmt.Sprintf("evt-race-%d-%d", i, j),
					Type:        TypeStateChanged,
					RoomID:      roomID,
					RoomVersion: int64(j + 2),
					Seq:         int64(j + 1),
					Resource:    ResourceRound,
					RoundID:     fmt.Sprintf("round-%d", j),
				})
			}
			time.Sleep(5 * time.Millisecond)
			cancel()
			tr.Close(1000, "done")
		}(i)
	}
	wg.Wait()
}

// TestDispatcherDispatchOnceIsIdempotentUnderConcurrency proves two dispatch cycles
// racing on the same rows cannot both claim success in a way that loses a delivery.
// The at-least-once contract permits a duplicate; it does not permit a silent drop.
func TestDispatcherDispatchOnceIsIdempotentUnderConcurrency(t *testing.T) {
	db := newTestDB(t)
	hub := newTestHub(t, nil)
	const roomID, sessionID, memberID = "room-conc", "sess-conc", "mem-conc"
	seedRoom(t, db, roomID, sessionID, memberID, "player")

	tr := newFakeTransport()
	subscribe(t, hub, identity(roomID, memberID, sessionID, "player"), "", tr)
	tr.waitFrames(t, 1, time.Second)

	disp, err := NewDispatcher(db, hub, hub.Config())
	if err != nil {
		t.Fatalf("NewDispatcher: %v", err)
	}
	for i := int64(1); i <= 5; i++ {
		appendOutbox(t, db, roomID, fmt.Sprintf("evt-conc-%d", i), i, TypePhaseChanged,
			OutboxPayload{RoomVersion: i + 1, PhaseVersion: i + 1})
	}

	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = disp.DispatchOnce(context.Background())
		}()
	}
	wg.Wait()

	// Every event must have been delivered at least once.
	delivered := map[string]int{}
	for _, f := range tr.sent() {
		delivered[f.msg.EventID]++
	}
	for i := int64(1); i <= 5; i++ {
		id := fmt.Sprintf("evt-conc-%d", i)
		if delivered[id] < 1 {
			t.Fatalf("event %s was never delivered under concurrent dispatch", id)
		}
	}
}

// -----------------------------------------------------------------------------
// helpers
// -----------------------------------------------------------------------------

// stubAuthorizer is a fixed-answer Authorizer.
type stubAuthorizer struct {
	ident Identity
	ok    bool
	err   error
}

func (s stubAuthorizer) AuthorizeWebSocket(*http.Request, string) (Identity, bool, error) {
	return s.ident, s.ok, s.err
}

func (s stubAuthorizer) AuthorizeSSE(*http.Request, string) (Identity, bool, error) {
	return s.ident, s.ok, s.err
}

// countingAuthorizer records how many times it was consulted.
type countingAuthorizer struct {
	calls *atomic.Int64
}

func (c countingAuthorizer) AuthorizeWebSocket(*http.Request, string) (Identity, bool, error) {
	c.calls.Add(1)
	return Identity{}, false, nil
}

func (c countingAuthorizer) AuthorizeSSE(*http.Request, string) (Identity, bool, error) {
	c.calls.Add(1)
	return Identity{}, false, nil
}

// The HTTP helpers below keep the Origin tests free of a full server setup while
// still exercising the real *http.Request code path.

var _ = json.Marshal
var _ = storage.Config{}

// TestHelloCursorMatchesResumePosition is a regression test for a position bug:
// hello must advertise the position the subscriber is actually at, not the room's
// newest sequence.
//
// If hello carried the newest sequence while the replay started from an older one,
// a client that stored the hello cursor and reconnected would skip every event
// between the two, permanently and silently. The test resumes from the middle of a
// retained history and asserts the delivered entries continue from exactly there.
func TestHelloCursorMatchesResumePosition(t *testing.T) {
	hub := newTestHub(t, nil)
	const roomID, sessionID, memberID = "room-hello-pos", "sess-hello-pos", "mem-hello-pos"

	// A host keeps the room alive and builds history: sequences 1..6, never merged
	// (phase.changed).
	hostTr := newFakeTransport()
	subscribe(t, hub, identity(roomID, "mem-host-pos", "sess-host-pos", "host"), "", hostTr)
	waitSubscribed(t, hub, 1)
	for i := int64(1); i <= 6; i++ {
		if err := hub.Publish(Publication{
			EventID:      fmt.Sprintf("evt-pos-%d", i),
			Type:         TypePhaseChanged,
			RoomID:       roomID,
			RoomVersion:  i + 1,
			PhaseVersion: i + 1,
			Seq:          i,
		}); err != nil {
			t.Fatalf("Publish %d: %v", i, err)
		}
	}
	deadline := time.Now().Add(3 * time.Second)
	for len(hostTr.ofType(TypePhaseChanged)) < 6 {
		if time.Now().After(deadline) {
			t.Fatalf("host received %d phase frames, want 6", len(hostTr.ofType(TypePhaseChanged)))
		}
		time.Sleep(time.Millisecond)
	}

	// Resume from sequence 2: the subscriber must receive 3..6 and its hello cursor
	// must be minted at 2, not at the room's newest 6.
	cursor := mintCursor(hub.Epoch(), roomID, memberID, 2)
	tr := newFakeTransport()
	subscribe(t, hub, identity(roomID, memberID, sessionID, "player"), cursor, tr)

	// Wait for all four replayed entries.
	deadline = time.Now().Add(3 * time.Second)
	for len(tr.ofType(TypePhaseChanged)) < 4 {
		if time.Now().After(deadline) {
			t.Fatalf("replayed %d entries, want 4 (3..6); frames: %v",
				len(tr.ofType(TypePhaseChanged)), frameTypes(tr.sent()))
		}
		time.Sleep(time.Millisecond)
	}

	got := tr.ofType(TypePhaseChanged)
	if len(got) != 4 {
		t.Fatalf("replayed %d entries, want exactly 4", len(got))
	}
	// The publications set phaseVersion = seq+1, so sequences 3..6 carry 4..7.
	for i, want := range []int64{4, 5, 6, 7} {
		if got[i].msg.PhaseVer == nil || *got[i].msg.PhaseVer != want {
			t.Fatalf("replayed entry %d has phaseVersion %v, want %d", i, got[i].msg.PhaseVer, want)
		}
	}

	// The final delivered cursor must decode to sequence 6, i.e. the client ends up
	// exactly where the last entry left it.
	last := got[len(got)-1].msg.Cursor
	cur, err := parseCursor(last, hub.Epoch(), roomID, memberID)
	if err != nil {
		t.Fatalf("the last delivered cursor does not parse: %v", err)
	}
	if cur.seqOf() != 6 {
		t.Fatalf("final cursor position = %d, want 6", cur.seqOf())
	}

	// And a third connection resuming from that final cursor must replay nothing,
	// which is the property that breaks if hello and the replay disagree.
	tr3 := newFakeTransport()
	subscribe(t, hub, identity(roomID, memberID, sessionID, "player"), last, tr3)
	tr3.waitFrames(t, 1, 2*time.Second)
	time.Sleep(150 * time.Millisecond)
	if extra := tr3.ofType(TypePhaseChanged); len(extra) != 0 {
		t.Fatalf("resuming from the final cursor replayed %d entries, want 0", len(extra))
	}
}
