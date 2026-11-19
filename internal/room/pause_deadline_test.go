package room_test

import (
	"context"
	"testing"

	"github.com/bhcn/umaguessinggame/internal/httpapi/problem"
)

func TestConsentPauseCannotReopenExpiredWindow(t *testing.T) {
	ctx := context.Background()
	for _, atResponse := range []bool{false, true} {
		t.Run(map[bool]string{false: "request", true: "accept"}[atResponse], func(t *testing.T) {
			f := newTeamRound(t, "memory")
			if atResponse {
				request, err := f.svc.CreatePauseRequest(ctx, f.actor("a1"), f.roundID, 1)
				if err != nil {
					t.Fatal(err)
				}
				f.exec(t, `UPDATE rounds SET deadline_at=1 WHERE id=?`, f.roundID)
				_, err = f.svc.RespondToPause(ctx, f.actor("b1"), f.roundID, request.ID, true)
				if problem.As(err).Code != "deadline-passed" {
					t.Fatalf("late accept: %v", err)
				}
			} else {
				f.exec(t, `UPDATE rounds SET deadline_at=1 WHERE id=?`, f.roundID)
				_, err := f.svc.CreatePauseRequest(ctx, f.actor("a1"), f.roundID, 1)
				if problem.As(err).Code != "deadline-passed" {
					t.Fatalf("late request: %v", err)
				}
			}
			var paused any
			if err := f.db.Read().QueryRow(`SELECT paused_at FROM rounds WHERE id=?`, f.roundID).Scan(&paused); err != nil {
				t.Fatal(err)
			}
			if paused != nil {
				t.Fatal("expired clock was paused")
			}
		})
	}
}
