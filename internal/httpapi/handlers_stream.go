package httpapi

import (
	"encoding/json"
	"fmt"
	"net/http"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/bhcn/umaguessinggame/internal/httpapi/problem"
	"github.com/bhcn/umaguessinggame/internal/room"
	"github.com/bhcn/umaguessinggame/internal/session"
)

// ---- room event log ------------------------------------------------------

// handleListRoomEvents returns bounded notifications after a cursor.
func (s *Server) handleListRoomEvents(w http.ResponseWriter, r *http.Request) {
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
	_, limit, err := pagination(r)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	actor, err := s.rooms.LoadActor(r.Context(), sess.ID, roomID)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	items, next, resync, err := s.rooms.ListRoomEvents(r.Context(), actor, r.URL.Query().Get("cursor"), limit)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	out := make([]eventNotificationDTO, 0, len(items))
	for _, n := range items {
		out = append(out, toNotificationDTO(n))
	}
	var nextPtr *string
	if next != "" {
		nextPtr = &next
	}
	s.writeJSON(w, r, http.StatusOK, notificationListDTO{
		Items:          out,
		NextCursor:     nextPtr,
		ResyncRequired: resync,
	})
}

// toNotificationDTO maps an outbox row onto the realtime notification schema.
//
// The envelope fields are mandatory in the contract; resource/roundId/phaseVersion
// are lifted out of the stored payload only when present, so the notification never
// carries fields the schema does not declare.
func toNotificationDTO(n room.Notification) eventNotificationDTO {
	dto := eventNotificationDTO{
		SchemaVersion: "1",
		EventID:       n.ID,
		Type:          n.Type,
		RoomID:        n.RoomID,
		RoomVersion:   n.RoomVersion,
		ServerEpoch:   n.ServerEpoch,
		ServerTime:    rfc3339(n.ServerTime),
		Cursor:        n.Cursor,
	}
	var payload struct {
		Resource     string `json:"resource"`
		RoundID      string `json:"roundId"`
		PhaseVersion *int64 `json:"phaseVersion"`
		Reason       string `json:"reason"`
		Phase        string `json:"phase"`
	}
	if len(n.Payload) > 0 {
		if err := jsonUnmarshal(n.Payload, &payload); err == nil {
			dto.Resource = payload.Resource
			dto.RoundID = payload.RoundID
			dto.PhaseVersion = payload.PhaseVersion
			dto.Reason = payload.Reason
		}
	}
	return dto
}

// handleStreamRoomEvents subscribes over Server-Sent Events.
//
// SSE is the first degradation step below WebSocket . The handler:
// - authorises the subscription on every connection, so a revoked role stops
// receiving immediately;
// - writes only the notification envelope, never a full room snapshot;
// - sends a comment heartbeat so a proxy does not idle the connection out;
// - stops when the client disconnects.
//
// It deliberately does not hold a read transaction open: the polled query runs per
// tick, so a slow client cannot pin a database reader .
func (s *Server) handleStreamRoomEvents(w http.ResponseWriter, r *http.Request) {
	// Push first. The polling loop below stays as the documented fallback for a
	// server built without a hub (test harnesses), but a deployment always has one:
	// polling costs a query per connection per tick and adds up to 150ms of latency
	// to every notification, including a revocation.
	if s.streamHandler != nil {
		s.streamHandler.ServeSSE(w, r)
		return
	}
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
	flusher, ok := w.(http.Flusher)
	if !ok {
		s.writeError(w, r, problem.Internal(fmt.Errorf("streaming unsupported by the connection")))
		return
	}
	actor, err := s.rooms.LoadActor(r.Context(), sess.ID, roomID)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	// Origin is validated on the handshake just like a WebSocket upgrade .
	if err := s.sessions.OriginAllowedForStream(r); err != nil {
		s.writeError(w, r, err)
		return
	}

	h := w.Header()
	h.Set("Content-Type", "text/event-stream")
	h.Set("Cache-Control", "no-store")
	h.Set("Connection", "keep-alive")
	h.Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	cursor := r.URL.Query().Get("cursor")
	// hello carries the connection parameters the client needs .
	s.writeSSE(w, flusher, "hello", map[string]any{
		"schemaVersion":       "1",
		"type":                "hello",
		"roomId":              roomID,
		"serverEpoch":         s.rooms.Epoch(),
		"serverTime":          rfc3339(nowMS()),
		"cursor":              cursor,
		"heartbeatIntervalMs": 15000,
	})

	// A ticker drives both the notification poll and the keepalive comment. The
	// interval is well below the 15s SSE keepalive requirement while still batching
	// state notifications inside the 100-200ms merge window .
	ticker := time.NewTicker(150 * time.Millisecond)
	defer ticker.Stop()
	heartbeat := time.NewTicker(15 * time.Second)
	defer heartbeat.Stop()

	for {
		select {
		case <-r.Context().Done():
			return
		case <-heartbeat.C:
			// A comment line keeps intermediaries from closing an idle stream.
			fmt.Fprint(w, ": keepalive\n\n")
			flusher.Flush()
		case <-ticker.C:
			// Re-authorise on every poll: a removed member must stop receiving.
			if _, aerr := s.rooms.LoadActor(r.Context(), sess.ID, roomID); aerr != nil {
				s.writeSSE(w, flusher, "session.revoked", map[string]any{
					"schemaVersion": "1", "type": "session.revoked", "roomId": roomID,
					"serverEpoch": s.rooms.Epoch(), "serverTime": rfc3339(nowMS()),
					"reason": "authorization-changed", "cursor": cursor,
				})
				return
			}
			items, next, resync, lerr := s.rooms.ListRoomEvents(r.Context(), actor, cursor, 20)
			if lerr != nil {
				s.writeSSE(w, flusher, "resync.required", map[string]any{
					"schemaVersion": "1", "type": "resync.required", "roomId": roomID,
					"serverEpoch": s.rooms.Epoch(), "serverTime": rfc3339(nowMS()),
					"reason": "stream-error", "cursor": cursor,
				})
				return
			}
			if resync {
				s.writeSSE(w, flusher, "resync.required", map[string]any{
					"schemaVersion": "1", "type": "resync.required", "roomId": roomID,
					"serverEpoch": s.rooms.Epoch(), "serverTime": rfc3339(nowMS()),
					"reason": "cursor-unusable", "cursor": cursor,
				})
				return
			}
			for _, n := range items {
				s.writeSSE(w, flusher, n.Type, toNotificationDTO(n))
			}
			cursor = next
		}
	}
}

