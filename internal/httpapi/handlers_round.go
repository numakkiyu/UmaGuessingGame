package httpapi

import (
	"encoding/json"
	"net/http"
	"sort"
	"strings"

	"github.com/bhcn/umaguessinggame/internal/httpapi/problem"
	"github.com/bhcn/umaguessinggame/internal/modes"
	"github.com/bhcn/umaguessinggame/internal/room"
	"github.com/bhcn/umaguessinggame/internal/session"
)

// ---- round reads ---------------------------------------------------------

// handleGetRound returns the authorised, viewer-specific round view.
//
// The projection is built by the mode from server-side records. Nothing is
// serialised first and filtered afterwards .
func (s *Server) handleGetRound(w http.ResponseWriter, r *http.Request) {
	sess, ok := sessionOf(r)
	if !ok {
		s.writeError(w, r, session.ErrNoSession)
		return
	}
	roundID, err := pathParam(r, "roundId")
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	round, rm, actor, err := s.rooms.LoadRoundWithActor(r.Context(), sess.ID, roundID)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	view, err := s.rooms.BuildRoundView(r.Context(), round.ID, rm, actor)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	summary := toRoundSummaryDTO(view.Round)

	// A competing viewer receives the player projection; everyone else receives the
	// public one. The split is decided from the server-side role.
	competing := actor.Has(room.CapPlay)
	if competing {
		dto := playerRoundViewDTO{
			ViewerRole:                view.ViewerRole,
			Round:                     summary,
			PublicState:               jsonOr(view.PublicState, "{}"),
			SubjectNames:              view.SubjectNames,
			ServerTime:                rfc3339(view.ServerTime),
			RoomVersion:               view.RoomVersion,
			PrivateState:              jsonOr(view.PrivateState, "{}"),
			ControlGeneration:         view.ControlGeneration,
			SubmissionRevision:        view.SubmissionRevision,
			SubmissionRevisionsBySlot: view.SubmissionRevisionsBySlot,
			CanSubmit:                 view.CanSubmit,
			TeamControllers:           toTeamControllerDTOs(view.TeamControllers),

			RevealedAnswerCharacterID: view.RevealedAnswerCharacterID,
		}
		if revealedPhase(view.Round.Status) {
			results := toResultDTOs(view.Results)
			dto.Results = &results
		}
		s.writeJSON(w, r, http.StatusOK, dto)
		return
	}
	dto := publicRoundViewDTO{
		CanSubmit: view.CanSubmit, ControlGeneration: view.ControlGeneration, SubmissionRevision: view.SubmissionRevision, SubmissionRevisionsBySlot: view.SubmissionRevisionsBySlot,
		ViewerRole:      view.ViewerRole,
		Round:           summary,
		PublicState:     jsonOr(view.PublicState, "{}"),
		SubjectNames:    view.SubjectNames,
		ServerTime:      rfc3339(view.ServerTime),
		RoomVersion:     view.RoomVersion,
		TeamControllers: toTeamControllerDTOs(view.TeamControllers),
		// 同上：观众与主持看到的公开视图也要带上揭晓答案。
		RevealedAnswerCharacterID: view.RevealedAnswerCharacterID,
	}
	if revealedPhase(view.Round.Status) {
		results := toResultDTOs(view.Results)
		dto.Results = &results
	}
	s.writeJSON(w, r, http.StatusOK, dto)
}

// revealedPhase reports whether answer material may be projected.
func revealedPhase(p modes.Phase) bool {
	return p == modes.PhaseRevealed || p == modes.PhaseSettled
}

// toResultDTOs maps stored results onto the wire schema.
func toResultDTOs(results []room.StoredResult) []resultDTO {
	// Stored scores already include surrender adjustments. Reconstruct the same
	// competition ranks used by settlement from those immutable scores; the DB
	// predates a rank column, so reading a result must not silently drop its rank.
	values := make([]int, len(results))
	for i, res := range results {
		values[i] = res.ScoreUnits
	}
	sort.Sort(sort.Reverse(sort.IntSlice(values)))
	ranks := make(map[int]int, len(values))
	for i, value := range values {
		if _, exists := ranks[value]; !exists {
			ranks[value] = i + 1
		}
	}
	out := make([]resultDTO, 0, len(results))
	for _, res := range results {
		kind := string(res.SubjectKind)
		if kind == "" {
			kind = "player"
		}
		out = append(out, resultDTO{
			SubjectID:   res.SubjectID,
			SubjectType: kind,
			ScoreUnits:  res.ScoreUnits,
			MaxUnits:    res.MaxScoreUnits,
			Rank:        ranks[res.ScoreUnits],
			Outcome:     outcomeFromExplanation(res.Explanation),
			Details:     []scoreDetailDTO{},
		})
	}
	return out
}

