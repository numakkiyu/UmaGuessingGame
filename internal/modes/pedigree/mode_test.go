package pedigree

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

// entrySpec is the on-the-wire spelling of one compiled pedigree entry.
type entrySpec struct {
	CharacterID    string            `json:"characterId"`
	Name           string            `json:"name"`
	NKBID          string            `json:"nkbId"`
	Pedigree       map[string]string `json:"pedigree"`
	PrototypeNames struct {
		Ja           string `json:"ja"`
		ZhHans       string `json:"zhHans"`
		En           string `json:"en"`
		JaConfidence string `json:"jaConfidence"`
	} `json:"prototypeNames"`
}

// buildEntry assembles one entry with all six slots filled from a name stem.
func buildEntry(charID, stem string) entrySpec {
	e := entrySpec{
		CharacterID: charID,
		Name:        "角色" + stem,
		NKBID:       "nkb-" + stem,
		Pedigree: map[string]string{
			"sire":     stem + "-父",
			"dam":      stem + "-母",
			"sireSire": stem + "-父父",
			"sireDam":  stem + "-父母",
			"damSire":  stem + "-母父",
			"damDam":   stem + "-母母",
		},
	}
	e.PrototypeNames.Ja = "プロト" + stem
	e.PrototypeNames.JaConfidence = "confirmed"
	e.PrototypeNames.ZhHans = "原型" + stem
	e.PrototypeNames.En = "Proto " + stem
	return e
}

// packOf builds a content source carrying n generated entries.
func packOf(n int) modes.ContentSource {
	entries := make([]entrySpec, 0, n)
	for i := 0; i < n; i++ {
		entries = append(entries, buildEntry(fmt.Sprintf("c%03d", i), fmt.Sprintf("e%02d", i)))
	}
	raw, err := json.Marshal(entries)
	if err != nil {
		panic(err)
	}
	return fakeContent{"pedigree.entries": raw}
}

