// Package strategyrace implements G10 回合制策略赛道 .
//
// Two to eight teams each drive one marker over eight turns of twenty seconds.
// Every team picks one of four actions per turn; all decisions are revealed
// together. Positions and stamina start at 0 and 6 (cap 8), and the four actions
// are:
//
//	steady position +3, stamina unchanged
//	sprint position +5, stamina -2 (needs at least 2 at turn start)
//	recover position +1, stamina +2 capped at the ceiling
//	follow position +4 when some team is 1–3 cells ahead at turn start,
//
// otherwise +2; stamina unchanged
//
// Follow eligibility is judged from the turn-start snapshot only, so the result
// cannot depend on the order teams are visited. A team that misses the deadline
// is treated as having chosen steady, which is announced before the round.
//
// Scoring: a single-team practice round produces no competitive rank at all. With
// n≥2 teams, each team's display score is floor(100×(n-r)/(n-1)) where r is its
// shared competition rank; tied positions share a rank and therefore a score.
package strategyrace

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/bhcn/umaguessinggame/internal/modes"
	"github.com/bhcn/umaguessinggame/internal/modes/modeutil"
)

// ModeID is the stable mode identifier (contract ModeId enum).
const ModeID = "strategy-race"

// RulesVersion is the rule revision this implementation satisfies.
const RulesVersion = "2.0.0-draft.1"

// Action kinds.
//
// The value is the contract discriminator (ModeAction → StrategyAction), not an
// internal name: the discriminator is what selects the payload schema .
const (
	// ActionKindDecision submits one team's decision for one turn.
	ActionKindDecision = "strategy.action"
)

// ContractKinds returns the ModeAction discriminators this mode accepts.
func (*Mode) ContractKinds() []string { return []string{ActionKindDecision} }

// Rejection codes.
const (
	CodeInvalidPayload      = "invalid_payload"
	CodeUnknownAction       = "unknown_action"
	CodeUnknownDecision     = "unknown_decision"
	CodeTurnClosed          = "turn_closed"
	CodeTurnMismatch        = "turn_mismatch"
	CodeRaceFinished        = "race_finished"
	CodeInsufficientStamina = "insufficient_stamina"
	CodeDuplicateTurn       = "duplicate_turn"
	CodeNotOnTrack          = "not_on_track"
)

// Subject-count bounds .
const (
	MinTeams = 2
	MaxTeams = 8
	// PracticeTeams is the single-team practice form, which is allowed but is
	// explicitly non-competitive .
	PracticeTeams = 1
)

// Config is the round configuration.
type Config struct {
	// TotalTurns is the number of decision rounds. Defaults to 8.
	TotalTurns int `json:"totalTurns"`
	// DecisionSeconds is the per-turn window. Defaults to 20.
	DecisionSeconds int `json:"decisionSeconds"`
	// Practice marks the single-team form: no competitive score is produced.
	Practice bool `json:"practice"`
}

// decodeConfig parses and normalises a configuration blob. An absent blob means
// the documented defaults.
func decodeConfig(raw json.RawMessage) (Config, error) {
	cfg := Config{TotalTurns: TotalTurns, DecisionSeconds: DecisionSeconds}
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &cfg); err != nil {
			return Config{}, fmt.Errorf("strategyrace: invalid config: %w", err)
		}
	}
	if cfg.TotalTurns == 0 {
		cfg.TotalTurns = TotalTurns
	}
	if cfg.DecisionSeconds == 0 {
		cfg.DecisionSeconds = DecisionSeconds
	}
	// The race length is CONTENT, not a config knob: the pack publishes it in
	// rules.track and Prepare checks the config against it. The bound here is only a
	// sanity range, because ValidateConfig runs before any pack is available and cannot
	// know what the pack will say. A config that disagrees with the loaded baseline is
	// refused in Prepare, where both values are known.
	if cfg.TotalTurns < 1 || cfg.TotalTurns > 32 {
		return Config{}, fmt.Errorf(
			"strategyrace: totalTurns %d outside [1,32]", cfg.TotalTurns)
	}
	if cfg.DecisionSeconds < 1 || cfg.DecisionSeconds > 120 {
		return Config{}, fmt.Errorf(
			"strategyrace: decisionSeconds %d outside [1,120]", cfg.DecisionSeconds)
	}
	return cfg, nil
}

// rosterEntry is one participant recorded in the public prepared state.
type rosterEntry struct {
	SubjectID   string            `json:"subjectId"`
	SubjectKind modes.SubjectKind `json:"subjectKind"`
}

