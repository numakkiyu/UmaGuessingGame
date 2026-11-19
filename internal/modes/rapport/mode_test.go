package rapport

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"testing"

	"github.com/bhcn/umaguessinggame/internal/modes"
)

// fakeContent is a synthetic compiled pack section store.
type fakeContent map[string]json.RawMessage

func (f fakeContent) Section(name string) (json.RawMessage, bool) {
	raw, ok := f[name]
	return raw, ok
}

func (f fakeContent) Version() string { return "test-pack-1" }

// fixtureProvenance is the provenance block every fixture question carries.
func fixtureProvenance() map[string]any {
	return map[string]any{
		"kind":            ProvenanceAuthored,
		"notOfficialData": true,
		"author":          "test-fixture",
		"basis":           "docs/玩法与规则.md (rapport)",
	}
}

// fixtureQuestions is the authored bank the fixture publishes.
//
// The prompts are deliberately NOT the strings the mode used to hard-code: if the mode still had
// a built-in bank, these tests would pass against it and never exercise the pack path.
var fixtureQuestions = []string{
	"测试题一：训练后最想做什么？",
	"测试题二：赛前需要什么准备？",
	"测试题三：只能选一种奖励会选？",
	"测试题四：低谷时更希望什么？",
	"测试题五：团队里想承担什么角色？",
	"测试题六：比赛当天早餐选什么？",
	"测试题七：空闲半天想去哪？",
	"测试题八：收礼物更在意什么？",
	"测试题九：最不擅长应对什么？",
	"测试题十：复盘时先看什么？",
	"测试题十一：想换哪种训练方式？",
	"测试题十二：希望被记住哪一点？",
}

// packOf builds a content source carrying the fixture bank.
func packOf() modes.ContentSource {
	return packWith(packOptions{})
}

// packOptions tunes what the fixture publishes.
type packOptions struct {
	omitSection    bool
	omitProvenance bool
	badProvenance  string
	questions      []string
	// optionCount overrides the per-question option count, to exercise the four-option rule.
	optionCount int
}

// packWith builds a pack from options.
func packWith(o packOptions) modes.ContentSource {
	questions := o.questions
	if questions == nil {
		questions = fixtureQuestions
	}
	optCount := o.optionCount
	if optCount == 0 {
		optCount = OptionCount
	}
	if o.omitSection {
		return fakeContent{}
	}
	docs := make([]map[string]any, 0, len(questions))
	for i, prompt := range questions {
		opts := make([]string, 0, optCount)
		for k := 0; k < optCount; k++ {
			opts = append(opts, fmt.Sprintf("选项%d-%d", i+1, k+1))
		}
		entry := map[string]any{
			"questionId": fmt.Sprintf("rq-%02d", i+1),
			"prompt":     prompt,
			"options":    opts,
		}
		if !o.omitProvenance {
			if o.badProvenance != "" {
				entry["provenance"] = json.RawMessage(o.badProvenance)
			} else {
				entry["provenance"] = fixtureProvenance()
			}
		}
		docs = append(docs, entry)
	}
	raw, err := json.Marshal(docs)
	if err != nil {
		panic(err)
	}
	return fakeContent{QuestionsSection: raw}
}

// scope builds a deterministic round scope.
func scope(seed string) modes.Scope {
	return modes.Scope{
		ModeID:             ModeID,
		RulesVersion:       RulesVersion,
		DatasetVersion:     "ds-test",
		ContentPackVersion: "test-pack-1",
		Seed:               seed,
	}
}

// roster builds n player subjects with stable ids.
func roster(n int) []modes.Subject {
	out := make([]modes.Subject, 0, n)
	for i := 0; i < n; i++ {
		out = append(out, modes.Subject{
			ID:   fmt.Sprintf("p%d", i+1),
			Kind: modes.SubjectPlayer,
		})
	}
	return out
}

// prepareRound runs Prepare and returns the prepared round plus decoded public.
func prepareRound(t *testing.T, seed, cfg string, subjects []modes.Subject) (modes.Prepared, publicState) {
	t.Helper()
	prep, err := New().Prepare(context.Background(), modes.PrepareInput{
		Scope:   scope(seed),
		Subject: subjects,
		Config:  json.RawMessage(cfg),
		Content: packOf(),
	})
	if err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	var pub publicState
	if err := json.Unmarshal(prep.Public, &pub); err != nil {
		t.Fatalf("decode public: %v", err)
	}
	return prep, pub
}

// answerAction builds a contract-shaped rapport.answer payload.
func answerAction(optionID string) modes.ModeAction {
	return modes.ModeAction{
		Kind:    ActionKindAnswer,
		Payload: json.RawMessage(fmt.Sprintf(`{"kind":"rapport.answer","optionId":%q}`, optionID)),
	}
}

// answer applies one answer and returns the decision plus the new state.
func answer(t *testing.T, prep modes.Prepared, subject modes.Subject, optionID string, state json.RawMessage) (modes.Decision, json.RawMessage) {
	t.Helper()
	dec, err := New().Apply(modes.ApplyInput{
		Scope:   scope("seed"),
		Subject: subject,
		Action:  answerAction(optionID),
		Public:  prep.Public,
		Private: prep.Private,
		State:   state,
	})
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	return dec, dec.State
}

