package draft

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"testing"

	"github.com/bhcn/umaguessinggame/internal/modes"
	"github.com/bhcn/umaguessinggame/internal/modes/modeutil"
)

// fakeContent is a synthetic compiled pack section store.
type fakeContent map[string]json.RawMessage

func (f fakeContent) Section(name string) (json.RawMessage, bool) {
	raw, ok := f[name]
	return raw, ok
}

func (f fakeContent) Version() string { return "test-pack-1" }

// cardSpec is the on-the-wire spelling of a market card.
type cardSpec struct {
	CharacterID string                       `json:"characterId"`
	Name        string                       `json:"name"`
	Price       int                          `json:"price"`
	Aptitudes   map[string]map[string]string `json:"aptitudes"`
}

// taskSpec is the on-the-wire spelling of a task.
type taskSpec struct {
	TaskID   string `json:"taskId"`
	Surface  string `json:"surface"`
	Distance string `json:"distance"`
	Style    string `json:"style"`
}

// marketSection is the on-the-wire draft section.
type marketSection struct {
	Cards []cardSpec `json:"cards"`
	Tasks []taskSpec `json:"tasks"`
}

// packOf builds a draft pack.
func packOf(cards []cardSpec, tasks []taskSpec) fakeContent {
	raw, err := json.Marshal(marketSection{Cards: cards, Tasks: tasks})
	if err != nil {
		panic(err)
	}
	return fakeContent{"draft.market": raw}
}

// defaultTasks is the four-task set every test market uses.
func defaultTasks() []taskSpec {
	return []taskSpec{
		{TaskID: "t1", Surface: "草地", Distance: "短", Style: "逃"},
		{TaskID: "t2", Surface: "草地", Distance: "英", Style: "先"},
		{TaskID: "t3", Surface: "草地", Distance: "中", Style: "差"},
		{TaskID: "t4", Surface: "泥地", Distance: "长", Style: "追"},
	}
}

// graded builds a card graded A on exactly one distance and one style, B on
// surface, with a price derived from the documented formula.
func graded(id, name, distance, style string, price int) cardSpec {
	return cardSpec{
		CharacterID: id,
		Name:        name,
		Price:       price,
		Aptitudes: map[string]map[string]string{
			"surface":  {"草地": "A", "泥地": "A"},
			"distance": {distance: "A"},
			"style":    {style: "A"},
		},
	}
}

// defaultMarket is a twelve-card market whose prices all fit several lineups of
// four within the budget of 24.
func defaultMarket() fakeContent {
	cards := make([]cardSpec, 0, MarketSize)
	// Six "specialist" cards priced 6 each: any four of them cost exactly 24.
	specials := []struct{ d, s string }{
		{"短", "逃"}, {"英", "先"}, {"中", "差"}, {"长", "追"},
		{"短", "先"}, {"英", "差"},
	}
	for i, sp := range specials {
		cards = append(cards, graded(fmt.Sprintf("spec_%d", i), fmt.Sprintf("专%d", i), sp.d, sp.s, 6))
	}
	// Six cheap "generalist" cards priced 2 each: four of them cost 8.
	for i := 0; i < 6; i++ {
		cards = append(cards, graded(fmt.Sprintf("gen_%d", i), fmt.Sprintf("通%d", i), "短", "逃", 2))
	}
	return packOf(cards, defaultTasks())
}

// scope builds a deterministic round scope.
func scope(seed string) modes.Scope {
	return modes.Scope{
		ModeID:             ModeID,
		RulesVersion:       RulesVersion,
		DatasetVersion:     "ds-1",
		ContentPackVersion: "test-pack-1",
		Seed:               seed,
	}
}

// prepare runs Prepare and fails on error.
func prepare(t *testing.T, pack fakeContent, seed, cfg string, subjects ...modes.Subject) (modes.Prepared, publicPrepared) {
	t.Helper()
	prep, err := New().Prepare(context.Background(), modes.PrepareInput{
		Scope:   scope(seed),
		Subject: subjects,
		Config:  json.RawMessage(cfg),
		Content: pack,
	})
	if err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	var pub publicPrepared
	if err := json.Unmarshal(prep.Public, &pub); err != nil {
		t.Fatalf("decode public: %v", err)
	}
	return prep, pub
}

