package room_test

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"

	"github.com/bhcn/umaguessinggame/internal/httpapi/problem"
	"github.com/bhcn/umaguessinggame/internal/modes"
	"github.com/bhcn/umaguessinggame/internal/room"
	"github.com/bhcn/umaguessinggame/internal/storage"
)

// A calibrated mode gives distinct scores for successive rounds. Real modes
// are exercised separately in the browser; this pins programme arithmetic.
type eventScoreMode struct {
	sharedStub
	mu         sync.Mutex
	settled    int
	fixedScore int
	fixedMax   int
}

func (m *eventScoreMode) Prepare(_ context.Context, in modes.PrepareInput) (modes.Prepared, error) {
	public, _ := json.Marshal(struct {
		Subjects []modes.Subject
	}{in.Subject})
	return modes.Prepared{Public: public}, nil
}
func (m *eventScoreMode) Settle(in modes.SettleInput) ([]modes.Score, error) {
	var state struct {
		Subjects []modes.Subject
		Score    int
	}
	if err := json.Unmarshal(in.Public, &state); err != nil {
		return nil, err
	}
	m.mu.Lock()
	m.settled++
	n := m.settled
	m.mu.Unlock()
	state.Score = 2500
	if n%2 == 0 {
		state.Score = 7500
	}
	maxScore := 10000
	if m.fixedMax > 0 {
		state.Score = m.fixedScore
		maxScore = m.fixedMax
	}
	out := make([]modes.Score, 0, len(state.Subjects))
	for _, subject := range state.Subjects {
		out = append(out, modes.Score{Subject: subject, ScoreUnits: state.Score, MaxScoreUnits: maxScore})
	}
	return out, nil
}

type eventFixture struct {
	svc     *room.Service
	db      *storage.DB
	actor   room.Actor
	segment room.Segment
	event   room.Event
	rules   json.RawMessage
	invite  string
}

