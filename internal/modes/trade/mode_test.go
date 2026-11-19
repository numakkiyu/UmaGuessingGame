package trade

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

// fixtureProvenance is the provenance block every fixture entry carries. It mirrors what
// the content compiler publishes, so a test that passes here exercises the same validation
// a real pack goes through.
func fixtureProvenance() map[string]any {
	return map[string]any{
		"kind":            ProvenanceAuthored,
		"notOfficialData": true,
		"author":          "test-fixture",
		"basis":           "docs/玩法与规则.md (trade)",
	}
}

// fixtureCardKinds is the card-kind catalogue the fixture publishes.
//
// It is deliberately NOT the same list the mode used to hard-code: if the mode still had
// a built-in catalogue, these tests would pass against it and never exercise the pack
// path. Using different labels makes that failure mode visible.
var fixtureCardKinds = []struct{ Kind, Label string }{
	{"speed", "速度档案"}, {"stamina", "耐力档案"}, {"power", "力量档案"},
	{"wit", "智慧档案"}, {"charm", "魅力档案"}, {"grit", "毅力档案"},
	{"grace", "仪态档案"}, {"insight", "洞察档案"}, {"rhythm", "节奏档案"},
	{"bond", "羁绊档案"},
}

// fixtureObjectivePairs is the objective catalogue the fixture publishes. Sixteen pairs
// cover eight players times two objectives without repeating, which is what the mode now
// requires.
var fixtureObjectivePairs = [][2]string{
	{"speed", "stamina"}, {"power", "stamina"}, {"power", "wit"}, {"charm", "wit"},
	{"charm", "grit"}, {"grace", "grit"}, {"grace", "insight"}, {"insight", "rhythm"},
	{"bond", "rhythm"}, {"bond", "speed"}, {"power", "speed"}, {"stamina", "wit"},
	{"charm", "power"}, {"grit", "wit"}, {"charm", "grace"}, {"grit", "insight"},
}

// packOf builds a content source carrying the fixture catalogue.
func packOf() modes.ContentSource {
	return packWithObjectives(len(fixtureObjectivePairs))
}

// packWithObjectives builds a pack whose objective pool holds n entries. It is used to
// exercise the "pool too small" gate without inventing a second fixture.
func packWithObjectives(n int) modes.ContentSource {
	cards := make([]map[string]any, 0, len(fixtureCardKinds))
	for _, k := range fixtureCardKinds {
		cards = append(cards, map[string]any{
			"cardId":     k.Kind,
			"label":      k.Label,
			"provenance": fixtureProvenance(),
		})
	}
	objectives := make([]map[string]any, 0, n)
	for i := 0; i < n && i < len(fixtureObjectivePairs); i++ {
		pair := fixtureObjectivePairs[i]
		objectives = append(objectives, map[string]any{
			"objectiveId": fmt.Sprintf("ob-%02d", i+1),
			"text":        fmt.Sprintf("最终持有%s与%s档案各一张", pair[0], pair[1]),
			"kinds":       []string{pair[0], pair[1]},
			"points":      50,
			"provenance":  fixtureProvenance(),
		})
	}
	cardsRaw, err := json.Marshal(cards)
	if err != nil {
		panic(err)
	}
	objRaw, err := json.Marshal(objectives)
	if err != nil {
		panic(err)
	}
	return fakeContent{
		CardsSection:      cardsRaw,
		ObjectivesSection: objRaw,
	}
}

