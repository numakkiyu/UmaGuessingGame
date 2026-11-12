package room_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/bhcn/umaguessinggame/internal/modes"
	"github.com/bhcn/umaguessinggame/internal/modes/cluepuzzle"
	"github.com/bhcn/umaguessinggame/internal/room"
	"github.com/bhcn/umaguessinggame/internal/storage"
)

func newClueService(t *testing.T) (*storage.DB, *room.Service, *time.Time) {
	t.Helper()
	db, err := storage.Open(context.Background(), storage.Config{Dir: t.TempDir(), ReadConns: 2})
	if err != nil {
		t.Fatalf("open storage: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	reg, err := modes.NewRegistry(cluepuzzle.New())
	if err != nil {
		t.Fatalf("registry: %v", err)
	}
	svc := room.New(db, room.Config{}, room.Options{Epoch: "test", Modes: reg})
	t.Cleanup(svc.Close)

	clock := time.Date(2026, 11, 10, 0, 0, 0, 0, time.UTC)
	svc.SetClock(func() time.Time { return clock })
	return db, svc, &clock
}

// seedClueRound writes one collecting clue round. The public state carries only what
// the sweep and the projection read: the config that owns the 240-second window.
func seedClueRound(t *testing.T, db *storage.DB, now time.Time, deadline time.Time) (roomID, roundID string) {
	t.Helper()
	ctx := context.Background()

	public := map[string]any{
		"modeId":       cluepuzzle.ModeID,
		"rulesVersion": cluepuzzle.RulesVersion,
		"config": map[string]any{
			"cards": 5, "participants": 3, "constraintCount": 4, "sharedCount": 1,
			"unique": true, "answerSeconds": 240,
		},
		"slots": []map[string]any{},
		"subjects": []map[string]any{
			{"subjectId": "team-a", "subjectKind": "team"},
			{"subjectId": "team-b", "subjectKind": "team"},
		},
	}
	publicJSON, err := json.Marshal(public)
	if err != nil {
		t.Fatalf("marshal public: %v", err)
	}
	privateJSON := []byte(`{"modeId":"clue-puzzle","note":"no hidden answer in this seed"}`)

	roomID = "rom_clue_deadline"
	roundID = "rnd_clue_deadline"
	ms := now.UnixMilli()

	if _, err := db.Write().ExecContext(ctx, `
		INSERT INTO sessions (id, token_hash, display_name, csrf_token_hash, kind, created_at, expires_at)
		VALUES ('ses_owner', 'h', 'Owner', 'c', 'player', ?, ?)`,
		ms, ms+3600_000); err != nil {
		t.Fatalf("seed session: %v", err)
	}
	if _, err := db.Write().ExecContext(ctx, `
		INSERT INTO rooms (id, owner_session_id, name, status, config_json, room_version, created_at, updated_at)
		VALUES (?, 'ses_owner', 'clue-deadline', 'playing', '{}', 1, ?, ?)`,
		roomID, ms, ms); err != nil {
		t.Fatalf("seed room: %v", err)
	}
	if _, err := db.Write().ExecContext(ctx, `
		INSERT INTO rounds (id, room_id, ordinal, mode_id, rules_version, dataset_version,
		                    content_pack_version, status, phase_version, deadline_at,
		                    seed, config_json, created_at, updated_at)
		VALUES (?, ?, 1, ?, ?, 'ds-test', 'pack-test', 'collecting', 1, ?, 'seed-1', ?, ?, ?)`,
		roundID, roomID, cluepuzzle.ModeID, cluepuzzle.RulesVersion,
		deadline.UnixMilli(), `{"cards":5,"participants":3,"answerSeconds":240}`, ms, ms); err != nil {
		t.Fatalf("seed round: %v", err)
	}
	if _, err := db.Write().ExecContext(ctx, `
		INSERT INTO round_private_state (round_id, scope, state_json) VALUES (?, 'public', ?)`,
		roundID, string(publicJSON)); err != nil {
		t.Fatalf("seed public state: %v", err)
	}
	if _, err := db.Write().ExecContext(ctx, `
		INSERT INTO round_private_state (round_id, scope, state_json) VALUES (?, 'round', ?)`,
		roundID, string(privateJSON)); err != nil {
		t.Fatalf("seed private state: %v", err)
	}
	return roomID, roundID
}

// TestClueRoundClosesNaturallyAtItsOwnWindow proves two things at once: the room
// layer takes the 240-second window from the mode rather than from a service
// default, and an expired collecting round advances to locked with nobody acting.
func TestClueRoundClosesNaturallyAtItsOwnWindow(t *testing.T) {
	t.Parallel()
	db, svc, clock := newClueService(t)
	ctx := context.Background()

	start := *clock
	roomID, roundID := seedClueRound(t, db, start, start.Add(240*time.Second))

	// One second short of the window: nothing may happen.
	*clock = start.Add(239 * time.Second)
	if _, err := svc.SweepExpired(ctx); err != nil {
		t.Fatalf("early sweep: %v", err)
	}
	summary, err := svc.CurrentRoundSummary(ctx, roomID)
	if err != nil {
		t.Fatalf("load round: %v", err)
	}
	if summary == nil || summary.Status != modes.PhaseCollecting {
		t.Fatalf("round closed before its 240-second window: %+v", summary)
	}

	// Past the window: the round must close on its own.
	*clock = start.Add(241 * time.Second)
	if _, err := svc.SweepExpired(ctx); err != nil {
		t.Fatalf("sweep at deadline: %v", err)
	}
	summary, err = svc.CurrentRoundSummary(ctx, roomID)
	if err != nil {
		t.Fatalf("load round after sweep: %v", err)
	}
	if summary == nil {
		t.Fatal("the round disappeared when its answering window closed")
	}
	if summary.Status != modes.PhaseLocked {
		t.Fatalf("status after the answering window = %q, want %q",
			summary.Status, modes.PhaseLocked)
	}
	// A closed round carries the short locked deadline, not a stale 240-second one.
	// The locked phase is deliberately brief (about a second), so the fresh deadline
	// sits just after the sweep instant rather than a whole answering window away.
	if summary.DeadlineAt == nil || !summary.DeadlineAt.After(*clock) {
		t.Fatalf("locked round did not receive a fresh future deadline: %+v", summary.DeadlineAt)
	}
	if gap := summary.DeadlineAt.Sub(*clock); gap > 5*time.Second {
		t.Fatalf("locked deadline is %s away, want the short locked window", gap)
	}

	// The round still exists and is readable after the natural close.
	var rows int
	if err := db.Read().QueryRowContext(ctx,
		`SELECT COUNT(*) FROM rounds WHERE id = ?`, roundID).Scan(&rows); err != nil {
		t.Fatalf("round row vanished: %v", err)
	}
	if rows != 1 {
		t.Fatalf("round rows after the natural close = %d, want 1", rows)
	}
	assertLatestDeadlineVersion(t, db, roomID)
}

// Both timed phases and turn-based windows must invalidate a connected client
// that already learned the current room version from its hello message.
func assertLatestDeadlineVersion(t *testing.T, db *storage.DB, roomID string) {
	t.Helper()
	var version int64
	var raw string
	if err := db.Read().QueryRowContext(context.Background(), `SELECT room_version FROM rooms WHERE id = ?`, roomID).Scan(&version); err != nil {
		t.Fatal(err)
	}
	if err := db.Read().QueryRowContext(context.Background(), `SELECT payload_json FROM outbox_events WHERE room_id = ? AND type = 'state.changed' ORDER BY event_sequence DESC LIMIT 1`, roomID).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	var payload struct {
		RoomVersion int64 `json:"roomVersion"`
	}
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		t.Fatal(err)
	}
	if payload.RoomVersion <= 1 || payload.RoomVersion != version {
		t.Fatalf("automatic deadline notification version = %d, room = %d", payload.RoomVersion, version)
	}
}
