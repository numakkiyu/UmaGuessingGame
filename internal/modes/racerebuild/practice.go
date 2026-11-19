package racerebuild

import (
	"errors"
	"fmt"
	"github.com/bhcn/umaguessinggame/internal/modes"
	"sort"
)

// ErrPracticeUnavailable marks a valid pack that cannot supply this practice
// variant. Malformed or contradictory facts remain internal content errors.
var ErrPracticeUnavailable = errors.New("racerebuild: historical practice unavailable")

// PracticeRace is an explicit historical-fact export for unranked practice.
// These facts already describe public historical results. This is deliberately
// separate from competitive projections, which must continue to hide the answer.
type PracticeRace struct {
	RaceID        string          `json:"raceId"`
	Name          string          `json:"name"`
	Date          string          `json:"date"`
	Course        string          `json:"course"`
	Distance      string          `json:"distance"`
	Surface       string          `json:"surface"`
	Entries       []PracticeEntry `json:"entries"`
	ExcludedCount int             `json:"excludedCount"`
}
type PracticeEntry struct {
	CharacterID string `json:"characterId"`
	Name        string `json:"name"`
	FinishRank  int    `json:"finishRank"`
}

func PracticeRaces(source modes.ContentSource) ([]PracticeRace, error) {
	races, err := loadRaces(source)
	if err != nil {
		return nil, err
	}
	answers, err := loadAnswers(source)
	if err != nil {
		return nil, err
	}
	out := []PracticeRace{}
	for _, race := range races {
		answer, ok := answers[race.RaceID]
		if !ok {
			continue
		}
		entries := usableStarters(race, answer)
		if len(entries) < MinParticipants || len(entries) > MaxParticipants {
			continue
		}
		if len(answer.Ranks) == 0 {
			return nil, fmt.Errorf("%w: recorded ranks required", ErrPracticeUnavailable)
		}
		// An old order-only pack cannot prove actual source rank numbers. Refuse it
		// instead of silently pretending its consecutive positions are real finishes.
		ranks := make(map[string]int, len(entries))
		used := map[string]bool{}
		for _, entry := range entries {
			rank, exists := answer.Ranks[entry.CharacterID]
			if !exists || rank < 1 || rank > 100 || used[entry.CharacterID] {
				return nil, fmt.Errorf("racerebuild: practice needs explicit recorded ranks")
			}
			ranks[entry.CharacterID] = rank
			used[entry.CharacterID] = true
		}
		// Reject contradictory explicit tie metadata rather than hiding it in export.
		for _, ids := range answer.Ties {
			if len(ids) < 2 {
				continue
			}
			rank := ranks[ids[0]]
			for _, id := range ids {
				if ranks[id] != rank || rank == 0 {
					return nil, fmt.Errorf("racerebuild: contradictory recorded tie")
				}
			}
		}
		row := PracticeRace{RaceID: race.RaceID, Name: race.Name, Date: race.Date, Course: race.Course, Distance: race.Distance, Surface: race.Surface, Entries: []PracticeEntry{}, ExcludedCount: len(answer.Dropped)}
		for _, entry := range entries {
			row.Entries = append(row.Entries, PracticeEntry{entry.CharacterID, entry.Name, ranks[entry.CharacterID]})
		}
		sort.Slice(row.Entries, func(i, j int) bool { return row.Entries[i].CharacterID < row.Entries[j].CharacterID })
		out = append(out, row)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("%w: no eligible historical race", ErrPracticeUnavailable)
	}
	return out, nil
}
