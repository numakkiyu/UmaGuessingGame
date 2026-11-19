package strategyrace

import "github.com/bhcn/umaguessinggame/internal/modes"

// PracticeRules deliberately exports only the authored public action baseline.
// No committed room decisions or pending player actions are read here.
func PracticeRules(src modes.ContentSource) (any, error) {
	track, _, err := loadTrack(src)
	if err != nil {
		return nil, err
	}
	return struct {
		Track           trackRules `json:"track"`
		Simulation      bool       `json:"simulation"`
		NotOfficialData bool       `json:"notOfficialData"`
	}{track, true, true}, nil
}
