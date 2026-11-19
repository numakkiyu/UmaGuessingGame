package content

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
)

type audioLibraryManifest struct {
	Format   string `json:"format"`
	Encoding struct {
		Codec         string `json:"codec"`
		Container     string `json:"container"`
		TargetBitrate int    `json:"targetBitrate"`
		VBR           bool   `json:"vbr"`
		SampleRate    int    `json:"sampleRate"`
		Channels      int    `json:"channels"`
	} `json:"encoding"`
	Clips []struct {
		File           string `json:"file"`
		SHA256         string `json:"sha256"`
		Bytes          int    `json:"bytes"`
		DurationMillis int    `json:"durationMillis"`
	} `json:"clips"`
}

// AudioLibraryReport counts the files and bytes verified against the source manifest.
type AudioLibraryReport struct {
	Clips int   `json:"clips"`
	Bytes int64 `json:"bytes"`
}

// VerifyAudioLibrary fully decodes every listed clip, including clips not admitted to a question pack.
func VerifyAudioLibrary(root string) (AudioLibraryReport, error) {
	var report AudioLibraryReport
	f, err := os.Open(filepath.Join(root, "data/assets/audio/manifest.json"))
	if err != nil {
		return report, err
	}
	defer f.Close()
	var manifest audioLibraryManifest
	decoder := json.NewDecoder(io.LimitReader(f, 50*1024*1024))
	if err := decoder.Decode(&manifest); err != nil {
		return report, fmt.Errorf("audio manifest: %w", err)
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return report, fmt.Errorf("audio manifest: expected a single JSON document")
	}
	e := manifest.Encoding
	if manifest.Format != "audio/webm; codecs=opus" || e.Codec != "opus" || e.Container != "webm" ||
		e.TargetBitrate != 40000 || !e.VBR || e.SampleRate != 48000 || e.Channels != 1 || len(manifest.Clips) == 0 {
		return report, fmt.Errorf("audio manifest: expected 40 kbps VBR, mono 48 kHz WebM/Opus")
	}
	dir := filepath.Join(root, audioDir)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return report, err
	}
	if len(entries) != len(manifest.Clips) {
		return report, fmt.Errorf("audio library: file count differs from manifest")
	}
	files := make(map[string]bool, len(entries))
	for _, entry := range entries {
		if !entry.Type().IsRegular() {
			return report, fmt.Errorf("audio library: %s is not a regular file", entry.Name())
		}
		files[entry.Name()] = true
	}
	namePattern := regexp.MustCompile(`^[A-Za-z0-9_-]+\.webm$`)
	for _, clip := range manifest.Clips {
		if !namePattern.MatchString(clip.File) || !files[clip.File] {
			return report, fmt.Errorf("audio manifest: missing, duplicate or invalid file %q", clip.File)
		}
		delete(files, clip.File)
		info, err := inspectAudioFile(filepath.Join(dir, clip.File))
		if err != nil {
			return report, fmt.Errorf("%s: %w", clip.File, err)
		}
		if info.SHA256 != clip.SHA256 || info.ByteLength != clip.Bytes || info.DurationMillis != clip.DurationMillis {
			return report, fmt.Errorf("%s: decoded media differs from manifest", clip.File)
		}
		report.Clips++
		report.Bytes += int64(info.ByteLength)
	}
	return report, nil
}
