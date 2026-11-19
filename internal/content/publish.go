package content

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/bhcn/umaguessinggame/internal/assets"
)

// Pack layout under content/ :
//
//	content/<version>/
//
// manifest.json publication manifest + per-file sha256
// public/ servable over HTTP (cards, name index, assets index)
// private/ server-only (answers, secret material)
//
// Public and private are separate trees by construction: nothing under
// private/ is ever reachable from the public document root, and the answer
// material is never written under web/ or a Vite public directory.
const (
	ManifestName    = "manifest.json"
	PublicDirName   = "public"
	PrivateDirName  = "private"
	CurrentFileName = "current"
)

// Manifest is the publication record. Every field named in is present.
type Manifest struct {
	SchemaVersion        string           `json:"schemaVersion"`
	DatasetVersion       string           `json:"datasetVersion"`
	ContentPackVersion   string           `json:"contentPackVersion"`
	GeneratedAt          string           `json:"generatedAt"`
	SourceSnapshotHashes []SourceSnapshot `json:"sourceSnapshotHashes"`
	Locale               string           `json:"locale"`
	Region               string           `json:"region"`
	ReviewStatus         ReviewStatus     `json:"reviewStatus"`
	EntityCounts         EntityCounts     `json:"entityCounts"`
	Assets               AssetSummary     `json:"assets"`
	// SHA256 is the checksum over the manifest body, excluding this field.
	SHA256 string `json:"sha256"`

	// Modes is the per-mode admission verdict. It is part of the manifest so a
	// server can refuse to open an unavailable mode without reading the pack.
	Modes []ModeAdmission `json:"modes"`
	// Files lists every published file with its own checksum.
	Files []FileChecksum `json:"files"`
	// PublicSections and PrivateSections name the sections each tree carries.
	PublicSections  []string `json:"publicSections"`
	PrivateSections []string `json:"privateSections"`
}

// EntityCounts are the recomputed entity totals. They are computed from the
// published pack, not copied from any snapshot's self-reported meta .
type EntityCounts struct {
	Characters         int `json:"characters"`
	CharactersPlayable int `json:"charactersPlayable"`
	CharactersPlanned  int `json:"charactersPlanned"`
	CharactersNPC      int `json:"charactersNpc"`
	ClassicReady       int `json:"classicReady"`
	ClassicBlocked     int `json:"classicBlocked"`
	AmbiguousVectors   int `json:"ambiguousVectors"`
	Outfits            int `json:"outfits"`
	Races              int `json:"races"`
	RaceEntries        int `json:"raceEntries"`
	PrototypeHorses    int `json:"prototypeHorses"`
	PrototypeWithNKB   int `json:"prototypeWithNkbId"`
	Goals              int `json:"goals"`
	Secrets            int `json:"secrets"`
	Aliases            int `json:"aliases"`
	Voices             int `json:"voices"`
}

// AssetSummary is the verified state of both avatar sources. The two sets are
// reported separately because they disagree completely: the legacy manifest
// declares paths that do not exist, while the canonical directory holds verified avatar files.
type AssetSummary struct {
	// Legacy manifest set, from data/assets/asset-manifest.json.
	ManifestEntries         int `json:"manifestEntries"`
	ManifestMissingOnDisk   int `json:"manifestMissingOnDisk"`
	CacheIndexEntries       int `json:"cacheIndexEntries"`
	CacheIndexMissingOnDisk int `json:"cacheIndexMissingOnDisk"`
	OnlyInManifest          int `json:"onlyInManifest"`
	OnlyInCacheIndex        int `json:"onlyInCacheIndex"`

	// Canonical avatar files verified in the tracked data directory
	StagingDeclared int      `json:"stagingDeclared"`
	StagingVerified int      `json:"stagingVerified"`
	StagingMissing  []string `json:"stagingMissing,omitempty"`
	StagingOrphans  []string `json:"stagingOrphans,omitempty"`

	// VerifiedOnDisk is the number of characters with an avatar confirmed by
	// os.Stat, counted after both sources were tried. This is the figure the
	// memory mode is admitted on.
	VerifiedOnDisk int `json:"verifiedOnDisk"`
	// UniqueImages is the number of distinct digests among the verified files. A
	// pair board needs visually distinct images, so duplicates would shrink the
	// usable pool .
	UniqueImages int `json:"uniqueImages"`
	// Source names which avatar set supplied the published files.
	Source string `json:"source"`
	// CopiedIntoPack counts verified avatar files copied into the public content tree.
	CopiedIntoPack int `json:"copiedIntoPack"`
	// CopyrightNotice is the attribution a site must display.
	CopyrightNotice string `json:"copyrightNotice"`
}

