// Package draft implements G03 公开卡牌编队夺标 .
//
// A round publishes one market of twelve cards and four tasks. Every team picks
// four distinct characters and assigns one to each task within a budget of 24.
// Each task grades three axes at A=3, B=2, C=1 (anything else 0), so the maximum
// total is 36; a valid final submission scores floor(100×total/36).
package draft

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
const ModeID = "draft"

// RulesVersion is the rule revision this implementation satisfies.
const RulesVersion = "2.0.0-draft.1"

// Documented round parameters .
const (
	// Budget is the total card price a team may spend.
	Budget = 24
	// MarketSize is how many cards a round publishes from the pack's card pool.
	MarketSize = 12
	// TaskCount is how many tasks a round publishes from the pack's task pool.
	//
	// The pack publishes the full task catalogue (one task per surface × distance
	// × style combination, 32 in the current build) and the round draws four of
	// them, which is what specifies as the published set: 统一公开 12 张卡、
	// 4 张任务、预算 24.
	TaskCount = 4
	// PicksPerTeam is how many characters a team selects.
	PicksPerTeam = TaskCount
	// PlanningSeconds is the planning window.
	PlanningSeconds = 240
)

// Grade points per axis .
const (
	GradeA = 3
	GradeB = 2
	GradeC = 1
)

// MaxTotalPoints is the maximum achievable total across four tasks with three
// axes each at grade A.
const MaxTotalPoints = TaskCount * 3 * GradeA

// Action kinds.
//
// The value is the contract discriminator (ModeAction → DraftLineup), not an
// internal name: the discriminator selects the payload schema .
const (
	// ActionDraft sets a team's complete final lineup. It is a full replacement,
	// which is what makes concurrent revisions resolve deterministically.
	ActionDraft = "draft.lineup"
)

// ContractKinds returns the ModeAction discriminators this mode accepts.
func (*Mode) ContractKinds() []string { return []string{ActionDraft} }

// Rejection codes.
const (
	CodeInvalidPayload     = "invalid_payload"
	CodeUnknownAction      = "unknown_action"
	CodeUnknownCharacter   = "unknown_character"
	CodeUnknownTask        = "unknown_task"
	CodeDuplicateCharacter = "duplicate_character"
	CodeOverBudget         = "over_budget"
	CodeIncomplete         = "incomplete_lineup"
	CodeLineupLocked       = "lineup_locked"
)

// Variant is the play form. Multi-team rounds use independent markets by
// default: teams do not compete for the same card .
type Variant string

const (
	// VariantSolo is a single-player planning round.
	VariantSolo Variant = "solo"
	// VariantTeams is a 2–8 team round with one independent market per team.
	VariantTeams Variant = "teams"
)

// Config is the round configuration.
type Config struct {
	Variant Variant `json:"variant"`
	// Budget overrides the default only when a rule version says so; the draft
	// baseline is 24 and the value is frozen into the pack.
	Budget int `json:"budget"`
	// PlanningSeconds is the planning window.
	PlanningSeconds int `json:"planningSeconds"`
}

// decodeConfig normalises the configuration.
func decodeConfig(raw json.RawMessage) (Config, error) {
	cfg := Config{Variant: VariantSolo, Budget: Budget, PlanningSeconds: PlanningSeconds}
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &cfg); err != nil {
			return Config{}, fmt.Errorf("draft: invalid config: %w", err)
		}
	}
	if cfg.Variant == "" {
		cfg.Variant = VariantSolo
	}
	switch cfg.Variant {
	case VariantSolo, VariantTeams:
	default:
		return Config{}, fmt.Errorf("draft: unknown variant %q", cfg.Variant)
	}
	if cfg.Budget == 0 {
		cfg.Budget = Budget
	}
	if cfg.Budget < 1 {
		return Config{}, fmt.Errorf("draft: budget %d must be positive", cfg.Budget)
	}
	if cfg.PlanningSeconds == 0 {
		cfg.PlanningSeconds = PlanningSeconds
	}
	if cfg.PlanningSeconds < 0 {
		return Config{}, fmt.Errorf("draft: negative planningSeconds %d", cfg.PlanningSeconds)
	}
	return cfg, nil
}

// Aptitudes is one card's graded aptitudes.
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

