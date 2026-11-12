package classic

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/bhcn/umaguessinggame/internal/modes"
	"github.com/bhcn/umaguessinggame/internal/modes/modeutil"
)

// fakeContent is a synthetic compiled pack section store. Every mode test builds
// its pack this way instead of depending on internal/content, so rule tests stay
// isolated from the pipeline .
type fakeContent map[string]json.RawMessage

func (f fakeContent) Section(name string) (json.RawMessage, bool) {
	raw, ok := f[name]
	return raw, ok
}

func (f fakeContent) Version() string { return "test-pack-1" }

// packEntry is the on-the-wire spelling of a classic character, using the same
// snake_case names the existing data/question_bank_ready.json uses.
type packEntry struct {
	ID                string   `json:"id"`
	NameZh            string   `json:"name_zh"`
	Star              *int     `json:"star"`
	SurfaceGroup      []string `json:"surface_group"`
	DistanceGroup     []string `json:"distance_group"`
	RunningStyleGroup []string `json:"running_style_group"`
	SexType           string   `json:"sex_type"`
	G1Bracket         string   `json:"g1_bracket"`
	G23Bracket        string   `json:"g23_bracket"`
	G1HasJpn          bool     `json:"g1_has_jpn"`
	G23HasJpn         bool     `json:"g23_has_jpn"`
	SchoolGrade       string   `json:"school_grade"`
	Dormitory         string   `json:"dormitory"`
	ImageLocalPath    string   `json:"image_local_path"`
	AmbiguityGroup    string   `json:"ambiguity_group,omitempty"`
}

func intPtr(v int) *int { return &v }

// buildPack returns a small but fully populated synthetic classic pack.
func buildPack(entries ...packEntry) fakeContent {
	raw, err := json.Marshal(entries)
	if err != nil {
		panic(err)
	}
	return fakeContent{"classic.characters": raw}
}