// FileChecksum is one published file and its digest.
type FileChecksum struct {
	Path   string `json:"path"`
	Bytes  int64  `json:"bytes"`
	SHA256 string `json:"sha256"`
	// Visibility says whether the file is in the public or private tree.
	Visibility string `json:"visibility"`
}

// computeManifestChecksum hashes the manifest with its SHA256 field cleared, so
// the checksum covers every other field .
func computeManifestChecksum(m *Manifest) (string, error) {
	clone := *m
	clone.SHA256 = ""
	b, err := json.Marshal(clone)
	if err != nil {
		return "", fmt.Errorf("content: marshal manifest for checksum: %w", err)
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:]), nil
}

// VerifyManifest recomputes the checksum and rejects a drifted manifest. A
// mismatch means the file was modified after publication .
func VerifyManifest(m *Manifest) error {
	if m.SchemaVersion != SchemaVersion {
		return fmt.Errorf("content: manifest schemaVersion %q does not match supported %q", m.SchemaVersion, SchemaVersion)
	}
	if m.ContentPackVersion == "" || m.DatasetVersion == "" {
		return fmt.Errorf("content: manifest is missing a version field")
	}
	if m.SHA256 == "" {
		return fmt.Errorf("content: manifest has no checksum")
	}
	want, err := computeManifestChecksum(m)
	if err != nil {
		return err
	}
	if want != m.SHA256 {
		return fmt.Errorf("content: manifest checksum mismatch: declared %s, recomputed %s", m.SHA256, want)
	}
	return nil
}

// PublishOptions controls a build-and-publish run.
type PublishOptions struct {
	// Root is the repository root.
	Root string
	// OutputDir is the content directory to publish into (default content/).
	OutputDir string
	// Now is the generation timestamp; injected for deterministic tests.
	Now time.Time
	// DatasetVersion overrides the computed dataset version when non-empty.
	DatasetVersion string
	// DryRun builds everything but publishes nothing.
	DryRun bool
	// KeepVersions prunes older published versions beyond this count. Zero keeps
	// every version, because an archived event may still reference one.
	KeepVersions int
}

// PublishResult reports what a run produced.
type PublishResult struct {
	Version    string          `json:"version"`
	Path       string          `json:"path"`
	Manifest   *Manifest       `json:"manifest"`
	Admissions []ModeAdmission `json:"modes"`
	Report     *QualityReport  `json:"quality"`

	// sections holds the public payloads; privateSections holds the answer
	// material that must never be served. They are unexported so a caller cannot
	// accidentally serialise the secrets into a log or a public response.
	sections        map[string]any
	privateSections map[string]any
	// avatars are the files verified on disk that must be copied into the pack.
	avatars map[string]verifiedAvatar
	// memoryPairs maps each published memory pair id to the character whose avatar
	// it shows, so one image copy per pair can be written.
	memoryPairs map[string]string
	// root is the repository root, needed to read the avatar source bytes.
	root string
}

