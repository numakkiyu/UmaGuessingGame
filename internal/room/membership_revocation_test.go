package room_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/bhcn/umaguessinggame/internal/modes"
	"github.com/bhcn/umaguessinggame/internal/room"
)

func TestMembershipRemovalEmitsRoomRefreshAndRevokesOnlyTarget(t *testing.T) {
	ctx := context.Background()
	db := newDB(t)
	registry, err := modes.NewRegistry(&sharedStub{id: "classic"})
	if err != nil {
		t.Fatal(err)
	}
	svc := room.New(db, room.Config{}, room.Options{Modes: registry, Epoch: "removal"})
	t.Cleanup(svc.Close)
	for _, id := range []string{"owner", "removed", "remaining"} {
		if _, err := db.Write().Exec(`INSERT INTO sessions(id,token_hash,display_name,csrf_token_hash,kind,created_at,expires_at) VALUES(?,?,?,?,'player',0,9999999999999)`, id, "h-"+id, id, "c-"+id); err != nil {
			t.Fatal(err)
		}
	}
	made, err := svc.CreateRoom(ctx, "owner", room.RoomCreateInput{Name: "移除测试", Kind: "friend", ModeConfig: json.RawMessage(`{"modeId":"classic","config":{}}`), MaxPlayers: 8, Visibility: "invite"})
	if err != nil {
		t.Fatal(err)
	}
	var target room.Membership
	for _, id := range []string{"removed", "remaining"} {
		m, err := svc.Join(ctx, id, made.Room.ID, room.JoinInput{InviteCode: made.InviteCode, RequestedRole: "player"})
		if err != nil {
			t.Fatal(err)
		}
		if id == "removed" {
			target = m
		}
	}
	owner, err := svc.LoadActor(ctx, "owner", made.Room.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.LeaveOrRemoveMembership(ctx, owner, target.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.LoadActor(ctx, "removed", made.Room.ID); err == nil {
		t.Fatal("removed member retained room authority")
	}
	remaining, err := svc.LoadActor(ctx, "remaining", made.Room.ID)
	if err != nil {
		t.Fatalf("remaining member lost room authority: %v", err)
	}
	var kind, payload string
	if err := db.Read().QueryRow(`SELECT type,payload_json FROM outbox_events WHERE room_id=? ORDER BY event_sequence DESC LIMIT 1`, made.Room.ID).Scan(&kind, &payload); err != nil {
		t.Fatal(err)
	}
	var routing struct {
		Target string `json:"revokeMembershipId"`
	}
	if err := json.Unmarshal([]byte(payload), &routing); err != nil {
		t.Fatal(err)
	}
	if kind != "state.changed" || routing.Target != target.ID {
		t.Fatalf("removal notification = %s %s", kind, payload)
	}
	// Persisted rows from the previous build must also remain safe for polling.
	if _, err := db.Write().Exec(`UPDATE outbox_events SET type='session.revoked' WHERE room_id=? AND json_extract(payload_json,'$.revokeMembershipId')=?`, made.Room.ID, target.ID); err != nil {
		t.Fatal(err)
	}
	notifications, _, _, err := svc.ListRoomEvents(ctx, remaining, "", 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(notifications) == 0 {
		t.Fatal("remaining member received no room refresh")
	}
	for _, event := range notifications {
		if event.Type == "session.revoked" {
			t.Fatal("polling unrelated member receives terminal revocation")
		}
	}
}
