package room_test

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/bhcn/umaguessinggame/internal/modes"
	"github.com/bhcn/umaguessinggame/internal/modes/aptitudegrid"
	"github.com/bhcn/umaguessinggame/internal/room"
	"github.com/bhcn/umaguessinggame/internal/storage"
)

func strategyActor(roomID, subject string) room.Actor {
	return room.Actor{SessionID: "ses_" + subject, RoomID: roomID,
		Membership:   &room.Membership{ID: subject, Role: room.RolePlayer},
		Capabilities: map[room.Capability]bool{room.CapPlay: true}}
}

func seedStrategySeats(t *testing.T, db *storage.DB, roomID string) {
	t.Helper()
	for _, id := range []string{"mem_a", "mem_b"} {
		if _, err := db.Write().Exec(`INSERT INTO sessions(id,token_hash,display_name,csrf_token_hash,kind,created_at,expires_at) VALUES(?,?,?,?, 'player',?,?)`, "ses_"+id, "token_"+id, id, "csrf_"+id, time.Now().UnixMilli(), time.Now().Add(time.Hour).UnixMilli()); err != nil {
			t.Fatal(err)
		}
		if _, err := db.Write().Exec(`INSERT INTO memberships(id,room_id,session_id,role,ready,joined_at) VALUES(?,?,?,'player',1,?)`, id, roomID, "ses_"+id, time.Now().UnixMilli()); err != nil {
			t.Fatal(err)
		}
	}
}

func strategyCommit(t *testing.T, db *storage.DB, svc *room.Service, roomID, roundID, subject string, turn, revision int) room.SubmissionReceipt {
	t.Helper()
	var version int64
	if err := db.Read().QueryRowContext(context.Background(), `SELECT phase_version FROM rounds WHERE id = ?`, roundID).Scan(&version); err != nil {
		t.Fatal(err)
	}
	receipt, err := svc.CreateSubmission(context.Background(), strategyActor(roomID, subject), roundID, room.SubmissionInput{
		PhaseVersion: version, SubmissionRevision: int64(revision),
		ModeAction: modes.ModeAction{Kind: "strategy.action", Payload: json.RawMessage(fmt.Sprintf(`{"turn":%d,"action":"steady"}`, turn))},
	})
	if err != nil {
		t.Fatalf("submit %s turn %d: %v", subject, turn, err)
	}
	return receipt
}

func committedCount(t *testing.T, db *storage.DB, roundID string) int {
	t.Helper()
	var raw string
	if err := db.Read().QueryRowContext(context.Background(), `SELECT state_json FROM round_private_state WHERE round_id = ? AND scope = 'public'`, roundID).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	var state struct {
		Committed []json.RawMessage `json:"committed"`
	}
	if err := json.Unmarshal([]byte(raw), &state); err != nil {
		t.Fatal(err)
	}
	return len(state.Committed)
}

func TestStrategySubmissionDoesNotDefaultOtherTeams(t *testing.T) {
	t.Parallel()
	db, svc, _ := newStrategyService(t)
	start := time.Now().UTC()
	roomID, roundID := seedStrategyRound(t, db, start, start.Add(20*time.Second))
	seedStrategySeats(t, db, roomID)
	original, _ := readDeadline(t, db, roundID)
	strategyCommit(t, db, svc, roomID, roundID, "mem_a", 1, 0)
	if got := readCurrentTurn(t, db, roundID); got != 1 {
		t.Fatalf("first submission advanced to turn %d", got)
	}
	if got := committedCount(t, db, roundID); got != 1 {
		t.Fatalf("first submission defaulted other teams: commits=%d", got)
	}
	if got, _ := readDeadline(t, db, roundID); got != original {
		t.Fatal("partial submission extended the clock")
	}
	receipt := strategyCommit(t, db, svc, roomID, roundID, "mem_b", 1, 0)
	if got := readCurrentTurn(t, db, roundID); got != 2 {
		t.Fatalf("all submitted: turn=%d, want 2", got)
	}
	if got := committedCount(t, db, roundID); got != 2 {
		t.Fatalf("next turn was defaulted: commits=%d", got)
	}
	if got, _ := readDeadline(t, db, roundID); got != receipt.AcceptedAt+20000 {
		t.Fatalf("new deadline=%d, want %d", got, receipt.AcceptedAt+20000)
	}
	// The next turn must still accept each player's choice.
	strategyCommit(t, db, svc, roomID, roundID, "mem_a", 2, 1)
	if got := readCurrentTurn(t, db, roundID); got != 2 {
		t.Fatalf("partial second turn advanced to %d", got)
	}
}

