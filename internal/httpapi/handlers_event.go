package httpapi

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"

	"github.com/bhcn/umaguessinggame/internal/httpapi/problem"
	"github.com/bhcn/umaguessinggame/internal/room"
	"github.com/bhcn/umaguessinggame/internal/session"
)

// ---- matchmaking ---------------------------------------------------------

// handleCreateMatchmakingTicket queues the caller.
func (s *Server) handleCreateMatchmakingTicket(w http.ResponseWriter, r *http.Request) {
	sess, ok := sessionOf(r)
	if !ok {
		s.writeError(w, r, session.ErrNoSession)
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
	if replayed, err := s.replayIfPresent(w, r, sess.ID, "createMatchmakingTicket", "", key, raw); err != nil || replayed {
		if err != nil {
			s.writeError(w, r, err)
		}
		return
	}
	var body ticketCreateDTO
	if err := decodeJSON(&body, raw); err != nil {
		s.writeError(w, r, err)
		return
	}
	ticket, err := s.rooms.CreateMatchmakingTicket(r.Context(), sess.ID, room.CreateTicketInput{
		ModeConfig: body.ModeConfig.Raw,
		PoolID:     body.PoolID, Proof: body.Proof, Command: &room.CreationCommand{Key: key, Payload: raw},
	})
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	dto := toTicketDTO(ticket)
	w.Header().Set("Location", "/api/v1/matchmaking-tickets/"+ticket.ID)
	if err := s.storeReceipt(r, sess.ID, "createMatchmakingTicket", "", key, raw, http.StatusCreated, dto); err != nil {
		s.writeError(w, r, err)
		return
	}
	s.writeJSON(w, r, http.StatusCreated, dto)
}

// handleGetMatchmakingTicket reads an owned ticket.
func (s *Server) handleGetMatchmakingTicket(w http.ResponseWriter, r *http.Request) {
	sess, ok := sessionOf(r)
	if !ok {
		s.writeError(w, r, session.ErrNoSession)
		return
	}
	ticketID, err := pathParam(r, "ticketId")
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	ticket, err := s.rooms.GetMatchmakingTicket(r.Context(), sess.ID, ticketID)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	s.writeJSON(w, r, http.StatusOK, toTicketDTO(ticket))
}

// handleCancelMatchmakingTicket cancels an owned ticket.
func (s *Server) handleCancelMatchmakingTicket(w http.ResponseWriter, r *http.Request) {
	sess, ok := sessionOf(r)
	if !ok {
		s.writeError(w, r, session.ErrNoSession)
		return
	}
	ticketID, err := pathParam(r, "ticketId")
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
	if replayed, err := s.replayIfPresent(w, r, sess.ID, "cancelMatchmakingTicket", ticketID, key, raw); err != nil || replayed {
		if err != nil {
			s.writeError(w, r, err)
		}
		return
	}
	if err := s.rooms.CancelMatchmakingTicket(r.Context(), sess.ID, ticketID); err != nil {
		s.writeError(w, r, err)
		return
	}
	if err := s.storeReceipt(r, sess.ID, "cancelMatchmakingTicket", ticketID, key, raw, http.StatusNoContent, nil); err != nil {
		s.writeError(w, r, err)
		return
	}
	s.writeNoContent(w, r)
}

// handleRespondToMatch accepts or declines a proposed match.
func (s *Server) handleRespondToMatch(w http.ResponseWriter, r *http.Request) {
	sess, ok := sessionOf(r)
	if !ok {
		s.writeError(w, r, session.ErrNoSession)
		return
	}
	ticketID, err := pathParam(r, "ticketId")
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
	if replayed, err := s.replayIfPresent(w, r, sess.ID, "respondToMatch", ticketID, key, raw); err != nil || replayed {
		if err != nil {
			s.writeError(w, r, err)
		}
		return
	}
	var body ticketResponseDTO
	if err := decodeJSON(&body, raw); err != nil {
		s.writeError(w, r, err)
		return
	}
	if body.Accept && s.blockRiskAdmission(w, r) {
		return
	}
	ticket, err := s.rooms.RespondToMatch(r.Context(), sess.ID, ticketID, body.Accept)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	dto := toTicketDTO(ticket)
	w.Header().Set("Location", "/api/v1/matchmaking-tickets/"+ticket.ID)
	if err := s.storeReceipt(r, sess.ID, "respondToMatch", ticketID, key, raw, http.StatusCreated, dto); err != nil {
		s.writeError(w, r, err)
		return
	}
	s.writeJSON(w, r, http.StatusCreated, dto)
}

// toTicketDTO maps a ticket onto the wire schema.
//
// The internal status vocabulary is mapped onto the contract's enum: an
// unmatched queued ticket is "queueing", a matched-but-unconfirmed one is
// "matched-pending-accept".
func toTicketDTO(t room.Ticket) ticketDTO {
	var roomID *string
	if t.RoomID != "" {
		v := t.RoomID
		roomID = &v
	}
	return ticketDTO{
		TicketID: t.ID,
		PoolID:   t.PoolID, Players: t.Players, Accepted: t.Accepted,
		Status:     mapTicketStatus(t.Status),
		RoomID:     roomID,
		ExpiresAt:  rfc3339(t.ExpiresAt),
		ServerTime: rfc3339(nowMS()),
	}
}

// mapTicketStatus converts the stored status to the contract vocabulary.
// mapTicketStatus normalises a stored ticket status onto the contract vocabulary.
//
// Since migration 0002 the database already stores the contract words
// (queueing / matched-pending-accept / ready / playing / cancelled / expired /
// finished), so the normal path is a pass-through. Only the pre-0002 spellings are
// rewritten, for rows that survived the migration.
//
// The previous revision switched on the OLD words and fell back to "queueing" for
// anything unrecognised. Because the new words matched no case, a ticket that was
// really matched-pending-accept was reported as "queueing" — the pairing happened,
// the database said so, and only the HTTP response disagreed. That is why a
// service-level test passed while the API was wrong: the defect lived entirely in
// this mapping.
func mapTicketStatus(status string) string {
	switch status {
	case "queued":
		return "queueing"
	case "matched":
		return "matched-pending-accept"
	case "confirmed":
		return "ready"
	}
	return status
}

// ---- events --------------------------------------------------------------

// handleCreateEvent creates an event programme.
func (s *Server) handleCreateEvent(w http.ResponseWriter, r *http.Request) {
	sess, ok := sessionOf(r)
	if !ok {
		s.writeError(w, r, session.ErrNoSession)
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
	if replayed, err := s.replayIfPresent(w, r, sess.ID, "createEvent", "", key, raw); err != nil || replayed {
		if err != nil {
			s.writeError(w, r, err)
		}
		return
	}
	var body eventCreateDTO
	if err := decodeJSON(&body, raw); err != nil {
		s.writeError(w, r, err)
		return
	}
	event, err := s.rooms.CreateEvent(r.Context(), sess.ID, room.CreateEventInput{
		Command:   &room.CreationCommand{Key: key, Payload: raw},
		RoomID:    body.RoomID,
		Name:      body.Name,
		TiePolicy: body.TiePolicy,
	})
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	dto := eventDTO{
		TiePolicy:    event.TiePolicy,
		EventID:      event.ID,
		RoomID:       event.RoomID,
		Name:         event.Name,
		Status:       string(event.Status),
		EventVersion: event.EventVersion,
		Segments:     []string{},
	}
	w.Header().Set("Location", "/api/v1/events/"+event.ID)
	if err := s.storeReceipt(r, sess.ID, "createEvent", "", key, raw, http.StatusCreated, dto); err != nil {
		s.writeError(w, r, err)
		return
	}
	s.writeJSON(w, r, http.StatusCreated, dto)
}

// handleGetEvent reads event metadata.
func (s *Server) handleGetEvent(w http.ResponseWriter, r *http.Request) {
	sess, ok := sessionOf(r)
	if !ok {
		s.writeError(w, r, session.ErrNoSession)
		return
	}
	eventID, err := pathParam(r, "eventId")
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	event, segments, err := s.rooms.GetEvent(r.Context(), sess.ID, eventID)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	ids := make([]string, 0, len(segments))
	details := make([]segmentDTO, 0, len(segments))
	for _, seg := range segments {
		ids = append(ids, seg.ID)
		details = append(details, toSegmentDTO(seg))
	}
	s.writeJSON(w, r, http.StatusOK, eventDTO{
		TiePolicy:      event.TiePolicy,
		EventID:        event.ID,
		RoomID:         event.RoomID,
		Name:           event.Name,
		Status:         string(event.Status),
		EventVersion:   event.EventVersion,
		Segments:       ids,
		SegmentDetails: details,
	})
}

// handleCreateEventTransition changes the event lifecycle.
func (s *Server) handleCreateEventTransition(w http.ResponseWriter, r *http.Request) {
	sess, ok := sessionOf(r)
	if !ok {
		s.writeError(w, r, session.ErrNoSession)
		return
	}
	eventID, err := pathParam(r, "eventId")
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
	if replayed, err := s.replayIfPresent(w, r, sess.ID, "createEventTransition", eventID, key, raw); err != nil || replayed {
		if err != nil {
			s.writeError(w, r, err)
		}
		return
	}
	var body eventTransitionCreateDTO
	if err := decodeJSON(&body, raw); err != nil {
		s.writeError(w, r, err)
		return
	}
	event, err := s.rooms.GetEventForActor(r.Context(), sess.ID, eventID)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	updated, err := s.rooms.CreateEventTransitionAs(r.Context(), event, body.Target, body.ExpectedEventVersion, body.Reason)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	dto := eventDTO{
		TiePolicy:    updated.TiePolicy,
		EventID:      updated.ID,
		RoomID:       updated.RoomID,
		Name:         updated.Name,
		Status:       string(updated.Status),
		EventVersion: updated.EventVersion,
		Segments:     []string{},
	}
	w.Header().Set("Location", "/api/v1/events/"+updated.ID)
	if err := s.storeReceipt(r, sess.ID, "createEventTransition", eventID, key, raw, http.StatusCreated, dto); err != nil {
		s.writeError(w, r, err)
		return
	}
	s.writeJSON(w, r, http.StatusCreated, dto)
}

// handleCreateSegment appends an event segment.
func (s *Server) handleCreateSegment(w http.ResponseWriter, r *http.Request) {
	sess, ok := sessionOf(r)
	if !ok {
		s.writeError(w, r, session.ErrNoSession)
		return
	}
	eventID, err := pathParam(r, "eventId")
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
	if replayed, err := s.replayIfPresent(w, r, sess.ID, "createSegment", eventID, key, raw); err != nil || replayed {
		if err != nil {
			s.writeError(w, r, err)
		}
		return
	}
	var body segmentCreateDTO
	if err := decodeJSON(&body, raw); err != nil {
		s.writeError(w, r, err)
		return
	}
	event, err := s.rooms.GetEventForActor(r.Context(), sess.ID, eventID)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	seg, err := s.rooms.CreateSegmentAs(r.Context(), event, room.SegmentCreateInput{
		ModeConfig:  body.ModeConfig.Raw,
		Label:       body.Label,
		ScoreBudget: body.ScoreBudget,
		RoundCount:  body.RoundCount,
		Competitive: body.Competitive,
	})
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	dto := toSegmentDTO(seg)
	w.Header().Set("Location", "/api/v1/events/"+eventID+"/segments/"+seg.ID)
	if err := s.storeReceipt(r, sess.ID, "createSegment", eventID, key, raw, http.StatusCreated, dto); err != nil {
		s.writeError(w, r, err)
		return
	}
	s.writeJSON(w, r, http.StatusCreated, dto)
}

// handleUpdateSegment edits a planned segment.
func (s *Server) handleUpdateSegment(w http.ResponseWriter, r *http.Request) {
	sess, ok := sessionOf(r)
	if !ok {
		s.writeError(w, r, session.ErrNoSession)
		return
	}
	eventID, err := pathParam(r, "eventId")
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	segmentID, err := pathParam(r, "segmentId")
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
	if replayed, err := s.replayIfPresent(w, r, sess.ID, "updateSegment", segmentID, key, raw); err != nil || replayed {
		if err != nil {
			s.writeError(w, r, err)
		}
		return
	}
	var body segmentPatchDTO
	if err := decodeJSON(&body, raw); err != nil {
		s.writeError(w, r, err)
		return
	}
	event, err := s.rooms.GetEventForActor(r.Context(), sess.ID, eventID)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	seg, err := s.rooms.UpdateSegmentAs(r.Context(), event, segmentID, room.SegmentPatchInput{
		ExpectedEventVersion: body.ExpectedEventVersion,
		Label:                body.Label,
		ScoreBudget:          body.ScoreBudget,
		ModeConfig:           body.ModeConfig.Raw,
	})
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	dto := toSegmentDTO(seg)
	if err := s.storeReceipt(r, sess.ID, "updateSegment", segmentID, key, raw, http.StatusOK, dto); err != nil {
		s.writeError(w, r, err)
		return
	}
	s.writeJSON(w, r, http.StatusOK, dto)
}

// toSegmentDTO maps a segment onto the wire schema.
//
// modeConfig is extracted from the stored config blob, and roundCount is read back
// from the same blob, so the response reflects what was actually persisted.
func toSegmentDTO(seg room.Segment) segmentDTO {
	dto := segmentDTO{
		Results:           []eventItemResultDTO{},
		Competitive:       seg.Competitive(),
		SegmentID:         seg.ID,
		EventID:           seg.EventID,
		Ordinal:           seg.Ordinal,
		Status:            seg.Status,
		Name:              seg.Name,
		RoundID:           seg.RoundID,
		ModeConfig:        rawJSON("{}"),
		ScoreBudget:       seg.Budget,
		RoundCount:        1,
		StartedRounds:     seg.StartedRounds,
		CompletedRounds:   seg.CompletedRounds,
		CurrentRoundPhase: seg.CurrentRoundPhase,
	}
	for _, result := range seg.Results {
		dto.Results = append(dto.Results, eventItemResultDTO{PublicCode: result.PublicCode, SubjectID: result.SubjectID, DisplayName: result.DisplayName, ScoreUnits: result.ScoreUnits, MaxUnits: result.MaxUnits})
	}

	var cfg struct {
		ModeConfig json.RawMessage `json:"modeConfig"`
		RoundCount int             `json:"roundCount"`
	}
	if len(seg.Config) > 0 {
		if err := jsonUnmarshal(seg.Config, &cfg); err == nil {
			if len(cfg.ModeConfig) > 0 {
				dto.ModeConfig = rawJSON(cfg.ModeConfig)
			}
			if cfg.RoundCount > 0 {
				dto.RoundCount = cfg.RoundCount
			}
		}
	}
	return dto
}

// handleGetStandings reads the aggregate table.
func (s *Server) handleGetStandings(w http.ResponseWriter, r *http.Request) {
	sess, ok := sessionOf(r)
	if !ok {
		s.writeError(w, r, session.ErrNoSession)
		return
	}
	eventID, err := pathParam(r, "eventId")
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	standings, err := s.rooms.GetStandings(r.Context(), sess.ID, eventID)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	items := make([]standingDTO, 0, len(standings.Items))
	for _, it := range standings.Items {
		segs := make([]segmentScoreDTO, 0, len(it.Segments))
		for _, sg := range it.Segments {
			segs = append(segs, segmentScoreDTO{
				SegmentID:  sg.SegmentID,
				ScoreUnits: sg.ScoreUnits,
				MaxUnits:   sg.MaxUnits,
			})
		}
		items = append(items, standingDTO{
			SubjectID:   it.SubjectID,
			DisplayName: it.DisplayName,
			PublicCode:  it.PublicCode,
			Rank:        it.Rank,
			ScoreUnits:  it.ScoreUnits,
			Segments:    segs,
		})
	}
	ids := standings.CorrectionIDs
	if ids == nil {
		ids = []string{}
	}
	s.writeJSON(w, r, http.StatusOK, standingsDTO{
		EventID:       standings.EventID,
		EventVersion:  standings.EventVersion,
		Items:         items,
		CorrectionIDs: ids,
	})
}

// handleCreateScoreCorrection records an auditable score change.
func (s *Server) handleCreateScoreCorrection(w http.ResponseWriter, r *http.Request) {
	sess, ok := sessionOf(r)
	if !ok {
		s.writeError(w, r, session.ErrNoSession)
		return
	}
	eventID, err := pathParam(r, "eventId")
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
	if replayed, err := s.replayIfPresent(w, r, sess.ID, "createScoreCorrection", eventID, key, raw); err != nil || replayed {
		if err != nil {
			s.writeError(w, r, err)
		}
		return
	}
	var body correctionCreateDTO
	if err := decodeJSON(&body, raw); err != nil {
		s.writeError(w, r, err)
		return
	}
	event, err := s.rooms.GetEventForActor(r.Context(), sess.ID, eventID)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	id, version, acceptedAt, err := s.rooms.CreateScoreCorrectionAs(r.Context(), event, room.CorrectionInput{
		SegmentID:            body.SegmentID,
		SubjectID:            body.SubjectID,
		NewScoreUnits:        body.NewScoreUnits,
		Reason:               body.Reason,
		ExpectedEventVersion: body.ExpectedEventVersion,
	})
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	dto := correctionDTO{
		CorrectionID: id,
		EventVersion: version,
		AcceptedAt:   rfc3339(acceptedAt),
	}
	w.Header().Set("Location", "/api/v1/events/"+eventID+"/score-corrections/"+id)
	if err := s.storeReceipt(r, sess.ID, "createScoreCorrection", eventID, key, raw, http.StatusCreated, dto); err != nil {
		s.writeError(w, r, err)
		return
	}
	s.writeJSON(w, r, http.StatusCreated, dto)
}

// ---- grants --------------------------------------------------------------

// handleCreateDisplayGrant issues a display pairing code.
func (s *Server) handleCreateDisplayGrant(w http.ResponseWriter, r *http.Request) {
	sess, ok := sessionOf(r)
	if !ok {
		s.writeError(w, r, session.ErrNoSession)
		return
	}
	eventID, err := pathParam(r, "eventId")
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
	if replayed, err := s.replayIfPresent(w, r, sess.ID, "createDisplayGrant", eventID, key, raw); err != nil || replayed {
		if err != nil {
			s.writeError(w, r, err)
		}
		return
	}
	event, err := s.rooms.GetEventForActor(r.Context(), sess.ID, eventID)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	grant, err := s.rooms.CreateDisplayGrantAs(r.Context(), event)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	dto := grantDTO{
		GrantCode: grant.Code,
		ExpiresAt: rfc3339(grant.ExpiresAt),
		SingleUse: true,
	}
	w.Header().Set("Location", "/api/v1/events/"+eventID+"/display-grants")
	if err := s.storeReceipt(r, sess.ID, "createDisplayGrant", eventID, key, raw, http.StatusCreated, dto); err != nil {
		s.writeError(w, r, err)
		return
	}
	s.writeJSON(w, r, http.StatusCreated, dto)
}

// handleRedeemDisplayGrant exchanges a code for a restricted display cookie.
//
// The display session uses its own cookie name so it can never overwrite the
// host's console cookie .
func (s *Server) handleRedeemDisplayGrant(w http.ResponseWriter, r *http.Request) {
	if s.sessions == nil || s.rooms == nil {
		s.writeError(w, r, problem.NotImplemented("unavailable", "grant redemption is not configured"))
		return
	}
	if err := s.sessions.OriginAllowed(r); err != nil {
		s.writeError(w, r, err)
		return
	}
	raw, err := readRawBody(r)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	var body displayGrantRedeemDTO
	if err := decodeJSON(&body, raw); err != nil {
		s.writeError(w, r, err)
		return
	}
	// Refuse a host-authority caller BEFORE consuming the code.
	//
	// A display session is a new, restricted identity. If the caller already holds
	// an interactive session with host authority, overwriting would break their
	// console, so the exchange is refused with an explicit instruction instead.
	//
	// The order matters: redemption is single-use, so checking afterwards would
	// burn the code on every accidental click from the host's own browser — the
	// host would then need a fresh code each time, and a legitimately shared code
	// would be dead. The lookup below only reads; nothing is consumed until the
	// caller is known to be eligible.
	if token := s.sessions.CookieToken(r); token != "" {
		if existing, lerr := s.sessions.Lookup(r.Context(), token); lerr == nil {
			// The grant code is the only thing that names the room, so resolve the
			// room without consuming it. PeekDisplayGrant is read-only.
			if peekRoom, perr := s.rooms.PeekDisplayGrant(r.Context(), body.GrantCode); perr == nil {
				if actor, aerr := s.rooms.LoadActor(r.Context(), existing.ID, peekRoom); aerr == nil {
					if actor.Has(room.CapHost) || actor.Has(room.CapManageRoom) {
						s.writeError(w, r, problem.Forbidden("use-separate-profile",
							"this browser already holds host authority; redeem the display code in a separate browser profile"))
						return
					}
				}
			}
		}
	}

	eventID, roomID, err := s.rooms.RedeemDisplayGrant(r.Context(), body.GrantCode, body.EventID)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	res, err := s.sessions.CreateDisplay(r.Context())
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	// Give the new display session a read-only membership in the room.
	//
	// A session by itself carries no room relationship, and every read path
	// authorises through LoadActor, which resolves capabilities from memberships
	// and grants. Without this row the paired screen authenticates successfully and
	// then sees nothing (401 on the room, 404 on standings).
	if err := s.rooms.AttachDisplaySession(r.Context(), roomID, res.Session.ID); err != nil {
		s.writeError(w, r, err)
		return
	}
	s.sessions.SetDisplayCookie(w, res.Token, res.Session.ExpiresAt)
	w.Header().Set("Location", "/api/v1/events/"+eventID)
	s.writeJSON(w, r, http.StatusCreated, displaySessionDTO{
		EventID:   eventID,
		RoomID:    roomID,
		Role:      "display",
		ExpiresAt: rfc3339(res.Session.ExpiresAt.UnixMilli()),
	})
}

// handleCreateRecoveryGrant issues a membership recovery code.
func (s *Server) handleCreateRecoveryGrant(w http.ResponseWriter, r *http.Request) {
	sess, ok := sessionOf(r)
	if !ok {
		s.writeError(w, r, session.ErrNoSession)
		return
	}
	membershipID, err := pathParam(r, "membershipId")
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
	if replayed, err := s.replayIfPresent(w, r, sess.ID, "createRecoveryGrant", membershipID, key, raw); err != nil || replayed {
		if err != nil {
			s.writeError(w, r, err)
		}
		return
	}
	var body recoveryCreateDTO
	if err := decodeJSON(&body, raw); err != nil {
		s.writeError(w, r, err)
		return
	}
	rm, err := s.rooms.RoomOfMembership(r.Context(), membershipID)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	actor, err := s.rooms.LoadActor(r.Context(), sess.ID, rm)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	grant, err := s.rooms.CreateRecoveryGrant(r.Context(), actor, membershipID, body.Reason)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	dto := grantDTO{
		GrantCode: grant.Code,
		ExpiresAt: rfc3339(grant.ExpiresAt),
		SingleUse: true,
	}
	w.Header().Set("Location", "/api/v1/memberships/"+membershipID+"/recovery-grants")
	if err := s.storeReceipt(r, sess.ID, "createRecoveryGrant", membershipID, key, raw, http.StatusCreated, dto); err != nil {
		s.writeError(w, r, err)
		return
	}
	s.writeJSON(w, r, http.StatusCreated, dto)
}

// handleRedeemRecoveryGrant exchanges a recovery code for a session.
//
// The recovered identity is the original membership, not a new one, and the
// previous controller lease is revoked so two devices cannot both write.
func (s *Server) handleRedeemRecoveryGrant(w http.ResponseWriter, r *http.Request) {
	if s.sessions == nil || s.rooms == nil {
		s.writeError(w, r, problem.NotImplemented("unavailable", "recovery is not configured"))
		return
	}
	if err := s.sessions.OriginAllowed(r); err != nil {
		s.writeError(w, r, err)
		return
	}
	raw, err := readRawBody(r)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	var body grantRedeemDTO
	if err := decodeJSON(&body, raw); err != nil {
		s.writeError(w, r, err)
		return
	}
	previous, form, accessErr := s.rooms.RecoveryAccess(r.Context(), body.GrantCode)
	if accessErr != nil {
		s.writeError(w, r, problem.Internal(nil))
		return
	}
	if previous != "" && !s.requireAccount(w, r, form, previous) {
		return
	}
	var res session.CreateResult
	_, _, err = s.rooms.RedeemRecoveryGrant(r.Context(), body.GrantCode, func(ctx context.Context, tx *sql.Tx, name string) (string, error) {
		var err error
		res, err = s.sessions.CreateInTransaction(ctx, tx, name)
		return res.Session.ID, err
	})
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	s.sessions.SetCookie(w, res.Token, res.Session.ExpiresAt)
	w.Header().Set("Location", "/api/v1/sessions/current")
	s.writeJSON(w, r, http.StatusCreated, sessionDTO{
		SessionID:   res.Session.ID,
		DisplayName: res.Session.DisplayName,
		CSRFToken:   res.CSRFToken,
		ExpiresAt:   rfc3339(res.Session.ExpiresAt.UnixMilli()),
		ServerTime:  rfc3339(nowMS()),
	})
}

// handleResolveRoomInvitation maps an invite code to a minimal join target.
func (s *Server) handleResolveRoomInvitation(w http.ResponseWriter, r *http.Request) {
	if s.rooms == nil {
		s.writeError(w, r, problem.NotImplemented("unavailable", "rooms are not configured"))
		return
	}
	if err := s.sessions.OriginAllowed(r); err != nil {
		s.writeError(w, r, err)
		return
	}
	raw, err := readRawBody(r)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	var body inviteResolutionCreateDTO
	if err := decodeJSON(&body, raw); err != nil {
		s.writeError(w, r, err)
		return
	}
	roomID, joinable, err := s.rooms.ResolveInvitation(r.Context(), body.InviteCode)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	s.writeJSON(w, r, http.StatusOK, inviteResolutionDTO{RoomID: roomID, Joinable: joinable})
}