// defaultPack is a six-character pack with distinct field vectors.
func defaultPack() fakeContent {
	return buildPack(
		packEntry{
			ID: "special_week", NameZh: "特别周", Star: intPtr(3),
			SurfaceGroup: []string{"草地"}, DistanceGroup: []string{"中", "长"},
			RunningStyleGroup: []string{"先", "差"}, SexType: "牝",
			G1Bracket: "3-4", G23Bracket: "4-6",
			SchoolGrade: "中等部", Dormitory: "栗东宿舍",
			ImageLocalPath: "/assets/special_week",
		},
		packEntry{
			ID: "silence_suzuka", NameZh: "无声铃鹿", Star: intPtr(3),
			SurfaceGroup: []string{"草地"}, DistanceGroup: []string{"英", "中"},
			RunningStyleGroup: []string{"逃"}, SexType: "牝",
			G1Bracket: "1-2", G23Bracket: "1-3",
			SchoolGrade: "高等部", Dormitory: "栗东宿舍",
			ImageLocalPath: "/assets/silence_suzuka",
		},
		packEntry{
			ID: "tokai_teio", NameZh: "东海帝王", Star: intPtr(2),
			SurfaceGroup: []string{"草地"}, DistanceGroup: []string{"中"},
			RunningStyleGroup: []string{"先"}, SexType: "牡",
			G1Bracket: "3-4", G23Bracket: "4-6",
			SchoolGrade: "中等部", Dormitory: "栗东宿舍",
			ImageLocalPath: "/assets/tokai_teio",
		},
		packEntry{
			ID: "oguri_cap", NameZh: "小栗帽", Star: intPtr(1),
			SurfaceGroup: []string{"草地", "泥地"}, DistanceGroup: []string{"中"},
			RunningStyleGroup: []string{"差", "追"}, SexType: "牡",
			G1Bracket: "5+", G23Bracket: "7+",
			SchoolGrade: "高等部", Dormitory: "美浦宿舍",
			ImageLocalPath: "/assets/oguri_cap",
		},
		packEntry{
			ID: "grass_wonder", NameZh: "草上飞", Star: intPtr(3),
			SurfaceGroup: []string{"草地"}, DistanceGroup: []string{"长"},
			RunningStyleGroup: []string{"追"}, SexType: "牡",
			G1Bracket: "0", G23Bracket: "0",
			SchoolGrade: "高等部", Dormitory: "美浦宿舍",
			ImageLocalPath: "/assets/grass_wonder",
		},
		packEntry{
			ID: "haru_urara", NameZh: "春乌拉拉", Star: intPtr(1),
			SurfaceGroup: []string{"泥地"}, DistanceGroup: []string{"短"},
			RunningStyleGroup: []string{"逃"}, SexType: "牝",
			G1Bracket: "0", G23Bracket: "0",
			SchoolGrade: "中等部", Dormitory: "美浦宿舍",
			ImageLocalPath: "/assets/haru_urara",
		},
	)
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

// prepareRound runs Prepare with the given pack, seed and config.
func prepareRound(t *testing.T, pack fakeContent, seed, cfg string, subjects ...modes.Subject) (modes.Prepared, privatePrepared, publicPrepared) {
	t.Helper()
	m := New()
	prep, err := m.Prepare(context.Background(), modes.PrepareInput{
		Scope:   scope(seed),
		Subject: subjects,
		Config:  json.RawMessage(cfg),
		Content: pack,
	})
	if err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	var priv privatePrepared
	if err := json.Unmarshal(prep.Private, &priv); err != nil {
		t.Fatalf("decode private: %v", err)
	}
	var pub publicPrepared
	if err := json.Unmarshal(prep.Public, &pub); err != nil {
		t.Fatalf("decode public: %v", err)
	}
	return prep, priv, pub
}

// guessAction builds an ActionGuess payload.
func guessAction(id string) modes.ModeAction {
	raw, _ := json.Marshal(guessPayload{CharacterID: id})
	return modes.ModeAction{Kind: ActionGuess, Payload: raw}
}

// apply runs one Apply call against the round state.
func apply(t *testing.T, prep modes.Prepared, subj modes.Subject, prev json.RawMessage, count int, action modes.ModeAction) (modes.Decision, state) {
	t.Helper()
	dec, err := New().Apply(modes.ApplyInput{
		Scope:           scope("seed"),
		Subject:         subj,
		Action:          action,
		Public:          prep.Public,
		Private:         prep.Private,
		State:           prev,
		SubmissionCount: count,
		ReceivedAt:      1000 + int64(count),
	})
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	var next state
	if len(dec.State) > 0 {
		if err := json.Unmarshal(dec.State, &next); err != nil {
			t.Fatalf("decode state: %v", err)
		}
	}
	return dec, next
}

// TestPrepareDeterministic asserts the hard determinism gate: the same scope
// always yields byte-identical public and private material, and a different seed
// selects a different answer .
func TestPrepareDeterministic(t *testing.T) {
	subjects := []modes.Subject{{ID: "p1", Kind: modes.SubjectPlayer}}
	pack := defaultPack()

	first, privA, _ := prepareRound(t, pack, "seed-alpha", `{}`, subjects...)
	second, privB, _ := prepareRound(t, pack, "seed-alpha", `{}`, subjects...)

	if string(first.Public) != string(second.Public) {
		t.Fatalf("public state differs between two identical prepares")
	}
	if string(first.Private) != string(second.Private) {
		t.Fatalf("private state differs between two identical prepares")
	}
	if privA.AnswerID != privB.AnswerID {
		t.Fatalf("answer differs: %q vs %q", privA.AnswerID, privB.AnswerID)
	}

	// Across many seeds the answer must actually vary; a constant answer would
	// pass the equality check above but fail the mode.
	seen := map[string]struct{}{}
	for _, seed := range []string{"s1", "s2", "s3", "s4", "s5", "s6", "s7", "s8"} {
		_, priv, _ := prepareRound(t, pack, seed, `{}`, subjects...)
		seen[priv.AnswerID] = struct{}{}
	}
	if len(seen) < 2 {
		t.Fatalf("answer never varies across seeds, saw %v", seen)
	}
}

// TestGuessWrongDecrementsBudget asserts one wrong guess costs exactly one
// budget unit .
func TestGuessWrongDecrementsBudget(t *testing.T) {
	subjects := []modes.Subject{{ID: "p1", Kind: modes.SubjectPlayer}}
	prep, priv, pub := prepareRound(t, defaultPack(), "budget-seed", `{}`, subjects...)

	var wrongID string
	for _, c := range priv.Pool {
		if c.ID != priv.AnswerID {
			wrongID = c.ID
			break
		}
	}
	if wrongID == "" {
		t.Fatal("no wrong candidate in the pool")
	}

	dec, st := apply(t, prep, subjects[0], nil, 0, guessAction(wrongID))
	if !dec.Accepted {
		t.Fatalf("wrong guess rejected: %s %s", dec.Code, dec.Detail)
	}
	if st.RemainingWrong != pub.Config.WrongBudget-1 {
		t.Fatalf("remainingWrong = %d, want %d", st.RemainingWrong, pub.Config.WrongBudget-1)
	}
	if st.Attempts != 1 {
		t.Fatalf("attempts = %d, want 1", st.Attempts)
	}
	if st.Solved || st.Failed {
		t.Fatalf("wrong guess must not finish the round")
	}
	if dec.Finished {
		t.Fatal("wrong guess reported Finished")
	}
}

// TestCorrectGuessDoesNotDecrementBudget ports the legacy assertion
// "stops decreasing remaining guesses after a correct guess" .
func TestCorrectGuessDoesNotDecrementBudget(t *testing.T) {
	subjects := []modes.Subject{{ID: "p1", Kind: modes.SubjectPlayer}}
	prep, priv, pub := prepareRound(t, defaultPack(), "correct-seed", `{}`, subjects...)

	var wrongID string
	for _, c := range priv.Pool {
		if c.ID != priv.AnswerID {
			wrongID = c.ID
			break
		}
	}
	_, st := apply(t, prep, subjects[0], nil, 0, guessAction(wrongID))
	before := st.RemainingWrong

	dec, st := apply(t, prep, subjects[0], decState(t, st), st.Attempts, guessAction(priv.AnswerID))
	if !dec.Accepted {
		t.Fatalf("correct guess rejected: %s", dec.Code)
	}
	if st.RemainingWrong != before {
		t.Fatalf("correct guess changed the budget: %d -> %d", before, st.RemainingWrong)
	}
	if !st.Solved {
		t.Fatal("correct guess did not mark the round solved")
	}
	if !dec.Finished {
		t.Fatal("correct guess did not report Finished")
	}
	if st.Attempts != 2 {
		t.Fatalf("attempts = %d, want 2", st.Attempts)
	}
	if st.SolvedAtAttempt != 2 {
		t.Fatalf("solvedAtAttempt = %d, want 2", st.SolvedAtAttempt)
	}
	_ = pub
}

// decState re-encodes a state for the next Apply call.
func decState(t *testing.T, st state) json.RawMessage {
	t.Helper()
	return modeutil.Marshal(st)
}

// TestDuplicateGuessRejectedWithoutCost asserts a repeated guess is refused and
// costs no budget, so a retried request cannot burn an attempt .
func TestDuplicateGuessRejectedWithoutCost(t *testing.T) {
	subjects := []modes.Subject{{ID: "p1", Kind: modes.SubjectPlayer}}
	prep, priv, _ := prepareRound(t, defaultPack(), "dup-seed", `{}`, subjects...)

	var wrongID string
	for _, c := range priv.Pool {
		if c.ID != priv.AnswerID {
			wrongID = c.ID
			break
		}
	}

	_, st := apply(t, prep, subjects[0], nil, 0, guessAction(wrongID))
	budgetAfterFirst := st.RemainingWrong
	attemptsAfterFirst := st.Attempts

	dec, st2 := apply(t, prep, subjects[0], decState(t, st), st.Attempts, guessAction(wrongID))
	if dec.Accepted {
		t.Fatal("duplicate guess was accepted")
	}
	if dec.Code != CodeDuplicateGuess {
		t.Fatalf("code = %q, want %q", dec.Code, CodeDuplicateGuess)
	}
	if len(dec.State) != 0 {
		t.Fatal("a rejected action must not return new state")
	}
	if st2.RemainingWrong != 0 && st2.Attempts != 0 {
		t.Fatalf("rejected duplicate mutated state: %+v", st2)
	}
	if budgetAfterFirst != 7 || attemptsAfterFirst != 1 {
		t.Fatalf("first guess bookkeeping wrong: budget=%d attempts=%d", budgetAfterFirst, attemptsAfterFirst)
	}
}

// TestUnknownCharacterRejectedWithoutCost asserts an id outside the round pool
// is refused and never counted as a wrong guess .
func TestUnknownCharacterRejectedWithoutCost(t *testing.T) {
	subjects := []modes.Subject{{ID: "p1", Kind: modes.SubjectPlayer}}
	prep, _, pub := prepareRound(t, defaultPack(), "unknown-seed", `{}`, subjects...)

	dec, st := apply(t, prep, subjects[0], nil, 0, guessAction("does_not_exist"))
	if dec.Accepted {
		t.Fatal("unknown character was accepted")
	}
	if dec.Code != CodeUnknownCharacter {
		t.Fatalf("code = %q, want %q", dec.Code, CodeUnknownCharacter)
	}
	if st.Attempts != 0 || st.RemainingWrong != 0 {
		t.Fatalf("rejected unknown guess mutated state: %+v", st)
	}
	// The configured budget must survive a rejection, so the next real guess
	// still starts from the documented budget.
	dec2, st2 := apply(t, prep, subjects[0], nil, 0, guessAction(pub.Pool[0].ID))
	if !dec2.Accepted {
		t.Fatalf("valid guess after a rejection was refused: %s", dec2.Code)
	}
	if st2.Attempts != 1 {
		t.Fatalf("attempts = %d, want 1", st2.Attempts)
	}
}

// TestBudgetExhaustionLoses asserts the round is lost exactly when the budget
// reaches zero, including the last-guess boundary .
func TestBudgetExhaustionLoses(t *testing.T) {
	subjects := []modes.Subject{{ID: "p1", Kind: modes.SubjectPlayer}}
	// A four-guess budget keeps the table short while exercising the boundary.
	prep, priv, pub := prepareRound(t, defaultPack(), "exhaust-seed", `{"wrongBudget":4}`, subjects...)

	wrongIDs := make([]string, 0, len(priv.Pool))
	for _, c := range priv.Pool {
		if c.ID != priv.AnswerID {
			wrongIDs = append(wrongIDs, c.ID)
		}
	}
	if len(wrongIDs) < pub.Config.WrongBudget {
		t.Fatalf("need %d wrong candidates, have %d", pub.Config.WrongBudget, len(wrongIDs))
	}

	var st state
	for i := 0; i < pub.Config.WrongBudget; i++ {
		dec, next := apply(t, prep, subjects[0], decStateOrNil(t, st), st.Attempts, guessAction(wrongIDs[i]))
		if !dec.Accepted {
			t.Fatalf("guess %d rejected: %s", i+1, dec.Code)
		}
		st = next
		wantRemaining := pub.Config.WrongBudget - (i + 1)
		if st.RemainingWrong != wantRemaining {
			t.Fatalf("after guess %d remainingWrong = %d, want %d", i+1, st.RemainingWrong, wantRemaining)
		}
		wantFinished := wantRemaining == 0
		if st.Failed != wantFinished {
			t.Fatalf("after guess %d failed = %v, want %v", i+1, st.Failed, wantFinished)
		}
		if dec.Finished != wantFinished {
			t.Fatalf("after guess %d Finished = %v, want %v", i+1, dec.Finished, wantFinished)
		}
	}

	// A further guess must be refused as finished, not silently accepted.
	dec, _ := apply(t, prep, subjects[0], decState(t, st), st.Attempts, guessAction(wrongIDs[0]))
	if dec.Accepted || dec.Code != CodeRoundFinished {
		t.Fatalf("post-exhaustion guess: accepted=%v code=%q", dec.Accepted, dec.Code)
	}
}

// decStateOrNil encodes a state only when it is non-zero.
func decStateOrNil(t *testing.T, st state) json.RawMessage {
	t.Helper()
	if st.Attempts == 0 {
		return nil
	}
	return modeutil.Marshal(st)
}

// TestApplyIsDeterministic asserts two identical action sequences produce
// byte-identical state and feedback .
func TestApplyIsDeterministic(t *testing.T) {
	subjects := []modes.Subject{{ID: "p1", Kind: modes.SubjectPlayer}}
	run := func() (string, string) {
		prep, priv, _ := prepareRound(t, defaultPack(), "apply-seed", `{}`, subjects...)
		var st state
		var lastFeedback string
		for _, c := range priv.Pool {
			if c.ID == priv.AnswerID {
				continue
			}
			dec, next := apply(t, prep, subjects[0], decStateOrNil(t, st), st.Attempts, guessAction(c.ID))
			if !dec.Accepted {
				t.Fatalf("guess rejected: %s", dec.Code)
			}
			st = next
			lastFeedback = string(dec.Feedback)
			if st.Failed {
				break
			}
		}
		return string(modeutil.Marshal(st)), lastFeedback
	}
	stateA, feedbackA := run()
	stateB, feedbackB := run()
	if stateA != stateB {
		t.Fatalf("state differs between identical runs:\n%s\n%s", stateA, stateB)
	}
	if feedbackA != feedbackB {
		t.Fatalf("feedback differs between identical runs")
	}
}

// TestProjectDoesNotLeakAnswer asserts the answer never reaches an unauthorised
// viewer, and that the opponent shadow hides the guessed character (,
// ).
func TestProjectDoesNotLeakAnswer(t *testing.T) {
	subjects := []modes.Subject{
		{ID: "p1", Kind: modes.SubjectPlayer},
		{ID: "p2", Kind: modes.SubjectPlayer},
	}
	prep, priv, _ := prepareRound(t, defaultPack(), "proj-seed", `{"variant":"friend"}`, subjects...)

	var wrongID string
	for _, c := range priv.Pool {
		if c.ID != priv.AnswerID {
			wrongID = c.ID
			break
		}
	}
	_, st := apply(t, prep, subjects[0], nil, 0, guessAction(wrongID))
	states := map[string]json.RawMessage{"p1": modeutil.Marshal(st)}

	// p2 is the opponent: they must not learn p1's guessed character, and
	// nobody but a referee learns the answer.
	out, err := New().Project(modes.ProjectInput{
		Scope:   scope("proj-seed"),
		Viewer:  modes.Viewer{MembershipID: "m2", Role: "player", Subject: subjects[1]},
		Public:  prep.Public,
		Private: prep.Private,
		States:  states,
		Phase:   modes.PhaseCollecting,
	})
	if err != nil {
		t.Fatalf("Project: %v", err)
	}
	if strings.Contains(string(out), priv.AnswerID) {
		t.Fatalf("projection leaked the answer id %q", priv.AnswerID)
	}
	if strings.Contains(string(out), wrongID) {
		t.Fatalf("opponent shadow leaked the guessed character id %q", wrongID)
	}
	if strings.Contains(string(out), `"answer"`) {
		t.Fatal("projection included an answer field for an unauthorised viewer")
	}

	// The referee does get the answer.
	refOut, err := New().Project(modes.ProjectInput{
		Scope:   scope("proj-seed"),
		Viewer:  modes.Viewer{MembershipID: "host", Role: "host", IsReferee: true},
		Public:  prep.Public,
		Private: prep.Private,
		States:  states,
		Phase:   modes.PhaseCollecting,
	})
	if err != nil {
		t.Fatalf("referee Project: %v", err)
	}
	if !strings.Contains(string(refOut), priv.AnswerID) {
		t.Fatal("referee projection did not include the answer")
	}

	// The answer is revealed to everyone once the round is terminal and the
	// caller asks for it.
	revealOut, err := New().Project(modes.ProjectInput{
		Scope:     scope("proj-seed"),
		Viewer:    modes.Viewer{MembershipID: "m2", Role: "player", Subject: subjects[1]},
		Public:    prep.Public,
		Private:   prep.Private,
		States:    states,
		Phase:     modes.PhaseSettled,
		RevealNow: true,
	})
	if err != nil {
		t.Fatalf("reveal Project: %v", err)
	}
	if !strings.Contains(string(revealOut), priv.AnswerID) {
		t.Fatal("terminal reveal did not include the answer")
	}
}

// TestProjectOwnRowsVisible asserts a subject always sees their own full
// feedback rows .
func TestProjectOwnRowsVisible(t *testing.T) {
	subjects := []modes.Subject{{ID: "p1", Kind: modes.SubjectPlayer}}
	prep, priv, _ := prepareRound(t, defaultPack(), "own-seed", `{}`, subjects...)
	var wrongID string
	for _, c := range priv.Pool {
		if c.ID != priv.AnswerID {
			wrongID = c.ID
			break
		}
	}
	_, st := apply(t, prep, subjects[0], nil, 0, guessAction(wrongID))

	out, err := New().Project(modes.ProjectInput{
		Scope:   scope("own-seed"),
		Viewer:  modes.Viewer{MembershipID: "m1", Role: "player", Subject: subjects[0]},
		Public:  prep.Public,
		Private: prep.Private,
		States:  map[string]json.RawMessage{"p1": modeutil.Marshal(st)},
		Phase:   modes.PhaseCollecting,
	})
	if err != nil {
		t.Fatalf("Project: %v", err)
	}
	var view struct {
		Self *struct {
			Rows []GuessRow `json:"rows"`
		} `json:"self"`
	}
	if err := json.Unmarshal(out, &view); err != nil {
		t.Fatalf("decode projection: %v", err)
	}
	if view.Self == nil || len(view.Self.Rows) != 1 {
		t.Fatalf("own rows missing from the projection")
	}
	if view.Self.Rows[0].CharacterID != wrongID {
		t.Fatalf("own row character = %q, want %q", view.Self.Rows[0].CharacterID, wrongID)
	}
}

// TestSettleRanksAndTies asserts scoring, ordering and shared ranks: ties share
// a rank and are never broken by arrival time .
func TestSettleRanksAndTies(t *testing.T) {
	subjects := []modes.Subject{
		{ID: "p1", Kind: modes.SubjectPlayer},
		{ID: "p2", Kind: modes.SubjectPlayer},
		{ID: "p3", Kind: modes.SubjectPlayer},
	}
	prep, _, _ := prepareRound(t, defaultPack(), "settle-seed", `{}`, subjects...)

	// p1 solves on attempt 1 (100), p2 solves on attempt 3 (84), p3 never
	// solves (0). p1 and p2 differ so the ranking is strict.
	states := map[string]json.RawMessage{
		"p1": modeutil.Marshal(state{Attempts: 1, Solved: true, SolvedAtAttempt: 1, RemainingWrong: 8}),
		"p2": modeutil.Marshal(state{Attempts: 3, Solved: true, SolvedAtAttempt: 3, RemainingWrong: 6}),
		"p3": modeutil.Marshal(state{Attempts: 8, Failed: true, RemainingWrong: 0}),
	}
	scores, err := New().Settle(modes.SettleInput{
		Scope:   scope("settle-seed"),
		Phase:   modes.PhaseSettled,
		Public:  prep.Public,
		Private: prep.Private,
		States:  states,
	})
	if err != nil {
		t.Fatalf("Settle: %v", err)
	}
	if len(scores) != 3 {
		t.Fatalf("scores = %d, want 3", len(scores))
	}
	want := map[string]int{"p1": 10000, "p2": 8400, "p3": 0}
	for _, s := range scores {
		if s.ScoreUnits != want[s.Subject.ID] {
			t.Fatalf("%s scoreUnits = %d, want %d", s.Subject.ID, s.ScoreUnits, want[s.Subject.ID])
		}
		if err := s.Validate(); err != nil {
			t.Fatalf("%s score invalid: %v", s.Subject.ID, err)
		}
	}
	// RankScores sorts by score descending, then subject id.
	if scores[0].Subject.ID != "p1" || scores[0].Rank != 1 {
		t.Fatalf("first = %s rank %d, want p1 rank 1", scores[0].Subject.ID, scores[0].Rank)
	}
	if scores[1].Subject.ID != "p2" || scores[1].Rank != 2 {
		t.Fatalf("second = %s rank %d, want p2 rank 2", scores[1].Subject.ID, scores[1].Rank)
	}
	if scores[2].Subject.ID != "p3" || scores[2].Rank != 3 {
		t.Fatalf("third = %s rank %d, want p3 rank 3", scores[2].Subject.ID, scores[2].Rank)
	}

	// Two solvers on the same attempt share rank 1 and skip ordinal 2.
	tied := map[string]json.RawMessage{
		"p1": modeutil.Marshal(state{Attempts: 2, Solved: true, SolvedAtAttempt: 2, RemainingWrong: 7}),
		"p2": modeutil.Marshal(state{Attempts: 2, Solved: true, SolvedAtAttempt: 2, RemainingWrong: 7}),
		"p3": modeutil.Marshal(state{Attempts: 8, Failed: true}),
	}
	tiedScores, err := New().Settle(modes.SettleInput{
		Scope:   scope("settle-seed"),
		Phase:   modes.PhaseSettled,
		Public:  prep.Public,
		Private: prep.Private,
		States:  tied,
	})
	if err != nil {
		t.Fatalf("Settle tied: %v", err)
	}
	if tiedScores[0].Rank != 1 || tiedScores[1].Rank != 1 {
		t.Fatalf("tied ranks = %d,%d, want 1,1", tiedScores[0].Rank, tiedScores[1].Rank)
	}
	if !tiedScores[0].SharedRank || !tiedScores[1].SharedRank {
		t.Fatal("tied scores must both report SharedRank")
	}
	if tiedScores[2].Rank != 3 {
		t.Fatalf("third rank = %d, want 3 (shared rank skips the ordinal)", tiedScores[2].Rank)
	}
}

// TestSettleIgnoresStateMapOrder asserts the settlement result does not depend
// on map iteration order: states are keyed by the roster order captured at
// prepare time .
func TestSettleIgnoresStateMapOrder(t *testing.T) {
	subjects := []modes.Subject{
		{ID: "p1", Kind: modes.SubjectPlayer},
		{ID: "p2", Kind: modes.SubjectPlayer},
		{ID: "p3", Kind: modes.SubjectPlayer},
	}
	prep, _, _ := prepareRound(t, defaultPack(), "order-seed", `{}`, subjects...)
	states := map[string]json.RawMessage{
		"p3": modeutil.Marshal(state{Attempts: 1, Solved: true, SolvedAtAttempt: 1}),
		"p1": modeutil.Marshal(state{Attempts: 5, Solved: true, SolvedAtAttempt: 5}),
		"p2": modeutil.Marshal(state{Attempts: 2, Solved: true, SolvedAtAttempt: 2}),
	}
	run := func() string {
		scores, err := New().Settle(modes.SettleInput{
			Scope:   scope("order-seed"),
			Phase:   modes.PhaseSettled,
			Public:  prep.Public,
			Private: prep.Private,
			States:  states,
		})
		if err != nil {
			t.Fatalf("Settle: %v", err)
		}
		return string(modeutil.Marshal(scores))
	}
	first, second := run(), run()
	if first != second {
		t.Fatalf("settle output depends on iteration order")
	}
}

// TestPrepareRejectsAmbiguousVectorsInCompetitiveRounds asserts a pack whose
// characters share identical nine fields is not admitted to a competitive round
// unless it is explicitly a practice round .
func TestPrepareRejectsAmbiguousVectorsInCompetitiveRounds(t *testing.T) {
	ambiguous := func(group string) fakeContent {
		return buildPack(
			packEntry{
				ID: "twin_a", NameZh: "甲", Star: intPtr(3),
				SurfaceGroup: []string{"草地"}, DistanceGroup: []string{"中"},
				RunningStyleGroup: []string{"先"}, SexType: "牡",
				G1Bracket: "3-4", G23Bracket: "4-6",
				SchoolGrade: "中等部", Dormitory: "栗东宿舍",
				AmbiguityGroup: group,
			},
			packEntry{
				ID: "twin_b", NameZh: "乙", Star: intPtr(3),
				SurfaceGroup: []string{"草地"}, DistanceGroup: []string{"中"},
				RunningStyleGroup: []string{"先"}, SexType: "牡",
				G1Bracket: "3-4", G23Bracket: "4-6",
				SchoolGrade: "中等部", Dormitory: "栗东宿舍",
				AmbiguityGroup: group,
			},
			packEntry{
				ID: "distinct", NameZh: "丙", Star: intPtr(1),
				SurfaceGroup: []string{"泥地"}, DistanceGroup: []string{"短"},
				RunningStyleGroup: []string{"追"}, SexType: "牝",
				G1Bracket: "0", G23Bracket: "0",
				SchoolGrade: "高等部", Dormitory: "美浦宿舍",
			},
		)
	}
	subjects := []modes.Subject{{ID: "p1", Kind: modes.SubjectPlayer}}

	// Competitive: only the distinct character survives, so the pool is too
	// small and Prepare must fail rather than silently accept ambiguity.
	if _, err := New().Prepare(context.Background(), modes.PrepareInput{
		Scope:   scope("amb-seed"),
		Subject: subjects,
		Config:  json.RawMessage(`{}`),
		Content: ambiguous("twin"),
	}); err == nil {
		t.Fatal("competitive round accepted an ambiguous pack")
	}

	// Practice: ambiguity is allowed and both twins are guessable.
	prep, err := New().Prepare(context.Background(), modes.PrepareInput{
		Scope:   scope("amb-seed"),
		Subject: subjects,
		Config:  json.RawMessage(`{"practice":true}`),
		Content: ambiguous("twin"),
	})
	if err != nil {
		t.Fatalf("practice round rejected the pack: %v", err)
	}
	var priv privatePrepared
	if err := json.Unmarshal(prep.Private, &priv); err != nil {
		t.Fatalf("decode private: %v", err)
	}
	if len(priv.Pool) != 3 {
		t.Fatalf("practice pool = %d, want 3", len(priv.Pool))
	}
}

// TestPrepareRequiresTwoSubjectsForFriend asserts the friend variant's roster
// requirement .
func TestPrepareRequiresTwoSubjectsForFriend(t *testing.T) {
	_, err := New().Prepare(context.Background(), modes.PrepareInput{
		Scope:   scope("friend-seed"),
		Subject: []modes.Subject{{ID: "p1", Kind: modes.SubjectPlayer}},
		Config:  json.RawMessage(`{"variant":"friend"}`),
		Content: defaultPack(),
	})
	if err == nil {
		t.Fatal("friend variant accepted a single subject")
	}
}

// TestPrepareRejectsEmptyPool asserts an unusable pack fails at prepare time
// rather than after the round starts .
func TestPrepareRejectsEmptyPool(t *testing.T) {
	cases := []struct {
		name string
		pack fakeContent
	}{
		{name: "no section", pack: fakeContent{}},
		{name: "empty array", pack: fakeContent{"classic.characters": json.RawMessage(`[]`)}},
		{name: "single character", pack: buildPack(packEntry{
			ID: "solo", NameZh: "独", Star: intPtr(3),
			SurfaceGroup: []string{"草地"}, DistanceGroup: []string{"中"},
			RunningStyleGroup: []string{"先"}, SexType: "牡",
			G1Bracket: "3-4", G23Bracket: "4-6",
			SchoolGrade: "中等部", Dormitory: "栗东宿舍",
		})},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := New().Prepare(context.Background(), modes.PrepareInput{
				Scope:   scope("empty-seed"),
				Subject: []modes.Subject{{ID: "p1", Kind: modes.SubjectPlayer}},
				Config:  json.RawMessage(`{}`),
				Content: tc.pack,
			})
			if err == nil {
				t.Fatal("expected prepare to fail on an unusable pool")
			}
		})
	}
}

