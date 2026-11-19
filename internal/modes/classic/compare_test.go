package classic

import (
	"encoding/json"
	"testing"

	"github.com/bhcn/umaguessinggame/internal/modes"
	"github.com/bhcn/umaguessinggame/internal/modes/modeutil"
)

// baseEntry mirrors the baseEntry fixture in tests/compare.test.ts so the Go
// engine is asserted against the same reference vector as the legacy
// TypeScript implementation.
func baseEntry() Character {
	return Character{
		ID:           "special_week",
		Name:         "特别周",
		ImagePath:    "/api/assets/special_week-thumb",
		Star:         3,
		StarKnown:    true,
		Surface:      []string{"草地"},
		Distance:     []string{"中", "长"},
		RunningStyle: []string{"先", "差"},
		Sex:          "牝",
		G1:           "3-4",
		G23:          "4-6",
		Grade:        "中等部",
		Dormitory:    "栗东宿舍",
	}
}

// TestCompareStarNearWrong pins the star rule: equal is correct, a difference of
// exactly one is near, anything else is wrong .
func TestCompareStarNearWrong(t *testing.T) {
	answer := baseEntry()
	cases := []struct {
		name  string
		guess int
		known bool
		want  CellStatus
	}{
		{name: "same star is correct", guess: 3, known: true, want: StatusCorrect},
		{name: "one lower is near", guess: 2, known: true, want: StatusNear},
		{name: "one higher is near", guess: 4, known: true, want: StatusNear},
		{name: "two lower is wrong", guess: 1, known: true, want: StatusWrong},
		{name: "unknown star never compares green", guess: 0, known: false, want: StatusWrong},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			guess := baseEntry()
			guess.Star, guess.StarKnown = tc.guess, tc.known
			if got := compareStar(guess, answer); got != tc.want {
				t.Fatalf("compareStar = %q, want %q", got, tc.want)
			}
		})
	}
}

// TestCompareSurface covers the set rules: identical set is correct, any overlap
// is near, disjoint is wrong .
func TestCompareSurface(t *testing.T) {
	answer := baseEntry()
	cases := []struct {
		name  string
		guess []string
		want  CellStatus
	}{
		{name: "identical set is correct", guess: []string{"草地"}, want: StatusCorrect},
		{name: "duplicate and reordered still correct", guess: []string{"草地", "草地"}, want: StatusCorrect},
		{name: "superset is not equal", guess: []string{"草地", "泥地"}, want: StatusNear},
		{name: "disjoint is wrong", guess: []string{"泥地"}, want: StatusWrong},
		{name: "empty is wrong", guess: nil, want: StatusWrong},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			guess := baseEntry()
			guess.Surface = tc.guess
			if got := compareSurface(guess, answer); got != tc.want {
				t.Fatalf("compareSurface = %q, want %q", got, tc.want)
			}
		})
	}
}

// TestCompareDistance ports the legacy assertion "marks overlapping distance as
// near" and pins the adjacency rule 短→英→中→长 .
func TestCompareDistance(t *testing.T) {
	answer := baseEntry() // 中/长
	cases := []struct {
		name  string
		guess []string
		want  CellStatus
	}{
		{name: "identical set is correct", guess: []string{"中", "长"}, want: StatusCorrect},
		{name: "reordered identical set is correct", guess: []string{"长", "中"}, want: StatusCorrect},
		{name: "legacy overlap case is near", guess: []string{"英", "中"}, want: StatusNear},
		{name: "short is two rungs from middle so it is wrong", guess: []string{"短"}, want: StatusWrong},
		{name: "adjacent english-to-middle is near", guess: []string{"英"}, want: StatusNear},
		{name: "english is adjacent to middle so it is near", guess: []string{"短", "英"}, want: StatusNear},
		{name: "short only is two rungs away so it is wrong", guess: []string{"短"}, want: StatusWrong},
		{name: "empty is wrong", guess: nil, want: StatusWrong},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			guess := baseEntry()
			guess.Distance = tc.guess
			if got := compareDistance(guess, answer); got != tc.want {
				t.Fatalf("compareDistance = %q, want %q", got, tc.want)
			}
		})
	}
}