// packWithRaw lets a test supply arbitrary section payloads, so a malformed entry can be
// exercised.
func packWithRaw(cards, objectives string) modes.ContentSource {
	out := fakeContent{}
	if cards != "" {
		out[CardsSection] = json.RawMessage(cards)
	}
	if objectives != "" {
		out[ObjectivesSection] = json.RawMessage(objectives)
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

// proposeAction builds a trade.propose payload.
func proposeAction(recipientID, offered, requested string) modes.ModeAction {
	body, err := json.Marshal(struct {
		Kind                string `json:"kind"`
		RecipientID         string `json:"recipientId"`
		OfferedInstanceID   string `json:"offeredInstanceId"`
		RequestedInstanceID string `json:"requestedInstanceId"`
	}{
		Kind:                ActionKindPropose,
		RecipientID:         recipientID,
		OfferedInstanceID:   offered,
		RequestedInstanceID: requested,
	})
	if err != nil {
		panic(err)
	}
	return modes.ModeAction{Kind: ActionKindPropose, Payload: body}
}

// respondAction builds a trade.respond payload.
func respondAction(tradeID string, accept bool) modes.ModeAction {
	body, err := json.Marshal(struct {
		Kind    string `json:"kind"`
		TradeID string `json:"tradeId"`
		Accept  bool   `json:"accept"`
	}{Kind: ActionKindRespond, TradeID: tradeID, Accept: accept})
	if err != nil {
		panic(err)
	}
	return modes.ModeAction{Kind: ActionKindRespond, Payload: body}
}

// cancelAction builds a trade.cancel payload.
func cancelAction(tradeID string) modes.ModeAction {
	body, err := json.Marshal(struct {
		Kind    string `json:"kind"`
		TradeID string `json:"tradeId"`
	}{Kind: ActionKindCancel, TradeID: tradeID})
	if err != nil {
		panic(err)
	}
	return modes.ModeAction{Kind: ActionKindCancel, Payload: body}
}

// apply runs Apply against the current state.
func apply(t *testing.T, prep modes.Prepared, subject modes.Subject, action modes.ModeAction, state json.RawMessage) modes.Decision {
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
	return dec
}

// commit applies then commits one action, returning the new prepared round and the
// caller's new state.
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
	prep = finishTrading(t, prep)
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

// firstInstanceOfKind returns the first instance id of a kind in a hand.
func firstInstanceOfKind(t *testing.T, pub publicState, subjectID, kind string) string {
	t.Helper()
	kindOf := make(map[string]string, len(pub.Instances))
	for _, inst := range pub.Instances {
		kindOf[inst.InstanceID] = inst.CardID
	}
	for _, id := range pub.Hands[subjectID] {
		if kindOf[id] == kind {
			return id
		}
	}
	t.Fatalf("%s holds no %q instance (hand %v)", subjectID, kind, pub.Hands[subjectID])
	return ""
}

// TestDealIsValid covers the opening deal: four distinct instances per player, two
// distinct objectives, and no instance shared between hands.
func TestDealIsValid(t *testing.T) {
	for _, players := range []int{3, 5, 8} {
		t.Run(fmt.Sprintf("players%d", players), func(t *testing.T) {
			prep, pub, priv := prepareRound(t, "deal-seed", `{}`, players)

			if len(pub.Subjects) != players {
				t.Fatalf("%d subjects, want %d", len(pub.Subjects), players)
			}
			if len(pub.Instances) != players*HandSize {
				t.Fatalf("%d instances, want %d", len(pub.Instances), players*HandSize)
			}
			// Every instance belongs to exactly one hand.
			owner := make(map[string]string, len(pub.Instances))
			for _, entry := range pub.Subjects {
				hand := pub.Hands[entry.SubjectID]
				if len(hand) != HandSize {
					t.Fatalf("%s holds %d instances, want %d", entry.SubjectID, len(hand), HandSize)
				}
				for _, id := range hand {
					if prev, dup := owner[id]; dup {
						t.Fatalf("instance %q is held by both %s and %s", id, prev, entry.SubjectID)
					}
					owner[id] = entry.SubjectID
				}
				// The objectives are secret but must exist and be distinct.
				objectives := priv.Objectives[entry.SubjectID]
				if len(objectives) != ObjectiveCount {
					t.Fatalf("%s has %d objectives, want %d",
						entry.SubjectID, len(objectives), ObjectiveCount)
				}
				ids := make(map[string]struct{}, len(objectives))
				for _, obj := range objectives {
					if _, dup := ids[obj.ObjectiveID]; dup {
						t.Fatalf("%s has the objective %q twice", entry.SubjectID, obj.ObjectiveID)
					}
					ids[obj.ObjectiveID] = struct{}{}
					if obj.Points != ObjectivePoints {
						t.Fatalf("objective %q is worth %d, want %d",
							obj.ObjectiveID, obj.Points, ObjectivePoints)
					}
					if len(obj.Kinds) < 2 {
						t.Fatalf("objective %q needs %d kinds, want at least 2",
							obj.ObjectiveID, len(obj.Kinds))
					}
				}
			}
			if len(owner) != players*HandSize {
				t.Fatalf("%d distinct instances in hands, want %d", len(owner), players*HandSize)
			}
			// The round starts at 1 and every hand is public.
			if pub.CurrentRound != 1 {
				t.Fatalf("currentRound = %d, want 1", pub.CurrentRound)
			}
			// QuestionCount is the usable OBJECTIVE pool, not the table size. The pool is the
			// same whatever the table size, because it is a property of the pack.
			if prep.QuestionCount != len(pub.Objectives) {
				t.Fatalf("questionCount = %d, the pack publishes %d objectives",
					prep.QuestionCount, len(pub.Objectives))
			}
		})
	}
}

// TestObjectiveAssignmentsAreSecret covers the 「任务私有」 requirement.
//
// requires two things that are easy to conflate:
//
//	开局给出所有任务计分条件 -> the objective CATALOGUE is public
//	自己的任务不能从公共快照读取 -> which player holds which objective is private
//
// This test asserts both halves. The first version of this test asserted only the second
// and treated the whole catalogue as secret, which contradicted the documented requirement
// that players must know every scoring condition before play.
func TestObjectiveAssignmentsAreSecret(t *testing.T) {
	prep, pub, priv := prepareRound(t, "secret-seed", `{}`, 3)

	// Half one: the catalogue IS public, with its scoring conditions.
	if len(pub.Objectives) == 0 {
		t.Fatal("the public state carries no objective catalogue")
	}
	for _, obj := range pub.Objectives {
		if obj.Text == "" || len(obj.Kinds) == 0 || obj.Points <= 0 {
			t.Fatalf("published objective is incomplete: %+v", obj)
		}
	}
	// Half two: no public field maps a subject to its objectives.
	//
	// The check is structural rather than textual. Scanning the raw JSON for an objective
	// id would always match, because the id legitimately appears in the public catalogue;
	// the leak would be a field that associates it with a subject. So the assertion is on
	// the decoded public state: it must expose the catalogue and nothing per-subject.
	if len(pub.Objectives) != len(priv.ObjectivePool) {
		t.Fatalf("public catalogue has %d entries, private pool has %d",
			len(pub.Objectives), len(priv.ObjectivePool))
	}
	// The public state type has no per-subject objective field. Assert that by round-
	// tripping it through a generic map: any key that pairs a subject id with objectives
	// would show up here.
	var generic map[string]json.RawMessage
	if err := json.Unmarshal(prep.Public, &generic); err != nil {
		t.Fatalf("decode public state generically: %v", err)
	}
	for key := range generic {
		lower := strings.ToLower(key)
		if strings.Contains(lower, "assignment") {
			t.Fatalf("the public state carries the field %q, which suggests an assignment", key)
		}
	}
	// The only per-subject public map is hands, and it holds instance ids, not objectives.
	handsRaw, ok := generic["hands"]
	if !ok {
		t.Fatal("the public state carries no hands field")
	}
	var hands map[string][]string
	if err := json.Unmarshal(handsRaw, &hands); err != nil {
		t.Fatalf("decode hands: %v", err)
	}
	objectiveIDs := make(map[string]struct{}, len(priv.ObjectivePool))
	for _, obj := range priv.ObjectivePool {
		objectiveIDs[obj.ObjectiveID] = struct{}{}
	}
	for subjectID, values := range hands {
		for _, v := range values {
			if _, isObjective := objectiveIDs[v]; isObjective {
				t.Fatalf("hands for %q holds the objective id %q", subjectID, v)
			}
		}
	}

	// A player sees only their own assignment.
	view := project(t, prep, modes.Viewer{MembershipID: "m1", Role: "player",
		Subject: modes.Subject{ID: "p1", Kind: modes.SubjectPlayer}}, modes.PhaseCollecting, nil)
	own := make(map[string]struct{}, len(priv.Objectives["p1"]))
	for _, obj := range priv.Objectives["p1"] {
		own[obj.ObjectiveID] = struct{}{}
	}
	if len(view.SelfObjectives) != len(own) {
		t.Fatalf("p1 sees %d assignments, owns %d", len(view.SelfObjectives), len(own))
	}
	for _, obj := range view.SelfObjectives {
		if _, mine := own[obj.ObjectiveID]; !mine {
			t.Fatalf("p1 sees objective %q which belongs to somebody else", obj.ObjectiveID)
		}
	}
	// Another player's assignment never appears.
	for _, otherID := range []string{"p2", "p3"} {
		for _, obj := range priv.Objectives[otherID] {
			if _, mine := own[obj.ObjectiveID]; mine {
				continue
			}
			for _, seen := range view.SelfObjectives {
				if seen.ObjectiveID == obj.ObjectiveID {
					t.Fatalf("p1 sees %s's assignment %q", otherID, obj.ObjectiveID)
				}
			}
		}
	}
}

// TestOneOpenProposalPerPlayer covers the 「单个未确认提案限制」 bullet.
func TestOneOpenProposalPerPlayer(t *testing.T) {
	prep, pub, _ := prepareRound(t, "single-seed", `{}`, 3)
	sender := modes.Subject{ID: "p1", Kind: modes.SubjectPlayer}

	offered := pub.Hands["p1"][0]
	requested := pub.Hands["p2"][0]
	dec := apply(t, prep, sender, proposeAction("p2", offered, requested), nil)
	if !dec.Accepted {
		t.Fatalf("first proposal rejected: %q %q", dec.Code, dec.Detail)
	}
	prep, state := commit(t, prep, sender, proposeAction("p2", offered, requested), nil)

	// A second proposal while the first is pending is refused.
	dec = apply(t, prep, sender, proposeAction("p3", pub.Hands["p1"][1], pub.Hands["p3"][0]), state)
	if dec.Accepted {
		t.Fatal("a second open proposal was accepted")
	}
	if dec.Code != CodeAlreadyOpen {
		t.Fatalf("code = %q, want %q", dec.Code, CodeAlreadyOpen)
	}

	// Cancelling frees the slot, and a new proposal is then accepted.
	next, state := commit(t, prep, sender, cancelAction(extractTradeID(t, dec0(t, prep, sender, state))), state)
	prep = next
	dec = apply(t, prep, sender, proposeAction("p3", pub.Hands["p1"][1], pub.Hands["p3"][0]), state)
	if !dec.Accepted {
		t.Fatalf("a proposal after cancelling was refused: %q %q", dec.Code, dec.Detail)
	}
}

// dec0 returns the open proposal id from the caller's state, which is how the test
// finds the proposal to cancel.
func dec0(t *testing.T, prep modes.Prepared, subject modes.Subject, state json.RawMessage) string {
	t.Helper()
	var st subjectState
	if err := json.Unmarshal(state, &st); err != nil {
		t.Fatalf("decode state: %v", err)
	}
	if st.OpenTradeID == "" {
		t.Fatal("the caller has no open proposal")
	}
	return st.OpenTradeID
}

// extractTradeID is an identity helper that documents intent at the call site.
func extractTradeID(t *testing.T, tradeID string) string {
	t.Helper()
	if tradeID == "" {
		t.Fatal("empty trade id")
	}
	return tradeID
}

// TestAtomicExchange covers the 「原子交换」 bullet: an accepted proposal moves both
// instances in one step, and the hands stay the same size.
func TestAtomicExchange(t *testing.T) {
	prep, pub, _ := prepareRound(t, "atomic-seed", `{}`, 3)
	sender := modes.Subject{ID: "p1", Kind: modes.SubjectPlayer}
	recipient := modes.Subject{ID: "p2", Kind: modes.SubjectPlayer}

	offered := pub.Hands["p1"][0]
	requested := pub.Hands["p2"][0]
	prep, senderState := commit(t, prep, sender, proposeAction("p2", offered, requested), nil)
	tradeID := dec0(t, prep, sender, senderState)

	next, recipientState := commit(t, prep, recipient, respondAction(tradeID, true), nil)
	prep = next

	var after publicState
	if err := json.Unmarshal(prep.Public, &after); err != nil {
		t.Fatalf("decode public: %v", err)
	}
	// Both instances moved.
	if !contains(after.Hands["p1"], requested) {
		t.Fatalf("p1 did not receive %q: %v", requested, after.Hands["p1"])
	}
	if contains(after.Hands["p1"], offered) {
		t.Fatalf("p1 still holds the offered instance %q", offered)
	}
	if !contains(after.Hands["p2"], offered) {
		t.Fatalf("p2 did not receive %q: %v", offered, after.Hands["p2"])
	}
	if contains(after.Hands["p2"], requested) {
		t.Fatalf("p2 still holds the requested instance %q", requested)
	}
	// Hand sizes are unchanged, which is what makes the swap a swap.
	for _, entry := range after.Subjects {
		if len(after.Hands[entry.SubjectID]) != HandSize {
			t.Fatalf("%s holds %d instances after the trade, want %d",
				entry.SubjectID, len(after.Hands[entry.SubjectID]), HandSize)
		}
	}
	// The proposal is recorded as accepted.
	record, ok := findTrade(after, tradeID)
	if !ok {
		t.Fatalf("proposal %q is missing from the log", tradeID)
	}
	if record.Status != StatusAccepted {
		t.Fatalf("proposal status = %q, want %q", record.Status, StatusAccepted)
	}
	_ = recipientState
}

// TestDuplicateConfirmationRefused covers the 「重复确认」 bullet.
func TestDuplicateConfirmationRefused(t *testing.T) {
	prep, pub, _ := prepareRound(t, "dup-confirm-seed", `{}`, 3)
	sender := modes.Subject{ID: "p1", Kind: modes.SubjectPlayer}
	recipient := modes.Subject{ID: "p2", Kind: modes.SubjectPlayer}

	prep, senderState := commit(t, prep, sender,
		proposeAction("p2", pub.Hands["p1"][0], pub.Hands["p2"][0]), nil)
	tradeID := dec0(t, prep, sender, senderState)
	prep, recipientState := commit(t, prep, recipient, respondAction(tradeID, true), nil)

	// A second acceptance is refused: the cards already moved, so accepting again
	// would swap them back without a new agreement.
	dec := apply(t, prep, recipient, respondAction(tradeID, true), recipientState)
	if dec.Accepted {
		t.Fatal("a proposal was accepted twice")
	}
	if dec.Code != CodeTradeClosed {
		t.Fatalf("code = %q, want %q", dec.Code, CodeTradeClosed)
	}
}

// TestConcurrentDoubleSaleRefused covers the 「并发多卖」 bullet.
//
// p1 offers the same instance to p2 and then to p3. The second proposal must be
// refused, because a proposal names a specific instance and the instance is in one
// hand. If it were accepted, one of the two recipients would end up without the card.
func TestConcurrentDoubleSaleRefused(t *testing.T) {
	prep, pub, _ := prepareRound(t, "double-sale-seed", `{}`, 3)
	p1 := modes.Subject{ID: "p1", Kind: modes.SubjectPlayer}
	contested := pub.Hands["p1"][0]

	// First sale to p2.
	prep, p1State := commit(t, prep, p1, proposeAction("p2", contested, pub.Hands["p2"][0]), nil)
	tradeID := dec0(t, prep, p1, p1State)

	// p1 tries to offer the same instance to p3 while the first is pending: refused
	// by the single-proposal rule.
	dec := apply(t, prep, p1, proposeAction("p3", contested, pub.Hands["p3"][0]), p1State)
	if dec.Accepted {
		t.Fatal("a second proposal for the same instance was accepted")
	}
	if dec.Code != CodeAlreadyOpen {
		t.Fatalf("code = %q, want %q", dec.Code, CodeAlreadyOpen)
	}

	// Cancel, then try to sell the same instance again: this must be refused once the
	// instance has moved. First complete the p2 trade.
	prep, p2State := commit(t, prep, modes.Subject{ID: "p2", Kind: modes.SubjectPlayer},
		respondAction(tradeID, true), nil)

	// p1 no longer holds `contested`, so offering it is refused.
	dec = apply(t, prep, p1, proposeAction("p3", contested, pub.Hands["p3"][0]), p1State)
	if dec.Accepted {
		t.Fatal("p1 offered an instance it no longer holds")
	}
	if dec.Code != CodeInstanceGone {
		t.Fatalf("code = %q, want %q", dec.Code, CodeInstanceGone)
	}
	_ = p2State
}

// TestCancelThenAcceptConflict covers the 「取消与接受冲突」 bullet.
//
// After the sender cancels, the recipient's acceptance must fail rather than move
// cards the sender already withdrew.
func TestCancelThenAcceptConflict(t *testing.T) {
	prep, pub, _ := prepareRound(t, "cancel-conflict-seed", `{}`, 3)
	sender := modes.Subject{ID: "p1", Kind: modes.SubjectPlayer}
	recipient := modes.Subject{ID: "p2", Kind: modes.SubjectPlayer}

	prep, senderState := commit(t, prep, sender,
		proposeAction("p2", pub.Hands["p1"][0], pub.Hands["p2"][0]), nil)
	tradeID := dec0(t, prep, sender, senderState)

	// The sender cancels.
	prep, _ = commit(t, prep, sender, cancelAction(tradeID), senderState)

	// The recipient's acceptance arrives afterwards and must be refused.
	dec := apply(t, prep, recipient, respondAction(tradeID, true), nil)
	if dec.Accepted {
		t.Fatal("a cancelled proposal was accepted")
	}
	if dec.Code != CodeTradeClosed {
		t.Fatalf("code = %q, want %q", dec.Code, CodeTradeClosed)
	}

	// The hands did not move.
	var after publicState
	if err := json.Unmarshal(prep.Public, &after); err != nil {
		t.Fatalf("decode public: %v", err)
	}
	if !contains(after.Hands["p1"], pub.Hands["p1"][0]) {
		t.Fatal("the cancelled trade still moved p1's card")
	}
}

// TestRejectDoesNotMoveCards asserts a rejection changes nothing but the status.
func TestRejectDoesNotMoveCards(t *testing.T) {
	prep, pub, _ := prepareRound(t, "reject-seed", `{}`, 3)
	sender := modes.Subject{ID: "p1", Kind: modes.SubjectPlayer}
	recipient := modes.Subject{ID: "p2", Kind: modes.SubjectPlayer}

	prep, senderState := commit(t, prep, sender,
		proposeAction("p2", pub.Hands["p1"][0], pub.Hands["p2"][0]), nil)
	tradeID := dec0(t, prep, sender, senderState)
	prep, _ = commit(t, prep, recipient, respondAction(tradeID, false), nil)

	var after publicState
	if err := json.Unmarshal(prep.Public, &after); err != nil {
		t.Fatalf("decode public: %v", err)
	}
	for _, entry := range pub.Subjects {
		if strings.Join(after.Hands[entry.SubjectID], ",") != strings.Join(pub.Hands[entry.SubjectID], ",") {
			t.Fatalf("%s's hand changed on a rejection", entry.SubjectID)
		}
	}
	record, _ := findTrade(after, tradeID)
	if record.Status != StatusRejected {
		t.Fatalf("status = %q, want %q", record.Status, StatusRejected)
	}
	// The sender may now open another proposal.
	dec := apply(t, prep, sender, proposeAction("p3", pub.Hands["p1"][1], pub.Hands["p3"][0]), senderState)
	if !dec.Accepted {
		t.Fatalf("a proposal after a rejection was refused: %q %q", dec.Code, dec.Detail)
	}
}

// TestOnlyRecipientCanRespond asserts the sender cannot accept their own proposal.
func TestOnlyRecipientCanRespond(t *testing.T) {
	prep, pub, _ := prepareRound(t, "recipient-seed", `{}`, 3)
	sender := modes.Subject{ID: "p1", Kind: modes.SubjectPlayer}

	prep, senderState := commit(t, prep, sender,
		proposeAction("p2", pub.Hands["p1"][0], pub.Hands["p2"][0]), nil)
	tradeID := dec0(t, prep, sender, senderState)

	dec := apply(t, prep, sender, respondAction(tradeID, true), senderState)
	if dec.Accepted {
		t.Fatal("the sender accepted their own proposal")
	}
	if dec.Code != CodeNotYourTrade {
		t.Fatalf("code = %q, want %q", dec.Code, CodeNotYourTrade)
	}

	// A third player also cannot respond.
	dec = apply(t, prep, modes.Subject{ID: "p3", Kind: modes.SubjectPlayer},
		respondAction(tradeID, true), nil)
	if dec.Accepted {
		t.Fatal("a third player responded to a proposal between two others")
	}
	if dec.Code != CodeNotYourTrade {
		t.Fatalf("code = %q, want %q", dec.Code, CodeNotYourTrade)
	}
}

// TestOnlySenderCanCancel asserts a recipient cannot cancel somebody else's proposal.
func TestOnlySenderCanCancel(t *testing.T) {
	prep, pub, _ := prepareRound(t, "cancel-owner-seed", `{}`, 3)
	sender := modes.Subject{ID: "p1", Kind: modes.SubjectPlayer}
	recipient := modes.Subject{ID: "p2", Kind: modes.SubjectPlayer}

	prep, senderState := commit(t, prep, sender,
		proposeAction("p2", pub.Hands["p1"][0], pub.Hands["p2"][0]), nil)
	tradeID := dec0(t, prep, sender, senderState)

	dec := apply(t, prep, recipient, cancelAction(tradeID), nil)
	if dec.Accepted {
		t.Fatal("the recipient cancelled the sender's proposal")
	}
	if dec.Code != CodeNotYourTrade {
		t.Fatalf("code = %q, want %q", dec.Code, CodeNotYourTrade)
	}
}

// TestDisconnectDoesNotMoveCards covers the 「断线卡片归属」 bullet.
//
// A disconnect is not an action: the hands are only changed by an accepted proposal,
// so a player who drops keeps their cards and the round can still settle.
func TestDisconnectDoesNotMoveCards(t *testing.T) {
	prep, pub, _ := prepareRound(t, "disconnect-seed", `{}`, 3)
	// p1 proposes and then goes silent; p2 never responds.
	sender := modes.Subject{ID: "p1", Kind: modes.SubjectPlayer}
	prep, _ = commit(t, prep, sender, proposeAction("p2", pub.Hands["p1"][0], pub.Hands["p2"][0]), nil)

	// Nothing moved, and the pending proposal does not hold the cards hostage: the
	// instances are still in their original hands.
	var after publicState
	if err := json.Unmarshal(prep.Public, &after); err != nil {
		t.Fatalf("decode public: %v", err)
	}
	for _, entry := range pub.Subjects {
		if strings.Join(after.Hands[entry.SubjectID], ",") != strings.Join(pub.Hands[entry.SubjectID], ",") {
			t.Fatalf("%s's hand changed while the proposal was pending", entry.SubjectID)
		}
	}

	// The round still settles with the pending proposal unresolved.
	scores := settle(t, prep, nil)
	if len(scores) != len(pub.Subjects) {
		t.Fatalf("settled %d scores, want %d", len(scores), len(pub.Subjects))
	}
	for _, entry := range pub.Subjects {
		if err := scores[entry.SubjectID].Validate(); err != nil {
			t.Fatalf("%s: %v", entry.SubjectID, err)
		}
	}
}

// TestDepartedPlayerHandStaysSettleable covers the 「成员退出」 bullet.
func TestDepartedPlayerHandStaysSettleable(t *testing.T) {
	prep, pub, priv := prepareRound(t, "depart-seed", `{}`, 3)
	// Simulate a departure by omitting p3's participant state entirely. The hand is
	// still in the public board, so the valuation must still work.
	states := map[string]json.RawMessage{}
	scores := settle(t, prep, states)
	if len(scores) != len(pub.Subjects) {
		t.Fatalf("settled %d scores, want %d", len(scores), len(pub.Subjects))
	}
	// p3 still has objectives valued from the private state.
	var exp struct {
		Total int `json:"total"`
	}
	if err := json.Unmarshal(scores["p3"].Explanation, &exp); err != nil {
		t.Fatalf("decode explanation: %v", err)
	}
	if exp.Total != len(priv.Objectives["p3"]) {
		t.Fatalf("p3's explanation counts %d objectives, want %d",
			exp.Total, len(priv.Objectives["p3"]))
	}
}

// TestObjectiveValuation asserts the final hand decides the objectives.
func TestObjectiveValuation(t *testing.T) {
	prep, _, priv := prepareRound(t, "valuation-seed", `{}`, 3)
	// Build a hand for p1 that satisfies their first objective, by rewriting the
	// public hands so the test does not depend on the random deal.
	var board publicState
	if err := json.Unmarshal(prep.Public, &board); err != nil {
		t.Fatalf("decode public: %v", err)
	}
	target := priv.Objectives["p1"][0]
	// Find one instance per required kind anywhere in the deck and give them to p1.
	byKind := make(map[string][]string, len(fixtureCardKinds))
	for _, inst := range board.Instances {
		byKind[inst.CardID] = append(byKind[inst.CardID], inst.InstanceID)
	}
	for _, ids := range byKind {
		sort.Strings(ids)
	}
	newHand := make([]string, 0, HandSize)
	for _, kind := range target.Kinds {
		ids := byKind[kind]
		if len(ids) == 0 {
			t.Fatalf("no instance of kind %q exists", kind)
		}
		newHand = append(newHand, ids[0])
	}
	// Fill the rest of the hand from any instances not already used.
	used := make(map[string]struct{}, len(newHand))
	for _, id := range newHand {
		used[id] = struct{}{}
	}
	for _, inst := range board.Instances {
		if len(newHand) >= HandSize {
			break
		}
		if _, taken := used[inst.InstanceID]; taken {
			continue
		}
		newHand = append(newHand, inst.InstanceID)
		used[inst.InstanceID] = struct{}{}
	}
	// Remove the reassigned instances from whoever held them.
	for _, entry := range board.Subjects {
		if entry.SubjectID == "p1" {
			continue
		}
		kept := make([]string, 0, len(board.Hands[entry.SubjectID]))
		for _, id := range board.Hands[entry.SubjectID] {
			if _, reassigned := used[id]; reassigned {
				continue
			}
			kept = append(kept, id)
		}
		board.Hands[entry.SubjectID] = kept
	}
	board.Hands["p1"] = newHand
	prep.Public = mustJSON(t, board)

	scores := settle(t, prep, nil)
	var exp struct {
		Completed int `json:"completed"`
		Points    int `json:"points"`
	}
	if err := json.Unmarshal(scores["p1"].Explanation, &exp); err != nil {
		t.Fatalf("decode explanation: %v", err)
	}
	if exp.Completed < 1 {
		t.Fatalf("p1 completed %d objectives after holding the required kinds", exp.Completed)
	}
	if exp.Points < ObjectivePoints {
		t.Fatalf("p1 earned %d points, want at least %d", exp.Points, ObjectivePoints)
	}
}

// TestInfeasibleObjectiveRefused covers the 「任务无解」 bullet.
//
// An objective naming a kind no card provides must be refused at prepare rather than
// dealt as a guaranteed loss.
func TestInfeasibleObjectiveRefused(t *testing.T) {
	// A single card kind means any objective needing two kinds is infeasible. The
	// deck builder rotates kinds, so this is checked through objectiveFeasible
	// directly, which is the function Prepare relies on.
	err := objectiveFeasible(objective{
		ObjectiveID: "bad", Text: "不可能", Kinds: []string{"speed", "nope"},
	}, map[string]int{"speed": 4})
	if err == nil {
		t.Fatal("an objective requiring a missing kind was accepted")
	}
	if !strings.Contains(err.Error(), "no card provides") {
		t.Fatalf("error %q does not name the missing kind", err)
	}

	// An objective repeating one kind is refused, because a single card would satisfy
	// it — the reuse the first version forbids.
	err = objectiveFeasible(objective{
		ObjectiveID: "reuse", Text: "复用", Kinds: []string{"speed", "speed"},
	}, map[string]int{"speed": 4})
	if err == nil {
		t.Fatal("an objective repeating a kind was accepted")
	}
	if !strings.Contains(err.Error(), "repeats the kind") {
		t.Fatalf("error %q does not name the repetition", err)
	}

	// Every objective in the authored pool must be feasible against a full deck.
	full := make(map[string]int, len(fixtureCardKinds))
	for _, kind := range fixtureCardKinds {
		full[kind.Kind] = 8
	}
	// The fixture catalogue is the one the tests actually publish, so it must be
	// feasible against the fixture deck.
	objectives, err := loadObjectives(packOf())
	if err != nil {
		t.Fatalf("loadObjectives: %v", err)
	}
	for _, obj := range objectives {
		if err := objectiveFeasible(obj, full); err != nil {
			t.Fatalf("fixture objective %q is infeasible: %v", obj.ObjectiveID, err)
		}
	}
}

// TestDefaultDoesNotEnterFormalCup covers the 「默认不进入正式杯」 bullet.
func TestDefaultDoesNotEnterFormalCup(t *testing.T) {
	prep, pub, _ := prepareRound(t, "noncompetitive-seed", `{}`, 3)
	if pub.Config.Competitive {
		t.Fatal("the default configuration is competitive")
	}
	scores := settle(t, prep, nil)
	for _, entry := range pub.Subjects {
		if got := scores[entry.SubjectID].ScoreUnits; got != 0 {
			t.Fatalf("%s scored %d units but the mode is non-competitive", entry.SubjectID, got)
		}
		var exp struct {
			Competitive bool   `json:"competitive"`
			Note        string `json:"note"`
		}
		if err := json.Unmarshal(scores[entry.SubjectID].Explanation, &exp); err != nil {
			t.Fatalf("decode explanation: %v", err)
		}
		if exp.Competitive {
			t.Fatalf("%s's explanation claims the round is competitive", entry.SubjectID)
		}
		if !strings.Contains(exp.Note, "不进入正式杯") {
			t.Fatalf("%s's explanation does not state the exclusion: %q", entry.SubjectID, exp.Note)
		}
	}
}

// TestCompetitiveOptInScores asserts an explicit opt-in produces objective points.
func TestCompetitiveOptInScores(t *testing.T) {
	prep, _, priv := prepareRound(t, "competitive-seed", `{"competitive":true}`, 3)
	// Give p1 a hand that satisfies their first objective.
	var board publicState
	if err := json.Unmarshal(prep.Public, &board); err != nil {
		t.Fatalf("decode public: %v", err)
	}
	target := priv.Objectives["p1"][0]
	byKind := make(map[string][]string, len(fixtureCardKinds))
	for _, inst := range board.Instances {
		byKind[inst.CardID] = append(byKind[inst.CardID], inst.InstanceID)
	}
	newHand := make([]string, 0, HandSize)
	used := make(map[string]struct{}, HandSize)
	for _, kind := range target.Kinds {
		ids := byKind[kind]
		sort.Strings(ids)
		newHand = append(newHand, ids[0])
		used[ids[0]] = struct{}{}
	}
	for _, inst := range board.Instances {
		if len(newHand) >= HandSize {
			break
		}
		if _, taken := used[inst.InstanceID]; taken {
			continue
		}
		newHand = append(newHand, inst.InstanceID)
		used[inst.InstanceID] = struct{}{}
	}
	for _, entry := range board.Subjects {
		if entry.SubjectID == "p1" {
			continue
		}
		kept := make([]string, 0, len(board.Hands[entry.SubjectID]))
		for _, id := range board.Hands[entry.SubjectID] {
			if _, reassigned := used[id]; reassigned {
				continue
			}
			kept = append(kept, id)
		}
		board.Hands[entry.SubjectID] = kept
	}
	board.Hands["p1"] = newHand
	prep.Public = mustJSON(t, board)

	scores := settle(t, prep, nil)
	if got := scores["p1"].ScoreUnits; got < ObjectivePoints*100 {
		t.Fatalf("p1 scored %d units, want at least %d", got, ObjectivePoints*100)
	}
	// The maximum is the objective total, so the score cannot exceed it.
	if got, max := scores["p1"].ScoreUnits, scores["p1"].MaxScoreUnits; got > max {
		t.Fatalf("p1 scored %d units, above the maximum %d", got, max)
	}
}

// TestInvalidActionsRejected covers payload validation for all three kinds.
func TestInvalidActionsRejected(t *testing.T) {
	prep, pub, _ := prepareRound(t, "invalid-seed", `{}`, 3)
	p1 := modes.Subject{ID: "p1", Kind: modes.SubjectPlayer}
	prep, p1State := commit(t, prep, p1,
		proposeAction("p2", pub.Hands["p1"][0], pub.Hands["p2"][0]), nil)
	tradeID := dec0(t, prep, p1, p1State)

	cases := []struct {
		name string
		kind string
		body string
		want string
	}{
		{name: "unknown action", kind: "trade.teleport", body: `{}`, want: CodeUnknownAction},
		{name: "propose empty payload", kind: ActionKindPropose, body: ``, want: CodeInvalidPayload},
		{name: "propose not json", kind: ActionKindPropose, body: `nope`, want: CodeInvalidPayload},
		{name: "propose kind mismatch", kind: ActionKindPropose,
			body: `{"kind":"trade.respond","recipientId":"p2"}`, want: CodeInvalidPayload},
		{name: "propose self", kind: ActionKindPropose,
			body: fmt.Sprintf(`{"kind":"trade.propose","recipientId":"p1",`+
				`"offeredInstanceId":%q,"requestedInstanceId":%q}`,
				pub.Hands["p1"][0], pub.Hands["p1"][1]), want: CodeSelfTrade},
		{name: "propose same instance", kind: ActionKindPropose,
			body: fmt.Sprintf(`{"kind":"trade.propose","recipientId":"p2",`+
				`"offeredInstanceId":%q,"requestedInstanceId":%q}`,
				pub.Hands["p1"][0], pub.Hands["p1"][0]), want: CodeSameInstance},
		{name: "respond empty payload", kind: ActionKindRespond, body: ``, want: CodeInvalidPayload},
		{name: "respond not json", kind: ActionKindRespond, body: `nope`, want: CodeInvalidPayload},
		{name: "respond empty trade", kind: ActionKindRespond,
			body: `{"kind":"trade.respond","accept":true}`, want: CodeInvalidPayload},
		{name: "respond unknown trade", kind: ActionKindRespond,
			body: `{"kind":"trade.respond","tradeId":"tr-nope","accept":true}`, want: CodeTradeNotFound},
		{name: "cancel empty payload", kind: ActionKindCancel, body: ``, want: CodeInvalidPayload},
		{name: "cancel unknown trade", kind: ActionKindCancel,
			body: `{"kind":"trade.cancel","tradeId":"tr-nope"}`, want: CodeTradeNotFound},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dec := apply(t, prep, p1,
				modes.ModeAction{Kind: tc.kind, Payload: json.RawMessage(tc.body)}, p1State)
			if dec.Accepted {
				t.Fatal("an invalid action was accepted")
			}
			if dec.Code != tc.want {
				t.Fatalf("code = %q, want %q (detail %q)", dec.Code, tc.want, dec.Detail)
			}
		})
	}
	// The valid cancel is still accepted.
	dec := apply(t, prep, p1, cancelAction(tradeID), p1State)
	if !dec.Accepted {
		t.Fatalf("the valid cancel was refused: %q %q", dec.Code, dec.Detail)
	}
}

