package content

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// SourcePaths are the read-only inputs of the pipeline . Every path is
// relative to the repository root and is never written to.
type SourcePaths struct {
	Root string
}

// DefaultSourcePaths returns the paths used by cmd/content.
func DefaultSourcePaths(root string) SourcePaths { return SourcePaths{Root: root} }

func (p SourcePaths) questionBankReady() string {
	return filepath.Join(p.Root, "data", "question_bank_ready.json")
}
func (p SourcePaths) questionBankBlocked() string {
	return filepath.Join(p.Root, "data", "question_bank_blocked.json")
}
func (p SourcePaths) fieldOverrides() string {
	return filepath.Join(p.Root, "data", "manual", "field-overrides.json")
}

// audioReviews is the optional listening-review ledger for audio clips.
func (p SourcePaths) audioReviews() string {
	return filepath.Join(p.Root, "data", "manual", "audio-clips.review.json")
}

// bluffReviews is the optional human-review ledger for bluff facts.
func (p SourcePaths) bluffReviews() string {
	return filepath.Join(p.Root, "data", "manual", "bluff-facts.review.json")
}
func (p SourcePaths) wikiCharacters() string {
	return filepath.Join(p.Root, "data", "wiki", "v2", "characters.json")
}
func (p SourcePaths) wikiOutfits() string {
	return filepath.Join(p.Root, "data", "wiki", "v2", "outfits.json")
}
func (p SourcePaths) wikiMeta() string {
	return filepath.Join(p.Root, "data", "wiki", "v2", "meta.json")
}
func (p SourcePaths) wikiSkills() string { return filepath.Join(p.Root, "data", "wiki", "skills.json") }
func (p SourcePaths) assetManifest() string {
	return filepath.Join(p.Root, "data", "assets", "asset-manifest.json")
}
func (p SourcePaths) assetCacheIndex() string {
	return filepath.Join(p.Root, "data", "assets", "cache-index.json")
}
func (p SourcePaths) normalizedDir() string {
	return filepath.Join(p.Root, "data", "normalized", "characters")
}

// RawInputs is every decoded source artifact plus its snapshot hash.
type RawInputs struct {
	Ready      []RawQuestionBankEntry
	Blocked    []RawBlockedEntry
	Overrides  RawFieldOverrides
	Wiki       RawWikiSnapshot
	Outfits    RawOutfitFile
	Skills     RawSkillFile
	Assets     []RawAssetManifestEntry
	CacheIndex map[string]string
	Normalized []RawNormalizedCharacter
	// BluffReviews carries human review decisions for bluff-mode facts. It is
	// optional: without it every fact stays reviewStatus=pending and the mode reports
	// itself unavailable, which is the honest answer rather than publishing an
	// unreviewed claim as verified truth.
	BluffReviews RawBluffReviews
	// AudioReviews is the optional listening-review ledger for audio clips. Without it
	// no clip is reviewed, and the mode reports itself unavailable rather than posing a
	// clip nobody checked.
	AudioReviews RawAudioReviews

	// SelfReportedCounts is what the snapshot claims about itself. It is carried
	// so the report can show claim vs recomputed figure .
	SelfReportedCounts map[string]int

	Snapshots []SourceSnapshot
}

// LoadError reports a missing or malformed required input. A required input
// failing must abort the build so the previous version stays live .
type LoadError struct {
	Path   string
	Reason string
	Err    error
}

func (e *LoadError) Error() string {
	if e.Err != nil {
		return fmt.Sprintf("content: load %s: %s: %v", e.Path, e.Reason, e.Err)
	}
	return fmt.Sprintf("content: load %s: %s", e.Path, e.Reason)
}

func (e *LoadError) Unwrap() error { return e.Err }

// Load reads every required input. Any failure is fatal for the whole build:
// publishing a partially read dataset would silently drop facts.
func Load(p SourcePaths) (*RawInputs, error) {
	in := &RawInputs{}

	if err := readJSONHashed(p.questionBankReady(), "question_bank_ready", &in.Ready, &in.Snapshots); err != nil {
		return nil, err
	}
	if err := readJSONHashed(p.questionBankBlocked(), "question_bank_blocked", &in.Blocked, &in.Snapshots); err != nil {
		return nil, err
	}
	if err := readJSONHashed(p.fieldOverrides(), "field_overrides", &in.Overrides, &in.Snapshots); err != nil {
		return nil, err
	}
	// The bluff review ledger is optional by design: a build with no reviews must
	// still compile, it simply leaves the mode unavailable with an honest reason.
	if err := readOptionalJSON(p.bluffReviews(), "bluff_reviews", &in.BluffReviews, &in.Snapshots); err != nil {
		return nil, err
	}
	if err := readOptionalJSON(p.audioReviews(), "audio_reviews", &in.AudioReviews, &in.Snapshots); err != nil {
		return nil, err
	}
	if err := readJSONHashed(p.wikiCharacters(), "wiki_v2_characters", &in.Wiki, &in.Snapshots); err != nil {
		return nil, err
	}
	if err := readJSONHashed(p.wikiOutfits(), "wiki_v2_outfits", &in.Outfits, &in.Snapshots); err != nil {
		return nil, err
	}
	if err := readJSONHashed(p.wikiSkills(), "wiki_skills", &in.Skills, &in.Snapshots); err != nil {
		return nil, err
	}
	if err := readJSONHashed(p.assetManifest(), "asset_manifest", &in.Assets, &in.Snapshots); err != nil {
		return nil, err
	}
	if err := readJSONHashed(p.assetCacheIndex(), "asset_cache_index", &in.CacheIndex, &in.Snapshots); err != nil {
		return nil, err
	}

	// The v2 meta file is advisory only: its counts are stale by design .
	var meta RawWikiMeta
	if err := readJSONHashed(p.wikiMeta(), "wiki_v2_meta", &meta, &in.Snapshots); err != nil {
		return nil, err
	}
	in.SelfReportedCounts = meta.Counts

	norm, snap, err := loadNormalizedDir(p.normalizedDir())
	if err != nil {
		return nil, err
	}
	in.Normalized = norm
	in.Snapshots = append(in.Snapshots, snap)

	sort.Slice(in.Snapshots, func(i, j int) bool { return in.Snapshots[i].Name < in.Snapshots[j].Name })
	return in, nil
}