// outcomeFromExplanation distinguishes solved and failed results when the mode supplies them.
func outcomeFromExplanation(raw json.RawMessage) string {
	if len(raw) == 0 {
		return "completed"
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return "completed"
	}
	if v, ok := fields["solved"]; ok && strings.Trim(string(v), `"`) == "true" {
		return "solved"
	}
	if v, ok := fields["failed"]; ok && strings.Trim(string(v), `"`) == "true" {
		return "failed"
	}
	return "completed"
}

// handleGetPathNeighbors returns the content-graph neighbours for a path round.
func (s *Server) handleGetPathNeighbors(w http.ResponseWriter, r *http.Request) {
	sess, ok := sessionOf(r)
	if !ok {
		s.writeError(w, r, session.ErrNoSession)
		return
	}
	roundID, err := pathParam(r, "roundId")
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	characterID := r.URL.Query().Get("characterId")
	if characterID == "" {
		s.writeError(w, r, problem.Validation("characterId is required",
			problem.FieldError{Field: "characterId", Message: "is required"}))
		return
	}
	if len(characterID) > 128 {
		s.writeError(w, r, problem.Validation("characterId is too long",
			problem.FieldError{Field: "characterId", Message: "must be at most 128 characters"}))
		return
	}
	round, neighbors, err := s.rooms.PathNeighbors(r.Context(), sess.ID, roundID, characterID)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	out := pathNeighborsDTO{
		RoundID:     round.ID,
		CharacterID: characterID,
		Neighbors:   make([]pathNeighborDTO, 0, len(neighbors)),
	}
	for _, n := range neighbors {
		ev := make([]evidenceDTO, 0, len(n.Evidence))
		for _, e := range n.Evidence {
			ev = append(ev, evidenceDTO{
				EvidenceID: e.EvidenceID,
				SourceURL:  e.SourceURL,
				Label:      e.Label,
				RaceID:     e.RaceID,
			})
		}
		out.Neighbors = append(out.Neighbors, pathNeighborDTO{
			Character: publicCardDTO{
				CardID:      n.CharacterID,
				CharacterID: n.CharacterID,
				DisplayName: n.DisplayName,
				AssetID:     n.AssetID,
			},
			Evidence: ev,
		})
	}
	s.writeJSON(w, r, http.StatusOK, out)
}

// ---- round writes --------------------------------------------------------

// handleCreateTransition performs an authorised phase transition.
func (s *Server) handleCreateTransition(w http.ResponseWriter, r *http.Request) {
	sess, ok := sessionOf(r)
	if !ok {
		s.writeError(w, r, session.ErrNoSession)
		return
	}
	roundID, err := pathParam(r, "roundId")
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	key, err := requireIdempotencyKey(r)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	raw, err := readRawBody(r)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	if replayed, err := s.replayIfPresent(w, r, sess.ID, "createTransition", roundID, key, raw); err != nil || replayed {
		if err != nil {
			s.writeError(w, r, err)
		}
		return
	}
	var body transitionCreateDTO
	if err := decodeJSON(&body, raw); err != nil {
		s.writeError(w, r, err)
		return
	}
	round, _, actor, err := s.rooms.LoadRoundWithActor(r.Context(), sess.ID, roundID)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	res, err := s.rooms.CreateTransition(r.Context(), actor, round.ID, room.TransitionInput{
		Target:               body.Target,
		ExpectedPhaseVersion: body.ExpectedPhaseVersion,
		Reason:               body.Reason,
		ExtensionSeconds:     body.ExtensionSeconds,
		ExcludedOptionIDs:    body.ExcludedOptionIDs,
	})
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	dto := transitionDTO{
		TransitionID: res.TransitionID,
		RoundID:      res.RoundID,
		PhaseVersion: res.PhaseVersion,
		AcceptedAt:   rfc3339(res.AcceptedAt),
		RoomVersion:  res.RoomVersion,
	}
	w.Header().Set("Location", "/api/v1/rounds/"+round.ID)
	if err := s.storeReceipt(r, sess.ID, "createTransition", roundID, key, raw, http.StatusCreated, dto); err != nil {
		s.writeError(w, r, err)
		return
	}
	s.writeJSON(w, r, http.StatusCreated, dto)
}

