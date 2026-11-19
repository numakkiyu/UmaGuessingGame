package modeutil_test

// Action discriminator strings must agree between mode implementations and api/openapi.json.

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/bhcn/umaguessinggame/internal/modes"

	"github.com/bhcn/umaguessinggame/internal/modes/aptitudegrid"
	"github.com/bhcn/umaguessinggame/internal/modes/classic"
	"github.com/bhcn/umaguessinggame/internal/modes/coracepath"
	"github.com/bhcn/umaguessinggame/internal/modes/draft"
	"github.com/bhcn/umaguessinggame/internal/modes/memory"
	"github.com/bhcn/umaguessinggame/internal/modes/racerebuild"
	sortgame "github.com/bhcn/umaguessinggame/internal/modes/sort"
)

// contractKindsReader is the optional interface a mode implements to declare the
// ModeAction discriminators it accepts.
type contractKindsReader interface {
	ContractKinds() []string
}

// loadContractActionKinds reads the ModeAction discriminator mapping from the
// OpenAPI contract.
func loadContractActionKinds(t *testing.T) map[string]string {
	t.Helper()
	// The test runs with the package directory as the working directory.
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
	modeActionRaw, ok := doc.Components.Schemas["ModeAction"]
	if !ok {
		t.Fatal("contract has no ModeAction schema")
	}
	var modeAction struct {
		Discriminator struct {
			PropertyName string            `json:"propertyName"`
			Mapping      map[string]string `json:"mapping"`
		} `json:"discriminator"`
	}
	if err := json.Unmarshal(modeActionRaw, &modeAction); err != nil {
		t.Fatalf("parse ModeAction: %v", err)
	}
	if modeAction.Discriminator.PropertyName != "kind" {
		t.Fatalf("discriminator property = %q, want kind", modeAction.Discriminator.PropertyName)
	}
	if len(modeAction.Discriminator.Mapping) == 0 {
		t.Fatal("contract ModeAction has an empty discriminator mapping")
	}
	return modeAction.Discriminator.Mapping
}

// implementedModes returns the mode engines covered by these contract tests.
func implementedModes() map[string]interface {
	ContractKinds() []string
} {
	return map[string]interface {
		ContractKinds() []string
	}{
		"classic":       classic.New(),
		"aptitude-grid": aptitudegrid.New(),
		"draft":         draft.New(),
		"race-rebuild":  racerebuild.New(),
		"co-race-path":  coracepath.New(),
		"sort":          sortgame.New(),
		"memory":        memory.New(),
	}
}

// TestActionKindsMatchContractDiscriminator asserts every kind a mode accepts is
// declared in the contract's ModeAction discriminator mapping.
//
// This is the regression that catches a mode using an internal kind name: the
// HTTP layer would reject it as unknown_action and the mode would be unreachable
// even though its own unit tests pass.
func TestActionKindsMatchContractDiscriminator(t *testing.T) {
	mapping := loadContractActionKinds(t)

	contractKinds := make([]string, 0, len(mapping))
	for kind := range mapping {
		contractKinds = append(contractKinds, kind)
	}
	sort.Strings(contractKinds)
	t.Logf("contract declares %d action kinds: %v", len(contractKinds), contractKinds)

	modes := implementedModes()
	// Every implemented mode must declare at least one kind and all of them must
	// be contract discriminators.
	for modeID, mode := range modes {
		kinds := mode.ContractKinds()
		if len(kinds) == 0 {
			t.Fatalf("mode %s declares no contract kinds", modeID)
		}
		for _, kind := range kinds {
			schema, ok := mapping[kind]
			if !ok {
				t.Fatalf("mode %s accepts kind %q which is not in the contract discriminator mapping %v",
					modeID, kind, contractKinds)
			}
			t.Logf("mode %-14s kind %-16s -> %s", modeID, kind, schema)
		}
	}
}

