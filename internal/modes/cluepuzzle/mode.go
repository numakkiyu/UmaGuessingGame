// Package cluepuzzle implements G09 团队情报拼图 .
//
// Three to six players reconstruct an ordered assignment of four to six cards.
// Every player holds private clues that nobody else can read; a shared area holds
// the task rules and a few weak constraints. The team talks, then the current
// controller submits the complete assignment.
//
// Two properties shape the implementation:
//
// 1. The puzzle is solver-verified. The generator commits to an answer,
// emits constraints that answer satisfies, and then an exact solver counts how
// many complete assignments satisfy the kept constraints. A puzzle that is
// unsolvable, or that has more solutions than the round allows, is refused
// before it reaches a player. "生成器先生成有效方案与约束，再求解" is
// the literal order of operations in solver.go.
//
// 2. Clues are private. A player's clues never appear in another player's
// projection, and never in the public area. The public area carries the card
// deck, the slot count and the shared constraints only. This is a structural
// guarantee: the private clue text is not part of the public prepared state at
// all, so no projection bug can leak it.
//
// Scoring : floor(100 × satisfied constraints / total constraints), with
// the total fixed at prepare time. An unsubmitted round scores 0.
package cluepuzzle

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/bhcn/umaguessinggame/internal/modes"
	"github.com/bhcn/umaguessinggame/internal/modes/modeutil"
)

// ModeID is the stable mode identifier (contract ModeId enum).
const ModeID = "clue-puzzle"

// RulesVersion is the rule revision this implementation satisfies.
const RulesVersion = "2.0.0-draft.1"

// Action kinds.
//
// The value is the contract discriminator (ModeAction → ClueAssignment), not an
// internal name .
const (
	// ActionKindAssignment submits the complete slot → card mapping.
	ActionKindAssignment = "clue.assignment"
)

// ContractKinds returns the ModeAction discriminators this mode accepts.
func (*Mode) ContractKinds() []string { return []string{ActionKindAssignment} }

// Rejection codes.
const (
	CodeInvalidPayload = "invalid_payload"
	CodeUnknownAction  = "unknown_action"
	CodeUnknownSlot    = "unknown_slot"
	CodeUnknownCard    = "unknown_card"
	CodeDuplicateCard  = "duplicate_card"
	CodeIncomplete     = "incomplete_assignment"
	CodeNotOnRoster    = "not_on_roster"
	// CodeNotController is returned when a participant who does not hold the
	// control lease tries to submit. Only the current controller may submit the
	// team's answer .
	CodeNotController = "not_controller"
)

// Documented parameters .
const (
	// MinCards and MaxCards bound the card count.
	MinCards = 4
	MaxCards = 6
	// MinParticipants and MaxParticipants bound the team size.
	MinParticipants = 3
	MaxParticipants = 6
	// AnswerSeconds is the answering window.
	AnswerSeconds = 240
	// DefaultConstraintCount is how many constraints a puzzle keeps. It must be at
	// least the participant count so every player gets a private clue.
	DefaultConstraintCount = 8
)

// Config is the round configuration.
type Config struct {
	// Cards is how many cards the puzzle uses (4–6).
	Cards int `json:"cards"`
	// Participants is the team size (3–6). It must match the subject count.
	Participants int `json:"participants"`
	// ConstraintCount is how many constraints the puzzle keeps.
	ConstraintCount int `json:"constraintCount"`
	// SharedCount is how many constraints are published in the shared area.
	SharedCount int `json:"sharedCount"`
	// Unique requires the puzzle to have exactly one solution. The plan allows a
	// multi-solution puzzle only when every accepted answer is verified
	// .
	Unique bool `json:"unique"`
	// AnswerSeconds is the answering window.
	AnswerSeconds int `json:"answerSeconds"`
}

// decodeConfig parses and normalises a configuration blob.
func decodeConfig(raw json.RawMessage) (Config, error) {
	cfg := Config{
		Cards:           MinCards,
		ConstraintCount: DefaultConstraintCount,
		SharedCount:     2,
		Unique:          true,
		AnswerSeconds:   AnswerSeconds,
	}
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &cfg); err != nil {
			return Config{}, fmt.Errorf("cluepuzzle: invalid config: %w", err)
		}
	}
	if cfg.Cards == 0 {
		cfg.Cards = MinCards
	}
	if cfg.ConstraintCount == 0 {
		cfg.ConstraintCount = DefaultConstraintCount
	}
	if cfg.AnswerSeconds == 0 {
		cfg.AnswerSeconds = AnswerSeconds
	}
	if cfg.Cards < MinCards || cfg.Cards > MaxCards {
		return Config{}, fmt.Errorf(
			"cluepuzzle: cards %d outside [%d,%d]", cfg.Cards, MinCards, MaxCards)
	}
	// Participants is optional: 0 means "take it from the roster", which is what a
	// caller that does not want to duplicate the roster count will send. Any other value
	// must be inside the documented bound.
	if cfg.Participants != 0 &&
		(cfg.Participants < MinParticipants || cfg.Participants > MaxParticipants) {
		return Config{}, fmt.Errorf(
			"cluepuzzle: participants %d outside [%d,%d]",
			cfg.Participants, MinParticipants, MaxParticipants)
	}
	// Every participant needs a private clue, so the constraint count must be at
	// least the participant count plus the shared ones.
	if cfg.ConstraintCount < MinParticipants {
		return Config{}, fmt.Errorf(
			"cluepuzzle: constraintCount %d cannot give %d participants a private clue",
			cfg.ConstraintCount, MinParticipants)
	}
	if cfg.ConstraintCount > 24 {
		return Config{}, fmt.Errorf("cluepuzzle: constraintCount %d above 24", cfg.ConstraintCount)
	}
	if cfg.SharedCount < 0 {
		return Config{}, fmt.Errorf("cluepuzzle: negative sharedCount %d", cfg.SharedCount)
	}
	if cfg.AnswerSeconds < 30 || cfg.AnswerSeconds > 900 {
		return Config{}, fmt.Errorf(
			"cluepuzzle: answerSeconds %d outside [30,900]", cfg.AnswerSeconds)
	}
	return cfg, nil
}

