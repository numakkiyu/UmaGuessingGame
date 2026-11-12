package strategyrace

// This file holds the pure rule simulation for G10 回合制策略赛道 .
//
// Everything here is a fold over the committed action log. Positions and stamina
// are never stored incrementally: they are always re-derived from the log, so a
// retried transaction cannot double-apply a turn and two different submission
// orders cannot produce two different races. That is the structural answer to the
// "重复结算" and "遍历顺序无关" requirements, not a defensive check.

import (
	"fmt"
	"sort"
)

// Baseline parameters . They are constants rather than configuration
// because the first version must stay explainable: a balance change is a new
// rules version, never a silent per-round tweak.
const (
	// TotalTurns is the number of decision rounds in one race.
	TotalTurns = 8
	// DecisionSeconds is the decision window per turn.
	DecisionSeconds = 20
	// StartPosition is where every marker begins.
	StartPosition = 0
	// StartStamina is the opening stamina.
	StartStamina = 6
	// StaminaCap is the maximum stamina a marker can hold.
	StaminaCap = 8
	// SteadyGain is the steady action's position gain.
	SteadyGain = 3
	// SprintGain is the sprint action's position gain.
	SprintGain = 5
	// SprintCost is the stamina sprint spends.
	SprintCost = 2
	// SprintMinStamina is the stamina sprint requires.
	SprintMinStamina = 2
	// RecoverGain is the recover action's position gain.
	RecoverGain = 1
	// RecoverStamina is the stamina recover restores, capped at StaminaCap.
	RecoverStamina = 2
	// FollowCloseGain applies when a team leads the follower by FollowMinGap
	// through FollowMaxGap cells at the start of the turn.
	FollowCloseGain = 4
	// FollowFarGain applies otherwise.
	FollowFarGain = 2
	// FollowMinGap and FollowMaxGap bound the leader gap that qualifies.
	FollowMinGap = 1
	FollowMaxGap = 3
)

// Action is one of the four documented decisions.
type Action string

const (
	// ActionSteady advances 3 and leaves stamina untouched.
	ActionSteady Action = "steady"
	// ActionSprint advances 5 and spends 2 stamina; needs at least 2.
	ActionSprint Action = "sprint"
	// ActionRecover advances 1 and restores 2 stamina up to the cap.
	ActionRecover Action = "recover"
	// ActionFollow advances 4 when a team is 1–3 cells ahead at turn start,
	// otherwise 2; stamina untouched.
	ActionFollow Action = "follow"
)

// DefaultAction is what a team that misses the deadline is assumed to have
// chosen. It is announced before the round starts (: 截止无有效行动默认稳进，
// 预先告知).
const DefaultAction = ActionSteady

// validAction reports whether a is one of the four documented actions.
func validAction(a Action) bool {
	switch a {
	case ActionSteady, ActionSprint, ActionRecover, ActionFollow:
		return true
	}
	return false
}

// actionRule is one row of the published action table. The round publishes it so
// the player sees the exact baseline before the first decision .
type actionRule struct {
	Action       Action `json:"action"`
	Label        string `json:"label"`
	PositionGain int    `json:"positionGain"`
	// AlternateGain is the follow action's gain when no team is 1–3 cells ahead.
	AlternateGain int `json:"alternateGain,omitempty"`
	StaminaDelta  int `json:"staminaDelta"`
	MinStamina    int `json:"minStamina,omitempty"`
	// LeaderGap reports that the gain depends on the turn-start snapshot.
	LeaderGap bool `json:"leaderGap,omitempty"`
}

// trackRules is the published baseline of one race.
type trackRules struct {
	TotalTurns      int          `json:"totalTurns"`
	DecisionSeconds int          `json:"decisionSeconds"`
	StartPosition   int          `json:"startPosition"`
	StartStamina    int          `json:"startStamina"`
	StaminaCap      int          `json:"staminaCap"`
	FollowMinGap    int          `json:"followMinGap"`
	FollowMaxGap    int          `json:"followMaxGap"`
	DefaultAction   Action       `json:"defaultAction"`
	Actions         []actionRule `json:"actions"`
	// TerrainChanges reports that the first version disables terrain changes so
	// the baseline stays explainable .
	TerrainChanges bool `json:"terrainChanges"`
	// RandomEvents reports that the first version has no random events; a later
	// version must publish a new rules version and a replayable seed instead.
	RandomEvents bool `json:"randomEvents"`
}

