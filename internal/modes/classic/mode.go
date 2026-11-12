package classic

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/bhcn/umaguessinggame/internal/modes"
	"github.com/bhcn/umaguessinggame/internal/modes/modeutil"
)

// Action kinds accepted by this mode.
//
// The value is the contract discriminator from
// api/openapi.json (ModeAction → ClassicGuess), not an
// internal name: the discriminator is what selects the payload schema, so the
// rule layer must recognise exactly the string the wire carries .
const (
	// ActionGuess submits one character guess. It is not retractable .
	ActionGuess = "classic.guess"
)

// Rejection codes returned in Decision.Code.
const (
	// CodeUnknownCharacter is returned for an id that is not in the round pack.
	// It never consumes an error budget .
	CodeUnknownCharacter = "unknown_character"
	// CodeDuplicateGuess is returned when the same character was already
	// submitted this round. It never consumes an error budget .
	CodeDuplicateGuess = "duplicate_guess"
	// CodeRoundFinished is returned once the subject has won or run out.
	CodeRoundFinished = "round_finished"
	// CodeInvalidPayload is returned when the action body does not parse.
	CodeInvalidPayload = "invalid_payload"
	// CodeUnknownAction is returned for an unsupported action kind.
	CodeUnknownAction = "unknown_action"
)

// Variant is the play form; it changes the clock and the ranking, never the
// answer or the feedback .
type Variant string

const (
	// VariantSingle is the unlimited-time solo form with the default budget.
	VariantSingle Variant = "single"
	// VariantFriend is the two-player race: same answer, separate budgets, the
	// first server-confirmed solve wins.
	VariantFriend Variant = "friend"
	// VariantPublic is the event form: same question, 3-minute window,
	// independent submissions, ranked by solved then by submission count.
	VariantPublic Variant = "public"
)

// WinPolicy selects when a multi-player round ends.
//
// The published schema has offered these three values since the contract was
// written, but the mode never read the field — a host choosing first-correct got
// the same behaviour as solo. fixes the competitive default: 猜对立即结束.
type WinPolicy string

const (
	// WinPolicySolo is the single-player form: the round ends when the only
	// subject solves or exhausts the budget.
	WinPolicySolo WinPolicy = "solo"
	// WinPolicyFirstCorrect ends the round as soon as any subject solves.
	WinPolicyFirstCorrect WinPolicy = "first-correct"
	// WinPolicyWindowAttempts ranks by attempts within the timed window.
	WinPolicyWindowAttempts WinPolicy = "window-attempts"
)

// Config is the round configuration for classic.
//
// Field names follow the published OpenAPI schema (ClassicConfig), because that
// is what a client sends and what the documentation promises. The struct used to
// carry its own private names (wrongBudget / timeLimitSeconds / variant), and the
// contract names were silently dropped: a host asking for 4 wrong guesses got the
// default 8, and winPolicy was not read at all. That is the worst kind of defect
// — the request succeeds, the round starts, and the game is quietly not the one
// that was configured.
//
// The legacy names stay accepted so an older client or a stored config blob still
// decodes; see decodeConfig for the precedence rule.
type Config struct {
	// MaxWrongGuesses is the allowed number of wrong guesses (4–12, default 8).
	MaxWrongGuesses int `json:"maxWrongGuesses"`
	// WrongBudget is the legacy name for MaxWrongGuesses. Accepted on input only.
	WrongBudget int `json:"wrongBudget,omitempty"`
	// WinPolicy selects when a multi-player round ends. : a correct guess
	// ends the round immediately.
	WinPolicy WinPolicy `json:"winPolicy,omitempty"`
	// DurationSeconds bounds the round for the timed variants; 0 means unlimited.
	DurationSeconds int `json:"durationSeconds,omitempty"`
	// TimeLimitSeconds is the legacy name for DurationSeconds.
	TimeLimitSeconds int `json:"timeLimitSeconds,omitempty"`
	// ShadowEnabled shows opponent progress in friend matches .
	ShadowEnabled bool `json:"shadowEnabled,omitempty"`
	// Variant selects the play form.
	Variant Variant `json:"variant"`
	// Practice marks a practice round, which may include ambiguous answer
	// vectors that competitive packs must exclude .
	Practice bool `json:"practice"`
}