// Build compiles the pack in memory without publishing.
func Build(opts PublishOptions) (*PublishResult, error) {
	now := opts.Now
	if now.IsZero() {
		now = time.Now().UTC()
	}
	generatedAt := now.UTC().Format(time.RFC3339)

	in, err := Load(DefaultSourcePaths(opts.Root))
	if err != nil {
		return nil, err
	}

	datasetVersion := opts.DatasetVersion
	if datasetVersion == "" {
		datasetVersion = computeDatasetVersion(in)
	}

	ds, joins, err := BuildDataset(in, generatedAt, datasetVersion, opts.Root)
	if err != nil {
		return nil, err
	}

	ast := verifyAssets(in, opts.Root, datasetVersion)
	admissions, sections, secrets, pairSources, err := compileModes(ds, ast, in, opts.Root)
	if err != nil {
		return nil, err
	}

	// Classic is the readiness gate: without a competitive classic pool the
	// publish must fail rather than ship a site whose default mode is dead
	// .
	if !modeAvailable(admissions, ModeClassic) {
		return nil, fmt.Errorf("content: classic pack is required for readiness but is unavailable")
	}

	report := buildQualityReport(ds, in, joins, ast, admissions)

	packVersion, err := computePackVersion(ds, admissions, sections, secrets)
	if err != nil {
		return nil, err
	}

	m := &Manifest{
		SchemaVersion:        SchemaVersion,
		DatasetVersion:       datasetVersion,
		ContentPackVersion:   packVersion,
		GeneratedAt:          generatedAt,
		SourceSnapshotHashes: ds.Sources,
		Locale:               ds.Locale,
		Region:               ds.Region,
		ReviewStatus:         ReviewPending,
		EntityCounts:         report.EntityCounts,
		Assets:               report.Assets,
		Modes:                admissions,
	}

	return &PublishResult{
		Version:         packVersion,
		Manifest:        m,
		Admissions:      admissions,
		Report:          report,
		sections:        sections,
		privateSections: secrets,
		avatars:         ast.byCharacter,
		memoryPairs:     pairSources,
		root:            opts.Root,
	}, nil
}

func modeAvailable(list []ModeAdmission, mode string) bool {
	for _, a := range list {
		if a.Mode == mode {
			return a.Available
		}
	}
	return false
}

// computeDatasetVersion derives a stable version from the source hashes, so the
// same inputs always produce the same dataset version .
func computeDatasetVersion(in *RawInputs) string {
	h := sha256.New()
	for _, s := range in.Snapshots {
		h.Write([]byte(s.Name))
		h.Write([]byte{0})
		h.Write([]byte(s.SHA256))
		h.Write([]byte{0})
	}
	return "ds-" + hex.EncodeToString(h.Sum(nil))[:16]
}

// computePackVersion derives the pack version from the dataset version plus the
// published content, so a change in either changes the version.
func computePackVersion(ds *Dataset, admissions []ModeAdmission, sections, secrets map[string]any) (string, error) {
	// Include the public name projection, which is written separately from the
	// mode sections. Corrections must change the immutable pack binding too.
	identitySections := make(map[string]any, len(sections)+1)
	for key, value := range sections {
		identitySections[key] = value
	}
	identitySections[PublicIndexSection] = buildPublicIndex(ds)
	h := sha256.New()
	h.Write([]byte(SchemaVersion))
	h.Write([]byte{0})
	h.Write([]byte(ds.DatasetVersion))
	h.Write([]byte{0})
	for _, a := range admissions {
		fmt.Fprintf(h, "%s|%t|%d|%s\n", a.Mode, a.Available, a.QuestionCount, a.Section)
	}
	// A correction to hidden grading facts must change the binding too; hashing
	// only public questions reused the old immutable pack after a rank fix.
	for _, tree := range []struct {
		name     string
		sections map[string]any
	}{{"public", identitySections}, {"private", secrets}} {
		h.Write([]byte(tree.name))
		h.Write([]byte{0})
		keys := make([]string, 0, len(tree.sections))
		for k := range tree.sections {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			b, err := json.Marshal(tree.sections[k])
			if err != nil {
				return "", fmt.Errorf("content: marshal %s section %q: %w", tree.name, k, err)
			}
			h.Write([]byte(k))
			h.Write([]byte{0})
			h.Write(b)
			h.Write([]byte{0})
		}
	}

	return "pack-" + hex.EncodeToString(h.Sum(nil))[:16], nil
}

// Publish builds and atomically publishes the pack. The new version is written
// to a temporary directory, verified, then renamed into place; on any failure the
// previous version stays live .
func Publish(opts PublishOptions) (*PublishResult, error) {
	res, err := Build(opts)
	if err != nil {
		return nil, err
	}
	if opts.DryRun {
		return res, nil
	}

	outDir := opts.OutputDir
	if outDir == "" {
		outDir = filepath.Join(opts.Root, "content")
	}
	if err := publishPack(outDir, res); err != nil {
		return nil, err
	}
	res.Path = filepath.Join(outDir, res.Version)
	return res, nil
}