// writeSSE emits one event frame.
func (s *Server) writeSSE(w http.ResponseWriter, flusher http.Flusher, event string, payload any) {
	body, err := json.Marshal(payload)
	if err != nil {
		return
	}
	fmt.Fprintf(w, "event: %s\n", event)
	// The id field is the opaque scope cursor, so a reconnect can resume.
	if id := sseID(payload); id != "" {
		fmt.Fprintf(w, "id: %s\n", id)
	}
	fmt.Fprintf(w, "data: %s\n\n", body)
	flusher.Flush()
}

// sseID extracts the cursor from a notification payload.
func sseID(payload any) string {
	switch v := payload.(type) {
	case eventNotificationDTO:
		return v.Cursor
	case map[string]any:
		if c, ok := v["cursor"].(string); ok {
			return c
		}
	}
	return ""
}

// handleUpgradeRoomSocket delegates to the configured realtime transport.
// Without that transport it validates session, membership and Origin before returning 501; SSE remains available.
func (s *Server) handleUpgradeRoomSocket(w http.ResponseWriter, r *http.Request) {
	// Same order as SSE: the push transport owns the upgrade when a hub exists.
	if s.streamHandler != nil {
		s.streamHandler.ServeWebSocket(w, r)
		return
	}
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
	// Authorisation and Origin are still validated, so an unauthorised caller gets
	// 401/403 rather than a misleading 501.
	if _, err := s.rooms.LoadActor(r.Context(), sess.ID, roomID); err != nil {
		s.writeError(w, r, err)
		return
	}
	if err := s.sessions.OriginAllowedForStream(r); err != nil {
		s.writeError(w, r, err)
		return
	}
	s.writeError(w, r, problem.NotImplemented("websocket-unavailable",
		"the WebSocket transport is not enabled on this server; use the SSE stream endpoint"))
}

// ---- health --------------------------------------------------------------

// handleLiveness proves only that the process is running.
//
// It must not touch the database: a busy or migrating database must not cause an
// orchestrator to restart a healthy process .
func (s *Server) handleLiveness(w http.ResponseWriter, r *http.Request) {
	s.writeJSON(w, r, http.StatusOK, healthDTO{
		Status:       "ok",
		BuildVersion: s.cfg.BuildVersion,
	})
}

// handleReadiness checks migrations, content and the database.
func (s *Server) handleReadiness(w http.ResponseWriter, r *http.Request) {
	ready := true
	if s.readyFn != nil {
		ok, failed := s.readyFn(r.Context())
		ready = ok
		if !ok {
			// The failing probe names are logged but never returned: a public
			// readiness body must not disclose infrastructure detail .
			s.log.Warn("readiness check failed", "requestId", requestIDOf(r), "probes", strings.Join(failed, ","))
		}
	}
	if !ready {
		s.writeJSON(w, r, http.StatusServiceUnavailable, healthDTO{
			Status:       "not-ready",
			BuildVersion: s.cfg.BuildVersion,
		})
		return
	}
	s.writeJSON(w, r, http.StatusOK, healthDTO{
		Status:       "ok",
		BuildVersion: s.cfg.BuildVersion,
	})
}

// handleMetrics renders Prometheus text for loopback or trusted callers.
func (s *Server) handleMetrics(w http.ResponseWriter, r *http.Request) {
	if !s.metricsAllowed(r) {
		s.writeError(w, r, problem.NotFound("metrics-not-available",
			"metrics are not available from this address"))
		return
	}
	var b strings.Builder
	s.metrics.Set("runtime_goroutines", float64(runtime.NumGoroutine()))
	if s.streamHandler != nil {
		connections, rooms, sessions := s.streamHandler.ConnectionStats()
		s.metrics.Set("realtime_connections", float64(connections))
		s.metrics.Set("realtime_connected_rooms", float64(rooms))
		s.metrics.Set("realtime_connected_sessions", float64(sessions))
	}
	s.metrics.WriteText(&b)
	h := w.Header()
	h.Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
	h.Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(b.String()))
}

// metricsInt renders an integer counter for headers and logs.
func metricsInt(v float64) string { return strconv.FormatInt(int64(v), 10) }
