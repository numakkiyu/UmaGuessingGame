package realtime

import (
	"sync"
	"time"
)

// messageClass decides how one notification competes for space in an outbound
// queue.
//
// draws the line that matters here: "慢客户端合并可替代的通知；不能丢终态/
// 撤权语义而不告知". So a message is either
//
// - critical: must reach the peer or the connection must die, because losing it
// would silently lose a terminal phase, a revocation or a resync instruction;
// - immediate: delivered without waiting for the merge window, but expendable
// under pressure (pong: the client measures RTT itself and will ping again);
// - windowed: an ordinary state notification, merged inside the window and the
// first thing sacrificed when the peer cannot keep up, because a snapshot
// rebuild reproduces it exactly.
type messageClass int

const (
	classCritical messageClass = iota
	classImmediate
	classWindowed
)

// classify maps a message type onto its delivery class.
//
// phase.changed is critical rather than windowed on purpose: a phase transition is
// the one ordinary-looking notification that carries terminal semantics (locked,
// revealed, settled, cancelled). Merging two of them could drop the transition
// that ends the round, which is exactly the silent loss forbids.
func classify(t MessageType) messageClass {
	switch t {
	case TypeHello, TypePhaseChanged, TypeResyncRequired, TypeSessionRevoked:
		return classCritical
	case TypePong:
		return classImmediate
	case TypeStateChanged, TypePresenceChange:
		return classWindowed
	default:
		// Unknown types cannot be constructed through the public helpers; treat
		// them as critical so a bug surfaces as backpressure instead of silence.
		return classCritical
	}
}

// replaceKey identifies notifications that supersede each other inside one
// connection.
//
// A newer state.changed for the same resource and round makes an older one
// pointless: the client is going to re-fetch that snapshot anyway. presence.changed
// is a pure aggregate, so only the newest count matters. Returning "" means the
// message is not substitutable.
func replaceKey(m ServerMessage) string {
	switch m.Type {
	case TypeStateChanged:
		return "state:" + string(m.Resource) + ":" + m.RoundID
	case TypePresenceChange:
		return "presence"
	case TypePong:
		return "pong"
	default:
		return ""
	}
}

// queued is one frame waiting to be written.
type queued struct {
	msg   ServerMessage
	frame []byte
	class messageClass
	key   string
}

// enqueueResult tells the caller what the queue did with a message.
type enqueueResult int

const (
	// enqueueOK means the message is queued (possibly after replacing an older
	// equivalent one).
	enqueueOK enqueueResult = iota
	// enqueueNeedResync means the peer is too far behind to receive ordinary
	// notifications; the caller must send resync.required and may then continue.
	enqueueNeedResync
	// enqueueNeedClose means even a critical message does not fit; the connection
	// must be closed so the client reconnects and rebuilds from a snapshot.
	enqueueNeedClose
)

// outboundStats exposes what happened to one connection, for tests and metrics.
type outboundStats struct {
	Queued      int64
	Coalesced   int64
	Evicted     int64
	Overflow    int64
	CloseNeeded int64
}

// outbound is one connection's bounded send queue.
//
// The bounds are 64 messages and 256 KiB . Both the ready queue and
// the merge window count toward the bound, because a message sitting in the merge
// window is just as undelivered as one sitting in the queue.
//
// Locking: one mutex covers the queue, the merge window and the timer. The writer
// goroutine only ever holds it for the duration of a pop, so a slow socket write
// never blocks a publisher. That separation is the whole point: the producer path
// must stay non-blocking, otherwise one unresponsive browser would stall the
// dispatcher for every room in the process.
type outbound struct {
	mu       sync.Mutex
	window   time.Duration
	maxMsgs  int
	maxBytes int

	queue []queued
	bytes int

	// inFlight counts frames that have been popped but whose write has not
	// completed. "The queue is empty" is not the same as "everything was
	// delivered": the writer may be blocked inside a socket write, and a caller
	// that needs a frame to actually reach the wire (revocation) must wait for this
	// counter too.
	inFlight int

	// pending holds at most one message per replace key while the window is open.
	pending      map[string]queued
	pendingBytes int
	timer        *time.Timer

	wake   chan struct{}
	closed bool

	// resyncOutstanding records that a resync.required is already queued and not yet
	// written. Without it, a peer that overflows repeatedly would queue one resync per
	// overflow, filling the very queue the resync is meant to relieve and eventually
	// forcing a close. One outstanding instruction is sufficient: the client is going
	// to rebuild from a snapshot either way.
	resyncOutstanding bool

	stats outboundStats
}