// TestProposeRequiresHeldInstances asserts a proposal naming an instance the sender
// does not hold is refused. It runs on a fresh round because a round with an open
// proposal fails the single-proposal rule first, which would mask the instance check.
func TestProposeRequiresHeldInstances(t *testing.T) {
	prep, pub, _ := prepareRound(t, "held-seed", `{}`, 3)
	p1 := modes.Subject{ID: "p1", Kind: modes.SubjectPlayer}

	cases := []struct {
		name    string
		offered string
		want    string
	}{
		{name: "offered instance does not exist", offered: "ti-nope", want: CodeInstanceGone},
		{name: "offered instance belongs to the recipient", offered: pub.Hands["p2"][0], want: CodeInstanceGone},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dec := apply(t, prep, p1,
				proposeAction("p2", tc.offered, pub.Hands["p2"][1]), nil)
			if dec.Accepted {
				t.Fatal("a proposal for an unheld instance was accepted")
			}
			if dec.Code != tc.want {
				t.Fatalf("code = %q, want %q (detail %q)", dec.Code, tc.want, dec.Detail)
			}
		})
	}

	// The requested instance must be in the recipient's hand.
	dec := apply(t, prep, p1, proposeAction("p2", pub.Hands["p1"][0], pub.Hands["p3"][0]), nil)
	if dec.Accepted {
		t.Fatal("a proposal requesting an instance the recipient does not hold was accepted")
	}
	if dec.Code != CodeInstanceGone {
		t.Fatalf("code = %q, want %q", dec.Code, CodeInstanceGone)
	}
}

