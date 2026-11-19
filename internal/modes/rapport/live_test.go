package rapport

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"testing"
	"time"

	"github.com/bhcn/umaguessinggame/internal/modes"
)

func liveFixture(t *testing.T, sizes []int, config string) (modes.PrepareInput, modes.Prepared, publicState) {
	t.Helper()
	in := modes.PrepareInput{Scope: scope("live-seed"), Config: json.RawMessage(config), Content: packOf()}
	for team, size := range sizes {
		for j := 0; j < size; j++ {
			id := fmt.Sprintf("p-%d-%d", team, j)
			sub := modes.Subject{ID: id, Kind: modes.SubjectPlayer}
			p := modes.Participant{MembershipID: id, Subject: sub}
			if len(sizes) > 1 {
				p.TeamID = fmt.Sprintf("t-%d", team)
			}
			in.Subject = append(in.Subject, sub)
			in.Participants = append(in.Participants, p)
		}
	}
	prep, err := New().Prepare(context.Background(), in)
	if err != nil {
		t.Fatal(err)
	}
	pub, err := decodePublic(prep.Public)
	if err != nil {
		t.Fatal(err)
	}
	return in, prep, pub
}
func liveSubmit(t *testing.T, prep *modes.Prepared, states map[string]json.RawMessage, id, option string) modes.Decision {
	t.Helper()
	in := modes.ApplyInput{Public: prep.Public, State: states[id], MembershipID: id, Subject: modes.Subject{ID: id, Kind: modes.SubjectPlayer}, Action: answerAction(option)}
	decision, err := New().Apply(in)
	if err != nil {
		t.Fatal(err)
	}
	if decision.Accepted {
		public, _, err := New().CommitShared(in, decision)
		if err != nil {
			t.Fatal(err)
		}
		states[id] = decision.State
		prep.Public = public
	}
	return decision
}
func liveProject(t *testing.T, prep modes.Prepared, states map[string]json.RawMessage, id string, phase modes.Phase) projection {
	t.Helper()
	raw, err := New().Project(modes.ProjectInput{Public: prep.Public, States: states, Phase: phase, Viewer: modes.Viewer{MembershipID: id, Subject: modes.Subject{ID: id, Kind: modes.SubjectPlayer}}})
	if err != nil {
		t.Fatal(err)
	}
	var out projection
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatal(err)
	}
	return out
}
func TestLiveIndividualTurnsPrivacyAndIndependentTeams(t *testing.T) {
	_, prep, pub := liveFixture(t, []int{2, 2}, `{"questions":2}`)
	states := map[string]json.RawMessage{}
	if New().TurnWindow(prep.Config) != 20*time.Second {
		t.Fatal("normalized config lost turn window")
	}
	if !New().IndividualSeatsForConfig(prep.Config) || New().IndividualSeatsForConfig(json.RawMessage(`{}`)) {
		t.Fatal("old round compatibility")
	}
	initial := liveProject(t, prep, states, "p-0-0", modes.PhaseCollecting)
	if len(initial.Questions) != 1 || len(initial.Groups) != 2 || initial.TotalSlots != 2 || !initial.CanAnswer {
		t.Fatalf("first question: %+v", initial)
	}
	future := pub.Questions[1].Options[0].OptionID
	if liveSubmit(t, &prep, states, "p-0-0", future).Accepted {
		t.Fatal("future answer accepted")
	}
	forged, err := New().Apply(modes.ApplyInput{Public: prep.Public, MembershipID: "p-0-0", Subject: modes.Subject{ID: "p-0-1", Kind: modes.SubjectPlayer}, Action: answerAction(pub.Questions[0].Options[0].OptionID)})
	if err != nil || forged.Accepted {
		t.Fatal("forged member accepted")
	}
	for qi, q := range pub.Questions {
		option := q.Options[0].OptionID
		for _, id := range pub.Live.Groups[0].Members {
			if !liveSubmit(t, &prep, states, id, option).Accepted {
				t.Fatal("team answer rejected")
			}
		}
		own := liveProject(t, prep, states, "p-0-0", modes.PhaseCollecting)
		other := liveProject(t, prep, states, "p-1-0", modes.PhaseCollecting)
		if own.Questions[qi].Reveal != nil || other.Questions[qi].Reveal != nil || own.CurrentQuestion != qi+1 || other.Questions[qi].SelfOptionID != "" {
			t.Fatal("team locked early or leaked picks")
		}
		if liveSubmit(t, &prep, states, "p-0-0", option).Accepted {
			t.Fatal("duplicate accepted")
		}
		for _, id := range pub.Live.Groups[1].Members {
			pick := q.Options[1].OptionID
			if id == pub.Live.Groups[1].Choosers[qi] {
				pick = option
			}
			if !liveSubmit(t, &prep, states, id, pick).Accepted {
				t.Fatal("second team rejected")
			}
		}
		if qi == 0 {
			view := liveProject(t, prep, states, "p-0-0", modes.PhaseCollecting)
			if !view.Questions[0].Locked || view.Questions[0].Reveal == nil || len(view.Questions) != 2 || view.Questions[0].SelfOptionID != option {
				t.Fatal("turn did not advance")
			}
			if liveSubmit(t, &prep, states, "p-0-0", option).Accepted {
				t.Fatal("old question accepted")
			}
		}
	}
	scores, err := New().Settle(modes.SettleInput{Public: prep.Public, States: states})
	if err != nil {
		t.Fatal(err)
	}
	if len(scores) != 2 {
		t.Fatalf("team scores %+v", scores)
	}
	for _, s := range scores {
		want := 0
		if s.Subject.ID == "t-0" {
			want = 10000
		}
		if s.ScoreUnits != want {
			t.Fatalf("wrong group score %+v", s)
		}
	}
	observer := liveProject(t, prep, states, "host", modes.PhaseSettled)
	if observer.Self != nil || observer.CanAnswer || len(observer.Final) != 2 {
		t.Fatal("observer became participant")
	}
	for _, q := range observer.Questions {
		if q.Reveal != nil || q.SelfOptionID != "" {
			t.Fatal("observer reads private picks")
		}
	}
}
func TestLiveTimeoutFixedDenominatorAndRotation(t *testing.T) {
	in, prep, pub := liveFixture(t, []int{6}, `{}`)
	if len(pub.Questions) != 6 {
		t.Fatal("default questions failed to cover rotation")
	}
	again, err := New().Prepare(context.Background(), in)
	if err != nil || !reflect.DeepEqual(again, prep) {
		t.Fatal("non deterministic")
	}
	seen := map[string]bool{}
	for _, id := range pub.Live.Groups[0].Choosers {
		seen[id] = true
	}
	if len(seen) != 6 {
		t.Fatal("not everyone chooses")
	}
	states := map[string]json.RawMessage{}
	// Everyone predicts the same value, but the chooser misses the deadline.
	for _, id := range pub.Live.Groups[0].Members {
		if id != pub.Live.Groups[0].Choosers[0] {
			liveSubmit(t, &prep, states, id, pub.Questions[0].Options[0].OptionID)
		}
	}
	raw, more, err := New().ExpireTurn(prep.Public)
	if err != nil || !more {
		t.Fatal("timeout")
	}
	prep.Public = raw
	view := liveProject(t, prep, states, "p-0-0", modes.PhaseCollecting)
	if view.TotalSlots != 30 || view.Questions[0].Reveal == nil || view.Questions[0].Reveal.ChosenOptionID != "" {
		t.Fatal("missing chooser forged or denominator changed")
	}
	for i := 1; i < 6; i++ {
		raw, _, err = New().ExpireTurn(prep.Public)
		if err != nil {
			t.Fatal(err)
		}
		prep.Public = raw
	}
	if _, more, err = New().OpenTurn(prep.Public); err != nil || more {
		t.Fatal("last turn still open")
	}
	scores, err := New().Settle(modes.SettleInput{Public: prep.Public, States: states})
	if err != nil || len(scores) != 6 {
		t.Fatal("party shares score")
	}
	for _, s := range scores {
		if s.ScoreUnits != 0 {
			t.Fatal("missing chooser counted as hit")
		}
	}
	in.Config = json.RawMessage(`{"questions":5}`)
	if _, err := New().Prepare(context.Background(), in); err == nil {
		t.Fatal("explicit insufficient questions accepted")
	}
	in.Config = json.RawMessage(`{"modeId":"rapport","config":{"questions":6,"questionSeconds":12}}`)
	enveloped, err := New().Prepare(context.Background(), in)
	if err != nil || New().TurnWindow(enveloped.Config) != 12*time.Second {
		t.Fatalf("envelope config ignored: %v", err)
	}
}
func TestLiveRejectsUnbalancedOrMixedRoster(t *testing.T) {
	in, _, _ := liveFixture(t, []int{2, 2}, `{}`)
	in.Participants[0].TeamID = ""
	if New().ValidateRoster(in.Subject, in.Participants) == nil {
		t.Fatal("mixed roster")
	}
	in.Participants[0].TeamID = "t-1"
	if New().ValidateRoster(in.Subject, in.Participants) == nil {
		t.Fatal("one member team")
	}
}

