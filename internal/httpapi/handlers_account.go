package httpapi

import (
	"net/http"
	"strings"

	"github.com/bhcn/umaguessinggame/internal/account"
	"github.com/bhcn/umaguessinggame/internal/httpapi/problem"
	"github.com/bhcn/umaguessinggame/internal/session"
)

func (s *Server) handleGetAccount(w http.ResponseWriter, r *http.Request) {
	st, err := s.accounts.Current(r.Context(), r)
	if err != nil {
		s.writeError(w, r, problem.Internal(nil))
		return
	}
	s.writeJSON(w, r, http.StatusOK, st)
}
func (s *Server) accountWrite(w http.ResponseWriter, r *http.Request) (account.Status, bool) {
	if !s.accounts.Enabled() {
		s.writeError(w, r, problem.NotFound("account-disabled", "账户登录暂未开放"))
		return account.Status{}, false
	}
	if err := s.sessions.OriginAllowed(r); err != nil {
		s.writeError(w, r, err)
		return account.Status{}, false
	}
	st, err := s.accounts.Current(r.Context(), r)
	if err != nil {
		s.writeError(w, r, problem.Internal(nil))
		return st, false
	}
	if !st.Authenticated {
		s.writeError(w, r, account.ErrRequired)
		return st, false
	}
	if !s.accounts.VerifyCSRF(r, st) {
		s.writeError(w, r, problem.Forbidden("account-csrf-invalid", "请刷新页面后重试"))
		return st, false
	}
	return st, true
}
func (s *Server) handleRenewAccount(w http.ResponseWriter, r *http.Request) {
	st, ok := s.accountWrite(w, r)
	if !ok {
		return
	}
	if err := s.accounts.Renew(r.Context(), w, r, st); err != nil {
		s.writeError(w, r, problem.Internal(nil))
		return
	}
	s.handleGetAccount(w, r)
}
func (s *Server) handleDeleteAccount(w http.ResponseWriter, r *http.Request) {
	st, ok := s.accountWrite(w, r)
	if !ok {
		return
	}
	if err := s.clearGameIdentity(w, r); err != nil {
		s.writeError(w, r, err)
		return
	}
	if err := s.accounts.Logout(r.Context(), w, st); err != nil {
		s.writeError(w, r, problem.Internal(nil))
		return
	}
	s.writeNoContent(w, r)
}
func (s *Server) clearGameIdentity(w http.ResponseWriter, r *http.Request) error {
	sess, err := s.sessions.Lookup(r.Context(), s.sessions.CookieToken(r))
	if err != nil {
		if p, ok := err.(*problem.Error); ok && p.Status == 401 {
			s.sessions.ClearCookie(w)
			return nil
		}
		return err
	}
	if sess.Kind != session.KindPlayer {
		return nil
	}
	if s.rooms != nil {
		ticket, err := s.rooms.ActiveMatchmakingTicket(r.Context(), sess.ID)
		if err != nil {
			return err
		}
		if ticket != nil && ticket.ID != "" && ticket.RoomID == "" {
			if err = s.rooms.CancelMatchmakingTicket(r.Context(), sess.ID, ticket.ID); err != nil {
				return err
			}
		}
		if err = s.rooms.ReleaseAccountSession(r.Context(), sess.ID); err != nil {
			return err
		}
	}
	if err = s.sessions.Revoke(r.Context(), sess); err != nil {
		return err
	}
	if s.realtimeHub != nil {
		s.realtimeHub.RevokeSession(sess.ID, "authorization-changed")
	}
	s.sessions.ClearCookie(w)
	return nil
}
func (s *Server) handleStartAccount(w http.ResponseWriter, r *http.Request) {
	if !s.accounts.Enabled() {
		s.writeError(w, r, problem.NotFound("account-disabled", "账户登录暂未开放"))
		return
	}
	// The SPA uses POST so third-party sites cannot silently initiate a login.
	if err := s.sessions.OriginAllowed(r); err != nil {
		s.writeError(w, r, err)
		return
	}
	var body struct {
		ReturnTo string `json:"returnTo"`
	}
	if err := decodeBody(w, r, &body, 1024); err != nil {
		s.writeError(w, r, err)
		return
	}
	target, err := s.accounts.Start(w, r, r.PathValue("providerId"), body.ReturnTo)
	if err != nil {
		s.writeError(w, r, problem.BadRequest("login-unavailable", "这个登录方式暂时用不了，换一个试试吧"))
		return
	}
	s.writeJSON(w, r, http.StatusOK, struct {
		AuthorizationURL string `json:"authorizationUrl"`
	}{target})
}
func (s *Server) handleAccountCallback(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Referrer-Policy", "no-referrer")
	target, err := s.accounts.Callback(r.Context(), w, r, r.PathValue("providerId"), func() error {
		if err := s.clearGameIdentity(w, r); err != nil {
			return err
		}
		previous, err := s.accounts.Current(r.Context(), r)
		if err != nil {
			return err
		}
		if previous.Authenticated {
			return s.accounts.Logout(r.Context(), w, previous)
		}
		return nil
	})
	// No upstream error text, code, state or token reaches the SPA URL or logs.
	http.Redirect(w, r, account.ResultURL(target, err == nil), http.StatusSeeOther)
}
func (s *Server) requireAccount(w http.ResponseWriter, r *http.Request, form, sid string) bool {
	if _, err := s.accounts.Require(r.Context(), r, form, sid); err != nil {
		if _, ok := err.(*problem.Error); !ok {
			err = problem.Internal(nil)
		}
		s.writeError(w, r, err)
		return false
	}
	return true
}
func (s *Server) checkAccountAccess(w http.ResponseWriter, r *http.Request, operation string) bool {
	if !s.accounts.Enabled() {
		return true
	}
	sess, ok := sessionOf(r)
	if !ok {
		return true
	}
	if sess.Kind == session.KindDisplay {
		return true
	} // Explicitly paired projection remains read-only.
	form := ""
	if strings.Contains(operation, "Matchmaking") || operation == "respondToMatch" {
		form = "matchmaking"
	}
	if operation == "createEvent" {
		form = "event"
	}
	if form == "" && s.rooms != nil {
		for _, kind := range []string{"room", "round", "event", "membership"} {
			if id := r.PathValue(kind + "Id"); id != "" {
				var err error
				form, err = s.rooms.AccessForm(r.Context(), kind, id)
				if err != nil {
					s.writeError(w, r, problem.Internal(nil))
					return false
				}
				break
			}
		}
	}
	return s.requireAccount(w, r, form, sess.ID)
}
