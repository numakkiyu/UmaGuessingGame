package aptitudegrid

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/bhcn/umaguessinggame/internal/modes"
	"github.com/bhcn/umaguessinggame/internal/modes/modeutil"
)

func TestVariantWindows(t *testing.T) {
	for _, tc := range []struct {
		config string
		round  time.Duration
		turn   time.Duration
	}{
		{`{}`, 180 * time.Second, 0},
		{`{"variant":"challenge"}`, 180 * time.Second, 0},
		{`{"variant":"duel"}`, 30 * time.Second, 30 * time.Second},
		{`{"variant":"invalid"}`, 0, 0},
	} {
		t.Run(tc.config, func(t *testing.T) {
			if got := New().RoundWindow(json.RawMessage(tc.config)); got != tc.round {
				t.Fatalf("round window = %v, want %v", got, tc.round)
			}
			if got := New().TurnWindow(json.RawMessage(tc.config)); got != tc.turn {
				t.Fatalf("turn window = %v, want %v", got, tc.turn)
			}
		})
	}
}

// fakeContent is a synthetic compiled pack section store.
type fakeContent map[string]json.RawMessage

func (f fakeContent) Section(name string) (json.RawMessage, bool) {
	raw, ok := f[name]
	return raw, ok
}

func (f fakeContent) Version() string { return "test-pack-1" }

// cardSpec is the on-the-wire spelling of an aptitude card.
type cardSpec struct {
	CharacterID string                       `json:"characterId"`
	Name        string                       `json:"name"`
	ImagePath   string                       `json:"imagePath"`
	Aptitudes   map[string]map[string]string `json:"aptitudes"`
}

// packOf builds an aptitude-grid pack from card specs.
func packOf(specs ...cardSpec) fakeContent {
	raw, err := json.Marshal(specs)
	if err != nil {
		panic(err)
	}
	return fakeContent{"aptitude-grid.cards": raw}
}

// fullCard builds a card graded A on one distance and one style.
func fullCard(id, name, distance, style string) cardSpec {
	return cardSpec{
		CharacterID: id,
		Name:        name,
		ImagePath:   "/assets/" + id,
		Aptitudes: map[string]map[string]string{
			"surface":  {"草地": "A"},
			"distance": {distance: "A"},
			"style":    {style: "A"},
		},
	}
}

// solvablePack returns a nine-card pack that can fill every board cell exactly
// once: card (i,j) is graded A on distanceLadder[i] and styleLadder[j].
func solvablePack() fakeContent {
	specs := make([]cardSpec, 0, CellCount)
	for i, d := range distanceLadder {
		for j, s := range styleLadder {
			specs = append(specs, fullCard(
				"c_"+d+"_"+s,
				d+s,
				d, s,
			))
			_ = i
			_ = j
		}
	}
	return packOf(specs...)
}

