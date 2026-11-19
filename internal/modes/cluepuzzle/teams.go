package cluepuzzle

import (
	"encoding/json"
	"fmt"

	"github.com/bhcn/umaguessinggame/internal/modes"
)

// ValidateRoster counts members, not the folded scoring subjects.
func (*Mode) ValidateRoster(subjects []modes.Subject, participants []modes.Participant) error {
	_, err := teamRoster(subjects, participants)
	return err
}

func teamRoster(subjects []modes.Subject, participants []modes.Participant) (map[string][]string, error) {
	if len(subjects) < 1 || len(subjects) > 8 {
		return nil, fmt.Errorf("cluepuzzle: requires 1–8 teams")
	}
	groups := make(map[string][]string, len(subjects))
	for _, subject := range subjects {
		if subject.Kind != modes.SubjectTeam || subject.ID == "" {
			return nil, fmt.Errorf("cluepuzzle: assign all players to teams before starting")
		}
		if _, exists := groups[subject.ID]; exists {
			return nil, fmt.Errorf("cluepuzzle: duplicate team")
		}
		groups[subject.ID] = nil
	}
	seen := make(map[string]bool, len(participants))
	for _, p := range participants {
		if _, ok := groups[p.Subject.ID]; !ok || p.Subject.Kind != modes.SubjectTeam || p.MembershipID == "" || seen[p.MembershipID] {
			return nil, fmt.Errorf("cluepuzzle: invalid or duplicate participant")
		}
		seen[p.MembershipID] = true
		groups[p.Subject.ID] = append(groups[p.Subject.ID], p.MembershipID)
	}
	for _, s := range subjects {
		n := len(groups[s.ID])
		if n < MinParticipants || n > MaxParticipants {
			return nil, fmt.Errorf("cluepuzzle: each team needs 3–6 players")
		}
	}
	return groups, nil
}

func hasMember(members []string, member string) bool {
	if member == "" {
		return false
	}
	for _, id := range members {
		if id == member {
			return true
		}
	}
	return false
}

// A complete accepted assignment hands the board to the next member. Partial
// edits stay in the current browser and never advance the team's controller.
func (*Mode) ControlTurn(_ json.RawMessage, state json.RawMessage) (string, bool, error) {
	st, err := decodeState(state)
	if err != nil {
		return "", false, err
	}
	return fmt.Sprint(st.Submissions), true, nil
}