// TestNonParticipantCannotAct asserts a subject outside the roster is refused.
func TestNonParticipantCannotAct(t *testing.T) {
	prep, pub, _ := prepareRound(t, "outsider-seed", `{}`, 3)
	outsider := modes.Subject{ID: "spectator-1", Kind: modes.SubjectPlayer}
	dec := apply(t, prep, outsider,
		proposeAction("p1", pub.Hands["p1"][0], pub.Hands["p1"][1]), nil)
	if dec.Accepted {
		t.Fatal("a non-participant proposal was accepted")
	}
	if dec.Code != CodeNotOnRoster {
		t.Fatalf("code = %q, want %q", dec.Code, CodeNotOnRoster)
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
		{name: "explicit", cfg: `{"rounds":3,"roundSeconds":60}`},
		{name: "zero rounds falls back", cfg: `{"rounds":0}`},
		{name: "seven rounds", cfg: `{"rounds":7}`, wantErr: "outside"},
		{name: "negative rounds", cfg: `{"rounds":-1}`, wantErr: "outside"},
		{name: "short window", cfg: `{"roundSeconds":19}`, wantErr: "outside"},
		{name: "long window", cfg: `{"roundSeconds":301}`, wantErr: "outside"},
		{name: "objective reuse requested", cfg: `{"allowObjectiveReuse":true}`,
			wantErr: "may not satisfy several objectives"},
		{name: "malformed", cfg: `{"rounds":"three"}`, wantErr: "invalid config"},
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

// TestParticipantBounds covers the 3–8 player bound.
func TestParticipantBounds(t *testing.T) {
	for _, players := range []int{0, 1, 2, 9} {
		t.Run(fmt.Sprintf("players%d", players), func(t *testing.T) {
			_, err := New().Prepare(context.Background(), modes.PrepareInput{
				Scope:   scope("bounds-seed"),
				Subject: roster(players),
				Config:  json.RawMessage(`{}`),
				Content: packOf(),
			})
			if err == nil {
				t.Fatalf("Prepare accepted a %d-player round", players)
			}
		})
	}
	// Three players is the documented minimum and must work.
	_, err := New().Prepare(context.Background(), modes.PrepareInput{
		Scope:   scope("minimum-seed"),
		Subject: roster(3),
		Config:  json.RawMessage(`{}`),
		Content: packOf(),
	})
	if err != nil {
		t.Fatalf("Prepare refused the three-player minimum: %v", err)
	}
}

// TestDuplicateSubjectRefused asserts a duplicated roster entry is refused.
func TestDuplicateSubjectRefused(t *testing.T) {
	subjects := []modes.Subject{
		{ID: "p1", Kind: modes.SubjectPlayer},
		{ID: "p1", Kind: modes.SubjectPlayer},
		{ID: "p3", Kind: modes.SubjectPlayer},
	}
	_, err := New().Prepare(context.Background(), modes.PrepareInput{
		Scope:   scope("dup-seed"),
		Subject: subjects,
		Config:  json.RawMessage(`{}`),
	})
	if err == nil {
		t.Fatal("Prepare accepted a duplicated subject")
	}
	if !strings.Contains(err.Error(), "duplicate subject") {
		t.Fatalf("error %q does not mention the duplicate", err)
	}
}

// TestRoundOverRefusesProposals asserts no proposal is accepted once every round is
// over.
func TestRoundOverRefusesProposals(t *testing.T) {
	prep, pub, _ := prepareRound(t, "over-seed", `{"rounds":1}`, 3)
	var board publicState
	if err := json.Unmarshal(prep.Public, &board); err != nil {
		t.Fatalf("decode public: %v", err)
	}
	board.CurrentRound = board.Config.Rounds + 1
	prep.Public = mustJSON(t, board)

	dec := apply(t, prep, modes.Subject{ID: "p1", Kind: modes.SubjectPlayer},
		proposeAction("p2", pub.Hands["p1"][0], pub.Hands["p2"][0]), nil)
	if dec.Accepted {
		t.Fatal("a proposal was accepted after the last round")
	}
	if dec.Code != CodeRoundClosed {
		t.Fatalf("code = %q, want %q", dec.Code, CodeRoundClosed)
	}
}

// TestHandsArePublic asserts every player sees every hand, which the first version
// fixes .
func TestHandsArePublic(t *testing.T) {
	prep, pub, _ := prepareRound(t, "public-hands-seed", `{}`, 3)
	for _, entry := range pub.Subjects {
		view := project(t, prep, modes.Viewer{MembershipID: "m", Role: "player",
			Subject: modes.Subject{ID: entry.SubjectID, Kind: modes.SubjectPlayer}},
			modes.PhaseCollecting, nil)
		if len(view.Hands) != len(pub.Subjects) {
			t.Fatalf("%s sees %d hands, want %d", entry.SubjectID, len(view.Hands), len(pub.Subjects))
		}
		for _, other := range pub.Subjects {
			if len(view.Hands[other.SubjectID]) != HandSize {
				t.Fatalf("%s cannot see %s's hand", entry.SubjectID, other.SubjectID)
			}
		}
	}
}

// TestSpectatorGetsNoAssignment asserts a viewer without a seat receives no per-player
// objective assignment.
//
// The objective CATALOGUE is public by design ( requires every scoring condition to
// be known before play), so a spectator legitimately reads the catalogue text. What a
// spectator must never receive is which player holds which objective — that is the private
// half. The first version of this test treated the whole catalogue as secret, which
// contradicted the documented requirement.
func TestSpectatorGetsNoAssignment(t *testing.T) {
	prep, _, priv := prepareRound(t, "spectator-seed", `{}`, 3)
	for _, viewer := range []modes.Viewer{
		{MembershipID: "s1", Role: "spectator"},
		{MembershipID: "d1", Role: "display"},
		{MembershipID: "h1", Role: "host", IsReferee: true},
		{MembershipID: "x1", Role: "player",
			Subject: modes.Subject{ID: "not-on-roster", Kind: modes.SubjectPlayer}},
	} {
		view := project(t, prep, viewer, modes.PhaseCollecting, nil)
		// No per-player assignment.
		if len(view.SelfObjectives) != 0 {
			t.Fatalf("%s received %d objective assignments", viewer.Role, len(view.SelfObjectives))
		}
		if view.Self != nil {
			t.Fatalf("%s received a self view", viewer.Role)
		}
		// The catalogue itself is fine to read, and must be there.
		if len(view.Objectives) != len(priv.ObjectivePool) {
			t.Fatalf("%s sees %d catalogue entries, want %d",
				viewer.Role, len(view.Objectives), len(priv.ObjectivePool))
		}
		// The assignment mapping must not be recoverable from the payload.
		var generic map[string]json.RawMessage
		if err := json.Unmarshal(mustJSON(t, view), &generic); err != nil {
			t.Fatalf("decode projection: %v", err)
		}
		for key := range generic {
			if strings.Contains(strings.ToLower(key), "assignment") {
				t.Fatalf("%s sees the field %q", viewer.Role, key)
			}
		}
	}
}

// TestResultOnlyAtSettle asserts the final valuation appears only once the match is
// over.
func TestResultOnlyAtSettle(t *testing.T) {
	prep, _, _ := prepareRound(t, "result-seed", `{}`, 3)
	player := modes.Viewer{MembershipID: "m1", Role: "player",
		Subject: modes.Subject{ID: "p1", Kind: modes.SubjectPlayer}}
	for _, phase := range []modes.Phase{modes.PhaseCollecting, modes.PhaseLocked} {
		view := project(t, prep, player, phase, nil)
		if view.Result != nil {
			t.Fatalf("the result is visible at phase %q", phase)
		}
	}
	prep = finishTrading(t, prep)
	settled := project(t, prep, player, modes.PhaseSettled, nil)
	if settled.Result == nil {
		t.Fatal("the settled projection carries no result")
	}
	if len(settled.Result.Players) != 3 {
		t.Fatalf("the result lists %d players, want 3", len(settled.Result.Players))
	}
	objectives, err := loadObjectives(packOf())
	if err != nil {
		t.Fatalf("loadObjectives: %v", err)
	}
	if len(settled.Result.Objectives) != len(objectives) {
		t.Fatalf("the result lists %d objectives, want the full catalogue %d",
			len(settled.Result.Objectives), len(objectives))
	}
}

// TestPrepareIsDeterministic asserts the deal is a pure function of the scope.
func TestPrepareIsDeterministic(t *testing.T) {
	first, _, _ := prepareRound(t, "det-seed", `{}`, 4)
	second, _, _ := prepareRound(t, "det-seed", `{}`, 4)
	if string(first.Public) != string(second.Public) {
		t.Fatal("public state is not deterministic")
	}
	if string(first.Private) != string(second.Private) {
		t.Fatal("private state is not deterministic")
	}
	// A different seed deals different objectives.
	_, _, otherPriv := prepareRound(t, "det-other", `{}`, 4)
	if string(mustJSON(t, otherPriv.Objectives)) == string(mustJSON(t, objectivesOf(t, first))) {
		t.Fatal("a different seed dealt the same objectives")
	}
}

// objectivesOf decodes the objectives of a prepared round.
func objectivesOf(t *testing.T, prep modes.Prepared) map[string][]objective {
	t.Helper()
	var priv privateState
	if err := json.Unmarshal(prep.Private, &priv); err != nil {
		t.Fatalf("decode private: %v", err)
	}
	return priv.Objectives
}

// TestInstanceIDsAreOpaque asserts an instance id does not name its card kind or its
// owner in a readable form, so the hand board cannot be reconstructed from ids alone
// in a way the labels do not already show.
func TestInstanceIDsAreOpaque(t *testing.T) {
	_, pub, _ := prepareRound(t, "opaque-seed", `{}`, 4)
	seen := make(map[string]struct{}, len(pub.Instances))
	for _, inst := range pub.Instances {
		if inst.InstanceID == "" || inst.CardID == "" || inst.Label == "" {
			t.Fatalf("instance is incomplete: %+v", inst)
		}
		if _, dup := seen[inst.InstanceID]; dup {
			t.Fatalf("instance id %q appears twice", inst.InstanceID)
		}
		seen[inst.InstanceID] = struct{}{}
		if strings.Contains(inst.InstanceID, inst.CardID) {
			t.Fatalf("instance id %q contains its card kind %q", inst.InstanceID, inst.CardID)
		}
		for _, entry := range pub.Subjects {
			if strings.Contains(inst.InstanceID, entry.SubjectID) {
				t.Fatalf("instance id %q contains an owner id %q", inst.InstanceID, entry.SubjectID)
			}
		}
	}
}

// TestCommitReplayIsIdempotent asserts folding the same accepted action twice cannot
// produce a different board, which is what makes a retried transaction safe.
func TestCommitReplayIsIdempotent(t *testing.T) {
	prep, pub, _ := prepareRound(t, "replay-seed", `{}`, 3)
	sender := modes.Subject{ID: "p1", Kind: modes.SubjectPlayer}
	recipient := modes.Subject{ID: "p2", Kind: modes.SubjectPlayer}

	prep, senderState := commit(t, prep, sender,
		proposeAction("p2", pub.Hands["p1"][0], pub.Hands["p2"][0]), nil)
	tradeID := dec0(t, prep, sender, senderState)

	in := modes.ApplyInput{
		Scope:   scope("replay-seed"),
		Subject: recipient,
		Action:  respondAction(tradeID, true),
		Public:  prep.Public,
		Private: prep.Private,
	}
	dec, err := New().Apply(in)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if !dec.Accepted {
		t.Fatalf("acceptance rejected: %q %q", dec.Code, dec.Detail)
	}
	firstPub, firstPriv, err := New().CommitShared(in, dec)
	if err != nil {
		t.Fatalf("CommitShared: %v", err)
	}
	secondPub, secondPriv, err := New().CommitShared(in, dec)
	if err != nil {
		t.Fatalf("CommitShared replay: %v", err)
	}
	if string(firstPub) != string(secondPub) {
		t.Fatal("replaying the same acceptance produced different public state")
	}
	if string(firstPriv) != string(secondPriv) {
		t.Fatal("replaying the same acceptance produced different private state")
	}
}

// TestCommitRejectsStaleBoard asserts the fold refuses an exchange whose instances
// have moved, so a half-swap is impossible.
func TestCommitRejectsStaleBoard(t *testing.T) {
	prep, pub, _ := prepareRound(t, "stale-seed", `{}`, 3)
	sender := modes.Subject{ID: "p1", Kind: modes.SubjectPlayer}
	recipient := modes.Subject{ID: "p2", Kind: modes.SubjectPlayer}

	prep, senderState := commit(t, prep, sender,
		proposeAction("p2", pub.Hands["p1"][0], pub.Hands["p2"][0]), nil)
	tradeID := dec0(t, prep, sender, senderState)

	// Rewrite the board so the sender no longer holds the offered instance, which
	// simulates a concurrent trade that moved it.
	var board publicState
	if err := json.Unmarshal(prep.Public, &board); err != nil {
		t.Fatalf("decode public: %v", err)
	}
	offered := pub.Hands["p1"][0]
	kept := make([]string, 0, len(board.Hands["p1"]))
	for _, id := range board.Hands["p1"] {
		if id != offered {
			kept = append(kept, id)
		}
	}
	board.Hands["p1"] = kept
	stalePublic := mustJSON(t, board)

	in := modes.ApplyInput{
		Scope:   scope("stale-seed"),
		Subject: recipient,
		Action:  respondAction(tradeID, true),
		Public:  stalePublic,
		Private: prep.Private,
	}
	// Apply refuses the acceptance because the instance is gone.
	dec, err := New().Apply(in)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if dec.Accepted {
		t.Fatal("an acceptance against a stale board was accepted")
	}
	if dec.Code != CodeInstanceGone {
		t.Fatalf("code = %q, want %q", dec.Code, CodeInstanceGone)
	}

	// And the fold refuses it directly too, so a caller that skipped Apply cannot
	// produce a half-swap.
	_, _, err = New().CommitShared(in, modes.Decision{Accepted: true})
	if err == nil {
		t.Fatal("CommitShared accepted a stale exchange")
	}
	if !strings.Contains(err.Error(), "no longer holds") {
		t.Fatalf("error %q does not explain the stale hand", err)
	}
	_ = senderState
}

// TestCancelFreesSlot asserts the caller may propose again after cancelling.
func TestCancelFreesSlot(t *testing.T) {
	prep, pub, _ := prepareRound(t, "free-slot-seed", `{}`, 3)
	sender := modes.Subject{ID: "p1", Kind: modes.SubjectPlayer}

	prep, senderState := commit(t, prep, sender,
		proposeAction("p2", pub.Hands["p1"][0], pub.Hands["p2"][0]), nil)
	tradeID := dec0(t, prep, sender, senderState)
	prep, senderState = commit(t, prep, sender, cancelAction(tradeID), senderState)

	// A new proposal is accepted.
	dec := apply(t, prep, sender,
		proposeAction("p3", pub.Hands["p1"][1], pub.Hands["p3"][0]), senderState)
	if !dec.Accepted {
		t.Fatalf("a proposal after cancelling was refused: %q %q", dec.Code, dec.Detail)
	}
	// The cancelled proposal stays in the log as cancelled.
	var after publicState
	if err := json.Unmarshal(prep.Public, &after); err != nil {
		t.Fatalf("decode public: %v", err)
	}
	record, ok := findTrade(after, tradeID)
	if !ok {
		t.Fatalf("the cancelled proposal %q left the log", tradeID)
	}
	if record.Status != StatusCancelled {
		t.Fatalf("status = %q, want %q", record.Status, StatusCancelled)
	}
}

// contains reports whether list holds value.
func contains(list []string, value string) bool {
	for _, item := range list {
		if item == value {
			return true
		}
	}
	return false
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

// TestPrepareRefusesWithoutPack asserts the mode has no built-in fallback.
//
// A fallback would make "the compiler produced no content" indistinguishable from "the
// mode works". That is the failure this project has hit twice (a review ledger that
// claimed approvals nobody made, and an admission report that said available while
// Prepare refused). The mode therefore refuses, and the refusal names the missing section
// so a host sees what is actually wrong.
func TestPrepareRefusesWithoutPack(t *testing.T) {
	cases := []struct {
		name    string
		content modes.ContentSource
		want    string
	}{
		{name: "nil content", content: nil, want: "no content source"},
		{name: "empty pack", content: fakeContent{}, want: "trade.cards"},
		{
			name:    "cards only",
			content: packWithRaw(`[{"cardId":"a","label":"A","provenance":{"kind":"authored-design-value","notOfficialData":true,"author":"t","basis":"b"}}]`, ""),
			want:    "trade.objectives",
		},
		{
			name:    "objectives only",
			content: packWithRaw("", `[{"objectiveId":"o1","text":"t","kinds":["a","b"],"points":50,"provenance":{"kind":"authored-design-value","notOfficialData":true,"author":"t","basis":"b"}}]`),
			want:    "trade.cards",
		},
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
			// The refusal must be actionable, not a panic or a bare decode error.
			if !strings.Contains(err.Error(), "required") {
				t.Fatalf("the refusal does not say the section is required: %q", err)
			}
		})
	}
}

// TestObjectivePoolMustCoverEverySeat asserts the pool gate.
//
// Eight players times two objectives is sixteen slots. A smaller pool must be refused with
// the exact shortfall, never satisfied by wrapping around: a repeated objective gives two
// players the identical goal, which forces them to cooperate or collide — a gameplay break
// rather than a tolerable shortage.
func TestObjectivePoolMustCoverEverySeat(t *testing.T) {
	cases := []struct {
		name    string
		players int
		pool    int
		wantErr bool
	}{
		{name: "3 players need 6, pool 16", players: 3, pool: 16},
		{name: "8 players need 16, pool 16", players: 8, pool: 16},
		{name: "8 players need 16, pool 15", players: 8, pool: 15, wantErr: true},
		{name: "3 players need 6, pool 5", players: 3, pool: 5, wantErr: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := New().Prepare(context.Background(), modes.PrepareInput{
				Scope:   scope("pool-seed"),
				Subject: roster(tc.players),
				Config:  json.RawMessage(`{}`),
				Content: packWithObjectives(tc.pool),
			})
			if tc.wantErr {
				if err == nil {
					t.Fatalf("Prepare admitted %d players on a %d-objective pool", tc.players, tc.pool)
				}
				if !strings.Contains(err.Error(), "objectives are needed") {
					t.Fatalf("error %q does not state the shortfall", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("Prepare refused a sufficient pool: %v", err)
			}
		})
	}
}

// TestObjectivesAreNotDealtTwice asserts no objective reaches two players.
func TestObjectivesAreNotDealtTwice(t *testing.T) {
	for _, players := range []int{3, 5, 8} {
		t.Run(fmt.Sprintf("players%d", players), func(t *testing.T) {
			_, _, priv := prepareRound(t, "no-dup-seed", `{}`, players)
			seen := make(map[string]string, players*ObjectiveCount)
			for subjectID, list := range priv.Objectives {
				if len(list) != ObjectiveCount {
					t.Fatalf("%s holds %d objectives, want %d", subjectID, len(list), ObjectiveCount)
				}
				for _, obj := range list {
					if prev, dup := seen[obj.ObjectiveID]; dup {
						t.Fatalf("objective %q was dealt to both %s and %s",
							obj.ObjectiveID, prev, subjectID)
					}
					seen[obj.ObjectiveID] = subjectID
				}
			}
			if len(seen) != players*ObjectiveCount {
				t.Fatalf("%d distinct objectives dealt, want %d", len(seen), players*ObjectiveCount)
			}
		})
	}
}

// TestProvenanceIsValidated asserts the provenance block is checked, not merely carried.
//
// The rule layer can verify the enum value, the notOfficialData flag and the presence of an
// author and a basis. It CANNOT verify that basis names a real document — that is a human
// review boundary. These cases cover exactly the part the program can check.
func TestProvenanceIsValidated(t *testing.T) {
	valid := `{"kind":"authored-design-value","notOfficialData":true,"author":"t","basis":"b"}`
	cardsWith := func(prov string) string {
		return `[{"cardId":"speed","label":"速度档案","provenance":` + prov + `}]`
	}
	objectives := `[{"objectiveId":"o1","text":"t","kinds":["speed","stamina"],"points":50,"provenance":` + valid + `}]`

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
				Content: packWithRaw(cardsWith(tc.prov), objectives),
			})
			if err == nil {
				t.Fatalf("Prepare accepted a card whose provenance was %q", tc.name)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error %q does not mention %q", err, tc.want)
			}
		})
	}

	// A missing provenance block is refused too, rather than defaulting to "official".
	t.Run("no provenance block", func(t *testing.T) {
		_, err := New().Prepare(context.Background(), modes.PrepareInput{
			Scope:   scope("prov-seed"),
			Subject: roster(3),
			Config:  json.RawMessage(`{}`),
			Content: packWithRaw(`[{"cardId":"speed","label":"速度档案"}]`, objectives),
		})
		if err == nil {
			t.Fatal("Prepare accepted a card with no provenance block")
		}
		if !strings.Contains(err.Error(), "no provenance block") {
			t.Fatalf("error %q does not mention the missing block", err)
		}
	})

	// The valid fixture passes.
	t.Run("valid", func(t *testing.T) {
		_, err := New().Prepare(context.Background(), modes.PrepareInput{
			Scope:   scope("prov-seed"),
			Subject: roster(3),
			Config:  json.RawMessage(`{}`),
			Content: packWithRaw(cardsWith(valid), objectives),
		})
		// The pool is too small for three players here, so the refusal is the pool gate,
		// not the provenance gate: assert it is not a provenance error.
		if err != nil && strings.Contains(err.Error(), "provenance") {
			t.Fatalf("a valid provenance block was refused: %v", err)
		}
	})
}