// solvablePlus returns a solvable nine-card pack with extra cards appended, so
// tests can add a special card without making the pool unsolvable.
func solvablePlus(extras ...cardSpec) fakeContent {
	var specs []cardSpec
	for _, d := range distanceLadder {
		for _, st := range styleLadder {
			specs = append(specs, fullCard("c_"+d+"_"+st, d+st, d, st))
		}
	}
	specs = append(specs, extras...)
	return packOf(specs...)
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

// preparedEnvelopeOf decodes the prepared public state.
func preparedEnvelopeOf(t *testing.T, prep modes.Prepared) preparedEnvelope {
	t.Helper()
	var env preparedEnvelope
	if err := json.Unmarshal(prep.Public, &env); err != nil {
		t.Fatalf("decode prepared public: %v", err)
	}
	return env
}

// prepare runs Prepare and fails the test on error.
func prepare(t *testing.T, pack fakeContent, seed, cfg string, subjects ...modes.Subject) modes.Prepared {
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
	return prep
}

// claimAction builds a contract-shaped GridClaim payload for a board cell index.
func claimAction(cell int, characterID string) modes.ModeAction {
	row, col := cell/GridCols, cell%GridCols
	raw, _ := json.Marshal(claimPayload{
		Kind:        ActionClaim,
		CellID:      cellIDFor(row, col),
		CharacterID: characterID,
	})
	return modes.ModeAction{Kind: ActionClaim, Payload: raw}
}

// claimActionByID builds a GridClaim payload from an explicit cell id.
func claimActionByID(cellID, characterID string) modes.ModeAction {
	raw, _ := json.Marshal(claimPayload{
		Kind: ActionClaim, CellID: cellID, CharacterID: characterID,
	})
	return modes.ModeAction{Kind: ActionClaim, Payload: raw}
}

// applyResult bundles the outcome of one Apply call.
type applyResult struct {
	dec modes.Decision
	st  state
}

// applyOne runs Apply then CommitShared, mirroring the room layer's transaction
// so tests exercise the real shared-board fold.
func applyOne(t *testing.T, prep modes.Prepared, subj modes.Subject, prev json.RawMessage, action modes.ModeAction) (applyResult, modes.Prepared) {
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
	next := prep
	if dec.Accepted {
		pub, priv, err := New().CommitShared(modes.ApplyInput{
			Scope:   scope("apply"),
			Subject: subj,
			Action:  action,
			Public:  prep.Public,
			Private: prep.Private,
			State:   prev,
		}, dec)
		if err != nil {
			t.Fatalf("CommitShared: %v", err)
		}
		next = modes.Prepared{Public: pub, Private: priv}
	}
	return applyResult{dec: dec, st: st}, next
}

// TestPrepareBuildsNineCellsWithExplicitConditions asserts every cell carries
// two concrete conditions, which the result screen must show .
func TestPrepareBuildsNineCellsWithExplicitConditions(t *testing.T) {
	subjects := []modes.Subject{{ID: "p1", Kind: modes.SubjectPlayer}}
	env := preparedEnvelopeOf(t, prepare(t, solvablePack(), "board-seed", `{}`, subjects...))

	if len(env.Round.Cells) != CellCount {
		t.Fatalf("cells = %d, want %d", len(env.Round.Cells), CellCount)
	}
	for _, cell := range env.Round.Cells {
		if cell.RowCond.Value == "" || cell.ColCond.Value == "" {
			t.Fatalf("cell %d has an empty condition: %+v", cell.Index, cell)
		}
		if cell.RowCond.Axis == cell.ColCond.Axis {
			t.Fatalf("cell %d uses the same axis twice", cell.Index)
		}
	}
	// The default board is distance × style.
	if env.Round.Cells[0].RowCond.Axis != "distance" || env.Round.Cells[0].ColCond.Axis != "style" {
		t.Fatalf("default axes = %s/%s, want distance/style",
			env.Round.Cells[0].RowCond.Axis, env.Round.Cells[0].ColCond.Axis)
	}
	// The transposed board swaps them.
	envT := preparedEnvelopeOf(t, prepare(t, solvablePack(), "board-seed", `{"rowAxis":"style"}`, subjects...))
	if envT.Round.Cells[0].RowCond.Axis != "style" || envT.Round.Cells[0].ColCond.Axis != "distance" {
		t.Fatalf("transposed axes = %s/%s", envT.Round.Cells[0].RowCond.Axis, envT.Round.Cells[0].ColCond.Axis)
	}
}

// TestUnsolvablePoolRejected covers the documented "不可解小牌池" case: a pool
// that cannot fill all nine cells with distinct characters is refused before the
// round opens .
func TestUnsolvablePoolRejected(t *testing.T) {
	subjects := []modes.Subject{{ID: "p1", Kind: modes.SubjectPlayer}}
	cases := []struct {
		name string
		pack fakeContent
	}{
		{
			name: "empty pool",
			pack: packOf(),
		},
		{
			name: "too few cards",
			pack: packOf(
				fullCard("a", "甲", "短", "逃"),
				fullCard("b", "乙", "英", "先"),
			),
		},
		{
			name: "many cards but one cell unreachable",
			pack: packOf(
				// Every card is graded A on 短/逃, so the 中/差 cell can never be
				// filled even though the pool is large.
				fullCard("a", "甲", "短", "逃"),
				fullCard("b", "乙", "短", "逃"),
				fullCard("c", "丙", "短", "逃"),
				fullCard("d", "丁", "短", "逃"),
				fullCard("e", "戊", "短", "逃"),
				fullCard("f", "己", "短", "逃"),
				fullCard("g", "庚", "短", "逃"),
				fullCard("h", "辛", "短", "逃"),
				fullCard("i", "壬", "短", "逃"),
			),
		},
		{
			name: "nine cards but two compete for one cell",
			pack: packOf(
				// 中/差 is unreachable: no card has 中 distance with 差 style.
				fullCard("a", "甲", "短", "逃"),
				fullCard("b", "乙", "短", "先"),
				fullCard("c", "丙", "短", "差"),
				fullCard("d", "丁", "英", "逃"),
				fullCard("e", "戊", "英", "先"),
				fullCard("f", "己", "英", "差"),
				fullCard("g", "庚", "中", "逃"),
				fullCard("h", "辛", "中", "先"),
				fullCard("i", "壬", "中", "先"),
			),
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := New().Prepare(context.Background(), modes.PrepareInput{
				Scope:   scope("unsolvable"),
				Subject: subjects,
				Config:  json.RawMessage(`{}`),
				Content: tc.pack,
			})
			if err == nil {
				t.Fatal("Prepare accepted an unsolvable pool")
			}
		})
	}
}