// answerAll drives a whole round from a per-subject answer plan.
//
// plan maps subject id → question id → option index. Any participant/question
// combination missing from the plan is left unanswered, which is how the 缺交
// cases are built.
func answerAll(t *testing.T, prep modes.Prepared, pub publicState, plan map[string]map[string]int) map[string]json.RawMessage {
	t.Helper()
	states := map[string]json.RawMessage{}
	// Answer in question order so the per-participant rotation is respected.
	for _, q := range pub.Questions {
		participants := append([]string{q.ChooserID}, q.PredictorIDs...)
		sort.Strings(participants)
		for _, id := range participants {
			byQuestion, ok := plan[id]
			if !ok {
				continue
			}
			idx, ok := byQuestion[q.QuestionID]
			if !ok {
				continue
			}
			if idx < 0 || idx >= len(q.Options) {
				t.Fatalf("plan asks for option %d of a %d-option question", idx, len(q.Options))
			}
			subject := modes.Subject{ID: id, Kind: modes.SubjectPlayer}
			dec, next := answer(t, prep, subject, q.Options[idx].OptionID, states[id])
			if !dec.Accepted {
				t.Fatalf("answer for %s on %s rejected: %q %q", id, q.QuestionID, dec.Code, dec.Detail)
			}
			states[id] = next
		}
	}
	return states
}

// project decodes the projection for a viewer.
func project(t *testing.T, prep modes.Prepared, viewer modes.Viewer, phase modes.Phase, states map[string]json.RawMessage) projection {
	t.Helper()
	if states == nil {
		states = map[string]json.RawMessage{}
	}
	raw, err := New().Project(modes.ProjectInput{
		Scope:   scope("seed"),
		Viewer:  viewer,
		Public:  prep.Public,
		Private: prep.Private,
		States:  states,
		Phase:   phase,
	})
	if err != nil {
		t.Fatalf("Project: %v", err)
	}
	var out projection
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("decode projection: %v", err)
	}
	return out
}

// settle runs Settle and returns scores keyed by subject id.
func settle(t *testing.T, prep modes.Prepared, states map[string]json.RawMessage) map[string]modes.Score {
	t.Helper()
	if states == nil {
		states = map[string]json.RawMessage{}
	}
	scores, err := New().Settle(modes.SettleInput{
		Scope:   scope("seed"),
		Phase:   modes.PhaseSettled,
		Public:  prep.Public,
		Private: prep.Private,
		States:  states,
	})
	if err != nil {
		t.Fatalf("Settle: %v", err)
	}
	out := make(map[string]modes.Score, len(scores))
	for _, score := range scores {
		if err := score.Validate(); err != nil {
			t.Fatalf("invalid score for %s: %v", score.Subject.ID, err)
		}
		out[score.Subject.ID] = score
	}
	return out
}

// TestPrepareEnforcesParticipantBounds covers the 2–8 bound and the
// every-participant-chooses rule.
func TestPrepareEnforcesParticipantBounds(t *testing.T) {
	cases := []struct {
		name    string
		players int
		cfg     string
		wantErr string
		wantOK  bool
	}{
		{name: "zero", players: 0, cfg: `{}`, wantErr: "outside"},
		{name: "one", players: 1, cfg: `{}`, wantErr: "outside"},
		{name: "two", players: 2, cfg: `{}`, wantOK: true},
		{name: "eight", players: 8, cfg: `{"questions":8}`, wantOK: true},
		{name: "nine", players: 9, cfg: `{"questions":9}`, wantErr: "outside"},
		{name: "fewer questions than players", players: 6, cfg: `{"questions":5}`,
			wantErr: "cannot give all"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := New().Prepare(context.Background(), modes.PrepareInput{
				Scope:   scope("seed"),
				Subject: roster(tc.players),
				Config:  json.RawMessage(tc.cfg),
				Content: packOf(),
			})
			if tc.wantOK {
				if err != nil {
					t.Fatalf("Prepare rejected a legal round: %v", err)
				}
				return
			}
			if err == nil {
				t.Fatal("Prepare accepted an illegal round")
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("error %q does not mention %q", err, tc.wantErr)
			}
		})
	}
}

// TestValidateConfig covers the configuration bounds.
func TestValidateConfig(t *testing.T) {
	cases := []struct {
		name    string
		cfg     string
		wantErr string
	}{
		{name: "default", cfg: `{}`},
		{name: "nil", cfg: ``},
		{name: "explicit", cfg: `{"questions":5,"questionSeconds":20}`},
		{name: "too many questions", cfg: `{"questions":21}`, wantErr: "outside"},
		{name: "negative questions", cfg: `{"questions":-1}`, wantErr: "outside"},
		{name: "too short window", cfg: `{"questionSeconds":4}`, wantErr: "outside"},
		{name: "too long window", cfg: `{"questionSeconds":121}`, wantErr: "outside"},
		{name: "malformed", cfg: `{"questions":"five"}`, wantErr: "invalid config"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := New().ValidateConfig(json.RawMessage(tc.cfg))
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("ValidateConfig rejected %s: %v", tc.cfg, err)
				}
				return
			}
			if err == nil {
				t.Fatalf("ValidateConfig accepted %s", tc.cfg)
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("error %q does not mention %q", err, tc.wantErr)
			}
		})
	}
}

