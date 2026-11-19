package content

import (
	"fmt"
	"strings"
	"testing"
)

func poolCharacter(i int) Character {
	image := fmt.Sprintf("/assets/avatars/%d.webp", i)
	return Character{ID: fmt.Sprintf("c%d", i), EntityKind: EntityPlayable, Names: Names{ZhHans: "角色"}, ImagePath: &image, Aptitudes: Aptitudes{
		Surface:      map[string]string{"turf": "A", "dirt": "G"},
		Distance:     map[string]string{"sprint": "A", "mile": "A", "medium": "A", "long": "G"},
		RunningStyle: map[string]string{"front": "A", "pace": "A", "late": "A", "end": "G"},
	}}
}

func TestGridAdmissionRequiresNineDistinctMatches(t *testing.T) {
	ds := &Dataset{}
	for i := 0; i < 9; i++ {
		c := poolCharacter(i)
		if i > 0 {
			c.Aptitudes.RunningStyle["late"] = "G"
		}
		ds.Characters = append(ds.Characters, c)
	}
	// All nine cells are individually covered, but three late-style cells share one card.
	result, err := compileAptitudeGrid(&compileContext{ds: ds})
	if err != nil {
		t.Fatal(err)
	}
	if result.admission.Available {
		t.Fatal("grid with no distinct-card solution was admitted")
	}
	for i := range ds.Characters {
		ds.Characters[i].Aptitudes.RunningStyle["late"] = "A"
	}
	result, err = compileAptitudeGrid(&compileContext{ds: ds})
	if err != nil || !result.admission.Available {
		t.Fatalf("solvable grid refused: %+v %v", result.admission, err)
	}
}

func TestSortAdmissionCountsValuesPerKey(t *testing.T) {
	ds := &Dataset{}
	for i := 0; i < 12; i++ {
		c := poolCharacter(i)
		h, m, d := 150+i, 1, i+1
		if i < 2 {
			c.Profile.HeightCm = &h
		} else {
			c.Profile.BirthdayMonth = &m
			c.Profile.BirthdayDay = &d
		}
		ds.Characters = append(ds.Characters, c)
	}
	result, err := compileSort(&compileContext{ds: ds})
	if err != nil {
		t.Fatal(err)
	}
	if result.admission.Available {
		t.Fatal("default height game admitted with only two height records")
	}
	for i := range ds.Characters {
		h := 150 + i
		ds.Characters[i].Profile.HeightCm = &h
		ds.Characters[i].Profile.BirthdayDay = nil
	}
	result, err = compileSort(&compileContext{ds: ds})
	if err != nil || !result.admission.Available {
		t.Fatalf("valid height library refused: %+v %v", result.admission, err)
	}
	section := result.sections[SectionSortCards].(sortSection)
	if len(section.SortableKeys) != 1 || section.SortableKeys[0] != "heightCm" {
		t.Fatalf("unsupported keys advertised: %+v", section.SortableKeys)
	}
}

func TestClassicAdmissionNeedsUnambiguousAnswers(t *testing.T) {
	ds := &Dataset{}
	star := 3
	for i := 0; i < classicMinPool; i++ {
		c := poolCharacter(i)
		c.Groups = &LegacyGroups{Star: &star, SurfaceGroup: []string{"草地"}, DistanceGroup: []string{"中"}, RunningStyleGroup: []string{"先"}, SexType: "牡", G1Bracket: "0", G23Bracket: "0", SchoolGrade: "中等部", Dormitory: "栗东宿舍"}
		ds.Characters = append(ds.Characters, c)
	}
	result, err := compileClassic(&compileContext{ds: ds})
	if err != nil {
		t.Fatal(err)
	}
	if result.admission.Available {
		t.Fatal("all-ambiguous answer library was admitted")
	}
}

func TestSourceIdentityCollisionsAreRejectedBeforeJoining(t *testing.T) {
	for _, test := range []struct {
		name    string
		input   RawInputs
		message string
	}{
		{"canonical id", RawInputs{Wiki: RawWikiSnapshot{Characters: []RawWikiCharacter{{ID: "same"}, {ID: "same"}}}}, "duplicate canonical id"},
		{"empty canonical id", RawInputs{Wiki: RawWikiSnapshot{Characters: []RawWikiCharacter{{}}}}, "empty canonical id"},
		{"normalized id", RawInputs{Normalized: []RawNormalizedCharacter{{ID: "same", FileName: "a"}, {ID: "same", FileName: "b"}}}, "duplicate normalized id"},
		{"normalized file", RawInputs{Normalized: []RawNormalizedCharacter{{ID: "a", FileName: "same"}, {ID: "b", FileName: "same"}}}, "duplicate normalized filename"},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, _, err := BuildDataset(&test.input, "2026-11-12T00:00:00Z", "ds-test", t.TempDir())
			if err == nil || !strings.Contains(err.Error(), test.message) {
				t.Fatalf("identity collision was not rejected: %v", err)
			}
		})
	}
}
