// Package aptitudegrid implements G02 适性交叉占格 .
//
// A 3×3 board whose rows are distance conditions and whose columns are running
// style conditions (the default), or the transposed pair. A cell is claimed by
// submitting a character that satisfies both of that cell's conditions; a valid
// claim is irreversible and a cell can only be claimed once.
//
// The mode supports the two documented play forms:
// - independent challenge: 180 s, nine different characters, one cell each,
// scored floor(100×correctCells/9);
// - two-player alternating: players take turns claiming cells, the board
// closes on a full board, no legal move, or nine valid claims, and the
// player with more cells wins with ties sharing a rank.
package aptitudegrid

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"time"

	"github.com/bhcn/umaguessinggame/internal/modes"
	"github.com/bhcn/umaguessinggame/internal/modes/modeutil"
)

// ModeID is the stable mode identifier.
const ModeID = "aptitude-grid"

// RulesVersion is the rule revision this implementation satisfies.
const RulesVersion = "2.0.0-draft.1"

// Board geometry: the board is always 3×3 .
const (
	// GridRows and GridCols define the board size.
	GridRows = 3
	GridCols = 3
	// CellCount is the total number of cells.
	CellCount = GridRows * GridCols
)

// Timing rules .
const (
	// TurnSeconds is the per-turn limit in the alternating form.
	TurnSeconds = 30
	// ChallengeSeconds is the independent-challenge window.
	ChallengeSeconds = 180
	// ConsecutiveTimeouts ends a personal match after two consecutive skips.
	ConsecutiveTimeouts = 2
)

// Action kinds.
//
// ActionClaim is the contract discriminator (ModeAction → GridClaim) and must be
// the exact wire string . ActionSkip has no contract discriminator: it is
// a server-side turn skip the room layer submits when a turn times out, so it
// stays an internal kind and is never accepted from a client submission.
const (
	// ActionClaim claims one cell with one character.
	ActionClaim = "grid.claim"
	// ActionSkip passes the turn (internal, submitted by the room layer on
	// timeout; not a client action).
	ActionSkip = "grid.skip"
)

// ContractKinds returns the ModeAction discriminators this mode accepts from a
// client. ActionSkip is deliberately absent: it is not in the contract's
// discriminator mapping.
func (*Mode) ContractKinds() []string { return []string{ActionClaim} }

// Rejection codes.
const (
	CodeInvalidPayload   = "invalid_payload"
	CodeUnknownAction    = "unknown_action"
	CodeCellOccupied     = "cell_occupied"
	CodeCharacterUsed    = "character_used"
	CodeNotSatisfied     = "condition_not_satisfied"
	CodeUnknownCharacter = "unknown_character"
	CodeOutOfTurn        = "out_of_turn"
	CodeBoardClosed      = "board_closed"
	CodeNoLegalMove      = "no_legal_move"
)

// Variant is the play form .
type Variant string

const (
	// VariantChallenge is the independent 180 s challenge.
	VariantChallenge Variant = "challenge"
	// VariantDuel is the two-player alternating board.
	VariantDuel Variant = "duel"
)

// Config is the round configuration for G02.
type Config struct {
	Variant Variant `json:"variant"`
	// RowAxis is the axis used for board rows: "distance" (default) or
	// "style". The columns use the other axis.
	RowAxis string `json:"rowAxis"`
}

// decodeConfig normalises the configuration.
func decodeConfig(raw json.RawMessage) (Config, error) {
	cfg := Config{Variant: VariantChallenge, RowAxis: "distance"}
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &cfg); err != nil {
			return Config{}, fmt.Errorf("aptitudegrid: invalid config: %w", err)
		}
	}
	if cfg.Variant == "" {
		cfg.Variant = VariantChallenge
	}
	if cfg.RowAxis == "" {
		cfg.RowAxis = "distance"
	}
	switch cfg.Variant {
	case VariantChallenge, VariantDuel:
	default:
		return Config{}, fmt.Errorf("aptitudegrid: unknown variant %q", cfg.Variant)
	}
	switch cfg.RowAxis {
	case "distance", "style":
	default:
		return Config{}, fmt.Errorf("aptitudegrid: rowAxis must be distance or style, got %q", cfg.RowAxis)
	}
	return cfg, nil
}

// Ladders used to build the board axes, in the documented order .
var (
	distanceLadder = []string{"短", "英", "中"}
	styleLadder    = []string{"逃", "先", "差"}
)

// Aptitudes is one character's graded aptitudes for the three axes.
//
// A missing grade is the empty string and means "unknown": an unknown grade
// never satisfies a condition .
type Aptitudes struct {
	Surface  map[string]string `json:"surface"`
	Distance map[string]string `json:"distance"`
	Style    map[string]string `json:"style"`
}

// gradeOn returns the grade for an axis value, or "" when unknown.
func (a Aptitudes) gradeOn(axis, value string) string {
	var table map[string]string
	switch axis {
	case "surface":
		table = a.Surface
	case "distance":
		table = a.Distance
	case "style":
		table = a.Style
	}
	if table == nil {
		return ""
	}
	return table[value]
}

// satisfies reports whether the character meets a cell's two conditions. Both
// conditions must be graded A .
func (a Aptitudes) satisfies(rowAxis, rowValue, colAxis, colValue string) bool {
	return a.gradeOn(rowAxis, rowValue) == "A" && a.gradeOn(colAxis, colValue) == "A"
}

// Card is one character in the round pool.
type Card struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	ImagePath string    `json:"imagePath"`
	Aptitudes Aptitudes `json:"aptitudes"`
}

