package racerebuild

import (
	"encoding/json"
	"errors"
	"github.com/bhcn/umaguessinggame/internal/modes"
	"os"
	"reflect"
	"strings"
	"testing"
)

func TestSharedPracticeRaceRelations(t *testing.T) {
	data, err := os.ReadFile("../../../web/src/lib/domain/testdata/race-relations.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixtures []struct {
		Name                       string
		Truth                      map[string]int
		Groups                     [][]string
		Correct, Total, ScoreUnits int
	}
	if err = json.Unmarshal(data, &fixtures); err != nil {
		t.Fatal(err)
	}
	for _, f := range fixtures {
		t.Run(f.Name, func(t *testing.T) {
			order, slots := expandGroups(f.Groups)
			correct, total := compareOrderWithSlots(order, slots, f.Truth, nil)
			if correct != f.Correct || total != f.Total || scoreUnits(correct, total) != f.ScoreUnits {
				t.Fatal("browser and Go scoring differ", correct, total)
			}
		})
	}
}
func TestPracticeExportsOnlyExplicitHistoricalRanks(t *testing.T) {
	race, answer := strictRace("r1")
	answer.Ranks = map[string]int{"a": 1, "b": 2, "c": 3, "d": 5, "e": 7}
	answer.Dropped = []string{"unknown:no_recorded_rank"}
	rows, err := PracticeRaces(packWithAnswers([]raceSpec{race}, []answerSpec{answer}))
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].Entries[4].FinishRank != 7 || rows[0].ExcludedCount != 1 {
		t.Fatal("real ranks lost", rows)
	}
	if _, err = PracticeRaces(strictPack("legacy")); !errors.Is(err, ErrPracticeUnavailable) {
		t.Fatal("old order-only pack pretended to preserve real ranks")
	}
	answer.Ties = map[string][]string{"2": {"b", "c"}}
	if _, err = PracticeRaces(packWithAnswers([]raceSpec{race}, []answerSpec{answer})); err == nil || errors.Is(err, ErrPracticeUnavailable) {
		t.Fatal("contradictory tie accepted")
	}
}
func TestRevisableRaceHidesCorrectnessAndRestoresTies(t *testing.T) {
	subject := modes.Subject{ID: "p1", Kind: modes.SubjectPlayer}
	prep, _, _ := prepare(t, strictPack("r1"), "s", "{}", subject)
	groups := [][]string{{"a"}, {"b", "c"}, {"d"}, {"e"}}
	dec, st := applyOne(t, prep, subject, nil, orderActionGroups(groups))
	if !reflect.DeepEqual(st.Groups, groups) {
		t.Fatal("tie groups lost in persistent state")
	}
	if strings.Contains(string(dec.Feedback), "correctPairs") || strings.Contains(string(dec.Feedback), "scoreUnits") {
		t.Fatal("revision feedback reveals answer quality")
	}
	for _, phase := range []modes.Phase{modes.PhaseCollecting, modes.PhaseRevealed} {
		raw, err := New().Project(modes.ProjectInput{Public: prep.Public, Private: prep.Private, States: map[string]json.RawMessage{"p1": dec.State}, Viewer: modes.Viewer{Role: "player", Subject: subject}, Phase: phase, RevealNow: phase == modes.PhaseRevealed})
		if err != nil {
			t.Fatal(err)
		}
		var result projection
		if err = json.Unmarshal(raw, &result); err != nil {
			t.Fatal(err)
		}
		if result.Self == nil || !reflect.DeepEqual(result.Self.Groups, groups) {
			t.Fatal("own tie groups cannot be restored")
		}
		if phase == modes.PhaseCollecting && (strings.Contains(string(raw), "correctPairs") || result.Answer != nil) {
			t.Fatal("player can probe score before reveal", string(raw))
		}
		if phase == modes.PhaseRevealed && (result.Self.CorrectPairs != st.CorrectPairs || result.Answer == nil) {
			t.Fatal("final result missing")
		}
	}
	// A second submission replaces the first one and remains open.
	next, updated := applyOne(t, prep, subject, dec.State, orderAction([]string{"a", "b", "c", "d", "e"}))
	if !next.Accepted || next.Finished || updated.Revisions != 2 || updated.CorrectPairs != 10 {
		t.Fatal("revision refused")
	}
}
