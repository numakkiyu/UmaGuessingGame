package content

import (
	"runtime"
	"sort"
	"time"
)

// Evidence records build inputs, content admission and reproducible checks.
type Evidence struct {
	Kind               string `json:"kind"`
	GeneratedAt        string `json:"generatedAt"`
	SchemaVersion      string `json:"schemaVersion"`
	ContentPackVersion string `json:"contentPackVersion"`
	DatasetVersion     string `json:"datasetVersion"`
	ElapsedMs          int64  `json:"elapsedMs"`

	Manifest ManifestEvidence `json:"manifest"`
	Modes    []ModeEvidence   `json:"modes"`
	Entities EntityCounts     `json:"entityCounts"`
	Assets   AssetSummary     `json:"assets"`
	// AvatarSources documents the two independent avatar sets and which one was
	// used, so the memory admission basis is auditable.
	AvatarSources []AvatarSourceEvidence `json:"avatarSources"`
	Joins         JoinReport             `json:"joins"`
	Gaps          []DataGap              `json:"dataGaps"`
	Playability   PlayabilityReport      `json:"playability"`

	// SectionSchemas documents the exact JSON shape of every published section so
	// the mode packages can be checked against it during integration.
	SectionSchemas []SectionSchema `json:"sectionSchemas"`

	// RejectionTotals aggregates the per-mode rejection distribution.
	RejectionTotals []ModeRejections `json:"rejectionTotals"`

	Limitations []string `json:"limitations"`

	// Reproduction records how this evidence was produced, so a reader can rerun
	// it instead of trusting the numbers.
	Reproduction Reproduction `json:"reproduction"`
}

// Reproduction is the command and verification record for one build.
type Reproduction struct {
	Command   string   `json:"command"`
	GoVersion string   `json:"goVersion"`
	Checks    []string `json:"checks"`
	PackDir   string   `json:"packDir,omitempty"`
}

// AvatarSourceEvidence documents one avatar source set.
type AvatarSourceEvidence struct {
	Source   string `json:"source"`
	Location string `json:"location"`
	Declared int    `json:"declared"`
	Verified int    `json:"verified"`
	Used     bool   `json:"used"`
	Notes    string `json:"notes"`
}

// ManifestEvidence is the manifest's identity block without the file list.
type ManifestEvidence struct {
	SchemaVersion        string           `json:"schemaVersion"`
	DatasetVersion       string           `json:"datasetVersion"`
	ContentPackVersion   string           `json:"contentPackVersion"`
	GeneratedAt          string           `json:"generatedAt"`
	Locale               string           `json:"locale"`
	Region               string           `json:"region"`
	ReviewStatus         ReviewStatus     `json:"reviewStatus"`
	SHA256               string           `json:"sha256"`
	SourceSnapshotHashes []SourceSnapshot `json:"sourceSnapshotHashes"`
	PublicSections       []string         `json:"publicSections"`
	PrivateSections      []string         `json:"privateSections"`
	FileCount            int              `json:"fileCount"`
}

// ModeEvidence is one mode's admission result.
type ModeEvidence struct {
	Mode          string        `json:"mode"`
	Available     bool          `json:"available"`
	QuestionCount int           `json:"questionCount"`
	MinItems      int           `json:"minItems"`
	Section       string        `json:"section,omitempty"`
	Reason        string        `json:"reason,omitempty"`
	Rejections    []ReasonCount `json:"rejections,omitempty"`
	Notes         []string      `json:"notes,omitempty"`
}

// ModeRejections is the rejection distribution for one mode, flattened for
// scanning.
type ModeRejections struct {
	Mode       string        `json:"mode"`
	Rejections []ReasonCount `json:"rejections"`
	Total      int           `json:"total"`
}

// SectionSchema documents one published section: its name, its JSON kind, and
// the exact field list a consumer may rely on.
type SectionSchema struct {
	Section      string   `json:"section"`
	Mode         string   `json:"mode"`
	Kind         string   `json:"kind"`
	ItemFields   []string `json:"itemFields,omitempty"`
	ObjectFields []string `json:"objectFields,omitempty"`
	Notes        string   `json:"notes,omitempty"`
}

