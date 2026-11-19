// Package bluff implements anonymous decoy submissions and truth voting.
// Questions require sourced facts that have passed semantic review.
package bluff

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
	"unicode"

	"github.com/bhcn/umaguessinggame/internal/modes"
	"github.com/bhcn/umaguessinggame/internal/modes/modeutil"
)

// ModeID is the stable mode identifier (contract ModeId enum).
const ModeID = "bluff"

// RulesVersion is the rule revision this implementation satisfies.
const RulesVersion = "2.0.0-draft.1"

// Action kinds.
//
// The values are the contract discriminators (ModeAction → BluffDecoy / BluffVote),
// not internal names .
const (
	// ActionKindDecoy submits a fake answer.
	ActionKindDecoy = "bluff.decoy"
	// ActionKindVote casts a ballot for the option believed to be the truth.
	ActionKindVote = "bluff.vote"
)

// ContractKinds returns the ModeAction discriminators this mode accepts.
func (*Mode) ContractKinds() []string { return []string{ActionKindDecoy, ActionKindVote} }

// Rejection codes.
const (
	CodeInvalidPayload = "invalid_payload"
	CodeUnknownAction  = "unknown_action"
	CodeNotOnRoster    = "not_on_roster"
	CodeDecoyClosed    = "decoy_closed"
	CodeVoteClosed     = "vote_closed"
	CodeDecoyTooShort  = "decoy_too_short"
	CodeDecoyTooLong   = "decoy_too_long"
	CodeNotNormalised  = "text_not_normalised"
	CodeDuplicateDecoy = "duplicate_decoy"
	CodeUnknownOption  = "unknown_option"
	CodeAlreadyVoted   = "already_voted"
	CodeOwnDecoy       = "cannot_vote_own_decoy"
	CodeTooManyDecoys  = "too_many_decoys"
)

// Documented parameters .
const (
	// MinSubjects and MaxSubjects bound the submitting units.
	MinSubjects = 3
	MaxSubjects = 8
	// WriteSeconds is the decoy-writing window.
	WriteSeconds = 60
	// VoteSeconds is the voting window.
	VoteSeconds = 30
	// MinDecoyCodePoints and MaxDecoyCodePoints bound one fake answer.
	MinDecoyCodePoints = 2
	MaxDecoyCodePoints = 40
	// MaxOptions is the largest ballot before the options are grouped.
	MaxOptions = 9
	// TruthPoints is the value of identifying the truth.
	TruthPoints = 70
	// DeceptionPoints is the maximum deception bonus.
	DeceptionPoints = 30
)

// Config is the round configuration.
type Config struct {
	// WriteSeconds is the decoy-writing window.
	WriteSeconds int `json:"writeSeconds"`
	// VoteSeconds is the voting window.
	VoteSeconds int `json:"voteSeconds"`
	// TruthID names the reviewed fact the round uses. An empty value means "draw the
	// first admitted fact", which Prepare refuses when no fact is admitted.
	TruthID string `json:"truthId"`
}

// decodeConfig parses and normalises a configuration blob.
func decodeConfig(raw json.RawMessage) (Config, error) {
	cfg := Config{WriteSeconds: WriteSeconds, VoteSeconds: VoteSeconds}
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &cfg); err != nil {
			return Config{}, fmt.Errorf("bluff: invalid config: %w", err)
		}
	}
	if cfg.WriteSeconds == 0 {
		cfg.WriteSeconds = WriteSeconds
	}
	if cfg.VoteSeconds == 0 {
		cfg.VoteSeconds = VoteSeconds
	}
	if cfg.WriteSeconds < 20 || cfg.WriteSeconds > 300 {
		return Config{}, fmt.Errorf("bluff: writeSeconds %d outside [20,300]", cfg.WriteSeconds)
	}
	if cfg.VoteSeconds < 10 || cfg.VoteSeconds > 180 {
		return Config{}, fmt.Errorf("bluff: voteSeconds %d outside [10,180]", cfg.VoteSeconds)
	}
	return cfg, nil
}

// fact is one reviewed true fact from the content pack.
//
// The fields mirror the compiled bluff section. Reviewed is the gate: only a fact
// with a human review and a source may be posed, and the current snapshot carries
// none.
type fact struct {
	FallbackAnswers []string `json:"fallbackAnswers,omitempty"`
	FactID          string   `json:"factId"`
	CharacterID     string   `json:"characterId"`
	CharacterName   string   `json:"characterName,omitempty"`
	// Text is the true statement. The round blanks exactly one phrase from it, which
	// is the part the players must reconstruct with decoys.
	Text string `json:"text"`
	// Answer is the exact phrase the round removed.
	Answer string `json:"answer"`
	// Source cites where the fact comes from.
	Source string `json:"source"`
	// ReviewStatus is the human review state. Only "reviewed" is admissible.
	ReviewStatus string `json:"reviewStatus"`
}

// reviewed reports whether the fact may be posed.
func (f fact) reviewed() bool {
	return f.ReviewStatus == ReviewStatusReviewed && f.Source != "" &&
		f.Text != "" && f.Answer != ""
}

// ReviewStatusReviewed is the only admissible review state.
const ReviewStatusReviewed = "reviewed"

// FactsSection is the pack section carrying the candidate facts.
const FactsSection = "bluff.facts"

