// Package creative implements G12 学园企划擂台 .
//
// Every team draws a character, an ordinary scene and a constraint, then writes a
// short pitch of at most 80 Unicode code points in 90 seconds. The pitches are
// shown anonymously, followed by a 30-second vote. A voter may not vote for their
// own work, and team votes are counted separately from audience votes.
//
// Three rules shape the implementation:
//
// 1. One vote per participating session. A vote is bound to a membership, not
// to a device, a tab or a socket. Opening the round on three devices does not
// produce three votes; the second attempt is refused with its own code. This is
// the machine-checkable half of "不能把设备数等同真实独立人".
//
// 2. Anonymous display. The published ballot carries an opaque work id and the
// text, and no author field. The mapping from work to author exists only in the
// server-only state, so no projection bug can attribute a pitch.
//
// 3. The vote is not an objective ranking. The settled result reports vote
// counts and the entertainment award; by default it contributes nothing to the
// competitive total (Config.EntertainmentBudget defaults to 0), and the
// explanation says so.
package creative

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"golang.org/x/text/unicode/norm"
	"sort"
	"strings"
	"time"

	"github.com/bhcn/umaguessinggame/internal/modes"
	"github.com/bhcn/umaguessinggame/internal/modes/modeutil"
)

// ModeID is the stable mode identifier (contract ModeId enum).
const ModeID = "creative"

// RulesVersion is the rule revision this implementation satisfies.
const RulesVersion = "2.0.0-draft.1"

// Action kinds.
//
// The values are the contract discriminators (ModeAction → CreativeWork /
// CreativeVote), not internal names .
const (
	// ActionKindWork submits a pitch.
	ActionKindWork = "creative.work"
	// ActionKindVote casts a ballot.
	ActionKindVote = "creative.vote"
)

// ContractKinds returns the ModeAction discriminators this mode accepts.
func (*Mode) ContractKinds() []string { return []string{ActionKindWork, ActionKindVote} }

// Rejection codes.
const (
	CodeInvalidPayload = "invalid_payload"
	CodeUnknownAction  = "unknown_action"
	CodeEmptyText      = "empty_text"
	CodeTextTooLong    = "text_too_long"
	CodeNotNormalised  = "text_not_normalised"
	CodeWorkClosed     = "work_closed"
	CodeVoteClosed     = "vote_closed"
	CodeUnknownWork    = "unknown_work"
	CodeOwnWork        = "cannot_vote_own_work"
	CodeAlreadyVoted   = "already_voted"
	CodeDuplicateWork  = "duplicate_work"
	CodeNotOnRoster    = "not_on_roster"
	CodeWorkRejected   = "work_rejected_by_host"
	CodeNoValidWork    = "no_valid_work"
)

// Documented parameters .
const (
	// MaxCodePoints is the pitch limit in Unicode code points.
	MaxCodePoints = 80
	// WriteSeconds is the writing window.
	WriteSeconds = 90
	// VoteSeconds is the voting window.
	VoteSeconds = 30
	// MinSubjects and MaxSubjects bound the team count.
	MinSubjects = 1
	MaxSubjects = 8
)

// VoteKind separates the two ballots .
type VoteKind string

const (
	// VoteTeam is a ballot cast by a competing team.
	VoteTeam VoteKind = "team"
	// VoteAudience is a ballot cast by a non-competing viewer.
	VoteAudience VoteKind = "audience"
)

// validVoteKind reports whether k is a documented ballot.
func validVoteKind(k VoteKind) bool {
	return k == VoteTeam || k == VoteAudience
}

// Config is the round configuration.
type Config struct {
	SeparateWindows bool `json:"separateWindows,omitempty"`
	// WriteSeconds is the writing window.
	WriteSeconds int `json:"writeSeconds"`
	// VoteSeconds is the voting window.
	VoteSeconds int `json:"voteSeconds"`
	// EntertainmentBudget is the display points this award contributes to the
	// competition. It defaults to 0, which is the documented default: the vote is
	// an entertainment award and does not enter the competitive total .
	EntertainmentBudget int `json:"entertainmentBudget"`
	// AllowAudienceVote reports that non-competing viewers may cast a ballot.
	AllowAudienceVote bool `json:"allowAudienceVote"`
}

// decodeConfig parses and normalises a configuration blob.
func decodeConfig(raw json.RawMessage) (Config, error) {
	var envelope struct {
		Config json.RawMessage `json:"config"`
	}
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &envelope); err != nil {
			return Config{}, err
		}
		if len(envelope.Config) > 0 {
			raw = envelope.Config
		}
	}
	cfg := Config{
		WriteSeconds:        WriteSeconds,
		VoteSeconds:         VoteSeconds,
		EntertainmentBudget: 0,
		AllowAudienceVote:   true,
	}
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &cfg); err != nil {
			return Config{}, fmt.Errorf("creative: invalid config: %w", err)
		}
	}
	if cfg.WriteSeconds == 0 {
		cfg.WriteSeconds = WriteSeconds
	}
	if cfg.VoteSeconds == 0 {
		cfg.VoteSeconds = VoteSeconds
	}
	if cfg.WriteSeconds < 30 || cfg.WriteSeconds > 600 {
		return Config{}, fmt.Errorf(
			"creative: writeSeconds %d outside [30,600]", cfg.WriteSeconds)
	}
	if cfg.VoteSeconds < 10 || cfg.VoteSeconds > 300 {
		return Config{}, fmt.Errorf(
			"creative: voteSeconds %d outside [10,300]", cfg.VoteSeconds)
	}
	// The budget is capped at one full award: the rules allow a small
	// entertainment bonus, never a decisive one (: 若开启少量娱乐加分，开局固化
	// 总预算，观众票不直接决定竞技冠军).
	if cfg.EntertainmentBudget < 0 || cfg.EntertainmentBudget > 20 {
		return Config{}, fmt.Errorf(
			"creative: entertainmentBudget %d outside [0,20]", cfg.EntertainmentBudget)
	}
	return cfg, nil
}

// prompt is the drawn character + scene + constraint.
type prompt struct {
	// CharacterID and CharacterName identify the drawn subject.
	CharacterID   string `json:"characterId"`
	CharacterName string `json:"characterName"`
	// Scene is the ordinary situation.
	Scene string `json:"scene"`
	// Constraint is the formal restriction the pitch must respect.
	Constraint string `json:"constraint"`
	// SceneID and ConstraintID name the authored entries, so a client can render provenance
	// without re-deriving it from the text.
	SceneID      string `json:"sceneId,omitempty"`
	ConstraintID string `json:"constraintId,omitempty"`
}

// rosterEntry is one participant recorded in the public prepared state.
type rosterEntry struct {
	SubjectID   string            `json:"subjectId"`
	SubjectKind modes.SubjectKind `json:"subjectKind"`
}

