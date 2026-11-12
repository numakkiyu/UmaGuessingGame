package content

import (
	"fmt"
	"github.com/bhcn/umaguessinggame/internal/characternames"
	"sort"
)

// CopyrightNotice is the attribution a site must display for the game assets.
// The avatars are Cygames material sourced through biliwiki, which itself states
// the data is quoted for reference only.
const CopyrightNotice = "素材版权归 Cygames 所有，本项目仅作非商业用途展示；游戏数据或信息的著作权属于 Cygames，仅以介绍为目的引用。"

// QualityReport is the honest field-quality record. Every documented gap in
// must appear here with its recomputed figure: the report exists so that a
// whole-library "PASS" cannot hide a per-field shortfall .
type QualityReport struct {
	SchemaVersion  string       `json:"schemaVersion"`
	DatasetVersion string       `json:"datasetVersion"`
	GeneratedAt    string       `json:"generatedAt"`
	Locale         string       `json:"locale"`
	Region         string       `json:"region"`
	ReviewStatus   ReviewStatus `json:"reviewStatus"`

	// Sources are the input snapshots this report was computed from.
	Sources []SourceSnapshot `json:"sources"`

	// Recomputed totals. These are counted from the loaded data, never copied
	// from a snapshot's self-reported meta.
	EntityCounts EntityCounts `json:"entityCounts"`

	// SelfReported is what the snapshot claims about itself, kept so a reader can
	// see claim vs recomputed figure side by side.
	SelfReported map[string]int `json:"selfReportedCounts,omitempty"`

	// Joins records how the artifacts actually linked up.
	Joins JoinReport `json:"joins"`

	// Assets is the verified media state.
	Assets AssetSummary `json:"assets"`

	// Modes is the per-mode admission verdict, including unavailable reasons.
	Modes []ModeAdmission `json:"modes"`

	// Gaps is the explicit list of known data gaps with their measured values.
	// This is the field the requirement forbids suppressing.
	Gaps []DataGap `json:"dataGaps"`

	// Playability explains the playable/blocked/NPC split.
	Playability PlayabilityReport `json:"playability"`

	// Limitations lists what this build does not claim.
	Limitations []string `json:"limitations"`

	// PublicSections / PrivateSections name the tree contents.
	PublicSections  []string `json:"publicSections"`
	PrivateSections []string `json:"privateSections"`

	// PublicIndex is the servable name index projection.
	PublicIndex []PublicIndexEntry `json:"-"`
}

// JoinReport records artifact join success rates.
type JoinReport struct {
	ReadyEntries           int      `json:"readyEntries"`
	BlockedEntries         int      `json:"blockedEntries"`
	NormalizedFiles        int      `json:"normalizedFiles"`
	WikiRecords            int      `json:"wikiRecords"`
	ReadyMatchedWiki       int      `json:"readyMatchedWiki"`
	BlockedMatchedWiki     int      `json:"blockedMatchedWiki"`
	NormalizedMatchedWiki  int      `json:"normalizedMatchedWiki"`
	ReadyMatchedNormalized int      `json:"readyMatchedNormalized"`
	LegacyMapped           int      `json:"legacyMapped"`
	ReadyUnmatched         []string `json:"readyUnmatched,omitempty"`
	BlockedUnmatched       []string `json:"blockedUnmatched,omitempty"`
	NormalizedUnmatched    []string `json:"normalizedUnmatched,omitempty"`
	WikiNotInAnyBank       []string `json:"wikiNotInAnyBank,omitempty"`
}

// PlayabilityReport explains the entity split. The raw snapshot's 147 records
// include non-playable cast, so implementation.status is not playability
// .
type PlayabilityReport struct {
	Total            int            `json:"total"`
	Playable         int            `json:"playable"`
	Planned          int            `json:"planned"`
	NPC              int            `json:"npc"`
	UnknownKind      int            `json:"unknownKind"`
	Ready            int            `json:"ready"`
	Blocked          int            `json:"blocked"`
	NotPlayable      int            `json:"notPlayable"`
	InferredNames    int            `json:"inferredNames"`
	MissingFieldHist map[string]int `json:"missingFieldHistogram,omitempty"`
	BlockedReasons   []ReasonCount  `json:"blockedReasons,omitempty"`
}

