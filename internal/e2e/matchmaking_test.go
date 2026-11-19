package e2e

import (
	"context"
	"errors"
	"fmt"
	"github.com/bhcn/umaguessinggame/internal/deployment"
	"github.com/bhcn/umaguessinggame/internal/room"
	"github.com/bhcn/umaguessinggame/internal/storage"
	"github.com/bhcn/umaguessinggame/internal/wiring"
	"path/filepath"
	"strconv"
	"testing"
	"time"
)

func TestPublishedMatchmakingPoolsStartRealGames(t *testing.T) {
	ctx := context.Background()
	src, err := wiring.OpenContent(ctx, filepath.Join(repoRoot(t), "content"))
	if err != nil {
		t.Fatal(err)
	}
	registered, err := wiring.BuildModes()
	if err != nil {
		t.Fatal(err)
	}
	for _, pool := range []string{"classic-duel", "classic-four", "sort-four", "memory-four"} {
		t.Run(pool, func(t *testing.T) {
			db, err := storage.Open(ctx, storage.Config{Dir: t.TempDir()})
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			svc := room.New(db, room.Config{Policy: deployment.Policy{Matchmaking: deployment.Matchmaking{ProofBits: 12}}}, room.Options{Modes: registered.Registry, Content: src})
			defer svc.Close()
			n := 4
			if pool == "classic-duel" {
				n = 2
			}
			tickets := make([]room.Ticket, n)
			for i := range tickets {
				sid := fmt.Sprintf("match-player-%d", i)
				_, err = db.Write().Exec(`INSERT INTO sessions(id,token_hash,display_name,csrf_token_hash,kind,created_at,expires_at) VALUES(?,?,?,?,'player',0,9999999999999)`, sid, "h"+sid, sid, "c"+sid)
				if err != nil {
					t.Fatal(err)
				}
				proof, err := svc.CreateMatchProof(ctx, sid, pool)
				if err != nil {
					t.Fatal(err)
				}
				nonce := ""
				for j := 0; j < 1<<24; j++ {
					s := strconv.Itoa(j)
					if room.ValidMatchProof(proof.ChallengeID, s, proof.Bits) {
						nonce = s
						break
					}
				}
				tickets[i], err = svc.CreateMatchmakingTicket(ctx, sid, room.CreateTicketInput{PoolID: pool, Proof: room.MatchProofAnswer{ChallengeID: proof.ChallengeID, Nonce: nonce}})
				if err != nil {
					t.Fatal(err)
				}
			}
			var done room.Ticket
			for i := range tickets {
				done, err = svc.RespondToMatch(ctx, fmt.Sprintf("match-player-%d", i), tickets[i].ID, true)
				if err != nil {
					t.Fatalf("accept: %v; cause: %v", err, errors.Unwrap(err))
				}
			}
			if done.Status != "playing" || done.RoomID == "" {
				t.Fatalf("not started: %+v", done)
			}
			active, err := svc.CurrentRoundSummary(ctx, done.RoomID)
			if err != nil || active == nil {
				t.Fatalf("no round %v", err)
			}
			if active.ModeID == "classic" && active.DeadlineAt == nil {
				t.Fatal("random classic must be timed")
			}
			for i := range tickets {
				actor, err := svc.LoadActor(ctx, fmt.Sprintf("match-player-%d", i), done.RoomID)
				if err != nil {
					t.Fatal(err)
				}
				rm, _, err := svc.GetRoom(ctx, actor.SessionID, done.RoomID)
				if err != nil {
					t.Fatal(err)
				}
				if _, err = svc.BuildRoundView(ctx, active.ID, rm, actor); err != nil {
					t.Fatalf("projection: %v", err)
				}
			}
			// Use the published mode's real answering window, excluding memory preview.
			if pool == "memory-four" {
				svc.SetClock(func() time.Time { return active.DeadlineAt.Add(time.Millisecond) })
				if _, err = svc.SweepExpired(ctx); err != nil {
					t.Fatal(err)
				}
				active, err = svc.CurrentRoundSummary(ctx, done.RoomID)
				if err != nil || active == nil {
					t.Fatal(err)
				}
			}
			duration := 90 * time.Second
			if active.ModeID == "classic" {
				duration = 180 * time.Second
			}
			start := active.DeadlineAt.Add(-duration)
			before := start.Add(59 * time.Second)
			svc.SetClock(func() time.Time { return before })
			if _, err = svc.SweepExpired(ctx); err != nil {
				t.Fatal(err)
			}
			var phase string
			if err = db.Read().QueryRow(`SELECT status FROM rounds WHERE id=?`, active.ID).Scan(&phase); err != nil {
				t.Fatal(err)
			}
			if phase != "collecting" {
				t.Fatalf("cancelled before answering grace: %s", phase)
			}
			after := start.Add(61 * time.Second)
			svc.SetClock(func() time.Time { return after })
			if _, err = svc.SweepExpired(ctx); err != nil {
				t.Fatal(err)
			}
			if err = db.Read().QueryRow(`SELECT status FROM rounds WHERE id=?`, active.ID).Scan(&phase); err != nil {
				t.Fatal(err)
			}
			if phase != "cancelled" {
				t.Fatalf("idle real pool did not cancel: %s", phase)
			}

		})
	}
}