// TestCompareStyle pins the running-style adjacency rule 逃→先→差→追 .
func TestCompareStyle(t *testing.T) {
	answer := baseEntry() // 先/差
	cases := []struct {
		name  string
		guess []string
		want  CellStatus
	}{
		{name: "identical set is correct", guess: []string{"先", "差"}, want: StatusCorrect},
		{name: "overlap is near", guess: []string{"逃", "先"}, want: StatusNear},
		{name: "adjacent escape is near", guess: []string{"逃"}, want: StatusNear},
		{name: "adjacent chase is near", guess: []string{"追"}, want: StatusNear},
		{name: "escape is adjacent to lead so it is near", guess: []string{"逃", "追"}, want: StatusNear},
		{name: "empty is wrong", guess: nil, want: StatusWrong},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			guess := baseEntry()
			guess.RunningStyle = tc.guess
			if got := compareStyle(guess, answer); got != tc.want {
				t.Fatalf("compareStyle = %q, want %q", got, tc.want)
			}
		})
	}
}

// TestCompareBrackets pins the two ordinal ladders and their adjacency rule:
// G1 is 0/1-2/3-4/5+ and G2G3 is 0/1-3/4-6/7+ .
func TestCompareBrackets(t *testing.T) {
	t.Run("g1", func(t *testing.T) {
		cases := []struct {
			name  string
			guess string
			want  CellStatus
		}{
			{name: "equal is correct", guess: "3-4", want: StatusCorrect},
			{name: "one rung down is near", guess: "1-2", want: StatusNear},
			{name: "one rung up is near", guess: "5+", want: StatusNear},
			{name: "two rungs down is wrong", guess: "0", want: StatusWrong},
			{name: "unknown is wrong", guess: "", want: StatusWrong},
			{name: "off-ladder value is wrong", guess: "9-9", want: StatusWrong},
		}
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				guess := baseEntry()
				guess.G1 = tc.guess
				if got := compareOrdinal(guess.G1, baseEntry().G1, g1Order); got != tc.want {
					t.Fatalf("g1 %q = %q, want %q", tc.guess, got, tc.want)
				}
			})
		}
	})
	t.Run("g23", func(t *testing.T) {
		cases := []struct {
			name  string
			guess string
			want  CellStatus
		}{
			{name: "equal is correct", guess: "4-6", want: StatusCorrect},
			{name: "one rung down is near", guess: "1-3", want: StatusNear},
			{name: "one rung up is near", guess: "7+", want: StatusNear},
			{name: "two rungs down is wrong", guess: "0", want: StatusWrong},
			{name: "g1 ladder value is not on the g23 ladder", guess: "1-2", want: StatusWrong},
		}
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				if got := compareOrdinal(tc.guess, baseEntry().G23, g23Order); got != tc.want {
					t.Fatalf("g23 %q = %q, want %q", tc.guess, got, tc.want)
				}
			})
		}
	})
}

// TestEqualityOnlyFields asserts that sex, school grade and dormitory only ever
// distinguish equal from unequal and never produce "near" .
func TestEqualityOnlyFields(t *testing.T) {
	answer := baseEntry()
	cases := []struct {
		name  string
		field string
		guess string
		want  CellStatus
	}{
		{name: "sex equal", field: FieldSex, guess: "牝", want: StatusCorrect},
		{name: "sex different", field: FieldSex, guess: "牡", want: StatusWrong},
		{name: "grade equal", field: FieldGrade, guess: "中等部", want: StatusCorrect},
		{name: "grade different", field: FieldGrade, guess: "高等部", want: StatusWrong},
		{name: "dorm equal", field: FieldDormitory, guess: "栗东宿舍", want: StatusCorrect},
		{name: "dorm different", field: FieldDormitory, guess: "美浦宿舍", want: StatusWrong},
		{name: "unknown dorm is wrong", field: FieldDormitory, guess: "", want: StatusWrong},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := compareEquality(tc.guess, answer.Dormitory); tc.field == FieldDormitory && got != tc.want {
				t.Fatalf("%s %q = %q, want %q", tc.field, tc.guess, got, tc.want)
			}
			if got := compareEquality(tc.guess, answer.Grade); tc.field == FieldGrade && got != tc.want {
				t.Fatalf("%s %q = %q, want %q", tc.field, tc.guess, got, tc.want)
			}
			if got := compareEquality(tc.guess, answer.Sex); tc.field == FieldSex && got != tc.want {
				t.Fatalf("%s %q = %q, want %q", tc.field, tc.guess, got, tc.want)
			}
		})
	}
}

