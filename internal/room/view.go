package room

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"

	"github.com/bhcn/umaguessinggame/internal/httpapi/problem"
	"github.com/bhcn/umaguessinggame/internal/modes"
	"github.com/bhcn/umaguessinggame/internal/modes/creative"
)

// hashTokenSHA256 hashes a stored secret.
func hashTokenSHA256(v string) string {
	sum := sha256.Sum256([]byte(v))
	return hex.EncodeToString(sum[:])
}

// RoundView is the authorised projection of a round.
//
// PrivateState is populated only for a competing viewer, and only from the
// mode's own Project output. There is no path where a full server state is
// serialised and then filtered in the browser .
type RoundView struct {
	ViewerRole                string
	Round                     Round
	PublicState               json.RawMessage
	PrivateState              json.RawMessage
	Results                   []StoredResult
	RevealedAnswerCharacterID string
	Evidence                  []EvidenceRef
	RoomVersion               int64
	ServerTime                int64
	ControlGeneration         int64
	SubmissionRevision        int64
	// SubmissionRevisionsBySlot carries the live revision for each slot this mode
	// can submit to, so a client never has to guess.
	//
	// A revision is per (round, subject, slot). SubmissionRevision above is a
	// single number and can only be right for a mode with one slot; for a mode with
	// two (creative: work + vote, bluff: decoy + vote) it was the maximum across
	// both, which is the wrong number for the second slot. The map is keyed by the
	// same slot names the mode's action kinds map to, and a slot the subject never
	// submitted to is absent — its first submission must use 0.
	SubmissionRevisionsBySlot map[string]int64
	CanSubmit                 bool
	TeamControllers           []TeamController
	Paused                    bool
	// SubjectNames maps every subject id in this round to a player-facing name.
	//
	// It exists because modes only ever see Subject{ID, Kind}: they cannot resolve a
	// membership or team label, and must not — that is room-layer knowledge. Without
	// this map a console can show only opaque ids ("tem_2cc002ada4...") or has to
	// rebuild the mapping itself from the member list, which duplicates the rule for
	// which id a team round uses.
	//
	// The map covers every subject of the round regardless of viewer, because a name
	// is public within the room: the standings and the submission panel already show
	// these labels to everyone. It never carries private state.
	SubjectNames map[string]string `json:"subjectNames,omitempty"`
}

