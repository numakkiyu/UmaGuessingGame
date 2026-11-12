// Package content normalises read-only source data, checks mode admission and atomically publishes versioned packs.
// Public media and server-only answers occupy separate directory trees.
package content

import (
	"encoding/json"
	"fmt"
	"github.com/bhcn/umaguessinggame/internal/characternames"
	"sort"
)

// SchemaVersion is the canonical model + pack layout version. It changes when a
// field's meaning changes, not when data changes.
const SchemaVersion = "1.0.0"

// Confidence records how well a fact is evidenced .
type Confidence string

const (
	// ConfidenceConfirmed means a primary source states the value directly.
	ConfidenceConfirmed Confidence = "confirmed"
	// ConfidenceInferred means the value was derived or cross-guessed and a
	// human has not confirmed it. Inferred values must not enter competitive
	// packs without explicit review .
	ConfidenceInferred Confidence = "inferred"
	// ConfidenceUnknown means the value is absent. Unknown is null, never zero
	// .
	ConfidenceUnknown Confidence = "unknown"
)

// Valid reports whether the confidence is one the schema accepts.
func (c Confidence) Valid() bool {
	return c == ConfidenceConfirmed || c == ConfidenceInferred || c == ConfidenceUnknown
}

// ReviewStatus is the human review state of a pack or a fact group .
type ReviewStatus string

const (
	// ReviewPending means no human has signed off yet.
	ReviewPending ReviewStatus = "pending"
	// ReviewPartial means some groups were reviewed, others were not.
	ReviewPartial ReviewStatus = "partial"
	// ReviewApproved means every published group was reviewed.
	ReviewApproved ReviewStatus = "approved"
)

// EntityKind separates a playable character from a non-playable one. The raw
// snapshot's implementation.status must not be read as playability .
type EntityKind string

const (
	// EntityPlayable is a character with complete in-game data.
	EntityPlayable EntityKind = "playable"
	// EntityPlanned is announced but has no in-game data yet.
	EntityPlanned EntityKind = "planned"
	// EntityNPC is a non-playable cast member (trainer, staff, mascot).
	EntityNPC EntityKind = "npc"
	// EntityUnknownKind means the scrape did not establish the kind.
	EntityUnknownKind EntityKind = "unknown"
)

// PlayableStatus is the content-admission verdict for one character, decided by
// this compiler rather than by the raw status string .
type PlayableStatus string

const (
	// PlayableReady passed every classic field check and may appear as an
	// answer.
	PlayableReady PlayableStatus = "ready"
	// PlayableBlocked is a real character that is missing required fields.
	PlayableBlocked PlayableStatus = "blocked"
	// PlayableNotPlayable has no in-game character data at all.
	PlayableNotPlayable PlayableStatus = "not_playable"
)

// SourceRef is one provenance record for a canonical fact .
type SourceRef struct {
	Site        string `json:"site"`
	Page        string `json:"page,omitempty"`
	URL         string `json:"url,omitempty"`
	RetrievedAt string `json:"retrievedAt,omitempty"`
}

// Alias is one searchable name with its locale and confidence.
type Alias struct {
	Text       string     `json:"text"`
	Locale     string     `json:"locale"`
	Confidence Confidence `json:"confidence"`
}

// Names holds the canonical name slots. Missing slots are empty strings; the
// confidence fields say whether a present value may be trusted .
type Names struct {
	Ja       string `json:"ja"`
	ZhHans   string `json:"zhHans"`
	ZhHant   string `json:"zhHant"`
	En       string `json:"en"`
	Romaji   string `json:"romaji,omitempty"`
	Nickname string `json:"nicknameJp,omitempty"`

	// JaConfidence is inferred for the five characters whose Japanese name was
	// guessed from the prototype horse name .
	JaConfidence Confidence `json:"jaConfidence"`
	JaSource     string     `json:"jaSource,omitempty"`
	// ZhHantSource records which cross-check produced the traditional name.
	ZhHantSource string `json:"zhHantSource,omitempty"`
}

// Validate rejects a character whose mandatory identity slots are empty.
func (n Names) Validate() error {
	if n.ZhHans == "" {
		return fmt.Errorf("content: character has no zhHans name")
	}
	if !n.JaConfidence.Valid() {
		return fmt.Errorf("content: invalid jaConfidence %q", n.JaConfidence)
	}
	return nil
}

