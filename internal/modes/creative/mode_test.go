package creative

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"testing"

	"github.com/bhcn/umaguessinggame/internal/modes"
)

// fakeContent is a synthetic compiled pack section store.
type fakeContent map[string]json.RawMessage

func (f fakeContent) Section(name string) (json.RawMessage, bool) {
	raw, ok := f[name]
	return raw, ok
}

func (f fakeContent) Version() string { return "test-pack-1" }

// fixtureProvenance is the provenance block every fixture prompt carries.
func fixtureProvenance() map[string]any {
	return map[string]any{
		"kind":            ProvenanceAuthored,
		"notOfficialData": true,
		"author":          "test-fixture",
		"basis":           "docs/玩法与规则.md (creative)",
	}
}

// fixtureScenes and fixtureConstraints are the authored pools the fixture publishes.
//
// They are deliberately NOT the strings the mode used to hard-code: if the mode still had
// built-in pools, these tests would pass against them and never exercise the pack path.
var (
	fixtureScenes = []string{
		"测试场景一：下雨天的校门口", "测试场景二：空掉的跑道", "测试场景三：食堂快关门",
		"测试场景四：走廊尽头", "测试场景五：等末班车", "测试场景六：雪停之后",
		"测试场景七：午休的社团室", "测试场景八：半夜的便利店",
	}
	fixtureConstraints = []string{
		"测试限制一：不能出现感叹号", "测试限制二：必须包含一个数字",
		"测试限制三：只能写一句话", "测试限制四：必须以问题结尾",
	}
)

// fixtureCharacters is the public name index the fixture publishes.
var fixtureCharacters = []struct{ ID, Name string }{
	{"c001", "角色一"}, {"c002", "角色二"}, {"c003", "角色三"}, {"c004", "角色四"},
	{"c005", "角色五"}, {"c006", "角色六"}, {"c007", "角色七"}, {"c008", "角色八"},
}

// packOf builds a content source carrying the fixture pools and name index.
func packOf() modes.ContentSource {
	return packWith(packOptions{})
}

// packOptions tunes what the fixture publishes, so a test can drop or spoil a section.
type packOptions struct {
	omitPrompts    bool
	omitCharacters bool
	omitProvenance bool
	badProvenance  string
	scenes         []string
	constraints    []string
	characters     []struct{ ID, Name string }
}

// packWith builds a pack from options.
func packWith(o packOptions) modes.ContentSource {
	scenes := o.scenes
	if scenes == nil {
		scenes = fixtureScenes
	}
	constraints := o.constraints
	if constraints == nil {
		constraints = fixtureConstraints
	}
	characters := o.characters
	if characters == nil {
		characters = fixtureCharacters
	}

	out := fakeContent{}
	if !o.omitPrompts {
		sceneDocs := make([]map[string]any, 0, len(scenes))
		for i, sc := range scenes {
			entry := map[string]any{
				"sceneId": fmt.Sprintf("sc-%02d", i+1),
				"text":    sc,
			}
			if !o.omitProvenance {
				if o.badProvenance != "" {
					entry["provenance"] = json.RawMessage(o.badProvenance)
				} else {
					entry["provenance"] = fixtureProvenance()
				}
			}
			sceneDocs = append(sceneDocs, entry)
		}
		constraintDocs := make([]map[string]any, 0, len(constraints))
		for i, ct := range constraints {
			entry := map[string]any{
				"constraintId": fmt.Sprintf("ct-%02d", i+1),
				"text":         ct,
			}
			if !o.omitProvenance {
				if o.badProvenance != "" {
					entry["provenance"] = json.RawMessage(o.badProvenance)
				} else {
					entry["provenance"] = fixtureProvenance()
				}
			}
			constraintDocs = append(constraintDocs, entry)
		}
		raw, err := json.Marshal(map[string]any{
			"scenes":      sceneDocs,
			"constraints": constraintDocs,
		})
		if err != nil {
			panic(err)
		}
		out[PromptsSection] = raw
	}
	if !o.omitCharacters {
		docs := make([]map[string]any, 0, len(characters))
		for _, c := range characters {
			docs = append(docs, map[string]any{"id": c.ID, "nameZh": c.Name})
		}
		raw, err := json.Marshal(docs)
		if err != nil {
			panic(err)
		}
		out[CharactersSection] = raw
	}
	return out
}

// scope builds a deterministic round scope.
func scope(seed string) modes.Scope {
	return modes.Scope{
		ModeID:             ModeID,
		RulesVersion:       RulesVersion,
		DatasetVersion:     "ds-test",
		ContentPackVersion: "test-pack-1",
		Seed:               seed,
	}
}

// roster builds n player subjects with stable ids.
func roster(n int) []modes.Subject {
	out := make([]modes.Subject, 0, n)
	for i := 0; i < n; i++ {
		out = append(out, modes.Subject{
			ID:   fmt.Sprintf("p%d", i+1),
			Kind: modes.SubjectPlayer,
		})
	}
	return out
}

// prepareRound runs Prepare and returns the prepared round with decoded halves.
func prepareRound(t *testing.T, seed, cfg string, players int) (modes.Prepared, publicState, privateState) {
	t.Helper()
	prep, err := New().Prepare(context.Background(), modes.PrepareInput{
		Scope:   scope(seed),
		Subject: roster(players),
		Config:  json.RawMessage(cfg),
		Content: packOf(),
	})
	if err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	var pub publicState
	if err := json.Unmarshal(prep.Public, &pub); err != nil {
		t.Fatalf("decode public: %v", err)
	}
	var priv privateState
	if err := json.Unmarshal(prep.Private, &priv); err != nil {
		t.Fatalf("decode private: %v", err)
	}
	return prep, pub, priv
}

// workAction builds a contract-shaped creative.work payload.
func workAction(text string) modes.ModeAction {
	body, err := json.Marshal(struct {
		Kind string `json:"kind"`
		Text string `json:"text"`
	}{Kind: ActionKindWork, Text: text})
	if err != nil {
		panic(err)
	}
	return modes.ModeAction{Kind: ActionKindWork, Payload: body}
}

// voteAction builds a contract-shaped creative.vote payload.
func voteAction(workID string) modes.ModeAction {
	body, err := json.Marshal(struct {
		Kind     string `json:"kind"`
		OptionID string `json:"optionId"`
	}{Kind: ActionKindVote, OptionID: workID})
	if err != nil {
		panic(err)
	}
	return modes.ModeAction{Kind: ActionKindVote, Payload: body}
}

// apply runs Apply against the current state.
func apply(t *testing.T, prep modes.Prepared, subject modes.Subject, action modes.ModeAction, state json.RawMessage) (modes.Decision, json.RawMessage) {
	t.Helper()
	dec, err := New().Apply(modes.ApplyInput{
		Scope:   scope("seed"),
		Subject: subject,
		Action:  action,
		Public:  prep.Public,
		Private: prep.Private,
		State:   state,
	})
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	return dec, dec.State
}

// commit applies then commits one action, returning the new prepared round and the
// caller's new state. It mirrors the room layer's transaction order.
func commit(t *testing.T, prep modes.Prepared, subject modes.Subject, action modes.ModeAction, state json.RawMessage) (modes.Prepared, json.RawMessage) {
	t.Helper()
	in := modes.ApplyInput{
		Scope:   scope("seed"),
		Subject: subject,
		Action:  action,
		Public:  prep.Public,
		Private: prep.Private,
		State:   state,
	}
	dec, err := New().Apply(in)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if !dec.Accepted {
		t.Fatalf("action rejected: code=%q detail=%q", dec.Code, dec.Detail)
	}
	pub, priv, err := New().CommitShared(in, dec)
	if err != nil {
		t.Fatalf("CommitShared: %v", err)
	}
	return modes.Prepared{Public: pub, Private: priv, QuestionCount: prep.QuestionCount}, dec.State
}

