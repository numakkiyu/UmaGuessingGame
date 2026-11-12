package realtime

import (
	"sync"
	"time"
)

// Audience is the server-side visibility filter of one published event.
//
// This is the mechanism behind the "private filtering creates legitimate gaps"
// statement in A guess submission is visible to the submitting player, the
// host and the display; it is not visible to other players until reveal. Rather
// than write a per-recipient notification for every recipient, the publisher
// records the filter once and the hub applies it per subscriber.
//
// Audience is never serialised to a client: it lives in the outbox payload, which
// stays on the server, and it is deliberately absent from ServerMessage because
// the schema forbids additional properties.
type Audience struct {
	// Roles, when non-empty, restricts delivery to subscribers holding one of
	// these membership roles (player, team-captain, spectator, host, display).
	Roles []string `json:"roles,omitempty"`
	// MembershipIDs, when non-empty, restricts delivery to these memberships.
	MembershipIDs []string `json:"membershipIds,omitempty"`
	// ExcludeRoles removes roles from the recipient set after the include rules.
	ExcludeRoles []string `json:"excludeRoles,omitempty"`
}

// allows reports whether the audience admits one subscriber.
//
// An entirely empty audience means "everyone in the room", which is the common
// case for room-level state changes.
func (a Audience) allows(role, membershipID string) bool {
	if len(a.MembershipIDs) > 0 && !containsString(a.MembershipIDs, membershipID) {
		return false
	}
	if len(a.Roles) > 0 && !containsString(a.Roles, role) {
		return false
	}
	if len(a.ExcludeRoles) > 0 && containsString(a.ExcludeRoles, role) {
		return false
	}
	return true
}

func containsString(list []string, v string) bool {
	for _, s := range list {
		if s == v {
			return true
		}
	}
	return false
}

// historyEntry is one retained notification, in the form needed to replay it to a
// different subscriber later.
//
// The message is stored as a template: everything except cursor is already final,
// because those fields describe the event, not the recipient. cursor is
// per-recipient (it is scope-bound), so it is minted at replay time.
type historyEntry struct {
	seq      int64
	at       time.Time
	tmpl     ServerMessage
	audience Audience
}

// history is the bounded per-room replay buffer (: last 10 minutes or 10,000
// notifications per room, whichever bound is reached first).
//
// It is a plain slice with front trimming. 10,000 entries of ~200 bytes is about
// 2 MiB per room, which is acceptable for the documented baseline of one active
// room per instance; the pruning below keeps it from growing past that. A ring
// buffer would save the occasional copy on trim, which is not worth the added
// index arithmetic for a bound this small.
type history struct {
	maxAge   time.Duration
	maxCount int

	// mu guards entries. Publishing (append) and resuming (replay) run on
	// different goroutines, so the buffer needs its own lock; it is never held
	// while a socket is written to.
	mu      sync.Mutex
	entries []historyEntry
}

func newHistory(maxAge time.Duration, maxCount int) *history {
	return &history{maxAge: maxAge, maxCount: maxCount}
}

// append records one entry and prunes.
func (h *history) append(e historyEntry, now time.Time) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.entries = append(h.entries, e)
	h.prune(now)
}

// prune drops entries that fell out of the age window, then trims the oldest
// entries until the count bound holds.
func (h *history) prune(now time.Time) {
	if len(h.entries) == 0 {
		return
	}
	cutoff := now.Add(-h.maxAge)
	drop := 0
	for drop < len(h.entries) && h.entries[drop].at.Before(cutoff) {
		drop++
	}
	if drop > 0 {
		h.entries = append(h.entries[:0], h.entries[drop:]...)
	}
	if over := len(h.entries) - h.maxCount; over > 0 {
		h.entries = append(h.entries[:0], h.entries[over:]...)
	}
}

// replayResult is the outcome of a resume attempt.
type replayResult struct {
	entries []historyEntry
	// gap reports that continuity could not be proven, so the caller must send
	// resync.required instead of replaying. It never reports a gap merely because
	// sequence numbers are non-consecutive: gaps are the normal consequence of
	// private filtering and are not evidence of loss .
	gap bool
}

// replay returns the entries a subscriber with afterSeq has not yet been told
// about, filtered by that subscriber's audience.
//
// Continuity rules:
//
// - afterSeq == 0 means the client states it has no position. A fresh connection
// is given a baseline cursor by the caller, so reaching here with 0 means the
// caller really wants everything retained; that is still bounded by maxReplay.
// - afterSeq below the oldest retained sequence minus one means the buffer has
// pruned past the client, so continuity is unprovable.
// - afterSeq above the newest retained sequence is a cursor from the future;
// authentication already makes that near-impossible, so treat it as a gap
// rather than trusting it.
//
// Non-consecutive retained sequences are not treated as a gap. The outbox sequence
// is per room and monotonic, but a subscriber filtered out of events legitimately
// observes 4, 6, 9 — the client must not infer loss from that, and neither does
// this function.
func (h *history) replay(afterSeq int64, maxReplay int, allow func(historyEntry) bool) replayResult {
	h.mu.Lock()
	defer h.mu.Unlock()
	if len(h.entries) == 0 {
		if afterSeq > 0 {
			// The room has no retained history at all but the client claims a
			// position. The buffer was pruned to empty or the client's position is
			// from another room; either way a snapshot rebuild is the safe answer.
			return replayResult{gap: true}
		}
		return replayResult{}
	}

	oldest := h.entries[0].seq
	newest := h.entries[len(h.entries)-1].seq
	if afterSeq < oldest-1 {
		return replayResult{gap: true}
	}
	if afterSeq > newest {
		return replayResult{gap: true}
	}
	if afterSeq == newest {
		return replayResult{}
	}

	out := make([]historyEntry, 0, 8)
	for _, e := range h.entries {
		if e.seq <= afterSeq {
			continue
		}
		if allow != nil && !allow(e) {
			continue
		}
		out = append(out, e)
		if maxReplay > 0 && len(out) > maxReplay {
			// More pending notifications than the client can usefully absorb in one
			// reconnect. A snapshot is cheaper and strictly more correct than a flood
			// of notifications each followed by a debounced GET.
			return replayResult{gap: true}
		}
	}
	return replayResult{entries: out}
}
