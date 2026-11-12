package content

import (
	"bytes"
	"fmt"
	"io"
	"net/url"
	"os"
	"regexp"
	"strings"

	"github.com/hajimehoshi/go-mp3"
)

var audioNamePattern = regexp.MustCompile(`^[A-Za-z0-9_-]+\.mp3$`)

func audioFileName(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || strings.Contains(raw, "..") || strings.ContainsAny(raw, `\%#`) {
		return ""
	}
	name := raw[strings.LastIndexByte(raw, '/')+1:]
	if i := strings.IndexByte(name, '?'); i >= 0 {
		name = name[:i]
	}
	if !audioNamePattern.MatchString(name) {
		return ""
	}
	return name
}

type audioMediaInfo struct {
	DurationMillis, ByteLength int
	SHA256                     string
}

// Decode the complete bounded file. A frame-header/bitrate estimate is not proof
// of a playable clip. This pure Go decoder also works in the distroless tool.
func inspectAudioFile(path string) (audioMediaInfo, error) {
	f, err := os.Open(path)
	if err != nil {
		return audioMediaInfo{}, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() <= 0 || info.Size() > 2_000_000 {
		return audioMediaInfo{}, fmt.Errorf("audio: invalid file size/type")
	}
	b, err := io.ReadAll(io.LimitReader(f, 2_000_001))
	if err != nil || len(b) > 2_000_000 {
		return audioMediaInfo{}, fmt.Errorf("audio: file read limit")
	}
	d, err := mp3.NewDecoder(bytes.NewReader(b))
	if err != nil {
		return audioMediaInfo{}, err
	}
	const channelsAndSampleBytes = 4 // decoder emits stereo signed 16-bit PCM
	limit := int64(d.SampleRate()) * channelsAndSampleBytes * 60
	n, err := io.Copy(io.Discard, io.LimitReader(d, limit+1))
	if err != nil || n <= 0 || n > limit {
		return audioMediaInfo{}, fmt.Errorf("audio: decoding failed or duration exceeds 60 seconds")
	}
	ms := int(n * 1000 / (int64(d.SampleRate()) * channelsAndSampleBytes))
	if ms <= 0 {
		return audioMediaInfo{}, fmt.Errorf("audio: empty decoded clip")
	}
	return audioMediaInfo{DurationMillis: ms, ByteLength: len(b), SHA256: sha256Hex(b)}, nil
}
