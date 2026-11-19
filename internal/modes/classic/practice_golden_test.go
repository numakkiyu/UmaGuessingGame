package classic_test

import (
	"encoding/json"
	"github.com/bhcn/umaguessinggame/internal/modes/classic"
	"github.com/bhcn/umaguessinggame/internal/room"
	"os"
	"reflect"
	"testing"
)

// Shared with the browser engine: a rule change must update both implementations
// deliberately, rather than allowing offline and online hints to drift.
func TestPracticeComparisonGolden(t *testing.T) {
	raw, err := os.ReadFile("../../../web/src/lib/domain/testdata/classic-comparison.json")
	if err != nil {
		t.Fatal(err)
	}
	var cases []struct {
		Name     string                 `json:"name"`
		Guess    room.PracticeCharacter `json:"guess"`
		Answer   room.PracticeCharacter `json:"answer"`
		Expected classic.GuessRow       `json:"expected"`
	}
	if err := json.Unmarshal(raw, &cases); err != nil {
		t.Fatal(err)
	}
	convert := func(c room.PracticeCharacter) classic.Character {
		return classic.Character{ID: c.CharacterID, Name: c.DisplayName, ImagePath: c.ImagePath, Star: c.Star, StarKnown: c.StarKnown, Surface: c.Surface, Distance: c.Distance, RunningStyle: c.RunningStyle, Sex: c.Sex, G1: c.G1, G23: c.G23, G1HasJpn: c.G1HasJpn, G23HasJpn: c.G23HasJpn, Grade: c.Grade, Dormitory: c.Dormitory}
	}
	for _, test := range cases {
		t.Run(test.Name, func(t *testing.T) {
			got := classic.BuildGuessRow(convert(test.Guess), convert(test.Answer))
			got.Attempt = 1
			got.Correct = test.Guess.CharacterID == test.Answer.CharacterID
			if !reflect.DeepEqual(got, test.Expected) {
				t.Fatalf("golden mismatch: got %#v want %#v", got, test.Expected)
			}
		})
	}
}
