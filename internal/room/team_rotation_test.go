package room_test

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/bhcn/umaguessinggame/internal/httpapi/problem"
	"github.com/bhcn/umaguessinggame/internal/modes"
	"github.com/bhcn/umaguessinggame/internal/modes/memory"
	"github.com/bhcn/umaguessinggame/internal/modes/strategyrace"
	"github.com/bhcn/umaguessinggame/internal/room"
	"github.com/bhcn/umaguessinggame/internal/storage"
)

type teamRound struct {
	db              *storage.DB
	svc             *room.Service
	now             time.Time
	roomID, roundID string
}

func newTeamRound(t *testing.T, mode string) *teamRound {
	t.Helper()
	db := newDB(t)
	reg, err := modes.NewRegistry(memory.New(), strategyrace.New())
	if err != nil {
		t.Fatal(err)
	}
	f := &teamRound{db: db, now: time.Now().UTC()}
	f.svc = room.New(db, room.Config{}, room.Options{Epoch: "test", Modes: reg})
	f.svc.SetClock(func() time.Time { return f.now })
	t.Cleanup(f.svc.Close)
	f.roomID, f.roundID = seedStrategyRound(t, db, f.now, f.now.Add(20*time.Second))
	exec := func(query string, args ...any) {
		t.Helper()
		if _, err := db.Write().ExecContext(context.Background(), query, args...); err != nil {
			t.Fatal(err)
		}
	}
	for _, team := range []string{"a", "b"} {
		exec(`INSERT INTO teams(id,room_id,label,color,created_at) VALUES(?,?,?,?,?)`, "mem_"+team, f.roomID, "队伍"+team, "green", f.now.UnixMilli())
		for i := 1; i <= 2; i++ {
			id := fmt.Sprintf("%s%d", team, i)
			role := "player"
			if i == 1 {
				role = "team-captain"
			}
			exec(`INSERT INTO sessions(id,token_hash,display_name,csrf_token_hash,kind,created_at,expires_at) VALUES(?,?,?,?, 'player',?,?)`, "ses_"+id, "hash_"+id, "训练员"+id, "csrf_"+id, f.now.UnixMilli(), f.now.Add(time.Hour).UnixMilli())
			exec(`INSERT INTO memberships(id,room_id,session_id,role,team_id,ready,joined_at) VALUES(?,?,?,?,?,1,?)`, id, f.roomID, "ses_"+id, role, "mem_"+team, f.now.UnixMilli()+int64(i))
		}
	}
	if mode == "memory" {
		var cards []map[string]any
		pairOf, images, labels := map[string]string{}, map[string]string{}, map[string]string{}
		for i := 0; i < 16; i++ {
			id := fmt.Sprintf("c%d", i)
			pair := fmt.Sprintf("p%d", i/2)
			cards = append(cards, map[string]any{"cardId": id, "position": i})
			pairOf[id] = pair
			images[id] = "/assets/" + id + ".webp"
			labels[pair] = pair
		}
		pub, _ := json.Marshal(map[string]any{"modeId": "memory", "rulesVersion": memory.RulesVersion, "config": map[string]any{"maxAttempts": 20, "viewSeconds": 8, "playSeconds": 90}, "rows": 4, "cols": 4, "cards": cards, "subjects": []map[string]string{{"subjectId": "mem_a", "subjectKind": "team"}, {"subjectId": "mem_b", "subjectKind": "team"}}})
		priv, _ := json.Marshal(map[string]any{"pairOf": pairOf, "cardImage": images, "pairLabel": labels})
		exec(`UPDATE rounds SET mode_id='memory',rules_version=?,config_json=? WHERE id=?`, memory.RulesVersion, `{"modeId":"memory","config":{"maxAttempts":20,"viewSeconds":8,"playSeconds":90}}`, f.roundID)
		exec(`UPDATE round_private_state SET state_json=? WHERE round_id=? AND scope='public'`, string(pub), f.roundID)
		exec(`UPDATE round_private_state SET state_json=? WHERE round_id=? AND scope='round'`, string(priv), f.roundID)
	}
	return f
}
func (f *teamRound) actor(id string) room.Actor {
	team := "mem_" + id[:1]
	role := room.RolePlayer
	if id[1:] == "1" {
		role = room.RoleTeamCaptain
	}
	return room.Actor{SessionID: "ses_" + id, RoomID: f.roomID, Membership: &room.Membership{ID: id, RoomID: f.roomID, TeamID: team, Role: role}, Capabilities: map[room.Capability]bool{room.CapPlay: true}}
}
func (f *teamRound) view(t *testing.T, id string) room.RoundView {
	t.Helper()
	v, err := f.svc.BuildRoundView(context.Background(), f.roundID, room.Room{ID: f.roomID, RoomVersion: 1}, f.actor(id))
	if err != nil {
		t.Fatal(err)
	}
	return v
}
func (f *teamRound) submit(t *testing.T, id, kind, payload string) room.SubmissionReceipt {
	t.Helper()
	v := f.view(t, id)
	rec, err := f.svc.CreateSubmission(context.Background(), f.actor(id), f.roundID, room.SubmissionInput{PhaseVersion: v.Round.PhaseVersion, SubmissionRevision: v.SubmissionRevision, ControlGeneration: v.ControlGeneration, ModeAction: modes.ModeAction{Kind: kind, Payload: json.RawMessage(payload)}})
	if err != nil {
		t.Fatalf("%s %s: %v", id, kind, err)
	}
	return rec
}
func (f *teamRound) control(t *testing.T, team, holder string, generation int64) {
	t.Helper()
	v := f.view(t, holder)
	for _, c := range v.TeamControllers {
		if c.TeamID == team {
			if c.MembershipID != holder || c.Generation != generation || !v.CanSubmit || v.ControlGeneration != generation {
				t.Fatalf("controller=%+v canSubmit=%v controlGeneration=%d", c, v.CanSubmit, v.ControlGeneration)
			}
			return
		}
	}
	t.Fatal("missing team controller")
}
func (f *teamRound) handover(t *testing.T, id string, gen int64) room.ControlLease {
	t.Helper()
	lease, err := f.svc.IssueControlLease(context.Background(), room.Actor{SessionID: "ses_owner", RoomID: f.roomID, Capabilities: map[room.Capability]bool{room.CapHost: true}}, room.IssueControlLeaseInput{RoundID: f.roundID, MembershipID: id, ExpectedGeneration: gen})
	if err != nil {
		t.Fatal(err)
	}
	return lease
}
func (f *teamRound) reject(t *testing.T, id, code string, in room.SubmissionInput) {
	t.Helper()
	_, err := f.svc.CreateSubmission(context.Background(), f.actor(id), f.roundID, in)
	if err == nil || problem.As(err).Code != code {
		t.Fatalf("want %s, got %v", code, err)
	}
}
func (f *teamRound) exec(t *testing.T, query string, args ...any) {
	t.Helper()
	if _, err := f.db.Write().ExecContext(context.Background(), query, args...); err != nil {
		t.Fatal(err)
	}
}

