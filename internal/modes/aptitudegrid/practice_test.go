package aptitudegrid

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

func TestSharedPracticeGridRules(t *testing.T) {
	data, err := os.ReadFile("../../../web/src/lib/domain/testdata/aptitude-rules.json")
	if err != nil {
		t.Fatal(err)
	}
	var f struct {
		Grid []struct {
			Name, Distance, Style string
			Fits                  bool
		}
	}
	if err = json.Unmarshal(data, &f); err != nil {
		t.Fatal(err)
	}
	for _, c := range f.Grid {
		t.Run(c.Name, func(t *testing.T) {
			a := Aptitudes{Distance: map[string]string{"短": c.Distance}, Style: map[string]string{"逃": c.Style}}
			if a.satisfies("distance", "短", "style", "逃") != c.Fits {
				t.Fatal("Go and browser rule fixture disagrees")
			}
		})
	}
}
func TestPracticeGridWhitelistsFactsAndRejectsUnsolvablePool(t *testing.T) {
	p := solvablePack()
	var records []map[string]any
	if err := json.Unmarshal(p["aptitude-grid.cards"], &records); err != nil {
		t.Fatal(err)
	}
	for _, r := range records {
		r["secret"] = "DO_NOT_EXPORT"
		apt := r["aptitudes"].(map[string]any)
		apt["surface"].(map[string]any)["hidden"] = "A"
		apt["surface"].(map[string]any)["泥地"] = "DO_NOT_EXPORT"
	}
	raw, _ := json.Marshal(records)
	p["aptitude-grid.cards"] = raw
	cards, err := PracticeCards(p)
	if err != nil {
		t.Fatal(err)
	}
	data, _ := json.Marshal(cards)
	if strings.Contains(string(data), "DO_NOT_EXPORT") || strings.Contains(string(data), "hidden") {
		t.Fatal("non-public field escaped")
	}
	if _, err = PracticeCards(packOf(fullCard("one", "甲", "短", "逃"))); err == nil {
		t.Fatal("unsolvable library accepted")
	}
}