// BuildRoundView assembles the viewer-specific round projection.
//
// The ordering is deliberate:
// 1. resolve the viewer from server-side records;
// 2. ask the mode to project, passing secrets only because the mode decides;
// 3. add results and the revealed answer only when the phase permits.
func (s *Service) BuildRoundView(ctx context.Context, roundID string, room Room, actor Actor) (RoundView, error) {
	r, err := s.loadRound(ctx, roundID)
	if err != nil {
		return RoundView{}, err
	}
	if r.RoomID != room.ID {
		return RoundView{}, errRoundNotFound
	}

	viewer, err := s.roundViewerQ(ctx, s.db.Read(), r, actor)
	if err != nil {
		return RoundView{}, err
	}

	viewer.CanManageRound = actor.Has(CapHost) || actor.Has(CapManageRoom)
	publicState, err := roundPublicStateQ(ctx, s.db.Read(), r.ID)
	if err != nil {
		return RoundView{}, err
	}
	privateState, err := roundPrivateStateQ(ctx, s.db.Read(), r.ID)
	if err != nil {
		return RoundView{}, err
	}
	states, err := participantStatesQ(ctx, s.db.Read(), r.ID)
	if err != nil {
		return RoundView{}, err
	}

	// A spectator, display or host gets the public projection; only a competing
	// viewer receives a private state.
	competing := actor.Has(CapPlay)

	view := RoundView{
		ViewerRole:  viewer.Role,
		Round:       r,
		RoomVersion: room.RoomVersion,
		ServerTime:  s.nowMS(),
		Paused:      r.Paused(),
	}
	view.TeamControllers, err = s.teamControllersQ(ctx, s.db.Read(), r)
	if err != nil {
		return RoundView{}, err
	}
	view.CanSubmit = competing
	if actor.Membership != nil && actor.Membership.TeamID != "" && !s.individualRoundSeats(r) {
		view.CanSubmit = false
		for _, controller := range view.TeamControllers {
			if controller.TeamID == actor.Membership.TeamID {
				view.CanSubmit = controller.MembershipID == actor.MembershipID()
			}
		}
	}
	viewer.CanControl = view.CanSubmit

	/*
	 * 揭晓后把答案的角色标识填进视图。
	 *
	 * 契约的 PlayerRoundView / PublicRoundView 都声明了
	 * `revealedAnswerCharacterId`，DTO 也有对应字段，但从来没有人给它赋过值：
	 * 字段定义、契约、前端归一化三处齐全，只有中间这一步缺失，于是前端永远
	 * 读到 null——分享文本因此写不出答案（端到端实测：剪贴板只有「猜了 0 次」）。
	 *
	 * 答案本身在公开投影的 publicState.answer 里（揭晓后 modes.Project 才会放它），
	 * 这里只取标识，不额外泄露任何东西。
	 */
	if s.modes != nil {
		if mode, ok := s.modes.Get(r.ModeID); ok {
			projected, perr := mode.Project(modes.ProjectInput{
				Scope:      r.Scope(),
				Viewer:     viewer,
				Public:     publicState,
				Private:    privateState,
				States:     states,
				Phase:      r.Status,
				RevealNow:  r.Status == modes.PhaseRevealed || r.Status == modes.PhaseSettled,
				ServerTime: s.nowMS(),
			})
			if perr != nil {
				return RoundView{}, problem.Internal(fmt.Errorf("room: project round: %w", perr))
			}
			view.PublicState = projected
			if r.ModeID == "classic" {
				view.CanSubmit = view.CanSubmit && !r.Paused() && (r.Status == modes.PhaseCollecting || r.Status == modes.PhaseBriefing)
				var progress struct {
					Self *struct {
						Solved      bool `json:"solved"`
						Failed      bool `json:"failed"`
						Surrendered bool `json:"surrendered"`
					} `json:"self"`
				}
				if err := json.Unmarshal(projected, &progress); err != nil {
					return RoundView{}, problem.Internal(err)
				}
				if progress.Self != nil {
					view.CanSubmit = view.CanSubmit && !progress.Self.Solved && !progress.Self.Failed && !progress.Self.Surrendered
				}
			}
			if r.ModeID == "rapport" && s.individualRoundSeats(r) {
				var authority struct {
					CanAnswer bool `json:"canAnswer"`
				}
				if json.Unmarshal(projected, &authority) != nil {
					return RoundView{}, problem.Internal(fmt.Errorf("room: invalid rapport projection"))
				}
				view.CanSubmit = view.CanSubmit && authority.CanAnswer
			}
			if r.ModeID == "clue-puzzle" {
				var authority struct {
					CanSubmit bool `json:"selfIsController"`
				}
				if json.Unmarshal(projected, &authority) != nil {
					return RoundView{}, problem.Internal(fmt.Errorf("room: invalid clue projection"))
				}
				view.CanSubmit = view.CanSubmit && authority.CanSubmit
			}

			// Read the answer after Project adds it during reveal; stored public state has no answer
			if r.Status == modes.PhaseRevealed || r.Status == modes.PhaseSettled {
				view.RevealedAnswerCharacterID = answerCharacterIDQ(view.PublicState)
			}
		} else {
			// The mode implementation is absent. Returning the raw stored public
			// state would risk shipping secret fields, so nothing is projected.
			view.PublicState = nil
		}
	}
	if competing {
		// The private state comes from the mode's projection, never from the raw
		// stored blob. When no mode can project, it stays empty.
		view.PrivateState = privateProjection(view.PublicState, privateState, viewer, states, r)
	}

	// Results and the revealed answer are gated on the phase.
	if r.Status == modes.PhaseRevealed || r.Status == modes.PhaseSettled {
		results, rerr := s.RoundResults(ctx, r.ID)
		if rerr != nil {
			return RoundView{}, rerr
		}
		view.Results = results
		if competing {
			gen, gerr := s.controlGenerationQ(ctx, s.db.Read(), r.RoomID, r.ID, actor.MembershipID())
			if gerr != nil {
				return RoundView{}, gerr
			}
			view.ControlGeneration = gen
			if actor.Membership != nil {
				rev, rerr := s.CurrentSubmissionRevision(ctx, r.ID, viewer.Subject.ID)
				if rerr != nil {
					return RoundView{}, rerr
				}
				view.SubmissionRevision = rev
				bySlot, serr := s.SubmissionRevisionsBySlot(ctx, r.ID, viewer.Subject.ID)
				if serr != nil {
					return RoundView{}, serr
				}
				view.SubmissionRevisionsBySlot = bySlot
			}
		}
	}

	if competing {
		gen, gerr := s.controlGenerationQ(ctx, s.db.Read(), r.RoomID, r.ID, actor.MembershipID())
		if gerr != nil {
			return RoundView{}, gerr
		}
		view.ControlGeneration = gen
		if actor.Membership != nil {
			rev, rerr := s.CurrentSubmissionRevision(ctx, r.ID, viewer.Subject.ID)
			if rerr != nil {
				return RoundView{}, rerr
			}
			view.SubmissionRevision = rev
			bySlot, serr := s.SubmissionRevisionsBySlot(ctx, r.ID, viewer.Subject.ID)
			if serr != nil {
				return RoundView{}, serr
			}
			view.SubmissionRevisionsBySlot = bySlot
		}
	}

	if r.ModeID == "creative" {
		var authority struct {
			Subphase string `json:"subphase"`
			Vote     *struct {
				CanVote bool `json:"canVote"`
			} `json:"vote"`
		}
		if err := json.Unmarshal(view.PublicState, &authority); err != nil {
			return RoundView{}, problem.Internal(err)
		}
		if authority.Subphase == "voting" || authority.Subphase == "closed" {
			view.CanSubmit = authority.Vote != nil && authority.Vote.CanVote && !r.Paused()
			view.ControlGeneration = 0
		} else {
			view.CanSubmit = view.CanSubmit && authority.Subphase == "writing"
		}
		if authority.Vote != nil {
			voteHeads, err := s.SubmissionRevisionsBySlot(ctx, r.ID, creative.VoteSubject(actor.SessionID).ID)
			voteRevision := voteHeads["vote"]
			if err != nil {
				return RoundView{}, err
			}
			if view.SubmissionRevisionsBySlot == nil {
				view.SubmissionRevisionsBySlot = map[string]int64{}
			}
			view.SubmissionRevisionsBySlot["vote"] = voteRevision
			if authority.Subphase != "writing" {
				view.SubmissionRevision = voteRevision
			}
		}
	}
	// Phase and surrender gates apply after mode-specific authority, including
	// creative voting, so a projection cannot reopen a closed participant.
	view.CanSubmit = view.CanSubmit && !r.Paused() && !isSurrendered(states[viewer.Subject.ID]) &&
		(r.Status == modes.PhaseCollecting || r.Status == modes.PhaseBriefing)
	if r.DeadlineAt != nil && s.nowMS() > r.DeadlineAt.UnixMilli() {
		view.CanSubmit = false
	}
	if r.Status == modes.PhaseBriefing && (s.previewWindowMS(r) > 0 || r.ModeID == "rapport" || r.ModeID == "bluff" || r.ModeID == "strategy-race" || r.ModeID == "creative" || r.ModeID == "trade") {
		view.CanSubmit = false
	}
	// Attach player-facing names for every subject in this round.
	//
	// Modes see only Subject{ID, Kind} and must not resolve room membership or team
	// labels themselves — that knowledge belongs to this layer. Without the map a
	// console can render only opaque ids, or has to rebuild the mapping from the
	// member list and duplicate the rule for which id a team round uses.
	//
	// A lookup failure is not fatal: the round stays readable, names are simply
	// absent, and the client falls back to a placeholder rather than an id.
	if names, nerr := s.subjectDisplayNames(ctx, r.RoomID); nerr == nil {
		view.SubjectNames = names
	}
	return view, nil
}

