package room

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/bhcn/umaguessinggame/internal/characternames"
	"strings"

	"github.com/bhcn/umaguessinggame/internal/httpapi/problem"
	"github.com/bhcn/umaguessinggame/internal/modes"
	"github.com/bhcn/umaguessinggame/internal/modes/audio"
	"github.com/bhcn/umaguessinggame/internal/modes/bluff"
	"github.com/bhcn/umaguessinggame/internal/modes/rapport"
	"github.com/bhcn/umaguessinggame/internal/modes/strategyrace"
	"github.com/bhcn/umaguessinggame/internal/modes/trade"
)

// ContentIndex is the read-only compiled content handle.
//
// It extends modes.ContentSource with the dataset version, which the round
// binding must record so an old round keeps referencing the pack it started
// with even after a content update .
type ContentIndex interface {
	modes.ContentSource
	// DatasetVersion is the canonical dataset version of the loaded pack.
	DatasetVersion() string
	// AssetBaseURL is the public prefix for media references.
	AssetBaseURL() string
	// Character returns a public character projection by id.
	Character(id string) (CharacterCard, bool)
	// Characters returns a page of public character projections.
	Characters(cursor string, limit int, query string) (items []CharacterCard, next string, err error)
	// PathNeighbors returns content-graph neighbours for an open-book path round.
	PathNeighbors(characterID string) ([]Neighbor, bool)
	// ModeAdmission returns the compiler's authoritative admission record for a
	// mode: whether enough usable questions were compiled, how many, and the
	// section that carries them.
	//
	// This is the only correct source for "can this mode be opened". It must not
	// be inferred from ContentRequirement.Fields, which names canonical fact
	// fields (star, aptitudes.distance) rather than pack section names, so
	// checking those against Section() would report every mode as unavailable.
	ModeAdmission(modeID string) (ModeAdmission, bool)
}

// ModeAdmission is the content compiler's verdict for one mode.
type ModeAdmission struct {
	Mode          string
	Available     bool
	QuestionCount int
	Section       string
	Reason        string
}

// CharacterCard is the public character projection.
//
// It contains only what the contract's Character schema allows. Answers, secret
// seeds and private clue material are never part of this type .
type CharacterCard struct {
	SearchNames     *characternames.Names
	LocalizedNames  map[string]string
	CharacterID     string
	DisplayName     string
	Aliases         []string
	EntityKind      string
	PortraitAssetID string
	// ImagePath is the local media route, always present for a published character.
	ImagePath string
	// RemoteImageURL is the upstream CDN URL, empty when the snapshot has none.
	RemoteImageURL string
	Birthday       string
	HeightCm       int
	Aptitudes      map[string]string
	DatasetVersion string
}

// Neighbor is one content-graph edge with its evidence.
type Neighbor struct {
	CharacterID string
	DisplayName string
	AssetID     string
	Evidence    []EvidenceRef
}

// EvidenceRef is one source citation for a content edge.
type EvidenceRef struct {
	EvidenceID string
	SourceURL  string
	Label      string
	RaceID     string
}

// ---- phase transitions ---------------------------------------------------

// TransitionInput is a requested phase change.
type TransitionInput struct {
	Target               string
	ExpectedPhaseVersion int64
	Reason               string
	ExtensionSeconds     int
	ExcludedOptionIDs    []string
}

// TransitionResult is the accepted transition receipt.
type TransitionResult struct {
	TransitionID string
	RoundID      string
	PhaseVersion int64
	AcceptedAt   int64
	RoomVersion  int64
}

// transitionTargets are the contract's allowed target values.
var transitionTargets = map[string]bool{
	"briefing": true, "collecting": true, "locked": true,
	"creative-voting": true, "bluff-voting": true, "revealed": true, "settled": true, "pause": true, "resume": true, "cancel": true,
}