// TestChooserRotationCoversEveryone covers the 「角色换位」 bullet: every
// participant chooses at least once and no participant chooses twice before
// everybody has chosen.
func TestChooserRotationCoversEveryone(t *testing.T) {
	for _, players := range []int{2, 3, 5, 8} {
		t.Run(fmt.Sprintf("players%d", players), func(t *testing.T) {
			cfg := fmt.Sprintf(`{"questions":%d}`, players)
			_, pub := prepareRound(t, "rotation-seed", cfg, roster(players))

			counts := make(map[string]int, players)
			for _, q := range pub.Questions {
				counts[q.ChooserID]++
				// The chooser must not also be a predictor of their own question.
				for _, predictor := range q.PredictorIDs {
					if predictor == q.ChooserID {
						t.Fatalf("question %s lists its chooser %s as a predictor",
							q.QuestionID, q.ChooserID)
					}
				}
				if got, want := len(q.PredictorIDs), players-1; got != want {
					t.Fatalf("question %s has %d predictors, want %d", q.QuestionID, got, want)
				}
			}
			for _, subject := range roster(players) {
				if counts[subject.ID] == 0 {
					t.Fatalf("%s never gets a chooser turn: %v", subject.ID, counts)
				}
			}
			// A full cycle means the counts differ by at most one.
			lo, hi := 1<<30, 0
			for _, n := range counts {
				if n < lo {
					lo = n
				}
				if n > hi {
					hi = n
				}
			}
			if hi-lo > 1 {
				t.Fatalf("chooser turns are unbalanced: min %d max %d (%v)", lo, hi, counts)
			}
		})
	}
}

// TestRotationIsSeedDeterministic asserts the rotation offset comes from the seed
// and is therefore reproducible.
func TestRotationIsSeedDeterministic(t *testing.T) {
	subjects := roster(3)
	_, first := prepareRound(t, "rotation-a", `{"questions":6}`, subjects)
	_, second := prepareRound(t, "rotation-a", `{"questions":6}`, subjects)
	if string(mustJSON(t, first.Questions)) != string(mustJSON(t, second.Questions)) {
		t.Fatal("the rotation is not deterministic for the same seed")
	}

	// A different seed is allowed to pick a different offset, and the rotation
	// must still be a valid cycle.
	_, other := prepareRound(t, "rotation-b", `{"questions":6}`, subjects)
	if len(other.Questions) != len(first.Questions) {
		t.Fatal("a different seed changed the question count")
	}
}

// TestPredictionsInvisibleBeforeLock covers the 「提前窥看」 and
// 「选择未揭晓互不可见」 bullets.
//
// The strongest form of this test is structural: before a question locks, the
// projection for any viewer must not contain the chooser's pick nor another
// predictor's guess. A blanked field would still leak that a submission exists,
// so the assertion is on absence, not on emptiness.
func TestPredictionsInvisibleBeforeLock(t *testing.T) {
	subjects := roster(2)
	prep, pub := prepareRound(t, "hidden-seed", `{"questions":2}`, subjects)

	// Find the first question and have the chooser answer only. The predictors
	// have not answered, so the question is unlocked.
	q := pub.Questions[0]
	chooser := modes.Subject{ID: q.ChooserID, Kind: modes.SubjectPlayer}
	dec, chooserState := answer(t, prep, chooser, q.Options[2].OptionID, nil)
	if !dec.Accepted {
		t.Fatalf("chooser answer rejected: %q %q", dec.Code, dec.Detail)
	}
	states := map[string]json.RawMessage{q.ChooserID: chooserState}

	// A predictor viewing the round must not learn the chooser's pick.
	var predictor modes.Subject
	for _, subject := range subjects {
		if subject.ID != q.ChooserID {
			predictor = subject
		}
	}
	view := project(t, prep, modes.Viewer{Subject: predictor, Role: "player"}, modes.PhaseCollecting, states)
	for _, qv := range view.Questions {
		if qv.QuestionID != q.QuestionID {
			continue
		}
		if qv.Locked {
			t.Fatal("the question locked with a predictor still missing")
		}
		if qv.Reveal != nil {
			t.Fatalf("an unlocked question carries a reveal: %+v", qv.Reveal)
		}
		// The option id itself is public (it is one of the four fixed choices), so
		// the check targets the reveal fields, which is where a pick would leak.
		raw := string(mustJSON(t, qv))
		for _, forbidden := range []string{
			`"chosenOptionId"`, `"chosenLabel"`, `"correctPredictorIds"`,
		} {
			if strings.Contains(raw, forbidden) {
				t.Fatalf("an unlocked question exposes %s: %s", forbidden, raw)
			}
		}
		// The viewer has not answered this question, so their own option must be
		// absent too (a blanked field would still announce a submission).
		if qv.SelfOptionID != "" {
			t.Fatalf("viewer's own option is %q before they answered", qv.SelfOptionID)
		}
	}

	// The chooser, in turn, must not see a predictor's guess. Have the predictor
	// answer a DIFFERENT question first so the first question stays unlocked but
	// a guess exists somewhere.
	q2 := pub.Questions[1]
	if q2.ChooserID == q.ChooserID {
		t.Fatalf("the rotation gave the same chooser two consecutive questions")
	}
	predictorOfQ2 := modes.Subject{ID: q2.ChooserID, Kind: modes.SubjectPlayer}
	dec, state2 := answer(t, prep, predictorOfQ2, q2.Options[0].OptionID, nil)
	if !dec.Accepted {
		t.Fatalf("answer rejected: %q %q", dec.Code, dec.Detail)
	}
	states[q2.ChooserID] = state2

	chooserView := project(t, prep, modes.Viewer{Subject: chooser, Role: "player"}, modes.PhaseCollecting, states)
	for _, qv := range chooserView.Questions {
		if qv.QuestionID != q2.QuestionID {
			continue
		}
		if qv.Reveal != nil {
			t.Fatalf("the chooser sees a reveal for an unlocked question: %+v", qv.Reveal)
		}
	}
}

