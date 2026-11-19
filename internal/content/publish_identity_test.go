package content

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPublishRejectsDifferentFactsUnderExistingIdentity(t *testing.T) {
	makePack := func(version, generated string, rank int) *PublishResult {
		return &PublishResult{
			Version:         version,
			Manifest:        &Manifest{SchemaVersion: SchemaVersion, DatasetVersion: "ds-test", ContentPackVersion: version, GeneratedAt: generated},
			Report:          &QualityReport{PrivateSections: []string{PrivateRaceRebuildAnswers}},
			privateSections: map[string]any{PrivateRaceRebuildAnswers: map[string]int{"recorded-rank": rank}},
		}
	}
	out := t.TempDir()
	first := makePack("pack-original", "2026-11-11T00:00:00Z", 7)
	if err := publishPack(out, first); err != nil {
		t.Fatal(err)
	}
	// Equal game content can be rebuilt at another time, but the returned
	// identity must describe the bytes actually kept on disk.
	same := makePack(first.Version, "2026-11-12T00:00:00Z", 7)
	if err := publishPack(out, same); err != nil || same.Manifest.SHA256 != first.Manifest.SHA256 {
		t.Fatalf("identical rebuild did not reuse stored manifest: %v", err)
	}
	if err := publishPack(out, makePack("pack-current", "2026-11-11T00:00:00Z", 3)); err != nil {
		t.Fatal(err)
	}
	if err := publishPack(out, makePack(first.Version, "2026-11-12T00:00:00Z", 5)); err == nil || !strings.Contains(err.Error(), "different payload") {
		t.Fatalf("different hidden fact reused existing identity: %v", err)
	}
	current, err := os.ReadFile(filepath.Join(out, CurrentFileName))
	if err != nil || strings.TrimSpace(string(current)) != "pack-current" {
		t.Fatal("failed publication moved the current pointer", string(current), err)
	}
	if err := verifyPublishedPack(filepath.Join(out, first.Version)); err != nil {
		t.Fatal("failed publication damaged original pack", err)
	}
	staging, _ := filepath.Glob(filepath.Join(out, ".staging-*"))
	if len(staging) != 0 {
		t.Fatal("publication leaked staging directories", staging)
	}
}
