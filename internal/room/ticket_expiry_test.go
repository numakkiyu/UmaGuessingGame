package room_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/bhcn/umaguessinggame/internal/modes"
	"github.com/bhcn/umaguessinggame/internal/room"
)

// TestExpiredTicketStopsBlockingNewOnes pins that a lapsed ticket is actually
// retired rather than merely filtered out of queries.
//
// Expiry used to be a read-time interpretation only: no code ever wrote the
// 'expired' status, and the active-ticket check did not look at expires_at. A ticket
// whose window had passed therefore stayed 'queueing' forever, the partial unique
// index still counted it, and the same session could never queue again without
// cancelling by hand. A player who closed the tab was locked out of matchmaking for
// good — and the client's "match expired" branch could never fire, because the server
// never reported that status.
func TestExpiredTicketStopsBlockingNewOnes(t *testing.T) {
	t.Parallel()
	db := newDB(t)
	reg, err := modes.NewRegistry(&sharedStub{id: "classic"})
	if err != nil {
		t.Fatalf("registry: %v", err)
	}
	svc := room.New(db, room.Config{}, room.Options{Modes: reg, Epoch: "test"})
	defer svc.Close()
	ctx := context.Background()

	if _, err := db.Write().ExecContext(ctx, `
		INSERT INTO sessions (id, token_hash, display_name, csrf_token_hash, kind, created_at, expires_at)
		VALUES ('ses_q', 'h', 'queueing', 'c', 'player', 0, 9999999999999)`); err != nil {
		t.Fatalf("seed session: %v", err)
	}

	in := room.CreateTicketInput{ModeConfig: json.RawMessage(`{"modeId":"classic","config":{}}`)}
	first, err := svc.CreateMatchmakingTicket(ctx, "ses_q", in)
	if err != nil {
		t.Fatalf("first ticket: %v", err)
	}

	// A second ticket while the first is live must be refused.
	if _, err := svc.CreateMatchmakingTicket(ctx, "ses_q", in); err == nil {
		t.Fatal("a live ticket must block a second one")
	}

	// Push the ticket into the past, exactly as time would.
	if _, err := db.Write().ExecContext(ctx,
		`UPDATE matchmaking_tickets SET expires_at = ? WHERE id = ?`,
		time.Now().Add(-time.Hour).UnixMilli(), first.ID); err != nil {
		t.Fatalf("expire ticket: %v", err)
	}

	// The old ticket must be retired, not left in an active status.
	var status string
	if err := db.Read().QueryRowContext(ctx,
		`SELECT status FROM matchmaking_tickets WHERE id = ?`, first.ID).Scan(&status); err != nil {
		t.Fatalf("read status: %v", err)
	}

	// Queuing again is the behaviour that matters: without it the player is stuck.
	second, err := svc.CreateMatchmakingTicket(ctx, "ses_q", in)
	if err != nil {
		t.Fatalf("a lapsed ticket must not block a new one: %v", err)
	}
	if second.ID == first.ID {
		t.Error("the new ticket must be a distinct row")
	}

	// And the stale row must now report itself as expired, so the client's terminal
	// branch can fire.
	if err := db.Read().QueryRowContext(ctx,
		`SELECT status FROM matchmaking_tickets WHERE id = ?`, first.ID).Scan(&status); err != nil {
		t.Fatalf("re-read status: %v", err)
	}
	if status != "expired" {
		t.Errorf("the lapsed ticket must be marked expired, got %q", status)
	}
}

// TestExpiredTicketCannotBeAccepted pins that a lapsed pairing cannot still become a
// room.
//
// Acceptance used to check only the status, never expires_at. An expired ticket
// could therefore be accepted, flip to 'ready', and — once its partner also accepted
// — create a room for a pair that had already timed out. The window is real: the
// acceptance screen can sit open for minutes while the ticket quietly lapses.
func TestExpiredTicketCannotBeAccepted(t *testing.T) {
	t.Parallel()
	db := newDB(t)
	reg, err := modes.NewRegistry(&sharedStub{id: "classic"})
	if err != nil {
		t.Fatalf("registry: %v", err)
	}
	svc := room.New(db, room.Config{}, room.Options{Modes: reg, Epoch: "test"})
	defer svc.Close()
	ctx := context.Background()

	for _, id := range []string{"ses_a", "ses_b"} {
		if _, err := db.Write().ExecContext(ctx, `
			INSERT INTO sessions (id, token_hash, display_name, csrf_token_hash, kind, created_at, expires_at)
			VALUES (?, ?, ?, ?, 'player', 0, 9999999999999)`, id, "h_"+id, id, "c_"+id); err != nil {
			t.Fatalf("seed session %s: %v", id, err)
		}
	}

	in := room.CreateTicketInput{ModeConfig: json.RawMessage(`{"modeId":"classic","config":{}}`)}
	first, err := svc.CreateMatchmakingTicket(ctx, "ses_a", in)
	if err != nil {
		t.Fatalf("first ticket: %v", err)
	}
	second, err := svc.CreateMatchmakingTicket(ctx, "ses_b", in)
	if err != nil {
		t.Fatalf("second ticket: %v", err)
	}

	// Pair them, then let the first lapse.
	if _, err := db.Write().ExecContext(ctx,
		`UPDATE matchmaking_tickets SET status='matched-pending-accept', matched_with=? WHERE id=?`,
		second.ID, first.ID); err != nil {
		t.Fatalf("pair first: %v", err)
	}
	if _, err := db.Write().ExecContext(ctx,
		`UPDATE matchmaking_tickets SET status='matched-pending-accept', matched_with=? WHERE id=?`,
		first.ID, second.ID); err != nil {
		t.Fatalf("pair second: %v", err)
	}
	if _, err := db.Write().ExecContext(ctx,
		`UPDATE matchmaking_tickets SET expires_at=? WHERE id=?`,
		time.Now().Add(-time.Minute).UnixMilli(), first.ID); err != nil {
		t.Fatalf("expire first: %v", err)
	}

	// Accepting a lapsed pairing must fail, not silently create a room.
	if _, err := svc.RespondToMatch(ctx, "ses_a", first.ID, true); err == nil {
		t.Error("accepting an expired ticket must be refused")
	}

	// No room may have been created for this pair.
	var rooms int
	if err := db.Read().QueryRowContext(ctx,
		`SELECT COUNT(1) FROM rooms WHERE name = '快速匹配'`).Scan(&rooms); err != nil {
		t.Fatalf("count rooms: %v", err)
	}
	if rooms != 0 {
		t.Errorf("no room may be created for an expired pairing, found %d", rooms)
	}
}