// DataGap is one measured data gap. Severity tells a reader whether a mode is
// blocked by it.
type DataGap struct {
	ID       string `json:"id"`
	Area     string `json:"area"`
	Severity string `json:"severity"`
	// Measured is the real recomputed figure.
	Measured string `json:"measured"`
	// Expected is the documented target or total, when one exists.
	Expected string `json:"expected,omitempty"`
	// Impact names the modes affected.
	Impact []string `json:"impact,omitempty"`
	Detail string   `json:"detail"`
}

// PublicIndexEntry is one servable name-index row. It carries identity and
// search text only: no nine-field answer vector, no secret fact.
type PublicIndexEntry struct {
	SearchNames    *characternames.Names `json:"searchNames,omitempty"`
	LocalizedNames map[string]string     `json:"localizedNames,omitempty"`
	ID             string                `json:"id"`
	LegacyID       string                `json:"legacyId,omitempty"`
	NameZh         string                `json:"nameZh"`
	NameJp         string                `json:"nameJp"`
	NameEn         string                `json:"nameEn,omitempty"`
	Aliases        []string              `json:"aliases,omitempty"`
	ImagePath      *string               `json:"imagePath"`
	// RemoteImageURL is the upstream CDN URL, empty when the snapshot has none.
	// A client prefers it and falls back to ImagePath so an offline/LAN event still
	// works .
	RemoteImageURL string `json:"remoteImageUrl,omitempty"`
	Playable       bool   `json:"playable"`
}

