// Package modes defines the pure rule-engine contract every game mode implements.
//
// Rules in this package must be deterministic: identical inputs and seed always
// produce identical outputs. They must not depend on map iteration order, float
// rounding, system locale or real network arrival order .
//
// This file is the interface contract between the HTTP/room layer and the mode
// implementations. Mode packages must not import net/http, database/sql or any
// browser type.
package modes

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"time"
)

// Phase is the shared round phase machine .
type Phase string

const (
	PhasePrepared   Phase = "prepared"
	PhaseBriefing   Phase = "briefing"
	PhaseCollecting Phase = "collecting"
	PhaseLocked     Phase = "locked"
	PhaseRevealed   Phase = "revealed"
	PhaseSettled    Phase = "settled"
	PhaseCancelled  Phase = "cancelled"
)

// Terminal reports whether no further submission may change the outcome.
func (p Phase) Terminal() bool {
	return p == PhaseRevealed || p == PhaseSettled || p == PhaseCancelled
}

// SubjectKind distinguishes an individual player from a whole team.
type SubjectKind string

const (
	SubjectPlayer SubjectKind = "player"
	SubjectTeam   SubjectKind = "team"
)

// Valid reports whether the kind is one the schema accepts.
func (k SubjectKind) Valid() bool {
	return k == SubjectPlayer || k == SubjectTeam
}

// Subject identifies who a submission or score belongs to. The value always
// comes from a server-side membership record, never from a request body.
type Subject struct {
	ID   string      `json:"subjectId"`
	Kind SubjectKind `json:"subjectKind"`
}

// Validate rejects malformed subjects before they reach SQL.
func (s Subject) Validate() error {
	if s.ID == "" {
		return errors.New("modes: empty subject id")
	}
	if !s.Kind.Valid() {
		return fmt.Errorf("modes: invalid subject kind %q", s.Kind)
	}
	return nil
}

// Scope carries the immutable versions a round is bound to. A content or rules
// update only affects new rounds .
type Scope struct {
	ModeID             string `json:"modeId"`
	RulesVersion       string `json:"rulesVersion"`
	DatasetVersion     string `json:"datasetVersion"`
	ContentPackVersion string `json:"contentPackVersion"`
	Seed               string `json:"seed"`
}

// Validate rejects an incomplete binding.
func (s Scope) Validate() error {
	switch {
	case s.ModeID == "":
		return errors.New("modes: empty modeId")
	case s.RulesVersion == "":
		return errors.New("modes: empty rulesVersion")
	case s.DatasetVersion == "":
		return errors.New("modes: empty datasetVersion")
	case s.ContentPackVersion == "":
		return errors.New("modes: empty contentPackVersion")
	case s.Seed == "":
		return errors.New("modes: empty seed")
	}
	return nil
}

// Viewer describes the identity asking for a projection. Secret fields are
// selected from this, never filtered in the browser .
type Viewer struct {
	// SessionID is server-resolved and never copied to a player projection.
	SessionID string
	// CanManageRound is server-resolved moderation authority, separate from secrets.
	CanManageRound bool
	// MembershipID is the server-resolved membership of the caller.
	MembershipID string
	// Role is player, team-captain, spectator, host or display.
	Role string
	// Subject is the caller's own scoring subject, when they are competing.
	Subject Subject
	// CanControl reports whether this viewer currently holds the control lease.
	CanControl bool
	// IsReferee reports whether this viewer holds the reveal-secrets authority.
	IsReferee bool
}

// SeesSecrets reports whether the viewer may receive answer material.
func (v Viewer) SeesSecrets() bool { return v.IsReferee }

// ModeAction is one typed submission. Kind selects the payload schema; the
// payload is validated by the mode, not by a permissive JSON blob .
type ModeAction struct {
	Kind    string          `json:"kind"`
	Payload json.RawMessage `json:"payload"`
}

// Validate requires a kind and a non-empty payload.
func (a ModeAction) Validate() error {
	if a.Kind == "" {
		return errors.New("modes: empty action kind")
	}
	if len(a.Payload) == 0 {
		return errors.New("modes: empty action payload")
	}
	if !json.Valid(a.Payload) {
		return errors.New("modes: action payload is not valid JSON")
	}
	return nil
}

