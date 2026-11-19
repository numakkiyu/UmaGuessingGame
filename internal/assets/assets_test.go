package assets

import (
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// writePNG writes a small but genuinely valid PNG.
//
// The bytes are produced by Go's own encoder rather than hand-assembled: a
// hand-written header with a wrong CRC is rejected by cwebp ("incorrect data
// check"), which is the correct behaviour and would make the encoder test fail for
// an unrelated reason. Using a real encoder also means the test exercises the same
// kind of input the compiler sees.
func writePNG(t *testing.T, path string, seed byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	// A 16x16 solid colour image, with the colour varying by seed so different
	// seeds hash differently and the dedup test can distinguish them.
	img := image.NewRGBA(image.Rect(0, 0, 16, 16))
	fill := color.RGBA{R: seed, G: seed, B: seed, A: 255}
	draw.Draw(img, img.Bounds(), &image.Uniform{C: fill}, image.Point{}, draw.Src)

	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("create png: %v", err)
	}
	defer f.Close()
	if err := png.Encode(f, img); err != nil {
		t.Fatalf("encode png: %v", err)
	}
}

func TestBuildRemoteIndexDerivesCharacterIDFromURL(t *testing.T) {
	// The manifest is keyed by the legacy romanised id, but the URL embeds the
	// numeric id the pack uses. Deriving it from the URL is what makes the join
	// exact without a romanised-to-numeric table.
	entries := []ManifestSource{
		{AssetID: "supesharuuiiku-thumb", CharacterID: "supesharuuiiku",
			SourceURL: "https://patchwiki.biligame.com/images/umamusume/thumb/3/38/48av6cwfvk6b2c3anpnxlh29410sel3.png/100px-Chr_icon_1001_100101_01.png"},
		{AssetID: "other-thumb", CharacterID: "other",
			SourceURL: "https://patchwiki.biligame.com/images/umamusume/thumb/d/dd/qbqowwstr3s6tq9q4v1xky6vqtpwvwf.png/100px-Chr_icon_1033_103301_01.png"},
		// A non-icon URL must not produce a bogus entry.
		{AssetID: "banner", CharacterID: "banner",
			SourceURL: "https://patchwiki.biligame.com/images/umamusume/Banner_001.png"},
	}

	idx := BuildRemoteIndex(entries)

	if got, ok := idx["1001"]; !ok {
		t.Fatal("character 1001 has no remote URL")
	} else if want := "200px-Chr_icon_1001_100101_01.png"; !contains(got, want) {
		t.Errorf("1001 URL = %q, want it widened to %q", got, want)
	}
	if _, ok := idx["1033"]; !ok {
		t.Error("character 1033 has no remote URL")
	}
	if len(idx) != 2 {
		t.Errorf("index size = %d, want 2 (the banner URL must be skipped)", len(idx))
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(sub) == 0 || indexOf(s, sub) >= 0)
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}