// TestSolvablePoolAccepted asserts a fully solvable pool opens, including the
// case where several characters compete for the same cell (all legal
// alternative solutions must be accepted, ).
func TestSolvablePoolAccepted(t *testing.T) {
	subjects := []modes.Subject{{ID: "p1", Kind: modes.SubjectPlayer}}
	pack := solvablePack()
	prep, err := New().Prepare(context.Background(), modes.PrepareInput{
		Scope:   scope("solvable"),
		Subject: subjects,
		Config:  json.RawMessage(`{}`),
		Content: pack,
	})
	if err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	env := preparedEnvelopeOf(t, prep)
	if !env.Round.Solvable {
		t.Fatal("prepared round is not marked solvable")
	}
	if prep.QuestionCount != CellCount {
		t.Fatalf("questionCount = %d, want %d", prep.QuestionCount, CellCount)
	}
}

// TestClaimRequiresBothConditions asserts a claim is refused unless the character
// satisfies both of the cell's conditions .
func TestClaimRequiresBothConditions(t *testing.T) {
	subjects := []modes.Subject{{ID: "p1", Kind: modes.SubjectPlayer}}
	// Cell 0 is 短×逃; a card graded A on 短 but only B on 逃 must be refused.
	pack := solvablePlus(cardSpec{
		CharacterID: "half", Name: "半",
		Aptitudes: map[string]map[string]string{
			"distance": {"短": "A"},
			"style":    {"逃": "B"},
		},
	})
	prep2 := prepare(t, pack, "cond-seed", `{}`, subjects...)

	res, _ := applyOne(t, prep2, subjects[0], nil, claimAction(0, "half"))
	if res.dec.Accepted {
		t.Fatal("claim with a B grade was accepted")
	}
	if res.dec.Code != CodeNotSatisfied {
		t.Fatalf("code = %q, want %q", res.dec.Code, CodeNotSatisfied)
	}
	// An illegal claim must not consume the turn or the cell .
	if res.st.Attempts != 0 || len(res.st.Claims) != 0 {
		t.Fatalf("illegal claim mutated state: %+v", res.st)
	}

	// The correctly graded card is accepted.
	res, _ = applyOne(t, prep2, subjects[0], nil, claimAction(0, "c_短_逃"))
	if !res.dec.Accepted {
		t.Fatalf("valid claim rejected: %s %s", res.dec.Code, res.dec.Detail)
	}
}

// TestDuplicateCharacterRejected covers the documented "同一角色重复" case in the
// challenge form: each independent board must use nine different characters
// .
func TestDuplicateCharacterRejected(t *testing.T) {
	subjects := []modes.Subject{{ID: "p1", Kind: modes.SubjectPlayer}}

	// A card that satisfies two cells (短×逃 and 短×先) lets the reuse rule be
	// isolated from the condition rule.
	pack := solvablePlus(cardSpec{
		CharacterID: "wide", Name: "广",
		Aptitudes: map[string]map[string]string{
			"distance": {"短": "A"},
			"style":    {"逃": "A", "先": "A"},
		},
	})
	prepWide := prepare(t, pack, "dup-seed", `{}`, subjects...)

	res, next := applyOne(t, prepWide, subjects[0], nil, claimAction(0, "wide"))
	if !res.dec.Accepted {
		t.Fatalf("first claim rejected: %s %s", res.dec.Code, res.dec.Detail)
	}
	// Cell 1 is 短×先, which "wide" also satisfies, so the only reason to
	// refuse is character reuse.
	res2, _ := applyOne(t, next, subjects[0], modeutil.Marshal(res.st), claimAction(1, "wide"))
	if res2.dec.Accepted {
		t.Fatal("reusing a character on the same board was accepted")
	}
	if res2.dec.Code != CodeCharacterUsed {
		t.Fatalf("code = %q, want %q", res2.dec.Code, CodeCharacterUsed)
	}
}

