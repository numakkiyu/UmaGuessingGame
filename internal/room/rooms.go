package room

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"strings"

	"github.com/bhcn/umaguessinggame/internal/httpapi/problem"
	"github.com/bhcn/umaguessinggame/internal/modes"
)

// RoomCreateInput is the validated command to create a room.
type RoomCreateInput struct {
	Command         *CreationCommand
	Name            string
	Kind            string
	ModeConfig      json.RawMessage
	MaxPlayers      int
	MaxSpectators   int
	Visibility      string
	AllowSpectators bool
}

// RoomCreateResult carries the created room and its invitation.
type RoomCreateResult struct {
	Room       Room
	InviteCode string
}

// createRoomKinds are the room kinds the contract accepts.
var createRoomKinds = map[string]bool{
	"solo": true, "friend": true, "matchmaking": true, "event": true,
}

// createRoomVisibilities are the contract visibility values.
var createRoomVisibilities = map[string]bool{"invite": true, "public": true}

// CreateRoom creates a room, its owner membership and its manage-room grant in
// one transaction.
//
// The creator becomes a player by default and simultaneously holds manage-room
// authority. Those are two separate facts : an owner must be able to
// play, and being a player must never confer management.
func (s *Service) CreateRoom(ctx context.Context, sessionID string, in RoomCreateInput) (RoomCreateResult, error) {
	if in.Command != nil {
		if id, err := createdResource(ctx, s.db.Read(), "rooms", "creator_session_id", sessionID, in.Command); err != nil {
			return RoomCreateResult{}, err
		} else if id != "" {
			rm, err := s.loadRoom(ctx, id)
			return RoomCreateResult{Room: rm, InviteCode: rm.InviteCode}, err
		}
	}

	if err := validateRoomCreate(in); err != nil {
		return RoomCreateResult{}, err
	}
	if err := s.validateModeConfig(in.ModeConfig); err != nil {
		return RoomCreateResult{}, err
	}

	if err := s.checkModeForm(in.ModeConfig, in.Kind); err != nil {
		return RoomCreateResult{}, err
	}
	if in.Kind == "matchmaking" {
		return RoomCreateResult{}, problem.Forbidden("matchmaking-reserved", "use matchmaking to enter this room type")
	}
	limits := s.cfg.Policy.Limits
	if limits.PlayersPerRoom > 0 && in.MaxPlayers > limits.PlayersPerRoom || limits.SpectatorsPerRoom > 0 && in.MaxSpectators > limits.SpectatorsPerRoom {
		return RoomCreateResult{}, problem.Validation("room capacity exceeds this instance's configured limit")
	}

	roomID, err := newID("rom_")
	if err != nil {
		return RoomCreateResult{}, problem.Internal(err)
	}
	membershipID, err := newID("mem_")
	if err != nil {
		return RoomCreateResult{}, problem.Internal(err)
	}
	grantID, err := newID("grt_")
	if err != nil {
		return RoomCreateResult{}, problem.Internal(err)
	}
	inviteCode, err := s.generateInviteCode()
	if err != nil {
		return RoomCreateResult{}, err
	}

	cfg := RoomConfig{
		Kind:            in.Kind,
		ModeConfig:      in.ModeConfig,
		MaxPlayers:      in.MaxPlayers,
		MaxSpectators:   in.MaxSpectators,
		Visibility:      in.Visibility,
		AllowSpectators: in.AllowSpectators,
	}
	cfgJSON, err := json.Marshal(cfg)
	if err != nil {
		return RoomCreateResult{}, problem.Internal(fmt.Errorf("room: encode config: %w", err))
	}

	now := s.nowMS()
	// Capacity bounds the total participant count the schema enforces.
	capacity := in.MaxPlayers + in.MaxSpectators
	if capacity <= 0 {
		capacity = 1
	}
	if capacity > 500 {
		capacity = 500
	}

	tx, err := s.db.Write().BeginTx(ctx, nil)
	if err != nil {
		return RoomCreateResult{}, problem.Internal(fmt.Errorf("room: begin create: %w", err))
	}
	defer tx.Rollback()
	if id, err := createdResource(ctx, tx, "rooms", "creator_session_id", sessionID, in.Command); err != nil {
		return RoomCreateResult{}, err
	} else if id != "" {
		rm, err := s.loadRoomQ(ctx, tx, id)
		return RoomCreateResult{Room: rm, InviteCode: rm.InviteCode}, err
	}
	creationKey, creationHash, err := in.Command.identity()
	if err != nil {
		return RoomCreateResult{}, err
	}

	if _, err := tx.ExecContext(ctx, `
		INSERT INTO rooms (id, invite_code, owner_session_id, name, status, room_version,
		                   config_json, capacity, created_at, updated_at,creation_key,creation_hash)
		VALUES (?, ?, ?, ?, 'lobby', 1, ?, ?, ?, ?,?,?)`,
		roomID, inviteCode, sessionID, in.Name, string(cfgJSON), capacity, now, now, creationKey, creationHash); err != nil {
		return RoomCreateResult{}, problem.Internal(fmt.Errorf("room: insert room: %w", err))
	}

	// Event organisers operate the programme rather than silently becoming an
	// extra contestant. Ordinary rooms retain their playing owner.
	ownerRole := "player"
	if in.Kind == "event" {
		ownerRole = "host"
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO memberships (id, room_id, session_id, role, team_id, ready, joined_at)
		VALUES (?, ?, ?, ?, NULL, 0, ?)`,
		membershipID, roomID, sessionID, ownerRole, now); err != nil {
		return RoomCreateResult{}, problem.Internal(fmt.Errorf("room: insert owner membership: %w", err))
	}

	// Management authority is recorded separately from the participation role.
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO room_grants (id, room_id, session_id, capability, granted_at)
		VALUES (?, ?, ?, 'manage-room', ?)`,
		grantID, roomID, sessionID, now); err != nil {
		return RoomCreateResult{}, problem.Internal(fmt.Errorf("room: insert owner grant: %w", err))
	}

	if err := emitOutbox(ctx, tx, roomID, "state.changed", map[string]any{
		"resource": "room", "roomVersion": 1,
	}, now); err != nil {
		return RoomCreateResult{}, err
	}
	if err := tx.Commit(); err != nil {
		return RoomCreateResult{}, problem.Internal(fmt.Errorf("room: commit create: %w", err))
	}

	room, err := s.loadRoom(ctx, roomID)
	if err != nil {
		return RoomCreateResult{}, err
	}
	return RoomCreateResult{Room: room, InviteCode: inviteCode}, nil
}

