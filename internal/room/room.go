// Package room authorizes membership and coordinates transactional game commands.
// Actor, team and role authority comes from stored grants, not request-body claims.
// HTTP response mapping belongs to internal/httpapi.
package room

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/bhcn/umaguessinggame/internal/deployment"
	"github.com/bhcn/umaguessinggame/internal/httpapi/problem"
	"github.com/bhcn/umaguessinggame/internal/modes"
	"github.com/bhcn/umaguessinggame/internal/storage"
)

// Capability is a coarse authorisation flag surfaced to the client.
//
// The enum matches Membership.capabilities in the contract. manage-room is the
// management authority (owner) and is orthogonal to the participation role
// ; host is the live control authority.
type Capability string

const (
	// CapManageRoom is room administration: rename, invite, kick, create teams.
	CapManageRoom Capability = "manage-room"
	// CapHost is live control: phase transitions, corrections, grants.
	CapHost Capability = "host"
	// CapPlay is participation as a scoring subject.
	CapPlay Capability = "play"
	// CapObserve is read-only participation as a spectator.
	CapObserve Capability = "observe"
	// CapDisplay is the restricted projector authority.
	CapDisplay Capability = "display"
)

// Role is the participation identity stored on a membership.
type Role string

const (
	RolePlayer      Role = "player"
	RoleTeamCaptain Role = "team-captain"
	RoleSpectator   Role = "spectator"
	RoleHost        Role = "host"
	RoleDisplay     Role = "display"
)

// Valid reports whether the role satisfies the membership CHECK constraint.
func (r Role) Valid() bool {
	switch r {
	case RolePlayer, RoleTeamCaptain, RoleSpectator, RoleHost, RoleDisplay:
		return true
	}
	return false
}

// SubjectKind returns the scoring subject kind this role competes as.
func (r Role) SubjectKind() modes.SubjectKind {
	if r == RoleTeamCaptain {
		return modes.SubjectTeam
	}
	return modes.SubjectPlayer
}

// RoomStatus is the room lifecycle state .
type RoomStatus string

const (
	StatusLobby     RoomStatus = "lobby"
	StatusReady     RoomStatus = "ready"
	StatusPlaying   RoomStatus = "playing"
	StatusFinished  RoomStatus = "finished"
	StatusArchived  RoomStatus = "archived"
	StatusCancelled RoomStatus = "cancelled"
)

// Terminal reports whether the room accepts no further lifecycle change.
//
// finished is deliberately NOT terminal. Per the room lifecycle is
// lobby → ready → playing → finished → archived, and "finished" means this round is
// over, not that the room is spent: a rematch starts a new round in the same room
// and does not overwrite the old result. Treating finished as terminal blocked every
// rematch and every multi-segment event, where one room hosts segment after segment.
//
// Only archived and cancelled end a room for good.
//
// WARNING: do not add finished to this list. It has been attempted once and it broke
// every rematch and every multi-segment event, because one room hosts segment after
// segment and each settlement sets finished. See before changing the state
// machine here.
func (s RoomStatus) Terminal() bool { return s == StatusArchived || s == StatusCancelled }

