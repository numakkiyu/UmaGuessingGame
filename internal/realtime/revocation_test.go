package realtime

import (
	"context"
	"encoding/json"
	"testing"
	"time"
)

func TestCommittedMembershipRevocationKeepsOtherStreamsAndReplay(t *testing.T) {
	for _, scenario := range []string{"current", "legacy", "rejoined"} {
		t.Run(scenario, func(t *testing.T) {
			db := newTestDB(t)
			hub := newTestHub(t, nil)
			const rid, sid, mid = "room-target", "sess-target", "mem-target"
			seedRoom(t, db, rid, sid, mid, "player")
			target, other := newFakeSSETransport(), newFakeSSETransport()
			subscribe(t, hub, identity(rid, mid, sid, "player"), "", target)
			subscribe(t, hub, identity(rid, "mem-other", "sess-other", "player"), "", other)
			target.waitType(t, TypeHello, time.Second)
			other.waitType(t, TypeHello, time.Second)
			if scenario != "rejoined" {
				if _, err := db.Write().Exec(`UPDATE memberships SET left_at=1 WHERE id=?`, mid); err != nil {
					t.Fatal(err)
				}
			}
			payload := OutboxPayload{RoomVersion: 2, Resource: ResourceRoom, RevokeMembershipID: mid}
			if scenario == "legacy" {
				body, _ := json.Marshal(map[string]any{"resource": "room", "roomVersion": 2, "membershipId": mid})
				if _, err := db.Write().Exec(`INSERT INTO outbox_events(id,room_id,event_sequence,type,payload_json,created_at) VALUES('evt-remove',?,1,'session.revoked',?,?)`, rid, string(body), time.Now().UnixMilli()); err != nil {
					t.Fatal(err)
				}
			} else {
				appendOutbox(t, db, rid, "evt-remove", 1, TypeStateChanged, payload)
			}
			dispatch, err := NewDispatcher(db, hub, hub.Config())
			if err != nil {
				t.Fatal(err)
			}
			if n, err := dispatch.DispatchOnce(context.Background()); err != nil || n != 1 {
				t.Fatalf("dispatch = %d, %v", n, err)
			}
			if scenario != "rejoined" {
				target.waitType(t, TypeSessionRevoked, time.Second)
				target.waitClosed(t, time.Second)
			} else {
				target.waitType(t, TypeStateChanged, time.Second)
				select {
				case <-target.closed:
					t.Fatal("delayed notification closed rejoined member")
				default:
				}
			}
			changed := other.waitType(t, TypeStateChanged, time.Second)
			// A notification after the removal must still arrive on this same stream.
			if err := hub.Publish(Publication{EventID: "evt-next", RoomID: rid, Type: TypePhaseChanged, RoomVersion: 3, PhaseVersion: 2, Seq: 2, RoundID: "round-next"}); err != nil {
				t.Fatal(err)
			}
			other.waitType(t, TypePhaseChanged, time.Second)
			for _, frame := range other.sent() {
				if frame.msg.Type == TypeSessionRevoked {
					t.Fatal("unrelated member was revoked")
				}
				wire := mustJSON(t, frame.raw)
				if wire["membershipId"] != nil || wire["revokeMembershipId"] != nil || wire["audience"] != nil {
					t.Fatalf("server-only routing leaked: %s", frame.raw)
				}
			}
			// Removal must not poison a later connection's replay with a terminal event.
			replay := newFakeSSETransport()
			subscribe(t, hub, identity(rid, "mem-other", "sess-other", "player"), changed.msg.Cursor, replay)
			replay.waitType(t, TypePhaseChanged, time.Second)
			for _, frame := range replay.sent() {
				if frame.msg.Type == TypeSessionRevoked {
					t.Fatal("replay revoked unrelated member")
				}
			}
		})
	}
}

func TestRoomPublicationRejectsConnectionMessages(t *testing.T) {
	hub := newTestHub(t, nil)
	for _, kind := range []MessageType{TypeSessionRevoked, TypeHello, TypePong, TypeResyncRequired} {
		if err := hub.Publish(Publication{EventID: "evt-invalid", RoomID: "room-invalid", Type: kind}); err == nil {
			t.Fatalf("allowed room broadcast of %q", kind)
		}
	}
}

func TestRecoveryRevokesOnlyTheTransferredBinding(t *testing.T) {
	h := newTestHub(t, nil)
	old := identity("room-recovery", "seat-old", "device-old", "player")
	target := newFakeSSETransport()
	subscribe(t, h, old, "", target)
	target.waitType(t, TypeHello, time.Second)
	for _, ident := range []Identity{
		identity("room-other", "seat-other", "device-old", "player"),
		identity("room-recovery", "seat-new", "device-old", "player"),
		identity("room-recovery", "seat-old", "device-new", "player"),
	} {
		tr := newFakeSSETransport()
		subscribe(t, h, ident, "", tr)
		tr.waitType(t, TypeHello, time.Second)
		defer func() {
			select {
			case <-tr.closed:
				t.Error("unrelated binding was closed")
			default:
			}
		}()
	}
	if n := h.RevokeIdentity(old, "authorization-changed"); n != 1 {
		t.Fatalf("revoked %d bindings", n)
	}
	target.waitType(t, TypeSessionRevoked, time.Second)
	target.waitClosed(t, time.Second)
}
