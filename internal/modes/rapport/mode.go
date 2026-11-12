// Package rapport implements G11 默契连线 .
//
// Two partners (or two balanced small teams) play five questions of twenty
// seconds each. On every question one designated chooser picks a preference from
// four fixed options; everybody else predicts that choice. The chooser rotates so
// each participant chooses at least once.
//
// Nothing is revealed until the question is locked: a prediction is invisible to
// the chooser and to the other predictors, and the chooser's own pick is
// invisible to the predictors. That is the whole game, so the projection enforces
// it structurally rather than by hiding fields in the browser.
//
// Scoring: every correct prediction is one hit point. The team score is
//
//	floor(100 × hits / totalPredictionSlots)
//
// where totalPredictionSlots is announced when the round starts and is the fixed
// denominator — a missing prediction still occupies its slot, so not submitting
// can only lower the score, never raise it.
//
// The questions describe player preferences. No official popularity figure, no
// character personality claim and no external poll is involved, and the option set
// is authored by the round, not scraped (: 题目描述玩家偏好，不捏造官方人格/
// 人气答案).
package rapport

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"time"

	"github.com/bhcn/umaguessinggame/internal/modes"
	"github.com/bhcn/umaguessinggame/internal/modes/modeutil"
)

// ModeID is the stable mode identifier (contract ModeId enum).
const ModeID = "rapport"

// RulesVersion is the rule revision this implementation satisfies.
const RulesVersion = "2.0.0-draft.1"

// Action kinds.
//
// The value is the contract discriminator (ModeAction → RapportAnswer), not an
// internal name .
const (
	// ActionKindAnswer submits either the chooser's preference or a predictor's
	// guess, depending on the caller's role in the current question.
	ActionKindAnswer = "rapport.answer"
)

// ContractKinds returns the ModeAction discriminators this mode accepts.
func (*Mode) ContractKinds() []string { return []string{ActionKindAnswer} }

// Rejection codes.
const (
	CodeInvalidPayload  = "invalid_payload"
	CodeUnknownAction   = "unknown_action"
	CodeUnknownOption   = "unknown_option"
	CodeQuestionClosed  = "question_closed"
	CodeRoundFinished   = "round_finished"
	CodeDuplicateAnswer = "duplicate_answer"
	CodeNotOnRoster     = "not_on_roster"
	// CodeCannotPredictOwnChoice is returned when the chooser tries to submit a
	// prediction slot for their own question. The chooser answers instead.
	CodeCannotPredictOwnChoice = "cannot_predict_own_choice"
)

// Documented parameters .
const (
	// DefaultQuestions is the number of questions in one round.
	DefaultQuestions = 5
	// QuestionSeconds is the answering window per question.
	QuestionSeconds = 20
	// OptionCount is the fixed number of options per question.
	OptionCount = 4
	// MinSubjects and MaxSubjects bound the participant count. The documented
	// form is a two-person partnership; a balanced small team is allowed because
	// says 两人搭档或均衡小队.
	MinSubjects = 2
	MaxSubjects = 8
)

// Config is the round configuration.
type Config struct {
	SimultaneousTurns bool `json:"simultaneousTurns,omitempty"`
	// Questions is the number of questions. Defaults to 5 and is capped by the
	// authored question bank.
	Questions int `json:"questions"`
	// QuestionSeconds is the per-question window. Defaults to 20.
	QuestionSeconds int `json:"questionSeconds"`
}

// decodeConfig parses and normalises a configuration blob.
func decodeConfig(raw json.RawMessage) (Config, error) {
	cfg := Config{Questions: DefaultQuestions, QuestionSeconds: QuestionSeconds}
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &cfg); err != nil {
			return Config{}, fmt.Errorf("rapport: invalid config: %w", err)
		}
	}
	if cfg.Questions == 0 {
		cfg.Questions = DefaultQuestions
	}
	if cfg.QuestionSeconds == 0 {
		cfg.QuestionSeconds = QuestionSeconds
	}
	if cfg.Questions < 1 || cfg.Questions > 20 {
		return Config{}, fmt.Errorf("rapport: questions %d outside [1,20]", cfg.Questions)
	}
	if cfg.QuestionSeconds < 5 || cfg.QuestionSeconds > 120 {
		return Config{}, fmt.Errorf(
			"rapport: questionSeconds %d outside [5,120]", cfg.QuestionSeconds)
	}
	return cfg, nil
}