// sha256Hex returns the hex digest used in source snapshots.
func sha256Hex(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// RawAudioReviews is the human listening-review ledger for audio clips.
//
// The audio mode asks a player to identify a speaker by ear, so a clip must be one a
// human actually listened to and confirmed. This ledger is where that happens; it is
// deliberately a file a person edits, because the compiler cannot hear.
type RawAudioReviews struct {
	// ReviewedAt records when the listening pass happened.
	ReviewedAt string `json:"reviewedAt"`
	// Reviewer names who listened. An entry with no reviewer is not auditable and does
	// not count.
	Reviewer string `json:"reviewer"`
	// Clips lists the clips that were listened to.
	Clips []RawAudioReview `json:"clips"`
}

// RawAudioReview is one listened-to clip.
type RawAudioReview struct {
	ClipID             string `json:"clipId"`
	SHA256             string `json:"sha256"`
	SpeakerVerified    bool   `json:"speakerVerified"`
	TranscriptVerified bool   `json:"transcriptVerified"`
	NoNameSpoiler      bool   `json:"noNameSpoiler"`
	// Listened must be true. Absence means "not listened", so a malformed ledger
	// under-publishes rather than over-publishes.
	Listened bool `json:"listened"`
}

// RawBluffReviews is the human-review ledger for bluff-mode facts.
//
// The bluff mode presents one true fact among distractors, so a published fact is a
// claim of truth. The wiki's trivia entries are unverified prose, and publishing one
// without review would fabricate a verified claim . This ledger is where a
// human records that they read the fact, checked it against the cited source, and
// accepted it.
//
// It is deliberately a separate file that a human edits, not something the compiler
// can infer: the whole point is that a person looked at it.
type RawBluffReviews struct {
	// ReviewedAt records when the review pass happened, so a later reader can judge
	// whether it predates a change to the source snapshot.
	ReviewedAt string `json:"reviewedAt"`
	// Reviewer names who performed the review. A claim of review with no reviewer is
	// not auditable, so an entry without one is treated as unreviewed.
	Reviewer string `json:"reviewer"`
	// Facts lists the accepted facts. Only an explicit approved=true counts.
	Facts []RawBluffReview `json:"facts"`
}

// RawBluffReview is one accepted fact.
type RawBluffReview struct {
	FactID string `json:"factId"`
	// Approved must be true. An absent field means "not approved", so a malformed
	// ledger under-publishes rather than over-publishes.
	Approved bool `json:"approved"`
	// Source is the citation the reviewer verified against. The mode requires one.
	Source string `json:"source"`
	// Answer is the phrase the fact is about, in the fact's own wording. The mode
	// requires it to grade a ballot, and it must be copied by the reviewer rather than
	// guessed by the compiler: deriving it from the sentence would invent a claim.
	Answer string `json:"answer"`
	// FallbackAnswers are deliberately false, reviewed alternatives for missing submissions.
	FallbackAnswers []string `json:"fallbackAnswers,omitempty"`
}

// readOptionalJSON reads a source that is allowed to be absent.
//
// A missing file is not an error here: it means "no decision has been recorded yet",
// which the caller reports as unavailability rather than a build failure. A file that
// exists but cannot be parsed IS an error, because that is a broken ledger, not an
// empty one — silently ignoring it would hide malformed review data.
func readOptionalJSON(path, name string, dst any, snapshots *[]SourceSnapshot) error {
	b, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return &LoadError{Path: path, Reason: "unreadable", Err: err}
	}
	if err := json.Unmarshal(b, dst); err != nil {
		return &LoadError{Path: path, Reason: "invalid JSON", Err: err}
	}
	if snapshots != nil {
		*snapshots = append(*snapshots, SourceSnapshot{
			Name: name, Path: path, SizeBytes: int64(len(b)), SHA256: sha256Hex(b),
		})
	}
	return nil
}

