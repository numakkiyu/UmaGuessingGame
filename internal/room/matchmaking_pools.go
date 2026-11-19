package room

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"strconv"

	"github.com/bhcn/umaguessinggame/internal/httpapi/problem"
)

// MatchPool is a fixed ruleset; clients cannot fragment a queue with custom rules.
type MatchPool struct {
	ID         string          `json:"poolId"`
	ModeID     string          `json:"modeId"`
	Players    int             `json:"players"`
	ModeConfig json.RawMessage `json:"modeConfig"`
	Queued     int             `json:"queued"`
	Confirming int             `json:"confirming"`
	Playing    int             `json:"playing"`
	Rooms      int             `json:"rooms"`
}

var matchPools = []MatchPool{
	{ID: "classic-duel", ModeID: "classic", Players: 2, ModeConfig: json.RawMessage(`{"modeId":"classic","variant":"friend","maxWrongGuesses":8,"winPolicy":"first-correct","durationSeconds":180,"shadowEnabled":false}`)},
	{ID: "classic-four", ModeID: "classic", Players: 4, ModeConfig: json.RawMessage(`{"modeId":"classic","variant":"public","maxWrongGuesses":8,"winPolicy":"first-correct","durationSeconds":180,"shadowEnabled":false}`)},
	{ID: "sort-four", ModeID: "sort", Players: 4, ModeConfig: json.RawMessage(`{"modeId":"sort","informationStyle":"closed","config":{"key":"height","cardCount":6,"questionSeconds":90,"eventRegion":"JP"}}`)},
	{ID: "memory-four", ModeID: "memory", Players: 4, ModeConfig: json.RawMessage(`{"modeId":"memory","config":{"viewSeconds":8,"answerSeconds":90,"maxAttempts":20}}`)},
}

func (s *Service) MatchmakingLobbyEnabled() bool { return s.cfg.Policy.Matchmaking.LobbyOpen() }
func (s *Service) matchPool(id string) (MatchPool, error) {
	for _, p := range matchPools {
		if p.ID == id {
			if !s.cfg.Policy.Allowed(p.ModeID, "matchmaking") {
				return MatchPool{}, problem.Forbidden("play-disabled", "this matchmaking pool is closed")
			}
			if s.modes == nil {
				return MatchPool{}, problem.NotImplemented("modes-unavailable", "no game mode is registered")
			}
			m, ok := s.modes.Get(p.ModeID)
			if !ok || !s.ModeAvailability(m).Available {
				return MatchPool{}, problem.Unprocessable("matchmaking-mode-unavailable", "this mode is not available")
			}
			if limit := s.cfg.Policy.Limits.PlayersPerRoom; limit > 0 && p.Players > limit {
				return MatchPool{}, problem.Forbidden("play-disabled", "this pool exceeds the configured player limit")
			}
			if err := s.validateModeConfig(p.ModeConfig); err != nil {
				return MatchPool{}, err
			}
			return p, nil
		}
	}
	return MatchPool{}, problem.Validation("unknown matchmaking pool")
}

// MatchProof is a short-lived, session-bound work challenge. Its random ID is the nonce prefix.
type MatchProof struct {
	ChallengeID string `json:"challengeId"`
	Bits        int    `json:"bits"`
	ExpiresAt   int64  `json:"-"`
}
type MatchProofAnswer struct {
	ChallengeID string `json:"challengeId"`
	Nonce       string `json:"nonce"`
}

func ValidMatchProof(id, nonce string, bits int) bool {
	if len(nonce) == 0 || len(nonce) > 16 || bits < 1 || bits > 24 {
		return false
	}
	for _, digit := range nonce {
		if digit < '0' || digit > '9' {
			return false
		}
	}
	if _, err := strconv.ParseUint(nonce, 10, 64); err != nil {
		return false
	}
	sum := sha256.Sum256([]byte(id + ":" + nonce))
	for i := 0; i < bits; i++ {
		if sum[i/8]&(1<<uint(7-i%8)) != 0 {
			return false
		}
	}
	return true
}

func (s *Service) CreateMatchProof(ctx context.Context, sessionID, poolID string) (MatchProof, error) {
	if _, err := s.matchPool(poolID); err != nil {
		return MatchProof{}, err
	}
	id, err := newID("mwp_")
	if err != nil {
		return MatchProof{}, problem.Internal(err)
	}
	now := s.nowMS()
	p := MatchProof{ChallengeID: id, Bits: s.cfg.Policy.Matchmaking.Difficulty(), ExpiresAt: now + 90000}
	tx, err := s.db.Write().BeginTx(ctx, nil)
	if err != nil {
		return MatchProof{}, problem.Internal(err)
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, `DELETE FROM matchmaking_proofs WHERE expires_at<=?`, now); err != nil {
		return MatchProof{}, problem.Internal(err)
	}
	var count int
	if err = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM matchmaking_proofs WHERE session_id<>?`, sessionID).Scan(&count); err != nil {
		return MatchProof{}, problem.Internal(err)
	}
	if count >= s.cfg.Policy.Matchmaking.Capacity()*2 {
		return MatchProof{}, problem.Busy(10)
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO matchmaking_proofs(id,session_id,pool_id,bits,expires_at) VALUES(?,?,?,?,?) ON CONFLICT(session_id) DO UPDATE SET id=excluded.id,pool_id=excluded.pool_id,bits=excluded.bits,expires_at=excluded.expires_at,used=0`, id, sessionID, poolID, p.Bits, p.ExpiresAt); err != nil {
		return MatchProof{}, problem.Internal(err)
	}
	return p, tx.Commit()
}

func consumeMatchProof(ctx context.Context, tx *sql.Tx, sid, poolID string, p MatchProofAnswer, now int64) error {
	var bits int
	err := tx.QueryRowContext(ctx, `SELECT bits FROM matchmaking_proofs WHERE id=? AND session_id=? AND pool_id=? AND used=0 AND expires_at>?`, p.ChallengeID, sid, poolID, now).Scan(&bits)
	if errors.Is(err, sql.ErrNoRows) {
		return problem.Forbidden("matchmaking-proof-required", "please prepare matchmaking admission again")
	}
	if err != nil {
		return problem.Internal(err)
	}
	if !ValidMatchProof(p.ChallengeID, p.Nonce, bits) {
		return problem.Forbidden("matchmaking-proof-invalid", "matchmaking admission could not be confirmed")
	}
	_, err = tx.ExecContext(ctx, `UPDATE matchmaking_proofs SET used=1 WHERE id=?`, p.ChallengeID)
	if err != nil {
		return problem.Internal(err)
	}
	return nil
}