// buildQualityReport assembles the report from the loaded inputs and compiled
// dataset. Every figure is counted here; none is passed in from a claim.
func buildQualityReport(ds *Dataset, in *RawInputs, joins *joinStats, ast assetVerification, admissions []ModeAdmission) *QualityReport {
	r := &QualityReport{
		SchemaVersion:  SchemaVersion,
		DatasetVersion: ds.DatasetVersion,
		GeneratedAt:    ds.GeneratedAt,
		Locale:         ds.Locale,
		Region:         ds.Region,
		ReviewStatus:   ReviewPending,
		Sources:        ds.Sources,
		SelfReported:   in.SelfReportedCounts,
		Modes:          admissions,
	}

	// Entity counts, recomputed.
	ec := EntityCounts{Characters: len(ds.Characters), Outfits: len(ds.Outfits)}
	pr := PlayabilityReport{Total: len(ds.Characters), MissingFieldHist: map[string]int{}}
	blockedReasons := RejectTally{}
	for i := range ds.Characters {
		c := &ds.Characters[i]
		switch c.EntityKind {
		case EntityPlayable:
			pr.Playable++
		case EntityPlanned:
			pr.Planned++
		case EntityNPC:
			pr.NPC++
		default:
			pr.UnknownKind++
		}
		switch c.PlayableStatus {
		case PlayableReady:
			pr.Ready++
			ec.ClassicReady++
		case PlayableBlocked:
			pr.Blocked++
			ec.ClassicBlocked++
			for _, f := range c.MissingFields {
				pr.MissingFieldHist[f]++
			}
			if c.BlockReason != "" {
				blockedReasons.add(RejectReason(firstToken(c.BlockReason)))
			}
		case PlayableNotPlayable:
			pr.NotPlayable++
		}
		if c.Names.JaConfidence == ConfidenceInferred {
			pr.InferredNames++
		}
		ec.Goals += len(c.Goals)
		ec.Secrets += len(c.Secrets)
		ec.Aliases += len(c.Aliases)
		if c.Prototype != nil {
			ec.PrototypeHorses++
			if c.Prototype.HasNKBID() {
				ec.PrototypeWithNKB++
			}
		}
	}
	pr.BlockedReasons = blockedReasons.sorted()
	ec.CharactersNPC = pr.NPC
	ec.CharactersPlanned = pr.Planned
	ec.CharactersPlayable = pr.Playable
	ec.Races = len(ds.Races)
	for _, race := range ds.Races {
		ec.RaceEntries += len(race.Entries)
	}
	ec.Voices = countVoices(in)
	r.EntityCounts = ec
	r.Playability = pr

	// Join report.
	r.Joins = JoinReport{
		ReadyEntries:           joins.ReadyEntries,
		BlockedEntries:         joins.BlockedEntries,
		NormalizedFiles:        joins.NormalizedFiles,
		WikiRecords:            joins.WikiRecords,
		ReadyMatchedWiki:       joins.ReadyMatchedWiki,
		BlockedMatchedWiki:     joins.BlockedMatchedWiki,
		NormalizedMatchedWiki:  joins.NormalizedMatchedWiki,
		ReadyMatchedNormalized: joins.ReadyMatchedNormalized,
		LegacyMapped:           joins.LegacyMapped,
		ReadyUnmatched:         joins.ReadyUnmatched,
		BlockedUnmatched:       joins.BlockedUnmatched,
		NormalizedUnmatched:    joins.NormalizedUnmatched,
		WikiNotInAnyBank:       joins.WikiNotInAnyBank,
	}

	// Asset verification, reported per source so the two sets cannot be confused.
	uniqueDigests := make(map[string]bool, len(ast.byCharacter))
	for _, av := range ast.byCharacter {
		uniqueDigests[av.SHA256] = true
	}
	source := ""
	if ast.stagingVerified > 0 {
		source = string(AvatarSourceCanonical)
	}
	r.Assets = AssetSummary{
		ManifestEntries:         ast.manifestEntries,
		ManifestMissingOnDisk:   ast.manifestMissingOnDisk,
		CacheIndexEntries:       ast.cacheIndexEntries,
		CacheIndexMissingOnDisk: ast.cacheIndexMissingOnDisk,
		OnlyInManifest:          ast.onlyInManifest,
		OnlyInCacheIndex:        ast.onlyInCacheIndex,
		StagingDeclared:         ast.stagingDeclared,
		StagingVerified:         ast.stagingVerified,
		StagingMissing:          ast.stagingMissing,
		StagingOrphans:          ast.stagingOrphans,
		VerifiedOnDisk:          ast.verifiedOnDisk,
		UniqueImages:            len(uniqueDigests),
		Source:                  source,
		CopyrightNotice:         CopyrightNotice,
	}

	// Ambiguity vectors, measured on the published classic pool.
	ec.AmbiguousVectors = countAmbiguityVectors(ds)
	r.EntityCounts = ec

	r.Gaps = buildDataGaps(ds, in, joins, ast, pr, ec)
	r.Limitations = buildLimitations()
	r.PublicIndex = buildPublicIndex(ds)

	// Section lists, derived from the compiled admissions.
	for _, a := range admissions {
		if !a.Available {
			continue
		}
		// Every published section must be listed, not just the primary one: the pack
		// writer iterates this list, so an unlisted section is compiled and then dropped.
		if len(a.Sections) > 0 {
			r.PublicSections = append(r.PublicSections, a.Sections...)
			continue
		}
		if a.Section != "" {
			r.PublicSections = append(r.PublicSections, a.Section)
		}
	}
	sort.Strings(r.PublicSections)
	r.PrivateSections = privateSectionNames(admissions)
	return r
}

// privateSectionNames are the server-only sections: the answer material and the
// solution seeds .
func privateSectionNames(admissions []ModeAdmission) []string {
	var out []string
	avail := make(map[string]bool, len(admissions))
	for _, a := range admissions {
		if a.Available {
			avail[a.Mode] = true
		}
	}
	if avail[ModeClassic] {
		out = append(out, "classic.answer-pool")
	}
	if avail[ModeRaceRebuild] {
		out = append(out, "race-rebuild.answers")
	}
	if avail[ModeMemory] {
		out = append(out, "memory.solution")
	}
	sort.Strings(out)
	return out
}

func countVoices(in *RawInputs) int {
	n := 0
	for i := range in.Wiki.Characters {
		n += len(in.Wiki.Characters[i].Voices)
	}
	return n
}

