package pedigree

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bhcn/umaguessinggame/internal/modes"
)

// contractMapping reads the ModeAction discriminator mapping from the OpenAPI contract.
func contractMapping(t *testing.T) map[string]string {
	t.Helper()
	path := filepath.Join("..", "..", "..", "api", "openapi.json")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("contract not readable at %s: %v", path, err)
	}
	var doc struct {
		Components struct {
			Schemas map[string]json.RawMessage `json:"schemas"`
		} `json:"components"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("parse contract: %v", err)
	}
	modeAction, ok := doc.Components.Schemas["ModeAction"]
	if !ok {
		t.Fatal("contract has no ModeAction schema")
	}
	var schema struct {
		Discriminator struct {
			Mapping map[string]string `json:"mapping"`
		} `json:"discriminator"`
	}
	if err := json.Unmarshal(modeAction, &schema); err != nil {
		t.Fatalf("parse ModeAction: %v", err)
	}
	return schema.Discriminator.Mapping
}

// TestContractKindsAreDiscriminators asserts the mode's action kind is a contract
// discriminator .
func TestContractKindsAreDiscriminators(t *testing.T) {
	mapping := contractMapping(t)
	for _, kind := range New().ContractKinds() {
		if _, ok := mapping[kind]; !ok {
			t.Fatalf("pedigree accepts kind %q which is not in the contract discriminator mapping", kind)
		}
		t.Logf("pedigree kind %q -> %s", kind, mapping[kind])
	}
}

// TestContractPayloadFieldsMatch asserts the payload decoder reads the contract's required
// field names.
//
// The contract's PedigreeAssignment carries assignments[] with {slotId, horseId}. A
// payload missing either field must be refused, which proves the decoder reads that name.
func TestContractPayloadFieldsMatch(t *testing.T) {
	prep, _, _ := prepareRound(t, packOf(12), "contract-seed", `{"slotCount":4}`)
	subject := modes.Subject{ID: "p1", Kind: modes.SubjectPlayer}

	cases := []struct {
		name string
		body string
		want string
	}{
		{name: "missing horseId", body: `{"kind":"pedigree.assignment","assignments":[{"slotId":"sire"}]}`,
			want: CodeUnknownHorse},
		{name: "missing slotId", body: `{"kind":"pedigree.assignment","assignments":[{"horseId":"ph-x"}]}`,
			want: CodeUnknownSlot},
		{name: "both present but unknown", body: `{"kind":"pedigree.assignment","assignments":[{"slotId":"sire","horseId":"ph-x"}]}`,
			want: CodeUnknownHorse},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dec, _ := apply(t, prep, subject,
				modes.ModeAction{Kind: ActionKindAssignment, Payload: json.RawMessage(tc.body)}, nil)
			if dec.Accepted {
				t.Fatal("a payload with a missing or unknown field was accepted")
			}
			if dec.Code != tc.want {
				t.Fatalf("code = %q, want %q (detail %q)", dec.Code, tc.want, dec.Detail)
			}
		})
	}
}

// realPack is a ContentSource backed by the published pack files on disk.
type realPack struct {
	public  map[string]json.RawMessage
	private map[string]json.RawMessage
	version string
}

func (p *realPack) Section(name string) (json.RawMessage, bool) {
	raw, ok := p.public[name]
	return raw, ok
}

func (p *realPack) Version() string { return p.version }

func (p *realPack) Secrets() modes.ContentSource {
	return &privateOnly{private: p.private, version: p.version}
}

type privateOnly struct {
	private map[string]json.RawMessage
	version string
}

func (p *privateOnly) Section(name string) (json.RawMessage, bool) {
	raw, ok := p.private[name]
	return raw, ok
}

func (p *privateOnly) Version() string { return p.version }

// loadRealPack locates and loads the current compiled pack.
func loadRealPack(t *testing.T) *realPack {
	t.Helper()
	root := filepath.Join("..", "..", "..")
	versionBytes, err := os.ReadFile(filepath.Join(root, "content", "current"))
	if err != nil {
		return nil
	}
	version := strings.TrimSpace(string(versionBytes))
	if version == "" {
		return nil
	}
	dir := filepath.Join(root, "content", version)
	if _, err := os.Stat(dir); err != nil {
		return nil
	}
	pack := &realPack{
		public:  map[string]json.RawMessage{},
		private: map[string]json.RawMessage{},
		version: version,
	}
	read := func(sub string, into map[string]json.RawMessage) {
		entries, err := os.ReadDir(filepath.Join(dir, sub))
		if err != nil {
			return
		}
		for _, entry := range entries {
			if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
				continue
			}
			raw, err := os.ReadFile(filepath.Join(dir, sub, entry.Name()))
			if err != nil {
				t.Fatalf("read %s/%s: %v", sub, entry.Name(), err)
			}
			name := strings.ReplaceAll(strings.TrimSuffix(entry.Name(), ".json"), "_", ".")
			into[name] = json.RawMessage(raw)
		}
	}
	read("public", pack.public)
	read("private", pack.private)
	if len(pack.public) == 0 {
		return nil
	}
	return pack
}

// TestRealPackPosesASolvablePuzzle is the end-to-end admission assertion for G14.
//
// The compiled pack carries 100 complete prototype pedigrees, so this mode is expected to
// be available. The test asserts it prepares, that the puzzle is solvable (every slot's
// answer is among the published candidates), and that the disclaimer is present.
func TestRealPackPosesASolvablePuzzle(t *testing.T) {
	pack := loadRealPack(t)
	if pack == nil {
		t.Skip("no compiled content pack present; run cmd/content first")
	}
	scope := modes.Scope{
		ModeID:             ModeID,
		RulesVersion:       RulesVersion,
		DatasetVersion:     "real-pack-admission",
		ContentPackVersion: pack.version,
		Seed:               "admission-probe",
	}
	subjects := []modes.Subject{{ID: "p1", Kind: modes.SubjectPlayer}}
	prep, err := New().Prepare(context.Background(), modes.PrepareInput{
		Scope:   scope,
		Subject: subjects,
		Config:  json.RawMessage(`{"slotCount":6,"candidateCount":12}`),
		Content: pack,
	})
	if err != nil {
		t.Fatalf("pedigree failed to prepare against the real pack: %v", err)
	}
	var pub publicState
	if err := json.Unmarshal(prep.Public, &pub); err != nil {
		t.Fatalf("decode public: %v", err)
	}
	var priv privateState
	if err := json.Unmarshal(prep.Private, &priv); err != nil {
		t.Fatalf("decode private: %v", err)
	}
	if len(pub.Slots) != 6 {
		t.Fatalf("board has %d slots, want 6", len(pub.Slots))
	}
	if pub.Disclaimer != Disclaimer {
		t.Fatalf("disclaimer = %q", pub.Disclaimer)
	}
	// Every slot's answer must be among the published candidates, or the puzzle is
	// unsolvable.
	candidates := make(map[string]struct{}, len(pub.Candidates))
	for _, c := range pub.Candidates {
		candidates[c.HorseID] = struct{}{}
	}
	for slot, horseID := range priv.Solution {
		if _, ok := candidates[horseID]; !ok {
			t.Fatalf("slot %q is answered by %q which is not a published candidate",
				slot, horseID)
		}
	}
	// The subject must carry an independent prototype id.
	if pub.Subject.NKBID == "" {
		t.Fatal("the posed subject carries no independent prototype id")
	}
	t.Logf("pedigree posed %s (%s, nkb %s) with %d slots over %d candidates",
		pub.Subject.PrototypeName, pub.Subject.CharacterName, pub.Subject.NKBID,
		len(pub.Slots), len(pub.Candidates))

	// Prepare must be deterministic against the real pack.
	again, err := New().Prepare(context.Background(), modes.PrepareInput{
		Scope:   scope,
		Subject: subjects,
		Config:  json.RawMessage(`{"slotCount":6,"candidateCount":12}`),
		Content: pack,
	})
	if err != nil {
		t.Fatalf("second prepare: %v", err)
	}
	if string(prep.Public) != string(again.Public) || string(prep.Private) != string(again.Private) {
		t.Fatal("Prepare is not deterministic against the real pack")
	}

	// A correct submission must score full marks.
	pairs := correctPairs(priv)
	dec, state := apply(t, prep, subjects[0], assignAction(pairs...), nil)
	if !dec.Accepted {
		t.Fatalf("the correct submission was rejected: %q %q", dec.Code, dec.Detail)
	}
	scores := settle(t, prep, map[string]json.RawMessage{"p1": state})
	if got := scores["p1"].ScoreUnits; got != 10000 {
		t.Fatalf("the correct submission scored %d units, want 10000", got)
	}
}
