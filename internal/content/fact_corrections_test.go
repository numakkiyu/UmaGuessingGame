package content

import (
	"encoding/json"
	"net/url"
	"strings"
	"testing"
)

func TestRealSourceSchemaPreservesWikiAndMoegirlPages(t *testing.T) {
	var w RawWikiCharacter
	err := json.Unmarshal([]byte(`{"names":{"wikiTitle":"东海帝皇"},"sources":{"moegirl":"东海帝王","biliwiki":"biliwiki 赛马娘WIKI"}}`), &w)
	if err != nil {
		t.Fatal(err)
	}
	sources := sourcesFromWiki(&w)
	want := map[string]string{"biliwiki": "东海帝皇", "moegirl": "东海帝王"}
	if len(sources) != len(want) {
		t.Fatalf("sources: %#v", sources)
	}
	for _, source := range sources {
		if want[source.Site] != source.Page {
			t.Fatalf("lost source identity: %#v", source)
		}
		u, err := url.Parse(source.URL)
		if err != nil || u.Scheme != "https" || !strings.HasSuffix(u.Path, source.Page) {
			t.Fatalf("unusable source URL: %#v", source)
		}
		if source.RetrievedAt != "" {
			t.Fatal("constructed locator must not invent a fetch date")
		}
	}
}

func TestRacingCorrectionsReachPublishedFactsWithoutMutatingSnapshot(t *testing.T) {
	in, err := Load(DefaultSourcePaths(fixtureRoot(t)))
	if err != nil {
		t.Fatal(err)
	}
	var original RawQuestionBankEntry
	for _, r := range in.Ready {
		if r.ID == "goorudoshippu" {
			original = r
		}
	}
	ds, _, err := BuildDataset(in, "2026-11-17T00:00:00Z", "facts-test", fixtureRoot(t))
	if err != nil {
		t.Fatal(err)
	}
	c, ok := ds.CharacterByID("1007")
	if !ok || c.Groups == nil || c.Groups.G23Bracket != "4-6" {
		t.Fatalf("Gold Ship correction missing: %#v", c)
	}
	if original.G23Bracket != "1-3" {
		t.Fatal("test requires original legacy discrepancy")
	}
	for _, r := range in.Ready {
		if r.ID == original.ID && r.G23Bracket != original.G23Bracket {
			t.Fatal("raw snapshot was mutated")
		}
	}
	for _, r := range ds.Races {
		if r.RaceID != "198409030610" {
			continue
		}
		if r.Grade != "G1" {
			t.Fatalf("1984 Takarazuka grade = %s", r.Grade)
		}
		for _, e := range r.Entries {
			if e.Grade != "G1" {
				t.Fatal("starter grade disagrees with race grade")
			}
		}
	}
	ship, _ := ds.CharacterByID("1046")
	if !ship.Groups.G1HasJpn || !ship.Groups.G23HasJpn || ship.Groups.G23Bracket != "7+" {
		t.Fatal("Smart Falcon local graded wins were lost")
	}
	believe, ok := ds.CharacterByID("1095")
	if !ok || believe.Groups == nil || believe.Groups.G1Bracket != "1-2" {
		t.Fatal("Believe's two GI wins were replaced by missing local race history")
	}
}

func TestUnsupportedRacingCorrectionBlocksCompilation(t *testing.T) {
	in := &RawInputs{Ready: []RawQuestionBankEntry{{ID: "example"}}, Overrides: RawFieldOverrides{"example": {"g23_bracket": "4-6"}}}
	if err := validateFieldOverrides(in); err == nil {
		t.Fatal("unsourced correction was accepted")
	}
	in.Overrides["example"]["racing_source_url"] = "https://www.jra.go.jp/example/"
	in.Overrides["example"]["racing_checked_at"] = "2026-11-17"
	in.Overrides["example"]["racing_note"] = "All wins counted, including repeat wins"
	if err := validateFieldOverrides(in); err != nil {
		t.Fatal(err)
	}
	in.Overrides["example"]["g23_bracket"] = "5+"
	if err := validateFieldOverrides(in); err == nil {
		t.Fatal("wrong bracket vocabulary was accepted")
	}
}

func TestMalformedKnowledgeCannotBecomeAnAnswer(t *testing.T) {
	rank := 0
	c := RawWikiCharacter{ID: "example"}
	c.Game.Aptitudes.Surface = map[string]string{"turf": "S"}
	if err := validateKnowledge(&RawInputs{Wiki: RawWikiSnapshot{Characters: []RawWikiCharacter{c}}}); err == nil {
		t.Fatal("unsupported initial aptitude accepted")
	}
	c.Game.Aptitudes.Surface["turf"] = "A"
	c.PrototypeHorse.RaceHistory = []RawRaceRow{{ID: "race", FinishRank: &rank}}
	if err := validateKnowledge(&RawInputs{Wiki: RawWikiSnapshot{Characters: []RawWikiCharacter{c}}}); err == nil {
		t.Fatal("zero was accepted as a finish rank")
	}
	rank = 1
	other := c
	other.ID = "other"
	other.PrototypeHorse.RaceHistory = []RawRaceRow{{ID: "race", Distance: "2400m", FinishRank: &rank}}
	c.PrototypeHorse.RaceHistory[0].Distance = "2000m"
	if err := validateKnowledge(&RawInputs{Wiki: RawWikiSnapshot{Characters: []RawWikiCharacter{c, other}}}); err == nil {
		t.Fatal("contradictory race metadata accepted")
	}
}

func TestLegacyAptitudeGroupsMatchInitialGameData(t *testing.T) {
	in, err := Load(DefaultSourcePaths(fixtureRoot(t)))
	if err != nil {
		t.Fatal(err)
	}
	byName := map[string]RawWikiCharacter{}
	for _, c := range in.Wiki.Characters {
		byName[c.Names.Ja] = c
	}
	for _, r := range in.Ready {
		c, exists := byName[r.NameJp]
		if !exists {
			t.Fatalf("unresolved character %s", r.ID)
		}
		if r.Star == nil || c.Game.Star == nil || *r.Star != *c.Game.Star {
			t.Fatalf("initial rarity differs for %s", r.ID)
		}
		for _, group := range []struct {
			raw    map[string]string
			names  map[string]string
			legacy []string
		}{
			{c.Game.Aptitudes.Surface, map[string]string{"turf": "草地", "dirt": "泥地"}, r.SurfaceGroup},
			{c.Game.Aptitudes.Distance, map[string]string{"sprint": "短", "mile": "英", "medium": "中", "long": "长"}, r.DistanceGroup},
			{c.Game.Aptitudes.RunningStyle, map[string]string{"front": "逃", "pace": "先", "late": "差", "end": "追"}, r.RunningStyleGroup},
		} {
			n := 0
			for key, value := range group.raw {
				if value == "A" {
					n++
					if !containsString(group.legacy, group.names[key]) {
						t.Fatalf("legacy A group omits %s/%s", r.ID, key)
					}
				}
			}
			if n != len(group.legacy) {
				t.Fatalf("legacy A group has extra entries for %s", r.ID)
			}
		}
	}
}
