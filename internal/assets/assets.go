// Package assets optimises published media with WebP encoding and content-hash deduplication.
// Local files remain available when upstream image URLs cannot be reached.
package assets

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// WebPQuality is the encoder quality. 82 was chosen by measuring the real files:
// it keeps the character art legible at the sizes the UI renders (40-96 CSS px)
// while reaching the 75% reduction. Raising it costs bytes for no visible gain at
// those sizes; lowering it starts to smear the thin outlines.
const WebPQuality = 82

// AvatarWidth is the width the upstream CDN serves. BiliWiki's thumbnail API
// accepts 100 and 200 but returns 404 at 400, so 200 is the largest usable
// variant. The original file is 256x280.
const AvatarWidth = 200

// remoteIconPattern extracts the canonical character id from an upstream URL such
// as
//
//	.../200px-Chr_icon_1129_112901_01.png
//
// The first number after "Chr_icon_" is the character id used by the wiki and by
// the content pack, so the join is exact rather than fuzzy.
var remoteIconPattern = regexp.MustCompile(`Chr_icon_(\d+)_`)

// RemoteIndex maps a canonical character id to its upstream image URL.
type RemoteIndex map[string]string

// BuildRemoteIndex derives character id -> upstream URL from the asset manifest.
//
// The manifest is keyed by the legacy romanised id, but its source_url embeds the
// numeric character id, which is what the pack uses. Deriving the mapping from the
// URL avoids depending on a romanised-to-numeric table that does not exist in the
// snapshot.
func BuildRemoteIndex(manifestEntries []ManifestSource) RemoteIndex {
	idx := make(RemoteIndex, len(manifestEntries))
	for _, e := range manifestEntries {
		m := remoteIconPattern.FindStringSubmatch(e.SourceURL)
		if m == nil {
			continue
		}
		// Prefer the widest variant the CDN serves so the reference is usable at
		// every render size, including the 96 px task card.
		idx[m[1]] = widenRemoteURL(e.SourceURL)
	}
	return idx
}

// widenRemoteURL rewrites a "100px-" thumbnail path to the 200px variant.
//
// The snapshot stores the 100px URL. The UI renders avatars up to 96 CSS px, which
// is 192 device px on a 2x screen, so 100px would be visibly soft. 200px is the
// largest variant the CDN serves (400px returns 404), so it is the correct target.
func widenRemoteURL(u string) string {
	if strings.Contains(u, "/100px-") {
		return strings.Replace(u, "/100px-", "/200px-", 1)
	}
	return u
}

// ManifestSource is the subset of an asset-manifest entry this package needs.
type ManifestSource struct {
	AssetID     string
	CharacterID string
	SourceURL   string
	LocalPath   string
}

// EncodeResult reports what happened to one file.
type EncodeResult struct {
	SourcePath string
	DestPath   string
	SourceSize int64
	DestSize   int64
	SHA256     string
}

// EncodeWebP converts one PNG file to WebP at the package quality.
//
// It shells out to cwebp because the pure-Go encoders available in this project's
// dependency set do not implement the lossy WebP encoder, and adding a CGO
// dependency would break the single-binary, cross-architecture container build
// . cwebp is a build-time tool only: the runtime image never contains it.
func EncodeWebP(src, dst string) (EncodeResult, error) {
	info, err := os.Stat(src)
	if err != nil {
		return EncodeResult{}, fmt.Errorf("assets: stat %s: %w", src, err)
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return EncodeResult{}, fmt.Errorf("assets: mkdir for %s: %w", dst, err)
	}
	cmd := exec.Command("cwebp",
		"-q", strconv.Itoa(WebPQuality),
		"-quiet",
		"-metadata", "none", // drop EXIF/ICC: the source icons carry no useful metadata and it costs bytes
		src, "-o", dst,
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		return EncodeResult{}, fmt.Errorf("assets: cwebp %s: %w: %s", filepath.Base(src), err, strings.TrimSpace(string(out)))
	}
	destInfo, err := os.Stat(dst)
	if err != nil {
		return EncodeResult{}, fmt.Errorf("assets: stat %s: %w", dst, err)
	}
	if destInfo.Size() == 0 {
		return EncodeResult{}, fmt.Errorf("assets: cwebp produced an empty file for %s", filepath.Base(src))
	}
	digest, err := fileSHA256(dst)
	if err != nil {
		return EncodeResult{}, err
	}
	return EncodeResult{
		SourcePath: src,
		DestPath:   dst,
		SourceSize: info.Size(),
		DestSize:   destInfo.Size(),
		SHA256:     digest,
	}, nil
}

// DedupPlan describes how pair images reuse avatar bytes.
type DedupPlan struct {
	// Shared maps a pair image name (without extension) to the avatar character id
	// whose bytes it duplicates.
	Shared map[string]string
	// Unique lists pair names with no avatar twin; these need their own file.
	Unique []string
	// SavedBytes is the total size of the duplicate files that need not be stored.
	SavedBytes int64
}

