package room_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/bhcn/umaguessinggame/internal/modes"
	"github.com/bhcn/umaguessinggame/internal/room"
)

// TestRoomNumberIsALabelNotACredential pins the parts of the boundary that can
// actually fail.
func TestRoomNumberIsALabelNotACredential(t *testing.T) {
	ctx := context.Background()
	db := newDB(t)
	reg, err := modes.NewRegistry(&sharedStub{id: "classic"})
	if err != nil {
		t.Fatal(err)
	}
	svc := room.New(db, room.Config{}, room.Options{Modes: reg, Epoch: "number-not-credential"})
	t.Cleanup(svc.Close)

	for _, id := range []string{"owner", "alice", "mallory"} {
		if _, err := db.Write().Exec(
			`INSERT INTO sessions(id,token_hash,display_name,csrf_token_hash,kind,created_at,expires_at)
			 VALUES(?,?,?,?,'player',0,9999999999999)`, id, "h-"+id, id, "c-"+id); err != nil {
			t.Fatal(err)
		}
	}
	newRoom := func(name string) room.RoomCreateResult {
		made, rerr := svc.CreateRoom(ctx, "owner", room.RoomCreateInput{
			Name:       name,
			Kind:       "friend",
			ModeConfig: json.RawMessage(`{"modeId":"classic","config":{}}`),
			MaxPlayers: 8,
			Visibility: "invite",
		})
		if rerr != nil {
			t.Fatalf("create room %s: %v", name, rerr)
		}
		return made
	}
	first := newRoom("一号房")
	if _, err := svc.Join(ctx, "alice", first.Room.ID, room.JoinInput{
		InviteCode: first.InviteCode, RequestedRole: "player",
	}); err != nil {
		t.Fatal(err)
	}
	second := newRoom("二号房")
	if _, err := svc.Join(ctx, "mallory", second.Room.ID, room.JoinInput{
		InviteCode: second.InviteCode, RequestedRole: "player",
	}); err != nil {
		t.Fatal(err)
	}

	// (A) 号码是房间局部的。给两个房间各塞足够多成员，若实现改成全局唯一，第二个房间
	// 的分配会失败或与第一个房间冲突——这条能真的红。
	aliceMembers, err := svc.ListMembers(ctx, first.Room.ID)
	if err != nil {
		t.Fatalf("list first room: %v", err)
	}
	malloryMembers, err := svc.ListMembers(ctx, second.Room.ID)
	if err != nil {
		t.Fatalf("list second room: %v", err)
	}
	var alice, mallory room.Membership
	for _, m := range aliceMembers {
		if m.SessionID == "alice" {
			alice = m
		}
	}
	for _, m := range malloryMembers {
		if m.SessionID == "mallory" {
			mallory = m
		}
	}
	if alice.ID == "" || mallory.ID == "" {
		t.Fatalf("missing memberships: alice=%q mallory=%q", alice.ID, mallory.ID)
	}
	// 每个房间内部唯一（这一条能抓到「同房号码重复」）。
	assertUniqueCodes(t, aliceMembers, "一号房")
	assertUniqueCodes(t, malloryMembers, "二号房")

	// (B) 号码不参与授权。两个人在各自房间都是 player；把其中一人升为房内管理者，
	// 另一人的能力不应因为号码相同而改变。这条能抓到「号码被接进能力判定」。
	aliceActor, err := svc.LoadActor(ctx, "alice", first.Room.ID)
	if err != nil {
		t.Fatalf("load alice actor: %v", err)
	}
	malloryActor, err := svc.LoadActor(ctx, "mallory", second.Room.ID)
	if err != nil {
		t.Fatalf("load mallory actor: %v", err)
	}
	if aliceActor.MembershipID() == malloryActor.MembershipID() {
		t.Fatalf("two sessions in different rooms resolved to the same membership")
	}
	// Alice 的能力必须只由她在一号房的角色决定。
	if !aliceActor.Has(room.CapPlay) {
		t.Fatalf("alice lost CapPlay in her own room")
	}
	if malloryActor.Has(room.CapManageRoom) {
		t.Fatalf("mallory gained CapManageRoom without being a manager — capability "+
			"must come from the role, never from a room number (hers: %q)", mallory.PublicCode)
	}

	// (C) 号码不进入成员查找。按 membership id 能查到；把号码当 id 用则查不到任何
	// 成员。这条能抓到「有人给按号码查成员加了支持」。
	if _, ok := lookupMemberByID(ctx, t, svc, first.Room.ID, alice.ID); !ok {
		t.Fatalf("membership id %q did not resolve — the lookup path is broken", alice.ID)
	}
	if _, ok := lookupMemberByID(ctx, t, svc, first.Room.ID, alice.PublicCode); ok {
		t.Fatalf("room number %q resolved as a membership id — numbers must never be "+
			"lookup keys", alice.PublicCode)
	}
}

// lookupMemberByID reports whether id matches any membership id in the room. It is
// deliberately a plain scan: the point is to show the number is not an id, not to
// exercise any particular lookup API.
func lookupMemberByID(ctx context.Context, t *testing.T, svc *room.Service, roomID, id string) (room.Membership, bool) {
	t.Helper()
	members, err := svc.ListMembers(ctx, roomID)
	if err != nil {
		t.Fatalf("list members: %v", err)
	}
	for _, m := range members {
		if m.ID == id {
			return m, true
		}
	}
	return room.Membership{}, false
}

// assertUniqueCodes fails if two members of the same room share a number.
func assertUniqueCodes(t *testing.T, members []room.Membership, label string) {
	t.Helper()
	seen := map[string]string{}
	for _, m := range members {
		if m.PublicCode == "" {
			t.Fatalf("%s: membership %s has no room number", label, m.ID)
		}
		if prior, dup := seen[m.PublicCode]; dup {
			t.Fatalf("%s: number %q issued to both %s and %s", label, m.PublicCode, prior, m.ID)
		}
		seen[m.PublicCode] = m.ID
	}
}