// card is one published puzzle card. Its label is the game's own card name; the
// puzzle is about ordering, not about identifying a character, so the label is
// public.
type card struct {
	CardID string `json:"cardId"`
	Label  string `json:"label"`
}

// slotView is one board slot as published.
type slotView struct {
	SlotIndex int    `json:"slotIndex"`
	Label     string `json:"label"`
}

// sharedConstraint is a constraint published in the shared area.
type sharedConstraint struct {
	ConstraintID string   `json:"constraintId"`
	Text         string   `json:"text"`
	Kind         string   `json:"kind"`
	Cards        []string `json:"cards"`
	Position     int      `json:"position,omitempty"`
}

// rosterEntry is one participant recorded in the public prepared state.
type rosterEntry struct {
	SubjectID   string            `json:"subjectId"`
	SubjectKind modes.SubjectKind `json:"subjectKind"`
}

// publicState is the non-secret round state.
//
// It carries the deck, the slots, the shared constraints and the scoring
// denominator. It deliberately carries NO private clue: private clues live in the
// server-only state, keyed by participant, and are projected one participant at a
// time.
type publicState struct {
	ModeID       string        `json:"modeId"`
	RulesVersion string        `json:"rulesVersion"`
	Config       Config        `json:"config"`
	Subjects     []rosterEntry `json:"subjects"`
	Cards        []card        `json:"cards"`
	Slots        []slotView    `json:"slots"`
	// Shared is the public constraint area. It is the team's common starting
	// point, and it never contains a private clue.
	Shared []sharedConstraint `json:"shared"`
	// ConstraintCount is the fixed denominator of the score .
	ConstraintCount int `json:"constraintCount"`
	// SharedEmptyReason explains why the shared area is empty, when it is.
	//
	// An empty shared area is legitimate on a small board — every constraint is needed as
	// somebody's private clue — but the client must be told which case it is, so it can say
	// "this board has no shared starting clues" instead of rendering a silent gap.
	SharedEmptyReason string `json:"sharedEmptyReason,omitempty"`
	Scoring           string `json:"scoring"`
	// ControlSubjectID is the participant who currently holds the submit
	// authority. The room layer owns the lease; this field records the initial
	// holder so a projection has a value before any lease is granted.
	ControlSubjectID string `json:"controlSubjectId"`
}

// privateState is the server-only round state: the solution and every private
// clue. Nothing here is ever projected to a player other than the owner.
type privateState struct {
	ModeID string `json:"modeId"`
	// Solution maps slot index (1-based) → card id.
	Solution map[string]string `json:"solution"`
	// Constraints is the full constraint list, keyed by id, so a submitted answer
	// can be graded without re-deriving the puzzle.
	Constraints []constraint `json:"constraints"`
	// PrivateClues maps subject id → the constraints that subject holds.
	PrivateClues map[string][]constraint `json:"privateClues"`
	// SolutionCount is how many complete answers satisfy every constraint. It is
	// recorded so the reveal can say whether the answer was unique.
	SolutionCount int `json:"solutionCount"`
	// Teams freezes member-level clue authorisation for new team rounds.
	Teams map[string][]string `json:"teams,omitempty"`
}

// subjectState is one participant's own persisted state.
type subjectState struct {
	// Assignment is the current submitted mapping, slot index → card id.
	Assignment map[string]string `json:"assignment"`
	// Submissions counts accepted revisions.
	Submissions int `json:"submissions"`
}

// Mode implements modes.Mode for G09.
type Mode struct{}

// New returns the clue-puzzle mode.
func New() *Mode { return &Mode{} }

// ID implements modes.Mode.
func (*Mode) ID() string { return ModeID }

// RulesVersion implements modes.Mode.
func (*Mode) RulesVersion() string { return RulesVersion }

// Title implements modes.Mode.
func (*Mode) Title() string { return "团队情报拼图" }

// Summary implements modes.Mode.
func (*Mode) Summary() string {
	return "每人握着别人看不到的线索，靠交流把卡片排进正确槽位；由当前控制者提交完整答案。"
}

// InformationStyles reports which information forms this mode implements.
func (*Mode) InformationStyles() []string {
	return []string{"closed"}
}

// RoundWindow implements modes.Mode.
//
// 整局一次作答窗口。返回 0 表示本模式没有自己的窗口意见，交给房间层默认值。
func (*Mode) RoundWindow(raw json.RawMessage) time.Duration {
	cfg, err := decodeConfig(raw)
	if err != nil {
		return 0
	}
	return time.Duration(cfg.AnswerSeconds) * time.Second
}

// TurnWindow implements modes.Mode. This mode is not turn-based: it runs on the
// single RoundWindow deadline, so there is no per-turn clock to report.
func (*Mode) TurnWindow(json.RawMessage) time.Duration { return 0 }

// MinSubjects implements modes.Mode.
func (*Mode) MinSubjects() int { return MinParticipants }

