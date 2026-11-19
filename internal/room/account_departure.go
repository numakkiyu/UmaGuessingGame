package room

import (
	"context"
	"errors"

	"github.com/bhcn/umaguessinggame/internal/httpapi/problem"
	"github.com/bhcn/umaguessinggame/internal/modes"
)

// ReleaseAccountSession leaves through the room rules before the browser's game
// identity is revoked. A departing team member cannot concede for the whole team.
func (s *Service) ReleaseAccountSession(ctx context.Context, sid string) error {
	rows, err := s.db.Read().QueryContext(ctx, `SELECT room_id,id FROM memberships WHERE session_id=? AND left_at IS NULL AND revoked_at IS NULL`, sid)
	if err != nil {
		return problem.Internal(err)
	}
	type seat struct{ roomID, membershipID string }
	var seats []seat
	for rows.Next() {
		var v seat
		if err = rows.Scan(&v.roomID, &v.membershipID); err != nil {
			rows.Close()
			return problem.Internal(err)
		}
		seats = append(seats, v)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return problem.Internal(err)
	}
	for _, v := range seats {
		actor, err := s.LoadActor(ctx, sid, v.roomID)
		if err != nil {
			var p *problem.Error
			if errors.As(err, &p) && (p.Status == 403 || p.Status == 404) {
				continue // A concurrent departure already removed this seat.
			}
			return err
		}
		current, err := s.CurrentRoundSummary(ctx, v.roomID)
		if err != nil {
			return err
		}
		if current != nil && actor.Has(CapPlay) {
			r, err := s.loadRound(ctx, current.ID)
			if err != nil {
				return err
			}
			if (r.Status == modes.PhaseBriefing || r.Status == modes.PhaseCollecting) && (actor.Membership.TeamID == "" || s.individualRoundSeats(r)) {
				if _, _, err = s.CreateSurrender(ctx, actor, r.ID, r.PhaseVersion); err != nil {
					var p *problem.Error
					if !errors.As(err, &p) || (p.Code != "round-not-collecting" && p.Code != "participant-surrendered" && p.Code != "deadline-passed" && p.Code != "matchmaking-idle") {
						return err
					}
				}
			}
		}
		// Relinquishing one's own seat requires no management authority. This also
		// permits a host to leave an already-cancelled room during account cleanup.
		delete(actor.Capabilities, CapManageRoom)
		delete(actor.Capabilities, CapHost)
		if err = s.LeaveOrRemoveMembership(ctx, actor, v.membershipID); err != nil {
			return err
		}
	}
	return nil
}