// draftAction builds a contract-shaped DraftLineup payload from a task → character
// map. Assignments are emitted in sorted slot order so the payload is stable.
func draftAction(assignments map[string]string) modes.ModeAction {
	slots := make([]string, 0, len(assignments))
	for slot := range assignments {
		slots = append(slots, slot)
	}
	sort.Strings(slots)
	list := make([]draftAssignment, 0, len(slots))
	for _, slot := range slots {
		list = append(list, draftAssignment{SlotID: slot, CharacterID: assignments[slot]})
	}
	raw, _ := json.Marshal(draftPayload{Kind: ActionDraft, Assignments: list})
	return modes.ModeAction{Kind: ActionDraft, Payload: raw}
}

// applyOne runs one Apply call.
func applyOne(t *testing.T, prep modes.Prepared, subj modes.Subject, prev json.RawMessage, action modes.ModeAction) (modes.Decision, state) {
	t.Helper()
	dec, err := New().Apply(modes.ApplyInput{
		Scope:   scope("apply"),
		Subject: subj,
		Action:  action,
		Public:  prep.Public,
		Private: prep.Private,
		State:   prev,
	})
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	var st state
	if len(dec.State) > 0 {
		if err := json.Unmarshal(dec.State, &st); err != nil {
			t.Fatalf("decode state: %v", err)
		}
	}
	return dec, st
}

// TestPublishedPrice pins the documented trial price formula :
// 3 + ceil(distanceA/2) + ceil(styleA/2) + (dirtA ? 1 : 0).
func TestPublishedPrice(t *testing.T) {
	all := func(axis string, values ...string) map[string]string {
		m := map[string]string{}
		for _, v := range values {
			m[v] = "A"
		}
		return m
	}
	cases := []struct {
		name string
		apt  Aptitudes
		want int
	}{
		{
			name: "one distance A and one style A with no dirt",
			apt: Aptitudes{
				Distance: all("distance", "短"),
				Style:    all("style", "逃"),
				Surface:  map[string]string{"草地": "A"},
			},
			want: 3 + 1 + 1,
		},
		{
			name: "two distance A and two style A",
			apt: Aptitudes{
				Distance: all("distance", "短", "英"),
				Style:    all("style", "逃", "先"),
				Surface:  map[string]string{"草地": "A"},
			},
			want: 3 + 1 + 1,
		},
		{
			name: "four distance A and four style A",
			apt: Aptitudes{
				Distance: all("distance", "短", "英", "中", "长"),
				Style:    all("style", "逃", "先", "差", "追"),
				Surface:  map[string]string{"草地": "A"},
			},
			want: 3 + 2 + 2,
		},
		{
			name: "three distance A rounds up",
			apt: Aptitudes{
				Distance: all("distance", "短", "英", "中"),
				Style:    all("style", "逃"),
				Surface:  map[string]string{"草地": "A"},
			},
			want: 3 + 2 + 1,
		},
		{
			name: "dirt A adds one",
			apt: Aptitudes{
				Distance: all("distance", "短"),
				Style:    all("style", "逃"),
				Surface:  map[string]string{"泥地": "A"},
			},
			want: 3 + 1 + 1 + 1,
		},
		{
			name: "dirt B does not add",
			apt: Aptitudes{
				Distance: all("distance", "短"),
				Style:    all("style", "逃"),
				Surface:  map[string]string{"泥地": "B"},
			},
			want: 3 + 1 + 1,
		},
		{
			name: "unknown aptitudes price at the base",
			apt:  Aptitudes{},
			want: 3,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := PublishedPrice(tc.apt); got != tc.want {
				t.Fatalf("PublishedPrice = %d, want %d", got, tc.want)
			}
		})
	}
}