// publicState is the non-secret round state.
//
// It carries the ballot and the per-participant prompts, and it never carries the
// work → author mapping: that lives in the server-only state so anonymity is
// structural rather than a projection filter.
type publicState struct {
	Subphase string `json:"subphase,omitempty"`
	// Frozen session-to-author seat bindings never appear in a projection.
	VoterSubjects map[string]string `json:"voterSubjects,omitempty"`
	VotedSessions map[string]bool   `json:"votedSessions,omitempty"`
	ModeID        string            `json:"modeId"`
	RulesVersion  string            `json:"rulesVersion"`
	Config        Config            `json:"config"`
	Subjects      []rosterEntry     `json:"subjects"`
	// Prompts maps subject id → the prompt that subject must answer. Each team's
	// prompt is public: everybody knows what everybody was asked, which is what
	// makes the anonymity meaningful (the text is judged, not the asker).
	Prompts map[string]prompt `json:"prompts"`
	// WorkIDs maps subject id → the opaque id that subject's work will receive.
	// It is public so a team can find its own entry to check it was accepted, but
	// the ballot itself never pairs a work id with an author.
	WorkIDs map[string]string `json:"workIds"`
	// MaxCodePoints is the published limit so the client can count the same way.
	MaxCodePoints int `json:"maxCodePoints"`
	// TextRules describes the limit; the server normalizes and counts the text.
	TextRules string `json:"textRules"`
	Scoring   string `json:"scoring"`
	// OnBallot is the round-level registry of works that are on the ballot. It is
	// maintained by CommitShared, because the rule layer cannot read another
	// participant's state during Apply. A work id maps to true exactly when its
	// author submitted it and the host did not exclude it.
	OnBallot map[string]bool `json:"onBallot"`
	// VoteCount is how many ballots have been cast. It closes the writing phase:
	// a pitch cannot change after somebody voted on it.
	VoteCount int `json:"voteCount"`
}

// privateState is the server-only round state: the anonymous ballot and the
// work → author mapping.
type privateState struct {
	ModeID string `json:"modeId"`
	// AuthorOf maps work id → subject id. It is the anonymity boundary: it never
	// appears in a projection.
	AuthorOf map[string]string `json:"authorOf"`
	// TextOf maps work id → the submitted text, used for the reveal.
	TextOf map[string]string `json:"textOf"`
}

// subjectState is one participant's own persisted state.
type subjectState struct {
	VoterID string `json:"voterId,omitempty"`
	// WorkID is the work this participant submitted, if any.
	WorkID string `json:"workId,omitempty"`
	// WorkText is the submitted text.
	WorkText string `json:"workText,omitempty"`
	// Submitted reports whether a work was accepted.
	Submitted bool `json:"submitted"`
	// HostRejected reports that the host excluded this work from the ballot
	// .
	HostRejected bool `json:"hostRejected,omitempty"`
	// VoteWorkID is the work this participant voted for, if any.
	VoteWorkID string `json:"voteWorkId,omitempty"`
	// VoteKind is the ballot this participant cast.
	VoteKind VoteKind `json:"voteKind,omitempty"`
	// Voted reports whether a ballot was cast.
	Voted bool `json:"voted"`
}

// Mode implements modes.Mode for G12.
type Mode struct{}

// New returns the creative mode.
func New() *Mode { return &Mode{} }

// ID implements modes.Mode.
func (*Mode) ID() string { return ModeID }

// RulesVersion implements modes.Mode.
func (*Mode) RulesVersion() string { return RulesVersion }

// Title implements modes.Mode.
func (*Mode) Title() string { return "学园企划擂台" }

// Summary implements modes.Mode.
func (*Mode) Summary() string {
	return "抽角色、场景与限制，写一段不超过 80 字的企划；匿名展示后由队票与观众票分别评选创意奖。"
}

// InformationStyles reports which information forms this mode implements.
func (*Mode) InformationStyles() []string {
	return []string{"closed"}
}

// RoundWindow implements modes.Mode.
//
// 创作 + 投票两段。返回 0 表示本模式没有自己的窗口意见，交给房间层默认值。
func (*Mode) RoundWindow(raw json.RawMessage) time.Duration {
	cfg, err := decodeConfig(raw)
	if err != nil {
		return 0
	}
	if cfg.SeparateWindows {
		return time.Duration(cfg.WriteSeconds) * time.Second
	}
	return time.Duration(cfg.WriteSeconds+cfg.VoteSeconds) * time.Second
}

// TurnWindow implements modes.Mode. This mode is not turn-based: it runs on the
// single RoundWindow deadline, so there is no per-turn clock to report.
func (*Mode) TurnWindow(json.RawMessage) time.Duration { return 0 }

// MinSubjects implements modes.Mode.
func (*Mode) MinSubjects() int { return MinSubjects }

// MaxSubjects implements modes.Mode.
func (*Mode) MaxSubjects() int { return MaxSubjects }

// Requirement implements modes.Mode.
//
// creative is a subjective-creation mode: the prompts are authored by the round and
// the pitches come from players, so it needs no scraped fact and no verified media.
func (*Mode) Requirement() modes.ContentRequirement {
	// Fields must name EVERY section this mode reads. loadPrompts draws the character from the
	// public name index and loadPromptPool draws the scene and constraint from the authored
	// pool, so both are declared. Declaring only one is how this project produced "available
	// but cannot start" defects.
	//
	// MinEntities is the smallest viable CONTENT POOL. One scene and one constraint are enough
	// to open a round, so the floor is 1. It is NOT MinSubjects: how many teams may sit down is
	// the room layer's question, and conflating the two is what forced other modes to report a
	// table size as a pool size.
	return modes.ContentRequirement{
		Fields:      []string{PromptsSection, CharactersSection},
		MinEntities: 1,
		NeedsAssets: false,
	}
}

// ValidateConfig implements modes.Mode.
func (*Mode) ValidateConfig(cfg json.RawMessage) error {
	_, err := decodeConfig(cfg)
	return err
}

// Section names this mode reads. They are exactly the fields Requirement().Fields
// declares: a mode whose declaration does not match what it reads is how this project
// produced "available but cannot start" defects.
const (
	// PromptsSection carries the authored scene and constraint pool.
	PromptsSection = "creative.prompts"
	// CharactersSection carries the public name index the drawn character comes from.
	CharactersSection = "characters.index"
)

// Provenance kinds accepted in the authored prompts.
const (
	ProvenanceAuthored = "authored-design-value"
	ProvenanceDerived  = "derived-from-doc"
	ProvenanceOfficial = "official-data"
)

// promptProvenance records where an authored prompt came from.
//
// The rule layer can verify Kind is one of the enumerated values, NotOfficialData is true for
// anything this project designed, and Author and Basis are non-empty. It CANNOT verify that
// Basis names a real document — that is a human review boundary, recorded here rather than
// papered over.
type promptProvenance struct {
	Kind            string `json:"kind"`
	NotOfficialData bool   `json:"notOfficialData"`
	Author          string `json:"author"`
	Basis           string `json:"basis"`
}

