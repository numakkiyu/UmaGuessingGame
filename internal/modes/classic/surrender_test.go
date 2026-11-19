package classic

import (
	"encoding/json"
	"testing"
)

func TestSurrenderWinnerOnlyEndsFirstCorrectDuels(t *testing.T) {
	for _, tc := range []struct {
		name, policy string
		count        int
		states       map[string]json.RawMessage
		want         string
	}{
		{"duel", "first-correct", 2, map[string]json.RawMessage{"a": json.RawMessage(`{"surrendered":true}`)}, "b"},
		{"exhausted opponent wins", "first-correct", 2, map[string]json.RawMessage{"a": json.RawMessage(`{"surrendered":true}`), "b": json.RawMessage(`{"failed":true}`)}, "b"},
		{"exhaustion is not surrender", "first-correct", 2, map[string]json.RawMessage{"a": json.RawMessage(`{"failed":true}`)}, ""},
		{"four player race continues", "first-correct", 4, map[string]json.RawMessage{"a": json.RawMessage(`{"surrendered":true}`)}, ""},
		{"window scoring continues", "window-attempts", 2, map[string]json.RawMessage{"a": json.RawMessage(`{"surrendered":true}`)}, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			subjects := []map[string]string{}
			for _, id := range []string{"a", "b", "c", "d"}[:tc.count] {
				subjects = append(subjects, map[string]string{"subjectId": id})
			}
			public, err := json.Marshal(map[string]any{"config": map[string]string{"winPolicy": tc.policy}, "subjects": subjects})
			if err != nil {
				t.Fatal(err)
			}
			got, err := SurrenderWinner(public, tc.states)
			if err != nil || got != tc.want {
				t.Fatalf("winner = %q, error = %v, want %q", got, err, tc.want)
			}
		})
	}
}
