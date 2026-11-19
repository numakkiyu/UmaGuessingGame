package e2e

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/bhcn/umaguessinggame/internal/httpapi/problem"
	"github.com/bhcn/umaguessinggame/internal/modes"
	"github.com/bhcn/umaguessinggame/internal/room"
	"github.com/bhcn/umaguessinggame/internal/storage"
	"github.com/bhcn/umaguessinggame/internal/wiring"
)

// All registered modes pass through the same room authority checks, including
// those whose participant-state structs do not contain a surrender field.
func TestAllModesEnforceRoundBoundaries(t *testing.T) {
	ctx := context.Background()
	content, err := wiring.OpenContent(ctx, filepath.Join(repoRoot(t), "content"))
	if err != nil {
		t.Fatal(err)
	}
	registered, err := wiring.BuildModes()
	if err != nil {
		t.Fatal(err)
	}
	for _, mode := range registered.Registry.All() {
		t.Run(mode.ID(), func(t *testing.T) {
			db, err := storage.Open(ctx, storage.Config{Dir: t.TempDir()})
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			svc := room.New(db, room.Config{}, room.Options{Modes: registered.Registry, Content: content})
			defer svc.Close()
			clock := time.Now().UTC()
			svc.SetClock(func() time.Time { return clock })
			n := mode.MinSubjects()
			if mode.ID() == "strategy-race" {
				n = 2
			}
			cfg, _ := json.Marshal(map[string]any{"modeId": mode.ID()})
			var rid, invite string
			for i := 0; i < n; i++ {
				sid := fmt.Sprintf("boundary-%d", i)
				if _, err := db.Write().Exec(`INSERT INTO sessions(id,token_hash,display_name,csrf_token_hash,kind,created_at,expires_at) VALUES(?,?,?,?,'player',0,9999999999999)`, sid, "h"+sid, sid, "c"+sid); err != nil {
					t.Fatal(err)
				}
				if i == 0 {
					made, err := svc.CreateRoom(ctx, sid, room.RoomCreateInput{Name: "规则检查", Kind: "friend", MaxPlayers: max(n, 2), Visibility: "invite", ModeConfig: cfg})
					if err != nil {
						t.Fatal(err)
					}
					rid, invite = made.Room.ID, made.InviteCode
				} else if _, err := svc.Join(ctx, sid, rid, room.JoinInput{InviteCode: invite, RequestedRole: "player"}); err != nil {
					t.Fatal(err)
				}
			}
			actor, err := svc.LoadActor(ctx, "boundary-0", rid)
			if err != nil {
				t.Fatal(err)
			}
			if mode.ID() == "clue-puzzle" {
				team, err := svc.CreateTeam(ctx, actor, "一起解谜", "green")
				if err != nil {
					t.Fatal(err)
				}
				if _, err := db.Write().Exec(`UPDATE memberships SET team_id=? WHERE room_id=?`, team.ID, rid); err != nil {
					t.Fatal(err)
				}
				if _, err := db.Write().Exec(`UPDATE memberships SET role='team-captain' WHERE room_id=? AND session_id=?`, rid, "boundary-0"); err != nil {
					t.Fatal(err)
				}
				actor, err = svc.LoadActor(ctx, "boundary-0", rid)
				if err != nil {
					t.Fatal(err)
				}
			}
			round, err := svc.CreateRound(ctx, actor, room.RoundCreateInput{})
			if admission, ok := content.ModeAdmission(mode.ID()); !ok || !admission.Available {
				if err == nil {
					t.Fatal("unavailable mode unexpectedly prepared a round")
				}
				t.Skipf("current content pack cannot start %s; mode fixture tests cover its rules", mode.ID())
			}
			if err != nil {
				t.Fatal(err)
			}
			rm, _, err := svc.GetRoom(ctx, actor.SessionID, rid)
			if err != nil {
				t.Fatal(err)
			}
			for _, phase := range []string{"prepared", "locked", "revealed", "settled", "cancelled"} {
				t.Run(phase, func(t *testing.T) {
					if _, err := db.Write().Exec(`UPDATE rounds SET status=?,deadline_at=NULL WHERE id=?`, phase, round.ID); err != nil {
						t.Fatal(err)
					}
					view, err := svc.BuildRoundView(ctx, round.ID, rm, actor)
					if err != nil {
						t.Fatal(err)
					}
					if view.CanSubmit {
						t.Errorf("%s still permits submissions in %s", mode.ID(), phase)
					}
				})
			}
			if _, err := db.Write().Exec(`UPDATE rounds SET status='collecting',deadline_at=NULL,paused_at=1000,paused_remaining_ms=1000,paused_previous_phase='collecting' WHERE id=?`, round.ID); err != nil {
				t.Fatal(err)
			}
			view, err := svc.BuildRoundView(ctx, round.ID, rm, actor)
			if err != nil {
				t.Fatal(err)
			}
			if view.CanSubmit {
				t.Error("paused participant still has submission authority")
			}
			if _, err := db.Write().Exec(`UPDATE rounds SET paused_at=NULL,paused_remaining_ms=NULL,paused_previous_phase=NULL WHERE id=?`, round.ID); err != nil {
				t.Fatal(err)
			}
			if _, err := db.Write().Exec(`UPDATE rounds SET deadline_at=1 WHERE id=?`, round.ID); err != nil {
				t.Fatal(err)
			}
			view, err = svc.BuildRoundView(ctx, round.ID, rm, actor)
			if err != nil || view.CanSubmit {
				t.Fatalf("expired round remains actionable: %v", err)
			}
			if _, _, err := svc.CreateSurrender(ctx, actor, round.ID, round.PhaseVersion); problem.As(err).Code != "deadline-passed" {
				t.Fatalf("late surrender = %v", err)
			}
			if _, err := db.Write().Exec(`UPDATE rounds SET deadline_at=NULL WHERE id=?`, round.ID); err != nil {
				t.Fatal(err)
			}
			var proposal room.Trade
			var recipient room.Actor
			if mode.ID() == "trade" {
				if _, err := db.Write().Exec(`UPDATE rounds SET deadline_at=9999999999999 WHERE id=?`, round.ID); err != nil {
					t.Fatal(err)
				}
				recipient, err = svc.LoadActor(ctx, "boundary-1", rid)
				if err != nil {
					t.Fatal(err)
				}
				view, err = svc.BuildRoundView(ctx, round.ID, rm, actor)
				if err != nil {
					t.Fatal(err)
				}
				var board struct {
					Hands map[string][]string `json:"hands"`
				}
				if err := json.Unmarshal(view.PublicState, &board); err != nil {
					t.Fatal(err)
				}
				proposal, err = svc.CreateTrade(ctx, actor, round.ID, room.TradeInput{
					PhaseVersion: round.PhaseVersion, RecipientMembershipID: recipient.MembershipID(),
					OfferedInstanceID: board.Hands[actor.MembershipID()][0], RequestedInstanceID: board.Hands[recipient.MembershipID()][0],
				})
				if err != nil {
					t.Fatal(err)
				}
			}
			if _, _, err := svc.CreateSurrender(ctx, actor, round.ID, round.PhaseVersion); err != nil {
				t.Fatal(err)
			}
			if mode.ID() == "trade" {
				if _, err := svc.RespondToTrade(ctx, recipient, round.ID, proposal.ID, true, nil); problem.As(err).Code != "trade-closed" {
					t.Fatalf("accept surrendered sender's proposal: %v", err)
				}
				if _, err := svc.CreateTrade(ctx, recipient, round.ID, room.TradeInput{PhaseVersion: round.PhaseVersion, RecipientMembershipID: actor.MembershipID()}); problem.As(err).Code != "trade-closed" {
					t.Fatalf("offer to surrendered recipient: %v", err)
				}
				if _, err := svc.RespondToTrade(ctx, actor, round.ID, proposal.ID, true, nil); problem.As(err).Code != "participant-surrendered" {
					t.Fatalf("surrendered actor responded to proposal: %v", err)
				}
			}
			view, err = svc.BuildRoundView(ctx, round.ID, rm, actor)
			if err != nil {
				t.Fatal(err)
			}
			if view.CanSubmit {
				t.Error("surrendered participant still has submission authority")
			}
			if mode.ID() == "trade" {
				if _, err := db.Write().Exec(`UPDATE rounds SET deadline_at=9999999999999 WHERE id=?`, round.ID); err != nil {
					t.Fatal(err)
				}
				_, err = svc.CreateTrade(ctx, actor, round.ID, room.TradeInput{PhaseVersion: round.PhaseVersion})
			} else {
				kind := mode.(interface{ ContractKinds() []string }).ContractKinds()[0]
				_, err = svc.CreateSubmission(ctx, actor, round.ID, room.SubmissionInput{PhaseVersion: round.PhaseVersion, ModeAction: modes.ModeAction{Kind: kind, Payload: json.RawMessage(`{}`)}})
			}
			var failure *problem.Error
			if !errors.As(err, &failure) || failure.Code != "participant-surrendered" {
				t.Errorf("surrender guard = %v, want participant-surrendered before mode validation", err)
			}
			if _, err := db.Write().Exec(`UPDATE rounds SET deadline_at=? WHERE id=?`, clock.Add(time.Second).UnixMilli(), round.ID); err != nil {
				t.Fatal(err)
			}
			// Advance authoritative time without waiting in real time. Creative's
			// review gate is explicitly approved by the host, never bypassed.
			for tick := 0; tick < 32; tick++ {
				clock = clock.Add(time.Minute)
				if _, err := svc.SweepExpired(ctx); err != nil {
					t.Fatal(err)
				}
				current, err := svc.BuildRoundView(ctx, round.ID, rm, actor)
				if err != nil {
					t.Fatal(err)
				}
				if mode.ID() == "creative" && current.Round.Status == modes.PhaseLocked && current.Round.DeadlineAt == nil {
					if _, err := svc.CreateTransition(ctx, actor, round.ID, room.TransitionInput{Target: "creative-voting", ExpectedPhaseVersion: current.Round.PhaseVersion}); err != nil {
						t.Fatal(err)
					}
				}
				if current.Round.Status == modes.PhaseSettled {
					if current.CanSubmit || len(current.Results) == 0 {
						t.Fatal("settled round has no result or remains actionable")
					}
					found := false
					for _, result := range current.Results {
						if result.SubjectID == actor.MembershipID() || mode.ID() == "clue-puzzle" {
							found = true
							if result.ScoreUnits != 0 {
								t.Fatal("surrendered participant regained points")
							}
						}
					}
					if !found {
						t.Fatal("surrendered participant missing from settlement")
					}
					return
				}
			}
			t.Fatal("bounded timeout simulation did not reach settlement")
		})
	}
}
