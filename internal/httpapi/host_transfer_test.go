package httpapi_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"sync"
	"testing"

	"github.com/bhcn/umaguessinggame/internal/modes"
	"github.com/bhcn/umaguessinggame/internal/modes/classic"
	"github.com/bhcn/umaguessinggame/internal/room"
)

type hostFixture struct {
	h                         *harness
	browser                   *http.Client
	owner                     testSession
	room, code, member, event string
}

func newHostFixture(t *testing.T) hostFixture {
	reg, _ := modes.NewRegistry(classic.New())
	h := newHarness(t, func(c *harnessConfig) { c.Modes = reg })
	browser, owner := h.newSession("同名训练员")
	made := h.doAs(browser, "POST", "/api/v1/rooms", map[string]any{"name": "游玩会", "kind": "event", "modeConfig": map[string]any{"modeId": "classic"}, "maxPlayers": 8, "maxSpectators": 8, "allowSpectators": true, "visibility": "invite"}, writeHeaders(h, owner, idemKey("host-room")))
	if made.Status != 201 {
		t.Fatalf("create: %d %s", made.Status, made.Body)
	}
	var body struct {
		Room struct {
			RoomID string `json:"roomId"`
			Viewer struct {
				MembershipID string `json:"membershipId"`
			} `json:"viewer"`
		} `json:"room"`
		InviteCode string `json:"inviteCode"`
	}
	if err := json.Unmarshal(made.Body, &body); err != nil {
		t.Fatal(err)
	}
	f := hostFixture{h: h, browser: browser, owner: owner, room: body.Room.RoomID, code: body.InviteCode, member: body.Room.Viewer.MembershipID}
	ev := h.doAs(browser, "POST", "/api/v1/events", map[string]any{"roomId": f.room, "name": "聚会", "tiePolicy": "shared-rank"}, writeHeaders(h, owner, idemKey("host-programme")))
	if ev.Status != 201 {
		t.Fatalf("event: %d %s", ev.Status, ev.Body)
	}
	var event struct {
		EventID string `json:"eventId"`
	}
	_ = json.Unmarshal(ev.Body, &event)
	f.event = event.EventID
	return f
}
func (f hostFixture) join(t *testing.T, name string) (*http.Client, testSession, string, string) {
	t.Helper()
	client, sess := f.h.newSession(name)
	res := f.h.doAs(client, "POST", "/api/v1/rooms/"+f.room+"/memberships", map[string]any{"inviteCode": f.code, "requestedRole": "player"}, writeHeaders(f.h, sess, idemKey("join-member")))
	if res.Status != 201 {
		t.Fatalf("join: %d %s", res.Status, res.Body)
	}
	var member struct {
		ID   string `json:"membershipId"`
		Code string `json:"publicCode"`
	}
	_ = json.Unmarshal(res.Body, &member)
	return client, sess, member.ID, member.Code
}
func (f hostFixture) version(t *testing.T) int64 {
	t.Helper()
	var version int64
	if err := f.h.db.Read().QueryRow(`SELECT room_version FROM rooms WHERE id=?`, f.room).Scan(&version); err != nil {
		t.Fatal(err)
	}
	return version
}
func TestHostTransferIsAtomicAndPreservesIdentity(t *testing.T) {
	f := newHostFixture(t)
	h := f.h
	client, next, member, code := f.join(t, "同名训练员")
	_, _, other, _ := f.join(t, "同名训练员")
	if !regexp.MustCompile(`^[0-9]{6}$`).MatchString(code) {
		t.Fatalf("public code %q", code)
	}
	before, err := h.rooms.LoadActor(context.Background(), f.owner.ID, f.room)
	if err != nil {
		t.Fatal(err)
	}
	if !before.Has(room.CapHost) {
		t.Fatal("creator is not host")
	}
	path := "/api/v1/rooms/" + f.room + "/host-transfers"
	body := map[string]any{"targetMembershipId": member, "expectedRoomVersion": f.version(t)}
	denied := h.doAs(client, "POST", path, body, writeHeaders(h, next, idemKey("not-host")))
	if denied.Status != 403 {
		t.Fatalf("non-host transfer: %d %s", denied.Status, denied.Body)
	}
	key := idemKey("real-transfer")
	moved := h.doAs(f.browser, "POST", path, body, writeHeaders(h, f.owner, key))
	if moved.Status != 200 {
		t.Fatalf("transfer: %d %s", moved.Status, moved.Body)
	}
	var owner, eventHost string
	if err := h.db.Read().QueryRow(`SELECT owner_session_id FROM rooms WHERE id=?`, f.room).Scan(&owner); err != nil {
		t.Fatal(err)
	}
	if err := h.db.Read().QueryRow(`SELECT host_session_id FROM event_sessions WHERE id=?`, f.event).Scan(&eventHost); err != nil {
		t.Fatal(err)
	}
	if owner != next.ID || eventHost != next.ID {
		t.Fatal("room/programme authority diverged")
	}
	old, _ := h.rooms.LoadActor(context.Background(), f.owner.ID, f.room)
	current, _ := h.rooms.LoadActor(context.Background(), next.ID, f.room)
	if old.Has(room.CapHost) || old.Has(room.CapManageRoom) || !current.Has(room.CapHost) || !current.Has(room.CapPlay) {
		t.Fatal("handoff kept old authority or changed participation")
	}
	// A stale actor resolved before transfer must fail after entering its write tx.
	_, err = h.rooms.CreateTeam(context.Background(), before, "不应创建", "green")
	if err == nil {
		t.Fatal("stale controller wrote after handoff")
	}
	// Domain receipt, not the HTTP receipt, survives a lost transfer response.
	replay := h.doAs(f.browser, "POST", path, body, writeHeaders(h, f.owner, key))
	if replay.Status != 200 || string(replay.Body) != string(moved.Body) {
		t.Fatalf("transfer replay: %d %s", replay.Status, replay.Body)
	}
	body["targetMembershipId"] = other
	conflict := h.doAs(f.browser, "POST", path, body, writeHeaders(h, f.owner, key))
	if conflict.Status != 409 {
		t.Fatalf("changed command accepted: %d", conflict.Status)
	}
	// Public host badge must agree for every viewer, not just for the new host.
	for _, browser := range []*http.Client{client, f.browser} {
		snapshot := h.doAs(browser, "GET", "/api/v1/rooms/"+f.room, nil, nil)
		var data struct {
			Room struct {
				Members []struct {
					ID   string   `json:"membershipId"`
					Code string   `json:"publicCode"`
					Caps []string `json:"capabilities"`
				} `json:"members"`
			} `json:"room"`
		}
		if snapshot.Status != 200 {
			t.Fatal(string(snapshot.Body))
		}
		if err := json.Unmarshal(append(append([]byte(`{"room":`), snapshot.Body...), '}'), &data); err != nil {
			t.Fatal(err)
		}
		codes := map[string]bool{}
		hosts := 0
		for _, m := range data.Room.Members {
			if codes[m.Code] || !regexp.MustCompile(`^[0-9]{6}$`).MatchString(m.Code) {
				t.Fatal("invalid/duplicate code")
			}
			codes[m.Code] = true
			for _, cap := range m.Caps {
				if cap == "host" {
					hosts++
					if m.ID != member {
						t.Fatal("wrong host badge")
					}
				}
			}
		}
		if hosts != 1 {
			t.Fatalf("host count=%d", hosts)
		}
	}
	// New host's ID survives a rename and departure/re-entry.
	renamed := h.doAs(client, "PATCH", "/api/v1/sessions/current", map[string]any{"displayName": "特别周"}, writeHeaders(h, next, idemKey("rename-host")))
	if renamed.Status != 200 {
		t.Fatal(string(renamed.Body))
	}
	left := h.doAs(client, "DELETE", "/api/v1/rooms/"+f.room+"/memberships/"+member, nil, writeHeaders(h, next, idemKey("leave-host")))
	if left.Status != 204 {
		t.Fatalf("host leave: %d %s", left.Status, left.Body)
	}
	if _, err = h.rooms.LoadActor(context.Background(), next.ID, f.room); err == nil {
		t.Fatal("departed owner retained access")
	}
	rejoin := h.doAs(client, "POST", "/api/v1/rooms/"+f.room+"/memberships", map[string]any{"inviteCode": f.code, "requestedRole": "player"}, writeHeaders(h, next, idemKey("rejoin-host")))
	if rejoin.Status != 201 {
		t.Fatalf("rejoin %d %s", rejoin.Status, rejoin.Body)
	}
	var restored struct {
		Code string `json:"publicCode"`
	}
	_ = json.Unmarshal(rejoin.Body, &restored)
	if restored.Code != code {
		t.Fatal("room ID changed on re-entry")
	}
}
func TestHostDepartureWithoutSuccessorClosesAndCanBeConfirmed(t *testing.T) {
	f := newHostFixture(t)
	h := f.h
	// A revoked member is never selected as successor.
	_, _, member, _ := f.join(t, "已离开的伙伴")
	if _, err := h.db.Write().Exec(`UPDATE memberships SET revoked_at=1 WHERE id=?`, member); err != nil {
		t.Fatal(err)
	}
	path := "/api/v1/rooms/" + f.room + "/memberships/" + f.member
	for i := 0; i < 2; i++ {
		res := h.doAs(f.browser, "DELETE", path, nil, writeHeaders(h, f.owner, idemKey(fmt.Sprintf("leave-confirm-%d", i))))
		if res.Status != 204 {
			t.Fatalf("leave confirmation: %d %s", res.Status, res.Body)
		}
	}
	var status, eventStatus string
	_ = h.db.Read().QueryRow(`SELECT status FROM rooms WHERE id=?`, f.room).Scan(&status)
	_ = h.db.Read().QueryRow(`SELECT status FROM event_sessions WHERE id=?`, f.event).Scan(&eventStatus)
	if status != "cancelled" || eventStatus != "cancelled" {
		t.Fatalf("abandoned room live: %s %s", status, eventStatus)
	}
	if _, err := h.rooms.LoadActor(context.Background(), f.owner.ID, f.room); err == nil {
		t.Fatal("departed last host retained administrative access")
	}
}
func TestConcurrentHostTransfersHaveOneWinner(t *testing.T) {
	f := newHostFixture(t)
	_, _, one, _ := f.join(t, "甲")
	_, _, two, _ := f.join(t, "乙")
	version := f.version(t)
	var wg sync.WaitGroup
	results := make(chan error, 2)
	for _, target := range []string{one, two} {
		wg.Add(1)
		go func(target string) {
			defer wg.Done()
			_, err := f.h.rooms.TransferHost(context.Background(), f.owner.ID, f.room, room.HostTransferInput{ExpectedRoomVersion: version, TargetMembershipID: target})
			results <- err
		}(target)
	}
	wg.Wait()
	close(results)
	wins := 0
	for err := range results {
		if err == nil {
			wins++
		}
	}
	if wins != 1 {
		t.Fatalf("concurrent transfer wins=%d", wins)
	}
	var audits int
	_ = f.h.db.Read().QueryRow(`SELECT COUNT(*) FROM audit_events WHERE room_id=? AND action='host.transferred'`, f.room).Scan(&audits)
	if audits != 1 {
		t.Fatalf("transfer effects=%d", audits)
	}
}