// validate reports whether the block may be published.
func (p promptProvenance) validate() error {
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

// authoredScene is one authored situation.
type authoredScene struct {
	SceneID    string           `json:"sceneId"`
	Text       string           `json:"text"`
	Provenance promptProvenance `json:"provenance"`
}

// authoredConstraint is one authored formal restriction.
type authoredConstraint struct {
	ConstraintID string           `json:"constraintId"`
	Text         string           `json:"text"`
	Provenance   promptProvenance `json:"provenance"`
}

// promptPool is the loaded authored pool.
type promptPool struct {
	Scenes      []authoredScene
	Constraints []authoredConstraint
}

// loadPromptPool reads the authored scene and constraint pool from the pack.
//
// There is deliberately no built-in fallback: a fallback would make "the compiler produced no
// prompts" look identical to "the mode works", which is the failure this project keeps
// hitting. A pack without this section means the mode cannot start.
func loadPromptPool(src modes.ContentSource) (promptPool, error) {
	if src == nil {
		return promptPool{}, fmt.Errorf(
			"creative: no content source; %q is required and there is no built-in fallback",
			PromptsSection)
	}
	raw, err := modeutil.Section(src, PromptsSection)
	if err != nil {
		return promptPool{}, fmt.Errorf(
			"creative: %q is required: %w (there is no built-in fallback; the pack must "+
				"publish the scene and constraint pool)", PromptsSection, err)
	}
	var doc struct {
		Scenes      json.RawMessage `json:"scenes"`
		Constraints json.RawMessage `json:"constraints"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		return promptPool{}, fmt.Errorf("creative: decode %q: %w", PromptsSection, err)
	}
	var out promptPool

	sceneRecords, err := modeutil.Records(doc.Scenes, "scenes", "items")
	if err != nil {
		return promptPool{}, fmt.Errorf("creative: decode scenes in %q: %w", PromptsSection, err)
	}
	seenScene := make(map[string]struct{}, len(sceneRecords))
	for _, rec := range sceneRecords {
		sc := authoredScene{
			SceneID: rec.Str("sceneId", "scene_id", "id"),
			Text:    rec.Str("text", "label"),
		}
		if sc.SceneID == "" || sc.Text == "" {
			continue
		}
		if _, dup := seenScene[sc.SceneID]; dup {
			return promptPool{}, fmt.Errorf("creative: scene %q appears twice", sc.SceneID)
		}
		seenScene[sc.SceneID] = struct{}{}
		provRaw, ok := rec.Object("provenance")
		if !ok {
			return promptPool{}, fmt.Errorf("creative: scene %q carries no provenance block", sc.SceneID)
		}
		sc.Provenance = promptProvenance{
			Kind:            provRaw.Str("kind"),
			NotOfficialData: provRaw.Bool("notOfficialData", "not_official_data"),
			Author:          provRaw.Str("author"),
			Basis:           provRaw.Str("basis"),
		}
		if err := sc.Provenance.validate(); err != nil {
			return promptPool{}, fmt.Errorf("creative: scene %q: %w", sc.SceneID, err)
		}
		out.Scenes = append(out.Scenes, sc)
	}

	constraintRecords, err := modeutil.Records(doc.Constraints, "constraints", "items")
	if err != nil {
		return promptPool{}, fmt.Errorf(
			"creative: decode constraints in %q: %w", PromptsSection, err)
	}
	seenConstraint := make(map[string]struct{}, len(constraintRecords))
	for _, rec := range constraintRecords {
		ct := authoredConstraint{
			ConstraintID: rec.Str("constraintId", "constraint_id", "id"),
			Text:         rec.Str("text", "label"),
		}
		if ct.ConstraintID == "" || ct.Text == "" {
			continue
		}
		if _, dup := seenConstraint[ct.ConstraintID]; dup {
			return promptPool{}, fmt.Errorf(
				"creative: constraint %q appears twice", ct.ConstraintID)
		}
		seenConstraint[ct.ConstraintID] = struct{}{}
		provRaw, ok := rec.Object("provenance")
		if !ok {
			return promptPool{}, fmt.Errorf(
				"creative: constraint %q carries no provenance block", ct.ConstraintID)
		}
		ct.Provenance = promptProvenance{
			Kind:            provRaw.Str("kind"),
			NotOfficialData: provRaw.Bool("notOfficialData", "not_official_data"),
			Author:          provRaw.Str("author"),
			Basis:           provRaw.Str("basis"),
		}
		if err := ct.Provenance.validate(); err != nil {
			return promptPool{}, fmt.Errorf("creative: constraint %q: %w", ct.ConstraintID, err)
		}
		out.Constraints = append(out.Constraints, ct)
	}

	if len(out.Scenes) == 0 {
		return promptPool{}, fmt.Errorf("creative: %q carries no usable scene", PromptsSection)
	}
	if len(out.Constraints) == 0 {
		return promptPool{}, fmt.Errorf(
			"creative: %q carries no usable constraint", PromptsSection)
	}
	return out, nil
}

// loadPrompts reads the authored character pool from the pack when present and// loadPrompts reads the authored character pool from the pack when present and
// falls back to a small built-in list otherwise.
//
// The prompt uses the character pool only for a name; no aptitude, no field and no
// fact about the character is used, so the mode needs no content admission of its
// own facts.
// loadPrompts reads the character pool from the public name index.
//
// There is deliberately no built-in fallback. A fallback would make "the pack carries no name
// index" look identical to "the mode works", and it would also contradict
// Requirement().Fields, which declares CharactersSection. A mode whose declaration promises a
// section it can do without is exactly the inconsistency this project keeps shipping.
func loadPrompts(src modes.ContentSource) ([]struct{ ID, Name string }, error) {
	if src == nil {
		return nil, fmt.Errorf(
			"creative: no content source; %q is required and there is no built-in fallback",
			CharactersSection)
	}
	// classic.characters is accepted as a fallback SPELLING of the same public data, not as a
	// substitute dataset: both publish identity and display name, never a secret.
	var raw json.RawMessage
	for _, section := range []string{CharactersSection, "classic.characters"} {
		if got, ok := src.Section(section); ok && len(got) > 0 {
			raw = got
			break
		}
	}
	if len(raw) == 0 {
		return nil, fmt.Errorf(
			"creative: %q is required: the pack publishes no name index (there is no built-in "+
				"fallback)", CharactersSection)
	}
	records, err := modeutil.Records(raw, "characters", "items")
	if err != nil {
		return nil, fmt.Errorf("creative: decode %q: %w", CharactersSection, err)
	}
	out := make([]struct{ ID, Name string }, 0, len(records))
	for _, rec := range records {
		id := rec.Str("id", "characterId")
		name := rec.Str("nameZh", "name", "label")
		if id == "" || name == "" {
			continue
		}
		out = append(out, struct{ ID, Name string }{ID: id, Name: name})
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("creative: %q carries no usable character", CharactersSection)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

// Prepare implements modes.Mode.// Prepare implements modes.Mode.
//
// The draw is seeded: each team's character, scene and constraint come from the
// round seed, so the same scope always yields the same prompts. The work ids are
// opaque and seeded too, so a work id cannot be turned into an author by reading it.
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
			return modes.Prepared{}, fmt.Errorf("creative: duplicate subject %q", subj.ID)
		}
		seen[subj.ID] = struct{}{}
		roster = append(roster, rosterEntry{SubjectID: subj.ID, SubjectKind: subj.Kind})
	}
	if len(roster) < MinSubjects || len(roster) > MaxSubjects {
		return modes.Prepared{}, fmt.Errorf(
			"creative: %d participants outside [%d,%d]",
			len(roster), MinSubjects, MaxSubjects)
	}

	if len(in.Participants) > 0 {
		cfg.SeparateWindows = true
	}
	promptPool, err := loadPromptPool(in.Content)
	if err != nil {
		return modes.Prepared{}, err
	}
	pool, err := loadPrompts(in.Content)
	if err != nil {
		return modes.Prepared{}, err
	}
	if len(pool) < len(roster) {
		return modes.Prepared{}, fmt.Errorf(
			"creative: the character pool holds %d entries but the round has %d teams",
			len(pool), len(roster))
	}

	// Draw without replacement so two teams never pitch the same character.
	stream := modeutil.NewStream(in.Scope.Seed + "\x00creative.pool")
	indices := make([]int, len(pool))
	for i := range indices {
		indices[i] = i
	}
	stream.Shuffle(len(indices), func(i, j int) { indices[i], indices[j] = indices[j], indices[i] })

	prompts := make(map[string]prompt, len(roster))
	workIDs := make(map[string]string, len(roster))
	authorOf := make(map[string]string, len(roster))
	for i, entry := range roster {
		char := pool[indices[i]]
		// The scene and the constraint are drawn per seat, independently, so two
		// teams with the same scene still differ in their restriction.
		sceneIdx := modeutil.PickIndex(
			in.Scope.Seed, "creative.scene."+entry.SubjectID, len(promptPool.Scenes))
		consIdx := modeutil.PickIndex(
			in.Scope.Seed, "creative.constraint."+entry.SubjectID, len(promptPool.Constraints))
		prompts[entry.SubjectID] = prompt{
			CharacterID:   char.ID,
			CharacterName: char.Name,
			Scene:         promptPool.Scenes[sceneIdx].Text,
			Constraint:    promptPool.Constraints[consIdx].Text,
			SceneID:       promptPool.Scenes[sceneIdx].SceneID,
			ConstraintID:  promptPool.Constraints[consIdx].ConstraintID,
		}
		workID := workIDFor(in.Scope.Seed, entry.SubjectID)
		workIDs[entry.SubjectID] = workID
		authorOf[workID] = entry.SubjectID
	}
	// Every work id must be distinct, otherwise the ballot could not address a
	// single work.
	if len(authorOf) != len(roster) {
		return modes.Prepared{}, fmt.Errorf(
			"creative: %d work ids for %d teams; the ids are not distinct", len(authorOf), len(roster))
	}

	pub := publicState{
		ModeID:        ModeID,
		RulesVersion:  RulesVersion,
		Config:        cfg,
		Subjects:      roster,
		Prompts:       prompts,
		WorkIDs:       workIDs,
		MaxCodePoints: MaxCodePoints,
		TextRules:     "最多80字；部分组合表情会占多个字数，以输入框旁的计数为准。超出时请精简后再确认。",
		Scoring: "本场创意奖：队票与观众票分开统计；" +
			"默认不进入正式竞技总分",
		OnBallot: map[string]bool{},
	}
	if cfg.SeparateWindows {
		pub.Subphase = "writing"
		pub.VoterSubjects = map[string]string{}
		pub.VotedSessions = map[string]bool{}
		for _, p := range in.Participants {
			if p.SessionID == "" || p.MembershipID == "" || pub.VoterSubjects[p.SessionID] != "" || pub.WorkIDs[p.Subject.ID] == "" {
				return modes.Prepared{}, fmt.Errorf("creative: invalid frozen voter roster")
			}
			pub.VoterSubjects[p.SessionID] = p.Subject.ID
		}
	}
	priv := privateState{
		ModeID:   ModeID,
		AuthorOf: authorOf,
		TextOf:   map[string]string{},
	}
	return modes.Prepared{
		Public:        modeutil.Marshal(pub),
		Private:       modeutil.Marshal(priv),
		Config:        modeutil.Marshal(cfg),
		QuestionCount: len(roster),
	}, nil
}

// workIDFor builds an opaque, stable work id for one author.
//
// It is a seeded digest, not "work-<subjectId>": a readable id would hand the
// author over and defeat the anonymity the mode depends on.
func workIDFor(seed, subjectID string) string {
	sum := sha256.Sum256([]byte(seed + "\x00creative.work\x00" + subjectID))
	return "cw-" + hex.EncodeToString(sum[:8])
}

// countCodePoints counts Unicode code points, which is the documented unit
// .
func countCodePoints(s string) int {
	n := 0
	for range s {
		n++
	}
	return n
}

// workPayload is the body of ActionKindWork, matching the contract's CreativeWork
// schema: {kind, text} with additionalProperties false.
type workPayload struct {
	Kind string `json:"kind"`
	Text string `json:"text"`
}

// votePayload is the body of ActionKindVote, matching the contract's CreativeVote
// schema: {kind, optionId}.
type votePayload struct {
	Kind     string `json:"kind"`
	OptionID string `json:"optionId"`
}

// decodePublic unmarshals the public state.
func decodePublic(raw json.RawMessage) (publicState, error) {
	var pub publicState
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &pub); err != nil {
			return publicState{}, fmt.Errorf("creative: decode public state: %w", err)
		}
	}
	return pub, nil
}

// decodePrivate unmarshals the anonymity mapping.
func decodePrivate(raw json.RawMessage) (privateState, error) {
	var priv privateState
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &priv); err != nil {
			return privateState{}, fmt.Errorf("creative: decode private state: %w", err)
		}
	}
	return priv, nil
}

// decodeState unmarshals a participant state.
func decodeState(raw json.RawMessage) (subjectState, error) {
	var st subjectState
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &st); err != nil {
			return subjectState{}, fmt.Errorf("creative: decode participant state: %w", err)
		}
	}
	return st, nil
}

// Apply implements modes.Mode.
//
// A work submission and a ballot are separate actions with separate windows: a work
// is accepted while the writing phase is open, a vote while the voting phase is
// open. The rule layer cannot see the phase, so it enforces the ordering it can see
// — a ballot requires that at least one work exists — and the room layer owns the
// clock.
func (*Mode) Apply(in modes.ApplyInput) (modes.Decision, error) {
	switch in.Action.Kind {
	case ActionKindWork:
		return applyWork(in)
	case ActionKindVote:
		return applyVote(in)
	}
	return modes.Rejected(CodeUnknownAction,
		fmt.Sprintf("creative: unsupported action %q", in.Action.Kind)), nil
}

// applyWork validates and records a pitch.
func applyWork(in modes.ApplyInput) (modes.Decision, error) {
	if err := in.Action.Validate(); err != nil {
		return modes.Rejected(CodeInvalidPayload, err.Error()), nil
	}
	var payload workPayload
	if err := json.Unmarshal(in.Action.Payload, &payload); err != nil {
		return modes.Rejected(CodeInvalidPayload, "creative: payload must carry text"), nil
	}
	if payload.Kind != "" && payload.Kind != ActionKindWork {
		return modes.Rejected(CodeInvalidPayload, fmt.Sprintf(
			"creative: payload kind %q does not match action %q",
			payload.Kind, ActionKindWork)), nil
	}

	pub, err := decodePublic(in.Public)
	if err != nil {
		return modes.Decision{}, err
	}
	if pub.Config.SeparateWindows && pub.Subphase != "writing" {
		return modes.Rejected(CodeWorkClosed, "creative: writing is closed"), nil
	}
	if _, ok := pub.WorkIDs[in.Subject.ID]; !ok {
		return modes.Rejected(CodeNotOnRoster, fmt.Sprintf(
			"creative: subject %q is not on this roster", in.Subject.ID)), nil
	}
	st, err := decodeState(in.State)
	if err != nil {
		return modes.Decision{}, err
	}

	// A work may be revised before the deadline, but once a ballot exists the
	// writing phase is over: changing a pitch after somebody voted on it would
	// invalidate that vote.
	// ApplyInput carries only the caller's own state, so the round-wide vote count
	// cannot be read here. It is carried in the public round state by
	// CommitShared, which the room layer calls inside the same transaction, so the
	// count is authoritative because it is persisted with the ballot that produced
	// it.
	if pub.VoteCount > 0 {
		return modes.Rejected(CodeWorkClosed,
			"creative: voting has started, so the pitch can no longer be changed"), nil
	}

	text := norm.NFC.String(strings.TrimSpace(payload.Text))
	if text == "" {
		return modes.Rejected(CodeEmptyText, "creative: the pitch is empty"), nil
	}
	// The limit is counted in code points, which is the documented unit.
	if n := countCodePoints(text); n > MaxCodePoints {
		return modes.Rejected(CodeTextTooLong, fmt.Sprintf(
			"creative: the pitch is %d code points; the limit is %d", n, MaxCodePoints)), nil
	}
	// Count the equivalent normalized form on the server; never truncate.

	st.WorkID = pub.WorkIDs[in.Subject.ID]
	st.WorkText = text
	st.Submitted = true
	st.HostRejected = false

	feedback := modeutil.Marshal(struct {
		WorkID string `json:"workId"`
		// CodePoints is the counted length, so the client shows the same number.
		CodePoints int `json:"codePoints"`
		// Limit is the published maximum.
		Limit int    `json:"limit"`
		Note  string `json:"note"`
	}{
		WorkID:     st.WorkID,
		CodePoints: countCodePoints(text),
		Limit:      MaxCodePoints,
		Note:       "作品已记录；投票开始前可以修改，投票开始后不能再改",
	})
	return modes.Accepted(feedback, modeutil.Marshal(st)), nil
}

// applyVote validates and records a ballot.
func applyVote(in modes.ApplyInput) (modes.Decision, error) {
	pubCheck, err := decodePublic(in.Public)
	if err != nil {
		return modes.Decision{}, err
	}
	if pubCheck.Config.SeparateWindows {
		return modes.Rejected(CodeVoteClosed, "creative: use the authenticated session ballot"), nil
	}
	if err := in.Action.Validate(); err != nil {
		return modes.Rejected(CodeInvalidPayload, err.Error()), nil
	}
	var payload votePayload
	if err := json.Unmarshal(in.Action.Payload, &payload); err != nil {
		return modes.Rejected(CodeInvalidPayload, "creative: payload must carry optionId"), nil
	}
	if payload.Kind != "" && payload.Kind != ActionKindVote {
		return modes.Rejected(CodeInvalidPayload, fmt.Sprintf(
			"creative: payload kind %q does not match action %q",
			payload.Kind, ActionKindVote)), nil
	}
	if payload.OptionID == "" {
		return modes.Rejected(CodeInvalidPayload, "creative: empty optionId"), nil
	}

	pub, err := decodePublic(in.Public)
	if err != nil {
		return modes.Decision{}, err
	}
	priv, err := decodePrivate(in.Private)
	if err != nil {
		return modes.Decision{}, err
	}
	st, err := decodeState(in.State)
	if err != nil {
		return modes.Decision{}, err
	}

	// The ballot must address a work that is actually on the ballot.
	authorID, ok := priv.AuthorOf[payload.OptionID]
	if !ok {
		return modes.Rejected(CodeUnknownWork, fmt.Sprintf(
			"creative: work %q is not on this ballot", payload.OptionID)), nil
	}
	// The work must be on the ballot: submitted and not excluded by the host. The
	// round-level registry is the authority, because ApplyInput cannot see another
	// participant's state.
	if !pub.OnBallot[payload.OptionID] {
		return modes.Rejected(CodeWorkRejected, fmt.Sprintf(
			"creative: work %q is not on the ballot", payload.OptionID)), nil
	}
	// 不投自己: the author may not vote for their own pitch .
	if authorID == in.Subject.ID {
		return modes.Rejected(CodeOwnWork,
			"creative: you cannot vote for your own work"), nil
	}
	// One vote per session. A membership that already voted cannot vote again,
	// whichever device or tab the second attempt comes from. This is the rule
	// layer's answer to "不能把设备数等同真实独立人": the vote is bound to the
	// membership the room resolved, not to a connection.
	if st.Voted {
		return modes.Rejected(CodeAlreadyVoted,
			"creative: this session has already voted"), nil
	}

	// The ballot kind follows from whether the voter submitted a work: a team that
	// pitched is a team vote, everybody else is an audience vote. The client cannot
	// choose its own weight .
	kind := VoteAudience
	if st.Submitted && !st.HostRejected {
		kind = VoteTeam
	}
	if kind == VoteAudience && !pub.Config.AllowAudienceVote {
		return modes.Rejected(CodeVoteClosed,
			"creative: audience voting is disabled for this round"), nil
	}

	st.VoteWorkID = payload.OptionID
	st.VoteKind = kind
	st.Voted = true

	feedback := modeutil.Marshal(struct {
		WorkID string   `json:"workId"`
		Kind   VoteKind `json:"voteKind"`
		// Note repeats that the ballot is final.
		Note string `json:"note"`
	}{
		WorkID: payload.OptionID,
		Kind:   kind,
		Note:   "选票已记录，不能修改；结果在投票结束后公布",
	})
	return modes.Accepted(feedback, modeutil.Marshal(st)), nil
}

// CommitShared implements modeutil.SharedStateMode.
//
// creative needs a round-level registry for two things the per-subject state cannot
// express:
//
// - which works are on the ballot, so a ballot can be validated during Apply
// without reading another participant's state;
// - whether any ballot has been cast, so the writing phase closes as soon as
// voting starts.
//
// The room layer calls this inside its write transaction after Apply accepted the
// action, and persists the returned public state. The fold is idempotent: replaying
// the same accepted action yields the same registry, so a retried transaction
// cannot double-count a vote.
func (*Mode) CommitShared(in modes.ApplyInput, dec modes.Decision) (json.RawMessage, json.RawMessage, error) {
	if !dec.Accepted {
		return nil, nil, fmt.Errorf("creative: cannot commit a rejected action")
	}
	pub, err := decodePublic(in.Public)
	if err != nil {
		return nil, nil, err
	}
	priv, err := decodePrivate(in.Private)
	if err != nil {
		return nil, nil, err
	}
	// The registry follows the state Apply produced, not the state before the
	// action: on a first submission the pre-action state is empty, so folding it
	// would record "not submitted" for a pitch that was just accepted.
	st, err := decodeState(dec.State)
	if err != nil {
		return nil, nil, err
	}
	if pub.OnBallot == nil {
		pub.OnBallot = make(map[string]bool, len(pub.Subjects))
	}

	switch in.Action.Kind {
	case ActionKindWork:
		// The state returned by Apply is the accepted pitch, so the registry
		// follows it exactly.
		if st.Submitted && !st.HostRejected && st.WorkID != "" {
			pub.OnBallot[st.WorkID] = true
			priv.TextOf[st.WorkID] = st.WorkText
		}
	case ActionKindVote:
		// A ballot increments the count only once per voter; Apply has already
		// refused a second ballot for the same session, so reaching here with
		// st.Voted means this is that ballot.
		if pub.Config.SeparateWindows {
			if pub.VotedSessions == nil {
				pub.VotedSessions = map[string]bool{}
			}
			if st.VoterID == "" {
				return nil, nil, fmt.Errorf("creative: missing voter identity")
			}
			pub.VotedSessions[st.VoterID] = true
			pub.VoteCount = len(pub.VotedSessions)
		} else {
			pub.VoteCount++
		}
	default:
		return nil, nil, fmt.Errorf("creative: cannot commit action %q", in.Action.Kind)
	}
	return modeutil.Marshal(pub), modeutil.Marshal(priv), nil
}

// ballotEntry is one anonymous work on the ballot.
type ballotEntry struct {
	WorkID string `json:"workId"`
	Text   string `json:"text"`
	// Prompt repeats the ask so the ballot is readable without a second panel.
	Prompt prompt `json:"prompt"`
}

// projection is the viewer-specific round state.
type projection struct {
	Subphase      string        `json:"subphase,omitempty"`
	Review        []ballotEntry `json:"reviewOptions,omitempty"`
	Vote          *voterView    `json:"vote,omitempty"`
	ModeID        string        `json:"modeId"`
	RulesVersion  string        `json:"rulesVersion"`
	Phase         modes.Phase   `json:"phase"`
	Config        Config        `json:"config"`
	MaxCodePoints int           `json:"maxCodePoints"`
	TextRules     string        `json:"textRules"`
	Scoring       string        `json:"scoring"`
	// Prompt is the caller's own draw.
	Prompt *prompt `json:"prompt,omitempty"`
	// Self is the caller's own submission and ballot.
	Self *selfView `json:"self,omitempty"`
	// Ballot is the anonymous work list, published once the writing phase is over.
	// It carries no author field by construction.
	//
	// It is empty while the writing phase is open, which is deliberate: showing an unfinished
	// ballot would let a team copy a stronger pitch. BallotHiddenReason says so, so a client can
	// render "the ballot opens after writing" instead of a silent empty list.
	Ballot             []ballotEntry `json:"ballot"`
	BallotHiddenReason string        `json:"ballotHiddenReason,omitempty"`
	// Progress is the coarse per-participant progress.
	Progress []progressView `json:"progress"`
	// Result is the vote tally, published once the round is terminal.
	Result *resultView `json:"result,omitempty"`
	// Authors is the work → author reveal, published once the round is terminal.
	Authors map[string]string `json:"authors,omitempty"`
}

// selfView is the caller's own state.
type selfView struct {
	SubjectID    string   `json:"subjectId"`
	WorkID       string   `json:"workId,omitempty"`
	WorkText     string   `json:"workText,omitempty"`
	Submitted    bool     `json:"submitted"`
	HostRejected bool     `json:"hostRejected,omitempty"`
	Voted        bool     `json:"voted"`
	VoteWorkID   string   `json:"voteWorkId,omitempty"`
	VoteKind     VoteKind `json:"voteKind,omitempty"`
}

// progressView is another participant's progress. It reports whether they pitched
// and voted, never which work they chose, so the ballot stays anonymous until the
// reveal.
type progressView struct {
	SubjectID string `json:"subjectId"`
	Submitted bool   `json:"submitted"`
	Voted     bool   `json:"voted"`
}

// resultView is the settled tally.
type resultView struct {
	// Works is the per-work tally, in descending vote order then work id.
	Works []workTally `json:"works"`
	// TeamVotes and AudienceVotes are the two separate pools .
	TeamVotes     int `json:"teamVotes"`
	AudienceVotes int `json:"audienceVotes"`
	// ValidWorks is how many works were on the ballot.
	ValidWorks int `json:"validWorks"`
	// Award is the entertainment award, if any work received a vote.
	Award            *awardView `json:"award,omitempty"`
	CompetitiveBonus *awardView `json:"competitiveBonus,omitempty"`
}

// workTally is one work's vote count.
type workTally struct {
	WorkID        string `json:"workId"`
	TeamVotes     int    `json:"teamVotes"`
	AudienceVotes int    `json:"audienceVotes"`
	Total         int    `json:"total"`
	Rank          int    `json:"rank"`
	Shared        bool   `json:"shared"`
}

// awardView is the entertainment award winner.
type awardView struct {
	WorkIDs []string `json:"workIds"`
	// Shared reports that several works tied for the award.
	Shared bool `json:"shared"`
	// Budget is the display points the award carries, 0 by default.
	Budget int `json:"budget"`
}

// Project implements modes.Mode.
//
// The anonymity rules, all enforced here:
//
// - The ballot carries work ids and texts only. The author mapping lives in the
// private state and is published only when the round is terminal.
// - A caller sees their own work and their own ballot; another participant's
// ballot is never disclosed before the reveal, because knowing it would let a
// team count votes and coordinate.
// - The tally appears only at the end.
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
		Subphase:      pub.Subphase,
		ModeID:        ModeID,
		RulesVersion:  RulesVersion,
		Phase:         in.Phase,
		Config:        pub.Config,
		MaxCodePoints: pub.MaxCodePoints,
		TextRules:     pub.TextRules,
		Scoring:       pub.Scoring,
		Ballot:        []ballotEntry{},
		Progress:      make([]progressView, 0, len(pub.Subjects)),
	}

	// Iterate the roster order recorded at prepare time, never the map, so the
	// projection is byte-identical across runs .
	for _, entry := range pub.Subjects {
		st, err := decodeState(in.States[entry.SubjectID])
		if err != nil {
			return nil, fmt.Errorf("creative: decode state of %q: %w", entry.SubjectID, err)
		}
		voted := st.Voted
		if pub.Config.SeparateWindows {
			// A team's work is shared, but the vote is stored per frozen
			// session. Project only whether at least one member has voted;
			// never expose the selected work or a session identifier.
			for session, seat := range pub.VoterSubjects {
				if seat != entry.SubjectID {
					continue
				}
				voteState, voteErr := decodeState(in.States[VoteSubject(session).ID])
				if voteErr != nil {
					return nil, voteErr
				}
				voted = voted || voteState.Voted
			}
		}
		out.Progress = append(out.Progress, progressView{
			SubjectID: entry.SubjectID,
			Submitted: st.Submitted,
			Voted:     voted,
		})
		if entry.SubjectID == in.Viewer.Subject.ID {
			pr := pub.Prompts[entry.SubjectID]
			out.Prompt = &pr
			out.Self = &selfView{
				SubjectID:    entry.SubjectID,
				WorkID:       st.WorkID,
				WorkText:     st.WorkText,
				Submitted:    st.Submitted,
				HostRejected: st.HostRejected || ((pub.Subphase == "voting" || pub.Subphase == "closed") && pub.Config.SeparateWindows && st.Submitted && !pub.OnBallot[st.WorkID]),
				Voted:        st.Voted,
				VoteWorkID:   st.VoteWorkID,
				VoteKind:     st.VoteKind,
			}
		}
	}

	// Legacy fixtures retain their old shape. Live rounds publish only the frozen ballot.
	selfSubmitted := false
	if own, err := decodeState(in.States[in.Viewer.Subject.ID]); err == nil {
		selfSubmitted = own.Submitted
	}
	showBallot := in.Phase != modes.PhaseCollecting || in.Viewer.IsReferee ||
		in.Phase.Terminal() || selfSubmitted
	if pub.Config.SeparateWindows {
		showBallot = pub.Subphase == "voting" || pub.Subphase == "closed"
		if in.Phase == modes.PhaseCancelled {
			showBallot = false
		}
	}
	if in.Phase == modes.PhaseCancelled {
		showBallot = false
	}
	if showBallot {
		for _, entry := range pub.Subjects {
			st, err := decodeState(in.States[entry.SubjectID])
			if err != nil {
				return nil, err
			}
			if !st.Submitted || st.HostRejected || (pub.Config.SeparateWindows && !pub.OnBallot[st.WorkID]) {
				continue
			}
			out.Ballot = append(out.Ballot, ballotEntry{
				WorkID: st.WorkID,
				Text:   st.WorkText,
				Prompt: pub.Prompts[entry.SubjectID],
			})
		}
		// Sort by work id so the order carries no information about submission
		// time or roster position.
		sort.Slice(out.Ballot, func(i, j int) bool {
			return out.Ballot[i].WorkID < out.Ballot[j].WorkID
		})
		if len(out.Ballot) == 0 {
			out.BallotHiddenReason = "还没有人提交作品，投票开始后才会展示候选"
		}
	} else {
		// The writing phase is still open, which includes the prepared state before anyone has
		// acted. The reason must say that, because an empty ballot here means "nothing is
		// published yet by design", not "nobody submitted".
		out.BallotHiddenReason = "写作阶段进行中：作品在投票开始时才会匿名展示，避免被抄袭"
	}

	// The tally and the authorship are published only once nothing can change.
	if (in.Phase == modes.PhaseRevealed || in.Phase == modes.PhaseSettled) && (!pub.Config.SeparateWindows || pub.Subphase == "closed") {
		result := tallyWorks(pub, priv, in.States)
		out.Result = &result
		authors := make(map[string]string, len(priv.AuthorOf))
		for workID, authorID := range priv.AuthorOf {
			if !pub.Config.SeparateWindows || pub.OnBallot[workID] {
				authors[workID] = authorID
			}
		}
		out.Authors = authors
	}
	if pub.Config.SeparateWindows && in.Phase != modes.PhaseCancelled {
		if err := projectSessionVote(&out, pub, in); err != nil {
			return nil, err
		}
	}
	return modeutil.Marshal(out), nil
}

// tallyWorks counts the two vote pools and derives the award.
func tallyWorks(pub publicState, priv privateState, states map[string]json.RawMessage) resultView {
	tallies := make(map[string]*workTally, len(pub.Subjects))
	for _, entry := range pub.Subjects {
		st, err := decodeState(states[entry.SubjectID])
		if err != nil {
			continue
		}
		if !st.Submitted || st.HostRejected || (pub.Config.SeparateWindows && !pub.OnBallot[st.WorkID]) {
			continue
		}
		tallies[st.WorkID] = &workTally{WorkID: st.WorkID}
	}

	result := resultView{Works: make([]workTally, 0, len(tallies))}
	voteStates := map[string]json.RawMessage{}
	if pub.Config.SeparateWindows {
		for session := range pub.VotedSessions {
			id := VoteSubject(session).ID
			voteStates[id] = states[id]
		}
	} else {
		for _, entry := range pub.Subjects {
			voteStates[entry.SubjectID] = states[entry.SubjectID]
		}
	}
	for _, raw := range voteStates {
		st, err := decodeState(raw)
		if err != nil {
			continue
		}
		if !st.Voted {
			continue
		}
		tally, ok := tallies[st.VoteWorkID]
		if !ok {
			// A ballot for a work that was excluded by the host does not count:
			// the work is not on the ballot, so the vote is void rather than
			// silently moved.
			continue
		}
		switch st.VoteKind {
		case VoteTeam:
			tally.TeamVotes++
			result.TeamVotes++
		case VoteAudience:
			tally.AudienceVotes++
			result.AudienceVotes++
		default:
			// An unknown ballot kind cannot be counted; it also cannot occur
			// through Apply, which derives the kind from the state.
			continue
		}
		tally.Total = tally.TeamVotes + tally.AudienceVotes
	}

	// Deterministic order: total votes descending, then work id ascending.
	for _, tally := range tallies {
		result.Works = append(result.Works, *tally)
	}
	sort.Slice(result.Works, func(i, j int) bool {
		if result.Works[i].Total != result.Works[j].Total {
			return result.Works[i].Total > result.Works[j].Total
		}
		return result.Works[i].WorkID < result.Works[j].WorkID
	})
	result.ValidWorks = len(result.Works)

	// Competition ranks on total votes, sharing a rank on a tie .
	for i := range result.Works {
		ahead, same := 0, 0
		for j := range result.Works {
			if i == j {
				continue
			}
			switch {
			case result.Works[j].Total > result.Works[i].Total:
				ahead++
			case result.Works[j].Total == result.Works[i].Total:
				same++
			}
		}
		result.Works[i].Rank = ahead + 1
		result.Works[i].Shared = same > 0
	}

	// The award goes to every work on the top rank, and only when somebody voted:
	// a ballot with zero votes has no winner rather than every work tying at 0.
	if len(result.Works) > 0 && result.Works[0].Total > 0 {
		top := result.Works[0].Total
		ids := make([]string, 0, len(result.Works))
		for _, w := range result.Works {
			if w.Total == top {
				ids = append(ids, w.WorkID)
			}
		}
		sort.Strings(ids)
		result.Award = &awardView{
			WorkIDs: ids,
			Shared:  len(ids) > 1,
			Budget:  pub.Config.EntertainmentBudget,
		}
	}
	if pub.Config.SeparateWindows {
		if result.Award != nil {
			result.Award.Budget = 0
		}
		if pub.Config.EntertainmentBudget > 0 {
			top := 0
			ids := []string{}
			for _, work := range result.Works {
				if work.TeamVotes > top {
					top = work.TeamVotes
				}
			}
			if top > 0 {
				for _, work := range result.Works {
					if work.TeamVotes == top {
						ids = append(ids, work.WorkID)
					}
				}
			}
			sort.Strings(ids)
			result.CompetitiveBonus = &awardView{WorkIDs: ids, Shared: len(ids) > 1, Budget: pub.Config.EntertainmentBudget}
		}
	}
	return result
}

// Settle implements modes.Mode.
//
// By default the award contributes nothing to the competitive total
// (Config.EntertainmentBudget is 0), which is the documented default
// . When a round does enable a small
// bonus, the budget is fixed at prepare time and split between the works on the top
// rank, so the audience vote cannot decide the competition on its own.
//
// Every participant receives the same award value: the creative award belongs to
// the work, and a team is scored as one subject.
func (*Mode) Settle(in modes.SettleInput) ([]modes.Score, error) {
	pub, err := decodePublic(in.Public)
	if err != nil {
		return nil, err
	}
	priv, err := decodePrivate(in.Private)
	if err != nil {
		return nil, err
	}
	if in.Phase == modes.PhaseCancelled || (pub.Config.SeparateWindows && pub.Subphase != "closed") {
		return nil, fmt.Errorf("creative: voting is not closed")
	}
	result := tallyWorks(pub, priv, in.States)

	// Units per winning work: the budget split over the top rank, floored.
	awardUnits := 0
	winner := make(map[string]struct{}, len(pub.Subjects))
	if pub.Config.SeparateWindows {
		top := 0
		for _, work := range result.Works {
			if work.TeamVotes > top {
				top = work.TeamVotes
			}
		}
		if top > 0 {
			for _, work := range result.Works {
				if work.TeamVotes == top {
					winner[work.WorkID] = struct{}{}
				}
			}
		}
		if len(winner) > 0 {
			awardUnits = (pub.Config.EntertainmentBudget / len(winner)) * modeutil.UnitsPerPoint
		}
	} else if result.Award != nil {
		for _, id := range result.Award.WorkIDs {
			winner[id] = struct{}{}
		}
		if len(winner) > 0 {
			awardUnits = (result.Award.Budget / len(winner)) * modeutil.UnitsPerPoint
		}
	}

	scores := make([]modes.Score, 0, len(pub.Subjects))
	for _, entry := range pub.Subjects {
		st, err := decodeState(in.States[entry.SubjectID])
		if err != nil {
			return nil, fmt.Errorf("creative: decode state of %q: %w", entry.SubjectID, err)
		}
		units := 0
		if _, won := winner[st.WorkID]; won && st.Submitted && !st.HostRejected {
			units = awardUnits
		}
		// MaxScoreUnits is the full award budget, or one point when the award is
		// disabled. It is never 0, which Score.Validate rejects.
		maxUnits := modeutil.Units(1)
		if pub.Config.EntertainmentBudget > 0 {
			maxUnits = modeutil.Units(pub.Config.EntertainmentBudget)
		}
		explanation := modeutil.Marshal(struct {
			WorkID              string   `json:"workId,omitempty"`
			Submitted           bool     `json:"submitted"`
			HostRejected        bool     `json:"hostRejected,omitempty"`
			Voted               bool     `json:"voted"`
			VoteKind            VoteKind `json:"voteKind,omitempty"`
			ValidWorks          int      `json:"validWorks"`
			TeamVotes           int      `json:"teamVotes"`
			AudienceVotes       int      `json:"audienceVotes"`
			WonAward            bool     `json:"wonAward"`
			EntertainmentBudget int      `json:"entertainmentBudget"`
			Formula             string   `json:"formula"`
			Note                string   `json:"note"`
		}{
			WorkID:              st.WorkID,
			Submitted:           st.Submitted,
			HostRejected:        st.HostRejected,
			Voted:               st.Voted,
			VoteKind:            st.VoteKind,
			ValidWorks:          result.ValidWorks,
			TeamVotes:           result.TeamVotes,
			AudienceVotes:       result.AudienceVotes,
			WonAward:            units > 0,
			EntertainmentBudget: pub.Config.EntertainmentBudget,
			Formula:             "默认不计入正式总分；启用时按开局固化的娱乐预算在并列头名间平分",
			Note:                settleNote(pub.Config, result),
		})
		score := modes.Score{
			Subject:       modes.Subject{ID: entry.SubjectID, Kind: entry.SubjectKind},
			ScoreUnits:    units,
			MaxScoreUnits: maxUnits,
			Explanation:   explanation,
		}
		if err := score.Validate(); err != nil {
			return nil, err
		}
		scores = append(scores, score)
	}
	return modes.RankScores(scores), nil
}

// settleNote explains why a round scored zero or how a bonus was applied.
func settleNote(cfg Config, result resultView) string {
	switch {
	case cfg.EntertainmentBudget == 0:
		return "本场创意奖仅公布票数，不计入正式竞技总分（默认设置）"
	case result.Award == nil:
		return "没有有效选票，因此本场不产生创意奖"
	default:
		return "娱乐奖按总票数评选；正式加分只按参赛者票数在并列首位间平分，观众票不参与正式加分"
	}
}
