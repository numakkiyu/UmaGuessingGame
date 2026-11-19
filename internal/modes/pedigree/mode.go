// Package pedigree implements G14 原型血统拼图 .
//
// One historical prototype's six-slot pedigree is shown as a puzzle: four to six
// named slots (父 / 母 / 父父 / 父母 / 母父 / 母母) must each be filled with the
// right ancestor from a published candidate list. Scoring is
//
//	floor(100 × correct slots / total slots)
//
// Three constraints shape the whole implementation:
//
// 1. 史实原型血统，不是角色家庭关系. The puzzle is about the real racehorse
// each character is based on. The public payload carries that statement
// verbatim and the UI must show it; a player who reads the board as "who is
// this character's father" would be answering a different, false question.
//
// 2. 只用已确认原型ID关系. A relation exists only when the compiled entry
// carries it in an explicit pedigree field. A shared surname or a matching
// name prefix is never treated as kinship: the ancestor identity is the
// published candidate, and a look-alike name is a separate candidate.
//
// 3. 未知、歧义名称不出题. Prepare refuses a puzzle whose slots cannot be
// answered unambiguously, rather than shipping a slot with two defensible
// answers.
package pedigree

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/bhcn/umaguessinggame/internal/modes"
	"github.com/bhcn/umaguessinggame/internal/modes/modeutil"
)

// ModeID is the stable mode identifier (contract ModeId enum).
const ModeID = "pedigree"

// RulesVersion is the rule revision this implementation satisfies.
const RulesVersion = "2.0.0-draft.1"

// Action kinds.
//
// The value is the contract discriminator (ModeAction → PedigreeAssignment), not
// an internal name .
const (
	// ActionKindAssignment submits one slot → candidate mapping.
	ActionKindAssignment = "pedigree.assignment"
)

// ContractKinds returns the ModeAction discriminators this mode accepts.
func (*Mode) ContractKinds() []string { return []string{ActionKindAssignment} }

// Rejection codes.
const (
	CodeInvalidPayload  = "invalid_payload"
	CodeUnknownAction   = "unknown_action"
	CodeUnknownSlot     = "unknown_slot"
	CodeUnknownHorse    = "unknown_horse"
	CodeDuplicateHorse  = "duplicate_horse"
	CodeDuplicateSlot   = "duplicate_slot"
	CodeRoundFinished   = "round_finished"
	CodeNotOnRoster     = "not_on_roster"
	CodeEmptyAssignment = "empty_assignment"
)

// Documented parameters .
const (
	// MinSlots and MaxSlots bound the number of pedigree slots in one puzzle.
	MinSlots = 4
	MaxSlots = 6
	// AnswerSeconds is the answering window.
	AnswerSeconds = 180
	// MinSubjects and MaxSubjects bound the participant count: single player or a
	// two-person cooperative pair .
	MinSubjects = 1
	MaxSubjects = 2
	// DefaultCandidates is how many ancestor candidates are published. It is the
	// slot count plus distractors, so a player cannot win by elimination alone
	// with fewer choices than slots.
	DefaultCandidates = 10
)

// Disclaimer is the statement the board must carry. It is a constant rather than
// a config value so no round can quietly drop it (: 题面一直标"史实原型血统"，
// 不能把它描述为角色家庭关系).
const Disclaimer = "史实原型血统：本题展示的是角色所依据的真实赛马的祖先关系，不是角色之间的家庭关系。"

// slotDef is one pedigree slot's direction.
type slotDef struct {
	// SlotID is the stable wire id.
	SlotID string `json:"slotId"`
	// Field is the compiled pedigree field the slot reads.
	Field string `json:"-"`
	// Label is the Chinese direction label the board shows.
	Label string `json:"label"`
	// Path is the direction in words, e.g. 父 → 父 for 父父.
	Path []string `json:"path"`
	// Generation is 1 for a parent, 2 for a grandparent.
	Generation int `json:"generation"`
}

// slotDefs is the fixed six-slot pedigree, in the documented order.
//
// The order and the labels are the direction rule: a player who swaps 父 and 母
// is wrong, and the scoring counts each slot separately .
var slotDefs = []slotDef{
	{SlotID: "sire", Field: "sire", Label: "父", Path: []string{"父"}, Generation: 1},
	{SlotID: "dam", Field: "dam", Label: "母", Path: []string{"母"}, Generation: 1},
	{SlotID: "sireSire", Field: "sireSire", Label: "父父", Path: []string{"父", "父"}, Generation: 2},
	{SlotID: "sireDam", Field: "sireDam", Label: "父母", Path: []string{"父", "母"}, Generation: 2},
	{SlotID: "damSire", Field: "damSire", Label: "母父", Path: []string{"母", "父"}, Generation: 2},
	{SlotID: "damDam", Field: "damDam", Label: "母母", Path: []string{"母", "母"}, Generation: 2},
}

// slotByID finds a slot definition.
func slotByID(slotID string) (slotDef, bool) {
	for _, def := range slotDefs {
		if def.SlotID == slotID {
			return def, true
		}
	}
	return slotDef{}, false
}

// Config is the round configuration.
type Config struct {
	// SlotCount is how many of the six slots the puzzle uses (4–6).
	SlotCount int `json:"slotCount"`
	// CandidateCount is how many ancestor candidates are published.
	CandidateCount int `json:"candidateCount"`
	// AnswerSeconds is the answering window.
	AnswerSeconds int `json:"answerSeconds"`
	// Cooperative marks the two-person form, which shares one score.
	Cooperative bool `json:"cooperative"`
}