// privateProjection returns the caller's own participant state.
//
// It is only called for a competing viewer. The value is the caller's own row,
// which by construction contains nothing belonging to another participant.
func privateProjection(projected json.RawMessage, roundPrivate json.RawMessage, viewer modes.Viewer, states map[string]json.RawMessage, r Round) json.RawMessage {
	if r.ModeID == "creative" {
		var own struct {
			Self json.RawMessage `json:"self"`
			Vote json.RawMessage `json:"vote"`
		}
		if json.Unmarshal(projected, &own) != nil {
			return nil
		}
		return marshal(map[string]any{"modeId": "creative", "self": own.Self, "vote": own.Vote})
	}
	if r.ModeID == "rapport" {
		var own struct {
			Questions []struct {
				Locked bool   `json:"locked"`
				Option string `json:"selfOptionId"`
			} `json:"questions"`
		}
		if json.Unmarshal(projected, &own) != nil {
			return nil
		}
		var selected any
		for _, q := range own.Questions {
			if !q.Locked && q.Option != "" {
				selected = q.Option
			}
		}
		return marshal(map[string]any{"modeId": "rapport", "selectedOptionId": selected})
	}
	if r.ModeID == "clue-puzzle" {
		// Consume only the already authorised mode projection. Never filter raw
		// team secrets by a team identifier: clues belong to individual members.
		var own struct {
			Clues []struct {
				Text string `json:"text"`
			} `json:"privateClues"`
			Self *struct {
				Assignment map[string]string `json:"assignment"`
			} `json:"self"`
		}
		if json.Unmarshal(projected, &own) != nil {
			return nil
		}
		clues := make([]string, 0, len(own.Clues))
		for _, clue := range own.Clues {
			clues = append(clues, clue.Text)
		}
		assignments := make([]map[string]string, 0)
		if own.Self != nil {
			keys := make([]string, 0, len(own.Self.Assignment))
			for slot := range own.Self.Assignment {
				keys = append(keys, slot)
			}
			sort.Strings(keys)
			for _, slot := range keys {
				assignments = append(assignments, map[string]string{"slotId": slot, "characterId": own.Self.Assignment[slot]})
			}
		}
		return marshal(map[string]any{"modeId": "clue-puzzle", "clues": clues, "draftAssignments": assignments})
	}
	// Two shapes of private state exist and both must reach the viewer:
	//
	// - per-subject rows (trade objectives, bluff decoys, audio progress) — already
	// isolated by construction, so the caller's own row is returned as-is;
	// - a round-level blob (clue-puzzle's private clues) — keyed by subject inside,
	// so it must be filtered down to the caller's entry before it is handed out.
	//
	// Returning nil here — which is what this function used to do despite taking the
	// states map — meant neither shape arrived: every per-player feature was silently
	// empty on the wire while its unit tests passed.
	own, hasOwn := states[viewer.Subject.ID]
	if r.ModeID == "memory" {
		// The public projection already contains this team's visible board. Keep
		// the private response minimal and aligned with PrivateMemory rather than
		// forwarding storage-only fields such as pairing ids or old reveal windows.
		var progress struct {
			Attempts int `json:"attempts"`
		}
		if len(own) > 0 {
			if err := json.Unmarshal(own, &progress); err != nil {
				return nil
			}
		}
		return marshal(map[string]any{"modeId": "memory", "attemptsUsed": progress.Attempts})
	}

	// Filter the round-level blob to this viewer. Anything else in it belongs to
	// another participant and must never leave the server.
	filtered, hasRound := filterRoundPrivate(roundPrivate, viewer.Subject.ID)

	switch {
	case hasOwn && hasRound:
		return mergePrivate(own, filtered)
	case hasOwn:
		return own
	case hasRound:
		return filtered
	default:
		return nil
	}
}

