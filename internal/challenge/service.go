package challenge

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"regexp"
	"time"

	"github.com/bhcn/umaguessinggame/internal/deployment"
	"github.com/bhcn/umaguessinggame/internal/httpapi/problem"
	"github.com/bhcn/umaguessinggame/internal/modes"
	"github.com/bhcn/umaguessinggame/internal/modes/classic"
	"github.com/bhcn/umaguessinggame/internal/storage"
)

type Service struct {
	db        *storage.DB
	source    modes.ContentSource
	policy    deployment.Policy
	keys      *Keyring
	now       func() time.Time
	admitted  bool
	maxActive int
}

func New(db *storage.DB, source modes.ContentSource, keys *Keyring, policy deployment.Policy, admitted bool) *Service {
	return &Service{db: db, source: source, keys: keys, policy: policy, admitted: admitted, now: time.Now, maxActive: policy.ResolvedLimits().VerifiedChallenges}
}

type CreateInput struct {
	ModeID          string `json:"modeId"`
	ClientTimestamp int64  `json:"clientTimestamp"`
	Nonce           string `json:"nonce"`
	WrongBudget     int    `json:"wrongBudget"`
}
type GuessInput struct {
	Ticket           string `json:"ticket"`
	CharacterID      string `json:"characterId"`
	ExpectedRevision int    `json:"expectedRevision"`
}
type FinishInput struct {
	Ticket           string `json:"ticket"`
	ExpectedRevision int    `json:"expectedRevision"`
}
type Answer struct {
	CharacterID string `json:"characterId"`
	DisplayName string `json:"displayName"`
	AvatarPath  string `json:"avatarPath"`
}
type View struct {
	CharacterIDs   []string           `json:"characterIds"`
	ChallengeID    string             `json:"challengeId"`
	ModeID         string             `json:"modeId"`
	RulesVersion   string             `json:"rulesVersion"`
	Ticket         string             `json:"ticket"`
	ExpiresAt      string             `json:"expiresAt"`
	ServerTime     string             `json:"serverTime"`
	WrongBudget    int                `json:"wrongBudget"`
	RemainingWrong int                `json:"remainingWrong"`
	Revision       int                `json:"revision"`
	Status         string             `json:"status"`
	Attempts       int                `json:"attempts"`
	Rows           []classic.GuessRow `json:"rows"`
	ScoreUnits     int                `json:"scoreUnits"`
	Answer         *Answer            `json:"answer,omitempty"`
}
type ticketClaims struct {
	ID          string `json:"id"`
	Session     string `json:"session"`
	Mode        string `json:"mode"`
	Rules       string `json:"rules"`
	Content     string `json:"content"`
	Budget      int    `json:"budget"`
	Issued      int64  `json:"issued"`
	Expires     int64  `json:"expires"`
	ContextHash string `json:"contextHash"`
}
type record struct {
	ID, Session, Content, Answer, Status, Ticket string
	Budget, Revision                             int
	Rows                                         []classic.GuessRow
	Created, Expires, Updated                    int64
}

var noncePattern = regexp.MustCompile(`^[a-zA-Z0-9_-]{16,80}$`)
var requestKeyPattern = regexp.MustCompile(`^[a-zA-Z0-9._:-]{8,128}$`)

func digest(raw []byte) string  { sum := sha256.Sum256(raw); return hex.EncodeToString(sum[:]) }
func millis(t time.Time) int64  { return t.UTC().UnixMilli() }
func timestamp(ms int64) string { return time.UnixMilli(ms).UTC().Format(time.RFC3339Nano) }