// publicState is the non-secret round state. It carries the whole committed
// action log, which is the authoritative race input.
type publicState struct {
	ModeID       string     `json:"modeId"`
	RulesVersion string     `json:"rulesVersion"`
	Config       Config     `json:"config"`
	Track        trackRules `json:"track"`
	// Provenance records that the baseline is an entertainment simulation rather than an
	// official game formula . It is published so a player and a host both see it.
	Provenance trackProvenance `json:"provenance"`
	Subjects   []rosterEntry   `json:"subjects"`
	// Competitive reports whether a competitive rank is produced. It is false
	// for the single-team practice form.
	Competitive bool `json:"competitive"`
	// Committed is the append-only action log, ordered by (turn, subjectId) so
	// the serialised form is stable regardless of arrival order.
	Committed []committedAction `json:"committed"`
}

// privateState is the server-only round state. This mode has no hidden answer
// material: every input is a player decision that is revealed together. The
// struct exists so the field is explicit rather than an empty blob.
type privateState struct {
	ModeID string `json:"modeId"`
	// Note documents why there is no secret here, so a future reader does not
	// assume the field was forgotten.
	Note string `json:"note"`
}

// Mode implements modes.Mode for G10.
type Mode struct{}

// New returns the strategy-race mode.
func New() *Mode { return &Mode{} }

// ID implements modes.Mode.
func (*Mode) ID() string { return ModeID }

// RulesVersion implements modes.Mode.
func (*Mode) RulesVersion() string { return RulesVersion }

// Title implements modes.Mode.
func (*Mode) Title() string { return "回合制策略赛道" }

// Summary implements modes.Mode.
func (*Mode) Summary() string {
	return "八回合同时揭晓的体力与位置博弈：稳进、冲刺、恢复、跟跑，位置最高者胜。"
}

// InformationStyles reports which information forms this mode implements.
func (*Mode) InformationStyles() []string {
	return []string{"closed"}
}

// ExpireTurn implements modes.TurnExpirer.
//
// The room layer calls this when a turn's clock runs out. Every team that has
// not committed the open turn is recorded as having taken the default action
// ("steady", announced before the round — : 截止无有效行动默认稳进).
// Recording it in the committed log rather than special-casing it later keeps the
// rest of the mode unchanged: resolveRace already treats "no commit" as the
// default, and currentTurn() advances as soon as every team has a commit.
//
// `more` reports whether any turn is still open after this one closes, so the
// room layer knows whether to start another turn clock or leave the round to run
// out its whole-round deadline.
func (*Mode) ExpireTurn(public json.RawMessage) (json.RawMessage, bool, error) {
	pub, err := decodePublic(public)
	if err != nil {
		return nil, false, err
	}
	total := pub.Config.TotalTurns
	if total <= 0 {
		total = TotalTurns
	}
	open := pub.currentTurn()
	if open > total {
		// Nothing left to expire.
		return public, false, nil
	}
	// Default every silent team for the open turn, in roster order so the log
	// stays deterministic regardless of who happened to submit.
	for _, entry := range pub.Subjects {
		if _, done := pub.committedAt(open, entry.SubjectID); done {
			continue
		}
		pub.Committed = append(pub.Committed, committedAction{
			Turn:      open,
			SubjectID: entry.SubjectID,
			Action:    pub.Track.DefaultAction,
			Defaulted: true,
		})
	}
	sortCommitted(pub.Committed)
	next := modeutil.Marshal(pub)
	return next, pub.currentTurn() <= total, nil
}

// ControlTurn changes only after every team has committed or the clock expires.
func (m *Mode) ControlTurn(public, _ json.RawMessage) (string, bool, error) {
	return m.OpenTurn(public)
}

// OpenTurn implements modes.TurnProgress without defaulting anyone's action.
func (*Mode) OpenTurn(public json.RawMessage) (string, bool, error) {
	pub, err := decodePublic(public)
	if err != nil {
		return "", false, err
	}
	turn := pub.currentTurn()
	return fmt.Sprint(turn), turn <= pub.Config.TotalTurns, nil
}

// RoundWindow implements modes.Mode.
//
// A race is turn-based: each turn gives every team decisionSeconds to choose, so
// the round window is the number of turns times that window. The track the
// players see states both numbers, and the round clock must agree with them —
// before this, the room layer knew nothing about decisionSeconds and ran the
// race under its five-minute default while the screen said "每回合 20 秒" .
//
// The per-turn window is also the deadline that closes a turn, because
// currentTurn() advances as soon as every team has committed; a turn that is
// still open when the window ends is resolved with the default action.
func (*Mode) RoundWindow(cfg json.RawMessage) time.Duration {
	c, err := decodeConfig(cfg)
	if err != nil {
		// An invalid config is rejected by ValidateConfig before this runs; a
		// zero result here means "no opinion" and the room default applies.
		return 0
	}
	return time.Duration(c.DecisionSeconds) * time.Second * time.Duration(c.TotalTurns)
}