// TestPrepareAcceptsSnakeCaseAndCamelCase asserts the loader reads both field
// spellings, so the compiled pack and the legacy question bank both work.
func TestPrepareAcceptsSnakeCaseAndCamelCase(t *testing.T) {
	camel := fakeContent{"classic.characters": json.RawMessage(`[
		{"id":"a","nameZh":"甲","star":3,"surfaceGroup":["草地"],"distanceGroup":["中"],
		 "runningStyleGroup":["先"],"sexType":"牡","g1Bracket":"3-4","g23Bracket":"4-6",
		 "schoolGrade":"中等部","dormitory":"栗东宿舍"},
		{"id":"b","nameZh":"乙","star":1,"surfaceGroup":["泥地"],"distanceGroup":["短"],
		 "runningStyleGroup":["追"],"sexType":"牝","g1Bracket":"0","g23Bracket":"0",
		 "schoolGrade":"高等部","dormitory":"美浦宿舍"}
	]`)}
	prep, err := New().Prepare(context.Background(), modes.PrepareInput{
		Scope:   scope("camel-seed"),
		Subject: []modes.Subject{{ID: "p1", Kind: modes.SubjectPlayer}},
		Config:  json.RawMessage(`{}`),
		Content: camel,
	})
	if err != nil {
		t.Fatalf("camelCase pack rejected: %v", err)
	}
	var priv privatePrepared
	if err := json.Unmarshal(prep.Private, &priv); err != nil {
		t.Fatalf("decode private: %v", err)
	}
	if len(priv.Pool) != 2 {
		t.Fatalf("pool = %d, want 2", len(priv.Pool))
	}
	if priv.Pool[0].Name == "" {
		t.Fatal("camelCase nameZh was not read")
	}
}

// TestUnknownStarNeverComparesGreen asserts an unknown star is not treated as a
// value that could match .
func TestUnknownStarNeverComparesGreen(t *testing.T) {
	answer := baseEntry()
	guess := baseEntry()
	guess.StarKnown = false
	row := BuildGuessRow(guess, answer)
	status, ok := row.StatusOf(FieldStar)
	if !ok {
		t.Fatal("star cell missing")
	}
	if status != StatusWrong {
		t.Fatalf("unknown star status = %q, want wrong", status)
	}
	if got := row.Cells.Star.Value; got != "未知" {
		t.Fatalf("unknown star value = %q, want 未知", got)
	}
}
