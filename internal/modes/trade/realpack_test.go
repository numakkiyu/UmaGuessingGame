package trade

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bhcn/umaguessinggame/internal/modes"
)

// The compiler and engine must agree on section names and payload fields.
func TestLoadsRealPublishedPack(t *testing.T) {
	root := filepath.Join("..", "..", "..")
	cur, err := os.ReadFile(filepath.Join(root, "content", "current"))
	if err != nil {
		t.Skip("no pack")
	}
	version := strings.TrimSpace(string(cur))
	dir := filepath.Join(root, "content", version, "public")
	pack := fakeContent{}
	for _, f := range []struct{ file, section string }{
		{"trade_cards.json", CardsSection},
		{"trade_objectives.json", ObjectivesSection},
	} {
		raw, err := os.ReadFile(filepath.Join(dir, f.file))
		if err != nil {
			t.Skipf("pack lacks %s: %v", f.file, err)
		}
		pack[f.section] = json.RawMessage(raw)
	}

	subjects := []modes.Subject{
		{ID: "p1", Kind: modes.SubjectPlayer},
		{ID: "p2", Kind: modes.SubjectPlayer},
		{ID: "p3", Kind: modes.SubjectPlayer},
	}
	prep, err := New().Prepare(context.Background(), modes.PrepareInput{
		Scope: modes.Scope{
			ModeID: ModeID, RulesVersion: RulesVersion,
			DatasetVersion: "real", ContentPackVersion: version, Seed: "real-pack-seed",
		},
		Subject: subjects,
		Config:  json.RawMessage(`{}`),
		Content: pack,
	})
	if err != nil {
		t.Fatalf("Prepare refused the REAL published pack: %v", err)
	}
	var pub publicState
	if err := json.Unmarshal(prep.Public, &pub); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(pub.Objectives) != 16 {
		t.Fatalf("loaded %d objectives from the real pack, want 16", len(pub.Objectives))
	}
	if len(pub.CardKinds) != 10 {
		t.Fatalf("loaded %d card kinds from the real pack, want 10", len(pub.CardKinds))
	}
	for _, entry := range subjects {
		if len(pub.Hands[entry.ID]) != HandSize {
			t.Fatalf("%s holds %d cards, want %d", entry.ID, len(pub.Hands[entry.ID]), HandSize)
		}
	}
	t.Logf("real pack %s: %d objectives, %d card kinds, %d hands",
		version, len(pub.Objectives), len(pub.CardKinds), len(pub.Hands))
}