// ruleFor returns the published row for an action.
//
// The simulation reads its numbers from this row rather than from package constants: the
// baseline is content, and a round must run on the baseline its pack published. Reading a
// constant here would let a pack publish one thing and the simulation apply another, which
// is the "declared but not used" defect class this project has already hit twice.
func (t trackRules) ruleFor(a Action) (actionRule, error) {
	for _, r := range t.Actions {
		if r.Action == a {
			return r, nil
		}
	}
	return actionRule{}, fmt.Errorf(
		"strategyrace: the published baseline has no row for action %q", a)
}

// validate checks that a published baseline is complete and internally consistent.
//
// A baseline is content, so a malformed one is a content error and must be refused rather
// than silently defaulted: defaulting would make a broken pack look playable.
func (t trackRules) validate() error {
	if t.TotalTurns < 1 || t.TotalTurns > 32 {
		return fmt.Errorf("strategyrace: totalTurns %d outside [1,32]", t.TotalTurns)
	}
	if t.DecisionSeconds < 5 || t.DecisionSeconds > 300 {
		return fmt.Errorf("strategyrace: decisionSeconds %d outside [5,300]", t.DecisionSeconds)
	}
	if t.StaminaCap < 1 {
		return fmt.Errorf("strategyrace: staminaCap %d must be positive", t.StaminaCap)
	}
	if t.StartStamina < 0 || t.StartStamina > t.StaminaCap {
		return fmt.Errorf(
			"strategyrace: startStamina %d outside [0,%d]", t.StartStamina, t.StaminaCap)
	}
	if t.FollowMinGap < 1 || t.FollowMaxGap < t.FollowMinGap {
		return fmt.Errorf(
			"strategyrace: follow gap range [%d,%d] is invalid", t.FollowMinGap, t.FollowMaxGap)
	}
	// Every documented action must be present exactly once, or a player could choose an
	// action the simulation cannot resolve.
	seen := make(map[Action]struct{}, len(t.Actions))
	for _, r := range t.Actions {
		if !validAction(r.Action) {
			return fmt.Errorf("strategyrace: the baseline carries unknown action %q", r.Action)
		}
		if _, dup := seen[r.Action]; dup {
			return fmt.Errorf("strategyrace: the baseline lists action %q twice", r.Action)
		}
		seen[r.Action] = struct{}{}
		if r.PositionGain < 0 {
			return fmt.Errorf("strategyrace: action %q has a negative position gain", r.Action)
		}
		if r.Action == ActionFollow && r.AlternateGain <= 0 {
			return fmt.Errorf("strategyrace: the follow row carries no alternate gain")
		}
		if r.Action == ActionSprint && r.MinStamina <= 0 {
			return fmt.Errorf("strategyrace: the sprint row carries no minimum stamina")
		}
	}
	for _, required := range []Action{ActionSteady, ActionSprint, ActionRecover, ActionFollow} {
		if _, ok := seen[required]; !ok {
			return fmt.Errorf("strategyrace: the baseline omits action %q", required)
		}
	}
	if !validAction(t.DefaultAction) {
		return fmt.Errorf("strategyrace: defaultAction %q is not a documented action", t.DefaultAction)
	}
	// The timeout action must be legal at zero stamina, with no hidden cost.
	// A sprint default can otherwise corrupt a race after enough missed turns.
	def, err := t.ruleFor(t.DefaultAction)
	if err != nil {
		return err
	}
	if t.DefaultAction == ActionSprint || def.MinStamina > 0 || def.StaminaDelta < 0 {
		return fmt.Errorf("strategyrace: defaultAction must be available without stamina")
	}
	return nil
}