// project decodes the projection for a viewer.
func project(t *testing.T, prep modes.Prepared, viewer modes.Viewer, phase modes.Phase, states map[string]json.RawMessage) projection {
	t.Helper()
	if states == nil {
		states = map[string]json.RawMessage{}
	}
	raw, err := New().Project(modes.ProjectInput{
		Scope:   scope("seed"),
		Viewer:  viewer,
		Public:  prep.Public,
		Private: prep.Private,
		States:  states,
		Phase:   phase,
	})
	if err != nil {
		t.Fatalf("Project: %v", err)
	}
	var out projection
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("decode projection: %v", err)
	}
	return out
}

// settle runs Settle.
func settle(t *testing.T, prep modes.Prepared, states map[string]json.RawMessage) map[string]modes.Score {
	t.Helper()
	if states == nil {
		states = map[string]json.RawMessage{}
	}
	scores, err := New().Settle(modes.SettleInput{
		Scope:   scope("seed"),
		Phase:   modes.PhaseSettled,
		Public:  prep.Public,
		Private: prep.Private,
		States:  states,
	})
	if err != nil {
		t.Fatalf("Settle: %v", err)
	}
	out := make(map[string]modes.Score, len(scores))
	for _, score := range scores {
		if err := score.Validate(); err != nil {
			t.Fatalf("invalid score for %s: %v", score.Subject.ID, err)
		}
		out[score.Subject.ID] = score
	}
	return out
}

// submitAll drives every participant through a pitch.
func submitAll(t *testing.T, prep modes.Prepared, pub publicState) (modes.Prepared, map[string]json.RawMessage) {
	t.Helper()
	states := map[string]json.RawMessage{}
	for _, entry := range pub.Subjects {
		text := fmt.Sprintf("%s 的企划：%s，%s。",
			entry.SubjectID,
			pub.Prompts[entry.SubjectID].Scene,
			pub.Prompts[entry.SubjectID].Constraint)
		next, state := commit(t, prep, modes.Subject{ID: entry.SubjectID, Kind: modes.SubjectPlayer},
			workAction(text), states[entry.SubjectID])
		prep = next
		states[entry.SubjectID] = state
	}
	return prep, states
}

// TestAnonymousBallotHasNoAuthor covers the 「匿名」 bullet.
//
// The projection must not pair a work id with an author, in any field, before the
// round is terminal.
func TestAnonymousBallotHasNoAuthor(t *testing.T) {
	prep, pub, priv := prepareRound(t, "anon-seed", `{}`, 3)
	prep, states := submitAll(t, prep, pub)

	view := project(t, prep, modes.Viewer{
		MembershipID: "m1", Role: "player",
		Subject: modes.Subject{ID: "p1", Kind: modes.SubjectPlayer},
	}, modes.PhaseLocked, states)

	if len(view.Ballot) != 3 {
		t.Fatalf("ballot has %d works, want 3", len(view.Ballot))
	}
	if view.Authors != nil {
		t.Fatalf("the ballot exposes the author map before the reveal: %v", view.Authors)
	}
	// The ballot entry struct has no author field, so the check is that no author
	// id appears in the serialised ballot.
	raw := string(mustJSON(t, view.Ballot))
	for _, entry := range pub.Subjects {
		if strings.Contains(raw, `"`+entry.SubjectID+`"`) {
			t.Fatalf("the ballot carries the author id %q", entry.SubjectID)
		}
	}
	// Each work maps to its author in the private state only.
	for _, entry := range view.Ballot {
		authorID, ok := priv.AuthorOf[entry.WorkID]
		if !ok {
			t.Fatalf("work %q has no author in the private state", entry.WorkID)
		}
		if authorID == "" {
			t.Fatalf("work %q maps to an empty author", entry.WorkID)
		}
	}
	// The ballot order must not follow the roster order, which would attribute by
	// position.
	workIDs := make([]string, 0, len(view.Ballot))
	for _, entry := range view.Ballot {
		workIDs = append(workIDs, entry.WorkID)
	}
	sorted := append([]string{}, workIDs...)
	sort.Strings(sorted)
	if strings.Join(workIDs, ",") != strings.Join(sorted, ",") {
		t.Fatalf("the ballot is not in a neutral order: %v", workIDs)
	}
}

// TestCannotVoteOwnWork covers the 「不能投自己」 bullet.
func TestCannotVoteOwnWork(t *testing.T) {
	prep, pub, _ := prepareRound(t, "own-vote-seed", `{}`, 3)
	prep, states := submitAll(t, prep, pub)

	author := modes.Subject{ID: "p1", Kind: modes.SubjectPlayer}
	ownWorkID := pub.WorkIDs["p1"]
	dec, _ := apply(t, prep, author, voteAction(ownWorkID), states["p1"])
	if dec.Accepted {
		t.Fatal("an author voted for their own work")
	}
	if dec.Code != CodeOwnWork {
		t.Fatalf("code = %q, want %q", dec.Code, CodeOwnWork)
	}

	// Another team's work is accepted.
	otherWorkID := pub.WorkIDs["p2"]
	dec, _ = apply(t, prep, author, voteAction(otherWorkID), states["p1"])
	if !dec.Accepted {
		t.Fatalf("voting for another work was refused: %q %q", dec.Code, dec.Detail)
	}
}

// TestDuplicateVoteRefused covers the 「重复票」 bullet.
func TestDuplicateVoteRefused(t *testing.T) {
	prep, pub, _ := prepareRound(t, "dup-vote-seed", `{}`, 3)
	prep, states := submitAll(t, prep, pub)

	voter := modes.Subject{ID: "p1", Kind: modes.SubjectPlayer}
	dec, state := apply(t, prep, voter, voteAction(pub.WorkIDs["p2"]), states["p1"])
	if !dec.Accepted {
		t.Fatalf("first ballot refused: %q %q", dec.Code, dec.Detail)
	}

	// A second ballot, even for a different work, is refused: the ballot is final.
	dec, _ = apply(t, prep, voter, voteAction(pub.WorkIDs["p3"]), state)
	if dec.Accepted {
		t.Fatal("a second ballot was accepted")
	}
	if dec.Code != CodeAlreadyVoted {
		t.Fatalf("code = %q, want %q", dec.Code, CodeAlreadyVoted)
	}
}

// TestDeviceCountIsNotPersonCount covers the 「合并设备」 and 「跨会话独立票权」 bullets.
//
// A vote is bound to the membership the room resolved. Two tabs, two devices and two
// sockets for one membership are one vote; a different membership is a different
// vote even from the same device. The rule layer sees only the membership id, which
// is exactly the identity the room guarantees.
func TestDeviceCountIsNotPersonCount(t *testing.T) {
	prep, pub, _ := prepareRound(t, "device-seed", `{}`, 3)
	prep, states := submitAll(t, prep, pub)

	voter := modes.Subject{ID: "p1", Kind: modes.SubjectPlayer}
	target := pub.WorkIDs["p2"]

	// First ballot from device A.
	dec, state := apply(t, prep, voter, voteAction(target), states["p1"])
	if !dec.Accepted {
		t.Fatalf("first ballot refused: %q %q", dec.Code, dec.Detail)
	}
	// Second ballot from device B: the room resolves the same membership, so the
	// persisted state already says Voted and the ballot is refused.
	dec, _ = apply(t, prep, voter, voteAction(target), state)
	if dec.Accepted {
		t.Fatal("the same membership voted twice from two devices")
	}
	if dec.Code != CodeAlreadyVoted {
		t.Fatalf("code = %q, want %q", dec.Code, CodeAlreadyVoted)
	}

	// A different membership is a separate vote, which is the documented rule: one
	// vote per participating session, not one per device.
	other := modes.Subject{ID: "p3", Kind: modes.SubjectPlayer}
	dec, _ = apply(t, prep, other, voteAction(target), states["p3"])
	if !dec.Accepted {
		t.Fatalf("a different membership was refused: %q %q", dec.Code, dec.Detail)
	}
}