// publishPack writes the pack to a temp directory, verifies every checksum, and
// renames it into place.
func publishPack(outDir string, res *PublishResult) error {
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return fmt.Errorf("content: create output dir: %w", err)
	}
	// The temp dir must be on the same filesystem as the target for rename to be
	// atomic.
	tmp, err := os.MkdirTemp(outDir, ".staging-"+res.Version+"-")
	if err != nil {
		return fmt.Errorf("content: create staging dir: %w", err)
	}
	cleanup := true
	defer func() {
		if cleanup {
			os.RemoveAll(tmp)
		}
	}()

	files, pubSections, privSections, err := writePackFiles(tmp, res)
	if err != nil {
		return err
	}
	res.Manifest.Files = files
	res.Manifest.PublicSections = pubSections
	res.Manifest.PrivateSections = privSections

	sum, err := computeManifestChecksum(res.Manifest)
	if err != nil {
		return err
	}
	res.Manifest.SHA256 = sum
	if err := writeJSONFile(filepath.Join(tmp, ManifestName), res.Manifest); err != nil {
		return err
	}

	// Verify from disk before publishing: a pack that fails its own checks must
	// never become current .
	if err := verifyPublishedPack(tmp); err != nil {
		return err
	}

	final := filepath.Join(outDir, res.Version)
	if _, err := os.Stat(final); err == nil {
		// Self-consistency alone is insufficient: two independently valid packs
		// must not bind different facts to the same immutable identity.
		if err := verifyPublishedPack(final); err != nil {
			return fmt.Errorf("content: existing version %s fails verification and blocks publish: %w", res.Version, err)
		}
		var existing Manifest
		mb, err := os.ReadFile(filepath.Join(final, ManifestName))
		if err != nil {
			return err
		}
		if err := json.Unmarshal(mb, &existing); err != nil {
			return err
		}
		// The quality report contains the build time; compare the game payloads
		// and media instead, allowing a later identical build to reuse the pack.
		payloads := make(map[string]FileChecksum, len(existing.Files))
		for _, f := range existing.Files {
			if f.Path != PublicDirName+"/quality-report.json" {
				payloads[f.Path] = f
			}
		}
		for _, f := range files {
			if f.Path == PublicDirName+"/quality-report.json" {
				continue
			}
			if old, ok := payloads[f.Path]; !ok || old != f {
				return fmt.Errorf("content: immutable version %s has different payload %s", res.Version, f.Path)
			}
			delete(payloads, f.Path)
		}
		if len(payloads) != 0 || existing.ContentPackVersion != res.Version || existing.DatasetVersion != res.Manifest.DatasetVersion {
			return fmt.Errorf("content: immutable version %s has different content binding", res.Version)
		}
		// Report the manifest actually stored, rather than the discarded build's
		// timestamp and checksum.
		*res.Manifest = existing
		cleanup = true
	} else if os.IsNotExist(err) {
		if err := os.Rename(tmp, final); err != nil {
			return fmt.Errorf("content: atomic rename: %w", err)
		}
		cleanup = false
	} else {
		return fmt.Errorf("content: inspect existing version: %w", err)
	}

	return writeCurrentPointer(outDir, res.Version)
}

// writeCurrentPointer records which version a server should load. It is written
// after the version directory is in place, so a reader either sees the old
// pointer with the old version or the new pointer with the new version.
func writeCurrentPointer(outDir, version string) error {
	tmp := filepath.Join(outDir, "."+CurrentFileName+".tmp")
	if err := os.WriteFile(tmp, []byte(version+"\n"), 0o644); err != nil {
		return fmt.Errorf("content: write current pointer: %w", err)
	}
	if err := os.Rename(tmp, filepath.Join(outDir, CurrentFileName)); err != nil {
		return fmt.Errorf("content: commit current pointer: %w", err)
	}
	return nil
}