// parseCard converts one decoded record into a Card.
func parseCard(fm modeutil.FieldMap) (Card, bool) {
	id := fm.Str("characterId", "id", "character_id")
	if id == "" {
		return Card{}, false
	}
	c := Card{
		ID:        id,
		Name:      fm.Str("name", "nameZh", "name_zh"),
		ImagePath: fm.Str("imagePath", "image_local_path"),
	}
	if apt, ok := fm.Object("aptitudes", "aptitude"); ok {
		c.Aptitudes = parseAptitudes(apt)
	}
	return c, true
}

// parseAptitudes reads the three aptitude tables. Both a nested
// {surface:{...},distance:{...},style:{...}} object and a flat
// {"草地":"A"} map are tolerated.
func parseAptitudes(fm modeutil.FieldMap) Aptitudes {
	out := Aptitudes{
		Surface:  map[string]string{},
		Distance: map[string]string{},
		Style:    map[string]string{},
	}
	if sub, ok := fm.Object("surface"); ok {
		for k := range sub {
			out.Surface[k] = sub.Str(k)
		}
	}
	if sub, ok := fm.Object("distance"); ok {
		for k := range sub {
			out.Distance[k] = sub.Str(k)
		}
	}
	if sub, ok := fm.Object("style", "runningStyle", "running_style"); ok {
		for k := range sub {
			out.Style[k] = sub.Str(k)
		}
	}
	return out
}