func newEventFixture(t *testing.T, rounds, budget int, calibrated ...*eventScoreMode) eventFixture {
	t.Helper()
	db := newDB(t)
	mode := &eventScoreMode{sharedStub: sharedStub{id: "sort"}}
	if len(calibrated) > 0 {
		mode = calibrated[0]
	}
	reg, _ := modes.NewRegistry(mode, &sharedStub{id: "creative"}, &sharedStub{id: "trade"})
	svc := room.New(db, room.Config{}, room.Options{Modes: reg, Epoch: "event-test"})
	t.Cleanup(svc.Close)
	ctx := context.Background()
	for _, id := range []string{"owner", "guest"} {
		if _, err := db.Write().Exec(`INSERT INTO sessions(id,token_hash,display_name,csrf_token_hash,kind,created_at,expires_at) VALUES(?,?,?,?,'player',0,9999999999999)`, id, "h-"+id, id, "c-"+id); err != nil {
			t.Fatal(err)
		}
	}
	rules := json.RawMessage(`{"modeId":"sort","informationStyle":"open","config":{"questionSeconds":35}}`)
	made, err := svc.CreateRoom(ctx, "owner", room.RoomCreateInput{Name: "活动", Kind: "event", ModeConfig: rules, MaxPlayers: 4, Visibility: "invite"})
	if err != nil {
		t.Fatal(err)
	}
	actor, err := svc.LoadActor(ctx, "owner", made.Room.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !actor.Has(room.CapHost) || actor.Has(room.CapPlay) {
		t.Fatal("event organiser must host, not silently compete")
	}
	if _, err := svc.Join(ctx, "guest", made.Room.ID, room.JoinInput{InviteCode: made.InviteCode, RequestedRole: "player"}); err != nil {
		t.Fatal(err)
	}
	event, err := svc.CreateEvent(ctx, "owner", room.CreateEventInput{RoomID: made.Room.ID, Name: "连续回合", TiePolicy: "shared-rank"})
	if err != nil {
		t.Fatal(err)
	}
	seg, err := svc.CreateSegment(ctx, actor, event.ID, room.SegmentCreateInput{ModeConfig: rules, Label: "排序两局", ScoreBudget: budget, RoundCount: rounds, Competitive: true})
	if err != nil {
		t.Fatal(err)
	}
	return eventFixture{svc, db, actor, seg, event, rules, made.InviteCode}
}
func (f eventFixture) start(t *testing.T) room.Round {
	t.Helper()
	r, err := f.svc.CreateRound(context.Background(), f.actor, room.RoundCreateInput{ModeConfig: f.rules, SegmentID: f.segment.ID})
	if err != nil {
		t.Fatal(err)
	}
	return r
}
func (f eventFixture) settle(t *testing.T, r room.Round) {
	t.Helper()
	version := r.PhaseVersion
	for _, target := range []string{"briefing", "collecting", "locked", "revealed", "settled"} {
		res, err := f.svc.CreateTransition(context.Background(), f.actor, r.ID, room.TransitionInput{Target: target, ExpectedPhaseVersion: version})
		if err != nil {
			t.Fatalf("%s: %v", target, err)
		}
		version = res.PhaseVersion
	}
}
func wantProblem(t *testing.T, err error, code string) {
	t.Helper()
	var p *problem.Error
	if !errors.As(err, &p) || p.Code != code {
		t.Fatalf("want %s, got %v", code, err)
	}
}

func TestEventMultipleRoundsKeepBudgetAndHistory(t *testing.T) {
	f := newEventFixture(t, 2, 60)
	ctx := context.Background()
	first := f.start(t)
	_, err := f.svc.CreateRound(ctx, f.actor, room.RoundCreateInput{ModeConfig: f.rules, SegmentID: f.segment.ID})
	wantProblem(t, err, "round-already-open")
	f.settle(t, first)
	event, segs, err := f.svc.GetEvent(ctx, "owner", f.event.ID)
	if err != nil {
		t.Fatal(err)
	}
	if segs[0].Status != "running" || segs[0].CompletedRounds != 1 || segs[0].StartedRounds != 1 {
		t.Fatalf("premature completion: %+v", segs[0])
	}
	standing, err := f.svc.GetStandings(ctx, "owner", event.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(standing.Items) != 1 || standing.Items[0].ScoreUnits != 750 || standing.Items[0].Segments[0].MaxUnits != 6000 {
		t.Fatalf("partial fixed denominator: %+v", standing)
	}
	if len(standing.Items[0].PublicCode) != 6 {
		t.Fatalf("formal entrant has no public room number: %+v", standing.Items[0])
	}
	subject := standing.Items[0].SubjectID
	_, _, _, err = f.svc.CreateScoreCorrection(ctx, f.actor, event.ID, room.CorrectionInput{SegmentID: f.segment.ID, SubjectID: subject, NewScoreUnits: 100, Reason: "提前更正测试原因", ExpectedEventVersion: event.EventVersion})
	wantProblem(t, err, "segment-not-settled")
	second := f.start(t)
	f.settle(t, second)
	event, segs, err = f.svc.GetEvent(ctx, "owner", event.ID)
	if err != nil {
		t.Fatal(err)
	}
	if segs[0].Status != "completed" || segs[0].CompletedRounds != 2 || segs[0].RoundID != second.ID {
		t.Fatalf("last round: %+v", segs[0])
	}
	standing, err = f.svc.GetStandings(ctx, "owner", event.ID)
	if err != nil {
		t.Fatal(err)
	}
	if standing.Items[0].ScoreUnits != 3000 {
		t.Fatalf("25%% + 75%% over two rounds at 60 points: %+v", standing)
	}
	_, err = f.svc.CreateRound(ctx, f.actor, room.RoundCreateInput{ModeConfig: f.rules, SegmentID: f.segment.ID})
	wantProblem(t, err, "segment-closed")
	_, _, _, err = f.svc.CreateScoreCorrection(ctx, f.actor, event.ID, room.CorrectionInput{SegmentID: f.segment.ID, SubjectID: subject, NewScoreUnits: 6100, Reason: "超过分项预算测试", ExpectedEventVersion: event.EventVersion})
	if err == nil {
		t.Fatal("correction exceeded budget")
	}
	_, _, _, err = f.svc.CreateScoreCorrection(ctx, f.actor, event.ID, room.CorrectionInput{SegmentID: f.segment.ID, SubjectID: subject, NewScoreUnits: 2000, Reason: "人工核对更正成绩", ExpectedEventVersion: event.EventVersion})
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range []room.Round{first, second} {
		results, err := f.svc.RoundResults(ctx, r.ID)
		if err != nil || len(results) != 1 {
			t.Fatal("lost round history", err)
		}
		want := 2500
		if r.ID == second.ID {
			want = 7500
		}
		if results[0].ScoreUnits != want {
			t.Fatal("correction rewrote raw round history")
		}
	}
}

func TestEventSegmentBindingRejectsOtherRoomRulesAndRoster(t *testing.T) {
	f := newEventFixture(t, 2, 100)
	ctx := context.Background()
	other, err := f.svc.CreateRoom(ctx, "owner", room.RoomCreateInput{Name: "另一个房间", Kind: "friend", ModeConfig: f.rules, MaxPlayers: 4, Visibility: "invite"})
	if err != nil {
		t.Fatal(err)
	}
	actor, err := f.svc.LoadActor(ctx, "owner", other.Room.ID)
	if err != nil {
		t.Fatal(err)
	}
	_, err = f.svc.CreateRound(ctx, actor, room.RoundCreateInput{ModeConfig: f.rules, SegmentID: f.segment.ID})
	wantProblem(t, err, "segment-not-found")
	_, err = f.svc.CreateRound(ctx, f.actor, room.RoundCreateInput{ModeConfig: json.RawMessage(`{"modeId":"sort","informationStyle":"closed"}`), SegmentID: f.segment.ID})
	wantProblem(t, err, "segment-rules-mismatch")
	r := f.start(t)
	f.settle(t, r)
	if _, err := f.db.Write().Exec(`UPDATE memberships SET role='spectator' WHERE session_id='guest' AND room_id=?`, f.actor.RoomID); err != nil {
		t.Fatal(err)
	}
	// Another entrant changes the scoring identity even if headcount is identical.
	if _, err := f.db.Write().Exec(`UPDATE memberships SET role='player' WHERE session_id='owner' AND room_id=?`, f.actor.RoomID); err != nil {
		t.Fatal(err)
	}
	_, err = f.svc.CreateRound(ctx, f.actor, room.RoundCreateInput{ModeConfig: f.rules, SegmentID: f.segment.ID})
	wantProblem(t, err, "segment-roster-changed")
}

func TestCancelledEventCannotBindNewRound(t *testing.T) {
	f := newEventFixture(t, 1, 100)
	ctx := context.Background()
	event, _, err := f.svc.GetEvent(ctx, "owner", f.event.ID)
	if err != nil {
		t.Fatal(err)
	}
	_, err = f.svc.CreateEventTransition(ctx, f.actor, event.ID, "cancelled", event.EventVersion, "活动取消测试原因")
	if err != nil {
		t.Fatal(err)
	}
	_, err = f.svc.CreateRound(ctx, f.actor, room.RoundCreateInput{ModeConfig: f.rules, SegmentID: f.segment.ID})
	wantProblem(t, err, "event-closed")
}

func TestEventFractionalScoresRoundOnceAndEntertainmentStaysOutOfStandings(t *testing.T) {
	f := newEventFixture(t, 2, 100, &eventScoreMode{sharedStub: sharedStub{id: "sort"}, fixedScore: 1, fixedMax: 3})
	ctx := context.Background()
	for i := 0; i < 2; i++ {
		f.settle(t, f.start(t))
	}
	standings, err := f.svc.GetStandings(ctx, "owner", f.event.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(standings.Items) != 1 || standings.Items[0].ScoreUnits != 3333 {
		t.Fatalf("exact one-third aggregate: %+v", standings)
	}
	if _, err = f.db.Write().Exec(`UPDATE event_segments SET config_json=json_set(config_json,'$.competitive',json('false')) WHERE id=?`, f.segment.ID); err != nil {
		t.Fatal(err)
	}
	standings, err = f.svc.GetStandings(ctx, "owner", f.event.ID)
	if err != nil || len(standings.Items) != 0 {
		t.Fatalf("entertainment changed formal standings: %+v %v", standings, err)
	}
	event, segs, err := f.svc.GetEvent(ctx, "owner", f.event.ID)
	if err != nil {
		t.Fatal(err)
	}
	results, err := f.svc.RoundResults(ctx, segs[0].RoundID)
	if len(segs[0].Results) != 1 || segs[0].Results[0].ScoreUnits != 3333 || segs[0].Results[0].DisplayName != "guest" {
		t.Fatalf("separate entertainment results: %+v", segs[0])
	}
	if len(segs[0].Results[0].PublicCode) != 6 {
		t.Fatalf("entertainment entrant has no public room number: %+v", segs[0].Results[0])
	}
	if err != nil || len(results) != 1 || results[0].ScoreUnits != 1 {
		t.Fatalf("entertainment lost round results: %+v %v event=%s", results, err, event.ID)
	}
}

func TestEventConcurrentStartsBindOnlyOneRound(t *testing.T) {
	f := newEventFixture(t, 2, 100)
	ctx := context.Background()
	start := make(chan struct{})
	outcomes := make(chan error, 2)
	for i := 0; i < 2; i++ {
		go func() {
			<-start
			_, err := f.svc.CreateRound(ctx, f.actor, room.RoundCreateInput{ModeConfig: f.rules, SegmentID: f.segment.ID})
			outcomes <- err
		}()
	}
	close(start)
	accepted := 0
	for i := 0; i < 2; i++ {
		if <-outcomes == nil {
			accepted++
		}
	}
	var links, count int
	if err := f.db.Read().QueryRow(`SELECT COUNT(*) FROM event_segment_rounds WHERE segment_id=?`, f.segment.ID).Scan(&links); err != nil {
		t.Fatal(err)
	}
	if err := f.db.Read().QueryRow(`SELECT COUNT(*) FROM rounds WHERE room_id=?`, f.actor.RoomID).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if accepted != 1 || links != 1 || count != 1 {
		t.Fatalf("concurrent starts accepted=%d links=%d rounds=%d", accepted, links, count)
	}
}

func TestEventCancellingRoundVoidsWholeSegment(t *testing.T) {
	f := newEventFixture(t, 2, 100)
	ctx := context.Background()
	f.settle(t, f.start(t))
	second := f.start(t)
	if _, err := f.svc.CreateTransition(ctx, f.actor, second.ID, room.TransitionInput{Target: "cancel", ExpectedPhaseVersion: second.PhaseVersion, Reason: "题目资料出现问题"}); err != nil {
		t.Fatal(err)
	}
	_, segments, err := f.svc.GetEvent(ctx, "owner", f.event.ID)
	if err != nil || segments[0].Status != "voided" {
		t.Fatalf("cancelled segment: %+v %v", segments, err)
	}
	standings, err := f.svc.GetStandings(ctx, "owner", f.event.ID)
	if err != nil || len(standings.Items) != 0 {
		t.Fatalf("voided scores leaked: %+v %v", standings, err)
	}
	_, err = f.svc.CreateRound(ctx, f.actor, room.RoundCreateInput{ModeConfig: f.rules, SegmentID: f.segment.ID})
	wantProblem(t, err, "segment-closed")
}

func TestEntertainmentModeCannotBePromotedByCreatingOrEditingSegment(t *testing.T) {
	f := newEventFixture(t, 1, 100)
	ctx := context.Background()
	for _, mode := range []string{"creative", "trade"} {
		rules := json.RawMessage(`{"modeId":"` + mode + `","informationStyle":"open"}`)
		_, err := f.svc.CreateSegment(ctx, f.actor, f.event.ID, room.SegmentCreateInput{ModeConfig: rules, Label: "娱乐项目", ScoreBudget: 10, RoundCount: 1, Competitive: true})
		wantProblem(t, err, "event-entertainment-only")
		event, _, err := f.svc.GetEvent(ctx, "owner", f.event.ID)
		if err != nil {
			t.Fatal(err)
		}
		_, err = f.svc.UpdateSegment(ctx, f.actor, f.event.ID, f.segment.ID, room.SegmentPatchInput{ModeConfig: rules, ExpectedEventVersion: event.EventVersion})
		wantProblem(t, err, "event-entertainment-only")
		made, err := f.svc.CreateSegment(ctx, f.actor, f.event.ID, room.SegmentCreateInput{ModeConfig: rules, Label: "娱乐项目", ScoreBudget: 10, RoundCount: 1, Competitive: false})
		if err != nil || made.Competitive() {
			t.Fatalf("entertainment creation: %+v %v", made, err)
		}
	}
}