// decodeConfig parses and normalises a configuration blob. An absent blob means
// the documented defaults.
func decodeConfig(raw json.RawMessage) (Config, error) {
	cfg := Config{WrongBudget: DefaultWrongBudget, Variant: VariantSingle, ShadowEnabled: true}
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &cfg); err != nil {
			return Config{}, fmt.Errorf("classic: invalid config: %w", err)
		}
	}
	// Legacy names win only when the contract name is absent, so a client that
	// sends both gets the documented field.
	if cfg.MaxWrongGuesses == 0 {
		cfg.MaxWrongGuesses = cfg.WrongBudget
	}
	if cfg.MaxWrongGuesses == 0 {
		cfg.MaxWrongGuesses = DefaultWrongBudget
	}
	if cfg.DurationSeconds == 0 {
		cfg.DurationSeconds = cfg.TimeLimitSeconds
	}
	if cfg.Variant == "" {
		cfg.Variant = VariantSingle
	}
	if cfg.WinPolicy == "" {
		// solo has one subject; a multi-subject round is competitive
		// and ends on the first correct guess.
		if cfg.Variant == VariantSingle {
			cfg.WinPolicy = WinPolicySolo
		} else {
			cfg.WinPolicy = WinPolicyFirstCorrect
		}
	}
	switch cfg.WinPolicy {
	case WinPolicySolo, WinPolicyFirstCorrect, WinPolicyWindowAttempts:
	default:
		return Config{}, fmt.Errorf("classic: unknown winPolicy %q", cfg.WinPolicy)
	}
	switch cfg.Variant {
	case VariantSingle, VariantFriend, VariantPublic:
	default:
		return Config{}, fmt.Errorf("classic: unknown variant %q", cfg.Variant)
	}
	if cfg.MaxWrongGuesses < MinWrongBudget || cfg.MaxWrongGuesses > MaxWrongBudget {
		return Config{}, fmt.Errorf(
			"classic: maxWrongGuesses %d out of range [%d,%d]",
			cfg.MaxWrongGuesses, MinWrongBudget, MaxWrongBudget,
		)
	}
	if cfg.DurationSeconds < 0 {
		return Config{}, fmt.Errorf("classic: negative durationSeconds %d", cfg.DurationSeconds)
	}
	if cfg.Variant == VariantSingle {
		if cfg.DurationSeconds != 0 {
			return Config{}, errors.New("classic: the single variant does not use a time limit")
		}
	} else if cfg.DurationSeconds == 0 {
		// fixes the public-event window at 3 minutes; the friend match
		// reuses it unless the host sets one explicitly.
		cfg.DurationSeconds = 180
	}
	// Keep the legacy aliases in sync for readers that still use them. This runs
	// last so the defaulting above (the 180s window) is already applied.
	cfg.WrongBudget = cfg.MaxWrongGuesses
	cfg.TimeLimitSeconds = cfg.DurationSeconds
	return cfg, nil
}

// state is one subject's persisted participant state.
type state struct {
	// RemainingWrong is how many wrong guesses are still allowed. A correct
	// guess does not decrease it .
	RemainingWrong int `json:"remainingWrong"`
	// Attempts counts every accepted guess, correct ones included.
	Attempts int `json:"attempts"`
	// Guessed lists the character ids already submitted, in submission order.
	Guessed []string `json:"guessed"`
	// Guesses is the caller's feedback history. Rows mirrors it for older clients and stored state.
	Guesses []GuessRow `json:"guesses"`
	// Rows is the legacy name for Guesses. Kept so an older client or a stored
	// state blob still decodes; see Guesses for why both exist.
	Rows []GuessRow `json:"rows"`
	// Solved and Failed are terminal flags.
	Solved bool `json:"solved"`
	Failed bool `json:"failed"`
	// SolvedAtAttempt is the attempt index that solved the round, 0 if unsolved.
	SolvedAtAttempt int `json:"solvedAtAttempt"`
	// SolvedAtMillis is the server-assigned arrival time of the solving guess.
	// It is used only by the friend variant, where "first server-confirmed
	// solve wins" is the documented rule . Public events must not use
	// it: there, ties share a rank .
	SolvedAtMillis int64 `json:"solvedAtMillis"`
}

