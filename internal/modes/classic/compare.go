// Package classic implements G01 经典弗一把, the classic character guessing mode
// .
//
// The nine-field comparison is a direct port of the legacy TypeScript
// implementation in src/lib/game/compare.ts, which stays the behavioural
// baseline: tests/compare.test.ts and tests/game-rules.test.ts must keep
// passing against the old code, and the cases in this package assert that the
// Go engine answers identically.
package classic

import (
	"encoding/json"
	"fmt"
	"sort"

	"github.com/bhcn/umaguessinggame/internal/modes"
	"github.com/bhcn/umaguessinggame/internal/modes/modeutil"
)

// ModeID is the stable identifier used in URLs and round records.
const ModeID = "classic"

// RulesVersion is the rule revision this implementation satisfies (05 header).
const RulesVersion = "2.0.0-draft.1"

// DefaultWrongBudget is the default number of wrong guesses a subject may make
// before losing .
const DefaultWrongBudget = 8

// Wrong budget configuration bounds .
const (
	MinWrongBudget = 4
	MaxWrongBudget = 12
)

// Activity-score formula constants .
const (
	// activityBase is the score for solving on the first submission.
	activityBase = 100
	// activityStep is subtracted per extra submission.
	activityStep = 8
	// activityFloor is the minimum activity score for a solve.
	activityFloor = 44
	// extendedFloor applies from the 9th submission under a non-default budget,
	// where the formula result may fall below 20.
	extendedFloor = 20
	// defaultBudgetLastSolve is the last submission index reachable under the
	// default budget .
	defaultBudgetLastSolve = 8
)

// CellStatus is one field's feedback: correct / near / wrong .
type CellStatus string

const (
	StatusCorrect CellStatus = "correct"
	StatusNear    CellStatus = "near"
	StatusWrong   CellStatus = "wrong"
)

// Field identifiers, in the fixed order the nine-field row is emitted.
const (
	FieldStar      = "star"
	FieldSurface   = "surface"
	FieldDistance  = "distance"
	FieldStyle     = "style"
	FieldSex       = "sex"
	FieldG1        = "g1"
	FieldG23       = "g23"
	FieldGrade     = "grade"
	FieldDormitory = "dormitory"
)

// FieldOrder is the canonical emission order of the nine fields.
var FieldOrder = []string{
	FieldStar, FieldSurface, FieldDistance, FieldStyle, FieldSex,
	FieldG1, FieldG23, FieldGrade, FieldDormitory,
}

// Ordinal ladders. These are the exact legacy orders from compare.ts; the
// "near" rule for distance and running style is a discrete hint rule of this
// project and must not be described as real speed or win probability .
var (
	// distanceOrder is 短 → 英 → 中 → 长.
	distanceOrder = []string{"短", "英", "中", "长"}
	// runningStyleOrder is 逃 → 先 → 差 → 追.
	runningStyleOrder = []string{"逃", "先", "差", "追"}
	// g1Order is 0 / 1-2 / 3-4 / 5+.
	g1Order = []string{"0", "1-2", "3-4", "5+"}
	// g23Order is 0 / 1-3 / 4-6 / 7+.
	g23Order = []string{"0", "1-3", "4-6", "7+"}
)

// Character is one comparable answer vector from the classic pack.
//
// Unknown values are represented by the ok flags being false (or by nil
// slices), never by a zero value: forbids treating an unknown field as
// a green or grey comparison.
type Character struct {
	ID           string
	Name         string
	ImagePath    string
	Star         int
	StarKnown    bool
	Surface      []string
	Distance     []string
	RunningStyle []string
	Sex          string
	G1           string
	G23          string
	G1HasJpn     bool
	G23HasJpn    bool
	Grade        string
	Dormitory    string
	// AmbiguityGroup marks a vector whose nine fields are identical to another
	// character's. Practice rounds may show them; competitive packs must
	// exclude them .
	AmbiguityGroup string
}

// Cell is one rendered field: the value shown and its colour status.
type Cell struct {
	Field  string     `json:"field"`
	Value  string     `json:"value"`
	Status CellStatus `json:"status"`
}

// Cells is the nine-field feedback for one guess.
//
// It is a named object, not an array, because the OpenAPI contract declares
// GuessFeedback.cells as an object keyed by field name (star, surface, ...).
// An array forced every client to re-key the values by scanning for "field",
// and it also made the wire shape differ from the documented schema.
type Cells struct {
	Star      Cell `json:"star"`
	Surface   Cell `json:"surface"`
	Distance  Cell `json:"distance"`
	Style     Cell `json:"style"`
	Sex       Cell `json:"sex"`
	G1        Cell `json:"g1"`
	G23       Cell `json:"g23"`
	Grade     Cell `json:"grade"`
	Dormitory Cell `json:"dormitory"`
}