// decodeConfig parses and normalises a configuration blob.
func decodeConfig(raw json.RawMessage) (Config, error) {
	cfg := Config{
		SlotCount:      MinSlots,
		CandidateCount: DefaultCandidates,
		AnswerSeconds:  AnswerSeconds,
	}
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &cfg); err != nil {
			return Config{}, fmt.Errorf("pedigree: invalid config: %w", err)
		}
	}
	if cfg.SlotCount == 0 {
		cfg.SlotCount = MinSlots
	}
	if cfg.CandidateCount == 0 {
		cfg.CandidateCount = DefaultCandidates
	}
	if cfg.AnswerSeconds == 0 {
		cfg.AnswerSeconds = AnswerSeconds
	}
	if cfg.SlotCount < MinSlots || cfg.SlotCount > MaxSlots {
		return Config{}, fmt.Errorf(
			"pedigree: slotCount %d outside [%d,%d]", cfg.SlotCount, MinSlots, MaxSlots)
	}
	// The candidate pool must be strictly larger than the slot count, otherwise a
	// player could fill the board by elimination without knowing any relation.
	if cfg.CandidateCount <= cfg.SlotCount {
		return Config{}, fmt.Errorf(
			"pedigree: candidateCount %d must exceed slotCount %d",
			cfg.CandidateCount, cfg.SlotCount)
	}
	if cfg.CandidateCount > 20 {
		return Config{}, fmt.Errorf("pedigree: candidateCount %d above 20", cfg.CandidateCount)
	}
	if cfg.AnswerSeconds < 10 || cfg.AnswerSeconds > 600 {
		return Config{}, fmt.Errorf(
			"pedigree: answerSeconds %d outside [10,600]", cfg.AnswerSeconds)
	}
	return cfg, nil
}

// countrySuffix strips a trailing stud-book country marker such as "(USA)".
//
// Two spellings that differ only by the marker are the same horse, so the marker
// is removed before any comparison. This is a normalisation for *comparison*, not
// a licence to treat two different names as one: the remaining string still has
// to match exactly .
var countrySuffix = regexp.MustCompile(`\s*\((?:USA|IRE|CAN|GB|FR|JPN|AUS|NZ|GER|BRA|BRZ|ARG|CHI|SAU|UAE|KOR|IND|PER|URU|SWE|DEN|ITY|SPA|POL|TUR|CZE|HUN|NOR|FIN)\)\s*$`)

// normalizeName is the comparison form of an ancestor name.
func normalizeName(name string) string {
	s := strings.TrimSpace(name)
	s = countrySuffix.ReplaceAllString(s, "")
	// Collapse internal whitespace so "Wishing Well" and "Wishing Well" agree.
	s = strings.Join(strings.Fields(s), " ")
	return s
}

// entry is one compiled pedigree record .
type entry struct {
	CharacterID    string            `json:"characterId"`
	Name           string            `json:"name"`
	NKBID          string            `json:"nkbId"`
	Pedigree       map[string]string `json:"pedigree"`
	PrototypeNames struct {
		Ja           string `json:"ja"`
		ZhHans       string `json:"zhHans"`
		En           string `json:"en"`
		JaConfidence string `json:"jaConfidence"`
	} `json:"prototypeNames"`
}

// complete reports whether the record carries every documented slot non-empty.
func (e entry) complete() bool {
	for _, def := range slotDefs {
		if normalizeName(e.Pedigree[def.Field]) == "" {
			return false
		}
	}
	return true
}

// loadEntries reads the compiled pedigree section.
func loadEntries(src modes.ContentSource) ([]entry, error) {
	if src == nil {
		return nil, fmt.Errorf("pedigree: no content source")
	}
	raw, err := modeutil.Section(src, "pedigree.entries", "pedigree")
	if err != nil {
		return nil, err
	}
	records, err := modeutil.Records(raw, "entries", "items")
	if err != nil {
		return nil, err
	}
	out := make([]entry, 0, len(records))
	for _, rec := range records {
		e := entry{
			CharacterID: rec.Str("characterId", "character_id", "id"),
			Name:        rec.Str("name", "nameZh"),
			NKBID:       rec.Str("nkbId", "nkb_id", "nkbID"),
		}
		ped, ok := rec.Object("pedigree")
		if !ok {
			continue
		}
		e.Pedigree = make(map[string]string, len(slotDefs))
		for _, def := range slotDefs {
			e.Pedigree[def.Field] = ped.Str(def.Field, strings.ToLower(def.Field))
		}
		if names, okNames := rec.Object("prototypeNames"); okNames {
			e.PrototypeNames.Ja = names.Str("ja")
			e.PrototypeNames.ZhHans = names.Str("zhHans")
			e.PrototypeNames.En = names.Str("en")
			e.PrototypeNames.JaConfidence = names.Str("jaConfidence")
		}
		out = append(out, e)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("pedigree: section %q carries no usable entry", "pedigree.entries")
	}
	// Deterministic order: the compiled section is already sorted, but sorting
	// again makes the draw independent of file order .
	sort.Slice(out, func(i, j int) bool { return out[i].CharacterID < out[j].CharacterID })
	return out, nil
}