func validateRoomCreate(in RoomCreateInput) error {
	var fields []problem.FieldError
	if in.Kind == "solo" && (in.MaxPlayers != 1 || in.MaxSpectators != 0 || in.AllowSpectators) {
		fields = append(fields, problem.FieldError{Field: "kind", Message: "solo play has exactly one player and no spectators; choose friend or event for shared play"})
	}
	name := strings.TrimSpace(in.Name)
	if n := len([]rune(name)); n < 1 || n > 48 {
		fields = append(fields, problem.FieldError{Field: "name", Message: "must be 1..48 characters"})
	}
	if !createRoomKinds[in.Kind] {
		fields = append(fields, problem.FieldError{Field: "kind", Message: "must be one of solo, friend, matchmaking, event"})
	}
	if in.MaxPlayers < 1 || in.MaxPlayers > 200 {
		fields = append(fields, problem.FieldError{Field: "maxPlayers", Message: "must be 1..200"})
	}
	if in.MaxSpectators < 0 || in.MaxSpectators > 300 {
		fields = append(fields, problem.FieldError{Field: "maxSpectators", Message: "must be 0..300"})
	}
	if !createRoomVisibilities[in.Visibility] {
		fields = append(fields, problem.FieldError{Field: "visibility", Message: "must be invite or public"})
	}
	if len(fields) > 0 {
		return problem.Validation("the room configuration is invalid", fields...)
	}
	return nil
}