// Profile is the non-mechanical biography block.
type Profile struct {
	BirthdayMonth *int   `json:"birthMonth"`
	BirthdayDay   *int   `json:"birthDay"`
	HeightCm      *int   `json:"heightCm"`
	CV            string `json:"cv,omitempty"`
	SchoolGrade   string `json:"schoolGrade,omitempty"`
	Dormitory     string `json:"dormitory,omitempty"`
	Star          *int   `json:"star"`
	SexType       string `json:"sexType,omitempty"`
	Introduction  string `json:"introduction,omitempty"`
}

// Aptitudes holds the raw A–G grades from the game data block. These are a
// different field from the legacy *_group buckets and must not be substituted
// for them without review .
type Aptitudes struct {
	Surface      map[string]string `json:"surface,omitempty"`
	Distance     map[string]string `json:"distance,omitempty"`
	RunningStyle map[string]string `json:"runningStyle,omitempty"`
}

// Complete reports whether all ten graded keys are present and in A–G.
func (a Aptitudes) Complete() bool {
	keys := []struct {
		m    map[string]string
		name []string
	}{
		{a.Surface, []string{"turf", "dirt"}},
		{a.Distance, []string{"sprint", "mile", "medium", "long"}},
		{a.RunningStyle, []string{"front", "pace", "late", "end"}},
	}
	for _, g := range keys {
		for _, k := range g.name {
			v, ok := g.m[k]
			if !ok || !validGrade(v) {
				return false
			}
		}
	}
	return true
}

// CountA counts A grades in one group.
func (a Aptitudes) CountA(group string) int {
	var m map[string]string
	switch group {
	case "surface":
		m = a.Surface
	case "distance":
		m = a.Distance
	case "runningStyle":
		m = a.RunningStyle
	default:
		return 0
	}
	n := 0
	for _, v := range m {
		if v == "A" {
			n++
		}
	}
	return n
}

func validGrade(v string) bool {
	return len(v) == 1 && v[0] >= 'A' && v[0] <= 'G'
}

// LegacyGroups holds the nine classic comparison fields exactly as reviewed in
// the legacy question bank . They are kept verbatim: the new aptitude
// grades are not a substitute for these buckets .
type LegacyGroups struct {
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
}

// Missing lists the classic nine fields that are absent or empty.
func (g LegacyGroups) Missing() []string {
	var out []string
	if g.Star == nil {
		out = append(out, "star")
	}
	if len(g.SurfaceGroup) == 0 {
		out = append(out, "surfaceGroup")
	}
	if len(g.DistanceGroup) == 0 {
		out = append(out, "distanceGroup")
	}
	if len(g.RunningStyleGroup) == 0 {
		out = append(out, "runningStyleGroup")
	}
	if g.SexType == "" {
		out = append(out, "sexType")
	}
	if g.G1Bracket == "" {
		out = append(out, "g1Bracket")
	}
	if g.G23Bracket == "" {
		out = append(out, "g23Bracket")
	}
	if g.SchoolGrade == "" {
		out = append(out, "schoolGrade")
	}
	if g.Dormitory == "" {
		out = append(out, "dormitory")
	}
	return out
}

// Signature is the deterministic comparison key of the nine fields. Two
// characters sharing a signature are indistinguishable to the classic mode and
// are an ambiguity vector .
func (g LegacyGroups) Signature() string {
	star := "?"
	if g.Star != nil {
		star = fmt.Sprintf("%d", *g.Star)
	}
	return fmt.Sprintf("%s|%s|%s|%s|%s|%s|%s|%s|%s",
		star,
		joinSorted(g.SurfaceGroup),
		joinSorted(g.DistanceGroup),
		joinSorted(g.RunningStyleGroup),
		g.SexType, g.G1Bracket, g.G23Bracket, g.SchoolGrade, g.Dormitory)
}

func joinSorted(v []string) string {
	if len(v) == 0 {
		return ""
	}
	c := append([]string(nil), v...)
	sort.Strings(c)
	return fmt.Sprintf("%v", c)
}

// Outfit is one costume variant. Outfits are not separate answers; they carry
// their own identity so a variant is never mistaken for a character .
type Outfit struct {
	OutfitID       string `json:"outfitId"`
	CharacterID    string `json:"characterId"`
	NicknameJp     string `json:"nicknameJp"`
	NicknameZhHans string `json:"nicknameZhHans,omitempty"`
	NicknameZhHant string `json:"nicknameZhHant,omitempty"`
	Title          string `json:"title"`
	Type           string `json:"type"`
	Star           *int   `json:"star"`
	Implemented    bool   `json:"implemented"`
}