// PlanPairDedup compares pair images against avatar images by content digest.
//
// The memory mode draws one image per pair, and that image is byte-identical to
// the character's avatar in the current snapshot. Storing both wastes roughly half
// the media budget, so the pack stores the avatar once and lets the pair route
// resolve to it. Dedup is by SHA-256, not by name, so a future snapshot where the
// images genuinely differ will not be silently merged.
func PlanPairDedup(avatarDir, pairDir string) (DedupPlan, error) {
	avatarByDigest, err := digestIndex(avatarDir)
	if err != nil {
		return DedupPlan{}, err
	}
	plan := DedupPlan{Shared: make(map[string]string)}

	entries, err := os.ReadDir(pairDir)
	if err != nil {
		if os.IsNotExist(err) {
			return plan, nil
		}
		return DedupPlan{}, fmt.Errorf("assets: read %s: %w", pairDir, err)
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if !e.IsDir() {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)

	for _, name := range names {
		full := filepath.Join(pairDir, name)
		info, statErr := os.Stat(full)
		if statErr != nil || info.IsDir() || info.Size() == 0 {
			continue
		}
		digest, derr := fileSHA256(full)
		if derr != nil {
			return DedupPlan{}, derr
		}
		base := strings.TrimSuffix(name, filepath.Ext(name))
		if characterID, ok := avatarByDigest[digest]; ok {
			plan.Shared[base] = characterID
			plan.SavedBytes += info.Size()
			continue
		}
		plan.Unique = append(plan.Unique, base)
	}
	return plan, nil
}

// digestIndex maps content digest -> first file name (without extension) in dir.
func digestIndex(dir string) (map[string]string, error) {
	out := make(map[string]string)
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return out, nil
		}
		return nil, fmt.Errorf("assets: read %s: %w", dir, err)
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if !e.IsDir() {
			names = append(names, e.Name())
		}
	}
	// Sort so the mapping is deterministic when two avatars share content.
	sort.Strings(names)
	for _, name := range names {
		full := filepath.Join(dir, name)
		info, statErr := os.Stat(full)
		if statErr != nil || info.IsDir() || info.Size() == 0 {
			continue
		}
		digest, derr := fileSHA256(full)
		if derr != nil {
			return nil, derr
		}
		if _, exists := out[digest]; !exists {
			out[digest] = strings.TrimSuffix(name, filepath.Ext(name))
		}
	}
	return out, nil
}

// fileSHA256 streams a file through SHA-256.
func fileSHA256(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("assets: open %s: %w", path, err)
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", fmt.Errorf("assets: hash %s: %w", path, err)
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// OptimiseReport is the machine-readable outcome, written into the pack manifest
// so an operator can see the real saving rather than a claim.
type OptimiseReport struct {
	// Format is the encoding that was published ("webp").
	Format string `json:"format"`
	// Quality is the encoder quality used.
	Quality int `json:"quality"`
	// AvatarsEncoded and PairsEncoded count files actually converted.
	AvatarsEncoded int `json:"avatarsEncoded"`
	PairsEncoded   int `json:"pairsEncoded"`
	// SourceBytes and PublishedBytes are the totals before and after encoding.
	SourceBytes    int64 `json:"sourceBytes"`
	PublishedBytes int64 `json:"publishedBytes"`
	// DedupedPairs counts pair images that reuse an avatar file.
	DedupedPairs int `json:"dedupedPairs"`
	// DedupSavedBytes is the media the pack did not need to store twice.
	DedupSavedBytes int64 `json:"dedupSavedBytes"`
	// RemoteReferences counts characters carrying an upstream CDN URL.
	RemoteReferences int `json:"remoteReferences"`
	// RemoteMissing counts characters with no upstream URL, which therefore always
	// use the local file.
	RemoteMissing int `json:"remoteMissing"`
	// EncoderAvailable reports whether cwebp was found. False means the pack kept
	// the PNG sources and the report says so instead of pretending to compress.
	EncoderAvailable bool `json:"encoderAvailable"`
	// Note carries a human-facing caveat when something was skipped.
	Note string `json:"note,omitempty"`
}

// SavingsPercent returns the measured reduction, or 0 when nothing was encoded.
func (r OptimiseReport) SavingsPercent() float64 {
	if r.SourceBytes == 0 {
		return 0
	}
	return float64(r.SourceBytes-r.PublishedBytes) / float64(r.SourceBytes) * 100
}

// EncoderAvailable reports whether cwebp is on PATH.
//
// The compiler must degrade gracefully: a machine without cwebp still produces a
// working pack (PNG), it just does not get the size saving. Failing the whole build
// because an optional optimiser is missing would be worse than shipping larger
// images.
func EncoderAvailable() bool {
	_, err := exec.LookPath("cwebp")
	return err == nil
}
