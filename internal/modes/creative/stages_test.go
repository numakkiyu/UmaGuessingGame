package creative

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/bhcn/umaguessinggame/internal/modes"
	"github.com/bhcn/umaguessinggame/internal/modes/modeutil"
)

func stagedCreative(t *testing.T, budget int) (modes.Prepared, map[string]json.RawMessage, []modes.Subject) {
	t.Helper()
	seats := roster(3)
	parts := []modes.Participant{}
	for i, session := range []string{"s1", "s2", "s3", "s4"} {
		seat := seats[i%3]
		parts = append(parts, modes.Participant{SessionID: session, MembershipID: "m" + session, Subject: seat})
	}
	cfg, _ := json.Marshal(map[string]any{"entertainmentBudget": budget})
	p, err := New().Prepare(context.Background(), modes.PrepareInput{Scope: scope("live"), Subject: seats, Participants: parts, Config: cfg, Content: packOf()})
	if err != nil {
		t.Fatal(err)
	}
	states := map[string]json.RawMessage{}
	for i, seat := range seats {
		var st json.RawMessage
		savedConfig := p.Config
		p, st = commit(t, p, seat, workAction(strings.Repeat("e\u0301", 40)+string(rune('A'+i))), nil)
		p.Config = savedConfig
		states[seat.ID] = st
	}
	return p, states, seats
}

