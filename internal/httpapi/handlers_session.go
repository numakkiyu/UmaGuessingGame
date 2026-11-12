package httpapi

import (
	"errors"
	"net/http"
	"sort"
	"strings"

	"github.com/bhcn/umaguessinggame/internal/httpapi/problem"
	"github.com/bhcn/umaguessinggame/internal/modes"
	"github.com/bhcn/umaguessinggame/internal/room"
	"github.com/bhcn/umaguessinggame/internal/session"
)

// ---- sessions ------------------------------------------------------------

// handleCreateSession creates an anonymous session.
//
// This is the one cookie-writing operation with no prior CSRF token, so it relies
// on a strict Origin check plus the public-IP limiter . A missing Origin
// is rejected unless the deployment explicitly opted out for a private network.
func (s *Server) handleCreateSession(w http.ResponseWriter, r *http.Request) {
	if s.sessions == nil {
		s.writeError(w, r, problem.NotImplemented("sessions-unavailable", "session support is not configured"))
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
	var body sessionCreateDTO
	if err := decodeJSON(&body, raw); err != nil {
		s.writeError(w, r, err)
		return
	}
	// Repeated creation with a valid cookie restores that identity rather than
	// silently replacing a participant and losing their room membership.
	if current, lookupErr := s.sessions.Lookup(r.Context(), s.sessions.CookieToken(r)); lookupErr == nil && current.Kind == session.KindPlayer {
		csrf, rotateErr := s.sessions.RotateCSRF(r.Context(), current)
		if rotateErr != nil {
			s.writeError(w, r, rotateErr)
			return
		}
		s.writeJSON(w, r, http.StatusOK, sessionDTO{SessionID: current.ID, DisplayName: current.DisplayName, CSRFToken: csrf, ExpiresAt: rfc3339(current.ExpiresAt.UnixMilli()), ServerTime: rfc3339(nowMS())})
		return
	} else if lookupErr != nil && !isMissingSession(lookupErr) {
		s.writeError(w, r, lookupErr)
		return
	}
	res, err := s.sessions.Create(r.Context(), body.DisplayName)
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

// handleGetCurrentSession reads the current session.
func (s *Server) handleGetCurrentSession(w http.ResponseWriter, r *http.Request) {
	sess, ok := sessionOf(r)
	if !ok {
		s.writeError(w, r, session.ErrNoSession)
		return
	}
	full, err := s.sessions.Lookup(r.Context(), s.sessions.CookieToken(r))
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	// The CSRF token is not recoverable from the stored hash, so the session body
	// for a read carries an empty csrfToken. A client that needs it re-reads the
	// value it received at creation, which is the only time it is issued.
	s.writeJSON(w, r, http.StatusOK, sessionDTO{
		SessionID:   sess.ID,
		DisplayName: full.DisplayName,
		CSRFToken:   "",
		ExpiresAt:   rfc3339(full.ExpiresAt.UnixMilli()),
		ServerTime:  rfc3339(nowMS()),
	})
}

// handleUpdateCurrentSession renames the session.
func (s *Server) handleUpdateCurrentSession(w http.ResponseWriter, r *http.Request) {
	sess, ok := sessionOf(r)
	if !ok {
		s.writeError(w, r, session.ErrNoSession)
		return
	}
	raw, err := readRawBody(r)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	key := r.Header.Get("Idempotency-Key")
	replayed, err := s.replayIfPresent(w, r, sess.ID, "updateCurrentSession", "", key, raw)
	if err != nil || replayed {
		if err != nil {
			s.writeError(w, r, err)
		}
		return
	}
	var body sessionUpdateDTO
	if err := decodeJSON(&body, raw); err != nil {
		s.writeError(w, r, err)
		return
	}
	full, err := s.sessions.Lookup(r.Context(), s.sessions.CookieToken(r))
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	updated, err := s.sessions.Rename(r.Context(), full, body.DisplayName)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	dto := sessionDTO{
		SessionID:   updated.ID,
		DisplayName: updated.DisplayName,
		CSRFToken:   "",
		ExpiresAt:   rfc3339(updated.ExpiresAt.UnixMilli()),
		ServerTime:  rfc3339(nowMS()),
	}
	if err := s.storeReceipt(r, sess.ID, "updateCurrentSession", "", key, raw, http.StatusOK, dto); err != nil {
		s.writeError(w, r, err)
		return
	}
	s.writeJSON(w, r, http.StatusOK, dto)
}

// handleDeleteCurrentSession revokes the session.
//
// A repeat call still returns 204 but never reactivates the identity .
func (s *Server) handleDeleteCurrentSession(w http.ResponseWriter, r *http.Request) {
	sess, ok := sessionOf(r)
	if !ok {
		s.writeError(w, r, session.ErrNoSession)
		return
	}
	raw, err := readRawBody(r)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	key := r.Header.Get("Idempotency-Key")
	replayed, err := s.replayIfPresent(w, r, sess.ID, "deleteCurrentSession", "", key, raw)
	if err != nil || replayed {
		if err != nil {
			s.writeError(w, r, err)
		}
		return
	}
	full, err := s.sessions.Lookup(r.Context(), s.sessions.CookieToken(r))
	if err != nil {
		// Already revoked or expired: the outcome the caller wanted already holds.
		s.sessions.ClearCookie(w)
		s.writeNoContent(w, r)
		return
	}
	if err := s.sessions.Revoke(r.Context(), full); err != nil {
		s.writeError(w, r, err)
		return
	}
	s.sessions.ClearCookie(w)
	if err := s.storeReceipt(r, sess.ID, "deleteCurrentSession", "", key, raw, http.StatusNoContent, nil); err != nil {
		s.writeError(w, r, err)
		return
	}
	s.writeNoContent(w, r)
}

// ---- config, modes, characters -------------------------------------------

// handleGetPublicConfig returns the minimal public configuration.
//
// Only genuinely usable capabilities are advertised: a mode whose content gate
// fails is not listed as enabled .
func (s *Server) handleGetPublicConfig(w http.ResponseWriter, r *http.Request) {
	enabled := []string{}
	if s.rooms != nil && s.rooms.Modes() != nil {
		for _, m := range s.rooms.Modes().All() {
			if s.rooms.ModeAvailability(m).Available {
				enabled = append(enabled, m.ID())
			}
		}
	}
	sort.Strings(enabled)
	s.writeJSON(w, r, http.StatusOK, publicConfigDTO{
		SiteName:          s.cfg.SiteName,
		Admission:         s.admission.Public(),
		APIVersion:        s.cfg.APIVersion,
		ServerTime:        rfc3339(nowMS()),
		DeploymentMode:    s.cfg.DeploymentMode,
		MaxRoomMembers:    s.cfg.MaxRoomMembers,
		MaxRoomSpectators: s.cfg.MaxRoomSpectators,
		EnabledModeIDs:    enabled,
		ModeCapabilities:  s.rooms.ModeCapabilities(),
		DefaultTransport:  s.cfg.DefaultTransport,
		AssetBaseURL:      s.assetBaseURL(),
		BuildVersion:      s.cfg.BuildVersion,
	})
}

// assetBaseURL prefers the configured value, then the content index.
func (s *Server) assetBaseURL() string {
	if s.cfg.AssetBaseURL != "" {
		return s.cfg.AssetBaseURL
	}
	if s.rooms != nil {
		return s.rooms.AssetBaseURL()
	}
	return "/assets/"
}

// handleListModes lists registered modes with their real availability.

// primaryInformationStyle picks the form to advertise in the mode list.
//
// A mode that implements both forms is advertised as "open", because that is the
// one a host cannot otherwise discover: the closed form is the default and needs
// no mention, while the open form changes how the round plays.
func primaryInformationStyle(styles []string) string {
	for _, style := range styles {
		if style == "open" {
			return "open"
		}
	}
	return "closed"
}

func (s *Server) handleListModes(w http.ResponseWriter, r *http.Request) {
	items := []modeDTO{}
	if s.rooms != nil && s.rooms.Modes() != nil {
		for _, m := range s.rooms.Modes().All() {
			avail := s.rooms.ModeAvailability(m)
			item := modeDTO{
				ModeID:           m.ID(),
				Name:             m.Title(),
				Description:      m.Summary(),
				RulesVersion:     m.RulesVersion(),
				Availability:     "available",
				MinPlayers:       maxInt(m.MinSubjects(), 1),
				MaxPlayers:       maxInt(m.MaxSubjects(), 1),
				EstimatedMinutes: 10,
				InformationStyle: primaryInformationStyle(m.InformationStyles()),
				SupportsTeams:    m.MaxSubjects() > 1,
			}
			if !avail.Available {
				item.Availability = "disabled"
				item.UnavailableReason = avail.Reason
			}
			items = append(items, item)
		}
	}
	s.writeJSON(w, r, http.StatusOK, modeListDTO{
		Items:      items,
		ServerTime: rfc3339(nowMS()),
	})
}

// handleListCharacters pages the public character index.
func (s *Server) handleListCharacters(w http.ResponseWriter, r *http.Request) {
	cursor, limit, err := pagination(r)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	q := r.URL.Query().Get("q")
	items, next, datasetVersion, err := s.rooms.Characters(r.Context(), cursor, limit, q)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	out := make([]characterDTO, 0, len(items))
	for _, c := range items {
		out = append(out, toCharacterDTO(c))
	}
	var nextPtr *string
	if next != "" {
		nextPtr = &next
	}
	s.writeJSON(w, r, http.StatusOK, characterListDTO{
		Items:          out,
		NextCursor:     nextPtr,
		DatasetVersion: datasetVersion,
	})
}

// handleGetCharacter reads one public character.
func (s *Server) handleGetCharacter(w http.ResponseWriter, r *http.Request) {
	id, err := pathParam(r, "characterId")
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	card, err := s.rooms.Character(r.Context(), id)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	s.writeJSON(w, r, http.StatusOK, toCharacterDTO(card))
}

// toCharacterDTO maps a public card onto the wire schema.
func toCharacterDTO(c room.CharacterCard) characterDTO {
	aliases := c.Aliases
	if aliases == nil {
		aliases = []string{}
	}
	kind := c.EntityKind
	if kind == "" {
		kind = "unclassified"
	}
	dto := characterDTO{
		LocalizedNames:  c.LocalizedNames,
		SearchNames:     c.SearchNames,
		CharacterID:     c.CharacterID,
		DisplayName:     c.DisplayName,
		Aliases:         aliases,
		EntityKind:      kind,
		PortraitAssetID: c.PortraitAssetID,
		ImagePath:       c.ImagePath,
		RemoteImageURL:  c.RemoteImageURL,
		Birthday:        c.Birthday,
		HeightCm:        c.HeightCm,
		DatasetVersion:  c.DatasetVersion,
	}
	if len(c.Aptitudes) > 0 {
		dto.Aptitudes = toAptitudesDTO(c.Aptitudes)
	}
	return dto
}

// toAptitudesDTO maps the ten aptitude grades, requiring all ten.
func toAptitudesDTO(m map[string]string) *aptitudesDTO {
	keys := []string{"turf", "dirt", "sprint", "mile", "medium", "long", "front", "pace", "late", "end"}
	for _, k := range keys {
		if m[k] == "" {
			// A partial aptitude set would violate the contract's required list,
			// so it is omitted rather than emitted with holes.
			return nil
		}
	}
	return &aptitudesDTO{
		Turf: m["turf"], Dirt: m["dirt"], Sprint: m["sprint"], Mile: m["mile"],
		Medium: m["medium"], Long: m["long"], Front: m["front"], Pace: m["pace"],
		Late: m["late"], End: m["end"],
	}
}

// maxInt returns the larger of a and b.
func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// modeConfigModeID extracts the modeId from a mode configuration object.
func modeConfigModeID(raw []byte) string {
	var probe struct {
		ModeID string `json:"modeId"`
	}
	if err := jsonUnmarshal(raw, &probe); err != nil {
		return ""
	}
	return strings.TrimSpace(probe.ModeID)
}

// knownModeIDs is the contract's ModeId enum, used to reject unknown modes before
// the registry is consulted.
var knownModeIDs = map[string]bool{
	"classic": true, "aptitude-grid": true, "draft": true, "race-rebuild": true,
	"co-race-path": true, "sort": true, "memory": true, "bluff": true,
	"clue-puzzle": true, "strategy-race": true, "rapport": true, "creative": true,
	"trade": true, "pedigree": true, "audio": true,
}

// modeRegistryAll returns the registry's modes for iteration, or nil.
func (s *Server) modeRegistryAll() []modes.Mode {
	if s.rooms == nil || s.rooms.Modes() == nil {
		return nil
	}
	return s.rooms.Modes().All()
}

// handleRotateSessionCsrf issues a fresh CSRF token for the current session.
// It requires a session cookie and allowed Origin, but not the lost token; membership is preserved.
func (s *Server) handleRotateSessionCsrf(w http.ResponseWriter, r *http.Request) {
	sess, ok := sessionOf(r)
	if !ok {
		s.writeError(w, r, session.ErrNoSession)
		return
	}
	token, err := s.sessions.RotateCSRF(r.Context(), session.Session{ID: sess.ID, Kind: sess.Kind})
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	s.writeJSON(w, r, http.StatusOK, map[string]any{
		"csrfToken": token,
		"sessionId": sess.ID,
	})
}

func isMissingSession(err error) bool {
	var p *problem.Error
	return errors.As(err, &p) && p.Status == http.StatusUnauthorized
}
func (s *Server) handleVerifyAdmission(w http.ResponseWriter, r *http.Request) {
	sess, ok := sessionOf(r)
	if !ok {
		s.writeError(w, r, session.ErrNoSession)
		return
	}
	if s.admission.Allowed(r, sess.ID) {
		s.writeJSON(w, r, http.StatusOK, map[string]bool{"verified": true})
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 4096)
	raw, err := readRawBody(r)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	var body struct {
		Token         string `json:"token"`
		BrowserSignal string `json:"browserSignal,omitempty"`
	}
	if err := decodeJSON(&body, raw); err != nil {
		s.writeError(w, r, err)
		return
	}
	if key, rate, burst := s.admission.SignalBucket(body.BrowserSignal); key != "" {
		if ok, retry := s.limiter.Allow(key, Rate{PerMinute: rate, BurstTokens: burst}); !ok {
			s.writeError(w, r, problem.RateLimited(retry))
			return
		}
	}
	if err := s.admission.Verify(r.Context(), body.Token); err != nil {
		s.writeError(w, r, err)
		return
	}
	s.admission.Grant(w, sess.ID, sess.ExpiresAt)
	s.writeJSON(w, r, http.StatusOK, map[string]bool{"verified": true})
}
