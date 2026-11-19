package httpapi

import "net/http"

func (s *Server) handleGetAudioPracticePack(w http.ResponseWriter, r *http.Request) {
	pack, err := s.rooms.AudioPracticePack()
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	s.writeJSON(w, r, http.StatusOK, pack)
}

func (s *Server) handleGetStrategyPracticePack(w http.ResponseWriter, r *http.Request) {
	pack, err := s.rooms.StrategyPracticePack()
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	s.writeJSON(w, r, http.StatusOK, pack)
}

func (s *Server) handleGetPathPracticePack(w http.ResponseWriter, r *http.Request) {
	pack, err := s.rooms.PathPracticePack()
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	s.writeJSON(w, r, http.StatusOK, pack)
}

func (s *Server) handleGetSortPracticePack(w http.ResponseWriter, r *http.Request) {
	pack, err := s.rooms.SortPracticePack()
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	s.writeJSON(w, r, http.StatusOK, pack)
}

func (s *Server) handleGetClassicPracticePack(w http.ResponseWriter, r *http.Request) {
	pack, err := s.rooms.ClassicPracticePack()
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	s.writeJSON(w, r, http.StatusOK, pack)
}

func (s *Server) handleGetMemoryPracticePack(w http.ResponseWriter, r *http.Request) {
	pack, err := s.rooms.MemoryPracticePack()
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	s.writeJSON(w, r, http.StatusOK, pack)
}

func (s *Server) handleGetAptitudePracticePack(w http.ResponseWriter, r *http.Request) {
	pack, err := s.rooms.AptitudePracticePack()
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	s.writeJSON(w, r, http.StatusOK, pack)
}
func (s *Server) handleGetDraftPracticePack(w http.ResponseWriter, r *http.Request) {
	pack, err := s.rooms.DraftPracticePack()
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	s.writeJSON(w, r, http.StatusOK, pack)
}

func (s *Server) handleGetRacePracticePack(w http.ResponseWriter, r *http.Request) {
	pack, err := s.rooms.RacePracticePack()
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	s.writeJSON(w, r, http.StatusOK, pack)
}

func (s *Server) handleGetPedigreePracticePack(w http.ResponseWriter, r *http.Request) {
	pack, err := s.rooms.PedigreePracticePack()
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	s.writeJSON(w, r, http.StatusOK, pack)
}
