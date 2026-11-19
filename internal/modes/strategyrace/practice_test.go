package strategyrace

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

func TestSharedSoloPracticeActionVectors(t *testing.T) {
	raw, err := os.ReadFile("../../../web/src/lib/domain/testdata/strategy-actions.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixtures []struct {
		Name         string
		Position     int
		Stamina      int
		Action       Action
		WantPosition int
		WantStamina  int
		Rejected     bool
	}
	if err = json.Unmarshal(raw, &fixtures); err != nil {
		t.Fatal(err)
	}
	for _, f := range fixtures {
		t.Run(f.Name, func(t *testing.T) {
			point := teamPoint{SubjectID: "solo", Position: f.Position, Stamina: f.Stamina}
			got, err := applyAction(fixtureTrack(t), f.Action, point, []teamPoint{point})
			if f.Rejected {
				if err == nil {
					t.Fatal("invalid action accepted")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if got.Position != f.WantPosition || got.Stamina != f.WantStamina {
				t.Fatalf("got %d/%d want %d/%d", got.Position, got.Stamina, f.WantPosition, f.WantStamina)
			}
		})
	}
}

func TestPracticeRulesExportOmitsInternalMetadata(t *testing.T) {
	baseline := fixtureBaseline()
	baseline["pendingActions"] = "DO_NOT_EXPORT"
	baseline["seed"] = "DO_NOT_EXPORT"
	raw, _ := json.Marshal(baseline)
	exported, err := PracticeRules(fakeContent{TrackSection: raw})
	if err != nil {
		t.Fatal(err)
	}
	encoded, _ := json.Marshal(exported)
	for _, forbidden := range []string{"DO_NOT_EXPORT", "test-fixture", "05-玩法", "basis", "author"} {
		if strings.Contains(string(encoded), forbidden) {
			t.Fatal("internal metadata exported", string(encoded))
		}
	}
	if !strings.Contains(string(encoded), `"simulation":true`) || !strings.Contains(string(encoded), `"notOfficialData":true`) {
		t.Fatal("missing simulation notice")
	}
	if _, err = PracticeRules(fakeContent{}); err == nil {
		t.Fatal("missing rules accepted")
	}
}