func TestStrategyFinalTimeoutSettlesWithoutHost(t *testing.T) {
	t.Parallel()
	db, svc, clock := newStrategyService(t)
	roomID, roundID := seedStrategyRound(t, db, *clock, clock.Add(20*time.Second))
	for i := 0; i < 8; i++ {
		*clock = clock.Add(21 * time.Second)
		if n, err := svc.SweepExpired(context.Background()); err != nil || n != 1 {
			t.Fatalf("turn %d sweep=%d err=%v", i+1, n, err)
		}
	}
	for _, delta := range []time.Duration{time.Second, 3 * time.Second} {
		*clock = clock.Add(delta)
		if _, err := svc.SweepExpired(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	var status, roomStatus string
	var results int
	if err := db.Read().QueryRowContext(context.Background(), `SELECT status FROM rounds WHERE id=?`, roundID).Scan(&status); err != nil {
		t.Fatal(err)
	}
	if err := db.Read().QueryRowContext(context.Background(), `SELECT status FROM rooms WHERE id=?`, roomID).Scan(&roomStatus); err != nil {
		t.Fatal(err)
	}
	if err := db.Read().QueryRowContext(context.Background(), `SELECT COUNT(*) FROM round_results WHERE round_id=?`, roundID).Scan(&results); err != nil {
		t.Fatal(err)
	}
	if status != "settled" || roomStatus != "finished" || results != 2 {
		t.Fatalf("status=%s room=%s results=%d", status, roomStatus, results)
	}
	if n, err := svc.SweepExpired(context.Background()); err != nil || n != 0 {
		t.Fatalf("terminal repeat sweep=%d err=%v", n, err)
	}
}

func TestStrategyAllSubmittedFinalTurnCloses(t *testing.T) {
	t.Parallel()
	db, svc, clock := newStrategyService(t)
	start := time.Now().UTC()
	roomID, roundID := seedStrategyRound(t, db, start, start.Add(20*time.Second))
	seedStrategySeats(t, db, roomID)
	for turn := 1; turn <= 8; turn++ {
		strategyCommit(t, db, svc, roomID, roundID, "mem_a", turn, turn-1)
		strategyCommit(t, db, svc, roomID, roundID, "mem_b", turn, turn-1)
	}
	*clock = time.Now().UTC().Add(2 * time.Second)
	if _, err := svc.SweepExpired(context.Background()); err != nil {
		t.Fatal(err)
	}
	*clock = clock.Add(4 * time.Second)
	if _, err := svc.SweepExpired(context.Background()); err != nil {
		t.Fatal(err)
	}
	var status string
	if err := db.Read().QueryRowContext(context.Background(), `SELECT status FROM rounds WHERE id=?`, roundID).Scan(&status); err != nil {
		t.Fatal(err)
	}
	if status != "settled" {
		t.Fatalf("fully submitted race stuck at %s", status)
	}
}

func TestDuelSubmissionPassesExactlyOnce(t *testing.T) {
	t.Parallel()
	db, _, _ := newStrategyService(t)
	reg, err := modes.NewRegistry(aptitudegrid.New())
	if err != nil {
		t.Fatal(err)
	}
	svc := room.New(db, room.Config{}, room.Options{Epoch: "test", Modes: reg})
	defer svc.Close()
	start := time.Now().UTC()
	roomID, roundID := seedStrategyRound(t, db, start, start.Add(30*time.Second))
	seedStrategySeats(t, db, roomID)
	public := `{"modeId":"aptitude-grid","rulesVersion":"test","round":{"config":{"variant":"duel"},"subjects":[{"subjectId":"mem_a"},{"subjectId":"mem_b"}]},"board":{"claims":[],"closed":false,"turnSubjectId":"mem_a"}}`
	ctx := context.Background()
	if _, err := db.Write().ExecContext(ctx, `UPDATE rounds SET mode_id='aptitude-grid', config_json=? WHERE id=?`, `{"modeId":"aptitude-grid","config":{"variant":"duel"}}`, roundID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Write().ExecContext(ctx, `UPDATE round_private_state SET state_json=? WHERE round_id=? AND scope='public'`, public, roundID); err != nil {
		t.Fatal(err)
	}
	_, err = svc.CreateSubmission(ctx, strategyActor(roomID, "mem_a"), roundID, room.SubmissionInput{PhaseVersion: 1, ModeAction: modes.ModeAction{Kind: "grid.skip", Payload: json.RawMessage(`{}`)}})
	if err != nil {
		t.Fatal(err)
	}
	var raw string
	if err := db.Read().QueryRowContext(ctx, `SELECT state_json FROM round_private_state WHERE round_id=? AND scope='public'`, roundID).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	var env struct {
		Board struct {
			TurnSubjectID string `json:"turnSubjectId"`
		} `json:"board"`
	}
	if err := json.Unmarshal([]byte(raw), &env); err != nil {
		t.Fatal(err)
	}
	if env.Board.TurnSubjectID != "mem_b" {
		t.Fatalf("duel advanced twice: holder=%s", env.Board.TurnSubjectID)
	}
	// Out-of-turn passing cannot steal the other player's turn.
	_, err = svc.CreateSubmission(ctx, strategyActor(roomID, "mem_a"), roundID, room.SubmissionInput{PhaseVersion: 1, SubmissionRevision: 1, ModeAction: modes.ModeAction{Kind: "grid.skip", Payload: json.RawMessage(`{}`)}})
	if err == nil {
		t.Fatal("out-of-turn skip accepted")
	}
}
