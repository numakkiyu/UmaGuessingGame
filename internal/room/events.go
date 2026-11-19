package room

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/bhcn/umaguessinggame/internal/httpapi/problem"
	"github.com/bhcn/umaguessinggame/internal/modes"
)

// EventStatus is the event lifecycle state .
type EventStatus string

const (
	EventDraft     EventStatus = "draft"
	EventLobby     EventStatus = "lobby"
	EventRunning   EventStatus = "running"
	EventCompleted EventStatus = "completed"
	EventCancelled EventStatus = "cancelled"
)

// Event is one activity programme bound to a room.
type Event struct {
	ID            string
	RoomID        string
	HostSessionID string
	Name          string
	TiePolicy     string
	Status        EventStatus
	ScoreBudget   int
	EventVersion  int64
	CreatedAt     int64
	UpdatedAt     int64
}

type EventItemResult struct {
	PublicCode  string `json:"publicCode,omitempty"`
	SubjectID   string `json:"subjectId"`
	DisplayName string `json:"displayName"`
	ScoreUnits  int    `json:"scoreUnits"`
	MaxUnits    int    `json:"maxUnits"`
}

// Segment is one event item.
type Segment struct {
	Results           []EventItemResult
	ID                string
	EventID           string
	Ordinal           int64
	ModeID            string
	Name              string
	Status            string
	Budget            int
	RoundID           string
	Config            json.RawMessage
	StartedRounds     int
	CompletedRounds   int
	CurrentRoundPhase string
}

// SegmentStatus values the schema accepts.
var segmentStatuses = map[string]bool{
	"planned": true, "running": true, "completed": true, "voided": true,
}

// CreateEventInput is the command to create an event.
type CreateEventInput struct {
	Command   *CreationCommand
	RoomID    string
	Name      string
	TiePolicy string
}