// candidate is one published ancestor choice.
//
// HorseID is opaque: it carries no direction, no rank and no hint about which
// slot the ancestor belongs in. A readable id such as "sire-1" would hand the
// answer over without any knowledge, so it is a seeded digest instead.
type candidate struct {
	HorseID string `json:"horseId"`
	Name    string `json:"name"`
	// NKBID is present only when the ancestor is itself a compiled prototype with
	// an independent id, which is the only case where the identity is confirmed
	// independently of the name .
	NKBID string `json:"nkbId,omitempty"`
}

// puzzleSlot is one slot as the board publishes it.
type puzzleSlot struct {
	SlotID string   `json:"slotId"`
	Label  string   `json:"label"`
	Path   []string `json:"path"`
	// Generation is 1 for a parent, 2 for a grandparent.
	Generation int `json:"generation"`
}

// rosterEntry is one participant recorded in the public prepared state.
type rosterEntry struct {
	SubjectID   string            `json:"subjectId"`
	SubjectKind modes.SubjectKind `json:"subjectKind"`
}

// publicState is the non-secret round state. It carries the board and the
// candidates but never the solution.
type publicState struct {
	ModeID       string `json:"modeId"`
	RulesVersion string `json:"rulesVersion"`
	Config       Config `json:"config"`
	// Disclaimer must be rendered with the board .
	Disclaimer string `json:"disclaimer"`
	// Subject describes the prototype being reconstructed. It names the horse,
	// not the character: the puzzle is about the historical animal.
	Subject prototypeSubject `json:"subject"`
	Slots   []puzzleSlot     `json:"slots"`
	// Candidates is the published ancestor list, in a stable seeded order.
	Candidates []candidate   `json:"candidates"`
	Subjects   []rosterEntry `json:"subjects"`
	Scoring    string        `json:"scoring"`
	// Rules carries the direction legend so the board never has to invent one.
	Rules []slotRule `json:"rules"`
}

// prototypeSubject is the horse whose pedigree is being reconstructed.
type prototypeSubject struct {
	// CharacterID is the game character the prototype belongs to. It is shown so
	// the player knows which record they are reading, and it is never the answer.
	CharacterID string `json:"characterId"`
	// CharacterName is the character's display name.
	CharacterName string `json:"characterName"`
	// PrototypeName is the historical horse's name.
	PrototypeName string `json:"prototypeName"`
	// NKBID is the independent prototype identity .
	NKBID string `json:"nkbId"`
}

// slotRule is one row of the published direction legend.
type slotRule struct {
	SlotID string `json:"slotId"`
	Label  string `json:"label"`
	Means  string `json:"means"`
}

// privateState is the server-only solution.
type privateState struct {
	ModeID string `json:"modeId"`
	// Solution maps slot id → the correct candidate's horse id.
	Solution map[string]string `json:"solution"`
	// SlotNames maps slot id → the ancestor's readable name, for the reveal.
	SlotNames map[string]string `json:"slotNames"`
}

// subjectState is one participant's own persisted state.
type subjectState struct {
	// Assignments is the caller's current slot → horse mapping. A later
	// submission replaces it, which is the documented "截止前可修订" rule.
	Assignments []assignmentRecord `json:"assignments"`
	// Submissions counts accepted revisions.
	Submissions int `json:"submissions"`
}

// assignmentRecord is one slot → horse pair as stored.
type assignmentRecord struct {
	SlotID  string `json:"slotId"`
	HorseID string `json:"horseId"`
}

// Mode implements modes.Mode for G14.
type Mode struct{}

// New returns the pedigree mode.
func New() *Mode { return &Mode{} }

// ID implements modes.Mode.
func (*Mode) ID() string { return ModeID }

// RulesVersion implements modes.Mode.
func (*Mode) RulesVersion() string { return RulesVersion }

// Title implements modes.Mode.
func (*Mode) Title() string { return "原型血统拼图" }

