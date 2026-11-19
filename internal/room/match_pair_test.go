package room_test

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"sync"
	"testing"

	"github.com/bhcn/umaguessinggame/internal/deployment"
	"github.com/bhcn/umaguessinggame/internal/httpapi/problem"
	"github.com/bhcn/umaguessinggame/internal/modes"
	"github.com/bhcn/umaguessinggame/internal/room"
	"github.com/bhcn/umaguessinggame/internal/storage"
)

type matchIndex struct{ practiceOnlyIndex }

func (matchIndex) ModeAdmission(id string) (room.ModeAdmission, bool) {
	return room.ModeAdmission{Mode: id, Available: true, QuestionCount: 100}, true
}
func matchingService(t *testing.T, policy deployment.Policy) (*room.Service, *storage.DB) {
	t.Helper()
	db := newDB(t)
	reg, _ := modes.NewRegistry(&sharedStub{id: "classic"}, &sharedStub{id: "sort"}, &sharedStub{id: "memory"})
	svc := room.New(db, room.Config{Policy: policy}, room.Options{Modes: reg, Content: matchIndex{}})
	t.Cleanup(svc.Close)
	for i := 0; i < 16; i++ {
		id := fmt.Sprintf("s%d", i)
		if _, err := db.Write().Exec(`INSERT INTO sessions(id,token_hash,display_name,csrf_token_hash,kind,created_at,expires_at) VALUES(?,?,?,?,'player',0,9999999999999)`, id, "h"+id, "同名训练员", "c"+id); err != nil {
			t.Fatal(err)
		}
	}
	return svc, db
}
func work(t *testing.T, svc *room.Service, sid, pool string) room.MatchProofAnswer {
	t.Helper()
	p, err := svc.CreateMatchProof(context.Background(), sid, pool)
	if err != nil {
		t.Fatal(err)
	}
	for n := 0; n < 1<<25; n++ {
		nonce := strconv.Itoa(n)
		if room.ValidMatchProof(p.ChallengeID, nonce, p.Bits) {
			return room.MatchProofAnswer{ChallengeID: p.ChallengeID, Nonce: nonce}
		}
	}
	t.Fatal("proof search exhausted")
	return room.MatchProofAnswer{}
}
func queueMatch(t *testing.T, svc *room.Service, sid, pool string) room.Ticket {
	t.Helper()
	v, err := svc.CreateMatchmakingTicket(context.Background(), sid, room.CreateTicketInput{PoolID: pool, Proof: work(t, svc, sid, pool)})
	if err != nil {
		t.Fatal(err)
	}
	return v
}
func TestMatchesStartAutomaticallyWithoutPlayerManagement(t *testing.T) {
	for _, pool := range []string{"classic-duel", "classic-four", "sort-four", "memory-four"} {
		t.Run(pool, func(t *testing.T) {
			svc, db := matchingService(t, deployment.Policy{})
			ctx := context.Background()
			n := 4
			if pool == "classic-duel" {
				n = 2
			}
			tickets := make([]room.Ticket, n)
			for i := range tickets {
				tickets[i] = queueMatch(t, svc, fmt.Sprintf("s%d", i), pool)
			}
			var done room.Ticket
			for i := n - 1; i >= 0; i-- {
				var err error
				done, err = svc.RespondToMatch(ctx, fmt.Sprintf("s%d", i), tickets[i].ID, true)
				if err != nil {
					t.Fatal(err)
				}
				if i > 0 && done.RoomID != "" {
					t.Fatal("room before all confirmations")
				}
			}
			if done.Status != "playing" || done.RoomID == "" {
				t.Fatalf("no playing room: %+v", done)
			}
			for i := range tickets {
				a, err := svc.LoadActor(ctx, fmt.Sprintf("s%d", i), done.RoomID)
				if err != nil {
					t.Fatal(err)
				}
				if !a.Has(room.CapPlay) || a.Has(room.CapManageRoom) || a.Has(room.CapHost) {
					t.Fatalf("unsafe actor: %+v", a)
				}
				if _, err = svc.CreateRound(ctx, a, room.RoundCreateInput{}); err == nil {
					t.Fatal("player can create a second round")
				}
				if _, err = svc.CreateTransition(ctx, a, "missing", room.TransitionInput{Target: "cancel"}); err == nil {
					t.Fatal("player can control phases")
				}
			}
			r, err := svc.CurrentRoundSummary(ctx, done.RoomID)
			if err != nil || r == nil {
				t.Fatalf("round: %v", err)
			}
			expected := modes.PhaseCollecting
			if pool == "memory-four" {
				expected = modes.PhaseBriefing
			}
			if r.Status != expected {
				t.Fatalf("phase %s", r.Status)
			}
			var code string
			if err = db.Read().QueryRow(`SELECT invite_code FROM rooms WHERE id=?`, done.RoomID).Scan(&code); err != nil {
				t.Fatal(err)
			}
			if _, err = svc.Join(ctx, "s15", done.RoomID, room.JoinInput{InviteCode: code, RequestedRole: "player"}); err == nil {
				t.Fatal("invite bypass")
			}
			active, err := svc.ActiveMatchmakingTicket(ctx, "s0")
			if err != nil || active == nil || active.RoomID != done.RoomID {
				t.Fatalf("restore: %v %+v", err, active)
			}
			if _, err = svc.CreateMatchmakingTicket(ctx, "s0", room.CreateTicketInput{PoolID: pool, Proof: work(t, svc, "s0", pool)}); problem.As(err).Code != "ticket-already-active" {
				t.Fatalf("second active: %v", err)
			}
			lobby, err := svc.MatchmakingLobby(ctx)
			if err != nil || lobby.Playing != n || lobby.Rooms != 1 || lobby.Queued != 0 {
				t.Fatalf("counts: %+v %v", lobby, err)
			}
			actor, _ := svc.LoadActor(ctx, "s0", done.RoomID)
			if err = svc.LeaveOrRemoveMembership(ctx, actor, actor.MembershipID()); err != nil {
				t.Fatal(err)
			}
			if active, err = svc.ActiveMatchmakingTicket(ctx, "s0"); err != nil || active != nil {
				t.Fatalf("departure still blocks: %+v %v", active, err)
			}
		})
	}
}
func TestFixedPoolsRejectCustomRulesAndDoNotMix(t *testing.T) {
	svc, _ := matchingService(t, deployment.Policy{})
	ctx := context.Background()
	_, err := svc.CreateMatchmakingTicket(ctx, "s0", room.CreateTicketInput{PoolID: "classic-duel", ModeConfig: json.RawMessage(`{"modeId":"classic","maxWrongGuesses":4}`)})
	if problem.As(err).Code != "matchmaking-rules-fixed" {
		t.Fatal(err)
	}
	a := queueMatch(t, svc, "s0", "classic-duel")
	b := queueMatch(t, svc, "s1", "classic-four")
	if a.Status != "queueing" || b.Status != "queueing" {
		t.Fatal("different pools paired")
	}
	if _, err = svc.RespondToMatch(ctx, "s0", a.ID, true); problem.As(err).Code != "ticket-not-pending" {
		t.Fatalf("unpaired accepted: %v", err)
	}
}
func TestMatchProofAlwaysRequiredBoundAndConsumed(t *testing.T) {
	svc, db := matchingService(t, deployment.Policy{})
	ctx := context.Background()
	proof := work(t, svc, "s0", "classic-duel")
	for _, in := range []room.CreateTicketInput{{PoolID: "classic-duel"}, {PoolID: "classic-four", Proof: proof}} {
		if _, err := svc.CreateMatchmakingTicket(ctx, "s0", in); err == nil {
			t.Fatal("missing or foreign-pool proof accepted")
		}
	}
	if _, err := svc.CreateMatchmakingTicket(ctx, "s1", room.CreateTicketInput{PoolID: "classic-duel", Proof: proof}); err == nil {
		t.Fatal("foreign session proof accepted")
	}
	in := room.CreateTicketInput{PoolID: "classic-duel", Proof: proof, Command: &room.CreationCommand{Key: "match-create-0001", Payload: []byte(`{"poolId":"classic-duel"}`)}}
	a, err := svc.CreateMatchmakingTicket(ctx, "s0", in)
	if err != nil {
		t.Fatal(err)
	}
	// The ticket persists its creation identity even if the HTTP receipt is lost.
	b, err := svc.CreateMatchmakingTicket(ctx, "s0", in)
	if err != nil || a.ID != b.ID {
		t.Fatalf("replay %v %+v", err, b)
	}
	changed := in
	changed.Command = &room.CreationCommand{Key: in.Command.Key, Payload: []byte(`{"poolId":"classic-four"}`)}
	if _, err = svc.CreateMatchmakingTicket(ctx, "s0", changed); problem.As(err).Code != "idempotency-conflict" {
		t.Fatal(err)
	}
	if _, err = db.Write().Exec(`UPDATE matchmaking_tickets SET status='finished' WHERE id=?`, a.ID); err != nil {
		t.Fatal(err)
	}
	in.Command = nil
	if _, err = svc.CreateMatchmakingTicket(ctx, "s0", in); problem.As(err).Code != "matchmaking-proof-required" {
		t.Fatalf("proof reused %v", err)
	}
	p := work(t, svc, "s2", "classic-duel")
	if _, err = db.Write().Exec(`UPDATE matchmaking_proofs SET expires_at=0 WHERE id=?`, p.ChallengeID); err != nil {
		t.Fatal(err)
	}
	if _, err = svc.CreateMatchmakingTicket(ctx, "s2", room.CreateTicketInput{PoolID: "classic-duel", Proof: p}); err == nil {
		t.Fatal("expired proof accepted")
	}
}
func TestConcurrentConfirmationsCreateOneRoom(t *testing.T) {
	svc, db := matchingService(t, deployment.Policy{})
	ctx := context.Background()
	ids := make([]room.Ticket, 4)
	for i := range ids {
		ids[i] = queueMatch(t, svc, fmt.Sprintf("s%d", i), "classic-four")
	}
	var wg sync.WaitGroup
	errs := make(chan error, 4)
	for i := range ids {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, e := svc.RespondToMatch(ctx, fmt.Sprintf("s%d", i), ids[i].ID, true)
			errs <- e
		}(i)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	var rooms, rounds int
	_ = db.Read().QueryRow(`SELECT COUNT(*) FROM rooms`).Scan(&rooms)
	_ = db.Read().QueryRow(`SELECT COUNT(*) FROM rounds`).Scan(&rounds)
	if rooms != 1 || rounds != 1 {
		t.Fatalf("rooms %d rounds %d", rooms, rounds)
	}
}
func TestMatchCapacityAndLobbySwitchAreIndependent(t *testing.T) {
	off := false
	svc, _ := matchingService(t, deployment.Policy{Matchmaking: deployment.Matchmaking{LobbyEnabled: &off, QueueCapacity: 1, ProofBits: 12}})
	if _, err := svc.MatchmakingLobby(context.Background()); problem.As(err).Code != "matchmaking-lobby-disabled" {
		t.Fatal(err)
	}
	queueMatch(t, svc, "s0", "classic-duel")
	if _, err := svc.CreateMatchmakingTicket(context.Background(), "s1", room.CreateTicketInput{PoolID: "classic-duel", Proof: work(t, svc, "s1", "classic-duel")}); problem.As(err).Status != 503 {
		t.Fatalf("capacity %v", err)
	}
}
func TestMatchPreparationRecovery(t *testing.T) {
	svc, db := matchingService(t, deployment.Policy{})
	ctx := context.Background()
	a := queueMatch(t, svc, "s0", "classic-duel")
	b := queueMatch(t, svc, "s1", "classic-duel")
	_, _ = svc.RespondToMatch(ctx, "s0", a.ID, true)
	done, err := svc.RespondToMatch(ctx, "s1", b.ID, true)
	if err != nil {
		t.Fatal(err)
	}
	// Simulate restarting between a prepared round and its first transition.
	if _, err = db.Write().Exec(`UPDATE rounds SET status='prepared',phase_version=1,deadline_at=NULL WHERE room_id=?`, done.RoomID); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Write().Exec(`UPDATE matchmaking_tickets SET status='ready' WHERE room_id=?`, done.RoomID); err != nil {
		t.Fatal(err)
	}
	reg, _ := modes.NewRegistry(&sharedStub{id: "classic"})
	next := room.New(db, room.Config{}, room.Options{Modes: reg, Content: matchIndex{}})
	defer next.Close()
	got, err := next.GetMatchmakingTicket(ctx, "s0", a.ID)
	if err != nil || got.Status != "playing" {
		t.Fatalf("recovery %+v %v", got, err)
	}
	var n int
	_ = db.Read().QueryRow(`SELECT COUNT(*) FROM rounds`).Scan(&n)
	if n != 1 {
		t.Fatal("recovery created another round")
	}
	// Natural terminal state frees the session without waiting for ticket expiry.
	if _, err = db.Write().Exec(`UPDATE rounds SET status='settled' WHERE room_id=?`, done.RoomID); err != nil {
		t.Fatal(err)
	}
	if active, err := next.ActiveMatchmakingTicket(ctx, "s0"); err != nil || active != nil {
		t.Fatalf("ended game blocks queue %+v %v", active, err)
	}
}