// TestScoreTaskGrades pins A=3, B=2, C=1, anything else 0 .
func TestScoreTaskGrades(t *testing.T) {
	task := Task{TaskID: "t1", Surface: "草地", Distance: "短", Style: "逃"}
	cases := []struct {
		name string
		apt  Aptitudes
		want int
	}{
		{
			name: "all A is 9",
			apt: Aptitudes{
				Surface:  map[string]string{"草地": "A"},
				Distance: map[string]string{"短": "A"},
				Style:    map[string]string{"逃": "A"},
			},
			want: 9,
		},
		{
			name: "all B is 6",
			apt: Aptitudes{
				Surface:  map[string]string{"草地": "B"},
				Distance: map[string]string{"短": "B"},
				Style:    map[string]string{"逃": "B"},
			},
			want: 6,
		},
		{
			name: "all C is 3",
			apt: Aptitudes{
				Surface:  map[string]string{"草地": "C"},
				Distance: map[string]string{"短": "C"},
				Style:    map[string]string{"逃": "C"},
			},
			want: 3,
		},
		{
			name: "mixed A B C is 6",
			apt: Aptitudes{
				Surface:  map[string]string{"草地": "A"},
				Distance: map[string]string{"短": "B"},
				Style:    map[string]string{"逃": "C"},
			},
			want: 6,
		},
		{
			name: "grade D scores zero",
			apt: Aptitudes{
				Surface:  map[string]string{"草地": "D"},
				Distance: map[string]string{"短": "A"},
				Style:    map[string]string{"逃": "A"},
			},
			want: 6,
		},
		{
			name: "unknown grade scores zero",
			apt: Aptitudes{
				Surface:  map[string]string{"草地": "A"},
				Distance: map[string]string{"短": "A"},
			},
			want: 6,
		},
		{
			name: "wrong axis value scores zero",
			apt: Aptitudes{
				Surface:  map[string]string{"泥地": "A"},
				Distance: map[string]string{"长": "A"},
				Style:    map[string]string{"追": "A"},
			},
			want: 0,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ts := scoreTask(task, Card{ID: "c", Aptitudes: tc.apt})
			if ts.Total != tc.want {
				t.Fatalf("task total = %d, want %d", ts.Total, tc.want)
			}
			if ts.Total > 9 {
				t.Fatalf("task total %d exceeds the 9-point maximum", ts.Total)
			}
		})
	}
}

// TestMaxTotalPoints asserts the documented 36-point maximum .
func TestMaxTotalPoints(t *testing.T) {
	if MaxTotalPoints != 36 {
		t.Fatalf("MaxTotalPoints = %d, want 36", MaxTotalPoints)
	}
	// A perfect lineup must score exactly 100.
	tasks := defaultTasks()
	apt := Aptitudes{
		Surface:  map[string]string{"草地": "A", "泥地": "A"},
		Distance: map[string]string{"短": "A", "英": "A", "中": "A", "长": "A"},
		Style:    map[string]string{"逃": "A", "先": "A", "差": "A", "追": "A"},
	}
	total := 0
	for _, task := range tasks {
		total += scoreTask(Task{
			TaskID: task.TaskID, Surface: task.Surface,
			Distance: task.Distance, Style: task.Style,
		}, Card{ID: "perfect", Aptitudes: apt}).Total
	}
	if total != MaxTotalPoints {
		t.Fatalf("perfect lineup total = %d, want %d", total, MaxTotalPoints)
	}
	if units := modeutil.Units(total * 100 / MaxTotalPoints); units != 10000 {
		t.Fatalf("perfect score = %d units, want 10000", units)
	}
}