// MaxSubjects implements modes.Mode.
func (*Mode) MaxSubjects() int { return 8 * MaxParticipants }

// Requirement implements modes.Mode.
//
// clue-puzzle is generated from rules, not scraped: the deck is authored per round
// and the constraints are produced by the generator. It therefore declares the
// minimum card count it needs and no content field. The content compiler records
// the mode as unavailable for a different reason — it has no constraint inventory
// to admit — and this package never invents one at admission time; it only
// generates a puzzle when a round is actually prepared.
func (*Mode) Requirement() modes.ContentRequirement {
	return modes.ContentRequirement{
		Fields:      []string{DeckSection},
		MinEntities: MinCards,
		NeedsAssets: false,
	}
}

// ValidateConfig implements modes.Mode.
func (*Mode) ValidateConfig(cfg json.RawMessage) error {
	_, err := decodeConfig(cfg)
	return err
}

// loadDeck builds the card deck for a round.
//
// The deck is authored by the round from a fixed label set rather than scraped:
// the puzzle tests reasoning about order, so any set of distinguishable labels
// works and inventing character facts would be worse. A pack may override the
// labels through a section, which is how a later content build can theme it.
// DeckSection is the pack section carrying the authored card deck.
const DeckSection = "cluepuzzle.deck"

// Provenance kinds accepted in the authored deck.
const (
	ProvenanceAuthored = "authored-design-value"
	ProvenanceDerived  = "derived-from-doc"
	ProvenanceOfficial = "official-data"
)

// deckProvenance records where an authored card came from.
//
// The rule layer can verify Kind is one of the enumerated values, NotOfficialData is true for
// anything this project designed, and Author and Basis are non-empty. It CANNOT verify that
// Basis names a real document — that is a human review boundary, recorded here rather than
// papered over.
type deckProvenance struct {
	Kind            string `json:"kind"`
	NotOfficialData bool   `json:"notOfficialData"`
	Author          string `json:"author"`
	Basis           string `json:"basis"`
}

// validate reports whether the block may be published.
func (p deckProvenance) validate() error {
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
	return nil
}

// loadDeck reads the authored card deck from the pack.
//
// There is deliberately no built-in fallback. A fallback would make "the compiler produced no
// deck" look identical to "the mode works", which is the failure this project keeps hitting.
// A pack without this section means the mode cannot start, and Prepare says so.
func loadDeck(src modes.ContentSource) ([]card, error) {
	if src == nil {
		return nil, fmt.Errorf(
			"cluepuzzle: no content source; %q is required and there is no built-in fallback",
			DeckSection)
	}
	raw, err := modeutil.Section(src, DeckSection)
	if err != nil {
		return nil, fmt.Errorf(
			"cluepuzzle: %q is required: %w (there is no built-in fallback; the pack must "+
				"publish the card deck)", DeckSection, err)
	}
	records, err := modeutil.Records(raw, "cards", "items")
	if err != nil {
		return nil, fmt.Errorf("cluepuzzle: decode %q: %w", DeckSection, err)
	}
	out := make([]card, 0, len(records))
	seen := make(map[string]struct{}, len(records))
	for _, rec := range records {
		id := rec.Str("cardId", "card_id", "id")
		label := rec.Str("label", "name")
		if id == "" || label == "" {
			continue
		}
		if _, dup := seen[id]; dup {
			return nil, fmt.Errorf("cluepuzzle: card %q appears twice", id)
		}
		provRaw, ok := rec.Object("provenance")
		if !ok {
			return nil, fmt.Errorf("cluepuzzle: card %q carries no provenance block", id)
		}
		prov := deckProvenance{
			Kind:            provRaw.Str("kind"),
			NotOfficialData: provRaw.Bool("notOfficialData", "not_official_data"),
			Author:          provRaw.Str("author"),
			Basis:           provRaw.Str("basis"),
		}
		if err := prov.validate(); err != nil {
			return nil, fmt.Errorf("cluepuzzle: card %q: %w", id, err)
		}
		seen[id] = struct{}{}
		out = append(out, card{CardID: id, Label: label})
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("cluepuzzle: %q carries no usable card", DeckSection)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CardID < out[j].CardID })
	return out, nil
}

// drawDeck picks count distinct cards from the pool, deterministically by seed.
func drawDeck(seed string, pool []card, count int) []card {
	indices := make([]int, len(pool))
	for i := range indices {
		indices[i] = i
	}
	stream := modeutil.NewStream(seed + "\x00cluepuzzle.draw")
	stream.Shuffle(len(indices), func(i, j int) { indices[i], indices[j] = indices[j], indices[i] })
	out := make([]card, 0, count)
	for i := 0; i < count && i < len(indices); i++ {
		out = append(out, pool[indices[i]])
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CardID < out[j].CardID })
	return out
}

// slotLabel renders the solver's 1-based position without shifting it.
func slotLabel(index int) string {
	return fmt.Sprintf("第 %d 位", index)
}