// loadReviewedFacts reads the pack's bluff facts and returns only the reviewed ones,
// together with the rejection counts so the refusal can explain itself.
func loadReviewedFacts(src modes.ContentSource) (admitted []fact, total, unreviewed, unsourced int, err error) {
	if src == nil {
		return nil, 0, 0, 0, fmt.Errorf("bluff: no content source")
	}
	raw, ok := src.Section(FactsSection)
	if !ok || len(raw) == 0 {
		// The section may also be absent entirely, which is a different report from
		// "present but unreviewed".
		return nil, 0, 0, 0, nil
	}
	records, decErr := modeutil.Records(raw, "facts", "items")
	if decErr != nil {
		return nil, 0, 0, 0, fmt.Errorf("bluff: decode section %q: %w", FactsSection, decErr)
	}
	for _, rec := range records {
		total++
		f := fact{
			FactID:          rec.Str("factId", "fact_id", "id"),
			CharacterID:     rec.Str("characterId", "character_id"),
			CharacterName:   rec.Str("characterName", "name"),
			Text:            rec.Str("text", "fact"),
			Answer:          rec.Str("answer", "phrase"),
			Source:          rec.Str("source"),
			ReviewStatus:    rec.Str("reviewStatus", "review_status"),
			FallbackAnswers: rec.StrList("fallbackAnswers"),
		}
		switch {
		case f.ReviewStatus != ReviewStatusReviewed:
			unreviewed++
		case f.Source == "":
			unsourced++
		default:
			admitted = append(admitted, f)
		}
	}
	// Deterministic order so a refusal message and a draw are stable.
	sort.Slice(admitted, func(i, j int) bool { return admitted[i].FactID < admitted[j].FactID })
	return admitted, total, unreviewed, unsourced, nil
}

// AdmissionError is the refusal Prepare returns when no reviewed fact exists.
//
// It is a distinct type so a caller can recognise the case and show the mode as
// unavailable rather than as broken, and so the message can carry the counts without
// a caller parsing prose.
type AdmissionError struct {
	// Total is how many candidate facts the pack carries.
	Total int
	// Unreviewed is how many lack a human review.
	Unreviewed int
	// Unsourced is how many are reviewed but cite no source.
	Unsourced int
	// Reason is the short machine-readable cause.
	Reason string
}

// Error implements error.
func (e *AdmissionError) Error() string {
	if e.Total == 0 {
		return "bluff: no facts are available in the loaded content pack"
	}
	return fmt.Sprintf(
		"bluff: no reviewed facts available (%d candidates, %d unreviewed, %d without a source); "+
			"a human semantic review and a source citation are required before play",
		e.Total, e.Unreviewed, e.Unsourced)
}

// ReasonCode returns the machine-readable cause.
func (e *AdmissionError) ReasonCode() string {
	if e.Reason != "" {
		return e.Reason
	}
	if e.Total == 0 {
		return "no_facts"
	}
	return "no_reviewed_facts"
}

// decoy is one submitted fake answer.
type decoy struct {
	DecoyID string `json:"decoyId"`
	// Normalized is the comparison form, used for de-duplication.
	Normalized string `json:"-"`
	// Text is the display form.
	Text string `json:"text"`
	// AuthorIDs lists every unit that submitted this text. A merged decoy has more
	// than one, which is how co-authorship is recorded .
	AuthorIDs []string `json:"authorIds"`
}

// option is one ballot entry.
type option struct {
	OptionID string `json:"optionId"`
	Text     string `json:"text"`
	// IsTruth is never published before the reveal.
	IsTruth bool `json:"-"`
	// AuthorIDs is empty for the truth option.
	AuthorIDs []string `json:"authorIds,omitempty"`
}

// rosterEntry is one submitting unit.
type rosterEntry struct {
	SubjectID   string            `json:"subjectId"`
	SubjectKind modes.SubjectKind `json:"subjectKind"`
}

// publicState is the non-secret round state.
//
// It carries the blanked fact and the anonymous ballot. It never carries which option
// is the truth, nor which unit authored which decoy: both live in the server-only
// state.
type publicState struct {
	Subphase     string        `json:"subphase,omitempty"`
	ModeID       string        `json:"modeId"`
	RulesVersion string        `json:"rulesVersion"`
	Config       Config        `json:"config"`
	Subjects     []rosterEntry `json:"subjects"`
	// Prompt is the fact with the answer phrase blanked.
	Prompt string `json:"prompt"`
	// PromptSource is the fact's citation, shown only at the reveal.
	PromptSource string `json:"promptSource,omitempty"`
	// Options is the anonymous ballot, in a stable seeded order.
	Options []option `json:"options"`
	// DecoyCount and VoteCount close the two phases.
	DecoyCount int `json:"decoyCount"`
	VoteCount  int `json:"voteCount"`
	// OptionsGrouped reports that more than MaxOptions candidates existed, so the
	// ballot must be split rather than crammed onto one screen (: 选项最多 9 个；
	// 超出分组).
	OptionsGrouped bool `json:"optionsGrouped"`
	// TextRules is the player-facing statement of how a fake answer is counted and what the
	// client must do before submitting.
	//
	// It is here rather than only in the error message because the NFC requirement is a
	// capability boundary of the rule layer, not a surprise a player should discover by being
	// rejected: the standard library has no Unicode normaliser and this package may not add a
	// module dependency, so the client is responsible for calling normalize('NFC').
	TextRules string `json:"textRules"`
	Scoring   string `json:"scoring"`
}

// privateState is the server-only round state.
type privateState struct {
	FallbackAnswers []string `json:"fallbackAnswers,omitempty"`
	ModeID          string   `json:"modeId"`
	// Truth is the exact answer phrase.
	Truth string `json:"truth"`
	// TruthOptionID is the ballot entry that carries the truth.
	TruthOptionID string `json:"truthOptionId"`
	// Decoys is the merged decoy list.
	Decoys []decoy `json:"decoys"`
	// AuthorOf maps decoy id → author ids. It duplicates the decoy list for direct
	// lookup during scoring.
	AuthorOf map[string][]string `json:"authorOf"`
	// FactID records which fact was posed, so the evidence can name it.
	FactID string `json:"factId"`
	// FactSource is the citation.
	FactSource string `json:"factSource"`
}

