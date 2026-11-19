package classic

import "encoding/json"

// SurrenderWinner identifies the remaining player in a two-subject race.
// Exhausting guesses is distinct from conceding and does not trigger this rule.
func SurrenderWinner(public json.RawMessage, states map[string]json.RawMessage) (string, error) {
	var pub publicPrepared
	if err := json.Unmarshal(public, &pub); err != nil {
		return "", err
	}
	if len(pub.Subjects) != 2 || pub.Config.WinPolicy != WinPolicyFirstCorrect {
		return "", nil
	}
	var winner string
	surrendered := 0
	for _, subject := range pub.Subjects {
		st, err := decodeState(states[subject.SubjectID])
		if err != nil {
			return "", err
		}
		if st.Surrendered {
			surrendered++
		} else {
			winner = subject.SubjectID
		}
	}
	if surrendered != 1 {
		return "", nil
	}
	return winner, nil
}