// TestBudgetBoundary covers the documented "预算边界" case: a lineup costing
// exactly the budget is accepted, one costing more is refused .
func TestBudgetBoundary(t *testing.T) {
	subjects := []modes.Subject{{ID: "team1", Kind: modes.SubjectTeam}}
	prep, pub := prepare(t, defaultMarket(), "budget-seed", `{}`, subjects...)

	// Four specialist cards at 6 each cost exactly 24.
	atBudget := map[string]string{
		"t1": "spec_0", "t2": "spec_1", "t3": "spec_2", "t4": "spec_3",
	}
	dec, st := applyOne(t, prep, subjects[0], nil, draftAction(atBudget))
	if !dec.Accepted {
		t.Fatalf("lineup at the budget was rejected: %s %s", dec.Code, dec.Detail)
	}
	if st.Spent != pub.Config.Budget {
		t.Fatalf("spent = %d, want %d", st.Spent, pub.Config.Budget)
	}

	// Over budget: replace one 6-priced card with a 7-priced card. Build a
	// market that has such a card.
	pricey := defaultMarket()
	var sec marketSection
	if err := json.Unmarshal(pricey["draft.market"], &sec); err != nil {
		t.Fatalf("decode market: %v", err)
	}
	sec.Cards = append(sec.Cards, graded("expensive", "贵", "短", "逃", 7))
	raw, _ := json.Marshal(sec)
	pricey["draft.market"] = raw
	prepP, _ := prepare(t, pricey, "budget-seed", `{}`, subjects...)

	overBudget := map[string]string{
		"t1": "expensive", "t2": "spec_1", "t3": "spec_2", "t4": "spec_3",
	}
	dec2, st2 := applyOne(t, prepP, subjects[0], nil, draftAction(overBudget))
	if dec2.Accepted {
		t.Fatal("over-budget lineup was accepted")
	}
	if dec2.Code != CodeOverBudget {
		t.Fatalf("code = %q, want %q", dec2.Code, CodeOverBudget)
	}
	// A refused draft must not lock anything .
	if st2.Locked || st2.Revisions != 0 {
		t.Fatalf("over-budget draft mutated state: %+v", st2)
	}
}

// TestDuplicateCharacterRejected covers the documented "同一角色不重复" rule:
// one character may appear at most once per segment .
func TestDuplicateCharacterRejected(t *testing.T) {
	subjects := []modes.Subject{{ID: "team1", Kind: modes.SubjectTeam}}
	prep, _ := prepare(t, defaultMarket(), "dup-seed", `{}`, subjects...)

	dup := map[string]string{
		"t1": "spec_0", "t2": "spec_0", "t3": "spec_2", "t4": "spec_3",
	}
	dec, _ := applyOne(t, prep, subjects[0], nil, draftAction(dup))
	if dec.Accepted {
		t.Fatal("lineup reusing a character was accepted")
	}
	if dec.Code != CodeDuplicateCharacter {
		t.Fatalf("code = %q, want %q", dec.Code, CodeDuplicateCharacter)
	}
}

// TestUnknownAptitudeNotTreatedAsMatch covers the documented "未知适性" case: a
// card with no grade for a task axis scores zero for that axis rather than
// silently matching .
func TestUnknownAptitudeNotTreatedAsMatch(t *testing.T) {
	subjects := []modes.Subject{{ID: "team1", Kind: modes.SubjectTeam}}
	cards := []cardSpec{
		// No aptitude data at all.
		{CharacterID: "blank", Name: "空", Price: 2},
		graded("a1", "甲", "短", "逃", 2),
		graded("a2", "乙", "英", "先", 2),
		graded("a3", "丙", "中", "差", 2),
		graded("a4", "丁", "长", "追", 2),
		// Fillers so the market publishes the required twelve cards.
		graded("f1", "戊", "短", "逃", 2),
		graded("f2", "己", "英", "先", 2),
		graded("f3", "庚", "中", "差", 2),
		graded("f4", "辛", "长", "追", 2),
		graded("f5", "壬", "短", "逃", 2),
		graded("f6", "癸", "英", "先", 2),
		graded("f7", "子", "中", "差", 2),
	}
	prep, _ := prepare(t, packOf(cards, defaultTasks()), "unknown-seed", `{}`, subjects...)

	lineup := map[string]string{
		"t1": "blank", "t2": "a2", "t3": "a3", "t4": "a4",
	}
	dec, st := applyOne(t, prep, subjects[0], nil, draftAction(lineup))
	if !dec.Accepted {
		t.Fatalf("lineup with an ungraded card rejected: %s %s", dec.Code, dec.Detail)
	}
	// The blank card contributes 0 for t1, so the total is the other three
	// tasks' scores only.
	for _, ts := range st.TaskScores {
		if ts.TaskID == "t1" && ts.Total != 0 {
			t.Fatalf("ungraded card scored %d on t1, want 0", ts.Total)
		}
	}
	wantTotal := 0
	for _, ts := range st.TaskScores {
		if ts.TaskID != "t1" {
			wantTotal += ts.Total
		}
	}
	if st.TotalPoints != wantTotal {
		t.Fatalf("totalPoints = %d, want %d", st.TotalPoints, wantTotal)
	}
}