// TurnWindow implements modes.Mode.
//
// One turn of a race is decisionSeconds. The room layer uses this as the
// authoritative per-turn clock: when it expires, every team that has not
// committed is treated as having chosen the default action and the turn closes.
// Before this existed the race only had a whole-race deadline, so a table where
// nobody submitted stayed on turn 1 for the entire 160 seconds.
func (*Mode) TurnWindow(cfg json.RawMessage) time.Duration {
	c, err := decodeConfig(cfg)
	if err != nil {
		return 0
	}
	return time.Duration(c.DecisionSeconds) * time.Second
}

// MinSubjects implements modes.Mode. The single-team form is a documented
// practice mode, so the bound is 1 and the competitive gate is Config.Practice.
func (*Mode) MinSubjects() int { return PracticeTeams }

// MaxSubjects implements modes.Mode.
func (*Mode) MaxSubjects() int { return MaxTeams }

// Requirement implements modes.Mode.
//
// strategy-race reads no scraped content: it is a rule simulation over player
// decisions . It therefore declares the minimum entity count it needs
// (two teams, or one for practice) and no content fields. The content gate
// still has to admit it, and this build's compiler refuses to admit a rules
// baseline that has not been playtested, so the lobby reports it unavailable
// with that reason rather than offering an unplaytested balance.
func (*Mode) Requirement() modes.ContentRequirement {
	// MinEntities is the smallest viable CONTENT POOL, not the smallest table. A round needs
	// exactly one published baseline, so the floor is 1. Using MinTeams here confused two
	// different questions — "how many players may sit down" (the room layer's
	// not-enough-participants check) and "how many content items must the pack provide" — and
	// the confusion forced the compiler to report QuestionCount as a turn count instead of a
	// pool size, which contradicted the meaning every other mode uses.
	//
	// Fields must name the section loadTrack reads. Declaring a different name (or none) is how
	// this project produced "available but cannot start" defects.
	return modes.ContentRequirement{
		Fields:      []string{TrackSection},
		MinEntities: 1,
		NeedsAssets: false,
	}
}

// TrackSection is the pack section carrying the published baseline.
const TrackSection = "rules.track"

// Provenance kinds accepted in the baseline. Only these three values pass.
const (
	ProvenanceAuthored = "authored-design-value"
	ProvenanceDerived  = "derived-from-doc"
	ProvenanceOfficial = "official-data"
)

// trackProvenance records where a baseline came from.
//
// # What the program can and cannot check
//
// The rule layer verifies that Kind is one of the three enumerated values, that
// NotOfficialData is true for anything this project designed, and that Basis is non-empty.
// It CANNOT verify that Basis names a real document. That is a human review boundary and is
// recorded here rather than papered over — this project already shipped a review ledger
// whose "source" held a character name instead of a citation.
type trackProvenance struct {
	Kind            string `json:"kind"`
	NotOfficialData bool   `json:"notOfficialData"`
	Author          string `json:"author"`
	Basis           string `json:"basis"`
	// Simulation marks the baseline as an entertainment simulation rather than an official
	// game formula .
	Simulation bool `json:"simulation"`
}

// validate reports whether the block may be published.
func (p trackProvenance) validate() error {
	switch p.Kind {
	case ProvenanceAuthored, ProvenanceDerived, ProvenanceOfficial:
	default:
		return fmt.Errorf("unknown provenance kind %q", p.Kind)
	}
	if p.Kind != ProvenanceOfficial && !p.NotOfficialData {
		return fmt.Errorf("provenance kind %q must set notOfficialData=true", p.Kind)
	}
	if p.Author == "" {
		return fmt.Errorf("provenance carries no author")
	}
	if p.Basis == "" {
		return fmt.Errorf("provenance carries no basis")
	}
	// The baseline is an entertainment simulation. Requiring the flag makes it impossible
	// to publish one that claims to be an official formula.
	if !p.Simulation {
		return fmt.Errorf(
			"provenance must set simulation=true: the race baseline is an entertainment " +
				"simulation, not an official game formula")
	}
	return nil
}

