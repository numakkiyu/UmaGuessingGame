package content

import (
	"fmt"
	"reflect"
	"testing"
)

func TestRaceCompilerPreservesSourceRanksAndUnknowns(t *testing.T) {
	ds := &Dataset{}
	for i := 0; i < raceRebuildMinRaces; i++ {
		entries := []RaceEntry{}
		for j, rank := range []int{1, 3, 3, 7} {
			r := rank
			entries = append(entries, RaceEntry{CharacterID: fmt.Sprintf("c%d", j), CharacterName: "角色", FinishRank: &r})
		}
		entries = append(entries, RaceEntry{CharacterID: "unknown", CharacterName: "未知"})
		ds.Races = append(ds.Races, Race{RaceID: fmt.Sprintf("race%d", i), Name: "测试赛", Date: "2026-01-02", Entries: entries})
	}
	result, err := compileRaceRebuild(&compileContext{ds: ds})
	if err != nil {
		t.Fatal(err)
	}
	answers, ok := result.secrets[PrivateRaceRebuildAnswers].(privateRaceAnswers)
	if !ok || len(answers.Races) != raceRebuildMinRaces {
		t.Fatal("missing compiled ranks")
	}
	for _, a := range answers.Races {
		if !reflect.DeepEqual(a.Ranks, map[string]int{"c0": 1, "c1": 3, "c2": 3, "c3": 7}) {
			t.Fatal("source rank renumbered", a)
		}
		if len(a.Dropped) != 1 || !reflect.DeepEqual(a.Ties["3"], []string{"c1", "c2"}) {
			t.Fatal("tie or missing rank lost", a)
		}
	}
	public := result.sections[SectionRaceRebuildRaces].([]raceRebuildRace)
	for _, r := range public {
		if len(r.Starters) != 4 {
			t.Fatal("unknown rank was not excluded")
		}
	}
}

func TestPrivateRankCorrectionChangesPackBinding(t *testing.T) {
	ds := &Dataset{DatasetVersion: "ds-same"}
	public := map[string]any{"race-rebuild.races": []string{"same-question"}}
	a, err := computePackVersion(ds, nil, public, map[string]any{"race-rebuild.answers": map[string]int{"a": 1, "b": 7}})
	if err != nil {
		t.Fatal(err)
	}
	b, err := computePackVersion(ds, nil, public, map[string]any{"race-rebuild.answers": map[string]int{"a": 1, "b": 5}})
	if err != nil {
		t.Fatal(err)
	}
	if a == b {
		t.Fatal("different hidden grading facts reused immutable pack identity")
	}
	stable, err := computePackVersion(ds, nil, public, map[string]any{"race-rebuild.answers": map[string]int{"b": 7, "a": 1}})
	if err != nil || a != stable {
		t.Fatal("map insertion order changed pack identity")
	}
}