func (s *Service) Create(ctx context.Context, sessionID, key string, in CreateInput) (View, error) {
	if sessionID == "" || !requestKeyPattern.MatchString(key) {
		return View{}, problem.Validation("invalid challenge request")
	}
	if in.ModeID != "classic" || in.WrongBudget < 4 || in.WrongBudget > 12 || !noncePattern.MatchString(in.Nonce) {
		return View{}, problem.Validation("invalid challenge settings")
	}
	payload, _ := json.Marshal(in)
	hash := digest(payload)
	tx, err := s.db.Write().BeginTx(ctx, nil)
	if err != nil {
		return View{}, problem.Internal(err)
	}
	defer tx.Rollback()
	if view, ok, err := replay(ctx, tx, sessionID, "create", key, hash); ok || err != nil {
		return view, err
	}
	if !s.policy.Allowed("classic", "verified") {
		return View{}, problem.Forbidden("play-disabled", "this play format is closed on this instance")
	}
	if !s.admitted || s.source == nil {
		return View{}, problem.Unprocessable("challenge-unavailable", "challenge content is unavailable")
	}
	now := millis(s.now())
	if in.ClientTimestamp < now-600000 || in.ClientTimestamp > now+600000 {
		return View{}, problem.Unprocessable("device-clock-mismatch", "device time is too far from challenge time")
	}
	// Bounded retention and allocation; no per-challenge timer or broadcaster.
	if _, err := tx.ExecContext(ctx, `DELETE FROM verified_challenges WHERE id IN (SELECT id FROM verified_challenges WHERE expires_at < ? ORDER BY expires_at LIMIT 256)`, now-24*3600*1000); err != nil {
		return View{}, problem.Internal(err)
	}
	var active, total, recent, ownActive int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*),COALESCE(SUM(CASE WHEN status='active' AND expires_at>? THEN 1 ELSE 0 END),0) FROM verified_challenges`, now).Scan(&total, &active); err != nil {
		return View{}, problem.Internal(err)
	}
	if total >= s.maxActive*8 || active >= s.maxActive {
		return View{}, problem.Busy(5)
	}
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*),COALESCE(SUM(CASE WHEN status='active' AND expires_at>? THEN 1 ELSE 0 END),0) FROM verified_challenges WHERE session_id=? AND created_at>?`, now, sessionID, now-3600000).Scan(&recent, &ownActive); err != nil {
		return View{}, problem.Internal(err)
	}
	if recent >= 24 || ownActive >= 3 {
		return View{}, problem.RateLimited(60)
	}
	pool, err := classic.PracticeCharacters(s.source, false)
	if err != nil {
		return View{}, problem.Internal(err)
	}
	eligible := make([]classic.Character, 0, len(pool))
	for _, c := range pool {
		if c.AmbiguityGroup == "" {
			eligible = append(eligible, c)
		}
	}
	if len(eligible) < 2 {
		return View{}, problem.Unprocessable("challenge-unavailable", "too few unambiguous characters")
	}
	// Unpredictable server entropy decides the answer. Client timestamp/nonce is
	// context only and never determines score, deadline or the selected answer.
	index, err := rand.Int(rand.Reader, big.NewInt(int64(len(eligible))))
	if err != nil {
		return View{}, problem.Internal(err)
	}
	poolJSON, err := json.Marshal(pool)
	if err != nil {
		return View{}, problem.Internal(err)
	}
	contentID := s.source.Version() + ":" + digest(poolJSON)
	if _, err := tx.ExecContext(ctx, `INSERT INTO verified_content(id,characters_json,created_at) VALUES(?,?,?) ON CONFLICT(id) DO NOTHING`, contentID, string(poolJSON), now); err != nil {
		return View{}, problem.Internal(err)
	}
	id, err := randomID("chl_")
	if err != nil {
		return View{}, problem.Internal(err)
	}
	r := record{ID: id, Session: sessionID, Content: contentID, Answer: eligible[index.Int64()].ID, Budget: in.WrongBudget, Revision: 0, Status: "active", Rows: []classic.GuessRow{}, Created: now, Expires: now + 20*60*1000, Updated: now}
	claims := ticketClaims{ID: id, Session: sessionID, Mode: "classic", Rules: classic.RulesVersion, Content: contentID, Budget: r.Budget, Issued: r.Created, Expires: r.Expires, ContextHash: hash}
	r.Ticket, err = s.keys.seal(claims)
	if err != nil {
		return View{}, problem.Internal(err)
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO verified_challenges(id,session_id,content_id,answer_id,wrong_budget,revision,status,state_json,ticket,created_at,expires_at,updated_at) VALUES(?,?,?,?,?,0,'active','[]',?,?,?,?)`, r.ID, r.Session, r.Content, r.Answer, r.Budget, r.Ticket, r.Created, r.Expires, r.Updated); err != nil {
		return View{}, problem.Internal(err)
	}
	view := r.view(now, pool)
	if err := receipt(ctx, tx, sessionID, "create", key, hash, view, now); err != nil {
		return View{}, err
	}
	if err := tx.Commit(); err != nil {
		return View{}, problem.Internal(err)
	}
	return view, nil
}
func load(ctx context.Context, q interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}, session, id string) (record, error) {
	var r record
	var raw string
	err := q.QueryRowContext(ctx, `SELECT id,session_id,content_id,answer_id,wrong_budget,revision,status,state_json,ticket,created_at,expires_at,updated_at FROM verified_challenges WHERE id=? AND session_id=?`, id, session).Scan(&r.ID, &r.Session, &r.Content, &r.Answer, &r.Budget, &r.Revision, &r.Status, &raw, &r.Ticket, &r.Created, &r.Expires, &r.Updated)
	if errors.Is(err, sql.ErrNoRows) {
		return r, problem.NotFound("challenge-not-found", "challenge not found")
	}
	if err != nil {
		return r, problem.Internal(err)
	}
	if err = json.Unmarshal([]byte(raw), &r.Rows); err != nil {
		return r, problem.Internal(err)
	}
	return r, nil
}
func content(ctx context.Context, q interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}, id string) ([]classic.Character, error) {
	var raw string
	if err := q.QueryRowContext(ctx, `SELECT characters_json FROM verified_content WHERE id=?`, id).Scan(&raw); err != nil {
		return nil, problem.Internal(err)
	}
	var pool []classic.Character
	if err := json.Unmarshal([]byte(raw), &pool); err != nil {
		return nil, problem.Internal(err)
	}
	return pool, nil
}
func (s *Service) Get(ctx context.Context, session, id string) (View, error) {
	r, err := load(ctx, s.db.Read(), session, id)
	if err != nil {
		return View{}, err
	}
	pool, err := content(ctx, s.db.Read(), r.Content)
	if err != nil {
		return View{}, err
	}
	return r.view(millis(s.now()), pool), nil
}
func (s *Service) Guess(ctx context.Context, session, id, key string, in GuessInput) (View, error) {
	return s.act(ctx, session, id, key, in.Ticket, in.ExpectedRevision, in.CharacterID, false)
}
func (s *Service) Finish(ctx context.Context, session, id, key string, in FinishInput) (View, error) {
	return s.act(ctx, session, id, key, in.Ticket, in.ExpectedRevision, "", true)
}
func (s *Service) act(ctx context.Context, session, id, key, ticket string, revision int, characterID string, finish bool) (View, error) {
	if !requestKeyPattern.MatchString(key) || revision < 0 || revision > 13 {
		return View{}, problem.Validation("invalid challenge action")
	}
	claims, err := s.keys.open(ticket)
	if err != nil || claims.ID != id || claims.Session != session || claims.Mode != "classic" || claims.Rules != classic.RulesVersion {
		return View{}, problem.Forbidden("challenge-ticket-invalid", "challenge credential is invalid")
	}
	operation := "guess:" + id
	if finish {
		operation = "finish:" + id
	}
	payload, _ := json.Marshal([]any{ticket, revision, characterID, finish})
	hash := digest(payload)
	tx, err := s.db.Write().BeginTx(ctx, nil)
	if err != nil {
		return View{}, problem.Internal(err)
	}
	defer tx.Rollback()
	r, err := load(ctx, tx, session, id)
	if err != nil {
		return View{}, err
	}
	if claims.Content != r.Content || claims.Budget != r.Budget || claims.Issued != r.Created || claims.Expires != r.Expires || ticket != r.Ticket {
		return View{}, problem.Forbidden("challenge-ticket-invalid", "challenge credential does not match")
	}
	// Replay before expiry/revision/terminal checks, including after the final solve.
	if view, ok, err := replay(ctx, tx, session, operation, key, hash); ok || err != nil {
		return view, err
	}
	now := millis(s.now())
	if now >= r.Expires {
		return View{}, problem.Conflict("challenge-expired", "challenge has ended")
	}
	if r.Status != "active" {
		return View{}, problem.Conflict("challenge-ended", "challenge has ended")
	}
	if r.Revision != revision {
		return View{}, problem.Conflict("challenge-revision-conflict", "newer challenge progress exists")
	}
	pool, err := content(ctx, tx, r.Content)
	if err != nil {
		return View{}, err
	}
	if finish {
		r.Status = "ended"
	} else {
		var guess, answer classic.Character
		for _, c := range pool {
			if c.ID == characterID {
				guess = c
			}
			if c.ID == r.Answer {
				answer = c
			}
		}
		if guess.ID == "" || answer.ID == "" {
			return View{}, problem.Validation("unknown character")
		}
		for _, row := range r.Rows {
			if row.CharacterID == characterID {
				return View{}, problem.Conflict("duplicate-guess", "character has already been guessed")
			}
		}
		row := classic.BuildGuessRow(guess, answer)
		row.Attempt = len(r.Rows) + 1
		row.Correct = characterID == r.Answer
		r.Rows = append(r.Rows, row)
		if row.Correct {
			r.Status = "solved"
		} else if r.remaining() == 0 {
			r.Status = "failed"
		}
	}
	r.Revision++
	r.Updated = now
	raw, err := json.Marshal(r.Rows)
	if err != nil {
		return View{}, problem.Internal(err)
	}
	if _, err := tx.ExecContext(ctx, `UPDATE verified_challenges SET state_json=?,status=?,revision=?,updated_at=? WHERE id=?`, string(raw), r.Status, r.Revision, r.Updated, id); err != nil {
		return View{}, problem.Internal(err)
	}
	view := r.view(now, pool)
	if err := receipt(ctx, tx, session, operation, key, hash, view, now); err != nil {
		return View{}, err
	}
	if err := tx.Commit(); err != nil {
		return View{}, problem.Internal(err)
	}
	return view, nil
}
func (r record) remaining() int {
	wrong := 0
	for _, row := range r.Rows {
		if !row.Correct {
			wrong++
		}
	}
	return max(0, r.Budget-wrong)
}
func (r record) view(now int64, pool []classic.Character) View {
	status := r.Status
	if status == "active" && now >= r.Expires {
		status = "expired"
	}
	v := View{ChallengeID: r.ID, ModeID: "classic", RulesVersion: classic.RulesVersion, Ticket: r.Ticket, ExpiresAt: timestamp(r.Expires), ServerTime: timestamp(now), WrongBudget: r.Budget, RemainingWrong: r.remaining(), Revision: r.Revision, Status: status, Attempts: len(r.Rows), Rows: r.Rows, ScoreUnits: classic.ActivityScore(len(r.Rows), status == "solved", r.Budget)}
	for _, c := range pool {
		v.CharacterIDs = append(v.CharacterIDs, c.ID)
	}
	if status != "active" {
		for _, c := range pool {
			if c.ID == r.Answer {
				v.Answer = &Answer{CharacterID: c.ID, DisplayName: c.Name, AvatarPath: c.ImagePath}
				break
			}
		}
	}
	return v
}
func replay(ctx context.Context, tx *sql.Tx, session, operation, key, hash string) (View, bool, error) {
	var oldHash, raw string
	var view View
	err := tx.QueryRowContext(ctx, `SELECT payload_hash,response_json FROM verified_receipts WHERE session_id=? AND operation=? AND request_key=?`, session, operation, key).Scan(&oldHash, &raw)
	if errors.Is(err, sql.ErrNoRows) {
		return view, false, nil
	}
	if err != nil {
		return view, false, problem.Internal(err)
	}
	if oldHash != hash {
		return view, false, problem.Conflict("idempotency-payload-conflict", "request key was used for a different action")
	}
	if err = json.Unmarshal([]byte(raw), &view); err != nil {
		return view, false, problem.Internal(err)
	}
	return view, true, nil
}
func receipt(ctx context.Context, tx *sql.Tx, session, operation, key, hash string, view View, now int64) error {
	raw, err := json.Marshal(view)
	if err != nil {
		return problem.Internal(err)
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO verified_receipts(session_id,operation,request_key,challenge_id,payload_hash,response_json,created_at)VALUES(?,?,?,?,?,?,?)`, session, operation, key, view.ChallengeID, hash, string(raw), now)
	if err != nil {
		return problem.Internal(fmt.Errorf("challenge receipt: %w", err))
	}
	return nil
}