// Prepare implements modes.Mode.
//
// The generator runs here, outside the write transaction, and the whole puzzle is
// frozen into the prepared state: the solution and every private clue go to the
// server-only half, the deck and the shared constraints go to the public half.
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
			return modes.Prepared{}, fmt.Errorf("cluepuzzle: duplicate subject %q", subj.ID)
		}
		seen[subj.ID] = struct{}{}
		roster = append(roster, rosterEntry{SubjectID: subj.ID, SubjectKind: subj.Kind})
	}
	teams := map[string][]string(nil)
	if len(in.Participants) > 0 {
		teams, err = teamRoster(in.Subject, in.Participants)
		if err != nil {
			return modes.Prepared{}, err
		}
		count := 0
		for _, entry := range roster {
			n := len(teams[entry.SubjectID])
			if cfg.Participants != 0 && cfg.Participants != n {
				return modes.Prepared{}, fmt.Errorf("cluepuzzle: configured team size does not match the roster")
			}
			if n > count {
				count = n
			}
		}
		cfg.Participants = count
	} else {
		// Legacy prepared rounds and rule-engine fixtures retain their original topology.
		if len(roster) < MinParticipants || len(roster) > MaxParticipants {
			return modes.Prepared{}, fmt.Errorf("cluepuzzle: %d participants outside [%d,%d]", len(roster), MinParticipants, MaxParticipants)
		}
		if cfg.Participants == 0 {
			cfg.Participants = len(roster)
		}
		if cfg.Participants != len(roster) {
			return modes.Prepared{}, fmt.Errorf("cluepuzzle: config says %d participants but the roster has %d", cfg.Participants, len(roster))
		}
	}

	fullDeck, err := loadDeck(in.Content)
	if err != nil {
		return modes.Prepared{}, err
	}
	// The pool must cover the round's card count. A pool smaller than the requested count cannot
	// fill the board, and silently shrinking the round would change the documented parameters.
	if len(fullDeck) < cfg.Cards {
		return modes.Prepared{}, fmt.Errorf(
			"cluepuzzle: the pack publishes %d cards but the round needs %d",
			len(fullDeck), cfg.Cards)
	}
	// Draw the round's cards from the pool with the round seed, so two rounds do not always use
	// the first N cards of the deck.
	deck := drawDeck(in.Scope.Seed, fullDeck, cfg.Cards)

	cardIDs := make([]string, 0, len(deck))
	for _, c := range deck {
		cardIDs = append(cardIDs, c.CardID)
	}

	gen, err := generate(in.Scope.Seed, cardIDs, cfg)
	if err != nil {
		return modes.Prepared{}, err
	}

	// Render card names once without changing the solver's opaque identifiers.
	ordered := append(deck[:0:0], deck...)
	sort.Slice(ordered, func(i, j int) bool { return len(ordered[i].CardID) > len(ordered[j].CardID) })
	var replacements []string
	for _, c := range ordered {
		replacements = append(replacements, c.CardID, c.Label)
	}
	render := strings.NewReplacer(replacements...)
	for i := range gen.Constraints {
		gen.Constraints[i].Text = render.Replace(gen.Constraints[i].Text)
	}
	privateClues := make(map[string][]constraint)
	byID := make(map[string]constraint, len(gen.Constraints))
	for _, c := range gen.Constraints {
		byID[c.ConstraintID] = c
	}
	if teams == nil {
		for i, entry := range roster {
			for _, id := range gen.PrivateClueIDs[participantID(i)] {
				privateClues[entry.SubjectID] = append(privateClues[entry.SubjectID], byID[id])
			}
		}
		if err := assertPrivateCluesUseful(privateClues); err != nil {
			return modes.Prepared{}, err
		}
	} else {
		// Every team receives the same complete information, even at different
		// sizes. Distribution is unique within a team, never globally unique.
		var privateIDs []string
		for i := 0; i < cfg.Participants; i++ {
			privateIDs = append(privateIDs, gen.PrivateClueIDs[participantID(i)]...)
		}
		sort.Strings(privateIDs)
		for _, entry := range roster {
			members := teams[entry.SubjectID]
			group := make(map[string][]constraint, len(members))
			for i, id := range privateIDs {
				member := members[i%len(members)]
				group[member] = append(group[member], byID[id])
			}
			if err := assertPrivateCluesUseful(group); err != nil {
				return modes.Prepared{}, err
			}
			for member, clues := range group {
				privateClues[member] = clues
			}
		}
	}

	// The shared area publishes only the constraints the generator marked shared.
	sharedByID := make(map[string]constraint, len(gen.Constraints))
	for _, c := range gen.Constraints {
		sharedByID[c.ConstraintID] = c
	}
	shared := make([]sharedConstraint, 0, len(gen.SharedConstraintIDs))
	for _, id := range gen.SharedConstraintIDs {
		c, ok := sharedByID[id]
		if !ok {
			return modes.Prepared{}, fmt.Errorf(
				"cluepuzzle: shared constraint %q is not among the kept constraints", id)
		}
		shared = append(shared, sharedConstraint{
			ConstraintID: c.ConstraintID,
			Text:         c.Text,
			Kind:         c.Kind,
			Cards:        c.Cards,
			Position:     c.Position,
		})
	}

	pubCards := make([]card, 0, len(deck))
	for _, c := range deck {
		pubCards = append(pubCards, card{CardID: c.CardID, Label: c.Label})
	}
	// Publish the deck in a stable order that is not the answer order, so the card
	// list itself does not leak the solution.
	sort.Slice(pubCards, func(i, j int) bool { return pubCards[i].CardID < pubCards[j].CardID })

	slots := make([]slotView, 0, cfg.Cards)
	for i := 1; i <= cfg.Cards; i++ {
		slots = append(slots, slotView{SlotIndex: i, Label: slotLabel(i)})
	}

	solution := make(map[string]string, len(gen.Solution))
	for slot, cardID := range gen.Solution {
		solution[fmt.Sprintf("%d", slot)] = cardID
	}

	pub := publicState{
		ModeID:            ModeID,
		RulesVersion:      RulesVersion,
		Config:            cfg,
		Subjects:          roster,
		Cards:             pubCards,
		Slots:             slots,
		Shared:            shared,
		ConstraintCount:   len(gen.Constraints),
		SharedEmptyReason: gen.SharedEmptyReason,
		Scoring:           "floor(100 × 满足约束数 / 固定约束总数)",
		ControlSubjectID:  roster[0].SubjectID,
	}
	if teams != nil {
		pub.ControlSubjectID = ""
	}
	priv := privateState{
		ModeID:        ModeID,
		Solution:      solution,
		Constraints:   gen.Constraints,
		PrivateClues:  privateClues,
		Teams:         teams,
		SolutionCount: gen.SolutionCount,
	}
	return modes.Prepared{
		Public:  modeutil.Marshal(pub),
		Private: modeutil.Marshal(priv),
		// QuestionCount is the usable POOL size, counted in DECK CARDS, because MinEntities is
		// also counted in cards. It used to be the generated constraint count, which is a
		// different unit: the gate compared constraints against a card floor.
		QuestionCount: len(fullDeck),
	}, nil
}