// countAmbiguityVectors counts characters whose nine-field vector collides.
func countAmbiguityVectors(ds *Dataset) int {
	seen := make(map[string]int)
	for i := range ds.Characters {
		c := &ds.Characters[i]
		if c.Groups == nil || len(c.Groups.Missing()) > 0 {
			continue
		}
		seen[c.Groups.Signature()]++
	}
	n := 0
	for _, k := range seen {
		if k > 1 {
			n += k
		}
	}
	return n
}

// buildDataGaps enumerates the documented gaps with recomputed values. Each gap
// from is represented, whether or not it blocks a mode.
func buildDataGaps(ds *Dataset, in *RawInputs, joins *joinStats, ast assetVerification, pr PlayabilityReport, ec EntityCounts) []DataGap {
	var gaps []DataGap

	gaps = append(gaps, DataGap{
		ID:       "GAP-001",
		Area:     "entities",
		Severity: "high",
		Measured: fmt.Sprintf("%d wiki records: %d playable, %d planned, %d npc, %d unknown", pr.Total, pr.Playable, pr.Planned, pr.NPC, pr.UnknownKind),
		Expected: fmt.Sprintf("%d trainable characters", pr.Playable),
		Impact:   []string{ModeClassic, ModeAptitudeGrid, ModeDraft, ModeSort, ModeMemory},
		Detail:   "the raw snapshot's 147 records include non-playable cast; implementation.status is not a playability signal, so entityKind and playableStatus are computed here instead",
	})

	gaps = append(gaps, DataGap{
		ID:       "GAP-002",
		Area:     "names",
		Severity: "medium",
		Measured: fmt.Sprintf("%d characters with an inferred Japanese name", pr.InferredNames),
		Expected: "0 unreviewed inferred names",
		Impact:   []string{ModeClassic, ModeBluff},
		Detail:   "these Japanese names were inferred from the prototype horse's name rather than read from a primary source; the flat translation table loses the confidence flag, so it is restored per record here and the characters are flagged",
	})

	gaps = append(gaps, DataGap{
		ID:       "GAP-003",
		Area:     "aptitudes",
		Severity: "high",
		Measured: fmt.Sprintf("%d of %d characters carry a graded aptitude set; %d outfits have none of their own", countWithAptitudes(ds), len(ds.Characters), len(ds.Outfits)),
		Expected: "per-outfit aptitude sets",
		Impact:   []string{ModeAptitudeGrid, ModeDraft},
		Detail:   "aptitudes exist per character record only; the 272 outfits do not each have an independent aptitude set, and copying the character's set onto every outfit would claim accuracy that does not exist",
	})

	skill := measureSkillAttribution(in.Wiki.Characters, in.Skills.Skills)
	gaps = append(gaps, DataGap{
		ID:       "GAP-004",
		Area:     "skills",
		Severity: "high",
		Measured: fmt.Sprintf("%d attribution rows over %d characters; %d rows match a definition name exactly; %d definitions", skill.Rows, skill.Characters, skill.ExactMatches, skill.DefinitionCount),
		Expected: fmt.Sprintf("%d of %d rows matched", skill.Rows, skill.Rows),
		Impact:   []string{},
		Detail:   "attribution names are Japanese while definitions are Chinese, so name matching cannot join them into a completed skill library; no mode consumes skills in this build",
	})

	goal := measureGoals(in.Wiki.Characters)
	gaps = append(gaps, DataGap{
		ID:       "GAP-005",
		Area:     "goals",
		Severity: "medium",
		Measured: fmt.Sprintf("%d goal rows over %d characters; indices present %v, max %d", goal.Total, goal.Characters, goal.Indices, goal.MaxIndex),
		Expected: "complete training-goal list per character",
		Impact:   []string{},
		Detail:   "the scrape only ever yields goal indices 1-7, so the parsed list must not be presented as the complete objective set",
	})

	gaps = append(gaps, DataGap{
		ID:       "GAP-006",
		Area:     "prototype",
		Severity: "high",
		Measured: fmt.Sprintf("%d prototype blocks, %d with an NKB id, %d without", ec.PrototypeHorses, ec.PrototypeWithNKB, ec.PrototypeHorses-ec.PrototypeWithNKB),
		Expected: fmt.Sprintf("%d with independent ids", ec.PrototypeHorses),
		Impact:   []string{ModePedigree},
		Detail:   "a prototype without an independent NKB id can only be linked by name, and a shared surname must not be treated as kinship, so those entries are excluded from pedigree questions",
	})

	voices := countVoices(in)
	missingChs := countVoicesWithoutChs(in)
	gaps = append(gaps, DataGap{
		ID:       "GAP-007",
		Area:     "audio",
		Severity: "high",
		Measured: fmt.Sprintf("%d voice rows; %d without a simplified-Chinese transcript; 0 with a local decodable file", voices, missingChs),
		Expected: fmt.Sprintf("%d rows with transcript and local audio", voices),
		Impact:   []string{ModeAudio},
		Detail:   "voice rows carry remote URLs only and no audio file exists in the repository, so no clip passed the decode and listening review",
	})

	gaps = append(gaps, DataGap{
		ID:       "GAP-008",
		Area:     "assets",
		Severity: "medium",
		Measured: fmt.Sprintf("legacy manifest declares %d paths (%d absent from disk) and cache index declares %d paths (%d absent); the canonical avatar directory declares %d avatars of which %d verified on disk as %d distinct images",
			ast.manifestEntries, ast.manifestMissingOnDisk, ast.cacheIndexEntries, ast.cacheIndexMissingOnDisk,
			ast.stagingDeclared, ast.stagingVerified, len(distinctAvatarDigests(ast))),
		Expected: fmt.Sprintf("%d character avatars", pr.Total),
		Impact:   []string{ModeMemory},
		Detail: fmt.Sprintf("TWO INDEPENDENT AVATAR SETS EXIST AND THEY DISAGREE. The legacy data/assets/asset-manifest.json declares public/assets/characters/thumbnails/*.png and marks them cached, but none of those %d files exist on disk; %d ids appear only in the manifest and %d only in the cache index. A second set under %s holds real PNGs named by the canonical numeric character id. The compiler verifies every candidate with os.Stat and copies the confirmed bytes into the pack, so runtime serving does not depend on the source directory. The old manifest must not be treated as evidence of a cached file.",
			ast.manifestEntries, ast.onlyInManifest, ast.onlyInCacheIndex, avatarDir),
	})

	gaps = append(gaps, DataGap{
		ID:       "GAP-009",
		Area:     "races",
		Severity: "high",
		Measured: fmt.Sprintf("%d starts over %d raceIds; %d races have 4-7 starters with a known rank", ec.RaceEntries, ec.Races, countUsableRaces(ds)),
		Expected: "complete starter field per race",
		Impact:   []string{ModeRaceRebuild, ModeCoRacePath},
		Detail:   "starts are recorded only for characters whose prototype appears in the snapshot; a race's recorded starters are not its full field, and the question text must say so",
	})

	gaps = append(gaps, DataGap{
		ID:       "GAP-010",
		Area:     "classic",
		Severity: "high",
		Measured: fmt.Sprintf("%d characters have a reviewed nine-field vector; %d share their vector with another character", ec.ClassicReady, ec.AmbiguousVectors),
		Expected: fmt.Sprintf("%d distinct answerable characters", ec.ClassicReady),
		Impact:   []string{ModeClassic},
		Detail:   "characters with an identical nine-field vector cannot be told apart by the classic comparison, so they are excluded from competitive packs and kept only in practice packs with an explicit ambiguityGroup label",
	})

	gaps = append(gaps, DataGap{
		ID:       "GAP-011",
		Area:     "translations",
		Severity: "low",
		Measured: fmt.Sprintf("%d flat translation rows", measureTranslations(in.Wiki.Characters).Rows),
		Impact:   []string{},
		Detail:   "the snapshot's flat translation table does not carry a confidence column, so per-slot confidence is restored from the per-record name block",
	})

	gaps = append(gaps, DataGap{
		ID:       "GAP-012",
		Area:     "joins",
		Severity: "medium",
		Measured: fmt.Sprintf("%d of %d ready entries and %d of %d blocked entries matched a wiki record by Japanese name; %d wiki records matched no bank",
			joins.ReadyMatchedWiki, joins.ReadyEntries, joins.BlockedMatchedWiki, joins.BlockedEntries, len(joins.WikiNotInAnyBank)),
		Impact: []string{ModeClassic},
		Detail: "the legacy banks and the wiki snapshot share only the Japanese name, so the join is by Japanese name and every unmatched row is listed rather than dropped silently",
	})

	gaps = append(gaps, DataGap{
		ID:       "GAP-013",
		Area:     "manual",
		Severity: "low",
		Measured: fmt.Sprintf("%d manual field overrides", len(in.Overrides)),
		Impact:   []string{ModeClassic},
		Detail:   "the manual correction table currently patches a single character's schoolGrade; it is applied by legacy id and its small size is reported so it is not mistaken for broad review",
	})

	return gaps
}