func TestHostTargetsAreValidatedAndRejectedWritesHaveNoEffects(t *testing.T) {
	f := newHostFixture(t)
	h := f.h
	_, expired, expiredID, _ := f.join(t, "过期伙伴")
	_, _, leftID, _ := f.join(t, "离开伙伴")
	_, display := h.newDisplaySession(t)
	displayID := h.seedMember(t, f.room, display.ID, "display")
	otherBrowser, other := h.newSession("外场伙伴")
	_, foreignResponse := h.createRoom(otherBrowser, other, "另一间房")
	if foreignResponse.Status != 201 {
		t.Fatal(string(foreignResponse.Body))
	}
	var foreignID string
	if err := h.db.Read().QueryRow(`SELECT id FROM memberships WHERE session_id=?`, other.ID).Scan(&foreignID); err != nil {
		t.Fatal(err)
	}
	if _, err := h.db.Write().Exec(`UPDATE sessions SET expires_at=1 WHERE id=?`, expired.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := h.db.Write().Exec(`UPDATE memberships SET left_at=1 WHERE id=?`, leftID); err != nil {
		t.Fatal(err)
	}
	original := f.version(t)
	for i, target := range []string{f.member, expiredID, leftID, displayID, foreignID} {
		result := h.doAs(f.browser, "POST", "/api/v1/rooms/"+f.room+"/host-transfers", map[string]any{"targetMembershipId": target, "expectedRoomVersion": original}, writeHeaders(h, f.owner, idemKey(fmt.Sprintf("invalid-host-%d", i))))
		if result.Status != 422 {
			t.Fatalf("target %s: %d %s", target, result.Status, result.Body)
		}
	}
	if f.version(t) != original {
		t.Fatal("rejected transfers changed room version")
	}
	var count int
	_ = h.db.Read().QueryRow(`SELECT COUNT(*) FROM host_transfers WHERE room_id=?`, f.room).Scan(&count)
	if count != 0 {
		t.Fatal("rejected transfers persisted effects")
	}
}

func TestSoloRoomsNeverAcceptASecondPlayer(t *testing.T) {
	reg, _ := modes.NewRegistry(classic.New())
	h := newHarness(t, func(c *harnessConfig) { c.Modes = reg })
	client, sess := h.newSession("练习训练员")
	makeRoom := func(players int, key string) response {
		return h.doAs(client, "POST", "/api/v1/rooms", map[string]any{"name": "一个人的练习", "kind": "solo", "modeConfig": map[string]any{"modeId": "classic"}, "maxPlayers": players, "maxSpectators": 0, "allowSpectators": false, "visibility": "invite"}, writeHeaders(h, sess, idemKey(key)))
	}
	if invalid := makeRoom(2, "solo-two"); invalid.Status != 400 {
		t.Fatalf("two-player solo accepted: %d %s", invalid.Status, invalid.Body)
	}
	made := makeRoom(1, "solo-one")
	if made.Status != 201 {
		t.Fatalf("solo creation: %d %s", made.Status, made.Body)
	}
	var body struct {
		Room struct {
			ID string `json:"roomId"`
		} `json:"room"`
		Code string `json:"inviteCode"`
	}
	_ = json.Unmarshal(made.Body, &body)
	peer, peerSession := h.newSession("朋友")
	joined := h.doAs(peer, "POST", "/api/v1/rooms/"+body.Room.ID+"/memberships", map[string]any{"inviteCode": body.Code, "requestedRole": "player"}, writeHeaders(h, peerSession, idemKey("solo-join")))
	if joined.Status != 403 || joined.Problem.Code != "solo-room" {
		t.Fatalf("second player entered solo: %d %s", joined.Status, joined.Body)
	}
}