// subjectState is one unit's own state.
type subjectState struct {
	// DecoyText is the submitted fake answer, in display form.
	DecoyText string `json:"decoyText,omitempty"`
	// DecoyID is the id of the (possibly merged) decoy this unit authored.
	DecoyID string `json:"decoyId,omitempty"`
	// DecoyCount is how many accepted submissions this unit made.
	DecoyCount int `json:"decoyCount"`
	// Voted reports whether a ballot was cast.
	Voted bool `json:"voted"`
	// VoteOptionID is the ballot entry chosen.
	VoteOptionID string `json:"voteOptionId,omitempty"`
}

// Mode implements modes.Mode for G08.
type Mode struct{}

// New returns the bluff mode.
func New() *Mode { return &Mode{} }

// ID implements modes.Mode.
func (*Mode) ID() string { return ModeID }

// RulesVersion implements modes.Mode.
func (*Mode) RulesVersion() string { return RulesVersion }

// Title implements modes.Mode.
func (*Mode) Title() string { return "假档案发布会" }

// Summary implements modes.Mode.
func (*Mode) Summary() string {
	return "围绕一条已审核的真事实各写一个假答案，匿名混在一起投票找出真相，并靠骗到别人加分。"
}

// InformationStyles reports which information forms this mode implements.
func (*Mode) InformationStyles() []string {
	return []string{"closed"}
}