// assertPrivateCluesUseful verifies that every participant holds at least one clue
// that no other participant holds.
//
// This is the machine-checkable half of "线索必要性": if two participants held
// identical clue sets, one of them would be redundant and the round would not need
// their contribution. The human half — whether the clue set is *sufficient* to
// solve — is the solver's job and is asserted in the generator.
func assertPrivateCluesUseful(clues map[string][]constraint) error {
	if len(clues) == 0 {
		return fmt.Errorf("cluepuzzle: no participant holds a clue")
	}
	// Count how many participants hold each clue.
	holders := make(map[string]int, 64)
	for _, list := range clues {
		if len(list) == 0 {
			return fmt.Errorf("cluepuzzle: a participant holds no clue")
		}
		for _, c := range list {
			holders[c.ConstraintID]++
		}
	}
	for subjectID, list := range clues {
		exclusive := 0
		for _, c := range list {
			if holders[c.ConstraintID] == 1 {
				exclusive++
			}
		}
		if exclusive == 0 {
			return fmt.Errorf(
				"cluepuzzle: participant %q holds no clue that the others cannot see", subjectID)
		}
	}
	return nil
}

// assignmentPayload is the body of ActionKindAssignment, matching the contract's
// ClueAssignment schema: {kind, assignments[]} where each assignment is
// {slotId, characterId}.
//
// The contract names the second field "characterId" because the generic
// Assignment schema is shared with the draft mode. For this mode the value is a
// card id; the field name is the wire contract and is decoded as-is.
type assignmentPayload struct {
	Kind        string `json:"kind"`
	Assignments []struct {
		SlotID      string `json:"slotId"`
		CharacterID string `json:"characterId"`
	} `json:"assignments"`
}

// decodePublic unmarshals the public state.
func decodePublic(raw json.RawMessage) (publicState, error) {
	var pub publicState
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &pub); err != nil {
			return publicState{}, fmt.Errorf("cluepuzzle: decode public state: %w", err)
		}
	}
	return pub, nil
}

// decodePrivate unmarshals the solution and the clue allocation.
func decodePrivate(raw json.RawMessage) (privateState, error) {
	var priv privateState
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &priv); err != nil {
			return privateState{}, fmt.Errorf("cluepuzzle: decode private state: %w", err)
		}
	}
	return priv, nil
}

// decodeState unmarshals a participant state.
func decodeState(raw json.RawMessage) (subjectState, error) {
	var st subjectState
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &st); err != nil {
			return subjectState{}, fmt.Errorf("cluepuzzle: decode participant state: %w", err)
		}
	}
	return st, nil
}