func TestLiveCancellationKeepsOnlyActuallyClosedHistory(t *testing.T) {
	_, prep, pub := liveFixture(t, []int{2, 3}, `{"questions":3}`)
	states := map[string]json.RawMessage{}
	for _, p := range pub.Subjects {
		liveSubmit(t, &prep, states, p.SubjectID, pub.Questions[0].Options[0].OptionID)
	}
	// Only the chooser in group 0 answers question 2. Cancellation must not
	// convert that still-private preference into a public answer.
	chooser := pub.Live.Groups[0].Choosers[1]
	liveSubmit(t, &prep, states, chooser, pub.Questions[1].Options[1].OptionID)
	for _, phase := range []modes.Phase{modes.PhaseCancelled, modes.PhaseLocked, modes.PhaseRevealed, modes.PhaseSettled} {
		v := liveProject(t, prep, states, pub.Live.Groups[0].Members[0], phase)
		if len(v.Questions) != 2 || !v.Questions[0].Locked || v.Questions[0].Reveal == nil || v.Questions[1].Locked || v.Questions[1].Reveal != nil || len(v.Final) != 0 || v.CanAnswer {
			t.Fatalf("unfinished phase %s leaked or scored: %+v", phase, v)
		}
		other := liveProject(t, prep, states, "p-1-0", phase)
		if other.Questions[1].SelfOptionID != "" || other.Questions[1].Reveal != nil {
			t.Fatal("other group saw private choice")
		}
		observer := liveProject(t, prep, states, "host", phase)
		if observer.Self != nil || observer.CanAnswer {
			t.Fatal("observer has personal seat")
		}
		for _, q := range observer.Questions {
			if q.Reveal != nil || q.SelfOptionID != "" {
				t.Fatal("observer reads group history")
			}
		}
	}
	if _, err := New().Settle(modes.SettleInput{Public: prep.Public, States: states}); err == nil {
		t.Fatal("settled unfinished questions")
	}
	// Group-specific denominators are announced, even for unequal balanced teams.
	v := liveProject(t, prep, states, "p-1-0", modes.PhaseCollecting)
	if v.TotalSlots != 6 || len(v.Groups) != 2 || v.Groups[0].TotalSlots != 3 || v.Groups[1].TotalSlots != 6 {
		t.Fatalf("fixed slots: %+v", v)
	}
	for i := 1; i < 3; i++ {
		var err error
		prep.Public, _, err = New().ExpireTurn(prep.Public)
		if err != nil {
			t.Fatal(err)
		}
	}
	cancelled := liveProject(t, prep, states, "p-0-0", modes.PhaseCancelled)
	if len(cancelled.Final) != 0 {
		t.Fatal("completed but cancelled round has scores")
	}
	if _, err := New().Settle(modes.SettleInput{Public: prep.Public, States: states, Phase: modes.PhaseCancelled}); err == nil {
		t.Fatal("settled cancelled round")
	}
}