// GuessRow is one submitted guess with all nine cells.
type GuessRow struct {
	CharacterID string `json:"characterId"`
	DisplayName string `json:"displayName"`
	AvatarPath  string `json:"avatarPath"`
	Cells       Cells  `json:"cells"`
	// Attempt is the 1-based submission index for this subject.
	Attempt int `json:"attempt"`
	// Correct reports whether this guess was the answer.
	Correct bool `json:"correct"`
}

// all returns the cells in canonical field order, for iteration.
func (c Cells) all() []Cell {
	return []Cell{c.Star, c.Surface, c.Distance, c.Style, c.Sex, c.G1, c.G23, c.Grade, c.Dormitory}
}

// cellAt returns the cell for a field name.
func (r GuessRow) cellAt(field string) (Cell, bool) {
	for _, c := range r.Cells.all() {
		if c.Field == field {
			return c, true
		}
	}
	return Cell{}, false
}

// StatusOf returns the status of one field of a row.
func (r GuessRow) StatusOf(field string) (CellStatus, bool) {
	c, ok := r.cellAt(field)
	if !ok {
		return "", false
	}
	return c.Status, true
}

// indexOf returns the position of v in order, or -1.
func indexOf(order []string, v string) int {
	for i, item := range order {
		if item == v {
			return i
		}
	}
	return -1
}

// sameSet reports whether two collections are equal as sets, ignoring order and
// duplicates .
func sameSet(left, right []string) bool {
	l := modeutil.DedupSorted(left)
	r := modeutil.DedupSorted(right)
	if len(l) != len(r) {
		return false
	}
	for i := range l {
		if l[i] != r[i] {
			return false
		}
	}
	return true
}

// hasOverlap reports whether the two collections share at least one member.
func hasOverlap(left, right []string) bool {
	set := make(map[string]struct{}, len(right))
	for _, v := range right {
		if v != "" {
			set[v] = struct{}{}
		}
	}
	for _, v := range left {
		if v == "" {
			continue
		}
		if _, ok := set[v]; ok {
			return true
		}
	}
	return false
}

// hasAdjacent reports whether any member of left sits directly next to a member
// of right on the given ordinal ladder .
func hasAdjacent(left, right []string, order []string) bool {
	for _, item := range left {
		idx := indexOf(order, item)
		if idx < 0 {
			continue
		}
		if idx > 0 && contains(right, order[idx-1]) {
			return true
		}
		if idx+1 < len(order) && contains(right, order[idx+1]) {
			return true
		}
	}
	return false
}

func contains(list []string, v string) bool {
	for _, item := range list {
		if item == v {
			return true
		}
	}
	return false
}

// compareOrdinal grades a bracket comparison: equal is correct, one rung apart
// on the ladder is near, everything else (including values outside the ladder)
// is wrong.
func compareOrdinal(left, right string, order []string) CellStatus {
	if left == "" || right == "" {
		// Unknown never compares green or grey .
		return StatusWrong
	}
	if left == right {
		return StatusCorrect
	}
	li, ri := indexOf(order, left), indexOf(order, right)
	if li < 0 || ri < 0 {
		return StatusWrong
	}
	delta := li - ri
	if delta < 0 {
		delta = -delta
	}
	if delta == 1 {
		return StatusNear
	}
	return StatusWrong
}

// compareStar grades the initial star rating: equal is correct, a difference of
// exactly one is near.
func compareStar(guess, answer Character) CellStatus {
	if !guess.StarKnown || !answer.StarKnown {
		return StatusWrong
	}
	if guess.Star == answer.Star {
		return StatusCorrect
	}
	delta := guess.Star - answer.Star
	if delta < 0 {
		delta = -delta
	}
	if delta == 1 {
		return StatusNear
	}
	return StatusWrong
}

// compareSurface grades the surface set: identical set is correct, any overlap
// is near, disjoint is wrong.
func compareSurface(guess, answer Character) CellStatus {
	if len(guess.Surface) == 0 || len(answer.Surface) == 0 {
		return StatusWrong
	}
	if sameSet(guess.Surface, answer.Surface) {
		return StatusCorrect
	}
	if hasOverlap(guess.Surface, answer.Surface) {
		return StatusNear
	}
	return StatusWrong
}