// Apply implements modes.Mode.
//
// Only the current controller may submit, which is the documented team rule
// . The caller learns whether they are the controller from the viewer
// projection, but the rule layer checks it against the control subject recorded in
// the round state, never against a request field.
func (*Mode) Apply(in modes.ApplyInput) (modes.Decision, error) {
	if in.Action.Kind != ActionKindAssignment {
		return modes.Rejected(CodeUnknownAction,
			fmt.Sprintf("cluepuzzle: unsupported action %q", in.Action.Kind)), nil
	}
	if err := in.Action.Validate(); err != nil {
		return modes.Rejected(CodeInvalidPayload, err.Error()), nil
	}
	var payload assignmentPayload
	if err := json.Unmarshal(in.Action.Payload, &payload); err != nil {
		return modes.Rejected(CodeInvalidPayload,
			"cluepuzzle: payload must carry assignments"), nil
	}
	if payload.Kind != "" && payload.Kind != ActionKindAssignment {
		return modes.Rejected(CodeInvalidPayload, fmt.Sprintf(
			"cluepuzzle: payload kind %q does not match action %q",
			payload.Kind, ActionKindAssignment)), nil
	}

	pub, err := decodePublic(in.Public)
	if err != nil {
		return modes.Decision{}, err
	}
	if !onRoster(pub.Subjects, in.Subject.ID) {
		return modes.Rejected(CodeNotOnRoster, fmt.Sprintf(
			"cluepuzzle: subject %q is not on this roster", in.Subject.ID)), nil
	}
	priv, err := decodePrivate(in.Private)
	if err != nil {
		return modes.Decision{}, err
	}
	if priv.Teams != nil {
		if !hasMember(priv.Teams[in.Subject.ID], in.MembershipID) {
			return modes.Rejected(CodeNotOnRoster, "cluepuzzle: actor did not hold a seat at preparation"), nil
		}
	} else if pub.ControlSubjectID != "" && pub.ControlSubjectID != in.Subject.ID {
		return modes.Rejected(CodeNotController, "cluepuzzle: subject does not hold submit authority"), nil
	}

	st, err := decodeState(in.State)
	if err != nil {
		return modes.Decision{}, err
	}

	// The submission must cover every slot with a distinct card. A partial answer
	// is refused rather than scored partially: the score is a ratio over the fixed
	// constraint total, and a missing card would make the mapping ill-defined.
	knownSlots := make(map[string]struct{}, len(pub.Slots))
	for _, slot := range pub.Slots {
		knownSlots[fmt.Sprintf("%d", slot.SlotIndex)] = struct{}{}
	}
	knownCards := make(map[string]struct{}, len(pub.Cards))
	for _, c := range pub.Cards {
		knownCards[c.CardID] = struct{}{}
	}
	assignment := make(map[string]string, len(payload.Assignments))
	usedCards := make(map[string]string, len(payload.Assignments))
	for _, item := range payload.Assignments {
		if _, ok := knownSlots[item.SlotID]; !ok {
			return modes.Rejected(CodeUnknownSlot, fmt.Sprintf(
				"cluepuzzle: slot %q is not on this board", item.SlotID)), nil
		}
		if _, ok := knownCards[item.CharacterID]; !ok {
			return modes.Rejected(CodeUnknownCard, fmt.Sprintf(
				"cluepuzzle: card %q is not on this board", item.CharacterID)), nil
		}
		if otherSlot, dup := usedCards[item.CharacterID]; dup {
			return modes.Rejected(CodeDuplicateCard, fmt.Sprintf(
				"cluepuzzle: card %q is assigned to both slot %q and slot %q",
				item.CharacterID, otherSlot, item.SlotID)), nil
		}
		if _, dupSlot := assignment[item.SlotID]; dupSlot {
			return modes.Rejected(CodeInvalidPayload, fmt.Sprintf(
				"cluepuzzle: slot %q is assigned twice", item.SlotID)), nil
		}
		usedCards[item.CharacterID] = item.SlotID
		assignment[item.SlotID] = item.CharacterID
	}
	if len(assignment) != len(knownSlots) {
		return modes.Rejected(CodeIncomplete, fmt.Sprintf(
			"cluepuzzle: the answer covers %d of %d slots",
			len(assignment), len(knownSlots))), nil
	}

	st.Assignment = assignment
	st.Submissions++

	feedback := modeutil.Marshal(struct {
		// AssignedSlots and TotalSlots let the client confirm coverage without
		// recomputing the board size.
		AssignedSlots int `json:"assignedSlots"`
		TotalSlots    int `json:"totalSlots"`
		Submissions   int `json:"submissions"`
		// Note explains the revision rule and, importantly, that the feedback does
		// not reveal whether the answer is right: that would let the team probe.
		Note string `json:"note"`
	}{
		AssignedSlots: len(assignment),
		TotalSlots:    len(knownSlots),
		Submissions:   st.Submissions,
		Note:          "已记录；截止前可修订。提交不会告知对错，结果在揭晓时统一公布。",
	})

	dec := modes.Accepted(feedback, modeutil.Marshal(st))
	return dec, nil
}

// onRoster reports whether subjectID participates.
func onRoster(roster []rosterEntry, subjectID string) bool {
	for _, entry := range roster {
		if entry.SubjectID == subjectID {
			return true
		}
	}
	return false
}

// clueView is one private clue as its owner sees it.
type clueView struct {
	ConstraintID string   `json:"constraintId"`
	Text         string   `json:"text"`
	Kind         string   `json:"kind"`
	Cards        []string `json:"cards"`
	Position     int      `json:"position,omitempty"`
}

// projection is the viewer-specific round state.
type projection struct {
	ModeID            string             `json:"modeId"`
	RulesVersion      string             `json:"rulesVersion"`
	Phase             modes.Phase        `json:"phase"`
	Config            Config             `json:"config"`
	Cards             []card             `json:"cards"`
	Slots             []slotView         `json:"slots"`
	Shared            []sharedConstraint `json:"shared"`
	ConstraintCount   int                `json:"constraintCount"`
	SharedEmptyReason string             `json:"sharedEmptyReason,omitempty"`
	Scoring           string             `json:"scoring"`
	ControlSubjectID  string             `json:"controlSubjectId"`
	// SelfIsController tells the viewer whether they may submit.
	SelfIsController bool `json:"selfIsController"`
	// PrivateClues is the caller's OWN clue list. It is omitted for any viewer
	// that holds no seat, which is how a spectator or the display is prevented
	// from reading a player's clues.
	PrivateClues []clueView `json:"privateClues,omitempty"`
	// Self is the caller's own submitted draft.
	Self *selfView `json:"self,omitempty"`
	// TeamDraft is the current submitted mapping, visible to the whole team. It is
	// the "公开草稿" the plan allows members to see ; it is not a private
	// clue.
	TeamDraft map[string]string `json:"teamDraft,omitempty"`
	// Progress is the coarse per-participant progress.
	Progress []progressView `json:"progress"`
	// Answer is the solution, emitted only when authorised.
	Answer *answerReveal `json:"answer,omitempty"`
}

// selfView is the caller's own state.
type selfView struct {
	SubjectID   string            `json:"subjectId"`
	Assignment  map[string]string `json:"assignment"`
	Submissions int               `json:"submissions"`
}

// progressView is another participant's progress. It reports how many clues they
// hold and whether they have submitted, never what their clues say.
type progressView struct {
	SubjectID   string `json:"subjectId"`
	ClueCount   int    `json:"clueCount"`
	Submissions int    `json:"submissions"`
}