// TestBuildGuessRowValues ports the legacy row assertion: the values are
// rendered and the statuses attached, in the fixed nine-field order.
func TestBuildGuessRowValues(t *testing.T) {
	row := BuildGuessRow(baseEntry(), baseEntry())
	// cells is a named object in the contract, so the nine fields are addressed by
	// name rather than by array position.
	if got := len(row.Cells.all()); got != 9 {
		t.Fatalf("cells = %d, want 9", got)
	}
	for i, field := range FieldOrder {
		cell, ok := row.cellAt(field)
		if !ok {
			t.Fatalf("cell %d (%q) is missing", i, field)
		}
		if cell.Field != field {
			t.Fatalf("cell %d field = %q, want %q", i, cell.Field, field)
		}
	}
	if got := row.Cells.Star.Value; got != "3星" {
		t.Fatalf("star value = %q, want %q", got, "3星")
	}
	if got := row.Cells.Star.Status; got != StatusCorrect {
		t.Fatalf("star status = %q, want correct", got)
	}
	if got := row.Cells.Surface.Value; got != "草地" {
		t.Fatalf("surface value = %q, want 草地", got)
	}
	if row.DisplayName != "特别周" {
		t.Fatalf("displayName = %q", row.DisplayName)
	}
}

// TestBuildGuessRowJMarker ports the legacy J assertion exactly: the marker is
// shown only when that bracket matched and the answer carries a Jpn grade
// .
func TestBuildGuessRowJMarker(t *testing.T) {
	answer := baseEntry()
	answer.G1HasJpn = true
	answer.G23HasJpn = true

	correctRow := BuildGuessRow(baseEntry(), answer)
	if got := correctRow.Cells.G1.Value; got != "3-4J" {
		t.Fatalf("g1 value = %q, want 3-4J", got)
	}
	if got := correctRow.Cells.G23.Value; got != "4-6J" {
		t.Fatalf("g23 value = %q, want 4-6J", got)
	}

	nearGuess := baseEntry()
	nearGuess.G1 = "1-2"
	nearGuess.G23 = "7+"
	nearRow := BuildGuessRow(nearGuess, answer)
	if got := nearRow.Cells.G1.Value; got != "1-2" {
		t.Fatalf("near g1 value = %q, want 1-2 without J", got)
	}
	if got := nearRow.Cells.G23.Value; got != "7+" {
		t.Fatalf("near g23 value = %q, want 7+ without J", got)
	}

	// A matching bracket with no Jpn grade in the answer must not gain a J.
	plainRow := BuildGuessRow(baseEntry(), baseEntry())
	if got := plainRow.Cells.G1.Value; got != "3-4" {
		t.Fatalf("plain g1 value = %q, want 3-4 without J", got)
	}
}

// TestActivityScore covers the activity formula including both floors
// .
func TestActivityScore(t *testing.T) {
	cases := []struct {
		name        string
		submissions int
		solved      bool
		budget      int
		wantUnits   int
	}{
		{name: "first try is 100", submissions: 1, solved: true, budget: 8, wantUnits: 10000},
		{name: "second try is 92", submissions: 2, solved: true, budget: 8, wantUnits: 9200},
		{name: "eighth try is 44", submissions: 8, solved: true, budget: 8, wantUnits: 4400},
		{name: "unsolved is 0", submissions: 8, solved: false, budget: 8, wantUnits: 0},
		{name: "no submissions is 0", submissions: 0, solved: false, budget: 8, wantUnits: 0},
		{name: "default budget floor holds at 44", submissions: 20, solved: true, budget: 8, wantUnits: 4400},
		{name: "non-default budget ninth try uses the 20 floor", submissions: 9, solved: true, budget: 12, wantUnits: 3600},
		{name: "non-default budget tenth try uses the 20 floor", submissions: 10, solved: true, budget: 12, wantUnits: 2800},
		{name: "non-default budget eleventh try clamps to 20", submissions: 11, solved: true, budget: 12, wantUnits: 2000},
		{name: "non-default budget twelfth try stays at 20", submissions: 12, solved: true, budget: 12, wantUnits: 2000},
		{name: "non-default budget eighth try keeps the formula", submissions: 8, solved: true, budget: 12, wantUnits: 4400},
		{name: "non-default budget fourth try is 76", submissions: 4, solved: true, budget: 12, wantUnits: 7600},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := ActivityScore(tc.submissions, tc.solved, tc.budget); got != tc.wantUnits {
				t.Fatalf("ActivityScore(%d,%v,%d) = %d, want %d",
					tc.submissions, tc.solved, tc.budget, got, tc.wantUnits)
			}
		})
	}
}