// RaceEntry is one prototype-horse start in one race.
type RaceEntry struct {
	RaceID   string `json:"raceId"`
	RaceName string `json:"name"`
	// CharacterID is the canonical character id whose prototype ran here. It is
	// required by race-rebuild and co-race-path, so an entry without it is
	// dropped rather than published without identity.
	CharacterID string `json:"characterId"`
	// CharacterName is the display name, carried so a client need not join.
	CharacterName string `json:"characterName,omitempty"`
	Date          string `json:"date"` // ISO YYYY-MM-DD after normalisation
	DateRaw       string `json:"dateRaw,omitempty"`
	Course        string `json:"course"`
	Surface       string `json:"surface,omitempty"`
	Grade         string `json:"grade,omitempty"`
	Distance      string `json:"distance,omitempty"`
	FinishRank    *int   `json:"finishRank"`
}

// Race groups entries by raceId .
type Race struct {
	RaceID   string      `json:"raceId"`
	Name     string      `json:"name"`
	Date     string      `json:"date"`
	DateRaw  string      `json:"dateRaw,omitempty"`
	Course   string      `json:"course"`
	Surface  string      `json:"surface,omitempty"`
	Grade    string      `json:"grade,omitempty"`
	Distance string      `json:"distance,omitempty"`
	Region   string      `json:"region,omitempty"`
	Entries  []RaceEntry `json:"entries"`
}

// Pedigree is the prototype horse's immediate bloodline. Missing slots stay
// empty; pedigree mode must not infer relations from a shared surname .
type Pedigree struct {
	Sire     string `json:"sire,omitempty"`
	Dam      string `json:"dam,omitempty"`
	SireSire string `json:"sireSire,omitempty"`
	SireDam  string `json:"sireDam,omitempty"`
	DamSire  string `json:"damSire,omitempty"`
	DamDam   string `json:"damDam,omitempty"`
}

// Complete reports whether all six immediate ancestors are recorded.
func (p Pedigree) Complete() bool {
	return p.Sire != "" && p.Dam != "" && p.SireSire != "" && p.SireDam != "" && p.DamSire != "" && p.DamDam != ""
}

// PrototypeHorse is the real racehorse a character is based on. It is kept
// separate from the fictional character: they are different entities .
type PrototypeHorse struct {
	NKBID       string      `json:"nkbId,omitempty"`
	Names       Names       `json:"names"`
	Gender      string      `json:"gender,omitempty"`
	BirthDate   string      `json:"birthDate,omitempty"`
	Owner       string      `json:"owner,omitempty"`
	Trainer     string      `json:"trainer,omitempty"`
	Farm        string      `json:"farm,omitempty"`
	Nation      string      `json:"nation,omitempty"`
	RaceResult  string      `json:"raceResult,omitempty"`
	MainWinning string      `json:"mainWinning,omitempty"`
	Glory       string      `json:"glory,omitempty"`
	Earnings    string      `json:"earnings,omitempty"`
	Pedigree    Pedigree    `json:"pedigree"`
	RaceCount   int         `json:"raceCount"`
	Sources     []SourceRef `json:"sources,omitempty"`
}

// HasNKBID reports whether the prototype has an independent NetKeiba identity.
// 34 of 134 prototype blocks lack it .
func (p *PrototypeHorse) HasNKBID() bool { return p != nil && p.NKBID != "" }

// Goal is one training objective. The scrape only ever yields indices 1–7, so
// the list must never be presented as the complete goal set .
type Goal struct {
	Index     int    `json:"index"`
	Name      string `json:"name"`
	Time      string `json:"time,omitempty"`
	Condition string `json:"condition,omitempty"`
	Race      string `json:"race,omitempty"`
}

// Secret is one trivia secret usable as a bluff fact only after review.
type Secret struct {
	Text   string `json:"text"`
	Source string `json:"source,omitempty"`
}