// TestCellOccupiedConcurrently covers the documented "同格并发" case: two claims
// on the same cell in the duel can only succeed once, because the second
// transaction re-reads the committed board .
func TestCellOccupiedConcurrently(t *testing.T) {
	subjects := []modes.Subject{
		{ID: "p1", Kind: modes.SubjectPlayer},
		{ID: "p2", Kind: modes.SubjectPlayer},
	}
	prep := prepare(t, solvablePack(), "conc-seed", `{"variant":"duel"}`, subjects...)
	env := preparedEnvelopeOf(t, prep)

	// Work out whose turn it is, then simulate both players racing for cell 0.
	turnHolder := env.Board.TurnSubjectID
	var first, second modes.Subject
	for _, subj := range subjects {
		if subj.ID == turnHolder {
			first = subj
		} else {
			second = subj
		}
	}

	res, next := applyOne(t, prep, first, nil, claimAction(0, "c_短_逃"))
	if !res.dec.Accepted {
		t.Fatalf("first claim rejected: %s %s", res.dec.Code, res.dec.Detail)
	}

	// The other player now holds the turn and tries the same cell.
	res2, _ := applyOne(t, next, second, nil, claimAction(0, "c_英_先"))
	if res2.dec.Accepted {
		t.Fatal("second claim on the same cell was accepted")
	}
	if res2.dec.Code != CodeCellOccupied {
		t.Fatalf("code = %q, want %q", res2.dec.Code, CodeCellOccupied)
	}

	// A stale board snapshot (the pre-claim state) must not allow a claim that
	// the committed board already forbids.
	res3, _ := applyOne(t, prep, second, nil, claimAction(0, "c_英_先"))
	if res3.dec.Accepted {
		t.Fatal("claim against a stale board snapshot was accepted")
	}
}

// TestTurnOrderAlternates covers the documented "先后手" case: the duel alternates
// turns, a subject cannot act out of turn, and the first mover is seed-derived so
// it is deterministic rather than arrival-order dependent .
func TestTurnOrderAlternates(t *testing.T) {
	subjects := []modes.Subject{
		{ID: "p1", Kind: modes.SubjectPlayer},
		{ID: "p2", Kind: modes.SubjectPlayer},
	}

	// The first mover is stable for a fixed seed.
	firstA := preparedEnvelopeOf(t, prepare(t, solvablePack(), "turn-seed", `{"variant":"duel"}`, subjects...)).Board.TurnSubjectID
	firstB := preparedEnvelopeOf(t, prepare(t, solvablePack(), "turn-seed", `{"variant":"duel"}`, subjects...)).Board.TurnSubjectID
	if firstA != firstB {
		t.Fatalf("first mover is not deterministic: %q vs %q", firstA, firstB)
	}

	prep := prepare(t, solvablePack(), "turn-seed", `{"variant":"duel"}`, subjects...)
	var holder, other modes.Subject
	for _, subj := range subjects {
		if subj.ID == firstA {
			holder = subj
		} else {
			other = subj
		}
	}

	// Acting out of turn is refused.
	res, _ := applyOne(t, prep, other, nil, claimAction(0, "c_短_逃"))
	if res.dec.Accepted {
		t.Fatal("out-of-turn claim was accepted")
	}
	if res.dec.Code != CodeOutOfTurn {
		t.Fatalf("code = %q, want %q", res.dec.Code, CodeOutOfTurn)
	}

	// The holder acts, and the turn passes to the other subject.
	res, next := applyOne(t, prep, holder, nil, claimAction(0, "c_短_逃"))
	if !res.dec.Accepted {
		t.Fatalf("holder claim rejected: %s", res.dec.Code)
	}
	if env := preparedEnvelopeOf(t, next); env.Board.TurnSubjectID != other.ID {
		t.Fatalf("turn holder = %q, want %q", env.Board.TurnSubjectID, other.ID)
	}
}

// TestDuelNeedsTwoSubjects asserts the roster requirement for the alternating
// form .
func TestDuelNeedsTwoSubjects(t *testing.T) {
	for _, n := range []int{0, 1, 3} {
		subjects := make([]modes.Subject, 0, n)
		for i := 0; i < n; i++ {
			subjects = append(subjects, modes.Subject{
				ID:   string(rune('a' + i)),
				Kind: modes.SubjectPlayer,
			})
		}
		_, err := New().Prepare(context.Background(), modes.PrepareInput{
			Scope:   scope("duel-roster"),
			Subject: subjects,
			Config:  json.RawMessage(`{"variant":"duel"}`),
			Content: solvablePack(),
		})
		if err == nil {
			t.Fatalf("duel accepted %d subjects", n)
		}
	}
}