// TestTeamAndAudienceVotesAreSeparate covers the 「队票与观众票分开」 bullet.
//
// A participant who submitted a work casts a team vote; a participant who did not
// casts an audience vote. The client cannot choose its own weight.
func TestTeamAndAudienceVotesAreSeparate(t *testing.T) {
	prep, pub, _ := prepareRound(t, "pools-seed", `{}`, 3)
	// Only p1 and p2 pitch, so p3 is an audience voter.
	states := map[string]json.RawMessage{}
	for _, id := range []string{"p1", "p2"} {
		next, state := commit(t, prep, modes.Subject{ID: id, Kind: modes.SubjectPlayer},
			workAction("一段企划 "+id), states[id])
		prep = next
		states[id] = state
	}

	// p1 (a team) votes for p2's work; p3 (audience) votes for the same work.
	target := pub.WorkIDs["p2"]
	next, state := commit(t, prep, modes.Subject{ID: "p1", Kind: modes.SubjectPlayer},
		voteAction(target), states["p1"])
	prep = next
	states["p1"] = state

	next, state = commit(t, prep, modes.Subject{ID: "p3", Kind: modes.SubjectPlayer},
		voteAction(target), states["p3"])
	prep = next
	states["p3"] = state

	view := project(t, prep, modes.Viewer{MembershipID: "m1", Role: "player",
		Subject: modes.Subject{ID: "p1", Kind: modes.SubjectPlayer}}, modes.PhaseSettled, states)
	if view.Result == nil {
		t.Fatal("the settled projection carries no tally")
	}
	if view.Result.TeamVotes != 1 {
		t.Fatalf("teamVotes = %d, want 1", view.Result.TeamVotes)
	}
	if view.Result.AudienceVotes != 1 {
		t.Fatalf("audienceVotes = %d, want 1", view.Result.AudienceVotes)
	}
	for _, work := range view.Result.Works {
		if work.WorkID != target {
			continue
		}
		if work.TeamVotes != 1 || work.AudienceVotes != 1 || work.Total != 2 {
			t.Fatalf("tally for the target work = %+v", work)
		}
	}
}

// TestAwardDisabledByDefault covers the 「默认不纳入竞技总分」 bullet.
func TestAwardDisabledByDefault(t *testing.T) {
	prep, pub, _ := prepareRound(t, "default-award-seed", `{}`, 3)
	prep, states := submitAll(t, prep, pub)
	// p1 votes for p2.
	next, state := commit(t, prep, modes.Subject{ID: "p1", Kind: modes.SubjectPlayer},
		voteAction(pub.WorkIDs["p2"]), states["p1"])
	prep = next
	states["p1"] = state

	if pub.Config.EntertainmentBudget != 0 {
		t.Fatalf("default entertainmentBudget = %d, want 0", pub.Config.EntertainmentBudget)
	}
	scores := settle(t, prep, states)
	for _, entry := range pub.Subjects {
		if got := scores[entry.SubjectID].ScoreUnits; got != 0 {
			t.Fatalf("%s scored %d units but the award is disabled", entry.SubjectID, got)
		}
	}
	// The explanation must say why, and the tally is still published.
	var exp struct {
		EntertainmentBudget int    `json:"entertainmentBudget"`
		WonAward            bool   `json:"wonAward"`
		TeamVotes           int    `json:"teamVotes"`
		Note                string `json:"note"`
	}
	if err := json.Unmarshal(scores["p2"].Explanation, &exp); err != nil {
		t.Fatalf("decode explanation: %v", err)
	}
	if exp.EntertainmentBudget != 0 {
		t.Fatalf("explanation budget = %d", exp.EntertainmentBudget)
	}
	if exp.WonAward {
		t.Fatal("a work won an award that is disabled")
	}
	if exp.TeamVotes != 1 {
		t.Fatalf("explanation teamVotes = %d, want 1", exp.TeamVotes)
	}
	if !strings.Contains(exp.Note, "不计入") {
		t.Fatalf("the explanation does not say the award is excluded: %q", exp.Note)
	}
}

// TestAwardBudgetSplitsOnTie covers the 「并列」 bullet with an enabled budget.
func TestAwardBudgetSplitsOnTie(t *testing.T) {
	prep, pub, _ := prepareRound(t, "tie-award-seed", `{"entertainmentBudget":10}`, 4)
	prep, states := submitAll(t, prep, pub)

	// Give two works one vote each, so they tie for the top rank.
	next, state := commit(t, prep, modes.Subject{ID: "p1", Kind: modes.SubjectPlayer},
		voteAction(pub.WorkIDs["p2"]), states["p1"])
	prep = next
	states["p1"] = state
	next, state = commit(t, prep, modes.Subject{ID: "p2", Kind: modes.SubjectPlayer},
		voteAction(pub.WorkIDs["p1"]), states["p2"])
	prep = next
	states["p2"] = state

	view := project(t, prep, modes.Viewer{MembershipID: "m1", Role: "player",
		Subject: modes.Subject{ID: "p1", Kind: modes.SubjectPlayer}}, modes.PhaseSettled, states)
	if view.Result == nil || view.Result.Award == nil {
		t.Fatal("the tied round produced no award")
	}
	if len(view.Result.Award.WorkIDs) != 2 {
		t.Fatalf("award workIds = %v, want 2 tied works", view.Result.Award.WorkIDs)
	}
	if !view.Result.Award.Shared {
		t.Fatal("the tied award is not marked shared")
	}
	// The two tied works share rank 1.
	for _, work := range view.Result.Works {
		if work.Total == 1 && work.Rank != 1 {
			t.Fatalf("tied work %q has rank %d, want 1", work.WorkID, work.Rank)
		}
		if work.Total == 1 && !work.Shared {
			t.Fatalf("tied work %q is not marked shared", work.WorkID)
		}
	}
	// The budget is split: 10 over 2 works = 5 each.
	scores := settle(t, prep, states)
	for _, id := range []string{"p1", "p2"} {
		if got := scores[id].ScoreUnits; got != 500 {
			t.Fatalf("%s scored %d units, want 500 (half of a 10-point budget)", id, got)
		}
	}
	// A work with no votes scores nothing.
	for _, id := range []string{"p3", "p4"} {
		if got := scores[id].ScoreUnits; got != 0 {
			t.Fatalf("%s scored %d units but received no vote", id, got)
		}
	}
}