// Decision is the outcome of one action. A rejected action must carry a code and
// must not mutate persistent state .
type Decision struct {
	Accepted bool   `json:"accepted"`
	Code     string `json:"code,omitempty"`
	Detail   string `json:"detail,omitempty"`
	// Issues identify rejected inputs without requiring clients to parse prose.
	// They describe only the attempted action, never a hidden solution.
	Issues []ActionIssue `json:"issues,omitempty"`
	// Feedback is the caller's own, already-public feedback for this action.
	Feedback json.RawMessage `json:"feedback,omitempty"`
	// State is the caller's updated participant state after the action.
	State json.RawMessage `json:"state,omitempty"`
	// Finished reports that this action ended the round for this subject.
	Finished bool `json:"finished,omitempty"`
	// RoundOver reports that this action ended the round for everyone, so the
	// room should close it now instead of waiting for the deadline.
	//
	// The distinction matters: Finished is per-subject (this player solved or ran
	// out), while RoundOver is a mode-level decision that no further submission
	// can change the outcome. A mode whose win policy is first-correct sets both
	// when someone solves; a solo mode sets only Finished, because the remaining
	// subjects are not in the round at all.
	RoundOver bool `json:"roundOver,omitempty"`
}

type ActionIssue struct {
	Field string `json:"field"`
	Code  string `json:"code"`
}

// Rejected builds a rejection decision.
func Rejected(code, detail string) Decision {
	return Decision{Accepted: false, Code: code, Detail: detail}
}

// Accepted builds an acceptance decision.
func Accepted(feedback, state json.RawMessage) Decision {
	return Decision{Accepted: true, Feedback: feedback, State: state}
}

// Score is one subject's settled result. ScoreUnits are integer hundredths of a
// display point: 100 units == 1 point .
type Score struct {
	Subject       Subject         `json:"subject"`
	ScoreUnits    int             `json:"scoreUnits"`
	MaxScoreUnits int             `json:"maxScoreUnits"`
	Explanation   json.RawMessage `json:"explanation"`
	Rank          int             `json:"rank,omitempty"`
	SharedRank    bool            `json:"sharedRank,omitempty"`
}

// RankScores assigns competition ranks to already-computed scores, sharing a
// rank on ties and skipping the following ordinal (1,1,3). Ordering is by score
// descending, then by subject id ascending so the result is deterministic.
func RankScores(scores []Score) []Score {
	out := make([]Score, len(scores))
	copy(out, scores)
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].ScoreUnits != out[j].ScoreUnits {
			return out[i].ScoreUnits > out[j].ScoreUnits
		}
		return out[i].Subject.ID < out[j].Subject.ID
	})
	for i := range out {
		if i > 0 && out[i].ScoreUnits == out[i-1].ScoreUnits {
			out[i].Rank = out[i-1].Rank
			out[i].SharedRank = true
		} else {
			out[i].Rank = i + 1
			out[i].SharedRank = false
		}
	}
	// Mark every member of a tie group, not just the later ones.
	for i := range out {
		if out[i].SharedRank {
			continue
		}
		for j := range out {
			if j != i && out[j].ScoreUnits == out[i].ScoreUnits {
				out[i].SharedRank = true
				break
			}
		}
	}
	return out
}

// Validate enforces the integer score invariant.
func (s Score) Validate() error {
	if err := s.Subject.Validate(); err != nil {
		return err
	}
	if s.ScoreUnits < 0 {
		return fmt.Errorf("modes: negative scoreUnits %d", s.ScoreUnits)
	}
	if s.MaxScoreUnits <= 0 {
		return fmt.Errorf("modes: non-positive maxScoreUnits %d", s.MaxScoreUnits)
	}
	if s.ScoreUnits > s.MaxScoreUnits {
		return fmt.Errorf("modes: scoreUnits %d exceeds max %d", s.ScoreUnits, s.MaxScoreUnits)
	}
	return nil
}

// ContentRequirement is what a mode needs from the compiled content pack.
type ContentRequirement struct {
	// Fields lists canonical fact fields the mode reads.
	Fields []string
	// MinEntities is the smallest usable CONTENT POOL this mode accepts.
	//
	// It is compared against ModeAdmission.QuestionCount (the number of items the
	// compiler produced), NOT against a participant count. The two are different
	// questions and conflating them has already caused real defects:
	//
	// - strategyrace set MinEntities to MinTeams (2), so a complete one-baseline pack
	// was rejected because 1 < 2;
	// - bluff set it to MinSubjects (3), so a pack with two reviewed facts could not
	// start a three-player round;
	// - trade multiplied players by hand size, comparing a card count against an
	// objective pool — the units did not match.
	//
	// A mode whose round needs N players but can run on one baseline item sets
	// MinEntities=1. The player count is enforced separately by the room, which is the
	// only layer that knows how many people actually joined.
	MinEntities int
	// NeedsAssets reports whether the mode requires verified media.
	NeedsAssets bool
}

