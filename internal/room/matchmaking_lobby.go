package room

import (
	"context"
	"github.com/bhcn/umaguessinggame/internal/httpapi/problem"
	"time"
)

type MatchLobby struct {
	IdleSeconds int         `json:"idleSeconds"`
	Pools       []MatchPool `json:"pools"`
	Queued      int         `json:"queued"`
	Confirming  int         `json:"confirming"`
	Playing     int         `json:"playing"`
	Rooms       int         `json:"rooms"`
	ServerTime  string      `json:"serverTime"`
}

// MatchmakingLobby reports counts only; no session or participant identifiers escape.
func (s *Service) MatchmakingLobby(ctx context.Context) (MatchLobby, error) {
	if !s.MatchmakingLobbyEnabled() {
		return MatchLobby{}, problem.Forbidden("matchmaking-lobby-disabled", "the matchmaking lobby is closed")
	}
	s.matchStatsMu.Lock()
	defer s.matchStatsMu.Unlock()
	now := s.nowMS()
	if now >= s.matchStatsAt && now-s.matchStatsAt < 2000 && s.matchStats.Pools != nil {
		return s.matchStats, nil
	}
	if err := s.SweepMatchmaking(ctx); err != nil {
		return MatchLobby{}, err
	}
	lobby := MatchLobby{IdleSeconds: s.cfg.Policy.Matchmaking.IdleWindow(), Pools: []MatchPool{}, ServerTime: time.UnixMilli(now).UTC().Format(time.RFC3339Nano)}
	for _, base := range matchPools {
		p, err := s.matchPool(base.ID)
		if err != nil {
			continue
		}
		if err = s.db.Read().QueryRowContext(ctx, `SELECT COALESCE(SUM(CASE WHEN t.status='queueing' AND t.lease_until>? THEN 1 ELSE 0 END),0),COALESCE(SUM(CASE WHEN t.status IN ('matched-pending-accept','ready') AND t.room_id IS NULL AND t.lease_until>? THEN 1 ELSE 0 END),0),COALESCE(SUM(CASE WHEN t.status IN ('playing','ready') AND t.room_id IS NOT NULL AND EXISTS(SELECT 1 FROM rounds r WHERE r.room_id=t.room_id AND r.status NOT IN ('settled','cancelled')) THEN 1 ELSE 0 END),0),COUNT(DISTINCT CASE WHEN t.status IN ('playing','ready') AND t.room_id IS NOT NULL AND EXISTS(SELECT 1 FROM rounds r WHERE r.room_id=t.room_id AND r.status NOT IN ('settled','cancelled')) THEN t.room_id END) FROM matchmaking_tickets t WHERE t.pool_id=?`, now, now, p.ID).Scan(&p.Queued, &p.Confirming, &p.Playing, &p.Rooms); err != nil {
			return MatchLobby{}, problem.Internal(err)
		}
		lobby.Pools = append(lobby.Pools, p)
		lobby.Queued += p.Queued
		lobby.Confirming += p.Confirming
		lobby.Playing += p.Playing
		lobby.Rooms += p.Rooms
	}
	s.matchStats = lobby
	s.matchStatsAt = now
	return lobby, nil
}