// TestDisconnectSkipAndConsecutiveTimeouts covers the documented "断线跳过" case:
// a timeout skips the turn, and two consecutive timeouts end the personal match
// .
func TestDisconnectSkipAndConsecutiveTimeouts(t *testing.T) {
	subjects := []modes.Subject{
		{ID: "p1", Kind: modes.SubjectPlayer},
		{ID: "p2", Kind: modes.SubjectPlayer},
	}
	prep := prepare(t, solvablePack(), "skip-seed", `{"variant":"duel"}`, subjects...)
	env := preparedEnvelopeOf(t, prep)
	holderID := env.Board.TurnSubjectID
	var holder modes.Subject
	for _, subj := range subjects {
		if subj.ID == holderID {
			holder = subj
		}
	}

	skip := modes.ModeAction{Kind: ActionSkip, Payload: json.RawMessage(`{}`)}

	res, next := applyOne(t, prep, holder, nil, skip)
	if !res.dec.Accepted {
		t.Fatalf("skip rejected: %s", res.dec.Code)
	}
	if res.st.ConsecutiveTimeouts != 1 {
		t.Fatalf("consecutiveTimeouts = %d, want 1", res.st.ConsecutiveTimeouts)
	}
	if res.dec.Finished {
		t.Fatal("a single timeout must not finish the match")
	}
	// The turn passes after a skip.
	if got := preparedEnvelopeOf(t, next).Board.TurnSubjectID; got == holderID {
		t.Fatal("the turn did not pass after a skip")
	}

	// The opponent must take their turn before the holder can time out again.
	var opponent modes.Subject
	for _, subj := range subjects {
		if subj.ID != holderID {
			opponent = subj
		}
	}
	opponentResult, returned := applyOne(t, next, opponent, nil, skip)
	if !opponentResult.dec.Accepted {
		t.Fatalf("opponent skip rejected: %s", opponentResult.dec.Code)
	}
	// A second consecutive timeout on this subject's next turn ends their match.
	res2, _ := applyOne(t, returned, holder, modeutil.Marshal(res.st), skip)
	if res2.st.ConsecutiveTimeouts != 2 {
		t.Fatalf("consecutiveTimeouts = %d, want 2", res2.st.ConsecutiveTimeouts)
	}
	if !res2.st.TimedOut {
		t.Fatal("two consecutive timeouts did not drop the subject")
	}
	if !res2.dec.Finished {
		t.Fatal("the second timeout did not report Finished")
	}

	// A claim after being dropped is refused.
	res3, _ := applyOne(t, next, holder, modeutil.Marshal(res2.st), claimAction(0, "c_短_逃"))
	if res3.dec.Accepted {
		t.Fatal("a dropped subject could still claim")
	}
}

// TestAllLegalAlternativeSolutionsAccepted covers the documented "全部合法替代解"
// case: any character that satisfies a cell is a legal claim, so different
// characters filling the same cell are all accepted on separate boards .
func TestAllLegalAlternativeSolutionsAccepted(t *testing.T) {
	subjects := []modes.Subject{
		{ID: "p1", Kind: modes.SubjectPlayer},
		{ID: "p2", Kind: modes.SubjectPlayer},
	}
	// Two independent boards, so both subjects may use the same character.
	prep := prepare(t, solvablePack(), "alt-seed", `{}`, subjects...)

	res1, _ := applyOne(t, prep, subjects[0], nil, claimAction(0, "c_短_逃"))
	if !res1.dec.Accepted {
		t.Fatalf("p1 claim rejected: %s", res1.dec.Code)
	}
	// p2 has an independent board, so cell 0 is still free for them.
	res2, _ := applyOne(t, prep, subjects[1], nil, claimAction(0, "c_短_逃"))
	if !res2.dec.Accepted {
		t.Fatalf("p2 claim on their own board rejected: %s %s", res2.dec.Code, res2.dec.Detail)
	}
	// And a different character satisfying the same cell also works.
	res3, _ := applyOne(t, prep, subjects[1], modeutil.Marshal(res2.st), claimAction(1, "c_短_先"))
	if !res3.dec.Accepted {
		t.Fatalf("alternative legal character rejected: %s %s", res3.dec.Code, res3.dec.Detail)
	}
}

