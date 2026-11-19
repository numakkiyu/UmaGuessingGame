package bluff

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
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
			PropertyName string            `json:"propertyName"`
			Mapping      map[string]string `json:"mapping"`
		} `json:"discriminator"`
	}
	if err := json.Unmarshal(modeAction, &schema); err != nil {
		t.Fatalf("parse ModeAction: %v", err)
	}
	if schema.Discriminator.PropertyName != "kind" {
		t.Fatalf("discriminator property = %q, want kind", schema.Discriminator.PropertyName)
	}
	return schema.Discriminator.Mapping
}

// TestContractKindsAreDiscriminators asserts every kind this mode accepts is a contract
// discriminator, so the HTTP layer can actually route it .
func TestContractKindsAreDiscriminators(t *testing.T) {
	mapping := contractMapping(t)
	for _, kind := range New().ContractKinds() {
		if _, ok := mapping[kind]; !ok {
			t.Fatalf("bluff accepts kind %q which is not in the contract discriminator mapping", kind)
		}
		t.Logf("bluff kind %q -> %s", kind, mapping[kind])
	}
}

// realPack reads published sections without depending on the content loader.
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

// ModeAvailable reads the pack's own admission record for a mode.
//
// The manifest is the compiler's self-report. This test exists to check that report
// against what Prepare actually does, so the test needs to read it.
func (p *realPack) ModeAvailable(modeID string) (modeAdmission, bool) {
	raw, ok := p.public[manifestKey]
	if !ok || len(raw) == 0 {
		return modeAdmission{}, false
	}
	var m struct {
		Modes []modeAdmission `json:"modes"`
	}
	if err := json.Unmarshal(raw, &m); err != nil {
		return modeAdmission{}, false
	}
	for _, a := range m.Modes {
		if a.Mode == modeID {
			return a, true
		}
	}
	return modeAdmission{}, false
}

// manifestKey is the in-memory section name the pack adapter stores the manifest under.
const manifestKey = "__manifest"

// modeAdmission mirrors the fields this test reads from the manifest's modes array.
type modeAdmission struct {
	Mode          string `json:"mode"`
	Available     bool   `json:"available"`
	QuestionCount int    `json:"questionCount"`
	MinItems      int    `json:"minItems"`
	Reason        string `json:"reason"`
}

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
	// The manifest is not a section: it is the pack's own report. Loading it under a
	// reserved key lets the test read the compiler's admission claim.
	if manifestRaw, err := os.ReadFile(filepath.Join(dir, "manifest.json")); err == nil {
		pack.public[manifestKey] = json.RawMessage(manifestRaw)
	}
	return pack
}

// TestRealPackAdmissionMatchesPrepare checks that manifest admission agrees with the engine.
func TestRealPackAdmissionMatchesPrepare(t *testing.T) {
	pack := loadRealPack(t)
	if pack == nil {
		t.Skip("no compiled content pack present; run cmd/content first")
	}
	admission, ok := pack.ModeAvailable(ModeID)
	if !ok {
		t.Skipf("pack %s carries no admission record for %s", pack.version, ModeID)
	}
	scope := modes.Scope{
		ModeID:             ModeID,
		RulesVersion:       RulesVersion,
		DatasetVersion:     "real-pack-admission",
		ContentPackVersion: pack.version,
		Seed:               "admission-probe",
	}
	subjects := []modes.Subject{
		{ID: "p1", Kind: modes.SubjectPlayer},
		{ID: "p2", Kind: modes.SubjectPlayer},
		{ID: "p3", Kind: modes.SubjectPlayer},
	}
	prep, err := New().Prepare(context.Background(), modes.PrepareInput{
		Scope:   scope,
		Subject: subjects,
		Config:  json.RawMessage(`{}`),
		Content: pack,
	})

	if !admission.Available {
		if err == nil {
			t.Fatalf("pack %s reports bluff unavailable but Prepare admitted a round",
				pack.version)
		}
		var admissionErr *AdmissionError
		if !errors.As(err, &admissionErr) {
			t.Fatalf("the refusal is not an AdmissionError: %v", err)
		}
		if admissionErr.ReasonCode() == "" {
			t.Fatal("the refusal carries no reason code")
		}
		t.Logf("pack %s reports unavailable (%d facts) and Prepare agrees: %v",
			pack.version, admission.QuestionCount, err)
		return
	}

	if err != nil {
		t.Fatalf("pack %s reports bluff available (%d facts) but Prepare refused: %v",
			pack.version, admission.QuestionCount, err)
	}
	var pub publicState
	if err := json.Unmarshal(prep.Public, &pub); err != nil {
		t.Fatalf("decode public state: %v", err)
	}
	var priv privateState
	if err := json.Unmarshal(prep.Private, &priv); err != nil {
		t.Fatalf("decode private state: %v", err)
	}
	if !strings.Contains(pub.Prompt, "____") {
		t.Fatalf("the prompt carries no blank: %q", pub.Prompt)
	}
	if priv.Truth == "" {
		t.Fatal("the round carries no answer phrase")
	}
	if strings.Contains(pub.Prompt, priv.Truth) {
		t.Fatalf("the prompt still shows the answer %q: %q", priv.Truth, pub.Prompt)
	}
	if priv.FactSource == "" {
		t.Fatal("the posed fact carries no citation")
	}
	if strings.Contains(string(prep.Public), `"promptSource"`) {
		t.Fatal("the public state carries the citation before the reveal")
	}
	t.Logf("pack %s reports available and Prepare agrees: fact %s",
		pack.version, priv.FactID)

	again, err := New().Prepare(context.Background(), modes.PrepareInput{
		Scope:   scope,
		Subject: subjects,
		Config:  json.RawMessage(`{}`),
		Content: pack,
	})
	if err != nil {
		t.Fatalf("second prepare: %v", err)
	}
	if string(prep.Public) != string(again.Public) || string(prep.Private) != string(again.Private) {
		t.Fatal("Prepare is not deterministic against the real pack")
	}
}