// TestPartnerExpiryFailsTheAcceptLoudly pins that a partner lapsing mid-accept
// produces an error, not a success.
//
// The first version of the expiry fix returned nil here, so RespondToMatch answered
// 201: the accepting player saw "match found", their ticket stayed on 'ready', and no
// room ever appeared. Suppressing the room was necessary but not sufficient — a
// silent success is worse than a failure, because nothing retries and nothing
// explains.
func TestPartnerExpiryFailsTheAcceptLoudly(t *testing.T) {
	t.Parallel()
	db := newDB(t)
	reg, err := modes.NewRegistry(&sharedStub{id: "classic"})
	if err != nil {
		t.Fatalf("registry: %v", err)
	}
	svc := room.New(db, room.Config{}, room.Options{Modes: reg, Epoch: "test"})
	defer svc.Close()
	ctx := context.Background()

	for _, id := range []string{"ses_p", "ses_q2"} {
		if _, err := db.Write().ExecContext(ctx, `
			INSERT INTO sessions (id, token_hash, display_name, csrf_token_hash, kind, created_at, expires_at)
			VALUES (?, ?, ?, ?, 'player', 0, 9999999999999)`, id, "h_"+id, id, "c_"+id); err != nil {
			t.Fatalf("seed session %s: %v", id, err)
		}
	}

	in := room.CreateTicketInput{ModeConfig: json.RawMessage(`{"modeId":"classic","config":{}}`)}
	mine, err := svc.CreateMatchmakingTicket(ctx, "ses_p", in)
	if err != nil {
		t.Fatalf("my ticket: %v", err)
	}
	theirs, err := svc.CreateMatchmakingTicket(ctx, "ses_q2", in)
	if err != nil {
		t.Fatalf("their ticket: %v", err)
	}

	// Pair them, then let the PARTNER lapse — the partner has not accepted yet, so its
	// status is still matched-pending-accept.
	if _, err := db.Write().ExecContext(ctx,
		`UPDATE matchmaking_tickets SET status='matched-pending-accept', matched_with=? WHERE id=?`,
		theirs.ID, mine.ID); err != nil {
		t.Fatalf("pair mine: %v", err)
	}
	if _, err := db.Write().ExecContext(ctx,
		`UPDATE matchmaking_tickets SET status='matched-pending-accept', matched_with=?, expires_at=? WHERE id=?`,
		mine.ID, time.Now().Add(-time.Minute).UnixMilli(), theirs.ID); err != nil {
		t.Fatalf("pair theirs: %v", err)
	}

	// Accepting must fail loudly.
	if _, err := svc.RespondToMatch(ctx, "ses_p", mine.ID, true); err == nil {
		t.Fatal("a lapsed partner must fail the accept, not report success")
	}

	// The accepting ticket must not remain ready when its partner can no longer accept.
	var status string
	if err := db.Read().QueryRowContext(ctx,
		`SELECT status FROM matchmaking_tickets WHERE id = ?`, mine.ID).Scan(&status); err != nil {
		t.Fatalf("read my status: %v", err)
	}
	if status == "ready" {
		t.Error("my ticket must not stay on ready when the partner expired; the accept rolled back")
	}

	var rooms int
	if err := db.Read().QueryRowContext(ctx,
		`SELECT COUNT(1) FROM rooms WHERE name = '快速匹配'`).Scan(&rooms); err != nil {
		t.Fatalf("count rooms: %v", err)
	}
	if rooms != 0 {
		t.Errorf("no room may be created for an expired pairing, found %d", rooms)
	}
}
