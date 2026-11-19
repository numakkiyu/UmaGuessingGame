package e2e

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/bhcn/umaguessinggame/internal/deployment"
	"github.com/bhcn/umaguessinggame/internal/modes"
	"github.com/bhcn/umaguessinggame/internal/room"
	"github.com/bhcn/umaguessinggame/internal/storage"
	"github.com/bhcn/umaguessinggame/internal/wiring"
)

func TestClassicDuelKeepsOneSecretUntilBothAreDone(t *testing.T) {
	ctx := context.Background()
	content, err := wiring.OpenContent(ctx, filepath.Join(repoRoot(t), "content"))
	if err != nil {
		t.Fatal(err)
	}
	registered, err := wiring.BuildModes()
	if err != nil {
		t.Fatal(err)
	}
	for _, kind := range []string{"friend", "matchmaking"} {
		for _, ending := range []string{"opponent-wins", "both-exhausted", "deadline", "surrender-a", "surrender-b", "surrender-exhausted-opponent", "surrender-simultaneous"} {
			t.Run(kind+"/"+ending, func(t *testing.T) {
				db, err := storage.Open(ctx, storage.Config{Dir: t.TempDir()})
				if err != nil {
					t.Fatal(err)
				}
				defer db.Close()
				svc := room.New(db, room.Config{Policy: deployment.Policy{Matchmaking: deployment.Matchmaking{ProofBits: 12}}}, room.Options{Modes: registered.Registry, Content: content})
				defer svc.Close()
				for i := 0; i < 2; i++ {
					sid := fmt.Sprintf("duel-%d", i)
					if _, err = db.Write().Exec(`INSERT INTO sessions(id,token_hash,display_name,csrf_token_hash,kind,created_at,expires_at) VALUES(?,?,?,?,'player',0,9999999999999)`, sid, "h"+sid, sid, "c"+sid); err != nil {
						t.Fatal(err)
					}
				}
				rid := ""
				if kind == "matchmaking" {
					tickets := make([]room.Ticket, 2)
					for i := range tickets {
						sid := fmt.Sprintf("duel-%d", i)
						proof, err := svc.CreateMatchProof(ctx, sid, "classic-duel")
						if err != nil {
							t.Fatal(err)
						}
						nonce := ""
						for j := 0; j < 1<<24; j++ {
							nonce = strconv.Itoa(j)
							if room.ValidMatchProof(proof.ChallengeID, nonce, proof.Bits) {
								break
							}
						}
						tickets[i], err = svc.CreateMatchmakingTicket(ctx, sid, room.CreateTicketInput{PoolID: "classic-duel", Proof: room.MatchProofAnswer{ChallengeID: proof.ChallengeID, Nonce: nonce}})
						if err != nil {
							t.Fatal(err)
						}
					}
					for i, v := range tickets {
						done, err := svc.RespondToMatch(ctx, fmt.Sprintf("duel-%d", i), v.ID, true)
						if err != nil {
							t.Fatal(err)
						}
						rid = done.RoomID
					}
				} else {
					made, err := svc.CreateRoom(ctx, "duel-0", room.RoomCreateInput{Name: "好友对决", Kind: "friend", MaxPlayers: 2, Visibility: "invite", ModeConfig: json.RawMessage(`{"modeId":"classic","variant":"friend","maxWrongGuesses":8,"winPolicy":"first-correct","durationSeconds":180}`)})
					if err != nil {
						t.Fatal(err)
					}
					rid = made.Room.ID
					if _, err = svc.Join(ctx, "duel-1", rid, room.JoinInput{InviteCode: made.InviteCode, RequestedRole: "player"}); err != nil {
						t.Fatal(err)
					}
					owner, err := svc.LoadActor(ctx, "duel-0", rid)
					if err != nil {
						t.Fatal(err)
					}
					r, err := svc.CreateRound(ctx, owner, room.RoundCreateInput{})
					if err != nil {
						t.Fatal(err)
					}
					for i, target := range []string{"briefing", "collecting"} {
						if _, err = svc.CreateTransition(ctx, owner, r.ID, room.TransitionInput{Target: target, ExpectedPhaseVersion: int64(i + 1)}); err != nil {
							t.Fatal(err)
						}
					}
				}
				active, err := svc.CurrentRoundSummary(ctx, rid)
				if err != nil || active == nil {
					t.Fatal(err)
				}
				var secret struct {
					AnswerID string `json:"answerId"`
					Pool     []struct {
						ID string `json:"ID"`
					} `json:"pool"`
				}
				var private string
				if err = db.Read().QueryRow(`SELECT state_json FROM round_private_state WHERE round_id=? AND scope='round'`, active.ID).Scan(&private); err != nil {
					t.Fatal(err)
				}
				if err = json.Unmarshal([]byte(private), &secret); err != nil {
					t.Fatal(err)
				}
				wrong := []string{}
				for _, c := range secret.Pool {
					if c.ID != secret.AnswerID {
						wrong = append(wrong, c.ID)
					}
				}
				if len(wrong) < 8 {
					t.Fatal("insufficient fixture candidates")
				}
				actors := make([]room.Actor, 2)
				rooms := make([]room.Room, 2)
				for i := range actors {
					actors[i], err = svc.LoadActor(ctx, fmt.Sprintf("duel-%d", i), rid)
					if err != nil {
						t.Fatal(err)
					}
					rooms[i], _, err = svc.GetRoom(ctx, actors[i].SessionID, rid)
					if err != nil {
						t.Fatal(err)
					}
				}
				submit := func(player, revision int, id string) {
					t.Helper()
					payload, _ := json.Marshal(map[string]string{"characterId": id})
					if _, err := svc.CreateSubmission(ctx, actors[player], active.ID, room.SubmissionInput{PhaseVersion: 3, SubmissionRevision: int64(revision), ModeAction: modes.ModeAction{Kind: "classic.guess", Payload: payload}}); err != nil {
						t.Fatalf("player %d revision %d: %v", player, revision, err)
					}
					var correct bool
					if err := db.Read().QueryRow(`SELECT correct FROM classic_guesses WHERE round_id=? AND subject_id=? AND character_id=?`, active.ID, actors[player].MembershipID(), id).Scan(&correct); err != nil {
						t.Fatal(err)
					}
					if correct != (id == secret.AnswerID) {
						t.Fatal("stored guess confuses exhausted attempts with a correct answer")
					}
				}
				project := func(player int) room.RoundView {
					t.Helper()
					v, err := svc.BuildRoundView(ctx, active.ID, rooms[player], actors[player])
					if err != nil {
						t.Fatal(err)
					}
					return v
				}
				if strings.HasPrefix(ending, "surrender-") {
					loser := 0
					if ending == "surrender-b" || ending == "surrender-exhausted-opponent" {
						loser = 1
					}
					if ending == "surrender-exhausted-opponent" {
						for i := 0; i < 8; i++ {
							submit(0, i, wrong[i])
						}
					}
					if ending == "surrender-simultaneous" {
						type attempt struct {
							player int
							err    error
						}
						results := make(chan attempt, 2)
						for i := range actors {
							go func(player int) {
								_, _, err := svc.CreateSurrender(ctx, actors[player], active.ID, 3)
								results <- attempt{player, err}
							}(i)
						}
						accepted := 0
						for i := 0; i < 2; i++ {
							result := <-results
							if result.err == nil {
								accepted++
								loser = result.player
							}
						}
						if accepted != 1 {
							t.Fatalf("accepted %d simultaneous surrenders", accepted)
						}
					} else if _, _, err := svc.CreateSurrender(ctx, actors[loser], active.ID, 3); err != nil {
						t.Fatal(err)
					}
					winner := 1 - loser
					for i := range actors {
						v := project(i)
						if v.Round.Status != "settled" || v.CanSubmit || v.Round.DeadlineAt != nil || v.RevealedAnswerCharacterID != secret.AnswerID {
							t.Fatal("surrender did not immediately settle and reveal the duel")
						}
						var pub struct {
							Winner string                             `json:"surrenderWinnerSubjectId"`
							Self   struct{ Solved, Surrendered bool } `json:"self"`
						}
						if err := json.Unmarshal(v.PublicState, &pub); err != nil {
							t.Fatal(err)
						}
						if pub.Winner != actors[winner].MembershipID() || pub.Self.Solved || pub.Self.Surrendered != (i == loser) {
							t.Fatal("forfeit winner was not distinguished from a correct guess")
						}
						var score int
						var explanation string
						if err := db.Read().QueryRow(`SELECT score_units,explanation_json FROM round_results WHERE round_id=? AND subject_id=?`, active.ID, actors[i].MembershipID()).Scan(&score, &explanation); err != nil {
							t.Fatal(err)
						}
						if i == loser && score != 0 || i == winner && (score <= 0 || !strings.Contains(explanation, `"wonBySurrender":true`)) {
							t.Fatalf("incorrect surrender score %d: %s", score, explanation)
						}
						if _, _, err := svc.CreateSurrender(ctx, actors[i], active.ID, 3); err == nil {
							t.Fatal("late surrender reversed an ended duel")
						}
						payload, _ := json.Marshal(map[string]string{"characterId": secret.AnswerID})
						if _, err := svc.CreateSubmission(ctx, actors[i], active.ID, room.SubmissionInput{PhaseVersion: 3, ModeAction: modes.ModeAction{Kind: "classic.guess", Payload: payload}}); err == nil {
							t.Fatal("ended duel accepted another guess")
						}
					}
					live, _, err := svc.GetRoom(ctx, actors[0].SessionID, rid)
					if err != nil || live.Status != "finished" {
						t.Fatal("room not finished after surrender", err)
					}
					return
				}
				// Both players receive identical feedback for the same guess, from one server-prepared answer.
				submit(0, 0, wrong[0])
				submit(1, 0, wrong[0])
				a, b := project(0), project(1)
				var pa, pb struct {
					Self struct {
						Rows []json.RawMessage `json:"rows"`
					} `json:"self"`
				}
				json.Unmarshal(a.PublicState, &pa)
				json.Unmarshal(b.PublicState, &pb)
				if len(pa.Self.Rows) != 1 || len(pb.Self.Rows) != 1 || string(pa.Self.Rows[0]) != string(pb.Self.Rows[0]) {
					t.Fatal("different questions or feedback")
				}
				for i := 1; i < 8; i++ {
					submit(0, i, wrong[i])
				}
				a, b = project(0), project(1)
				if a.CanSubmit || !b.CanSubmit || a.Round.Status != "collecting" {
					t.Fatal("one exhausted player prematurely ended the opponent's chance")
				}
				for _, v := range []room.RoundView{a, b} {
					if v.RevealedAnswerCharacterID != "" || strings.Contains(string(v.PublicState), `"answer":`) || strings.Contains(string(v.PrivateState), `"answerId"`) {
						t.Fatal("answer revealed before opponent finished")
					}
				}
				answerPayload, err := json.Marshal(map[string]string{"characterId": secret.AnswerID})
				if err != nil {
					t.Fatal(err)
				}
				if _, err = svc.CreateSubmission(ctx, actors[0], active.ID, room.SubmissionInput{PhaseVersion: 3, SubmissionRevision: 8, ModeAction: modes.ModeAction{Kind: "classic.guess", Payload: answerPayload}}); err == nil {
					t.Fatal("an exhausted player was allowed another guess")
				}
				if ending == "opponent-wins" {
					submit(1, 1, secret.AnswerID)
				} else if ending == "both-exhausted" {
					for i := 1; i < 8; i++ {
						submit(1, i, wrong[i])
					}
				} else {
					past := active.DeadlineAt.Add(time.Second)
					svc.SetClock(func() time.Time { return past })
					if _, err = svc.SweepExpired(ctx); err != nil {
						t.Fatal(err)
					}
				}
				v := project(0)
				if v.Round.Status != "locked" {
					t.Fatalf("did not close: %s", v.Round.Status)
				}
				if _, err = svc.CreateSubmission(ctx, actors[1], active.ID, room.SubmissionInput{PhaseVersion: 3, SubmissionRevision: 8, ModeAction: modes.ModeAction{Kind: "classic.guess", Payload: answerPayload}}); err == nil {
					t.Fatal("a closed round accepted another guess")
				}
				// Reveal and settlement are timer driven. No participant forces a phase change.
				finish := active.DeadlineAt.Add(10 * time.Second)
				for i := 0; i < 3; i++ {
					tick := finish.Add(time.Duration(i) * 10 * time.Second)
					svc.SetClock(func() time.Time { return tick })
					if _, err = svc.SweepExpired(ctx); err != nil {
						t.Fatal(err)
					}
				}
				for i := range actors {
					v = project(i)
					if v.Round.Status != "settled" || v.RevealedAnswerCharacterID != secret.AnswerID {
						t.Fatal("final reveal missing")
					}
				}
				var scoreA, scoreB int64
				if err = db.Read().QueryRow(`SELECT score_units FROM round_results WHERE round_id=? AND subject_id=?`, active.ID, actors[0].MembershipID()).Scan(&scoreA); err != nil {
					t.Fatal(err)
				}
				if err = db.Read().QueryRow(`SELECT score_units FROM round_results WHERE round_id=? AND subject_id=?`, active.ID, actors[1].MembershipID()).Scan(&scoreB); err != nil {
					t.Fatal(err)
				}
				if scoreA != 0 || (ending == "opponent-wins" && scoreB <= 0) || (ending != "opponent-wins" && scoreB != 0) {
					t.Fatalf("wrong outcome %d/%d", scoreA, scoreB)
				}
			})
		}
	}
}