// points converts a grade letter to its point value. An unknown grade is 0 and
// never counts as a partial match ( draft row: 未知适性 must not be treated
// as a satisfied axis).
func points(grade string) int {
	switch grade {
	case "A":
		return GradeA
	case "B":
		return GradeB
	case "C":
		return GradeC
	default:
		return 0
	}
}

// countA returns how many of the given axis values are graded A, which is what
// the documented price formula counts.
func (a Aptitudes) countA(axis string, values []string) int {
	n := 0
	for _, v := range values {
		if a.gradeOn(axis, v) == "A" {
			n++
		}
	}
	return n
}

// Card is one market card.
type Card struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	ImagePath string `json:"imagePath"`
	// Price is the published price frozen into the pack.
	Price     int       `json:"price"`
	Aptitudes Aptitudes `json:"aptitudes"`
}

// Task is one task with its three required conditions.
type Task struct {
	TaskID   string `json:"taskId"`
	Surface  string `json:"surface"`
	Distance string `json:"distance"`
	Style    string `json:"style"`
}

// TaskScore is one task's grading for one assigned character.
type TaskScore struct {
	TaskID string `json:"taskId"`
	CardID string `json:"cardId"`
	// SurfacePoints, DistancePoints and StylePoints are the three axis grades.
	SurfacePoints  int `json:"surfacePoints"`
	DistancePoints int `json:"distancePoints"`
	StylePoints    int `json:"stylePoints"`
	// Total is the task's summed points, at most 9.
	Total int `json:"total"`
}

// scoreTask grades one character against one task.
func scoreTask(task Task, card Card) TaskScore {
	ts := TaskScore{
		TaskID:         task.TaskID,
		CardID:         card.ID,
		SurfacePoints:  points(card.Aptitudes.gradeOn("surface", task.Surface)),
		DistancePoints: points(card.Aptitudes.gradeOn("distance", task.Distance)),
		StylePoints:    points(card.Aptitudes.gradeOn("style", task.Style)),
	}
	ts.Total = ts.SurfacePoints + ts.DistancePoints + ts.StylePoints
	return ts
}

// PublishedPrice recomputes the documented trial price :
//
//	3 + ceil(距离A数量/2) + ceil(跑法A数量/2) + (泥地A ? 1 : 0)
//
// It is exported so the content pipeline can cross-check a pack's published
// price against the formula instead of trusting it blindly.
func PublishedPrice(a Aptitudes) int {
	price := 3
	price += modeutil.CeilDiv(a.countA("distance", []string{"短", "英", "中", "长"}), 2)
	price += modeutil.CeilDiv(a.countA("style", []string{"逃", "先", "差", "追"}), 2)
	if a.gradeOn("surface", "泥地") == "A" {
		price++
	}
	return price
}

// parseAptitudes reads the three aptitude tables.
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

// market is the decoded pack section.
type market struct {
	Cards []Card `json:"cards"`
	Tasks []Task `json:"tasks"`
}

// loadMarket reads the draft section.
func loadMarket(src modes.ContentSource) (market, error) {
	raw, err := modeutil.Section(src, "draft.market", "draft", "draft.cards")
	if err != nil {
		return market{}, err
	}
	fm, err := modeutil.DecodeFieldMap(raw)
	if err != nil {
		return market{}, fmt.Errorf("draft: decode section: %w", err)
	}

	var out market
	cardRecords, ok := fm.List("cards")
	if !ok {
		return market{}, fmt.Errorf("draft: section has no cards array")
	}
	for _, rec := range cardRecords {
		id := rec.Str("characterId", "id", "character_id")
		if id == "" {
			continue
		}
		card := Card{
			ID:        id,
			Name:      rec.Str("name", "nameZh", "name_zh"),
			ImagePath: rec.Str("imagePath", "image_local_path"),
		}
		if apt, ok := rec.Object("aptitudes", "aptitude"); ok {
			card.Aptitudes = parseAptitudes(apt)
		}
		if price, ok := rec.Int("price"); ok {
			card.Price = price
		} else {
			// A pack that omits the price still needs one; the documented
			// formula is the only defensible default.
			card.Price = PublishedPrice(card.Aptitudes)
		}
		out.Cards = append(out.Cards, card)
	}
	taskRecords, ok := fm.List("tasks")
	if !ok {
		return market{}, fmt.Errorf("draft: section has no tasks array")
	}
	for _, rec := range taskRecords {
		id := rec.Str("taskId", "id", "task_id")
		if id == "" {
			continue
		}
		out.Tasks = append(out.Tasks, Task{
			TaskID:   id,
			Surface:  rec.Str("surface"),
			Distance: rec.Str("distance"),
			Style:    rec.Str("style"),
		})
	}
	sort.Slice(out.Cards, func(i, j int) bool { return out.Cards[i].ID < out.Cards[j].ID })
	sort.Slice(out.Tasks, func(i, j int) bool { return out.Tasks[i].TaskID < out.Tasks[j].TaskID })
	return out, nil
}

