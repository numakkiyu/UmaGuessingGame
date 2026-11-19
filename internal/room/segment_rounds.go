package room

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"reflect"
	"sort"

	"github.com/bhcn/umaguessinggame/internal/httpapi/problem"
	"github.com/bhcn/umaguessinggame/internal/modes"
)

type segmentRules struct {
	ModeConfig  json.RawMessage `json:"modeConfig"`
	RoundCount  int             `json:"roundCount"`
	Competitive *bool           `json:"competitive"`
}

func readSegmentRules(raw []byte) (segmentRules, error) {
	var cfg segmentRules
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return cfg, problem.Internal(fmt.Errorf("room: segment rules: %w", err))
	}
	if cfg.RoundCount == 0 {
		cfg.RoundCount = 1
	} // Legacy single-round programme.
	if cfg.RoundCount < 1 || cfg.RoundCount > 20 {
		return cfg, problem.Internal(errors.New("room: invalid stored round count"))
	}
	return cfg, nil
}

func sameJSON(a, b []byte) bool {
	var x, y any
	return json.Unmarshal(a, &x) == nil && json.Unmarshal(b, &y) == nil && reflect.DeepEqual(x, y)
}

// validateSegmentRoundQ runs before preparation and again under the write lock.
// Possessing a room's host capability never authorises binding another event.
func (s *Service) validateSegmentRoundQ(ctx context.Context, q queryer, roomID string, in RoundCreateInput, subjects []modes.Subject) (int, error) {
	var config, status, eventStatus, roster, legacyIDs string
	var count int
	err := q.QueryRowContext(ctx, `SELECT es.config_json,es.status,ev.status,
 (SELECT COUNT(*) FROM event_segment_rounds sr WHERE sr.segment_id=es.id),
 COALESCE((SELECT roster_json FROM event_segment_rounds sr WHERE sr.segment_id=es.id ORDER BY ordinal LIMIT 1),'[]'),
 COALESCE((SELECT json_group_array(rr.subject_id) FROM round_results rr JOIN event_segment_rounds sr ON sr.round_id=rr.round_id WHERE sr.segment_id=es.id AND sr.ordinal=1),'[]')
 FROM event_segments es JOIN event_sessions ev ON ev.id=es.event_id
 WHERE es.id=? AND ev.room_id=?`, in.SegmentID, roomID).Scan(&config, &status, &eventStatus, &count, &roster, &legacyIDs)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, problem.NotFound("segment-not-found", "the segment does not belong to this room")
	}
	if err != nil {
		return 0, problem.Internal(fmt.Errorf("room: check segment binding: %w", err))
	}
	if eventStatus == string(EventCompleted) || eventStatus == string(EventCancelled) {
		return 0, problem.Conflict("event-closed", "this event is closed")
	}
	if status == "completed" || status == "voided" {
		return 0, problem.Conflict("segment-closed", "this segment is closed")
	}
	cfg, err := readSegmentRules([]byte(config))
	if err != nil {
		return 0, err
	}
	if !sameJSON(cfg.ModeConfig, in.ModeConfig) {
		return 0, problem.Conflict("segment-rules-mismatch", "round rules must equal the programme rules")
	}
	if count >= cfg.RoundCount {
		return 0, problem.Conflict("segment-rounds-complete", "all planned rounds have already been created")
	}
	if count > 0 && subjects != nil {
		var first []modes.Subject
		if err := json.Unmarshal([]byte(roster), &first); err != nil {
			return 0, problem.Internal(err)
		}
		ids := make([]string, 0, len(first))
		for _, v := range first {
			ids = append(ids, string(v.Kind)+":"+v.ID)
		}
		current := make([]string, 0, len(subjects))
		for _, v := range subjects {
			current = append(current, string(v.Kind)+":"+v.ID)
		}
		if len(first) == 0 { // Backfilled old rounds have immutable results, not a roster.
			if err := json.Unmarshal([]byte(legacyIDs), &ids); err != nil {
				return 0, problem.Internal(err)
			}
			current = current[:0]
			for _, v := range subjects {
				current = append(current, v.ID)
			}
		}
		sort.Strings(ids)
		sort.Strings(current)
		if !reflect.DeepEqual(ids, current) {
			return 0, problem.Conflict("segment-roster-changed", "restore this segment's original scoring subjects before continuing")
		}
	}
	return count + 1, nil
}