func TestFourPlayerCancelReleasesEveryReadyPartner(t *testing.T) {
	svc, _ := matchingService(t, deployment.Policy{})
	ctx := context.Background()
	tickets := make([]room.Ticket, 4)
	for i := range tickets {
		tickets[i] = queueMatch(t, svc, fmt.Sprintf("s%d", i), "classic-four")
	}
	for i := 0; i < 3; i++ {
		if _, err := svc.RespondToMatch(ctx, fmt.Sprintf("s%d", i), tickets[i].ID, true); err != nil {
			t.Fatal(err)
		}
	}
	if err := svc.CancelMatchmakingTicket(ctx, "s3", tickets[3].ID); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 3; i++ {
		got, err := svc.GetMatchmakingTicket(ctx, fmt.Sprintf("s%d", i), tickets[i].ID)
		if err != nil || got.Status != "queueing" || got.Accepted != 0 {
			t.Fatalf("stranded member %+v %v", got, err)
		}
	}
}

func TestFailedMatchPreparationIsBounded(t *testing.T) {
	svc, db := matchingService(t, deployment.Policy{})
	ctx := context.Background()
	a := queueMatch(t, svc, "s0", "classic-duel")
	b := queueMatch(t, svc, "s1", "classic-duel")
	if _, err := svc.RespondToMatch(ctx, "s0", a.ID, true); err != nil {
		t.Fatal(err)
	}
	done, err := svc.RespondToMatch(ctx, "s1", b.ID, true)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.Write().Exec(`UPDATE matchmaking_tickets SET status='ready',updated_at=0 WHERE room_id=?`, done.RoomID); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Write().Exec(`UPDATE rounds SET status='prepared',deadline_at=NULL WHERE room_id=?`, done.RoomID); err != nil {
		t.Fatal(err)
	}
	if err = svc.SweepMatchmaking(ctx); err != nil {
		t.Fatal(err)
	}
	var open int
	if err = db.Read().QueryRow(`SELECT COUNT(*) FROM matchmaking_tickets WHERE room_id=? AND status IN ('ready','playing')`, done.RoomID).Scan(&open); err != nil {
		t.Fatal(err)
	}
	if open != 0 {
		t.Fatal("preparation retained queue slots")
	}
	var phase string
	if err = db.Read().QueryRow(`SELECT status FROM rounds WHERE room_id=?`, done.RoomID).Scan(&phase); err != nil {
		t.Fatal(err)
	}
	if phase != "cancelled" {
		t.Fatalf("stuck preparation: %s", phase)
	}
}
