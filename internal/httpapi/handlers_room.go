package httpapi

import (
	"bytes"
	"encoding/json"
	"net/http"

	"github.com/bhcn/umaguessinggame/internal/httpapi/problem"
	"github.com/bhcn/umaguessinggame/internal/room"
	"github.com/bhcn/umaguessinggame/internal/session"
)

// ---- rooms ---------------------------------------------------------------

// handleCreateRoom creates a room and its owner membership.
func (s *Server) handleCreateRoom(w http.ResponseWriter, r *http.Request) {
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
	var body roomCreateDTO
	if err := decodeJSON(&body, raw); err != nil {
		s.writeError(w, r, err)
		return
	}
	if !s.requireAccount(w, r, body.Kind, sess.ID) {
		return
	}
	if replayed, err := s.replayIfPresent(w, r, sess.ID, "createRoom", "", key, raw); err != nil || replayed {
		if err != nil {
			s.writeError(w, r, err)
		}
		return
	}
	res, err := s.rooms.CreateRoom(r.Context(), sess.ID, room.RoomCreateInput{
		Command:         &room.CreationCommand{Key: key, Payload: raw},
		Name:            body.Name,
		Kind:            body.Kind,
		ModeConfig:      body.ModeConfig.Raw,
		MaxPlayers:      body.MaxPlayers,
		MaxSpectators:   body.MaxSpectators,
		Visibility:      body.Visibility,
		AllowSpectators: body.AllowSpectators,
	})
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	snapshot, err := s.buildRoomSnapshot(r, res.Room)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	dto := roomCreationDTO{
		Room:       snapshot,
		InviteCode: res.InviteCode,
		InviteURL:  "/join?code=" + res.InviteCode,
	}
	w.Header().Set("Location", "/api/v1/rooms/"+res.Room.ID)
	if err := s.storeReceipt(r, sess.ID, "createRoom", "", key, raw, http.StatusCreated, dto); err != nil {
		s.writeError(w, r, err)
		return
	}
	s.writeJSON(w, r, http.StatusCreated, dto)
}

// handleGetRoom returns the authorised room snapshot.
func (s *Server) handleGetRoom(w http.ResponseWriter, r *http.Request) {
	sess, ok := sessionOf(r)
	if !ok {
		s.writeError(w, r, session.ErrNoSession)
		return
	}
	roomID, err := pathParam(r, "roomId")
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	rm, _, err := s.rooms.GetRoom(r.Context(), sess.ID, roomID)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	snapshot, err := s.buildRoomSnapshot(r, rm)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	s.writeJSON(w, r, http.StatusOK, snapshot)
}

// handleUpdateRoom changes lobby configuration.
func (s *Server) handleUpdateRoom(w http.ResponseWriter, r *http.Request) {
	sess, ok := sessionOf(r)
	if !ok {
		s.writeError(w, r, session.ErrNoSession)
		return
	}
	roomID, err := pathParam(r, "roomId")
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
	if replayed, err := s.replayIfPresent(w, r, sess.ID, "updateRoom", roomID, key, raw); err != nil || replayed {
		if err != nil {
			s.writeError(w, r, err)
		}
		return
	}
	var body roomPatchDTO
	if err := decodeJSON(&body, raw); err != nil {
		s.writeError(w, r, err)
		return
	}
	actor, err := s.rooms.LoadActor(r.Context(), sess.ID, roomID)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	updated, err := s.rooms.UpdateRoom(r.Context(), actor, room.RoomPatchInput{
		ExpectedRoomVersion: body.ExpectedRoomVersion,
		Name:                body.Name,
		ModeConfig:          body.ModeConfig.Raw,
		AllowSpectators:     body.AllowSpectators,
	})
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	snapshot, err := s.buildRoomSnapshot(r, updated)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	if err := s.storeReceipt(r, sess.ID, "updateRoom", roomID, key, raw, http.StatusOK, snapshot); err != nil {
		s.writeError(w, r, err)
		return
	}
	s.writeJSON(w, r, http.StatusOK, snapshot)
}