// aggregateSegmentResultsQ retains round results and applies one fixed programme
// denominator. Exact fractions avoid losing one unit to floating-point floor.
func (s *Service) aggregateSegmentResultsQ(ctx context.Context, tx *sql.Tx, roundID string) error {
	var segID, raw string
	var budget int
	err := tx.QueryRowContext(ctx, `SELECT es.id,es.config_json,es.budget FROM event_segments es
 JOIN event_segment_rounds sr ON sr.segment_id=es.id WHERE sr.round_id=?`, roundID).Scan(&segID, &raw, &budget)
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
	rows, err := tx.QueryContext(ctx, `SELECT rr.subject_id,rr.score_units,rr.max_score_units FROM round_results rr
 JOIN event_segment_rounds sr ON sr.round_id=rr.round_id JOIN rounds r ON r.id=rr.round_id
 WHERE sr.segment_id=? AND r.status='settled'`, segID)
	if err != nil {
		return problem.Internal(err)
	}
	totals := map[string]*big.Rat{}
	for rows.Next() {
		var id string
		var score, max int64
		if err := rows.Scan(&id, &score, &max); err != nil {
			rows.Close()
			return problem.Internal(err)
		}
		if max <= 0 || score < 0 || score > max {
			rows.Close()
			return problem.Internal(errors.New("room: invalid event score range"))
		}
		if totals[id] == nil {
			totals[id] = new(big.Rat)
		}
		totals[id].Add(totals[id], big.NewRat(score, max))
	}
	readErr := rows.Err()
	rows.Close()
	if readErr != nil {
		return problem.Internal(readErr)
	}
	for id, total := range totals {
		total.Mul(total, big.NewRat(int64(budget*100), int64(cfg.RoundCount)))
		score := new(big.Int).Quo(total.Num(), total.Denom()).Int64()
		if _, err := tx.ExecContext(ctx, `INSERT INTO segment_results (segment_id,subject_id,score_units,max_score_units) VALUES (?,?,?,?)
 ON CONFLICT(segment_id,subject_id) DO UPDATE SET score_units=excluded.score_units,max_score_units=excluded.max_score_units`, segID, id, score, budget*100); err != nil {
			return problem.Internal(err)
		}
	}
	return nil
}

// EntertainmentOnlyMode keeps negotiated exchanges and subjective votes outside
// formal event standings. Their own round results remain available as awards.
func EntertainmentOnlyMode(mode string) bool { return mode == "creative" || mode == "trade" }
func (seg Segment) Competitive() bool {
	if EntertainmentOnlyMode(seg.ModeID) {
		return false
	}
	cfg, err := readSegmentRules(seg.Config)
	return err == nil && (cfg.Competitive == nil || *cfg.Competitive)
}
func checkSegmentScoring(mode string, competitive bool) error {
	if competitive && EntertainmentOnlyMode(mode) {
		return problem.Unprocessable("event-entertainment-only", "this mode is an entertainment item and cannot contribute to formal standings")
	}
	return nil
}

// attachEntertainmentResults exposes settled aggregate facts independently of
// formal standings. No works, ballots, private objectives or live answers enter it.
func (s *Service) attachEntertainmentResults(ctx context.Context, roomID, eventID string, segs []Segment) error {
	indices := map[string]int{}
	for i, seg := range segs {
		if !seg.Competitive() && seg.Status != "voided" {
			indices[seg.ID] = i
		}
	}
	if len(indices) == 0 {
		return nil
	}
	names, err := s.subjectDisplayNames(ctx, roomID)
	if err != nil {
		return err
	}
	codes, err := s.subjectPublicCodes(ctx, roomID)
	if err != nil {
		return err
	}
	rows, err := s.db.Read().QueryContext(ctx, `SELECT sr.segment_id,sr.subject_id,sr.score_units,sr.max_score_units FROM segment_results sr JOIN event_segments es ON es.id=sr.segment_id WHERE es.event_id=? AND es.status<>'voided' ORDER BY sr.subject_id`, eventID)
	if err != nil {
		return problem.Internal(err)
	}
	defer rows.Close()
	for rows.Next() {
		var segID, id string
		var score, max int
		if err := rows.Scan(&segID, &id, &score, &max); err != nil {
			return problem.Internal(err)
		}
		if index, ok := indices[segID]; ok {
			segs[index].Results = append(segs[index].Results, EventItemResult{SubjectID: id, DisplayName: names[id], PublicCode: codes[id], ScoreUnits: score, MaxUnits: max})
		}
	}
	return rows.Err()
}