// questionBank is the authored set of preference questions.
//
// Each question asks the chooser about their OWN preference. The options are
// labelled by the round and carry no official claim, so a predictor is guessing
// their partner's taste rather than recalling a fact about the franchise.
type questionTemplate struct {
	ID     string `json:"id"`
	Prompt string `json:"prompt"`
	// Options are the four fixed choices, in a stable order.
	Options []string `json:"options"`
}

// bankQuestion is one question as the round publishes it.
type bankQuestion struct {
	QuestionID string       `json:"questionId"`
	Prompt     string       `json:"prompt"`
	Options    []bankOption `json:"options"`
	// ChooserID is the participant who picks a preference on this question.
	ChooserID string `json:"chooserId"`
	// PredictorIDs are the participants who predict the chooser's pick.
	PredictorIDs []string `json:"predictorIds"`
	// PredictionSlots is the fixed denominator contribution of this question:
	// the number of predictors. It is published so the score denominator is
	// known before play .
	PredictionSlots int `json:"predictionSlots"`
}

// bankOption is one fixed option.
type bankOption struct {
	OptionID string `json:"optionId"`
	Label    string `json:"label"`
}

// rosterEntry is one participant recorded in the public prepared state.
type rosterEntry struct {
	SubjectID   string            `json:"subjectId"`
	SubjectKind modes.SubjectKind `json:"subjectKind"`
}

// publicState is the non-secret round state.
//
// It carries the questions, the option labels, the rotation and the total slot
// count. It deliberately carries no choice and no prediction: those are the
// hidden material and live in the per-subject state until the question is locked.
type publicState struct {
	Live         *liveState     `json:"live,omitempty"`
	ModeID       string         `json:"modeId"`
	RulesVersion string         `json:"rulesVersion"`
	Config       Config         `json:"config"`
	Subjects     []rosterEntry  `json:"subjects"`
	Questions    []bankQuestion `json:"questions"`
	TotalSlots   int            `json:"totalSlots"`
	// Scoring explains the published denominator so the UI never has to
	// recompute it and never shows a different one.
	Scoring string `json:"scoring"`
}

// privateState is the server-only round state. Rapport has no answer key: the
// chooser's pick IS the answer and it is produced by a player, so there is
// nothing to hide from the server beyond the per-subject state.
type privateState struct {
	ModeID string `json:"modeId"`
	Note   string `json:"note"`
}

// subjectState is one participant's own persisted state.
type subjectState struct {
	// Answers holds this participant's own submissions, one per question.
	Answers []answerRecord `json:"answers"`
}

// answerRecord is one submission.
type answerRecord struct {
	QuestionID string `json:"questionId"`
	// OptionID is the chosen or predicted option.
	OptionID string `json:"optionId"`
	// AsChooser reports whether this participant was the chooser for this
	// question, so a single list can hold both roles without ambiguity.
	AsChooser bool `json:"asChooser"`
}

// answered reports whether this participant already submitted for question.
func (s subjectState) answered(questionID string) bool {
	for _, a := range s.Answers {
		if a.QuestionID == questionID {
			return true
		}
	}
	return false
}

// option returns this participant's submission for a question.
func (s subjectState) option(questionID string) (answerRecord, bool) {
	for _, a := range s.Answers {
		if a.QuestionID == questionID {
			return a, true
		}
	}
	return answerRecord{}, false
}

// Mode implements modes.Mode for G11.
type Mode struct{}

// New returns the rapport mode.
func New() *Mode { return &Mode{} }

// ID implements modes.Mode.
func (*Mode) ID() string { return ModeID }

// RulesVersion implements modes.Mode.
func (*Mode) RulesVersion() string { return RulesVersion }

// Title implements modes.Mode.
func (*Mode) Title() string { return "默契连线" }

// Summary implements modes.Mode.
func (*Mode) Summary() string {
	return "轮流选择自己的偏好，其余人预测：锁定后一起揭晓，命中越多默契越高。"
}

// InformationStyles reports which information forms this mode implements.
func (*Mode) InformationStyles() []string {
	return []string{"closed"}
}

// RoundWindow implements modes.Mode.
//
// 每题窗口 × 题数。返回 0 表示本模式没有自己的窗口意见，交给房间层默认值。
func (*Mode) RoundWindow(raw json.RawMessage) time.Duration {
	cfg, err := decodeConfig(raw)
	if err != nil {
		return 0
	}
	return time.Duration(cfg.QuestionSeconds) * time.Second * time.Duration(cfg.Questions)
}

