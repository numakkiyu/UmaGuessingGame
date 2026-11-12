package room_test

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"testing"

	"github.com/bhcn/umaguessinggame/internal/modes"
	"github.com/bhcn/umaguessinggame/internal/room"
)

// sixDigits is the exact shape a room-local number must have: six ASCII digits,
// leading zeros included.
var sixDigits = regexp.MustCompile(`^[0-9]{6}$`)

// TestRoomNumbersAreSixDigitStringsWithLeadingZeros fills a room and asserts every
// issued number is a six-character digit string.
//
// It also checks the two properties that make the number usable as a label:
// uniqueness inside the room, and that a member who leaves and rejoins keeps the
// same number (otherwise two players could swap labels after a refresh).
func TestRoomNumbersAreSixDigitStringsWithLeadingZeros(t *testing.T) {
	ctx := context.Background()
	db := newDB(t)
	mode := &sharedStub{id: "classic"}
	reg, err := modes.NewRegistry(mode)
	if err != nil {
		t.Fatal(err)
	}
	svc := room.New(db, room.Config{}, room.Options{Modes: reg, Epoch: "room-number"})
	t.Cleanup(svc.Close)

	// A room large enough that the generator has to cross low numeric values,
	// which is where a leading zero shows up. 40 members keeps the test fast while
	// still making an all-high-numbers draw very unlikely.
	const members = 40
	sessions := make([]string, 0, members+1)
	sessions = append(sessions, "owner")
	for i := 0; i < members; i++ {
		sessions = append(sessions, fmt.Sprintf("p%02d", i))
	}
	for _, id := range sessions {
		if _, err := db.Write().Exec(
			`INSERT INTO sessions(id,token_hash,display_name,csrf_token_hash,kind,created_at,expires_at)
			 VALUES(?,?,?,?,'player',0,9999999999999)`, id, "h-"+id, id, "c-"+id); err != nil {
			t.Fatal(err)
		}
	}

	made, err := svc.CreateRoom(ctx, "owner", room.RoomCreateInput{
		Name:       "号码房",
		Kind:       "friend",
		ModeConfig: json.RawMessage(`{"modeId":"classic","config":{}}`),
		MaxPlayers: members + 2,
		Visibility: "invite",
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range sessions[1:] {
		if _, err := svc.Join(ctx, id, made.Room.ID, room.JoinInput{
			InviteCode: made.InviteCode, RequestedRole: "player",
		}); err != nil {
			t.Fatalf("join %s: %v", id, err)
		}
	}

	list, err := svc.ListMembers(ctx, made.Room.ID)
	if err != nil {
		t.Fatalf("list members: %v", err)
	}
	if len(list) != len(sessions) {
		t.Fatalf("members = %d, want %d", len(list), len(sessions))
	}

	seen := map[string]string{} // code -> membership id
	var leadingZero int
	for _, m := range list {
		code := m.PublicCode
		if !sixDigits.MatchString(code) {
			t.Fatalf("membership %s has public code %q, want exactly six digits "+
				"(a leading zero must survive as a character, not be dropped as an integer)",
				m.ID, code)
		}
		if prior, dup := seen[code]; dup {
			t.Fatalf("public code %q issued to both %s and %s", code, prior, m.ID)
		}
		seen[code] = m.ID
		if code[0] == '0' {
			leadingZero++
		}
	}

	// With 41 members drawn from a 1,000,000 space, seeing zero leading zeros is
	// possible but unlikely; more importantly the assertion above already proves
	// the format holds for every value. This log makes the coverage visible instead
	// of silently passing on an all-high draw.
	t.Logf("%d members, %d numbers starting with 0", len(list), leadingZero)

	// A member who leaves and comes back must keep the same label: the number is an
	// identity aid, so a rejoin that renumbered them would let two people swap.
	// Pick a joiner (not the owner) and leave AS THAT MEMBER: only the member
	// themselves or a room manager may remove a membership, and leaving as the
	// owner would exercise the manager path instead of the self-leave path.
	/*
	 * Pick the leaver by SESSION ID, never by list position.
	 *
	 * The previous version assumed `ListMembers` returns the host first and took
	 * list[i] to match sessions[i]. That order is not guaranteed — when the host
	 * happened to sort later, the loop picked the host as the "joiner" and the
	 * self-leave below failed with 403 insufficient-capability. It only showed up
	 * under `make ci` (different ordering) and not when the package was run alone,
	 * which is the signature of a flaky test.
	 */
	bySession := map[string]room.Membership{}
	for _, m := range list {
		if m.SessionID != "" {
			bySession[m.SessionID] = m
		}
	}
	var leaver room.Membership
	var leaverSession string
	// sessions[0] is the owner; any later one is a plain joiner.
	for _, sid := range sessions[1:] {
		if m, ok := bySession[sid]; ok && m.Role == room.RolePlayer {
			leaver, leaverSession = m, sid
			break
		}
	}
	if leaver.ID == "" || leaverSession == "" {
		t.Fatalf("could not identify a joiner to leave and rejoin")
	}
	actor, err := svc.LoadActor(ctx, leaverSession, made.Room.ID)
	if err != nil {
		t.Fatalf("load actor: %v", err)
	}
	if err := svc.LeaveOrRemoveMembership(ctx, actor, leaver.ID); err != nil {
		t.Fatalf("leave: %v", err)
	}
	if _, err := svc.Join(ctx, leaverSession, made.Room.ID, room.JoinInput{
		InviteCode: made.InviteCode, RequestedRole: "player",
	}); err != nil {
		t.Fatalf("rejoin: %v", err)
	}
	again, err := svc.ListMembers(ctx, made.Room.ID)
	if err != nil {
		t.Fatalf("list members after rejoin: %v", err)
	}
	for _, m := range again {
		if m.ID != leaver.ID {
			continue
		}
		if m.PublicCode != leaver.PublicCode {
			t.Fatalf("rejoin changed the room number: %q -> %q", leaver.PublicCode, m.PublicCode)
		}
		return
	}
	t.Fatalf("the rejoining member %s is missing from the roster", leaver.ID)
}