// RoundWindow implements modes.Mode.
//
// 写诱饵 + 投票两段。返回 0 表示本模式没有自己的窗口意见，交给房间层默认值。
func (*Mode) RoundWindow(raw json.RawMessage) time.Duration {
	cfg, err := decodeConfig(raw)
	if err != nil {
		return 0
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
// bluff needs reviewed facts with sources: the semantic review cannot be done by the
// program, so the requirement declares the field and the admission gate refuses the
// round when the pack carries none .
//
// MinEntities is the smallest viable CONTENT POOL, counted in facts. One reviewed fact is
// enough to pose one question, so the floor is 1. It used to be MinSubjects, which is the
// smallest TABLE size — a different question, answered by the room layer's
// not-enough-participants check. Conflating the two made the pool floor rise with the table
// size, so a pack with two good facts was refused for a three-player room.
func (*Mode) Requirement() modes.ContentRequirement {
	return modes.ContentRequirement{
		Fields:      []string{"bluff.fact.text", "bluff.fact.answer", "bluff.fact.source"},
		MinEntities: 1,
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
// This is the admission gate. It refuses a round unless the pack carries a reviewed
// fact with a source, and it reports the counts so the lobby can say why. The engine
// below is complete, but no unreviewed fact is ever posed.
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
			return modes.Prepared{}, fmt.Errorf("bluff: duplicate subject %q", subj.ID)
		}
		seen[subj.ID] = struct{}{}
		roster = append(roster, rosterEntry{SubjectID: subj.ID, SubjectKind: subj.Kind})
	}
	if len(roster) < MinSubjects || len(roster) > MaxSubjects {
		return modes.Prepared{}, fmt.Errorf(
			"bluff: %d units outside [%d,%d]", len(roster), MinSubjects, MaxSubjects)
	}

	admitted, total, unreviewed, unsourced, err := loadReviewedFacts(in.Content)
	if err != nil {
		return modes.Prepared{}, err
	}
	if len(admitted) == 0 {
		// The refusal. It names the counts and the reason, and it never falls back to
		// an unreviewed fact.
		return modes.Prepared{}, &AdmissionError{
			Total:      total,
			Unreviewed: unreviewed,
			Unsourced:  unsourced,
			Reason:     "no_reviewed_facts",
		}
	}

	// Pick the fact: the configured id when given, otherwise the first admitted one,
	// which is a deterministic choice because the list is sorted by id.
	var chosen fact
	if cfg.TruthID != "" {
		found := false
		for _, f := range admitted {
			if f.FactID == cfg.TruthID {
				chosen, found = f, true
				break
			}
		}
		if !found {
			return modes.Prepared{}, fmt.Errorf(
				"bluff: the configured fact %q is not an admitted reviewed fact", cfg.TruthID)
		}
	} else {
		idx := modeutil.PickIndex(in.Scope.Seed, "bluff.fact", len(admitted))
		chosen = admitted[idx]
	}
	// The blanking must leave a prompt with the phrase actually removed, otherwise the
	// players would be asked to fill in something already visible.
	prompt, err := blankPhrase(chosen.Text, chosen.Answer)
	if err != nil {
		return modes.Prepared{}, fmt.Errorf("bluff: fact %q: %w", chosen.FactID, err)
	}

	pub := publicState{
		Subphase:     "writing",
		ModeID:       ModeID,
		RulesVersion: RulesVersion,
		Config:       cfg,
		Subjects:     roster,
		Prompt:       prompt,
		Options:      []option{},
		TextRules: "假答案需 " + fmt.Sprintf("%d", MinDecoyCodePoints) + "–" +
			fmt.Sprintf("%d", MaxDecoyCodePoints) +
			" 个字（一个汉字、一个字母、一个表情都算 1 个字）。" +
			"多出来的部分不会被悄悄截掉——超了会直接告诉你还差几个字。",
		Scoring: "识别真相 " + fmt.Sprintf("%d", TruthPoints) + " 分；" +
			"欺骗奖励最多 " + fmt.Sprintf("%d", DeceptionPoints) +
			" 分；假答案仅本局有效，不写入事实库",
	}
	priv := privateState{
		FallbackAnswers: chosen.FallbackAnswers,
		ModeID:          ModeID,
		Truth:           chosen.Answer,
		TruthOptionID:   truthOptionID(in.Scope.Seed),
		Decoys:          []decoy{},
		AuthorOf:        map[string][]string{},
		FactID:          chosen.FactID,
		FactSource:      chosen.Source,
	}
	return modes.Prepared{
		Public:        modeutil.Marshal(pub),
		Private:       modeutil.Marshal(priv),
		QuestionCount: 1,
	}, nil
}

// truthOptionID builds the ballot entry id that carries the truth.
//
// It is a seeded digest rather than a fixed name: a fixed id such as "truth" would be
// readable from the wire and hand the answer over.
func truthOptionID(seed string) string {
	sum := sha256.Sum256([]byte(seed + "\x00bluff.truth"))
	return "bo-" + hex.EncodeToString(sum[:6])
}

// decoyIDFor builds an opaque decoy id from the normalised text, so two units
// submitting the same text merge into one decoy with two authors.
func decoyIDFor(normalized string) string {
	sum := sha256.Sum256([]byte("bluff.decoy\x00" + normalized))
	return "bd-" + hex.EncodeToString(sum[:6])
}

// optionIDFor builds an opaque ballot entry id from a decoy id.
func optionIDFor(decoyID string) string {
	sum := sha256.Sum256([]byte("bluff.option\x00" + decoyID))
	return "bo-" + hex.EncodeToString(sum[:6])
}

// blankPhrase removes the answer phrase from the fact text.
//
// The comparison is case-insensitive and whitespace-tolerant but exact on the
// characters: comparison uses NFC, whitespace,
// full-width forms) rather than a fuzzy match, and inventing a semantic match here
// would be exactly the review the program cannot perform.
func blankPhrase(text, answer string) (string, error) {
	answer = strings.TrimSpace(answer)
	if answer == "" {
		return "", fmt.Errorf("the answer phrase is empty")
	}
	text = strings.TrimSpace(text)
	if text == "" {
		return "", fmt.Errorf("the fact text is empty")
	}
	idx := strings.Index(strings.ToLower(text), strings.ToLower(answer))
	if idx < 0 {
		return "", fmt.Errorf("the fact text does not contain the answer phrase %q", answer)
	}
	blanked := text[:idx] + "____" + text[idx+len(answer):]
	if blanked == text {
		return "", fmt.Errorf("blanking did not change the text")
	}
	return blanked, nil
}

// normalizeDecoy is the comparison form of a fake answer.
//
// Equality uses server-side NFC, collapsed whitespace, full-width ASCII and case.
func normalizeDecoy(s string) string {
	s = norm.NFC.String(strings.TrimSpace(s))
	var b strings.Builder
	b.Grow(len(s))
	prevSpace := false
	for _, r := range s {
		// Fold full-width ASCII (U+FF01–U+FF5E) to half-width, which is the
		// full-width/half-width rule the plan names.
		if r >= 0xFF01 && r <= 0xFF5E {
			r = r - 0xFEE0
		}
		if unicode.IsSpace(r) {
			if prevSpace {
				continue
			}
			prevSpace = true
			b.WriteRune(' ')
			continue
		}
		prevSpace = false
		b.WriteRune(r)
	}
	return strings.ToLower(b.String())
}

// countCodePoints counts Unicode code points.
func countCodePoints(s string) int {
	n := 0
	for range s {
		n++
	}
	return n
}

// decoyPayload is the body of ActionKindDecoy, matching the contract's BluffDecoy
// schema: {kind, text} with additionalProperties false.
type decoyPayload struct {
	Kind string `json:"kind"`
	Text string `json:"text"`
}

// votePayload is the body of ActionKindVote, matching the contract's BluffVote
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
			return publicState{}, fmt.Errorf("bluff: decode public state: %w", err)
		}
	}
	return pub, nil
}

// decodePrivate unmarshals the server-only state.
func decodePrivate(raw json.RawMessage) (privateState, error) {
	var priv privateState
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &priv); err != nil {
			return privateState{}, fmt.Errorf("bluff: decode private state: %w", err)
		}
	}
	return priv, nil
}

// decodeState unmarshals a unit state.
func decodeState(raw json.RawMessage) (subjectState, error) {
	var st subjectState
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &st); err != nil {
			return subjectState{}, fmt.Errorf("bluff: decode unit state: %w", err)
		}
	}
	return st, nil
}

// Apply implements modes.Mode.
func (*Mode) Apply(in modes.ApplyInput) (modes.Decision, error) {
	switch in.Action.Kind {
	case ActionKindDecoy:
		return applyDecoy(in)
	case ActionKindVote:
		return applyVote(in)
	}
	return modes.Rejected(CodeUnknownAction,
		fmt.Sprintf("bluff: unsupported action %q", in.Action.Kind)), nil
}