// handleJoinRoom joins using an invitation code.
func (s *Server) handleJoinRoom(w http.ResponseWriter, r *http.Request) {
	sess, ok := sessionOf(r)
	if !ok {
		s.writeError(w, r, session.ErrNoSession)
		return
	}
	roomID, err := pathParam(r, "roomId")
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
	if replayed, err := s.replayIfPresent(w, r, sess.ID, "joinRoom", roomID, key, raw); err != nil || replayed {
		if err != nil {
			s.writeError(w, r, err)
		}
		return
	}
	var body membershipCreateDTO
	if err := decodeJSON(&body, raw); err != nil {
		s.writeError(w, r, err)
		return
	}
	m, err := s.rooms.Join(r.Context(), sess.ID, roomID, room.JoinInput{
		InviteCode:    body.InviteCode,
		RequestedRole: body.RequestedRole,
	})
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	dto := toMembershipDTO(m, s.actorCapabilities(r, sess.ID, roomID, m.ID))
	w.Header().Set("Location", "/api/v1/rooms/"+roomID+"/memberships/"+m.ID)
	if err := s.storeReceipt(r, sess.ID, "joinRoom", roomID, key, raw, http.StatusCreated, dto); err != nil {
		s.writeError(w, r, err)
		return
	}
	s.writeJSON(w, r, http.StatusCreated, dto)
}