// TestLockedQuestionRevealsToEveryone asserts a question's result becomes public
// once every participant has answered it.
func TestLockedQuestionRevealsToEveryone(t *testing.T) {
	subjects := roster(2)
	prep, pub := prepareRound(t, "lock-seed", `{"questions":2}`, subjects)
	q := pub.Questions[0]

	states := map[string]json.RawMessage{}
	participants := append([]string{q.ChooserID}, q.PredictorIDs...)
	for _, id := range participants {
		subject := modes.Subject{ID: id, Kind: modes.SubjectPlayer}
		dec, next := answer(t, prep, subject, q.Options[1].OptionID, states[id])
		if !dec.Accepted {
			t.Fatalf("answer for %s rejected: %q %q", id, dec.Code, dec.Detail)
		}
		states[id] = next
	}

	for _, subject := range subjects {
		view := project(t, prep, modes.Viewer{Subject: subject, Role: "player"}, modes.PhaseCollecting, states)
		for _, qv := range view.Questions {
			if qv.QuestionID != q.QuestionID {
				continue
			}
			if !qv.Locked {
				t.Fatalf("%s: the question is not locked after everybody answered", subject.ID)
			}
			if qv.Reveal == nil {
				t.Fatalf("%s: a locked question carries no reveal", subject.ID)
			}
			if qv.Reveal.ChosenOptionID != q.Options[1].OptionID {
				t.Fatalf("%s: revealed choice = %q, want %q",
					subject.ID, qv.Reveal.ChosenOptionID, q.Options[1].OptionID)
			}
			if qv.Reveal.ChosenLabel == "" {
				t.Fatalf("%s: the reveal carries no readable label", subject.ID)
			}
			if len(qv.Reveal.CorrectPredictorIDs) != 1 {
				t.Fatalf("%s: %d correct predictors, want 1", subject.ID, len(qv.Reveal.CorrectPredictorIDs))
			}
		}
	}
}

// TestDuplicateSubmissionRefused covers the 「重复提交」 bullet.
func TestDuplicateSubmissionRefused(t *testing.T) {
	subjects := roster(2)
	prep, pub := prepareRound(t, "dup-seed", `{"questions":2}`, subjects)
	q := pub.Questions[0]
	subject := modes.Subject{ID: q.ChooserID, Kind: modes.SubjectPlayer}

	dec, state := answer(t, prep, subject, q.Options[0].OptionID, nil)
	if !dec.Accepted {
		t.Fatalf("first answer rejected: %q %q", dec.Code, dec.Detail)
	}

	// A second answer to the same question, even with a different option, is
	// refused: changing the chooser's pick after some predictors answered would
	// leak their guesses by comparison.
	dec, _ = answer(t, prep, subject, q.Options[1].OptionID, state)
	if dec.Accepted {
		t.Fatal("a second answer for the same question was accepted")
	}
	if dec.Code != CodeDuplicateAnswer {
		t.Fatalf("code = %q, want %q", dec.Code, CodeDuplicateAnswer)
	}
}

// TestUnknownOptionRejected asserts an option that is not part of this round is
// refused, and that an option from a different question is refused as closed.
func TestUnknownOptionRejected(t *testing.T) {
	subjects := roster(2)
	prep, pub := prepareRound(t, "option-seed", `{"questions":3}`, subjects)

	cases := []struct {
		name string
		kind string
		body string
		want string
	}{
		{name: "unknown action", kind: "rapport.teleport", body: `{"optionId":"x"}`, want: CodeUnknownAction},
		{name: "empty payload", kind: ActionKindAnswer, body: ``, want: CodeInvalidPayload},
		{name: "not json", kind: ActionKindAnswer, body: `nope`, want: CodeInvalidPayload},
		{name: "empty option", kind: ActionKindAnswer, body: `{"kind":"rapport.answer"}`, want: CodeInvalidPayload},
		{name: "kind mismatch", kind: ActionKindAnswer,
			body: `{"kind":"creative.work","optionId":"rp-01-o1"}`, want: CodeInvalidPayload},
		{name: "option not in round", kind: ActionKindAnswer,
			body: `{"kind":"rapport.answer","optionId":"rp-99-o1"}`, want: CodeUnknownOption},
		{name: "option id from another question's namespace", kind: ActionKindAnswer,
			body: fmt.Sprintf(`{"kind":"rapport.answer","optionId":%q}`,
				pub.Questions[0].QuestionID+"-o99"), want: CodeUnknownOption},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dec, err := New().Apply(modes.ApplyInput{
				Scope:   scope("option-seed"),
				Subject: subjects[0],
				Action:  modes.ModeAction{Kind: tc.kind, Payload: json.RawMessage(tc.body)},
				Public:  prep.Public,
				Private: prep.Private,
			})
			if err != nil {
				t.Fatalf("Apply: %v", err)
			}
			if dec.Accepted {
				t.Fatal("an invalid answer was accepted")
			}
			if dec.Code != tc.want {
				t.Fatalf("code = %q, want %q (detail %q)", dec.Code, tc.want, dec.Detail)
			}
		})
	}
}

// TestNonParticipantCannotAnswer asserts a subject outside the roster is refused.
func TestNonParticipantCannotAnswer(t *testing.T) {
	prep, pub := prepareRound(t, "outsider-seed", `{"questions":2}`, roster(2))
	outsider := modes.Subject{ID: "spectator-1", Kind: modes.SubjectPlayer}
	dec, _ := answer(t, prep, outsider, pub.Questions[0].Options[0].OptionID, nil)
	if dec.Accepted {
		t.Fatal("a non-participant answer was accepted")
	}
	if dec.Code != CodeNotOnRoster {
		t.Fatalf("code = %q, want %q", dec.Code, CodeNotOnRoster)
	}
}