// Availability explains whether a mode may be opened right now.
type Availability struct {
	Available bool   `json:"available"`
	Reason    string `json:"reason,omitempty"`
	// QuestionCount is the number of usable items in the current pack.
	QuestionCount int `json:"questionCount"`
}

// Mode is implemented once per game mode. Every method must be safe for
// concurrent use and must not retain mutable state between calls.
type Mode interface {
	// ID is the stable mode identifier used in URLs and rounds.
	ID() string
	// RulesVersion is the version this implementation satisfies.
	RulesVersion() string
	// Title and Summary feed the lobby card .
	Title() string
	Summary() string
	// InformationStyles lists the open/closed forms this mode actually
	// implements, most representative first.
	//
	// The lobby used to hardcode "closed" for every mode, so a host looking at
	// the mode list could not tell that sort/co-race-path/race-rebuild support an
	// open form at all — the option was invisible before entering a room.
	InformationStyles() []string
	// MinSubjects and MaxSubjects bound the participant count.
	MinSubjects() int
	MaxSubjects() int
	// Requirement declares content needs for admission.
	Requirement() ContentRequirement
	// RoundWindow reports how long one round of this mode may run, given its
	// config. Zero means "no opinion" and the room layer falls back to its own
	// default.
	//
	// Without this the room layer only knew about durationSeconds, a field that
	// classic carries and most other modes do not. Every other mode therefore
	// silently got the five-minute service default, which for strategy-race meant
	// a 300-second window while the track the players are looking at says
	// "每回合 20 秒" for eight turns . The screen and the clock disagreed.
	//
	// A mode knows its own shape: a per-turn window times the number of turns, or
	// a single deadline. The room layer cannot derive that from a JSON blob it is
	// not allowed to interpret.
	RoundWindow(cfg json.RawMessage) time.Duration
	// TurnWindow reports how long ONE turn of a turn-based mode may run, given
	// its config. Zero means "this mode is not turn-based" and the round runs on
	// the single RoundWindow deadline.
	//
	// RoundWindow alone was not enough for strategy-race: the room layer only
	// knew one deadline per round, so a race where nobody submitted sat in turn 1
	// forever while the screen said "每回合 20 秒". The per-turn clock is a
	// different thing from the whole-race clock and only the mode knows it.
	TurnWindow(cfg json.RawMessage) time.Duration
	// ValidateConfig rejects an illegal round configuration before start.
	ValidateConfig(cfg json.RawMessage) error
	// Prepare loads the immutable question set for a round. It runs outside the
	// write transaction; the caller stores the result and later re-checks its
	// version inside the transaction .
	Prepare(ctx context.Context, in PrepareInput) (Prepared, error)
	// Apply validates one action against the current state and returns the
	// decision plus the new per-subject state. It must not perform I/O.
	Apply(in ApplyInput) (Decision, error)
	// Project builds the viewer-specific state. Secret material is only included
	// when the viewer is authorised.
	Project(in ProjectInput) (json.RawMessage, error)
	// Settle computes final scores deterministically from committed state.
	Settle(in SettleInput) ([]Score, error)
}

// TurnExpirer is implemented by turn-based modes that can close an expired turn
// on the server's authority.
//
// The room layer owns the clock but the mode owns the race state, so the room
// asks the mode to record "this turn is over; everyone who did not commit is
// treated as having taken the default action". The returned public state is the
// new authoritative state and `more` reports whether any turn is still open.
//
// A mode that does not implement this keeps the old behaviour: the round runs on
// the single RoundWindow deadline.
type TurnExpirer interface {
	ExpireTurn(public json.RawMessage) (next json.RawMessage, more bool, err error)
}

// TurnProgress reports the open turn without applying an action. The token must
// change only when the turn changes; more is false once the match is complete.
// Submissions use this read-only query; ExpireTurn is reserved for elapsed clocks.
type TurnProgress interface {
	OpenTurn(public json.RawMessage) (token string, more bool, err error)
}

// TeamControlProgress defines when a team's next member takes over. The token
// changes only after a complete attempt/turn; more is false when play is over.
// The room persists the handover in the same transaction as the game action.
type TeamControlProgress interface {
	ControlTurn(public, state json.RawMessage) (token string, more bool, err error)
}

// TimedPreview reserves the first part of RoundWindow for memorisation. The
// server enforces it; refreshing or a host's early start cannot skip the window.
type TimedPreview interface {
	PreviewWindow(config json.RawMessage) time.Duration
}

// IndividualParticipation keeps personal hands and secrets separate even when
// room members retain activity team labels. It does not opt a mode into team scoring.
type IndividualParticipation interface {
	UsesIndividualSeats() bool
}