// Summary implements modes.Mode.
func (*Mode) Summary() string {
	return "把史实原型赛马的祖先填回正确的血统槽位：只看已确认的原型资料，按槽位计分。"
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
func (*Mode) MinSubjects() int { return MinSubjects }

// MaxSubjects implements modes.Mode.
func (*Mode) MaxSubjects() int { return MaxSubjects }

// Requirement implements modes.Mode .
func (*Mode) Requirement() modes.ContentRequirement {
	return modes.ContentRequirement{
		Fields:      []string{"pedigree.sire", "pedigree.dam", "prototype.nkbId"},
		MinEntities: MinSlots,
		NeedsAssets: false,
	}
}

// ValidateConfig implements modes.Mode.
func (*Mode) ValidateConfig(cfg json.RawMessage) error {
	_, err := decodeConfig(cfg)
	return err
}

// candidateID builds an opaque, stable candidate id.
//
// It is a digest of the round seed and the normalised name, so it is stable for a
// round, distinct for distinct ancestors, and carries no information about the
// direction the ancestor belongs to.
func candidateID(seed, normalizedName string) string {
	sum := sha256.Sum256([]byte(seed + "\x00pedigree.candidate\x00" + normalizedName))
	return "ph-" + hex.EncodeToString(sum[:8])
}

// Prepare implements modes.Mode.
//
// The puzzle is drawn from the seed: one complete entry, then a slot subset, then
// a candidate pool that mixes the answers with distractors drawn from other
// entries. Every step is validated so an ambiguous puzzle is refused instead of
// shipped .
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

	entries, err := loadEntries(in.Content)
	if err != nil {
		return modes.Prepared{}, err
	}

	roster := make([]rosterEntry, 0, len(in.Subject))
	seen := make(map[string]struct{}, len(in.Subject))
	for _, subj := range in.Subject {
		if err := subj.Validate(); err != nil {
			return modes.Prepared{}, err
		}
		if _, dup := seen[subj.ID]; dup {
			return modes.Prepared{}, fmt.Errorf("pedigree: duplicate subject %q", subj.ID)
		}
		seen[subj.ID] = struct{}{}
		roster = append(roster, rosterEntry{SubjectID: subj.ID, SubjectKind: subj.Kind})
	}
	if len(roster) < MinSubjects || len(roster) > MaxSubjects {
		return modes.Prepared{}, fmt.Errorf(
			"pedigree: %d participants outside [%d,%d]",
			len(roster), MinSubjects, MaxSubjects)
	}
	cooperative := len(roster) == MaxSubjects
	if cfg.Cooperative != cooperative {
		return modes.Prepared{}, fmt.Errorf(
			"pedigree: cooperative=%v does not match %d participants",
			cfg.Cooperative, len(roster))
	}

	// Only complete records with an independent prototype id may be a subject:
	// an incomplete pedigree would publish a slot with no confirmed answer.
	ids, prototypes, names := identityCounts(entries)
	usable := make([]entry, 0, len(entries))
	for _, e := range entries {
		if !confirmedIdentity(e, ids, prototypes, names) || !e.complete() {
			continue
		}
		selfAncestor := false
		for _, slot := range slotDefs {
			if normalizeName(e.Pedigree[slot.Field]) == normalizeName(e.PrototypeNames.Ja) {
				selfAncestor = true
				break
			}
		}
		if selfAncestor {
			continue
		}
		usable = append(usable, e)
	}
	if len(usable) == 0 {
		return modes.Prepared{}, fmt.Errorf(
			"pedigree: none of the %d compiled entries carries a complete pedigree "+
				"with an independent prototype id", len(entries))
	}

	subjectIdx := modeutil.PickIndex(in.Scope.Seed, "pedigree.subject", len(usable))
	subject := usable[subjectIdx]

	// Choose which slots the puzzle uses. The parent slots are always included
	// when fewer than six are requested, because a puzzle without parents reads
	// as a different question; the remainder is drawn from the grandparents.
	slots, err := pickSlots(in.Scope.Seed, cfg.SlotCount)
	if err != nil {
		return modes.Prepared{}, err
	}

	// Build the answer set and reject an ambiguous one.
	answers := make(map[string]string, len(slots))      // slotId → normalized name
	answerName := make(map[string]string, len(slots))   // slotId → display name
	byNormalized := make(map[string]string, len(slots)) // normalized → slotId
	for _, slot := range slots {
		raw := subject.Pedigree[slot.Field]
		norm := normalizeName(raw)
		if norm == "" {
			return modes.Prepared{}, fmt.Errorf(
				"pedigree: subject %q has no confirmed ancestor for slot %q",
				subject.CharacterID, slot.SlotID)
		}
		// 重复原型: one ancestor cannot occupy two slots. If the data ever put the
		// same horse in two slots, the puzzle would have no single correct answer.
		if other, dup := byNormalized[norm]; dup {
			return modes.Prepared{}, fmt.Errorf(
				"pedigree: subject %q names the same ancestor %q in slots %q and %q",
				subject.CharacterID, norm, other, slot.SlotID)
		}
		byNormalized[norm] = slot.SlotID
		answers[slot.SlotID] = norm
		answerName[slot.SlotID] = strings.TrimSpace(raw)
	}

	// Build the candidate pool: the answers first, then distractors drawn from
	// other entries' ancestors.
	pool := make([]candidate, 0, cfg.CandidateCount)
	used := make(map[string]struct{}, cfg.CandidateCount)
	for _, slot := range slots {
		norm := answers[slot.SlotID]
		used[norm] = struct{}{}
		pool = append(pool, candidate{
			HorseID: candidateID(in.Scope.Seed, norm),
			Name:    answerName[slot.SlotID],
			NKBID:   ancestorNKBID(entries, norm),
		})
	}

	// Distractors come from a deterministic, seeded sweep of every other entry's
	// ancestors, so the pool is reproducible and not biased by file order.
	distractors := collectDistractors(entries, used)
	stream := modeutil.NewStream(in.Scope.Seed + "\x00pedigree.distractors")
	order := make([]int, len(distractors))
	for i := range order {
		order[i] = i
	}
	stream.Shuffle(len(order), func(i, j int) { order[i], order[j] = order[j], order[i] })
	for _, idx := range order {
		if len(pool) >= cfg.CandidateCount {
			break
		}
		pool = append(pool, candidate{
			HorseID: candidateID(in.Scope.Seed, distractors[idx].normalized),
			Name:    distractors[idx].display,
			NKBID:   ancestorNKBID(entries, distractors[idx].normalized),
		})
	}
	if len(pool) < cfg.CandidateCount {
		return modes.Prepared{}, fmt.Errorf(
			"pedigree: only %d distinct ancestor candidates exist, need %d",
			len(pool), cfg.CandidateCount)
	}

	// The published order must not group the answers together or place them in
	// slot order: either would leak the solution to a player who reads the list.
	shuffleStream := modeutil.NewStream(in.Scope.Seed + "\x00pedigree.pool")
	shuffleStream.Shuffle(len(pool), func(i, j int) { pool[i], pool[j] = pool[j], pool[i] })

	// Verify the published pool is unambiguous: exactly one candidate per answer,
	// and no distractor whose normalised name equals an answer's.
	checkIDs := make(map[string]struct{}, len(pool))
	checkNames := make(map[string]struct{}, len(pool))
	for _, cand := range pool {
		if _, dup := checkIDs[cand.HorseID]; dup {
			return modes.Prepared{}, fmt.Errorf(
				"pedigree: candidate id collision on %q", cand.Name)
		}
		checkIDs[cand.HorseID] = struct{}{}
		norm := normalizeName(cand.Name)
		if _, dup := checkNames[norm]; dup {
			// Two published candidates with the same normalised name would let a
			// player pick either and be told one is wrong — the "同冠名误连"
			// failure the plan requires us to avoid .
			return modes.Prepared{}, fmt.Errorf(
				"pedigree: candidate pool publishes %q twice after normalisation", norm)
		}
		checkNames[norm] = struct{}{}
	}

	// The solution maps slot → horse id.
	solution := make(map[string]string, len(slots))
	slotNames := make(map[string]string, len(slots))
	for _, slot := range slots {
		norm := answers[slot.SlotID]
		solution[slot.SlotID] = candidateID(in.Scope.Seed, norm)
		slotNames[slot.SlotID] = answerName[slot.SlotID]
	}

	pubSlots := make([]puzzleSlot, 0, len(slots))
	for _, slot := range slots {
		pubSlots = append(pubSlots, puzzleSlot{
			SlotID:     slot.SlotID,
			Label:      slot.Label,
			Path:       slot.Path,
			Generation: slot.Generation,
		})
	}

	pub := publicState{
		ModeID:       ModeID,
		RulesVersion: RulesVersion,
		Config:       cfg,
		Disclaimer:   Disclaimer,
		Subject: prototypeSubject{
			CharacterID:   subject.CharacterID,
			CharacterName: subject.Name,
			PrototypeName: prototypeDisplayName(subject),
			NKBID:         subject.NKBID,
		},
		Slots:      pubSlots,
		Candidates: pool,
		Subjects:   roster,
		Scoring:    "按答对的位置占比换算为100分，最后取整数；未填写的按未答处理。",
		Rules:      buildSlotRules(),
	}
	priv := privateState{
		ModeID:    ModeID,
		Solution:  solution,
		SlotNames: slotNames,
	}
	return modes.Prepared{
		Public:        modeutil.Marshal(pub),
		Private:       modeutil.Marshal(priv),
		QuestionCount: len(pubSlots),
	}, nil
}