// packWith builds a content source from explicit entries.
func packWith(entries ...entrySpec) modes.ContentSource {
	raw, err := json.Marshal(entries)
	if err != nil {
		panic(err)
	}
	return fakeContent{"pedigree.entries": raw}
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

// prepareRound runs Prepare and returns the prepared round with decoded halves.
func prepareRound(t *testing.T, pack modes.ContentSource, seed, cfg string, subjects ...modes.Subject) (modes.Prepared, publicState, privateState) {
	t.Helper()
	if len(subjects) == 0 {
		subjects = []modes.Subject{{ID: "p1", Kind: modes.SubjectPlayer}}
	}
	prep, err := New().Prepare(context.Background(), modes.PrepareInput{
		Scope:   scope(seed),
		Subject: subjects,
		Config:  json.RawMessage(cfg),
		Content: pack,
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

// assignAction builds a contract-shaped pedigree.assignment payload.
func assignAction(pairs ...string) modes.ModeAction {
	// pairs are slotId, horseId, slotId, horseId...
	type item struct {
		SlotID  string `json:"slotId"`
		HorseID string `json:"horseId"`
	}
	items := make([]item, 0, len(pairs)/2)
	for i := 0; i+1 < len(pairs); i += 2 {
		items = append(items, item{SlotID: pairs[i], HorseID: pairs[i+1]})
	}
	body, err := json.Marshal(struct {
		Kind        string `json:"kind"`
		Assignments []item `json:"assignments"`
	}{Kind: ActionKindAssignment, Assignments: items})
	if err != nil {
		panic(err)
	}
	return modes.ModeAction{Kind: ActionKindAssignment, Payload: body}
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

// correctPairs returns the slot/horse pairs of the solution as flat arguments.
func correctPairs(priv privateState) []string {
	slots := make([]string, 0, len(priv.Solution))
	for slot := range priv.Solution {
		slots = append(slots, slot)
	}
	sort.Slice(slots, func(i, j int) bool { return slotOrder(slots[i]) < slotOrder(slots[j]) })
	out := make([]string, 0, len(slots)*2)
	for _, slot := range slots {
		out = append(out, slot, priv.Solution[slot])
	}
	return out
}

// TestDisclaimerAlwaysPresent covers the 「史实原型血统标注」 requirement: the board
// must always state that this is historical prototype lineage, not a character
// family relation.
func TestDisclaimerAlwaysPresent(t *testing.T) {
	prep, pub, _ := prepareRound(t, packOf(12), "disclaimer-seed", `{}`)
	if pub.Disclaimer != Disclaimer {
		t.Fatalf("disclaimer = %q, want %q", pub.Disclaimer, Disclaimer)
	}
	// The wording must contain both halves of the claim.
	for _, want := range []string{"史实原型血统", "不是角色"} {
		if !strings.Contains(pub.Disclaimer, want) {
			t.Fatalf("disclaimer %q does not contain %q", pub.Disclaimer, want)
		}
	}
	// It must also reach the projection, which is what the UI renders.
	view := project(t, prep, modes.Viewer{Subject: modes.Subject{ID: "p1", Kind: modes.SubjectPlayer}, Role: "player"}, modes.PhaseCollecting, nil)
	if view.Disclaimer != Disclaimer {
		t.Fatalf("projection disclaimer = %q", view.Disclaimer)
	}
	// And the settled explanation.
	scores := settle(t, prep, nil)
	var exp struct {
		Disclaimer string `json:"disclaimer"`
	}
	if err := json.Unmarshal(scores["p1"].Explanation, &exp); err != nil {
		t.Fatalf("decode explanation: %v", err)
	}
	if exp.Disclaimer != Disclaimer {
		t.Fatalf("explanation disclaimer = %q", exp.Disclaimer)
	}
}

// TestEntityMappingSeparatesPrototypeFromCharacter covers the 「原型与角色 ID 分离」
// and 「实体映射」 bullets.
//
// The subject block must carry the character id AND the prototype name AND the
// independent prototype id, and no slot may be answered by the character id.
func TestEntityMappingSeparatesPrototypeFromCharacter(t *testing.T) {
	pack := packOf(12)
	prep, pub, priv := prepareRound(t, pack, "entity-seed", `{"slotCount":6}`)

	if pub.Subject.CharacterID == "" {
		t.Fatal("the subject carries no character id")
	}
	if pub.Subject.PrototypeName == "" {
		t.Fatal("the subject carries no prototype name")
	}
	if pub.Subject.NKBID == "" {
		t.Fatal("the subject carries no independent prototype id")
	}
	if pub.Subject.CharacterName == "" {
		t.Fatal("the subject carries no character name")
	}
	// The character id must not be usable as an answer.
	known := make(map[string]struct{}, len(pub.Candidates))
	for _, cand := range pub.Candidates {
		known[cand.HorseID] = struct{}{}
	}
	if _, ok := known[pub.Subject.CharacterID]; ok {
		t.Fatal("the character id is published as an ancestor candidate")
	}
	// The solution must reference candidate ids, never character ids.
	for slot, horseID := range priv.Solution {
		if horseID == pub.Subject.CharacterID {
			t.Fatalf("slot %s is answered by the character id", slot)
		}
		if _, ok := known[horseID]; !ok {
			t.Fatalf("slot %s solution %q is not a published candidate", slot, horseID)
		}
	}

	// The projection must show the same separation.
	view := project(t, prep, modes.Viewer{Subject: modes.Subject{ID: "p1", Kind: modes.SubjectPlayer}, Role: "player"}, modes.PhaseCollecting, nil)
	if view.Subject.CharacterID != pub.Subject.CharacterID ||
		view.Subject.PrototypeName != pub.Subject.PrototypeName ||
		view.Subject.NKBID != pub.Subject.NKBID {
		t.Fatalf("projection subject = %+v", view.Subject)
	}
}

// TestRelationDirectionIsDistinct covers the 「关系方向」 bullet.
//
// Swapping 父 and 母 must cost a slot. The test builds a submission with the two
// parent slots swapped and asserts the score drops.
func TestRelationDirectionIsDistinct(t *testing.T) {
	prep, pub, priv := prepareRound(t, packOf(12), "direction-seed", `{"slotCount":4}`)

	// The four-slot puzzle always includes both parents.
	slotIDs := make(map[string]struct{}, len(pub.Slots))
	for _, slot := range pub.Slots {
		slotIDs[slot.SlotID] = struct{}{}
	}
	for _, required := range []string{"sire", "dam"} {
		if _, ok := slotIDs[required]; !ok {
			t.Fatalf("a %d-slot puzzle omits the %q slot", len(pub.Slots), required)
		}
	}

	// Correct submission scores 100.
	dec, state := apply(t, prep, modes.Subject{ID: "p1", Kind: modes.SubjectPlayer},
		assignAction(correctPairs(priv)...), nil)
	if !dec.Accepted {
		t.Fatalf("correct submission rejected: %q %q", dec.Code, dec.Detail)
	}
	perfect := settle(t, prep, map[string]json.RawMessage{"p1": state})
	if got := perfect["p1"].ScoreUnits; got != 10000 {
		t.Fatalf("correct submission scored %d units, want 10000", got)
	}

	// Swapping sire and dam must lose exactly those two slots.
	swapped := make([]string, 0, len(priv.Solution)*2)
	for _, slot := range pub.Slots {
		switch slot.SlotID {
		case "sire":
			swapped = append(swapped, "sire", priv.Solution["dam"])
		case "dam":
			swapped = append(swapped, "dam", priv.Solution["sire"])
		default:
			swapped = append(swapped, slot.SlotID, priv.Solution[slot.SlotID])
		}
	}
	dec, state = apply(t, prep, modes.Subject{ID: "p1", Kind: modes.SubjectPlayer},
		assignAction(swapped...), nil)
	if !dec.Accepted {
		t.Fatalf("swapped submission rejected: %q %q", dec.Code, dec.Detail)
	}
	swappedScore := settle(t, prep, map[string]json.RawMessage{"p1": state})
	// Two slots wrong out of four -> floor(100*2/4) = 50.
	if got := swappedScore["p1"].ScoreUnits; got != 5000 {
		t.Fatalf("swapped submission scored %d units, want 5000", got)
	}

	// The direction legend must spell out the difference.
	legend := make(map[string]string, len(pub.Rules))
	for _, rule := range pub.Rules {
		legend[rule.SlotID] = rule.Means
	}
	if legend["sire"] == legend["dam"] {
		t.Fatal("the published legend gives 父 and 母 the same meaning")
	}
	if !strings.Contains(legend["sireDam"], "父亲") || !strings.Contains(legend["damSire"], "母亲") {
		t.Fatalf("grandparent directions are not spelled out: %+v", legend)
	}
}

// TestDuplicateAncestorRejected covers the 「重复原型」 bullet.
func TestDuplicateAncestorRejected(t *testing.T) {
	prep, _, priv := prepareRound(t, packOf(12), "dup-seed", `{"slotCount":4}`)
	pairs := correctPairs(priv)
	if len(pairs) < 4 {
		t.Fatalf("solution has %d slots", len(pairs)/2)
	}
	// Assign the same candidate to two different slots.
	duplicated := []string{
		pairs[0], pairs[1],
		pairs[2], pairs[1],
	}
	dec, _ := apply(t, prep, modes.Subject{ID: "p1", Kind: modes.SubjectPlayer},
		assignAction(duplicated...), nil)
	if dec.Accepted {
		t.Fatal("a submission assigning one ancestor to two slots was accepted")
	}
	if dec.Code != CodeDuplicateHorse {
		t.Fatalf("code = %q, want %q", dec.Code, CodeDuplicateHorse)
	}
}

// TestDuplicateAncestorInSourceRefused asserts a record that names the same horse
// in two slots is not used as a puzzle at all.
func TestDuplicateAncestorInSourceRefused(t *testing.T) {
	// Every entry is ambiguous: sire and dam are the same horse. With no usable
	// entry, Prepare must refuse rather than ship an unsolvable puzzle.
	bad := buildEntry("c001", "bad")
	bad.Pedigree["dam"] = bad.Pedigree["sire"]
	_, err := New().Prepare(context.Background(), modes.PrepareInput{
		Scope:   scope("dup-source-seed"),
		Subject: []modes.Subject{{ID: "p1", Kind: modes.SubjectPlayer}},
		Config:  json.RawMessage(`{}`),
		Content: packWith(bad),
	})
	if err == nil {
		t.Fatal("Prepare accepted a record that repeats an ancestor across slots")
	}
	if !strings.Contains(err.Error(), "same ancestor") {
		t.Fatalf("error %q does not name the ambiguity", err)
	}
}

// TestLookAlikeNamesAreDistinct covers the 「同冠名误连」 bullet.
//
// Two ancestors whose names share a long prefix (the classic シンボリ… mistake) are
// different horses. The test asserts they are published as separate candidates
// with separate ids, and that choosing the look-alike is scored wrong.
func TestLookAlikeNamesAreDistinct(t *testing.T) {
	// Build a pack where the subject's sire is "シンボリルドルフ" and a distractor is
	// "シンボリクリスエス(USA)" — a real pair that a prefix match would merge.
	//
	// Only the first entry may be a puzzle subject: the other entries deliberately
	// lack an independent prototype id, so the seed cannot pick one of them. They
	// still contribute their ancestors as distractors, which is exactly the
	// decoy pool this test needs.
	subject := buildEntry("c001", "main")
	subject.Pedigree["sire"] = "シンボリルドルフ"
	subject.Pedigree["dam"] = "トウカイナチユラル"
	subject.Pedigree["sireSire"] = "パーソロン(IRE)"
	subject.Pedigree["sireDam"] = "スイートルナ"
	subject.Pedigree["damSire"] = "ナイスダンサー(CAN)"
	subject.Pedigree["damDam"] = "トウカイミドリ"

	lookAlike := buildEntry("c002", "other")
	lookAlike.Pedigree["sire"] = "シンボリクリスエス(USA)"
	lookAlike.Pedigree["dam"] = "トウカイポイント"
	lookAlike.Pedigree["sireSire"] = "トニービン(IRE)"
	lookAlike.Pedigree["sireDam"] = "スカーレットブーケ"
	lookAlike.Pedigree["damSire"] = "ダンシングブレーヴ(USA)"
	lookAlike.Pedigree["damDam"] = "スカーレツトインク(USA)"

	lookAlike.NKBID = ""
	fill1 := buildEntry("c003", "fill1")
	fill1.NKBID = ""
	fill2 := buildEntry("c004", "fill2")
	fill2.NKBID = ""
	fill3 := buildEntry("c005", "fill3")
	fill3.NKBID = ""
	pack := packWith(subject, lookAlike, fill1, fill2, fill3)

	prep, pub, priv := prepareRound(t, pack, "lookalike-seed", `{"slotCount":4,"candidateCount":8}`)

	// The two look-alike names must both be candidates, with different ids.
	byName := make(map[string]string, len(pub.Candidates))
	for _, cand := range pub.Candidates {
		if prev, dup := byName[cand.Name]; dup {
			t.Fatalf("candidate name %q published twice (ids %s and %s)", cand.Name, prev, cand.HorseID)
		}
		byName[cand.Name] = cand.HorseID
	}
	rightID, okRight := byName["シンボリルドルフ"]
	if !okRight {
		t.Fatalf("the correct ancestor is not published: %v", byName)
	}
	wrongID, okWrong := byName["シンボリクリスエス(USA)"]
	if !okWrong {
		t.Fatalf("the look-alike ancestor is not published: %v", byName)
	}
	if rightID == wrongID {
		t.Fatal("the two look-alike ancestors share one candidate id")
	}

	// The solution must name the correct one.
	if priv.Solution["sire"] != rightID {
		t.Fatalf("sire solution = %q, want %q", priv.Solution["sire"], rightID)
	}

	// Choosing the look-alike for the sire slot must be scored wrong.
	// Submit the whole board with the sire slot answered by the look-alike and
	// every other slot correct. Exactly one slot is wrong, so the score is
	// floor(100*(n-1)/n) where n is the board size.
	pairs := correctPairs(priv)
	submission := make([]string, 0, len(pairs))
	for i := 0; i+1 < len(pairs); i += 2 {
		slotID, horseID := pairs[i], pairs[i+1]
		if slotID == "sire" {
			horseID = wrongID
		}
		submission = append(submission, slotID, horseID)
	}
	dec, state := apply(t, prep, modes.Subject{ID: "p1", Kind: modes.SubjectPlayer},
		assignAction(submission...), nil)
	if !dec.Accepted {
		t.Fatalf("submission rejected: %q %q", dec.Code, dec.Detail)
	}
	scores := settle(t, prep, map[string]json.RawMessage{"p1": state})
	n := len(pub.Slots)
	want := 100 * (n - 1) / n * 100
	if got := scores["p1"].ScoreUnits; got != want {
		t.Fatalf("look-alike submission scored %d units, want %d (one wrong of %d)",
			got, want, n)
	}
}

// TestCountrySuffixIsNormalised covers the name-normalisation rule: the same horse
// written with and without a stud-book marker is one horse, so the pool must not
// publish both spellings.
func TestCountrySuffixIsNormalised(t *testing.T) {
	cases := []struct {
		a, b string
		same bool
	}{
		{a: "サンデーサイレンス(USA)", b: "サンデーサイレンス", same: true},
		{a: "Halo(USA)", b: "Halo", same: true},
		{a: "  Halo (USA) ", b: "Halo", same: true},
		{a: "Wishing Well(USA)", b: "Wishing  Well", same: true},
		{a: "シンボリルドルフ", b: "シンボリクリスエス(USA)", same: false},
		{a: "トウカイナチユラル", b: "トウカイミドリ", same: false},
		{a: "Halo(USA)", b: "Halo(IRE)", same: true},
	}
	for _, tc := range cases {
		t.Run(tc.a+"_vs_"+tc.b, func(t *testing.T) {
			got := normalizeName(tc.a) == normalizeName(tc.b)
			if got != tc.same {
				t.Fatalf("normalizeName(%q)=%q vs (%q)=%q: same=%v, want %v",
					tc.a, normalizeName(tc.a), tc.b, normalizeName(tc.b), got, tc.same)
			}
		})
	}
}

// TestSlotCountBounds covers the 4–6 slot rule and the candidate-count gate.
func TestSlotCountBounds(t *testing.T) {
	cases := []struct {
		name    string
		cfg     string
		wantErr string
	}{
		{name: "default is four", cfg: `{}`},
		{name: "four", cfg: `{"slotCount":4}`},
		{name: "five", cfg: `{"slotCount":5}`},
		{name: "six", cfg: `{"slotCount":6}`},
		{name: "three", cfg: `{"slotCount":3}`, wantErr: "outside"},
		{name: "seven", cfg: `{"slotCount":7}`, wantErr: "outside"},
		{name: "candidates not above slots", cfg: `{"slotCount":6,"candidateCount":6}`,
			wantErr: "must exceed"},
		{name: "too many candidates", cfg: `{"candidateCount":21}`, wantErr: "above 20"},
		{name: "window too short", cfg: `{"answerSeconds":9}`, wantErr: "outside"},
		{name: "window too long", cfg: `{"answerSeconds":601}`, wantErr: "outside"},
		{name: "malformed", cfg: `{"slotCount":"four"}`, wantErr: "invalid config"},
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

// TestParentSlotsAlwaysIncluded covers the "a four-slot puzzle is not four
// grandparents" rule, which is what makes the board read as a pedigree.
func TestParentSlotsAlwaysIncluded(t *testing.T) {
	for _, count := range []int{4, 5, 6} {
		t.Run(fmt.Sprintf("slots%d", count), func(t *testing.T) {
			_, pub, _ := prepareRound(t, packOf(12), "slots-seed",
				fmt.Sprintf(`{"slotCount":%d}`, count))
			if len(pub.Slots) != count {
				t.Fatalf("board has %d slots, want %d", len(pub.Slots), count)
			}
			present := make(map[string]bool, count)
			for _, slot := range pub.Slots {
				present[slot.SlotID] = true
				if slot.Label == "" || len(slot.Path) == 0 {
					t.Fatalf("slot %s has no label or path", slot.SlotID)
				}
			}
			for _, required := range []string{"sire", "dam"} {
				if !present[required] {
					t.Fatalf("a %d-slot board omits %q", count, required)
				}
			}
			// Slots are published in the documented order.
			for i := 1; i < len(pub.Slots); i++ {
				if slotOrder(pub.Slots[i-1].SlotID) >= slotOrder(pub.Slots[i].SlotID) {
					t.Fatalf("slots are out of documented order: %v", pub.Slots)
				}
			}
		})
	}
}

// TestUnknownRecordNotPosed covers the 「未知不出题」 requirement.
//
// A record without an independent prototype id, or with an empty slot, must not
// become a puzzle.
func TestUnknownRecordNotPosed(t *testing.T) {
	cases := []struct {
		name    string
		mutate  func(*entrySpec)
		wantErr string
	}{
		{
			name:    "no nkb id",
			mutate:  func(e *entrySpec) { e.NKBID = "" },
			wantErr: "complete pedigree",
		},
		{
			name:    "empty sire",
			mutate:  func(e *entrySpec) { e.Pedigree["sire"] = "" },
			wantErr: "complete pedigree",
		},
		{
			name:    "empty damDam",
			mutate:  func(e *entrySpec) { e.Pedigree["damDam"] = "" },
			wantErr: "complete pedigree",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e := buildEntry("c001", "only")
			tc.mutate(&e)
			_, err := New().Prepare(context.Background(), modes.PrepareInput{
				Scope:   scope("unknown-seed"),
				Subject: []modes.Subject{{ID: "p1", Kind: modes.SubjectPlayer}},
				Config:  json.RawMessage(`{}`),
				Content: packWith(e),
			})
			if err == nil {
				t.Fatal("Prepare posed a puzzle from an unusable record")
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("error %q does not mention %q", err, tc.wantErr)
			}
		})
	}
}

// TestEmptyPackRefused asserts Prepare refuses a pack with no section at all,
// rather than inventing a puzzle.
func TestEmptyPackRefused(t *testing.T) {
	_, err := New().Prepare(context.Background(), modes.PrepareInput{
		Scope:   scope("empty-seed"),
		Subject: []modes.Subject{{ID: "p1", Kind: modes.SubjectPlayer}},
		Config:  json.RawMessage(`{}`),
		Content: fakeContent{},
	})
	if err == nil {
		t.Fatal("Prepare accepted a pack with no pedigree section")
	}
	_, err = New().Prepare(context.Background(), modes.PrepareInput{
		Scope:   scope("empty-seed"),
		Subject: []modes.Subject{{ID: "p1", Kind: modes.SubjectPlayer}},
		Config:  json.RawMessage(`{}`),
		Content: nil,
	})
	if err == nil {
		t.Fatal("Prepare accepted a nil content source")
	}
}

// TestParticipantBounds covers the single/two-person forms.
func TestParticipantBounds(t *testing.T) {
	pack := packOf(12)
	cases := []struct {
		name     string
		subjects []modes.Subject
		cfg      string
		wantErr  string
	}{
		{name: "single", subjects: []modes.Subject{{ID: "p1", Kind: modes.SubjectPlayer}}, cfg: `{}`},
		{name: "pair cooperative", subjects: []modes.Subject{
			{ID: "p1", Kind: modes.SubjectPlayer}, {ID: "p2", Kind: modes.SubjectPlayer},
		}, cfg: `{"cooperative":true}`},
		{name: "pair not marked cooperative", subjects: []modes.Subject{
			{ID: "p1", Kind: modes.SubjectPlayer}, {ID: "p2", Kind: modes.SubjectPlayer},
		}, cfg: `{}`, wantErr: "does not match"},
		{name: "single marked cooperative", subjects: []modes.Subject{
			{ID: "p1", Kind: modes.SubjectPlayer},
		}, cfg: `{"cooperative":true}`, wantErr: "does not match"},
		{name: "three", subjects: []modes.Subject{
			{ID: "p1", Kind: modes.SubjectPlayer},
			{ID: "p2", Kind: modes.SubjectPlayer},
			{ID: "p3", Kind: modes.SubjectPlayer},
		}, cfg: `{}`, wantErr: "outside"},
		{name: "none", subjects: nil, cfg: `{}`, wantErr: "outside"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := New().Prepare(context.Background(), modes.PrepareInput{
				Scope:   scope("bounds-seed"),
				Subject: tc.subjects,
				Config:  json.RawMessage(tc.cfg),
				Content: pack,
			})
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("Prepare rejected a legal round: %v", err)
				}
				return
			}
			if err == nil {
				t.Fatal("Prepare accepted an illegal round")
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("error %q does not mention %q", err, tc.wantErr)
			}
		})
	}
}

// TestCooperativePairSharesOneScore covers the 「单人/双人合作」 bullet.
func TestCooperativePairSharesOneScore(t *testing.T) {
	prep, pub, priv := prepareRound(t, packOf(12), "coop-seed", `{"slotCount":4,"cooperative":true}`,
		modes.Subject{ID: "p1", Kind: modes.SubjectPlayer},
		modes.Subject{ID: "p2", Kind: modes.SubjectPlayer})

	pairs := correctPairs(priv)
	// p1 fills the first two slots, p2 fills the last two. Together they are
	// complete, and the pair must score 100.
	half := len(pairs) / 2
	firstHalf := pairs[:half]
	secondHalf := pairs[half:]

	states := map[string]json.RawMessage{}
	dec, state := apply(t, prep, modes.Subject{ID: "p1", Kind: modes.SubjectPlayer},
		assignAction(firstHalf...), nil)
	if !dec.Accepted {
		t.Fatalf("p1 rejected: %q %q", dec.Code, dec.Detail)
	}
	states["p1"] = state

	dec, state = apply(t, prep, modes.Subject{ID: "p2", Kind: modes.SubjectPlayer},
		assignAction(secondHalf...), nil)
	if !dec.Accepted {
		t.Fatalf("p2 rejected: %q %q", dec.Code, dec.Detail)
	}
	states["p2"] = state

	scores := settle(t, prep, states)
	if scores["p1"].ScoreUnits != 10000 || scores["p2"].ScoreUnits != 10000 {
		t.Fatalf("cooperative pair scored %d and %d, want 10000 each",
			scores["p1"].ScoreUnits, scores["p2"].ScoreUnits)
	}
	// The explanation says each partner's own contribution and the union.
	var exp struct {
		CorrectSlots int  `json:"correctSlots"`
		OwnCorrect   int  `json:"ownCorrectSlots"`
		Cooperative  bool `json:"cooperative"`
	}
	if err := json.Unmarshal(scores["p1"].Explanation, &exp); err != nil {
		t.Fatalf("decode explanation: %v", err)
	}
	if exp.CorrectSlots != 4 {
		t.Fatalf("union correctSlots = %d, want 4", exp.CorrectSlots)
	}
	if exp.OwnCorrect != 2 {
		t.Fatalf("p1 ownCorrectSlots = %d, want 2", exp.OwnCorrect)
	}
	if !exp.Cooperative {
		t.Fatal("the explanation does not mark the round cooperative")
	}
	_ = pub
}

// TestSoloScoreIsOwnOnly asserts a solo round is not scored by a union with a
// non-existent partner.
func TestSoloScoreIsOwnOnly(t *testing.T) {
	prep, _, priv := prepareRound(t, packOf(12), "solo-seed", `{"slotCount":4}`)
	pairs := correctPairs(priv)
	// Fill only half the slots correctly.
	half := (len(pairs) / 2) / 2 * 2
	dec, state := apply(t, prep, modes.Subject{ID: "p1", Kind: modes.SubjectPlayer},
		assignAction(pairs[:half]...), nil)
	if !dec.Accepted {
		t.Fatalf("rejected: %q %q", dec.Code, dec.Detail)
	}
	scores := settle(t, prep, map[string]json.RawMessage{"p1": state})
	// Two of four correct -> 50.
	if got := scores["p1"].ScoreUnits; got != 5000 {
		t.Fatalf("solo half-correct scored %d units, want 5000", got)
	}
}

// TestPartialAndEmptySubmissions covers the scoring boundaries.
func TestPartialAndEmptySubmissions(t *testing.T) {
	prep, pub, priv := prepareRound(t, packOf(12), "partial-seed", `{"slotCount":6}`)
	if len(pub.Slots) != 6 {
		t.Fatalf("board has %d slots, want 6", len(pub.Slots))
	}
	pairs := correctPairs(priv)

	cases := []struct {
		name  string
		pairs []string
		want  int
	}{
		{name: "none", pairs: nil, want: 0},
		{name: "one of six", pairs: pairs[:2], want: 16},
		{name: "three of six", pairs: pairs[:6], want: 50},
		{name: "five of six", pairs: pairs[:10], want: 83},
		{name: "all six", pairs: pairs, want: 100},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			states := map[string]json.RawMessage{}
			if len(tc.pairs) > 0 {
				dec, state := apply(t, prep, modes.Subject{ID: "p1", Kind: modes.SubjectPlayer},
					assignAction(tc.pairs...), nil)
				if !dec.Accepted {
					t.Fatalf("rejected: %q %q", dec.Code, dec.Detail)
				}
				states["p1"] = state
			}
			scores := settle(t, prep, states)
			if got := scores["p1"].ScoreUnits; got != tc.want*100 {
				t.Fatalf("score = %d units, want %d", got, tc.want*100)
			}
		})
	}
}

// TestRevisionReplacesPreviousSubmission covers 「截止前可修订」.
func TestRevisionReplacesPreviousSubmission(t *testing.T) {
	prep, _, priv := prepareRound(t, packOf(12), "revise-seed", `{"slotCount":4}`)
	pairs := correctPairs(priv)

	// First submit a deliberately wrong mapping (swap the first two candidates).
	wrong := make([]string, len(pairs))
	copy(wrong, pairs)
	wrong[1], wrong[3] = wrong[3], wrong[1]

	dec, state := apply(t, prep, modes.Subject{ID: "p1", Kind: modes.SubjectPlayer},
		assignAction(wrong...), nil)
	if !dec.Accepted {
		t.Fatalf("first submission rejected: %q %q", dec.Code, dec.Detail)
	}
	first := settle(t, prep, map[string]json.RawMessage{"p1": state})
	if first["p1"].ScoreUnits == 10000 {
		t.Fatal("the deliberately wrong mapping scored full marks")
	}

	// Then revise to the correct mapping; the last accepted submission counts.
	dec, state = apply(t, prep, modes.Subject{ID: "p1", Kind: modes.SubjectPlayer},
		assignAction(pairs...), state)
	if !dec.Accepted {
		t.Fatalf("revision rejected: %q %q", dec.Code, dec.Detail)
	}
	second := settle(t, prep, map[string]json.RawMessage{"p1": state})
	if got := second["p1"].ScoreUnits; got != 10000 {
		t.Fatalf("revised submission scored %d units, want 10000", got)
	}
	// The revision is counted in the state.
	var st subjectState
	if err := json.Unmarshal(state, &st); err != nil {
		t.Fatalf("decode state: %v", err)
	}
	if st.Submissions != 2 {
		t.Fatalf("submissions = %d, want 2", st.Submissions)
	}
}

// TestInvalidSubmissionsRejected covers payload and board validation.
func TestInvalidSubmissionsRejected(t *testing.T) {
	prep, pub, _ := prepareRound(t, packOf(12), "invalid-seed", `{"slotCount":4}`)
	validHorse := pub.Candidates[0].HorseID

	cases := []struct {
		name string
		kind string
		body string
		want string
	}{
		{name: "unknown action", kind: "pedigree.teleport",
			body: `{"assignments":[{"slotId":"sire","horseId":"x"}]}`, want: CodeUnknownAction},
		{name: "empty payload", kind: ActionKindAssignment, body: ``, want: CodeInvalidPayload},
		{name: "not json", kind: ActionKindAssignment, body: `nope`, want: CodeInvalidPayload},
		{name: "kind mismatch", kind: ActionKindAssignment,
			body: `{"kind":"clue.assignment","assignments":[{"slotId":"sire","horseId":"x"}]}`,
			want: CodeInvalidPayload},
		{name: "no assignments", kind: ActionKindAssignment,
			body: `{"kind":"pedigree.assignment","assignments":[]}`, want: CodeEmptyAssignment},
		{name: "unknown slot", kind: ActionKindAssignment,
			body: `{"kind":"pedigree.assignment","assignments":[{"slotId":"grandsire","horseId":"x"}]}`,
			want: CodeUnknownSlot},
		{name: "unknown horse", kind: ActionKindAssignment,
			body: `{"kind":"pedigree.assignment","assignments":[{"slotId":"sire","horseId":"ph-nope"}]}`,
			want: CodeUnknownHorse},
		{name: "valid", kind: ActionKindAssignment,
			body: `{"kind":"pedigree.assignment","assignments":[{"slotId":"sire","horseId":"` + validHorse + `"}]}`,
			want: ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dec, _ := apply(t, prep, modes.Subject{ID: "p1", Kind: modes.SubjectPlayer},
				modes.ModeAction{Kind: tc.kind, Payload: json.RawMessage(tc.body)}, nil)
			if tc.want == "" {
				if !dec.Accepted {
					t.Fatalf("valid submission rejected: %q %q", dec.Code, dec.Detail)
				}
				return
			}
			if dec.Accepted {
				t.Fatal("an invalid submission was accepted")
			}
			if dec.Code != tc.want {
				t.Fatalf("code = %q, want %q (detail %q)", dec.Code, tc.want, dec.Detail)
			}
		})
	}
}

