package modeutil_test

// The external test package avoids an import cycle while exercising every mode against a compiled pack.
// Without a pack the test skips; a present but incompatible pack fails.

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
	"github.com/bhcn/umaguessinggame/internal/modes/modeutil"
	"github.com/bhcn/umaguessinggame/internal/modes/racerebuild"
	sortgame "github.com/bhcn/umaguessinggame/internal/modes/sort"
)

// packSections is a ContentSource backed by the published files on disk.
type packSections struct {
	public  map[string]json.RawMessage
	private map[string]json.RawMessage
	version string
}

// Section implements modes.ContentSource: public sections only.
func (p *packSections) Section(name string) (json.RawMessage, bool) {
	raw, ok := p.public[name]
	return raw, ok
}

// Version implements modes.ContentSource.
func (p *packSections) Version() string { return p.version }

// Secrets implements modeutil.SecretSource.
func (p *packSections) Secrets() modes.ContentSource {
	return &privateSections{private: p.private, version: p.version}
}

// privateSections exposes only the private sections.
type privateSections struct {
	private map[string]json.RawMessage
	version string
}

func (p *privateSections) Section(name string) (json.RawMessage, bool) {
	raw, ok := p.private[name]
	return raw, ok
}

func (p *privateSections) Version() string { return p.version }

// sectionFile maps a canonical section name to the on-disk file name.
//
// The compiler writes each section as a flat file named by replacing the dot with
// an underscore (for example classic.characters →
// public/classic_characters.json), so the adapter derives the name the same way
// instead of hard-coding a table that could drift.
func sectionFile(section string) string {
	return strings.ReplaceAll(section, ".", "_") + ".json"
}

// loadRealPack locates and loads the current compiled pack, or reports "" when
// none is present.
func loadRealPack(t *testing.T) *packSections {
	t.Helper()
	// The test runs with the package directory as the working directory, so the
	// repository root is three levels up.
	root := filepath.Join("..", "..", "..")

	// content/current is a file holding the active version directory name.
	currentPtr := filepath.Join(root, "content", "current")
	versionBytes, err := os.ReadFile(currentPtr)
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

	pack := &packSections{
		public:  map[string]json.RawMessage{},
		private: map[string]json.RawMessage{},
		version: version,
	}
	readDir := func(sub string, into map[string]json.RawMessage) {
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
			// Restore the dotted canonical name from the flat file name.
			name := strings.TrimSuffix(entry.Name(), ".json")
			name = strings.ReplaceAll(name, "_", ".")
			into[name] = json.RawMessage(raw)
		}
	}
	readDir("public", pack.public)
	readDir("private", pack.private)
	if len(pack.public) == 0 {
		return nil
	}
	return pack
}