// prototypeDisplayName prefers the Japanese prototype name, which is the
// historical horse's registered name, and falls back to the Chinese one.
func prototypeDisplayName(e entry) string {
	if e.PrototypeNames.Ja != "" {
		return e.PrototypeNames.Ja
	}
	if e.PrototypeNames.ZhHans != "" {
		return e.PrototypeNames.ZhHans
	}
	return e.Name
}

// ancestorNKBID returns the ancestor's own independent prototype id when the
// ancestor is itself a compiled record. An empty result means the identity rests
// on the published name alone, which is why the name is published verbatim.
func ancestorNKBID(entries []entry, normalized string) string {
	ids, prototypes, names := identityCounts(entries)
	for _, e := range entries {
		if e.NKBID == "" || e.PrototypeNames.JaConfidence != "confirmed" {
			continue
		}
		if confirmedIdentity(e, ids, prototypes, names) && normalizeName(prototypeDisplayName(e)) == normalized {
			return e.NKBID
		}
	}
	return ""
}

// distractor is one candidate ancestor name available as a decoy.
type distractor struct {
	normalized string
	display    string
}

// collectDistractors gathers every distinct ancestor name not already used.
func collectDistractors(entries []entry, used map[string]struct{}) []distractor {
	seen := make(map[string]struct{}, 512)
	out := make([]distractor, 0, 512)
	for _, e := range entries {
		for _, def := range slotDefs {
			raw := strings.TrimSpace(e.Pedigree[def.Field])
			norm := normalizeName(raw)
			if norm == "" {
				continue
			}
			if _, taken := used[norm]; taken {
				continue
			}
			if _, dup := seen[norm]; dup {
				continue
			}
			seen[norm] = struct{}{}
			out = append(out, distractor{normalized: norm, display: raw})
		}
	}
	// Sort by the normalised name so the pre-shuffle order is deterministic even
	// if the input slice order ever changes.
	sort.Slice(out, func(i, j int) bool { return out[i].normalized < out[j].normalized })
	return out
}

