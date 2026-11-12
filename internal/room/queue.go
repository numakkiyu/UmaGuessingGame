package room

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/bhcn/umaguessinggame/internal/httpapi/problem"
)

// Dispatcher serialises commands per room through a bounded queue.
//
// Why a per-room queue rather than a global lock :
//
// - receivedAt must be assigned by the server at the moment the command
// enters the queue, and that timestamp decides whether the command beat the
// deadline. Assigning it after waiting for a global lock would make the
// deadline depend on unrelated rooms' load.
// - A single global mutex would serialise content computation and network
// work across rooms.
// - The queue must be bounded and its overflow behaviour deterministic: the
// caller receives 503 with Retry-After and nothing is silently dropped.
//
// A room's commands run one at a time, which gives ordering; SQL constraints
// still guard every invariant, because a queue is not a substitute for them.
type Dispatcher struct {
	capacity   int
	wait       time.Duration
	maxPending int
	mu         sync.Mutex
	rooms      map[string]*roomQueue
	pending    int
	closed     bool
	done       chan struct{}
}

type roomQueue struct {
	active  *queuedCommand
	waiting []*queuedCommand
}
type queuedCommand struct{ ready chan struct{} }

// NewDispatcher preserves the default global admission budget.
func NewDispatcher(capacity int, wait time.Duration) *Dispatcher {
	return NewDispatcherWithLimit(capacity, wait, 4096)
}

// NewDispatcherWithLimit bounds all admitted commands as well as each room.
func NewDispatcherWithLimit(capacity int, wait time.Duration, maxPending int) *Dispatcher {
	if capacity <= 0 {
		capacity = 128
	}
	if wait <= 0 {
		wait = 5 * time.Second
	}
	if maxPending <= 0 {
		maxPending = 4096
	}
	return &Dispatcher{capacity: capacity, wait: wait, maxPending: maxPending, rooms: make(map[string]*roomQueue), done: make(chan struct{})}
}
func (d *Dispatcher) Capacity() int { return d.capacity }
func (d *Dispatcher) Pending(roomID string) int {
	d.mu.Lock()
	defer d.mu.Unlock()
	q := d.rooms[roomID]
	if q == nil {
		return 0
	}
	n := len(q.waiting)
	if q.active != nil {
		n++
	}
	return n
}

// Close stops new admission and immediately wakes queued callers. Running
// commands finish normally and release their own permits.
func (d *Dispatcher) Close() {
	d.mu.Lock()
	defer d.mu.Unlock()
	if !d.closed {
		d.closed = true
		close(d.done)
	}
}

// Enqueue gives each room a FIFO queue. The receipt time is assigned on
// admission, before any wait, and cancellation cannot strand the next caller.
func (d *Dispatcher) Enqueue(ctx context.Context, roomID string) (time.Time, func(), error) {
	if err := ctx.Err(); err != nil {
		return time.Time{}, nil, problem.Busy(1).WithCause(err)
	}
	// The empty key is reserved by the deadline sweeper's serialised batch.
	if len(roomID) > 160 {
		return time.Time{}, nil, problem.Busy(1)
	}
	d.mu.Lock()
	q := d.rooms[roomID]
	roomPending := 0
	if q != nil {
		roomPending = len(q.waiting)
		if q.active != nil {
			roomPending++
		}
	}
	if d.closed || d.pending >= d.maxPending || roomPending >= d.capacity {
		d.mu.Unlock()
		return time.Time{}, nil, problem.Busy(1)
	}
	if q == nil {
		q = &roomQueue{}
		d.rooms[roomID] = q
	}
	command := &queuedCommand{ready: make(chan struct{})}
	d.pending++
	received := time.Now().UTC()
	if q.active == nil {
		q.active = command
		close(command.ready)
	} else {
		q.waiting = append(q.waiting, command)
	}
	d.mu.Unlock()
	var once sync.Once
	release := func() {
		once.Do(func() {
			d.mu.Lock()
			defer d.mu.Unlock()
			if q.active == command {
				q.active = nil
				if len(q.waiting) > 0 {
					q.active = q.waiting[0]
					q.waiting = q.waiting[1:]
					close(q.active.ready)
				}
			} else {
				for i, w := range q.waiting {
					if w == command {
						q.waiting = append(q.waiting[:i], q.waiting[i+1:]...)
						break
					}
				}
			}
			d.pending--
			if q.active == nil && len(q.waiting) == 0 {
				delete(d.rooms, roomID)
			}
		})
	}
	timer := time.NewTimer(d.wait)
	defer timer.Stop()
	select {
	case <-command.ready:
		if err := ctx.Err(); err != nil {
			release()
			return time.Time{}, nil, problem.Busy(1).WithCause(err)
		}
		d.mu.Lock()
		closed := d.closed
		d.mu.Unlock()
		if closed {
			release()
			return time.Time{}, nil, problem.Busy(1).WithCause(ErrQueueClosed)
		}
		return received, release, nil
	case <-timer.C:
		release()
		return time.Time{}, nil, problem.Busy(1)
	case <-ctx.Done():
		release()
		return time.Time{}, nil, problem.Busy(1).WithCause(ctx.Err())
	case <-d.done:
		release()
		return time.Time{}, nil, problem.Busy(1).WithCause(ErrQueueClosed)
	}
}

// newID returns a prefixed opaque identifier from a cryptographic source.
func newID(prefix string) (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", fmt.Errorf("room: random id: %w", err)
	}
	return prefix + hex.EncodeToString(b[:]), nil
}

// ErrQueueClosed reports use of a closed dispatcher.
var ErrQueueClosed = errors.New("room: dispatcher closed")