// TestNoValidWorkProducesNoAward covers the 「无有效作品」 bullet.
func TestNoValidWorkProducesNoAward(t *testing.T) {
	prep, pub, _ := prepareRound(t, "no-work-seed", `{"entertainmentBudget":10}`, 3)
	// Nobody submits a work; a ballot is therefore impossible.
	dec, _ := apply(t, prep, modes.Subject{ID: "p1", Kind: modes.SubjectPlayer},
		voteAction(pub.WorkIDs["p2"]), nil)
	if dec.Accepted {
		t.Fatal("a ballot was accepted with no work on the ballot")
	}
	if dec.Code != CodeWorkRejected {
		t.Fatalf("code = %q, want %q", dec.Code, CodeWorkRejected)
	}

	scores := settle(t, prep, nil)
	for _, entry := range pub.Subjects {
		if got := scores[entry.SubjectID].ScoreUnits; got != 0 {
			t.Fatalf("%s scored %d units with no valid work", entry.SubjectID, got)
		}
	}
	view := project(t, prep, modes.Viewer{MembershipID: "m1", Role: "player",
		Subject: modes.Subject{ID: "p1", Kind: modes.SubjectPlayer}}, modes.PhaseSettled, nil)
	if view.Result == nil {
		t.Fatal("the settled projection carries no result")
	}
	if view.Result.Award != nil {
		t.Fatalf("a round with no valid work produced an award: %+v", view.Result.Award)
	}
	if view.Result.ValidWorks != 0 {
		t.Fatalf("validWorks = %d, want 0", view.Result.ValidWorks)
	}
	if len(view.Ballot) != 0 {
		t.Fatalf("the ballot holds %d works but nobody submitted", len(view.Ballot))
	}
}

// TestHostRejectedWorkLeavesBallot covers the 「审核」 bullet.
//
// The host's exclusion is expressed by the work leaving the ballot. A ballot for an
// excluded work is void rather than being moved to another work.
func TestHostRejectedWorkLeavesBallot(t *testing.T) {
	prep, pub, _ := prepareRound(t, "review-seed", `{}`, 3)
	prep, states := submitAll(t, prep, pub)

	// Exclude p2's work by rewriting its state, which is how the room layer records
	// a host decision.
	var p2 subjectState
	if err := json.Unmarshal(states["p2"], &p2); err != nil {
		t.Fatalf("decode state: %v", err)
	}
	p2.HostRejected = true
	states["p2"] = mustJSON(t, p2)

	// The round-level registry must drop it too, or a ballot could still address it.
	var pubState publicState
	if err := json.Unmarshal(prep.Public, &pubState); err != nil {
		t.Fatalf("decode public: %v", err)
	}
	delete(pubState.OnBallot, pub.WorkIDs["p2"])
	prep.Public = mustJSON(t, pubState)

	// A ballot for the excluded work is refused.
	dec, _ := apply(t, prep, modes.Subject{ID: "p1", Kind: modes.SubjectPlayer},
		voteAction(pub.WorkIDs["p2"]), states["p1"])
	if dec.Accepted {
		t.Fatal("a ballot for an excluded work was accepted")
	}
	if dec.Code != CodeWorkRejected {
		t.Fatalf("code = %q, want %q", dec.Code, CodeWorkRejected)
	}

	// The ballot no longer lists it.
	view := project(t, prep, modes.Viewer{MembershipID: "m1", Role: "player",
		Subject: modes.Subject{ID: "p1", Kind: modes.SubjectPlayer}}, modes.PhaseLocked, states)
	for _, entry := range view.Ballot {
		if entry.WorkID == pub.WorkIDs["p2"] {
			t.Fatal("the excluded work is still on the ballot")
		}
	}
	if len(view.Ballot) != 2 {
		t.Fatalf("ballot has %d works, want 2", len(view.Ballot))
	}

	// A stale ballot cast before the exclusion is void, not moved.
	var p1 subjectState
	if err := json.Unmarshal(states["p1"], &p1); err != nil {
		t.Fatalf("decode state: %v", err)
	}
	p1.Voted = true
	p1.VoteWorkID = pub.WorkIDs["p2"]
	p1.VoteKind = VoteTeam
	states["p1"] = mustJSON(t, p1)

	result := project(t, prep, modes.Viewer{MembershipID: "m1", Role: "player",
		Subject: modes.Subject{ID: "p1", Kind: modes.SubjectPlayer}}, modes.PhaseSettled, states)
	if result.Result == nil {
		t.Fatal("no result")
	}
	if result.Result.TeamVotes != 0 {
		t.Fatalf("teamVotes = %d, want 0: a ballot for an excluded work must not count",
			result.Result.TeamVotes)
	}
}

// TestWorkLengthIsCountedInCodePoints covers the 80-code-point limit.
func TestWorkLengthIsCountedInCodePoints(t *testing.T) {
	prep, pub, _ := prepareRound(t, "length-seed", `{}`, 2)
	subject := modes.Subject{ID: "p1", Kind: modes.SubjectPlayer}

	cases := []struct {
		name    string
		text    string
		wantErr string
	}{
		{name: "empty", text: "", wantErr: CodeEmptyText},
		{name: "whitespace only", text: "   \t\n ", wantErr: CodeEmptyText},
		{name: "one character", text: "a"},
		{name: "exactly eighty ascii", text: strings.Repeat("a", 80)},
		{name: "eighty one ascii", text: strings.Repeat("a", 81), wantErr: CodeTextTooLong},
		// A CJK character is one code point even though it is three UTF-8 bytes;
		// counting bytes would refuse a legal pitch.
		{name: "eighty CJK", text: strings.Repeat("馬", 80)},
		{name: "eighty one CJK", text: strings.Repeat("馬", 81), wantErr: CodeTextTooLong},
		// An astral character (an emoji) is one code point, not two UTF-16 units.
		{name: "eighty astral", text: strings.Repeat("🐎", 80)},
		{name: "eighty one astral", text: strings.Repeat("🐎", 81), wantErr: CodeTextTooLong},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dec, _ := apply(t, prep, subject, workAction(tc.text), nil)
			if tc.wantErr == "" {
				if !dec.Accepted {
					t.Fatalf("a legal pitch was refused: %q %q", dec.Code, dec.Detail)
				}
				var fb struct {
					CodePoints int `json:"codePoints"`
					Limit      int `json:"limit"`
				}
				if err := json.Unmarshal(dec.Feedback, &fb); err != nil {
					t.Fatalf("decode feedback: %v", err)
				}
				if fb.Limit != MaxCodePoints {
					t.Fatalf("feedback limit = %d, want %d", fb.Limit, MaxCodePoints)
				}
				if want := countCodePoints(tc.text); fb.CodePoints != want {
					t.Fatalf("feedback codePoints = %d, want %d", fb.CodePoints, want)
				}
				return
			}
			if dec.Accepted {
				t.Fatalf("an invalid pitch was accepted: %q", tc.text)
			}
			if dec.Code != tc.wantErr {
				t.Fatalf("code = %q, want %q (detail %q)", dec.Code, tc.wantErr, dec.Detail)
			}
		})
	}
	_ = pub
}

// TestDecomposedTextIsAccepted asserts the rule layer accepts a text that arrives in
// decomposed form instead of refusing it and telling the player to normalise.
//
// The old behaviour refused with "call NFC first", which asked a player to handle an
// encoding concept that their input method, not they, produced. Normalisation now
// happens in the browser before submit (String.normalize is native there); the rule
// layer accepts what it receives and counts code points, so nothing is truncated or
// rewritten on the way in.
func TestDecomposedTextIsAccepted(t *testing.T) {
	prep, pub, _ := prepareRound(t, "nfc-seed", `{}`, 2)
	subject := modes.Subject{ID: "p1", Kind: modes.SubjectPlayer}

	// "e" + COMBINING ACUTE ACCENT is the decomposed form of "é". A player typing this
	// must not be told to normalise it.
	decomposed := "e\u0301"
	dec, _ := apply(t, prep, subject, workAction(decomposed), nil)
	if !dec.Accepted {
		t.Fatalf("a decomposed text was refused: %q %q", dec.Code, dec.Detail)
	}

	// The player-facing rules must not leak encoding vocabulary.
	if strings.Contains(pub.TextRules, "NFC") || strings.Contains(pub.TextRules, "Unicode") ||
		strings.Contains(pub.TextRules, "码点") {
		t.Fatalf("textRules still exposes encoding terms: %q", pub.TextRules)
	}
	if !strings.Contains(pub.TextRules, "字") {
		t.Fatalf("textRules does not state the limit in plain words: %q", pub.TextRules)
	}

	// The precomposed form is accepted and counted as one code point.
	prep2, _, _ := prepareRound(t, "nfc-seed2", `{}`, 2)
	precomposed := "\u00e9"
	dec, _ = apply(t, prep2, subject, workAction(precomposed), nil)
	if !dec.Accepted {
		t.Fatalf("the precomposed form was refused: %q %q", dec.Code, dec.Detail)
	}
	var fb struct {
		CodePoints int `json:"codePoints"`
	}
	if err := json.Unmarshal(dec.Feedback, &fb); err != nil {
		t.Fatalf("decode feedback: %v", err)
	}
	if fb.CodePoints != 1 {
		t.Fatalf("codePoints = %d, want 1", fb.CodePoints)
	}
}