// buildTrackRules assembles the published baseline for a race of total turns.
func buildTrackRules(total int) trackRules {
	return trackRules{
		TotalTurns:      total,
		DecisionSeconds: DecisionSeconds,
		StartPosition:   StartPosition,
		StartStamina:    StartStamina,
		StaminaCap:      StaminaCap,
		FollowMinGap:    FollowMinGap,
		FollowMaxGap:    FollowMaxGap,
		DefaultAction:   DefaultAction,
		Actions: []actionRule{
			{Action: ActionSteady, Label: "稳进", PositionGain: SteadyGain, StaminaDelta: 0},
			{Action: ActionSprint, Label: "冲刺", PositionGain: SprintGain,
				StaminaDelta: -SprintCost, MinStamina: SprintMinStamina},
			{Action: ActionRecover, Label: "恢复", PositionGain: RecoverGain,
				StaminaDelta: RecoverStamina},
			{Action: ActionFollow, Label: "跟跑", PositionGain: FollowCloseGain,
				AlternateGain: FollowFarGain, StaminaDelta: 0, LeaderGap: true},
		},
		TerrainChanges: false,
		RandomEvents:   false,
	}
}

// committedAction is one persisted decision.
type committedAction struct {
	Turn      int    `json:"turn"`
	SubjectID string `json:"subjectId"`
	Action    Action `json:"action"`
	Defaulted bool   `json:"defaulted,omitempty"`
}

// teamPoint is a marker's position and stamina at one instant.
type teamPoint struct {
	SubjectID string `json:"subjectId"`
	Position  int    `json:"position"`
	Stamina   int    `json:"stamina"`
}

// teamApplied is one team's outcome for one turn.
type teamApplied struct {
	SubjectID string `json:"subjectId"`
	// Action is the action that was applied.
	Action Action `json:"action"`
	// Defaulted reports that the team missed the window and DefaultAction was
	// used instead .
	Defaulted bool `json:"defaulted,omitempty"`
	// LeaderGap is true when the follow action found a qualifying leader. It is
	// meaningful only for ActionFollow and is computed from the turn-start
	// snapshot, never from the order teams were visited.
	LeaderGap bool `json:"leaderGap,omitempty"`
	// PositionGain and StaminaDelta are the deltas actually applied.
	PositionGain int `json:"positionGain"`
	StaminaDelta int `json:"staminaDelta"`
	// Position and Stamina are the values after the turn.
	Position int `json:"position"`
	Stamina  int `json:"stamina"`
}

// turnResult is the fully resolved state of one turn.
type turnResult struct {
	Turn int `json:"turn"`
	// Start is the snapshot taken before any team moved. Follow eligibility and
	// every delta are computed from it, so the outcome cannot depend on the
	// order teams are visited.
	Start []teamPoint `json:"start"`
	// Applied holds one entry per roster team, in roster order.
	Applied []teamApplied `json:"applied"`
}

// raceResult is the whole replay.
type raceResult struct {
	// Turns are the resolved turns, in ascending order.
	Turns []turnResult `json:"turns"`
	// Final holds one entry per roster team, in roster order.
	Final []teamPoint `json:"final"`
	// CurrentTurn is the turn currently accepting decisions; it is TotalTurns+1
	// once every turn is complete.
	CurrentTurn int `json:"currentTurn"`
	// Complete reports that every turn has been resolved.
	Complete bool `json:"complete"`
}

// committedAt returns the committed action of subject at turn, if any.
func (p publicState) committedAt(turn int, subjectID string) (Action, bool) {
	for _, ca := range p.Committed {
		if ca.Turn == turn && ca.SubjectID == subjectID {
			return ca.Action, true
		}
	}
	return "", false
}

// fullyCommitted reports whether every roster subject committed a legal action
// at turn.
func (p publicState) fullyCommitted(turn int) bool {
	if len(p.Subjects) == 0 {
		return false
	}
	for _, entry := range p.Subjects {
		if _, ok := p.committedAt(turn, entry.SubjectID); !ok {
			return false
		}
	}
	return true
}

// currentTurn derives the open turn from the committed log. It is a pure
// function of the log, so a duplicate commit cannot advance the race twice.
func (p publicState) currentTurn() int {
	turn := 1
	for turn <= p.Config.TotalTurns && p.fullyCommitted(turn) {
		turn++
	}
	return turn
}