// validateModeConfig rejects a mode configuration the registry does not know or
// the mode itself refuses.
func (s *Service) validateModeConfig(raw json.RawMessage) error {
	if len(raw) == 0 {
		return problem.Validation("modeConfig is required",
			problem.FieldError{Field: "modeConfig", Message: "must be present"})
	}
	resolved, err := resolveModeConfig(raw)
	if err != nil {
		return problem.Validation("modeConfig is not a consistent rule object",
			problem.FieldError{Field: "modeConfig", Message: err.Error()})
	}
	raw = resolved
	var probe struct {
		ModeID string `json:"modeId"`
		// InformationStyle is the shared open/closed switch every pack-backed
		// mode takes. It is validated here, not in each mode, because it is the
		// same two values everywhere and a mode that forgot to read it silently
		// turned an "open" round into a closed one.
		InformationStyle string `json:"informationStyle"`
	}
	if err := json.Unmarshal(raw, &probe); err != nil {
		return problem.Validation("modeConfig is not a valid object",
			problem.FieldError{Field: "modeConfig", Message: "must be a JSON object with a modeId"})
	}
	if probe.ModeID == "" {
		return problem.Validation("modeConfig.modeId is required",
			problem.FieldError{Field: "modeConfig.modeId", Message: "must be present"})
	}
	if s.modes == nil {
		// No mode is registered yet: the configuration cannot be validated, and
		// pretending otherwise would create an unplayable room.
		return problem.NotImplemented("modes-unavailable",
			"no game mode is registered on this server yet, so a room cannot be created")
	}
	m, ok := s.modes.Get(probe.ModeID)
	if !ok {
		return problem.Validation("unknown mode",
			problem.FieldError{Field: "modeConfig.modeId", Message: fmt.Sprintf("mode %q is not registered", probe.ModeID)})
	}
	// Empty means "the mode's own default" (closed for every pack-backed mode).
	switch probe.InformationStyle {
	case "", "closed", "open":
	default:
		return problem.Validation("modeConfig.informationStyle is not a known value",
			problem.FieldError{Field: "modeConfig.informationStyle",
				Message: fmt.Sprintf("must be \"open\" or \"closed\", got %q", probe.InformationStyle)})
	}
	if err := m.ValidateConfig(raw); err != nil {
		return problem.Validation("modeConfig was rejected by the mode",
			problem.FieldError{Field: "modeConfig", Message: err.Error()})
	}
	return nil
}

// GetRoom returns the room and the caller's resolved actor.
func (s *Service) GetRoom(ctx context.Context, sessionID, roomID string) (Room, Actor, error) {
	actor, err := s.LoadActor(ctx, sessionID, roomID)
	if err != nil {
		return Room{}, Actor{}, err
	}
	room, err := s.loadRoom(ctx, roomID)
	if err != nil {
		return Room{}, Actor{}, err
	}
	return room, actor, nil
}

// RoomPatchInput is a lobby configuration update.
type RoomPatchInput struct {
	ExpectedRoomVersion int64
	Name                *string
	ModeConfig          json.RawMessage
	AllowSpectators     *bool
}

