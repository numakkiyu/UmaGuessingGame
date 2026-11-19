package sortgame

import (
	"encoding/json"
	"os"
	"testing"
)

func TestPracticeSortRelationsGolden(t *testing.T) {
	raw, err := os.ReadFile("../../../web/src/lib/domain/testdata/sort-relations.json")
	if err != nil {
		t.Fatal(err)
	}
	var cases []struct {
		Name       string         `json:"name"`
		Values     map[string]int `json:"values"`
		Groups     [][]string     `json:"groups"`
		Correct    int            `json:"correct"`
		Total      int            `json:"total"`
		ScoreUnits int            `json:"scoreUnits"`
	}
	if err = json.Unmarshal(raw, &cases); err != nil {
		t.Fatal(err)
	}
	for _, test := range cases {
		t.Run(test.Name, func(t *testing.T) {
			order, slots := expandGroups(test.Groups)
			correct, total := compareOrder(order, slots, test.Values)
			if correct != test.Correct || total != test.Total || scoreUnits(correct, total) != test.ScoreUnits {
				t.Fatalf("got %d/%d %d", correct, total, scoreUnits(correct, total))
			}
		})
	}
}