// CreateEvent creates an event programme for a room.
//
// Only a host or room manager may create one, and a room may have at most one
// active programme so two hosts cannot both drive the current round.
func (s *Service) CreateEvent(ctx context.Context, sessionID string, in CreateEventInput) (Event, error) {
	if in.Command != nil {
		if id, err := createdResource(ctx, s.db.Read(), "event_sessions", "creator_session_id", sessionID, in.Command); err != nil {
			return Event{}, err
		} else if id != "" {
			return s.loadEvent(ctx, id)
		}
	}

	if enabled, ok := s.cfg.Policy.Forms["event"]; ok && !enabled {
		return Event{}, problem.Forbidden("play-disabled", "activities are closed on this instance")
	}
	name := strings.TrimSpace(in.Name)
	if n := len([]rune(name)); n < 1 || n > 48 {
		return Event{}, problem.Validation("name is invalid",
			problem.FieldError{Field: "name", Message: "must be 1..48 characters"})
	}
	if in.TiePolicy != "shared-rank" && in.TiePolicy != "announced-tiebreak" {
		return Event{}, problem.Validation("tiePolicy is invalid",
			problem.FieldError{Field: "tiePolicy", Message: "must be shared-rank or announced-tiebreak"})
	}
	actor, err := s.LoadActor(ctx, sessionID, in.RoomID)
	if err != nil {
		return Event{}, err
	}
	if err := requireAnyCapability(actor, "only a host or room manager may create an event",
		CapHost, CapManageRoom); err != nil {
		return Event{}, err
	}

	id, err := newID("evs_")
	if err != nil {
		return Event{}, problem.Internal(err)
	}
	now := s.nowMS()

	tx, err := s.db.Write().BeginTx(ctx, nil)
	if err != nil {
		return Event{}, problem.Internal(fmt.Errorf("room: begin create event: %w", err))
	}
	defer tx.Rollback()
	if err := s.validateControllerQ(ctx, tx, actor); err != nil {
		return Event{}, err
	}
	if id, err := createdResource(ctx, tx, "event_sessions", "creator_session_id", sessionID, in.Command); err != nil {
		return Event{}, err
	} else if id != "" {
		return s.loadEventQ(ctx, tx, id)
	}
	creationKey, creationHash, err := in.Command.identity()
	if err != nil {
		return Event{}, err
	}

	var active int
	if err := tx.QueryRowContext(ctx, `
		SELECT COUNT(1) FROM event_sessions
		WHERE room_id = ? AND status NOT IN ('completed','cancelled')`, in.RoomID).Scan(&active); err != nil {
		return Event{}, problem.Internal(fmt.Errorf("room: check active event: %w", err))
	}
	if active > 0 {
		return Event{}, problem.Conflict("event-already-active",
			"this room already has an active event programme")
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO event_sessions (id, room_id, host_session_id, name, status, score_budget,
		                            event_version, created_at, updated_at,creation_key,creation_hash,tie_policy)
		VALUES (?, ?, ?, ?, 'draft', ?, 1, ?, ?,?,?,?)`,
		id, in.RoomID, sessionID, name, s.cfg.DefaultScoreBudget, now, now, creationKey, creationHash, in.TiePolicy); err != nil {
		return Event{}, problem.Internal(fmt.Errorf("room: insert event: %w", err))
	}
	// The creator becomes the host of their own event.
	if err := writeAudit(ctx, tx, in.RoomID, id, sessionID, "event.created", map[string]any{
		"name": name, "tiePolicy": in.TiePolicy,
	}, now); err != nil {
		return Event{}, err
	}
	version, err := bumpRoomVersion(ctx, tx, in.RoomID, now)
	if err != nil {
		return Event{}, err
	}
	if err := emitOutbox(ctx, tx, in.RoomID, "state.changed", map[string]any{
		"resource": "event", "eventId": id, "roomVersion": version,
	}, now); err != nil {
		return Event{}, err
	}
	if err := tx.Commit(); err != nil {
		return Event{}, problem.Internal(fmt.Errorf("room: commit event: %w", err))
	}
	return s.loadEvent(ctx, id)
}

// loadEvent reads an event row.
func (s *Service) loadEvent(ctx context.Context, eventID string) (Event, error) {
	return s.loadEventQ(ctx, s.db.Read(), eventID)
}

func (s *Service) loadEventQ(ctx context.Context, q queryer, eventID string) (Event, error) {
	var (
		e      Event
		status string
	)
	err := q.QueryRowContext(ctx, `
		SELECT id, room_id, host_session_id, name, status, score_budget, event_version,
		       created_at, updated_at, tie_policy
		FROM event_sessions WHERE id = ?`, eventID).
		Scan(&e.ID, &e.RoomID, &e.HostSessionID, &e.Name, &status, &e.ScoreBudget,
			&e.EventVersion, &e.CreatedAt, &e.UpdatedAt, &e.TiePolicy)
	if errors.Is(err, sql.ErrNoRows) {
		return Event{}, errEventNotFound
	}
	if err != nil {
		return Event{}, problem.Internal(fmt.Errorf("room: load event: %w", err))
	}
	e.Status = EventStatus(status)
	return e, nil
}

// GetEvent returns an event after authorising the caller against its room.
func (s *Service) GetEvent(ctx context.Context, sessionID, eventID string) (Event, []Segment, error) {
	e, err := s.loadEvent(ctx, eventID)
	if err != nil {
		return Event{}, nil, err
	}
	if _, _, err := s.GetRoom(ctx, sessionID, e.RoomID); err != nil {
		return Event{}, nil, err
	}
	segs, err := s.listSegments(ctx, eventID)
	if err != nil {
		return Event{}, nil, err
	}
	if err := s.attachEntertainmentResults(ctx, e.RoomID, eventID, segs); err != nil {
		return Event{}, nil, err
	}
	return e, segs, nil
}

// listSegments returns an event's segments in ordinal order.
func (s *Service) listSegments(ctx context.Context, eventID string) ([]Segment, error) {
	rows, err := s.db.Read().QueryContext(ctx, `
		SELECT id, event_id, ordinal, mode_id, name, status, budget,
		       COALESCE(round_id, ''), config_json,
		       (SELECT COUNT(*) FROM event_segment_rounds sr WHERE sr.segment_id=es.id),
		       (SELECT COUNT(*) FROM event_segment_rounds sr JOIN rounds r ON r.id=sr.round_id WHERE sr.segment_id=es.id AND r.status='settled'),
		       COALESCE((SELECT status FROM rounds WHERE id=es.round_id),'')
		FROM event_segments es WHERE event_id = ? ORDER BY ordinal ASC`, eventID)
	if err != nil {
		return nil, problem.Internal(fmt.Errorf("room: list segments: %w", err))
	}
	defer rows.Close()
	var out []Segment
	for rows.Next() {
		var (
			seg    Segment
			config string
		)
		if err := rows.Scan(&seg.ID, &seg.EventID, &seg.Ordinal, &seg.ModeID, &seg.Name,
			&seg.Status, &seg.Budget, &seg.RoundID, &config, &seg.StartedRounds, &seg.CompletedRounds, &seg.CurrentRoundPhase); err != nil {
			return nil, problem.Internal(err)
		}
		seg.Config = json.RawMessage(config)
		out = append(out, seg)
	}
	return out, rows.Err()
}

// SegmentCreateInput is the command to append a segment.
type SegmentCreateInput struct {
	ModeConfig  json.RawMessage
	Label       string
	ScoreBudget int
	RoundCount  int
	Competitive bool
}

// CreateSegment appends an event item.
//
// Only a draft or lobby programme accepts new items, and only a host or manager
// may add one .
func (s *Service) CreateSegment(ctx context.Context, actor Actor, eventID string, in SegmentCreateInput) (Segment, error) {
	if err := requireAnyCapability(actor, "only a host or room manager may add a segment",
		CapHost, CapManageRoom); err != nil {
		return Segment{}, err
	}
	if err := s.validateModeConfig(in.ModeConfig); err != nil {
		return Segment{}, err
	}
	if err := s.checkModeForm(in.ModeConfig, "event"); err != nil {
		return Segment{}, err
	}
	label := strings.TrimSpace(in.Label)
	if n := len([]rune(label)); n < 1 || n > 48 {
		return Segment{}, problem.Validation("label is invalid",
			problem.FieldError{Field: "label", Message: "must be 1..48 characters"})
	}
	if in.ScoreBudget < 0 || in.ScoreBudget > 100 {
		return Segment{}, problem.Validation("scoreBudget is invalid",
			problem.FieldError{Field: "scoreBudget", Message: "must be 0..100"})
	}
	if in.RoundCount < 1 || in.RoundCount > 20 {
		return Segment{}, problem.Validation("roundCount is invalid",
			problem.FieldError{Field: "roundCount", Message: "must be 1..20"})
	}
	var probe struct {
		ModeID string `json:"modeId"`
	}
	if err := json.Unmarshal(in.ModeConfig, &probe); err != nil {
		return Segment{}, problem.Validation("modeConfig is invalid",
			problem.FieldError{Field: "modeConfig", Message: "must be a JSON object"})
	}

	if err := checkSegmentScoring(probe.ModeID, in.Competitive); err != nil {
		return Segment{}, err
	}

	id, err := newID("seg_")
	if err != nil {
		return Segment{}, problem.Internal(err)
	}
	now := s.nowMS()

	tx, err := s.db.Write().BeginTx(ctx, nil)
	if err != nil {
		return Segment{}, problem.Internal(fmt.Errorf("room: begin segment: %w", err))
	}
	defer tx.Rollback()
	if err := s.validateControllerQ(ctx, tx, actor); err != nil {
		return Segment{}, err
	}

	event, err := s.loadEventQ(ctx, tx, eventID)
	if err != nil {
		return Segment{}, err
	}
	if event.RoomID != actor.RoomID {
		return Segment{}, errEventNotFound
	}
	if event.Status != EventDraft && event.Status != EventLobby {
		return Segment{}, problem.Conflict("event-not-editable",
			fmt.Sprintf("the event is %s; segments may only be added before it starts", event.Status))
	}
	var maxOrdinal sql.NullInt64
	if err := tx.QueryRowContext(ctx,
		`SELECT MAX(ordinal) FROM event_segments WHERE event_id = ?`, eventID).Scan(&maxOrdinal); err != nil {
		return Segment{}, problem.Internal(fmt.Errorf("room: read segment ordinal: %w", err))
	}
	ordinal := maxOrdinal.Int64 + 1

	cfg := map[string]any{
		"modeConfig":  json.RawMessage(in.ModeConfig),
		"roundCount":  in.RoundCount,
		"competitive": in.Competitive,
	}
	cfgJSON, err := json.Marshal(cfg)
	if err != nil {
		return Segment{}, problem.Internal(fmt.Errorf("room: encode segment config: %w", err))
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO event_segments (id, event_id, ordinal, mode_id, name, status, budget, config_json)
		VALUES (?, ?, ?, ?, ?, 'planned', ?, ?)`,
		id, eventID, ordinal, probe.ModeID, label, in.ScoreBudget, string(cfgJSON)); err != nil {
		return Segment{}, problem.Internal(fmt.Errorf("room: insert segment: %w", err))
	}
	if _, err := bumpEventVersion(ctx, tx, eventID, now); err != nil {
		return Segment{}, err
	}
	version, err := bumpRoomVersion(ctx, tx, actor.RoomID, now)
	if err != nil {
		return Segment{}, err
	}
	if err := emitOutbox(ctx, tx, actor.RoomID, "state.changed", map[string]any{
		"resource": "event", "eventId": eventID, "roomVersion": version,
	}, now); err != nil {
		return Segment{}, err
	}
	if err := tx.Commit(); err != nil {
		return Segment{}, problem.Internal(fmt.Errorf("room: commit segment: %w", err))
	}
	return Segment{
		ID: id, EventID: eventID, Ordinal: ordinal, ModeID: probe.ModeID,
		Name: label, Status: "planned", Budget: in.ScoreBudget, Config: cfgJSON,
	}, nil
}

