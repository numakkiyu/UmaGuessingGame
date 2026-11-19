package strategyrace

import (
	"context"
	"encoding/json"
	"fmt"
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

// fixtureProvenance is the provenance block the fixture baseline carries. It marks the
// baseline as an entertainment simulation, which the mode requires.
func fixtureProvenance() map[string]any {
	return map[string]any{
		"kind":            ProvenanceAuthored,
		"notOfficialData": true,
		"author":          "test-fixture",
		"basis":           "docs/玩法与规则.md (strategy-race)",
		"simulation":      true,
	}
}

// fixtureBaseline builds the baseline object the fixture publishes.
//
// The numbers are the documented trial values, so a test that reads them is testing
// the same baseline a player would get. They are built here rather than read from package
// constants on purpose: if the mode still applied constants instead of the pack baseline, a
// test that changes a number here would not notice.
func fixtureBaseline() map[string]any {
	return map[string]any{
		"totalTurns":      TotalTurns,
		"decisionSeconds": DecisionSeconds,
		"startPosition":   0,
		"startStamina":    6,
		"staminaCap":      8,
		"followMinGap":    1,
		"followMaxGap":    3,
		"defaultAction":   string(DefaultAction),
		"actions": []map[string]any{
			{"action": "steady", "label": "稳进", "positionGain": 3, "staminaDelta": 0},
			{"action": "sprint", "label": "冲刺", "positionGain": 5,
				"staminaDelta": -2, "minStamina": 2},
			{"action": "recover", "label": "恢复", "positionGain": 1, "staminaDelta": 2},
			{"action": "follow", "label": "跟跑", "positionGain": 4,
				"alternateGain": 2, "staminaDelta": 0, "leaderGap": true},
		},
		"terrainChanges": false,
		"randomEvents":   false,
		"provenance":     fixtureProvenance(),
	}
}

// packOf builds a content source carrying the fixture baseline.
func packOf() modes.ContentSource {
	raw, err := json.Marshal(fixtureBaseline())
	if err != nil {
		panic(err)
	}
	return fakeContent{TrackSection: raw}
}

// fixtureTrack decodes the fixture baseline into a trackRules value, for the tests that call
// applyAction directly.
func fixtureTrack(t *testing.T) trackRules {
	t.Helper()
	raw, err := json.Marshal(fixtureBaseline())
	if err != nil {
		t.Fatalf("marshal fixture: %v", err)
	}
	var tr trackRules
	if err := json.Unmarshal(raw, &tr); err != nil {
		t.Fatalf("decode fixture: %v", err)
	}
	if err := tr.validate(); err != nil {
		t.Fatalf("fixture baseline is invalid: %v", err)
	}
	return tr
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
			ID:   fmt.Sprintf("t%d", i+1),
			Kind: modes.SubjectPlayer,
		})
	}
	return out
}