// loadCards reads the aptitude card section.
func loadCards(src modes.ContentSource) ([]Card, error) {
	raw, err := modeutil.Section(src, "aptitude-grid.cards", "aptitudeGrid.cards", "aptitude-grid")
	if err != nil {
		return nil, err
	}
	records, err := modeutil.Records(raw, "cards", "characters", "items")
	if err != nil {
		return nil, err
	}
	out := make([]Card, 0, len(records))
	for _, rec := range records {
		if c, ok := parseCard(rec); ok {
			out = append(out, c)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

// Condition is one board condition on an axis.
type Condition struct {
	Axis  string `json:"axis"`
	Value string `json:"value"`
}

// Cell is one board cell with its two explicit conditions. The result screen
// must show both concrete conditions per cell .
type Cell struct {
	// CellID is the opaque string the contract's GridClaim addresses a cell by.
	// It is derived from the board position so it is stable for a layout, but it
	// carries nothing a client could exploit to skip validation.
	CellID  string    `json:"cellId"`
	Index   int       `json:"index"`
	Row     int       `json:"row"`
	Col     int       `json:"col"`
	RowCond Condition `json:"rowCondition"`
	ColCond Condition `json:"colCondition"`
}

// buildBoard lays out the nine cells deterministically from the config.
func buildBoard(cfg Config) []Cell {
	rowLadder, colLadder := distanceLadder, styleLadder
	rowAxis, colAxis := "distance", "style"
	if cfg.RowAxis == "style" {
		rowLadder, colLadder = styleLadder, distanceLadder
		rowAxis, colAxis = "style", "distance"
	}
	cells := make([]Cell, 0, CellCount)
	for r := 0; r < GridRows; r++ {
		for c := 0; c < GridCols; c++ {
			index := r*GridCols + c
			cells = append(cells, Cell{
				CellID:  cellIDFor(r, c),
				Index:   index,
				Row:     r,
				Col:     c,
				RowCond: Condition{Axis: rowAxis, Value: rowLadder[r]},
				ColCond: Condition{Axis: colAxis, Value: colLadder[c]},
			})
		}
	}
	return cells
}

// cellIDFor builds the opaque cell id for a board position.
func cellIDFor(row, col int) string {
	return fmt.Sprintf("cell-%d-%d", row, col)
}

// cellIndexByID resolves an opaque cell id to its board index, or -1.
func cellIndexByID(cells []Cell, cellID string) int {
	for _, cell := range cells {
		if cell.CellID == cellID {
			return cell.Index
		}
	}
	return -1
}

// solvable reports whether the pool can fill every cell with a distinct
// character. This is the pool solvability check the competitive board requires
// before it opens .
//
// It runs an augmenting-path bipartite matching (Kuhn's algorithm). Cell order
// and candidate order are both fixed by the sorted pool, so the result is
// deterministic.
func solvable(cards []Card, cells []Cell) bool {
	// matchCell[cellIndex] = card index
	matchCell := make([]int, len(cells))
	for i := range matchCell {
		matchCell[i] = -1
	}

	// Precompute the adjacency so the recursion does no repeated work.
	adj := make([][]int, len(cells))
	for ci, cell := range cells {
		for cardIdx, card := range cards {
			if card.Aptitudes.satisfies(cell.RowCond.Axis, cell.RowCond.Value,
				cell.ColCond.Axis, cell.ColCond.Value) {
				adj[ci] = append(adj[ci], cardIdx)
			}
		}
	}

	// Standard Kuhn: for each cell in order, try to find an augmenting path.
	assigned := make([]int, len(cards))
	for i := range assigned {
		assigned[i] = -1
	}
	var augment func(cell int, visited []bool) bool
	augment = func(cell int, visited []bool) bool {
		for _, cardIdx := range adj[cell] {
			if visited[cardIdx] {
				continue
			}
			visited[cardIdx] = true
			if assigned[cardIdx] == -1 || augment(assigned[cardIdx], visited) {
				assigned[cardIdx] = cell
				matchCell[cell] = cardIdx
				return true
			}
		}
		return false
	}
	matched := 0
	for ci := range cells {
		visited := make([]bool, len(cards))
		if augment(ci, visited) {
			matched++
		}
	}
	return matched == len(cells)
}

// claim is one occupied cell.
type claim struct {
	CellIndex   int    `json:"cellIndex"`
	CharacterID string `json:"characterId"`
	// SubjectID records who claimed it; it is what makes a concurrent claim on
	// the same cell succeed exactly once.
	SubjectID string `json:"subjectId"`
	// Turn is the global claim ordinal, 1-based.
	Turn int `json:"turn"`
}

// publicPrepared is the non-secret prepared state.
type publicPrepared struct {
	ModeID       string        `json:"modeId"`
	RulesVersion string        `json:"rulesVersion"`
	Config       Config        `json:"config"`
	Cells        []Cell        `json:"cells"`
	Pool         []poolCard    `json:"pool"`
	Subjects     []rosterEntry `json:"subjects"`
	// Solvable reports whether the full board can be filled with distinct
	// characters from this pool.
	Solvable bool `json:"solvable"`
}

// poolCard is the public view of a card.
type poolCard struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	ImagePath string `json:"imagePath"`
}

// rosterEntry is one participant.
type rosterEntry struct {
	SubjectID   string            `json:"subjectId"`
	SubjectKind modes.SubjectKind `json:"subjectKind"`
}

// privatePrepared carries the graded aptitudes, which are the answer material:
// the challenge form is closed-book on aptitudes until reveal.
type privatePrepared struct {
	Cards []Card `json:"cards"`
}

// boardState is the shared board, stored in the round's public state because
// every participant sees which cells are taken.
type boardState struct {
	Claims []claim `json:"claims"`
	// Closed reports that the board can no longer accept claims.
	Closed bool `json:"closed"`
	// TurnSubjectID is whose turn it is in the alternating form.
	TurnSubjectID string         `json:"turnSubjectId"`
	TurnNumber    int            `json:"turnNumber,omitempty"`
	Timeouts      map[string]int `json:"timeouts,omitempty"`
}

// recordTimeout retains per-player consecutive timeouts even when no client is
// connected to submit a skip. The duel ends when a player misses two own turns.
func (b *boardState) recordTimeout(subjectID string) {
	if b.Timeouts == nil {
		b.Timeouts = make(map[string]int)
	}
	b.Timeouts[subjectID]++
	if b.Timeouts[subjectID] >= ConsecutiveTimeouts {
		b.Closed = true
	}
}

// claimedCell reports whether a cell index is taken in the shared (duel) board,
// and by which claim.
func (b boardState) claimedCell(index int) (claim, bool) {
	for _, c := range b.Claims {
		if c.CellIndex == index {
			return c, true
		}
	}
	return claim{}, false
}

// claimedCellBy reports whether a subject already claimed a cell on their own
// board. The challenge form gives every subject an independent board, so
// occupancy there is per subject, not global .
func (b boardState) claimedCellBy(subjectID string, index int) (claim, bool) {
	for _, c := range b.Claims {
		if c.SubjectID == subjectID && c.CellIndex == index {
			return c, true
		}
	}
	return claim{}, false
}

// usedCharacter reports whether a character was already played on the shared
// (duel) board.
func (b boardState) usedCharacter(id string) bool {
	for _, c := range b.Claims {
		if c.CharacterID == id {
			return true
		}
	}
	return false
}

// usedCharacterBy reports whether a subject already played a character on their
// own board. Each challenge board must use nine different characters, but the
// same character may appear on two independent boards .
func (b boardState) usedCharacterBy(subjectID, characterID string) bool {
	for _, c := range b.Claims {
		if c.SubjectID == subjectID && c.CharacterID == characterID {
			return true
		}
	}
	return false
}

// claimsBy returns the claims a subject owns, in board order.
func (b boardState) claimsBy(subjectID string) []claim {
	out := make([]claim, 0, CellCount)
	for _, c := range b.Claims {
		if c.SubjectID == subjectID {
			out = append(out, c)
		}
	}
	return out
}

// state is one subject's participant state.
type state struct {
	// Claims lists the cell indices this subject owns.
	Claims []int `json:"claims"`
	// Attempts and Timeouts track the per-subject counters.
	Attempts int `json:"attempts"`
	// ConsecutiveTimeouts ends a personal match after two in a row .
	ConsecutiveTimeouts int `json:"consecutiveTimeouts"`
	// TimedOut reports that the subject was dropped by consecutive timeouts.
	TimedOut bool `json:"timedOut"`
}

// Mode implements modes.Mode for G02.
type Mode struct{}

// New returns the aptitude-grid mode.
func New() *Mode { return &Mode{} }

// ID implements modes.Mode.
func (*Mode) ID() string { return ModeID }

// RulesVersion implements modes.Mode.
func (*Mode) RulesVersion() string { return RulesVersion }

// Title implements modes.Mode.
func (*Mode) Title() string { return "适性交叉占格" }

// Summary implements modes.Mode.
func (*Mode) Summary() string {
	return "在 3×3 适性交叉盘上用满足条件的角色占格，占格不可撤销。"
}

// InformationStyles reports which information forms this mode implements.
func (*Mode) InformationStyles() []string {
	return []string{"closed"}
}

// RoundWindow implements modes.Mode.
//
// Independent challenges have a complete 180-second board window. Duels use
// the per-turn window below; the room layer refreshes that clock on each turn.
func (*Mode) RoundWindow(raw json.RawMessage) time.Duration {
	cfg, err := decodeConfig(raw)
	if err != nil {
		return 0
	}
	if cfg.Variant == VariantChallenge {
		return time.Duration(ChallengeSeconds) * time.Second
	}
	return time.Duration(TurnSeconds) * time.Second
}

// TurnWindow implements modes.Mode.
//
// The duel form IS turn-based: one subject holds the turn and only they may act,
// with TurnSeconds to decide. Before this existed the round only had a single
// whole-round deadline, so if the player holding the turn did nothing the board
// sat on their turn until the entire round expired — the same defect strategy-race
// had. The challenge form is not turn-based, so it reports 0.
func (*Mode) TurnWindow(raw json.RawMessage) time.Duration {
	cfg, err := decodeConfig(raw)
	if err != nil {
		return 0
	}
	if cfg.Variant != VariantDuel {
		return 0
	}
	return time.Duration(TurnSeconds) * time.Second
}

// ExpireTurn implements modes.TurnExpirer.
//
// When the turn clock runs out, the turn passes to the next subject. Nobody
// gains a cell: a timeout is not a move, so the board is unchanged except for
// whose turn it is. That keeps the rule "no silent free claim" while making sure
// an absent player cannot stall the board.
//
// `more` is false once the board is closed or nobody has a legal move, so the
// room layer stops renewing the turn clock and lets the round finish.
func (*Mode) ExpireTurn(public json.RawMessage) (json.RawMessage, bool, error) {
	env, err := decodePublic(public)
	if err != nil {
		return nil, false, err
	}
	if env.Round.Config.Variant != VariantDuel || env.Board.Closed {
		return public, false, nil
	}
	if len(env.Round.Subjects) == 0 {
		return public, false, nil
	}
	// Pass the turn; do not invent a claim.
	env.Board.recordTimeout(env.Board.TurnSubjectID)
	env.Board.TurnNumber++
	env.Board.TurnSubjectID = nextTurn(env.Round.Subjects, env.Board.TurnSubjectID)
	next := modeutil.Marshal(env)
	return next, !env.Board.Closed, nil
}

// OpenTurn implements modes.TurnProgress. TurnNumber also distinguishes a pass
// in a one-subject practice room where the holder does not change.
func (*Mode) OpenTurn(public json.RawMessage) (string, bool, error) {
	env, err := decodePublic(public)
	if err != nil {
		return "", false, err
	}
	return fmt.Sprintf("%d:%d:%s", env.Board.TurnNumber, len(env.Board.Claims), env.Board.TurnSubjectID),
		env.Round.Config.Variant == VariantDuel && !env.Board.Closed, nil
}

// MinSubjects implements modes.Mode.
func (*Mode) MinSubjects() int { return 1 }

// MaxSubjects implements modes.Mode.
func (*Mode) MaxSubjects() int { return 2 }

// Requirement implements modes.Mode .
func (*Mode) Requirement() modes.ContentRequirement {
	return modes.ContentRequirement{
		Fields:      []string{"aptitudes.distance", "aptitudes.style", "aptitudes.surface"},
		MinEntities: CellCount,
		NeedsAssets: false,
	}
}

// ValidateConfig implements modes.Mode.
func (*Mode) ValidateConfig(cfg json.RawMessage) error {
	_, err := decodeConfig(cfg)
	return err
}

// Prepare implements modes.Mode. It refuses an unsolvable pool before the round
// opens rather than after .
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
	cards, err := loadCards(in.Content)
	if err != nil {
		return modes.Prepared{}, err
	}
	cells := buildBoard(cfg)

	roster := make([]rosterEntry, 0, len(in.Subject))
	seen := make(map[string]struct{}, len(in.Subject))
	for _, subj := range in.Subject {
		if err := subj.Validate(); err != nil {
			return modes.Prepared{}, err
		}
		if _, dup := seen[subj.ID]; dup {
			return modes.Prepared{}, fmt.Errorf("aptitudegrid: duplicate subject %q", subj.ID)
		}
		seen[subj.ID] = struct{}{}
		roster = append(roster, rosterEntry{SubjectID: subj.ID, SubjectKind: subj.Kind})
	}
	switch cfg.Variant {
	case VariantDuel:
		if len(roster) != 2 {
			return modes.Prepared{}, fmt.Errorf(
				"aptitudegrid: the duel variant needs exactly 2 subjects, got %d", len(roster),
			)
		}
	case VariantChallenge:
		if len(roster) < 1 || len(roster) > 2 {
			return modes.Prepared{}, fmt.Errorf(
				"aptitudegrid: the challenge variant needs 1–2 subjects, got %d", len(roster),
			)
		}
	}

	// The pool must be able to fill the whole board with distinct characters.
	if !solvable(cards, cells) {
		return modes.Prepared{}, fmt.Errorf(
			"aptitudegrid: pool of %d cards cannot fill the %dx%d board with distinct characters",
			len(cards), GridRows, GridCols,
		)
	}

	pubCards := make([]poolCard, 0, len(cards))
	for _, c := range cards {
		pubCards = append(pubCards, poolCard{ID: c.ID, Name: c.Name, ImagePath: c.ImagePath})
	}

	board := boardState{}
	if cfg.Variant == VariantDuel {
		// Deterministic first mover: the seed decides, not the arrival order.
		board.TurnSubjectID = roster[modeutil.PickIndex(in.Scope.Seed, "aptitudegrid.first", len(roster))].SubjectID
	}

	pub := publicPrepared{
		ModeID:       ModeID,
		RulesVersion: RulesVersion,
		Config:       cfg,
		Cells:        cells,
		Pool:         pubCards,
		Subjects:     roster,
		Solvable:     true,
	}
	return modes.Prepared{
		// The board travels in the public prepared state: it is visible to all
		// participants, and the room layer rewrites it as claims land.
		Public: modeutil.Marshal(struct {
			Round publicPrepared `json:"round"`
			Board boardState     `json:"board"`
		}{Round: pub, Board: board}),
		Private:       modeutil.Marshal(privatePrepared{Cards: cards}),
		QuestionCount: CellCount,
	}, nil
}

// preparedEnvelope mirrors the wrapper Prepare emits.
type preparedEnvelope struct {
	Round publicPrepared `json:"round"`
	Board boardState     `json:"board"`
}

// decodePublic splits the prepared public state back into its two parts.
func decodePublic(raw json.RawMessage) (preparedEnvelope, error) {
	var env preparedEnvelope
	if len(raw) == 0 {
		return env, nil
	}
	if err := json.Unmarshal(raw, &env); err != nil {
		return env, err
	}
	return env, nil
}

// claimPayload is the body of ActionClaim, matching the contract's GridClaim
// schema: {kind, cellId, characterId}, where cellId is the cell's opaque string
// identifier. CellIndex is derived from cellId via the board layout.
type claimPayload struct {
	Kind        string `json:"kind"`
	CellID      string `json:"cellId"`
	CharacterID string `json:"characterId"`
}

// Apply implements modes.Mode.
func (*Mode) Apply(in modes.ApplyInput) (modes.Decision, error) {
	switch in.Action.Kind {
	case ActionClaim:
	case ActionSkip:
		return (*Mode)(nil).applySkip(in)
	default:
		return modes.Rejected(CodeUnknownAction,
			fmt.Sprintf("aptitudegrid: unsupported action %q", in.Action.Kind)), nil
	}
	if err := in.Action.Validate(); err != nil {
		return modes.Rejected(CodeInvalidPayload, err.Error()), nil
	}
	var payload claimPayload
	if err := json.Unmarshal(in.Action.Payload, &payload); err != nil {
		return modes.Rejected(CodeInvalidPayload,
			"aptitudegrid: claim payload must carry cellIndex and characterId"), nil
	}
	if payload.Kind != "" && payload.Kind != ActionClaim {
		return modes.Rejected(CodeInvalidPayload, fmt.Sprintf(
			"aptitudegrid: payload kind %q does not match action %q", payload.Kind, ActionClaim,
		)), nil
	}
	if payload.CharacterID == "" {
		return modes.Rejected(CodeInvalidPayload, "aptitudegrid: empty characterId"), nil
	}
	if payload.CellID == "" {
		return modes.Rejected(CodeInvalidPayload, "aptitudegrid: empty cellId"), nil
	}

	env, err := decodePublic(in.Public)
	if err != nil {
		return modes.Decision{}, fmt.Errorf("aptitudegrid: decode public state: %w", err)
	}
	var priv privatePrepared
	if len(in.Private) > 0 {
		if err := json.Unmarshal(in.Private, &priv); err != nil {
			return modes.Decision{}, fmt.Errorf("aptitudegrid: decode private state: %w", err)
		}
	}
	var st state
	if len(in.State) > 0 {
		if err := json.Unmarshal(in.State, &st); err != nil {
			return modes.Decision{}, fmt.Errorf("aptitudegrid: decode subject state: %w", err)
		}
	}

	// Resolve the opaque cell id against this board layout.
	cellIndex := cellIndexByID(env.Round.Cells, payload.CellID)
	if cellIndex < 0 {
		return modes.Rejected(CodeInvalidPayload,
			fmt.Sprintf("aptitudegrid: cell %q is not on this board", payload.CellID)), nil
	}

	board := env.Board
	if env.Round.Config.Variant == VariantDuel && board.Closed {
		return modes.Rejected(CodeBoardClosed, "aptitudegrid: the board is closed"), nil
	}
	if st.TimedOut {
		return modes.Rejected(CodeBoardClosed, "aptitudegrid: you were dropped from this board"), nil
	}
	// Alternating form: only the subject holding the turn may act .
	if env.Round.Config.Variant == VariantDuel && board.TurnSubjectID != "" &&
		board.TurnSubjectID != in.Subject.ID {
		return modes.Rejected(CodeOutOfTurn,
			fmt.Sprintf("aptitudegrid: it is not your turn (turn holder is %q)", board.TurnSubjectID)), nil
	}

	// Occupancy and character reuse are global in the duel (one shared board)
	// and per subject in the challenge (one independent board each), per
	duel := env.Round.Config.Variant == VariantDuel
	if duel {
		// Two concurrent claims on the same cell can only succeed once: the
		// second transaction re-reads the committed board and sees the first
		// claim .
		if existing, taken := board.claimedCell(cellIndex); taken {
			return modes.Rejected(CodeCellOccupied, fmt.Sprintf(
				"aptitudegrid: cell %d was already claimed by %q", cellIndex, existing.SubjectID)), nil
		}
		if board.usedCharacter(payload.CharacterID) {
			return modes.Rejected(CodeCharacterUsed,
				fmt.Sprintf("aptitudegrid: character %q was already used", payload.CharacterID)), nil
		}
	} else {
		if existing, taken := board.claimedCellBy(in.Subject.ID, cellIndex); taken {
			return modes.Rejected(CodeCellOccupied, fmt.Sprintf(
				"aptitudegrid: you already claimed cell %d", existing.CellIndex)), nil
		}
		if board.usedCharacterBy(in.Subject.ID, payload.CharacterID) {
			return modes.Rejected(CodeCharacterUsed,
				fmt.Sprintf("aptitudegrid: character %q was already used on your board", payload.CharacterID)), nil
		}
	}

	var card Card
	found := false
	for _, c := range priv.Cards {
		if c.ID == payload.CharacterID {
			card, found = c, true
			break
		}
	}
	if !found {
		return modes.Rejected(CodeUnknownCharacter,
			fmt.Sprintf("aptitudegrid: character %q is not in this round's pool", payload.CharacterID)), nil
	}

	cell := env.Round.Cells[cellIndex]
	if !card.Aptitudes.satisfies(cell.RowCond.Axis, cell.RowCond.Value,
		cell.ColCond.Axis, cell.ColCond.Value) {
		// An illegal claim does not consume the turn .
		return modes.Rejected(CodeNotSatisfied, fmt.Sprintf(
			"aptitudegrid: character %q does not satisfy %s=%s and %s=%s",
			payload.CharacterID,
			cell.RowCond.Axis, cell.RowCond.Value,
			cell.ColCond.Axis, cell.ColCond.Value,
		)), nil
	}

	st.Attempts++
	st.Claims = append(st.Claims, cellIndex)
	st.ConsecutiveTimeouts = 0

	feedback := modeutil.Marshal(struct {
		CellIndex   int       `json:"cellIndex"`
		CharacterID string    `json:"characterId"`
		RowCond     Condition `json:"rowCondition"`
		ColCond     Condition `json:"colCondition"`
		Claims      int       `json:"claims"`
	}{
		CellIndex:   cellIndex,
		CharacterID: payload.CharacterID,
		RowCond:     cell.RowCond,
		ColCond:     cell.ColCond,
		Claims:      len(st.Claims),
	})

	dec := modes.Accepted(feedback, modeutil.Marshal(st))
	// A claim only finishes the subject in the challenge form, where nine
	// claims exhaust their board .
	if env.Round.Config.Variant == VariantChallenge && len(st.Claims) >= CellCount {
		dec.Finished = true
	}
	return dec, nil
}

// applySkip handles a timeout/pass action.
func (*Mode) applySkip(in modes.ApplyInput) (modes.Decision, error) {
	env, err := decodePublic(in.Public)
	if err != nil {
		return modes.Decision{}, fmt.Errorf("aptitudegrid: decode public state: %w", err)
	}
	var st state
	if len(in.State) > 0 {
		if err := json.Unmarshal(in.State, &st); err != nil {
			return modes.Decision{}, fmt.Errorf("aptitudegrid: decode subject state: %w", err)
		}
	}
	if env.Round.Config.Variant == VariantDuel && env.Board.Closed {
		return modes.Rejected(CodeBoardClosed, "aptitudegrid: the board is closed"), nil
	}
	if env.Round.Config.Variant == VariantDuel && env.Board.TurnSubjectID != in.Subject.ID {
		return modes.Rejected(CodeOutOfTurn, "aptitudegrid: wait for your turn"), nil
	}

	st.ConsecutiveTimeouts++
	if st.ConsecutiveTimeouts >= ConsecutiveTimeouts {
		// Two consecutive timeouts end the subject's personal match .
		st.TimedOut = true
	}
	feedback := modeutil.Marshal(struct {
		ConsecutiveTimeouts int  `json:"consecutiveTimeouts"`
		TimedOut            bool `json:"timedOut"`
	}{
		ConsecutiveTimeouts: st.ConsecutiveTimeouts,
		TimedOut:            st.TimedOut,
	})
	dec := modes.Accepted(feedback, modeutil.Marshal(st))
	dec.Finished = st.TimedOut
	return dec, nil
}

// CommitShared implements modeutil.SharedStateMode.
//
// It folds an accepted claim into the shared board. The room layer calls this
// inside its write transaction, after Apply accepted the action, and persists
// the returned public state. Because it is a pure fold over the pre-action
// board, two concurrent claims on the same cell cannot both succeed: the second
// transaction re-reads the committed board and Apply rejects it as occupied.
func (*Mode) CommitShared(in modes.ApplyInput, dec modes.Decision) (json.RawMessage, json.RawMessage, error) {
	env, err := decodePublic(in.Public)
	if err != nil {
		return nil, nil, fmt.Errorf("aptitudegrid: decode public state: %w", err)
	}
	var priv privatePrepared
	if len(in.Private) > 0 {
		if err := json.Unmarshal(in.Private, &priv); err != nil {
			return nil, nil, fmt.Errorf("aptitudegrid: decode private state: %w", err)
		}
	}
	var st state
	if len(in.State) > 0 {
		if err := json.Unmarshal(in.State, &st); err != nil {
			return nil, nil, fmt.Errorf("aptitudegrid: decode subject state: %w", err)
		}
	}

	switch in.Action.Kind {
	case ActionClaim:
		var payload claimPayload
		if err := json.Unmarshal(in.Action.Payload, &payload); err != nil {
			return nil, nil, fmt.Errorf("aptitudegrid: decode claim payload: %w", err)
		}
		// Resolve the opaque cell id the same way Apply does, so the committed
		// board and the validated action always agree.
		cellIndex := cellIndexByID(env.Round.Cells, payload.CellID)
		if cellIndex < 0 {
			return nil, nil, fmt.Errorf("aptitudegrid: cell %q is not on this board", payload.CellID)
		}
		board := env.Board
		board.Claims = append(board.Claims, claim{
			CellIndex:   cellIndex,
			CharacterID: payload.CharacterID,
			SubjectID:   in.Subject.ID,
			Turn:        len(board.Claims) + 1,
		})
		if env.Round.Config.Variant == VariantDuel {
			delete(board.Timeouts, in.Subject.ID)
			board.TurnNumber++
			board.TurnSubjectID = nextTurn(env.Round.Subjects, in.Subject.ID)
			// The duel board closes when it is full or when no legal move
			// remains for anyone .
			if len(board.Claims) >= CellCount || !hasLegalMove(env, board, priv.Cards) {
				board.Closed = true
			}
		}
		env.Board = board
	case ActionSkip:
		if env.Round.Config.Variant == VariantDuel {
			env.Board.recordTimeout(in.Subject.ID)
			env.Board.TurnNumber++
			env.Board.TurnSubjectID = nextTurn(env.Round.Subjects, in.Subject.ID)
		}
	default:
		return nil, nil, fmt.Errorf("aptitudegrid: cannot commit action %q", in.Action.Kind)
	}

	return modeutil.Marshal(env), in.Private, nil
}

// nextTurn returns the next subject id in roster order.
func nextTurn(roster []rosterEntry, current string) string {
	if len(roster) == 0 {
		return ""
	}
	for i, entry := range roster {
		if entry.SubjectID == current {
			return roster[(i+1)%len(roster)].SubjectID
		}
	}
	return roster[0].SubjectID
}

// hasLegalMove reports whether any unclaimed cell can still be filled by an
// unused character, given the graded aptitudes captured at prepare time.
//
// A board with no legal move closes immediately (: 满盘、无合法动作或达到
// 9 次有效占格结束).
func hasLegalMove(env preparedEnvelope, board boardState, cards []Card) bool {
	for _, cell := range env.Round.Cells {
		if _, taken := board.claimedCell(cell.Index); taken {
			continue
		}
		for _, card := range cards {
			if board.usedCharacter(card.ID) {
				continue
			}
			if card.Aptitudes.satisfies(cell.RowCond.Axis, cell.RowCond.Value,
				cell.ColCond.Axis, cell.ColCond.Value) {
				return true
			}
		}
	}
	return false
}

// cellView is one cell as projected to a viewer.
type cellView struct {
	// CellID is the opaque id a claim addresses this cell by.
	//
	// It MUST be published. Apply resolves a claim through cellIndexByID over the
	// prepared board, so a client that does not receive the id can only guess it —
	// and guessing means re-implementing cellIDFor, a private server detail, in the
	// browser. The web client carried exactly that fallback (see
	// web/src/lib/api/aptitude-grid.ts), which meant the only way to play G02 was to
	// duplicate the server's formula. Publishing it removes the guess.
	CellID  string     `json:"cellId"`
	Index   int        `json:"index"`
	Row     int        `json:"row"`
	Col     int        `json:"col"`
	RowCond Condition  `json:"rowCondition"`
	ColCond Condition  `json:"colCondition"`
	Claim   *claimView `json:"claim,omitempty"`
}

// claimView is an occupied cell's public content.
type claimView struct {
	SubjectID   string `json:"subjectId"`
	CharacterID string `json:"characterId"`
	Name        string `json:"name"`
	ImagePath   string `json:"imagePath"`
	Turn        int    `json:"turn"`
	// Satisfied is shown only after reveal so the board does not become an
	// aptitude lookup table during play.
	Satisfied *bool `json:"satisfied,omitempty"`
}

// subjectProgress is the coarse per-subject progress everyone may see.
type subjectProgress struct {
	SubjectID string `json:"subjectId"`
	Claims    int    `json:"claims"`
	TimedOut  bool   `json:"timedOut"`
}

// projection is the viewer-specific board state.
type projection struct {
	ModeID        string            `json:"modeId"`
	RulesVersion  string            `json:"rulesVersion"`
	Phase         modes.Phase       `json:"phase"`
	Config        Config            `json:"config"`
	Cells         []cellView        `json:"cells"`
	Pool          []poolCard        `json:"pool"`
	Solvable      bool              `json:"solvable"`
	Closed        bool              `json:"closed"`
	TurnSubjectID string            `json:"turnSubjectId,omitempty"`
	Subjects      []subjectProgress `json:"subjects"`
	Self          *selfBoardView    `json:"self,omitempty"`
	// Aptitudes is the graded table, released only on reveal (: 开卷提供
	// 检索与候选; the closed-book challenge must not expose it early).
	Aptitudes []aptitudeReveal `json:"aptitudes,omitempty"`
}

// selfBoardView is the caller's own board state.
type selfBoardView struct {
	SubjectID           string `json:"subjectId"`
	Claims              []int  `json:"claims"`
	Attempts            int    `json:"attempts"`
	ConsecutiveTimeouts int    `json:"consecutiveTimeouts"`
	TimedOut            bool   `json:"timedOut"`
}

// aptitudeReveal is one card's graded aptitudes, emitted on reveal.
type aptitudeReveal struct {
	CharacterID string    `json:"characterId"`
	Aptitudes   Aptitudes `json:"aptitudes"`
}

// Project implements modes.Mode.
func (*Mode) Project(in modes.ProjectInput) (json.RawMessage, error) {
	env, err := decodePublic(in.Public)
	if err != nil {
		return nil, fmt.Errorf("aptitudegrid: decode public state: %w", err)
	}
	var priv privatePrepared
	if len(in.Private) > 0 {
		if err := json.Unmarshal(in.Private, &priv); err != nil {
			return nil, fmt.Errorf("aptitudegrid: decode private state: %w", err)
		}
	}

	reveal := in.Viewer.SeesSecrets() || (in.RevealNow && in.Phase.Terminal())
	byID := make(map[string]Card, len(priv.Cards))
	for _, c := range priv.Cards {
		byID[c.ID] = c
	}

	out := projection{
		ModeID:        ModeID,
		RulesVersion:  RulesVersion,
		Phase:         in.Phase,
		Config:        env.Round.Config,
		Pool:          env.Round.Pool,
		Solvable:      env.Round.Solvable,
		Closed:        env.Board.Closed,
		TurnSubjectID: env.Board.TurnSubjectID,
		Subjects:      make([]subjectProgress, 0, len(env.Round.Subjects)),
	}

	// Cells are emitted in board order, never map order.
	for _, cell := range env.Round.Cells {
		cv := cellView{
			CellID:  cell.CellID,
			Index:   cell.Index,
			Row:     cell.Row,
			Col:     cell.Col,
			RowCond: cell.RowCond,
			ColCond: cell.ColCond,
		}
		claimed, occupied := env.Board.claimedCell(cell.Index)
		if env.Round.Config.Variant == VariantChallenge {
			// Independent boards must not display or block another subject's
			// claim, even if both filled the same position with different cards.
			claimed, occupied = env.Board.claimedCellBy(in.Viewer.Subject.ID, cell.Index)
		}
		if occupied {
			view := &claimView{
				SubjectID:   claimed.SubjectID,
				CharacterID: claimed.CharacterID,
				Turn:        claimed.Turn,
			}
			if card, found := byID[claimed.CharacterID]; found {
				view.Name = card.Name
				view.ImagePath = card.ImagePath
			}
			if reveal {
				satisfied := true
				if card, found := byID[claimed.CharacterID]; found {
					satisfied = card.Aptitudes.satisfies(
						cell.RowCond.Axis, cell.RowCond.Value,
						cell.ColCond.Axis, cell.ColCond.Value,
					)
				}
				view.Satisfied = &satisfied
			}
			cv.Claim = view
		}
		out.Cells = append(out.Cells, cv)
	}

	for _, entry := range env.Round.Subjects {
		var st state
		if raw, ok := in.States[entry.SubjectID]; ok && len(raw) > 0 {
			if err := json.Unmarshal(raw, &st); err != nil {
				return nil, fmt.Errorf("aptitudegrid: decode state of %q: %w", entry.SubjectID, err)
			}
		}
		out.Subjects = append(out.Subjects, subjectProgress{
			SubjectID: entry.SubjectID,
			Claims:    len(st.Claims),
			TimedOut:  st.TimedOut || env.Board.Timeouts[entry.SubjectID] >= ConsecutiveTimeouts,
		})
		if entry.SubjectID == in.Viewer.Subject.ID {
			out.Self = &selfBoardView{
				SubjectID:           entry.SubjectID,
				Claims:              st.Claims,
				Attempts:            st.Attempts,
				ConsecutiveTimeouts: st.ConsecutiveTimeouts,
				TimedOut:            st.TimedOut || env.Board.Timeouts[entry.SubjectID] >= ConsecutiveTimeouts,
			}
		}
	}

	if reveal {
		for _, card := range priv.Cards {
			out.Aptitudes = append(out.Aptitudes, aptitudeReveal{
				CharacterID: card.ID,
				Aptitudes:   card.Aptitudes,
			})
		}
	}
	return modeutil.Marshal(out), nil
}

// Settle implements modes.Mode.
//
// Challenge: floor(100×correctCells/9) for each subject independently .
// Duel: the subject with more claimed cells wins; ties share a rank, and the
// independent-challenge score is never used to compare duel players.
func (*Mode) Settle(in modes.SettleInput) ([]modes.Score, error) {
	env, err := decodePublic(in.Public)
	if err != nil {
		return nil, fmt.Errorf("aptitudegrid: decode public state: %w", err)
	}

	scores := make([]modes.Score, 0, len(env.Round.Subjects))
	switch env.Round.Config.Variant {
	case VariantDuel:
		for _, entry := range env.Round.Subjects {
			var st state
			if raw, ok := in.States[entry.SubjectID]; ok && len(raw) > 0 {
				if err := json.Unmarshal(raw, &st); err != nil {
					return nil, fmt.Errorf("aptitudegrid: decode state of %q: %w", entry.SubjectID, err)
				}
			}
			st.TimedOut = st.TimedOut || env.Board.Timeouts[entry.SubjectID] >= ConsecutiveTimeouts
			claims := len(st.Claims)
			// The duel compares claimed cells; the score is the claim count
			// scaled to the same 100-point budget so Score.Validate holds.
			units := modeutil.Units(claims * modeutil.UnitsPerPoint / CellCount)
			explanation := modeutil.Marshal(struct {
				Claims   int     `json:"claims"`
				TimedOut bool    `json:"timedOut"`
				Variant  Variant `json:"variant"`
			}{Claims: claims, TimedOut: st.TimedOut, Variant: VariantDuel})
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
	case VariantChallenge:
		for _, entry := range env.Round.Subjects {
			var st state
			if raw, ok := in.States[entry.SubjectID]; ok && len(raw) > 0 {
				if err := json.Unmarshal(raw, &st); err != nil {
					return nil, fmt.Errorf("aptitudegrid: decode state of %q: %w", entry.SubjectID, err)
				}
			}
			correct := 0
			for _, index := range st.Claims {
				if _, ok := env.Board.claimedCellBy(entry.SubjectID, index); ok {
					correct++
				}
			}
			units := modeutil.Units(correct * 100 / CellCount)
			explanation := modeutil.Marshal(struct {
				CorrectCells int     `json:"correctCells"`
				CellCount    int     `json:"cellCount"`
				TimedOut     bool    `json:"timedOut"`
				Variant      Variant `json:"variant"`
			}{CorrectCells: correct, CellCount: CellCount, TimedOut: st.TimedOut, Variant: VariantChallenge})
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
	default:
		return nil, fmt.Errorf("aptitudegrid: unknown variant %q", env.Round.Config.Variant)
	}
	return modes.RankScores(scores), nil
}