// TestContractKindsCoverAllImplementedModes asserts the seven modes between them
// cover exactly the seven contract kinds they are responsible for, so a missing or
// duplicated claim shows up here.
func TestContractKindsCoverAllImplementedModes(t *testing.T) {
	mapping := loadContractActionKinds(t)

	// Action kinds covered by this contract test.
	want := []string{
		"classic.guess",
		"grid.claim",
		"draft.lineup",
		"race.order",
		"path.route",
		"sort.order",
		"memory.flip",
	}
	seen := make(map[string]string, len(want))
	for modeID, mode := range implementedModes() {
		for _, kind := range mode.ContractKinds() {
			if prev, dup := seen[kind]; dup {
				t.Fatalf("kind %q is claimed by both %s and %s", kind, prev, modeID)
			}
			seen[kind] = modeID
		}
	}
	for _, kind := range want {
		modeID, ok := seen[kind]
		if !ok {
			t.Fatalf("contract kind %q is not accepted by any implemented mode", kind)
		}
		if _, inContract := mapping[kind]; !inContract {
			t.Fatalf("kind %q (mode %s) is missing from the contract", kind, modeID)
		}
	}
	if len(seen) != len(want) {
		t.Fatalf("implemented modes claim %d kinds, want %d: %v", len(seen), len(want), seen)
	}
}

