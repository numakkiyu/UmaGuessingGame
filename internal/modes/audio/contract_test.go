package audio

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

// TestAudioContractKindsAreDiscriminators asserts every kind this mode accepts is a contract
// discriminator, so the HTTP layer can actually route it .
func TestAudioContractKindsAreDiscriminators(t *testing.T) {
	mapping := contractMapping(t)
	for _, kind := range New().ContractKinds() {
		if _, ok := mapping[kind]; !ok {
			t.Fatalf("audio accepts kind %q which is not in the contract discriminator mapping", kind)
		}
		t.Logf("audio kind %q -> %s", kind, mapping[kind])
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
		// The pack says unavailable. Prepare must refuse, and the refusal must be an
		// actionable AdmissionError rather than a decode crash.
		if err == nil {
			t.Fatalf("pack %s reports audio unavailable but Prepare admitted a round",
				pack.version)
		}
		var admissionErr *AdmissionError
		if !errors.As(err, &admissionErr) {
			t.Fatalf("the refusal is not an AdmissionError: %v", err)
		}
		if admissionErr.ReasonCode() == "" {
			t.Fatal("the refusal carries no reason code")
		}
		t.Logf("pack %s reports unavailable (%d clips) and Prepare agrees: %v",
			pack.version, admission.QuestionCount, err)
		return
	}

	// The pack says available. Prepare must start a round with playable content.
	if err != nil {
		t.Fatalf("pack %s reports audio available (%d clips) but Prepare refused: %v",
			pack.version, admission.QuestionCount, err)
	}
	var pub publicState
	if err := json.Unmarshal(prep.Public, &pub); err != nil {
		t.Fatalf("decode public state: %v", err)
	}
	if len(pub.Clips) < MinClips {
		t.Fatalf("round published %d clips, want at least %d", len(pub.Clips), MinClips)
	}
	for _, c := range pub.Clips {
		if c.ClipID == "" || c.AudioPath == "" || c.DurationMillis <= 0 || c.MaxPlays <= 0 {
			t.Fatalf("published clip is unplayable: %+v", c)
		}
	}
	if len(pub.Candidates) <= len(pub.Clips) {
		t.Fatalf("%d candidates for %d clips; the answer space is not larger than the answer",
			len(pub.Candidates), len(pub.Clips))
	}
	if strings.Contains(string(prep.Public), `"transcript"`) {
		t.Fatal("the public state carries a transcript, which would let a player skip listening")
	}
	t.Logf("pack %s reports available and Prepare agrees: %d clips over %d candidates",
		pack.version, len(pub.Clips), len(pub.Candidates))

	// Determinism holds in the admitting branch too.
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

// TestAdmissionGateExcludesEachSpoiledClip asserts the admission MECHANISM with a
// deterministic fixture, so the test does not depend on the state of the human review
// ledger in data/manual/.
//
// The real pack's review state is a moving target by design: a human approves or revokes
// clips, and the pack follows. A test that pinned "the real pack admits" would go red on
// every revocation, which trains people to ignore it.
//
// The assertion is on the GATE, not on the round's overall outcome. Spoiling one clip out
// of six leaves five usable ones, which is still enough to start a round — that is the
// correct behaviour, not a failure. What must hold is that the spoiled clip is never
// posed: a clip failing any one of the four gates must be absent from the published
// board.
func TestAdmissionGateExcludesEachSpoiledClip(t *testing.T) {
	// spoiledKind is the character whose clip is mutated. It must never appear in the
	// round's speaker map.
	const spoiledKind = "c001"

	cases := []struct {
		name   string
		mutate func(*clipSpec)
	}{
		{name: "not decodable", mutate: func(c *clipSpec) { c.Decodable = false }},
		{name: "speaker unverified", mutate: func(c *clipSpec) { c.SpeakerVerified = false }},
		{name: "not reviewed", mutate: func(c *clipSpec) { c.Reviewed = false }},
		{name: "no local file", mutate: func(c *clipSpec) { c.AudioPath = "" }},
		{name: "remote url only", mutate: func(c *clipSpec) {
			c.AudioPath = "https://example.invalid/v.mp3"
		}},
		{name: "zero duration", mutate: func(c *clipSpec) { c.DurationMillis = 0 }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// Six clips so five usable ones remain after the spoilage: the round can
			// still start, which is what makes this a test of the gate rather than of
			// the minimum-clip rule.
			clips := make([]clipSpec, 0, 6)
			for i := 0; i < 6; i++ {
				c := clipSpec{
					ClipID:          fmt.Sprintf("clip-%03d", i+1),
					CharacterID:     fmt.Sprintf("c%03d", i+1),
					AudioPath:       fmt.Sprintf("/assets/audio/clip-%03d.mp3", i+1),
					DurationMillis:  2000 + i*100,
					Transcript:      fmt.Sprintf("第%d段台词", i+1),
					SpeakerVerified: true,
					Reviewed:        true,
					Decodable:       true,
				}
				if i == 0 {
					tc.mutate(&c)
				}
				clips = append(clips, c)
			}
			raw, err := json.Marshal(clips)
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}
			prep, prepErr := New().Prepare(context.Background(), modes.PrepareInput{
				Scope:   scope("ledger-fixture"),
				Subject: roster(2),
				Config:  json.RawMessage(`{"clips":4}`),
				Content: fakeContent{ClipsSection: raw},
			})
			if prepErr != nil {
				t.Fatalf("Prepare refused a round with five good clips: %v", prepErr)
			}
			var priv privateState
			if err := json.Unmarshal(prep.Private, &priv); err != nil {
				t.Fatalf("decode private state: %v", err)
			}
			// The spoiled clip must not be on the board.
			for clipID, speaker := range priv.SpeakerOf {
				if speaker == spoiledKind {
					t.Fatalf("the spoiled clip %q (speaker %s) was posed despite failing %q",
						clipID, speaker, tc.name)
				}
			}
			if len(priv.SpeakerOf) != 4 {
				t.Fatalf("round posed %d clips, want 4", len(priv.SpeakerOf))
			}
		})
	}

	// A pack where every clip is spoiled must refuse outright.
	t.Run("all spoiled", func(t *testing.T) {
		clips := make([]clipSpec, 0, 6)
		for i := 0; i < 6; i++ {
			clips = append(clips, clipSpec{
				ClipID:          fmt.Sprintf("clip-%03d", i+1),
				CharacterID:     fmt.Sprintf("c%03d", i+1),
				AudioPath:       fmt.Sprintf("/assets/audio/clip-%03d.mp3", i+1),
				DurationMillis:  2000,
				Transcript:      "台词",
				Decodable:       true,
				Reviewed:        false, // fails the review gate
				SpeakerVerified: true,
			})
		}
		raw, err := json.Marshal(clips)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		_, prepErr := New().Prepare(context.Background(), modes.PrepareInput{
			Scope:   scope("ledger-fixture"),
			Subject: roster(2),
			Config:  json.RawMessage(`{"clips":4}`),
			Content: fakeContent{ClipsSection: raw},
		})
		if prepErr == nil {
			t.Fatal("Prepare admitted a round where every clip failed the review gate")
		}
		var admissionErr *AdmissionError
		if !errors.As(prepErr, &admissionErr) {
			t.Fatalf("the refusal is not an AdmissionError: %v", prepErr)
		}
	})
}