// CreateTransition performs an authorised, version-checked phase change.
//
// State machine :
//
//	prepared → briefing → collecting → locked → revealed → settled
//
// pause and resume are orthogonal: they do not create paused_* phases, they set
// pausedAt plus remainingMs and restore the previous phase on resume. Going
// backwards out of settled is refused; a replay creates a new round instead
// .
func (s *Service) CreateTransition(ctx context.Context, actor Actor, roundID string, in TransitionInput) (TransitionResult, error) {
	if err := requireAnyCapability(actor, "only a host or room manager may change the round phase",
		CapHost, CapManageRoom); err != nil {
		return TransitionResult{}, err
	}
	if !transitionTargets[in.Target] {
		return TransitionResult{}, problem.Validation("target is invalid",
			problem.FieldError{Field: "target", Message: "is not an allowed phase target"})
	}
	if len(in.ExcludedOptionIDs) > 32 || (len(in.ExcludedOptionIDs) > 0 && in.Target != "bluff-voting" && in.Target != "creative-voting") {
		return TransitionResult{}, problem.Validation("invalid review exclusions")
	}
	if in.ExtensionSeconds < 0 || in.ExtensionSeconds > 600 {
		return TransitionResult{}, problem.Validation("extensionSeconds is invalid",
			problem.FieldError{Field: "extensionSeconds", Message: "must be 0..600"})
	}
	if n := len([]rune(in.Reason)); n > 240 {
		return TransitionResult{}, problem.Validation("reason is too long",
			problem.FieldError{Field: "reason", Message: "must be at most 240 characters"})
	}

	// The queue assigns receivedAt at enqueue time; the transaction then decides
	// against that timestamp, never against client time .
	receivedAt, release, err := s.queue.Enqueue(ctx, actor.RoomID)
	if err != nil {
		return TransitionResult{}, err
	}
	defer release()

	transitionID, err := newID("trn_")
	if err != nil {
		return TransitionResult{}, problem.Internal(err)
	}
	now := receivedAt.UTC().UnixMilli()

	tx, err := s.db.Write().BeginTx(ctx, nil)
	if err != nil {
		return TransitionResult{}, problem.Internal(fmt.Errorf("room: begin transition: %w", err))
	}
	defer tx.Rollback()
	if err := s.validateControllerQ(ctx, tx, actor); err != nil {
		return TransitionResult{}, err
	}

	r, err := s.loadRoundQ(ctx, tx, roundID)
	if err != nil {
		return TransitionResult{}, err
	}
	if r.RoomID != actor.RoomID {
		return TransitionResult{}, errRoundNotFound
	}
	if r.PhaseVersion != in.ExpectedPhaseVersion {
		return TransitionResult{}, phaseConflict(r.PhaseVersion)
	}
	if r.Paused() && in.Target != "resume" && in.Target != "cancel" {
		return TransitionResult{}, problem.Conflict("round-paused", "resume before changing the phase")
	}

	newPhase, newVersion, deadline, pausedAt, pausedRemaining, pausedPrev, err := s.applyTransition(r, in, now)
	if r.ModeID == "bluff" {
		newPhase, newVersion, deadline, err = s.bluffTransitionQ(ctx, tx, r, in, now)
	} else if r.ModeID == "creative" {
		newPhase, newVersion, deadline, err = s.creativeTransitionQ(ctx, tx, r, in, now)
	} else if in.Target == "bluff-voting" || in.Target == "creative-voting" {
		err = problem.Conflict("invalid-transition", "this transition is only for bluff")
	}
	if err != nil {
		return TransitionResult{}, err
	}

	// Sequential races and preference questions must actually run every window.
	// A host may pause or cancel, but cannot skip unfinished questions.
	if (r.ModeID == "strategy-race" || r.ModeID == "rapport" || r.ModeID == "trade") && (in.Target == "locked" || in.Target == "revealed" || in.Target == "settled") {
		public, err := roundPublicStateQ(ctx, tx, r.ID)
		if err != nil {
			return TransitionResult{}, err
		}
		more, err := hasUnfinishedWindows(r.ModeID, public)
		if err != nil {
			return TransitionResult{}, problem.Internal(err)
		}
		if more {
			return TransitionResult{}, problem.Conflict(r.ModeID+"-incomplete", "finish all windows or cancel the round")
		}
	}

	if r.ModeID == "audio" && in.Target == "collecting" && r.Status != modes.PhaseCollecting {
		public, err := roundPublicStateQ(ctx, tx, r.ID)
		if err != nil {
			return TransitionResult{}, err
		}
		states, err := participantStatesQ(ctx, tx, r.ID)
		if err != nil {
			return TransitionResult{}, err
		}
		ready, err := audio.AllPrepared(public, states)
		if err != nil {
			return TransitionResult{}, problem.Internal(err)
		}
		if !ready {
			return TransitionResult{}, problem.Conflict("audio-preparation-required", "every playing subject must confirm media preparation before starting")
		}
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE rounds SET status = ?, phase_version = ?, deadline_at = ?, paused_at = ?,
		                  paused_remaining_ms = ?, paused_previous_phase = ?, updated_at = ?
		WHERE id = ?`,
		string(newPhase), newVersion, nullableInt(deadline), nullableInt(pausedAt),
		nullableInt(pausedRemaining), nullableString(string(pausedPrev)), now, r.ID); err != nil {
		return TransitionResult{}, problem.Internal(fmt.Errorf("room: update phase: %w", err))
	}

	// Keep the event segment's status in step with the round it is bound to.
	//
	// Without this a host console sees a segment stuck on "planned" forever, so it
	// cannot tell a queued programme item from a running or finished one. The update
	// is scoped by round_id, so a round that belongs to no event changes nothing.
	//
	// running ← collecting (the phase players can actually act in)
	// completed ← settled
	// voided ← cancelled
	if err := s.syncSegmentStatusQ(ctx, tx, r.ID, newPhase, now); err != nil {
		return TransitionResult{}, err
	}

	// A terminal phase settles the round: scores are computed inside this same
	// transaction so no observer can see "settled" without results .
	if newPhase == modes.PhaseSettled || newPhase == modes.PhaseCancelled {
		if err := s.settleRoundQ(ctx, tx, r, newPhase, now); err != nil {
			return TransitionResult{}, err
		}
	}
	if newPhase == modes.PhaseSettled || newPhase == modes.PhaseCancelled {
		if _, err := tx.ExecContext(ctx,
			`UPDATE rooms SET status = 'finished', updated_at = ? WHERE id = ? AND status = 'playing'`,
			now, r.RoomID); err != nil {
			return TransitionResult{}, problem.Internal(fmt.Errorf("room: finish room: %w", err))
		}
	}

	if in.Reason != "" || newPhase == modes.PhaseSettled || newPhase == modes.PhaseCancelled {
		if err := writeAudit(ctx, tx, r.RoomID, "", actor.SessionID, "round.transition", map[string]any{
			"roundId": r.ID, "target": in.Target, "from": string(r.Status),
			"to": string(newPhase), "reason": in.Reason, "phaseVersion": newVersion,
		}, now); err != nil {
			return TransitionResult{}, err
		}
	}

	roomVersion, err := bumpRoomVersion(ctx, tx, r.RoomID, now)
	if err != nil {
		return TransitionResult{}, err
	}
	if err := emitOutbox(ctx, tx, r.RoomID, "phase.changed", map[string]any{
		"resource": "round", "roundId": r.ID,
		"phaseVersion": newVersion, "roomVersion": roomVersion,
		"phase": string(newPhase),
	}, now); err != nil {
		return TransitionResult{}, err
	}
	if err := tx.Commit(); err != nil {
		return TransitionResult{}, problem.Internal(fmt.Errorf("room: commit transition: %w", err))
	}
	return TransitionResult{
		TransitionID: transitionID, RoundID: r.ID, PhaseVersion: newVersion,
		AcceptedAt: now, RoomVersion: roomVersion,
	}, nil
}

// applyTransition computes the next phase and its associated window fields.
//
// It is a pure function of the current round plus the request, which makes the
// state machine testable without a database.
func (s *Service) applyTransition(r Round, in TransitionInput, now int64) (
	phase modes.Phase, version int64, deadline, pausedAt, pausedRemaining *int64,
	pausedPrev modes.Phase, err error,
) {
	version = r.PhaseVersion
	deadline = nil
	if r.DeadlineAt != nil {
		v := r.DeadlineAt.UnixMilli()
		deadline = &v
	}
	if r.PausedAt != nil {
		v := r.PausedAt.UnixMilli()
		pausedAt = &v
	}
	pausedRemaining = r.PausedRemainingMS
	pausedPrev = r.PausedPrevious

	if r.Status == modes.PhaseSettled {
		return "", 0, nil, nil, nil, "", problem.Conflict("round-settled",
			"the round is settled; start a replacement round instead of reverting it")
	}
	if r.Status == modes.PhaseCancelled {
		return "", 0, nil, nil, nil, "", problem.Conflict("round-cancelled",
			"the round is cancelled and cannot change phase")
	}

	// A live round past its deadline cannot accept a forward window change
	// without an explicit extension, so an accidental late transition is caught.
	if in.ExtensionSeconds > 0 {
		if r.Status != modes.PhaseCollecting && r.Status != modes.PhaseBriefing {
			return "", 0, nil, nil, nil, "", problem.Conflict("no-window-to-extend",
				"only a briefing or collecting window can be extended")
		}
		base := now
		if deadline != nil && *deadline > now {
			base = *deadline
		}
		ext := base + int64(in.ExtensionSeconds)*1000
		deadline = &ext
		// Extending is a window-semantic change, so phaseVersion advances but the
		// question set is untouched .
		return r.Status, version + 1, deadline, pausedAt, pausedRemaining, pausedPrev, nil
	}

	switch in.Target {
	case "pause":
		if r.Status != modes.PhaseBriefing && r.Status != modes.PhaseCollecting {
			return "", 0, nil, nil, nil, "", problem.Conflict("not-pausable",
				"only a briefing or collecting window can be paused")
		}
		if r.PausedAt != nil {
			return "", 0, nil, nil, nil, "", problem.Conflict("already-paused", "the round is already paused")
		}
		var remaining *int64
		if deadline != nil {
			ms := max(int64(0), *deadline-now)
			remaining = &ms
		}
		pausedAt = &now
		pausedRemaining = remaining
		pausedPrev = r.Status
		deadline = nil
		return r.Status, version + 1, deadline, pausedAt, pausedRemaining, pausedPrev, nil

	case "resume":
		if r.PausedAt == nil {
			return "", 0, nil, nil, nil, "", problem.Conflict("not-paused", "the round is not paused")
		}
		deadline = nil
		if pausedRemaining != nil {
			next := now + *pausedRemaining
			deadline = &next
		}
		pausedAt = nil
		pausedRemaining = nil
		phase := pausedPrev
		if phase == "" {
			phase = modes.PhaseCollecting
		}
		// Resume produces a fresh deadline and advances phaseVersion .
		return phase, version + 1, deadline, pausedAt, pausedRemaining, "", nil

	case "briefing":
		if r.Status != modes.PhasePrepared {
			return "", 0, nil, nil, nil, "", problem.Conflict("invalid-transition",
				fmt.Sprintf("cannot move from %s to briefing", r.Status))
		}
		if preview := s.previewWindowMS(r); preview > 0 {
			next := now + preview
			deadline = &next
		}
		return modes.PhaseBriefing, version + 1, deadline, pausedAt, pausedRemaining, pausedPrev, nil

	case "collecting":
		if r.Status != modes.PhaseBriefing && r.Status != modes.PhasePrepared {
			return "", 0, nil, nil, nil, "", problem.Conflict("invalid-transition",
				fmt.Sprintf("cannot move from %s to collecting", r.Status))
		}
		if s.previewWindowMS(r) > 0 && (r.Status != modes.PhaseBriefing || r.DeadlineAt == nil || now < r.DeadlineAt.UnixMilli()) {
			return "", 0, nil, nil, nil, "", problem.Conflict("preview-in-progress", "the viewing window must finish before answering")
		}
		/*
		 * A turn-based mode starts on its TURN clock, not its round clock: the
		 * screen promises "每回合 20 秒", so the first deadline the player sees
		 * must be one turn, not the whole race. The round clock still bounds the
		 * round overall because the round's deadline keeps being refreshed one
		 * turn at a time.
		 */
		window := s.collectingWindowMS(r)
		if turn := s.turnWindowMS(r); turn > 0 {
			window = turn
		}
		next := now + window
		deadline = &next
		return modes.PhaseCollecting, version + 1, deadline, pausedAt, pausedRemaining, pausedPrev, nil

	case "locked":
		if r.Status != modes.PhaseCollecting && r.Status != modes.PhaseBriefing {
			return "", 0, nil, nil, nil, "", problem.Conflict("invalid-transition",
				fmt.Sprintf("cannot move from %s to locked", r.Status))
		}
		return modes.PhaseLocked, version + 1, deadline, pausedAt, pausedRemaining, pausedPrev, nil

	case "revealed":
		if r.Status != modes.PhaseLocked && r.Status != modes.PhaseCollecting {
			return "", 0, nil, nil, nil, "", problem.Conflict("invalid-transition",
				fmt.Sprintf("cannot move from %s to revealed", r.Status))
		}
		return modes.PhaseRevealed, version + 1, deadline, pausedAt, pausedRemaining, pausedPrev, nil

	case "settled":
		// Settling twice must not double-score: the settled guard above already
		// rejected the repeat, and any other phase may settle once.
		return modes.PhaseSettled, version + 1, deadline, pausedAt, pausedRemaining, pausedPrev, nil

	case "cancel":
		return modes.PhaseCancelled, version + 1, deadline, pausedAt, pausedRemaining, pausedPrev, nil
	}
	return "", 0, nil, nil, nil, "", problem.Validation("target is invalid",
		problem.FieldError{Field: "target", Message: "is not an allowed phase target"})
}

// collectingWindowMS derives the collecting window for a round.
//
// The mode answers first: it knows whether its window is a single deadline
// (classic durationSeconds) or a per-turn window times the number of turns
// (strategy-race: 8 turns × 20 s). The room layer must not interpret a mode's
// config blob itself — doing that is exactly how every non-classic mode ended up
// on the five-minute service default while its own screen said "每回合 20 秒"
// . The mode's answer is read from the STORED config, so a reload cannot
// change an already-started round's window.
//
// Order of resolution:
// 1. the mode's declared window, when it has an opinion (> 0)
// 2. durationSeconds in the config envelope, for a mode that predates RoundWindow
// 3. the service default
func (s *Service) collectingWindowMS(r Round) int64 {
	if r.ModeID == "bluff" {
		writing, _, err := bluff.Windows(modeConfigOf(r.Config))
		if err == nil {
			return writing
		}
	}
	if s.modes != nil {
		if mode, ok := s.modes.Get(r.ModeID); ok {
			if window := mode.RoundWindow(modeConfigOf(r.Config)); window > 0 {
				if preview, ok := mode.(modes.TimedPreview); ok {
					window -= preview.PreviewWindow(modeConfigOf(r.Config))
				}
				return window.Milliseconds()
			}
		}
	}
	// The stored config is the whole round-create envelope, not the mode config alone:
	// {"modeId": "...", "config": {"durationSeconds": 3}}. Reading durationSeconds off
	// the top level therefore never matched, every round silently fell back to the
	// service default, and a three-second round ran for five minutes. Both shapes are
	// accepted so an older row that stored the mode config directly still works.
	var outer struct {
		DurationSeconds int `json:"durationSeconds"`
		Config          *struct {
			DurationSeconds int `json:"durationSeconds"`
		} `json:"config"`
	}
	if len(r.Config) > 0 {
		_ = json.Unmarshal(r.Config, &outer)
	}
	if outer.Config != nil && outer.Config.DurationSeconds > 0 {
		return int64(outer.Config.DurationSeconds) * 1000
	}
	if outer.DurationSeconds > 0 {
		return int64(outer.DurationSeconds) * 1000
	}
	return s.cfg.DefaultRoundDuration.Milliseconds()
}

func (s *Service) previewWindowMS(r Round) int64 {
	if s.modes != nil {
		if mode, ok := s.modes.Get(r.ModeID); ok {
			if preview, ok := mode.(modes.TimedPreview); ok {
				return preview.PreviewWindow(modeConfigOf(r.Config)).Milliseconds()
			}
		}
	}
	return 0
}

// modeConfigOf extracts the mode config from a stored round config envelope.
//
// A round stores {"modeId": ..., "config": {...}}; older rows stored the mode
// config directly. Modes expect the inner object, so both shapes are unwrapped
// here rather than in every mode.
// turnWindowMS reports how long one TURN of this round may run, or 0 when the
// mode is not turn-based.
//
// This is deliberately separate from collectingWindowMS: a turn-based mode has
// two clocks. The round clock (RoundWindow) bounds the whole round; the turn
// clock bounds one turn and is what "每回合 20 秒" on screen actually refers to.
// Before this existed the room layer only had the round clock, so a race where
// nobody submitted stayed on turn 1 for the full 160 seconds.
func (s *Service) turnWindowMS(r Round) int64 {
	if s.modes == nil {
		return 0
	}
	mode, ok := s.modes.Get(r.ModeID)
	if !ok {
		return 0
	}
	window := mode.TurnWindow(modeConfigOf(r.Config))
	if window <= 0 {
		return 0
	}
	return window.Milliseconds()
}

func modeConfigOf(stored json.RawMessage) json.RawMessage {
	if len(stored) == 0 {
		return nil
	}
	var envelope struct {
		Config json.RawMessage `json:"config"`
	}
	if err := json.Unmarshal(stored, &envelope); err == nil && len(envelope.Config) > 0 {
		return envelope.Config
	}
	return stored
}

// settleRoundQ computes and stores results for a terminating round.
//
// A mode that cannot settle (not registered, or returns an error) must not
// produce fabricated scores: the whole transaction fails and the round stays in
// its previous phase, so the operator can retry or cancel explicitly.
func (s *Service) settleRoundQ(ctx context.Context, tx *sql.Tx, r Round, phase modes.Phase, now int64) error {
	if phase == modes.PhaseCancelled {
		// Cancellation voids the round. It writes no scores; existing results, if
		// any, are left untouched rather than silently zeroed.
		return nil
	}
	if s.modes == nil {
		return problem.NotImplemented("settlement-unavailable",
			"no game mode is registered, so the round cannot be settled")
	}
	mode, ok := s.modes.Get(r.ModeID)
	if !ok {
		return problem.NotImplemented("settlement-unavailable",
			fmt.Sprintf("mode %s is not registered, so the round cannot be settled", r.ModeID))
	}
	publicState, err := roundPublicStateQ(ctx, tx, r.ID)
	if err != nil {
		return err
	}
	privateState, err := roundPrivateStateQ(ctx, tx, r.ID)
	if err != nil {
		return err
	}
	states, err := participantStatesQ(ctx, tx, r.ID)
	if err != nil {
		return err
	}
	scores, err := mode.Settle(modes.SettleInput{
		Scope:   r.Scope(),
		Phase:   phase,
		Public:  publicState,
		Private: privateState,
		States:  states,
	})
	if err != nil {
		var reason interface{ ReasonCode() string }
		if errors.As(err, &reason) && reason.ReasonCode() == "audio-playback-review-required" {
			return problem.Conflict(reason.ReasonCode(), "reported media failure requires host cancellation and a replacement round")
		}
		return problem.Unprocessable("settlement-failed",
			fmt.Sprintf("the mode could not settle this round: %s", err.Error()))
	}
	// A surrender voids that subject's score, whatever the mode.
	//
	// The surrender endpoint records {"surrendered": true} in the participant state, but
	// no mode read it: a player could press surrender and then keep guessing, and still
	// collect a full score. Fixing that per mode would mean fifteen copies of the same
	// rule and a mode added later would silently miss it, so the rule lives here where
	// every mode's scores pass through.
	//
	// The explanation is annotated so a player can see why they scored zero rather than
	// being left to guess.
	for i := range scores {
		if !isSurrendered(states[scores[i].Subject.ID]) {
			continue
		}
		scores[i].ScoreUnits = 0
		scores[i].Explanation = annotateSurrender(scores[i].Explanation)
	}
	ranked := modes.RankScores(scores)
	for _, sc := range ranked {
		if err := sc.Validate(); err != nil {
			return problem.Internal(fmt.Errorf("room: invalid score from mode: %w", err))
		}
		explanation := sc.Explanation
		if len(explanation) == 0 {
			explanation = json.RawMessage("{}")
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO round_results (round_id, subject_id, score_units, max_score_units,
			                           explanation_json, settled_version, subject_kind)
			VALUES (?, ?, ?, ?, ?, ?, ?)
			ON CONFLICT (round_id, subject_id) DO UPDATE SET
				score_units = excluded.score_units,
				max_score_units = excluded.max_score_units,
				explanation_json = excluded.explanation_json,
				settled_version = excluded.settled_version,
				subject_kind = excluded.subject_kind`,
			r.ID, sc.Subject.ID, sc.ScoreUnits, sc.MaxScoreUnits,
			string(explanation), r.PhaseVersion+1, string(sc.Subject.Kind)); err != nil {
			return problem.Internal(fmt.Errorf("room: insert result: %w", err))
		}
	}

	// Copy the round's scores into the event segment it belongs to.
	//
	// The standings endpoint reads segment_results, and until this existed nothing
	// ever wrote to that table: a settled round stored round_results, the segment
	// flipped to completed, and the event total stayed empty. Doing it in the same
	// transaction is what makes "segment completed" and "scores visible" atomic.
	//
	// Rounds outside an event have no segment and are skipped.
	if err := s.copyResultsToSegmentQ(ctx, tx, r.ID); err != nil {
		return err
	}
	return nil
}

