package room_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/bhcn/umaguessinggame/internal/modes"
	"github.com/bhcn/umaguessinggame/internal/room"
)

// TestMatchPairingGivesEarlierTicketOwnership pins the two defects the frontend
// found:
//
// 1. the create response must report matched-pending-accept once a partner is
// found, not the pre-pairing "queueing";
// 2. management authority must go to the EARLIER ticket, not to whichever side
// happened to accept second.
//
// Both are order-dependent, so the test runs the accept in the order that used to
// break it: the earlier ticket accepts first.
func TestMatchPairingGivesEarlierTicketOwnership(t *testing.T) {
	t.Parallel()
	db := newDB(t)
	reg, err := modes.NewRegistry(&sharedStub{id: "classic"})
	if err != nil {
		t.Fatalf("registry: %v", err)
	}
	svc := room.New(db, room.Config{}, room.Options{Modes: reg, Epoch: "test"})
	defer svc.Close()
	ctx := context.Background()

	for _, id := range []string{"ses_first", "ses_second"} {
		if _, err := db.Write().ExecContext(ctx, `
			INSERT INTO sessions (id, token_hash, display_name, csrf_token_hash, kind, created_at, expires_at)
			VALUES (?, ?, ?, ?, 'player', 0, 9999999999999)`, id, "h-"+id, id, "c-"+id); err != nil {
			t.Fatalf("seed session: %v", err)
		}
	}
	cfg := json.RawMessage(`{"modeId":"classic","config":{}}`)

	first, err := svc.CreateMatchmakingTicket(ctx, "ses_first", room.CreateTicketInput{ModeConfig: cfg})
	if err != nil {
		t.Fatalf("first ticket: %v", err)
	}
	if first.Status != "queueing" {
		t.Errorf("a lone ticket must be queueing, got %q", first.Status)
	}

	second, err := svc.CreateMatchmakingTicket(ctx, "ses_second", room.CreateTicketInput{ModeConfig: cfg})
	if err != nil {
		t.Fatalf("second ticket: %v", err)
	}
	// Defect 1: the create response must already report the pairing.
	if second.Status != "matched-pending-accept" {
		t.Errorf("the arriving ticket must report matched-pending-accept, got %q", second.Status)
	}

	// The earlier ticket accepts first — the order that used to hand ownership to
	// the wrong session.
	if _, err := svc.RespondToMatch(ctx, "ses_first", first.ID, true); err != nil {
		t.Fatalf("first accepts: %v", err)
	}
	done, err := svc.RespondToMatch(ctx, "ses_second", second.ID, true)
	if err != nil {
		t.Fatalf("second accepts: %v", err)
	}
	if done.RoomID == "" {
		t.Fatal("both accepted but no room was created")
	}

	// Defect 2: ownership follows the earlier ticket regardless of accept order.
	firstActor, err := svc.LoadActor(ctx, "ses_first", done.RoomID)
	if err != nil {
		t.Fatalf("load first actor: %v", err)
	}
	secondActor, err := svc.LoadActor(ctx, "ses_second", done.RoomID)
	if err != nil {
		t.Fatalf("load second actor: %v", err)
	}
	if !firstActor.Has(room.CapManageRoom) {
		t.Error("the earlier ticket must hold manage-room")
	}
	if secondActor.Has(room.CapManageRoom) {
		t.Error("the later ticket must NOT hold manage-room")
	}
	if !firstActor.Has(room.CapPlay) || !secondActor.Has(room.CapPlay) {
		t.Error("both matched players must hold play")
	}
	// The lobby starts with an empty RoundCreate body, just like a friend room.
	round, err := svc.CreateRound(ctx, firstActor, room.RoundCreateInput{})
	if err != nil || round.ModeID != "classic" {
		t.Fatalf("default matched-room start: %v, %+v", err, round)
	}
}

func TestMatchingKeepsAgreedRules(t *testing.T) {
	t.Parallel()
	db := newDB(t)
	reg, _ := modes.NewRegistry(&sharedStub{id: "classic"})
	svc := room.New(db, room.Config{}, room.Options{Modes: reg, Epoch: "test"})
	defer svc.Close()
	ctx := context.Background()
	for _, id := range []string{"ses_eight", "ses_four", "ses_four_again"} {
		if _, err := db.Write().ExecContext(ctx, `INSERT INTO sessions (id, token_hash, display_name, csrf_token_hash, kind, created_at, expires_at)
		VALUES (?, ?, ?, ?, 'player', 0, 9999999999999)`, id, "h-"+id, id, "c-"+id); err != nil {
			t.Fatal(err)
		}
	}
	_, err := svc.CreateMatchmakingTicket(ctx, "ses_eight", room.CreateTicketInput{ModeConfig: json.RawMessage(`{"modeId":"classic","maxWrongGuesses":8}`)})
	if err != nil {
		t.Fatal(err)
	}
	four, err := svc.CreateMatchmakingTicket(ctx, "ses_four", room.CreateTicketInput{ModeConfig: json.RawMessage(`{"modeId":"classic","maxWrongGuesses":4}`)})
	if err != nil {
		t.Fatal(err)
	}
	if four.Status != "queueing" {
		t.Fatal("different rules must not pair")
	}
	other, err := svc.CreateMatchmakingTicket(ctx, "ses_four_again", room.CreateTicketInput{ModeConfig: json.RawMessage(`{ "config":{"maxWrongGuesses":4}, "modeId":"classic" }`)})
	if err != nil {
		t.Fatal(err)
	}
	if other.Status != "matched-pending-accept" {
		t.Fatal("flat and nested equal rules must not split matching")
	}
	if _, err := svc.RespondToMatch(ctx, "ses_four", four.ID, true); err != nil {
		t.Fatal(err)
	}
	done, err := svc.RespondToMatch(ctx, "ses_four_again", other.ID, true)
	if err != nil {
		t.Fatal(err)
	}
	var raw string
	var capacity int
	if err := db.Read().QueryRowContext(ctx, `SELECT config_json, capacity FROM rooms WHERE id=?`, done.RoomID).Scan(&raw, &capacity); err != nil {
		t.Fatal(err)
	}
	var cfg room.RoomConfig
	if err := json.Unmarshal([]byte(raw), &cfg); err != nil {
		t.Fatal(err)
	}
	var rules struct {
		MaxWrongGuesses int `json:"maxWrongGuesses"`
	}
	if err := json.Unmarshal(cfg.ModeConfig, &rules); err != nil {
		t.Fatal(err)
	}
	if cfg.Kind != "matchmaking" || rules.MaxWrongGuesses != 4 || cfg.MaxPlayers != 2 || capacity != 2 {
		t.Fatalf("agreed rules lost: %s capacity=%d", raw, capacity)
	}
}