// Drafts may change before final confirmation; a new request after the final
// lineup is locked must not overwrite the accepted assignment or its score.
func TestFinalLineupCannotBeReplaced(t *testing.T) {
	subjects := []modes.Subject{{ID: "team1", Kind: modes.SubjectTeam}}
	prep, _ := prepare(t, defaultMarket(), "rev-seed", `{}`, subjects...)

	first := map[string]string{"t1": "spec_0", "t2": "spec_1", "t3": "spec_2", "t4": "spec_3"}
	dec1, st1 := applyOne(t, prep, subjects[0], nil, draftAction(first))
	if !dec1.Accepted {
		t.Fatalf("first draft rejected: %s", dec1.Code)
	}
	if st1.Revisions != 1 {
		t.Fatalf("revisions = %d, want 1", st1.Revisions)
	}

	// A second, different lineup cannot replace the first.
	second := map[string]string{"t1": "gen_0", "t2": "gen_1", "t3": "gen_2", "t4": "gen_3"}
	dec2, _ := applyOne(t, prep, subjects[0], modeutil.Marshal(st1), draftAction(second))
	if dec2.Accepted || dec2.Code != CodeLineupLocked || len(dec2.State) != 0 {
		t.Fatalf("locked lineup replaced: %+v", dec2)
	}
}

// TestSameScoreSharesRank covers the documented "同分" case: equal totals share a
// rank and skip the following ordinal .
func TestSameScoreSharesRank(t *testing.T) {
	subjects := []modes.Subject{
		{ID: "team1", Kind: modes.SubjectTeam},
		{ID: "team2", Kind: modes.SubjectTeam},
		{ID: "team3", Kind: modes.SubjectTeam},
	}
	prep, _ := prepare(t, defaultMarket(), "tie-seed", `{}`, subjects...)

	// team1 and team2 lock identical lineups, so they tie; team3 submits nothing.
	lineup := map[string]string{"t1": "spec_0", "t2": "spec_1", "t3": "spec_2", "t4": "spec_3"}
	_, st1 := applyOne(t, prep, subjects[0], nil, draftAction(lineup))
	_, st2 := applyOne(t, prep, subjects[1], nil, draftAction(lineup))

	scores, err := New().Settle(modes.SettleInput{
		Scope:   scope("tie-seed"),
		Phase:   modes.PhaseSettled,
		Public:  prep.Public,
		Private: prep.Private,
		States: map[string]json.RawMessage{
			"team1": modeutil.Marshal(st1),
			"team2": modeutil.Marshal(st2),
		},
	})
	if err != nil {
		t.Fatalf("Settle: %v", err)
	}
	if len(scores) != 3 {
		t.Fatalf("scores = %d, want 3", len(scores))
	}
	if scores[0].ScoreUnits != scores[1].ScoreUnits {
		t.Fatalf("tied totals differ: %d vs %d", scores[0].ScoreUnits, scores[1].ScoreUnits)
	}
	if scores[0].Rank != 1 || scores[1].Rank != 1 {
		t.Fatalf("tied ranks = %d,%d, want 1,1", scores[0].Rank, scores[1].Rank)
	}
	if scores[2].Rank != 3 {
		t.Fatalf("third rank = %d, want 3", scores[2].Rank)
	}
	// The team with no submission scores 0 .
	if scores[2].ScoreUnits != 0 {
		t.Fatalf("no-submission score = %d, want 0", scores[2].ScoreUnits)
	}
}