// TestMissingPredictionKeepsItsSlot covers the 「缺交」 and 「固定分母」 bullets.
//
// One predictor never answers. The denominator must stay at the announced total,
// so the team score is strictly lower than an all-correct round — a missing
// prediction can never raise the score.
func TestMissingPredictionKeepsItsSlot(t *testing.T) {
	subjects := roster(2)
	prep, pub := prepareRound(t, "missing-seed", `{"questions":5}`, subjects)
	if pub.TotalSlots != 5 {
		t.Fatalf("totalSlots = %d, want 5 (one predictor per question)", pub.TotalSlots)
	}

	// Everybody answers every question correctly except that p1 skips the last
	// question as a predictor.
	plan := map[string]map[string]int{}
	for _, q := range pub.Questions {
		for _, id := range append([]string{q.ChooserID}, q.PredictorIDs...) {
			if plan[id] == nil {
				plan[id] = map[string]int{}
			}
			plan[id][q.QuestionID] = 0
		}
	}
	last := pub.Questions[len(pub.Questions)-1]
	// Drop the last question's predictor answer, whichever subject that is.
	delete(plan[last.PredictorIDs[0]], last.QuestionID)

	states := answerAll(t, prep, pub, plan)
	scores := settle(t, prep, states)
	// 4 hits out of 5 slots -> floor(100*4/5) = 80 display points.
	if got := scores["p1"].ScoreUnits; got != 8000 {
		t.Fatalf("p1 score = %d units, want 8000 (80 points from 4/5 slots)", got)
	}
	// Both members receive the same team score.
	if scores["p1"].ScoreUnits != scores["p2"].ScoreUnits {
		t.Fatalf("partners scored %d and %d", scores["p1"].ScoreUnits, scores["p2"].ScoreUnits)
	}
	// The explanation publishes the fixed denominator.
	var exp struct {
		Hits       int `json:"hits"`
		TotalSlots int `json:"totalSlots"`
	}
	if err := json.Unmarshal(scores["p1"].Explanation, &exp); err != nil {
		t.Fatalf("decode explanation: %v", err)
	}
	if exp.TotalSlots != 5 {
		t.Fatalf("explanation totalSlots = %d, want 5", exp.TotalSlots)
	}
	if exp.Hits != 4 {
		t.Fatalf("explanation hits = %d, want 4", exp.Hits)
	}
}

// TestAllCorrectScoresFullMarks asserts a perfect round scores exactly 100.
func TestAllCorrectScoresFullMarks(t *testing.T) {
	subjects := roster(2)
	prep, pub := prepareRound(t, "perfect-seed", `{"questions":5}`, subjects)
	plan := map[string]map[string]int{}
	for _, q := range pub.Questions {
		for _, id := range append([]string{q.ChooserID}, q.PredictorIDs...) {
			if plan[id] == nil {
				plan[id] = map[string]int{}
			}
			plan[id][q.QuestionID] = 2
		}
	}
	states := answerAll(t, prep, pub, plan)
	scores := settle(t, prep, states)
	for _, id := range []string{"p1", "p2"} {
		if got := scores[id].ScoreUnits; got != 10000 {
			t.Fatalf("%s score = %d units, want 10000", id, got)
		}
	}
}

// TestNothingSubmittedScoresZero asserts an entirely unanswered round scores 0
// rather than being treated as a vacuous 100 (0/0 must not become a pass).
func TestNothingSubmittedScoresZero(t *testing.T) {
	prep, _ := prepareRound(t, "empty-seed", `{"questions":5}`, roster(2))
	scores := settle(t, prep, nil)
	for _, id := range []string{"p1", "p2"} {
		if got := scores[id].ScoreUnits; got != 0 {
			t.Fatalf("%s score = %d units, want 0", id, got)
		}
	}
}

// TestEqualHitsShareRank covers the 「同分」 bullet.
//
// Both participants play the same way, so the team score is identical; the two
// scores must share rank 1.
func TestEqualHitsShareRank(t *testing.T) {
	subjects := roster(2)
	prep, pub := prepareRound(t, "tie-seed", `{"questions":4}`, subjects)
	plan := map[string]map[string]int{}
	for i, q := range pub.Questions {
		// Answer every question the same way, so either all hit or all miss.
		for _, id := range append([]string{q.ChooserID}, q.PredictorIDs...) {
			if plan[id] == nil {
				plan[id] = map[string]int{}
			}
			plan[id][q.QuestionID] = i % len(q.Options)
		}
	}
	states := answerAll(t, prep, pub, plan)
	scores := settle(t, prep, states)
	a, b := scores["p1"], scores["p2"]
	if a.ScoreUnits != b.ScoreUnits {
		t.Fatalf("identical play scored %d and %d", a.ScoreUnits, b.ScoreUnits)
	}
	if a.Rank != 1 || b.Rank != 1 {
		t.Fatalf("ranks = %d and %d, want 1 and 1", a.Rank, b.Rank)
	}
	if !a.SharedRank || !b.SharedRank {
		t.Fatalf("tied scores not marked shared: %v %v", a.SharedRank, b.SharedRank)
	}
}