// pickSlots chooses which slots the puzzle uses.
//
// The parent generation is always included first (in the documented order), then
// grandparents are drawn from the seed. That keeps a four-slot puzzle a genuine
// parent-and-grandparent question rather than, say, four grandparents.
func pickSlots(seed string, count int) ([]slotDef, error) {
	if count < MinSlots || count > MaxSlots {
		return nil, fmt.Errorf("pedigree: slot count %d outside [%d,%d]", count, MinSlots, MaxSlots)
	}
	parents := make([]slotDef, 0, 2)
	grandparents := make([]slotDef, 0, 4)
	for _, def := range slotDefs {
		if def.Generation == 1 {
			parents = append(parents, def)
		} else {
			grandparents = append(grandparents, def)
		}
	}
	chosen := make([]slotDef, 0, count)
	chosen = append(chosen, parents...)

	// Draw grandparents in a seeded order.
	stream := modeutil.NewStream(seed + "\x00pedigree.slots")
	order := make([]int, len(grandparents))
	for i := range order {
		order[i] = i
	}
	stream.Shuffle(len(order), func(i, j int) { order[i], order[j] = order[j], order[i] })
	for _, idx := range order {
		if len(chosen) >= count {
			break
		}
		chosen = append(chosen, grandparents[idx])
	}
	if len(chosen) != count {
		return nil, fmt.Errorf("pedigree: cannot build a %d-slot puzzle", count)
	}
	// Publish the slots in the documented order so the board reads consistently.
	sort.Slice(chosen, func(i, j int) bool {
		return slotOrder(chosen[i].SlotID) < slotOrder(chosen[j].SlotID)
	})
	return chosen, nil
}

// slotOrder is the index of a slot in the documented order.
func slotOrder(slotID string) int {
	for i, def := range slotDefs {
		if def.SlotID == slotID {
			return i
		}
	}
	return len(slotDefs)
}

// buildSlotRules assembles the published direction legend.
func buildSlotRules() []slotRule {
	out := make([]slotRule, 0, len(slotDefs))
	for _, def := range slotDefs {
		out = append(out, slotRule{
			SlotID: def.SlotID,
			Label:  def.Label,
			Means:  directionSentence(def),
		})
	}
	return out
}

// directionSentence spells a slot's direction out in words.
func directionSentence(def slotDef) string {
	switch def.SlotID {
	case "sire":
		return "父：原型赛马的父亲"
	case "dam":
		return "母：原型赛马的母亲"
	case "sireSire":
		return "父父：父亲的父亲（祖父）"
	case "sireDam":
		return "父母：父亲的母亲（祖母）"
	case "damSire":
		return "母父：母亲的父亲（外祖父）"
	case "damDam":
		return "母母：母亲的母亲（外祖母）"
	}
	return def.Label
}

// assignmentPayload is the body of ActionKindAssignment, matching the contract's
// PedigreeAssignment schema: {kind, assignments[]} where each assignment is
// {slotId, horseId}.
type assignmentPayload struct {
	Kind        string `json:"kind"`
	Assignments []struct {
		SlotID  string `json:"slotId"`
		HorseID string `json:"horseId"`
	} `json:"assignments"`
}

// decodePublic unmarshals the public state.
func decodePublic(raw json.RawMessage) (publicState, error) {
	var pub publicState
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &pub); err != nil {
			return publicState{}, fmt.Errorf("pedigree: decode public state: %w", err)
		}
	}
	return pub, nil
}

// decodePrivate unmarshals the solution.
func decodePrivate(raw json.RawMessage) (privateState, error) {
	var priv privateState
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &priv); err != nil {
			return privateState{}, fmt.Errorf("pedigree: decode private state: %w", err)
		}
	}
	return priv, nil
}

// decodeState unmarshals a participant state.
func decodeState(raw json.RawMessage) (subjectState, error) {
	var st subjectState
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &st); err != nil {
			return subjectState{}, fmt.Errorf("pedigree: decode participant state: %w", err)
		}
	}
	return st, nil
}