// TestEveryTaskHasMultipleSolutions covers the documented "所有任务有多解" case:
// a market must admit at least two distinct complete feasible lineups, otherwise
// Prepare refuses it .
func TestEveryTaskHasMultipleSolutions(t *testing.T) {
	subjects := []modes.Subject{{ID: "team1", Kind: modes.SubjectTeam}}

	// Only one feasible four-card lineup: four cards at 6 each and everything
	// else overpriced.
	onlyOne := []cardSpec{
		graded("a", "甲", "短", "逃", 6),
		graded("b", "乙", "英", "先", 6),
		graded("c", "丙", "中", "差", 6),
		graded("d", "丁", "长", "追", 6),
		graded("e", "戊", "短", "逃", 10),
		graded("f", "己", "短", "逃", 10),
		graded("g", "庚", "短", "逃", 10),
		graded("h", "辛", "短", "逃", 10),
		graded("i", "壬", "短", "逃", 10),
		graded("j", "癸", "短", "逃", 10),
		graded("k", "子", "短", "逃", 10),
		graded("l", "丑", "短", "逃", 10),
	}
	_, err := New().Prepare(context.Background(), modes.PrepareInput{
		Scope:   scope("one-solution"),
		Subject: subjects,
		Config:  json.RawMessage(`{}`),
		Content: packOf(onlyOne, defaultTasks()),
	})
	if err == nil {
		t.Fatal("a market with a single feasible lineup was accepted")
	}

	// The default market has many lineups.
	_, pub := prepare(t, defaultMarket(), "many-solutions", `{}`, subjects...)
	if pub.FeasibleLineups < 2 {
		t.Fatalf("feasible lineups = %d, want at least 2", pub.FeasibleLineups)
	}
}

// TestMultipurposeCardDoesNotDominate covers the documented "多面手是否垄断" case:
// a card graded A on several axes can fill more than one task, but the
// no-duplicate rule means it still only occupies one slot, so it cannot carry a
// whole lineup .
func TestMultipurposeCardDoesNotDominate(t *testing.T) {
	subjects := []modes.Subject{{ID: "team1", Kind: modes.SubjectTeam}}
	cards := []cardSpec{
		// One card graded A on every axis, priced to eat most of the budget.
		{
			CharacterID: "omni", Name: "全能", Price: 18,
			Aptitudes: map[string]map[string]string{
				"surface":  {"草地": "A", "泥地": "A"},
				"distance": {"短": "A", "英": "A", "中": "A", "长": "A"},
				"style":    {"逃": "A", "先": "A", "差": "A", "追": "A"},
			},
		},
		graded("a", "甲", "短", "逃", 2),
		graded("b", "乙", "英", "先", 2),
		graded("c", "丙", "中", "差", 2),
		graded("d", "丁", "长", "追", 2),
		graded("e", "戊", "短", "逃", 2),
		graded("f", "己", "英", "先", 2),
		graded("g", "庚", "中", "差", 2),
		graded("h", "辛", "长", "追", 2),
		graded("i", "壬", "短", "逃", 2),
		graded("j", "癸", "英", "先", 2),
		graded("k", "子", "中", "差", 2),
	}
	prep, _ := prepare(t, packOf(cards, defaultTasks()), "omni-seed", `{}`, subjects...)

	// The omni card can only fill one task, so a lineup using it must still fill
	// the other three from the cheap cards.
	withOmni := map[string]string{"t1": "omni", "t2": "b", "t3": "c", "t4": "d"}
	dec, st := applyOne(t, prep, subjects[0], nil, draftAction(withOmni))
	if !dec.Accepted {
		t.Fatalf("lineup with the omni card rejected: %s %s", dec.Code, dec.Detail)
	}
	// Its cost is 18, leaving 6 for three cards at 2 each: exactly the budget.
	if st.Spent != Budget {
		t.Fatalf("spent = %d, want %d", st.Spent, Budget)
	}
	// Using it twice is refused, which is what stops it dominating.
	dupOmni := map[string]string{"t1": "omni", "t2": "omni", "t3": "c", "t4": "d"}
	dec2, _ := applyOne(t, prep, subjects[0], nil, draftAction(dupOmni))
	if dec2.Accepted {
		t.Fatal("the omni card was allowed to fill two tasks")
	}
	if dec2.Code != CodeDuplicateCharacter {
		t.Fatalf("code = %q, want %q", dec2.Code, CodeDuplicateCharacter)
	}
}

