package room_test

import (
	"context"
	"github.com/bhcn/umaguessinggame/internal/deployment"
	"github.com/bhcn/umaguessinggame/internal/httpapi/problem"
	"github.com/bhcn/umaguessinggame/internal/room"
	"testing"
	"time"
)

func TestDeclineOrCancelReleasesAcceptedPartner(t *testing.T) {
	for _, cancel := range []bool{false, true} {
		t.Run(map[bool]string{false: "decline", true: "cancel"}[cancel], func(t *testing.T) {
			svc, _ := matchingService(t, deployment.Policy{})
			ctx := context.Background()
			a := queueMatch(t, svc, "s0", "classic-duel")
			b := queueMatch(t, svc, "s1", "classic-duel")
			if _, err := svc.RespondToMatch(ctx, "s0", a.ID, true); err != nil {
				t.Fatal(err)
			}
			var err error
			if cancel {
				err = svc.CancelMatchmakingTicket(ctx, "s1", b.ID)
			} else {
				_, err = svc.RespondToMatch(ctx, "s1", b.ID, false)
			}
			if err != nil {
				t.Fatal(err)
			}
			got, err := svc.GetMatchmakingTicket(ctx, "s0", a.ID)
			if err != nil || got.Status != "queueing" {
				t.Fatalf("stranded accepted player %+v %v", got, err)
			}
			third := queueMatch(t, svc, "s2", "classic-duel")
			if third.Status != "matched-pending-accept" {
				t.Fatal("released player not matchable")
			}
			got, _ = svc.GetMatchmakingTicket(ctx, "s0", a.ID)
			if got.Status != "matched-pending-accept" || got.Accepted != 0 {
				t.Fatal("new group inherited old acceptance")
			}
		})
	}
}
func TestStaleTicketsAreRetiredAndHeartbeatCannotReviveThem(t *testing.T) {
	svc, db := matchingService(t, deployment.Policy{})
	ctx := context.Background()
	a := queueMatch(t, svc, "s0", "classic-duel")
	if _, err := db.Write().Exec(`UPDATE matchmaking_tickets SET lease_until=0 WHERE id=?`, a.ID); err != nil {
		t.Fatal(err)
	}
	got, err := svc.HeartbeatMatchmakingTicket(ctx, "s0", a.ID)
	if err != nil || got.Status != "expired" {
		t.Fatalf("lease revived %+v %v", got, err)
	}
	if next := queueMatch(t, svc, "s0", "classic-duel"); next.ID == a.ID {
		t.Fatal("old ID reused")
	}
}
func TestConfirmationExpiryReleasesGroupAndCannotBeAccepted(t *testing.T) {
	svc, db := matchingService(t, deployment.Policy{})
	ctx := context.Background()
	a := queueMatch(t, svc, "s0", "classic-duel")
	b := queueMatch(t, svc, "s1", "classic-duel")
	_, _ = svc.RespondToMatch(ctx, "s0", a.ID, true)
	if _, err := db.Write().Exec(`UPDATE matchmaking_tickets SET expires_at=0 WHERE id=?`, b.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.RespondToMatch(ctx, "s1", b.ID, true); problem.As(err).Code != "ticket-expired" {
		t.Fatalf("expired accepted %v", err)
	}
	got, err := svc.GetMatchmakingTicket(ctx, "s0", a.ID)
	if err != nil || got.Status != "queueing" || got.RoomID != "" {
		t.Fatalf("group not released %+v %v", got, err)
	}
}
func TestHeartbeatDoesNotExtendConfirmationDeadlineAndCancellationCoolsDown(t *testing.T) {
	svc, _ := matchingService(t, deployment.Policy{})
	ctx := context.Background()
	now := time.Now()
	svc.SetClock(func() time.Time { return now })
	a := queueMatch(t, svc, "s0", "classic-duel")
	queueMatch(t, svc, "s1", "classic-duel")
	before, _ := svc.GetMatchmakingTicket(ctx, "s0", a.ID)
	now = now.Add(10 * time.Second)
	after, err := svc.HeartbeatMatchmakingTicket(ctx, "s0", a.ID)
	if err != nil || after.ExpiresAt != before.ExpiresAt {
		t.Fatalf("deadline extended %+v %v", after, err)
	}
	if err = svc.CancelMatchmakingTicket(ctx, "s0", a.ID); err != nil {
		t.Fatal(err)
	}
	proof := work(t, svc, "s0", "classic-duel")
	if _, err = svc.CreateMatchmakingTicket(ctx, "s0", room.CreateTicketInput{PoolID: "classic-duel", Proof: proof}); problem.As(err).Status != 429 {
		t.Fatalf("no cooldown %v", err)
	}
	now = now.Add(16 * time.Second)
	if _, err = svc.CreateMatchmakingTicket(ctx, "s0", room.CreateTicketInput{PoolID: "classic-duel", Proof: proof}); err != nil {
		t.Fatal(err)
	}
}
func TestTicketOwnershipStillHidesOtherSessions(t *testing.T) {
	svc, _ := matchingService(t, deployment.Policy{})
	a := queueMatch(t, svc, "s0", "classic-duel")
	ctx := context.Background()
	if _, err := svc.GetMatchmakingTicket(ctx, "s1", a.ID); problem.As(err).Status != 404 {
		t.Fatal(err)
	}
	if _, err := svc.HeartbeatMatchmakingTicket(ctx, "s1", a.ID); problem.As(err).Status != 404 {
		t.Fatal(err)
	}
	if err := svc.CancelMatchmakingTicket(ctx, "s1", a.ID); problem.As(err).Status != 404 {
		t.Fatal(err)
	}
}
