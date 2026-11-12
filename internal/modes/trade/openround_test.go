package trade

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/bhcn/umaguessinggame/internal/modes"
)

// TestOpenRoundWithDeliveredContent drives a full 3-player round with the delivered
// content file, asserting the public state carries everything a player needs.
func TestOpenRoundWithDeliveredContent(t *testing.T) {
	path := filepath.Join("..", "..", "..", "data", "manual", "trade-content.json")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("delivered content not present at %s: %v", path, err)
	}
	var content struct {
		Cards      json.RawMessage `json:"cards"`
		Objectives json.RawMessage `json:"objectives"`
	}
	if err := json.Unmarshal(raw, &content); err != nil {
		t.Fatalf("decode delivered content: %v", err)
	}
	pack := fakeContent{
		CardsSection:      content.Cards,
		ObjectivesSection: content.Objectives,
	}

	subjects := []modes.Subject{
		{ID: "p1", Kind: modes.SubjectPlayer},
		{ID: "p2", Kind: modes.SubjectPlayer},
		{ID: "p3", Kind: modes.SubjectPlayer},
	}
	scope := modes.Scope{
		ModeID:             ModeID,
		RulesVersion:       RulesVersion,
		DatasetVersion:     "delivered-content",
		ContentPackVersion: "delivered-content-v1",
		Seed:               "open-round-seed",
	}
	prep, err := New().Prepare(context.Background(), modes.PrepareInput{
		Scope:   scope,
		Subject: subjects,
		Config:  json.RawMessage(`{}`),
		Content: pack,
	})
	if err != nil {
		t.Fatalf("Prepare refused the delivered content: %v", err)
	}

	var pub publicState
	if err := json.Unmarshal(prep.Public, &pub); err != nil {
		t.Fatalf("decode public: %v", err)
	}

	// 1. The full objective catalogue is published at start .
	if len(pub.Objectives) != 16 {
		t.Fatalf("published %d objectives, want 16", len(pub.Objectives))
	}
	// 2. The card catalogue is published.
	if len(pub.CardKinds) != 10 {
		t.Fatalf("published %d card kinds, want 10", len(pub.CardKinds))
	}
	// 3. Every hand has four instances and hands are public.
	if len(pub.Hands) != 3 {
		t.Fatalf("published %d hands, want 3", len(pub.Hands))
	}
	for _, entry := range subjects {
		if len(pub.Hands[entry.ID]) != HandSize {
			t.Fatalf("%s holds %d instances, want %d", entry.ID, len(pub.Hands[entry.ID]), HandSize)
		}
	}
	// 4. Every objective publishes its scoring condition and marks itself as authored.
	for _, obj := range pub.Objectives {
		if obj.Points != 50 {
			t.Fatalf("objective %q is worth %d, want 50", obj.ObjectiveID, obj.Points)
		}
		if obj.Provenance.Kind != ProvenanceAuthored || !obj.Provenance.NotOfficialData {
			t.Fatalf("objective %q does not mark itself as authored design", obj.ObjectiveID)
		}
	}

	// 5. Each player's assignment is private, two objectives, no duplicates.
	var priv privateState
	if err := json.Unmarshal(prep.Private, &priv); err != nil {
		t.Fatalf("decode private: %v", err)
	}
	seen := make(map[string]string, 6)
	for _, entry := range subjects {
		list := priv.Objectives[entry.ID]
		if len(list) != ObjectiveCount {
			t.Fatalf("%s holds %d objectives, want %d", entry.ID, len(list), ObjectiveCount)
		}
		for _, obj := range list {
			if prev, dup := seen[obj.ObjectiveID]; dup {
				t.Fatalf("objective %q dealt to both %s and %s", obj.ObjectiveID, prev, entry.ID)
			}
			seen[obj.ObjectiveID] = entry.ID
		}
	}

	// 6. Projection: the caller sees the catalogue and only their own assignment.
	view, err := New().Project(modes.ProjectInput{
		Scope:   scope,
		Viewer:  modes.Viewer{MembershipID: "m1", Role: "player", Subject: subjects[0]},
		Public:  prep.Public,
		Private: prep.Private,
		States:  map[string]json.RawMessage{},
		Phase:   modes.PhaseCollecting,
	})
	if err != nil {
		t.Fatalf("Project: %v", err)
	}
	var proj projection
	if err := json.Unmarshal(view, &proj); err != nil {
		t.Fatalf("decode projection: %v", err)
	}
	if len(proj.Objectives) != 16 {
		t.Fatalf("projection publishes %d objectives, want 16", len(proj.Objectives))
	}
	if len(proj.CardKinds) != 10 {
		t.Fatalf("projection publishes %d card kinds, want 10", len(proj.CardKinds))
	}
	if len(proj.SelfObjectives) != ObjectiveCount {
		t.Fatalf("caller sees %d assignments, want %d", len(proj.SelfObjectives), ObjectiveCount)
	}
	for _, obj := range proj.SelfObjectives {
		if seen[obj.ObjectiveID] != subjects[0].ID {
			t.Fatalf("caller sees objective %q which belongs to %s",
				obj.ObjectiveID, seen[obj.ObjectiveID])
		}
	}

	// 7. Settle produces a valid score per player after all real windows close.
	prep = finishTrading(t, prep)
	scores, err := New().Settle(modes.SettleInput{
		Scope:   scope,
		Phase:   modes.PhaseSettled,
		Public:  prep.Public,
		Private: prep.Private,
		States:  map[string]json.RawMessage{},
	})
	if err != nil {
		t.Fatalf("Settle: %v", err)
	}
	if len(scores) != 3 {
		t.Fatalf("settled %d scores, want 3", len(scores))
	}
	for _, s := range scores {
		if err := s.Validate(); err != nil {
			t.Fatalf("invalid score: %v", err)
		}
	}

	t.Logf("opened a 3-player trade round: %d objectives, %d card kinds, %d hands of %d",
		len(pub.Objectives), len(pub.CardKinds), len(pub.Hands), HandSize)
}