// BuildEvidence assembles the evidence record for a published run.
func BuildEvidence(res *PublishResult, elapsed time.Duration) *Evidence {
	r := res.Report
	m := res.Manifest

	ev := &Evidence{
		Kind:               "content-pipeline-build",
		GeneratedAt:        m.GeneratedAt,
		SchemaVersion:      m.SchemaVersion,
		ContentPackVersion: m.ContentPackVersion,
		DatasetVersion:     m.DatasetVersion,
		ElapsedMs:          elapsed.Milliseconds(),
		Manifest: ManifestEvidence{
			SchemaVersion:        m.SchemaVersion,
			DatasetVersion:       m.DatasetVersion,
			ContentPackVersion:   m.ContentPackVersion,
			GeneratedAt:          m.GeneratedAt,
			Locale:               m.Locale,
			Region:               m.Region,
			ReviewStatus:         m.ReviewStatus,
			SHA256:               m.SHA256,
			SourceSnapshotHashes: m.SourceSnapshotHashes,
			PublicSections:       m.PublicSections,
			PrivateSections:      m.PrivateSections,
			FileCount:            len(m.Files),
		},
		Entities:       r.EntityCounts,
		Assets:         r.Assets,
		AvatarSources:  avatarSourceEvidence(r.Assets),
		Joins:          r.Joins,
		Gaps:           r.Gaps,
		Playability:    r.Playability,
		SectionSchemas: SectionSchemas(),
		Limitations:    r.Limitations,
		Reproduction: Reproduction{
			Command:   "go run ./cmd/content build --root . --evidence .cache/reports/content.json",
			GoVersion: runtime.Version(),
			Checks: []string{
				"go build ./...",
				"go vet ./...",
				"go test ./internal/content/...",
				"go run ./cmd/content verify",
			},
			PackDir: res.Path,
		},
	}

	for _, a := range res.Admissions {
		ev.Modes = append(ev.Modes, ModeEvidence{
			Mode:          a.Mode,
			Available:     a.Available,
			QuestionCount: a.QuestionCount,
			MinItems:      a.MinItems,
			Section:       a.Section,
			Reason:        a.Reason,
			Rejections:    a.Rejections,
			Notes:         a.Notes,
		})
		total := 0
		for _, rc := range a.Rejections {
			total += rc.Count
		}
		ev.RejectionTotals = append(ev.RejectionTotals, ModeRejections{
			Mode:       a.Mode,
			Rejections: a.Rejections,
			Total:      total,
		})
	}
	sort.Slice(ev.Modes, func(i, j int) bool { return ev.Modes[i].Mode < ev.Modes[j].Mode })
	sort.Slice(ev.RejectionTotals, func(i, j int) bool { return ev.RejectionTotals[i].Mode < ev.RejectionTotals[j].Mode })
	return ev
}

