package room

import (
	"errors"

	"github.com/bhcn/umaguessinggame/internal/httpapi/problem"
	"github.com/bhcn/umaguessinggame/internal/modes/racerebuild"
)

type racePracticeEntry struct {
	racerebuild.PracticeEntry
	LocalizedNames map[string]string `json:"localizedNames,omitempty"`
}
type racePracticeRace struct {
	racerebuild.PracticeRace
	Entries []racePracticeEntry `json:"entries"`
}

func (s *Service) RacePracticePack() (PracticePack, error) {
	if err := s.practiceAdmission("race-rebuild"); err != nil {
		return PracticePack{}, err
	}
	s.racePracticeOnce.Do(func() {
		races, err := racerebuild.PracticeRaces(s.content)
		if err != nil {
			if errors.Is(err, racerebuild.ErrPracticeUnavailable) {
				s.racePracticeErr = problem.Unprocessable("practice-unavailable", "historical practice content unavailable").WithCause(err)
			} else {
				s.racePracticeErr = problem.Internal(err)
			}
			return
		}
		public := make([]racePracticeRace, 0, len(races))
		for _, race := range races {
			entries := make([]racePracticeEntry, 0, len(race.Entries))
			for _, entry := range race.Entries {
				entries = append(entries, racePracticeEntry{PracticeEntry: entry, LocalizedNames: s.practiceNames(entry.CharacterID)})
			}
			public = append(public, racePracticeRace{PracticeRace: race, Entries: entries})
		}
		s.racePracticePack, s.racePracticeErr = s.publicPracticeEnvelope("race-rebuild", racerebuild.RulesVersion, struct {
			Races []racePracticeRace `json:"races"`
		}{public})
	})
	return s.racePracticePack, s.racePracticeErr
}