// UpdateRoom applies a lobby configuration change.
//
// Configuration is only mutable while the room is in lobby : changing the
// mode under a running round would invalidate the round's immutable binding.
func (s *Service) UpdateRoom(ctx context.Context, actor Actor, in RoomPatchInput) (Room, error) {
	if err := requireCapability(actor, CapManageRoom, "only a room manager may change the configuration"); err != nil {
		return Room{}, err
	}
	tx, err := s.db.Write().BeginTx(ctx, nil)
	if err != nil {
		return Room{}, problem.Internal(fmt.Errorf("room: begin update: %w", err))
	}
	defer tx.Rollback()
	if err := s.validateControllerQ(ctx, tx, actor); err != nil {
		return Room{}, err
	}

	room, err := s.loadRoomQ(ctx, tx, actor.RoomID)
	if err != nil {
		return Room{}, err
	}
	if room.Status != StatusLobby {
		return Room{}, problem.Conflict("room-not-editable",
			fmt.Sprintf("the room is %s; configuration may only change in lobby", room.Status))
	}
	if in.ExpectedRoomVersion != room.RoomVersion {
		return Room{}, versionConflict(room.RoomVersion)
	}

	cfg := room.Config
	if in.Name != nil {
		name := strings.TrimSpace(*in.Name)
		if n := len([]rune(name)); n < 1 || n > 48 {
			return Room{}, problem.Validation("name is invalid",
				problem.FieldError{Field: "name", Message: "must be 1..48 characters"})
		}
		room.Name = name
	}
	if len(in.ModeConfig) > 0 {
		if err := s.validateModeConfig(in.ModeConfig); err != nil {
			return Room{}, err
		}
		cfg.ModeConfig = in.ModeConfig
	}
	if in.AllowSpectators != nil {
		cfg.AllowSpectators = *in.AllowSpectators
	}
	cfgJSON, err := json.Marshal(cfg)
	if err != nil {
		return Room{}, problem.Internal(fmt.Errorf("room: encode config: %w", err))
	}

	now := s.nowMS()
	if _, err := tx.ExecContext(ctx,
		`UPDATE rooms SET name = ?, config_json = ?, updated_at = ? WHERE id = ?`,
		room.Name, string(cfgJSON), now, room.ID); err != nil {
		return Room{}, problem.Internal(fmt.Errorf("room: update room: %w", err))
	}
	newVersion, err := bumpRoomVersion(ctx, tx, room.ID, now)
	if err != nil {
		return Room{}, err
	}
	if err := emitOutbox(ctx, tx, room.ID, "state.changed", map[string]any{
		"resource": "room", "roomVersion": newVersion,
	}, now); err != nil {
		return Room{}, err
	}
	if err := tx.Commit(); err != nil {
		return Room{}, problem.Internal(fmt.Errorf("room: commit update: %w", err))
	}
	return s.loadRoom(ctx, room.ID)
}

// versionConflict builds the standard 409 for a stale expected version.
func versionConflict(current int64) *problem.Error {
	e := problem.Conflict("version-conflict",
		fmt.Sprintf("expected version does not match the current version %d", current))
	e.Fields = []problem.FieldError{{Field: "expectedRoomVersion",
		Message: fmt.Sprintf("current version is %d", current)}}
	return e
}

// phaseConflict builds the standard 409 for a stale phaseVersion.
func phaseConflict(current int64) *problem.Error {
	e := problem.Conflict("phase-version-conflict",
		fmt.Sprintf("expected phaseVersion does not match the current phaseVersion %d", current))
	e.Fields = []problem.FieldError{{Field: "phaseVersion",
		Message: fmt.Sprintf("current phaseVersion is %d", current)}}
	return e
}

// generateInviteCode produces a human-readable code that is unique among active
// rooms.
//
// The alphabet omits characters that are easy to confuse when read aloud or
// retyped (0/O, 1/I/L), because the code is dictated in a room.
func (s *Service) generateInviteCode() (string, error) {
	const alphabet = "ABCDEFGHJKMNPQRSTUVWXYZ23456789"
	n := s.cfg.InviteCodeLen
	for attempt := 0; attempt < 16; attempt++ {
		buf := make([]byte, n)
		for i := 0; i < n; i++ {
			idx, err := rand.Int(rand.Reader, big.NewInt(int64(len(alphabet))))
			if err != nil {
				return "", problem.Internal(fmt.Errorf("room: invite code: %w", err))
			}
			buf[i] = alphabet[idx.Int64()]
		}
		code := string(buf)
		var exists int
		err := s.db.Read().QueryRowContext(context.Background(),
			`SELECT COUNT(1) FROM rooms WHERE invite_code = ? AND status NOT IN ('archived','cancelled')`,
			code).Scan(&exists)
		if err != nil {
			return "", problem.Internal(fmt.Errorf("room: invite code uniqueness: %w", err))
		}
		if exists == 0 {
			return code, nil
		}
	}
	return "", problem.Busy(1).WithCause(errors.New("room: invite code space exhausted"))
}