// applyDecoy validates and records a fake answer.
//
// A submission matching the truth earns identification points and is excluded
// from the author's decoys.
func applyDecoy(in modes.ApplyInput) (modes.Decision, error) {
	if err := in.Action.Validate(); err != nil {
		return modes.Rejected(CodeInvalidPayload, err.Error()), nil
	}
	var payload decoyPayload
	if err := json.Unmarshal(in.Action.Payload, &payload); err != nil {
		return modes.Rejected(CodeInvalidPayload, "bluff: payload must carry text"), nil
	}
	if payload.Kind != "" && payload.Kind != ActionKindDecoy {
		return modes.Rejected(CodeInvalidPayload, fmt.Sprintf(
			"bluff: payload kind %q does not match action %q",
			payload.Kind, ActionKindDecoy)), nil
	}

	pub, err := decodePublic(in.Public)
	if err != nil {
		return modes.Decision{}, err
	}
	priv, err := decodePrivate(in.Private)
	if err != nil {
		return modes.Decision{}, err
	}
	if !onRoster(pub.Subjects, in.Subject.ID) {
		return modes.Rejected(CodeNotOnRoster, fmt.Sprintf(
			"bluff: subject %q is not on this roster", in.Subject.ID)), nil
	}
	// Writing closes before moderation, not when somebody casts the first vote.
	if pub.Subphase != "writing" || pub.VoteCount > 0 {
		return modes.Rejected(CodeDecoyClosed,
			"bluff: voting has started, so a fake answer can no longer be submitted"), nil
	}
	st, err := decodeState(in.State)
	if err != nil {
		return modes.Decision{}, err
	}
	// A unit submits one fake answer. The plan allows 2–40 code points per answer, not
	// several answers per unit, and the ballot would otherwise be dominated by whoever
	// types fastest.
	if st.DecoyCount > 0 {
		return modes.Rejected(CodeTooManyDecoys,
			"bluff: this unit already submitted its fake answer"), nil
	}

	text := norm.NFC.String(strings.TrimSpace(payload.Text))
	n := countCodePoints(text)
	if n < MinDecoyCodePoints {
		return modes.Rejected(CodeDecoyTooShort, fmt.Sprintf(
			"bluff: the fake answer is %d code points; the minimum is %d",
			n, MinDecoyCodePoints)), nil
	}
	if n > MaxDecoyCodePoints {
		return modes.Rejected(CodeDecoyTooLong, fmt.Sprintf(
			"bluff: the fake answer is %d code points; the maximum is %d",
			n, MaxDecoyCodePoints)), nil
	}

	normalized := normalizeDecoy(text)
	// The truth submitted as a decoy identifies the truth instead.
	if normalizeDecoy(priv.Truth) == normalized {
		st.DecoyText = text
		st.DecoyCount++
		feedback := modeutil.Marshal(struct {
			// IdentifiedTruth tells the unit their answer matched the truth. This is
			// not a leak: they already knew what they typed, and the plan scores it.
			IdentifiedTruth bool   `json:"identifiedTruth"`
			Note            string `json:"note"`
		}{
			IdentifiedTruth: true,
			Note:            "你提交的内容与真答案一致，将按识别真相计分，不作为自己的假项",
		})
		return modes.Accepted(feedback, modeutil.Marshal(st)), nil
	}

	decoyID := decoyIDFor(normalized)
	st.DecoyID = decoyID
	st.DecoyText = text
	st.DecoyCount++

	feedback := modeutil.Marshal(struct {
		DecoyID         string `json:"decoyId"`
		IdentifiedTruth bool   `json:"identifiedTruth"`
		// Merged reports that another unit already submitted the same text, so the
		// authorship is shared.
		Merged bool   `json:"merged"`
		Note   string `json:"note"`
	}{
		DecoyID:         decoyID,
		IdentifiedTruth: false,
		Merged:          decoyExists(priv, decoyID),
		Note:            "假答案已记录；揭晓前不会显示作者",
	})
	return modes.Accepted(feedback, modeutil.Marshal(st)), nil
}

// decoyExists reports whether a decoy id is already in the merged list.
func decoyExists(priv privateState, decoyID string) bool {
	for _, d := range priv.Decoys {
		if d.DecoyID == decoyID {
			return true
		}
	}
	return false
}