// loadTrack reads the published baseline from the pack.
//
// There is deliberately no built-in fallback. A fallback would make "the compiler produced
// no baseline" look identical to "the mode works", which is the failure this project keeps
// hitting. A pack without this section means the mode cannot start, and Prepare says so.
func loadTrack(src modes.ContentSource) (trackRules, trackProvenance, error) {
	if src == nil {
		return trackRules{}, trackProvenance{}, fmt.Errorf(
			"strategyrace: no content source; %q is required and there is no built-in fallback",
			TrackSection)
	}
	raw, err := modeutil.Section(src, TrackSection)
	if err != nil {
		return trackRules{}, trackProvenance{}, fmt.Errorf(
			"strategyrace: %q is required: %w (there is no built-in fallback; the pack must "+
				"publish the race baseline)", TrackSection, err)
	}
	var t trackRules
	// The section may publish the baseline directly, or wrap it under one of the accepted
	// keys. modeutil.Records is not used for the wrapped case because it expects an ARRAY
	// of records, while a baseline is a single OBJECT: feeding it a wrapped object returns
	// no records and the baseline silently decodes as zero-valued, which then fails
	// validation with a confusing message. Handling both shapes explicitly avoids that.
	// The envelope is decoded unconditionally: even when the baseline decodes directly, the
	// provenance block is read through the same accessor.
	envelope, errEnv := modeutil.DecodeFieldMap(raw)
	if errEnv != nil {
		return trackRules{}, trackProvenance{}, fmt.Errorf(
			"strategyrace: decode %q: %w", TrackSection, errEnv)
	}
	decoded := false
	if err := json.Unmarshal(raw, &t); err == nil && t.TotalTurns > 0 {
		decoded = true
	}
	if !decoded {
		var inner modeutil.FieldMap
		for _, key := range []string{"track", "baseline"} {
			if obj, ok := envelope.Object(key); ok {
				inner = obj
				break
			}
		}
		if inner == nil {
			return trackRules{}, trackProvenance{}, fmt.Errorf(
				"strategyrace: %q publishes neither a baseline object nor a track/baseline wrapper",
				TrackSection)
		}
		t = trackRules{
			TotalTurns:      intOr(inner, 0, "totalTurns"),
			DecisionSeconds: intOr(inner, 0, "decisionSeconds"),
			StartPosition:   intOr(inner, 0, "startPosition"),
			StartStamina:    intOr(inner, 0, "startStamina"),
			StaminaCap:      intOr(inner, 0, "staminaCap"),
			FollowMinGap:    intOr(inner, 0, "followMinGap"),
			FollowMaxGap:    intOr(inner, 0, "followMaxGap"),
			DefaultAction:   Action(inner.Str("defaultAction")),
			TerrainChanges:  inner.Bool("terrainChanges"),
			RandomEvents:    inner.Bool("randomEvents"),
		}
		if list, ok := inner.List("actions"); ok {
			for _, a := range list {
				t.Actions = append(t.Actions, actionRule{
					Action:        Action(a.Str("action")),
					Label:         a.Str("label"),
					PositionGain:  intOr(a, 0, "positionGain"),
					AlternateGain: intOr(a, 0, "alternateGain"),
					StaminaDelta:  intOr(a, 0, "staminaDelta"),
					MinStamina:    intOr(a, 0, "minStamina"),
					LeaderGap:     a.Bool("leaderGap"),
				})
			}
		}
	}
	if err := t.validate(); err != nil {
		return trackRules{}, trackProvenance{}, err
	}

	// The provenance block lives beside the baseline, either at the top level or inside the
	// wrapper. Both shapes are checked with the same accessor.
	var prov trackProvenance
	var provMap modeutil.FieldMap
	if obj, ok := envelope.Object("provenance"); ok {
		provMap = obj
	} else {
		for _, key := range []string{"track", "baseline"} {
			inner, ok := envelope.Object(key)
			if !ok {
				continue
			}
			if obj, okInner := inner.Object("provenance"); okInner {
				provMap = obj
				break
			}
		}
	}
	if provMap != nil {
		prov = trackProvenance{
			Kind:            provMap.Str("kind"),
			NotOfficialData: provMap.Bool("notOfficialData", "not_official_data"),
			Author:          provMap.Str("author"),
			Basis:           provMap.Str("basis"),
			Simulation:      provMap.Bool("simulation"),
		}
	}
	if prov.Kind == "" {
		return trackRules{}, trackProvenance{}, fmt.Errorf(
			"strategyrace: %q carries no provenance block", TrackSection)
	}
	if err := prov.validate(); err != nil {
		return trackRules{}, trackProvenance{}, fmt.Errorf("strategyrace: %w", err)
	}
	return t, prov, nil
}

// intOr reads an integer field, falling back to def when absent.
func intOr(rec modeutil.FieldMap, def int, keys ...string) int {
	if v, ok := rec.Int(keys...); ok {
		return v
	}
	return def
}

// ValidateConfig implements modes.Mode.
func (*Mode) ValidateConfig(cfg json.RawMessage) error {
	_, err := decodeConfig(cfg)
	return err
}

