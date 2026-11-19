package admission

import (
	"net/http"
	"strconv"
	"time"
)

const maxRiskEntries = 8192

type riskState struct {
	window   time.Time
	expires  time.Time
	count    int
	required bool
	revision uint64
}

// Adaptive decisions use repeated limits on the authenticated session, never a
// shared address or a client-supplied fingerprint. Existing limits still apply.
func (s *Service) adaptive() bool {
	return s != nil && s.cfg.Enabled && (s.cfg.Adaptive == nil || *s.cfg.Adaptive)
}
func (s *Service) ObserveLimit(sessionID string) {
	if !s.adaptive() || sessionID == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now()
	state := s.risks[sessionID]
	if state.expires.Before(now) {
		state = riskState{}
	}
	if state.required {
		return
	}
	if now.Sub(state.window) > time.Duration(s.cfg.RiskWindowSeconds)*time.Second {
		state.window = now
		state.count = 0
	}
	state.count++
	if state.count >= s.cfg.RiskThreshold {
		s.riskSequence++
		state.required = true
		state.revision = s.riskSequence
	}
	// Retain the revision until every previously signed grant has expired.
	state.expires = now.Add(time.Duration(s.cfg.GrantSeconds+s.cfg.RiskWindowSeconds) * time.Second)
	if _, ok := s.risks[sessionID]; !ok && len(s.risks) >= maxRiskEntries {
		for key, v := range s.risks {
			if !v.expires.After(now) {
				delete(s.risks, key)
			}
		}
		if len(s.risks) >= maxRiskEntries {
			return
		} // Request rate limits remain enforced.
	}
	s.risks[sessionID] = state
}
func (s *Service) RiskRequired(sessionID string) bool {
	if !s.adaptive() {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	state := s.risks[sessionID]
	return state.required && state.expires.After(s.now())
}

// Caller holds s.mu across risk clearing and grant signing.
func (s *Service) clearRiskLocked(sessionID string) {
	if !s.adaptive() {
		return
	}
	state, ok := s.risks[sessionID]
	if !ok {
		return
	}
	state.required = false
	state.count = 0
	state.window = s.now()
	state.expires = s.now().Add(time.Duration(s.cfg.GrantSeconds+s.cfg.RiskWindowSeconds) * time.Second)
	s.risks[sessionID] = state
}
func (s *Service) grantPayloadLocked(sessionID, stamp string) string {
	var revision uint64
	if s.adaptive() {
		state := s.risks[sessionID]
		if state.expires.After(s.now()) {
			revision = state.revision
		}
	}
	return sessionID + "|" + stamp + "|" + strconv.FormatUint(revision, 10)
}

// A consumed token cannot grant another identity even during provider convergence.
type tokenState struct {
	expires  time.Time
	owner    string
	busy     bool
	consumed bool
}

func (s *Service) reserveToken(digest [32]byte, owner string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now()
	if state, ok := s.tokens[digest]; ok && state.expires.After(now) {
		if state.owner != owner || state.busy || state.consumed {
			return false
		}
		state.busy = true
		s.tokens[digest] = state
		return true
	}
	if len(s.tokens) >= maxRiskEntries {
		for key, state := range s.tokens {
			if !state.expires.After(now) && !state.busy {
				delete(s.tokens, key)
			}
		}
		if len(s.tokens) >= maxRiskEntries {
			return false
		}
	}
	s.tokens[digest] = tokenState{expires: now.Add(5 * time.Minute), owner: owner, busy: true}
	return true
}

type Status struct {
	Enabled  bool `json:"enabled"`
	Required bool `json:"required"`
	Verified bool `json:"verified"`
}

func (s *Service) Status(r *http.Request, sessionID string) Status {
	if s == nil || !s.cfg.Enabled {
		return Status{}
	}
	return Status{Enabled: true, Required: s.RiskRequired(sessionID), Verified: sessionID != "" && s.Allowed(r, sessionID)}
}
