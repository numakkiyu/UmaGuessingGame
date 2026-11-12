package content

import (
	"fmt"
	assetutil "github.com/bhcn/umaguessinggame/internal/assets"
	"sort"
	"strings"
)

// Mode identifiers. These match the section-name prefixes modes read
// (see sections.go) and are stable across releases.
const (
	ModeClassic      = "classic"
	ModeAptitudeGrid = "aptitude-grid"
	ModeDraft        = "draft"
	ModeRaceRebuild  = "race-rebuild"
	ModeCoRacePath   = "co-race-path"
	ModeSort         = "sort"
	ModeMemory       = "memory"
	ModeBluff        = "bluff"
	ModeCluePuzzle   = "clue-puzzle"
	ModeStrategyRace = "strategy-race"
	ModePedigree     = "pedigree"
	ModeAudio        = "audio"
	ModeTrade        = "trade"
	ModeCreative     = "creative"
	ModeRapport      = "rapport"
)

// AllModes lists supported modes in stable presentation order.
var AllModes = []string{
	ModeClassic, ModeAptitudeGrid, ModeDraft, ModeRaceRebuild, ModeCoRacePath,
	ModeSort, ModeMemory, ModeBluff, ModeCluePuzzle, ModeStrategyRace,
	ModePedigree, ModeAudio, ModeTrade, ModeCreative, ModeRapport,
}

// Avatar bytes are copied into the public content pack for self-contained serving
const (
	// PackAvatarDir is the pack-relative directory the avatar bytes are written
	// into, under the public tree.
	PackAvatarDir = "assets/avatars"
	// PackAvatarRoutePrefix is the HTTP route a server mounts PackAvatarDir on.
	// It must equal "/" + PackAvatarDir, because the served route is the pack's
	// public-relative path and nothing else.
	PackAvatarRoutePrefix = "/" + PackAvatarDir + "/"
	// PackMemoryPairDir is where the per-pair image copies live, inside the pack's
	// public tree. A pair's route is /assets/faces/<pairId>.png.
	PackMemoryPairDir = "assets/faces"
	// PackMemoryPairRoutePrefix is the HTTP route for a pair image.
	PackMemoryPairRoutePrefix = "/" + PackMemoryPairDir + "/"
)

// MemoryLabelPlaceholder is the label every public memory pair carries. It must
// stay meaningless: the mode tests memory, not character recognition, so a public
// label would hand the answer over .
const MemoryLabelPlaceholder = "???"

// PackAvatarRoute is the HTTP route for one character's avatar inside a pack. The
// file itself lives at PackAvatarDir/<id>.png under the pack's public tree, so a
// server can mount that directory statically without exposing anything else.
func PackAvatarRoute(characterID string) string {
	return PackAvatarRoutePrefix + characterID + PackImageExt()
}

// PackImageExt is the file extension published media uses.
//
// The route and the written file must agree, so both derive from this one place.
// WebP is used when the encoder is available at build time; otherwise the PNG
// sources are copied unchanged and the extension follows. Hard-coding ".png" while
// the publisher wrote ".webp" produced routes that pointed at files which did not
// exist — a class of bug that only a real filesystem check catches.
func PackImageExt() string {
	if assetutil.EncoderAvailable() {
		return ".webp"
	}
	return ".png"
}

// PackMemoryPairRoute is the HTTP route for one memory pair's approved image.
//
// The route is derived from the pack-relative file location, so a published
// imagePath always matches a file that really exists under the pack's public
// tree. A server mounts that tree at the document root, so the URL resolves.
func PackMemoryPairRoute(pairID string) string {
	return PackMemoryPairRoutePrefix + pairID + PackImageExt()
}

// RejectReason is a machine-readable cause for excluding one candidate item.
// Reasons are counted in the report so a shortfall is attributable .
type RejectReason string