// TestAdmissionRequiresReviewAndCitation asserts the admission MECHANISM with a
// deterministic fixture, so the test does not depend on the state of the human review
// ledger in data/manual/.
//
// The rule that must not vary: a fact is posed only when it is marked reviewed AND carries
// a citation AND its answer phrase actually occurs in its text. A fact failing any of the
// three is excluded, and a pack with none left must refuse.
func TestAdmissionRequiresReviewAndCitation(t *testing.T) {
	build := func(mutate func(*factSpec)) modes.ContentSource {
		facts := make([]factSpec, 0, 8)
		for i := 0; i < 8; i++ {
			f := factSpec{
				FactID:       fmt.Sprintf("fact-%03d", i+1),
				CharacterID:  fmt.Sprintf("c%03d", i+1),
				Text:         fmt.Sprintf("角色%d 的得意科目是数学", i+1),
				Answer:       "数学",
				Source:       "https://example.invalid/source",
				ReviewStatus: ReviewStatusReviewed,
			}
			if i == 0 {
				mutate(&f)
			}
			facts = append(facts, f)
		}
		raw, err := json.Marshal(facts)
		if err != nil {
			panic(err)
		}
		return fakeContent{FactsSection: raw}
	}

	cases := []struct {
		name   string
		mutate func(*factSpec)
	}{
		{name: "unreviewed", mutate: func(f *factSpec) { f.ReviewStatus = "pending" }},
		{name: "no citation", mutate: func(f *factSpec) { f.Source = "" }},
		{name: "answer not in text", mutate: func(f *factSpec) { f.Answer = "音乐" }},
		{name: "empty answer", mutate: func(f *factSpec) { f.Answer = "" }},
		{name: "empty text", mutate: func(f *factSpec) { f.Text = "" }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			prep, err := New().Prepare(context.Background(), modes.PrepareInput{
				Scope:   scope("ledger-fixture"),
				Subject: roster(3),
				Config:  json.RawMessage(`{}`),
				Content: build(tc.mutate),
			})
			if err != nil {
				t.Fatalf("Prepare refused a pack with seven good facts: %v", err)
			}
			var priv privateState
			if err := json.Unmarshal(prep.Private, &priv); err != nil {
				t.Fatalf("decode private state: %v", err)
			}
			// The spoiled fact must never be the posed one.
			if priv.FactID == "fact-001" {
				t.Fatalf("the spoiled fact was posed despite failing %q", tc.name)
			}
			// Whatever was posed must be internally consistent.
			var pub publicState
			if err := json.Unmarshal(prep.Public, &pub); err != nil {
				t.Fatalf("decode public state: %v", err)
			}
			if !strings.Contains(pub.Prompt, "____") {
				t.Fatalf("the posed prompt carries no blank: %q", pub.Prompt)
			}
		})
	}

	// A pack where every fact fails must refuse outright.
	t.Run("all unreviewed", func(t *testing.T) {
		facts := make([]factSpec, 0, 8)
		for i := 0; i < 8; i++ {
			facts = append(facts, factSpec{
				FactID:       fmt.Sprintf("fact-%03d", i+1),
				CharacterID:  fmt.Sprintf("c%03d", i+1),
				Text:         fmt.Sprintf("角色%d 的得意科目是数学", i+1),
				Answer:       "数学",
				Source:       "",
				ReviewStatus: "pending",
			})
		}
		raw, err := json.Marshal(facts)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		_, prepErr := New().Prepare(context.Background(), modes.PrepareInput{
			Scope:   scope("ledger-fixture"),
			Subject: roster(3),
			Config:  json.RawMessage(`{}`),
			Content: fakeContent{FactsSection: raw},
		})
		if prepErr == nil {
			t.Fatal("Prepare admitted a round where every fact was unreviewed")
		}
		var admissionErr *AdmissionError
		if !errors.As(prepErr, &admissionErr) {
			t.Fatalf("the refusal is not an AdmissionError: %v", prepErr)
		}
		if admissionErr.Unreviewed != 8 {
			t.Fatalf("admission counted %d unreviewed, want 8", admissionErr.Unreviewed)
		}
	})
}