// TestChallengeScore asserts the independent challenge score is
// floor(100×correctCells/9) .
func TestChallengeScore(t *testing.T) {
	subjects := []modes.Subject{{ID: "p1", Kind: modes.SubjectPlayer}}
	prep := prepare(t, solvablePack(), "score-seed", `{}`, subjects...)

	cases := []struct {
		name        string
		cellIndices []int
		wantUnits   int
	}{
		{name: "zero correct", cellIndices: nil, wantUnits: 0},
		{name: "one correct is 11", cellIndices: []int{0}, wantUnits: 1100},
		{name: "three correct is 33", cellIndices: []int{0, 1, 2}, wantUnits: 3300},
		{name: "five correct is 55", cellIndices: []int{0, 1, 2, 3, 4}, wantUnits: 5500},
		{name: "nine correct is 100", cellIndices: []int{0, 1, 2, 3, 4, 5, 6, 7, 8}, wantUnits: 10000},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// Build a board state by claiming the requested cells.
			st := state{Claims: tc.cellIndices}
			board := boardState{}
			for i, cell := range tc.cellIndices {
				board.Claims = append(board.Claims, claim{
					CellIndex: cell, SubjectID: "p1", Turn: i + 1,
				})
			}
			pub := modeutil.Marshal(struct {
				Round publicPrepared `json:"round"`
				Board boardState     `json:"board"`
			}{
				Round: preparedEnvelopeOf(t, prep).Round,
				Board: board,
			})
			scores, err := New().Settle(modes.SettleInput{
				Scope:   scope("score-seed"),
				Phase:   modes.PhaseSettled,
				Public:  pub,
				Private: prep.Private,
				States:  map[string]json.RawMessage{"p1": modeutil.Marshal(st)},
			})
			if err != nil {
				t.Fatalf("Settle: %v", err)
			}
			if len(scores) != 1 {
				t.Fatalf("scores = %d, want 1", len(scores))
			}
			if scores[0].ScoreUnits != tc.wantUnits {
				t.Fatalf("scoreUnits = %d, want %d", scores[0].ScoreUnits, tc.wantUnits)
			}
			if err := scores[0].Validate(); err != nil {
				t.Fatalf("invalid score: %v", err)
			}
		})
	}
}

// TestDuelSettleRanksByClaims asserts the duel ranks by claimed cells and shares
// a rank on a tie .
func TestDuelSettleRanksByClaims(t *testing.T) {
	subjects := []modes.Subject{
		{ID: "p1", Kind: modes.SubjectPlayer},
		{ID: "p2", Kind: modes.SubjectPlayer},
	}
	prep := prepare(t, solvablePack(), "duel-score", `{"variant":"duel"}`, subjects...)
	env := preparedEnvelopeOf(t, prep)

	board := boardState{Closed: true}
	for i := 0; i < 5; i++ {
		board.Claims = append(board.Claims, claim{CellIndex: i, SubjectID: "p1", Turn: i + 1})
	}
	for i := 5; i < 8; i++ {
		board.Claims = append(board.Claims, claim{CellIndex: i, SubjectID: "p2", Turn: i + 1})
	}
	pub := modeutil.Marshal(struct {
		Round publicPrepared `json:"round"`
		Board boardState     `json:"board"`
	}{Round: env.Round, Board: board})

	scores, err := New().Settle(modes.SettleInput{
		Scope:   scope("duel-score"),
		Phase:   modes.PhaseSettled,
		Public:  pub,
		Private: prep.Private,
		States: map[string]json.RawMessage{
			"p1": modeutil.Marshal(state{Claims: []int{0, 1, 2, 3, 4}}),
			"p2": modeutil.Marshal(state{Claims: []int{5, 6, 7}}),
		},
	})
	if err != nil {
		t.Fatalf("Settle: %v", err)
	}
	if scores[0].Subject.ID != "p1" || scores[0].Rank != 1 {
		t.Fatalf("winner = %s rank %d, want p1 rank 1", scores[0].Subject.ID, scores[0].Rank)
	}
	if scores[1].Subject.ID != "p2" || scores[1].Rank != 2 {
		t.Fatalf("runner-up = %s rank %d, want p2 rank 2", scores[1].Subject.ID, scores[1].Rank)
	}
	if scores[0].ScoreUnits <= scores[1].ScoreUnits {
		t.Fatalf("winner score %d not above runner-up %d",
			scores[0].ScoreUnits, scores[1].ScoreUnits)
	}

	// A tie shares rank 1.
	tiedBoard := boardState{Closed: true}
	for i := 0; i < 4; i++ {
		tiedBoard.Claims = append(tiedBoard.Claims, claim{CellIndex: i, SubjectID: "p1", Turn: i + 1})
	}
	for i := 4; i < 8; i++ {
		tiedBoard.Claims = append(tiedBoard.Claims, claim{CellIndex: i, SubjectID: "p2", Turn: i + 1})
	}
	tiedPub := modeutil.Marshal(struct {
		Round publicPrepared `json:"round"`
		Board boardState     `json:"board"`
	}{Round: env.Round, Board: tiedBoard})
	tiedScores, err := New().Settle(modes.SettleInput{
		Scope:   scope("duel-score"),
		Phase:   modes.PhaseSettled,
		Public:  tiedPub,
		Private: prep.Private,
		States: map[string]json.RawMessage{
			"p1": modeutil.Marshal(state{Claims: []int{0, 1, 2, 3}}),
			"p2": modeutil.Marshal(state{Claims: []int{4, 5, 6, 7}}),
		},
	})
	if err != nil {
		t.Fatalf("Settle tied: %v", err)
	}
	if tiedScores[0].Rank != 1 || tiedScores[1].Rank != 1 {
		t.Fatalf("tied ranks = %d,%d, want 1,1", tiedScores[0].Rank, tiedScores[1].Rank)
	}
	if !tiedScores[0].SharedRank || !tiedScores[1].SharedRank {
		t.Fatal("tied duel scores must both report SharedRank")
	}
}

