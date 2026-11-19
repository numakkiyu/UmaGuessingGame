package content

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/bhcn/umaguessinggame/internal/modes"
)

// fixtureRoot returns the repository root. The tests build against the real
// scrape outputs because a fabricated fixture would not prove the adapter matches
// the real files.
func fixtureRoot(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	root := filepath.Clean(filepath.Join(wd, "..", ".."))
	if _, err := os.Stat(filepath.Join(root, "data", "question_bank_ready.json")); err != nil {
		t.Skipf("real data not available at %s: %v", root, err)
	}
	return root
}

// buildInto compiles a pack into a temp dir so tests never touch content/.
func buildInto(t *testing.T) (*PublishResult, string) {
	t.Helper()
	root := fixtureRoot(t)
	out := t.TempDir()
	res, err := Publish(PublishOptions{
		Root:      root,
		OutputDir: out,
		Now:       time.Date(2026, 11, 8, 0, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("publish: %v", err)
	}
	return res, out
}

func TestLoadRealInputs(t *testing.T) {
	root := fixtureRoot(t)
	in, err := Load(DefaultSourcePaths(root))
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	// The counts are the documented snapshot figures; a change here means the
	// scrape output moved and the adapter needs re-checking.
	if got, want := len(in.Ready), 118; got != want {
		t.Errorf("ready entries = %d, want %d", got, want)
	}
	if got, want := len(in.Blocked), 23; got != want {
		t.Errorf("blocked entries = %d, want %d", got, want)
	}
	if got, want := len(in.Wiki.Characters), 147; got != want {
		t.Errorf("wiki characters = %d, want %d", got, want)
	}
	if got, want := len(in.Outfits.Outfits), 272; got != want {
		t.Errorf("outfits = %d, want %d", got, want)
	}
	if got, want := len(in.Normalized), 141; got != want {
		t.Errorf("normalized files = %d, want %d", got, want)
	}
	// Every source snapshot must carry a real hash, otherwise a pack cannot be
	// tied to its inputs.
	for _, s := range in.Snapshots {
		if len(s.SHA256) != 64 {
			t.Errorf("snapshot %q has sha256 %q, want 64 hex chars", s.Name, s.SHA256)
		}
	}
}

func TestLegacyIDMappingIsBidirectional(t *testing.T) {
	root := fixtureRoot(t)
	in, err := Load(DefaultSourcePaths(root))
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	ds, _, err := BuildDataset(in, "2026-11-08T00:00:00Z", "ds-test", root)
	if err != nil {
		t.Fatalf("build dataset: %v", err)
	}
	if len(ds.ByLegacyID) == 0 {
		t.Fatal("no legacy id mapping was built")
	}
	for legacy, numeric := range ds.ByLegacyID {
		back, ok := ds.ByNumericID[numeric]
		if !ok {
			t.Fatalf("numeric id %q has no reverse legacy mapping", numeric)
		}
		if back != legacy {
			t.Fatalf("round trip %q -> %q -> %q is not stable", legacy, numeric, back)
		}
	}
	// The documented join is by Japanese name; the romanised id must resolve to a
	// canonical id that exists.
	canonical, ok := ds.CanonicalID("supesharuuiiku")
	if !ok {
		t.Fatal("legacy id supesharuuiiku did not resolve")
	}
	c, ok := ds.CharacterByID(canonical)
	if !ok {
		t.Fatalf("canonical id %q not found", canonical)
	}
	if c.Names.Ja != "スペシャルウィーク" {
		t.Errorf("legacy id resolved to %q, want スペシャルウィーク", c.Names.Ja)
	}
}

func TestManifestContainsEveryRequiredField(t *testing.T) {
	res, _ := buildInto(t)
	m := res.Manifest

	if m.SchemaVersion != SchemaVersion {
		t.Errorf("schemaVersion = %q, want %q", m.SchemaVersion, SchemaVersion)
	}
	if m.DatasetVersion == "" {
		t.Error("datasetVersion is empty")
	}
	if m.ContentPackVersion == "" {
		t.Error("contentPackVersion is empty")
	}
	if m.GeneratedAt == "" {
		t.Error("generatedAt is empty")
	}
	if len(m.SourceSnapshotHashes) == 0 {
		t.Error("sourceSnapshotHashes is empty")
	}
	if m.Locale == "" || m.Region == "" {
		t.Errorf("locale/region = %q/%q, want both set", m.Locale, m.Region)
	}
	if m.ReviewStatus == "" {
		t.Error("reviewStatus is empty")
	}
	if m.EntityCounts.Characters == 0 {
		t.Error("entityCounts.characters is zero")
	}
	if m.SHA256 == "" {
		t.Error("sha256 is empty")
	}
	if len(m.Modes) != len(AllModes) {
		t.Errorf("manifest lists %d modes, want %d", len(m.Modes), len(AllModes))
	}
	// A manifest must verify against its own checksum.
	if err := VerifyManifest(m); err != nil {
		t.Fatalf("manifest does not verify: %v", err)
	}
}

func TestChecksumDriftIsRejected(t *testing.T) {
	_, out := buildInto(t)
	dir, err := ResolveCurrent(out)
	if err != nil {
		t.Fatalf("resolve current: %v", err)
	}
	// A clean pack opens.
	if _, err := OpenPack(dir); err != nil {
		t.Fatalf("open clean pack: %v", err)
	}

	// Flip one byte inside a published public section: the manifest is unchanged,
	// so only the per-file digest can catch it.
	sectionPath := filepath.Join(dir, PublicDirName, sectionFileName(SectionClassicCharacters))
	orig, err := os.ReadFile(sectionPath)
	if err != nil {
		t.Fatalf("read section: %v", err)
	}
	if err := os.WriteFile(sectionPath, append(orig, ' '), 0o644); err != nil {
		t.Fatalf("tamper section: %v", err)
	}
	if _, err := OpenPack(dir); err == nil {
		t.Fatal("tampered section was accepted, want a checksum rejection")
	}
	if err := os.WriteFile(sectionPath, orig, 0o644); err != nil {
		t.Fatalf("restore section: %v", err)
	}

	// Now tamper the manifest body itself: its own checksum must catch it.
	manifestPath := filepath.Join(dir, ManifestName)
	mb, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatalf("read manifest: %v", err)
	}
	var m Manifest
	if err := json.Unmarshal(mb, &m); err != nil {
		t.Fatalf("parse manifest: %v", err)
	}
	m.EntityCounts.Characters = 999
	tampered, err := json.Marshal(&m)
	if err != nil {
		t.Fatalf("marshal tampered manifest: %v", err)
	}
	if err := os.WriteFile(manifestPath, tampered, 0o644); err != nil {
		t.Fatalf("write tampered manifest: %v", err)
	}
	if _, err := OpenPack(dir); err == nil {
		t.Fatal("tampered manifest was accepted, want a checksum rejection")
	}
}

func TestUnavailableModesReportReasonAndHaveNoSection(t *testing.T) {
	res, _ := buildInto(t)

	// Every mode must be classified, and an unavailable one must explain itself
	// and must not advertise a section.
	seen := make(map[string]bool)
	for _, a := range res.Admissions {
		seen[a.Mode] = true
		if a.Available {
			if a.QuestionCount < a.MinItems {
				t.Errorf("%s: available with %d items but minimum is %d", a.Mode, a.QuestionCount, a.MinItems)
			}
			if a.Section == "" {
				t.Errorf("%s: available but names no section", a.Mode)
			}
			continue
		}
		if a.Reason == "" {
			t.Errorf("%s: unavailable with no reason", a.Mode)
		}
		if a.Section != "" {
			t.Errorf("%s: unavailable but names section %q", a.Mode, a.Section)
		}
		if a.QuestionCount != 0 {
			t.Errorf("%s: unavailable but reports %d items", a.Mode, a.QuestionCount)
		}
	}
	for _, m := range AllModes {
		if !seen[m] {
			t.Errorf("mode %q was never classified", m)
		}
	}

	// Classic is the readiness gate, so it must be available with real content.
	classic, ok := findAdmission(res.Admissions, ModeClassic)
	if !ok {
		t.Fatal("classic admission missing")
	}
	if !classic.Available {
		t.Fatalf("classic is unavailable: %s", classic.Reason)
	}
	if classic.QuestionCount < 100 {
		t.Errorf("classic pool = %d, want at least 100", classic.QuestionCount)
	}
}

func TestMissingDataDisablesModeWithoutFailingTheBuild(t *testing.T) {
	res, out := buildInto(t)

	// memory needs verified avatar files; the checkout has none, so it must be
	// unavailable with an asset reason rather than the build failing.
	mem, ok := findAdmission(res.Admissions, ModeMemory)
	if !ok {
		t.Fatal("memory admission missing")
	}
	if mem.Available {
		t.Skip("this checkout has verified avatar files, so memory is available")
	}
	if mem.Reason == "" {
		t.Error("memory is unavailable with no reason")
	}
	if len(mem.Rejections) == 0 {
		t.Error("memory is unavailable but reports no rejection distribution")
	}

	// The pack itself still loads and still carries the available sections.
	dir, err := ResolveCurrent(out)
	if err != nil {
		t.Fatalf("resolve current: %v", err)
	}
	pack, err := OpenPack(dir)
	if err != nil {
		t.Fatalf("open pack: %v", err)
	}
	if _, ok := pack.Section(SectionMemoryPairs); ok {
		t.Error("memory.pairs is published even though memory is unavailable")
	}
	if _, ok := pack.Section(SectionClassicCharacters); !ok {
		t.Error("classic.characters is missing from a pack whose classic mode is available")
	}
}

func TestPrimarySectionNamesResolve(t *testing.T) {
	res, out := buildInto(t)
	dir, err := ResolveCurrent(out)
	if err != nil {
		t.Fatalf("resolve current: %v", err)
	}
	pack, err := OpenPack(dir)
	if err != nil {
		t.Fatalf("open pack: %v", err)
	}

	// The seven primary names are the contract with the mode packages. A name
	// whose mode is available must resolve; a name whose mode is unavailable must
	// miss cleanly instead of returning an empty payload.
	primary := map[string]string{
		SectionClassicCharacters: ModeClassic,
		SectionAptitudeGridCards: ModeAptitudeGrid,
		SectionDraftMarket:       ModeDraft,
		SectionRaceRebuildRaces:  ModeRaceRebuild,
		SectionCoRacePathGraph:   ModeCoRacePath,
		SectionSortCards:         ModeSort,
		SectionMemoryPairs:       ModeMemory,
	}
	for section, mode := range primary {
		adm, ok := findAdmission(res.Admissions, mode)
		if !ok {
			t.Fatalf("no admission for %s", mode)
		}
		b, found := pack.Section(section)
		if adm.Available {
			if !found {
				t.Errorf("%s: mode is available but section %q does not resolve", mode, section)
				continue
			}
			if !json.Valid(b) {
				t.Errorf("%s: section %q is not valid JSON", mode, section)
			}
			if len(b) == 0 {
				t.Errorf("%s: section %q is empty", mode, section)
			}
			continue
		}
		if found {
			t.Errorf("%s: mode is unavailable but section %q still resolves", mode, section)
		}
	}
}

func TestAliasesResolveToPrimarySections(t *testing.T) {
	_, out := buildInto(t)
	dir, err := ResolveCurrent(out)
	if err != nil {
		t.Fatalf("resolve current: %v", err)
	}
	pack, err := OpenPack(dir)
	if err != nil {
		t.Fatalf("open pack: %v", err)
	}
	for alias, primaryName := range sectionAliases {
		bAlias, okAlias := pack.Section(alias)
		bPrimary, okPrimary := pack.Section(primaryName)
		if okAlias != okPrimary {
			t.Errorf("alias %q resolves=%v but primary %q resolves=%v", alias, okAlias, primaryName, okPrimary)
			continue
		}
		if okAlias && string(bAlias) != string(bPrimary) {
			t.Errorf("alias %q and primary %q returned different payloads", alias, primaryName)
		}
	}
}

func TestPackSatisfiesContentSource(t *testing.T) {
	_, out := buildInto(t)
	dir, err := ResolveCurrent(out)
	if err != nil {
		t.Fatalf("resolve current: %v", err)
	}
	pack, err := OpenPack(dir)
	if err != nil {
		t.Fatalf("open pack: %v", err)
	}
	// Assign to the interface the rule engine declares, so a signature drift is a
	// compile error rather than a runtime surprise.
	var src modes.ContentSource = pack
	if src.Version() == "" {
		t.Error("ContentSource.Version() is empty")
	}
	if src.Version() != pack.Manifest().ContentPackVersion {
		t.Errorf("Version() = %q, want %q", src.Version(), pack.Manifest().ContentPackVersion)
	}
	if _, ok := src.Section("this.section.does.not.exist"); ok {
		t.Error("Section() returned ok for an unknown name")
	}
}

func TestPublicAndPrivateAreSeparate(t *testing.T) {
	_, out := buildInto(t)
	dir, err := ResolveCurrent(out)
	if err != nil {
		t.Fatalf("resolve current: %v", err)
	}
	pubDir := filepath.Join(dir, PublicDirName)
	privDir := filepath.Join(dir, PrivateDirName)

	if _, err := os.Stat(pubDir); err != nil {
		t.Fatalf("public dir missing: %v", err)
	}
	if _, err := os.Stat(privDir); err != nil {
		t.Fatalf("private dir missing: %v", err)
	}

	// The answer pool must exist in private and must not exist in public.
	answerInPrivate := filepath.Join(privDir, sectionFileName(PrivateClassicAnswerPool))
	if _, err := os.Stat(answerInPrivate); err != nil {
		t.Fatalf("classic answer pool missing from private tree: %v", err)
	}
	entries, err := os.ReadDir(pubDir)
	if err != nil {
		t.Fatalf("read public dir: %v", err)
	}
	for _, e := range entries {
		if e.Name() == sectionFileName(PrivateClassicAnswerPool) {
			t.Fatal("classic answer pool was written into the public tree")
		}
	}

	// The public tree must never be reachable from the private tree's parent as a
	// secret: assert no public file mentions the answer seed namespace.
	answerBytes, err := os.ReadFile(answerInPrivate)
	if err != nil {
		t.Fatalf("read answer pool: %v", err)
	}
	var pool privateClassicPool
	if err := json.Unmarshal(answerBytes, &pool); err != nil {
		t.Fatalf("parse answer pool: %v", err)
	}
	if pool.SeedNamespace == "" {
		t.Error("answer pool carries no seed namespace")
	}
	if len(pool.AnswerIDs) == 0 {
		t.Error("answer pool carries no answer ids")
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		b, err := os.ReadFile(filepath.Join(pubDir, e.Name()))
		if err != nil {
			t.Fatalf("read public file %s: %v", e.Name(), err)
		}
		if containsBytes(b, []byte(pool.SeedNamespace)) {
			t.Errorf("public file %s leaks the answer seed namespace", e.Name())
		}
	}

	// Answer material must never be published under web/ either .
	root := fixtureRoot(t)
	webDir := filepath.Join(root, "web")
	if _, err := os.Stat(webDir); err == nil {
		_ = filepath.Walk(webDir, func(path string, info os.FileInfo, err error) error {
			if err != nil || info.IsDir() {
				return nil
			}
			if info.Name() == sectionFileName(PrivateClassicAnswerPool) {
				t.Errorf("answer pool was published under web/: %s", path)
			}
			return nil
		})
	}
}

func TestAtomicPublishKeepsPreviousVersionOnFailure(t *testing.T) {
	root := fixtureRoot(t)
	out := t.TempDir()

	// First publish succeeds and becomes current.
	first, err := Publish(PublishOptions{Root: root, OutputDir: out})
	if err != nil {
		t.Fatalf("first publish: %v", err)
	}
	firstCurrent, err := ResolveCurrent(out)
	if err != nil {
		t.Fatalf("resolve after first publish: %v", err)
	}
	if firstCurrent != first.Path {
		t.Fatalf("current points at %q, want %q", firstCurrent, first.Path)
	}

	// A failing build must not disturb the live version. An empty root has no
	// data/, so Load fails.
	emptyRoot := t.TempDir()
	if _, err := Publish(PublishOptions{Root: emptyRoot, OutputDir: out}); err == nil {
		t.Fatal("publish against an empty root succeeded, want a load error")
	}
	stillCurrent, err := ResolveCurrent(out)
	if err != nil {
		t.Fatalf("resolve after failed publish: %v", err)
	}
	if stillCurrent != first.Path {
		t.Fatalf("current moved to %q after a failed publish, want %q", stillCurrent, first.Path)
	}
	if _, err := OpenPack(stillCurrent); err != nil {
		t.Fatalf("previous version no longer opens: %v", err)
	}

	// No staging directory may survive a failed run.
	entries, err := os.ReadDir(out)
	if err != nil {
		t.Fatalf("read output dir: %v", err)
	}
	for _, e := range entries {
		if len(e.Name()) > 9 && e.Name()[:9] == ".staging-" {
			t.Errorf("staging directory %q was left behind", e.Name())
		}
	}
}

func TestRepublishIsDeterministic(t *testing.T) {
	root := fixtureRoot(t)
	now := time.Date(2026, 11, 8, 0, 0, 0, 0, time.UTC)

	a, err := Publish(PublishOptions{Root: root, OutputDir: t.TempDir(), Now: now})
	if err != nil {
		t.Fatalf("publish a: %v", err)
	}
	b, err := Publish(PublishOptions{Root: root, OutputDir: t.TempDir(), Now: now})
	if err != nil {
		t.Fatalf("publish b: %v", err)
	}
	if a.Version != b.Version {
		t.Errorf("pack version is not deterministic: %q vs %q", a.Version, b.Version)
	}
	if a.Manifest.SHA256 != b.Manifest.SHA256 {
		t.Errorf("manifest checksum is not deterministic: %q vs %q", a.Manifest.SHA256, b.Manifest.SHA256)
	}
	if a.Manifest.DatasetVersion != b.Manifest.DatasetVersion {
		t.Errorf("dataset version is not deterministic: %q vs %q", a.Manifest.DatasetVersion, b.Manifest.DatasetVersion)
	}
}

func TestQualityReportStatesDocumentedGaps(t *testing.T) {
	res, _ := buildInto(t)
	r := res.Report

	// The documented gaps must each be represented. A report that omits one would
	// be hiding a known shortfall .
	wantAreas := []string{
		"entities", "names", "aptitudes", "skills", "goals",
		"prototype", "audio", "assets", "races", "classic", "translations", "joins", "manual",
	}
	got := make(map[string]bool, len(r.Gaps))
	for _, g := range r.Gaps {
		got[g.Area] = true
		if g.Measured == "" {
			t.Errorf("gap %s has no measured value", g.ID)
		}
		if g.Detail == "" {
			t.Errorf("gap %s has no detail", g.ID)
		}
		if g.Severity == "" {
			t.Errorf("gap %s has no severity", g.ID)
		}
	}
	for _, area := range wantAreas {
		if !got[area] {
			t.Errorf("quality report is missing the %q data gap", area)
		}
	}

	// The specific documented figures must be present, not a blanket pass.
	if r.EntityCounts.CharactersNPC+r.EntityCounts.CharactersPlanned+r.EntityCounts.CharactersPlayable != r.EntityCounts.Characters {
		t.Errorf("entity kinds (%d+%d+%d) do not sum to the character total %d",
			r.EntityCounts.CharactersPlayable, r.EntityCounts.CharactersPlanned,
			r.EntityCounts.CharactersNPC, r.EntityCounts.Characters)
	}
	if r.Playability.InferredNames != 5 {
		t.Errorf("inferred Japanese names = %d, want the documented 5", r.Playability.InferredNames)
	}
	if r.EntityCounts.PrototypeHorses != 134 {
		t.Errorf("prototype blocks = %d, want the documented 134", r.EntityCounts.PrototypeHorses)
	}
	if got := r.EntityCounts.PrototypeHorses - r.EntityCounts.PrototypeWithNKB; got != 34 {
		t.Errorf("prototypes without an NKB id = %d, want the documented 34", got)
	}
	if r.EntityCounts.Races != 1590 {
		t.Errorf("raceIds = %d, want the documented 1590", r.EntityCounts.Races)
	}
	if r.EntityCounts.RaceEntries != 2127 {
		t.Errorf("race entries = %d, want the documented 2127", r.EntityCounts.RaceEntries)
	}
	if r.Assets.VerifiedOnDisk != 0 {
		t.Logf("note: this checkout has %d verified avatar files", r.Assets.VerifiedOnDisk)
	}
	if r.Assets.ManifestEntries == 0 || r.Assets.CacheIndexEntries == 0 {
		t.Error("asset summary did not read the manifest or the cache index")
	}

	// The report must not claim approval while review is pending.
	if r.ReviewStatus == ReviewApproved {
		t.Error("quality report claims approved review while nothing was reviewed")
	}
	if len(r.Limitations) == 0 {
		t.Error("quality report lists no limitations")
	}
}

func TestSkillAndGoalGapsMatchMeasuredFigures(t *testing.T) {
	root := fixtureRoot(t)
	in, err := Load(DefaultSourcePaths(root))
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	skill := measureSkillAttribution(in.Wiki.Characters, in.Skills.Skills)
	if skill.Rows != 343 {
		t.Errorf("skill attribution rows = %d, want the documented 343", skill.Rows)
	}
	if skill.Characters != 41 {
		t.Errorf("characters with skill rows = %d, want the documented 41", skill.Characters)
	}
	if skill.ExactMatches != 22 {
		t.Errorf("exact skill-name matches = %d, want the documented 22", skill.ExactMatches)
	}
	if len(in.Skills.Skills) != 1289 {
		t.Errorf("skill definitions = %d, want the documented 1289", len(in.Skills.Skills))
	}

	goal := measureGoals(in.Wiki.Characters)
	if goal.Total != 265 {
		t.Errorf("goal rows = %d, want the documented 265", goal.Total)
	}
	if goal.Characters != 38 {
		t.Errorf("characters with goals = %d, want the documented 38", goal.Characters)
	}
	if goal.MaxIndex != 7 {
		t.Errorf("max goal index = %d, want the documented 7 (the scrape only parses 1-7)", goal.MaxIndex)
	}
}

func TestClassicExcludesAmbiguousVectorsFromCompetitivePool(t *testing.T) {
	_, out := buildInto(t)
	dir, err := ResolveCurrent(out)
	if err != nil {
		t.Fatalf("resolve current: %v", err)
	}
	pack, err := OpenPack(dir)
	if err != nil {
		t.Fatalf("open pack: %v", err)
	}
	raw, ok := pack.Section(SectionClassicCharacters)
	if !ok {
		t.Fatal("classic.characters missing")
	}
	var sec classicSection
	if err := json.Unmarshal(raw, &sec); err != nil {
		t.Fatalf("parse classic section: %v", err)
	}
	if sec.CompetitiveCount != len(sec.Cards) {
		t.Errorf("competitiveCount = %d but cards = %d", sec.CompetitiveCount, len(sec.Cards))
	}
	if sec.AmbiguousExcluded == 0 {
		t.Fatal("no ambiguous vector was excluded, but the data has colliding nine-field vectors")
	}
	// No competitive card may carry an ambiguity label.
	for _, c := range sec.Cards {
		if c.AmbiguityGroup != "" {
			t.Errorf("competitive card %s carries ambiguityGroup %q", c.ID, c.AmbiguityGroup)
		}
	}
	// The practice pool keeps them, and every excluded card is labelled.
	labelled := 0
	for _, c := range sec.PracticeCards {
		if c.AmbiguityGroup != "" {
			labelled++
		}
	}
	if labelled != sec.AmbiguousExcluded {
		t.Errorf("practice pool labels %d ambiguous cards, want %d", labelled, sec.AmbiguousExcluded)
	}
	// Every practice card must also appear in the competitive pool or be labelled.
	if len(sec.PracticeCards) != len(sec.Cards)+sec.AmbiguousExcluded {
		t.Errorf("practice pool = %d, want competitive %d + ambiguous %d",
			len(sec.PracticeCards), len(sec.Cards), sec.AmbiguousExcluded)
	}
}

func TestRaceRebuildPublicSectionCarriesNoAnswer(t *testing.T) {
	_, out := buildInto(t)
	dir, err := ResolveCurrent(out)
	if err != nil {
		t.Fatalf("resolve current: %v", err)
	}
	pack, err := OpenPack(dir)
	if err != nil {
		t.Fatalf("open pack: %v", err)
	}
	raw, ok := pack.Section(SectionRaceRebuildRaces)
	if !ok {
		t.Skip("race-rebuild is unavailable in this checkout")
	}

	// The finish rank is the answer. It must not appear in the public payload at
	// all, not even under another name: a client that receives it can skip the
	// question .
	for _, forbidden := range []string{"finishRank", "\"rank\"", "tieGroup"} {
		if containsBytes(raw, []byte(forbidden)) {
			t.Errorf("public race-rebuild section leaks %q", forbidden)
		}
	}

	var races []raceRebuildRace
	if err := json.Unmarshal(raw, &races); err != nil {
		t.Fatalf("parse races: %v", err)
	}
	if len(races) == 0 {
		t.Fatal("race-rebuild is available but published no races")
	}
	ids := make(map[string]bool, len(races))
	for _, r := range races {
		if r.Date == "" {
			t.Errorf("race %s has no ISO date", r.RaceID)
		}
		if !isoDateRe.MatchString(r.Date) {
			t.Errorf("race %s has a non-ISO date %q", r.RaceID, r.Date)
		}
		if r.StarterCount != len(r.Starters) {
			t.Errorf("race %s starterCount = %d but starters = %d", r.RaceID, r.StarterCount, len(r.Starters))
		}
		if len(r.Starters) < raceRebuildMinStarters || len(r.Starters) > raceRebuildMaxStarters {
			t.Errorf("race %s has %d starters, want %d-%d",
				r.RaceID, len(r.Starters), raceRebuildMinStarters, raceRebuildMaxStarters)
		}
		// The published starter order is by character id, so array position
		// carries no hint of the result.
		for i := 1; i < len(r.Starters); i++ {
			if r.Starters[i-1].CharacterID >= r.Starters[i].CharacterID {
				t.Errorf("race %s starters are not ordered by character id", r.RaceID)
			}
		}
		for _, st := range r.Starters {
			if st.CharacterID == "" {
				t.Errorf("race %s published a starter without a character id", r.RaceID)
			}
		}
		if ids[r.RaceID] {
			t.Errorf("race %s published twice", r.RaceID)
		}
		ids[r.RaceID] = true
	}

	// The private tree must carry exactly the same race ids, with the ranks.
	secrets, err := OpenSecrets(dir)
	if err != nil {
		t.Fatalf("open secrets: %v", err)
	}
	sraw, ok := secrets.Section(PrivateRaceRebuildAnswers)
	if !ok {
		t.Fatal("race-rebuild.answers missing from the private tree")
	}
	var ans privateRaceAnswers
	if err := json.Unmarshal(sraw, &ans); err != nil {
		t.Fatalf("parse race answers: %v", err)
	}
	if len(ans.Races) != len(races) {
		t.Errorf("private answer count = %d but public race count = %d", len(ans.Races), len(races))
	}
	answerIDs := make(map[string]bool, len(ans.Races))
	for _, a := range ans.Races {
		answerIDs[a.RaceID] = true
		if len(a.Order) == 0 {
			t.Errorf("race %s has an empty answer order", a.RaceID)
		}
		// A rank gap is preserved: the order is by ascending rank, and a dropped
		// starter is recorded rather than renumbered away.
		seen := make(map[string]bool, len(a.Order))
		for _, id := range a.Order {
			if seen[id] {
				t.Errorf("race %s lists %s twice in its answer order", a.RaceID, id)
			}
			seen[id] = true
		}
	}
	for _, r := range races {
		if !answerIDs[r.RaceID] {
			t.Errorf("public race %s has no private answer", r.RaceID)
		}
	}
}

func TestPrivateAnswersAreNotReachableFromPublicPack(t *testing.T) {
	_, out := buildInto(t)
	dir, err := ResolveCurrent(out)
	if err != nil {
		t.Fatalf("resolve current: %v", err)
	}
	pack, err := OpenPack(dir)
	if err != nil {
		t.Fatalf("open pack: %v", err)
	}
	// Every private section name must miss on the public ContentSource, even when
	// asked for by its exact name.
	for _, name := range []string{
		PrivateClassicAnswerPool, PrivateRaceRebuildAnswers, PrivateMemorySolution,
	} {
		if _, ok := pack.Section(name); ok {
			t.Errorf("public pack resolved the private section %q", name)
		}
	}
	// The manifest must list the two trees separately and never both.
	pub := make(map[string]bool, len(pack.Manifest().PublicSections))
	for _, s := range pack.Manifest().PublicSections {
		pub[s] = true
	}
	for _, s := range pack.Manifest().PrivateSections {
		if pub[s] {
			t.Errorf("section %q is listed as both public and private", s)
		}
	}
}

func TestCoRacePathGraphIsConsistent(t *testing.T) {
	_, out := buildInto(t)
	dir, err := ResolveCurrent(out)
	if err != nil {
		t.Fatalf("resolve current: %v", err)
	}
	pack, err := OpenPack(dir)
	if err != nil {
		t.Fatalf("open pack: %v", err)
	}
	raw, ok := pack.Section(SectionCoRacePathGraph)
	if !ok {
		t.Skip("co-race-path is unavailable in this checkout")
	}
	var g coRacePathSection
	if err := json.Unmarshal(raw, &g); err != nil {
		t.Fatalf("parse graph: %v", err)
	}
	nodes := make(map[string]bool, len(g.Nodes))
	for _, n := range g.Nodes {
		nodes[n.CharacterID] = true
	}
	pairs := make(map[string]bool, len(g.Edges))
	for _, e := range g.Edges {
		if !nodes[e.From] || !nodes[e.To] {
			t.Errorf("edge %s->%s references an unknown node", e.From, e.To)
		}
		if e.From >= e.To {
			t.Errorf("edge %s->%s is not in canonical order", e.From, e.To)
		}
		key := e.From + "|" + e.To
		if pairs[key] {
			t.Errorf("duplicate undirected edge %s", key)
		}
		pairs[key] = true
		if e.RaceID == "" {
			t.Errorf("edge %s has no race evidence", key)
		}
	}
	// Components must partition the nodes exactly once.
	seen := make(map[string]int, len(g.Nodes))
	for _, comp := range g.Components {
		for _, id := range comp {
			seen[id]++
		}
	}
	for id := range nodes {
		if seen[id] != 1 {
			t.Errorf("node %s appears in %d components, want exactly 1", id, seen[id])
		}
	}
}

func TestSortCardsCarryExplicitEventRegion(t *testing.T) {
	_, out := buildInto(t)
	dir, err := ResolveCurrent(out)
	if err != nil {
		t.Fatalf("resolve current: %v", err)
	}
	pack, err := OpenPack(dir)
	if err != nil {
		t.Fatalf("open pack: %v", err)
	}
	raw, ok := pack.Section(SectionSortCards)
	if !ok {
		t.Skip("sort is unavailable in this checkout")
	}
	var sec sortSection
	if err := json.Unmarshal(raw, &sec); err != nil {
		t.Fatalf("parse sort section: %v", err)
	}
	if len(sec.Cards) == 0 {
		t.Fatal("sort is available but published no cards")
	}
	for _, c := range sec.Cards {
		if c.EventDate == nil {
			continue
		}
		// An event date without an explicit region would silently mix regions
		// .
		if c.EventDate.Region == "" {
			t.Errorf("card %s has an event date with no region", c.CharacterID)
		}
		if !isoDateRe.MatchString(c.EventDate.Date) {
			t.Errorf("card %s has a non-ISO event date %q", c.CharacterID, c.EventDate.Date)
		}
	}
}

func TestDraftPricesFollowTheTrialFormula(t *testing.T) {
	_, out := buildInto(t)
	dir, err := ResolveCurrent(out)
	if err != nil {
		t.Fatalf("resolve current: %v", err)
	}
	pack, err := OpenPack(dir)
	if err != nil {
		t.Fatalf("open pack: %v", err)
	}
	raw, ok := pack.Section(SectionDraftMarket)
	if !ok {
		t.Skip("draft is unavailable in this checkout")
	}
	var sec draftSection
	if err := json.Unmarshal(raw, &sec); err != nil {
		t.Fatalf("parse draft section: %v", err)
	}
	if sec.Budget != draftBudget || sec.Roster != draftRosterSize {
		t.Errorf("budget/roster = %d/%d, want %d/%d", sec.Budget, sec.Roster, draftBudget, draftRosterSize)
	}
	for _, c := range sec.Cards {
		distA, styleA, dirtA := 0, 0, 0
		for _, g := range c.Aptitudes.Distance {
			if g == "A" {
				distA++
			}
		}
		for _, g := range c.Aptitudes.Style {
			if g == "A" {
				styleA++
			}
		}
		if c.Aptitudes.Surface["泥地"] == "A" {
			dirtA = 1
		}
		want := 3 + ceilDiv(distA, 2) + ceilDiv(styleA, 2) + dirtA
		if c.Price != want {
			t.Errorf("card %s price = %d, want %d from the published formula", c.CharacterID, c.Price, want)
		}
	}
}

func TestEvidenceDocumentsSectionSchemas(t *testing.T) {
	res, _ := buildInto(t)
	ev := BuildEvidence(res, 100*time.Millisecond)
	if ev.Kind == "" {
		t.Error("evidence has no kind")
	}
	if len(ev.SectionSchemas) == 0 {
		t.Fatal("evidence documents no section schemas")
	}
	documented := make(map[string]bool, len(ev.SectionSchemas))
	for _, s := range ev.SectionSchemas {
		documented[s.Section] = true
		// The public name index is shared infrastructure rather than one mode's
		// payload, so it legitimately has no owning mode.
		if s.Mode == "" && s.Section != PublicIndexSection {
			t.Errorf("section %q has no owning mode", s.Section)
		}
		if s.Kind == "" {
			t.Errorf("section %q has no JSON kind", s.Section)
		}
	}
	// The seven primary sections must all be documented, so an integration check
	// can compare the mode parser against this list.
	for _, name := range []string{
		SectionClassicCharacters, SectionAptitudeGridCards, SectionDraftMarket,
		SectionRaceRebuildRaces, SectionCoRacePathGraph, SectionSortCards, SectionMemoryPairs,
	} {
		if !documented[name] {
			t.Errorf("section %q is not documented in the evidence schema list", name)
		}
	}
	// Every mode's evidence row must agree with its admission.
	for _, m := range ev.Modes {
		adm, ok := findAdmission(res.Admissions, m.Mode)
		if !ok {
			t.Fatalf("no admission for %s", m.Mode)
		}
		if m.Available != adm.Available || m.QuestionCount != adm.QuestionCount {
			t.Errorf("mode %s evidence disagrees with its admission", m.Mode)
		}
	}
}

func TestNormalizeDate(t *testing.T) {
	cases := []struct{ in, want string }{
		{"1997年11月29日", "1997-11-29"},
		{"1998年1月6日", "1998-01-06"},
		{"1995/05/02", "1995-05-02"},
		{"2026-11-08", "2026-11-08"},
		{"", ""},
		{"not a date", ""},
		{"1998年13月1日", ""},
		{"1998年1月32日", ""},
	}
	for _, c := range cases {
		if got := NormalizeDate(c.in); got != c.want {
			t.Errorf("NormalizeDate(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestRegionForCourseDoesNotAssumeJapan(t *testing.T) {
	if got := RegionForCourse("阪神"); got != "JP" {
		t.Errorf("RegionForCourse(阪神) = %q, want JP", got)
	}
	for _, foreign := range []string{"香港", "法国", "英国", "美国", "アイルランド"} {
		if got := RegionForCourse(foreign); got != "" {
			t.Errorf("RegionForCourse(%q) = %q, want empty for a non-Japanese course", foreign, got)
		}
	}
	if got := RegionForCourse(""); got != "" {
		t.Errorf("RegionForCourse(\"\") = %q, want empty", got)
	}
}

func TestLegacyGroupsMissingDetectsEveryField(t *testing.T) {
	star := 3
	full := LegacyGroups{
		Star:              &star,
		SurfaceGroup:      []string{"草地"},
		DistanceGroup:     []string{"中"},
		RunningStyleGroup: []string{"先"},
		SexType:           "牡", G1Bracket: "3-4", G23Bracket: "4-6",
		SchoolGrade: "中等部", Dormitory: "栗东宿舍",
	}
	if missing := full.Missing(); len(missing) != 0 {
		t.Errorf("complete vector reports missing %v", missing)
	}
	empty := LegacyGroups{}
	if missing := empty.Missing(); len(missing) != 9 {
		t.Errorf("empty vector reports %d missing fields, want 9: %v", len(missing), missing)
	}
}

func TestAptitudesCompleteRequiresAllTen(t *testing.T) {
	full := Aptitudes{
		Surface:      map[string]string{"turf": "A", "dirt": "G"},
		Distance:     map[string]string{"sprint": "F", "mile": "C", "medium": "A", "long": "A"},
		RunningStyle: map[string]string{"front": "G", "pace": "A", "late": "A", "end": "C"},
	}
	if !full.Complete() {
		t.Error("a fully graded set reports incomplete")
	}
	if full.CountA("distance") != 2 {
		t.Errorf("distance A count = %d, want 2", full.CountA("distance"))
	}
	partial := Aptitudes{
		Surface:      map[string]string{"turf": "A", "dirt": "G"},
		Distance:     map[string]string{"sprint": "F"},
		RunningStyle: map[string]string{"front": "G"},
	}
	if partial.Complete() {
		t.Error("a partial set reports complete")
	}
	bad := Aptitudes{
		Surface:      map[string]string{"turf": "A", "dirt": "Z"},
		Distance:     map[string]string{"sprint": "F", "mile": "C", "medium": "A", "long": "A"},
		RunningStyle: map[string]string{"front": "G", "pace": "A", "late": "A", "end": "C"},
	}
	if bad.Complete() {
		t.Error("an out-of-range grade reports complete")
	}
}

func TestPublicIndexHasNoAnswerVector(t *testing.T) {
	res, _ := buildInto(t)
	if len(res.Report.PublicIndex) == 0 {
		t.Fatal("public index is empty")
	}
	// Serialise an index row and assert no comparison field leaked into it.
	b, err := json.Marshal(res.Report.PublicIndex[0])
	if err != nil {
		t.Fatalf("marshal index row: %v", err)
	}
	for _, forbidden := range []string{"g1Bracket", "g23Bracket", "surfaceGroup", "distanceGroup", "runningStyleGroup"} {
		if containsBytes(b, []byte(forbidden)) {
			t.Errorf("public index row leaks the comparison field %q", forbidden)
		}
	}
}

func TestResolveCurrentRejectsMissingVersion(t *testing.T) {
	out := t.TempDir()
	if _, err := ResolveCurrent(out); err == nil {
		t.Fatal("ResolveCurrent succeeded with no pointer")
	}
	if err := os.WriteFile(filepath.Join(out, CurrentFileName), []byte("does-not-exist\n"), 0o644); err != nil {
		t.Fatalf("write pointer: %v", err)
	}
	if _, err := ResolveCurrent(out); err == nil {
		t.Fatal("ResolveCurrent accepted a pointer to a missing version")
	}
}

func TestOpenPackRejectsUnknownSchemaVersion(t *testing.T) {
	_, out := buildInto(t)
	dir, err := ResolveCurrent(out)
	if err != nil {
		t.Fatalf("resolve current: %v", err)
	}
	manifestPath := filepath.Join(dir, ManifestName)
	mb, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatalf("read manifest: %v", err)
	}
	var m Manifest
	if err := json.Unmarshal(mb, &m); err != nil {
		t.Fatalf("parse manifest: %v", err)
	}
	m.SchemaVersion = "99.0.0"
	// Recompute the checksum so only the schema check can fail.
	sum, err := computeManifestChecksum(&m)
	if err != nil {
		t.Fatalf("recompute checksum: %v", err)
	}
	m.SHA256 = sum
	b, err := json.Marshal(&m)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if err := os.WriteFile(manifestPath, b, 0o644); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
	if _, err := OpenPack(dir); err == nil {
		t.Fatal("OpenPack accepted an unsupported schema version")
	}
}

func findAdmission(list []ModeAdmission, mode string) (ModeAdmission, bool) {
	for _, a := range list {
		if a.Mode == mode {
			return a, true
		}
	}
	return ModeAdmission{}, false
}

func containsBytes(haystack, needle []byte) bool {
	if len(needle) == 0 {
		return false
	}
	for i := 0; i+len(needle) <= len(haystack); i++ {
		match := true
		for j := range needle {
			if haystack[i+j] != needle[j] {
				match = false
				break
			}
		}
		if match {
			return true
		}
	}
	return false
}

func TestDocumentedSectionSchemasMatchPublishedPayloads(t *testing.T) {
	res, out := buildInto(t)
	dir, err := ResolveCurrent(out)
	if err != nil {
		t.Fatalf("resolve current: %v", err)
	}
	pack, err := OpenPack(dir)
	if err != nil {
		t.Fatalf("open pack: %v", err)
	}

	// The evidence documents each section's fields so the mode packages can be
	// checked against it during integration. This test is the guarantee that the
	// documentation matches the bytes actually published.
	for _, schema := range SectionSchemas() {
		raw, ok := pack.Section(schema.Section)
		if !ok {
			// An unavailable mode publishes nothing; its schema is still documented
			// for the day the data gap closes.
			continue
		}

		switch schema.Kind {
		case "object":
			var obj map[string]json.RawMessage
			if err := json.Unmarshal(raw, &obj); err != nil {
				t.Fatalf("%s: not a JSON object: %v", schema.Section, err)
			}
			for _, field := range schema.ObjectFields {
				if _, ok := obj[field]; !ok {
					t.Errorf("%s: documented object field %q is absent from the payload", schema.Section, field)
				}
			}
			for field := range obj {
				if !containsString(schema.ObjectFields, field) {
					t.Errorf("%s: payload carries undocumented object field %q", schema.Section, field)
				}
			}
		case "array":
			var arr []map[string]json.RawMessage
			if err := json.Unmarshal(raw, &arr); err != nil {
				t.Fatalf("%s: not a JSON array: %v", schema.Section, err)
			}
			if len(arr) == 0 {
				continue
			}
			for _, field := range schema.ItemFields {
				if _, ok := arr[0][field]; !ok {
					t.Errorf("%s: documented item field %q is absent from the payload", schema.Section, field)
				}
			}
			for field := range arr[0] {
				if !containsString(schema.ItemFields, field) {
					t.Errorf("%s: payload carries undocumented item field %q", schema.Section, field)
				}
			}
		default:
			t.Errorf("%s: unknown documented kind %q", schema.Section, schema.Kind)
		}
	}

	// Every published section must be documented, so nothing is published that a
	// consumer cannot look up.
	documented := make(map[string]bool)
	for _, s := range SectionSchemas() {
		documented[s.Section] = true
	}
	for _, name := range pack.SectionNames() {
		if !documented[name] {
			t.Errorf("section %q is published but not documented in the evidence schemas", name)
		}
	}
	_ = res
}

func TestMemoryAdmittedFromCanonicalAvatars(t *testing.T) {
	res, out := buildInto(t)
	adm, ok := findAdmission(res.Admissions, ModeMemory)
	if !ok {
		t.Fatal("memory admission missing")
	}

	// Verified canonical avatars must admit enough pairs for memory play.
	if !adm.Available {
		t.Fatalf("memory is unavailable: %s", adm.Reason)
	}
	if adm.QuestionCount < memoryMinPairs {
		t.Errorf("memory pair pool = %d, want at least %d", adm.QuestionCount, memoryMinPairs)
	}
	if adm.Section != SectionMemoryPairs {
		t.Errorf("memory section = %q, want %q", adm.Section, SectionMemoryPairs)
	}

	// Report canonical and legacy manifest sources separately.
	as := res.Report.Assets
	if as.VerifiedOnDisk < memoryMinPairs {
		t.Errorf("verifiedOnDisk = %d, want at least %d", as.VerifiedOnDisk, memoryMinPairs)
	}
	if as.Source != string(AvatarSourceCanonical) {
		t.Errorf("asset source = %q, want %q", as.Source, AvatarSourceCanonical)
	}
	if as.StagingVerified == 0 {
		t.Error("stagingVerified is zero while memory is available")
	}
	if as.UniqueImages < as.VerifiedOnDisk {
		t.Errorf("uniqueImages = %d is smaller than verifiedOnDisk = %d", as.UniqueImages, as.VerifiedOnDisk)
	}
	if as.CopyrightNotice == "" {
		t.Error("asset summary carries no copyright notice")
	}

	// Publish avatar bytes inside the pack so runtime serving is self-contained.
	dir, err := ResolveCurrent(out)
	if err != nil {
		t.Fatalf("resolve current: %v", err)
	}
	copied := 0
	for _, f := range res.Manifest.Files {
		if containsString([]string{PackAvatarDir}, filepath.Dir(strings.TrimPrefix(f.Path, PublicDirName+"/"))) {
			copied++
		}
	}
	if copied == 0 {
		t.Error("no avatar file was copied into the pack")
	}
	// Every declared avatar file must really exist on disk.
	for _, f := range res.Manifest.Files {
		if !strings.Contains(f.Path, PackAvatarDir) {
			continue
		}
		if _, err := os.Stat(filepath.Join(dir, filepath.FromSlash(f.Path))); err != nil {
			t.Errorf("declared avatar %s is missing: %v", f.Path, err)
		}
	}
}

func TestMemoryPairsMatchTheModeContract(t *testing.T) {
	res, out := buildInto(t)
	adm, ok := findAdmission(res.Admissions, ModeMemory)
	if !ok {
		t.Fatal("memory admission missing")
	}
	if !adm.Available {
		t.Skipf("memory is unavailable: %s", adm.Reason)
	}

	dir, err := ResolveCurrent(out)
	if err != nil {
		t.Fatalf("resolve current: %v", err)
	}
	pack, err := OpenPack(dir)
	if err != nil {
		t.Fatalf("open pack: %v", err)
	}
	raw, ok := pack.Section(SectionMemoryPairs)
	if !ok {
		t.Fatal("memory.pairs does not resolve")
	}

	// The memory mode reads {pairId, imagePath, label}. All three must be present
	// with exactly those names, otherwise its loadPairs skips every record and the
	// mode reports 0 pairs.
	var recs []map[string]json.RawMessage
	if err := json.Unmarshal(raw, &recs); err != nil {
		t.Fatalf("memory.pairs is not an array: %v", err)
	}
	if len(recs) != adm.QuestionCount {
		t.Errorf("memory.pairs has %d records but the admission says %d", len(recs), adm.QuestionCount)
	}
	seenPairs := make(map[string]bool, len(recs))
	for i, rec := range recs {
		for _, field := range []string{"pairId", "imagePath", "label"} {
			if _, ok := rec[field]; !ok {
				t.Fatalf("record %d is missing the contract field %q", i, field)
			}
		}
		var pairID, imagePath, label string
		json.Unmarshal(rec["pairId"], &pairID)
		json.Unmarshal(rec["imagePath"], &imagePath)
		json.Unmarshal(rec["label"], &label)
		if pairID == "" {
			t.Errorf("record %d has an empty pairId", i)
		}
		if seenPairs[pairID] {
			t.Errorf("duplicate pairId %q", pairID)
		}
		seenPairs[pairID] = true
		if imagePath == "" {
			t.Errorf("pair %s has an empty imagePath", pairID)
		}
		if label == "" {
			t.Errorf("pair %s has an empty label", pairID)
		}

		// The published route must resolve to a file that really exists in the
		// pack. A route that 404s would make the board unplayable.
		rel := strings.TrimPrefix(imagePath, "/")
		full := filepath.Join(dir, PublicDirName, filepath.FromSlash(rel))
		info, statErr := os.Stat(full)
		if statErr != nil {
			t.Errorf("pair %s imagePath %q does not resolve to a file: %v", pairID, imagePath, statErr)
			continue
		}
		if info.Size() == 0 {
			t.Errorf("pair %s image file is empty", pairID)
		}
		// The route must live under the pack's public tree exactly as published:
		// the prefix must be the public-relative directory, not a made-up namespace.
		if !strings.HasPrefix(imagePath, "/"+PackMemoryPairDir+"/") {
			t.Errorf("pair %s imagePath %q is not under /%s/", pairID, imagePath, PackMemoryPairDir)
		}
	}
}

func TestEveryPublishedImagePathResolves(t *testing.T) {
	_, out := buildInto(t)
	dir, err := ResolveCurrent(out)
	if err != nil {
		t.Fatalf("resolve current: %v", err)
	}
	pack, err := OpenPack(dir)
	if err != nil {
		t.Fatalf("open pack: %v", err)
	}

	// Walk every published section and confirm each imagePath / image route points
	// at a real file. This is the check that keeps a section from advertising an
	// asset the pack does not contain.
	checked := 0
	for _, name := range pack.SectionNames() {
		raw, _ := pack.Section(name)
		var doc any
		if err := json.Unmarshal(raw, &doc); err != nil {
			continue
		}
		var walk func(v any)
		walk = func(v any) {
			switch node := v.(type) {
			case map[string]any:
				for k, child := range node {
					if k == "imagePath" {
						if s, ok := child.(string); ok && s != "" {
							checked++
							rel := strings.TrimPrefix(s, "/")
							if _, err := os.Stat(filepath.Join(dir, PublicDirName, filepath.FromSlash(rel))); err != nil {
								t.Errorf("section %s publishes imagePath %q with no file: %v", name, s, err)
							}
						}
						continue
					}
					walk(child)
				}
			case []any:
				for _, child := range node {
					walk(child)
				}
			}
		}
		walk(doc)
	}
	if checked == 0 {
		t.Log("no imagePath was published in this pack")
	}
}

func TestBothAvatarSourcesAreReported(t *testing.T) {
	res, _ := buildInto(t)
	as := res.Report.Assets

	// The legacy manifest declares files that are absent; the report must say so
	// rather than presenting a single "assets ok" figure.
	if as.ManifestEntries == 0 {
		t.Error("legacy manifest entry count is zero; the source was not read")
	}
	if as.ManifestMissingOnDisk == 0 {
		t.Logf("note: this checkout has %d of %d legacy manifest paths on disk", as.ManifestEntries-as.ManifestMissingOnDisk, as.ManifestEntries)
	}
	// The two sets are separate figures, never merged into one.
	if as.StagingDeclared == 0 {
		t.Error("staging avatar count is zero; the second source was not read")
	}
	if as.StagingVerified > as.StagingDeclared {
		t.Errorf("stagingVerified = %d exceeds stagingDeclared = %d", as.StagingVerified, as.StagingDeclared)
	}
	// The gap entry must describe both sets.
	var found bool
	for _, g := range res.Report.Gaps {
		if g.Area != "assets" {
			continue
		}
		found = true
		if !strings.Contains(g.Detail, "TWO INDEPENDENT AVATAR SETS") {
			t.Errorf("assets gap does not explain that two sets exist: %s", g.Detail)
		}
		if !strings.Contains(g.Measured, fmt.Sprintf("canonical avatar directory declares %d avatars of which %d verified", as.StagingDeclared, as.StagingVerified)) {
			t.Errorf("assets gap does not report the canonical avatar counts: %s", g.Measured)
		}
	}
	if !found {
		t.Error("quality report has no assets gap entry")
	}
}

func TestCopiedAssetCountMatchesDisk(t *testing.T) {
	res, out := buildInto(t)
	dir, err := ResolveCurrent(out)
	if err != nil {
		t.Fatalf("resolve current: %v", err)
	}

	// Count the image files really written into the pack's public tree.
	actual := 0
	for _, sub := range []string{PackAvatarDir, PackMemoryPairDir} {
		entries, err := os.ReadDir(filepath.Join(dir, PublicDirName, filepath.FromSlash(sub)))
		if err != nil {
			continue
		}
		for _, e := range entries {
			// Count whatever extension the publisher actually wrote: WebP when the
			// encoder is available, PNG otherwise. Hard-coding ".png" here made the
			// test fail for a reason unrelated to the behaviour under test.
			if e.IsDir() {
				continue
			}
			if ext := strings.ToLower(filepath.Ext(e.Name())); ext == ".png" || ext == ".webp" {
				actual++
			}
		}
	}

	// The manifest and the quality report must both agree with the filesystem. A
	// zero here used to mean the summary was snapshotted before the copy ran.
	if res.Manifest.Assets.CopiedIntoPack != actual {
		t.Errorf("manifest copiedIntoPack = %d but %d image files exist", res.Manifest.Assets.CopiedIntoPack, actual)
	}
	if res.Report.Assets.CopiedIntoPack != actual {
		t.Errorf("report copiedIntoPack = %d but %d image files exist", res.Report.Assets.CopiedIntoPack, actual)
	}
	if actual == 0 {
		t.Skip("no image files in this pack")
	}

	// Every declared image file must appear in the manifest's file list, so a
	// server can verify what it serves.
	declared := make(map[string]bool, len(res.Manifest.Files))
	for _, f := range res.Manifest.Files {
		declared[f.Path] = true
	}
	for _, sub := range []string{PackAvatarDir, PackMemoryPairDir} {
		entries, err := os.ReadDir(filepath.Join(dir, PublicDirName, filepath.FromSlash(sub)))
		if err != nil {
			continue
		}
		for _, e := range entries {
			if e.IsDir() {
				continue
			}
			want := filepath.ToSlash(filepath.Join(PublicDirName, sub, e.Name()))
			if !declared[want] {
				t.Errorf("image file %s is not declared in the manifest", want)
			}
		}
	}
}

func TestMemoryImageRouteMatchesFileLocation(t *testing.T) {
	// The published route must be derived from the same directory constant as the
	// file, otherwise a client gets a 404. This test pins that relationship.
	if got, want := PackMemoryPairRoute("mp-abc"), "/"+PackMemoryPairDir+"/mp-abc"+PackImageExt(); got != want {
		t.Errorf("PackMemoryPairRoute = %q, want %q", got, want)
	}
	if got, want := PackAvatarRoute("1001"), "/"+PackAvatarDir+"/1001"+PackImageExt(); got != want {
		t.Errorf("PackAvatarRoute = %q, want %q", got, want)
	}
	// A route must never point at a namespace that is not the public tree.
	if strings.HasPrefix(PackMemoryPairRoute("x"), "/content-assets") {
		t.Error("the pair route uses a namespace that does not match the public tree")
	}
}

func TestImageDependentModesExcludeCharactersWithoutAvatar(t *testing.T) {
	res, out := buildInto(t)
	dir, err := ResolveCurrent(out)
	if err != nil {
		t.Fatalf("resolve current: %v", err)
	}
	pack, err := OpenPack(dir)
	if err != nil {
		t.Fatalf("open pack: %v", err)
	}

	// Every mode that renders a card face must publish an image for every item.
	// A null or missing imagePath would be a blank card in the UI, so the compiler
	// must have excluded such a character rather than publishing it .
	imageModes := []struct {
		section string
		mode    string
	}{
		{SectionClassicCharacters, ModeClassic},
		{SectionSortCards, ModeSort},
		{SectionDraftMarket, ModeDraft},
		{SectionAptitudeGridCards, ModeAptitudeGrid},
	}
	for _, im := range imageModes {
		raw, ok := pack.Section(im.section)
		if !ok {
			continue
		}
		var doc struct {
			Cards []struct {
				ID        string  `json:"id"`
				CharID    string  `json:"characterId"`
				NameZh    string  `json:"nameZh"`
				Name      string  `json:"name"`
				ImagePath *string `json:"imagePath"`
			} `json:"cards"`
		}
		if err := json.Unmarshal(raw, &doc); err != nil {
			t.Fatalf("%s: parse: %v", im.section, err)
		}
		for _, c := range doc.Cards {
			who := c.ID + c.CharID + c.NameZh + c.Name
			if c.ImagePath == nil {
				t.Errorf("%s: item %s has no imagePath; an avatar-less character was published into an image mode", im.section, who)
				continue
			}
			if *c.ImagePath == "" {
				t.Errorf("%s: item %s has an empty imagePath", im.section, who)
				continue
			}
			// The route must resolve to a real file in the pack.
			rel := strings.TrimPrefix(*c.ImagePath, "/")
			if _, err := os.Stat(filepath.Join(dir, PublicDirName, filepath.FromSlash(rel))); err != nil {
				t.Errorf("%s: item %s imagePath %q does not resolve: %v", im.section, who, *c.ImagePath, err)
			}
		}
	}

	// The exclusion must be a real rule, not a coincidence: the characters that
	// lack an avatar must not appear in any image mode's pool.
	noAvatar := make(map[string]bool)
	for i := range res.Report.PublicIndex {
		e := res.Report.PublicIndex[i]
		if e.ImagePath == nil {
			noAvatar[e.ID] = true
		}
	}
	if len(noAvatar) == 0 {
		t.Log("every indexed character has an avatar in this checkout, so the exclusion rule has nothing to drop")
	}
	for _, im := range imageModes {
		raw, ok := pack.Section(im.section)
		if !ok {
			continue
		}
		for id := range noAvatar {
			if containsBytes(raw, []byte(`"`+id+`"`)) {
				t.Errorf("%s: character %s has no avatar but appears in the pool", im.section, id)
			}
		}
	}
}

func TestMemoryPublicLabelCarriesNoCharacterName(t *testing.T) {
	_, out := buildInto(t)
	dir, err := ResolveCurrent(out)
	if err != nil {
		t.Fatalf("resolve current: %v", err)
	}
	pack, err := OpenPack(dir)
	if err != nil {
		t.Fatalf("open pack: %v", err)
	}
	raw, ok := pack.Section(SectionMemoryPairs)
	if !ok {
		t.Skip("memory is unavailable in this checkout")
	}
	var pairs []memoryPair
	if err := json.Unmarshal(raw, &pairs); err != nil {
		t.Fatalf("parse pairs: %v", err)
	}
	if len(pairs) == 0 {
		t.Fatal("memory.pairs is empty")
	}
	for _, p := range pairs {
		// The public label must be the meaningless placeholder, never a name. The
		// mode tests memory, not recognition .
		if p.Label != MemoryLabelPlaceholder {
			t.Errorf("pair %s publishes label %q, want the placeholder %q", p.PairID, p.Label, MemoryLabelPlaceholder)
		}
	}

	// No character name may appear anywhere in the public payload.
	root := fixtureRoot(t)
	in, err := Load(DefaultSourcePaths(root))
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	for i := range in.Wiki.Characters {
		c := &in.Wiki.Characters[i]
		for _, name := range []string{c.Names.ZhHans, c.Names.Ja, c.Names.En} {
			if len(name) >= 3 && containsBytes(raw, []byte(name)) {
				t.Errorf("public memory section contains the character name %q", name)
			}
		}
	}

	// The readable names must be in the private solution, keyed by pair id.
	secrets, err := OpenSecrets(dir)
	if err != nil {
		t.Fatalf("open secrets: %v", err)
	}
	sraw, ok := secrets.Section(PrivateMemorySolution)
	if !ok {
		t.Fatal("memory.solution missing")
	}
	var sol privateMemorySolution
	if err := json.Unmarshal(sraw, &sol); err != nil {
		t.Fatalf("parse solution: %v", err)
	}
	if len(sol.PairLabels) != len(pairs) {
		t.Errorf("private pairLabels has %d entries but %d pairs were published", len(sol.PairLabels), len(pairs))
	}
	for _, p := range pairs {
		name, ok := sol.PairLabels[p.PairID]
		if !ok || name == "" {
			t.Errorf("pair %s has no readable label in the private solution", p.PairID)
		}
	}
}