// finished reports whether the subject can no longer act.
func (s state) finished() bool { return s.Solved || s.Failed }

// rosterEntry is one participant recorded in the public prepared state.
type rosterEntry struct {
	SubjectID   string            `json:"subjectId"`
	SubjectKind modes.SubjectKind `json:"subjectKind"`
}

// poolCard is the public, non-secret view of a guessable character: the guess
// list only needs identity and artwork, never the nine hidden fields
// .
type poolCard struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	ImagePath string `json:"imagePath"`
}

// publicPrepared is the non-secret part of a prepared round.
type publicPrepared struct {
	ModeID       string        `json:"modeId"`
	RulesVersion string        `json:"rulesVersion"`
	Config       Config        `json:"config"`
	FieldOrder   []string      `json:"fieldOrder"`
	PoolSize     int           `json:"poolSize"`
	Pool         []poolCard    `json:"pool"`
	Subjects     []rosterEntry `json:"subjects"`
}

// privatePrepared is the server-only answer material.
//
// It embeds the comparable vectors so Apply needs no content access: the mode
// contract forbids I/O during Apply, and the round is bound to the pack version
// captured here .
type privatePrepared struct {
	AnswerID   string      `json:"answerId"`
	AnswerName string      `json:"answerName"`
	Answer     Character   `json:"answer"`
	Pool       []Character `json:"pool"`
}

// Mode implements modes.Mode for G01.
type Mode struct{}

// New returns the classic mode.
func New() *Mode { return &Mode{} }

// ID implements modes.Mode.
func (*Mode) ID() string { return ModeID }

// RulesVersion implements modes.Mode.
func (*Mode) RulesVersion() string { return RulesVersion }

// Title implements modes.Mode.
func (*Mode) Title() string { return "经典弗一把" }

// Summary implements modes.Mode.
func (*Mode) Summary() string {
	return "在限定错误额度内猜出角色身份，每次猜测给出九项字段提示。"
}

// InformationStyles reports which information forms this mode implements.
func (*Mode) InformationStyles() []string {
	return []string{"closed"}
}

// RoundWindow implements modes.Mode.
//
// Classic is the one mode whose window is a config field rather than a per-turn
// constant, and 0 is meaningful: a solo round has no time limit at all. Returning
// 0 there is correct — the room layer treats "no window" as no deadline, which is
// exactly what an untimed solo round is.
func (*Mode) RoundWindow(raw json.RawMessage) time.Duration {
	cfg, err := decodeConfig(raw)
	if err != nil {
		return 0
	}
	return time.Duration(cfg.DurationSeconds) * time.Second
}

// TurnWindow implements modes.Mode. This mode is not turn-based: it runs on the
// single RoundWindow deadline, so there is no per-turn clock to report.
func (*Mode) TurnWindow(json.RawMessage) time.Duration { return 0 }

// MinSubjects implements modes.Mode.
func (*Mode) MinSubjects() int { return 1 }

// MaxSubjects implements modes.Mode.
func (*Mode) MaxSubjects() int { return 16 }

// ContractKinds returns the ModeAction discriminators this mode accepts. It is
// the list the contract-alignment test checks against openapi.json.
func (*Mode) ContractKinds() []string { return []string{ActionGuess} }

// Requirement implements modes.Mode .
func (*Mode) Requirement() modes.ContentRequirement {
	return modes.ContentRequirement{
		Fields: []string{
			"star", "surfaceGroup", "distanceGroup", "runningStyleGroup",
			"sexType", "g1Bracket", "g23Bracket", "schoolGrade", "dormitory",
		},
		// One usable character vector is enough to pose a round; the player count is
		// enforced by the room, not here. This used to be 2, which conflated the two.
		MinEntities: 1,
		NeedsAssets: false,
	}
}