// TurnWindow implements modes.Mode. This mode is not turn-based: it runs on the
// single RoundWindow deadline, so there is no per-turn clock to report.
func (*Mode) TurnWindow(raw json.RawMessage) time.Duration {
	cfg, err := decodeConfig(raw)
	if err != nil || !cfg.SimultaneousTurns {
		return 0
	}
	return time.Duration(cfg.QuestionSeconds) * time.Second
}

// MinSubjects implements modes.Mode.
func (*Mode) MinSubjects() int { return MinSubjects }

// MaxSubjects implements modes.Mode.
func (*Mode) MaxSubjects() int { return 64 }

// Requirement implements modes.Mode.
//
// rapport is a pure player-input mode: the question bank is authored in this
// package and carries no scraped fact, so it needs no content section. It
// declares the minimum entity count it needs (two participants) and no fields.
func (*Mode) Requirement() modes.ContentRequirement {
	// MinEntities is the smallest viable CONTENT POOL, expressed in questions: a round needs at
	// least DefaultQuestions (5) authored questions to run the documented number of rounds.
	// It is NOT MinSubjects: how many partners may sit down is the room layer's question.
	return modes.ContentRequirement{
		Fields:      []string{QuestionsSection},
		MinEntities: DefaultQuestions,
		NeedsAssets: false,
	}
}

// ValidateConfig implements modes.Mode.
func (*Mode) ValidateConfig(cfg json.RawMessage) error {
	_, err := decodeConfig(cfg)
	return err
}

// loadQuestions returns the question templates, preferring a pack section when
// one is present so a later content build can author them, and falling back to
// the compiled-in bank otherwise.
// QuestionsSection is the pack section carrying the authored question bank.
const QuestionsSection = "rapport.questions"

// Provenance kinds accepted in the authored questions.
const (
	ProvenanceAuthored = "authored-design-value"
	ProvenanceDerived  = "derived-from-doc"
	ProvenanceOfficial = "official-data"
)

// questionProvenance records where a question came from.
//
// The rule layer can verify Kind is one of the enumerated values, NotOfficialData is true for
// anything this project designed, and Author and Basis are non-empty. It CANNOT verify that
// Basis names a real document — that is a human review boundary, recorded here rather than
// papered over.
type questionProvenance struct {
	Kind            string `json:"kind"`
	NotOfficialData bool   `json:"notOfficialData"`
	Author          string `json:"author"`
	Basis           string `json:"basis"`
}