// filterRoundPrivate keeps only the entries of a round-level private blob that belong to
// the given subject.
//
// The blob is a JSON object whose keys are either subject ids or per-subject maps keyed by
// subject id. Only keys that equal the viewer's id, or maps that contain the viewer's id,
// survive. Everything else is dropped, so a projection can never leak another player's
// private data even if a mode stores it under a shared key.
func filterRoundPrivate(blob json.RawMessage, subjectID string) (json.RawMessage, bool) {
	if subjectID == "" || len(blob) == 0 {
		return nil, false
	}
	var top map[string]json.RawMessage
	if err := json.Unmarshal(blob, &top); err != nil {
		return nil, false
	}
	out := make(map[string]json.RawMessage, len(top))
	for key, value := range top {
		if key == subjectID {
			out[key] = value
			continue
		}
		// A nested map keyed by subject: keep only the caller's entry.
		var inner map[string]json.RawMessage
		if err := json.Unmarshal(value, &inner); err != nil {
			continue
		}
		if mine, ok := inner[subjectID]; ok {
			out[key] = mine
		}
	}
	if len(out) == 0 {
		return nil, false
	}
	encoded, err := json.Marshal(out)
	if err != nil {
		return nil, false
	}
	return encoded, true
}

// mergePrivate combines two private JSON objects into one.
//
// When either side is not an object the per-subject row wins: it is the narrower, already
// isolated value, so preferring it can never widen what the viewer sees.
func mergePrivate(own, filtered json.RawMessage) json.RawMessage {
	var a, b map[string]json.RawMessage
	if err := json.Unmarshal(own, &a); err != nil {
		return own
	}
	if err := json.Unmarshal(filtered, &b); err != nil {
		return own
	}
	for k, v := range b {
		if _, exists := a[k]; !exists {
			a[k] = v
		}
	}
	encoded, err := json.Marshal(a)
	if err != nil {
		return own
	}
	return encoded
}