// TestNonParticipantCannotSubmit asserts a subject outside the roster is refused.
func TestNonParticipantCannotSubmit(t *testing.T) {
	prep, pub, _ := prepareRound(t, packOf(12), "outsider-seed", `{"slotCount":4}`)
	dec, _ := apply(t, prep, modes.Subject{ID: "spectator-1", Kind: modes.SubjectPlayer},
		assignAction("sire", pub.Candidates[0].HorseID), nil)
	if dec.Accepted {
		t.Fatal("a non-participant submission was accepted")
	}
	if dec.Code != CodeNotOnRoster {
		t.Fatalf("code = %q, want %q", dec.Code, CodeNotOnRoster)
	}
}

// TestSolutionWithheldUntilReveal asserts the answer never reaches a player before
// the round is terminal.
func TestSolutionWithheldUntilReveal(t *testing.T) {
	prep, pub, priv := prepareRound(t, packOf(12), "withhold-seed", `{"slotCount":4}`)
	player := modes.Viewer{Subject: modes.Subject{ID: "p1", Kind: modes.SubjectPlayer}, Role: "player"}

	view := project(t, prep, player, modes.PhaseCollecting, nil)
	if view.Answer != nil {
		t.Fatal("the solution is visible while the round is collecting")
	}
	// The candidate ids appear in the pool (they must), but no slot mapping may.
	for _, revealSlot := range []string{"sire", "dam"} {
		for _, cand := range pub.Candidates {
			if cand.HorseID == priv.Solution[revealSlot] && cand.Name == "" {
				t.Fatal("a solution candidate carries no name")
			}
		}
	}

	// A referee may see it, which is the host's reveal authority.
	refView := project(t, prep, modes.Viewer{
		MembershipID: "host-1", Role: "host", IsReferee: true,
	}, modes.PhaseCollecting, nil)
	if refView.Answer == nil {
		t.Fatal("a referee cannot see the solution")
	}
	if len(refView.Answer.Slots) != len(pub.Slots) {
		t.Fatalf("reveal has %d slots, want %d", len(refView.Answer.Slots), len(pub.Slots))
	}
	for _, slot := range refView.Answer.Slots {
		if slot.HorseID == "" || slot.Name == "" {
			t.Fatalf("revealed slot %s is incomplete: %+v", slot.SlotID, slot)
		}
		if slot.HorseID != priv.Solution[slot.SlotID] {
			t.Fatalf("revealed slot %s = %q, solution says %q",
				slot.SlotID, slot.HorseID, priv.Solution[slot.SlotID])
		}
	}

	// Everybody sees it once the round is settled.
	settledView := project(t, prep, player, modes.PhaseSettled, nil)
	if settledView.Answer == nil {
		t.Fatal("the solution is not revealed after settle")
	}
}