// validate reports whether the block may be published.
func (p questionProvenance) validate() error {
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

// loadQuestions reads the authored question bank from the pack.
//
// There is deliberately no built-in fallback. A fallback would make "the compiler produced no
// questions" look identical to "the mode works", which is the failure this project keeps
// hitting. A pack without this section means the mode cannot start, and Prepare says so.
func loadQuestions(src modes.ContentSource) ([]questionTemplate, error) {
	if src == nil {
		return nil, fmt.Errorf(
			"rapport: no content source; %q is required and there is no built-in fallback",
			QuestionsSection)
	}
	raw, err := modeutil.Section(src, QuestionsSection)
	if err != nil {
		return nil, fmt.Errorf(
			"rapport: %q is required: %w (there is no built-in fallback; the pack must publish "+
				"the question bank)", QuestionsSection, err)
	}
	records, err := modeutil.Records(raw, "questions", "items")
	if err != nil {
		return nil, fmt.Errorf("rapport: decode %q: %w", QuestionsSection, err)
	}
	out := make([]questionTemplate, 0, len(records))
	seen := make(map[string]struct{}, len(records))
	for _, rec := range records {
		id := rec.Str("questionId", "question_id", "id")
		prompt := rec.Str("prompt", "text", "question")
		labels := rec.StrList("options", "labels")
		if id == "" || prompt == "" {
			continue
		}
		if _, dup := seen[id]; dup {
			return nil, fmt.Errorf("rapport: question %q appears twice", id)
		}
		// The bank requires exactly four distinct options: a question with fewer cannot be
		// answered as designed, and one with duplicates would make two choices identical.
		if len(labels) != OptionCount {
			return nil, fmt.Errorf(
				"rapport: question %q carries %d options; exactly %d are required",
				id, len(labels), OptionCount)
		}
		seenOptions := make(map[string]struct{}, OptionCount)
		for _, label := range labels {
			if label == "" {
				return nil, fmt.Errorf("rapport: question %q carries an empty option", id)
			}
			if _, dup := seenOptions[label]; dup {
				return nil, fmt.Errorf(
					"rapport: question %q repeats the option %q", id, label)
			}
			seenOptions[label] = struct{}{}
		}
		provRaw, ok := rec.Object("provenance")
		if !ok {
			return nil, fmt.Errorf("rapport: question %q carries no provenance block", id)
		}
		prov := questionProvenance{
			Kind:            provRaw.Str("kind"),
			NotOfficialData: provRaw.Bool("notOfficialData", "not_official_data"),
			Author:          provRaw.Str("author"),
			Basis:           provRaw.Str("basis"),
		}
		if err := prov.validate(); err != nil {
			return nil, fmt.Errorf("rapport: question %q: %w", id, err)
		}
		seen[id] = struct{}{}
		out = append(out, questionTemplate{ID: id, Prompt: prompt, Options: labels})
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("rapport: %q carries no usable question", QuestionsSection)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

// Prepare implements modes.Mode.// Prepare implements modes.Mode.
//
// The rotation is derived from the seed and the roster so that every participant
// chooses at least once, which requires ("不得选择只让部分成员有操作的题量").
// With q questions and n participants, chooser i of question k is
// roster[(offset + k) % n], and q ≥ n guarantees a full cycle.
func (*Mode) Prepare(ctx context.Context, in modes.PrepareInput) (modes.Prepared, error) {
	if len(in.Participants) > 0 {
		return prepareLive(ctx, in)
	}
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
			return modes.Prepared{}, fmt.Errorf("rapport: duplicate subject %q", subj.ID)
		}
		seen[subj.ID] = struct{}{}
		roster = append(roster, rosterEntry{SubjectID: subj.ID, SubjectKind: subj.Kind})
	}
	if len(roster) < MinSubjects || len(roster) > MaxSubjects {
		return modes.Prepared{}, fmt.Errorf(
			"rapport: %d participants outside [%d,%d]", len(roster), MinSubjects, MaxSubjects)
	}
	// Every participant must get at least one chooser turn, otherwise the round
	// is unplayable for part of the roster .
	if cfg.Questions < len(roster) {
		return modes.Prepared{}, fmt.Errorf(
			"rapport: %d questions cannot give all %d participants a chooser turn",
			cfg.Questions, len(roster))
	}

	templates, err := loadQuestions(in.Content)
	if err != nil {
		return modes.Prepared{}, err
	}
	if cfg.Questions > len(templates) {
		return modes.Prepared{}, fmt.Errorf(
			"rapport: %d questions requested but only %d are authored",
			cfg.Questions, len(templates))
	}

	// Draw the question set and the rotation offset from the seed, so the same
	// scope always yields the same round .
	stream := modeutil.NewStream(in.Scope.Seed + "\x00rapport.questions")
	indices := make([]int, len(templates))
	for i := range indices {
		indices[i] = i
	}
	stream.Shuffle(len(indices), func(i, j int) { indices[i], indices[j] = indices[j], indices[i] })
	picked := make([]questionTemplate, 0, cfg.Questions)
	for i := 0; i < cfg.Questions; i++ {
		picked = append(picked, templates[indices[i]])
	}
	// Order the drawn questions by id so the round is stable to read even though
	// the draw itself was seeded: the seeded choice is which questions, not a
	// random display order that would make the reveal harder to follow.
	sort.SliceStable(picked, func(i, j int) bool { return picked[i].ID < picked[j].ID })

	offset := modeutil.PickIndex(in.Scope.Seed, "rapport.rotation", len(roster))

	questions := make([]bankQuestion, 0, len(picked))
	totalSlots := 0
	for k, tmpl := range picked {
		chooser := roster[(offset+k)%len(roster)].SubjectID
		predictors := make([]string, 0, len(roster)-1)
		for _, entry := range roster {
			if entry.SubjectID == chooser {
				continue
			}
			predictors = append(predictors, entry.SubjectID)
		}
		options := make([]bankOption, 0, len(tmpl.Options))
		for i, label := range tmpl.Options {
			options = append(options, bankOption{
				OptionID: optionID(tmpl.ID, i),
				Label:    label,
			})
		}
		questions = append(questions, bankQuestion{
			QuestionID:      tmpl.ID,
			Prompt:          tmpl.Prompt,
			Options:         options,
			ChooserID:       chooser,
			PredictorIDs:    predictors,
			PredictionSlots: len(predictors),
		})
		totalSlots += len(predictors)
	}
	if totalSlots <= 0 {
		return modes.Prepared{}, fmt.Errorf(
			"rapport: %d participants over %d questions yields no prediction slot",
			len(roster), len(questions))
	}

	pub := publicState{
		ModeID:       ModeID,
		RulesVersion: RulesVersion,
		Config:       cfg,
		Subjects:     roster,
		Questions:    questions,
		TotalSlots:   totalSlots,
		Scoring: "floor(100 × 命中点 / 开局公布的总预测槽数); " +
			"缺交也占用固定分母",
	}
	priv := privateState{
		ModeID: ModeID,
		Note:   "the answer to each question is a player's own preference, stored per subject",
	}
	return modes.Prepared{
		Public:        modeutil.Marshal(pub),
		Private:       modeutil.Marshal(priv),
		QuestionCount: len(questions),
	}, nil
}

// optionID builds a stable opaque option id from a question id and an index. It
// is not a label and not a position in a hidden answer: the option set is public,
// so the id may be readable.
func optionID(questionID string, index int) string {
	return fmt.Sprintf("%s-o%d", questionID, index+1)
}

// answerPayload is the body of ActionKindAnswer, matching the contract's
// RapportAnswer schema: {kind, optionId} with additionalProperties false.
type answerPayload struct {
	Kind     string `json:"kind"`
	OptionID string `json:"optionId"`
}

// decodePublic unmarshals the public state.
func decodePublic(raw json.RawMessage) (publicState, error) {
	var pub publicState
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &pub); err != nil {
			return publicState{}, fmt.Errorf("rapport: decode public state: %w", err)
		}
	}
	return pub, nil
}