const (
	RejectMissingNineFields  RejectReason = "missing_classic_nine_fields"
	RejectAmbiguousVector    RejectReason = "ambiguous_nine_field_vector"
	RejectUnknownStar        RejectReason = "unknown_star"
	RejectNoAptitudes        RejectReason = "no_aptitude_grades"
	RejectIncompleteAptitude RejectReason = "incomplete_aptitude_grades"
	RejectNotPlayable        RejectReason = "not_playable_entity"
	RejectNoAsset            RejectReason = "no_verified_avatar_file"
	RejectNoPrice            RejectReason = "no_derivable_price"
	RejectRaceTooSmall       RejectReason = "race_below_minimum_starters"
	RejectRaceTooLarge       RejectReason = "race_above_maximum_starters"
	RejectRaceNoRank         RejectReason = "race_entry_without_rank"
	RejectRaceNoDate         RejectReason = "race_without_parseable_date"
	RejectRaceNoIdentity     RejectReason = "race_entry_without_character_id"
	RejectNoRaceEvidence     RejectReason = "no_shared_race_evidence"
	RejectNoHeightOrBirthday RejectReason = "no_height_or_birthday"
	RejectAllValuesEqual     RejectReason = "all_compared_values_equal"
	RejectInferredName       RejectReason = "inferred_name_unreviewed"
	RejectNoPedigree         RejectReason = "pedigree_incomplete"
	RejectNoNKBID            RejectReason = "prototype_missing_nkb_id"
	RejectNoAudioFile        RejectReason = "no_local_audio_file"
	RejectNoSkillDefinition  RejectReason = "skill_name_not_matched_to_definition"
	RejectNoGoals            RejectReason = "no_parsed_training_goals"
	RejectNoSecret           RejectReason = "no_reviewed_secret_fact"
	RejectNoGameData         RejectReason = "no_in_game_data"
	RejectUnreviewedFact     RejectReason = "unreviewed_fact_not_publishable"
	RejectDuplicateImage     RejectReason = "duplicate_avatar_image"
)

// RejectTally counts rejections by reason for one mode.
type RejectTally map[RejectReason]int

// add increments a reason and returns nothing, keeping call sites terse.
func (t RejectTally) add(r RejectReason) { t[r]++ }

// sorted returns reasons in descending count then name order.
func (t RejectTally) sorted() []ReasonCount {
	out := make([]ReasonCount, 0, len(t))
	for r, n := range t {
		out = append(out, ReasonCount{Reason: string(r), Count: n})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Count != out[j].Count {
			return out[i].Count > out[j].Count
		}
		return out[i].Reason < out[j].Reason
	})
	return out
}

// ReasonCount is one row of the rejection distribution.
type ReasonCount struct {
	Reason string `json:"reason"`
	Count  int    `json:"count"`
}

// ModeAdmission is the admission verdict for one mode . An
// unavailable mode must not stop the site from starting: it is disabled with an
// explicit reason instead.
type ModeAdmission struct {
	Mode string `json:"mode"`
	// Available reports whether at least MinItems usable questions compiled.
	Available bool `json:"available"`
	// QuestionCount is the number of usable items actually compiled.
	QuestionCount int `json:"questionCount"`
	// MinItems is the smallest pool this mode accepts.
	MinItems int `json:"minItems"`
	// Reason explains unavailability. Empty when available.
	Reason string `json:"reason,omitempty"`
	// Section names the pack's primary section for this mode. Empty when unavailable,
	// so Section() returns ok=false. It is the name a reader should look at first, not
	// the only section the mode publishes: a mode may carry several (trade has a card
	// catalogue and an objective set), and those are listed in Sections.
	Section string `json:"section,omitempty"`
	// Sections lists every section this mode publishes, sorted. Publishing a section
	// without listing it here meant the file was compiled but never written, so the mode
	// reported items that no client could read.
	Sections []string `json:"sections,omitempty"`
	// Rejections is the full rejection distribution for this mode.
	Rejections []ReasonCount `json:"rejections,omitempty"`
	// Notes carries human-facing caveats that must not be dropped.
	Notes []string `json:"notes,omitempty"`
}

// modeResult is the internal compile outcome for one mode.
type modeResult struct {
	admission ModeAdmission
	// sections are the public payloads, keyed by section name. Empty when the
	// mode is unavailable.
	sections map[string]any
	// secrets are the server-only payloads this mode produced. They are written
	// under private/ and never under public/ . A mode that publishes
	// answers must split them here rather than rely on a client to hide them.
	secrets map[string]any
	// pairSources maps an opaque memory pair id to the character whose avatar it
	// shows. It is used only while writing per-pair image files and is never
	// published .
	pairSources map[string]string
}

// compileContext carries the shared inputs of every mode compiler. Raw is kept
// so a mode can measure facts that exist only in the scrape output (voice rows,
// skill attribution, goal indices) instead of assuming them.
type compileContext struct {
	ds  *Dataset
	ast assetVerification
	raw *RawInputs
	// root is the repository root, needed to read authored content files that have no
	// scraped source (trade). It is empty in tests that do not exercise those paths.
	root string
}