// copyResultsToSegmentQ aggregates immutable round results using the fixed
// programme budget and round count, rather than replacing the previous round.
func (s *Service) copyResultsToSegmentQ(ctx context.Context, tx *sql.Tx, roundID string) error {
	return s.aggregateSegmentResultsQ(ctx, tx, roundID)
}

// participantStatesQ reads every subject's participant state for a round.
func participantStatesQ(ctx context.Context, q interface {
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
}, roundID string) (map[string]json.RawMessage, error) {
	rows, err := q.QueryContext(ctx,
		`SELECT subject_id, state_json FROM participant_round_state WHERE round_id = ?`, roundID)
	if err != nil {
		return nil, problem.Internal(fmt.Errorf("room: read participant states: %w", err))
	}
	defer rows.Close()
	out := map[string]json.RawMessage{}
	for rows.Next() {
		var (
			id   string
			body string
		)
		if err := rows.Scan(&id, &body); err != nil {
			return nil, problem.Internal(err)
		}
		out[id] = json.RawMessage(body)
	}
	return out, rows.Err()
}

// RoundResults reads the settled results of a round.
func (s *Service) RoundResults(ctx context.Context, roundID string) ([]StoredResult, error) {
	rows, err := s.db.Read().QueryContext(ctx, `
		SELECT round_id, subject_id, score_units, max_score_units, explanation_json, settled_version, subject_kind
		FROM round_results WHERE round_id = ?`, roundID)
	if err != nil {
		return nil, problem.Internal(fmt.Errorf("room: read results: %w", err))
	}
	defer rows.Close()
	var out []StoredResult
	for rows.Next() {
		var (
			r           StoredResult
			explanation string
		)
		if err := rows.Scan(&r.RoundID, &r.SubjectID, &r.ScoreUnits, &r.MaxScoreUnits,
			&explanation, &r.SettledVersion, &r.SubjectKind); err != nil {
			return nil, problem.Internal(err)
		}
		r.Explanation = json.RawMessage(explanation)
		out = append(out, r)
	}
	return out, rows.Err()
}