// bumpEventVersion increments event_version and returns the new value.
func bumpEventVersion(ctx context.Context, tx *sql.Tx, eventID string, at int64) (int64, error) {
	if _, err := tx.ExecContext(ctx,
		`UPDATE event_sessions SET event_version = event_version + 1, updated_at = ? WHERE id = ?`,
		at, eventID); err != nil {
		return 0, problem.Internal(fmt.Errorf("room: bump event version: %w", err))
	}
	var v int64
	if err := tx.QueryRowContext(ctx,
		`SELECT event_version FROM event_sessions WHERE id = ?`, eventID).Scan(&v); err != nil {
		return 0, problem.Internal(fmt.Errorf("room: read event version: %w", err))
	}
	return v, nil
}

// SegmentPatchInput is a segment edit.
type SegmentPatchInput struct {
	ExpectedEventVersion int64
	Label                *string
	ScoreBudget          *int
	ModeConfig           json.RawMessage
}

// UpdateSegment edits a segment that has not started.
//
// A settled segment must never be silently re-scored; that requires an explicit
// correction with a reason .
func (s *Service) UpdateSegment(ctx context.Context, actor Actor, eventID, segmentID string, in SegmentPatchInput) (Segment, error) {
	if err := requireAnyCapability(actor, "only a host or room manager may edit a segment",
		CapHost, CapManageRoom); err != nil {
		return Segment{}, err
	}
	now := s.nowMS()

	tx, err := s.db.Write().BeginTx(ctx, nil)
	if err != nil {
		return Segment{}, problem.Internal(fmt.Errorf("room: begin update segment: %w", err))
	}
	defer tx.Rollback()
	if err := s.validateControllerQ(ctx, tx, actor); err != nil {
		return Segment{}, err
	}

	event, err := s.loadEventQ(ctx, tx, eventID)
	if err != nil {
		return Segment{}, err
	}
	if event.RoomID != actor.RoomID {
		return Segment{}, errEventNotFound
	}
	if event.EventVersion != in.ExpectedEventVersion {
		e := problem.Conflict("event-version-conflict",
			fmt.Sprintf("expected version does not match the current eventVersion %d", event.EventVersion))
		e.Fields = []problem.FieldError{{Field: "expectedEventVersion",
			Message: fmt.Sprintf("current eventVersion is %d", event.EventVersion)}}
		return Segment{}, e
	}

	var (
		seg     Segment
		config  string
		roundID sql.NullString
	)
	err = tx.QueryRowContext(ctx, `
		SELECT id, event_id, ordinal, mode_id, name, status, budget, round_id, config_json
		FROM event_segments WHERE id = ? AND event_id = ?`, segmentID, eventID).
		Scan(&seg.ID, &seg.EventID, &seg.Ordinal, &seg.ModeID, &seg.Name, &seg.Status,
			&seg.Budget, &roundID, &config)
	if errors.Is(err, sql.ErrNoRows) {
		return Segment{}, problem.NotFound("segment-not-found", "the segment does not exist in this event")
	}
	if err != nil {
		return Segment{}, problem.Internal(fmt.Errorf("room: load segment: %w", err))
	}
	if seg.Status != "planned" {
		return Segment{}, problem.Conflict("segment-not-editable",
			fmt.Sprintf("the segment is %s; only a planned segment may be edited", seg.Status))
	}

	if in.Label != nil {
		label := strings.TrimSpace(*in.Label)
		if n := len([]rune(label)); n < 1 || n > 48 {
			return Segment{}, problem.Validation("label is invalid",
				problem.FieldError{Field: "label", Message: "must be 1..48 characters"})
		}
		seg.Name = label
	}
	if in.ScoreBudget != nil {
		if *in.ScoreBudget < 0 || *in.ScoreBudget > 100 {
			return Segment{}, problem.Validation("scoreBudget is invalid",
				problem.FieldError{Field: "scoreBudget", Message: "must be 0..100"})
		}
		seg.Budget = *in.ScoreBudget
	}
	if len(in.ModeConfig) > 0 {
		if err := s.checkModeForm(in.ModeConfig, "event"); err != nil {
			return Segment{}, err
		}
		if err := s.validateModeConfig(in.ModeConfig); err != nil {
			return Segment{}, err
		}
		var old map[string]any
		if err := json.Unmarshal([]byte(config), &old); err != nil {
			old = map[string]any{}
		}
		old["modeConfig"] = json.RawMessage(in.ModeConfig)
		var probe struct {
			ModeID string `json:"modeId"`
		}
		if err := json.Unmarshal(in.ModeConfig, &probe); err != nil {
			return Segment{}, problem.Validation("modeConfig is invalid")
		}
		seg.ModeID = probe.ModeID
		merged, err := json.Marshal(old)
		if err != nil {
			return Segment{}, problem.Internal(fmt.Errorf("room: merge segment config: %w", err))
		}
		config = string(merged)
	}

	storedRules, err := readSegmentRules([]byte(config))
	if err != nil {
		return Segment{}, err
	}
	if err := checkSegmentScoring(seg.ModeID, storedRules.Competitive == nil || *storedRules.Competitive); err != nil {
		return Segment{}, err
	}

	if _, err := tx.ExecContext(ctx,
		`UPDATE event_segments SET name = ?, budget = ?, config_json = ?, mode_id=? WHERE id = ?`,
		seg.Name, seg.Budget, config, seg.ModeID, seg.ID); err != nil {
		return Segment{}, problem.Internal(fmt.Errorf("room: update segment: %w", err))
	}
	if _, err := bumpEventVersion(ctx, tx, eventID, now); err != nil {
		return Segment{}, err
	}
	if _, err := bumpRoomVersion(ctx, tx, actor.RoomID, now); err != nil {
		return Segment{}, err
	}
	if err := tx.Commit(); err != nil {
		return Segment{}, problem.Internal(fmt.Errorf("room: commit update segment: %w", err))
	}
	seg.Config = json.RawMessage(config)
	return seg, nil
}

