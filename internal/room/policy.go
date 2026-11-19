package room

import (
	"encoding/json"
	"github.com/bhcn/umaguessinggame/internal/deployment"
	"github.com/bhcn/umaguessinggame/internal/httpapi/problem"
)

func (s *Service) checkModeForm(raw json.RawMessage, form string) error {
	var probe struct {
		ModeID string `json:"modeId"`
	}
	if err := json.Unmarshal(raw, &probe); err != nil {
		return problem.Validation("modeConfig is invalid")
	}
	if !s.cfg.Policy.Allowed(probe.ModeID, form) {
		return problem.Forbidden("play-disabled", "this play format is closed on this instance")
	}
	return nil
}

// ModeCapabilities is a public projection only; configuration never bypasses content gates.
func (s *Service) ModeCapabilities() map[string]map[string]bool {
	out := map[string]map[string]bool{}
	if s == nil || s.modes == nil {
		return out
	}
	for _, m := range s.modes.All() {
		caps := s.cfg.Policy.Capabilities(m.ID())
		// Only advertise execution adapters that actually exist.
		caps["local"] = caps["local"] && (m.ID() == "classic" || m.ID() == "sort" || m.ID() == "memory" || m.ID() == "aptitude-grid" || m.ID() == "draft" || m.ID() == "race-rebuild" || m.ID() == "co-race-path" || m.ID() == "strategy-race" || m.ID() == "pedigree" || m.ID() == "audio")
		caps["verified"] = caps["verified"] && m.ID() == "classic"
		// Public queues are available only for curated fixed-rule modes.
		caps["matchmaking"] = caps["matchmaking"] && (m.ID() == "classic" || m.ID() == "sort" || m.ID() == "memory")
		if limit := s.cfg.Policy.Limits.PlayersPerRoom; limit > 0 {
			minimum := 4
			if m.ID() == "classic" {
				minimum = 2
			}
			caps["matchmaking"] = caps["matchmaking"] && limit >= minimum
		}
		caps["solo"] = caps["solo"] && m.MinSubjects() <= 1
		if !s.ModeAvailability(m).Available {
			for form := range caps {
				caps[form] = false
			}
		}
		out[m.ID()] = caps
	}
	return out
}

func (s *Service) hasPlayableForm(modeID string, minSubjects int) bool {
	for _, form := range deployment.Forms {
		if form == "local" && modeID != "classic" && modeID != "sort" && modeID != "memory" && modeID != "aptitude-grid" && modeID != "draft" && modeID != "race-rebuild" && modeID != "co-race-path" && modeID != "strategy-race" && modeID != "pedigree" && modeID != "audio" {
			continue
		}
		if form == "verified" && modeID != "classic" {
			continue
		}
		if form == "matchmaking" && modeID != "classic" && modeID != "sort" && modeID != "memory" {
			continue
		}
		if form == "solo" && minSubjects > 1 {
			continue
		}
		if s.cfg.Policy.Allowed(modeID, form) {
			return true
		}
	}
	return false
}