// TestProgressIsPublicButChoicesAreNot asserts a cooperative partner sees the
// fill count but not the other partner's candidate choices.
func TestProgressIsPublicButChoicesAreNot(t *testing.T) {
	prep, pub, priv := prepareRound(t, packOf(12), "progress-seed", `{"slotCount":4,"cooperative":true}`,
		modes.Subject{ID: "p1", Kind: modes.SubjectPlayer},
		modes.Subject{ID: "p2", Kind: modes.SubjectPlayer})
	pairs := correctPairs(priv)

	dec, state := apply(t, prep, modes.Subject{ID: "p1", Kind: modes.SubjectPlayer},
		assignAction(pairs[:2]...), nil)
	if !dec.Accepted {
		t.Fatalf("rejected: %q %q", dec.Code, dec.Detail)
	}
	states := map[string]json.RawMessage{"p1": state}

	// p2 sees p1's fill count.
	view := project(t, prep, modes.Viewer{Subject: modes.Subject{ID: "p2", Kind: modes.SubjectPlayer}, Role: "player"},
		modes.PhaseCollecting, states)
	var p1Progress progressView
	for _, p := range view.Progress {
		if p.SubjectID == "p1" {
			p1Progress = p
		}
	}
	if p1Progress.AssignedSlots != 1 {
		t.Fatalf("p1 progress = %d slots, want 1", p1Progress.AssignedSlots)
	}
	// p2's own view is empty, and p1's choice is not attached to it.
	if view.Self == nil || len(view.Self.Assignments) != 0 {
		t.Fatalf("p2 self view = %+v", view.Self)
	}
	// The projection has no field carrying another participant's assignment list.
	raw := string(mustJSON(t, view))
	if strings.Contains(raw, `"assignments":[{"slotId"`) && view.Self != nil && len(view.Self.Assignments) == 0 {
		t.Fatal("another participant's assignment list leaked into the projection")
	}
	_ = pub
}

