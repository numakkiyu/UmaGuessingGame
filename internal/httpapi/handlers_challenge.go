package httpapi

import (
	"github.com/bhcn/umaguessinggame/internal/challenge"
	"github.com/bhcn/umaguessinggame/internal/httpapi/problem"
	"net/http"
)

func (s *Server) challengeSession(w http.ResponseWriter, r *http.Request) (string, bool) {
	if s.challenges == nil {
		s.writeError(w, r, problem.NotImplemented("challenge-unavailable", "challenge support is unavailable"))
		return "", false
	}
	sess, ok := sessionOf(r)
	return sess.ID, ok
}
func (s *Server) handleCreateChallenge(w http.ResponseWriter, r *http.Request) {
	session, ok := s.challengeSession(w, r)
	if !ok {
		return
	}
	var input challenge.CreateInput
	if err := decodeBody(w, r, &input, 4096); err != nil {
		s.writeError(w, r, err)
		return
	}
	key, err := requireIdempotencyKey(r)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	view, err := s.challenges.Create(r.Context(), session, key, input)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	w.Header().Set("Location", "/api/v1/challenges/"+view.ChallengeID)
	s.writeJSON(w, r, http.StatusCreated, view)
}
func (s *Server) handleGetChallenge(w http.ResponseWriter, r *http.Request) {
	session, ok := s.challengeSession(w, r)
	if !ok {
		return
	}
	view, err := s.challenges.Get(r.Context(), session, r.PathValue("challengeId"))
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	s.writeJSON(w, r, http.StatusOK, view)
}
func (s *Server) handleGuessChallenge(w http.ResponseWriter, r *http.Request) {
	session, ok := s.challengeSession(w, r)
	if !ok {
		return
	}
	var input challenge.GuessInput
	if err := decodeBody(w, r, &input, 4096); err != nil {
		s.writeError(w, r, err)
		return
	}
	key, err := requireIdempotencyKey(r)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	view, err := s.challenges.Guess(r.Context(), session, r.PathValue("challengeId"), key, input)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	s.writeJSON(w, r, http.StatusOK, view)
}
func (s *Server) handleFinishChallenge(w http.ResponseWriter, r *http.Request) {
	session, ok := s.challengeSession(w, r)
	if !ok {
		return
	}
	var input challenge.FinishInput
	if err := decodeBody(w, r, &input, 4096); err != nil {
		s.writeError(w, r, err)
		return
	}
	key, err := requireIdempotencyKey(r)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	view, err := s.challenges.Finish(r.Context(), session, r.PathValue("challengeId"), key, input)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	s.writeJSON(w, r, http.StatusOK, view)
}