// applyVote validates and records a ballot.
func applyVote(in modes.ApplyInput) (modes.Decision, error) {
	if err := in.Action.Validate(); err != nil {
		return modes.Rejected(CodeInvalidPayload, err.Error()), nil
	}
	var payload votePayload
	if err := json.Unmarshal(in.Action.Payload, &payload); err != nil {
		return modes.Rejected(CodeInvalidPayload, "bluff: payload must carry optionId"), nil
	}
	if payload.Kind != "" && payload.Kind != ActionKindVote {
		return modes.Rejected(CodeInvalidPayload, fmt.Sprintf(
			"bluff: payload kind %q does not match action %q",
			payload.Kind, ActionKindVote)), nil
	}
	if payload.OptionID == "" {
		return modes.Rejected(CodeInvalidPayload, "bluff: empty optionId"), nil
	}

	pub, err := decodePublic(in.Public)
	if err != nil {
		return modes.Decision{}, err
	}
	if pub.Subphase != "voting" {
		return modes.Rejected(CodeVoteClosed, "bluff: voting has not started"), nil
	}
	// The ballot carries every option with its author list, so a ballot can be
	// validated without reading the round secrets.
	if !onRoster(pub.Subjects, in.Subject.ID) {
		return modes.Rejected(CodeNotOnRoster, fmt.Sprintf(
			"bluff: subject %q is not on this roster", in.Subject.ID)), nil
	}
	st, err := decodeState(in.State)
	if err != nil {
		return modes.Decision{}, err
	}
	priv, err := decodePrivate(in.Private)
	if err != nil {
		return modes.Decision{}, err
	}
	if st.DecoyCount > 0 && normalizeDecoy(st.DecoyText) == normalizeDecoy(priv.Truth) {
		return modes.Rejected("truth_already_identified", "bluff: truth identification is already recorded"), nil
	}
	// One ballot per unit.
	if st.Voted {
		return modes.Rejected(CodeAlreadyVoted,
			"bluff: this unit has already voted"), nil
	}

	// The ballot entry must exist.
	var chosen *option
	for i := range pub.Options {
		if pub.Options[i].OptionID == payload.OptionID {
			chosen = &pub.Options[i]
			break
		}
	}
	if chosen == nil {
		return modes.Rejected(CodeUnknownOption, fmt.Sprintf(
			"bluff: option %q is not on this ballot", payload.OptionID)), nil
	}
	// 不能投自己: a unit may not vote for a decoy it authored .
	for _, authorID := range chosen.AuthorIDs {
		if authorID == in.Subject.ID {
			return modes.Rejected(CodeOwnDecoy,
				"bluff: you cannot vote for your own fake answer"), nil
		}
	}

	st.Voted = true
	st.VoteOptionID = payload.OptionID

	feedback := modeutil.Marshal(struct {
		OptionID string `json:"optionId"`
		Note     string `json:"note"`
	}{
		OptionID: payload.OptionID,
		Note:     "选票已记录，不能修改；结果在投票结束后公布",
	})
	return modes.Accepted(feedback, modeutil.Marshal(st)), nil
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

// CommitShared implements modeutil.SharedStateMode.
//
// It folds an accepted action into the round-level ballot: a decoy is merged into the
// list (recording a co-author when the text already exists), and a ballot increments
// the vote count.
func (*Mode) CommitShared(in modes.ApplyInput, dec modes.Decision) (json.RawMessage, json.RawMessage, error) {
	if !dec.Accepted {
		return nil, nil, fmt.Errorf("bluff: cannot commit a rejected action")
	}
	pub, err := decodePublic(in.Public)
	if err != nil {
		return nil, nil, err
	}
	priv, err := decodePrivate(in.Private)
	if err != nil {
		return nil, nil, err
	}
	st, err := decodeState(dec.State)
	if err != nil {
		return nil, nil, err
	}

	switch in.Action.Kind {
	case ActionKindDecoy:
		pub.DecoyCount++
		if st.DecoyID == "" {
			// The submission matched the truth: it earns identification and is not
			// added to the ballot as a decoy .
			break
		}
		merged := false
		for i := range priv.Decoys {
			if priv.Decoys[i].DecoyID != st.DecoyID {
				continue
			}
			// Merge: record the co-author once, in a stable order.
			if !containsString(priv.Decoys[i].AuthorIDs, in.Subject.ID) {
				priv.Decoys[i].AuthorIDs = append(priv.Decoys[i].AuthorIDs, in.Subject.ID)
				sort.Strings(priv.Decoys[i].AuthorIDs)
			}
			priv.AuthorOf[st.DecoyID] = priv.Decoys[i].AuthorIDs
			merged = true
			break
		}
		if !merged {
			priv.Decoys = append(priv.Decoys, decoy{
				DecoyID:    st.DecoyID,
				Normalized: normalizeDecoy(st.DecoyText),
				Text:       st.DecoyText,
				AuthorIDs:  []string{in.Subject.ID},
			})
			priv.AuthorOf[st.DecoyID] = []string{in.Subject.ID}
		}
		sort.Slice(priv.Decoys, func(i, j int) bool {
			return priv.Decoys[i].DecoyID < priv.Decoys[j].DecoyID
		})
		// Rebuild the ballot so it stays a pure function of the merged decoys.
		pub.Options = buildOptions(priv, in.Scope.Seed)

	case ActionKindVote:
		pub.VoteCount++

	default:
		return nil, nil, fmt.Errorf("bluff: cannot commit action %q", in.Action.Kind)
	}
	return modeutil.Marshal(pub), modeutil.Marshal(priv), nil
}

// buildOptions assembles the anonymous ballot from the merged decoys plus the truth.
//
// The order is a seeded shuffle of the decoy ids, which is stable for a round and
// carries no information: a ballot ordered by submission time would let a player infer
// who submitted what, and an ordered-by-id ballot would be reproducible from the
// decoy texts alone.
func buildOptions(priv privateState, seed string) []option {
	out := make([]option, 0, len(priv.Decoys)+1)
	for _, d := range priv.Decoys {
		out = append(out, option{
			OptionID:  optionIDFor(d.DecoyID),
			Text:      d.Text,
			AuthorIDs: append([]string{}, d.AuthorIDs...),
		})
	}
	// The truth is always on the ballot, and it is indistinguishable in shape.
	out = append(out, option{
		OptionID: priv.TruthOptionID,
		Text:     priv.Truth,
		IsTruth:  true,
	})
	stream := modeutil.NewStream(seed + "\x00bluff.ballot")
	stream.Shuffle(len(out), func(i, j int) { out[i], out[j] = out[j], out[i] })
	return out
}

// containsString reports whether list holds value.
func containsString(list []string, value string) bool {
	for _, item := range list {
		if item == value {
			return true
		}
	}
	return false
}

// optionView is one ballot entry as a viewer sees it.
type optionView struct {
	OptionID string `json:"optionId"`
	Text     string `json:"text"`
	// SelfAuthored tells the caller whether they wrote this decoy, so the client can
	// disable it without the payload listing authors.
	SelfAuthored bool     `json:"selfAuthored,omitempty"`
	AuthorIDs    []string `json:"authorIds,omitempty"`
}

// projection is the viewer-specific round state.
type projection struct {
	Subphase      string       `json:"subphase,omitempty"`
	ReviewOptions []optionView `json:"reviewOptions,omitempty"`
	ModeID        string       `json:"modeId"`
	RulesVersion  string       `json:"rulesVersion"`
	Phase         modes.Phase  `json:"phase"`
	Config        Config       `json:"config"`
	Prompt        string       `json:"prompt"`
	Scoring       string       `json:"scoring"`
	// Options is the anonymous ballot. It never carries an author list.
	Options []optionView `json:"options"`
	// OptionsGrouped reports that the ballot exceeds one screen.
	OptionsGrouped bool `json:"optionsGrouped"`
	// TextRules repeats the player-facing writing rules.
	TextRules string `json:"textRules"`
	// Self is the caller's own state.
	Self *selfView `json:"self,omitempty"`
	// Progress is the coarse per-unit progress.
	Progress []progressView `json:"progress"`
	// Reveal is published only once the round is terminal.
	Reveal *revealView `json:"reveal,omitempty"`
}

// selfView is the caller's own state.
type selfView struct {
	IdentifiedTruth bool   `json:"identifiedTruth"`
	DecoyRemoved    bool   `json:"decoyRemoved"`
	SubjectID       string `json:"subjectId"`
	DecoyText       string `json:"decoyText,omitempty"`
	DecoyCount      int    `json:"decoyCount"`
	Voted           bool   `json:"voted"`
	VoteOptionID    string `json:"voteOptionId,omitempty"`
}

// progressView is another unit's coarse progress. It reports whether they submitted
// and voted, never what they wrote or chose.
type progressView struct {
	SubjectID string `json:"subjectId"`
	Submitted bool   `json:"submitted"`
	Voted     bool   `json:"voted"`
}

// revealView is the answer, published only at the end.
type revealView struct {
	VoteCounts map[string]int   `json:"voteCounts"`
	Breakdown  []scoreBreakdown `json:"breakdown"`
	// Truth is the exact answer phrase.
	Truth string `json:"truth"`
	// TruthOptionID names the ballot entry that carried it.
	TruthOptionID string `json:"truthOptionId"`
	// Source is the fact's citation .
	Source string `json:"source"`
	// Decoys is the decoy list with its authors, which is what makes the deception
	// bonus explainable.
	Decoys []decoy `json:"decoys"`
	// FactID names the fact.
	FactID string `json:"factId"`
}

// Project implements modes.Mode.
//
// The visibility rules:
//
// - The ballot is anonymous: no option carries an author list, and the only
// self-related field is a boolean the client uses to disable the caller's own
// decoy.
// - Which option is the truth is withheld from every viewer until the round is
// terminal.
// - The citation and the decoy authorship appear only in the reveal.
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
		Subphase:       pub.Subphase,
		ModeID:         ModeID,
		RulesVersion:   RulesVersion,
		Phase:          in.Phase,
		Config:         pub.Config,
		Prompt:         pub.Prompt,
		Scoring:        pub.Scoring,
		OptionsGrouped: pub.OptionsGrouped || len(pub.Options) > MaxOptions,
		TextRules:      pub.TextRules,
		Options:        make([]optionView, 0, len(pub.Options)),
		Progress:       make([]progressView, 0, len(pub.Subjects)),
	}

	for _, opt := range pub.Options {
		view := optionView{OptionID: opt.OptionID, Text: opt.Text}
		if in.Phase == modes.PhaseRevealed || in.Phase == modes.PhaseSettled {
			view.AuthorIDs = append([]string(nil), opt.AuthorIDs...)
		}
		for _, authorID := range opt.AuthorIDs {
			if authorID == in.Viewer.Subject.ID {
				view.SelfAuthored = true
				break
			}
		}
		if pub.Subphase == "voting" || (in.Phase == modes.PhaseRevealed || in.Phase == modes.PhaseSettled) {
			out.Options = append(out.Options, view)
		}
		if pub.Subphase == "review" && (in.Viewer.CanManageRound || in.Viewer.SeesSecrets()) {
			out.ReviewOptions = append(out.ReviewOptions, optionView{OptionID: opt.OptionID, Text: opt.Text})
		}
	}

	for _, entry := range pub.Subjects {
		st, err := decodeState(in.States[entry.SubjectID])
		if err != nil {
			return nil, fmt.Errorf("bluff: decode state of %q: %w", entry.SubjectID, err)
		}
		out.Progress = append(out.Progress, progressView{
			SubjectID: entry.SubjectID,
			Submitted: st.DecoyCount > 0,
			Voted:     st.Voted,
		})
		if entry.SubjectID == in.Viewer.Subject.ID {
			out.Self = &selfView{
				IdentifiedTruth: st.DecoyCount > 0 && normalizeDecoy(st.DecoyText) == normalizeDecoy(priv.Truth),
				DecoyRemoved:    pub.Subphase != "writing" && st.DecoyID != "" && !decoyExists(priv, st.DecoyID),
				SubjectID:       entry.SubjectID,
				DecoyText:       st.DecoyText,
				DecoyCount:      st.DecoyCount,
				Voted:           st.Voted,
				VoteOptionID:    st.VoteOptionID,
			}
		}
	}

	if in.Phase == modes.PhaseRevealed || in.Phase == modes.PhaseSettled || in.Viewer.SeesSecrets() {
		out.Reveal = &revealView{
			Truth:         priv.Truth,
			TruthOptionID: priv.TruthOptionID,
			Source:        priv.FactSource,
			Decoys:        priv.Decoys,
			FactID:        priv.FactID,
		}
		out.Reveal.VoteCounts = map[string]int{}
		for _, raw := range in.States {
			st, err := decodeState(raw)
			if err != nil {
				return nil, err
			}
			if st.Voted {
				out.Reveal.VoteCounts[st.VoteOptionID]++
			}
		}
		scores, err := New().Settle(modes.SettleInput{Public: in.Public, Private: in.Private, States: in.States})
		if err != nil {
			return nil, err
		}
		for _, score := range scores {
			var b scoreBreakdown
			if err := json.Unmarshal(score.Explanation, &b); err != nil {
				return nil, err
			}
			b.SubjectID = score.Subject.ID
			out.Reveal.Breakdown = append(out.Reveal.Breakdown, b)
		}

	}
	return modeutil.Marshal(out), nil
}