// Apply implements modes.Mode.
//
// A submission is the full slot → horse mapping. It replaces the caller's
// previous mapping, which is the documented revision rule ,
// and it never touches another participant's state.
func (*Mode) Apply(in modes.ApplyInput) (modes.Decision, error) {
	if in.Action.Kind != ActionKindAssignment {
		return modes.Rejected(CodeUnknownAction,
			fmt.Sprintf("pedigree: unsupported action %q", in.Action.Kind)), nil
	}
	if err := in.Action.Validate(); err != nil {
		return modes.Rejected(CodeInvalidPayload, err.Error()), nil
	}
	var payload assignmentPayload
	if err := json.Unmarshal(in.Action.Payload, &payload); err != nil {
		return modes.Rejected(CodeInvalidPayload,
			"pedigree: payload must carry assignments"), nil
	}
	if payload.Kind != "" && payload.Kind != ActionKindAssignment {
		return modes.Rejected(CodeInvalidPayload, fmt.Sprintf(
			"pedigree: payload kind %q does not match action %q",
			payload.Kind, ActionKindAssignment)), nil
	}
	if len(payload.Assignments) == 0 {
		return modes.Rejected(CodeEmptyAssignment,
			"pedigree: at least one slot must be assigned"), nil
	}

	pub, err := decodePublic(in.Public)
	if err != nil {
		return modes.Decision{}, err
	}
	if !onRoster(pub.Subjects, in.Subject.ID) {
		return modes.Rejected(CodeNotOnRoster, fmt.Sprintf(
			"pedigree: subject %q is not on this roster", in.Subject.ID)), nil
	}
	st, err := decodeState(in.State)
	if err != nil {
		return modes.Decision{}, err
	}

	// Validate the submission against the published board. Nothing here compares
	// against the solution: a rejection must never tell the player whether an
	// answer is right, only whether the board accepts it.
	knownSlots := make(map[string]struct{}, len(pub.Slots))
	for _, slot := range pub.Slots {
		knownSlots[slot.SlotID] = struct{}{}
	}
	knownHorses := make(map[string]struct{}, len(pub.Candidates))
	for _, cand := range pub.Candidates {
		knownHorses[cand.HorseID] = struct{}{}
	}
	assignedHorse := make(map[string]string, len(payload.Assignments))
	assignedSlot := make(map[string]bool, len(payload.Assignments))
	for _, item := range payload.Assignments {
		if _, ok := knownSlots[item.SlotID]; !ok {
			return modes.Rejected(CodeUnknownSlot, fmt.Sprintf(
				"pedigree: slot %q is not on this board", item.SlotID)), nil
		}
		if _, ok := knownHorses[item.HorseID]; !ok {
			return modes.Rejected(CodeUnknownHorse, fmt.Sprintf(
				"pedigree: candidate %q is not on this board", item.HorseID)), nil
		}
		if assignedSlot[item.SlotID] {
			return modes.Rejected(CodeDuplicateSlot, "pedigree: each slot accepts exactly one candidate"), nil
		}
		assignedSlot[item.SlotID] = true
		// 重复原型: the same ancestor cannot fill two slots. Accepting it would
		// make one of the two slots necessarily wrong without telling the player
		// which, so the submission is refused as malformed instead.
		if otherSlot, dup := assignedHorse[item.HorseID]; dup {
			return modes.Rejected(CodeDuplicateHorse, fmt.Sprintf(
				"pedigree: candidate %q is assigned to both %q and %q",
				item.HorseID, otherSlot, item.SlotID)), nil
		}
		assignedHorse[item.HorseID] = item.SlotID
	}

	// Store the submission. A partial mapping is allowed so a player can lock in
	// the slots they are sure about; unassigned slots score zero at settle.
	st.Assignments = make([]assignmentRecord, 0, len(payload.Assignments))
	for _, item := range payload.Assignments {
		st.Assignments = append(st.Assignments, assignmentRecord{
			SlotID:  item.SlotID,
			HorseID: item.HorseID,
		})
	}
	// Canonical order so the stored bytes are stable regardless of submission
	// order .
	sort.Slice(st.Assignments, func(i, j int) bool {
		if slotOrder(st.Assignments[i].SlotID) != slotOrder(st.Assignments[j].SlotID) {
			return slotOrder(st.Assignments[i].SlotID) < slotOrder(st.Assignments[j].SlotID)
		}
		return st.Assignments[i].HorseID < st.Assignments[j].HorseID
	})
	st.Submissions++

	feedback := modeutil.Marshal(struct {
		// AssignedSlots is how many slots this submission filled.
		AssignedSlots int `json:"assignedSlots"`
		// TotalSlots is the board size, so the client shows "3/4 filled" without
		// computing the denominator itself.
		TotalSlots int `json:"totalSlots"`
		// Submissions counts accepted revisions.
		Submissions int `json:"submissions"`
		// Note repeats the revision rule.
		Note string `json:"note"`
	}{
		AssignedSlots: len(st.Assignments),
		TotalSlots:    len(pub.Slots),
		Submissions:   st.Submissions,
		Note:          "you can revise until the deadline; the last accepted submission counts",
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

// projection is the viewer-specific round state.
type projection struct {
	ModeID       string           `json:"modeId"`
	RulesVersion string           `json:"rulesVersion"`
	Phase        modes.Phase      `json:"phase"`
	Config       Config           `json:"config"`
	Disclaimer   string           `json:"disclaimer"`
	Subject      prototypeSubject `json:"subject"`
	Slots        []puzzleSlot     `json:"slots"`
	Candidates   []candidate      `json:"candidates"`
	Rules        []slotRule       `json:"rules"`
	Scoring      string           `json:"scoring"`
	// Self is the caller's own submission.
	Self *selfView `json:"self,omitempty"`
	// Progress is the coarse, public per-participant progress. It reports how many
	// slots each participant has filled, which is what a cooperative partner
	// needs, and never which candidate they chose.
	Progress []progressView `json:"progress"`
	// Answer is the solution, emitted only when authorised.
	Answer *answerReveal `json:"answer,omitempty"`
}

// selfView is the caller's own submission.
type selfView struct {
	SubjectID   string             `json:"subjectId"`
	Assignments []assignmentRecord `json:"assignments"`
	Submissions int                `json:"submissions"`
}

// progressView is another participant's fill count.
type progressView struct {
	SubjectID     string `json:"subjectId"`
	AssignedSlots int    `json:"assignedSlots"`
	Submissions   int    `json:"submissions"`
}

// answerReveal is the solution, emitted only when authorised.
type answerReveal struct {
	// Slots maps slot id → {horseId, name} so the reveal is readable.
	Slots []revealSlot `json:"slots"`
}

// revealSlot is one revealed slot.
type revealSlot struct {
	SlotID  string `json:"slotId"`
	Label   string `json:"label"`
	HorseID string `json:"horseId"`
	Name    string `json:"name"`
}

// Project implements modes.Mode.
//
// The solution is withheld from every viewer until the round is terminal or a
// referee explicitly asks for it. A partner's progress is public because the
// documented form is cooperative, but a partner's individual choices are not: the
// score is shared, so exposing a partner's half-answer would just be a shortcut.
func (*Mode) Project(in modes.ProjectInput) (json.RawMessage, error) {
	pub, err := decodePublic(in.Public)
	if err != nil {
		return nil, err
	}
	priv, err := decodePrivate(in.Private)
	if err != nil {
		return nil, err
	}

	out := projection{
		ModeID:       ModeID,
		RulesVersion: RulesVersion,
		Phase:        in.Phase,
		Config:       pub.Config,
		Disclaimer:   pub.Disclaimer,
		Subject:      pub.Subject,
		Slots:        pub.Slots,
		Candidates:   pub.Candidates,
		Rules:        pub.Rules,
		Scoring:      pub.Scoring,
		Progress:     make([]progressView, 0, len(pub.Subjects)),
	}

	// Iterate the roster order recorded at prepare time, never the map, so the
	// projection is byte-identical across runs .
	for _, entry := range pub.Subjects {
		st, err := decodeState(in.States[entry.SubjectID])
		if err != nil {
			return nil, fmt.Errorf("pedigree: decode state of %q: %w", entry.SubjectID, err)
		}
		out.Progress = append(out.Progress, progressView{
			SubjectID:     entry.SubjectID,
			AssignedSlots: len(st.Assignments),
			Submissions:   st.Submissions,
		})
		if entry.SubjectID == in.Viewer.Subject.ID {
			assignments := st.Assignments
			if assignments == nil {
				assignments = []assignmentRecord{}
			}
			out.Self = &selfView{
				SubjectID:   entry.SubjectID,
				Assignments: assignments,
				Submissions: st.Submissions,
			}
		}
	}

	// The solution is revealed to a referee, or to everybody once the round is
	// terminal . A terminal phase is enough on its
	// own: nothing can change after it, so withholding the answer would only
	// leave the result page unable to explain the score.
	if in.Viewer.SeesSecrets() || in.Phase == modes.PhaseRevealed || in.Phase == modes.PhaseSettled {
		reveal := answerReveal{Slots: make([]revealSlot, 0, len(pub.Slots))}
		for _, slot := range pub.Slots {
			reveal.Slots = append(reveal.Slots, revealSlot{
				SlotID:  slot.SlotID,
				Label:   slot.Label,
				HorseID: priv.Solution[slot.SlotID],
				Name:    priv.SlotNames[slot.SlotID],
			})
		}
		out.Answer = &reveal
	}
	return modeutil.Marshal(out), nil
}

// Settle implements modes.Mode.
//
// the display score is floor(100 × correct slots / total slots). The
// denominator is the board size, so leaving a slot empty lowers the score exactly
// as a wrong answer does — there is no partial credit for omitting a guess.
//
// The two-person form is cooperative: both participants receive the same score,
// computed from the team's combined best mapping. Combining means a slot counts
// when either partner assigned the correct candidate to it, which is what
// "cooperation" means here; a slot where the partners disagree and one of them is
// right still scores, because the pair did establish the fact.
func (*Mode) Settle(in modes.SettleInput) ([]modes.Score, error) {
	pub, err := decodePublic(in.Public)
	if err != nil {
		return nil, err
	}
	priv, err := decodePrivate(in.Private)
	if err != nil {
		return nil, err
	}
	if len(pub.Slots) == 0 {
		return nil, fmt.Errorf("pedigree: round has no slot to score")
	}

	// Per-participant correctness, then the cooperative union.
	correctBySubject := make(map[string]map[string]bool, len(pub.Subjects))
	union := make(map[string]bool, len(pub.Slots))
	for _, entry := range pub.Subjects {
		st, err := decodeState(in.States[entry.SubjectID])
		if err != nil {
			return nil, fmt.Errorf("pedigree: decode state of %q: %w", entry.SubjectID, err)
		}
		correct := make(map[string]bool, len(pub.Slots))
		for _, record := range st.Assignments {
			want, ok := priv.Solution[record.SlotID]
			if !ok {
				continue
			}
			if record.HorseID == want {
				correct[record.SlotID] = true
				union[record.SlotID] = true
			}
		}
		correctBySubject[entry.SubjectID] = correct
	}

	// The scored count is the cooperative union for a pair, and the single
	// participant's own count otherwise. With one participant the union is their
	// own set, so one expression covers both.
	correctSlots := len(union)
	totalSlots := len(pub.Slots)
	// floor(100 × correct / total) display points, stored as scoreUnits.
	units := (100 * correctSlots / totalSlots) * 100

	scores := make([]modes.Score, 0, len(pub.Subjects))
	for _, entry := range pub.Subjects {
		own := len(correctBySubject[entry.SubjectID])
		explanation := modeutil.Marshal(struct {
			CorrectSlots int      `json:"correctSlots"`
			TotalSlots   int      `json:"totalSlots"`
			OwnCorrect   int      `json:"ownCorrectSlots"`
			Cooperative  bool     `json:"cooperative"`
			Formula      string   `json:"formula"`
			Disclaimer   string   `json:"disclaimer"`
			WrongSlots   []string `json:"wrongSlots"`
		}{
			CorrectSlots: correctSlots,
			TotalSlots:   totalSlots,
			OwnCorrect:   own,
			Cooperative:  pub.Config.Cooperative,
			Formula:      "floor(100*correctSlots/totalSlots)",
			Disclaimer:   pub.Disclaimer,
			WrongSlots:   wrongSlots(pub.Slots, union),
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

// wrongSlots lists the slots that were not filled correctly, in board order.
func wrongSlots(slots []puzzleSlot, correct map[string]bool) []string {
	out := make([]string, 0, len(slots))
	for _, slot := range slots {
		if !correct[slot.SlotID] {
			out = append(out, slot.SlotID)
		}
	}
	return out
}