// publicPrepared is the non-secret prepared state. The market is public: every
// team sees all twelve cards and four tasks .
type publicPrepared struct {
	ModeID       string        `json:"modeId"`
	RulesVersion string        `json:"rulesVersion"`
	Config       Config        `json:"config"`
	Cards        []Card        `json:"cards"`
	Tasks        []Task        `json:"tasks"`
	Subjects     []rosterEntry `json:"subjects"`
	// FeasibleLineups is how many distinct complete lineups satisfy the budget.
	// A market needs at least two, otherwise it is not admitted .
	FeasibleLineups int `json:"feasibleLineups"`
}

// rosterEntry is one participant.
type rosterEntry struct {
	SubjectID   string            `json:"subjectId"`
	SubjectKind modes.SubjectKind `json:"subjectKind"`
}

// privatePrepared carries the generated reference solutions, kept server-side so
// the round can verify the market without recomputing it in a transaction.
type privatePrepared struct {
	// ReferenceLineups lists distinct feasible lineups as card-id sets. It is
	// server-only because publishing it would hand teams free answers.
	ReferenceLineups [][]string `json:"referenceLineups"`
}

// state is one team's participant state.
type state struct {
	// Assignments maps taskId → characterId for the current draft.
	Assignments map[string]string `json:"assignments"`
	// Revisions counts how many accepted drafts the team submitted.
	Revisions int `json:"revisions"`
	// Locked reports a final submission was accepted.
	Locked bool `json:"locked"`
	// TotalPoints is the graded total of the locked lineup.
	TotalPoints int `json:"totalPoints"`
	// Spent is the price total of the locked lineup.
	Spent int `json:"spent"`
	// TaskScores is the per-task breakdown of the locked lineup.
	TaskScores []TaskScore `json:"taskScores,omitempty"`
}

// Mode implements modes.Mode for G03.
type Mode struct{}

// New returns the draft mode.
func New() *Mode { return &Mode{} }

// ID implements modes.Mode.
func (*Mode) ID() string { return ModeID }

// RulesVersion implements modes.Mode.
func (*Mode) RulesVersion() string { return RulesVersion }

// Title implements modes.Mode.
func (*Mode) Title() string { return "公开卡牌编队夺标" }

// Summary implements modes.Mode.
func (*Mode) Summary() string {
	return "在预算内从公开牌池为四个任务各选一名不同角色，按三维适配度计分。"
}

// InformationStyles reports which information forms this mode implements.
func (*Mode) InformationStyles() []string {
	return []string{"closed"}
}

// RoundWindow implements modes.Mode.
//
// 一次规划窗口。返回 0 表示本模式没有自己的窗口意见，交给房间层默认值。
func (*Mode) RoundWindow(raw json.RawMessage) time.Duration {
	cfg, err := decodeConfig(raw)
	if err != nil {
		return 0
	}
	return time.Duration(cfg.PlanningSeconds) * time.Second
}

// TurnWindow implements modes.Mode. This mode is not turn-based: it runs on the
// single RoundWindow deadline, so there is no per-turn clock to report.
func (*Mode) TurnWindow(json.RawMessage) time.Duration { return 0 }

// MinSubjects implements modes.Mode.
func (*Mode) MinSubjects() int { return 1 }

// MaxSubjects implements modes.Mode.
func (*Mode) MaxSubjects() int { return 8 }