// TestConfigValidation covers the configurable budget bounds 4–12 and the
// variant/time-limit coupling .
func TestConfigValidation(t *testing.T) {
	cases := []struct {
		name    string
		cfg     string
		wantErr bool
		check   func(t *testing.T, cfg Config)
	}{
		{name: "empty config uses defaults", cfg: ``,
			check: func(t *testing.T, cfg Config) {
				if cfg.WrongBudget != DefaultWrongBudget {
					t.Fatalf("budget = %d, want %d", cfg.WrongBudget, DefaultWrongBudget)
				}
				if cfg.Variant != VariantSingle {
					t.Fatalf("variant = %q", cfg.Variant)
				}
			}},
		{name: "explicit 4 is allowed", cfg: `{"wrongBudget":4}`,
			check: func(t *testing.T, cfg Config) {
				if cfg.WrongBudget != 4 {
					t.Fatalf("budget = %d", cfg.WrongBudget)
				}
			}},
		{name: "explicit 12 is allowed", cfg: `{"wrongBudget":12}`,
			check: func(t *testing.T, cfg Config) {
				if cfg.WrongBudget != 12 {
					t.Fatalf("budget = %d", cfg.WrongBudget)
				}
			}},
		{name: "3 is rejected", cfg: `{"wrongBudget":3}`, wantErr: true},
		{name: "13 is rejected", cfg: `{"wrongBudget":13}`, wantErr: true},
		{name: "unknown variant is rejected", cfg: `{"variant":"bogus"}`, wantErr: true},
		{name: "solo with a time limit is rejected", cfg: `{"timeLimitSeconds":60}`, wantErr: true},
		{name: "public defaults to a 180s window", cfg: `{"variant":"public"}`,
			check: func(t *testing.T, cfg Config) {
				if cfg.TimeLimitSeconds != 180 {
					t.Fatalf("timeLimit = %d, want 180", cfg.TimeLimitSeconds)
				}
			}},
		{name: "friend needs two subjects", cfg: `{"variant":"friend"}`,
			check: func(t *testing.T, cfg Config) {
				if cfg.TimeLimitSeconds != 180 {
					t.Fatalf("timeLimit = %d, want 180", cfg.TimeLimitSeconds)
				}
			}},
	}
	m := New()
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := m.ValidateConfig(json.RawMessage(tc.cfg))
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected an error for %s", tc.cfg)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if tc.check != nil {
				cfg, err := decodeConfig(json.RawMessage(tc.cfg))
				if err != nil {
					t.Fatalf("decodeConfig: %v", err)
				}
				tc.check(t, cfg)
			}
		})
	}
}

// TestActivityScoreUnitsInvariant asserts the integer-units rule: every score is
// a whole number of display points and never exceeds the 100-point maximum
// .
func TestActivityScoreUnitsInvariant(t *testing.T) {
	for budget := MinWrongBudget; budget <= MaxWrongBudget; budget++ {
		for submissions := 1; submissions <= 20; submissions++ {
			units := ActivityScore(submissions, true, budget)
			if units%modeutil.UnitsPerPoint != 0 {
				t.Fatalf("budget %d submissions %d: units %d not a whole point", budget, submissions, units)
			}
			if units > modeutil.Units(100) {
				t.Fatalf("budget %d submissions %d: units %d exceeds the maximum", budget, submissions, units)
			}
			if units < modeutil.Units(extendedFloor) {
				t.Fatalf("budget %d submissions %d: units %d below the hard floor", budget, submissions, units)
			}
		}
	}
}

var _ modes.Mode = (*Mode)(nil)