// TestObjectiveCatalogueIsPublishedAtStart verifies the scoring catalogue is public at round start.
func TestObjectiveCatalogueIsPublishedAtStart(t *testing.T) {
	prep, pub, _ := prepareRound(t, "catalogue-seed", `{}`, 4)
	if len(pub.Objectives) != len(fixtureObjectivePairs) {
		t.Fatalf("published %d objectives, want the full catalogue %d",
			len(pub.Objectives), len(fixtureObjectivePairs))
	}
	if len(pub.CardKinds) != len(fixtureCardKinds) {
		t.Fatalf("published %d card kinds, want %d", len(pub.CardKinds), len(fixtureCardKinds))
	}
	// Every published objective must carry its scoring condition.
	for _, obj := range pub.Objectives {
		if obj.Points <= 0 {
			t.Fatalf("objective %q publishes no points", obj.ObjectiveID)
		}
		if len(obj.Kinds) == 0 {
			t.Fatalf("objective %q publishes no card requirement", obj.ObjectiveID)
		}
		if obj.Provenance.Kind == "" || !obj.Provenance.NotOfficialData {
			t.Fatalf("objective %q does not mark itself as authored design", obj.ObjectiveID)
		}
	}
	// The catalogue reaches the projection, so a player can plan before acting.
	view := project(t, prep, modes.Viewer{MembershipID: "m1", Role: "player",
		Subject: modes.Subject{ID: "p1", Kind: modes.SubjectPlayer}}, modes.PhaseCollecting, nil)
	if len(view.Objectives) != len(pub.Objectives) {
		t.Fatalf("projection publishes %d objectives, want %d",
			len(view.Objectives), len(pub.Objectives))
	}
}

