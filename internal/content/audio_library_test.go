package content

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestVerifyAudioLibrary(t *testing.T) {
	b, err := os.ReadFile("testdata/audio-tone.webm")
	if err != nil {
		t.Fatal(err)
	}
	info, err := inspectAudioFile("testdata/audio-tone.webm")
	if err != nil {
		t.Fatal(err)
	}
	for _, scenario := range []string{"valid", "wrong bitrate", "constant bitrate", "stereo", "hash mismatch", "duration mismatch", "size mismatch", "corrupt", "extra format", "missing file", "duplicate", "traversal", "symlink"} {
		t.Run(scenario, func(t *testing.T) {
			root := t.TempDir()
			dir := filepath.Join(root, audioDir)
			if err := os.MkdirAll(dir, 0755); err != nil {
				t.Fatal(err)
			}
			file := filepath.Join(dir, "tone.webm")
			if err := os.WriteFile(file, b, 0644); err != nil {
				t.Fatal(err)
			}
			var m audioLibraryManifest
			if err := json.Unmarshal([]byte(`{"format":"audio/webm; codecs=opus","encoding":{"codec":"opus","container":"webm","targetBitrate":40000,"vbr":true,"sampleRate":48000,"channels":1},"clips":[{"file":"tone.webm"}]}`), &m); err != nil {
				t.Fatal(err)
			}
			m.Clips[0].SHA256 = info.SHA256
			m.Clips[0].Bytes = info.ByteLength
			m.Clips[0].DurationMillis = info.DurationMillis
			var setupErr error
			switch scenario {
			case "wrong bitrate":
				m.Encoding.TargetBitrate = 24000
			case "constant bitrate":
				m.Encoding.VBR = false
			case "stereo":
				m.Encoding.Channels = 2
			case "hash mismatch":
				m.Clips[0].SHA256 = "changed"
			case "duration mismatch":
				m.Clips[0].DurationMillis++
			case "size mismatch":
				m.Clips[0].Bytes++
			case "corrupt":
				setupErr = os.WriteFile(file, b[:len(b)/2], 0644)
			case "extra format":
				setupErr = os.WriteFile(filepath.Join(dir, "tone.mp3"), []byte("ID3"), 0644)
			case "missing file":
				setupErr = os.Remove(file)
			case "duplicate":
				m.Clips = append(m.Clips, m.Clips[0])
				setupErr = os.WriteFile(filepath.Join(dir, "extra.webm"), b, 0644)
			case "traversal":
				m.Clips[0].File = "../tone.webm"
			case "symlink":
				if err := os.Remove(file); err != nil {
					t.Fatal(err)
				}
				setupErr = os.Symlink(filepath.Join(root, "outside.webm"), file)
			}
			if setupErr != nil {
				t.Fatal(setupErr)
			}
			data, err := json.Marshal(m)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(root, "data/assets/audio/manifest.json"), data, 0644); err != nil {
				t.Fatal(err)
			}
			report, err := VerifyAudioLibrary(root)
			if scenario != "valid" {
				if err == nil {
					t.Fatal("invalid library passed verification")
				}
				return
			}
			if err != nil || report.Clips != 1 || report.Bytes != int64(len(b)) {
				t.Fatalf("report=%+v err=%v", report, err)
			}
		})
	}
}