// TestWorkClosedAfterVotingStarts asserts a pitch cannot change once a ballot
// exists.
func TestWorkClosedAfterVotingStarts(t *testing.T) {
	prep, pub, _ := prepareRound(t, "closed-seed", `{}`, 3)
	prep, states := submitAll(t, prep, pub)

	// A ballot from p1 for p2's work closes the writing phase.
	next, state := commit(t, prep, modes.Subject{ID: "p1", Kind: modes.SubjectPlayer},
		voteAction(pub.WorkIDs["p2"]), states["p1"])
	prep = next
	states["p1"] = state

	// p3 can no longer change its pitch.
	dec, _ := apply(t, prep, modes.Subject{ID: "p3", Kind: modes.SubjectPlayer},
		workAction("改过的企划"), states["p3"])
	if dec.Accepted {
		t.Fatal("a pitch was changed after voting started")
	}
	if dec.Code != CodeWorkClosed {
		t.Fatalf("code = %q, want %q", dec.Code, CodeWorkClosed)
	}
}

// TestWorkRevisionBeforeVoting asserts a pitch may still be revised before anybody
// votes.
func TestWorkRevisionBeforeVoting(t *testing.T) {
	prep, pub, _ := prepareRound(t, "revise-seed", `{}`, 3)
	subject := modes.Subject{ID: "p1", Kind: modes.SubjectPlayer}

	next, state := commit(t, prep, subject, workAction("第一版"), nil)
	prep = next
	next, state = commit(t, prep, subject, workAction("第二版企划"), state)
	prep = next

	var st subjectState
	if err := json.Unmarshal(state, &st); err != nil {
		t.Fatalf("decode state: %v", err)
	}
	if st.WorkText != "第二版企划" {
		t.Fatalf("stored text = %q, want the revision", st.WorkText)
	}
	if st.WorkID != pub.WorkIDs["p1"] {
		t.Fatalf("work id changed on revision: %q", st.WorkID)
	}
}

// TestBallotHiddenDuringWriting asserts an unfinished ballot is not visible to a
// player, which would let a team copy a stronger pitch.
func TestBallotHiddenDuringWriting(t *testing.T) {
	prep, pub, _ := prepareRound(t, "hidden-seed", `{}`, 3)
	// Only p1 has submitted.
	next, p1State := commit(t, prep, modes.Subject{ID: "p1", Kind: modes.SubjectPlayer},
		workAction("只有我交了"), nil)
	prep = next
	states := map[string]json.RawMessage{"p1": p1State}

	view := project(t, prep, modes.Viewer{MembershipID: "m2", Role: "player",
		Subject: modes.Subject{ID: "p2", Kind: modes.SubjectPlayer}}, modes.PhaseCollecting, states)
	if len(view.Ballot) != 0 {
		t.Fatalf("the ballot is visible during writing: %+v", view.Ballot)
	}
	// The referee may inspect it, which is the host's moderation need.
	refView := project(t, prep, modes.Viewer{
		MembershipID: "host-1", Role: "host", IsReferee: true,
	}, modes.PhaseCollecting, states)
	if len(refView.Ballot) != 1 {
		t.Fatalf("the referee sees %d works, want 1", len(refView.Ballot))
	}
	_ = pub
}

// TestPromptsAreDistinctPerTeam asserts the draw does not hand two teams the same
// character.
func TestPromptsAreDistinctPerTeam(t *testing.T) {
	for _, players := range []int{2, 5, 8} {
		t.Run(fmt.Sprintf("players%d", players), func(t *testing.T) {
			_, pub, _ := prepareRound(t, "prompt-seed", `{}`, players)
			seen := make(map[string]string, players)
			for _, entry := range pub.Subjects {
				pr, ok := pub.Prompts[entry.SubjectID]
				if !ok {
					t.Fatalf("%s has no prompt", entry.SubjectID)
				}
				if pr.CharacterID == "" || pr.CharacterName == "" {
					t.Fatalf("%s has an incomplete prompt: %+v", entry.SubjectID, pr)
				}
				if pr.Scene == "" || pr.Constraint == "" {
					t.Fatalf("%s has no scene or constraint: %+v", entry.SubjectID, pr)
				}
				if prev, dup := seen[pr.CharacterID]; dup {
					t.Fatalf("%s and %s drew the same character %q",
						prev, entry.SubjectID, pr.CharacterID)
				}
				seen[pr.CharacterID] = entry.SubjectID
			}
		})
	}
}

// TestWorkIDsAreOpaque asserts a work id does not encode its author.
func TestWorkIDsAreOpaque(t *testing.T) {
	_, pub, _ := prepareRound(t, "opaque-seed", `{}`, 4)
	ids := make(map[string]struct{}, len(pub.WorkIDs))
	for subjectID, workID := range pub.WorkIDs {
		if _, dup := ids[workID]; dup {
			t.Fatalf("work id %q is used twice", workID)
		}
		ids[workID] = struct{}{}
		if strings.Contains(workID, subjectID) {
			t.Fatalf("work id %q contains the author id %q", workID, subjectID)
		}
		if strings.HasPrefix(workID, "work-") {
			t.Fatalf("work id %q is a readable counter", workID)
		}
	}
}

// TestConfigurationBounds covers the documented configuration limits.
func TestConfigurationBounds(t *testing.T) {
	cases := []struct {
		name    string
		cfg     string
		wantErr string
	}{
		{name: "default", cfg: `{}`},
		{name: "nil", cfg: ``},
		{name: "explicit", cfg: `{"writeSeconds":90,"voteSeconds":30}`},
		{name: "write too short", cfg: `{"writeSeconds":29}`, wantErr: "outside"},
		{name: "write too long", cfg: `{"writeSeconds":601}`, wantErr: "outside"},
		{name: "vote too short", cfg: `{"voteSeconds":9}`, wantErr: "outside"},
		{name: "vote too long", cfg: `{"voteSeconds":301}`, wantErr: "outside"},
		{name: "negative budget", cfg: `{"entertainmentBudget":-1}`, wantErr: "outside"},
		{name: "budget too high", cfg: `{"entertainmentBudget":21}`, wantErr: "outside"},
		{name: "malformed", cfg: `{"writeSeconds":"ninety"}`, wantErr: "invalid config"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := New().ValidateConfig(json.RawMessage(tc.cfg))
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("ValidateConfig rejected %s: %v", tc.cfg, err)
				}
				return
			}
			if err == nil {
				t.Fatalf("ValidateConfig accepted %s", tc.cfg)
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("error %q does not mention %q", err, tc.wantErr)
			}
		})
	}
}