// Prepare implements modes.Mode.
//
// Prepare binds the published track and its clock to the round configuration.
// It refuses a team count outside the documented range and a practice round
// that claims more than one team.
func (*Mode) Prepare(ctx context.Context, in modes.PrepareInput) (modes.Prepared, error) {
	if err := in.Scope.Validate(); err != nil {
		return modes.Prepared{}, err
	}
	cfg, err := decodeConfig(in.Config)
	if err != nil {
		return modes.Prepared{}, err
	}
	if err := ctx.Err(); err != nil {
		return modes.Prepared{}, err
	}

	roster := make([]rosterEntry, 0, len(in.Subject))
	seen := make(map[string]struct{}, len(in.Subject))
	for _, subj := range in.Subject {
		if err := subj.Validate(); err != nil {
			return modes.Prepared{}, err
		}
		if _, dup := seen[subj.ID]; dup {
			return modes.Prepared{}, fmt.Errorf("strategyrace: duplicate subject %q", subj.ID)
		}
		seen[subj.ID] = struct{}{}
		roster = append(roster, rosterEntry{SubjectID: subj.ID, SubjectKind: subj.Kind})
	}
	if len(roster) < PracticeTeams || len(roster) > MaxTeams {
		return modes.Prepared{}, fmt.Errorf(
			"strategyrace: %d teams outside [%d,%d]", len(roster), PracticeTeams, MaxTeams)
	}
	// A single team is the practice form and must be declared as such; a
	// competitive round needs at least two teams because the score is a rank.
	competitive := len(roster) >= MinTeams
	if !competitive && !cfg.Practice {
		return modes.Prepared{}, fmt.Errorf(
			"strategyrace: a %d-team round is the practice form and needs practice=true", len(roster))
	}
	if cfg.Practice && competitive {
		return modes.Prepared{}, errors.New(
			"strategyrace: practice=true is only valid for a single-team round")
	}

	// The baseline comes from the pack, and only from the pack.
	track, prov, err := loadTrack(in.Content)
	if err != nil {
		return modes.Prepared{}, err
	}
	// The published baseline fixes the race length. A config that disagrees is a
	// contradiction between two content sources, so it is refused rather than resolved in
	// favour of one of them.
	if track.TotalTurns != cfg.TotalTurns {
		return modes.Prepared{}, fmt.Errorf(
			"strategyrace: config totalTurns %d disagrees with the published baseline's %d",
			cfg.TotalTurns, track.TotalTurns)
	}

	if track.DecisionSeconds != cfg.DecisionSeconds {
		return modes.Prepared{}, fmt.Errorf(
			"strategyrace: config decisionSeconds %d disagrees with the published baseline's %d",
			cfg.DecisionSeconds, track.DecisionSeconds)
	}

	pub := publicState{
		ModeID:       ModeID,
		RulesVersion: RulesVersion,
		Config:       cfg,
		Track:        track,
		Provenance:   prov,
		Subjects:     roster,
		Competitive:  competitive,
		Committed:    []committedAction{},
	}
	priv := privateState{
		ModeID: ModeID,
		Note:   "strategy-race carries no hidden answer: every decision is revealed together",
	}
	return modes.Prepared{
		Public:  modeutil.Marshal(pub),
		Private: modeutil.Marshal(priv),
		// QuestionCount is the number of turns, which is what a host schedules.
		// QuestionCount is the number of turns, because a turn is what a player actually
		// experiences and what MinItems gates on. It is taken from the LOADED baseline, not
		// from the config or a constant, so a pack that publishes a ten-turn race reports
		// ten rather than a hard-coded eight.
		QuestionCount: track.TotalTurns,
	}, nil
}

// decisionPayload is the body of ActionKindDecision, matching the contract's
// StrategyAction schema: {kind, action, turn} with additionalProperties false.
type decisionPayload struct {
	Kind   string `json:"kind"`
	Action Action `json:"action"`
	Turn   int    `json:"turn"`
}

// decodePublic unmarshals the public state.
func decodePublic(raw json.RawMessage) (publicState, error) {
	var pub publicState
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &pub); err != nil {
			return publicState{}, fmt.Errorf("strategyrace: decode public state: %w", err)
		}
	}
	return pub, nil
}