// TestPrepareIsDeterministic asserts the puzzle is a pure function of the scope.
func TestPrepareIsDeterministic(t *testing.T) {
	pack := packOf(12)
	first, _, firstPriv := prepareRound(t, pack, "det-seed", `{"slotCount":5}`)
	second, _, secondPriv := prepareRound(t, pack, "det-seed", `{"slotCount":5}`)
	if string(first.Public) != string(second.Public) {
		t.Fatal("public state is not deterministic")
	}
	if string(first.Private) != string(second.Private) {
		t.Fatal("private state is not deterministic")
	}
	if string(mustJSON(t, firstPriv)) != string(mustJSON(t, secondPriv)) {
		t.Fatal("solution is not deterministic")
	}

	// A different seed must be allowed to pick a different subject.
	_, otherPub, _ := prepareRound(t, pack, "det-other", `{"slotCount":5}`)
	if otherPub.Subject.CharacterID == "" {
		t.Fatal("a different seed produced no subject")
	}
}

// TestCandidateIDsCarryNoDirection covers the leak-prevention rule: the published
// candidate id must not encode the slot it belongs to.
func TestCandidateIDsCarryNoDirection(t *testing.T) {
	prep, pub, priv := prepareRound(t, packOf(12), "leak-seed", `{"slotCount":6}`)
	for _, slot := range pub.Slots {
		horseID := priv.Solution[slot.SlotID]
		for _, forbidden := range []string{"sire", "dam", "father", "mother", slot.SlotID} {
			if forbidden == "" {
				continue
			}
			if strings.Contains(strings.ToLower(horseID), strings.ToLower(forbidden)) {
				t.Fatalf("candidate id %q for slot %q contains %q",
					horseID, slot.SlotID, forbidden)
			}
		}
	}
	// The candidate order must not place the answers first.
	answerSet := make(map[string]struct{}, len(priv.Solution))
	for _, horseID := range priv.Solution {
		answerSet[horseID] = struct{}{}
	}
	prefix := 0
	for _, cand := range pub.Candidates {
		if _, ok := answerSet[cand.HorseID]; !ok {
			break
		}
		prefix++
	}
	if prefix == len(priv.Solution) {
		t.Fatal("every answer appears before every distractor, which leaks the solution")
	}
	_ = prep
}

// TestQuestionCountMatchesBoard asserts QuestionCount is the board size.
func TestQuestionCountMatchesBoard(t *testing.T) {
	for _, count := range []int{4, 5, 6} {
		t.Run(fmt.Sprintf("slots%d", count), func(t *testing.T) {
			prep, pub, _ := prepareRound(t, packOf(12), "count-seed",
				fmt.Sprintf(`{"slotCount":%d}`, count))
			if prep.QuestionCount != len(pub.Slots) {
				t.Fatalf("questionCount = %d, board has %d slots",
					prep.QuestionCount, len(pub.Slots))
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