// TestParticipantBounds covers the roster bounds.
func TestParticipantBounds(t *testing.T) {
	for _, players := range []int{0, 9} {
		t.Run(fmt.Sprintf("players%d", players), func(t *testing.T) {
			_, err := New().Prepare(context.Background(), modes.PrepareInput{
				Scope:   scope("bounds-seed"),
				Subject: roster(players),
				Config:  json.RawMessage(`{}`),
				Content: packOf(),
			})
			if err == nil {
				t.Fatalf("Prepare accepted a %d-player roster", players)
			}
		})
	}
	// A single participant is allowed, which is the solo creative form.
	_, err := New().Prepare(context.Background(), modes.PrepareInput{
		Scope:   scope("solo-seed"),
		Subject: roster(1),
		Config:  json.RawMessage(`{}`),
		Content: packOf(),
	})
	if err != nil {
		t.Fatalf("Prepare refused a single-participant round: %v", err)
	}
}

// TestInvalidActionsRejected covers payload validation for both action kinds.
func TestInvalidActionsRejected(t *testing.T) {
	prep, pub, _ := prepareRound(t, "invalid-seed", `{}`, 3)
	prep, states := submitAll(t, prep, pub)
	subject := modes.Subject{ID: "p1", Kind: modes.SubjectPlayer}

	cases := []struct {
		name string
		kind string
		body string
		want string
	}{
		{name: "unknown action", kind: "creative.teleport", body: `{}`, want: CodeUnknownAction},
		{name: "work empty payload", kind: ActionKindWork, body: ``, want: CodeInvalidPayload},
		{name: "work not json", kind: ActionKindWork, body: `nope`, want: CodeInvalidPayload},
		{name: "work kind mismatch", kind: ActionKindWork,
			body: `{"kind":"creative.vote","text":"x"}`, want: CodeInvalidPayload},
		{name: "vote empty payload", kind: ActionKindVote, body: ``, want: CodeInvalidPayload},
		{name: "vote not json", kind: ActionKindVote, body: `nope`, want: CodeInvalidPayload},
		{name: "vote kind mismatch", kind: ActionKindVote,
			body: `{"kind":"creative.work","optionId":"x"}`, want: CodeInvalidPayload},
		{name: "vote empty option", kind: ActionKindVote,
			body: `{"kind":"creative.vote"}`, want: CodeInvalidPayload},
		{name: "vote unknown work", kind: ActionKindVote,
			body: `{"kind":"creative.vote","optionId":"cw-nope"}`, want: CodeUnknownWork},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dec, _ := apply(t, prep, subject,
				modes.ModeAction{Kind: tc.kind, Payload: json.RawMessage(tc.body)}, states["p1"])
			if dec.Accepted {
				t.Fatal("an invalid action was accepted")
			}
			if dec.Code != tc.want {
				t.Fatalf("code = %q, want %q (detail %q)", dec.Code, tc.want, dec.Detail)
			}
		})
	}
}

// TestNonParticipantCannotAct asserts a subject outside the roster is refused.
func TestNonParticipantCannotAct(t *testing.T) {
	prep, pub, _ := prepareRound(t, "outsider-seed", `{}`, 3)
	prep, _ = submitAll(t, prep, pub)
	outsider := modes.Subject{ID: "spectator-1", Kind: modes.SubjectPlayer}

	dec, _ := apply(t, prep, outsider, workAction("外部企划"), nil)
	if dec.Accepted {
		t.Fatal("a non-participant submitted a work")
	}
	if dec.Code != CodeNotOnRoster {
		t.Fatalf("code = %q, want %q", dec.Code, CodeNotOnRoster)
	}
}

// TestAudienceVoteCanBeDisabled asserts the round can refuse audience ballots.
func TestAudienceVoteCanBeDisabled(t *testing.T) {
	prep, pub, _ := prepareRound(t, "no-audience-seed", `{"allowAudienceVote":false}`, 3)
	// Only p1 and p2 pitch, so p3 would be an audience voter.
	states := map[string]json.RawMessage{}
	for _, id := range []string{"p1", "p2"} {
		next, state := commit(t, prep, modes.Subject{ID: id, Kind: modes.SubjectPlayer},
			workAction("企划 "+id), states[id])
		prep = next
		states[id] = state
	}

	dec, _ := apply(t, prep, modes.Subject{ID: "p3", Kind: modes.SubjectPlayer},
		voteAction(pub.WorkIDs["p1"]), states["p3"])
	if dec.Accepted {
		t.Fatal("an audience ballot was accepted while audience voting is disabled")
	}
	if dec.Code != CodeVoteClosed {
		t.Fatalf("code = %q, want %q", dec.Code, CodeVoteClosed)
	}

	// A team ballot is still allowed.
	dec, _ = apply(t, prep, modes.Subject{ID: "p1", Kind: modes.SubjectPlayer},
		voteAction(pub.WorkIDs["p2"]), states["p1"])
	if !dec.Accepted {
		t.Fatalf("a team ballot was refused: %q %q", dec.Code, dec.Detail)
	}
}

// TestVoteIsNotAnObjectiveCharacterRanking asserts the result does not present the
// vote as an objective ranking of characters .
func TestVoteIsNotAnObjectiveCharacterRanking(t *testing.T) {
	prep, pub, _ := prepareRound(t, "ranking-seed", `{}`, 3)
	prep, states := submitAll(t, prep, pub)
	next, state := commit(t, prep, modes.Subject{ID: "p1", Kind: modes.SubjectPlayer},
		voteAction(pub.WorkIDs["p2"]), states["p1"])
	prep = next
	states["p1"] = state

	view := project(t, prep, modes.Viewer{MembershipID: "m1", Role: "player",
		Subject: modes.Subject{ID: "p1", Kind: modes.SubjectPlayer}}, modes.PhaseSettled, states)
	raw := strings.ToLower(string(mustJSON(t, view)))
	for _, forbidden := range []string{"popularity", "officialrank", "canonicalrank", "characterrank"} {
		if strings.Contains(raw, forbidden) {
			t.Fatalf("the result presents the vote as %q", forbidden)
		}
	}
	// The scoring text must describe an in-session award, not a global ranking.
	if !strings.Contains(view.Scoring, "本场") {
		t.Fatalf("scoring text does not scope the award to this session: %q", view.Scoring)
	}
}

// TestAuthorsRevealedOnlyAtSettle asserts the author map appears only once the
// round is terminal.
func TestAuthorsRevealedOnlyAtSettle(t *testing.T) {
	prep, pub, priv := prepareRound(t, "reveal-seed", `{}`, 3)
	prep, states := submitAll(t, prep, pub)

	for _, phase := range []modes.Phase{modes.PhaseCollecting, modes.PhaseLocked, modes.PhaseRevealed} {
		t.Run(string(phase), func(t *testing.T) {
			view := project(t, prep, modes.Viewer{MembershipID: "m1", Role: "player",
				Subject: modes.Subject{ID: "p1", Kind: modes.SubjectPlayer}}, phase, states)
			if phase.Terminal() {
				if view.Authors == nil {
					t.Fatal("the author map is missing at a terminal phase")
				}
				if len(view.Authors) != len(priv.AuthorOf) {
					t.Fatalf("author map has %d entries, want %d",
						len(view.Authors), len(priv.AuthorOf))
				}
				return
			}
			if view.Authors != nil {
				t.Fatalf("the author map is visible at phase %q", phase)
			}
		})
	}
}