// ValidateConfig implements modes.Mode.
func (*Mode) ValidateConfig(cfg json.RawMessage) error {
	_, err := decodeConfig(cfg)
	return err
}

// Prepare implements modes.Mode. The answer is drawn deterministically from the
// seed, so the same scope always yields the same answer .
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
	chars, err := loadCharacters(in.Content, cfg.Practice)
	if err != nil {
		return modes.Prepared{}, err
	}

	// Competitive rounds must exclude ambiguous vectors: two characters with
	// identical nine fields cannot be told apart by the feedback alone, so the
	// program must not silently treat them as one identity .
	pool := make([]Character, 0, len(chars))
	for _, c := range chars {
		if !cfg.Practice && c.AmbiguityGroup != "" {
			continue
		}
		pool = append(pool, c)
	}
	if len(pool) < 2 {
		return modes.Prepared{}, fmt.Errorf(
			"classic: pool has %d comparable characters, need at least 2", len(pool),
		)
	}

	answerIdx := modeutil.PickIndex(in.Scope.Seed, "classic.answer", len(pool))
	answer := pool[answerIdx]

	roster := make([]rosterEntry, 0, len(in.Subject))
	seen := make(map[string]struct{}, len(in.Subject))
	for _, subj := range in.Subject {
		if err := subj.Validate(); err != nil {
			return modes.Prepared{}, err
		}
		if _, dup := seen[subj.ID]; dup {
			return modes.Prepared{}, fmt.Errorf("classic: duplicate subject %q", subj.ID)
		}
		seen[subj.ID] = struct{}{}
		roster = append(roster, rosterEntry{SubjectID: subj.ID, SubjectKind: subj.Kind})
	}
	if len(roster) < 1 || len(roster) > 16 {
		return modes.Prepared{}, fmt.Errorf("classic: %d subjects outside [1,16]", len(roster))
	}
	if cfg.Variant == VariantFriend && len(roster) != 2 {
		return modes.Prepared{}, fmt.Errorf(
			"classic: the friend variant needs exactly 2 subjects, got %d", len(roster),
		)
	}

	cards := make([]poolCard, 0, len(pool))
	for _, c := range pool {
		cards = append(cards, poolCard{ID: c.ID, Name: c.Name, ImagePath: c.ImagePath})
	}

	pub := publicPrepared{
		ModeID:       ModeID,
		RulesVersion: RulesVersion,
		Config:       cfg,
		FieldOrder:   FieldOrder,
		PoolSize:     len(pool),
		Pool:         cards,
		Subjects:     roster,
	}
	priv := privatePrepared{
		AnswerID:   answer.ID,
		AnswerName: answer.Name,
		Answer:     answer,
		Pool:       pool,
	}
	return modes.Prepared{
		Public:        modeutil.Marshal(pub),
		Private:       modeutil.Marshal(priv),
		QuestionCount: 1,
	}, nil
}

// guessPayload is the body of ActionGuess, matching the contract's ClassicGuess
// schema: {kind, characterId} with additionalProperties false. Kind is decoded so
// a payload whose kind disagrees with the envelope is rejected instead of being
// silently accepted.
type guessPayload struct {
	Kind        string `json:"kind"`
	CharacterID string `json:"characterId"`
}