// ResolveInvitation maps an invite code to the minimal join target.
//
// Only the room id and a joinable flag are returned: the resolution endpoint is
// reachable without a session and must not become a room-detail oracle
// . A failed resolution is a uniform 404.
func (s *Service) ResolveInvitation(ctx context.Context, code string) (roomID string, joinable bool, err error) {
	code = strings.ToUpper(strings.TrimSpace(code))
	if n := len(code); n < 6 || n > 16 {
		return "", false, problem.Validation("inviteCode is invalid",
			problem.FieldError{Field: "inviteCode", Message: "must be 6..16 characters"})
	}
	var (
		id     string
		status string
	)
	err = s.db.Read().QueryRowContext(ctx,
		`SELECT id, status FROM rooms WHERE invite_code = ?`, code).Scan(&id, &status)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, problem.NotFound("invitation-not-found", "the invitation code is not valid")
	}
	if err != nil {
		return "", false, problem.Internal(fmt.Errorf("room: resolve invitation: %w", err))
	}
	// Archived and cancelled rooms resolve but are not joinable, so the client
	// can show an accurate message without learning room details.
	return id, status != string(StatusArchived) && status != string(StatusCancelled), nil
}

// currentRound loads the single non-terminal round of a room, if any.
func (s *Service) currentRoundQ(ctx context.Context, q queryer, roomID string) (*Round, error) {
	row := q.QueryRowContext(ctx, `
		SELECT id, room_id, ordinal, mode_id, rules_version, dataset_version,
		       content_pack_version, status, phase_version, deadline_at, paused_at,
		       paused_remaining_ms, paused_previous_phase, seed, config_json,
		       created_at, updated_at
		FROM rounds
		WHERE room_id = ? AND status NOT IN ('settled','cancelled')
		ORDER BY ordinal DESC LIMIT 1`, roomID)
	r, err := scanRound(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, problem.Internal(fmt.Errorf("room: load current round: %w", err))
	}
	return r, nil
}

func scanRound(sc rowScanner) (*Round, error) {
	var (
		r                    Round
		status               string
		deadline, pausedAt   sql.NullInt64
		pausedRemaining      sql.NullInt64
		pausedPrevious       sql.NullString
		configJSON           string
		createdAt, updatedAt int64
	)
	if err := sc.Scan(&r.ID, &r.RoomID, &r.Ordinal, &r.ModeID, &r.RulesVersion,
		&r.DatasetVersion, &r.ContentPackVersion, &status, &r.PhaseVersion,
		&deadline, &pausedAt, &pausedRemaining, &pausedPrevious, &r.Seed,
		&configJSON, &createdAt, &updatedAt); err != nil {
		return nil, err
	}
	r.Status = modes.Phase(status)
	r.DeadlineAt = timePtr(deadline)
	r.PausedAt = timePtr(pausedAt)
	r.PausedRemainingMS = int64Ptr(pausedRemaining)
	r.PausedPrevious = modes.Phase(pausedPrevious.String)
	r.Config = json.RawMessage(configJSON)
	r.CreatedAt = millis(createdAt)
	r.UpdatedAt = millis(updatedAt)
	return &r, nil
}

// LatestRoundSummary returns the room's most recent round of any status.
//
// This is what the client needs to render a finished game. currentRoundQ
// deliberately excludes settled/cancelled because CreateRound uses it to decide
// whether a new round may open — reusing it for display would either hide the
// result (current behaviour: the page falls back to the room lobby, so the
// player never sees their score) or, if loosened, let a room open a second
// round on top of a finished one.
//
// Two questions, two queries.
func (s *Service) LatestRoundSummary(ctx context.Context, roomID string) (*Round, error) {
	row := s.db.Read().QueryRowContext(ctx, `
		SELECT id, room_id, ordinal, mode_id, rules_version, dataset_version,
		       content_pack_version, status, phase_version, deadline_at, paused_at,
		       paused_remaining_ms, paused_previous_phase, seed, config_json,
		       created_at, updated_at
		FROM rounds
		WHERE room_id = ?
		ORDER BY ordinal DESC LIMIT 1`, roomID)
	r, err := scanRound(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return r, nil
}

// CurrentRoundSummary returns the room's live round, or nil when none is open.
//
// It is exported so the HTTP layer can render currentRound without duplicating the
// "at most one live round" query.
func (s *Service) CurrentRoundSummary(ctx context.Context, roomID string) (*Round, error) {
	return s.currentRoundQ(ctx, s.db.Read(), roomID)
}