// Room is the persisted room row plus its decoded configuration.
type Room struct {
	ID          string
	InviteCode  string
	OwnerID     string
	Name        string
	Status      RoomStatus
	RoomVersion int64
	Capacity    int
	Config      RoomConfig
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// RoomConfig is the JSON blob stored in rooms.config_json.
//
// The columns do not exist individually because the mode configuration is a
// discriminated union that changes per mode; the fixed scalars are validated
// before being written and re-read through typed accessors.
type RoomConfig struct {
	Kind            string          `json:"kind"`
	ModeConfig      json.RawMessage `json:"modeConfig"`
	MaxPlayers      int             `json:"maxPlayers"`
	MaxSpectators   int             `json:"maxSpectators"`
	Visibility      string          `json:"visibility"`
	AllowSpectators bool            `json:"allowSpectators"`
}

// Membership is one participation record.
type Membership struct {
	EntryNumber int64
	PublicCode  string
	ID          string
	RoomID      string
	SessionID   string
	Role        Role
	TeamID      string
	Ready       bool
	JoinedAt    time.Time
	LeftAt      *time.Time
	RevokedAt   *time.Time
	DisplayName string
	Nickname    string
}

// Active reports whether the membership currently grants access.
func (m Membership) Active() bool { return m.LeftAt == nil && m.RevokedAt == nil }

// Team is a room team.
type Team struct {
	ID      string
	RoomID  string
	Label   string
	Color   string
	Created time.Time
}

// ControlLease is the round controller lease.
type ControlLease struct {
	ID           string
	RoomID       string
	RoundID      string
	Slot         string
	MembershipID string
	Generation   int64
	IssuedAt     time.Time
	ExpiresAt    time.Time
}

// Round is one version-bound question window.
type Round struct {
	ID                 string
	RoomID             string
	Ordinal            int64
	ModeID             string
	RulesVersion       string
	DatasetVersion     string
	ContentPackVersion string
	Status             modes.Phase
	PhaseVersion       int64
	DeadlineAt         *time.Time
	PausedAt           *time.Time
	PausedRemainingMS  *int64
	PausedPrevious     modes.Phase
	Seed               string
	Config             json.RawMessage
	CreatedAt          time.Time
	UpdatedAt          time.Time
}

// Paused reports the orthogonal pause state .
func (r Round) Paused() bool { return r.PausedAt != nil }

// Scope builds the immutable version binding handed to a mode.
func (r Round) Scope() modes.Scope {
	return modes.Scope{
		ModeID:             r.ModeID,
		RulesVersion:       r.RulesVersion,
		DatasetVersion:     r.DatasetVersion,
		ContentPackVersion: r.ContentPackVersion,
		Seed:               r.Seed,
	}
}

// Actor is the server-resolved caller for one room.
//
// Membership is nil when the caller is the owner but holds no participation
// record, which happens for an administrative-only owner.
type Actor struct {
	SessionID    string
	DisplayName  string
	RoomID       string
	Membership   *Membership
	Capabilities map[Capability]bool
}

// Has reports whether the actor holds a capability.
func (a Actor) Has(c Capability) bool { return a.Capabilities[c] }

// MembershipID returns the caller's membership id, or "" when absent.
func (a Actor) MembershipID() string {
	if a.Membership == nil {
		return ""
	}
	return a.Membership.ID
}

// Subject returns the caller's scoring subject.
//
// Assigned players compete as their team; unassigned players as themselves. The
// value comes from the membership row only.
func (a Actor) Subject() (modes.Subject, error) {
	if a.Membership == nil {
		return modes.Subject{}, problem.Forbidden("not-a-participant", "you are not a participant in this room")
	}
	if !a.Has(CapPlay) {
		return modes.Subject{}, problem.Forbidden("not-a-participant", "your role cannot submit for this room")
	}
	if a.Membership.TeamID != "" {
		return modes.Subject{ID: a.Membership.TeamID, Kind: modes.SubjectTeam}, nil
	}
	if a.Membership.Role == RoleTeamCaptain {
		return modes.Subject{}, problem.Unprocessable("team-required", "a team captain must be placed on a team before competing")
	}
	return modes.Subject{ID: a.Membership.ID, Kind: modes.SubjectPlayer}, nil
}

// Config configures the room service.
type Config struct {
	Policy deployment.Policy
	// QueueCapacity is the bounded per-room command queue size. Baseline 128
	// per
	QueueCapacity int
	// QueuedCommands bounds admitted commands across every room.
	QueuedCommands int
	// QueueWait bounds how long a caller waits for its queued command.
	QueueWait time.Duration
	// IdempotencyTTL is the receipt retention. sets a 24h floor.
	IdempotencyTTL time.Duration
	// InviteCodeLen is the generated invite code length in characters.
	InviteCodeLen int
	// GrantTTL is the lifetime of display and recovery grants. sets
	// 10 minutes for recovery codes.
	GrantTTL time.Duration
	// DefaultScoreBudget is the event score budget in display points.
	DefaultScoreBudget int
	// DefaultRoundDuration bounds a round window when the mode config does not
	// specify one.
	DefaultRoundDuration time.Duration
}

func (c Config) withDefaults() Config {
	if c.QueueCapacity <= 0 {
		c.QueueCapacity = 128
	}
	if c.QueueWait <= 0 {
		c.QueueWait = 5 * time.Second
	}
	if c.IdempotencyTTL <= 0 {
		c.IdempotencyTTL = 24 * time.Hour
	}
	if c.InviteCodeLen <= 0 {
		c.InviteCodeLen = 8
	}
	if c.GrantTTL <= 0 {
		c.GrantTTL = 10 * time.Minute
	}
	if c.DefaultScoreBudget <= 0 {
		c.DefaultScoreBudget = 100
	}
	if c.DefaultRoundDuration <= 0 {
		c.DefaultRoundDuration = 5 * time.Minute
	}
	return c
}

// Service is the room domain service.
type Service struct {
	matchStatsMu         sync.Mutex
	matchStatsAt         int64
	matchStats           MatchLobby
	audioPracticeOnce    sync.Once
	audioPracticePack    PracticePack
	audioPracticeErr     error
	pedigreePracticeOnce sync.Once
	pedigreePracticePack PracticePack
	pedigreePracticeErr  error
	strategyPracticeOnce sync.Once
	strategyPracticePack PracticePack
	strategyPracticeErr  error
	pathPracticeOnce     sync.Once
	pathPracticePack     PracticePack
	pathPracticeErr      error
	racePracticeOnce     sync.Once
	racePracticePack     PracticePack
	racePracticeErr      error
	aptitudePracticeOnce sync.Once
	aptitudePracticePack PracticePack
	aptitudePracticeErr  error
	draftPracticeOnce    sync.Once
	draftPracticePack    PracticePack
	draftPracticeErr     error
	memoryPracticeOnce   sync.Once
	memoryPracticePack   PracticePack
	memoryPracticeErr    error
	practiceOnce         sync.Once
	sortPracticeOnce     sync.Once
	sortPracticePack     PracticePack
	sortPracticeErr      error
	practicePack         PracticePack
	practiceErr          error
	db                   *storage.DB
	cfg                  Config
	queue                *Dispatcher
	modes                *modes.Registry
	content              ContentIndex
	now                  func() time.Time
	epoch                string
	// sweepErr is the last deadline-sweep failure, kept so a health check or a test can
	// see that the loop is failing instead of it dying silently.
	sweepErr     error
	receiptSweep receiptSweepState
}

// Options are the collaborators a Service needs.
type Options struct {
	// Modes is the mode registry. A nil registry makes round creation answer
	// 501 rather than pretend a round exists.
	Modes *modes.Registry
	// Content is the read-only public content index. A nil index makes content
	// endpoints answer 501 and modes report themselves unavailable.
	Content ContentIndex
	// Epoch identifies this process incarnation for realtime resync.
	Epoch string
}

// New builds a room service.
func New(db *storage.DB, cfg Config, opts Options) *Service {
	cfg = cfg.withDefaults()
	s := &Service{
		db:      db,
		cfg:     cfg,
		modes:   opts.Modes,
		content: opts.Content,
		now:     time.Now,
		epoch:   opts.Epoch,
	}
	s.queue = NewDispatcherWithLimit(cfg.QueueCapacity, cfg.QueueWait, cfg.QueuedCommands)
	return s
}

// SetClock overrides the clock for tests.
func (s *Service) SetClock(now func() time.Time) { s.now = now }

// Queue exposes the dispatcher so handlers can report saturation metrics.
func (s *Service) Queue() *Dispatcher { return s.queue }

// Epoch returns the process incarnation identifier.
func (s *Service) Epoch() string { return s.epoch }

// Close stops the per-room command workers.
func (s *Service) Close() { s.queue.Close() }

// nowMS is the single server clock used for receivedAt and deadline comparison.
func (s *Service) nowMS() int64 { return s.now().UTC().UnixMilli() }

// millis converts a stored integer timestamp.
func millis(v int64) time.Time { return time.UnixMilli(v).UTC() }

// timePtr converts a nullable integer timestamp.
func timePtr(v sql.NullInt64) *time.Time {
	if !v.Valid {
		return nil
	}
	t := millis(v.Int64)
	return &t
}

// int64Ptr converts a nullable integer.
func int64Ptr(v sql.NullInt64) *int64 {
	if !v.Valid {
		return nil
	}
	n := v.Int64
	return &n
}

// ---- authorisation -------------------------------------------------------

// LoadActor resolves the caller's membership and capabilities for a room.
//
// A caller who is neither a member nor the owner gets 404, not 403, so the
// existence of a room cannot be probed .
func (s *Service) LoadActor(ctx context.Context, sessionID, roomID string) (Actor, error) {
	var ownerID string
	err := s.db.Read().QueryRowContext(ctx,
		`SELECT owner_session_id FROM rooms WHERE id = ?`, roomID).Scan(&ownerID)

	if errors.Is(err, sql.ErrNoRows) {
		return Actor{}, errRoomNotFound
	}
	if err != nil {
		return Actor{}, problem.Internal(fmt.Errorf("room: load room: %w", err))
	}

	m, err := s.loadMembershipBySession(ctx, roomID, sessionID)
	if err != nil && !errors.Is(err, errMembershipAbsent) {
		return Actor{}, err
	}
	if errors.Is(err, errMembershipAbsent) {
		m = nil
	}
	inactiveMember := m != nil && !m.Active()
	if inactiveMember {
		m = nil
	}
	isOwner := ownerID == sessionID && !inactiveMember
	if m == nil && !isOwner {
		return Actor{}, errRoomNotFound
	}

	caps := map[Capability]bool{}
	if m != nil {
		switch m.Role {
		case RolePlayer, RoleTeamCaptain:
			caps[CapPlay] = true
		case RoleSpectator:
			caps[CapObserve] = true
		case RoleHost:
			caps[CapObserve] = true
		case RoleDisplay:
			caps[CapDisplay] = true
			caps[CapObserve] = true
		}
	}
	if isOwner {
		// Current ownership is the sole management/live-control authority.
		// The participation role does not change when the host is transferred.
		caps[CapManageRoom] = true
		var kind string
		_ = s.db.Read().QueryRowContext(ctx, `SELECT json_extract(config_json, '$.kind') FROM rooms WHERE id=?`, roomID).Scan(&kind)
		if kind == "event" {
			caps[CapHost] = true
		}
	}

	// Legacy grants are honoured only for the current owner. A historical host
	// must not retain management authority after a handoff.
	rows, err := s.db.Read().QueryContext(ctx,
		`SELECT capability FROM room_grants WHERE room_id = ? AND session_id = ? AND revoked_at IS NULL`,
		roomID, sessionID)
	if err != nil {
		return Actor{}, problem.Internal(fmt.Errorf("room: load grants: %w", err))
	}
	defer rows.Close()
	for rows.Next() {
		var cap string
		if err := rows.Scan(&cap); err != nil {
			return Actor{}, problem.Internal(err)
		}
		if isOwner {
			caps[Capability(cap)] = true
		}
	}
	if err := rows.Err(); err != nil {
		return Actor{}, problem.Internal(err)
	}

	var roomKind string
	if err := s.db.Read().QueryRowContext(ctx, `SELECT json_extract(config_json, '$.kind') FROM rooms WHERE id=?`, roomID).Scan(&roomKind); err != nil {
		return Actor{}, problem.Internal(err)
	}
	if roomKind == "matchmaking" {
		delete(caps, CapManageRoom)
		delete(caps, CapHost)
	}
	displayName := ""
	if m != nil {
		displayName = m.DisplayName
	}
	return Actor{
		SessionID:    sessionID,
		DisplayName:  displayName,
		RoomID:       roomID,
		Membership:   m,
		Capabilities: caps,
	}, nil
}

// requireCapability fails with 403 when the actor lacks the capability.
func requireCapability(a Actor, c Capability, detail string) error {
	if a.Has(c) {
		return nil
	}
	return problem.Forbidden("insufficient-capability", detail)
}

// requireAnyCapability succeeds when the actor holds at least one of caps.
func requireAnyCapability(a Actor, detail string, caps ...Capability) error {
	for _, c := range caps {
		if a.Has(c) {
			return nil
		}
	}
	return problem.Forbidden("insufficient-capability", detail)
}

// ---- shared helpers ------------------------------------------------------

// Domain errors reused across the package.
var (
	errRoomNotFound     = problem.NotFound("room-not-found", "the room does not exist or is not visible to you")
	errMembershipAbsent = errors.New("room: membership absent")
	errRoundNotFound    = problem.NotFound("round-not-found", "the round does not exist or is not visible to you")
	errEventNotFound    = problem.NotFound("event-not-found", "the event does not exist or is not visible to you")
	errTicketNotFound   = problem.NotFound("ticket-not-found", "the ticket does not exist for this session")
)

// loadMembershipBySession reads the single membership row for a session.
func (s *Service) loadMembershipBySession(ctx context.Context, roomID, sessionID string) (*Membership, error) {
	row := s.db.Read().QueryRowContext(ctx, `
		SELECT m.id, m.room_id, m.session_id, m.role, COALESCE(m.team_id, ''), m.ready,
		       m.joined_at, m.left_at, m.revoked_at, s.display_name, m.entry_number, m.public_code
		FROM memberships m JOIN sessions s ON s.id = m.session_id
		WHERE m.room_id = ? AND m.session_id = ?`, roomID, sessionID)
	m, err := scanMembership(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, errMembershipAbsent
	}
	if err != nil {
		return nil, problem.Internal(fmt.Errorf("room: load membership: %w", err))
	}
	return m, nil
}

// rowScanner is satisfied by *sql.Row and *sql.Rows.
type rowScanner interface {
	Scan(dest ...any) error
}

func scanMembership(sc rowScanner) (*Membership, error) {
	var (
		m             Membership
		role          string
		ready         int
		joinedAt      int64
		leftAt, revAt sql.NullInt64
	)
	if err := sc.Scan(&m.ID, &m.RoomID, &m.SessionID, &role, &m.TeamID, &ready,
		&joinedAt, &leftAt, &revAt, &m.DisplayName, &m.EntryNumber, &m.PublicCode); err != nil {
		return nil, err
	}
	m.Nickname = m.DisplayName
	m.Role = Role(role)
	m.Ready = ready == 1
	m.JoinedAt = millis(joinedAt)
	m.LeftAt = timePtr(leftAt)
	m.RevokedAt = timePtr(revAt)
	return &m, nil
}

// loadRoom reads a room row.
func (s *Service) loadRoom(ctx context.Context, roomID string) (Room, error) {
	return s.loadRoomQ(ctx, s.db.Read(), roomID)
}

// queryer is satisfied by *sql.DB and *sql.Tx.
type queryer interface {
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

func (s *Service) loadRoomQ(ctx context.Context, q queryer, roomID string) (Room, error) {
	var (
		r          Room
		invite     sql.NullString
		status     string
		configJSON string
		createdAt  int64
		updatedAt  int64
	)
	err := q.QueryRowContext(ctx, `
		SELECT id, invite_code, owner_session_id, name, status, room_version, capacity,
		       config_json, created_at, updated_at
		FROM rooms WHERE id = ?`, roomID).
		Scan(&r.ID, &invite, &r.OwnerID, &r.Name, &status, &r.RoomVersion, &r.Capacity,
			&configJSON, &createdAt, &updatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return Room{}, errRoomNotFound
	}
	if err != nil {
		return Room{}, problem.Internal(fmt.Errorf("room: load room: %w", err))
	}
	r.InviteCode = invite.String
	r.Status = RoomStatus(status)
	r.CreatedAt = millis(createdAt)
	r.UpdatedAt = millis(updatedAt)
	if configJSON != "" {
		if err := json.Unmarshal([]byte(configJSON), &r.Config); err != nil {
			return Room{}, problem.Internal(fmt.Errorf("room: decode config: %w", err))
		}
	}
	return r, nil
}

// bumpRoomVersion increments rooms.room_version inside the transaction and
// returns the new value. Every persistent public or member-visible change goes
// through here .
func bumpRoomVersion(ctx context.Context, tx *sql.Tx, roomID string, at int64) (int64, error) {
	if _, err := tx.ExecContext(ctx,
		`UPDATE rooms SET room_version = room_version + 1, updated_at = ? WHERE id = ?`,
		at, roomID); err != nil {
		return 0, problem.Internal(fmt.Errorf("room: bump version: %w", err))
	}
	var v int64
	if err := tx.QueryRowContext(ctx,
		`SELECT room_version FROM rooms WHERE id = ?`, roomID).Scan(&v); err != nil {
		return 0, problem.Internal(fmt.Errorf("room: read version: %w", err))
	}
	return v, nil
}

// emitOutbox records a to-be-published notification inside the transaction.
//
// The row is a transactional fact: a dispatcher may re-send it, and clients
// de-duplicate by eventId . Nothing is broadcast before commit.
func emitOutbox(ctx context.Context, tx *sql.Tx, roomID, kind string, payload map[string]any, at int64) error {
	// Automatic transitions use this same path as player actions. Stamp a
	// missing version from the transaction, so subscribers do not discard a
	// committed phase change as an obsolete notification (version zero).
	if _, exists := payload["roomVersion"]; !exists {
		var version int64
		if err := tx.QueryRowContext(ctx, `SELECT room_version FROM rooms WHERE id = ?`, roomID).Scan(&version); err != nil {
			return problem.Internal(fmt.Errorf("room: outbox version: %w", err))
		}
		stamped := make(map[string]any, len(payload)+1)
		for key, value := range payload {
			stamped[key] = value
		}
		stamped["roomVersion"] = version
		payload = stamped
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return problem.Internal(fmt.Errorf("room: encode outbox: %w", err))
	}
	var seq int64
	if err := tx.QueryRowContext(ctx,
		`SELECT COALESCE(MAX(event_sequence), 0) + 1 FROM outbox_events WHERE room_id = ?`,
		roomID).Scan(&seq); err != nil {
		return problem.Internal(fmt.Errorf("room: next outbox sequence: %w", err))
	}
	id, err := newID("evt_")
	if err != nil {
		return problem.Internal(err)
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO outbox_events (id, room_id, event_sequence, type, payload_json, created_at)
		VALUES (?, ?, ?, ?, ?, ?)`,
		id, roomID, seq, kind, string(body), at); err != nil {
		return problem.Internal(fmt.Errorf("room: insert outbox: %w", err))
	}
	return nil
}

// writeAudit records an auditable privileged action .
func writeAudit(ctx context.Context, tx *sql.Tx, roomID, eventID, actorID, action string, detail any, at int64) error {
	body, err := json.Marshal(detail)
	if err != nil {
		return problem.Internal(fmt.Errorf("room: encode audit: %w", err))
	}
	id, err := newID("aud_")
	if err != nil {
		return problem.Internal(err)
	}
	var room any
	if roomID != "" {
		room = roomID
	}
	var evt any
	if eventID != "" {
		evt = eventID
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO audit_events (id, room_id, event_id, actor_id, action, detail_json, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)`,
		id, room, evt, actorID, action, string(body), at); err != nil {
		return problem.Internal(fmt.Errorf("room: insert audit: %w", err))
	}
	return nil
}