// Apply implements modes.Mode. It performs no I/O: every vector it needs was
// frozen into the private prepared state by Prepare.
func (*Mode) Apply(in modes.ApplyInput) (modes.Decision, error) {
	if in.Action.Kind != ActionGuess {
		return modes.Rejected(CodeUnknownAction,
			fmt.Sprintf("classic: unsupported action %q", in.Action.Kind)), nil
	}
	if err := in.Action.Validate(); err != nil {
		return modes.Rejected(CodeInvalidPayload, err.Error()), nil
	}
	var payload guessPayload
	if err := json.Unmarshal(in.Action.Payload, &payload); err != nil {
		return modes.Rejected(CodeInvalidPayload, "classic: guess payload must carry characterId"), nil
	}
	if payload.Kind != "" && payload.Kind != ActionGuess {
		return modes.Rejected(CodeInvalidPayload, fmt.Sprintf(
			"classic: payload kind %q does not match action %q", payload.Kind, ActionGuess,
		)), nil
	}
	if payload.CharacterID == "" {
		return modes.Rejected(CodeInvalidPayload, "classic: empty characterId"), nil
	}

	var pub publicPrepared
	if len(in.Public) > 0 {
		if err := json.Unmarshal(in.Public, &pub); err != nil {
			return modes.Decision{}, fmt.Errorf("classic: decode public state: %w", err)
		}
	}
	var priv privatePrepared
	if len(in.Private) > 0 {
		if err := json.Unmarshal(in.Private, &priv); err != nil {
			return modes.Decision{}, fmt.Errorf("classic: decode private state: %w", err)
		}
	}
	st, err := decodeState(in.State)
	if err != nil {
		return modes.Decision{}, fmt.Errorf("classic: decode subject state: %w", err)
	}
	if st.Attempts == 0 && st.RemainingWrong == 0 {
		st.RemainingWrong = pub.Config.MaxWrongGuesses
		if st.RemainingWrong == 0 {
			st.RemainingWrong = DefaultWrongBudget
		}
	}

	// A finished subject cannot act again; the round is over for them.
	if st.finished() {
		return modes.Rejected(CodeRoundFinished, "classic: this round is already finished for you"), nil
	}

	// Resolve the guess against the frozen pool. An id that is unknown, absent
	// from this pack or not comparable is rejected and never scored as a wrong
	// guess .
	var guess Character
	found := false
	for _, c := range priv.Pool {
		if c.ID == payload.CharacterID {
			guess, found = c, true
			break
		}
	}
	if !found {
		return modes.Rejected(CodeUnknownCharacter,
			fmt.Sprintf("classic: character %q is not in this round's pool", payload.CharacterID)), nil
	}

	// A repeated guess is refused and costs nothing, so a retried request cannot
	// burn a budget .
	for _, guessed := range st.Guessed {
		if guessed == payload.CharacterID {
			return modes.Rejected(CodeDuplicateGuess,
				fmt.Sprintf("classic: character %q was already guessed", payload.CharacterID)), nil
		}
	}

	row := BuildGuessRow(guess, priv.Answer)
	row.Attempt = st.Attempts + 1
	row.Correct = payload.CharacterID == priv.AnswerID

	st.Attempts++
	st.Guessed = append(st.Guessed, payload.CharacterID)
	st.Rows = append(st.Rows, row)
	// 两个字段名指向同一份历史，必须同步追加，否则 guesses 会永远为空。
	st.Guesses = append(st.Guesses, row)

	if row.Correct {
		// a correct guess does not decrease the error budget; the UI
		// shows the real submission count separately.
		st.Solved = true
		st.SolvedAtAttempt = st.Attempts
		st.SolvedAtMillis = in.ReceivedAt
	} else {
		st.RemainingWrong--
		if st.RemainingWrong <= 0 {
			st.RemainingWrong = 0
			st.Failed = true
		}
	}

	feedback := modeutil.Marshal(struct {
		Row            GuessRow `json:"row"`
		RemainingWrong int      `json:"remainingWrong"`
		Attempts       int      `json:"attempts"`
		Solved         bool     `json:"solved"`
		Failed         bool     `json:"failed"`
	}{
		Row:            row,
		RemainingWrong: st.RemainingWrong,
		Attempts:       st.Attempts,
		Solved:         st.Solved,
		Failed:         st.Failed,
	})

	dec := modes.Accepted(feedback, modeutil.Marshal(st))
	dec.Finished = st.finished()
	// first-correct ends the round for everyone the moment someone solves
	// . Finished alone only closes this subject, which is
	// why a host asking for first-correct used to see the round sit in collecting
	// until the deadline while everyone else kept guessing.
	if st.Solved && pub.Config.WinPolicy == WinPolicyFirstCorrect {
		dec.RoundOver = true
	}
	return dec, nil
}