// writePackFiles materialises the public and private trees and returns their
// checksums.
func writePackFiles(dir string, res *PublishResult) (files []FileChecksum, pub, priv []string, err error) {
	pubDir := filepath.Join(dir, PublicDirName)
	privDir := filepath.Join(dir, PrivateDirName)
	if err := os.MkdirAll(pubDir, 0o755); err != nil {
		return nil, nil, nil, fmt.Errorf("content: create public dir: %w", err)
	}
	if err := os.MkdirAll(privDir, 0o755); err != nil {
		return nil, nil, nil, fmt.Errorf("content: create private dir: %w", err)
	}

	// Public: the card/name/material indexes. No answer material is here.
	publicSections := res.Report.PublicSections
	for _, name := range sortedSectionNames(publicSections) {
		payload, ok := res.sections[name]
		if !ok {
			return nil, nil, nil, fmt.Errorf("content: public section %q missing from compiled output", name)
		}
		path := filepath.Join(pubDir, sectionFileName(name))
		if err := writeJSONFile(path, payload); err != nil {
			return nil, nil, nil, err
		}
		fc, err := checksumFile(path, PublicDirName)
		if err != nil {
			return nil, nil, nil, err
		}
		files = append(files, fc)
		pub = append(pub, name)
	}

	// The public character index is a projection with no secret fields, so a
	// client can search names without ever receiving answers .
	indexPath := filepath.Join(pubDir, sectionFileName(PublicIndexSection))
	if err := writeJSONFile(indexPath, res.Report.PublicIndex); err != nil {
		return nil, nil, nil, err
	}
	fc, err := checksumFile(indexPath, PublicDirName)
	if err != nil {
		return nil, nil, nil, err
	}
	files = append(files, fc)
	pub = append(pub, PublicIndexSection)

	// Private: the answer sets and their generation seeds. Server-only.
	for _, name := range sortedSectionNames(res.Report.PrivateSections) {
		payload, ok := res.privateSections[name]
		if !ok {
			return nil, nil, nil, fmt.Errorf("content: private section %q missing from compiled output", name)
		}
		path := filepath.Join(privDir, sectionFileName(name))
		if err := writeJSONFile(path, payload); err != nil {
			return nil, nil, nil, err
		}
		fc, err := checksumFile(path, PrivateDirName)
		if err != nil {
			return nil, nil, nil, err
		}
		files = append(files, fc)
		priv = append(priv, name)
	}

	// Copy audio into the public pack so playback does not depend on a third-party CDN
	audioFiles, audioCopied, aerr := copyAudioClips(pubDir, res.root, publishedAudioNames(res))
	if aerr != nil {
		return nil, nil, nil, aerr
	}
	files = append(files, audioFiles...)
	_ = audioCopied

	avatarFiles, copied, err := copyAvatars(pubDir, res)
	if err != nil {
		return nil, nil, nil, err
	}
	files = append(files, avatarFiles...)
	// The manifest holds a copy of the asset summary taken at Build time, so it
	// must be updated too or it would report zero copied files.
	res.Report.Assets.CopiedIntoPack = copied
	res.Manifest.Assets.CopiedIntoPack = copied

	// The quality report is public: it states the real gaps, so hiding it would
	// defeat its purpose .
	reportPath := filepath.Join(pubDir, "quality-report.json")
	if err := writeJSONFile(reportPath, res.Report); err != nil {
		return nil, nil, nil, err
	}
	fc, err = checksumFile(reportPath, PublicDirName)
	if err != nil {
		return nil, nil, nil, err
	}
	files = append(files, fc)

	sort.Slice(files, func(i, j int) bool { return files[i].Path < files[j].Path })
	sort.Strings(pub)
	sort.Strings(priv)
	return files, pub, priv, nil
}

// sectionFileName maps a dotted section name to a flat file name.
func sectionFileName(section string) string {
	return strings.ReplaceAll(section, ".", "_") + ".json"
}

func sortedSectionNames(in []string) []string {
	out := append([]string(nil), in...)
	sort.Strings(out)
	return out
}