// rootDir returns the repository root, defaulting to the working directory.
//
// The compiler runs from the repository root in normal use, so an unset root resolves
// the same way the other file-reading paths do.
func (ctx *compileContext) rootDir() string {
	if ctx.root != "" {
		return ctx.root
	}
	return "."
}

// compileModes runs every mode compiler. A mode that cannot meet its minimum
// pool is recorded as unavailable with its reason; it never aborts the build
// .
// compileModes runs every mode compiler and returns the admissions plus the two
// separate payload maps: public and server-only. Keeping them apart here is what
// guarantees no answer material can reach the public tree.
func compileModes(ds *Dataset, ast assetVerification, raw *RawInputs, root string) ([]ModeAdmission, map[string]any, map[string]any, map[string]string, error) {
	ctx := &compileContext{ds: ds, ast: ast, raw: raw, root: root}
	compilers := []func(*compileContext) (modeResult, error){
		compileClassic,
		compileAptitudeGrid,
		compileDraft,
		compileRaceRebuild,
		compileCoRacePath,
		compileSort,
		compileMemory,
		compileBluff,
		compileCluePuzzle,
		compileStrategyRace,
		compilePedigree,
		compileAudio,
		compileTrade,
		compileCreative,
		compileRapport,
	}

	adm := make([]ModeAdmission, 0, len(compilers))
	sections := make(map[string]any)
	secrets := make(map[string]any)
	pairSources := make(map[string]string)
	for _, fn := range compilers {
		res, err := fn(ctx)
		if err != nil {
			return nil, nil, nil, nil, err
		}
		adm = append(adm, res.admission)
		if !res.admission.Available {
			// An unavailable mode must not leak a secret section either.
			continue
		}
		for k, v := range res.sections {
			if _, dup := sections[k]; dup {
				return nil, nil, nil, nil, fmt.Errorf("content: duplicate public section %q", k)
			}
			sections[k] = v
		}
		for k, v := range res.secrets {
			if _, dup := secrets[k]; dup {
				return nil, nil, nil, nil, fmt.Errorf("content: duplicate private section %q", k)
			}
			if _, clash := sections[k]; clash {
				return nil, nil, nil, nil, fmt.Errorf("content: section %q is published in both trees", k)
			}
			secrets[k] = v
		}
		for pair, character := range res.pairSources {
			pairSources[pair] = character
		}
	}
	sort.Slice(adm, func(i, j int) bool { return adm[i].Mode < adm[j].Mode })
	return adm, sections, secrets, pairSources, nil
}

// unavailable builds the admission record for a mode that cannot run.
func unavailable(mode, section string, minItems int, tally RejectTally, notes ...string) modeResult {
	return modeResult{
		admission: ModeAdmission{
			Mode:       mode,
			Available:  false,
			MinItems:   minItems,
			Section:    "",
			Reason:     reasonFor(mode, tally, minItems),
			Rejections: tally.sorted(),
			Notes:      notes,
		},
	}
}

// reasonFor renders a short, factual explanation of why a mode has no pool.
func reasonFor(mode string, tally RejectTally, minItems int) string {
	top := tally.sorted()
	var parts []string
	for i, rc := range top {
		if i >= 3 {
			break
		}
		parts = append(parts, fmt.Sprintf("%s=%d", rc.Reason, rc.Count))
	}
	base := fmt.Sprintf("%s: no usable items (minimum %d)", mode, minItems)
	if len(parts) > 0 {
		base += "; leading rejections: " + strings.Join(parts, ", ")
	}
	return base
}

// available builds the admission record for a mode that compiled.
func available(mode string, count, minItems int, tally RejectTally, notes []string, sections map[string]any, secrets ...map[string]any) modeResult {
	section := ""
	// The section name is the mode's primary section, recorded for the report.
	for k := range sections {
		if section == "" || k < section {
			section = k
		}
	}
	names := make([]string, 0, len(sections))
	for k := range sections {
		names = append(names, k)
	}
	sort.Strings(names)
	out := modeResult{
		admission: ModeAdmission{
			Mode:          mode,
			Available:     true,
			QuestionCount: count,
			MinItems:      minItems,
			Section:       section,
			Sections:      names,
			Rejections:    tally.sorted(),
			Notes:         notes,
		},
		sections: sections,
	}
	for _, extra := range secrets {
		if out.secrets == nil {
			out.secrets = make(map[string]any, len(extra))
		}
		for k, v := range extra {
			out.secrets[k] = v
		}
	}
	return out
}