// TestPrepareRejectsWrongMarketShape asserts a market of the wrong size is not
// admitted .
func TestPrepareRejectsWrongMarketShape(t *testing.T) {
	subjects := []modes.Subject{{ID: "team1", Kind: modes.SubjectTeam}}
	cases := []struct {
		name string
		pack fakeContent
	}{
		{name: "no section", pack: fakeContent{}},
		{name: "too few cards", pack: packOf([]cardSpec{
			graded("a", "甲", "短", "逃", 2),
			graded("b", "乙", "英", "先", 2),
		}, defaultTasks())},
		{name: "too few tasks", pack: packOf([]cardSpec{
			graded("a", "甲", "短", "逃", 2),
			graded("b", "乙", "英", "先", 2),
			graded("c", "丙", "中", "差", 2),
			graded("d", "丁", "长", "追", 2),
			graded("e", "戊", "短", "逃", 2),
			graded("f", "己", "英", "先", 2),
			graded("g", "庚", "中", "差", 2),
			graded("h", "辛", "长", "追", 2),
			graded("i", "壬", "短", "逃", 2),
			graded("j", "癸", "英", "先", 2),
			graded("k", "子", "中", "差", 2),
			graded("l", "丑", "长", "追", 2),
		}, []taskSpec{{TaskID: "t1", Surface: "草地", Distance: "短", Style: "逃"}})},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := New().Prepare(context.Background(), modes.PrepareInput{
				Scope:   scope("shape"),
				Subject: subjects,
				Config:  json.RawMessage(`{}`),
				Content: tc.pack,
			})
			if err == nil {
				t.Fatal("Prepare accepted a malformed market")
			}
		})
	}
}

// TestIncompleteLineupRejected asserts a partial draft is not lockable .
func TestIncompleteLineupRejected(t *testing.T) {
	subjects := []modes.Subject{{ID: "team1", Kind: modes.SubjectTeam}}
	prep, _ := prepare(t, defaultMarket(), "incomplete", `{}`, subjects...)
	cases := []struct {
		name string
		body map[string]string
		want string
	}{
		{name: "missing a task", body: map[string]string{"t1": "spec_0", "t2": "spec_1", "t3": "spec_2"}, want: CodeIncomplete},
		{name: "unknown task", body: map[string]string{
			"t1": "spec_0", "t2": "spec_1", "t3": "spec_2", "t4": "spec_3", "t5": "spec_4",
		}, want: CodeIncomplete},
		{name: "unknown character", body: map[string]string{
			"t1": "nope", "t2": "spec_1", "t3": "spec_2", "t4": "spec_3",
		}, want: CodeUnknownCharacter},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dec, _ := applyOne(t, prep, subjects[0], nil, draftAction(tc.body))
			if dec.Accepted {
				t.Fatalf("accepted an invalid draft: %+v", tc.body)
			}
			if dec.Code != tc.want {
				t.Fatalf("code = %q, want %q", dec.Code, tc.want)
			}
		})
	}
}

// TestPrepareDeterministic asserts the determinism gate for this mode .
func TestPrepareDeterministic(t *testing.T) {
	subjects := []modes.Subject{{ID: "team1", Kind: modes.SubjectTeam}}
	first, _ := prepare(t, defaultMarket(), "det-seed", `{}`, subjects...)
	second, _ := prepare(t, defaultMarket(), "det-seed", `{}`, subjects...)
	if string(first.Public) != string(second.Public) {
		t.Fatal("prepared public state differs between identical prepares")
	}
	if string(first.Private) != string(second.Private) {
		t.Fatal("prepared private state differs between identical prepares")
	}
}