// answerReveal is the solution, emitted only when authorised.
type answerReveal struct {
	// Slots is the correct mapping in slot order.
	Slots []revealSlot `json:"slots"`
	// SolutionCount reports whether the puzzle had a unique answer.
	SolutionCount int `json:"solutionCount"`
	// Constraints is the full constraint list with the text, so the reveal can
	// explain which clues produced the answer.
	Constraints []clueView `json:"constraints"`
}

// revealSlot is one revealed slot.
type revealSlot struct {
	SlotIndex int    `json:"slotIndex"`
	CardID    string `json:"cardId"`
	Label     string `json:"label"`
}

// Project implements modes.Mode.
//
// The visibility rules, all enforced here rather than in the browser:
//
// - A player sees only their own private clues. Another player's clues are not
// in the payload at all — the field is omitted, not blanked.
// - A spectator, host or display with no seat sees no private clue whatsoever.
// The host may grant a team-wide extension but must not read a private clue
// .
// - The team draft is public to the team, which the rules allow.
// - The solution is published once the round is terminal, or to a referee.
func (*Mode) Project(in modes.ProjectInput) (json.RawMessage, error) {
	pub, err := decodePublic(in.Public)
	if err != nil {
		return nil, err
	}
	priv, err := decodePrivate(in.Private)
	if err != nil {
		return nil, err
	}

	// Saved rounds may carry the former off-by-one label. Presentation uses
	// the stored solver position; no answer, state or score is migrated.
	for i := range pub.Slots {
		pub.Slots[i].Label = slotLabel(pub.Slots[i].SlotIndex)
	}

	out := projection{
		ModeID:            ModeID,
		RulesVersion:      RulesVersion,
		Phase:             in.Phase,
		Config:            pub.Config,
		Cards:             pub.Cards,
		Slots:             pub.Slots,
		Shared:            pub.Shared,
		ConstraintCount:   pub.ConstraintCount,
		SharedEmptyReason: pub.SharedEmptyReason,
		Scoring:           pub.Scoring,
		ControlSubjectID:  pub.ControlSubjectID,
		Progress:          make([]progressView, 0, len(pub.Subjects)),
	}

	// Iterate the roster order recorded at prepare time, never the map, so the
	// projection is byte-identical across runs .
	for _, entry := range pub.Subjects {
		st, err := decodeState(in.States[entry.SubjectID])
		if err != nil {
			return nil, fmt.Errorf("cluepuzzle: decode state of %q: %w", entry.SubjectID, err)
		}
		clueCount := len(priv.PrivateClues[entry.SubjectID])
		if priv.Teams != nil {
			clueCount = 0
			for _, member := range priv.Teams[entry.SubjectID] {
				clueCount += len(priv.PrivateClues[member])
			}
		}
		out.Progress = append(out.Progress, progressView{
			SubjectID:   entry.SubjectID,
			ClueCount:   clueCount,
			Submissions: st.Submissions,
		})
		memberID := entry.SubjectID
		authorised := entry.SubjectID == in.Viewer.Subject.ID
		if priv.Teams != nil {
			memberID = in.Viewer.MembershipID
			authorised = authorised && hasMember(priv.Teams[entry.SubjectID], memberID)
		}
		if authorised {
			// Only the frozen seat owner receives the clue text.
			clues := make([]clueView, 0, len(priv.PrivateClues[memberID]))
			for _, c := range priv.PrivateClues[memberID] {
				clues = append(clues, clueView{
					ConstraintID: c.ConstraintID,
					Text:         c.Text,
					Kind:         c.Kind,
					Cards:        c.Cards,
					Position:     c.Position,
				})
			}
			out.PrivateClues = clues
			out.SelfIsController = pub.ControlSubjectID == "" || pub.ControlSubjectID == entry.SubjectID
			if priv.Teams != nil {
				out.SelfIsController = in.Viewer.CanControl
			}
			assignment := st.Assignment
			if assignment == nil {
				assignment = map[string]string{}
			}
			out.Self = &selfView{
				SubjectID:   entry.SubjectID,
				Assignment:  assignment,
				Submissions: st.Submissions,
			}
		}
		// The team draft is the controller's latest accepted mapping. It is
		// published to the team because the plan allows members to see the public
		// draft, and because a team that cannot see the draft cannot coordinate.
		if authorised && len(st.Assignment) > 0 && len(out.TeamDraft) == 0 {
			draft := make(map[string]string, len(st.Assignment))
			for slot, cardID := range st.Assignment {
				draft[slot] = cardID
			}
			out.TeamDraft = draft
		}
	}

	// The solution is withheld from every viewer until the round is terminal; a
	// referee may inspect it at any time.
	//
	// The full constraint list is a different matter: it contains every player's
	// private clue, so it is published only once the round is terminal. A referee
	// inspecting the answer mid-round sees the mapping and nothing else — the host
	// may grant a team-wide extension but must not read a private clue
	// .
	published := in.Phase == modes.PhaseRevealed || in.Phase == modes.PhaseSettled
	if in.Phase != modes.PhaseCancelled && (in.Viewer.SeesSecrets() || published) {
		labels := make(map[string]string, len(pub.Cards))
		for _, c := range pub.Cards {
			labels[c.CardID] = c.Label
		}
		reveal := answerReveal{
			Slots:         make([]revealSlot, 0, len(pub.Slots)),
			SolutionCount: priv.SolutionCount,
			Constraints:   make([]clueView, 0, len(priv.Constraints)),
		}
		for _, slot := range pub.Slots {
			key := fmt.Sprintf("%d", slot.SlotIndex)
			cardID := priv.Solution[key]
			reveal.Slots = append(reveal.Slots, revealSlot{
				SlotIndex: slot.SlotIndex,
				CardID:    cardID,
				Label:     labels[cardID],
			})
		}
		if published {
			for _, c := range priv.Constraints {
				reveal.Constraints = append(reveal.Constraints, clueView{
					ConstraintID: c.ConstraintID,
					Text:         c.Text,
					Kind:         c.Kind,
					Cards:        c.Cards,
					Position:     c.Position,
				})
			}
		}
		out.Answer = &reveal
	}
	return modeutil.Marshal(out), nil
}