// Requirement implements modes.Mode .
func (*Mode) Requirement() modes.ContentRequirement {
	return modes.ContentRequirement{
		Fields:      []string{"aptitudes", "price", "tasks"},
		MinEntities: MarketSize,
		NeedsAssets: false,
	}
}

// ValidateConfig implements modes.Mode.
func (*Mode) ValidateConfig(cfg json.RawMessage) error {
	_, err := decodeConfig(cfg)
	return err
}

// Prepare implements modes.Mode.
//
// It rejects a market that does not publish the documented shape or that has
// fewer than two distinct feasible lineups (: 每个市场需至少两套不同完整
// 可行阵容).
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
	mkt, err := loadMarket(in.Content)
	if err != nil {
		return modes.Prepared{}, err
	}
	if len(mkt.Cards) < MarketSize {
		return modes.Prepared{}, fmt.Errorf(
			"draft: pack has %d cards, need at least %d", len(mkt.Cards), MarketSize,
		)
	}
	if len(mkt.Tasks) < TaskCount {
		return modes.Prepared{}, fmt.Errorf(
			"draft: pack has %d tasks, need at least %d", len(mkt.Tasks), TaskCount,
		)
	}

	roster := make([]rosterEntry, 0, len(in.Subject))
	seen := make(map[string]struct{}, len(in.Subject))
	for _, subj := range in.Subject {
		if err := subj.Validate(); err != nil {
			return modes.Prepared{}, err
		}
		if _, dup := seen[subj.ID]; dup {
			return modes.Prepared{}, fmt.Errorf("draft: duplicate subject %q", subj.ID)
		}
		seen[subj.ID] = struct{}{}
		roster = append(roster, rosterEntry{SubjectID: subj.ID, SubjectKind: subj.Kind})
	}
	if len(roster) < 1 || len(roster) > 8 {
		return modes.Prepared{}, fmt.Errorf("draft: %d subjects outside [1,8]", len(roster))
	}

	// Draw the round's published set from the pack pools. Cards are already
	// sorted by id and tasks by task id, so the draw is deterministic for a seed.
	cards := drawCards(mkt.Cards, MarketSize, in.Scope.Seed)
	tasks := drawTasks(mkt.Tasks, TaskCount, in.Scope.Seed)

	// A market needs at least two distinct complete feasible lineups (:
	// 每个市场需至少两套不同完整可行阵容). If the first draw cannot satisfy that,
	// the pack's card prices cannot support this budget and the round must not
	// open; retrying draws would hide a real content problem.
	lineups := feasibleLineups(cards, cfg.Budget)
	if len(lineups) < 2 {
		return modes.Prepared{}, fmt.Errorf(
			"draft: the drawn market has %d distinct feasible lineups under budget %d, need at least 2",
			len(lineups), cfg.Budget,
		)
	}

	pub := publicPrepared{
		ModeID:          ModeID,
		RulesVersion:    RulesVersion,
		Config:          cfg,
		Cards:           cards,
		Tasks:           tasks,
		Subjects:        roster,
		FeasibleLineups: len(lineups),
	}
	// Keep a bounded reference set so the private blob cannot grow without
	// limit on a large market.
	refLimit := 8
	if len(lineups) < refLimit {
		refLimit = len(lineups)
	}
	priv := privatePrepared{ReferenceLineups: lineups[:refLimit]}
	return modes.Prepared{
		Public:        modeutil.Marshal(pub),
		Private:       modeutil.Marshal(priv),
		QuestionCount: TaskCount,
	}, nil
}