// decodeState unmarshals a participant state.
func decodeState(raw json.RawMessage) (subjectState, error) {
	var st subjectState
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &st); err != nil {
			return subjectState{}, fmt.Errorf("rapport: decode participant state: %w", err)
		}
	}
	return st, nil
}

// questionByID finds a question in the published set.
func questionByID(pub publicState, questionID string) (bankQuestion, bool) {
	for _, q := range pub.Questions {
		if q.QuestionID == questionID {
			return q, true
		}
	}
	return bankQuestion{}, false
}

// Apply implements modes.Mode. It performs no I/O and no content access.
//
// The caller's role decides the meaning of the option: for the chooser it is
// their own preference, for everybody else it is a prediction. The rule layer
// knows the role from the published rotation, never from the request body.
func (*Mode) Apply(in modes.ApplyInput) (modes.Decision, error) {
	if in.Action.Kind != ActionKindAnswer {
		return modes.Rejected(CodeUnknownAction,
			fmt.Sprintf("rapport: unsupported action %q", in.Action.Kind)), nil
	}
	if err := in.Action.Validate(); err != nil {
		return modes.Rejected(CodeInvalidPayload, err.Error()), nil
	}
	var payload answerPayload
	if err := json.Unmarshal(in.Action.Payload, &payload); err != nil {
		return modes.Rejected(CodeInvalidPayload, "rapport: payload must carry optionId"), nil
	}
	if payload.Kind != "" && payload.Kind != ActionKindAnswer {
		return modes.Rejected(CodeInvalidPayload, fmt.Sprintf(
			"rapport: payload kind %q does not match action %q",
			payload.Kind, ActionKindAnswer)), nil
	}
	if payload.OptionID == "" {
		return modes.Rejected(CodeInvalidPayload, "rapport: empty optionId"), nil
	}

	pub, err := decodePublic(in.Public)
	if err != nil {
		return modes.Decision{}, err
	}
	if pub.Live != nil {
		return applyLive(pub, in, payload)
	}
	if !onRoster(pub.Subjects, in.Subject.ID) {
		return modes.Rejected(CodeNotOnRoster, fmt.Sprintf(
			"rapport: subject %q is not on this roster", in.Subject.ID)), nil
	}
	st, err := decodeState(in.State)
	if err != nil {
		return modes.Decision{}, err
	}

	// Resolve which question this answer belongs to. The option id carries its
	// question, so an answer can never be applied to the wrong question.
	target, ok := questionForOption(pub, payload.OptionID)
	if !ok {
		return modes.Rejected(CodeUnknownOption, fmt.Sprintf(
			"rapport: option %q is not part of this round", payload.OptionID)), nil
	}

	// The role comes from the published rotation, never from the request body.
	isChooser := target.ChooserID == in.Subject.ID
	if !isChooser && !contains(target.PredictorIDs, in.Subject.ID) {
		return modes.Rejected(CodeNotOnRoster, fmt.Sprintf(
			"rapport: subject %q has no role in question %q",
			in.Subject.ID, target.QuestionID)), nil
	}

	// Re-submitting the same question is refused rather than overwriting: the
	// chooser's pick is revealed to the predictors, so changing it after some
	// predictors answered would leak their predictions by comparison.
	if st.answered(target.QuestionID) {
		return modes.Rejected(CodeDuplicateAnswer, fmt.Sprintf(
			"rapport: you already answered question %q", target.QuestionID)), nil
	}
	// Once every question this subject has a role in is answered there is
	// nothing left to submit, so a further answer is a finished round rather
	// than an unknown option.
	if !hasPendingQuestion(pub, st, in.Subject.ID) {
		return modes.Rejected(CodeRoundFinished, "rapport: you have answered every question"), nil
	}

	st.Answers = append(st.Answers, answerRecord{
		QuestionID: target.QuestionID,
		OptionID:   payload.OptionID,
		AsChooser:  isChooser,
	})

	feedback := modeutil.Marshal(struct {
		QuestionID string `json:"questionId"`
		OptionID   string `json:"optionId"`
		// AsChooser tells the client which role this answer filled, so the UI
		// labels it correctly without inferring from the roster.
		AsChooser bool `json:"asChooser"`
		// Note repeats the visibility rule so the client never shows the answer
		// back to the wrong side.
		Note string `json:"note"`
	}{
		QuestionID: target.QuestionID,
		OptionID:   payload.OptionID,
		AsChooser:  isChooser,
		Note:       "your answer stays hidden from the other side until the question locks",
	})

	dec := modes.Accepted(feedback, modeutil.Marshal(st))
	return dec, nil
}