// Settle implements modes.Mode.
//
// the display score is floor(100 × satisfied constraints / fixed total
// constraints). The total is the number of constraints the puzzle published at
// prepare time, so omitting a card cannot shrink the denominator. An unsubmitted
// round scores 0 rather than being skipped.
func (*Mode) Settle(in modes.SettleInput) ([]modes.Score, error) {
	if in.Phase == modes.PhaseCancelled {
		return nil, fmt.Errorf("cluepuzzle: cancelled round cannot settle")
	}
	pub, err := decodePublic(in.Public)
	if err != nil {
		return nil, err
	}
	priv, err := decodePrivate(in.Private)
	if err != nil {
		return nil, err
	}
	total := pub.ConstraintCount
	if total <= 0 {
		total = len(priv.Constraints)
	}
	if total <= 0 {
		return nil, fmt.Errorf("cluepuzzle: the round has no constraint to score against")
	}
	cardCount := len(pub.Cards)

	// Saved legacy rounds used a single collaborative answer. Preserve their
	// scoring semantics; new rounds below grade each frozen team separately.
	var teamAssignment map[string]string
	for _, entry := range pub.Subjects {
		st, err := decodeState(in.States[entry.SubjectID])
		if err != nil {
			return nil, fmt.Errorf("cluepuzzle: decode state of %q: %w", entry.SubjectID, err)
		}
		if len(st.Assignment) == 0 {
			continue
		}
		if entry.SubjectID == pub.ControlSubjectID || teamAssignment == nil {
			teamAssignment = st.Assignment
		}
	}

	grade := func(teamAssignment map[string]string) (int, []string, []string, error) {
		satisfied := 0
		var satisfiedIDs, unsatisfiedIDs []string
		if len(teamAssignment) > 0 {
			sol := make(solution, len(teamAssignment))
			for slot, cardID := range teamAssignment {
				var index int
				if _, err := fmt.Sscanf(slot, "%d", &index); err != nil {
					return 0, nil, nil, fmt.Errorf("cluepuzzle: stored slot %q is not an index: %w", slot, err)
				}
				sol[index] = cardID
			}
			for _, c := range priv.Constraints {
				ok, err := satisfies(c, sol, cardCount)
				if err != nil {
					return 0, nil, nil, err
				}
				if ok {
					satisfied++
					satisfiedIDs = append(satisfiedIDs, c.ConstraintID)
				} else {
					unsatisfiedIDs = append(unsatisfiedIDs, c.ConstraintID)
				}
			}
		} else {
			for _, c := range priv.Constraints {
				unsatisfiedIDs = append(unsatisfiedIDs, c.ConstraintID)
			}
		}
		// Deterministic ordering so the explanation is byte-stable.
		sort.Strings(satisfiedIDs)
		sort.Strings(unsatisfiedIDs)

		// floor(100 × satisfied / total) display points, stored as scoreUnits.
		units := (100 * satisfied / total) * 100

		return units, satisfiedIDs, unsatisfiedIDs, nil
	}
	scores := make([]modes.Score, 0, len(pub.Subjects))
	for _, entry := range pub.Subjects {
		assignment := teamAssignment
		if priv.Teams != nil {
			st, err := decodeState(in.States[entry.SubjectID])
			if err != nil {
				return nil, err
			}
			assignment = st.Assignment
		}
		units, satisfiedIDs, unsatisfiedIDs, err := grade(assignment)
		if err != nil {
			return nil, err
		}
		satisfied := len(satisfiedIDs)

		explanation := modeutil.Marshal(struct {
			Satisfied      int      `json:"satisfiedConstraints"`
			Total          int      `json:"totalConstraints"`
			SatisfiedIDs   []string `json:"satisfiedIds"`
			UnsatisfiedIDs []string `json:"unsatisfiedIds"`
			Submitted      bool     `json:"submitted"`
			Formula        string   `json:"formula"`
			SolutionCount  int      `json:"solutionCount"`
		}{
			Satisfied:      satisfied,
			Total:          total,
			SatisfiedIDs:   nonNil(satisfiedIDs),
			UnsatisfiedIDs: nonNil(unsatisfiedIDs),
			Submitted:      len(assignment) > 0,
			Formula:        "floor(100*satisfied/total)",
			SolutionCount:  priv.SolutionCount,
		})
		score := modes.Score{
			Subject:       modes.Subject{ID: entry.SubjectID, Kind: entry.SubjectKind},
			ScoreUnits:    units,
			MaxScoreUnits: modeutil.Units(100),
			Explanation:   explanation,
		}
		if err := score.Validate(); err != nil {
			return nil, err
		}
		scores = append(scores, score)
	}
	return modes.RankScores(scores), nil
}

// nonNil returns an empty slice for nil so the JSON carries [] rather than null.
func nonNil(in []string) []string {
	if in == nil {
		return []string{}
	}
	return in
}
