package draft

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

func TestSharedPracticeDraftRules(t *testing.T) {
	data, err := os.ReadFile("../../../web/src/lib/domain/testdata/aptitude-rules.json")
	if err != nil {
		t.Fatal(err)
	}
	var f struct {
		Draft []struct {
			Name          string
			Aptitudes     Aptitudes
			Price, Points int
		}
	}
	if err = json.Unmarshal(data, &f); err != nil {
		t.Fatal(err)
	}
	for _, c := range f.Draft {
		t.Run(c.Name, func(t *testing.T) {
			if PublishedPrice(c.Aptitudes) != c.Price {
				t.Fatal("price differs from shared fixture")
			}
			points := scoreTask(Task{TaskID: "test", Surface: "草地", Distance: "短", Style: "逃"}, Card{Aptitudes: c.Aptitudes})
			if points.Total != c.Points {
				t.Fatal("points differ from shared fixture", points)
			}
		})
	}
}
func TestPracticeDraftWhitelistsFactsAndVerifiesPrices(t *testing.T) {
	p := defaultMarket()
	var content map[string]any
	if err := json.Unmarshal(p["draft.market"], &content); err != nil {
		t.Fatal(err)
	}
	records := content["cards"].([]any)
	for _, v := range records {
		r := v.(map[string]any)
		r["secret"] = "DO_NOT_EXPORT"
		apt := r["aptitudes"].(map[string]any)
		apt["surface"].(map[string]any)["hidden"] = "A"
		r["price"] = 6
	}
	raw, _ := json.Marshal(content)
	p["draft.market"] = raw
	library, err := PracticeContent(p)
	if err != nil {
		t.Fatal(err)
	}
	data, _ := json.Marshal(library)
	if strings.Contains(string(data), "DO_NOT_EXPORT") || strings.Contains(string(data), "hidden") {
		t.Fatal("non-public field escaped")
	}
	records[0].(map[string]any)["price"] = 1
	raw, _ = json.Marshal(content)
	p["draft.market"] = raw
	if _, err = PracticeContent(p); err == nil {
		t.Fatal("inconsistent frozen price accepted")
	}
}