// SectionSchemas is the authoritative field list of every section this compiler
// can publish. The mode packages must parse exactly these fields.
func SectionSchemas() []SectionSchema {
	return []SectionSchema{
		{
			Section: SectionClassicCharacters,
			Mode:    ModeClassic,
			Kind:    "object",
			ObjectFields: []string{
				"mode", "cards", "practiceCards", "competitiveCount", "practiceCount", "ambiguousExcluded",
			},
			ItemFields: []string{
				"id", "legacyId", "nameZh", "nameJp", "nameJpConfidence",
				"star", "surfaceGroup", "distanceGroup", "runningStyleGroup",
				"sexType", "g1Bracket", "g23Bracket", "g1HasJpn", "g23HasJpn",
				"schoolGrade", "dormitory", "imagePath", "remoteImageUrl", "ambiguityGroup",
			},
			Notes: "cards is the competitive answer pool with ambiguous vectors removed; practiceCards keeps them and sets ambiguityGroup. star is null when unknown and must not be compared as a value. g1Bracket is one of 0/1-2/3-4/5+, g23Bracket one of 0/1-3/4-6/7+.",
		},
		{
			Section:      SectionAptitudeGridCards,
			Mode:         ModeAptitudeGrid,
			Kind:         "object",
			ObjectFields: []string{"mode", "cards", "coverableCells", "totalCells", "gridDimensions"},
			ItemFields:   []string{"characterId", "name", "nameJp", "imagePath", "remoteImageUrl", "aptitudes"},
			Notes:        "aptitudes.surface is keyed 草地/泥地, aptitudes.distance 短/英/中/长, aptitudes.style 逃/先/差/追; every value is a single letter A-G. coverableCells counts the 16 distance x style cells that hold at least one A/A card.",
		},
		{
			Section:      SectionDraftMarket,
			Mode:         ModeDraft,
			Kind:         "object",
			ObjectFields: []string{"mode", "budget", "roster", "cards", "tasks", "feasibleTasks"},
			ItemFields:   []string{"characterId", "name", "imagePath", "remoteImageUrl", "price", "aptitudes"},
			Notes:        "cards[].price follows the trial formula 3 + ceil(distanceA/2) + ceil(styleA/2) + (dirtA?1:0) and is a non-official balance parameter. tasks[] carries {taskId, surface, distance, style}.",
		},
		{
			Section: SectionRaceRebuildRaces,
			Mode:    ModeRaceRebuild,
			Kind:    "array",
			ItemFields: []string{
				"raceId", "name", "date", "course", "surface", "grade", "distance", "region",
				"starters", "starterCount",
			},
			Notes: "starters[] is {characterId, characterName} and is ordered by characterId so array position carries no hint of the result. The finish ranks are the answer and are NOT in this section: they live in the private race-rebuild.answers section as {raceId, order[], ties?, dropped?}. A starter with no recorded rank is dropped before publication and the rest are never renumbered, so rank gaps (1,2,3,5,7) survive in the private answer. date is ISO YYYY-MM-DD and region is explicit.",
		},
		{
			Section:      SectionCoRacePathGraph,
			Mode:         ModeCoRacePath,
			Kind:         "object",
			ObjectFields: []string{"mode", "maxChainEdges", "nodes", "edges", "components", "largestComponent"},
			Notes:        "nodes[] is {characterId, name}; edges[] is {from, to, raceId, raceName, date, sharedRaces} and is undirected with from < to. components is an array of character-id arrays; a start/end pair must share a component.",
		},
		{
			Section:      SectionSortCards,
			Mode:         ModeSort,
			Kind:         "object",
			ObjectFields: []string{"mode", "cards", "defaultCards", "sortableKeys", "distinctHeights"},
			ItemFields:   []string{"characterId", "name", "imagePath", "remoteImageUrl", "heightCm", "birthMonth", "birthDay", "eventDate"},
			Notes:        "eventDate is {date, region, raceId, raceName} or null; date is ISO and region is explicit, never assumed. heightCm/birthMonth/birthDay may be null and a null is never compared.",
		},
		{
			Section:    SectionMemoryPairs,
			Mode:       ModeMemory,
			Kind:       "array",
			ItemFields: []string{"pairId", "imagePath", "label", "width", "height"},
			Notes:      "one record per pair with an opaque pairId, stored imagePath and placeholder label. Readable names remain in the private memory.solution section until reveal. Each board card is projected through its own opaque /" + PackMemoryPairDir + "/<token>" + PackImageExt() + " route, resolved to the stored image without a shared pair directory. Published image paths and dimensions are verified at build time; this section contains no round board layout.",
		},
		{
			Section: PublicIndexSection,
			Mode:    "",
			Kind:    "array",
			ItemFields: []string{
				"id", "legacyId", "nameZh", "nameJp", "nameEn", "aliases", "imagePath", "remoteImageUrl", "playable", "localizedNames", "searchNames",
			},
			Notes: "the servable name index for search. searchNames optionally separates Chinese translations, confirmed Japanese name, English name and source-recorded community nicknames; outfit titles are not nicknames. localizedNames optionally carries zh-Hans, source-backed zh-Hant and confirmed ja display names; missing regions fall back to nameZh. It carries identity and search text only: no nine-field comparison vector and no secret fact, so a client can search without receiving answer material. imagePath is the local fallback file and always exists for a published character; remoteImageUrl is the upstream CDN URL and is empty for characters the snapshot has no URL for, so a client must prefer remote and fall back to local. Both are needed: remote keeps server bandwidth low, local keeps a LAN or offline event working.",
		},
		{
			Section:    "pedigree.entries",
			Mode:       ModePedigree,
			Kind:       "array",
			ItemFields: []string{"characterId", "name", "nkbId", "pedigree", "prototypeNames"},
			Notes:      "one row per prototype horse that has an independent NKB id and a complete six-slot pedigree. pedigree is {sire, dam, sireSire, sireDam, damSire, damDam}. Relations come only from explicit pedigree fields; a shared surname is never treated as kinship.",
		},
		{
			Section:    SectionBluffFacts,
			Mode:       ModeBluff,
			Kind:       "array",
			ItemFields: []string{"factId", "characterId", "text", "source", "reviewStatus"},
			Notes:      "One row per candidate fact. reviewStatus is approved only when data/manual/bluff-facts.review.json records approved=true, a citation and a named reviewer; the mode asserts the fact is true, so a pending fact is never published.",
		},
		{
			Section:    SectionTradeCards,
			Mode:       ModeTrade,
			Kind:       "array",
			ItemFields: []string{"cardId", "label", "provenance"},
			Notes:      "The authored card-kind catalogue. Every entry carries structured provenance: kind=authored-design-value and notOfficialData=true, because these are designed values rather than official game data. The rule layer validates kind and the flag; it cannot verify that basis names a real document, so that remains a human review item.",
		},
		{
			Section:    SectionTradeObjectives,
			Mode:       ModeTrade,
			Kind:       "array",
			ItemFields: []string{"objectiveId", "text", "kinds", "points", "provenance"},
			Notes:      "The authored objective set. Sixteen entries so an eight-player round can deal two objectives each without repeating one: two players holding the same goal would be forced to cooperate or collide, which is a gameplay break rather than a tolerable shortage. Which objectives a specific player holds is private and never appears here.",
		},
		{
			Section: SectionRulesTrack,
			Mode:    ModeStrategyRace,
			Kind:    "object",
			ObjectFields: []string{
				"totalTurns", "decisionSeconds", "startPosition", "startStamina",
				"staminaCap", "followMinGap", "followMaxGap", "defaultAction",
				"actions", "terrainChanges", "randomEvents", "provenance",
			},
			Notes: "An entertainment-simulation baseline, not an official game formula: provenance.simulation must be true and notOfficialData must be true. The mode reads these numbers at Prepare and applies them, so this section is the single source of truth for the race rather than a copy of constants compiled into the binary.",
		},
		{
			Section:      SectionCreativePrompts,
			Mode:         ModeCreative,
			Kind:         "object",
			ObjectFields: []string{"scenes", "constraints"},
			ItemFields:   []string{"sceneId", "constraintId", "text", "provenance"},
			Notes:        "Authored writing prompts. Each seat draws one scene and one constraint; the character comes from characters.index, so this section carries no character identity. Every entry declares authored-design-value provenance.",
		},
		{
			Section:      SectionRapportQuestions,
			Mode:         ModeRapport,
			Kind:         "object",
			ObjectFields: []string{"questions"},
			ItemFields:   []string{"questionId", "prompt", "options", "provenance"},
			Notes:        "Authored rapport questions, each with a fixed set of options. The bank must hold at least one round's worth; a smaller bank cannot fill a round, which the mode refuses rather than padding.",
		},
		{
			Section:      SectionCluePuzzleDeck,
			Mode:         ModeCluePuzzle,
			Kind:         "object",
			ObjectFields: []string{"cards"},
			ItemFields:   []string{"cardId", "label", "provenance"},
			Notes:        "Authored clue cards. A round draws a seeded subset from this pool, so a deck larger than one round's need is used rather than ignored. The per-player clues derived from these cards are private and never appear in this section.",
		},
		{
			Section:    SectionAudioClips,
			Mode:       ModeAudio,
			Kind:       "array",
			ItemFields: []string{"characterId", "audioUrl", "jp", "chs", "cht", "hasLocalFile"},
			Notes:      "audioUrl is the upstream wiki CDN address recorded in the snapshot. hasLocalFile is true only when a non-empty clip exists under data/assets/audio; the mode needs a decodable local file, so a recorded URL alone is not material.",
		},
	}
}

// avatarSourceEvidence describes both avatar sets so a reader can see why the
// memory mode is admitted on one of them and not the other.
func avatarSourceEvidence(a AssetSummary) []AvatarSourceEvidence {
	return []AvatarSourceEvidence{
		{
			Source:   string(AvatarSourceCanonical),
			Location: avatarDir,
			Declared: a.StagingDeclared,
			Verified: a.StagingVerified,
			Used:     a.Source == string(AvatarSourceCanonical),
			Notes:    "PNG files from the tracked avatar directory, keyed by canonical character ID and copied into the public content pack",
		},
		{
			Source:   string(AvatarSourcePublic),
			Location: "public/assets/characters/thumbnails (declared by data/assets/asset-manifest.json)",
			Declared: a.ManifestEntries,
			Verified: a.ManifestEntries - a.ManifestMissingOnDisk,
			Used:     a.Source == string(AvatarSourcePublic),
			Notes:    "Legacy manifest paths are verified against disk; cached flags do not establish file availability",
		},
	}
}
