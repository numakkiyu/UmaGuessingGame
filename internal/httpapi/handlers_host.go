package httpapi

import (
	"github.com/bhcn/umaguessinggame/internal/room"
	"github.com/bhcn/umaguessinggame/internal/session"
	"net/http"
)

func (s *Server) handleTransferRoomHost(w http.ResponseWriter, r *http.Request) {
	sess, ok := sessionOf(r)
	if !ok {
		s.writeError(w, r, session.ErrNoSession)
		return
	}
	roomID, err := pathParam(r, "roomId")
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	key, err := requireIdempotencyKey(r)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	raw, err := readRawBody(r)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	var body struct {
		TargetMembershipID  string `json:"targetMembershipId"`
		ExpectedRoomVersion int64  `json:"expectedRoomVersion"`
	}
	if err := decodeJSON(&body, raw); err != nil {
		s.writeError(w, r, err)
		return
	}
	result, err := s.rooms.TransferHost(r.Context(), sess.ID, roomID, room.HostTransferInput{TargetMembershipID: body.TargetMembershipID, ExpectedRoomVersion: body.ExpectedRoomVersion, Command: &room.CreationCommand{Key: key, Payload: raw}})
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	s.writeJSON(w, r, http.StatusOK, struct {
		TransferID         string `json:"transferId"`
		RoomID             string `json:"roomId"`
		TargetMembershipID string `json:"targetMembershipId"`
		RoomVersion        int64  `json:"roomVersion"`
	}{result.ID, result.RoomID, result.TargetMembershipID, result.RoomVersion})
}