func newOutbound(window time.Duration, maxMsgs, maxBytes int) *outbound {
	return &outbound{
		window:   window,
		maxMsgs:  maxMsgs,
		maxBytes: maxBytes,
		pending:  make(map[string]queued, 8),
		wake:     make(chan struct{}, 1),
	}
}

// total and totalBytes count everything undelivered.
func (o *outbound) total() int { return len(o.queue) + len(o.pending) }

func (o *outbound) totalBytes() int { return o.bytes + o.pendingBytes }

// enqueue offers one frame to the queue.
//
// frame must be the already-encoded message. It is passed in rather than produced
// here so that encoding and validation happen once, in the caller, before any lock
// is taken.
func (o *outbound) enqueue(m ServerMessage, frame []byte) enqueueResult {
	class := classify(m.Type)
	key := replaceKey(m)

	o.mu.Lock()
	defer o.mu.Unlock()
	if o.closed {
		return enqueueNeedClose
	}

	switch class {
	case classWindowed:
		if key != "" {
			if prev, ok := o.pending[key]; ok {
				// Same scope already waiting: keep the newest, count the merge.
				o.pendingBytes += len(frame) - len(prev.frame)
				o.pending[key] = queued{msg: m, frame: frame, class: class, key: key}
				o.stats.Coalesced++
				return enqueueOK
			}
		}
		if o.fitsLocked(len(frame)) {
			o.pending[key] = queued{msg: m, frame: frame, class: class, key: key}
			o.pendingBytes += len(frame)
			o.armLocked()
			return enqueueOK
		}
		// The peer cannot absorb an ordinary notification. Drop the substitutable
		// backlog and escalate to a resync: a snapshot rebuild is both cheaper and
		// strictly more correct than a pile of notifications.
		o.evictSubstitutableLocked()
		o.stats.Overflow++
		return enqueueNeedResync

	case classImmediate:
		if key != "" {
			if i := o.findQueuedLocked(key); i >= 0 {
				// A queued pong is as good as this one.
				o.stats.Coalesced++
				return enqueueOK
			}
		}
		if o.fitsLocked(len(frame)) {
			o.queue = append(o.queue, queued{msg: m, frame: frame, class: class, key: key})
			o.bytes += len(frame)
			o.signalLocked()
			return enqueueOK
		}
		// A pong is expendable: the client will ping again and the protocol-level
		// ping/pong still proves liveness.
		o.stats.Evicted++
		return enqueueOK

	default: // classCritical
		if m.Type == TypeResyncRequired {
			if o.resyncOutstanding {
				o.stats.Coalesced++
				return enqueueOK
			}
		}
		if o.fitsLocked(len(frame)) {
			o.queue = append(o.queue, queued{msg: m, frame: frame, class: class, key: key})
			o.bytes += len(frame)
			if m.Type == TypeResyncRequired {
				o.resyncOutstanding = true
			}
			o.signalLocked()
			return enqueueOK
		}
		// Make room by discarding substitutable traffic. Terminal and revocation
		// notifications outrank ordinary state updates, always.
		o.evictSubstitutableLocked()
		if o.fitsLocked(len(frame)) {
			o.queue = append(o.queue, queued{msg: m, frame: frame, class: class, key: key})
			o.bytes += len(frame)
			if m.Type == TypeResyncRequired {
				o.resyncOutstanding = true
			}
			o.signalLocked()
			return enqueueOK
		}
		o.stats.CloseNeeded++
		return enqueueNeedClose
	}
}

// fitsLocked reports whether one more frame of n bytes is within both bounds.
func (o *outbound) fitsLocked(n int) bool {
	return o.total() < o.maxMsgs && o.totalBytes()+n <= o.maxBytes
}