// compareDistance grades the distance set: identical set is correct; near when
// the sets overlap or hold an adjacent pair on 短→英→中→长.
func compareDistance(guess, answer Character) CellStatus {
	if len(guess.Distance) == 0 || len(answer.Distance) == 0 {
		return StatusWrong
	}
	if sameSet(guess.Distance, answer.Distance) {
		return StatusCorrect
	}
	if hasOverlap(guess.Distance, answer.Distance) {
		return StatusNear
	}
	if hasAdjacent(guess.Distance, answer.Distance, distanceOrder) {
		return StatusNear
	}
	return StatusWrong
}

// compareStyle grades the running-style set: identical set is correct; near when
// the sets overlap or hold an adjacent pair on 逃→先→差→追.
func compareStyle(guess, answer Character) CellStatus {
	if len(guess.RunningStyle) == 0 || len(answer.RunningStyle) == 0 {
		return StatusWrong
	}
	if sameSet(guess.RunningStyle, answer.RunningStyle) {
		return StatusCorrect
	}
	if hasOverlap(guess.RunningStyle, answer.RunningStyle) {
		return StatusNear
	}
	if hasAdjacent(guess.RunningStyle, answer.RunningStyle, runningStyleOrder) {
		return StatusNear
	}
	return StatusWrong
}

// compareEquality grades a field that only distinguishes equal from unequal.
func compareEquality(guess, answer string) CellStatus {
	if guess == "" || answer == "" {
		return StatusWrong
	}
	if guess == answer {
		return StatusCorrect
	}
	return StatusWrong
}

// bracketLabel renders a bracket value with the legacy J suffix. The J marker is
// shown only when that bracket matched and the answer carries a Jpn grade
// .
func bracketLabel(bracket string, status CellStatus, answerHasJpn bool) string {
	if status != StatusCorrect || !answerHasJpn || bracket == "" {
		return bracket
	}
	return bracket + "J"
}

// formatGroup renders a set the way the legacy UI did: joined with "/".
func formatGroup(values []string) string {
	if len(values) == 0 {
		return ""
	}
	clean := modeutil.DedupSorted(values)
	if len(clean) == 0 {
		return ""
	}
	// Preserve the pack's declared order for display; only equality uses the
	// sorted form. The legacy formatter joined in declaration order.
	out := ""
	for _, v := range values {
		if v == "" {
			continue
		}
		if out != "" {
			out += "/"
		}
		out += v
	}
	return out
}

// formatStar renders the star cell value ("3星").
func formatStar(c Character) string {
	if !c.StarKnown {
		return "未知"
	}
	return fmt.Sprintf("%d星", c.Star)
}

// BuildGuessRow produces the nine-field feedback row for one guess against the
// answer. It is the Go equivalent of buildGuessRow in compare.ts.
func BuildGuessRow(guess, answer Character) GuessRow {
	g1Status := compareOrdinal(guess.G1, answer.G1, g1Order)
	g23Status := compareOrdinal(guess.G23, answer.G23, g23Order)

	cells := Cells{
		Star:      Cell{Field: FieldStar, Value: formatStar(guess), Status: compareStar(guess, answer)},
		Surface:   Cell{Field: FieldSurface, Value: formatGroup(guess.Surface), Status: compareSurface(guess, answer)},
		Distance:  Cell{Field: FieldDistance, Value: formatGroup(guess.Distance), Status: compareDistance(guess, answer)},
		Style:     Cell{Field: FieldStyle, Value: formatGroup(guess.RunningStyle), Status: compareStyle(guess, answer)},
		Sex:       Cell{Field: FieldSex, Value: guess.Sex, Status: compareEquality(guess.Sex, answer.Sex)},
		G1:        Cell{Field: FieldG1, Value: bracketLabel(guess.G1, g1Status, answer.G1HasJpn), Status: g1Status},
		G23:       Cell{Field: FieldG23, Value: bracketLabel(guess.G23, g23Status, answer.G23HasJpn), Status: g23Status},
		Grade:     Cell{Field: FieldGrade, Value: guess.Grade, Status: compareEquality(guess.Grade, answer.Grade)},
		Dormitory: Cell{Field: FieldDormitory, Value: guess.Dormitory, Status: compareEquality(guess.Dormitory, answer.Dormitory)},
	}
	return GuessRow{
		CharacterID: guess.ID,
		DisplayName: guess.Name,
		AvatarPath:  guess.ImagePath,
		Cells:       cells,
	}
}