// TestOpenRoundWithEightPlayers asserts the maximum table works with the delivered content.
func TestOpenRoundWithEightPlayers(t *testing.T) {
	path := filepath.Join("..", "..", "..", "data", "manual", "trade-content.json")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("delivered content not present: %v", err)
	}
	var content struct {
		Cards      json.RawMessage `json:"cards"`
		Objectives json.RawMessage `json:"objectives"`
	}
	if err := json.Unmarshal(raw, &content); err != nil {
		t.Fatalf("decode: %v", err)
	}
	pack := fakeContent{
		CardsSection:      content.Cards,
		ObjectivesSection: content.Objectives,
	}
	subjects := make([]modes.Subject, 0, 8)
	for i := 0; i < 8; i++ {
		subjects = append(subjects, modes.Subject{
			ID:   "p" + string(rune('1'+i)),
			Kind: modes.SubjectPlayer,
		})
	}
	prep, err := New().Prepare(context.Background(), modes.PrepareInput{
		Scope: modes.Scope{
			ModeID: ModeID, RulesVersion: RulesVersion,
			DatasetVersion: "delivered", ContentPackVersion: "v1", Seed: "eight-seed",
		},
		Subject: subjects,
		Config:  json.RawMessage(`{}`),
		Content: pack,
	})
	if err != nil {
		t.Fatalf("Prepare refused an 8-player round: %v", err)
	}
	var priv privateState
	if err := json.Unmarshal(prep.Private, &priv); err != nil {
		t.Fatalf("decode private: %v", err)
	}
	seen := make(map[string]struct{}, 16)
	for _, entry := range subjects {
		for _, obj := range priv.Objectives[entry.ID] {
			if _, dup := seen[obj.ObjectiveID]; dup {
				t.Fatalf("objective %q dealt twice at an 8-player table", obj.ObjectiveID)
			}
			seen[obj.ObjectiveID] = struct{}{}
		}
	}
	if len(seen) != 16 {
		t.Fatalf("%d distinct objectives dealt, want 16 (8 players x 2)", len(seen))
	}
	t.Logf("opened an 8-player trade round: all 16 objectives dealt exactly once")
}