// CreateEventTransition changes the event lifecycle with an explicit version.
func (s *Service) CreateEventTransition(ctx context.Context, actor Actor, eventID, target string, expectedVersion int64, reason string) (Event, error) {
	if err := requireAnyCapability(actor, "only a host or room manager may change the event state",
		CapHost, CapManageRoom); err != nil {
		return Event{}, err
	}
	allowed := map[string]bool{"lobby": true, "running": true, "completed": true, "cancelled": true}
	if !allowed[target] {
		return Event{}, problem.Validation("target is invalid",
			problem.FieldError{Field: "target", Message: "must be lobby, running, completed or cancelled"})
	}
	if n := len([]rune(reason)); n > 240 {
		return Event{}, problem.Validation("reason is too long",
			problem.FieldError{Field: "reason", Message: "must be at most 240 characters"})
	}
	now := s.nowMS()

	tx, err := s.db.Write().BeginTx(ctx, nil)
	if err != nil {
		return Event{}, problem.Internal(fmt.Errorf("room: begin event transition: %w", err))
	}
	defer tx.Rollback()
	if err := s.validateControllerQ(ctx, tx, actor); err != nil {
		return Event{}, err
	}

	event, err := s.loadEventQ(ctx, tx, eventID)
	if err != nil {
		return Event{}, err
	}
	if event.RoomID != actor.RoomID {
		return Event{}, errEventNotFound
	}
	if event.EventVersion != expectedVersion {
		e := problem.Conflict("event-version-conflict",
			fmt.Sprintf("expected version does not match the current eventVersion %d", event.EventVersion))
		e.Fields = []problem.FieldError{{Field: "expectedEventVersion",
			Message: fmt.Sprintf("current eventVersion is %d", event.EventVersion)}}
		return Event{}, e
	}
	if event.Status == EventCompleted || event.Status == EventCancelled {
		return Event{}, problem.Conflict("event-closed",
			fmt.Sprintf("the event is %s and cannot change state", event.Status))
	}
	if !eventTransitionAllowed(event.Status, EventStatus(target)) {
		return Event{}, problem.Conflict("invalid-event-transition",
			fmt.Sprintf("cannot move from %s to %s", event.Status, target))
	}
	// Completing an event requires every scoring segment to be completed or
	// explicitly voided : otherwise the standings would be a silent lie.
	if target == "completed" {
		var open int
		if err := tx.QueryRowContext(ctx, `
			SELECT COUNT(1) FROM event_segments
			WHERE event_id = ? AND status NOT IN ('completed','voided')`, eventID).Scan(&open); err != nil {
			return Event{}, problem.Internal(fmt.Errorf("room: check segments: %w", err))
		}
		if open > 0 {
			return Event{}, problem.Unprocessable("segments-incomplete",
				fmt.Sprintf("%d segment(s) are neither completed nor voided", open))
		}
	}

	if _, err := tx.ExecContext(ctx,
		`UPDATE event_sessions SET status = ?, updated_at = ? WHERE id = ?`,
		target, now, eventID); err != nil {
		return Event{}, problem.Internal(fmt.Errorf("room: update event status: %w", err))
	}
	if _, err := bumpEventVersion(ctx, tx, eventID, now); err != nil {
		return Event{}, err
	}
	if err := writeAudit(ctx, tx, actor.RoomID, eventID, actor.SessionID, "event.transition", map[string]any{
		"from": string(event.Status), "to": target, "reason": reason,
	}, now); err != nil {
		return Event{}, err
	}
	version, err := bumpRoomVersion(ctx, tx, actor.RoomID, now)
	if err != nil {
		return Event{}, err
	}
	if err := emitOutbox(ctx, tx, actor.RoomID, "state.changed", map[string]any{
		"resource": "event", "eventId": eventID, "roomVersion": version,
	}, now); err != nil {
		return Event{}, err
	}
	if err := tx.Commit(); err != nil {
		return Event{}, problem.Internal(fmt.Errorf("room: commit event transition: %w", err))
	}
	return s.loadEvent(ctx, eventID)
}

