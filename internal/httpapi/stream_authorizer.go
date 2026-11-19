package httpapi

import (
	"context"
	"net/http"

	"github.com/bhcn/umaguessinggame/internal/realtime"
)

func (a streamAuthorizer) IdentityActive(ctx context.Context, identity realtime.Identity) (bool, error) {
	if identity.Role != "display" {
		form, err := a.srv.rooms.AccessForm(ctx, "room", identity.RoomID)
		if err != nil {
			return false, err
		}
		if a.srv.accounts.Required(form) {
			active, err := a.srv.accounts.GameActive(ctx, identity.SessionID)
			if err != nil || !active {
				return false, err
			}
		}
	}
	return a.srv.rooms.StreamIdentityActive(ctx, identity.RoomID, identity.MembershipID, identity.SessionID, identity.Role)
}

// streamAuthorizer adapts the session and room services to realtime.Authorizer.
//
// The realtime package deliberately does not import session or room: it owns the
// transport and the notification policy, while this layer owns cookie parsing and
// membership lookup. That seam is why the adapter lives here rather than there.
//
// Both methods answer "not authorised" as (zero, false, nil) rather than an error,
// because a missing credential is an ordinary answer, not a failure.
type streamAuthorizer struct {
	srv *Server
}

// AuthorizeWebSocket authorises a WebSocket handshake.
func (a streamAuthorizer) AuthorizeWebSocket(r *http.Request, roomID string) (realtime.Identity, bool, error) {
	return a.authorize(r, roomID)
}

// AuthorizeSSE authorises an SSE stream request.
func (a streamAuthorizer) AuthorizeSSE(r *http.Request, roomID string) (realtime.Identity, bool, error) {
	return a.authorize(r, roomID)
}

// authorize resolves the caller's membership and maps it onto an Identity.
//
// Authorisation is evaluated on every handshake, so a membership that was revoked
// while the client was away is refused here rather than served a stale stream. That
// is why a revoked client's reconnect is rejected instead of being sent a revocation
// frame it can no longer receive.
func (a streamAuthorizer) authorize(r *http.Request, roomID string) (realtime.Identity, bool, error) {
	if err := a.srv.sessions.OriginAllowedForStream(r); err != nil {
		return realtime.Identity{}, false, nil
	}
	sess, ok := sessionOf(r)
	if !ok {
		return realtime.Identity{}, false, nil
	}
	actor, err := a.srv.rooms.LoadActor(r.Context(), sess.ID, roomID)
	if err != nil {
		// A revoked or absent membership is a normal "no", not a server fault.
		return realtime.Identity{}, false, nil
	}
	if actor.Membership == nil {
		return realtime.Identity{}, false, nil
	}
	return realtime.Identity{
		RoomID:       roomID,
		MembershipID: actor.Membership.ID,
		SessionID:    sess.ID,
		Role:         string(actor.Membership.Role),
	}, true, nil
}