func readJSONHashed(path, name string, dst any, snapshots *[]SourceSnapshot) error {
	b, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return &LoadError{Path: path, Reason: "required input missing"}
		}
		return &LoadError{Path: path, Reason: "unreadable", Err: err}
	}
	if err := json.Unmarshal(b, dst); err != nil {
		return &LoadError{Path: path, Reason: "invalid JSON", Err: err}
	}
	sum := sha256.Sum256(b)
	*snapshots = append(*snapshots, SourceSnapshot{
		Name:      name,
		Path:      filepath.ToSlash(path),
		SHA256:    hex.EncodeToString(sum[:]),
		SizeBytes: int64(len(b)),
	})
	return nil
}

// loadNormalizedDir reads the per-character normalised files and folds them into
// one snapshot hash so the pack records which directory state it used.
func loadNormalizedDir(dir string) ([]RawNormalizedCharacter, SourceSnapshot, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, SourceSnapshot{}, &LoadError{Path: dir, Reason: "required input missing"}
		}
		return nil, SourceSnapshot{}, &LoadError{Path: dir, Reason: "unreadable", Err: err}
	}
	var files []string
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		files = append(files, e.Name())
	}
	sort.Strings(files)

	out := make([]RawNormalizedCharacter, 0, len(files))
	h := sha256.New()
	var total int64
	for _, name := range files {
		full := filepath.Join(dir, name)
		b, err := os.ReadFile(full)
		if err != nil {
			return nil, SourceSnapshot{}, &LoadError{Path: full, Reason: "unreadable", Err: err}
		}
		var c RawNormalizedCharacter
		if err := json.Unmarshal(b, &c); err != nil {
			return nil, SourceSnapshot{}, &LoadError{Path: full, Reason: "invalid JSON", Err: err}
		}
		out = append(out, c)
		h.Write([]byte(name))
		h.Write([]byte{0})
		h.Write(b)
		h.Write([]byte{0})
		total += int64(len(b))
	}
	return out, SourceSnapshot{
		Name:      "normalized_characters_dir",
		Path:      filepath.ToSlash(dir),
		SHA256:    hex.EncodeToString(h.Sum(nil)),
		SizeBytes: total,
	}, nil
}

// ---------------------------------------------------------------------------
// Normalisation helpers
// ---------------------------------------------------------------------------

var isoDateRe = regexp.MustCompile(`^(\d{4})-(\d{2})-(\d{2})$`)
var jpDateRe = regexp.MustCompile(`^(\d{4})年(\d{1,2})月(\d{1,2})日$`)

// NormalizeDate converts the two date shapes actually present in the snapshot
// ("1997年11月29日" in raceHistory, "1995/05/02" in pedigree birth) into ISO.
// It returns "" when the input is not a recognised date: dates are never
// guessed, because sorting a literal string is a documented bug .
func NormalizeDate(raw string) string {
	s := strings.TrimSpace(raw)
	if s == "" {
		return ""
	}
	if isoDateRe.MatchString(s) {
		return s
	}
	if m := jpDateRe.FindStringSubmatch(s); m != nil {
		y, _ := strconv.Atoi(m[1])
		mo, _ := strconv.Atoi(m[2])
		d, _ := strconv.Atoi(m[3])
		if mo < 1 || mo > 12 || d < 1 || d > 31 {
			return ""
		}
		return fmt.Sprintf("%04d-%02d-%02d", y, mo, d)
	}
	if m := regexp.MustCompile(`^(\d{4})/(\d{1,2})/(\d{1,2})$`).FindStringSubmatch(s); m != nil {
		mo, _ := strconv.Atoi(m[2])
		d, _ := strconv.Atoi(m[3])
		if mo < 1 || mo > 12 || d < 1 || d > 31 {
			return ""
		}
		return fmt.Sprintf("%04d-%02d-%02d", atoiOr(m[1], 0), mo, d)
	}
	return ""
}

func atoiOr(s string, fallback int) int {
	v, err := strconv.Atoi(s)
	if err != nil {
		return fallback
	}
	return v
}

// jpCourseNames is the set of racecourses physically in Japan. Event dates in
// the sort mode need an explicit region, so a non-Japanese course must not be
// silently labelled "JP" .
var jpCourseNames = map[string]bool{
	"阪神": true, "京都": true, "中山": true, "东京": true, "中京": true,
	"札幌": true, "新潟": true, "笠松": true, "中京(地)": true, "名古屋": true,
	"函馆": true, "福岛": true, "川崎": true, "大井": true, "船桥": true,
	"盛冈": true, "小仓": true, "水泽": true, "金泽": true, "园田": true,
	"浦和": true, "佐贺": true, "门别": true, "高知": true, "上山": true,
}

// RegionForCourse maps a racecourse to its region tag, or "" when unknown.
func RegionForCourse(course string) string {
	if jpCourseNames[course] {
		return "JP"
	}
	return ""
}

func sortedKeys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func contains(haystack []string, needle string) bool {
	for _, s := range haystack {
		if s == needle {
			return true
		}
	}
	return false
}