// Apply implements modes.Mode. It performs no I/O and no content access.
//
// The per-subject state is deliberately empty: the authoritative race input is
// the round-level committed log, so a decision is validated against a replay of
// that log rather than against a client-visible counter. That is what makes a
// repeated turn impossible to double-apply.
func (*Mode) Apply(in modes.ApplyInput) (modes.Decision, error) {
	if in.Action.Kind != ActionKindDecision {
		return modes.Rejected(CodeUnknownAction,
			fmt.Sprintf("strategyrace: unsupported action %q", in.Action.Kind)), nil
	}
	if err := in.Action.Validate(); err != nil {
		return modes.Rejected(CodeInvalidPayload, err.Error()), nil
	}
	var payload decisionPayload
	if err := json.Unmarshal(in.Action.Payload, &payload); err != nil {
		return modes.Rejected(CodeInvalidPayload,
			"strategyrace: payload must carry action and turn"), nil
	}
	if payload.Kind != "" && payload.Kind != ActionKindDecision {
		return modes.Rejected(CodeInvalidPayload, fmt.Sprintf(
			"strategyrace: payload kind %q does not match action %q",
			payload.Kind, ActionKindDecision)), nil
	}
	if !validAction(payload.Action) {
		return modes.Rejected(CodeUnknownDecision, fmt.Sprintf(
			"strategyrace: unknown action %q", payload.Action)), nil
	}

	pub, err := decodePublic(in.Public)
	if err != nil {
		return modes.Decision{}, err
	}
	if !onTrack(pub.Subjects, in.Subject.ID) {
		return modes.Rejected(CodeNotOnTrack, fmt.Sprintf(
			"strategyrace: subject %q is not on this track", in.Subject.ID)), nil
	}

	open := pub.currentTurn()
	if open > pub.Config.TotalTurns {
		return modes.Rejected(CodeRaceFinished, "strategyrace: all turns are already resolved"), nil
	}
	// A decision for a past turn is refused rather than reinterpreted: the turn
	// has already been revealed and re-writing it would rewrite the race.
	if payload.Turn < open {
		return modes.Rejected(CodeTurnClosed, fmt.Sprintf(
			"strategyrace: turn %d is already resolved; the open turn is %d", payload.Turn, open)), nil
	}
	// A decision for a future turn is refused because follow eligibility depends
	// on the snapshot that only exists when the earlier turns are settled.
	if payload.Turn > open {
		return modes.Rejected(CodeTurnMismatch, fmt.Sprintf(
			"strategyrace: turn %d is not open yet; the open turn is %d", payload.Turn, open)), nil
	}
	if _, dup := pub.committedAt(payload.Turn, in.Subject.ID); dup {
		return modes.Rejected(CodeDuplicateTurn, fmt.Sprintf(
			"strategyrace: turn %d was already submitted", payload.Turn)), nil
	}

	stamina, err := pub.staminaAtOpenTurn(in.Subject.ID)
	if err != nil {
		return modes.Decision{}, err
	}
	position, err := pub.positionAtOpenTurn(in.Subject.ID)
	if err != nil {
		return modes.Decision{}, err
	}
	// Stamina shortage is rejected with its own code so the client can explain
	// it; the decision is not silently downgraded to steady (: 体力不足的
	// 冲刺拒绝).
	rule, err := pub.Track.ruleFor(payload.Action)
	if err != nil {
		return modes.Decision{}, err
	}
	if payload.Action == ActionSprint && stamina < rule.MinStamina {
		return modes.Rejected(CodeInsufficientStamina, fmt.Sprintf(
			"strategyrace: sprint needs %d stamina but you have %d",
			rule.MinStamina, stamina)), nil
	}

	// Project what this decision would do, from the current snapshot, so the
	// feedback explains the rule without revealing anybody else's choice.
	snapshot := make([]teamPoint, 0, len(pub.Subjects))
	res, err := resolveRace(pub, false)
	if err != nil {
		return modes.Decision{}, err
	}
	snapshot = append(snapshot, res.Final...)
	self := teamPoint{SubjectID: in.Subject.ID, Position: position, Stamina: stamina}
	projected, err := applyAction(pub.Track, payload.Action, self, snapshot)
	if err != nil {
		return modes.Decision{}, err
	}

	feedback := modeutil.Marshal(struct {
		Turn         int    `json:"turn"`
		Action       Action `json:"action"`
		PositionGain int    `json:"positionGain"`
		StaminaDelta int    `json:"staminaDelta"`
		// LeaderGap explains why a follow earned 4 rather than 2. It reports the
		// existence of a qualifying leader, never which team it is.
		LeaderGap bool `json:"leaderGap,omitempty"`
		// ProjectedPosition and ProjectedStamina are what this team's own marker
		// would reach if the turn resolved with this decision and the other
		// teams' decisions as currently known. They are labelled as projections
		// because the turn is not resolved yet.
		ProjectedPosition int `json:"projectedPosition"`
		ProjectedStamina  int `json:"projectedStamina"`
		// Note carries the published baseline reminder.
		Note string `json:"note"`
	}{
		Turn:              payload.Turn,
		Action:            payload.Action,
		PositionGain:      projected.PositionGain,
		StaminaDelta:      projected.StaminaDelta,
		LeaderGap:         projected.LeaderGap,
		ProjectedPosition: projected.Position,
		ProjectedStamina:  projected.Stamina,
		Note:              "positions are revealed together when the turn closes",
	})

	dec := modes.Accepted(feedback, nil)
	return dec, nil
}

