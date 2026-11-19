package room

import (
	"github.com/bhcn/umaguessinggame/internal/httpapi/problem"
	"github.com/bhcn/umaguessinggame/internal/modes/strategyrace"
)

func (s *Service) StrategyPracticePack() (PracticePack, error) {
	if err := s.practiceAdmission("strategy-race"); err != nil {
		return PracticePack{}, err
	}
	s.strategyPracticeOnce.Do(func() {
		rules, err := strategyrace.PracticeRules(s.content)
		if err != nil {
			s.strategyPracticeErr = problem.Internal(err)
			return
		}
		s.strategyPracticePack, s.strategyPracticeErr = s.publicPracticeEnvelope("strategy-race", strategyrace.RulesVersion, rules)
	})
	return s.strategyPracticePack, s.strategyPracticeErr
}