// shadowRow is the opponent-shadow projection: the nine statuses without
// revealing which character was guessed (: 只显示对手每次的九项状态，
// 不显示猜了谁).
type shadowRow struct {
	Attempt  int          `json:"attempt"`
	Statuses []shadowCell `json:"statuses"`
}

// shadowCell is one status without its value.
type shadowCell struct {
	Field  string     `json:"field"`
	Status CellStatus `json:"status"`
}

// progress is the coarse progress an opponent or spectator may see.
type progress struct {
	SubjectID string `json:"subjectId"`
	// Submitted reports whether this subject has made at least one guess.
	//
	// A host console needs it to tell "has not started" from "still thinking": the
	// attempt count alone cannot distinguish a player who is idle from one who is
	// mid-round, and a missing-submission panel would show everyone as outstanding.
	Submitted      bool `json:"submitted"`
	Attempts       int  `json:"attempts"`
	RemainingWrong int  `json:"remainingWrong"`
	Solved         bool `json:"solved"`
	Failed         bool `json:"failed"`
	// SolvedAtAttempt and SolvedAtMillis make "who solved it first" decidable from the
	// snapshot alone.
	//
	// They were computed and stored but never projected, so a client could see that two
	// players had solved but not which one was earlier — the very fact says
	// decides a friend match. Millis is the server's arrival time, not a client clock, so
	// the comparison cannot be influenced by a skewed browser.
	SolvedAtAttempt int   `json:"solvedAtAttempt,omitempty"`
	SolvedAtMillis  int64 `json:"solvedAtMillis,omitempty"`
}

// projection is the viewer-specific round state.
type projection struct {
	ModeID       string        `json:"modeId"`
	RulesVersion string        `json:"rulesVersion"`
	Phase        modes.Phase   `json:"phase"`
	Config       Config        `json:"config"`
	FieldOrder   []string      `json:"fieldOrder"`
	PoolSize     int           `json:"poolSize"`
	Subjects     []progress    `json:"subjects"`
	Self         *selfView     `json:"self,omitempty"`
	Shadows      []shadowRow   `json:"shadows,omitempty"`
	Answer       *answerReveal `json:"answer,omitempty"`
}

// selfView is the caller's own full state.
type selfView struct {
	SubjectID      string     `json:"subjectId"`
	Attempts       int        `json:"attempts"`
	RemainingWrong int        `json:"remainingWrong"`
	Solved         bool       `json:"solved"`
	Failed         bool       `json:"failed"`
	Rows           []GuessRow `json:"rows"`
}

// answerReveal is the secret answer, emitted only when authorised.
type answerReveal struct {
	CharacterID string `json:"characterId"`
	Name        string `json:"name"`
	ImagePath   string `json:"imagePath"`
}

