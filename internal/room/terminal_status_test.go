package room_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/bhcn/umaguessinggame/internal/modes"
	"github.com/bhcn/umaguessinggame/internal/room"
)

// TestFinishedIsNotTerminal pins the room lifecycle from
//
// The lifecycle is lobby → ready → playing → finished → archived, and only cancelled
// and archived end a room for good. finished means the round is over, not the room:
// a rematch starts a new round in the same room without overwriting the old result.
//
// This test exists in this exact form because the opposite assertion was written once
// already, on the assumption that "finished" must mean "done". It broke every rematch
// and every multi-segment event, where one room hosts segment after segment. The
// lesson is recorded here: the document defines the state machine, not intuition.
func TestFinishedIsNotTerminal(t *testing.T) {
	t.Parallel()
	cases := []struct {
		status room.RoomStatus
		want   bool
	}{
		{room.StatusLobby, false},
		{room.StatusReady, false},
		{room.StatusPlaying, false},
		{room.StatusFinished, false}, // between rounds: a rematch is allowed
		{room.StatusArchived, true},
		{room.StatusCancelled, true},
	}
	for _, c := range cases {
		if got := c.status.Terminal(); got != c.want {
			t.Errorf("%s.Terminal() = %v, want %v", c.status, got, c.want)
		}
	}
}

// TestFinishedRoomAcceptsRematch proves the behaviour requires: a finished room
// can host another round, which is what a rematch and every subsequent event segment
// rely on.
func TestFinishedRoomAcceptsRematch(t *testing.T) {
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
		VALUES ('ses_fin', 'h_fin', 'fin', 'c_fin', 'player', 0, 9999999999999)`); err != nil {
		t.Fatalf("seed session: %v", err)
	}
	cfg := json.RawMessage(`{"modeId":"classic","config":{}}`)
	created, err := svc.CreateRoom(ctx, "ses_fin", room.RoomCreateInput{
		Name: "fin", Kind: "solo", ModeConfig: cfg,
		MaxPlayers: 1, MaxSpectators: 0,
		Visibility: "invite", AllowSpectators: false,
	})
	if err != nil {
		t.Fatalf("create room: %v", err)
	}
	actor, err := svc.LoadActor(ctx, "ses_fin", created.Room.ID)
	if err != nil {
		t.Fatalf("load actor: %v", err)
	}

	// Drive the room to finished without playing: the assertion is about the guard,
	// not about how the room arrived there.
	if _, err := db.Write().ExecContext(ctx,
		`UPDATE rooms SET status = 'finished' WHERE id = ?`, created.Room.ID); err != nil {
		t.Fatalf("finish room: %v", err)
	}

	if _, err := svc.CreateRound(ctx, actor, room.RoundCreateInput{ModeConfig: cfg}); err != nil {
		t.Fatalf("a finished room must accept a rematch round: %v", err)
	}
}