func openCreative(t *testing.T, p modes.Prepared, excluded []string) modes.Prepared {
	t.Helper()
	var err error
	p.Public, err = CloseWriting(p.Public)
	if err != nil {
		t.Fatal(err)
	}
	p.Public, err = OpenVoting(p.Public, p.Private, excluded)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func sessionVote(t *testing.T, p modes.Prepared, states map[string]json.RawMessage, session, work string, audience bool) (modes.Prepared, modes.Decision) {
	t.Helper()
	subject := VoteSubject(session)
	in := modes.ApplyInput{Subject: subject, Action: voteAction(work), Public: p.Public, Private: p.Private, State: states[subject.ID]}
	d, err := ApplySessionVote(in, session, audience)
	if err != nil {
		t.Fatal(err)
	}
	if d.Accepted {
		states[subject.ID] = d.State
		p.Public, p.Private, err = New().CommitShared(in, d)
		if err != nil {
			t.Fatal(err)
		}
	}
	return p, d
}

func TestCreativeSeparateWindowsPrivacyAndPersonalVotes(t *testing.T) {
	p, states, seats := stagedCreative(t, 20)
	if New().RoundWindow(p.Config) != 90*time.Second {
		t.Fatal("writing must have its own 90 second clock")
	}
	pub, _ := decodePublic(p.Public)
	ids := pub.WorkIDs
	viewer := modes.Viewer{SessionID: "s1", Role: "player", Subject: seats[0]}
	for _, who := range []modes.Viewer{viewer, {Role: "spectator", SessionID: "audience"}, {Role: "host", CanManageRound: true}} {
		out := project(t, p, who, modes.PhaseCollecting, states)
		if len(out.Ballot) != 0 || len(out.Review) != 0 || out.Result != nil || len(out.Authors) != 0 {
			t.Fatal("writing exposed peers")
		}
		encoded, _ := json.Marshal(out)
		for _, secret := range []string{"voterSubjects", "votedSessions", "voterId", "\"s1\""} {
			if strings.Contains(string(encoded), secret) {
				t.Fatalf("credential storage field exposed: %s", secret)
			}
		}
	}
	_, d := sessionVote(t, p, states, "s1", ids[seats[1].ID], false)
	if d.Accepted || d.Code != CodeVoteClosed {
		t.Fatal("early vote accepted")
	}
	// A 40-combining-character input is normalized before the limit is applied.
	st, _ := decodeState(states[seats[0].ID])
	if countCodePoints(st.WorkText) != 41 || strings.Contains(st.WorkText, "\u0301") {
		t.Fatal("server did not normalize text")
	}
	var err error
	p.Public, err = CloseWriting(p.Public)
	if err != nil {
		t.Fatal(err)
	}
	if len(project(t, p, viewer, modes.PhaseLocked, states).Review) != 0 {
		t.Fatal("player received moderator works")
	}
	if len(project(t, p, modes.Viewer{Role: "host", CanManageRound: true}, modes.PhaseLocked, states).Review) != 3 {
		t.Fatal("host cannot review")
	}
	before := string(p.Public)
	if _, err := OpenVoting(p.Public, p.Private, []string{"unknown"}); err == nil {
		t.Fatal("unknown exclusion accepted")
	}
	if string(p.Public) != before {
		t.Fatal("failed review mutated state")
	}
	p.Public, err = OpenVoting(p.Public, p.Private, []string{ids[seats[2].ID]})
	if err != nil {
		t.Fatal(err)
	}
	blocked, _ := apply(t, p, seats[0], workAction("revision after freeze"), states[seats[0].ID])
	if blocked.Accepted || blocked.Code != CodeWorkClosed {
		t.Fatal("frozen work changed")
	}
}

func TestCreativeSessionPoolsOwnTeamAndAudienceBonusBoundary(t *testing.T) {
	p, states, seats := stagedCreative(t, 20)
	pub, _ := decodePublic(p.Public)
	ids := pub.WorkIDs
	p = openCreative(t, p, nil)
	for _, session := range []string{"s1", "s4"} {
		_, d := sessionVote(t, p, states, session, ids[seats[0].ID], false)
		if d.Accepted || d.Code != CodeOwnWork {
			t.Fatal("teammate could vote for shared work")
		}
		p, d = sessionVote(t, p, states, session, ids[seats[1].ID], false)
		if !d.Accepted {
			t.Fatal(d)
		}
	}
	var d modes.Decision
	p, d = sessionVote(t, p, states, "s2", ids[seats[0].ID], false)
	if !d.Accepted {
		t.Fatal(d)
	}
	_, d = sessionVote(t, p, states, "late-player", ids[seats[0].ID], false)
	if d.Accepted || d.Code != CodeNotOnRoster {
		t.Fatal("unfrozen player voted")
	}
	for _, session := range []string{"aud1", "aud2", "aud3"} {
		p, d = sessionVote(t, p, states, session, ids[seats[0].ID], true)
		if !d.Accepted {
			t.Fatal(d)
		}
	}
	_, d = sessionVote(t, p, states, "aud1", ids[seats[1].ID], true)
	if d.Accepted || d.Code != CodeAlreadyVoted {
		t.Fatal("duplicate session vote")
	}
	if _, err := New().Settle(modes.SettleInput{Public: p.Public, Private: p.Private, States: states, Phase: modes.PhaseCollecting}); err == nil {
		t.Fatal("open voting settled")
	}
	var err error
	p.Public, err = CloseVoting(p.Public)
	if err != nil {
		t.Fatal(err)
	}
	out := project(t, p, modes.Viewer{Role: "spectator", SessionID: "aud1"}, modes.PhaseRevealed, states)
	if out.Result.TeamVotes != 3 || out.Result.AudienceVotes != 3 || out.Result.Award.WorkIDs[0] != ids[seats[0].ID] || out.Result.CompetitiveBonus.WorkIDs[0] != ids[seats[1].ID] {
		t.Fatalf("pools/bonus mixed: %+v", out.Result)
	}
	scores, err := New().Settle(modes.SettleInput{Public: p.Public, Private: p.Private, States: states, Phase: modes.PhaseSettled})
	if err != nil {
		t.Fatal(err)
	}
	for _, score := range scores {
		want := 0
		if score.Subject.ID == seats[1].ID {
			want = 2000
		}
		if score.ScoreUnits != want {
			t.Fatalf("audience affected formal bonus: %+v", score)
		}
	}
	cancel := project(t, p, modes.Viewer{Role: "host", CanManageRound: true}, modes.PhaseCancelled, states)
	if cancel.Result != nil || len(cancel.Authors) > 0 || len(cancel.Ballot) > 0 || len(cancel.Review) > 0 {
		t.Fatal("cancellation revealed new material")
	}
	if _, err := New().Settle(modes.SettleInput{Public: p.Public, Private: p.Private, States: states, Phase: modes.PhaseCancelled}); err == nil {
		t.Fatal("cancelled round scored")
	}
}

func TestCreativeEmptyBallotAudienceDisabledAndRemovedAuthorPool(t *testing.T) {
	p, states, seats := stagedCreative(t, 20)
	pub, _ := decodePublic(p.Public)
	pub.Config.AllowAudienceVote = false
	p.Public = modeutil.Marshal(pub)
	removed := pub.WorkIDs[seats[0].ID]
	p = openCreative(t, p, []string{removed})
	_, d := sessionVote(t, p, states, "aud", pub.WorkIDs[seats[1].ID], true)
	if d.Accepted {
		t.Fatal("disabled audience cast a vote")
	}
	p, d = sessionVote(t, p, states, "s1", pub.WorkIDs[seats[1].ID], false)
	if !d.Accepted {
		t.Fatal("removed author lost participant vote")
	}
	st, _ := decodeState(d.State)
	if st.VoteKind != VoteTeam {
		t.Fatal("removed author became audience")
	}
	_, d = sessionVote(t, p, states, "s2", removed, false)
	if d.Accepted {
		t.Fatal("vote for removed work accepted")
	}
	empty, emptyStates, _ := stagedCreative(t, 20)
	raw, _ := decodePublic(empty.Public)
	excluded := []string{}
	for _, id := range raw.WorkIDs {
		excluded = append(excluded, id)
	}
	empty = openCreative(t, empty, excluded)
	var err error
	empty.Public, err = CloseVoting(empty.Public)
	if err != nil {
		t.Fatal(err)
	}
	out := project(t, empty, modes.Viewer{Role: "spectator"}, modes.PhaseRevealed, emptyStates)
	if out.Result.ValidWorks != 0 || out.Result.Award != nil || len(out.Authors) > 0 {
		t.Fatal("empty ballot invented award or author")
	}
	scores, err := New().Settle(modes.SettleInput{Public: empty.Public, Private: empty.Private, States: emptyStates, Phase: modes.PhaseSettled})
	if err != nil {
		t.Fatal(err)
	}
	for _, score := range scores {
		if score.ScoreUnits != 0 {
			t.Fatal("empty ballot awarded points")
		}
	}
}