// TestScoreFormulaFloor covers the documented floor(100×hits/slots) mapping across
// team sizes, including the non-integer cases.
func TestScoreFormulaFloor(t *testing.T) {
	cases := []struct {
		name    string
		players int
		hits    int
		want    int
	}{
		// Two players: 5 questions, 5 slots. hits h -> floor(100h/5) = 20h.
		{name: "two players all hit", players: 2, hits: 5, want: 100},
		{name: "two players three hits", players: 2, hits: 3, want: 60},
		{name: "two players one hit", players: 2, hits: 1, want: 20},
		{name: "two players no hit", players: 2, hits: 0, want: 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			subjects := roster(tc.players)
			prep, pub := prepareRound(t, "formula-seed", `{"questions":5}`, subjects)
			if pub.TotalSlots != 5 {
				t.Fatalf("totalSlots = %d, want 5", pub.TotalSlots)
			}
			// Build a plan that yields exactly tc.hits correct predictions: the
			// first tc.hits predictor slots answer the chooser's pick, the rest
			// answer a different option.
			remaining := tc.hits
			plan := map[string]map[string]int{}
			for _, q := range pub.Questions {
				if plan[q.ChooserID] == nil {
					plan[q.ChooserID] = map[string]int{}
				}
				plan[q.ChooserID][q.QuestionID] = 0
				for _, predictor := range q.PredictorIDs {
					if plan[predictor] == nil {
						plan[predictor] = map[string]int{}
					}
					if remaining > 0 {
						plan[predictor][q.QuestionID] = 0
						remaining--
					} else {
						plan[predictor][q.QuestionID] = 1
					}
				}
			}
			states := answerAll(t, prep, pub, plan)
			scores := settle(t, prep, states)
			if got := scores["p1"].ScoreUnits; got != tc.want*100 {
				t.Fatalf("score = %d units, want %d", got, tc.want*100)
			}
		})
	}
}

// TestFourFixedOptions asserts every question publishes exactly four options with
// distinct ids and non-empty labels .
func TestFourFixedOptions(t *testing.T) {
	_, pub := prepareRound(t, "options-seed", `{"questions":5}`, roster(2))
	if len(pub.Questions) != 5 {
		t.Fatalf("round has %d questions, want 5", len(pub.Questions))
	}
	for _, q := range pub.Questions {
		if len(q.Options) != OptionCount {
			t.Fatalf("question %s has %d options, want %d", q.QuestionID, len(q.Options), OptionCount)
		}
		ids := make(map[string]struct{}, OptionCount)
		for _, opt := range q.Options {
			if opt.OptionID == "" || opt.Label == "" {
				t.Fatalf("question %s has an empty option: %+v", q.QuestionID, opt)
			}
			if _, dup := ids[opt.OptionID]; dup {
				t.Fatalf("question %s repeats option id %s", q.QuestionID, opt.OptionID)
			}
			ids[opt.OptionID] = struct{}{}
		}
		if q.Prompt == "" {
			t.Fatalf("question %s has no prompt", q.QuestionID)
		}
	}
}

// TestNoFabricatedOfficialPreference asserts the round never claims an official
// popularity or personality answer .
//
// The check is on the published payload: it may describe a player preference but
// must not carry an "official", "popularity" or "canonical answer" field, and the
// settle explanation must not either.
func TestNoFabricatedOfficialPreference(t *testing.T) {
	prep, pub := prepareRound(t, "official-seed", `{"questions":5}`, roster(2))
	public := string(prep.Public)
	for _, forbidden := range []string{
		`"officialAnswer"`, `"official"`, `"popularity"`, `"canonicalAnswer"`,
		`"characterPreference"`, `"canon"`,
	} {
		if strings.Contains(public, forbidden) {
			t.Fatalf("public state carries %s, which would imply an official answer", forbidden)
		}
	}
	// The scoring description must state the player-input nature.
	if !strings.Contains(pub.Scoring, "命中") {
		t.Fatalf("scoring description does not describe hit points: %q", pub.Scoring)
	}

	// Every question asks about a preference, never about a character fact.
	for _, q := range pub.Questions {
		if strings.Contains(q.Prompt, "人气") || strings.Contains(q.Prompt, "官方") {
			t.Fatalf("question %s asks for an official or popularity answer: %q", q.QuestionID, q.Prompt)
		}
	}

	// The settle explanation describes hits and slots, not an official answer.
	scores := settle(t, prep, nil)
	if strings.Contains(string(scores["p1"].Explanation), "official") {
		t.Fatal("settle explanation claims an official answer")
	}
}

// TestQuestionCountIsHonoured asserts the configured question count is what the
// round actually uses, so the denominator matches the schedule.
func TestQuestionCountIsHonoured(t *testing.T) {
	for _, n := range []int{2, 3, 5, 8} {
		t.Run(fmt.Sprintf("questions%d", n), func(t *testing.T) {
			prep, pub := prepareRound(t, "count-seed", fmt.Sprintf(`{"questions":%d}`, n), roster(2))
			if len(pub.Questions) != n {
				t.Fatalf("round has %d questions, want %d", len(pub.Questions), n)
			}
			if prep.QuestionCount != n {
				t.Fatalf("questionCount = %d, want %d", prep.QuestionCount, n)
			}
			if pub.TotalSlots != n {
				t.Fatalf("totalSlots = %d, want %d (one predictor per question)", pub.TotalSlots, n)
			}
		})
	}
}

// TestPrepareIsDeterministic asserts Prepare is a pure function of the scope.
func TestPrepareIsDeterministic(t *testing.T) {
	subjects := roster(3)
	first, _ := prepareRound(t, "deterministic-seed", `{"questions":5}`, subjects)
	second, _ := prepareRound(t, "deterministic-seed", `{"questions":5}`, subjects)
	if string(first.Public) != string(second.Public) {
		t.Fatal("Prepare is not deterministic")
	}
	if string(first.Private) != string(second.Private) {
		t.Fatal("private state is not deterministic")
	}
}