// findQueuedLocked returns the index of a queued frame with the given key.
func (o *outbound) findQueuedLocked(key string) int {
	for i := range o.queue {
		if o.queue[i].key == key {
			return i
		}
	}
	return -1
}

// evictSubstitutableLocked drops every windowed message and every substitutable
// queued message. Critical frames are never removed.
func (o *outbound) evictSubstitutableLocked() {
	removed := len(o.pending)
	for k, q := range o.pending {
		o.pendingBytes -= len(q.frame)
		delete(o.pending, k)
	}
	kept := o.queue[:0]
	o.bytes = 0
	for _, q := range o.queue {
		if q.class == classWindowed {
			removed++
			continue
		}
		kept = append(kept, q)
		o.bytes += len(q.frame)
	}
	o.queue = kept
	o.stats.Evicted += int64(removed)
}

// armLocked starts the merge window if a windowed message is waiting and no timer
// is running.
func (o *outbound) armLocked() {
	if o.timer != nil || len(o.pending) == 0 {
		return
	}
	o.timer = time.AfterFunc(o.window, o.flushPending)
}

// flushPending moves merged messages into the ready queue when the window closes.
func (o *outbound) flushPending() {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.timer = nil
	if o.closed {
		return
	}
	for k, q := range o.pending {
		if !o.fitsLocked(len(q.frame)) {
			// The peer fell behind inside the window. Keep escalating rather than
			// exceeding the bound; the subscription turns this into resync.required.
			o.stats.Overflow++
			delete(o.pending, k)
			o.pendingBytes -= len(q.frame)
			continue
		}
		o.queue = append(o.queue, q)
		o.bytes += len(q.frame)
		o.pendingBytes -= len(q.frame)
		delete(o.pending, k)
	}
	if len(o.queue) > 0 {
		o.signalLocked()
	}
}

// signalLocked wakes the writer without blocking.
func (o *outbound) signalLocked() {
	select {
	case o.wake <- struct{}{}:
	default:
	}
}

// pop takes the next frame to write. It blocks until one is available, the
// connection closes, or the context is done.
//
// The returned stats snapshot lets the caller report queue pressure without
// touching the lock again.
func (o *outbound) pop(done <-chan struct{}) (queued, bool) {
	for {
		o.mu.Lock()
		if len(o.queue) > 0 {
			q := o.queue[0]
			o.queue = o.queue[1:]
			o.bytes -= len(q.frame)
			if q.msg.Type == TypeResyncRequired {
				o.resyncOutstanding = false
			}
			o.inFlight++
			o.mu.Unlock()
			return q, true
		}
		closed := o.closed
		o.mu.Unlock()
		if closed {
			return queued{}, false
		}
		select {
		case <-o.wake:
		case <-done:
			// One last drain attempt: a frame enqueued just before done must not be
			// lost if the caller is closing deliberately (revocation).
			o.mu.Lock()
			if len(o.queue) == 0 {
				o.mu.Unlock()
				return queued{}, false
			}
			o.mu.Unlock()
		}
	}
}

// finishWrite records that one popped frame has been written (or failed). It must
// be called exactly once per successful pop.
func (o *outbound) finishWrite() {
	o.mu.Lock()
	if o.inFlight > 0 {
		o.inFlight--
	}
	o.mu.Unlock()
}

// drained reports whether every accepted frame has been written.
func (o *outbound) drained() bool {
	o.mu.Lock()
	defer o.mu.Unlock()
	return len(o.queue) == 0 && o.inFlight == 0
}

// close marks the queue closed and stops the timer. The caller signals done
// separately so that pop unblocks.
func (o *outbound) close() {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.closed {
		return
	}
	o.closed = true
	if o.timer != nil {
		o.timer.Stop()
		o.timer = nil
	}
	o.pending = make(map[string]queued, 0)
	o.pendingBytes = 0
	o.resyncOutstanding = false
}

// snapshot returns a copy of the counters.
func (o *outbound) snapshot() outboundStats {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.stats
}