// resolveRace replays the race.
//
// When finalize is false only fully-committed turns are resolved, which is the
// correct view while a turn is still open: showing a position that already
// includes the open turn would reveal how the unfinished turn will end.
//
// When finalize is true (the round is locked, revealed or settled) every
// remaining turn is resolved, using DefaultAction for every team that did not
// submit . Resolving only the open turn would
// leave the race incomplete, and the rules define a fixed eight-turn race rather
// than a race that ends when the first team stops submitting. Because the default
// is the same "steady" for everybody, a turn nobody submitted cannot reorder the
// markers.
func resolveRace(p publicState, finalize bool) (raceResult, error) {
	total := p.Config.TotalTurns
	if total <= 0 {
		total = TotalTurns
	}
	open := p.currentTurn()

	limit := open - 1
	if finalize {
		limit = total
	}
	if limit > total {
		limit = total
	}

	res := raceResult{
		Turns:       make([]turnResult, 0, limit),
		Final:       make([]teamPoint, 0, len(p.Subjects)),
		CurrentTurn: open,
		Complete:    open > total,
	}

	points := initialPoints(p.Track, p.Subjects)

	for turn := 1; turn <= limit; turn++ {
		// Snapshot BEFORE any move: the only basis for follow eligibility and
		// for every delta .
		snapshot := make([]teamPoint, len(points))
		copy(snapshot, points)

		applied := make([]teamApplied, len(points))
		for i, point := range snapshot {
			action, ok := p.committedAt(turn, point.SubjectID)
			defaulted := false
			if !ok {
				action = p.Track.DefaultAction
				defaulted = true
			}
			for _, ca := range p.Committed {
				if ca.Turn == turn && ca.SubjectID == point.SubjectID {
					defaulted = ca.Defaulted
					break
				}
			}
			if !validAction(action) {
				return raceResult{}, fmt.Errorf(
					"strategyrace: turn %d subject %q carries unknown action %q",
					turn, point.SubjectID, action)
			}
			entry, err := applyAction(p.Track, action, point, snapshot)
			if err != nil {
				return raceResult{}, fmt.Errorf(
					"strategyrace: turn %d subject %q: %w", turn, point.SubjectID, err)
			}
			entry.Defaulted = defaulted
			applied[i] = entry
		}

		// Fold every delta only after all of them were computed from the same
		// snapshot, so visiting teams in another order cannot change the result.
		for i := range points {
			points[i].Position = applied[i].Position
			points[i].Stamina = applied[i].Stamina
		}
		res.Turns = append(res.Turns, turnResult{
			Turn:    turn,
			Start:   snapshot,
			Applied: applied,
		})
	}

	res.Final = append(res.Final, points...)
	return res, nil
}

// initialPoints builds the opening markers in roster order, from the published baseline.
func initialPoints(t trackRules, subjects []rosterEntry) []teamPoint {
	out := make([]teamPoint, 0, len(subjects))
	for _, entry := range subjects {
		out = append(out, teamPoint{
			SubjectID: entry.SubjectID,
			Position:  t.StartPosition,
			Stamina:   t.StartStamina,
		})
	}
	return out
}

// staminaAtOpenTurn returns a team's stamina at the START of the open turn.
// Apply validates a sprint against this value, which is derived from the log
// rather than trusted from a client-supplied state.
func (p publicState) staminaAtOpenTurn(subjectID string) (int, error) {
	res, err := resolveRace(p, false)
	if err != nil {
		return 0, err
	}
	for _, point := range res.Final {
		if point.SubjectID == subjectID {
			return point.Stamina, nil
		}
	}
	return 0, fmt.Errorf("strategyrace: subject %q is not on this track", subjectID)
}

// positionAtOpenTurn returns a team's position at the START of the open turn.
func (p publicState) positionAtOpenTurn(subjectID string) (int, error) {
	res, err := resolveRace(p, false)
	if err != nil {
		return 0, err
	}
	for _, point := range res.Final {
		if point.SubjectID == subjectID {
			return point.Position, nil
		}
	}
	return 0, fmt.Errorf("strategyrace: subject %q is not on this track", subjectID)
}

