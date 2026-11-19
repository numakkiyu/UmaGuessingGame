package httpapi

import (
	"github.com/bhcn/umaguessinggame/internal/httpapi/problem"
	"github.com/bhcn/umaguessinggame/internal/session"
	"net/http"
)

func (s *Server) handleMatchmakingLobby(w http.ResponseWriter, r *http.Request) {
	lobby, err := s.rooms.MatchmakingLobby(r.Context())
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	s.writeJSON(w, r, http.StatusOK, lobby)
}
func (s *Server) handleMatchmakingProof(w http.ResponseWriter, r *http.Request) {
	sess, ok := sessionOf(r)
	if !ok {
		s.writeError(w, r, session.ErrNoSession)
		return
	}
	// Use the same optional gate as ticket creation, before the browser spends work.
	if s.admission.Required("createMatchmakingTicket") && !s.admission.Allowed(r, sess.ID) {
		s.writeError(w, r, problem.Forbidden("admission-required", "开始前请先完成入场确认"))
		return
	}
	raw, err := readRawBody(r)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	var body struct {
		PoolID string `json:"poolId"`
	}
	if err = decodeJSON(&body, raw); err != nil {
		s.writeError(w, r, err)
		return
	}
	proof, err := s.rooms.CreateMatchProof(r.Context(), sess.ID, body.PoolID)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	s.writeJSON(w, r, http.StatusCreated, struct {
		ChallengeID string `json:"challengeId"`
		Bits        int    `json:"bits"`
		ExpiresAt   string `json:"expiresAt"`
	}{proof.ChallengeID, proof.Bits, rfc3339(proof.ExpiresAt)})
}
func (s *Server) handleActiveMatchmakingTicket(w http.ResponseWriter, r *http.Request) {
	sess, ok := sessionOf(r)
	if !ok {
		s.writeError(w, r, session.ErrNoSession)
		return
	}
	t, err := s.rooms.ActiveMatchmakingTicket(r.Context(), sess.ID)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	var dto *ticketDTO
	if t != nil {
		v := toTicketDTO(*t)
		dto = &v
	}
	s.writeJSON(w, r, http.StatusOK, struct {
		Ticket *ticketDTO `json:"ticket"`
	}{dto})
}
func (s *Server) handleMatchmakingHeartbeat(w http.ResponseWriter, r *http.Request) {
	sess, ok := sessionOf(r)
	if !ok {
		s.writeError(w, r, session.ErrNoSession)
		return
	}
	id, err := pathParam(r, "ticketId")
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	t, err := s.rooms.HeartbeatMatchmakingTicket(r.Context(), sess.ID, id)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	s.writeJSON(w, r, http.StatusOK, toTicketDTO(t))
}