// drawCards selects count cards from the pool deterministically.
func drawCards(pool []Card, count int, seed string) []Card {
	indices := make([]int, len(pool))
	for i := range indices {
		indices[i] = i
	}
	stream := modeutil.NewStream(seed + "\x00draft.cards")
	stream.Shuffle(len(indices), func(i, j int) { indices[i], indices[j] = indices[j], indices[i] })

	out := make([]Card, 0, count)
	for i := 0; i < count && i < len(indices); i++ {
		out = append(out, pool[indices[i]])
	}
	// Emit in id order so the published set is stable regardless of the draw.
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// drawTasks selects count tasks from the pool deterministically.
func drawTasks(pool []Task, count int, seed string) []Task {
	indices := make([]int, len(pool))
	for i := range indices {
		indices[i] = i
	}
	stream := modeutil.NewStream(seed + "\x00draft.tasks")
	stream.Shuffle(len(indices), func(i, j int) { indices[i], indices[j] = indices[j], indices[i] })

	out := make([]Task, 0, count)
	for i := 0; i < count && i < len(indices); i++ {
		out = append(out, pool[indices[i]])
	}
	sort.Slice(out, func(i, j int) bool { return out[i].TaskID < out[j].TaskID })
	return out
}

// feasibleLineups enumerates distinct four-card lineups whose summed price fits
// the budget. Cards are sorted by id, so the enumeration order is fixed and the
// result is deterministic.
func feasibleLineups(cards []Card, budget int) [][]string {
	out := make([][]string, 0, 16)
	n := len(cards)
	for a := 0; a < n; a++ {
		for b := a + 1; b < n; b++ {
			for c := b + 1; c < n; c++ {
				for d := c + 1; d < n; d++ {
					spent := cards[a].Price + cards[b].Price + cards[c].Price + cards[d].Price
					if spent > budget {
						continue
					}
					out = append(out, []string{
						cards[a].ID, cards[b].ID, cards[c].ID, cards[d].ID,
					})
				}
			}
		}
	}
	return out
}

// draftAssignment is one slot → character pairing, matching the contract's
// Assignment schema.
type draftAssignment struct {
	SlotID      string `json:"slotId"`
	CharacterID string `json:"characterId"`
}

// draftPayload is the body of ActionDraft, matching the contract's DraftLineup
// schema: {kind, assignments[]} with exactly four assignments.
type draftPayload struct {
	Kind        string            `json:"kind"`
	Assignments []draftAssignment `json:"assignments"`
}

// Apply implements modes.Mode.
func (*Mode) Apply(in modes.ApplyInput) (modes.Decision, error) {
	if in.Action.Kind != ActionDraft {
		return modes.Rejected(CodeUnknownAction,
			fmt.Sprintf("draft: unsupported action %q", in.Action.Kind)), nil
	}
	if err := in.Action.Validate(); err != nil {
		return modes.Rejected(CodeInvalidPayload, err.Error()), nil
	}
	var payload draftPayload
	if err := json.Unmarshal(in.Action.Payload, &payload); err != nil {
		return modes.Rejected(CodeInvalidPayload,
			"draft: draft payload must carry an assignments array"), nil
	}
	if payload.Kind != "" && payload.Kind != ActionDraft {
		return modes.Rejected(CodeInvalidPayload, fmt.Sprintf(
			"draft: payload kind %q does not match action %q", payload.Kind, ActionDraft,
		)), nil
	}

	var pub publicPrepared
	if len(in.Public) > 0 {
		if err := json.Unmarshal(in.Public, &pub); err != nil {
			return modes.Decision{}, fmt.Errorf("draft: decode public state: %w", err)
		}
	}
	var st state
	if len(in.State) > 0 {
		if err := json.Unmarshal(in.State, &st); err != nil {
			return modes.Decision{}, fmt.Errorf("draft: decode subject state: %w", err)
		}
	}
	// Client drafts may change before the one explicit final submission. Once
	// accepted, a different request cannot replace that final lineup. Replaying
	// the original request is handled by the receipt layer before this method.
	if st.Locked {
		return modes.Rejected(CodeLineupLocked, "draft: final lineup is already locked"), nil
	}

	// A complete draft assigns every published task exactly once. The contract
	// requires exactly four assignments (DraftLineup minItems=maxItems=4).
	if len(payload.Assignments) != len(pub.Tasks) {
		return modes.Rejected(CodeIncomplete, fmt.Sprintf(
			"draft: lineup carries %d assignments, need %d", len(payload.Assignments), len(pub.Tasks),
		)), nil
	}

	cardsByID := make(map[string]Card, len(pub.Cards))
	for _, c := range pub.Cards {
		cardsByID[c.ID] = c
	}
	tasksByID := make(map[string]Task, len(pub.Tasks))
	for _, task := range pub.Tasks {
		tasksByID[task.TaskID] = task
	}

	// Fold the array into a slot → character map, rejecting a duplicate slot
	// instead of letting a later entry silently win.
	byTask := make(map[string]string, len(payload.Assignments))
	for _, assignment := range payload.Assignments {
		if assignment.SlotID == "" {
			return modes.Rejected(CodeInvalidPayload, "draft: empty slotId"), nil
		}
		if _, dup := byTask[assignment.SlotID]; dup {
			return modes.Rejected(CodeUnknownTask, fmt.Sprintf(
				"draft: slot %q is assigned twice", assignment.SlotID,
			)), nil
		}
		byTask[assignment.SlotID] = assignment.CharacterID
	}
	// Reject an assignment naming a task that is not published.
	for slotID := range byTask {
		if _, ok := tasksByID[slotID]; !ok {
			return modes.Rejected(CodeUnknownTask,
				fmt.Sprintf("draft: task %q is not published in this market", slotID)), nil
		}
	}

	// Validate the lineup deterministically: iterate the task order, not the map.
	usedCards := make(map[string]string, len(byTask))
	spent := 0
	for _, task := range pub.Tasks {
		cardID := byTask[task.TaskID]
		if cardID == "" {
			return modes.Rejected(CodeIncomplete,
				fmt.Sprintf("draft: task %q has no assigned character", task.TaskID)), nil
		}
		card, exists := cardsByID[cardID]
		if !exists {
			return modes.Rejected(CodeUnknownCharacter,
				fmt.Sprintf("draft: character %q is not in this market", cardID)), nil
		}
		if prevTask, dup := usedCards[cardID]; dup {
			// 同一角色本分项最多出场一次.
			return modes.Rejected(CodeDuplicateCharacter, fmt.Sprintf(
				"draft: character %q is assigned to both %q and %q", cardID, prevTask, task.TaskID,
			)), nil
		}
		usedCards[cardID] = task.TaskID
		spent += card.Price
	}
	if spent > pub.Config.Budget {
		// 草稿超预算可提示但不能最终锁定.
		return modes.Rejected(CodeOverBudget, fmt.Sprintf(
			"draft: lineup costs %d, over the budget of %d", spent, pub.Config.Budget,
		)), nil
	}

	// Grade the lineup in task order.
	taskScores := make([]TaskScore, 0, len(pub.Tasks))
	total := 0
	for _, task := range pub.Tasks {
		ts := scoreTask(task, cardsByID[byTask[task.TaskID]])
		taskScores = append(taskScores, ts)
		total += ts.Total
	}

	st.Assignments = byTask
	st.Revisions++
	st.Locked = true
	st.TotalPoints = total
	st.Spent = spent
	st.TaskScores = taskScores

	units := modeutil.Units(total * 100 / MaxTotalPoints)
	feedback := modeutil.Marshal(struct {
		Spent       int         `json:"spent"`
		Budget      int         `json:"budget"`
		TotalPoints int         `json:"totalPoints"`
		MaxPoints   int         `json:"maxPoints"`
		ScoreUnits  int         `json:"scoreUnits"`
		TaskScores  []TaskScore `json:"taskScores"`
		Revisions   int         `json:"revisions"`
	}{
		Spent:       spent,
		Budget:      pub.Config.Budget,
		TotalPoints: total,
		MaxPoints:   MaxTotalPoints,
		ScoreUnits:  units,
		TaskScores:  taskScores,
		Revisions:   st.Revisions,
	})

	dec := modes.Accepted(feedback, modeutil.Marshal(st))
	// A locked lineup is final for this subject. Unsubmitted client drafts are
	// not scored, and a later request cannot overwrite this final confirmation.
	dec.Finished = true
	return dec, nil
}

// projection is the viewer-specific round state.
type projection struct {
	ModeID       string      `json:"modeId"`
	RulesVersion string      `json:"rulesVersion"`
	Phase        modes.Phase `json:"phase"`
	Config       Config      `json:"config"`
	Cards        []Card      `json:"cards"`
	Tasks        []Task      `json:"tasks"`
	Subjects     []progress  `json:"subjects"`
	Self         *selfDraft  `json:"self,omitempty"`
}

// progress is the coarse per-team progress everyone may see.
type progress struct {
	SubjectID   string `json:"subjectId"`
	Revisions   int    `json:"revisions"`
	Locked      bool   `json:"locked"`
	TotalPoints int    `json:"totalPoints,omitempty"`
	Spent       int    `json:"spent,omitempty"`
}

// selfDraft is the caller's own draft state.
type selfDraft struct {
	SubjectID   string            `json:"subjectId"`
	Assignments map[string]string `json:"assignments"`
	Revisions   int               `json:"revisions"`
	Locked      bool              `json:"locked"`
	TotalPoints int               `json:"totalPoints"`
	Spent       int               `json:"spent"`
	TaskScores  []TaskScore       `json:"taskScores,omitempty"`
	// RemainingBudget is what the team may still spend.
	RemainingBudget int `json:"remainingBudget"`
}

// Project implements modes.Mode.
func (*Mode) Project(in modes.ProjectInput) (json.RawMessage, error) {
	var pub publicPrepared
	if len(in.Public) > 0 {
		if err := json.Unmarshal(in.Public, &pub); err != nil {
			return nil, fmt.Errorf("draft: decode public state: %w", err)
		}
	}
	out := projection{
		ModeID:       ModeID,
		RulesVersion: RulesVersion,
		Phase:        in.Phase,
		Config:       pub.Config,
		Cards:        pub.Cards,
		Tasks:        pub.Tasks,
		Subjects:     make([]progress, 0, len(pub.Subjects)),
	}
	for _, entry := range pub.Subjects {
		var st state
		if raw, ok := in.States[entry.SubjectID]; ok && len(raw) > 0 {
			if err := json.Unmarshal(raw, &st); err != nil {
				return nil, fmt.Errorf("draft: decode state of %q: %w", entry.SubjectID, err)
			}
		}
		p := progress{
			SubjectID: entry.SubjectID,
			Revisions: st.Revisions,
			Locked:    st.Locked,
		}
		// Another team's lineup stays hidden until the round is revealed: the
		// markets are independent but the scores are still round results.
		if st.Locked && (in.Viewer.SeesSecrets() || (in.RevealNow && in.Phase.Terminal())) {
			p.TotalPoints = st.TotalPoints
			p.Spent = st.Spent
		}
		out.Subjects = append(out.Subjects, p)

		if entry.SubjectID == in.Viewer.Subject.ID {
			out.Self = &selfDraft{
				SubjectID:       entry.SubjectID,
				Assignments:     st.Assignments,
				Revisions:       st.Revisions,
				Locked:          st.Locked,
				TotalPoints:     st.TotalPoints,
				Spent:           st.Spent,
				TaskScores:      st.TaskScores,
				RemainingBudget: pub.Config.Budget - st.Spent,
			}
		}
	}
	return modeutil.Marshal(out), nil
}

// Settle implements modes.Mode.
//
// A valid locked lineup scores floor(100×total/36); no valid final submission
// scores 0 .
func (*Mode) Settle(in modes.SettleInput) ([]modes.Score, error) {
	var pub publicPrepared
	if len(in.Public) > 0 {
		if err := json.Unmarshal(in.Public, &pub); err != nil {
			return nil, fmt.Errorf("draft: decode public state: %w", err)
		}
	}
	scores := make([]modes.Score, 0, len(pub.Subjects))
	for _, entry := range pub.Subjects {
		var st state
		if raw, ok := in.States[entry.SubjectID]; ok && len(raw) > 0 {
			if err := json.Unmarshal(raw, &st); err != nil {
				return nil, fmt.Errorf("draft: decode state of %q: %w", entry.SubjectID, err)
			}
		}
		units := 0
		if st.Locked {
			units = modeutil.Units(st.TotalPoints * 100 / MaxTotalPoints)
		}
		explanation := modeutil.Marshal(struct {
			Locked      bool        `json:"locked"`
			TotalPoints int         `json:"totalPoints"`
			MaxPoints   int         `json:"maxPoints"`
			Spent       int         `json:"spent"`
			Budget      int         `json:"budget"`
			Revisions   int         `json:"revisions"`
			TaskScores  []TaskScore `json:"taskScores,omitempty"`
		}{
			Locked:      st.Locked,
			TotalPoints: st.TotalPoints,
			MaxPoints:   MaxTotalPoints,
			Spent:       st.Spent,
			Budget:      pub.Config.Budget,
			Revisions:   st.Revisions,
			TaskScores:  st.TaskScores,
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