func TestLegacyCancellationDoesNotRevealAnUnfinishedPreference(t *testing.T) {
	in := modes.PrepareInput{Scope: scope("legacy-cancel"), Content: packOf(), Config: json.RawMessage(`{"questions":2}`), Subject: []modes.Subject{{ID: "a", Kind: modes.SubjectPlayer}, {ID: "b", Kind: modes.SubjectPlayer}}}
	prep, err := New().Prepare(context.Background(), in)
	if err != nil {
		t.Fatal(err)
	}
	pub, err := decodePublic(prep.Public)
	if err != nil {
		t.Fatal(err)
	}
	q := pub.Questions[0]
	chooser := modes.Subject{ID: q.ChooserID, Kind: modes.SubjectPlayer}
	dec, err := New().Apply(modes.ApplyInput{Public: prep.Public, Subject: chooser, Action: answerAction(q.Options[0].OptionID)})
	if err != nil || !dec.Accepted {
		t.Fatal(dec, err)
	}
	raw, err := New().Project(modes.ProjectInput{Public: prep.Public, States: map[string]json.RawMessage{chooser.ID: dec.State}, Phase: modes.PhaseCancelled, Viewer: modes.Viewer{IsReferee: true}, RevealNow: true})
	if err != nil {
		t.Fatal(err)
	}
	var view projection
	if err := json.Unmarshal(raw, &view); err != nil {
		t.Fatal(err)
	}
	if len(view.Final) > 0 {
		t.Fatal("legacy cancellation fabricated scores")
	}
	for _, question := range view.Questions {
		if question.Reveal != nil || question.Locked {
			t.Fatal("cancel revealed unfinished legacy preference")
		}
	}
}