// eventTransitionAllowed encodes the event state machine.
func eventTransitionAllowed(from, to EventStatus) bool {
	switch from {
	case EventDraft:
		return to == EventLobby || to == EventRunning || to == EventCancelled
	case EventLobby:
		return to == EventRunning || to == EventCancelled
	case EventRunning:
		return to == EventCompleted || to == EventCancelled
	}
	return false
}

// Standing is one row of the aggregate table.
type Standing struct {
	PublicCode  string
	SubjectID   string
	DisplayName string
	Rank        int
	ScoreUnits  int
	Segments    []SegmentScore
}

// SegmentScore is one subject's score within a segment.
type SegmentScore struct {
	SegmentID  string
	ScoreUnits int
	MaxUnits   int
}

// Standings is the aggregate table plus its correction version.
type Standings struct {
	EventID       string
	EventVersion  int64
	Items         []Standing
	CorrectionIDs []string
}

// GetStandings computes the aggregate table with shared ranks and corrections.
//
// Ordering is deterministic: score descending then subject id ascending, so two
// readers never disagree about a tie .
func (s *Service) GetStandings(ctx context.Context, sessionID, eventID string) (Standings, error) {
	event, err := s.loadEvent(ctx, eventID)
	if err != nil {
		return Standings{}, err
	}
	if _, _, err := s.GetRoom(ctx, sessionID, event.RoomID); err != nil {
		return Standings{}, err
	}

	rows, err := s.db.Read().QueryContext(ctx, `
		SELECT sr.segment_id, sr.subject_id, sr.score_units, sr.max_score_units
		FROM segment_results sr
		JOIN event_segments es ON es.id = sr.segment_id
		WHERE es.event_id = ? AND es.status <> 'voided'
          AND COALESCE(json_extract(es.config_json,'$.competitive'),1)=1
          AND es.mode_id NOT IN ('creative','trade')`, eventID)
	if err != nil {
		return Standings{}, problem.Internal(fmt.Errorf("room: read standings: %w", err))
	}
	defer rows.Close()

	totals := map[string]int{}
	bySegment := map[string][]SegmentScore{}
	for rows.Next() {
		var (
			segID, subjectID string
			score, max       int
		)
		if err := rows.Scan(&segID, &subjectID, &score, &max); err != nil {
			return Standings{}, problem.Internal(err)
		}
		totals[subjectID] += score
		bySegment[subjectID] = append(bySegment[subjectID], SegmentScore{
			SegmentID: segID, ScoreUnits: score, MaxUnits: max,
		})
	}
	if err := rows.Err(); err != nil {
		return Standings{}, problem.Internal(err)
	}

	names, err := s.subjectDisplayNames(ctx, event.RoomID)
	if err != nil {
		return Standings{}, err
	}

	codes, err := s.subjectPublicCodes(ctx, event.RoomID)
	if err != nil {
		return Standings{}, err
	}
	items := make([]Standing, 0, len(totals))
	for subjectID, total := range totals {
		items = append(items, Standing{
			SubjectID: subjectID, DisplayName: names[subjectID], PublicCode: codes[subjectID],
			ScoreUnits: total, Segments: bySegment[subjectID],
		})
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].ScoreUnits != items[j].ScoreUnits {
			return items[i].ScoreUnits > items[j].ScoreUnits
		}
		return items[i].SubjectID < items[j].SubjectID
	})
	for i := range items {
		if i > 0 && items[i].ScoreUnits == items[i-1].ScoreUnits {
			items[i].Rank = items[i-1].Rank
		} else {
			items[i].Rank = i + 1
		}
	}

	corrections, err := s.correctionIDs(ctx, eventID)
	if err != nil {
		return Standings{}, err
	}
	return Standings{
		EventID: eventID, EventVersion: event.EventVersion,
		Items: items, CorrectionIDs: corrections,
	}, nil
}