// handleCreateSubmission persists one idempotent mode action.
//
// The mode action is validated twice: the server checks that its kind maps to a
// known slot, and the mode validates its own fields. A permissive JSON blob is
// never forwarded as an action .
func (s *Server) handleCreateSubmission(w http.ResponseWriter, r *http.Request) {
	sess, ok := sessionOf(r)
	if !ok {
		s.writeError(w, r, session.ErrNoSession)
		return
	}
	roundID, err := pathParam(r, "roundId")
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	key, err := requireIdempotencyKey(r)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	raw, err := readRawBody(r)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	if replayed, err := s.replayIfPresent(w, r, sess.ID, "createSubmission", roundID, key, raw); err != nil || replayed {
		if err != nil {
			s.writeError(w, r, err)
		}
		return
	}
	var body submissionCreateDTO
	if err := decodeJSON(&body, raw); err != nil {
		s.writeError(w, r, err)
		return
	}
	if !knownActionKinds[body.ModeAction.Kind] {
		s.writeError(w, r, problem.Validation("modeAction.kind is unknown",
			problem.FieldError{Field: "modeAction.kind",
				Message: "must be one of the documented action kinds"}))
		return
	}
	round, _, actor, err := s.rooms.LoadRoundWithActor(r.Context(), sess.ID, roundID)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	// The contract kind is forwarded verbatim.
	//
	// The modes accept the namespaced discriminator values (classic.guess,
	// grid.claim, ...) directly, so there is exactly one vocabulary on the wire
	// and in the rule engine. An earlier revision translated wire kinds to short
	// domain kinds here; that indirection is gone because it made the two layers
	// disagree the moment either side changed.
	receipt, err := s.rooms.CreateSubmission(r.Context(), actor, round.ID, room.SubmissionInput{
		Command:            &room.CreationCommand{Key: key, Payload: raw},
		PhaseVersion:       body.PhaseVersion,
		SubmissionRevision: body.SubmissionRevision,
		ControlGeneration:  body.ControlGeneration,
		ModeAction: modes.ModeAction{
			Kind:    body.ModeAction.Kind,
			Payload: body.ModeAction.Raw,
		},
	})
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	dto := submissionReceiptDTO{
		SubmissionID:       receipt.SubmissionID,
		ReceiptID:          receipt.ReceiptID,
		AcceptedAt:         rfc3339(receipt.AcceptedAt),
		RoomVersion:        receipt.RoomVersion,
		PhaseVersion:       receipt.PhaseVersion,
		SubmissionRevision: receipt.SubmissionRevision,
		Outcome:            "accepted",
		Feedback:           jsonOr(receipt.Feedback, "{}"),
	}
	w.Header().Set("Location", "/api/v1/rounds/"+round.ID+"/submissions/"+receipt.SubmissionID)
	if err := s.storeReceipt(r, sess.ID, "createSubmission", roundID, key, raw, http.StatusCreated, dto); err != nil {
		s.writeError(w, r, err)
		return
	}
	s.writeJSON(w, r, http.StatusCreated, dto)
}

// knownActionKinds is the contract's ModeAction discriminator set.
var knownActionKinds = map[string]bool{
	"classic.guess": true, "grid.claim": true, "draft.lineup": true,
	"race.order": true, "path.route": true, "sort.order": true,
	"memory.flip": true, "bluff.decoy": true, "bluff.vote": true,
	"clue.assignment": true, "strategy.action": true, "rapport.answer": true,
	"creative.work": true, "creative.vote": true, "pedigree.assignment": true,
	"audio.matches": true, "audio.ready": true,
}

// handleCreateSurrender records a participant leaving the round.
func (s *Server) handleCreateSurrender(w http.ResponseWriter, r *http.Request) {
	sess, ok := sessionOf(r)
	if !ok {
		s.writeError(w, r, session.ErrNoSession)
		return
	}
	roundID, err := pathParam(r, "roundId")
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	key, err := requireIdempotencyKey(r)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	raw, err := readRawBody(r)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	if replayed, err := s.replayIfPresent(w, r, sess.ID, "createSurrender", roundID, key, raw); err != nil || replayed {
		if err != nil {
			s.writeError(w, r, err)
		}
		return
	}
	var body surrenderCreateDTO
	if err := decodeJSON(&body, raw); err != nil {
		s.writeError(w, r, err)
		return
	}
	round, _, actor, err := s.rooms.LoadRoundWithActor(r.Context(), sess.ID, roundID)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	id, acceptedAt, err := s.rooms.CreateSurrender(r.Context(), actor, round.ID, body.PhaseVersion)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	dto := surrenderDTO{
		SurrenderID: id,
		RoundID:     round.ID,
		AcceptedAt:  rfc3339(acceptedAt),
	}
	w.Header().Set("Location", "/api/v1/rounds/"+round.ID+"/surrenders/"+id)
	if err := s.storeReceipt(r, sess.ID, "createSurrender", roundID, key, raw, http.StatusCreated, dto); err != nil {
		s.writeError(w, r, err)
		return
	}
	s.writeJSON(w, r, http.StatusCreated, dto)
}