// TestPrepareIsDeterministic asserts the draw is a pure function of the scope.
func TestPrepareIsDeterministic(t *testing.T) {
	first, _, _ := prepareRound(t, "det-seed", `{}`, 4)
	second, _, _ := prepareRound(t, "det-seed", `{}`, 4)
	if string(first.Public) != string(second.Public) {
		t.Fatal("public state is not deterministic")
	}
	if string(first.Private) != string(second.Private) {
		t.Fatal("private state is not deterministic")
	}

	// A different seed draws a different set.
	_, otherPub, _ := prepareRound(t, "det-other", `{}`, 4)
	if string(mustJSON(t, otherPub.Prompts)) == string(mustJSON(t, promptsOf(t, first))) {
		t.Fatal("a different seed produced the same prompts")
	}
}

// promptsOf decodes the prompts of a prepared round.
func promptsOf(t *testing.T, prep modes.Prepared) map[string]prompt {
	t.Helper()
	var pub publicState
	if err := json.Unmarshal(prep.Public, &pub); err != nil {
		t.Fatalf("decode public: %v", err)
	}
	return pub.Prompts
}

// TestSettleScoresAreValidAndRanked asserts the settled scores validate and carry
// a shared rank on a tie.
func TestSettleScoresAreValidAndRanked(t *testing.T) {
	prep, pub, _ := prepareRound(t, "settle-seed", `{"entertainmentBudget":8}`, 4)
	prep, states := submitAll(t, prep, pub)
	// p1 and p2 vote for p3; p3 and p4 vote for p1, so two works tie at 2.
	for _, pair := range []struct{ voter, target string }{
		{"p1", "p3"}, {"p2", "p3"}, {"p3", "p1"}, {"p4", "p1"},
	} {
		next, state := commit(t, prep, modes.Subject{ID: pair.voter, Kind: modes.SubjectPlayer},
			voteAction(pub.WorkIDs[pair.target]), states[pair.voter])
		prep = next
		states[pair.voter] = state
	}

	scores := settle(t, prep, states)
	for _, entry := range pub.Subjects {
		score, ok := scores[entry.SubjectID]
		if !ok {
			t.Fatalf("%s has no score", entry.SubjectID)
		}
		if err := score.Validate(); err != nil {
			t.Fatalf("%s: %v", entry.SubjectID, err)
		}
	}
	// p1 and p3 tie for the award, so they share rank 1 and the same score.
	if scores["p1"].ScoreUnits != scores["p3"].ScoreUnits {
		t.Fatalf("tied winners scored %d and %d",
			scores["p1"].ScoreUnits, scores["p3"].ScoreUnits)
	}
	if scores["p1"].Rank != 1 || scores["p3"].Rank != 1 {
		t.Fatalf("tied winners have ranks %d and %d", scores["p1"].Rank, scores["p3"].Rank)
	}
	if !scores["p1"].SharedRank || !scores["p3"].SharedRank {
		t.Fatal("tied winners are not marked shared")
	}
	// 8 points over 2 tied works = 4 each.
	if got := scores["p1"].ScoreUnits; got != 400 {
		t.Fatalf("tied winner scored %d units, want 400", got)
	}
	// The non-winners score 0.
	for _, id := range []string{"p2", "p4"} {
		if got := scores[id].ScoreUnits; got != 0 {
			t.Fatalf("%s scored %d units but did not win", id, got)
		}
	}
}

// TestCountCodePointsIsExact asserts the counting helper is exact for the cases the
// limit depends on.
func TestCountCodePointsIsExact(t *testing.T) {
	cases := []struct {
		in   string
		want int
	}{
		{in: "", want: 0},
		{in: "a", want: 1},
		{in: "abc", want: 3},
		{in: "馬", want: 1},
		{in: "🐎", want: 1},
		{in: "a馬🐎", want: 3},
		{in: "e\u0301", want: 2},
	}
	for _, tc := range cases {
		t.Run(fmt.Sprintf("%q", tc.in), func(t *testing.T) {
			if got := countCodePoints(tc.in); got != tc.want {
				t.Fatalf("countCodePoints(%q) = %d, want %d", tc.in, got, tc.want)
			}
		})
	}
}

// mustJSON marshals v or fails.
func mustJSON(t *testing.T, v any) json.RawMessage {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return raw
}

// TestPrepareRefusesWithoutPrompts asserts the mode has no built-in fallback.
func TestPrepareRefusesWithoutPrompts(t *testing.T) {
	cases := []struct {
		name    string
		content modes.ContentSource
		want    string
	}{
		{name: "nil content", content: nil, want: "no content source"},
		{name: "empty pack", content: fakeContent{}, want: "creative.prompts"},
		{name: "prompts only, no name index",
			content: packWith(packOptions{omitCharacters: true}), want: "characters.index"},
		{name: "name index only, no prompts",
			content: packWith(packOptions{omitPrompts: true}), want: "creative.prompts"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := New().Prepare(context.Background(), modes.PrepareInput{
				Scope:   scope("no-pack-seed"),
				Subject: roster(3),
				Config:  json.RawMessage(`{}`),
				Content: tc.content,
			})
			if err == nil {
				t.Fatal("Prepare admitted a round with no usable pack")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error %q does not name %q", err, tc.want)
			}
		})
	}
}

// TestPromptProvenanceIsValidated asserts the provenance block is checked, not merely carried.
func TestPromptProvenanceIsValidated(t *testing.T) {
	cases := []struct {
		name string
		prov string
		want string
	}{
		{name: "unknown kind",
			prov: `{"kind":"guess","notOfficialData":true,"author":"t","basis":"b"}`,
			want: "unknown provenance kind"},
		{name: "authored but claims official",
			prov: `{"kind":"authored-design-value","notOfficialData":false,"author":"t","basis":"b"}`,
			want: "must set notOfficialData=true"},
		{name: "no author",
			prov: `{"kind":"authored-design-value","notOfficialData":true,"basis":"b"}`,
			want: "no author"},
		{name: "no basis",
			prov: `{"kind":"authored-design-value","notOfficialData":true,"author":"t"}`,
			want: "no basis"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := New().Prepare(context.Background(), modes.PrepareInput{
				Scope:   scope("prov-seed"),
				Subject: roster(3),
				Config:  json.RawMessage(`{}`),
				Content: packWith(packOptions{badProvenance: tc.prov}),
			})
			if err == nil {
				t.Fatalf("Prepare accepted prompts whose provenance was %q", tc.name)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error %q does not mention %q", err, tc.want)
			}
		})
	}

	// A missing provenance block is refused rather than defaulted.
	t.Run("no provenance block", func(t *testing.T) {
		_, err := New().Prepare(context.Background(), modes.PrepareInput{
			Scope:   scope("prov-seed"),
			Subject: roster(3),
			Config:  json.RawMessage(`{}`),
			Content: packWith(packOptions{omitProvenance: true}),
		})
		if err == nil {
			t.Fatal("Prepare accepted prompts with no provenance block")
		}
		if !strings.Contains(err.Error(), "no provenance block") {
			t.Fatalf("error %q does not mention the missing block", err)
		}
	})
}