// TestSettleDeterministic asserts settlement does not depend on map order
// .
func TestSettleDeterministic(t *testing.T) {
	subjects := []modes.Subject{
		{ID: "team1", Kind: modes.SubjectTeam},
		{ID: "team2", Kind: modes.SubjectTeam},
	}
	prep, _ := prepare(t, defaultMarket(), "settle-det", `{}`, subjects...)
	lineupA := map[string]string{"t1": "spec_0", "t2": "spec_1", "t3": "spec_2", "t4": "spec_3"}
	lineupB := map[string]string{"t1": "gen_0", "t2": "gen_1", "t3": "gen_2", "t4": "gen_3"}
	_, st1 := applyOne(t, prep, subjects[0], nil, draftAction(lineupA))
	_, st2 := applyOne(t, prep, subjects[1], nil, draftAction(lineupB))

	run := func() string {
		scores, err := New().Settle(modes.SettleInput{
			Scope:   scope("settle-det"),
			Phase:   modes.PhaseSettled,
			Public:  prep.Public,
			Private: prep.Private,
			States: map[string]json.RawMessage{
				"team2": modeutil.Marshal(st2),
				"team1": modeutil.Marshal(st1),
			},
		})
		if err != nil {
			t.Fatalf("Settle: %v", err)
		}
		return string(modeutil.Marshal(scores))
	}
	if first, second := run(), run(); first != second {
		t.Fatal("settle output depends on iteration order")
	}
}

// TestProjectHidesOpponentLineupUntilReveal asserts another team's lineup is not
// exposed during play .
func TestProjectHidesOpponentLineupUntilReveal(t *testing.T) {
	subjects := []modes.Subject{
		{ID: "team1", Kind: modes.SubjectTeam},
		{ID: "team2", Kind: modes.SubjectTeam},
	}
	prep, _ := prepare(t, defaultMarket(), "proj-seed", `{}`, subjects...)
	lineup := map[string]string{"t1": "spec_0", "t2": "spec_1", "t3": "spec_2", "t4": "spec_3"}
	_, st1 := applyOne(t, prep, subjects[0], nil, draftAction(lineup))
	states := map[string]json.RawMessage{"team1": modeutil.Marshal(st1)}

	during, err := New().Project(modes.ProjectInput{
		Scope:   scope("proj-seed"),
		Viewer:  modes.Viewer{MembershipID: "m2", Role: "team-captain", Subject: subjects[1]},
		Public:  prep.Public,
		Private: prep.Private,
		States:  states,
		Phase:   modes.PhaseCollecting,
	})
	if err != nil {
		t.Fatalf("Project: %v", err)
	}
	var duringView projection
	if err := json.Unmarshal(during, &duringView); err != nil {
		t.Fatalf("decode projection: %v", err)
	}
	for _, p := range duringView.Subjects {
		if p.SubjectID == "team1" && p.TotalPoints != 0 {
			t.Fatal("another team's total was exposed during play")
		}
	}

	after, err := New().Project(modes.ProjectInput{
		Scope:     scope("proj-seed"),
		Viewer:    modes.Viewer{MembershipID: "m2", Role: "team-captain", Subject: subjects[1]},
		Public:    prep.Public,
		Private:   prep.Private,
		States:    states,
		Phase:     modes.PhaseSettled,
		RevealNow: true,
	})
	if err != nil {
		t.Fatalf("Project reveal: %v", err)
	}
	var afterView projection
	if err := json.Unmarshal(after, &afterView); err != nil {
		t.Fatalf("decode projection: %v", err)
	}
	revealed := false
	for _, p := range afterView.Subjects {
		if p.SubjectID == "team1" && p.TotalPoints > 0 {
			revealed = true
		}
	}
	if !revealed {
		t.Fatal("the lineup total was not released on reveal")
	}
}

// TestConfigValidation covers the config bounds .
func TestConfigValidation(t *testing.T) {
	cases := []struct {
		name    string
		cfg     string
		wantErr bool
	}{
		{name: "empty uses defaults", cfg: ``},
		{name: "solo valid", cfg: `{"variant":"solo"}`},
		{name: "teams valid", cfg: `{"variant":"teams"}`},
		{name: "zero budget rejected", cfg: `{"budget":-1}`, wantErr: true},
		{name: "unknown variant rejected", cfg: `{"variant":"clan"}`, wantErr: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := New().ValidateConfig(json.RawMessage(tc.cfg))
			if tc.wantErr && err == nil {
				t.Fatalf("expected an error for %s", tc.cfg)
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}
