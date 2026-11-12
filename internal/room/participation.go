package room

import (
	"encoding/json"
	"github.com/bhcn/umaguessinggame/internal/httpapi/problem"
	"github.com/bhcn/umaguessinggame/internal/modes"
)

func (s *Service) individualSeats(modeID string) bool {
	if s.modes == nil {
		return false
	}
	m, ok := s.modes.Get(modeID)
	if !ok {
		return false
	}
	individual, ok := m.(modes.IndividualParticipation)
	return ok && individual.UsesIndividualSeats()
}

func (s *Service) individualRoundSeats(r Round) bool {
	return s.individualSeatsForConfig(r.ModeID, modeConfigOf(r.Config))
}
func (s *Service) individualSeatsForConfig(modeID string, config json.RawMessage) bool {
	if s.modes != nil {
		if mode, ok := s.modes.Get(modeID); ok {
			if configured, ok := mode.(modes.ConfiguredParticipation); ok {
				return configured.IndividualSeatsForConfig(config)
			}
		}
	}
	return s.individualSeats(modeID)
}

func (s *Service) roundSubject(r Round, actor Actor) (modes.Subject, error) {
	if !s.individualRoundSeats(r) {
		return actor.Subject()
	}
	if actor.Membership == nil || !actor.Has(CapPlay) {
		return modes.Subject{}, problem.Forbidden("not-a-participant", "your role cannot play this round")
	}
	return modes.Subject{ID: actor.MembershipID(), Kind: modes.SubjectPlayer}, nil
}