func writeJSONFile(path string, v any) error {
	b, err := marshalDeterministic(v)
	if err != nil {
		return fmt.Errorf("content: marshal %s: %w", filepath.Base(path), err)
	}
	if err := os.WriteFile(path, b, 0o644); err != nil {
		return fmt.Errorf("content: write %s: %w", path, err)
	}
	return nil
}

func checksumFile(path, visibility string) (FileChecksum, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return FileChecksum{}, fmt.Errorf("content: read %s for checksum: %w", path, err)
	}
	sum := sha256.Sum256(b)
	rel, err := filepath.Rel(filepath.Dir(filepath.Dir(path)), path)
	if err != nil {
		rel = filepath.Base(path)
	}
	return FileChecksum{
		Path:       filepath.ToSlash(rel),
		Bytes:      int64(len(b)),
		SHA256:     hex.EncodeToString(sum[:]),
		Visibility: visibility,
	}, nil
}

// verifyPublishedPack re-reads a written pack and checks every digest. It is the
// gate that must pass before a version becomes current.
func verifyPublishedPack(dir string) error {
	mb, err := os.ReadFile(filepath.Join(dir, ManifestName))
	if err != nil {
		return fmt.Errorf("content: read manifest: %w", err)
	}
	var m Manifest
	if err := json.Unmarshal(mb, &m); err != nil {
		return fmt.Errorf("content: parse manifest: %w", err)
	}
	if err := VerifyManifest(&m); err != nil {
		return err
	}
	for _, f := range m.Files {
		full := filepath.Join(dir, filepath.FromSlash(f.Path))
		b, err := os.ReadFile(full)
		if err != nil {
			return fmt.Errorf("content: published file %s missing: %w", f.Path, err)
		}
		sum := sha256.Sum256(b)
		if got := hex.EncodeToString(sum[:]); got != f.SHA256 {
			return fmt.Errorf("content: published file %s checksum drift: declared %s, actual %s", f.Path, f.SHA256, got)
		}
	}
	return nil
}

// LoadPack reads a published pack from disk and verifies its manifest checksum.
// A drift is refused so a server never loads a partially modified pack
// .
func LoadPack(dir string) (*Manifest, error) {
	mb, err := os.ReadFile(filepath.Join(dir, ManifestName))
	if err != nil {
		return nil, fmt.Errorf("content: read manifest in %s: %w", dir, err)
	}
	var m Manifest
	if err := json.Unmarshal(mb, &m); err != nil {
		return nil, fmt.Errorf("content: parse manifest in %s: %w", dir, err)
	}
	if err := VerifyManifest(&m); err != nil {
		return nil, err
	}
	return &m, nil
}

// ResolveCurrent returns the directory of the currently published version.
func ResolveCurrent(outDir string) (string, error) {
	b, err := os.ReadFile(filepath.Join(outDir, CurrentFileName))
	if err != nil {
		return "", fmt.Errorf("content: read current pointer in %s: %w", outDir, err)
	}
	v := strings.TrimSpace(string(b))
	if v == "" {
		return "", fmt.Errorf("content: current pointer in %s is empty", outDir)
	}
	dir := filepath.Join(outDir, v)
	if st, err := os.Stat(dir); err != nil || !st.IsDir() {
		return "", fmt.Errorf("content: current pointer names missing version %q", v)
	}
	return dir, nil
}