// TestPromptsComeFromThePack asserts the drawn prompt uses the PACK text, not a built-in pool.
//
// This is the test that would catch the original defect: the mode declared creative.prompts but
// read characters.index, so publishing the section changed nothing. The fixture text differs
// from the old built-in strings on purpose.
func TestPromptsComeFromThePack(t *testing.T) {
	_, pub, _ := prepareRound(t, "from-pack-seed", `{}`, 3)
	sceneTexts := make(map[string]struct{}, len(fixtureScenes))
	for _, sc := range fixtureScenes {
		sceneTexts[sc] = struct{}{}
	}
	constraintTexts := make(map[string]struct{}, len(fixtureConstraints))
	for _, ct := range fixtureConstraints {
		constraintTexts[ct] = struct{}{}
	}
	for _, entry := range pub.Subjects {
		pr := pub.Prompts[entry.SubjectID]
		if _, ok := sceneTexts[pr.Scene]; !ok {
			t.Fatalf("%s's scene %q is not from the pack", entry.SubjectID, pr.Scene)
		}
		if _, ok := constraintTexts[pr.Constraint]; !ok {
			t.Fatalf("%s's constraint %q is not from the pack", entry.SubjectID, pr.Constraint)
		}
		if pr.SceneID == "" || pr.ConstraintID == "" {
			t.Fatalf("%s's prompt carries no authored ids: %+v", entry.SubjectID, pr)
		}
	}
}

// TestCharactersComeFromTheIndex asserts the character name comes from the public index.
func TestCharactersComeFromTheIndex(t *testing.T) {
	_, pub, _ := prepareRound(t, "chars-seed", `{}`, 3)
	names := make(map[string]string, len(fixtureCharacters))
	for _, c := range fixtureCharacters {
		names[c.ID] = c.Name
	}
	for _, entry := range pub.Subjects {
		pr := pub.Prompts[entry.SubjectID]
		want, ok := names[pr.CharacterID]
		if !ok {
			t.Fatalf("%s drew the character %q which is not in the index",
				entry.SubjectID, pr.CharacterID)
		}
		if pr.CharacterName != want {
			t.Fatalf("%s's character name = %q, index says %q",
				entry.SubjectID, pr.CharacterName, want)
		}
	}
}

// TestRequirementFieldsMatchWhatTheModeReads asserts the declaration matches the loaders.
func TestRequirementFieldsMatchWhatTheModeReads(t *testing.T) {
	fields := New().Requirement().Fields
	want := map[string]bool{PromptsSection: false, CharactersSection: false}
	if len(fields) != len(want) {
		t.Fatalf("Requirement().Fields = %v, want exactly [%q %q]",
			fields, PromptsSection, CharactersSection)
	}
	for _, f := range fields {
		seen, known := want[f]
		if !known {
			t.Fatalf("Requirement().Fields declares %q which no loader reads", f)
		}
		if seen {
			t.Fatalf("Requirement().Fields declares %q twice", f)
		}
		want[f] = true
	}
	for section, seen := range want {
		if !seen {
			t.Fatalf("the mode loads %q but does not declare it", section)
		}
	}
}

// TestEmptyBallotIsExplained asserts an empty ballot says WHY it is empty.
//
// The ballot is legitimately empty while the writing phase is open — showing an unfinished
// ballot would let a team copy a stronger pitch. But a player who sees an empty list must be
// told which case they are in, or it looks like the field was forgotten. This is the same rule
// clue-puzzle's sharedEmptyReason follows.
func TestEmptyBallotIsExplained(t *testing.T) {
	prep, pub, _ := prepareRound(t, "ballot-reason-seed", `{}`, 3)
	player := modes.Viewer{MembershipID: "m1", Role: "player",
		Subject: modes.Subject{ID: "p1", Kind: modes.SubjectPlayer}}

	// Collecting: the ballot is hidden on purpose.
	view := project(t, prep, player, modes.PhaseCollecting, nil)
	if len(view.Ballot) != 0 {
		t.Fatalf("the ballot is visible during writing: %+v", view.Ballot)
	}
	if view.BallotHiddenReason == "" {
		t.Fatal("the ballot is hidden but carries no explanation")
	}

	// Locked with no submissions: still empty, but now because nobody pitched.
	locked := project(t, prep, player, modes.PhaseLocked, nil)
	if len(locked.Ballot) != 0 {
		t.Fatalf("the ballot lists %d works but nobody submitted", len(locked.Ballot))
	}
	if locked.BallotHiddenReason == "" {
		t.Fatal("the ballot is empty with no explanation")
	}
	_ = pub
}

// TestBallotReasonDisappearsWhenPopulated asserts the explanation is omitted once works exist.
func TestBallotReasonDisappearsWhenPopulated(t *testing.T) {
	prep, pub, _ := prepareRound(t, "ballot-populated-seed", `{}`, 3)
	states := map[string]json.RawMessage{}
	for i, entry := range pub.Subjects {
		next, state := commit(t, prep, modes.Subject{ID: entry.SubjectID, Kind: modes.SubjectPlayer},
			workAction(fmt.Sprintf("作品%d号", i+1)), states[entry.SubjectID])
		prep = next
		states[entry.SubjectID] = state
	}
	view := project(t, prep, modes.Viewer{MembershipID: "m1", Role: "player",
		Subject: modes.Subject{ID: "p1", Kind: modes.SubjectPlayer}}, modes.PhaseLocked, states)
	if len(view.Ballot) == 0 {
		t.Fatal("the ballot is empty after everybody pitched")
	}
	if view.BallotHiddenReason != "" {
		t.Fatalf("a populated ballot still carries the empty explanation %q",
			view.BallotHiddenReason)
	}
}

// TestNFCBoundaryIsVisibleToPlayers asserts the NFC limitation is stated where a player reads it.
//
// The rule layer cannot compute NFC: the standard library has no normaliser and this package may
// not add a module dependency. So the client is responsible for calling normalize('NFC'). That is
// a capability boundary, and a boundary a player only discovers by being rejected is a bad one —
// the rules text must say it up front. This test pins that the statement exists, reaches the
// projection, and appears in the rejection detail.
func TestNFCBoundaryIsVisibleToPlayers(t *testing.T) {
	prep, pub, _ := prepareRound(t, "nfc-visible-seed", `{}`, 2)

	// 1. The public state carries the statement.
	if pub.TextRules == "" {
		t.Fatal("the public state carries no text rules, so a player cannot know the length limit")
	}
	// The rules must state the limit in plain words and must NOT expose encoding
	// vocabulary: normalisation is the program's job, not the player's.
	for _, banned := range []string{"NFC", "Unicode", "码点", "归一化"} {
		if strings.Contains(pub.TextRules, banned) {
			t.Fatalf("the text rules expose %q to players: %q", banned, pub.TextRules)
		}
	}
	if !strings.Contains(pub.TextRules, "字") {
		t.Fatalf("the text rules do not state the limit in plain words: %q", pub.TextRules)
	}
	// The limit must be the published number, not a hard-coded string.
	if !strings.Contains(pub.TextRules, fmt.Sprintf("%d", MaxCodePoints)) {
		t.Fatalf("the text rules do not state the %d code point limit: %q",
			MaxCodePoints, pub.TextRules)
	}

	// 2. It reaches the projection, not just the server.
	view := project(t, prep, modes.Viewer{MembershipID: "m1", Role: "player",
		Subject: modes.Subject{ID: "p1", Kind: modes.SubjectPlayer}}, modes.PhaseCollecting, nil)
	if view.TextRules == "" {
		t.Fatal("the projection drops the text rules, so the player never sees them")
	}

	// 3. A decomposed text is accepted, not refused.
	//
	// Normalisation happens in the browser before submit; the rule layer takes what it
	// receives rather than telling the player to call NFC themselves.
	dec, _ := apply(t, prep, modes.Subject{ID: "p1", Kind: modes.SubjectPlayer},
		workAction("e\u0301"), nil)
	if !dec.Accepted {
		t.Fatalf("a decomposed text was refused: %q %q", dec.Code, dec.Detail)
	}
	// The detail must not be a raw English internal message.
	if strings.HasPrefix(dec.Detail, "creative:") {
		t.Fatalf("the rejection leaks an internal prefix: %q", dec.Detail)
	}
}