// onTrack reports whether subjectID is on the roster.
func onTrack(roster []rosterEntry, subjectID string) bool {
	for _, entry := range roster {
		if entry.SubjectID == subjectID {
			return true
		}
	}
	return false
}

// CommitShared implements modeutil.SharedStateMode.
//
// The room layer calls this inside its write transaction after Apply accepted the
// action, and persists the returned public state. The fold appends the decision to
// the committed log; because the log is append-only and keyed by (turn, subject),
// a retried transaction that replays the same accepted action produces the same
// log instead of a duplicate turn.
func (*Mode) CommitShared(in modes.ApplyInput, dec modes.Decision) (json.RawMessage, json.RawMessage, error) {
	if !dec.Accepted {
		return nil, nil, errors.New("strategyrace: cannot commit a rejected action")
	}
	pub, err := decodePublic(in.Public)
	if err != nil {
		return nil, nil, err
	}
	var payload decisionPayload
	if err := json.Unmarshal(in.Action.Payload, &payload); err != nil {
		return nil, nil, fmt.Errorf("strategyrace: decode decision payload: %w", err)
	}
	if !validAction(payload.Action) {
		return nil, nil, fmt.Errorf("strategyrace: unknown action %q", payload.Action)
	}
	if !onTrack(pub.Subjects, in.Subject.ID) {
		return nil, nil, fmt.Errorf("strategyrace: subject %q is not on this track", in.Subject.ID)
	}
	// Idempotent append: a duplicate commit is a no-op rather than a second turn.
	if _, dup := pub.committedAt(payload.Turn, in.Subject.ID); dup {
		return modeutil.Marshal(pub), in.Private, nil
	}
	pub.Committed = append(pub.Committed, committedAction{
		Turn:      payload.Turn,
		SubjectID: in.Subject.ID,
		Action:    payload.Action,
	})
	// Keep the log in a canonical order so the serialised state is byte-stable
	// regardless of which team committed first .
	sortCommitted(pub.Committed)
	return modeutil.Marshal(pub), in.Private, nil
}

// sortCommitted orders the log by (turn, subjectId).
func sortCommitted(log []committedAction) {
	// Insertion sort keeps this dependency-free and is fine for 8×8 entries.
	for i := 1; i < len(log); i++ {
		for j := i; j > 0 && lessCommitted(log[j], log[j-1]); j-- {
			log[j], log[j-1] = log[j-1], log[j]
		}
	}
}

// lessCommitted reports whether a sorts before b.
func lessCommitted(a, b committedAction) bool {
	if a.Turn != b.Turn {
		return a.Turn < b.Turn
	}
	return a.SubjectID < b.SubjectID
}

// projection is the viewer-specific round state.
type projection struct {
	ModeID       string          `json:"modeId"`
	RulesVersion string          `json:"rulesVersion"`
	Phase        modes.Phase     `json:"phase"`
	Config       Config          `json:"config"`
	Track        trackRules      `json:"track"`
	Provenance   trackProvenance `json:"provenance"`
	Competitive  bool            `json:"competitive"`
	CurrentTurn  int             `json:"currentTurn"`
	// Standings is the public scoreboard: every marker's position and stamina at
	// the start of the open turn. It never includes an uncommitted decision.
	Standings []teamPoint `json:"standings"`
	// ResolvedTurns is the number of turns whose outcome is public.
	ResolvedTurns int `json:"resolvedTurns"`
	// History contains completed turns only, never rivals' open-turn choices.
	History []turnResult `json:"history"`
	// Self is the caller's own team view.
	Self *selfView `json:"self,omitempty"`
	// Final appears only once the race is over.
	Final []rankedTeam `json:"final,omitempty"`
}

// selfView is the caller's own marker plus its committed decisions.
type selfView struct {
	SubjectID string `json:"subjectId"`
	Position  int    `json:"position"`
	Stamina   int    `json:"stamina"`
	// Decisions lists this team's own committed decisions, so the player can see
	// what they already submitted this turn without reading anyone else's.
	Decisions []committedAction `json:"decisions"`
}