// applyAction computes one team's outcome from the turn-start snapshot.
//
// Every number comes from the published baseline (t), never from a package constant. That
// is what makes the pack the single source of truth for the race: a round runs on the
// baseline its pack published, and a pack that publishes a different baseline produces a
// different race.
func applyAction(t trackRules, action Action, self teamPoint, snapshot []teamPoint) (teamApplied, error) {
	rule, err := t.ruleFor(action)
	if err != nil {
		return teamApplied{}, err
	}
	out := teamApplied{SubjectID: self.SubjectID, Action: action}
	switch action {
	case ActionSteady:
		out.PositionGain = rule.PositionGain
		out.StaminaDelta = rule.StaminaDelta
	case ActionSprint:
		if self.Stamina < rule.MinStamina {
			// 体力不足的冲刺拒绝. Apply rejects it before it can be
			// committed; reaching this branch means the stored log is corrupt,
			// so the replay fails loudly instead of grading a wrong race.
			return teamApplied{}, fmt.Errorf(
				"sprint needs %d stamina but the team has %d", rule.MinStamina, self.Stamina)
		}
		out.PositionGain = rule.PositionGain
		out.StaminaDelta = rule.StaminaDelta
	case ActionRecover:
		out.PositionGain = rule.PositionGain
		// The gain is capped at the stamina ceiling, so the delta is what actually fits
		// rather than a flat +2 that could exceed the cap.
		room := t.StaminaCap - self.Stamina
		if room < 0 {
			room = 0
		}
		gain := rule.StaminaDelta
		if gain > room {
			gain = room
		}
		if gain < 0 {
			gain = 0
		}
		out.StaminaDelta = gain
	case ActionFollow:
		if hasQualifyingLeader(t, self.SubjectID, self.Position, snapshot) {
			out.PositionGain = rule.PositionGain
			out.LeaderGap = true
		} else {
			out.PositionGain = rule.AlternateGain
		}
		out.StaminaDelta = rule.StaminaDelta
	default:
		return teamApplied{}, fmt.Errorf("unknown action %q", action)
	}

	out.Position = self.Position + out.PositionGain
	out.Stamina = self.Stamina + out.StaminaDelta
	if out.Stamina > t.StaminaCap {
		out.Stamina = t.StaminaCap
	}
	if out.Stamina < 0 {
		out.Stamina = 0
	}
	return out, nil
}

// hasQualifyingLeader reports whether any OTHER team leads self by a gap inside the
// published follow range, judged from the turn-start snapshot.
func hasQualifyingLeader(t trackRules, selfID string, selfPosition int, snapshot []teamPoint) bool {
	for _, other := range snapshot {
		if other.SubjectID == selfID {
			continue
		}
		gap := other.Position - selfPosition
		if gap >= t.FollowMinGap && gap <= t.FollowMaxGap {
			return true
		}
	}
	return false
}

// rankedTeam is one team's final standing.
type rankedTeam struct {
	SubjectID string `json:"subjectId"`
	Position  int    `json:"position"`
	Stamina   int    `json:"stamina"`
	Rank      int    `json:"rank"`
	// Shared reports that another team finished on the same position. Ties share
	// a rank and are never broken by milliseconds .
	Shared bool `json:"shared"`
	// ScoreUnits is the settled competitive score.
	ScoreUnits int `json:"scoreUnits"`
}

// rankTeams assigns competition ranks by final position.
//
// Ordering is by position descending then subject id ascending purely so the
// output is byte-stable; the rank itself only depends on the position, so two
// teams on the same position always share it.
func rankTeams(final []teamPoint, teams int, competitive bool) []rankedTeam {
	out := make([]rankedTeam, 0, len(final))
	// Count how many teams sit strictly ahead of each team: that is the rank.
	ahead := make([]int, len(final))
	same := make([]int, len(final))
	for i := range final {
		for j := range final {
			if i == j {
				continue
			}
			switch {
			case final[j].Position > final[i].Position:
				ahead[i]++
			case final[j].Position == final[i].Position:
				same[i]++
			}
		}
	}
	for i := range final {
		rt := rankedTeam{
			SubjectID: final[i].SubjectID,
			Position:  final[i].Position,
			Stamina:   final[i].Stamina,
			Rank:      ahead[i] + 1,
			Shared:    same[i] > 0,
		}
		if competitive && teams >= 2 {
			// floor(100 × (n-r) / (n-1)) display points, stored as scoreUnits.
			display := 100 * (teams - rt.Rank) / (teams - 1)
			if display < 0 {
				display = 0
			}
			rt.ScoreUnits = display * 100
		}
		out = append(out, rt)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Position != out[j].Position {
			return out[i].Position > out[j].Position
		}
		return out[i].SubjectID < out[j].SubjectID
	})
	return out
}