// TestRequirementFieldsMatchWhatTheModeReads asserts the declared content requirement names
// exactly the sections the mode actually loads.
//
// This is the regression for the defect class that produced two "available but cannot
// start" failures in this project: a mode declared one section and read another (or
// declared nothing and relied on built-in constants). The check is structural — the
// declaration is compared against the loader constants — so it cannot drift silently.
func TestRequirementFieldsMatchWhatTheModeReads(t *testing.T) {
	fields := New().Requirement().Fields
	want := map[string]bool{
		CardsSection:      false,
		ObjectivesSection: false,
	}
	if len(fields) != len(want) {
		t.Fatalf("Requirement().Fields = %v, want exactly %v", fields, keysOf(want))
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

// keysOf returns the keys of a set map, for error messages.
func keysOf(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

// TestQuestionCountIsThePoolSize asserts QuestionCount reports usable objectives, not players.
//
// The availability gate compares QuestionCount against MinEntities, which is also a pool floor.
// Reporting the table size made the two incomparable and under-reported the pool: an 8-player
// round said 8 while the pack held 16 objectives.
func TestQuestionCountIsThePoolSize(t *testing.T) {
	for _, players := range []int{3, 5, 8} {
		t.Run(fmt.Sprintf("players%d", players), func(t *testing.T) {
			prep, pub, _ := prepareRound(t, "pool-size-seed", `{}`, players)
			if prep.QuestionCount != len(pub.Objectives) {
				t.Fatalf("questionCount = %d, the pack publishes %d objectives",
					prep.QuestionCount, len(pub.Objectives))
			}
			if prep.QuestionCount == players {
				t.Fatalf("questionCount equals the player count (%d); it must be the pool size",
					players)
			}
			// The gate must pass: the pool covers the table's demand.
			req := New().Requirement()
			if prep.QuestionCount < req.MinEntities {
				t.Fatalf("the pack reports %d objectives but MinEntities is %d",
					prep.QuestionCount, req.MinEntities)
			}
		})
	}
}

// TestMinEntitiesIsTheTableDemand asserts the trade pool floor is players×objectives, expressed
// with the right units.
func TestMinEntitiesIsTheTableDemand(t *testing.T) {
	req := New().Requirement()
	want := MinSubjects * ObjectiveCount
	if req.MinEntities != want {
		t.Fatalf("MinEntities = %d, want %d (MinSubjects %d × ObjectiveCount %d)",
			req.MinEntities, want, MinSubjects, ObjectiveCount)
	}
	// It must not be the card count: HandSize is cards per hand, a different unit from the
	// objective pool.
	if req.MinEntities == MinSubjects*HandSize {
		t.Fatal("MinEntities counts cards per hand against a pool of objectives")
	}
}

// TestEmptyTradesIsExplained asserts an empty proposal log says WHY it is empty.
//
// An empty log is the normal opening state — nobody has proposed yet. But a player who sees an
// empty list must be told that, or it looks like the field was forgotten. Same rule as
// clue-puzzle's sharedEmptyReason and creative's ballotHiddenReason.
func TestEmptyTradesIsExplained(t *testing.T) {
	prep, pub, _ := prepareRound(t, "trades-reason-seed", `{}`, 3)
	view := project(t, prep, modes.Viewer{MembershipID: "m1", Role: "player",
		Subject: modes.Subject{ID: "p1", Kind: modes.SubjectPlayer}}, modes.PhaseCollecting, nil)
	if len(view.Trades) != 0 {
		t.Fatalf("the log lists %d proposals at open", len(view.Trades))
	}
	if view.TradesEmptyReason == "" {
		t.Fatal("the log is empty but carries no explanation")
	}
	_ = pub
}

// TestTradesReasonDisappearsWhenPopulated asserts the explanation is omitted once a proposal
// exists.
func TestTradesReasonDisappearsWhenPopulated(t *testing.T) {
	prep, pub, _ := prepareRound(t, "trades-populated-seed", `{}`, 3)
	prep, _ = commit(t, prep, modes.Subject{ID: "p1", Kind: modes.SubjectPlayer},
		proposeAction("p2", pub.Hands["p1"][0], pub.Hands["p2"][0]), nil)

	view := project(t, prep, modes.Viewer{MembershipID: "m1", Role: "player",
		Subject: modes.Subject{ID: "p1", Kind: modes.SubjectPlayer}}, modes.PhaseCollecting, nil)
	if len(view.Trades) == 0 {
		t.Fatal("the log is empty after a proposal")
	}
	if view.TradesEmptyReason != "" {
		t.Fatalf("a populated log still carries the empty explanation %q", view.TradesEmptyReason)
	}
}