// handleCreatePauseRequest opens a consent-based pause.
func (s *Server) handleCreatePauseRequest(w http.ResponseWriter, r *http.Request) {
	sess, ok := sessionOf(r)
	if !ok {
		s.writeError(w, r, session.ErrNoSession)
		return
	}
	roundID, err := pathParam(r, "roundId")
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	key, err := requireIdempotencyKey(r)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	raw, err := readRawBody(r)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	if replayed, err := s.replayIfPresent(w, r, sess.ID, "createPauseRequest", roundID, key, raw); err != nil || replayed {
		if err != nil {
			s.writeError(w, r, err)
		}
		return
	}
	var body pauseCreateDTO
	if err := decodeJSON(&body, raw); err != nil {
		s.writeError(w, r, err)
		return
	}
	round, _, actor, err := s.rooms.LoadRoundWithActor(r.Context(), sess.ID, roundID)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	req, err := s.rooms.CreatePauseRequest(r.Context(), actor, round.ID, body.PhaseVersion)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	dto := pauseRequestDTO{
		RequestID: req.ID,
		RoundID:   req.RoundID,
		Status:    req.Status,
		ExpiresAt: rfc3339(req.ExpiresAt),
	}
	w.Header().Set("Location", "/api/v1/rounds/"+round.ID+"/pause-requests/"+req.ID)
	if err := s.storeReceipt(r, sess.ID, "createPauseRequest", roundID, key, raw, http.StatusCreated, dto); err != nil {
		s.writeError(w, r, err)
		return
	}
	s.writeJSON(w, r, http.StatusCreated, dto)
}

// handleRespondToPause accepts or rejects a pause request.
func (s *Server) handleRespondToPause(w http.ResponseWriter, r *http.Request) {
	sess, ok := sessionOf(r)
	if !ok {
		s.writeError(w, r, session.ErrNoSession)
		return
	}
	roundID, err := pathParam(r, "roundId")
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	requestID, err := pathParam(r, "requestId")
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	key, err := requireIdempotencyKey(r)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	raw, err := readRawBody(r)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	if replayed, err := s.replayIfPresent(w, r, sess.ID, "respondToPause", requestID, key, raw); err != nil || replayed {
		if err != nil {
			s.writeError(w, r, err)
		}
		return
	}
	var body pauseResponseDTO
	if err := decodeJSON(&body, raw); err != nil {
		s.writeError(w, r, err)
		return
	}
	round, _, actor, err := s.rooms.LoadRoundWithActor(r.Context(), sess.ID, roundID)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	req, err := s.rooms.RespondToPause(r.Context(), actor, round.ID, requestID, body.Accept)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	dto := pauseRequestDTO{
		RequestID: req.ID,
		RoundID:   req.RoundID,
		Status:    req.Status,
		ExpiresAt: rfc3339(req.ExpiresAt),
	}
	w.Header().Set("Location", "/api/v1/rounds/"+round.ID+"/pause-requests/"+req.ID)
	if err := s.storeReceipt(r, sess.ID, "respondToPause", requestID, key, raw, http.StatusCreated, dto); err != nil {
		s.writeError(w, r, err)
		return
	}
	s.writeJSON(w, r, http.StatusCreated, dto)
}

// handleRequestRematch records a rematch confirmation.
func (s *Server) handleRequestRematch(w http.ResponseWriter, r *http.Request) {
	sess, ok := sessionOf(r)
	if !ok {
		s.writeError(w, r, session.ErrNoSession)
		return
	}
	roundID, err := pathParam(r, "roundId")
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	key, err := requireIdempotencyKey(r)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	raw, err := readRawBody(r)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	if replayed, err := s.replayIfPresent(w, r, sess.ID, "requestRematch", roundID, key, raw); err != nil || replayed {
		if err != nil {
			s.writeError(w, r, err)
		}
		return
	}
	var body rematchCreateDTO
	if err := decodeJSON(&body, raw); err != nil {
		s.writeError(w, r, err)
		return
	}
	round, _, actor, err := s.rooms.LoadRoundWithActor(r.Context(), sess.ID, roundID)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	req, err := s.rooms.RequestRematch(r.Context(), actor, round.ID, body.Accept)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	dto := rematchRequestDTO{RequestID: req.ID, Status: req.Status, NewRoundID: req.NewRoundID}
	w.Header().Set("Location", "/api/v1/rounds/"+round.ID+"/rematch-requests/"+req.ID)
	if err := s.storeReceipt(r, sess.ID, "requestRematch", roundID, key, raw, http.StatusCreated, dto); err != nil {
		s.writeError(w, r, err)
		return
	}
	s.writeJSON(w, r, http.StatusCreated, dto)
}