// copyAvatars writes the verified avatar bytes into the pack's public tree and
// returns their checksums. Only files that were already confirmed on disk are
// copied, and each copy is re-hashed so a mid-build change cannot slip through.
//
// When the memory mode is available, each card face also gets its OWN copy of the
// image under a face-named file. Two faces of one pair therefore point at two
// different files with identical bytes, so neither the path nor the payload
// reveals the pairing .
func copyAvatars(pubDir string, res *PublishResult) ([]FileChecksum, int, error) {
	avatars := res.avatars
	if len(avatars) == 0 {
		return nil, 0, nil
	}

	avatarDir := filepath.Join(pubDir, filepath.FromSlash(PackAvatarDir))
	if err := os.MkdirAll(avatarDir, 0o755); err != nil {
		return nil, 0, fmt.Errorf("content: create avatar dir: %w", err)
	}

	ids := make([]string, 0, len(avatars))
	for id := range avatars {
		ids = append(ids, id)
	}
	sort.Strings(ids)

	var files []FileChecksum
	copied := 0
	// Encoding to WebP is measured, not assumed: when cwebp is unavailable the
	// pack keeps the PNG bytes and the report says so (assets.OptimiseReport).
	useWebP := assets.EncoderAvailable()
	ext := ".png"
	if useWebP {
		ext = ".webp"
	}
	for _, id := range ids {
		av := avatars[id]
		destName := id + ext
		fc, err := copyOrEncode(res.root, av, filepath.Join(avatarDir, destName),
			filepath.ToSlash(filepath.Join(PublicDirName, PackAvatarDir, destName)), useWebP)
		if err != nil {
			return nil, 0, err
		}
		files = append(files, fc)
		copied++
	}

	// One image copy per memory pair. Two cards of a pair share this one image,
	// which is what requires; the board layout is not in the file names.
	pairs := res.memoryPairs
	if len(pairs) > 0 {
		pairDir := filepath.Join(pubDir, filepath.FromSlash(PackMemoryPairDir))
		if err := os.MkdirAll(pairDir, 0o755); err != nil {
			return nil, 0, fmt.Errorf("content: create pair image dir: %w", err)
		}
		pairIDs := make([]string, 0, len(pairs))
		for pairID := range pairs {
			pairIDs = append(pairIDs, pairID)
		}
		sort.Strings(pairIDs)
		for _, pairID := range pairIDs {
			av, ok := avatars[pairs[pairID]]
			if !ok {
				return nil, 0, fmt.Errorf("content: memory pair %s references an unverified avatar", pairID)
			}
			fc, err := copyOrEncode(res.root, av, filepath.Join(pairDir, pairID+ext),
				filepath.ToSlash(filepath.Join(PublicDirName, PackMemoryPairDir, pairID+ext)), useWebP)
			if err != nil {
				return nil, 0, err
			}
			files = append(files, fc)
			copied++
		}
	}

	return files, copied, nil
}

// copyVerified copies one verified source file to dest and confirms the digest of
// the bytes actually written.
func copyVerified(root string, av verifiedAvatar, destPath, manifestPath string) (FileChecksum, error) {
	src, err := os.Open(filepath.Join(root, filepath.FromSlash(av.SourcePath)))
	if err != nil {
		return FileChecksum{}, fmt.Errorf("content: open avatar %s: %w", av.SourcePath, err)
	}
	defer src.Close()
	dst, err := os.Create(destPath)
	if err != nil {
		return FileChecksum{}, fmt.Errorf("content: create %s: %w", destPath, err)
	}
	h := sha256.New()
	n, copyErr := io.Copy(io.MultiWriter(dst, h), src)
	closeErr := dst.Close()
	if copyErr != nil {
		return FileChecksum{}, fmt.Errorf("content: copy avatar %s: %w", av.SourcePath, copyErr)
	}
	if closeErr != nil {
		return FileChecksum{}, fmt.Errorf("content: close %s: %w", destPath, closeErr)
	}
	got := hex.EncodeToString(h.Sum(nil))
	// The copy must be byte-identical to what was verified, otherwise the file
	// changed between the check and the copy.
	if got != av.SHA256 {
		return FileChecksum{}, fmt.Errorf("content: avatar %s changed during the build: verified %s, copied %s", av.SourcePath, av.SHA256, got)
	}
	return FileChecksum{
		Path:       manifestPath,
		Bytes:      n,
		SHA256:     got,
		Visibility: PublicDirName,
	}, nil
}