// Character is the canonical record the whole pipeline builds on.
type Character struct {
	// ID is the new opaque numeric id (e.g. "1001"). Never assume numeric.
	ID string `json:"id"`
	// LegacyIDs are the old romanised ids still used by the legacy question
	// bank and by public URLs. The mapping is preserved both ways .
	LegacyIDs []string `json:"legacyIds,omitempty"`
	Slug      string   `json:"slug,omitempty"`

	SearchNames *characternames.Names `json:"searchNames,omitempty"`
	Names       Names                 `json:"names"`
	Aliases     []Alias               `json:"aliases,omitempty"`
	Profile     Profile               `json:"profile"`
	Aptitudes   Aptitudes             `json:"aptitudes"`

	// Groups is the reviewed legacy nine-field vector, present only for
	// characters that passed the legacy bank's field check.
	Groups *LegacyGroups `json:"groups,omitempty"`

	EntityKind     EntityKind     `json:"entityKind"`
	PlayableStatus PlayableStatus `json:"playableStatus"`
	// BlockReason explains why a character is not usable as an answer.
	BlockReason   string   `json:"blockReason,omitempty"`
	MissingFields []string `json:"missingFields,omitempty"`

	Outfits   []Outfit        `json:"outfits,omitempty"`
	Prototype *PrototypeHorse `json:"prototype,omitempty"`
	Goals     []Goal          `json:"goals,omitempty"`
	Secrets   []Secret        `json:"secrets,omitempty"`

	// ImagePath is the server-controlled asset route. It is null when the asset
	// is not verified to exist on disk .
	ImagePath *string `json:"imagePath"`
	// RemoteImageURL is the upstream CDN URL for this character's icon, or empty
	// when the snapshot carries none.
	//
	// It exists so a deployment can serve media from the CDN and keep only a small
	// local fallback: the client tries the remote URL first and falls back to
	// ImagePath. Both are published because a LAN/offline event must still work
	// .
	RemoteImageURL string `json:"remoteImageUrl,omitempty"`
	AssetID        string `json:"assetId,omitempty"`

	Sources    []SourceRef `json:"sources,omitempty"`
	Confidence Confidence  `json:"confidence"`

	// ReviewNotes carries human-facing caveats that must reach the quality
	// report instead of being silently dropped.
	ReviewNotes []string `json:"reviewNotes,omitempty"`
}

// Validate rejects a character that cannot be published at all.
func (c Character) Validate() error {
	if c.ID == "" {
		return fmt.Errorf("content: character with empty id")
	}
	if err := c.Names.Validate(); err != nil {
		return fmt.Errorf("content: character %s: %w", c.ID, err)
	}
	if !c.EntityKind.Valid() {
		return fmt.Errorf("content: character %s: invalid entityKind %q", c.ID, c.EntityKind)
	}
	if !c.PlayableStatus.Valid() {
		return fmt.Errorf("content: character %s: invalid playableStatus %q", c.ID, c.PlayableStatus)
	}
	if !c.Confidence.Valid() {
		return fmt.Errorf("content: character %s: invalid confidence %q", c.ID, c.Confidence)
	}
	return nil
}

// Valid reports whether the entity kind is one the schema accepts.
func (k EntityKind) Valid() bool {
	switch k {
	case EntityPlayable, EntityPlanned, EntityNPC, EntityUnknownKind:
		return true
	}
	return false
}

// Valid reports whether the playable status is one the schema accepts.
func (s PlayableStatus) Valid() bool {
	switch s {
	case PlayableReady, PlayableBlocked, PlayableNotPlayable:
		return true
	}
	return false
}

// Dataset is the whole canonical model plus the indexes modes read.
type Dataset struct {
	SchemaVersion  string `json:"schemaVersion"`
	DatasetVersion string `json:"datasetVersion"`
	GeneratedAt    string `json:"generatedAt"`
	Locale         string `json:"locale"`
	Region         string `json:"region"`

	Characters []Character `json:"characters"`
	Outfits    []Outfit    `json:"outfits"`
	Races      []Race      `json:"races"`

	// ByLegacyID maps a legacy romanised id to the canonical id.
	ByLegacyID map[string]string `json:"byLegacyId"`
	// ByNumericID maps the canonical id back to its legacy romanised id.
	ByNumericID map[string]string `json:"byNumericId"`

	Sources []SourceSnapshot `json:"sources"`
}

// SourceSnapshot records one raw input file with its hash so a pack can be tied
// to the exact scrape it was built from .
type SourceSnapshot struct {
	Name      string `json:"name"`
	Path      string `json:"path"`
	SHA256    string `json:"sha256"`
	SizeBytes int64  `json:"sizeBytes"`
}

// CharacterByID returns the character with the given canonical id.
func (d *Dataset) CharacterByID(id string) (*Character, bool) {
	for i := range d.Characters {
		if d.Characters[i].ID == id {
			return &d.Characters[i], true
		}
	}
	return nil, false
}

// CanonicalID resolves either a canonical id or a legacy romanised id.
func (d *Dataset) CanonicalID(idOrLegacy string) (string, bool) {
	if _, ok := d.CharacterByID(idOrLegacy); ok {
		return idOrLegacy, true
	}
	if v, ok := d.ByLegacyID[idOrLegacy]; ok {
		return v, true
	}
	return "", false
}

// MarshalCanonical renders the dataset as stable, indented JSON.
func (d *Dataset) MarshalCanonical() ([]byte, error) {
	b, err := json.MarshalIndent(d, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("content: marshal dataset: %w", err)
	}
	return append(b, '\n'), nil
}