// TestPrepareDeterministic asserts the hard determinism gate for this mode: the
// same scope yields byte-identical prepared state, and the duel's first mover is
// seed-derived .
func TestPrepareDeterministic(t *testing.T) {
	subjects := []modes.Subject{
		{ID: "p1", Kind: modes.SubjectPlayer},
		{ID: "p2", Kind: modes.SubjectPlayer},
	}
	pack := solvablePack()
	first := prepare(t, pack, "det-seed", `{"variant":"duel"}`, subjects...)
	second := prepare(t, pack, "det-seed", `{"variant":"duel"}`, subjects...)
	if string(first.Public) != string(second.Public) {
		t.Fatal("prepared public state differs between two identical prepares")
	}
	if string(first.Private) != string(second.Private) {
		t.Fatal("prepared private state differs between two identical prepares")
	}
}

// TestApplyIsDeterministic asserts replaying the same claim yields byte-identical
// decisions .
func TestApplyIsDeterministic(t *testing.T) {
	subjects := []modes.Subject{{ID: "p1", Kind: modes.SubjectPlayer}}
	prep := prepare(t, solvablePack(), "apply-det", `{}`, subjects...)
	run := func() (string, string) {
		res, next := applyOne(t, prep, subjects[0], nil, claimAction(4, "c_英_先"))
		return string(res.dec.Feedback), string(next.Public)
	}
	fbA, pubA := run()
	fbB, pubB := run()
	if fbA != fbB {
		t.Fatal("feedback differs between identical runs")
	}
	if pubA != pubB {
		t.Fatal("committed board differs between identical runs")
	}
}

