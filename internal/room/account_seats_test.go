package room_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/bhcn/umaguessinggame/internal/deployment"
	"github.com/bhcn/umaguessinggame/internal/httpapi/problem"
	"github.com/bhcn/umaguessinggame/internal/room"
	"github.com/bhcn/umaguessinggame/internal/storage"
)

func bindTestAccounts(t *testing.T, db *storage.DB) {
	t.Helper()
	for _, id := range []string{"account-a", "account-b"} {
		if _, err := db.Write().Exec(`INSERT INTO accounts(id,created_at) VALUES(?,0)`, id); err != nil {
			t.Fatal(err)
		}
	}
	for sid, id := range map[string]string{"s0": "account-a", "s1": "account-a", "s2": "account-b"} {
		if _, err := db.Write().Exec(`INSERT INTO account_game_bindings(session_id,account_id,created_at) VALUES(?,?,0)`, sid, id); err != nil {
			t.Fatal(err)
		}
	}
}

func TestAccountSeatIsUniqueAndDepartureFreesIt(t *testing.T) {
	ctx := context.Background()
	svc, db := matchingService(t, deployment.Policy{Accounts: deployment.AccountAccess{Enabled: true, RequireLogin: map[string]bool{"friend": true}}})
	bindTestAccounts(t, db)
	made, err := svc.CreateRoom(ctx, "s0", room.RoomCreateInput{Name: "席位测试", Kind: "friend", ModeConfig: json.RawMessage(`{"modeId":"classic"}`), MaxPlayers: 2, Visibility: "invite"})
	if err != nil {
		t.Fatal(err)
	}
	join := room.JoinInput{InviteCode: made.InviteCode, RequestedRole: "player"}
	if _, err = svc.Join(ctx, "s1", made.Room.ID, join); problem.As(err).Code != "account-seat-active" {
		t.Fatalf("same account occupied two seats: %v", err)
	}
	if _, err = svc.Join(ctx, "s2", made.Room.ID, join); err != nil {
		t.Fatal(err)
	}
	if err = svc.ReleaseAccountSession(ctx, "s0"); err != nil {
		t.Fatal(err)
	}
	var owner string
	if err = db.Read().QueryRow(`SELECT owner_session_id FROM rooms WHERE id=?`, made.Room.ID).Scan(&owner); err != nil || owner != "s2" {
		t.Fatalf("host was not transferred: %s %v", owner, err)
	}
	if _, err = svc.Join(ctx, "s1", made.Room.ID, join); err != nil {
		t.Fatalf("old seat still blocks re-entry: %v", err)
	}
	if err = svc.ReleaseAccountSession(ctx, "s0"); err != nil {
		t.Fatalf("repeat cleanup failed: %v", err)
	}
}

func TestAccountQueueCannotMatchItselfAcrossBrowsers(t *testing.T) {
	ctx := context.Background()
	svc, db := matchingService(t, deployment.Policy{Accounts: deployment.AccountAccess{Enabled: true, RequireLogin: map[string]bool{"matchmaking": true}}})
	bindTestAccounts(t, db)
	first := queueMatch(t, svc, "s0", "classic-duel")
	if _, err := svc.CreateMatchmakingTicket(ctx, "s1", room.CreateTicketInput{PoolID: "classic-duel", Proof: work(t, svc, "s1", "classic-duel")}); problem.As(err).Code != "account-queue-active" {
		t.Fatalf("same account entered queue twice: %v", err)
	}
	if err := svc.CancelMatchmakingTicket(ctx, "s0", first.ID); err != nil {
		t.Fatal(err)
	}
	queueMatch(t, svc, "s1", "classic-duel")
	other := queueMatch(t, svc, "s2", "classic-duel")
	if other.Status != "matched-pending-accept" {
		t.Fatalf("different accounts did not match: %+v", other)
	}
}

func TestAccountDepartureConcedesAnIndividualRound(t *testing.T) {
	ctx := context.Background()
	svc, db := matchingService(t, deployment.Policy{})
	tickets := []room.Ticket{queueMatch(t, svc, "s0", "sort-four"), queueMatch(t, svc, "s1", "sort-four"), queueMatch(t, svc, "s2", "sort-four"), queueMatch(t, svc, "s3", "sort-four")}
	var match room.Ticket
	for i, sid := range []string{"s0", "s1", "s2", "s3"} {
		var err error
		match, err = svc.RespondToMatch(ctx, sid, tickets[i].ID, true)
		if err != nil {
			t.Fatal(err)
		}
	}
	current, err := svc.CurrentRoundSummary(ctx, match.RoomID)
	if err != nil || current == nil {
		t.Fatalf("missing round: %v", err)
	}
	actor, err := svc.LoadActor(ctx, "s0", match.RoomID)
	if err != nil {
		t.Fatal(err)
	}
	if err = svc.ReleaseAccountSession(ctx, "s0"); err != nil {
		t.Fatal(err)
	}
	var surrendered bool
	if err = db.Read().QueryRow(`SELECT json_extract(state_json,'$.surrendered') FROM participant_round_state WHERE round_id=? AND subject_id=?`, current.ID, actor.MembershipID()).Scan(&surrendered); err != nil || !surrendered {
		t.Fatalf("departure did not concede: %v %v", surrendered, err)
	}
	if _, err = svc.LoadActor(ctx, "s1", match.RoomID); err != nil {
		t.Fatalf("opponent lost access: %v", err)
	}
	if active, err := svc.ActiveMatchmakingTicket(ctx, "s0"); err != nil || active != nil {
		t.Fatalf("old match still occupies queue: %+v %v", active, err)
	}
}

func TestAccountDepartureCanReleaseACancelledRoom(t *testing.T) {
	ctx := context.Background()
	svc, db := matchingService(t, deployment.Policy{})
	made, err := svc.CreateRoom(ctx, "s0", room.RoomCreateInput{Name: "结束的房间", Kind: "friend", ModeConfig: json.RawMessage(`{"modeId":"classic"}`), MaxPlayers: 2, Visibility: "invite"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.Write().Exec(`UPDATE rooms SET status='cancelled' WHERE id=?`, made.Room.ID); err != nil {
		t.Fatal(err)
	}
	if err = svc.ReleaseAccountSession(ctx, "s0"); err != nil {
		t.Fatalf("cancelled room blocks logout: %v", err)
	}
	var count int
	if err = db.Read().QueryRow(`SELECT COUNT(*) FROM memberships WHERE session_id='s0' AND left_at IS NULL AND revoked_at IS NULL`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("stale membership: %d %v", count, err)
	}
}
