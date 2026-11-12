package sortgame

import "github.com/bhcn/umaguessinggame/internal/modes"

// PracticeProfile exports only established public archive facts, never a prepared
// online question, its seed, player state or hidden answer ordering.
type PracticeProfile struct {
	CharacterID string         `json:"characterId"`
	DisplayName string         `json:"displayName"`
	ImagePath   string         `json:"imagePath"`
	Values      map[string]int `json:"values"`
}

func PracticeProfiles(source modes.ContentSource) ([]PracticeProfile, error) {
	cards, err := loadCards(source)
	if err != nil {
		return nil, err
	}
	profiles := make([]PracticeProfile, 0, len(cards))
	for _, card := range cards {
		values := map[string]int{}
		for _, key := range []string{KeyHeight, KeyBirthday} {
			if value, ok := card.value(key); ok {
				values[key] = value.a
			}
		}
		if len(values) == 0 {
			continue
		}
		profiles = append(profiles, PracticeProfile{card.CharacterID, card.Name, card.ImagePath, values})
	}
	return profiles, nil
}