// handleCreateTrade proposes a trade.
func (s *Server) handleCreateTrade(w http.ResponseWriter, r *http.Request) {
	sess, ok := sessionOf(r)
	if !ok {
		s.writeError(w, r, session.ErrNoSession)
		return
	}
	roundID, err := pathParam(r, "roundId")
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	key, err := requireIdempotencyKey(r)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	raw, err := readRawBody(r)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	if replayed, err := s.replayIfPresent(w, r, sess.ID, "createTrade", roundID, key, raw); err != nil || replayed {
		if err != nil {
			s.writeError(w, r, err)
		}
		return
	}
	var body tradeCreateDTO
	if err := decodeJSON(&body, raw); err != nil {
		s.writeError(w, r, err)
		return
	}
	round, _, actor, err := s.rooms.LoadRoundWithActor(r.Context(), sess.ID, roundID)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	trade, err := s.rooms.CreateTrade(r.Context(), actor, round.ID, room.TradeInput{
		Command:               &room.CreationCommand{Key: key, Payload: raw},
		PhaseVersion:          body.PhaseVersion,
		RecipientMembershipID: body.RecipientMembershipID,
		OfferedInstanceID:     body.OfferedInstanceID,
		RequestedInstanceID:   body.RequestedInstanceID,
	})
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	dto := tradeDTO{
		TradeID:               trade.ID,
		SenderMembershipID:    trade.SenderMembershipID,
		RecipientMembershipID: trade.RecipientMembershipID,
		OfferedInstanceID:     trade.OfferedInstanceID,
		RequestedInstanceID:   trade.RequestedInstanceID,
		Status:                trade.Status,
		ExpiresAt:             rfc3339(trade.ExpiresAt),
	}
	w.Header().Set("Location", "/api/v1/rounds/"+round.ID+"/trades/"+trade.ID)
	if err := s.storeReceipt(r, sess.ID, "createTrade", roundID, key, raw, http.StatusCreated, dto); err != nil {
		s.writeError(w, r, err)
		return
	}
	s.writeJSON(w, r, http.StatusCreated, dto)
}

// handleRespondToTrade accepts or rejects a trade.
func (s *Server) handleRespondToTrade(w http.ResponseWriter, r *http.Request) {
	sess, ok := sessionOf(r)
	if !ok {
		s.writeError(w, r, session.ErrNoSession)
		return
	}
	roundID, err := pathParam(r, "roundId")
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	tradeID, err := pathParam(r, "tradeId")
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	key, err := requireIdempotencyKey(r)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	raw, err := readRawBody(r)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	if replayed, err := s.replayIfPresent(w, r, sess.ID, "respondToTrade", roundID+":"+tradeID, key, raw); err != nil || replayed {
		if err != nil {
			s.writeError(w, r, err)
		}
		return
	}
	var body tradeResponseDTO
	if err := decodeJSON(&body, raw); err != nil {
		s.writeError(w, r, err)
		return
	}
	round, _, actor, err := s.rooms.LoadRoundWithActor(r.Context(), sess.ID, roundID)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	trade, err := s.rooms.RespondToTrade(r.Context(), actor, round.ID, tradeID, body.Accept, &room.CreationCommand{Key: key, Payload: raw})
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	dto := tradeDTO{
		TradeID:               trade.ID,
		SenderMembershipID:    trade.SenderMembershipID,
		RecipientMembershipID: trade.RecipientMembershipID,
		OfferedInstanceID:     trade.OfferedInstanceID,
		RequestedInstanceID:   trade.RequestedInstanceID,
		Status:                trade.Status,
		ExpiresAt:             rfc3339(trade.ExpiresAt),
	}
	w.Header().Set("Location", "/api/v1/rounds/"+round.ID+"/trades/"+trade.ID)
	if err := s.storeReceipt(r, sess.ID, "respondToTrade", roundID+":"+tradeID, key, raw, http.StatusCreated, dto); err != nil {
		s.writeError(w, r, err)
		return
	}
	s.writeJSON(w, r, http.StatusCreated, dto)
}