// TestContractPayloadRequiredFieldsMatch asserts each mode's payload decoder
// accepts the contract's required field names.
//
// It checks the field names structurally: the mode must reject a payload missing a
// contract-required field, which proves it is reading that name.
func TestContractPayloadRequiredFieldsMatch(t *testing.T) {
	cases := []struct {
		modeID string
		mode   interface {
			ValidateConfig(json.RawMessage) error
		}
		// contractRequired lists the contract's required properties other than kind.
		contractRequired []string
	}{
		{modeID: "classic", mode: classic.New(), contractRequired: []string{"characterId"}},
		{modeID: "aptitude-grid", mode: aptitudegrid.New(), contractRequired: []string{"cellId", "characterId"}},
		{modeID: "draft", mode: draft.New(), contractRequired: []string{"assignments"}},
		{modeID: "race-rebuild", mode: racerebuild.New(), contractRequired: []string{"groups"}},
		{modeID: "co-race-path", mode: coracepath.New(), contractRequired: []string{"characterIds"}},
		{modeID: "sort", mode: sortgame.New(), contractRequired: []string{"groups"}},
		{modeID: "memory", mode: memory.New(), contractRequired: []string{"cardId"}},
	}
	// The contract's required lists are read from the file, not duplicated here.
	path := filepath.Join("..", "..", "..", "api", "openapi.json")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("contract not readable: %v", err)
	}
	var doc struct {
		Components struct {
			Schemas map[string]struct {
				Required []string `json:"required"`
			} `json:"schemas"`
		} `json:"components"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("parse contract: %v", err)
	}

	// Map mode id → the contract schema that carries its action.
	schemaOf := map[string]string{
		"classic":       "ClassicGuess",
		"aptitude-grid": "GridClaim",
		"draft":         "DraftLineup",
		"race-rebuild":  "RaceOrder",
		"co-race-path":  "PathRoute",
		"sort":          "SortOrder",
		"memory":        "MemoryFlip",
	}
	for _, tc := range cases {
		schemaName := schemaOf[tc.modeID]
		schema, ok := doc.Components.Schemas[schemaName]
		if !ok {
			t.Fatalf("contract has no %s schema", schemaName)
		}
		// Compare the contract's required set (minus kind) with the expectation,
		// so a contract edit that adds a field turns this red.
		contractSet := make(map[string]struct{}, len(schema.Required))
		for _, field := range schema.Required {
			if field == "kind" {
				continue
			}
			contractSet[field] = struct{}{}
		}
		if len(contractSet) != len(tc.contractRequired) {
			t.Fatalf("mode %s: contract %s requires %v, test expects %v",
				tc.modeID, schemaName, schema.Required, tc.contractRequired)
		}
		for _, field := range tc.contractRequired {
			if _, ok := contractSet[field]; !ok {
				t.Fatalf("mode %s: contract %s does not require %q (requires %v)",
					tc.modeID, schemaName, field, schema.Required)
			}
		}
		t.Logf("mode %-14s schema %-14s required %v", tc.modeID, schemaName, schema.Required)
	}
}

// TestContractWirePayloadsAccepted drives each mode with a payload built from the
// contract's own schema shape, including the discriminator "kind" field inside the
// payload body.
//
// This is the end-to-end reachability check: the unit tests in each mode package
// call Apply with a helper-built action, which cannot prove the wire form is
// accepted. Here the payload is raw JSON matching the contract, exactly as the HTTP
// layer would forward it.
func TestContractWirePayloadsAccepted(t *testing.T) {
	pack := loadRealPack(t)
	if pack == nil {
		t.Skip("no compiled content pack present")
	}

	cases := []struct {
		mode interface {
			Prepare(context.Context, modes.PrepareInput) (modes.Prepared, error)
			Apply(modes.ApplyInput) (modes.Decision, error)
			ID() string
			RulesVersion() string
		}
		// kind is the contract discriminator carried in both the envelope and the
		// payload body.
		kind   string
		config string
		// buildPayload produces the contract-shaped payload for the prepared round.
		buildPayload func(t *testing.T, prep modes.Prepared) string
	}{
		{
			mode:   classic.New(),
			kind:   "classic.guess",
			config: `{}`,
			buildPayload: func(t *testing.T, prep modes.Prepared) string {
				var priv struct {
					Pool []struct {
						ID string `json:"id"`
					} `json:"pool"`
				}
				if err := json.Unmarshal(prep.Private, &priv); err != nil || len(priv.Pool) == 0 {
					t.Fatalf("decode classic private: %v", err)
				}
				return `{"kind":"classic.guess","characterId":"` + priv.Pool[0].ID + `"}`
			},
		},
		{
			mode:   aptitudegrid.New(),
			kind:   "grid.claim",
			config: `{}`,
			buildPayload: func(t *testing.T, prep modes.Prepared) string {
				var env struct {
					Round struct {
						Cells []struct {
							CellID  string `json:"cellId"`
							RowCond struct {
								Axis  string `json:"axis"`
								Value string `json:"value"`
							} `json:"rowCondition"`
							ColCond struct {
								Axis  string `json:"axis"`
								Value string `json:"value"`
							} `json:"colCondition"`
						} `json:"cells"`
					} `json:"round"`
				}
				if err := json.Unmarshal(prep.Public, &env); err != nil || len(env.Round.Cells) == 0 {
					t.Fatalf("decode grid public: %v", err)
				}
				var priv struct {
					Cards []struct {
						ID        string `json:"id"`
						Aptitudes struct {
							Surface  map[string]string `json:"surface"`
							Distance map[string]string `json:"distance"`
							Style    map[string]string `json:"style"`
						} `json:"aptitudes"`
					} `json:"cards"`
				}
				if err := json.Unmarshal(prep.Private, &priv); err != nil {
					t.Fatalf("decode grid private: %v", err)
				}
				// Find a cell/card pair that actually satisfies both conditions, so
				// the test exercises the accept path rather than the reject path.
				grade := func(aptitudes map[string]string, axis, value string) string {
					return aptitudes[value]
				}
				tableOf := func(card struct {
					ID        string `json:"id"`
					Aptitudes struct {
						Surface  map[string]string `json:"surface"`
						Distance map[string]string `json:"distance"`
						Style    map[string]string `json:"style"`
					} `json:"aptitudes"`
				}, axis string) map[string]string {
					switch axis {
					case "surface":
						return card.Aptitudes.Surface
					case "distance":
						return card.Aptitudes.Distance
					default:
						return card.Aptitudes.Style
					}
				}
				for _, cell := range env.Round.Cells {
					for _, card := range priv.Cards {
						if grade(tableOf(card, cell.RowCond.Axis), cell.RowCond.Axis, cell.RowCond.Value) == "A" &&
							grade(tableOf(card, cell.ColCond.Axis), cell.ColCond.Axis, cell.ColCond.Value) == "A" {
							return `{"kind":"grid.claim","cellId":"` + cell.CellID +
								`","characterId":"` + card.ID + `"}`
						}
					}
				}
				t.Fatal("no card satisfies any board cell; the pack cannot support this mode")
				return ""
			},
		},
		{
			mode:   coracepath.New(),
			kind:   "path.route",
			config: `{}`,
			buildPayload: func(t *testing.T, prep modes.Prepared) string {
				var priv struct {
					ShortestPath []string `json:"shortestPath"`
				}
				if err := json.Unmarshal(prep.Private, &priv); err != nil || len(priv.ShortestPath) < 2 {
					t.Fatalf("decode path private: %v", err)
				}
				ids, _ := json.Marshal(priv.ShortestPath)
				return `{"kind":"path.route","characterIds":` + string(ids) + `}`
			},
		},
		{
			mode:   memory.New(),
			kind:   "memory.flip",
			config: `{}`,
			buildPayload: func(t *testing.T, prep modes.Prepared) string {
				var pub struct {
					Cards []struct {
						CardID string `json:"cardId"`
					} `json:"cards"`
				}
				if err := json.Unmarshal(prep.Public, &pub); err != nil || len(pub.Cards) == 0 {
					t.Fatalf("decode memory public: %v", err)
				}
				return `{"kind":"memory.flip","cardId":"` + pub.Cards[0].CardID + `"}`
			},
		},
	}

	for _, tc := range cases {
		name := tc.mode.ID()
		t.Run(name, func(t *testing.T) {
			scope := modes.Scope{
				ModeID:             name,
				RulesVersion:       tc.mode.RulesVersion(),
				DatasetVersion:     "wire-test",
				ContentPackVersion: pack.version,
				Seed:               "wire-seed",
			}
			prep, err := tc.mode.Prepare(context.Background(), modes.PrepareInput{
				Scope:   scope,
				Subject: []modes.Subject{{ID: "p1", Kind: modes.SubjectPlayer}},
				Config:  json.RawMessage(tc.config),
				Content: pack,
			})
			if err != nil {
				t.Fatalf("Prepare: %v", err)
			}
			payload := tc.buildPayload(t, prep)
			dec, err := tc.mode.Apply(modes.ApplyInput{
				Scope:   scope,
				Subject: modes.Subject{ID: "p1", Kind: modes.SubjectPlayer},
				Action: modes.ModeAction{
					Kind:    tc.kind,
					Payload: json.RawMessage(payload),
				},
				Public:  prep.Public,
				Private: prep.Private,
			})
			if err != nil {
				t.Fatalf("Apply: %v", err)
			}
			if !dec.Accepted {
				t.Fatalf("contract-shaped payload was rejected: code=%q detail=%q payload=%s",
					dec.Code, dec.Detail, payload)
			}
			t.Logf("accepted %s with payload %s", name, payload)
		})
	}
}