// prepareRace runs Prepare and returns the prepared round plus decoded halves.
func prepareRace(t *testing.T, seed, cfg string, subjects []modes.Subject) (modes.Prepared, publicState) {
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

// decisionAction builds a contract-shaped strategy.action payload.
func decisionAction(turn int, action Action) modes.ModeAction {
	return modes.ModeAction{
		Kind: ActionKindDecision,
		Payload: json.RawMessage(fmt.Sprintf(
			`{"kind":"strategy.action","action":%q,"turn":%d}`, action, turn)),
	}
}

// apply runs Apply against the current round state.
func apply(t *testing.T, prep modes.Prepared, subject modes.Subject, action modes.ModeAction) modes.Decision {
	t.Helper()
	dec, err := New().Apply(modes.ApplyInput{
		Scope:   scope("seed"),
		Subject: subject,
		Action:  action,
		Public:  prep.Public,
		Private: prep.Private,
	})
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	return dec
}

// commitAndPrepare applies then commits one decision, returning the new prepared
// round. It mirrors the room layer's transaction order.
func commitAndPrepare(t *testing.T, prep modes.Prepared, subject modes.Subject, action modes.ModeAction) modes.Prepared {
	t.Helper()
	in := modes.ApplyInput{
		Scope:   scope("seed"),
		Subject: subject,
		Action:  action,
		Public:  prep.Public,
		Private: prep.Private,
	}
	dec, err := New().Apply(in)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if !dec.Accepted {
		t.Fatalf("decision rejected: code=%q detail=%q", dec.Code, dec.Detail)
	}
	pub, priv, err := New().CommitShared(in, dec)
	if err != nil {
		t.Fatalf("CommitShared: %v", err)
	}
	return modes.Prepared{Public: pub, Private: priv, QuestionCount: prep.QuestionCount}
}

// project decodes the projection for a viewer.
func project(t *testing.T, prep modes.Prepared, viewer modes.Viewer, phase modes.Phase) projection {
	t.Helper()
	raw, err := New().Project(modes.ProjectInput{
		Scope:   scope("seed"),
		Viewer:  viewer,
		Public:  prep.Public,
		Private: prep.Private,
		States:  map[string]json.RawMessage{},
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

// expireRemaining explicitly closes each missing window before testing settlement.
func expireRemaining(t *testing.T, prep modes.Prepared) modes.Prepared {
	t.Helper()
	for i := 0; i < TotalTurns; i++ {
		var more bool
		var err error
		prep.Public, more, err = New().ExpireTurn(prep.Public)
		if err != nil {
			t.Fatal(err)
		}
		if !more {
			return prep
		}
	}
	t.Fatal("race did not complete")
	return prep
}

// settle runs Settle and returns the scores keyed by subject id.
func settle(t *testing.T, prep modes.Prepared) map[string]modes.Score {
	t.Helper()
	prep = expireRemaining(t, prep)
	scores, err := New().Settle(modes.SettleInput{
		Scope:   scope("seed"),
		Phase:   modes.PhaseSettled,
		Public:  prep.Public,
		Private: prep.Private,
		States:  map[string]json.RawMessage{},
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

// TestPrepareRejectsIllegalTeamCounts covers the documented 2–8 bound plus the
// single-team practice gate.
func TestPrepareRejectsIllegalTeamCounts(t *testing.T) {
	cases := []struct {
		name    string
		teams   int
		cfg     string
		wantErr string
		wantOK  bool
	}{
		{name: "zero teams", teams: 0, cfg: `{}`, wantErr: "outside"},
		{name: "one team without practice", teams: 1, cfg: `{}`, wantErr: "practice form"},
		{name: "one team with practice", teams: 1, cfg: `{"practice":true}`, wantOK: true},
		{name: "two teams", teams: 2, cfg: `{}`, wantOK: true},
		{name: "eight teams", teams: 8, cfg: `{}`, wantOK: true},
		{name: "nine teams", teams: 9, cfg: `{}`, wantErr: "outside"},
		{name: "two teams marked practice", teams: 2, cfg: `{"practice":true}`, wantErr: "only valid"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := New().Prepare(context.Background(), modes.PrepareInput{
				Scope:   scope("seed"),
				Subject: roster(tc.teams),
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

// TestValidateConfigRejectsBaselineDrift asserts the published baseline cannot be
// changed by configuration: a different race length is a different rules version.
func TestValidateConfigRejectsBaselineDrift(t *testing.T) {
	cases := []struct {
		name    string
		cfg     string
		wantErr string
	}{
		{name: "default", cfg: `{}`},
		{name: "nil config", cfg: ``},
		{name: "explicit baseline", cfg: `{"totalTurns":8,"decisionSeconds":20}`},
		// The turn count is content, so ValidateConfig only bounds it for sanity; a
		// disagreement with the loaded pack is refused in Prepare, where both values are
		// known. See TestConfigMustAgreeWithBaseline.
		{name: "nine turns passes the sanity bound", cfg: `{"totalTurns":9}`},
		{name: "thirty three turns", cfg: `{"totalTurns":33}`, wantErr: "outside"},
		{name: "zero decision seconds falls back", cfg: `{"decisionSeconds":0}`},
		{name: "negative decision seconds", cfg: `{"decisionSeconds":-1}`, wantErr: "outside"},
		{name: "too long decision window", cfg: `{"decisionSeconds":121}`, wantErr: "outside"},
		{name: "malformed", cfg: `{"totalTurns":"x"}`, wantErr: "invalid config"},
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

// TestFollowGapBoundaries covers the 「跟跑边界」 bullet: the follow gain is +4
// exactly when some team is 1–3 cells ahead at turn start, and +2 otherwise.
//
// The gap is asserted by giving the follower a known head start and checking the
// published feedback, which is the only place the rule becomes visible.
func TestFollowGapBoundaries(t *testing.T) {
	// leaderGap constructs a two-team race where team B trails by gap cells at
	// the start of turn 1. It does so by resolving turn 1 with A steady and B
	// steady, then reading turn 2's follow outcome — but the cleanest way to set
	// an exact gap is to drive the race and check the rule function directly.
	cases := []struct {
		name     string
		gap      int
		wantGain int
		wantQual bool
	}{
		{name: "same position", gap: 0, wantGain: FollowFarGain},
		{name: "one cell ahead", gap: 1, wantGain: FollowCloseGain, wantQual: true},
		{name: "two cells ahead", gap: 2, wantGain: FollowCloseGain, wantQual: true},
		{name: "three cells ahead", gap: 3, wantGain: FollowCloseGain, wantQual: true},
		{name: "four cells ahead", gap: 4, wantGain: FollowFarGain},
		{name: "far ahead", gap: 9, wantGain: FollowFarGain},
		{name: "behind the follower", gap: -3, wantGain: FollowFarGain},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// The follower sits at position 10; the leader sits at 10+gap.
			snapshot := []teamPoint{
				{SubjectID: "follower", Position: 10, Stamina: StartStamina},
				{SubjectID: "leader", Position: 10 + tc.gap, Stamina: StartStamina},
			}
			self := snapshot[0]
			got, err := applyAction(fixtureTrack(t), ActionFollow, self, snapshot)
			if err != nil {
				t.Fatalf("applyAction: %v", err)
			}
			if got.PositionGain != tc.wantGain {
				t.Fatalf("gap %d: positionGain = %d, want %d", tc.gap, got.PositionGain, tc.wantGain)
			}
			if got.LeaderGap != tc.wantQual {
				t.Fatalf("gap %d: leaderGap = %v, want %v", tc.gap, got.LeaderGap, tc.wantQual)
			}
			if got.Stamina != StartStamina {
				t.Fatalf("gap %d: follow changed stamina to %d", tc.gap, got.Stamina)
			}
		})
	}
}

// TestFollowIgnoresOwnPosition asserts a team cannot qualify itself as its own
// leader, which would make follow strictly better than steady for everybody.
func TestFollowIgnoresOwnPosition(t *testing.T) {
	snapshot := []teamPoint{{SubjectID: "solo", Position: 10, Stamina: StartStamina}}
	got, err := applyAction(fixtureTrack(t), ActionFollow, snapshot[0], snapshot)
	if err != nil {
		t.Fatalf("applyAction: %v", err)
	}
	if got.PositionGain != FollowFarGain {
		t.Fatalf("a lone team got follow gain %d, want %d", got.PositionGain, FollowFarGain)
	}
}

// TestSimultaneousResolutionOrderIndependence covers the 「同步结算」 and
// 「遍历顺序无关」 bullets.
//
// The same set of decisions is committed in two different orders and the
// resolved race must be byte-identical, including the follow outcomes that depend
// on the snapshot. This is the test that would catch a fold that updates
// positions team-by-team as it iterates.
func TestSimultaneousResolutionOrderIndependence(t *testing.T) {
	subjects := roster(3)
	// A sprint, B follow, C steady on turn 1 — a combination whose follow result
	// depends on the snapshot rather than on who is visited first.
	decisions := map[string]Action{
		"t1": ActionSprint,
		"t2": ActionFollow,
		"t3": ActionSteady,
	}
	orders := [][]string{
		{"t1", "t2", "t3"},
		{"t3", "t2", "t1"},
		{"t2", "t3", "t1"},
		{"t2", "t1", "t3"},
	}
	var want string
	for i, order := range orders {
		prep, _ := prepareRace(t, "order-seed", `{}`, subjects)
		for _, id := range order {
			var subject modes.Subject
			for _, s := range subjects {
				if s.ID == id {
					subject = s
				}
			}
			prep = commitAndPrepare(t, prep, subject, decisionAction(1, decisions[id]))
		}
		// Explicitly expire remaining windows; settlement itself must not invent them.
		prep = expireRemaining(t, prep)
		scores, err := New().Settle(modes.SettleInput{
			Scope:   scope("order-seed"),
			Phase:   modes.PhaseSettled,
			Public:  prep.Public,
			Private: prep.Private,
			States:  map[string]json.RawMessage{},
		})
		if err != nil {
			t.Fatalf("order %v: Settle: %v", order, err)
		}
		got := string(mustJSON(t, scores))
		if i == 0 {
			want = got
			continue
		}
		if got != want {
			t.Fatalf("commit order %v produced a different race:\n got %s\nwant %s", order, got, want)
		}
	}
}

// TestStaminaCeiling covers the 「体力封顶」 bullet: recover never pushes stamina
// above the cap, and the applied delta is the room that actually remained.
func TestStaminaCeiling(t *testing.T) {
	cases := []struct {
		name        string
		start       int
		wantStamina int
		wantDelta   int
	}{
		{name: "below cap has room", start: 0, wantStamina: 2, wantDelta: 2},
		{name: "one below cap", start: 7, wantStamina: 8, wantDelta: 1},
		{name: "exactly at cap", start: 8, wantStamina: 8, wantDelta: 0},
		{name: "above cap is clamped", start: 9, wantStamina: 8, wantDelta: 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			self := teamPoint{SubjectID: "t1", Position: 0, Stamina: tc.start}
			got, err := applyAction(fixtureTrack(t), ActionRecover, self, []teamPoint{self})
			if err != nil {
				t.Fatalf("applyAction: %v", err)
			}
			if got.Stamina != tc.wantStamina {
				t.Fatalf("stamina = %d, want %d", got.Stamina, tc.wantStamina)
			}
			if got.StaminaDelta != tc.wantDelta {
				t.Fatalf("staminaDelta = %d, want %d", got.StaminaDelta, tc.wantDelta)
			}
			if got.PositionGain != RecoverGain {
				t.Fatalf("positionGain = %d, want %d", got.PositionGain, RecoverGain)
			}
		})
	}
}

// TestSprintStaminaGate covers the documented refusal of a sprint without enough
// stamina, and asserts the rejection does not silently become a steady action.
func TestSprintStaminaGate(t *testing.T) {
	subjects := roster(2)
	prep, _ := prepareRace(t, "sprint-seed", `{}`, subjects)

	// Turn 1: t1 sprints twice worth of stamina is available (6), so the first
	// sprint is legal. Drain the team to below the gate and confirm the refusal.
	prep = commitAndPrepare(t, prep, subjects[0], decisionAction(1, ActionSprint))
	prep = commitAndPrepare(t, prep, subjects[1], decisionAction(1, ActionSteady))

	// After turn 1 t1 has 4 stamina; sprint is still legal on turn 2.
	prep = commitAndPrepare(t, prep, subjects[0], decisionAction(2, ActionSprint))
	prep = commitAndPrepare(t, prep, subjects[1], decisionAction(2, ActionSteady))

	// After turn 2 t1 has 2 stamina; one more sprint brings it to 0 on turn 3.
	prep = commitAndPrepare(t, prep, subjects[0], decisionAction(3, ActionSprint))
	prep = commitAndPrepare(t, prep, subjects[1], decisionAction(3, ActionSteady))

	// Now t1 has 0 stamina: the sprint must be refused with its own code.
	dec := apply(t, prep, subjects[0], decisionAction(4, ActionSprint))
	if dec.Accepted {
		t.Fatal("a sprint with insufficient stamina was accepted")
	}
	if dec.Code != CodeInsufficientStamina {
		t.Fatalf("rejection code = %q, want %q", dec.Code, CodeInsufficientStamina)
	}
	if dec.State != nil {
		t.Fatal("a rejected sprint must not produce participant state")
	}

	// A steady action is still legal, so the refusal is specific to the sprint.
	dec = apply(t, prep, subjects[0], decisionAction(4, ActionSteady))
	if !dec.Accepted {
		t.Fatalf("steady was rejected: %q %q", dec.Code, dec.Detail)
	}
}

// TestMissedDeadlineDefaultsToSteady covers the 「超时」 bullet: a team that never
// submitted is resolved with the announced default rather than being skipped.
func TestMissedDeadlineDefaultsToSteady(t *testing.T) {
	subjects := roster(2)
	prep, _ := prepareRace(t, "timeout-seed", `{}`, subjects)
	// Only t1 submits turn 1; t2 never does.
	prep = commitAndPrepare(t, prep, subjects[0], decisionAction(1, ActionSprint))

	// While the turn is open, the standings must NOT already include the open
	// turn: that would reveal how an unfinished turn resolves.
	view := project(t, prep, modes.Viewer{Subject: subjects[0], Role: "player"}, modes.PhaseCollecting)
	if view.ResolvedTurns != 0 {
		t.Fatalf("resolvedTurns = %d while turn 1 is open, want 0", view.ResolvedTurns)
	}
	for _, point := range view.Standings {
		if point.Position != StartPosition {
			t.Fatalf("open-turn standings already moved %s to %d", point.SubjectID, point.Position)
		}
	}

	// Expire each missing window before settling the completed race.
	scores := settle(t, prep)
	explanation := decodeExplanation(t, scores["t2"])
	if explanation.Position != StartPosition+SteadyGain*TotalTurns {
		t.Fatalf("t2 final position = %d, want %d (steady every turn)",
			explanation.Position, StartPosition+SteadyGain*TotalTurns)
	}
	if explanation.Formula == "" {
		t.Fatal("settle explanation carries no formula")
	}
}

// TestOpenTurnNotResolvedEarly asserts the projection never advances a marker on
// an unfinished turn, which is what would leak a decision.
func TestOpenTurnNotResolvedEarly(t *testing.T) {
	subjects := roster(2)
	prep, _ := prepareRace(t, "early-seed", `{}`, subjects)

	// Turn 1 fully committed: exactly one turn is resolved.
	prep = commitAndPrepare(t, prep, subjects[0], decisionAction(1, ActionSprint))
	prep = commitAndPrepare(t, prep, subjects[1], decisionAction(1, ActionSteady))

	view := project(t, prep, modes.Viewer{Subject: subjects[0], Role: "player"}, modes.PhaseCollecting)
	if view.ResolvedTurns != 1 {
		t.Fatalf("resolvedTurns = %d, want 1", view.ResolvedTurns)
	}
	if view.CurrentTurn != 2 {
		t.Fatalf("currentTurn = %d, want 2", view.CurrentTurn)
	}
	for _, point := range view.Standings {
		switch point.SubjectID {
		case "t1":
			if point.Position != StartPosition+SprintGain {
				t.Fatalf("t1 position = %d, want %d", point.Position, StartPosition+SprintGain)
			}
			if point.Stamina != StartStamina-SprintCost {
				t.Fatalf("t1 stamina = %d, want %d", point.Stamina, StartStamina-SprintCost)
			}
		case "t2":
			if point.Position != StartPosition+SteadyGain {
				t.Fatalf("t2 position = %d, want %d", point.Position, StartPosition+SteadyGain)
			}
		}
	}
}

// TestDuplicateTurnResolutionRefused covers the 「回合重复结算」 bullet.
func TestDuplicateTurnResolutionRefused(t *testing.T) {
	subjects := roster(2)
	prep, _ := prepareRace(t, "dup-seed", `{}`, subjects)

	prep = commitAndPrepare(t, prep, subjects[0], decisionAction(1, ActionSprint))

	// The same team submitting turn 1 again is refused before it reaches the log.
	dec := apply(t, prep, subjects[0], decisionAction(1, ActionSteady))
	if dec.Accepted {
		t.Fatal("a second decision for the same turn was accepted")
	}
	if dec.Code != CodeDuplicateTurn {
		t.Fatalf("code = %q, want %q", dec.Code, CodeDuplicateTurn)
	}

	// A direct CommitShared replay of an already committed turn must be a no-op
	// rather than a second log entry.
	in := modes.ApplyInput{
		Scope:   scope("dup-seed"),
		Subject: subjects[0],
		Action:  decisionAction(1, ActionSprint),
		Public:  prep.Public,
		Private: prep.Private,
	}
	pub, _, err := New().CommitShared(in, modes.Decision{Accepted: true})
	if err != nil {
		t.Fatalf("CommitShared: %v", err)
	}
	var after publicState
	if err := json.Unmarshal(pub, &after); err != nil {
		t.Fatalf("decode: %v", err)
	}
	count := 0
	for _, ca := range after.Committed {
		if ca.Turn == 1 && ca.SubjectID == "t1" {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("turn 1 for t1 appears %d times in the log, want 1", count)
	}
}

// TestClosedTurnRefused covers the "a past turn cannot be rewritten" rule.
func TestClosedTurnRefused(t *testing.T) {
	subjects := roster(2)
	prep, _ := prepareRace(t, "closed-seed", `{}`, subjects)
	prep = commitAndPrepare(t, prep, subjects[0], decisionAction(1, ActionSprint))
	prep = commitAndPrepare(t, prep, subjects[1], decisionAction(1, ActionSteady))

	// Turn 1 is closed; a decision addressed to it is refused as closed rather
	// than reinterpreted as turn 2.
	dec := apply(t, prep, subjects[0], decisionAction(1, ActionSteady))
	if dec.Accepted {
		t.Fatal("a decision for a closed turn was accepted")
	}
	if dec.Code != CodeDuplicateTurn && dec.Code != CodeTurnClosed {
		t.Fatalf("code = %q, want %q or %q", dec.Code, CodeDuplicateTurn, CodeTurnClosed)
	}

	// A decision for a future turn is refused because follow eligibility needs
	// the snapshot of the turns in between.
	dec = apply(t, prep, subjects[0], decisionAction(5, ActionSteady))
	if dec.Accepted {
		t.Fatal("a decision for a future turn was accepted")
	}
	if dec.Code != CodeTurnMismatch {
		t.Fatalf("code = %q, want %q", dec.Code, CodeTurnMismatch)
	}
}

// TestRaceFinishedRefused asserts no decision is accepted after the last turn.
func TestRaceFinishedRefused(t *testing.T) {
	subjects := roster(2)
	prep, _ := prepareRace(t, "finished-seed", `{}`, subjects)
	for turn := 1; turn <= TotalTurns; turn++ {
		prep = commitAndPrepare(t, prep, subjects[0], decisionAction(turn, ActionSteady))
		prep = commitAndPrepare(t, prep, subjects[1], decisionAction(turn, ActionSteady))
	}
	dec := apply(t, prep, subjects[0], decisionAction(TotalTurns+1, ActionSteady))
	if dec.Accepted {
		t.Fatal("a decision after the last turn was accepted")
	}
	if dec.Code != CodeRaceFinished {
		t.Fatalf("code = %q, want %q", dec.Code, CodeRaceFinished)
	}
}

// TestSpectatorCannotAct covers the 「观众干预禁止」 bullet.
//
// The rule layer refuses any subject that is not on the track. A spectator has no
// competing subject, so the room layer passes the viewer's own subject; if it ever
// passed a team id, the roster check is what stops it. The test asserts the roster
// check directly, which is the rule-layer half of the guarantee.
func TestSpectatorCannotAct(t *testing.T) {
	subjects := roster(2)
	prep, _ := prepareRace(t, "spectator-seed", `{}`, subjects)

	outsider := modes.Subject{ID: "spectator-1", Kind: modes.SubjectPlayer}
	dec := apply(t, prep, outsider, decisionAction(1, ActionSprint))
	if dec.Accepted {
		t.Fatal("a non-participant decision was accepted")
	}
	if dec.Code != CodeNotOnTrack {
		t.Fatalf("code = %q, want %q", dec.Code, CodeNotOnTrack)
	}

	// A host/referee identity is not on the track either: holding the reveal
	// authority must not grant a driving decision.
	dec = apply(t, prep, modes.Subject{ID: "host-1", Kind: modes.SubjectPlayer}, decisionAction(1, ActionSteady))
	if dec.Accepted {
		t.Fatal("a host identity was accepted as a team")
	}
}

// TestUnknownDecisionRejected covers payload validation.
func TestUnknownDecisionRejected(t *testing.T) {
	subjects := roster(2)
	prep, _ := prepareRace(t, "payload-seed", `{}`, subjects)

	cases := []struct {
		name string
		kind string
		body string
		want string
	}{
		{name: "unknown action kind", kind: "strategy.teleport", body: `{"turn":1}`, want: CodeUnknownAction},
		{name: "empty payload", kind: ActionKindDecision, body: ``, want: CodeInvalidPayload},
		{name: "not json", kind: ActionKindDecision, body: `not-json`, want: CodeInvalidPayload},
		{name: "unknown decision", kind: ActionKindDecision,
			body: `{"kind":"strategy.action","action":"teleport","turn":1}`, want: CodeUnknownDecision},
		{name: "kind mismatch", kind: ActionKindDecision,
			body: `{"kind":"classic.guess","action":"steady","turn":1}`, want: CodeInvalidPayload},
		{name: "empty decision", kind: ActionKindDecision,
			body: `{"kind":"strategy.action","turn":1}`, want: CodeUnknownDecision},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dec := apply(t, prep, subjects[0], modes.ModeAction{
				Kind:    tc.kind,
				Payload: json.RawMessage(tc.body),
			})
			if dec.Accepted {
				t.Fatal("an invalid decision was accepted")
			}
			if dec.Code != tc.want {
				t.Fatalf("code = %q, want %q (detail %q)", dec.Code, tc.want, dec.Detail)
			}
		})
	}
}

// TestTiedPositionsShareRankAndScore covers the 「并列」 bullet.
//
// Both teams take the identical action sequence, so they finish on the same
// position. They must share rank 1 and therefore the same score, and the score
// must be the full 100 rather than being split by arrival time.
func TestTiedPositionsShareRankAndScore(t *testing.T) {
	subjects := roster(2)
	prep, _ := prepareRace(t, "tie-seed", `{}`, subjects)
	for turn := 1; turn <= TotalTurns; turn++ {
		prep = commitAndPrepare(t, prep, subjects[0], decisionAction(turn, ActionSteady))
		prep = commitAndPrepare(t, prep, subjects[1], decisionAction(turn, ActionSteady))
	}
	scores := settle(t, prep)
	a, b := scores["t1"], scores["t2"]
	if a.ScoreUnits != b.ScoreUnits {
		t.Fatalf("tied teams scored %d and %d", a.ScoreUnits, b.ScoreUnits)
	}
	if a.Rank != 1 || b.Rank != 1 {
		t.Fatalf("tied ranks = %d and %d, want 1 and 1", a.Rank, b.Rank)
	}
	if !a.SharedRank || !b.SharedRank {
		t.Fatalf("tied teams not marked shared: %v %v", a.SharedRank, b.SharedRank)
	}
	// n=2, r=1 -> floor(100*(2-1)/(2-1)) = 100.
	if a.ScoreUnits != modeutilUnits(100) {
		t.Fatalf("tied score = %d units, want %d", a.ScoreUnits, modeutilUnits(100))
	}
}

// TestRankScoreFormula covers the documented floor(100×(n-r)/(n-1)) mapping for
// several team counts and every rank.
func TestRankScoreFormula(t *testing.T) {
	cases := []struct {
		teams int
		rank  int
		want  int
	}{
		{teams: 2, rank: 1, want: 100},
		{teams: 2, rank: 2, want: 0},
		{teams: 3, rank: 1, want: 100},
		{teams: 3, rank: 2, want: 50},
		{teams: 3, rank: 3, want: 0},
		{teams: 4, rank: 1, want: 100},
		{teams: 4, rank: 2, want: 66},
		{teams: 4, rank: 3, want: 33},
		{teams: 4, rank: 4, want: 0},
		{teams: 8, rank: 1, want: 100},
		{teams: 8, rank: 2, want: 85},
		{teams: 8, rank: 8, want: 0},
	}
	for _, tc := range cases {
		t.Run(fmt.Sprintf("teams%d_rank%d", tc.teams, tc.rank), func(t *testing.T) {
			// One team at each rank: build final positions descending.
			final := make([]teamPoint, 0, tc.teams)
			for i := 0; i < tc.teams; i++ {
				final = append(final, teamPoint{
					SubjectID: fmt.Sprintf("t%02d", i+1),
					Position:  (tc.teams - i) * 10,
					Stamina:   StartStamina,
				})
			}
			ranked := rankTeams(final, tc.teams, true)
			var got rankedTeam
			for _, rt := range ranked {
				if rt.Rank == tc.rank {
					got = rt
					break
				}
			}
			if got.Rank != tc.rank {
				t.Fatalf("no team finished rank %d in a %d-team race", tc.rank, tc.teams)
			}
			if want := tc.want * 100; got.ScoreUnits != want {
				t.Fatalf("rank %d of %d scored %d units, want %d", tc.rank, tc.teams, got.ScoreUnits, want)
			}
		})
	}
}

// TestPracticeProducesNoCompetitiveScore asserts the single-team form does not
// fabricate a 100-point win .
func TestPracticeProducesNoCompetitiveScore(t *testing.T) {
	prep, _ := prepareRace(t, "practice-seed", `{"practice":true}`, roster(1))
	prep = commitAndPrepare(t, prep, roster(1)[0], decisionAction(1, ActionSprint))

	scores := settle(t, prep)
	if len(scores) != 1 {
		t.Fatalf("practice settled %d scores, want 1", len(scores))
	}
	score := scores["t1"]
	if score.ScoreUnits != 0 {
		t.Fatalf("practice scored %d units, want 0", score.ScoreUnits)
	}
	if !strings.Contains(string(score.Explanation), "practice") {
		t.Fatalf("practice explanation does not say why: %s", score.Explanation)
	}
	// The positions are still published, so the practice form is playable.
	explanation := decodeExplanation(t, score)
	// Turn 1 sprinted (+5) and the seven remaining turns default to steady (+3).
	if explanation.Position != StartPosition+SprintGain+SteadyGain*(TotalTurns-1) {
		t.Fatalf("practice position = %d", explanation.Position)
	}
}

// TestRestartReplayDeterminism covers the 「重启」 bullet: the same committed log
// always replays to the same race, in a fresh process too.
func TestRestartReplayDeterminism(t *testing.T) {
	subjects := roster(4)
	build := func() modes.Prepared {
		prep, _ := prepareRace(t, "restart-seed", `{}`, subjects)
		plan := []struct {
			subject int
			turn    int
			action  Action
		}{
			{0, 1, ActionSprint}, {1, 1, ActionFollow}, {2, 1, ActionRecover}, {3, 1, ActionSteady},
			{0, 2, ActionFollow}, {1, 2, ActionSprint}, {2, 2, ActionSteady}, {3, 2, ActionRecover},
			{0, 3, ActionSteady}, {1, 3, ActionFollow}, {2, 3, ActionSprint}, {3, 3, ActionFollow},
		}
		for _, step := range plan {
			prep = commitAndPrepare(t, prep, subjects[step.subject], decisionAction(step.turn, step.action))
		}
		return prep
	}

	first := build()
	second := build()
	if string(first.Public) != string(second.Public) {
		t.Fatal("two identical replays produced different public state")
	}
	if string(first.Private) != string(second.Private) {
		t.Fatal("two identical replays produced different private state")
	}

	viewA := project(t, first, modes.Viewer{Subject: subjects[0], Role: "player"}, modes.PhaseCollecting)
	viewB := project(t, second, modes.Viewer{Subject: subjects[0], Role: "player"}, modes.PhaseCollecting)
	if string(mustJSON(t, viewA)) != string(mustJSON(t, viewB)) {
		t.Fatal("two identical replays produced different projections")
	}

	settledA := settle(t, first)
	settledB := settle(t, second)
	if string(mustJSON(t, settledA)) != string(mustJSON(t, settledB)) {
		t.Fatal("two identical replays produced different settlements")
	}
}

// TestPrepareIsDeterministic asserts Prepare itself is a pure function of scope.
func TestPrepareIsDeterministic(t *testing.T) {
	subjects := roster(5)
	first, _ := prepareRace(t, "prepare-seed", `{}`, subjects)
	second, _ := prepareRace(t, "prepare-seed", `{}`, subjects)
	if string(first.Public) != string(second.Public) {
		t.Fatal("Prepare is not deterministic")
	}
	if first.QuestionCount != TotalTurns {
		t.Fatalf("questionCount = %d, want %d", first.QuestionCount, TotalTurns)
	}
}

// TestPrivateStateCarriesNoSecret asserts this mode's private blob does not hold
// a hidden answer that a projection could leak, and that the public state carries
// the whole race input.
func TestPrivateStateCarriesNoSecret(t *testing.T) {
	subjects := roster(2)
	prep, _ := prepareRace(t, "private-seed", `{}`, subjects)
	var priv privateState
	if err := json.Unmarshal(prep.Private, &priv); err != nil {
		t.Fatalf("decode private: %v", err)
	}
	if priv.ModeID != ModeID {
		t.Fatalf("private modeId = %q", priv.ModeID)
	}
	if priv.Note == "" {
		t.Fatal("private state does not document why it is empty")
	}
}

// TestProjectionHidesUncommittedDecisions asserts another team's pending decision
// never reaches a viewer.
func TestProjectionHidesUncommittedDecisions(t *testing.T) {
	subjects := roster(3)
	prep, _ := prepareRace(t, "hide-seed", `{}`, subjects)
	// t1 and t2 commit turn 1; t3 does not. t3 chooses recover, which is
	// distinguishable from steady by the position delta.
	prep = commitAndPrepare(t, prep, subjects[0], decisionAction(1, ActionSprint))
	prep = commitAndPrepare(t, prep, subjects[1], decisionAction(1, ActionSteady))

	view := project(t, prep, modes.Viewer{Subject: subjects[2], Role: "player"}, modes.PhaseCollecting)
	if view.Self == nil {
		t.Fatal("the caller has no self view")
	}
	// The caller has not acted, so their own decision list must be empty.
	if len(view.Self.Decisions) != 0 {
		t.Fatalf("the caller's own view lists %d decisions before they acted", len(view.Self.Decisions))
	}
	// Structurally, the projection has no field that could carry another team's
	// pending decision: it publishes standings plus the caller's own list. Assert
	// the decoded shape rather than scanning raw text, because the published
	// action table legitimately names every action.
	var generic map[string]json.RawMessage
	if err := json.Unmarshal(mustJSON(t, view), &generic); err != nil {
		t.Fatalf("decode generic projection: %v", err)
	}
	for _, forbidden := range []string{"committed", "decisions"} {
		if _, present := generic[forbidden]; present && forbidden == "committed" {
			t.Fatalf("projection exposes the round-wide decision log as %q", forbidden)
		}
	}
	// The only "decisions" key allowed is the caller's own, nested under self.
	if _, present := generic["decisions"]; present {
		t.Fatal("projection carries a top-level decisions field")
	}
	// No team but the caller may appear with a decision list.
	if len(view.Standings) != len(subjects) {
		t.Fatalf("standings list %d teams, want %d", len(view.Standings), len(subjects))
	}
}

// TestSelfViewListsOnlyOwnDecisions asserts a player sees their own decisions but
// not another team's.
func TestSelfViewListsOnlyOwnDecisions(t *testing.T) {
	subjects := roster(2)
	prep, _ := prepareRace(t, "self-seed", `{}`, subjects)
	prep = commitAndPrepare(t, prep, subjects[0], decisionAction(1, ActionSprint))

	view := project(t, prep, modes.Viewer{Subject: subjects[0], Role: "player"}, modes.PhaseCollecting)
	if view.Self == nil || len(view.Self.Decisions) != 1 {
		t.Fatalf("self view decisions = %v, want exactly 1", view.Self)
	}
	if view.Self.Decisions[0].SubjectID != "t1" || view.Self.Decisions[0].Action != ActionSprint {
		t.Fatalf("self decision = %+v", view.Self.Decisions[0])
	}

	// The other team sees its own (empty) list, not t1's decision.
	other := project(t, prep, modes.Viewer{Subject: subjects[1], Role: "player"}, modes.PhaseCollecting)
	if other.Self == nil || len(other.Self.Decisions) != 0 {
		t.Fatalf("t2 sees %v decisions", other.Self)
	}
}

// TestNoDominantStrategyIsPureLuck asserts the baseline is not trivially
// dominated: sprint-every-turn is illegal (stamina) and steady-every-turn is
// beaten by a mixed line. This is the rule-layer half of the 「无脑最优序列」 check
// the plan requires before release ; the playtest half is not a unit test.
func TestNoDominantStrategyIsPureLuck(t *testing.T) {
	// A line that alternates sprint and recover should out-distance a pure steady
	// line over the same eight turns without exceeding the stamina cap.
	steadyPosition := StartPosition
	stamina := StartStamina
	position := StartPosition
	// Start 6, cap 8, sprint -2, recover +2. The line must never drop below the
	// sprint gate (2) at the moment it sprints.
	mixed := []Action{
		ActionSprint, ActionRecover, ActionSprint, ActionRecover,
		ActionSprint, ActionRecover, ActionSprint, ActionSprint,
	}
	for _, action := range mixed {
		self := teamPoint{SubjectID: "mixed", Position: position, Stamina: stamina}
		got, err := applyAction(fixtureTrack(t), action, self, []teamPoint{self})
		if err != nil {
			t.Fatalf("mixed line became illegal at %s: %v", action, err)
		}
		position, stamina = got.Position, got.Stamina
		if stamina > StaminaCap {
			t.Fatalf("mixed line exceeded the stamina cap: %d", stamina)
		}
	}
	steadyPosition += SteadyGain * TotalTurns
	if position <= steadyPosition {
		t.Fatalf("the mixed line (%d) does not beat pure steady (%d): steady dominates",
			position, steadyPosition)
	}

	// And pure sprint is not legal, so the game is not a one-button choice.
	stamina = StartStamina
	for turn := 1; turn <= TotalTurns; turn++ {
		self := teamPoint{SubjectID: "sprinter", Position: 0, Stamina: stamina}
		if _, err := applyAction(fixtureTrack(t), ActionSprint, self, []teamPoint{self}); err != nil {
			// The illegal turn is the point of the assertion.
			return
		}
		stamina -= SprintCost
	}
	t.Fatal("pure sprint stayed legal for all eight turns, so the stamina budget is not binding")
}

// mustJSON marshals v or fails the test.
func mustJSON(t *testing.T, v any) json.RawMessage {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return raw
}

// settleExplanation is the decoded settle explanation shape.
type settleExplanation struct {
	Position    int    `json:"position"`
	Stamina     int    `json:"stamina"`
	Rank        int    `json:"rank"`
	Shared      bool   `json:"shared"`
	Teams       int    `json:"teams"`
	Competitive bool   `json:"competitive"`
	Formula     string `json:"formula"`
	Note        string `json:"note"`
}

// decodeExplanation decodes a score explanation.
func decodeExplanation(t *testing.T, score modes.Score) settleExplanation {
	t.Helper()
	var out settleExplanation
	if err := json.Unmarshal(score.Explanation, &out); err != nil {
		t.Fatalf("decode explanation: %v", err)
	}
	return out
}

// modeutilUnits mirrors modeutil.Units without importing the package in tests,
// keeping the assertion explicit about the 100-units-per-point rule.
func modeutilUnits(display int) int { return display * 100 }

// TestPrepareRefusesWithoutBaseline asserts the mode has no built-in fallback.
//
// A fallback would make "the compiler produced no baseline" indistinguishable from "the mode
// works". That is the failure class this project has hit twice, so the mode refuses and the
// refusal names the missing section.
func TestPrepareRefusesWithoutBaseline(t *testing.T) {
	cases := []struct {
		name    string
		content modes.ContentSource
		want    string
	}{
		{name: "nil content", content: nil, want: "no content source"},
		{name: "empty pack", content: fakeContent{}, want: "rules.track"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := New().Prepare(context.Background(), modes.PrepareInput{
				Scope:   scope("no-baseline-seed"),
				Subject: roster(2),
				Config:  json.RawMessage(`{}`),
				Content: tc.content,
			})
			if err == nil {
				t.Fatal("Prepare admitted a round with no baseline")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error %q does not name %q", err, tc.want)
			}
			if !strings.Contains(err.Error(), "required") {
				t.Fatalf("the refusal does not say the section is required: %q", err)
			}
		})
	}
}

// TestBaselineProvenanceIsValidated asserts the baseline must declare itself a simulation.
//
// The rule layer can check the enum, the notOfficialData flag, the author, the basis and the
// simulation flag. It CANNOT check that basis names a real document — that is a human review
// boundary, recorded in the type comment rather than papered over.
func TestBaselineProvenanceIsValidated(t *testing.T) {
	withProv := func(prov map[string]any) modes.ContentSource {
		base := fixtureBaseline()
		base["provenance"] = prov
		raw, err := json.Marshal(base)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		return fakeContent{TrackSection: raw}
	}
	cases := []struct {
		name string
		prov map[string]any
		want string
	}{
		{name: "unknown kind", prov: map[string]any{
			"kind": "guess", "notOfficialData": true, "author": "t",
			"basis": "b", "simulation": true}, want: "unknown provenance kind"},
		{name: "authored but claims official", prov: map[string]any{
			"kind": "authored-design-value", "notOfficialData": false, "author": "t",
			"basis": "b", "simulation": true}, want: "must set notOfficialData=true"},
		{name: "no author", prov: map[string]any{
			"kind": "authored-design-value", "notOfficialData": true,
			"basis": "b", "simulation": true}, want: "no author"},
		{name: "no basis", prov: map[string]any{
			"kind": "authored-design-value", "notOfficialData": true,
			"author": "t", "simulation": true}, want: "no basis"},
		{name: "not marked as simulation", prov: map[string]any{
			"kind": "authored-design-value", "notOfficialData": true, "author": "t",
			"basis": "b", "simulation": false}, want: "entertainment simulation"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := New().Prepare(context.Background(), modes.PrepareInput{
				Scope:   scope("prov-seed"),
				Subject: roster(2),
				Config:  json.RawMessage(`{}`),
				Content: withProv(tc.prov),
			})
			if err == nil {
				t.Fatalf("Prepare accepted a baseline whose provenance was %q", tc.name)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error %q does not mention %q", err, tc.want)
			}
		})
	}

	// A missing provenance block is refused rather than defaulted.
	t.Run("no provenance block", func(t *testing.T) {
		base := fixtureBaseline()
		delete(base, "provenance")
		raw, err := json.Marshal(base)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		_, prepErr := New().Prepare(context.Background(), modes.PrepareInput{
			Scope:   scope("prov-seed"),
			Subject: roster(2),
			Config:  json.RawMessage(`{}`),
			Content: fakeContent{TrackSection: raw},
		})
		if prepErr == nil {
			t.Fatal("Prepare accepted a baseline with no provenance block")
		}
		if !strings.Contains(prepErr.Error(), "no provenance block") {
			t.Fatalf("error %q does not mention the missing block", prepErr)
		}
	})
}

// TestBaselineIsTheSingleSourceOfTruth asserts the simulation reads the PACK baseline, not
// package constants.
//
// This is the test that would have caught the original defect: the mode published a baseline
// but applied hard-coded constants, so changing the pack changed nothing. Here the fixture
// baseline uses different numbers, and the race must follow them.
func TestBaselineIsTheSingleSourceOfTruth(t *testing.T) {
	// A deliberately different baseline: steady gains 10 instead of 3.
	custom := fixtureBaseline()
	custom["actions"] = []map[string]any{
		{"action": "steady", "label": "稳进", "positionGain": 10, "staminaDelta": 0},
		{"action": "sprint", "label": "冲刺", "positionGain": 5,
			"staminaDelta": -2, "minStamina": 2},
		{"action": "recover", "label": "恢复", "positionGain": 1, "staminaDelta": 2},
		{"action": "follow", "label": "跟跑", "positionGain": 4,
			"alternateGain": 2, "staminaDelta": 0, "leaderGap": true},
	}
	raw, err := json.Marshal(custom)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	pack := fakeContent{TrackSection: raw}

	prep, err := New().Prepare(context.Background(), modes.PrepareInput{
		Scope:   scope("custom-baseline-seed"),
		Subject: roster(2),
		Config:  json.RawMessage(`{}`),
		Content: pack,
	})
	if err != nil {
		t.Fatalf("Prepare refused the custom baseline: %v", err)
	}

	// Both teams play steady for one turn; the position must follow the pack's +10.
	prep = commitAndPrepare(t, prep, modes.Subject{ID: "t1", Kind: modes.SubjectPlayer},
		decisionAction(1, ActionSteady))
	prep = commitAndPrepare(t, prep, modes.Subject{ID: "t2", Kind: modes.SubjectPlayer},
		decisionAction(1, ActionSteady))

	view := project(t, prep, modes.Viewer{Subject: modes.Subject{ID: "t1", Kind: modes.SubjectPlayer},
		Role: "player"}, modes.PhaseCollecting)
	for _, point := range view.Standings {
		if point.Position != 10 {
			t.Fatalf("%s position after one steady turn = %d, want 10 (the pack's gain, not the constant)",
				point.SubjectID, point.Position)
		}
	}
}

// TestMalformedBaselineIsRefused asserts an incomplete baseline cannot start a round.
func TestMalformedBaselineIsRefused(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(map[string]any)
		want   string
	}{
		{name: "no actions", mutate: func(m map[string]any) { m["actions"] = []any{} },
			want: "omits action"},
		{name: "missing sprint", mutate: func(m map[string]any) {
			m["actions"] = []map[string]any{
				{"action": "steady", "positionGain": 3},
				{"action": "recover", "positionGain": 1, "staminaDelta": 2},
				{"action": "follow", "positionGain": 4, "alternateGain": 2},
			}
		}, want: "omits action"},
		{name: "unknown action", mutate: func(m map[string]any) {
			m["actions"] = []map[string]any{
				{"action": "teleport", "positionGain": 3},
				{"action": "steady", "positionGain": 3},
				{"action": "sprint", "positionGain": 5, "staminaDelta": -2, "minStamina": 2},
				{"action": "recover", "positionGain": 1, "staminaDelta": 2},
				{"action": "follow", "positionGain": 4, "alternateGain": 2},
			}
		}, want: "unknown action"},
		{name: "stamina cap below start", mutate: func(m map[string]any) {
			m["staminaCap"] = 2
			m["startStamina"] = 6
		}, want: "startStamina"},
		{name: "bad follow range", mutate: func(m map[string]any) {
			m["followMinGap"] = 3
			m["followMaxGap"] = 1
		}, want: "follow gap range"},
		{name: "no default action", mutate: func(m map[string]any) {
			m["defaultAction"] = "teleport"
		}, want: "defaultAction"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			base := fixtureBaseline()
			tc.mutate(base)
			raw, err := json.Marshal(base)
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}
			_, prepErr := New().Prepare(context.Background(), modes.PrepareInput{
				Scope:   scope("malformed-seed"),
				Subject: roster(2),
				Config:  json.RawMessage(`{}`),
				Content: fakeContent{TrackSection: raw},
			})
			if prepErr == nil {
				t.Fatalf("Prepare accepted a baseline with %s", tc.name)
			}
			if !strings.Contains(prepErr.Error(), tc.want) {
				t.Fatalf("error %q does not mention %q", prepErr, tc.want)
			}
		})
	}
}

// TestProvenanceIsPublished asserts the simulation marker reaches the player.
func TestProvenanceIsPublished(t *testing.T) {
	prep, pub := prepareRace(t, "prov-pub-seed", `{}`, roster(2))
	if pub.Provenance.Kind != ProvenanceAuthored {
		t.Fatalf("published provenance kind = %q", pub.Provenance.Kind)
	}
	if !pub.Provenance.NotOfficialData {
		t.Fatal("the published baseline does not mark itself as non-official data")
	}
	if !pub.Provenance.Simulation {
		t.Fatal("the published baseline does not mark itself as a simulation")
	}
	view := project(t, prep, modes.Viewer{Subject: modes.Subject{ID: "t1", Kind: modes.SubjectPlayer},
		Role: "player"}, modes.PhaseCollecting)
	if !view.Provenance.Simulation {
		t.Fatal("the projection does not carry the simulation marker")
	}
	if view.Provenance.Kind == "" || !view.Provenance.NotOfficialData {
		t.Fatalf("projection provenance = %+v", view.Provenance)
	}
}

// TestRequirementFieldsMatchWhatTheModeReads asserts the declaration matches the loader.
func TestRequirementFieldsMatchWhatTheModeReads(t *testing.T) {
	fields := New().Requirement().Fields
	if len(fields) != 1 || fields[0] != TrackSection {
		t.Fatalf("Requirement().Fields = %v, want exactly [%q]", fields, TrackSection)
	}
}

// TestConfigMustAgreeWithBaseline asserts a config that contradicts the loaded pack is
// refused.
//
// The turn count lives in the pack, so the two sources can disagree. Resolving that in favour
// of either one would silently change the race length, so Prepare refuses and says which
// values clashed.
func TestConfigMustAgreeWithBaseline(t *testing.T) {
	_, err := New().Prepare(context.Background(), modes.PrepareInput{
		Scope:   scope("disagree-seed"),
		Subject: roster(2),
		// The fixture baseline publishes 8 turns; asking for 7 contradicts it.
		Config:  json.RawMessage(`{"totalTurns":7}`),
		Content: packOf(),
	})
	if err == nil {
		t.Fatal("Prepare accepted a config that disagrees with the baseline")
	}
	if !strings.Contains(err.Error(), "disagrees with the published baseline") {
		t.Fatalf("error %q does not explain the disagreement", err)
	}
}

// TestBaselineAcceptsWrappedShape asserts the loader handles a pack that wraps the baseline
// under a key, which is the other shape a content build may publish.
func TestBaselineAcceptsWrappedShape(t *testing.T) {
	wrapped := map[string]any{
		"track": fixtureBaseline(),
	}
	raw, err := json.Marshal(wrapped)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	prep, err := New().Prepare(context.Background(), modes.PrepareInput{
		Scope:   scope("wrapped-seed"),
		Subject: roster(2),
		Config:  json.RawMessage(`{}`),
		Content: fakeContent{TrackSection: raw},
	})
	if err != nil {
		t.Fatalf("Prepare refused a wrapped baseline: %v", err)
	}
	if prep.QuestionCount != TotalTurns {
		t.Fatalf("questionCount = %d, want %d", prep.QuestionCount, TotalTurns)
	}
}

// TestBaselineRejectsUndecodablePayload asserts a section that is neither shape is refused.
func TestBaselineRejectsUndecodablePayload(t *testing.T) {
	for _, body := range []string{`"just a string"`, `[1,2,3]`, `{"track":[]}`, `{}`} {
		t.Run(body, func(t *testing.T) {
			_, err := New().Prepare(context.Background(), modes.PrepareInput{
				Scope:   scope("bad-payload-seed"),
				Subject: roster(2),
				Config:  json.RawMessage(`{}`),
				Content: fakeContent{TrackSection: json.RawMessage(body)},
			})
			if err == nil {
				t.Fatalf("Prepare accepted the baseline payload %s", body)
			}
		})
	}
}

// TestQuestionCountFollowsTheBaseline asserts QuestionCount is the loaded baseline's turn
// count, not a hard-coded constant.
//
// MinItems gates on QuestionCount, so a mismatch between "how many turns the pack publishes"
// and "how many the admission reports" is exactly the kind of drift that makes a mode look
// available while it cannot start. Using a ten-turn fixture proves the value tracks content.
func TestQuestionCountFollowsTheBaseline(t *testing.T) {
	// Default fixture: 8 turns.
	prep, pub := prepareRace(t, "qc-seed", `{}`, roster(2))
	if prep.QuestionCount != pub.Track.TotalTurns {
		t.Fatalf("questionCount = %d, baseline says %d turns",
			prep.QuestionCount, pub.Track.TotalTurns)
	}

	// A ten-turn baseline must report ten.
	ten := fixtureBaseline()
	ten["totalTurns"] = 10
	raw, err := json.Marshal(ten)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	prepTen, err := New().Prepare(context.Background(), modes.PrepareInput{
		Scope:   scope("qc-ten-seed"),
		Subject: roster(2),
		// The config must agree with the published baseline, so ask for ten turns too.
		Config:  json.RawMessage(`{"totalTurns":10}`),
		Content: fakeContent{TrackSection: raw},
	})
	if err != nil {
		t.Fatalf("Prepare refused a ten-turn baseline: %v", err)
	}
	if prepTen.QuestionCount != 10 {
		t.Fatalf("questionCount = %d for a ten-turn baseline, want 10", prepTen.QuestionCount)
	}
}

// TestProvenanceFieldsAreActuallyEnforced asserts every provenance field the mode declares is
// actually READ and validated, not merely documented.
//
// This is the structural counterpart to TestRequirementFieldsMatchWhatTheModeReads. A field that
// appears in the type but is never checked is worse than a missing field: it looks like a
// guarantee. The table below drops or corrupts each field in turn and asserts Prepare refuses.
func TestProvenanceFieldsAreActuallyEnforced(t *testing.T) {
	// base returns a baseline with a valid provenance block.
	base := func() map[string]any {
		m := fixtureBaseline()
		return m
	}

	cases := []struct {
		name   string
		mutate func(map[string]any)
		want   string
	}{
		{
			name: "kind is read",
			mutate: func(m map[string]any) {
				m["provenance"].(map[string]any)["kind"] = "nonsense"
			},
			want: "unknown provenance kind",
		},
		{
			name: "notOfficialData is read",
			mutate: func(m map[string]any) {
				m["provenance"].(map[string]any)["notOfficialData"] = false
			},
			want: "must set notOfficialData=true",
		},
		{
			name: "author is read",
			mutate: func(m map[string]any) {
				m["provenance"].(map[string]any)["author"] = ""
			},
			want: "no author",
		},
		{
			name: "basis is read",
			mutate: func(m map[string]any) {
				m["provenance"].(map[string]any)["basis"] = ""
			},
			want: "no basis",
		},
		{
			name: "simulation is read",
			mutate: func(m map[string]any) {
				m["provenance"].(map[string]any)["simulation"] = false
			},
			want: "entertainment simulation",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := base()
			tc.mutate(m)
			raw, err := json.Marshal(m)
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}
			_, prepErr := New().Prepare(context.Background(), modes.PrepareInput{
				Scope:   scope("enforced-seed"),
				Subject: roster(2),
				Config:  json.RawMessage(`{}`),
				Content: fakeContent{TrackSection: raw},
			})
			if prepErr == nil {
				t.Fatalf("Prepare accepted a baseline where %s was not enforced", tc.name)
			}
			if !strings.Contains(prepErr.Error(), tc.want) {
				t.Fatalf("error %q does not mention %q", prepErr, tc.want)
			}
		})
	}

	// The valid block must pass, or the test above proves nothing.
	raw, err := json.Marshal(base())
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if _, prepErr := New().Prepare(context.Background(), modes.PrepareInput{
		Scope:   scope("enforced-seed"),
		Subject: roster(2),
		Config:  json.RawMessage(`{}`),
		Content: fakeContent{TrackSection: raw},
	}); prepErr != nil {
		t.Fatalf("the valid provenance block was refused: %v", prepErr)
	}
}

// TestMinEntitiesIsAPoolFloorNotATableSize asserts Requirement().MinEntities counts content
// items, not players.
//
// The two questions are different: "how many players may sit down" belongs to the room layer
// (it answers with not-enough-participants), while "how many content items must the pack
// provide" is what the availability gate compares QuestionCount against. Mixing them forced
// this mode to report a turn count as its pool size, contradicting every other mode.
func TestMinEntitiesIsAPoolFloorNotATableSize(t *testing.T) {
	req := New().Requirement()
	if req.MinEntities != 1 {
		t.Fatalf("MinEntities = %d; a round needs exactly one published baseline, so the pool "+
			"floor is 1", req.MinEntities)
	}
	// The table size requirement still exists, but it lives on MinSubjects where the room layer
	// reads it.
	if New().MinSubjects() < 1 {
		t.Fatalf("MinSubjects = %d, must be at least 1", New().MinSubjects())
	}
	// A single published baseline must satisfy the gate.
	if req.MinEntities > 1 {
		t.Fatal("a pool of one baseline would not satisfy the gate")
	}
}