// PathNeighbors returns the content-graph neighbours for an open-book path round.
//
// A closed-book round refuses with 403: the neighbour list would leak the answer
// graph .
func (s *Service) PathNeighbors(ctx context.Context, sessionID, roundID, characterID string) (Round, []Neighbor, error) {
	r, room, actor, err := s.LoadRoundWithActor(ctx, sessionID, roundID)
	if err != nil {
		return Round{}, nil, err
	}
	_ = room
	if r.ModeID != "co-race-path" {
		return Round{}, nil, problem.Forbidden("not-a-path-round",
			"path neighbours are only available in a co-race-path round")
	}
	// The prepared question is authoritative for existing as well as new rounds.
	// Missing searchAllowed must never make a closed-book question searchable.
	prepared, err := roundPublicStateQ(ctx, s.db.Read(), r.ID)
	if err != nil {
		return Round{}, nil, err
	}
	var question struct {
		Config struct {
			OpenBook bool `json:"openBook"`
		} `json:"config"`
	}
	if err := json.Unmarshal(prepared, &question); err != nil {
		return Round{}, nil, problem.Internal(err)
	}
	if !question.Config.OpenBook {
		return Round{}, nil, problem.Forbidden("closed-book", "this round does not allow searching the content graph")
	}
	// The search permission comes from the round's stored configuration, not from
	// the caller's claim.
	var cfg struct {
		SearchAllowed *bool `json:"searchAllowed"`
	}
	if len(r.Config) > 0 {
		_ = json.Unmarshal(modeConfigOf(r.Config), &cfg)
	}
	if cfg.SearchAllowed != nil && !*cfg.SearchAllowed {
		return Round{}, nil, problem.Forbidden("closed-book",
			"this round does not allow searching the content graph")
	}
	if !actor.Has(CapPlay) && !actor.Has(CapObserve) {
		return Round{}, nil, problem.Forbidden("insufficient-capability",
			"you are not a participant of this room")
	}
	if s.content == nil {
		return Round{}, nil, problem.NotImplemented("content-unavailable",
			"no content pack is loaded, so path neighbours cannot be resolved")
	}
	neighbors, ok := s.content.PathNeighbors(characterID)
	if !ok {
		return Round{}, nil, problem.NotFound("character-not-found",
			"the character is not present in the loaded content pack")
	}
	return r, neighbors, nil
}

// ---- content endpoints ---------------------------------------------------

// Characters pages the public character index.
func (s *Service) Characters(ctx context.Context, cursor string, limit int, query string) ([]CharacterCard, string, string, error) {
	if s.content == nil {
		return nil, "", "", problem.NotImplemented("content-unavailable",
			"no content pack is loaded, so characters cannot be listed")
	}
	if limit <= 0 {
		limit = 20
	}
	if limit > 100 {
		limit = 100
	}
	if n := len([]rune(query)); n > 80 {
		return nil, "", "", problem.Validation("q is too long",
			problem.FieldError{Field: "q", Message: "must be at most 80 characters"})
	}
	items, next, err := s.content.Characters(cursor, limit, query)
	if err != nil {
		return nil, "", "", problem.Internal(fmt.Errorf("room: list characters: %w", err))
	}
	return items, next, s.content.DatasetVersion(), nil
}