// Project implements modes.Mode.
//
// The round carries no secret answer material, so the only projection rule is
// that an uncommitted decision is invisible and a committed one is invisible
// until its turn resolves. Every viewer — player, spectator, host or display —
// therefore sees the same public scoreboard, and only the caller's own decisions
// are attached to their own view.
func (*Mode) Project(in modes.ProjectInput) (json.RawMessage, error) {
	pub, err := decodePublic(in.Public)
	if err != nil {
		return nil, err
	}
	// Projection never invents turns. Only ExpireTurn may record timeout actions;
	// cancellation and legacy early locks must not manufacture a finished race.
	res, err := resolveRace(pub, false)
	if err != nil {
		return nil, err
	}

	out := projection{
		ModeID:        ModeID,
		RulesVersion:  RulesVersion,
		Phase:         in.Phase,
		Config:        pub.Config,
		Track:         pub.Track,
		Provenance:    pub.Provenance,
		Competitive:   pub.Competitive,
		CurrentTurn:   res.CurrentTurn,
		Standings:     res.Final,
		ResolvedTurns: len(res.Turns),
		History:       res.Turns,
	}
	if out.Standings == nil {
		out.Standings = []teamPoint{}
	}

	for _, entry := range pub.Subjects {
		if entry.SubjectID != in.Viewer.Subject.ID {
			continue
		}
		own := make([]committedAction, 0, len(pub.Committed))
		for _, ca := range pub.Committed {
			if ca.SubjectID == entry.SubjectID {
				own = append(own, ca)
			}
		}
		if own == nil {
			own = []committedAction{}
		}
		var point teamPoint
		for _, p := range res.Final {
			if p.SubjectID == entry.SubjectID {
				point = p
				break
			}
		}
		out.Self = &selfView{
			SubjectID: entry.SubjectID,
			Position:  point.Position,
			Stamina:   point.Stamina,
			Decisions: own,
		}
		break
	}

	// The final standings are published once no further decision can change
	// them, and not before: a mid-race rank would tell a trailing team exactly
	// what it needs, which the rules do not grant.
	if res.Complete && (in.Phase == modes.PhaseRevealed || in.Phase == modes.PhaseSettled) {
		out.Final = rankTeams(res.Final, len(pub.Subjects), pub.Competitive)
	}
	return modeutil.Marshal(out), nil
}

// Settle implements modes.Mode.
//
// with n≥2 teams the display score is floor(100×(n-r)/(n-1)); a
// single-team practice round produces no competitive rank at all, so it settles
// to zero units with an explanation that says so rather than pretending a 100.
func (*Mode) Settle(in modes.SettleInput) ([]modes.Score, error) {
	pub, err := decodePublic(in.Public)
	if err != nil {
		return nil, err
	}
	res, err := resolveRace(pub, false)
	if err != nil {
		return nil, err
	}
	if !res.Complete {
		return nil, errors.New("strategyrace: cannot settle before all race turns complete")
	}
	teams := len(pub.Subjects)
	ranked := rankTeams(res.Final, teams, pub.Competitive)

	scores := make([]modes.Score, 0, len(ranked))
	for _, rt := range ranked {
		kind := modes.SubjectPlayer
		for _, entry := range pub.Subjects {
			if entry.SubjectID == rt.SubjectID {
				kind = entry.SubjectKind
				break
			}
		}
		explanation := modeutil.Marshal(struct {
			Position    int    `json:"position"`
			Stamina     int    `json:"stamina"`
			Rank        int    `json:"rank"`
			Shared      bool   `json:"shared"`
			Teams       int    `json:"teams"`
			Competitive bool   `json:"competitive"`
			Formula     string `json:"formula"`
			Note        string `json:"note,omitempty"`
		}{
			Position:    rt.Position,
			Stamina:     rt.Stamina,
			Rank:        rt.Rank,
			Shared:      rt.Shared,
			Teams:       teams,
			Competitive: pub.Competitive,
			Formula:     "floor(100*(n-r)/(n-1))",
			Note:        practiceNote(pub.Competitive),
		})
		score := modes.Score{
			Subject:       modes.Subject{ID: rt.SubjectID, Kind: kind},
			ScoreUnits:    rt.ScoreUnits,
			MaxScoreUnits: modeutil.Units(100),
			Explanation:   explanation,
			Rank:          rt.Rank,
			SharedRank:    rt.Shared,
		}
		if err := score.Validate(); err != nil {
			return nil, err
		}
		scores = append(scores, score)
	}
	return modes.RankScores(scores), nil
}

// practiceNote explains a practice round's zero score.
func practiceNote(competitive bool) string {
	if competitive {
		return ""
	}
	return "single-team practice: positions are shown, no competitive rank is produced"
}