// ---------------------------------------------------------------------------
// Shared card projections
// ---------------------------------------------------------------------------

// classicCard is one classic answer candidate. It carries exactly the nine
// comparison fields plus identity; unknown values are null, never zero .
type classicCard struct {
	ID                string   `json:"id"`
	LegacyID          string   `json:"legacyId,omitempty"`
	NameZh            string   `json:"nameZh"`
	NameJp            string   `json:"nameJp"`
	NameJpConfidence  string   `json:"nameJpConfidence"`
	Star              *int     `json:"star"`
	SurfaceGroup      []string `json:"surfaceGroup"`
	DistanceGroup     []string `json:"distanceGroup"`
	RunningStyleGroup []string `json:"runningStyleGroup"`
	SexType           string   `json:"sexType"`
	G1Bracket         string   `json:"g1Bracket"`
	G23Bracket        string   `json:"g23Bracket"`
	G1HasJpn          bool     `json:"g1HasJpn"`
	G23HasJpn         bool     `json:"g23HasJpn"`
	SchoolGrade       string   `json:"schoolGrade"`
	Dormitory         string   `json:"dormitory"`
	ImagePath         *string  `json:"imagePath"`
	// RemoteImageURL is the upstream CDN URL; the client prefers it and falls back
	// to ImagePath when the CDN is unreachable (LAN/offline events, ).
	RemoteImageURL string `json:"remoteImageUrl,omitempty"`
	// AmbiguityGroup is non-empty when the nine-field vector collides with
	// another character. Competitive packs exclude these .
	AmbiguityGroup string `json:"ambiguityGroup,omitempty"`
}

// aptitudeCard carries the ten graded keys for grid, draft and memory modes.
type aptitudeCard struct {
	CharacterID string       `json:"characterId"`
	Name        string       `json:"name"`
	NameJp      string       `json:"nameJp"`
	ImagePath   *string      `json:"imagePath"`
	Aptitudes   aptitudeView `json:"aptitudes"`
}

// aptitudeView is the graded aptitude set keyed by the Chinese bucket labels a
// player sees, because the grid is displayed in Chinese .
type aptitudeView struct {
	Surface  map[string]string `json:"surface"`
	Distance map[string]string `json:"distance"`
	Style    map[string]string `json:"style"`
}

// gradeToZh maps the raw game keys to the Chinese bucket labels used on cards.
var (
	surfaceZh  = map[string]string{"turf": "草地", "dirt": "泥地"}
	distanceZh = map[string]string{
		"sprint": "短", "mile": "英", "medium": "中", "long": "长",
	}
	styleZh = map[string]string{
		"front": "逃", "pace": "先", "late": "差", "end": "追",
	}
)

// toAptitudeView relabels the raw aptitude maps into display buckets. It returns
// false when any of the ten graded keys is missing, because a partially graded
// card cannot be compared .
func toAptitudeView(a Aptitudes) (aptitudeView, bool) {
	if !a.Complete() {
		return aptitudeView{}, false
	}
	v := aptitudeView{
		Surface:  make(map[string]string, 2),
		Distance: make(map[string]string, 4),
		Style:    make(map[string]string, 4),
	}
	for k, label := range surfaceZh {
		v.Surface[label] = a.Surface[k]
	}
	for k, label := range distanceZh {
		v.Distance[label] = a.Distance[k]
	}
	for k, label := range styleZh {
		v.Style[label] = a.RunningStyle[k]
	}
	return v, true
}

// card builds the aptitude card projection for one character.
func (ctx *compileContext) aptitudeCard(c *Character) (aptitudeCard, bool) {
	v, ok := toAptitudeView(c.Aptitudes)
	if !ok {
		return aptitudeCard{}, false
	}
	return aptitudeCard{
		CharacterID: c.ID,
		Name:        c.Names.ZhHans,
		NameJp:      c.Names.Ja,
		ImagePath:   c.ImagePath,
		Aptitudes:   v,
	}, true
}

// playableCharacters returns the records that are real, in-game characters.
func (ctx *compileContext) playableCharacters() []*Character {
	var out []*Character
	for i := range ctx.ds.Characters {
		c := &ctx.ds.Characters[i]
		if c.EntityKind == EntityPlayable {
			out = append(out, c)
		}
	}
	return out
}
