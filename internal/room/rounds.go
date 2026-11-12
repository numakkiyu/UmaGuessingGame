package room

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/bhcn/umaguessinggame/internal/httpapi/problem"
	"github.com/bhcn/umaguessinggame/internal/modes"
)

// timeNowMS is a package-level clock used by helpers that have no service
// receiver. Tests that need determinism go through Service.SetClock and the
// service methods, not these helpers.
func timeNowMS() int64 { return time.Now().UTC().UnixMilli() }

// RoundCreateInput is the command to prepare a round.
type RoundCreateInput struct {
	ModeConfig json.RawMessage
	SegmentID  string
}

// CreateRound prepares a version-bound round without starting it.
// Mode preparation keeps private state server-side; an unregistered mode returns 501.
func (s *Service) CreateRound(ctx context.Context, actor Actor, in RoundCreateInput) (Round, error) {
	if err := requireAnyCapability(actor, "only a host or room manager may prepare a round",
		CapHost, CapManageRoom); err != nil {
		return Round{}, err
	}
	if s.modes == nil {
		return Round{}, problem.NotImplemented("modes-unavailable",
			"no game mode is registered on this server yet, so a round cannot be prepared")
	}
	rm, err := s.loadRoom(ctx, actor.RoomID)
	if err != nil {
		return Round{}, err
	}
	if len(in.ModeConfig) == 0 {
		if in.SegmentID != "" {
			return Round{}, problem.Validation("event segments require an explicit modeConfig")
		}
		in.ModeConfig = rm.Config.ModeConfig
	}
	if err := s.validateModeConfig(in.ModeConfig); err != nil {
		return Round{}, err
	}
	effectiveConfig, err := resolveModeConfig(in.ModeConfig)
	if err != nil {
		return Round{}, problem.Internal(err) // validateModeConfig already checked it.
	}
	var probe struct {
		ModeID string `json:"modeId"`
	}
	if err := json.Unmarshal(in.ModeConfig, &probe); err != nil {
		return Round{}, problem.Validation("modeConfig is not a valid object",
			problem.FieldError{Field: "modeConfig", Message: "must be a JSON object"})
	}
	mode, ok := s.modes.Get(probe.ModeID)
	if !ok {
		return Round{}, problem.Validation("unknown mode",
			problem.FieldError{Field: "modeConfig.modeId", Message: "mode is not registered"})
	}

	room := rm
	form := room.Config.Kind
	if in.SegmentID != "" {
		form = "event"
	}
	if err := s.checkModeForm(in.ModeConfig, form); err != nil {
		return Round{}, err
	}
	if in.SegmentID != "" {
		if _, err := s.validateSegmentRoundQ(ctx, s.db.Read(), room.ID, in, nil); err != nil {
			return Round{}, err
		}
	}
	// Which room statuses may host a new round.
	//
	// finished is included on purpose: per a finished room is between rounds,
	// not spent, and a rematch or the next event segment starts a new round in it
	// without overwriting the previous result. Excluding it broke every rematch and
	// every multi-segment event.
	//
	// The list is a whitelist rather than a blacklist so a status added later is
	// refused by default instead of silently inheriting the right to start rounds.
	switch room.Status {
	case StatusLobby, StatusReady, StatusPlaying, StatusFinished:
	default:
		return Round{}, problem.Conflict("room-closed",
			fmt.Sprintf("the room is %s and cannot start a round", room.Status))
	}

	// Participants must exist before a round can be bound to them.
	members, err := s.ListMembers(ctx, room.ID)
	if err != nil {
		return Round{}, err
	}
	var subjects []modes.Subject
	var participants []modes.Participant
	seenTeams := make(map[string]bool)
	captains := make(map[string]int)
	for _, m := range members {
		if m.Role == RoleTeamCaptain {
			captains[m.TeamID]++
		}
	}
	for _, m := range members {
		if m.Role != RolePlayer && m.Role != RoleTeamCaptain {
			continue
		}
		if m.Role == RoleTeamCaptain && m.TeamID == "" {
			// A captain without a team cannot be a subject, and silently dropping them
			// produces a confusing "not enough participants" that names no cause. Refuse
			// with the actual reason instead: the room is misconfigured, not empty.
			return Round{}, problem.Unprocessable("captain-without-team",
				fmt.Sprintf("member %s is a team captain but has no team; assign a team or change the role before starting",
					m.ID))
		}
		if m.TeamID != "" && !s.individualSeats(mode.ID()) {
			participants = append(participants, modes.Participant{SessionID: m.SessionID, MembershipID: m.ID, TeamID: m.TeamID, Subject: modes.Subject{ID: m.TeamID, Kind: modes.SubjectTeam}})
			if captains[m.TeamID] != 1 {
				return Round{}, problem.Unprocessable("team-captain-required", "each competing team needs exactly one captain")
			}
			if seenTeams[m.TeamID] {
				continue
			}
			seenTeams[m.TeamID] = true
			subjects = append(subjects, modes.Subject{ID: m.TeamID, Kind: modes.SubjectTeam})
			continue
		}
		subjects = append(subjects, modes.Subject{ID: m.ID, Kind: modes.SubjectPlayer})
		participants = append(participants, modes.Participant{SessionID: m.SessionID, MembershipID: m.ID, TeamID: m.TeamID, Subject: modes.Subject{ID: m.ID, Kind: modes.SubjectPlayer}})
	}
	if validator, ok := mode.(modes.RosterValidator); ok {
		if err := validator.ValidateRoster(subjects, participants); err != nil {
			code := "team-roster-invalid"
			if mode.ID() == "rapport" {
				code = "rapport-roster-invalid"
			}
			return Round{}, problem.Unprocessable(code, err.Error())
		}
	} else if len(subjects) < mode.MinSubjects() {
		return Round{}, problem.Unprocessable("not-enough-participants",
			fmt.Sprintf("mode %s needs at least %d participants, found %d",
				mode.ID(), mode.MinSubjects(), len(subjects)))
	}
	if _, custom := mode.(modes.RosterValidator); !custom && len(subjects) > mode.MaxSubjects() {
		return Round{}, problem.Unprocessable("too-many-participants",
			fmt.Sprintf("mode %s allows at most %d participants, found %d",
				mode.ID(), mode.MaxSubjects(), len(subjects)))
	}
	scoringSubjects := subjects
	if scoring, ok := mode.(modes.ScoringRoster); ok {
		scoringSubjects, err = scoring.ScoringSubjects(participants)
		if err != nil {
			return Round{}, problem.Unprocessable("rapport-roster-invalid", err.Error())
		}
	}
	if in.SegmentID != "" {
		if _, err := s.validateSegmentRoundQ(ctx, s.db.Read(), room.ID, in, scoringSubjects); err != nil {
			return Round{}, err
		}
	}

	seed, err := newSeed()
	if err != nil {
		return Round{}, problem.Internal(err)
	}
	scope := modes.Scope{
		ModeID:             mode.ID(),
		RulesVersion:       mode.RulesVersion(),
		DatasetVersion:     s.datasetVersion(),
		ContentPackVersion: s.contentVersion(),
		Seed:               seed,
	}

	// Prepare runs outside the write transaction: it may be expensive and the
	// transaction must stay short . The version is re-checked below.
	prepared, err := mode.Prepare(ctx, modes.PrepareInput{
		Scope:        scope,
		Subject:      subjects,
		Participants: participants,
		Config:       effectiveConfig,
		Content:      s.content,
	})
	if err != nil {
		return Round{}, problem.Unprocessable("round-preparation-failed",
			fmt.Sprintf("the mode could not prepare a round: %s", err.Error()))
	}

	storedConfig := effectiveConfig
	if len(prepared.Config) > 0 {
		var envelope map[string]json.RawMessage
		if err := json.Unmarshal(storedConfig, &envelope); err != nil {
			return Round{}, problem.Internal(err)
		}
		envelope["config"] = prepared.Config
		storedConfig = marshal(envelope)
	}

	roundID, err := newID("rnd_")
	if err != nil {
		return Round{}, problem.Internal(err)
	}
	now := s.nowMS()

	tx, err := s.db.Write().BeginTx(ctx, nil)
	if err != nil {
		return Round{}, problem.Internal(fmt.Errorf("room: begin create round: %w", err))
	}
	defer tx.Rollback()
	if err := validateControllerQ(ctx, tx, actor); err != nil {
		return Round{}, err
	}
	// Preparation must not bind an obsolete roster or room configuration.
	var liveVersion int64
	if err := tx.QueryRowContext(ctx, `SELECT room_version FROM rooms WHERE id = ?`, room.ID).Scan(&liveVersion); err != nil {
		return Round{}, problem.Internal(err)
	}
	if liveVersion != room.RoomVersion {
		return Round{}, problem.Conflict("room-version-conflict", "the room changed while preparing this round; reload before starting")
	}

	// Re-check the invariants inside the transaction: a concurrent round may
	// have been created while Prepare was running.
	live, err := s.currentRoundQ(ctx, tx, room.ID)
	if err != nil {
		return Round{}, err
	}
	if live != nil {
		return Round{}, problem.Conflict("round-already-open",
			"the room already has an unfinished round; finish or cancel it first")
	}
	segmentOrdinal := 0
	if in.SegmentID != "" {
		segmentOrdinal, err = s.validateSegmentRoundQ(ctx, tx, room.ID, in, scoringSubjects)
		if err != nil {
			return Round{}, err
		}
	}
	var maxOrdinal sql.NullInt64
	if err := tx.QueryRowContext(ctx,
		`SELECT MAX(ordinal) FROM rounds WHERE room_id = ?`, room.ID).Scan(&maxOrdinal); err != nil {
		return Round{}, problem.Internal(fmt.Errorf("room: read ordinal: %w", err))
	}
	ordinal := maxOrdinal.Int64 + 1

	if _, err := tx.ExecContext(ctx, `
		INSERT INTO rounds (id, room_id, ordinal, mode_id, rules_version, dataset_version,
		                    content_pack_version, status, phase_version, seed, config_json,
		                    created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, 'prepared', 1, ?, ?, ?, ?)`,
		roundID, room.ID, ordinal, scope.ModeID, scope.RulesVersion, scope.DatasetVersion,
		scope.ContentPackVersion, seed, string(storedConfig), now, now); err != nil {
		return Round{}, problem.Internal(fmt.Errorf("room: insert round: %w", err))
	}

	// Private state is server-only. It is never part of any DTO.
	if len(prepared.Private) > 0 {
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO round_private_state (round_id, scope, state_json) VALUES (?, 'round', ?)`,
			roundID, string(prepared.Private)); err != nil {
			return Round{}, problem.Internal(fmt.Errorf("room: insert private state: %w", err))
		}
	}
	// Per-subject private state. Each row is readable only by its own subject, so a
	// trade player's objectives never reach another player. Without this the round-level
	// Private blob was the only place to put them, and since it is written once and
	// shared, projection had nothing to give an individual viewer.
	if len(prepared.SubjectPrivate) > 0 {
		subjects := make([]string, 0, len(prepared.SubjectPrivate))
		for subjectID := range prepared.SubjectPrivate {
			subjects = append(subjects, subjectID)
		}
		sort.Strings(subjects)
		for _, subjectID := range subjects {
			state := prepared.SubjectPrivate[subjectID]
			if len(state) == 0 {
				continue
			}
			if _, err := tx.ExecContext(ctx,
				`INSERT INTO participant_round_state (round_id, subject_id, subject_kind, state_json, updated_at)
				 VALUES (?, ?, 'player', ?, ?)`,
				roundID, subjectID, string(state), now); err != nil {
				return Round{}, problem.Internal(fmt.Errorf("room: insert participant state: %w", err))
			}
		}
	}
	if len(prepared.Public) > 0 {
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO round_private_state (round_id, scope, state_json) VALUES (?, 'public', ?)`,
			roundID, string(prepared.Public)); err != nil {
			return Round{}, problem.Internal(fmt.Errorf("room: insert public state: %w", err))
		}
	}

	// Link the round to its event segment, if one was supplied.
	if in.SegmentID != "" {
		roster, err := json.Marshal(scoringSubjects)
		if err != nil {
			return Round{}, problem.Internal(err)
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO event_segment_rounds(segment_id,round_id,ordinal,roster_json) VALUES(?,?,?,?)`, in.SegmentID, roundID, segmentOrdinal, string(roster)); err != nil {
			return Round{}, problem.Internal(err)
		}
		if _, err := tx.ExecContext(ctx,
			`UPDATE event_segments SET round_id = ?,status='running' WHERE id = ?`, roundID, in.SegmentID); err != nil {
			return Round{}, problem.Internal(fmt.Errorf("room: link segment: %w", err))
		}
		if _, err := tx.ExecContext(ctx, `UPDATE event_sessions SET event_version=event_version+1,updated_at=? WHERE id=(SELECT event_id FROM event_segments WHERE id=?)`, now, in.SegmentID); err != nil {
			return Round{}, problem.Internal(err)
		}
	}

	if _, err := tx.ExecContext(ctx,
		`UPDATE rooms SET status = 'playing', updated_at = ? WHERE id = ? AND status IN ('lobby','ready')`,
		now, room.ID); err != nil {
		return Round{}, problem.Internal(fmt.Errorf("room: set playing: %w", err))
	}
	version, err := bumpRoomVersion(ctx, tx, room.ID, now)
	if err != nil {
		return Round{}, err
	}
	if err := emitOutbox(ctx, tx, room.ID, "state.changed", map[string]any{
		"resource": "round", "roundId": roundID, "roomVersion": version,
	}, now); err != nil {
		return Round{}, err
	}
	if err := tx.Commit(); err != nil {
		return Round{}, problem.Internal(fmt.Errorf("room: commit create round: %w", err))
	}
	return s.loadRound(ctx, roundID)
}

// newSeed returns a cryptographically random seed.
//
// Seeds come from a secure source; a predictable seed would let a client
// precompute answers .
func newSeed() (string, error) {
	id, err := newID("")
	if err != nil {
		return "", err
	}
	return id, nil
}

func (s *Service) datasetVersion() string {
	if s.content == nil {
		return "unknown"
	}
	return s.content.DatasetVersion()
}

func (s *Service) contentVersion() string {
	if s.content == nil {
		return "unknown"
	}
	return s.content.Version()
}

// loadRound reads a round by id.
func (s *Service) loadRound(ctx context.Context, roundID string) (Round, error) {
	return s.loadRoundQ(ctx, s.db.Read(), roundID)
}

func (s *Service) loadRoundQ(ctx context.Context, q queryer, roundID string) (Round, error) {
	row := q.QueryRowContext(ctx, `
		SELECT id, room_id, ordinal, mode_id, rules_version, dataset_version,
		       content_pack_version, status, phase_version, deadline_at, paused_at,
		       paused_remaining_ms, paused_previous_phase, seed, config_json,
		       created_at, updated_at
		FROM rounds WHERE id = ?`, roundID)
	r, err := scanRound(row)
	if errors.Is(err, sql.ErrNoRows) {
		return Round{}, errRoundNotFound
	}
	if err != nil {
		return Round{}, problem.Internal(fmt.Errorf("room: load round: %w", err))
	}
	return *r, nil
}

// LoadRoundWithActor resolves the round, the owning room and the caller's actor.
func (s *Service) LoadRoundWithActor(ctx context.Context, sessionID, roundID string) (Round, Room, Actor, error) {
	r, err := s.loadRound(ctx, roundID)
	if err != nil {
		return Round{}, Room{}, Actor{}, err
	}
	room, actor, err := s.GetRoom(ctx, sessionID, r.RoomID)
	if err != nil {
		return Round{}, Room{}, Actor{}, err
	}
	return r, room, actor, nil
}

// roundPublicState reads the stored public question state.
func roundPublicStateQ(ctx context.Context, q queryer, roundID string) (json.RawMessage, error) {
	var body string
	err := q.QueryRowContext(ctx,
		`SELECT state_json FROM round_private_state WHERE round_id = ? AND scope = 'public'`,
		roundID).Scan(&body)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, problem.Internal(fmt.Errorf("room: read public state: %w", err))
	}
	return json.RawMessage(body), nil
}

// roundPrivateState reads the server-only secret state.
//
// The result must never be placed into a response DTO. It is only handed to a
// mode's Apply/Project, which decides what the viewer may see.
func roundPrivateStateQ(ctx context.Context, q queryer, roundID string) (json.RawMessage, error) {
	var body string
	err := q.QueryRowContext(ctx,
		`SELECT state_json FROM round_private_state WHERE round_id = ? AND scope = 'round'`,
		roundID).Scan(&body)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, problem.Internal(fmt.Errorf("room: read private state: %w", err))
	}
	return json.RawMessage(body), nil
}