// publicNames returns the sorted public section names.
func (p *packSections) publicNames() []string {
	out := make([]string, 0, len(p.public))
	for name := range p.public {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

// privateNames returns the sorted private section names.
func (p *packSections) privateNames() []string {
	out := make([]string, 0, len(p.private))
	for name := range p.private {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

// TestRealPackParsesAllModes drives Prepare, Project and Settle for every mode
// against the real compiled pack.
//
// This is the check that the section contract holds against actual bytes rather
// than against a hand-written fixture: a field rename or a shape change in the
// pipeline shows up here immediately .
func TestRealPackParsesAllModes(t *testing.T) {
	pack := loadRealPack(t)
	if pack == nil {
		t.Skip("no compiled content pack present; run cmd/content first")
	}
	t.Logf("pack %s public sections: %v", pack.version, pack.publicNames())
	t.Logf("pack %s private sections: %v", pack.version, pack.privateNames())

	subjects := []modes.Subject{{ID: "p1", Kind: modes.SubjectPlayer}}

	cases := []struct {
		mode   modes.Mode
		config string
		// unavailable reports that this mode is documented as not admitted by the
		// current pack, so Prepare must fail with a clear reason instead of a
		// panic or a silent empty round.
		unavailable bool
	}{
		{mode: classic.New(), config: `{}`},
		{mode: aptitudegrid.New(), config: `{}`},
		{mode: draft.New(), config: `{}`},
		{mode: racerebuild.New(), config: `{}`},
		{mode: coracepath.New(), config: `{}`},
		{mode: sortgame.New(), config: `{"key":"height"}`},
		{mode: memory.New(), config: `{}`},
	}

	for _, tc := range cases {
		name := tc.mode.ID()
		t.Run(name, func(t *testing.T) {
			scope := modes.Scope{
				ModeID:             name,
				RulesVersion:       tc.mode.RulesVersion(),
				DatasetVersion:     "real-pack-test",
				ContentPackVersion: pack.version,
				Seed:               "integration-seed",
			}
			prep, err := tc.mode.Prepare(context.Background(), modes.PrepareInput{
				Scope:   scope,
				Subject: subjects,
				Config:  json.RawMessage(tc.config),
				Content: pack,
			})
			if tc.unavailable {
				if err == nil {
					t.Fatalf("mode %s is documented unavailable but Prepare succeeded", name)
				}
				t.Logf("mode %s correctly refused: %v", name, err)
				return
			}
			if err != nil {
				t.Fatalf("mode %s failed to prepare against the real pack: %v", name, err)
			}
			if !json.Valid(prep.Public) {
				t.Fatalf("mode %s produced invalid public JSON", name)
			}
			if !json.Valid(prep.Private) {
				t.Fatalf("mode %s produced invalid private JSON", name)
			}
			if prep.QuestionCount <= 0 {
				t.Fatalf("mode %s produced questionCount %d", name, prep.QuestionCount)
			}

			// Prepare must be deterministic against the real pack too.
			again, err := tc.mode.Prepare(context.Background(), modes.PrepareInput{
				Scope:   scope,
				Subject: subjects,
				Config:  json.RawMessage(tc.config),
				Content: pack,
			})
			if err != nil {
				t.Fatalf("mode %s second prepare failed: %v", name, err)
			}
			if string(prep.Public) != string(again.Public) {
				t.Fatalf("mode %s public state is not deterministic", name)
			}
			if string(prep.Private) != string(again.Private) {
				t.Fatalf("mode %s private state is not deterministic", name)
			}

			// Project must succeed for a player.
			projected, err := tc.mode.Project(modes.ProjectInput{
				Scope:   scope,
				Viewer:  modes.Viewer{MembershipID: "m1", Role: "player", Subject: subjects[0]},
				Public:  prep.Public,
				Private: prep.Private,
				States:  map[string]json.RawMessage{},
				Phase:   modes.PhaseCollecting,
			})
			if err != nil {
				t.Fatalf("mode %s failed to project: %v", name, err)
			}
			if !json.Valid(projected) {
				t.Fatalf("mode %s produced invalid projection JSON", name)
			}

			// Settle must succeed and produce a valid score per subject.
			scores, err := tc.mode.Settle(modes.SettleInput{
				Scope:   scope,
				Phase:   modes.PhaseSettled,
				Public:  prep.Public,
				Private: prep.Private,
				States:  map[string]json.RawMessage{},
			})
			if err != nil {
				t.Fatalf("mode %s failed to settle: %v", name, err)
			}
			if len(scores) != len(subjects) {
				t.Fatalf("mode %s settled %d scores, want %d", name, len(scores), len(subjects))
			}
			for _, score := range scores {
				if err := score.Validate(); err != nil {
					t.Fatalf("mode %s produced an invalid score: %v", name, err)
				}
			}
		})
	}
}

// TestRealPackPrivateSectionsReachable asserts the private answer sections are
// readable through the SecretSource adapter, which is what race-rebuild depends on
// to grade at all .
func TestRealPackPrivateSectionsReachable(t *testing.T) {
	pack := loadRealPack(t)
	if pack == nil {
		t.Skip("no compiled content pack present; run cmd/content first")
	}
	secrets := pack.Secrets()
	if secrets == nil {
		t.Fatal("the pack adapter exposes no private store")
	}
	raw, ok := secrets.Section(racerebuild.PrivateAnswersSection)
	if !ok {
		t.Fatalf("private section %q is missing from the pack", racerebuild.PrivateAnswersSection)
	}
	if !json.Valid(raw) {
		t.Fatalf("private section %q is not valid JSON", racerebuild.PrivateAnswersSection)
	}

	// The public section must not carry the answer: no rank field anywhere.
	public, ok := pack.Section("race-rebuild.races")
	if !ok {
		t.Fatal("public section race-rebuild.races is missing")
	}
	for _, forbidden := range []string{"finishRank", "finish_rank", "tieGroup", "tie_group", "ranks"} {
		if strings.Contains(string(public), forbidden) {
			t.Fatalf("public race section contains the answer field %q", forbidden)
		}
	}
}

// TestRealPackRegistryBuildsAllModes asserts the seven modes register together
// without an id collision, which is what the lobby enumerates .
func TestRealPackRegistryBuildsAllModes(t *testing.T) {
	registry, err := modes.NewRegistry(
		classic.New(),
		aptitudegrid.New(),
		draft.New(),
		racerebuild.New(),
		coracepath.New(),
		sortgame.New(),
		memory.New(),
	)
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}
	want := []string{
		"aptitude-grid", "classic", "co-race-path", "draft",
		"memory", "race-rebuild", "sort",
	}
	got := registry.IDs()
	if len(got) != len(want) {
		t.Fatalf("registry ids = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("registry ids = %v, want %v", got, want)
		}
	}
	for _, mode := range registry.All() {
		if mode.Title() == "" || mode.Summary() == "" {
			t.Fatalf("mode %s has an empty title or summary", mode.ID())
		}
		if mode.MinSubjects() < 1 || mode.MaxSubjects() < mode.MinSubjects() {
			t.Fatalf("mode %s has invalid subject bounds %d..%d",
				mode.ID(), mode.MinSubjects(), mode.MaxSubjects())
		}
		req := mode.Requirement()
		if len(req.Fields) == 0 {
			t.Fatalf("mode %s declares no content fields", mode.ID())
		}
		if req.MinEntities < 1 {
			t.Fatalf("mode %s declares minEntities %d", mode.ID(), req.MinEntities)
		}
		if err := mode.ValidateConfig(nil); err != nil {
			t.Fatalf("mode %s rejects an empty config: %v", mode.ID(), err)
		}
	}
}

// ensure the adapter satisfies the interfaces the modes rely on.
var (
	_ modes.ContentSource   = (*packSections)(nil)
	_ modeutil.SecretSource = (*packSections)(nil)
)
