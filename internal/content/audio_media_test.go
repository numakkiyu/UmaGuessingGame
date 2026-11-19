package content

import (
	"os"
	"path/filepath"
	"testing"
)

func TestAudioCompilationReviewBoundToBytesAndSelectiveCopy(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, audioDir)
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile("testdata/audio-tone.webm")
	if err != nil {
		t.Fatal(err)
	}
	name := "synthetic-tone.webm"
	src := filepath.Join(dir, name)
	if err = os.WriteFile(src, b, 0644); err != nil {
		t.Fatal(err)
	}
	ctx := &compileContext{root: root, ds: &Dataset{}, raw: &RawInputs{}}
	c := RawWikiCharacter{ID: "tone-speaker"}
	c.Names.ZhHans = "合成信号样例"
	c.Voices = []RawVoice{{AudioURL: "https://example.invalid/" + name, Jp: "Synthetic test tone, not character dialogue"}}
	ctx.raw.Wiki.Characters = append(ctx.raw.Wiki.Characters, c)
	row := audioClipsFromRaw(ctx)[0]
	if row.Decodable || row.Reviewed || row.AudioPath != "" {
		t.Fatal("download alone admitted an unreviewed clip")
	}
	ctx.raw.AudioReviews = RawAudioReviews{ReviewedAt: "2026-11-10", Reviewer: "synthetic-test-fixture-only", Clips: []RawAudioReview{{ClipID: row.ClipID, SHA256: sha256Hex(b), Listened: true, SpeakerVerified: true, TranscriptVerified: true, NoNameSpoiler: true}}}
	row = audioClipsFromRaw(ctx)[0]
	if row.AudioPath != "/assets/audio/"+name || !row.Decodable || !row.Reviewed || row.DurationMillis < 250 || row.DurationMillis > 450 || row.ByteLength != len(b) {
		t.Fatal("compiled media differs from actual decoded fixture", row)
	}
	pub := t.TempDir()
	res := &PublishResult{sections: map[string]any{SectionAudioClips: []audioClip{row}}}
	files, n, err := copyAudioClips(pub, root, publishedAudioNames(res))
	if err != nil || n != 1 || len(files) != 1 {
		t.Fatal(n, files, err)
	}
	// In-place edits must not mutate a published version through a hardlink.
	if err = os.WriteFile(src, []byte("changed"), 0644); err != nil {
		t.Fatal(err)
	}
	copied, _ := os.ReadFile(filepath.Join(pub, audioPublicDir, name))
	if string(copied) != string(b) {
		t.Fatal("published media mutated with source")
	}
	row = audioClipsFromRaw(ctx)[0]
	if row.Reviewed || row.Decodable || row.AudioPath != "" {
		t.Fatal("old review approved changed bytes")
	}
	if err = os.WriteFile(src, []byte{0xff, 0xfb, 0x90, 0, 0, 0}, 0644); err != nil {
		t.Fatal(err)
	}
	if _, err = inspectAudioFile(src); err == nil {
		t.Fatal("a plausible frame header was treated as decoded audio")
	}
	for _, bad := range []string{"../x.webm", "https://x/%2e%2e.webm", "https://x/file.webm#suffix", "https://x/a.wav"} {
		if audioRouteFor(bad) != "" {
			t.Fatal("unsafe audio name", bad)
		}
	}
}

func TestWebMOpusRejectsCorruptMedia(t *testing.T) {
	b, err := os.ReadFile("testdata/audio-tone.webm")
	if err != nil {
		t.Fatal(err)
	}
	for name, invalid := range map[string][]byte{
		"empty":                 nil,
		"mp3 header":            []byte("ID3not-webm"),
		"truncated header":      b[:10],
		"truncated media":       b[:len(b)/2],
		"trailing invalid data": append(append([]byte(nil), b...), 0),
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := decodeWebMOpus(invalid); err == nil {
				t.Fatal("corrupt media was admitted")
			}
		})
	}
}