// Settle implements modes.Mode.
//
// , the documented scoring:
//
// - Identifying the truth earns TruthPoints (70).
// - A unit that submitted the truth earns the identification points but no
// deception bonus, and its own submission is not a decoy.
// - A unit that did not vote earns no identification points, but a compliant decoy
// still earns its deception bonus.
// - The deception bonus is
// floor(DeceptionPoints × other valid units that voted for this unit's decoy /
// valid units that could have voted for it), with 0 when the denominator is 0.
// - A merged decoy's bonus is split evenly, floored, among its authors.
//
// "Valid unit" means a unit that is on the roster and still participating; the
// denominator excludes the author, who cannot vote for their own decoy.
func (*Mode) Settle(in modes.SettleInput) ([]modes.Score, error) {
	pub, err := decodePublic(in.Public)
	if err != nil {
		return nil, err
	}
	priv, err := decodePrivate(in.Private)
	if err != nil {
		return nil, err
	}

	// Decode every unit state once.
	states := make(map[string]subjectState, len(pub.Subjects))
	for _, entry := range pub.Subjects {
		st, err := decodeState(in.States[entry.SubjectID])
		if err != nil {
			return nil, fmt.Errorf("bluff: decode state of %q: %w", entry.SubjectID, err)
		}
		states[entry.SubjectID] = st
	}

	// Tally the ballots per option.
	votesFor := make(map[string]int, len(pub.Options))
	for _, st := range states {
		if !st.Voted || st.VoteOptionID == "" {
			continue
		}
		votesFor[st.VoteOptionID]++
	}

	// The identification points: a unit earns them by voting for the truth option, or
	// by having submitted the truth as its own answer.
	identified := make(map[string]bool, len(pub.Subjects))
	for subjectID, st := range states {
		if st.Voted && st.VoteOptionID == priv.TruthOptionID {
			identified[subjectID] = true
			continue
		}
		// A unit that typed the truth is credited even without voting, because the
		// plan says such a submission scores as identifying the truth.
		if st.DecoyCount > 0 && normalizeDecoy(st.DecoyText) == normalizeDecoy(priv.Truth) {
			identified[subjectID] = true
		}
	}

	// The deception bonus, per author.
	deceptionUnits := make(map[string]int, len(pub.Subjects))
	for _, d := range priv.Decoys {
		optionID := optionIDFor(d.DecoyID)
		// The denominator: every valid unit except the authors, because an author
		// cannot vote for their own decoy.
		eligible := 0
		for _, entry := range pub.Subjects {
			if containsString(d.AuthorIDs, entry.SubjectID) {
				continue
			}
			st := states[entry.SubjectID]
			if st.DecoyCount > 0 && normalizeDecoy(st.DecoyText) == normalizeDecoy(priv.Truth) {
				continue
			}
			eligible++
		}
		if eligible == 0 {
			// The documented empty-denominator case scores 0 .
			continue
		}
		// The numerator: valid units that are not authors and voted for this decoy.
		voters := 0
		for _, entry := range pub.Subjects {
			if containsString(d.AuthorIDs, entry.SubjectID) {
				continue
			}
			st := states[entry.SubjectID]
			if st.Voted && st.VoteOptionID == optionID {
				voters++
			}
		}
		if voters == 0 {
			continue
		}
		// floor(30 × voters / eligible), then split among the authors.
		total := DeceptionPoints * voters / eligible
		authors := len(d.AuthorIDs)
		if authors == 0 {
			continue
		}
		share := total / authors
		for _, authorID := range d.AuthorIDs {
			deceptionUnits[authorID] += share
		}
	}

	scores := make([]modes.Score, 0, len(pub.Subjects))
	for _, entry := range pub.Subjects {
		st := states[entry.SubjectID]
		truthPoints := 0
		if identified[entry.SubjectID] {
			truthPoints = TruthPoints
		}
		// A unit that submitted the truth does not also earn a deception bonus
		// .
		deception := deceptionUnits[entry.SubjectID]
		if st.DecoyCount > 0 && normalizeDecoy(st.DecoyText) == normalizeDecoy(priv.Truth) {
			deception = 0
		}
		units := (truthPoints + deception) * modeutil.UnitsPerPoint
		maxUnits := modeutil.Units(TruthPoints + DeceptionPoints)
		explanation := modeutil.Marshal(struct {
			TruthPoints     int    `json:"truthPoints"`
			DeceptionPoints int    `json:"deceptionPoints"`
			IdentifiedTruth bool   `json:"identifiedTruth"`
			Voted           bool   `json:"voted"`
			Submitted       bool   `json:"submitted"`
			DecoyID         string `json:"decoyId,omitempty"`
			Formula         string `json:"formula"`
			Note            string `json:"note"`
		}{
			TruthPoints:     truthPoints,
			DeceptionPoints: deception,
			IdentifiedTruth: identified[entry.SubjectID],
			Voted:           st.Voted,
			Submitted:       st.DecoyCount > 0,
			DecoyID:         st.DecoyID,
			Formula: "识别真相 70；欺骗 floor(30*投中自己假项的其他有效单位数/可投该项的其他有效单位总数)，" +
				"合并作品在共同作者间均分向下取整",
			Note: "假答案仅本局有效，不写入事实库",
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