func firstToken(s string) string {
	for i := 0; i < len(s); i++ {
		if s[i] == ':' {
			return s[:i]
		}
	}
	return s
}

func countWithAptitudes(ds *Dataset) int {
	n := 0
	for i := range ds.Characters {
		if ds.Characters[i].Aptitudes.Complete() {
			n++
		}
	}
	return n
}

func countUsableRaces(ds *Dataset) int {
	n := 0
	for _, r := range ds.Races {
		known := 0
		for _, e := range r.Entries {
			if e.FinishRank != nil && e.CharacterID != "" {
				known++
			}
		}
		if known >= raceRebuildMinStarters && known <= raceRebuildMaxStarters && r.Date != "" {
			n++
		}
	}
	return n
}

func countVoicesWithoutChs(in *RawInputs) int {
	n := 0
	for i := range in.Wiki.Characters {
		for _, v := range in.Wiki.Characters[i].Voices {
			if v.Chs == "" {
				n++
			}
		}
	}
	return n
}

// buildLimitations states what this build does not claim. A limitation list is
// required so the report is not read as a clean bill of health .
func buildLimitations() []string {
	return []string{
		"reviewStatus is pending: no human has signed off on the published facts, so no mode may be labelled competitive-ready by this build alone",
		"no audio file was decoded and no clip was listened to; the audio mode is unavailable rather than unverified",
		"no question was playtested; per-mode question counts are compiled pool sizes, not validated difficulty or balance",
		"aptitudes are character-level; no per-outfit aptitude set exists and none was synthesised",
		"the race starter lists are the recorded subset, not a complete field; rank gaps are preserved and never renumbered",
		"skill attribution is measured but not used: no mode consumes skills in this build",
		"the classic pool keeps the legacy reviewed groups verbatim; the raw A-G aptitude grades were not substituted for them",
		"asset verification reads the local filesystem, so its result is only valid for the checkout the build ran in",
	}
}

func buildPublicIndex(ds *Dataset) []PublicIndexEntry {
	out := make([]PublicIndexEntry, 0, len(ds.Characters))
	for i := range ds.Characters {
		c := &ds.Characters[i]
		localized := map[string]string{"zh-Hans": c.Names.ZhHans}
		if c.Names.ZhHant != "" {
			localized["zh-Hant"] = c.Names.ZhHant
		}
		if c.Names.Ja != "" && c.Names.JaConfidence == ConfidenceConfirmed {
			localized["ja"] = c.Names.Ja
		}
		e := PublicIndexEntry{
			LocalizedNames: localized,
			SearchNames:    c.SearchNames,
			ID:             c.ID,
			NameZh:         c.Names.ZhHans,
			NameJp:         c.Names.Ja,
			NameEn:         c.Names.En,
			ImagePath:      c.ImagePath,
			RemoteImageURL: c.RemoteImageURL,
			Playable:       c.PlayableStatus == PlayableReady,
		}
		if len(c.LegacyIDs) > 0 {
			e.LegacyID = c.LegacyIDs[0]
		}
		for _, a := range c.Aliases {
			e.Aliases = append(e.Aliases, a.Text)
		}
		out = append(out, e)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}