// StoredResult is one persisted round result.
type StoredResult struct {
	RoundID        string
	SubjectID      string
	SubjectKind    modes.SubjectKind
	ScoreUnits     int
	MaxScoreUnits  int
	Explanation    json.RawMessage
	SettledVersion int64
}

// nullableInt converts an optional int64 into a driver value.
func nullableInt(v *int64) any {
	if v == nil {
		return nil
	}
	return *v
}

// nullableString converts an optional string into a driver value.
func nullableString(v string) any {
	if strings.TrimSpace(v) == "" {
		return nil
	}
	return v
}

// roundResultExists reports whether results were already written.
func roundResultExists(ctx context.Context, tx *sql.Tx, roundID string) (bool, error) {
	var n int
	err := tx.QueryRowContext(ctx,
		`SELECT COUNT(1) FROM round_results WHERE round_id = ?`, roundID).Scan(&n)
	if err != nil {
		return false, problem.Internal(fmt.Errorf("room: check results: %w", err))
	}
	return n > 0, nil
}

// ErrSettlementUnavailable is used when a mode cannot settle yet.
var ErrSettlementUnavailable = errors.New("room: settlement unavailable")

// syncSegmentStatusQ mirrors a round's phase onto the event segment bound to it.
//
// A segment is only touched when its round_id matches, so rounds outside an event
// are unaffected. The mapping is deliberately narrow: only phases that change what
// a host would do next are mirrored.
func (s *Service) syncSegmentStatusQ(
	ctx context.Context, tx *sql.Tx, roundID string, phase modes.Phase, now int64,
) error {
	if phase != modes.PhaseCollecting && phase != modes.PhaseSettled && phase != modes.PhaseCancelled {
		return nil
	}
	var id, eventID, raw string
	var settled int
	err := tx.QueryRowContext(ctx, `SELECT es.id,es.event_id,es.config_json,
        (SELECT COUNT(*) FROM event_segment_rounds sr JOIN rounds r ON r.id=sr.round_id WHERE sr.segment_id=es.id AND r.status='settled')
        FROM event_segments es WHERE es.round_id=?`, roundID).Scan(&id, &eventID, &raw, &settled)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return problem.Internal(err)
	}
	cfg, err := readSegmentRules([]byte(raw))
	if err != nil {
		return err
	}
	next := "running"
	if phase == modes.PhaseCancelled {
		next = "voided"
	} else if phase == modes.PhaseSettled && settled >= cfg.RoundCount {
		next = "completed"
	}
	if _, err := tx.ExecContext(ctx, `UPDATE event_segments SET status=? WHERE id=?`, next, id); err != nil {
		return problem.Internal(err)
	}
	_, err = bumpEventVersion(ctx, tx, eventID, now)
	return err
}