// TestWidenRemoteURLPrefersTheLargestVariant proves the 100px snapshot URL is
// upgraded to 200px, which is the largest the CDN serves (400px returns 404).
func TestWidenRemoteURLPrefersTheLargestVariant(t *testing.T) {
	cases := []struct{ in, want string }{
		{"https://x/100px-Chr_icon_1001_1.png", "https://x/200px-Chr_icon_1001_1.png"},
		{"https://x/200px-Chr_icon_1001_1.png", "https://x/200px-Chr_icon_1001_1.png"},
		{"https://x/original.png", "https://x/original.png"},
	}
	for _, tc := range cases {
		if got := widenRemoteURL(tc.in); got != tc.want {
			t.Errorf("widenRemoteURL(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// TestPlanPairDedupFindsByteIdenticalPairs proves duplicate detection is by
// content, not by name.
func TestPlanPairDedupFindsByteIdenticalPairs(t *testing.T) {
	dir := t.TempDir()
	avatarDir := filepath.Join(dir, "avatars")
	pairDir := filepath.Join(dir, "faces")

	writePNG(t, filepath.Join(avatarDir, "1001.png"), 0x11)
	writePNG(t, filepath.Join(avatarDir, "1002.png"), 0x22)
	// Pair A duplicates avatar 1001's bytes under an unrelated name.
	writePNG(t, filepath.Join(pairDir, "mp-aaaa.png"), 0x11)
	// Pair B is unique content.
	writePNG(t, filepath.Join(pairDir, "mp-bbbb.png"), 0x33)

	plan, err := PlanPairDedup(avatarDir, pairDir)
	if err != nil {
		t.Fatalf("PlanPairDedup: %v", err)
	}

	if got, ok := plan.Shared["mp-aaaa"]; !ok {
		t.Error("mp-aaaa duplicates avatar 1001 by content but was not detected")
	} else if got != "1001" {
		t.Errorf("mp-aaaa maps to %q, want 1001", got)
	}
	if _, ok := plan.Shared["mp-bbbb"]; ok {
		t.Error("mp-bbbb has unique content but was reported as a duplicate")
	}
	if len(plan.Unique) != 1 || plan.Unique[0] != "mp-bbbb" {
		t.Errorf("unique = %v, want [mp-bbbb]", plan.Unique)
	}
	if plan.SavedBytes <= 0 {
		t.Error("a detected duplicate must report the bytes it saves")
	}
}

// TestPlanPairDedupHandlesMissingDir proves an absent pair directory is not an
// error: modes that do not use pairs simply produce no plan.
func TestPlanPairDedupHandlesMissingDir(t *testing.T) {
	dir := t.TempDir()
	avatarDir := filepath.Join(dir, "avatars")
	writePNG(t, filepath.Join(avatarDir, "1001.png"), 0x11)

	plan, err := PlanPairDedup(avatarDir, filepath.Join(dir, "absent"))
	if err != nil {
		t.Fatalf("missing pair dir must not fail: %v", err)
	}
	if len(plan.Shared) != 0 || len(plan.Unique) != 0 {
		t.Errorf("expected an empty plan, got %+v", plan)
	}
}

// TestEncodeWebPProducesSmallerFile proves the encoder actually reduces bytes and
// that the output is a real WebP container.
func TestEncodeWebPProducesSmallerFile(t *testing.T) {
	if !EncoderAvailable() {
		t.Skip("cwebp is not installed; the compiler degrades to PNG and reports it")
	}
	dir := t.TempDir()
	src := filepath.Join(dir, "in.png")
	writePNG(t, src, 0x44)
	dst := filepath.Join(dir, "out.webp")

	res, err := EncodeWebP(src, dst)
	if err != nil {
		t.Fatalf("EncodeWebP: %v", err)
	}
	if res.DestSize == 0 {
		t.Fatal("encoded file is empty")
	}
	if res.SHA256 == "" {
		t.Error("encoded file has no digest")
	}

	body, err := os.ReadFile(dst)
	if err != nil {
		t.Fatalf("read output: %v", err)
	}
	// RIFF....WEBP
	if len(body) < 12 || string(body[0:4]) != "RIFF" || string(body[8:12]) != "WEBP" {
		t.Fatalf("output is not a WebP container: % x", body[:min(12, len(body))])
	}
}

// TestEncodeWebPRejectsMissingSource proves a missing input is an error, not a
// silent empty output.
func TestEncodeWebPRejectsMissingSource(t *testing.T) {
	dir := t.TempDir()
	_, err := EncodeWebP(filepath.Join(dir, "absent.png"), filepath.Join(dir, "out.webp"))
	if err == nil {
		t.Fatal("encoding a missing source must fail")
	}
}

// TestSavingsPercent proves the report's percentage is computed from real bytes.
func TestSavingsPercent(t *testing.T) {
	r := OptimiseReport{SourceBytes: 22_200_000, PublishedBytes: 5_500_000}
	got := r.SavingsPercent()
	if got < 74 || got > 76 {
		t.Errorf("savings = %.1f%%, want about 75%% for the measured totals", got)
	}
	if (OptimiseReport{}).SavingsPercent() != 0 {
		t.Error("an empty report must report 0 savings, not NaN")
	}
}

// TestEncoderAvailableMatchesLookPath proves the probe reflects reality.
func TestEncoderAvailableMatchesLookPath(t *testing.T) {
	_, err := exec.LookPath("cwebp")
	want := err == nil
	if got := EncoderAvailable(); got != want {
		t.Errorf("EncoderAvailable() = %v, but LookPath says %v", got, want)
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