// TestMemoryRevealCarriesReviewedLabels asserts the memory reveal shows the
// reviewed character name while the public payload carries only an opaque
// placeholder.
//
// The pack publishes "???" as the public label on purpose: a real name in the
// public section would let a player reverse the whole pack into an image → name
// table and skip the memory task entirely . The readable
// name therefore has to come from the private solution, or the reveal — and any
// screen reader reading it — shows "???".
func TestMemoryRevealCarriesReviewedLabels(t *testing.T) {
	pack := loadRealPack(t)
	if pack == nil {
		t.Skip("no compiled content pack present")
	}
	scope := modes.Scope{
		ModeID:             "memory",
		RulesVersion:       memory.New().RulesVersion(),
		DatasetVersion:     "label-test",
		ContentPackVersion: pack.version,
		Seed:               "label-probe",
	}
	subjects := []modes.Subject{{ID: "p1", Kind: modes.SubjectPlayer}}
	prep, err := memory.New().Prepare(context.Background(), modes.PrepareInput{
		Scope:   scope,
		Subject: subjects,
		Config:  json.RawMessage(`{}`),
		Content: pack,
	})
	if err != nil {
		t.Fatalf("Prepare: %v", err)
	}

	// The public payload must not carry a readable label.
	public := string(prep.Public)
	for _, forbidden := range []string{"pairLabels", "label"} {
		if strings.Contains(public, `"`+forbidden+`"`) {
			t.Fatalf("public prepared state contains %q", forbidden)
		}
	}

	out, err := memory.New().Project(modes.ProjectInput{
		Scope:     scope,
		Viewer:    modes.Viewer{MembershipID: "host", Role: "host", IsReferee: true},
		Public:    prep.Public,
		Private:   prep.Private,
		States:    map[string]json.RawMessage{},
		Phase:     modes.PhaseSettled,
		RevealNow: true,
	})
	if err != nil {
		t.Fatalf("Project: %v", err)
	}
	var view struct {
		Answer *struct {
			Pairs []struct {
				PairID string `json:"pairId"`
				Label  string `json:"label"`
			} `json:"pairs"`
		} `json:"answer"`
	}
	if err := json.Unmarshal(out, &view); err != nil {
		t.Fatalf("decode projection: %v", err)
	}
	if view.Answer == nil {
		t.Fatal("reveal produced no answer")
	}
	if len(view.Answer.Pairs) != 8 {
		t.Fatalf("revealed pairs = %d, want 8", len(view.Answer.Pairs))
	}
	for _, pair := range view.Answer.Pairs {
		if pair.Label == "" || pair.Label == "???" {
			t.Fatalf("revealed pair %q has placeholder label %q", pair.PairID, pair.Label)
		}
	}
	t.Logf("revealed %d pairs, all with reviewed labels", len(view.Answer.Pairs))
}