// Project implements modes.Mode. Secret material (the answer and the opponents'
// guessed characters) is included only for authorised viewers .
func (*Mode) Project(in modes.ProjectInput) (json.RawMessage, error) {
	var pub publicPrepared
	if len(in.Public) > 0 {
		if err := json.Unmarshal(in.Public, &pub); err != nil {
			return nil, fmt.Errorf("classic: decode public state: %w", err)
		}
	}
	var priv privatePrepared
	if len(in.Private) > 0 {
		if err := json.Unmarshal(in.Private, &priv); err != nil {
			return nil, fmt.Errorf("classic: decode private state: %w", err)
		}
	}

	out := projection{
		ModeID:       ModeID,
		RulesVersion: RulesVersion,
		Phase:        in.Phase,
		Config:       pub.Config,
		FieldOrder:   pub.FieldOrder,
		PoolSize:     pub.PoolSize,
		Subjects:     make([]progress, 0, len(pub.Subjects)),
	}

	// Iterate the roster order recorded at prepare time, never the map, so the
	// projection is byte-identical across runs .
	for _, entry := range pub.Subjects {
		st, err := decodeState(in.States[entry.SubjectID])
		if err != nil {
			return nil, fmt.Errorf("classic: decode state of %q: %w", entry.SubjectID, err)
		}
		out.Subjects = append(out.Subjects, progress{
			SubjectID:       entry.SubjectID,
			Submitted:       st.Attempts > 0 || st.Solved || st.Failed,
			Attempts:        st.Attempts,
			RemainingWrong:  st.RemainingWrong,
			Solved:          st.Solved,
			Failed:          st.Failed,
			SolvedAtAttempt: st.SolvedAtAttempt,
			SolvedAtMillis:  st.SolvedAtMillis,
		})
		if entry.SubjectID == in.Viewer.Subject.ID {
			out.Self = &selfView{
				SubjectID:      entry.SubjectID,
				Attempts:       st.Attempts,
				RemainingWrong: st.RemainingWrong,
				Solved:         st.Solved,
				Failed:         st.Failed,
				Rows:           st.Rows,
			}
			continue
		}
		// Opponent shadow: statuses only, never the guessed character. Only the
		// friend variant shows it; spectators of a public event do not get a
		// per-guess shadow of everyone .
		//
		// shadowEnabled lets the host turn it off for a friend match. The field is
		// in the published schema, so ignoring it meant a host who disabled the
		// shadow still saw opponent progress. It defaults to true (see
		// decodeConfig), so an older client that never sends it keeps today's
		// behaviour.
		if pub.Config.Variant == VariantFriend && pub.Config.ShadowEnabled {
			for _, row := range st.Rows {
				sr := shadowRow{Attempt: row.Attempt}
				for _, c := range row.Cells.all() {
					sr.Statuses = append(sr.Statuses, shadowCell{Field: c.Field, Status: c.Status})
				}
				out.Shadows = append(out.Shadows, sr)
			}
		}
	}

	// The answer is revealed to a referee, or to everyone once the round is
	// terminal .
	if in.Viewer.SeesSecrets() || (in.RevealNow && in.Phase.Terminal()) {
		out.Answer = &answerReveal{
			CharacterID: priv.AnswerID,
			Name:        priv.AnswerName,
			ImagePath:   priv.Answer.ImagePath,
		}
	}
	return modeutil.Marshal(out), nil
}

// Settle implements modes.Mode.
//
// Activity score : a solve scores 100 - 8×(submissions-1) with a floor
// of 44; an unsolved round scores 0. Under a non-default budget a solve on the
// 9th submission or later takes max(20, formula). Ranking is by score
// descending, ties share a rank and are never broken by arrival milliseconds.
func (*Mode) Settle(in modes.SettleInput) ([]modes.Score, error) {
	var pub publicPrepared
	if len(in.Public) > 0 {
		if err := json.Unmarshal(in.Public, &pub); err != nil {
			return nil, fmt.Errorf("classic: decode public state: %w", err)
		}
	}
	budget := pub.Config.MaxWrongGuesses
	if budget == 0 {
		budget = DefaultWrongBudget
	}

	scores := make([]modes.Score, 0, len(pub.Subjects))
	for _, entry := range pub.Subjects {
		st, err := decodeState(in.States[entry.SubjectID])
		if err != nil {
			return nil, fmt.Errorf("classic: decode state of %q: %w", entry.SubjectID, err)
		}
		units := ActivityScore(st.Attempts, st.Solved, budget)
		explanation := modeutil.Marshal(struct {
			Attempts       int  `json:"attempts"`
			RemainingWrong int  `json:"remainingWrong"`
			Solved         bool `json:"solved"`
			Failed         bool `json:"failed"`
			WrongBudget    int  `json:"wrongBudget"`
		}{
			Attempts:       st.Attempts,
			RemainingWrong: st.RemainingWrong,
			Solved:         st.Solved,
			Failed:         st.Failed,
			WrongBudget:    budget,
		})
		score := modes.Score{
			Subject:       modes.Subject{ID: entry.SubjectID, Kind: entry.SubjectKind},
			ScoreUnits:    units,
			MaxScoreUnits: modeutil.Units(activityBase),
			Explanation:   explanation,
		}
		if err := score.Validate(); err != nil {
			return nil, err
		}
		scores = append(scores, score)
	}
	return modes.RankScores(scores), nil
}