// TestRecoveryIsMechanical asserts that the mode needs NO code change to become playable
// again: a ledger with verified clips is the only input it is waiting for.
//
// This matters because audio and bluff are currently unavailable for a CONTENT reason (nobody
// has listened to the clips), not a code reason. The distinction is easy to lose: a mode that
// has been unavailable for weeks starts to look like it needs work. This test pins the fact
// that the engine is finished and the gate is mechanical — the same Prepare that refuses an
// unverified ledger admits a verified one, with no other change.
//
// The fixture is built here rather than read from data/manual/, because the real review ledger
// is a moving human artefact and a test pinned to it would go red on every revocation, which
// trains people to ignore it.
func TestRecoveryIsMechanical(t *testing.T) {
	// Build a ledger of clips that pass all four gates, exactly as a human-reviewed set would.
	const clipCount = 8
	clips := make([]clipSpec, 0, clipCount)
	for i := 0; i < clipCount; i++ {
		clips = append(clips, clipSpec{
			ClipID:          fmt.Sprintf("reviewed-%03d", i+1),
			CharacterID:     fmt.Sprintf("c%03d", i+1),
			AudioPath:       fmt.Sprintf("/assets/audio/reviewed-%03d.mp3", i+1),
			DurationMillis:  2000 + i*50,
			Transcript:      fmt.Sprintf("已听审台词 %d", i+1),
			SpeakerVerified: true,
			Reviewed:        true,
			Decodable:       true,
		})
	}
	raw, err := json.Marshal(clips)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	verified := fakeContent{ClipsSection: raw}

	// Before: the same pack with one gate failing must refuse.
	unreviewed := make([]clipSpec, len(clips))
	copy(unreviewed, clips)
	for i := range unreviewed {
		unreviewed[i].Reviewed = false
	}
	rawBad, err := json.Marshal(unreviewed)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if _, prepErr := New().Prepare(context.Background(), modes.PrepareInput{
		Scope:   scope("recovery-seed"),
		Subject: roster(2),
		Config:  json.RawMessage(`{}`),
		Content: fakeContent{ClipsSection: rawBad},
	}); prepErr == nil {
		t.Fatal("an unreviewed ledger was admitted")
	}

	// After: flipping the review flag is the ONLY change, and the round starts.
	prep, err := New().Prepare(context.Background(), modes.PrepareInput{
		Scope:   scope("recovery-seed"),
		Subject: roster(2),
		Config:  json.RawMessage(`{}`),
		Content: verified,
	})
	if err != nil {
		t.Fatalf("a fully verified ledger was refused: %v", err)
	}
	var pub publicState
	if err := json.Unmarshal(prep.Public, &pub); err != nil {
		t.Fatalf("decode public: %v", err)
	}
	if len(pub.Clips) != 4 {
		t.Fatalf("round posed %d clips, want the default 4", len(pub.Clips))
	}
	for _, c := range pub.Clips {
		if c.ClipID == "" || c.AudioPath == "" || c.DurationMillis <= 0 || c.MaxPlays <= 0 {
			t.Fatalf("posed clip is unplayable: %+v", c)
		}
	}
	// The whole round must be playable end to end: pair everything and score full marks.
	pairs := make([]string, 0, len(pub.Clips)*2)
	var priv privateState
	if err := json.Unmarshal(prep.Private, &priv); err != nil {
		t.Fatalf("decode private: %v", err)
	}
	for _, c := range pub.Clips {
		pairs = append(pairs, c.ClipID, priv.SpeakerOf[c.ClipID])
	}
	dec, state := apply(t, prep, modes.Subject{ID: "p1", Kind: modes.SubjectPlayer},
		matchesAction(pairs...), nil)
	if !dec.Accepted {
		t.Fatalf("the correct pairing was refused: %q %q", dec.Code, dec.Detail)
	}
	scores := settle(t, prep, map[string]json.RawMessage{"p1": state})
	if got := scores["p1"].ScoreUnits; got != 10000 {
		t.Fatalf("a fully correct round scored %d units, want 10000", got)
	}
	t.Logf("a verified ledger of %d clips yields a playable 4-clip round scoring 100", clipCount)
}