// handleUpdateMembership changes readiness, team or role.
func (s *Server) handleUpdateMembership(w http.ResponseWriter, r *http.Request) {
	sess, ok := sessionOf(r)
	if !ok {
		s.writeError(w, r, session.ErrNoSession)
		return
	}
	roomID, err := pathParam(r, "roomId")
	if err != nil {
		s.writeError(w, r, err)
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
	if replayed, err := s.replayIfPresent(w, r, sess.ID, "updateMembership", membershipID, key, raw); err != nil || replayed {
		if err != nil {
			s.writeError(w, r, err)
		}
		return
	}
	var body membershipPatchDTO
	if err := decodeJSON(&body, raw); err != nil {
		s.writeError(w, r, err)
		return
	}
	actor, err := s.rooms.LoadActor(r.Context(), sess.ID, roomID)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	// A nullable patch distinguishes an omitted field (keep placement) from
	// explicit null (leave team). A plain *string loses this distinction.
	var presence struct {
		TeamID json.RawMessage `json:"teamId"`
	}
	if err := json.Unmarshal(raw, &presence); err != nil {
		s.writeError(w, r, problem.BadRequest("invalid-json", "invalid JSON"))
		return
	}
	if bytes.Equal(bytes.TrimSpace(presence.TeamID), []byte("null")) {
		empty := ""
		body.TeamID = &empty
	}
	m, err := s.rooms.UpdateMembership(r.Context(), actor, membershipID, room.MembershipPatchInput{
		ExpectedRoomVersion: body.ExpectedRoomVersion,
		Ready:               body.Ready,
		TeamID:              body.TeamID,
		RequestedRole:       body.RequestedRole,
	})
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	dto := toMembershipDTO(m, s.actorCapabilities(r, sess.ID, roomID, m.ID))
	if err := s.storeReceipt(r, sess.ID, "updateMembership", membershipID, key, raw, http.StatusOK, dto); err != nil {
		s.writeError(w, r, err)
		return
	}
	s.writeJSON(w, r, http.StatusOK, dto)
}

// handleLeaveOrRemoveMembership removes a membership.
func (s *Server) handleLeaveOrRemoveMembership(w http.ResponseWriter, r *http.Request) {
	sess, ok := sessionOf(r)
	if !ok {
		s.writeError(w, r, session.ErrNoSession)
		return
	}
	roomID, err := pathParam(r, "roomId")
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	membershipID, err := pathParam(r, "membershipId")
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	// Only a player's own membership may leave without completing verification.
	if s.admission.RiskRequired(sess.ID) {
		actor, loadErr := s.rooms.LoadActor(r.Context(), sess.ID, roomID)
		// A completed departure can be retried after its response was lost.
		// LeaveOrRemoveBySession checks ownership for inactive memberships.
		if loadErr == nil && actor.MembershipID() != membershipID && s.blockRiskAdmission(w, r) {
			return
		}
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
	if replayed, err := s.replayIfPresent(w, r, sess.ID, "leaveOrRemoveMembership", membershipID, key, raw); err != nil || replayed {
		if err != nil {
			s.writeError(w, r, err)
		}
		return
	}
	if err := s.rooms.LeaveOrRemoveBySession(r.Context(), sess.ID, roomID, membershipID); err != nil {
		s.writeError(w, r, err)
		return
	}
	if err := s.storeReceipt(r, sess.ID, "leaveOrRemoveMembership", membershipID, key, raw, http.StatusNoContent, nil); err != nil {
		s.writeError(w, r, err)
		return
	}
	s.writeNoContent(w, r)
}

// handleCreateTeam adds a team in lobby.
func (s *Server) handleCreateTeam(w http.ResponseWriter, r *http.Request) {
	sess, ok := sessionOf(r)
	if !ok {
		s.writeError(w, r, session.ErrNoSession)
		return
	}
	roomID, err := pathParam(r, "roomId")
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
	if replayed, err := s.replayIfPresent(w, r, sess.ID, "createTeam", roomID, key, raw); err != nil || replayed {
		if err != nil {
			s.writeError(w, r, err)
		}
		return
	}
	var body teamCreateDTO
	if err := decodeJSON(&body, raw); err != nil {
		s.writeError(w, r, err)
		return
	}
	actor, err := s.rooms.LoadActor(r.Context(), sess.ID, roomID)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	team, err := s.rooms.CreateTeam(r.Context(), actor, body.Label, body.ColorToken)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	dto := teamDTO{TeamID: team.ID, Label: team.Label, ColorToken: team.Color}
	w.Header().Set("Location", "/api/v1/rooms/"+roomID+"/teams/"+team.ID)
	if err := s.storeReceipt(r, sess.ID, "createTeam", roomID, key, raw, http.StatusCreated, dto); err != nil {
		s.writeError(w, r, err)
		return
	}
	s.writeJSON(w, r, http.StatusCreated, dto)
}

// handleIssueControlLease issues or rotates the round controller lease.
func (s *Server) handleIssueControlLease(w http.ResponseWriter, r *http.Request) {
	sess, ok := sessionOf(r)
	if !ok {
		s.writeError(w, r, session.ErrNoSession)
		return
	}
	roomID, err := pathParam(r, "roomId")
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
	if replayed, err := s.replayIfPresent(w, r, sess.ID, "issueControlLease", roomID, key, raw); err != nil || replayed {
		if err != nil {
			s.writeError(w, r, err)
		}
		return
	}
	var body controlLeaseRequestDTO
	if err := decodeJSON(&body, raw); err != nil {
		s.writeError(w, r, err)
		return
	}
	actor, err := s.rooms.LoadActor(r.Context(), sess.ID, roomID)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	lease, err := s.rooms.IssueControlLease(r.Context(), actor, room.IssueControlLeaseInput{
		RoundID:            body.RoundID,
		MembershipID:       body.MembershipID,
		ExpectedGeneration: body.ExpectedGeneration,
	})
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	dto := controlLeaseDTO{
		LeaseID:           lease.ID,
		RoundID:           lease.RoundID,
		MembershipID:      lease.MembershipID,
		ControlGeneration: lease.Generation,
		ExpiresAt:         rfc3339(lease.ExpiresAt.UnixMilli()),
	}
	w.Header().Set("Location", "/api/v1/rooms/"+roomID+"/control-leases/"+lease.ID)
	if err := s.storeReceipt(r, sess.ID, "issueControlLease", roomID, key, raw, http.StatusCreated, dto); err != nil {
		s.writeError(w, r, err)
		return
	}
	s.writeJSON(w, r, http.StatusCreated, dto)
}

// handleCreateRound prepares a round.
func (s *Server) handleCreateRound(w http.ResponseWriter, r *http.Request) {
	sess, ok := sessionOf(r)
	if !ok {
		s.writeError(w, r, session.ErrNoSession)
		return
	}
	roomID, err := pathParam(r, "roomId")
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
	if replayed, err := s.replayIfPresent(w, r, sess.ID, "createRound", roomID, key, raw); err != nil || replayed {
		if err != nil {
			s.writeError(w, r, err)
		}
		return
	}
	var body roundCreateDTO
	if err := decodeJSON(&body, raw); err != nil {
		s.writeError(w, r, err)
		return
	}
	if body.ModeConfig.Set && len(body.ModeConfig.Raw) == 0 {
		s.writeError(w, r, problem.Validation("modeConfig must be an object when provided"))
		return
	}
	actor, err := s.rooms.LoadActor(r.Context(), sess.ID, roomID)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	round, err := s.rooms.CreateRound(r.Context(), actor, room.RoundCreateInput{
		ModeConfig: body.ModeConfig.Raw,
		SegmentID:  body.SegmentID,
	})
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	dto := toRoundSummaryDTO(round)
	w.Header().Set("Location", "/api/v1/rounds/"+round.ID)
	if err := s.storeReceipt(r, sess.ID, "createRound", roomID, key, raw, http.StatusCreated, dto); err != nil {
		s.writeError(w, r, err)
		return
	}
	s.writeJSON(w, r, http.StatusCreated, dto)
}

// buildRoomSnapshot assembles the authorised room snapshot.
//
// The viewer membership is always present because GetRoom already required either
// a membership or ownership; an administrative-only owner is synthesised as a host
// projection with manage-room, which is what the room API means by "viewer".
func (s *Server) buildRoomSnapshot(r *http.Request, rm room.Room) (roomSnapshotDTO, error) {
	sess, _ := sessionOf(r)
	actor, err := s.rooms.LoadActor(r.Context(), sess.ID, rm.ID)
	if err != nil {
		return roomSnapshotDTO{}, err
	}
	members, err := s.rooms.ListMembers(r.Context(), rm.ID)
	if err != nil {
		return roomSnapshotDTO{}, err
	}
	teams, err := s.rooms.ListTeams(r.Context(), rm.ID)
	if err != nil {
		return roomSnapshotDTO{}, err
	}
	memberDTOs := make([]membershipDTO, 0, len(members))
	for _, m := range members {
		memberDTOs = append(memberDTOs, toMembershipDTO(m, s.capabilitiesFor(actor, m, rm)))
	}
	teamDTOs := make([]teamDTO, 0, len(teams))
	for _, t := range teams {
		teamDTOs = append(teamDTOs, teamDTO{TeamID: t.ID, Label: t.Label, ColorToken: t.Color})
	}

	viewer := membershipDTO{
		MembershipID: actor.MembershipID(),
		DisplayName:  actor.DisplayName,
		Role:         string(room.RoleHost),
		Ready:        false,
		Online:       true,
		Capabilities: capabilityList(actor.Capabilities),
	}
	if actor.Membership != nil {
		viewer = toMembershipDTO(*actor.Membership, capabilityList(actor.Capabilities))
		for _, m := range members {
			if m.ID == actor.Membership.ID {
				viewer = toMembershipDTO(m, capabilityList(actor.Capabilities))
				break
			}
		}
	}

	// Fall back to the latest settled round for display only; round admission still uses the active round
	var current *roundSummaryDTO
	if cur, err := s.rooms.CurrentRoundSummary(r.Context(), rm.ID); err == nil && cur != nil {
		dto := toRoundSummaryDTO(*cur)
		current = &dto
	} else if latest, lerr := s.rooms.LatestRoundSummary(r.Context(), rm.ID); lerr == nil && latest != nil {
		dto := toRoundSummaryDTO(*latest)
		current = &dto
	}

	var configured struct {
		ModeID string `json:"modeId"`
	}
	_ = json.Unmarshal(rm.Config.ModeConfig, &configured)
	var inviteCode *string
	if rm.Config.Kind != "matchmaking" && (actor.Capabilities[room.CapManageRoom] || actor.Capabilities[room.CapHost]) {
		code := rm.InviteCode
		inviteCode = &code
	}
	return roomSnapshotDTO{
		RoomID:       rm.ID,
		ModeID:       configured.ModeID,
		InviteCode:   inviteCode,
		Name:         rm.Name,
		Kind:         rm.Config.Kind,
		Status:       string(rm.Status),
		RoomVersion:  rm.RoomVersion,
		Viewer:       viewer,
		Members:      memberDTOs,
		Teams:        teamDTOs,
		CurrentRound: current,
		ServerTime:   rfc3339(nowMS()),
		ServerEpoch:  s.rooms.Epoch(),
		Cursor:       "c0",
	}, nil
}

// actorCapabilities resolves one member's capability list.
func (s *Server) actorCapabilities(r *http.Request, sessionID, roomID, membershipID string) []string {
	actor, err := s.rooms.LoadActor(r.Context(), sessionID, roomID)
	if err != nil {
		return nil
	}
	return capabilityList(actor.Capabilities)
}

// capabilitiesFor uses the caller's resolved capabilities for themselves and
// the participation role plus current owner for everyone else. Ownership is
// already loaded with the room; badges do not require a lookup per member.
func (s *Server) capabilitiesFor(actor room.Actor, m room.Membership, rm room.Room) []string {
	if actor.MembershipID() == m.ID {
		return capabilityList(actor.Capabilities)
	}
	caps := map[room.Capability]bool{}
	switch m.Role {
	case room.RolePlayer, room.RoleTeamCaptain:
		caps[room.CapPlay] = true
	case room.RoleSpectator, room.RoleHost:
		caps[room.CapObserve] = true
	case room.RoleDisplay:
		caps[room.CapDisplay] = true
		caps[room.CapObserve] = true
	}
	if m.SessionID == rm.OwnerID && rm.Config.Kind != "matchmaking" {
		caps[room.CapManageRoom] = true
		if rm.Config.Kind == "event" {
			caps[room.CapHost] = true
		}
	}
	return capabilityList(caps)
}

// toMembershipDTO maps a membership onto the wire schema.
func toMembershipDTO(m room.Membership, caps []string) membershipDTO {
	var teamID *string
	if m.TeamID != "" {
		t := m.TeamID
		teamID = &t
	}
	if caps == nil {
		caps = []string{}
	}
	return membershipDTO{
		MembershipID: m.ID,
		PlayerNumber: m.EntryNumber,
		PublicCode:   m.PublicCode,
		Nickname:     m.Nickname,
		DisplayName:  m.DisplayName,
		Role:         string(m.Role),
		TeamID:       teamID,
		Ready:        m.Ready,
		Online:       m.Active(),
		Capabilities: caps,
	}
}

// capabilityList renders capabilities in a stable order.
func capabilityList(caps map[room.Capability]bool) []string {
	order := []room.Capability{
		room.CapManageRoom, room.CapHost, room.CapPlay, room.CapObserve, room.CapDisplay,
	}
	out := make([]string, 0, len(caps))
	for _, c := range order {
		if caps[c] {
			out = append(out, string(c))
		}
	}
	return out
}

// toRoundSummaryDTO maps a round onto its summary schema.
func toRoundSummaryDTO(r room.Round) roundSummaryDTO {
	var deadline *string
	if r.DeadlineAt != nil {
		v := rfc3339(r.DeadlineAt.UnixMilli())
		deadline = &v
	}
	return roundSummaryDTO{
		RoundID:            r.ID,
		ModeID:             r.ModeID,
		Phase:              string(r.Status),
		PhaseVersion:       r.PhaseVersion,
		DeadlineAt:         deadline,
		Paused:             r.Paused(),
		RulesVersion:       r.RulesVersion,
		DatasetVersion:     r.DatasetVersion,
		ContentPackVersion: r.ContentPackVersion,
	}
}