// copyOrEncode writes one verified source file into the pack, optionally
// re-encoding it to WebP.
//
// The fallback is deliberate: if cwebp fails on a particular file the compiler
// must not abort the whole build, because one unreadable source image should not
// block a release. It copies the original bytes instead and the file's manifest
// entry records the real digest of what was written.
func copyOrEncode(root string, av verifiedAvatar, destPath, manifestPath string, webp bool) (FileChecksum, error) {
	if !webp {
		return copyVerified(root, av, destPath, manifestPath)
	}
	srcPath := filepath.Join(root, filepath.FromSlash(av.SourcePath))
	res, err := assets.EncodeWebP(srcPath, destPath)
	if err != nil {
		// Fall back to the PNG bytes rather than failing the build. The manifest
		// still records a real checksum, so the pack stays verifiable.
		pngDest := strings.TrimSuffix(destPath, ".webp") + ".png"
		pngManifest := strings.TrimSuffix(manifestPath, ".webp") + ".png"
		return copyVerified(root, av, pngDest, pngManifest)
	}
	return FileChecksum{
		Path:   manifestPath,
		SHA256: res.SHA256,
		Bytes:  res.DestSize,
	}, nil
}

// copyAudioClips publishes the downloaded voice clips into the pack's public tree.
//
// Only files that exist and are non-empty are copied: the same rule the compiler uses
// to decide a clip is material. A missing source directory is not an error — a build
// with no downloaded clips simply publishes none, and the mode reports itself
// unavailable for the honest reason.
func copyAudioClips(pubDir, root string, publishedAudio map[string]bool) ([]FileChecksum, int, error) {
	srcDir := filepath.Join(root, filepath.FromSlash(audioDir))
	entries, err := os.ReadDir(srcDir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, 0, nil
		}
		return nil, 0, fmt.Errorf("read audio dir: %w", err)
	}

	destDir := filepath.Join(pubDir, filepath.FromSlash(audioPublicDir))
	if err := os.MkdirAll(destDir, 0o755); err != nil {
		return nil, 0, fmt.Errorf("create audio dir: %w", err)
	}

	var files []FileChecksum
	copied := 0
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		// The manifest is build bookkeeping, not a clip.
		if !strings.HasSuffix(name, ".webm") {
			continue
		}
		// Skip clips that no admitted route points at. The directory holds every
		// downloaded file (15k), but a pack only publishes the admitted ones; linking
		// the rest would grow the pack and slow every build for no served benefit.
		if !publishedAudio[name] {
			continue
		}
		src := filepath.Join(srcDir, name)
		b, err := os.ReadFile(src)
		if err != nil {
			return nil, 0, fmt.Errorf("read clip %s: %w", name, err)
		}
		if len(b) == 0 {
			// A zero-byte file is a failed download; publishing it would be a silent clip.
			continue
		}
		dest := filepath.Join(destDir, name)
		// Curated files are small; independent bytes keep a published pack immutable
		// even if the source gallery is later repaired or re-downloaded in place.
		if err := os.WriteFile(dest, b, 0o644); err != nil {
			return nil, 0, fmt.Errorf("write clip %s: %w", name, err)
		}
		// Checksum only the clips the pack actually publishes a route for.
		//
		// Hashing all fifteen thousand files on every build dominated the test suite
		// (91s for a single republish test) while proving nothing: a clip with no
		// admitted route is never fetched, so its digest protects nothing. The
		// published set is what the manifest needs to describe.
		if !publishedAudio[name] {
			continue
		}
		files = append(files, FileChecksum{
			Path:       filepath.ToSlash(filepath.Join(PublicDirName, audioPublicDir, name)),
			Bytes:      int64(len(b)),
			SHA256:     sha256Hex(b),
			Visibility: "public",
		})
		copied++
	}
	return files, copied, nil
}

// publishedAudioNames collects the file names the audio section actually publishes.
//
// It is derived from the compiled section rather than from the directory, so a clip
// that failed admission contributes no manifest entry and no hash work.
func publishedAudioNames(res *PublishResult) map[string]bool {
	out := make(map[string]bool)
	raw, ok := res.sections[SectionAudioClips]
	if !ok {
		return out
	}
	clips, ok := raw.([]audioClip)
	if !ok {
		return out
	}
	for _, c := range clips {
		if !c.Reviewed || !c.Decodable || !c.SpeakerVerified || c.AudioPath == "" || c.AudioPath != audioRouteFor(c.AudioURL) {
			continue
		}
		if i := strings.LastIndexByte(c.AudioPath, '/'); i >= 0 {
			out[c.AudioPath[i+1:]] = true
		}
	}
	return out
}