// subjectDisplayNames maps subject ids to display names for standings.
//
// Two keys are registered per membership. A player round is scored by membership id
// (mem_...), but a team round is scored by team id (tem_...) — see rounds.go, where a
// team-captain with a team becomes a SubjectTeam. Keying only by membership left every
// team row with an empty name: the scores were right and the labels were blank, which
// is worse than no standings at all because it looks like a working table.
//
// When a member has a team, the team id and the member id both resolve to the team
// label, so either kind of subject finds a name.
func (s *Service) subjectDisplayNames(ctx context.Context, roomID string) (map[string]string, error) {
	rows, err := s.db.Read().QueryContext(ctx, `
		SELECT m.id, COALESCE(t.label, s.display_name), m.team_id
		FROM memberships m
		JOIN sessions s ON s.id = m.session_id
		LEFT JOIN teams t ON t.id = m.team_id
		WHERE m.room_id = ? AND m.left_at IS NULL AND m.revoked_at IS NULL`, roomID)
	if err != nil {
		return nil, problem.Internal(fmt.Errorf("room: read names: %w", err))
	}
	defer rows.Close()
	out := map[string]string{}
	for rows.Next() {
		var (
			id     string
			name   string
			teamID sql.NullString
		)
		if err := rows.Scan(&id, &name, &teamID); err != nil {
			return nil, problem.Internal(err)
		}
		out[id] = name
		// A team subject resolves to the same label as its members. The first member
		// wins, so the label does not flip between members of the same team.
		if teamID.Valid && teamID.String != "" {
			if _, taken := out[teamID.String]; !taken {
				out[teamID.String] = name
			}
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	rows.Close()
	if s.cfg.Policy.Identity.DistinguishNames() {
		members, err := s.ListMembers(ctx, roomID)
		if err != nil {
			return nil, err
		}
		for _, m := range members {
			if m.TeamID == "" {
				out[m.ID] = m.DisplayName
			}
		}
	}
	return out, nil
}

// correctionIDs lists the corrections applied to an event.
func (s *Service) correctionIDs(ctx context.Context, eventID string) ([]string, error) {
	rows, err := s.db.Read().QueryContext(ctx,
		`SELECT id FROM score_corrections WHERE event_id = ? ORDER BY created_at ASC, id ASC`, eventID)
	if err != nil {
		return nil, problem.Internal(fmt.Errorf("room: read corrections: %w", err))
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, problem.Internal(err)
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

// CorrectionInput is an explicit score correction.
type CorrectionInput struct {
	SegmentID            string
	SubjectID            string
	NewScoreUnits        int
	Reason               string
	ExpectedEventVersion int64
}

// CreateScoreCorrection records an auditable score change.
//
// Corrections never overwrite history: a new row records before and after plus
// the reason . Both host and owner may correct, but never silently.
func (s *Service) CreateScoreCorrection(ctx context.Context, actor Actor, eventID string, in CorrectionInput) (correctionID string, eventVersion int64, acceptedAt int64, err error) {
	if err := requireAnyCapability(actor, "only a host or room manager may correct a score",
		CapHost, CapManageRoom); err != nil {
		return "", 0, 0, err
	}
	reason := strings.TrimSpace(in.Reason)
	if n := len([]rune(reason)); n < 5 || n > 240 {
		return "", 0, 0, problem.Validation("reason is invalid",
			problem.FieldError{Field: "reason", Message: "must be 5..240 characters"})
	}
	if in.NewScoreUnits < 0 || in.NewScoreUnits > 10000 {
		return "", 0, 0, problem.Validation("newScoreUnits is invalid",
			problem.FieldError{Field: "newScoreUnits", Message: "must be 0..10000"})
	}
	id, err := newID("cor_")
	if err != nil {
		return "", 0, 0, problem.Internal(err)
	}
	now := s.nowMS()

	tx, err := s.db.Write().BeginTx(ctx, nil)
	if err != nil {
		return "", 0, 0, problem.Internal(fmt.Errorf("room: begin correction: %w", err))
	}
	defer tx.Rollback()
	if err := s.validateControllerQ(ctx, tx, actor); err != nil {
		return "", 0, 0, err
	}

	event, err := s.loadEventQ(ctx, tx, eventID)
	if err != nil {
		return "", 0, 0, err
	}
	if event.RoomID != actor.RoomID {
		return "", 0, 0, errEventNotFound
	}
	if event.EventVersion != in.ExpectedEventVersion {
		e := problem.Conflict("event-version-conflict",
			fmt.Sprintf("expected version does not match the current eventVersion %d", event.EventVersion))
		e.Fields = []problem.FieldError{{Field: "expectedEventVersion",
			Message: fmt.Sprintf("current eventVersion is %d", event.EventVersion)}}
		return "", 0, 0, e
	}
	if event.Status == EventCompleted || event.Status == EventCancelled {
		return "", 0, 0, problem.Conflict("event-closed", "the event is closed")
	}
	var segStatus string
	var segBudget int
	if err := tx.QueryRowContext(ctx, `SELECT status,budget FROM event_segments WHERE id=? AND event_id=?`, in.SegmentID, eventID).Scan(&segStatus, &segBudget); errors.Is(err, sql.ErrNoRows) {
		return "", 0, 0, problem.NotFound("segment-not-found", "the segment does not belong to this event")
	} else if err != nil {
		return "", 0, 0, problem.Internal(err)
	}
	if segStatus != "completed" {
		return "", 0, 0, problem.Conflict("segment-not-settled", "finish the segment before correcting its score")
	}
	if in.NewScoreUnits > segBudget*100 {
		return "", 0, 0, problem.Validation("newScoreUnits exceeds the segment budget")
	}
	var before int
	err = tx.QueryRowContext(ctx, `
		SELECT score_units FROM segment_results WHERE segment_id = ? AND subject_id = ?`,
		in.SegmentID, in.SubjectID).Scan(&before)
	if errors.Is(err, sql.ErrNoRows) {
		return "", 0, 0, problem.NotFound("result-not-found",
			"no settled score exists for that subject in that segment")
	}
	if err != nil {
		return "", 0, 0, problem.Internal(fmt.Errorf("room: read segment result: %w", err))
	}

	if _, err := tx.ExecContext(ctx, `
		INSERT INTO score_corrections (id, event_id, segment_id, subject_id, reason,
		                               before_units, after_units, corrected_by, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		id, eventID, in.SegmentID, in.SubjectID, reason, before, in.NewScoreUnits,
		actor.SessionID, now); err != nil {
		return "", 0, 0, problem.Internal(fmt.Errorf("room: insert correction: %w", err))
	}
	// The current score is updated so standings reflect the correction, while the
	// before value remains in the correction row.
	if _, err := tx.ExecContext(ctx, `
		UPDATE segment_results SET score_units = ? WHERE segment_id = ? AND subject_id = ?`,
		in.NewScoreUnits, in.SegmentID, in.SubjectID); err != nil {
		return "", 0, 0, problem.Internal(fmt.Errorf("room: apply correction: %w", err))
	}
	newVersion, err := bumpEventVersion(ctx, tx, eventID, now)
	if err != nil {
		return "", 0, 0, err
	}
	if err := writeAudit(ctx, tx, actor.RoomID, eventID, actor.SessionID, "score.corrected", map[string]any{
		"segmentId": in.SegmentID, "subjectId": in.SubjectID,
		"before": before, "after": in.NewScoreUnits, "reason": reason,
	}, now); err != nil {
		return "", 0, 0, err
	}
	roomVersion, err := bumpRoomVersion(ctx, tx, actor.RoomID, now)
	if err != nil {
		return "", 0, 0, err
	}
	if err := emitOutbox(ctx, tx, actor.RoomID, "state.changed", map[string]any{
		"resource": "event", "eventId": eventID, "roomVersion": roomVersion,
	}, now); err != nil {
		return "", 0, 0, err
	}
	if err := tx.Commit(); err != nil {
		return "", 0, 0, problem.Internal(fmt.Errorf("room: commit correction: %w", err))
	}
	return id, newVersion, now, nil
}

// roundViewer builds the modes.Viewer for a caller in a round.
//
// Secrets are selected from the server-side role, never from a request flag.
// An owner or host has no implicit answer access .
func (s *Service) roundViewerQ(ctx context.Context, q queryer, r Round, actor Actor) (modes.Viewer, error) {
	v := modes.Viewer{SessionID: actor.SessionID}
	if actor.Membership != nil {
		v.MembershipID = actor.Membership.ID
		v.Role = string(actor.Membership.Role)
	}
	if v.Role == "" {
		// An administrative-only owner is treated as a host projection.
		v.Role = string(RoleHost)
	}
	if subject, err := s.roundSubject(r, actor); err == nil {
		v.Subject = subject
	}
	gen, err := s.controlGenerationQ(ctx, q, r.RoomID, r.ID, actor.MembershipID())
	if err != nil {
		return modes.Viewer{}, err
	}
	v.CanControl = gen != 0
	// Only a designated referee may see answer material, and that is a host
	// capability plus an explicit revealed phase. It is never inferred.
	v.IsReferee = false
	return v, nil
}

// EventActor bundles an event with the caller's resolved actor.
//
// The HTTP layer resolves authorisation once and then calls the "As" variants, so
// each write does not repeat the membership lookup.
type EventActor struct {
	Event Event
	Actor Actor
}

// GetEventForActor resolves an event and the caller's actor for its room.
func (s *Service) GetEventForActor(ctx context.Context, sessionID, eventID string) (EventActor, error) {
	e, err := s.loadEvent(ctx, eventID)
	if err != nil {
		return EventActor{}, err
	}
	_, actor, err := s.GetRoom(ctx, sessionID, e.RoomID)
	if err != nil {
		return EventActor{}, err
	}
	return EventActor{Event: e, Actor: actor}, nil
}

// CreateEventTransitionAs performs an event transition for a resolved actor.
func (s *Service) CreateEventTransitionAs(ctx context.Context, ea EventActor, target string, expectedVersion int64, reason string) (Event, error) {
	return s.CreateEventTransition(ctx, ea.Actor, ea.Event.ID, target, expectedVersion, reason)
}

// CreateSegmentAs appends a segment for a resolved actor.
func (s *Service) CreateSegmentAs(ctx context.Context, ea EventActor, in SegmentCreateInput) (Segment, error) {
	return s.CreateSegment(ctx, ea.Actor, ea.Event.ID, in)
}

// UpdateSegmentAs edits a segment for a resolved actor.
func (s *Service) UpdateSegmentAs(ctx context.Context, ea EventActor, segmentID string, in SegmentPatchInput) (Segment, error) {
	return s.UpdateSegment(ctx, ea.Actor, ea.Event.ID, segmentID, in)
}

// CreateScoreCorrectionAs records a correction for a resolved actor.
func (s *Service) CreateScoreCorrectionAs(ctx context.Context, ea EventActor, in CorrectionInput) (string, int64, int64, error) {
	return s.CreateScoreCorrection(ctx, ea.Actor, ea.Event.ID, in)
}

// CreateDisplayGrantAs issues a display grant for a resolved actor.
func (s *Service) CreateDisplayGrantAs(ctx context.Context, ea EventActor) (Grant, error) {
	return s.CreateDisplayGrant(ctx, ea.Actor, ea.Event.ID)
}

// RoomOfMembership resolves the room that owns a membership.
func (s *Service) RoomOfMembership(ctx context.Context, membershipID string) (string, error) {
	var roomID string
	err := s.db.Read().QueryRowContext(ctx,
		`SELECT room_id FROM memberships WHERE id = ?`, membershipID).Scan(&roomID)
	if errors.Is(err, sql.ErrNoRows) {
		return "", problem.NotFound("membership-not-found", "the membership does not exist")
	}
	if err != nil {
		return "", problem.Internal(fmt.Errorf("room: resolve membership room: %w", err))
	}
	return roomID, nil
}

// MembershipDisplayName reads a membership's display name.
func (s *Service) MembershipDisplayName(ctx context.Context, membershipID string) (string, error) {
	m, err := s.loadMembershipByID(ctx, membershipID)
	if err != nil {
		return "", err
	}
	return m.DisplayName, nil
}

// Public room numbers distinguish same-name individual entrants without exposing
// sessions. Membership numbers are never assigned to a team or used as credentials.
func (s *Service) subjectPublicCodes(ctx context.Context, roomID string) (map[string]string, error) {
	rows, err := s.db.Read().QueryContext(ctx, `SELECT id, public_code FROM memberships WHERE room_id = ?`, roomID)
	if err != nil {
		return nil, problem.Internal(err)
	}
	defer rows.Close()
	out := map[string]string{}
	for rows.Next() {
		var id, code string
		if err := rows.Scan(&id, &code); err != nil {
			return nil, problem.Internal(err)
		}
		if len(code) != 6 {
			continue
		}
		valid := true
		for _, r := range code {
			if r < '0' || r > '9' {
				valid = false
				break
			}
		}
		if valid {
			out[id] = code
		}
	}
	return out, rows.Err()
}