// ActivityScore computes the activity points for a solved round .
//
//	solved: 100 - 8×(submissions-1), floor 44
//	under a non-default budget, a solve on the 9th submission or later takes
//	max(20, the formula above)
//	unsolved: 0
//
// It returns integer scoreUnits (100 units == 1 display point).
func ActivityScore(submissions int, solved bool, wrongBudget int) int {
	if !solved || submissions <= 0 {
		return 0
	}
	display := activityBase - activityStep*(submissions-1)
	if wrongBudget != DefaultWrongBudget && submissions > defaultBudgetLastSolve {
		// The 44 floor is calibrated for the default budget, where the last
		// possible solve is the 8th and therefore scores exactly 44. Under a
		// larger budget a solve can happen on the 9th submission or later; the
		// documented rule is max(20, formula), which deliberately bypasses the
		// 44 floor so an extra guess is not free . Applying 44 first
		// would make max(20,44) a permanent no-op and break the rule.
		if display < extendedFloor {
			display = extendedFloor
		}
	} else if display < activityFloor {
		display = activityFloor
	}
	return modeutil.Units(display)
}

// parseCharacter converts one decoded record into a Character.
func parseCharacter(fm modeutil.FieldMap) (Character, bool) {
	id := fm.Str("id", "characterId", "character_id")
	if id == "" {
		return Character{}, false
	}
	c := Character{
		ID:             id,
		Name:           fm.Str("nameZh", "name_zh", "name", "displayName"),
		ImagePath:      fm.Str("imagePath", "image_local_path", "imageLocalPath", "avatarPath"),
		Surface:        fm.StrList("surfaceGroup", "surface_group"),
		Distance:       fm.StrList("distanceGroup", "distance_group"),
		RunningStyle:   fm.StrList("runningStyleGroup", "running_style_group"),
		Sex:            fm.Str("sexType", "sex_type"),
		G1:             fm.Str("g1Bracket", "g1_bracket"),
		G23:            fm.Str("g23Bracket", "g23_bracket"),
		G1HasJpn:       fm.Bool("g1HasJpn", "g1_has_jpn"),
		G23HasJpn:      fm.Bool("g23HasJpn", "g23_has_jpn"),
		Grade:          fm.Str("schoolGrade", "school_grade"),
		Dormitory:      fm.Str("dormitory"),
		AmbiguityGroup: fm.Str("ambiguityGroup", "ambiguity_group"),
	}
	if star, ok := fm.Int("star"); ok {
		c.Star, c.StarKnown = star, true
	}
	return c, true
}

// loadCharacters reads the classic section and returns the pool a round may draw
// from, sorted by ID (the deterministic tie-break order used downstream).
//
// The pack publishes two pools: "cards" is the competitive pool with ambiguous
// nine-field vectors already removed, and "practiceCards" is the wider pool that
// still contains them, each tagged with ambiguityGroup. A practice round draws
// from the wider pool; a competitive round must not, because two characters with
// identical feedback cannot be told apart .
func loadCharacters(src modes.ContentSource, practice bool) ([]Character, error) {
	raw, err := modeutil.Section(src, "classic.characters", "classic", "characters")
	if err != nil {
		return nil, err
	}
	fm, err := modeutil.DecodeFieldMap(raw)
	if err != nil {
		// A bare array section is also accepted.
		return loadCharactersFromArray(raw)
	}

	var records []modeutil.FieldMap
	if practice {
		// Prefer the explicit practice pool; fall back to the competitive pool
		// when a pack publishes only one list.
		if list, ok := fm.List("practiceCards", "practice_cards"); ok {
			records = list
		}
	}
	if records == nil {
		if list, ok := fm.List("cards", "characters", "items"); ok {
			records = list
		} else {
			// A bare array section is also accepted.
			return loadCharactersFromArray(raw)
		}
	}

	out := make([]Character, 0, len(records))
	for _, rec := range records {
		c, ok := parseCharacter(rec)
		if !ok {
			continue
		}
		out = append(out, c)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

// PracticeCharacters exposes only the explicit, public classic character section.
// It must never be replaced by a dump of arbitrary pack sections or Secrets.
func PracticeCharacters(src modes.ContentSource, widerPool bool) ([]Character, error) {
	return loadCharacters(src, widerPool)
}

// loadCharactersFromArray reads a section shaped as a bare JSON array.
func loadCharactersFromArray(raw json.RawMessage) ([]Character, error) {
	records, err := modeutil.Records(raw, "characters", "items", "cards")
	if err != nil {
		return nil, err
	}
	out := make([]Character, 0, len(records))
	for _, rec := range records {
		c, ok := parseCharacter(rec)
		if !ok {
			continue
		}
		out = append(out, c)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

// decodeState decodes a participant state blob, tolerating nil.
func decodeState(raw json.RawMessage) (state, error) {
	var s state
	if len(raw) == 0 {
		return s, nil
	}
	if err := json.Unmarshal(raw, &s); err != nil {
		return state{}, err
	}
	return s, nil
}