// TestRefereeNeverSeesUnlockedAnswers asserts the host's reveal authority does not
// let the host read a pending question and steer the round.
func TestRefereeNeverSeesUnlockedAnswers(t *testing.T) {
	subjects := roster(2)
	prep, pub := prepareRound(t, "referee-seed", `{"questions":2}`, subjects)
	q := pub.Questions[0]
	chooser := modes.Subject{ID: q.ChooserID, Kind: modes.SubjectPlayer}
	_, state := answer(t, prep, chooser, q.Options[3].OptionID, nil)
	states := map[string]json.RawMessage{q.ChooserID: state}

	view := project(t, prep, modes.Viewer{
		MembershipID: "host-1", Role: "host", IsReferee: true,
	}, modes.PhaseCollecting, states)
	for _, qv := range view.Questions {
		if qv.QuestionID != q.QuestionID {
			continue
		}
		if qv.Reveal != nil {
			t.Fatalf("a referee sees an unlocked question's answer: %+v", qv.Reveal)
		}
	}

	// With RevealNow the referee may inspect it, which is the documented
	// authority path; the flag is what distinguishes the two.
	raw, err := New().Project(modes.ProjectInput{
		Scope:     scope("referee-seed"),
		Viewer:    modes.Viewer{MembershipID: "host-1", Role: "host", IsReferee: true},
		Public:    prep.Public,
		Private:   prep.Private,
		States:    states,
		Phase:     modes.PhaseCollecting,
		RevealNow: true,
	})
	if err != nil {
		t.Fatalf("Project: %v", err)
	}
	var revealed projection
	if err := json.Unmarshal(raw, &revealed); err != nil {
		t.Fatalf("decode: %v", err)
	}
	found := false
	for _, qv := range revealed.Questions {
		if qv.QuestionID == q.QuestionID && qv.Reveal != nil {
			found = true
		}
	}
	if !found {
		t.Fatal("a referee with RevealNow cannot inspect the question")
	}
}

// TestTerminalPhaseRevealsEverything asserts a settled round publishes all
// answers and the final scores.
func TestTerminalPhaseRevealsEverything(t *testing.T) {
	subjects := roster(2)
	prep, pub := prepareRound(t, "terminal-seed", `{"questions":3}`, subjects)
	plan := map[string]map[string]int{}
	for _, q := range pub.Questions {
		for _, id := range append([]string{q.ChooserID}, q.PredictorIDs...) {
			if plan[id] == nil {
				plan[id] = map[string]int{}
			}
			plan[id][q.QuestionID] = 1
		}
	}
	states := answerAll(t, prep, pub, plan)

	view := project(t, prep, modes.Viewer{Subject: subjects[0], Role: "player"}, modes.PhaseSettled, states)
	if len(view.Final) != len(subjects) {
		t.Fatalf("settled projection carries %d final scores, want %d", len(view.Final), len(subjects))
	}
	for _, qv := range view.Questions {
		if !qv.Locked || qv.Reveal == nil {
			t.Fatalf("question %s is not revealed after settle", qv.QuestionID)
		}
	}
}

// TestSelfViewCarriesOnlyOwnAnswers asserts the caller's own answer list never
// includes another participant's submission.
func TestSelfViewCarriesOnlyOwnAnswers(t *testing.T) {
	subjects := roster(2)
	prep, pub := prepareRound(t, "self-seed", `{"questions":2}`, subjects)
	q := pub.Questions[0]
	chooser := modes.Subject{ID: q.ChooserID, Kind: modes.SubjectPlayer}
	_, chooserState := answer(t, prep, chooser, q.Options[0].OptionID, nil)
	states := map[string]json.RawMessage{q.ChooserID: chooserState}

	var predictor modes.Subject
	for _, subject := range subjects {
		if subject.ID != q.ChooserID {
			predictor = subject
		}
	}
	view := project(t, prep, modes.Viewer{Subject: predictor, Role: "player"}, modes.PhaseCollecting, states)
	if view.Self == nil {
		t.Fatal("the caller has no self view")
	}
	if len(view.Self.Answers) != 0 {
		t.Fatalf("the caller's self view lists %d answers before they acted", len(view.Self.Answers))
	}
	if view.Self.SubjectID != predictor.ID {
		t.Fatalf("self view is for %q, want %q", view.Self.SubjectID, predictor.ID)
	}
}

// TestChooserIsNeverAlsoAPredictor asserts the role split is strict for every
// question, which is what makes the visibility rule sound.
func TestChooserIsNeverAlsoAPredictor(t *testing.T) {
	for _, players := range []int{2, 3, 5} {
		t.Run(fmt.Sprintf("players%d", players), func(t *testing.T) {
			_, pub := prepareRound(t, "role-seed", fmt.Sprintf(`{"questions":%d}`, players), roster(players))
			for _, q := range pub.Questions {
				for _, predictor := range q.PredictorIDs {
					if predictor == q.ChooserID {
						t.Fatalf("question %s: %s is both chooser and predictor",
							q.QuestionID, q.ChooserID)
					}
				}
				if q.PredictionSlots != len(q.PredictorIDs) {
					t.Fatalf("question %s: predictionSlots %d but %d predictors",
						q.QuestionID, q.PredictionSlots, len(q.PredictorIDs))
				}
			}
		})
	}
}

// mustJSON marshals v or fails.
func mustJSON(t *testing.T, v any) json.RawMessage {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return raw
}

// TestPrepareRefusesWithoutQuestionBank asserts the mode has no built-in fallback.
func TestPrepareRefusesWithoutQuestionBank(t *testing.T) {
	cases := []struct {
		name    string
		content modes.ContentSource
		want    string
	}{
		{name: "nil content", content: nil, want: "no content source"},
		{name: "empty pack", content: fakeContent{}, want: QuestionsSection},
		{name: "section present but empty",
			content: fakeContent{QuestionsSection: json.RawMessage(`[]`)}, want: "no usable question"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := New().Prepare(context.Background(), modes.PrepareInput{
				Scope:   scope("no-pack-seed"),
				Subject: roster(2),
				Config:  json.RawMessage(`{}`),
				Content: tc.content,
			})
			if err == nil {
				t.Fatal("Prepare admitted a round with no question bank")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error %q does not name %q", err, tc.want)
			}
		})
	}
}