// hasPendingQuestion reports whether subjectID still has an unanswered question
// they hold a role in.
func hasPendingQuestion(pub publicState, st subjectState, subjectID string) bool {
	for _, q := range pub.Questions {
		if q.ChooserID != subjectID && !contains(q.PredictorIDs, subjectID) {
			continue
		}
		if !st.answered(q.QuestionID) {
			return true
		}
	}
	return false
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

// contains reports whether list holds value.
func contains(list []string, value string) bool {
	for _, item := range list {
		if item == value {
			return true
		}
	}
	return false
}

// questionForOption finds the question an option id belongs to.
func questionForOption(pub publicState, optionID string) (bankQuestion, bool) {
	for _, q := range pub.Questions {
		for _, opt := range q.Options {
			if opt.OptionID == optionID {
				return q, true
			}
		}
	}
	return bankQuestion{}, false
}

// projection is the viewer-specific round state.
type projection struct {
	CurrentQuestion int             `json:"currentQuestion,omitempty"`
	QuestionCount   int             `json:"questionCount,omitempty"`
	GroupID         string          `json:"groupId,omitempty"`
	Groups          []groupProgress `json:"groups,omitempty"`
	CanAnswer       bool            `json:"canAnswer"`
	ModeID          string          `json:"modeId"`
	RulesVersion    string          `json:"rulesVersion"`
	Phase           modes.Phase     `json:"phase"`
	Config          Config          `json:"config"`
	TotalSlots      int             `json:"totalSlots"`
	Scoring         string          `json:"scoring"`
	Questions       []questionView  `json:"questions"`
	// RevealedHits and RevealedSlots are the caller's own tally over the questions that have
	// already been revealed. They are the caller's own result, not a team-wide standing:
	// publishing a team tally mid-round would tell a predictor how many of the others already
	// got it right.
	//
	// They are deliberately NOT named "hits"/"slots": those bare names sat next to totalSlots
	// (the fixed denominator) and read as "the round has 0 slots" before any question resolved.
	// A zero here means "nothing has been revealed yet", which the field name now says.
	RevealedHits  int `json:"revealedHits"`
	RevealedSlots int `json:"revealedSlots"`
	// Self is the caller's own answers.
	Self *selfView `json:"self,omitempty"`
	// Final appears only once the round is over.
	Final []modes.Score `json:"final,omitempty"`
}

// questionView is one question as a specific viewer sees it.
type questionView struct {
	QuestionID string       `json:"questionId"`
	Prompt     string       `json:"prompt"`
	Options    []bankOption `json:"options"`
	// ChooserID is public: everybody knows whose preference is being predicted.
	ChooserID string `json:"chooserId"`
	// SelfIsChooser tells this viewer whether they choose or predict.
	SelfIsChooser bool `json:"selfIsChooser"`
	// Answered reports whether this viewer already submitted for this question.
	Answered bool `json:"answered"`
	// SelfOptionID is this viewer's own submission, once made.
	SelfOptionID string `json:"selfOptionId,omitempty"`
	// Locked reports that every participant answered and the result is visible.
	Locked bool `json:"locked"`
	// Reveal is present only for a locked question (or for a referee).
	Reveal *questionReveal `json:"reveal,omitempty"`
}

// questionReveal is the answer of one question, emitted only when allowed.
type questionReveal struct {
	// ChosenOptionID is the chooser's own preference.
	ChosenOptionID string `json:"chosenOptionId"`
	// ChosenLabel repeats the label so the reveal is readable without joining.
	ChosenLabel string `json:"chosenLabel"`
	// CorrectPredictorIDs lists who predicted correctly. It is part of the
	// reveal because the round is over for that question by then.
	CorrectPredictorIDs []string `json:"correctPredictorIds"`
}

// selfView is the caller's own answer list.
type selfView struct {
	SubjectID string         `json:"subjectId"`
	Answers   []answerRecord `json:"answers"`
}

// Project implements modes.Mode.
//
// The visibility rule, in one place:
//
// - Before a question locks, a viewer sees only their OWN submission. The
// chooser's pick and the other predictors' guesses are omitted entirely, not
// blanked: a null field still tells the client a submission exists.
// - Once every participant has answered the question (or the round is
// terminal), the reveal is published to everybody.
// - A referee may see a locked question early, which is the host's reveal
// authority; a referee never sees an unlocked question's answers, because
// that would let the host steer the round.
func (*Mode) Project(in modes.ProjectInput) (json.RawMessage, error) {
	pub, err := decodePublic(in.Public)
	if err != nil {
		return nil, err
	}
	if pub.Live != nil {
		return projectLive(pub, in)
	}

	out := projection{
		ModeID:       ModeID,
		RulesVersion: RulesVersion,
		Phase:        in.Phase,
		Config:       pub.Config,
		TotalSlots:   pub.TotalSlots,
		Scoring:      pub.Scoring,
		Questions:    make([]questionView, 0, len(pub.Questions)),
	}

	// Decode every state once, in roster order, so the projection is a pure
	// function of the stored bytes .
	states := make(map[string]subjectState, len(pub.Subjects))
	for _, entry := range pub.Subjects {
		st, err := decodeState(in.States[entry.SubjectID])
		if err != nil {
			return nil, fmt.Errorf("rapport: decode state of %q: %w", entry.SubjectID, err)
		}
		states[entry.SubjectID] = st
	}

	// The caller's own view.
	if self, ok := states[in.Viewer.Subject.ID]; ok {
		answers := self.Answers
		if answers == nil {
			answers = []answerRecord{}
		}
		out.Self = &selfView{SubjectID: in.Viewer.Subject.ID, Answers: answers}
	}

	for _, q := range pub.Questions {
		view := questionView{
			QuestionID: q.QuestionID,
			Prompt:     q.Prompt,
			Options:    q.Options,
			ChooserID:  q.ChooserID,
		}
		view.SelfIsChooser = q.ChooserID == in.Viewer.Subject.ID

		// A question is locked when every participant with a role answered it.
		locked := true
		for _, id := range append([]string{q.ChooserID}, q.PredictorIDs...) {
			if !states[id].answered(q.QuestionID) {
				locked = false
				break
			}
		}
		if in.Phase == modes.PhaseRevealed || in.Phase == modes.PhaseSettled {
			locked = true
		}

		if own, ok := states[in.Viewer.Subject.ID].option(q.QuestionID); ok {
			view.Answered = true
			view.SelfOptionID = own.OptionID
		}

		if locked {
			view.Locked = true
			reveal := buildReveal(q, states)
			view.Reveal = &reveal
			// The caller's own tally counts only locked questions, so it is never
			// ahead of what has actually been revealed.
			if !view.SelfIsChooser {
				out.RevealedSlots++
				if own, ok := states[in.Viewer.Subject.ID].option(q.QuestionID); ok {
					if chosen, okChosen := states[q.ChooserID].option(q.QuestionID); okChosen &&
						own.OptionID == chosen.OptionID {
						out.RevealedHits++
					}
				}
			}
		} else if in.Phase != modes.PhaseCancelled && in.Viewer.IsReferee && in.RevealNow {
			// A referee with the reveal authority may inspect an unlocked question
			// only when the room explicitly asks for it, and the payload is marked
			// as a referee view so the client never shows it to players.
			reveal := buildReveal(q, states)
			view.Reveal = &reveal
		}

		out.Questions = append(out.Questions, view)
	}

	// The final scores are the settled result, published once the round is over.
	if in.Phase == modes.PhaseRevealed || in.Phase == modes.PhaseSettled {
		scores, err := New().Settle(modes.SettleInput{
			Scope:   in.Scope,
			Phase:   in.Phase,
			Public:  in.Public,
			Private: in.Private,
			States:  in.States,
		})
		if err != nil {
			return nil, err
		}
		out.Final = scores
	}
	return modeutil.Marshal(out), nil
}

// buildReveal assembles one question's reveal from the stored answers.
//
// A missing chooser answer yields an empty chosen option: that happens only when
// the round is settled with an unfinished question, and reporting an empty choice
// is honest, whereas inventing one would fabricate a preference.
func buildReveal(q bankQuestion, states map[string]subjectState) questionReveal {
	reveal := questionReveal{CorrectPredictorIDs: []string{}}
	chosen, ok := states[q.ChooserID].option(q.QuestionID)
	if !ok {
		return reveal
	}
	reveal.ChosenOptionID = chosen.OptionID
	for _, opt := range q.Options {
		if opt.OptionID == chosen.OptionID {
			reveal.ChosenLabel = opt.Label
			break
		}
	}
	for _, predictor := range q.PredictorIDs {
		own, okOwn := states[predictor].option(q.QuestionID)
		if !okOwn {
			continue
		}
		if own.OptionID == chosen.OptionID {
			reveal.CorrectPredictorIDs = append(reveal.CorrectPredictorIDs, predictor)
		}
	}
	// Deterministic order: the roster order is the natural one, but sorting makes
	// the payload stable even if a state was written out of order.
	sort.Strings(reveal.CorrectPredictorIDs)
	return reveal
}

// Settle implements modes.Mode.
//
// the team score is floor(100 × hit points / total prediction slots), with
// the total announced at start and a missing prediction still occupying its slot.
// Every participant on the team receives the same team score, because the score is
// a property of the partnership, not of one member.
func (*Mode) Settle(in modes.SettleInput) ([]modes.Score, error) {
	pub, err := decodePublic(in.Public)
	if err != nil {
		return nil, err
	}
	if pub.Live != nil {
		return settleLive(pub, in)
	}

	states := make(map[string]subjectState, len(pub.Subjects))
	for _, entry := range pub.Subjects {
		st, err := decodeState(in.States[entry.SubjectID])
		if err != nil {
			return nil, fmt.Errorf("rapport: decode state of %q: %w", entry.SubjectID, err)
		}
		states[entry.SubjectID] = st
	}

	hits := 0
	for _, q := range pub.Questions {
		chosen, ok := states[q.ChooserID].option(q.QuestionID)
		if !ok {
			// No preference was recorded, so nobody can have hit it. The slots
			// still count, which is the documented fixed denominator.
			continue
		}
		for _, predictor := range q.PredictorIDs {
			own, okOwn := states[predictor].option(q.QuestionID)
			if okOwn && own.OptionID == chosen.OptionID {
				hits++
			}
		}
	}

	slots := pub.TotalSlots
	units := 0
	if slots > 0 {
		// floor(100 × hits / slots) display points, stored as scoreUnits.
		units = (100 * hits / slots) * 100
	}

	scores := make([]modes.Score, 0, len(pub.Subjects))
	for _, entry := range pub.Subjects {
		explanation := modeutil.Marshal(struct {
			Hits       int    `json:"hits"`
			TotalSlots int    `json:"totalSlots"`
			HitsBySelf int    `json:"hitsBySelf"`
			ChooserFor int    `json:"chooserFor"`
			Formula    string `json:"formula"`
		}{
			Hits:       hits,
			TotalSlots: slots,
			HitsBySelf: selfHits(pub, states, entry.SubjectID),
			ChooserFor: chooserCount(pub, entry.SubjectID),
			Formula:    "floor(100*hits/totalSlots)",
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

// selfHits counts one participant's own correct predictions.
func selfHits(pub publicState, states map[string]subjectState, subjectID string) int {
	hits := 0
	for _, q := range pub.Questions {
		if q.ChooserID == subjectID {
			continue
		}
		chosen, ok := states[q.ChooserID].option(q.QuestionID)
		if !ok {
			continue
		}
		own, okOwn := states[subjectID].option(q.QuestionID)
		if okOwn && own.OptionID == chosen.OptionID {
			hits++
		}
	}
	return hits
}

// chooserCount counts how many questions a participant chose on.
func chooserCount(pub publicState, subjectID string) int {
	n := 0
	for _, q := range pub.Questions {
		if q.ChooserID == subjectID {
			n++
		}
	}
	return n
}