func TestMemoryTeamRotatesOnlyAfterPairAndRejectsOldTabs(t *testing.T) {
	f := newTeamRound(t, "memory")
	f.control(t, "mem_a", "a1", 0)
	f.reject(t, "a2", "team-controller-required", room.SubmissionInput{PhaseVersion: 1, ModeAction: modes.ModeAction{Kind: "memory.flip", Payload: json.RawMessage(`{"cardId":"c0"}`)}})
	f.submit(t, "a1", "memory.flip", `{"cardId":"c0"}`)
	f.control(t, "mem_a", "a1", 0)
	f.submit(t, "a1", "memory.flip", `{"cardId":"c1"}`)
	f.control(t, "mem_a", "a2", 1)
	f.control(t, "mem_b", "b1", 0)
	if f.view(t, "a1").CanSubmit {
		t.Fatal("previous representative still allowed")
	}
	// Wrong pair also completes one attempt, without leaking another team's board.
	f.submit(t, "a2", "memory.flip", `{"cardId":"c2"}`)
	f.submit(t, "a2", "memory.flip", `{"cardId":"c4"}`)
	f.control(t, "mem_a", "a1", 2)
	v := f.view(t, "a1")
	if v.SubmissionRevision != 4 {
		t.Fatalf("shared revision=%d", v.SubmissionRevision)
	}
	var p struct {
		Self struct{ MatchedPairs, WrongAttempts, Attempts int }
	}
	if err := json.Unmarshal(v.PublicState, &p); err != nil {
		t.Fatal(err)
	}
	if p.Self.MatchedPairs != 1 || p.Self.WrongAttempts != 1 || p.Self.Attempts != 2 {
		t.Fatalf("shared progress=%+v", p.Self)
	}
	f.reject(t, "a1", "control-generation-conflict", room.SubmissionInput{PhaseVersion: 1, SubmissionRevision: 4, ControlGeneration: 0, ModeAction: modes.ModeAction{Kind: "memory.flip", Payload: json.RawMessage(`{"cardId":"c3"}`)}})
	f.reject(t, "a1", memory.CodeTooManyFlipped, room.SubmissionInput{PhaseVersion: 1, SubmissionRevision: 4, ControlGeneration: 2, ModeAction: modes.ModeAction{Kind: "memory.flip", Payload: json.RawMessage(`{"cardId":"c3"}`)}})
}
func TestMemoryFinalAttemptDoesNotRotate(t *testing.T) {
	f := newTeamRound(t, "memory")
	f.exec(t, `INSERT INTO participant_round_state(round_id,subject_id,subject_kind,state_json,updated_at) VALUES(?, 'mem_a','team',?,?)`, f.roundID, `{"attempts":19,"wrongAttempts":19}`, f.now.UnixMilli())
	f.submit(t, "a1", "memory.flip", `{"cardId":"c0"}`)
	f.submit(t, "a1", "memory.flip", `{"cardId":"c2"}`)
	f.control(t, "mem_a", "a1", 0)
	f.reject(t, "a1", memory.CodeOutOfAttempts, room.SubmissionInput{PhaseVersion: 1, SubmissionRevision: 2, ModeAction: modes.ModeAction{Kind: "memory.flip", Payload: json.RawMessage(`{"cardId":"c3"}`)}})
}
func TestMemoryManualHandoverContinuesFromActualRepresentative(t *testing.T) {
	f := newTeamRound(t, "memory")
	f.submit(t, "a1", "memory.flip", `{"cardId":"c0"}`)
	f.handover(t, "a2", 0)
	f.submit(t, "a2", "memory.flip", `{"cardId":"c1"}`)
	f.control(t, "mem_a", "a1", 2)
}
func TestStrategyTeamRotatesTogetherAfterAllCommit(t *testing.T) {
	f := newTeamRound(t, "strategy-race")
	f.submit(t, "a1", "strategy.action", `{"turn":1,"action":"steady"}`)
	f.control(t, "mem_a", "a1", 0)
	f.control(t, "mem_b", "b1", 0)
	f.submit(t, "b1", "strategy.action", `{"turn":1,"action":"steady"}`)
	f.control(t, "mem_a", "a2", 1)
	f.control(t, "mem_b", "b2", 1)
	f.reject(t, "a1", "team-controller-required", room.SubmissionInput{PhaseVersion: 1, SubmissionRevision: 1, ModeAction: modes.ModeAction{Kind: "strategy.action", Payload: json.RawMessage(`{"turn":2,"action":"sprint"}`)}})
	f.submit(t, "a2", "strategy.action", `{"turn":2,"action":"steady"}`)
	f.submit(t, "b2", "strategy.action", `{"turn":2,"action":"steady"}`)
	f.control(t, "mem_a", "a1", 2)
}
func TestStrategyTimeoutRotatesOnceAndPauseHolds(t *testing.T) {
	f := newTeamRound(t, "strategy-race")
	f.submit(t, "a1", "strategy.action", `{"turn":1,"action":"sprint"}`)
	f.exec(t, `UPDATE rounds SET paused_at=? WHERE id=?`, f.now.UnixMilli(), f.roundID)
	f.now = f.now.Add(21 * time.Second)
	if n, err := f.svc.SweepExpired(context.Background()); err != nil || n != 0 {
		t.Fatalf("paused sweep=%d %v", n, err)
	}
	f.control(t, "mem_a", "a1", 0)
	f.exec(t, `UPDATE rounds SET paused_at=NULL WHERE id=?`, f.roundID)
	if n, err := f.svc.SweepExpired(context.Background()); err != nil || n != 1 {
		t.Fatalf("expiry=%d %v", n, err)
	}
	f.control(t, "mem_a", "a2", 1)
	f.control(t, "mem_b", "b2", 1)
	if n, err := f.svc.SweepExpired(context.Background()); err != nil || n != 0 {
		t.Fatalf("repeat=%d %v", n, err)
	}
	f.control(t, "mem_a", "a2", 1)
}
func TestStrategyFinalTurnDoesNotRotateOrAcceptHandover(t *testing.T) {
	f := newTeamRound(t, "strategy-race")
	for turn := 1; turn <= 8; turn++ {
		i := 1 + (turn-1)%2
		f.submit(t, fmt.Sprintf("a%d", i), "strategy.action", fmt.Sprintf(`{"turn":%d,"action":"steady"}`, turn))
		f.submit(t, fmt.Sprintf("b%d", i), "strategy.action", fmt.Sprintf(`{"turn":%d,"action":"steady"}`, turn))
	}
	f.control(t, "mem_a", "a2", 7)
	_, err := f.svc.IssueControlLease(context.Background(), room.Actor{SessionID: "ses_owner", RoomID: f.roomID, Capabilities: map[room.Capability]bool{room.CapHost: true}}, room.IssueControlLeaseInput{RoundID: f.roundID, MembershipID: "a1", ExpectedGeneration: 7})
	if err == nil || problem.As(err).Code != "round-not-collecting" {
		t.Fatalf("terminal handover=%v", err)
	}
}
func TestTeamControlExpiryRevocationAndRestartPreserveEpoch(t *testing.T) {
	f := newTeamRound(t, "memory")
	first := f.handover(t, "a2", 0)
	if first.Generation != 1 {
		t.Fatal(first)
	}
	f.exec(t, `UPDATE control_leases SET expires_at=? WHERE id=?`, f.now.UnixMilli()-1, first.ID)
	f.control(t, "mem_a", "a1", 2)
	f.reject(t, "a1", "control-generation-conflict", room.SubmissionInput{PhaseVersion: 1, ControlGeneration: 0, ModeAction: modes.ModeAction{Kind: "memory.flip", Payload: json.RawMessage(`{"cardId":"c0"}`)}})
	second := f.handover(t, "a2", 2)
	if second.Generation != 3 {
		t.Fatal(second)
	}
	f.exec(t, `UPDATE control_leases SET revoked_at=? WHERE id=?`, f.now.UnixMilli(), second.ID)
	f.control(t, "mem_a", "a1", 4)
	reg, _ := modes.NewRegistry(memory.New(), strategyrace.New())
	restored := room.New(f.db, room.Config{}, room.Options{Epoch: "restart", Modes: reg})
	restored.SetClock(func() time.Time { return f.now })
	t.Cleanup(restored.Close)
	f.svc = restored
	f.control(t, "mem_a", "a1", 4)
	third := f.handover(t, "a2", 4)
	if third.Generation != 5 {
		t.Fatal(third)
	}
}
func TestTeamHistoryShadowsLegacyAndRemovedHolderFallsBack(t *testing.T) {
	f := newTeamRound(t, "memory")
	f.exec(t, `INSERT INTO control_leases(id,room_id,round_id,slot,membership_id,generation,issued_at,expires_at) VALUES('legacy',?,?,?,'a2',1,?,?)`, f.roomID, f.roundID, "round:"+f.roundID, f.now.UnixMilli(), f.now.Add(time.Hour).UnixMilli())
	f.control(t, "mem_a", "a2", 1)
	lease := f.handover(t, "a1", 1)
	if lease.Generation != 2 {
		t.Fatal(lease)
	}
	f.exec(t, `UPDATE memberships SET left_at=? WHERE id='a1'`, f.now.UnixMilli())
	f.control(t, "mem_a", "a2", 3)
	// The surviving member must echo the new epoch, not their still-live legacy lease.
	f.reject(t, "a2", "control-generation-conflict", room.SubmissionInput{PhaseVersion: 1, ControlGeneration: 1, ModeAction: modes.ModeAction{Kind: "memory.flip", Payload: json.RawMessage(`{"cardId":"c0"}`)}})
	f.submit(t, "a2", "memory.flip", `{"cardId":"c0"}`)
	next := f.handover(t, "a2", 3)
	if next.Generation != 4 {
		t.Fatal(next)
	}
}