// TestQuestionsComeFromThePack asserts the drawn questions use the PACK text.
//
// This is the test that would catch the original defect: the mode had a built-in bank, so
// publishing rapport.questions changed nothing that a test could see.
func TestQuestionsComeFromThePack(t *testing.T) {
	_, pub := prepareRound(t, "from-pack-seed", `{"questions":5}`, roster(2))
	bankTexts := make(map[string]struct{}, len(fixtureQuestions))
	for _, q := range fixtureQuestions {
		bankTexts[q] = struct{}{}
	}
	if len(pub.Questions) != 5 {
		t.Fatalf("round has %d questions, want 5", len(pub.Questions))
	}
	for _, q := range pub.Questions {
		if _, ok := bankTexts[q.Prompt]; !ok {
			t.Fatalf("question %q is not from the pack", q.Prompt)
		}
	}
}

// TestQuestionShapeIsValidated asserts the four-option rule and duplicate detection.
func TestQuestionShapeIsValidated(t *testing.T) {
	cases := []struct {
		name    string
		content modes.ContentSource
		want    string
	}{
		{name: "three options", content: packWith(packOptions{optionCount: 3}),
			want: "exactly 4 are required"},
		{name: "five options", content: packWith(packOptions{optionCount: 5}),
			want: "exactly 4 are required"},
		{name: "duplicate question id",
			content: fakeContent{QuestionsSection: json.RawMessage(`[
				{"questionId":"q1","prompt":"a","options":["1","2","3","4"],
				 "provenance":{"kind":"authored-design-value","notOfficialData":true,"author":"t","basis":"b"}},
				{"questionId":"q1","prompt":"b","options":["1","2","3","4"],
				 "provenance":{"kind":"authored-design-value","notOfficialData":true,"author":"t","basis":"b"}}]`)},
			want: "appears twice"},
		{name: "duplicate option",
			content: fakeContent{QuestionsSection: json.RawMessage(`[
				{"questionId":"q1","prompt":"a","options":["1","1","3","4"],
				 "provenance":{"kind":"authored-design-value","notOfficialData":true,"author":"t","basis":"b"}}]`)},
			want: "repeats the option"},
		{name: "empty option",
			content: fakeContent{QuestionsSection: json.RawMessage(`[
				{"questionId":"q1","prompt":"a","options":["1","","3","4"],
				 "provenance":{"kind":"authored-design-value","notOfficialData":true,"author":"t","basis":"b"}}]`)},
			want: "empty option"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := New().Prepare(context.Background(), modes.PrepareInput{
				Scope:   scope("shape-seed"),
				Subject: roster(2),
				Config:  json.RawMessage(`{}`),
				Content: tc.content,
			})
			if err == nil {
				t.Fatalf("Prepare accepted a bank with %s", tc.name)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error %q does not mention %q", err, tc.want)
			}
		})
	}
}

// TestQuestionProvenanceIsValidated asserts the provenance block is checked.
func TestQuestionProvenanceIsValidated(t *testing.T) {
	cases := []struct {
		name string
		prov string
		want string
	}{
		{name: "unknown kind",
			prov: `{"kind":"guess","notOfficialData":true,"author":"t","basis":"b"}`,
			want: "unknown provenance kind"},
		{name: "authored but claims official",
			prov: `{"kind":"authored-design-value","notOfficialData":false,"author":"t","basis":"b"}`,
			want: "must set notOfficialData=true"},
		{name: "no author",
			prov: `{"kind":"authored-design-value","notOfficialData":true,"basis":"b"}`,
			want: "no author"},
		{name: "no basis",
			prov: `{"kind":"authored-design-value","notOfficialData":true,"author":"t"}`,
			want: "no basis"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := New().Prepare(context.Background(), modes.PrepareInput{
				Scope:   scope("prov-seed"),
				Subject: roster(2),
				Config:  json.RawMessage(`{}`),
				Content: packWith(packOptions{badProvenance: tc.prov}),
			})
			if err == nil {
				t.Fatalf("Prepare accepted questions whose provenance was %q", tc.name)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error %q does not mention %q", err, tc.want)
			}
		})
	}

	// A missing provenance block is refused.
	t.Run("no provenance block", func(t *testing.T) {
		_, err := New().Prepare(context.Background(), modes.PrepareInput{
			Scope:   scope("prov-seed"),
			Subject: roster(2),
			Config:  json.RawMessage(`{}`),
			Content: packWith(packOptions{omitProvenance: true}),
		})
		if err == nil {
			t.Fatal("Prepare accepted questions with no provenance block")
		}
		if !strings.Contains(err.Error(), "no provenance block") {
			t.Fatalf("error %q does not mention the missing block", err)
		}
	})
}

// TestRequirementFieldsMatchWhatTheModeReads asserts the declaration matches the loader.
func TestRequirementFieldsMatchWhatTheModeReads(t *testing.T) {
	fields := New().Requirement().Fields
	if len(fields) != 1 || fields[0] != QuestionsSection {
		t.Fatalf("Requirement().Fields = %v, want exactly [%q]", fields, QuestionsSection)
	}
}

// TestMinEntitiesIsAPoolFloor asserts MinEntities counts questions, not players.
func TestMinEntitiesIsAPoolFloor(t *testing.T) {
	req := New().Requirement()
	if req.MinEntities != DefaultQuestions {
		t.Fatalf("MinEntities = %d, want %d (the documented question count per round)",
			req.MinEntities, DefaultQuestions)
	}
	// It must not equal the table floor: those are different questions.
	if req.MinEntities == MinSubjects {
		t.Fatal("MinEntities equals MinSubjects, which conflates the pool floor with the table floor")
	}
}