// isSurrendered reports whether a participant state blob records a surrender.
func isSurrendered(raw json.RawMessage) bool {
	if len(raw) == 0 {
		return false
	}
	var s struct {
		Surrendered bool `json:"surrendered"`
	}
	if err := json.Unmarshal(raw, &s); err != nil {
		return false
	}
	return s.Surrendered
}

// annotateSurrender adds a surrendered flag to a mode's explanation object.
//
// If the explanation is not a JSON object the original is returned unchanged: inventing a
// shape here would corrupt a mode's own explanation format.
func annotateSurrender(explanation json.RawMessage) json.RawMessage {
	if len(explanation) == 0 {
		return json.RawMessage(`{"surrendered":true}`)
	}
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(explanation, &obj); err != nil {
		return explanation
	}
	obj["surrendered"] = json.RawMessage("true")
	merged, err := json.Marshal(obj)
	if err != nil {
		return explanation
	}
	return merged
}

// Legacy rapport states have no live windows and keep their historical clock.
func hasUnfinishedWindows(modeID string, public json.RawMessage) (bool, error) {
	switch modeID {
	case "strategy-race":
		_, more, err := strategyrace.New().OpenTurn(public)
		return more, err
	case "rapport":
		_, more, err := rapport.New().OpenTurn(public)
		return more, err
	case "trade":
		return trade.Unfinished(public)
	default:
		return false, nil
	}
}