func TestMemoryPreviewCannotBeSkippedAndClockSurvivesRefresh(t *testing.T) {
	f := newTeamRound(t, "memory")
	f.exec(t, `UPDATE rounds SET status='prepared',deadline_at=NULL WHERE id=?`, f.roundID)
	host := room.Actor{SessionID: "ses_owner", RoomID: f.roomID, Capabilities: map[room.Capability]bool{room.CapHost: true}}
	_, err := f.svc.CreateTransition(context.Background(), host, f.roundID, room.TransitionInput{Target: "collecting", ExpectedPhaseVersion: 1})
	if err == nil || problem.As(err).Code != "preview-in-progress" {
		t.Fatalf("skip preview: %v", err)
	}
	receipt, err := f.svc.CreateTransition(context.Background(), host, f.roundID, room.TransitionInput{Target: "briefing", ExpectedPhaseVersion: 1})
	if err != nil {
		t.Fatal(err)
	}
	v := f.view(t, "a1")
	if v.Round.DeadlineAt == nil || v.Round.DeadlineAt.UnixMilli() != receipt.AcceptedAt+8000 {
		t.Fatalf("preview clock: %+v", v.Round)
	}
	_, err = f.svc.CreateTransition(context.Background(), host, f.roundID, room.TransitionInput{Target: "collecting", ExpectedPhaseVersion: 2})
	if err == nil || problem.As(err).Code != "preview-in-progress" {
		t.Fatalf("early collect: %v", err)
	}
	f.reject(t, "a1", "preview-in-progress", room.SubmissionInput{PhaseVersion: 2, ModeAction: modes.ModeAction{Kind: "memory.flip", Payload: json.RawMessage(`{"cardId":"c0"}`)}})
	deadline := v.Round.DeadlineAt.UnixMilli()
	f.now = time.UnixMilli(deadline - 1)
	if n, err := f.svc.SweepExpired(context.Background()); err != nil || n != 0 {
		t.Fatalf("early sweep=%d %v", n, err)
	}
	if f.view(t, "a2").Round.DeadlineAt.UnixMilli() != deadline {
		t.Fatal("other viewer got different preview deadline")
	}
	f.now = time.UnixMilli(deadline + 1)
	if n, err := f.svc.SweepExpired(context.Background()); err != nil || n != 1 {
		t.Fatalf("preview sweep=%d %v", n, err)
	}
	v = f.view(t, "a1")
	if v.Round.Status != modes.PhaseCollecting || v.Round.DeadlineAt.UnixMilli() != f.now.Add(90*time.Second).UnixMilli() {
		t.Fatalf("answer window=%+v", v.Round)
	}
	f.submit(t, "a1", "memory.flip", `{"cardId":"c0"}`)
}
