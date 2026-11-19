package httpapi

import (
	"encoding/json"
	"testing"

	"github.com/bhcn/umaguessinggame/internal/room"
)

func TestSurrenderResultsKeepTheirOutcome(t *testing.T) {
	for _, tc := range []struct {
		name, explanation, want string
	}{
		{"opponent conceded", `{"solved":false,"wonBySurrender":true}`, "won"},
		{"winner exhausted guesses", `{"solved":false,"failed":true,"wonBySurrender":true}`, "won"},
		{"conceded after exhausting guesses", `{"failed":true,"surrendered":true}`, "surrendered"},
		{"conceded", `{"solved":false,"surrendered":true}`, "surrendered"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			results := toResultDTOs([]room.StoredResult{{SubjectID: "player", Explanation: json.RawMessage(tc.explanation)}})
			if len(results) != 1 || results[0].Outcome != tc.want {
				t.Fatalf("result outcome = %+v, want %q", results, tc.want)
			}
		})
	}
}