// Character returns one public character projection.
func (s *Service) Character(ctx context.Context, characterID string) (CharacterCard, error) {
	if s.content == nil {
		return CharacterCard{}, problem.NotImplemented("content-unavailable",
			"no content pack is loaded, so characters cannot be read")
	}
	card, ok := s.content.Character(characterID)
	if !ok {
		return CharacterCard{}, problem.NotFound("character-not-found", "the character does not exist")
	}
	return card, nil
}

// ContentVersion exposes the loaded pack version for readiness.
func (s *Service) ContentVersion() string {
	if s.content == nil {
		return ""
	}
	return s.content.Version()
}

// AssetBaseURL exposes the configured public asset prefix.
func (s *Service) AssetBaseURL() string {
	if s.content == nil {
		return ""
	}
	return s.content.AssetBaseURL()
}

// ModeAvailability reports whether a mode can currently be opened.
//
// The answer comes from the registry plus the content gate, so a mode without a
// usable pack reports itself unavailable rather than offering an empty room
// .
func (s *Service) ModeAvailability(m modes.Mode) modes.Availability {
	if !s.hasPlayableForm(m.ID(), m.MinSubjects()) {
		return modes.Availability{Available: false, Reason: "this mode is closed on this instance"}
	}
	if s.modes == nil {
		return modes.Availability{Available: false, Reason: "no mode is registered on this server"}
	}
	if s.content == nil {
		return modes.Availability{Available: false, Reason: "no content pack is loaded"}
	}
	req := m.Requirement()
	if req.NeedsAssets && s.content == nil {
		return modes.Availability{Available: false, Reason: "no content pack with verified assets is loaded"}
	}

	// Availability is decided by the compiler's admission record, not by probing
	// section names. ContentRequirement.Fields names canonical fact fields
	// (star, aptitudes.distance) while Section() takes pack section names, so
	// comparing the two would mark every mode unavailable.
	adm, ok := s.content.ModeAdmission(m.ID())
	if !ok {
		return modes.Availability{
			Available: false,
			Reason:    "the loaded content pack carries no admission record for this mode",
		}
	}
	if !adm.Available {
		reason := adm.Reason
		if reason == "" {
			reason = "the loaded content pack has too few usable questions for this mode"
		}
		return modes.Availability{Available: false, Reason: reason, QuestionCount: adm.QuestionCount}
	}
	if adm.QuestionCount < req.MinEntities {
		return modes.Availability{
			Available:     false,
			Reason:        fmt.Sprintf("the pack compiled %d questions but this mode needs at least %d", adm.QuestionCount, req.MinEntities),
			QuestionCount: adm.QuestionCount,
		}
	}
	return modes.Availability{Available: true, QuestionCount: adm.QuestionCount}
}

// Modes returns the registry, or nil when none is configured.
func (s *Service) Modes() *modes.Registry { return s.modes }

// roundResultRow is used by tests to read a single result row.
func roundResultRow(ctx context.Context, q queryer, roundID, subjectID string) (int, bool, error) {
	var v int
	err := q.QueryRowContext(ctx,
		`SELECT score_units FROM round_results WHERE round_id = ? AND subject_id = ?`,
		roundID, subjectID).Scan(&v)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, err
	}
	return v, true, nil
}

// HashTokenForTest exposes the stored-secret hash so a test can seed a grant row
// with the same transform the production path uses.
func HashTokenForTest(v string) string { return hashTokenSHA256(v) }

// answerCharacterIDQ 从公开投影里取出揭晓答案的角色标识。
//
// 不同模式放答案的位置不同，这里只认最常见的 `answer.characterId`
// （classic 的 answerReveal 就是它）；取不到就返回空串，视图里该字段
// 因 omitempty 而不出现——不会伪造一个标识出来。
func answerCharacterIDQ(publicState json.RawMessage) string {
	if len(publicState) == 0 {
		return ""
	}
	var probe struct {
		Answer *struct {
			CharacterID string `json:"characterId"`
		} `json:"answer"`
	}
	if err := json.Unmarshal(publicState, &probe); err != nil {
		return ""
	}
	if probe.Answer == nil {
		return ""
	}
	return probe.Answer.CharacterID
}