// TestProjectHidesAptitudesUntilReveal asserts the graded aptitudes are not
// exposed during play, which would turn the closed-book board into a lookup
// table .
func TestProjectHidesAptitudesUntilReveal(t *testing.T) {
	subjects := []modes.Subject{{ID: "p1", Kind: modes.SubjectPlayer}}
	prep := prepare(t, solvablePack(), "proj-seed", `{}`, subjects...)

	during, err := New().Project(modes.ProjectInput{
		Scope:   scope("proj-seed"),
		Viewer:  modes.Viewer{MembershipID: "m1", Role: "player", Subject: subjects[0]},
		Public:  prep.Public,
		Private: prep.Private,
		States:  map[string]json.RawMessage{"p1": modeutil.Marshal(state{Claims: []int{0}})},
		Phase:   modes.PhaseCollecting,
	})
	if err != nil {
		t.Fatalf("Project: %v", err)
	}
	var duringView projection
	if err := json.Unmarshal(during, &duringView); err != nil {
		t.Fatalf("decode projection: %v", err)
	}
	if len(duringView.Aptitudes) != 0 {
		t.Fatal("aptitudes were exposed during play")
	}
	for _, cell := range duringView.Cells {
		if cell.Claim != nil && cell.Claim.Satisfied != nil {
			t.Fatalf("cell %d exposed a satisfied flag during play", cell.Index)
		}
	}

	// On reveal they appear.
	after, err := New().Project(modes.ProjectInput{
		Scope:     scope("proj-seed"),
		Viewer:    modes.Viewer{MembershipID: "m1", Role: "player", Subject: subjects[0]},
		Public:    prep.Public,
		Private:   prep.Private,
		States:    map[string]json.RawMessage{"p1": modeutil.Marshal(state{Claims: []int{0}})},
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
	if len(afterView.Aptitudes) == 0 {
		t.Fatal("aptitudes were not released on reveal")
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
		{name: "challenge is valid", cfg: `{"variant":"challenge"}`},
		{name: "duel is valid", cfg: `{"variant":"duel"}`},
		{name: "style rows are valid", cfg: `{"rowAxis":"style"}`},
		{name: "unknown variant rejected", cfg: `{"variant":"triple"}`, wantErr: true},
		{name: "unknown axis rejected", cfg: `{"rowAxis":"height"}`, wantErr: true},
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

// TestInvalidPayloadsRejected covers malformed claims.
func TestInvalidPayloadsRejected(t *testing.T) {
	subjects := []modes.Subject{{ID: "p1", Kind: modes.SubjectPlayer}}
	prep := prepare(t, solvablePack(), "payload-seed", `{}`, subjects...)
	cases := []struct {
		name string
		kind string
		body string
		want string
	}{
		{name: "unknown action", kind: "teleport", body: `{}`, want: CodeUnknownAction},
		{name: "empty character", kind: ActionClaim, body: `{"cellId":"cell-0-0"}`, want: CodeInvalidPayload},
		{name: "empty cell id", kind: ActionClaim, body: `{"characterId":"c_短_逃"}`, want: CodeInvalidPayload},
		{name: "unknown cell id", kind: ActionClaim, body: `{"cellId":"cell-9-9","characterId":"c_短_逃"}`, want: CodeInvalidPayload},
		{name: "unknown character", kind: ActionClaim, body: `{"cellId":"cell-0-0","characterId":"nope"}`, want: CodeUnknownCharacter},
		{name: "payload kind mismatch", kind: ActionClaim, body: `{"kind":"grid.nope","cellId":"cell-0-0","characterId":"c_短_逃"}`, want: CodeInvalidPayload},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			res, _ := applyOne(t, prep, subjects[0], nil, modes.ModeAction{
				Kind:    tc.kind,
				Payload: json.RawMessage(tc.body),
			})
			if res.dec.Accepted {
				t.Fatalf("accepted an invalid action: %s", tc.body)
			}
			if res.dec.Code != tc.want {
				t.Fatalf("code = %q, want %q", res.dec.Code, tc.want)
			}
		})
	}
}

// The challenge has one board per subject, including when two subjects occupy
// the same position with different cards. Coarse progress may be shared, not
// another subject's actual card choices.
func TestChallengeProjectionKeepsIndependentBoards(t *testing.T) {
	subjects := []modes.Subject{{ID: "p1", Kind: modes.SubjectPlayer}, {ID: "p2", Kind: modes.SubjectPlayer}}
	prep := prepare(t, solvablePlus(fullCard("second", "另一位", "短", "逃")), "independent-projection", `{"variant":"challenge"}`, subjects...)
	first, next := applyOne(t, prep, subjects[0], nil, claimAction(0, "c_短_逃"))
	if !first.dec.Accepted {
		t.Fatal(first.dec.Code)
	}
	second, next := applyOne(t, next, subjects[1], nil, claimAction(0, "second"))
	if !second.dec.Accepted {
		t.Fatal(second.dec.Code)
	}
	states := map[string]json.RawMessage{"p1": first.dec.State, "p2": second.dec.State}
	for _, reveal := range []bool{false, true} {
		phase := modes.PhaseCollecting
		if reveal {
			phase = modes.PhaseRevealed
		}
		for i, viewer := range []modes.Subject{subjects[0], subjects[1], {}} {
			raw, err := New().Project(modes.ProjectInput{Public: next.Public, Private: next.Private, States: states, Viewer: modes.Viewer{Role: "player", Subject: viewer}, Phase: phase, RevealNow: reveal})
			if err != nil {
				t.Fatal(err)
			}
			var projected projection
			if err := json.Unmarshal(raw, &projected); err != nil {
				t.Fatal(err)
			}
			if i == 2 {
				for _, cell := range projected.Cells {
					if cell.Claim != nil {
						t.Fatalf("observer received an arbitrary independent board: %s", raw)
					}
				}
				continue
			}
			want := []string{"c_短_逃", "second"}[i]
			if projected.Cells[0].Claim == nil || projected.Cells[0].Claim.CharacterID != want || projected.Cells[0].Claim.SubjectID != viewer.ID {
				t.Fatalf("wrong independent board: %s", raw)
			}
			if (projected.Cells[0].Claim.Satisfied != nil) != reveal {
				t.Fatalf("incorrect reveal boundary: %s", raw)
			}
			if projected.Self == nil || len(projected.Self.Claims) != 1 {
				t.Fatalf("own progress lost: %s", raw)
			}
		}
	}
	scores, err := New().Settle(modes.SettleInput{Public: next.Public, Private: next.Private, States: states, Phase: modes.PhaseSettled})
	if err != nil {
		t.Fatal(err)
	}
	for _, score := range scores {
		if score.ScoreUnits != 1100 {
			t.Fatalf("own one-cell score changed: %+v", score)
		}
	}
}
