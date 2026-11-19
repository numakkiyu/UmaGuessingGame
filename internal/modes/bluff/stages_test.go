package bluff

import (
	"encoding/json"
	"github.com/bhcn/umaguessinggame/internal/modes"

	"strings"
	"testing"
)

func rawAction(t *testing.T, p modes.Prepared, subject string, a modes.ModeAction, state json.RawMessage) modes.Decision {
	t.Helper()
	d, e := New().Apply(modes.ApplyInput{Scope: scope("seed"), Subject: modes.Subject{ID: subject, Kind: modes.SubjectPlayer}, Action: a, Public: p.Public, Private: p.Private, State: state})
	if e != nil {
		t.Fatal(e)
	}
	return d
}
func TestExplicitStagesFreezeBallotAndProtectReview(t *testing.T) {
	p, _, priv := prepareRound(t, reviewedPack(3), "stages", `{}`, 3)
	next, st := commit(t, p, modes.Subject{ID: "p1", Kind: modes.SubjectPlayer}, decoyAction("编写中的答案"), nil)
	p = next
	if d := rawAction(t, p, "p2", voteAction(priv.TruthOptionID), nil); d.Accepted || d.Code != CodeVoteClosed {
		t.Fatalf("early vote %+v", d)
	}
	viewer := modes.Viewer{Subject: modes.Subject{ID: "p1", Kind: modes.SubjectPlayer}, Role: "player"}
	view := project(t, p, viewer, modes.PhaseCollecting, map[string]json.RawMessage{"p1": st})
	if len(view.Options) > 0 || len(view.ReviewOptions) > 0 || view.Reveal != nil {
		t.Fatal("writing leaked ballot")
	}
	pub, secret, e := CloseWriting(p.Public, p.Private, "seed")
	if e != nil {
		t.Fatal(e)
	}
	p.Public, p.Private = pub, secret
	if d := rawAction(t, p, "p2", decoyAction("迟到的答案"), nil); d.Accepted {
		t.Fatal("late text accepted")
	}
	view = project(t, p, viewer, modes.PhaseLocked, nil)
	if len(view.Options) > 0 || len(view.ReviewOptions) > 0 || view.Reveal != nil {
		t.Fatal("participant saw review")
	}
	viewer.CanManageRound = true
	view = project(t, p, viewer, modes.PhaseLocked, nil)
	if len(view.ReviewOptions) != 4 || view.Reveal != nil {
		t.Fatalf("manager review=%s", mustJSON(t, view))
	}
	for _, o := range view.ReviewOptions {
		if o.SelfAuthored {
			t.Fatal("review disclosed author")
		}
	}
	for _, ids := range [][]string{{priv.TruthOptionID}, {"unknown"}, {view.ReviewOptions[0].OptionID, view.ReviewOptions[0].OptionID}} {
		if _, _, e := OpenVoting(pub, secret, "seed", ids); e == nil {
			t.Fatalf("bad exclusions accepted %v", ids)
		}
	}
	dropped := optionIDFor(decoyIDFor(normalizeDecoy("编写中的答案")))
	p.Public, p.Private, e = OpenVoting(pub, secret, "seed", []string{dropped})
	if e != nil {
		t.Fatal(e)
	}
	if d := rawAction(t, p, "p2", voteAction(dropped), nil); d.Accepted || d.Code != CodeUnknownOption {
		t.Fatal("removed option votable")
	}
	viewer.CanManageRound = false
	view = project(t, p, viewer, modes.PhaseCollecting, map[string]json.RawMessage{"p1": st})
	if !view.Self.DecoyRemoved || len(view.Options) != 3 || view.Reveal != nil {
		t.Fatal("removal projection")
	}
	for _, phase := range []modes.Phase{modes.PhaseCollecting, modes.PhaseLocked, modes.PhaseCancelled} {
		v := project(t, p, viewer, phase, nil)
		if v.Reveal != nil {
			t.Fatalf("reveal in %s", phase)
		}
	}
}
func TestTruthSubmitterCannotVoteAndDoesNotReduceDeception(t *testing.T) {
	p, _, priv := prepareRound(t, reviewedPack(3), "truth-no-vote", `{}`, 3)
	states := map[string]json.RawMessage{}
	for i, text := range []string{priv.Truth, "别人编的答案", "另一份答案"} {
		id := roster(3)[i].ID
		p, states[id] = commit(t, p, modes.Subject{ID: id, Kind: modes.SubjectPlayer}, decoyAction(text), nil)
	}
	p = openVotingFixture(t, p)
	if d := rawAction(t, p, "p1", voteAction(priv.TruthOptionID), states["p1"]); d.Accepted || d.Code != "truth_already_identified" {
		t.Fatal("truth submitter voted")
	}
	option := optionIDFor(decoyIDFor(normalizeDecoy("别人编的答案")))
	for id, choice := range map[string]string{"p2": priv.TruthOptionID, "p3": option} {
		d := rawAction(t, p, id, voteAction(choice), states[id])
		if !d.Accepted {
			t.Fatal(d.Code)
		}
		states[id] = d.State
	}
	complete, e := VotingComplete(p.Public, p.Private, states)
	if e != nil || !complete {
		t.Fatal("truth submitter blocked closure", e)
	}
	scores, e := New().Settle(modes.SettleInput{Public: p.Public, Private: p.Private, States: states})
	if e != nil {
		t.Fatal(e)
	}
	for _, s := range scores {
		if s.Subject.ID == "p2" && s.ScoreUnits != 10000 {
			t.Fatalf("eligible voter denominator %d", s.ScoreUnits)
		}
	}
}
func TestReviewedFallbacksOnlyAndLegacyFailsClosed(t *testing.T) {
	p, _, _ := prepareRound(t, reviewedPack(3), "fallback", `{}`, 3)
	pub, priv, e := CloseWriting(p.Public, p.Private, "seed")
	if e != nil {
		t.Fatal(e)
	}
	var state publicState
	json.Unmarshal(pub, &state)
	if len(state.Options) != 4 {
		t.Fatal("missing fallback", string(pub))
	}
	var secret privateState
	json.Unmarshal(priv, &secret)
	for _, d := range secret.Decoys {
		if len(d.AuthorIDs) != 0 {
			t.Fatal("fallback credited to player")
		}
	}
	// No fallback means no invented answer, and a one-option ballot cannot start.
	json.Unmarshal(p.Private, &secret)
	secret.FallbackAnswers = nil
	p.Private = mustJSON(t, secret)
	pub, priv, e = CloseWriting(p.Public, p.Private, "seed")
	if e != nil {
		t.Fatal(e)
	}
	if _, _, e = OpenVoting(pub, priv, "seed", nil); e == nil {
		t.Fatal("one-option ballot opened")
	}
	json.Unmarshal(p.Public, &state)
	state.Subphase = ""
	p.Public = mustJSON(t, state)
	if d := rawAction(t, p, "p1", decoyAction("旧版答案"), nil); d.Accepted {
		t.Fatal("legacy write accepted")
	}
	if d := rawAction(t, p, "p1", voteAction(secret.TruthOptionID), nil); d.Accepted {
		t.Fatal("legacy vote accepted")
	}
	if v := project(t, p, modes.Viewer{Role: "player"}, modes.PhaseCollecting, nil); v.Reveal != nil || len(v.Options) > 0 {
		t.Fatal("legacy leaked")
	}
}
func TestFallbackLoaderAndUnicodeEquality(t *testing.T) {
	p, _, priv := prepareRound(t, reviewedPack(3), "loader", `{}`, 3)
	if len(priv.FallbackAnswers) != 8 {
		t.Fatal("loader lost arrays")
	}
	if normalizeDecoy(" Ａ e\u0301  ") != "a é" {
		t.Fatal("normalisation")
	}
	_ = p
}

func TestFallbackNFCMatchesCompilerLength(t *testing.T) {
	p, _, secret := prepareRound(t, reviewedPack(3), "nfc-fallback", `{}`, 3)
	secret.FallbackAnswers = []string{strings.Repeat("e\u0301", 40)}
	p.Private = mustJSON(t, secret)
	pub, _, e := CloseWriting(p.Public, p.Private, "seed")
	if e != nil {
		t.Fatal(e)
	}
	var state publicState
	json.Unmarshal(pub, &state)
	if len(state.Options) != 2 {
		t.Fatal("valid composed fallback dropped")
	}
	for _, o := range state.Options {
		if o.OptionID != secret.TruthOptionID && countCodePoints(o.Text) != 40 {
			t.Fatal("fallback not normalised")
		}
	}
}