// ConfiguredParticipation preserves the topology of previously saved rounds.
type ConfiguredParticipation interface {
	IndividualSeatsForConfig(json.RawMessage) bool
}

// Participant separates a member's private information from the team's answer.
// Both identifiers are resolved by the room layer and frozen at preparation.
type Participant struct {
	SessionID    string
	MembershipID string
	Subject      Subject
	TeamID       string
}

// RosterValidator validates member topology rather than counting folded teams
// as individual players. Modes without it retain the existing subject bounds.
type RosterValidator interface {
	ValidateRoster(subjects []Subject, participants []Participant) error
}

// ScoringRoster separates individual submissions from team event results.
type ScoringRoster interface {
	ScoringSubjects([]Participant) ([]Subject, error)
}

// PrepareInput is everything a mode needs to build a question set.
type PrepareInput struct {
	Scope        Scope
	Subject      []Subject
	Participants []Participant
	Config       json.RawMessage
	// Content is the compiled pack handle; modes read only their own section.
	Content ContentSource
}

// Prepared is the immutable result of Prepare. Private is stored server-side
// only; Public is safe to project to any viewer.
type Prepared struct {
	// Config is an optional server-normalised inner config, persisted for clocks
	// and recovery. Omission preserves the caller's original configuration.
	Config  json.RawMessage
	Public  json.RawMessage
	Private json.RawMessage
	// SubjectPrivate carries state that belongs to one participant and must never be
	// shown to another: a trade player's own objectives, a bluff player's own decoy.
	//
	// It is keyed by subject id. A single round-level Private blob cannot serve this:
	// the room layer stores it once, so every viewer would either see nothing or see
	// everyone's secrets. Keeping the split here lets the room layer write one row per
	// subject and project only the caller's own row.
	SubjectPrivate map[string]json.RawMessage
	// QuestionCount reports how many items the round actually uses.
	QuestionCount int
}

// ApplyInput carries one validated action and the state it applies to.
type ApplyInput struct {
	// MembershipID is the authenticated actor, separate from a team subject.
	MembershipID string
	Scope        Scope
	Subject      Subject
	Action       ModeAction
	// Public is the round's public question state.
	Public json.RawMessage
	// Private is the round's server-only secret state.
	Private json.RawMessage
	// State is this subject's current participant state, nil on first action.
	State json.RawMessage
	// SubmissionCount is how many accepted actions this subject already made.
	SubmissionCount int
	// ReceivedAt is the server-assigned arrival time in UTC millis.
	ReceivedAt int64
}

// ProjectInput builds a viewer-specific projection.
type ProjectInput struct {
	Scope     Scope
	Viewer    Viewer
	Public    json.RawMessage
	Private   json.RawMessage
	States    map[string]json.RawMessage
	Phase     Phase
	RevealNow bool
	// ServerTime is an explicit input for short-lived presentation windows.
	// A mode must never read the wall clock itself.
	ServerTime int64
}

// SettleInput computes final scores.
type SettleInput struct {
	Scope   Scope
	Phase   Phase
	Public  json.RawMessage
	Private json.RawMessage
	States  map[string]json.RawMessage
}

// ContentSource gives modes read-only access to the compiled content pack.
type ContentSource interface {
	// Section returns the raw JSON for a named section of the pack.
	Section(name string) (json.RawMessage, bool)
	// Version is the pack version the source was loaded from.
	Version() string
}

// Registry holds the available modes by id.
type Registry struct {
	byID  map[string]Mode
	order []string
}

// NewRegistry builds a registry and rejects duplicate ids.
func NewRegistry(modes ...Mode) (*Registry, error) {
	r := &Registry{byID: make(map[string]Mode, len(modes))}
	for _, m := range modes {
		id := m.ID()
		if id == "" {
			return nil, errors.New("modes: mode with empty id")
		}
		if _, dup := r.byID[id]; dup {
			return nil, fmt.Errorf("modes: duplicate mode id %q", id)
		}
		r.byID[id] = m
		r.order = append(r.order, id)
	}
	sort.Strings(r.order)
	return r, nil
}

// Get returns a mode by id.
func (r *Registry) Get(id string) (Mode, bool) {
	m, ok := r.byID[id]
	return m, ok
}

// IDs returns the registered ids in sorted order.
func (r *Registry) IDs() []string {
	out := make([]string, len(r.order))
	copy(out, r.order)
	return out
}

// All returns modes in sorted id order.
func (r *Registry) All() []Mode {
	out := make([]Mode, 0, len(r.order))
	for _, id := range r.order {
		out = append(out, r.byID[id])
	}
	return out
}