// TestRecoveryIsMechanical asserts that the mode needs NO code change to become playable again:
// a ledger of reviewed facts with citations is the only input it is waiting for.
//
// This matters because bluff is currently unavailable for a CONTENT reason (no fact has passed
// a human semantic review), not a code reason. A mode that has been unavailable for weeks starts
// to look like it needs work, so this test pins the fact that the engine is finished and the
// gate is mechanical.
//
// The fixture is built here rather than read from data/manual/, because the real review ledger
// is a moving human artefact and a test pinned to it would go red on every revocation.
func TestRecoveryIsMechanical(t *testing.T) {
	build := func(reviewed bool, source string) modes.ContentSource {
		facts := make([]factSpec, 0, 8)
		for i := 0; i < 8; i++ {
			f := factSpec{
				FactID:       fmt.Sprintf("fact-%03d", i+1),
				CharacterID:  fmt.Sprintf("c%03d", i+1),
				Text:         fmt.Sprintf("角色%d 的得意科目是数学", i+1),
				Answer:       "数学",
				Source:       source,
				ReviewStatus: "pending",
			}
			if reviewed {
				f.ReviewStatus = ReviewStatusReviewed
			}
			facts = append(facts, f)
		}
		raw, err := json.Marshal(facts)
		if err != nil {
			panic(err)
		}
		return fakeContent{FactsSection: raw}
	}

	// Before: an unreviewed ledger must refuse.
	if _, prepErr := New().Prepare(context.Background(), modes.PrepareInput{
		Scope:   scope("recovery-seed"),
		Subject: roster(3),
		Config:  json.RawMessage(`{}`),
		Content: build(false, "https://example.invalid/source"),
	}); prepErr == nil {
		t.Fatal("an unreviewed ledger was admitted")
	}

	// A reviewed ledger WITHOUT a citation must also refuse: a review alone is not a source.
	if _, prepErr := New().Prepare(context.Background(), modes.PrepareInput{
		Scope:   scope("recovery-seed"),
		Subject: roster(3),
		Config:  json.RawMessage(`{}`),
		Content: build(true, ""),
	}); prepErr == nil {
		t.Fatal("a reviewed but unsourced ledger was admitted")
	}

	// After: review + citation is the ONLY change, and the round starts.
	prep, err := New().Prepare(context.Background(), modes.PrepareInput{
		Scope:   scope("recovery-seed"),
		Subject: roster(3),
		Config:  json.RawMessage(`{}`),
		Content: build(true, "https://example.invalid/source"),
	})
	if err != nil {
		t.Fatalf("a fully reviewed, sourced ledger was refused: %v", err)
	}
	var pub publicState
	if err := json.Unmarshal(prep.Public, &pub); err != nil {
		t.Fatalf("decode public: %v", err)
	}
	var priv privateState
	if err := json.Unmarshal(prep.Private, &priv); err != nil {
		t.Fatalf("decode private: %v", err)
	}
	if !strings.Contains(pub.Prompt, "____") {
		t.Fatalf("the prompt carries no blank: %q", pub.Prompt)
	}
	if priv.Truth == "" || priv.FactSource == "" {
		t.Fatalf("the posed fact is incomplete: truth=%q source=%q", priv.Truth, priv.FactSource)
	}
	// The round must be playable end to end: three units write decoys, one votes for the truth.
	states := map[string]json.RawMessage{}
	for i, id := range []string{"p1", "p2", "p3"} {
		next, state := commit(t, prep, modes.Subject{ID: id, Kind: modes.SubjectPlayer},
			decoyAction(fmt.Sprintf("假答案%d号", i+1)), states[id])
		prep = next
		states[id] = state
	}
	next, state := commit(t, prep, modes.Subject{ID: "p1", Kind: modes.SubjectPlayer},
		voteAction(priv.TruthOptionID), states["p1"])
	prep = next
	states["p1"] = state
	scores := settle(t, prep, states)
	if got := scores["p1"].ScoreUnits; got < TruthPoints*100 {
		t.Fatalf("the unit that identified the truth scored %d units, want at least %d",
			got, TruthPoints*100)
	}
	t.Logf("a reviewed ledger yields a playable 3-unit round; truth identifier scored %d units",
		scores["p1"].ScoreUnits)
}
