package content

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"math"
	"net/url"
	"os"
	"path"
	"regexp"
	"sort"
	"strings"

	"github.com/at-wat/ebml-go"
	"github.com/at-wat/ebml-go/webm"
	"github.com/pion/opus"
)

var audioNamePattern = regexp.MustCompile(`^[A-Za-z0-9_-]+\.(?:mp3|webm)$`)

// audioFileName resolves the WebM filename associated with a source URL.
func audioFileName(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || strings.Contains(raw, "..") || strings.ContainsAny(raw, `\%#`) {
		return ""
	}
	name := path.Base(u.Path)
	if !audioNamePattern.MatchString(name) {
		return ""
	}
	return name[:strings.LastIndexByte(name, '.')] + ".webm"
}

type audioMediaInfo struct {
	DurationMillis, ByteLength int
	SHA256                     string
}

// inspectAudioFile fully decodes a bounded, single-track WebM/Opus clip in Go.
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
	ms, err := decodeWebMOpus(b)
	if err != nil {
		return audioMediaInfo{}, err
	}
	return audioMediaInfo{DurationMillis: ms, ByteLength: len(b), SHA256: sha256Hex(b)}, nil
}

func decodeWebMOpus(b []byte) (int, error) {
	var document struct {
		Header  webm.EBMLHeader `ebml:"EBML"`
		Segment webm.Segment    `ebml:"Segment"`
	}
	if err := ebml.Unmarshal(bytes.NewReader(b), &document, ebml.WithMaxLeafElementSize(2_000_000)); err != nil {
		return 0, fmt.Errorf("audio: invalid WebM: %w", err)
	}
	s := document.Segment
	if document.Header.DocType != "webm" || len(s.Tracks.TrackEntry) != 1 || len(s.Cluster) == 0 {
		return 0, fmt.Errorf("audio: expected a single-track WebM file")
	}
	t := s.Tracks.TrackEntry[0]
	if t.CodecID != "A_OPUS" || t.TrackType != 2 || t.TrackNumber == 0 || t.Audio == nil || t.Video != nil ||
		t.Audio.SamplingFrequency != 48000 || t.Audio.Channels != 1 ||
		len(t.CodecPrivate) != 19 || string(t.CodecPrivate[:8]) != "OpusHead" || t.CodecPrivate[8] != 1 ||
		t.CodecPrivate[9] != byte(t.Audio.Channels) || t.CodecPrivate[18] != 0 {
		return 0, fmt.Errorf("audio: unsupported WebM/Opus track")
	}
	preSkip := int(binary.LittleEndian.Uint16(t.CodecPrivate[10:12]))
	if preSkip > 48000 {
		return 0, fmt.Errorf("audio: excessive Opus pre-skip")
	}
	scale := s.Info.TimecodeScale
	if scale == 0 {
		scale = 1_000_000
	}
	if scale > 1_000_000_000 || math.IsNaN(s.Info.Duration) || math.IsInf(s.Info.Duration, 0) || s.Info.Duration < 0 || s.Info.Duration*float64(scale) > 61e9 {
		return 0, fmt.Errorf("audio: invalid WebM duration")
	}
	decoder, err := opus.NewDecoderWithOutput(48000, int(t.Audio.Channels))
	if err != nil {
		return 0, err
	}
	pcm := make([]float32, 5760*int(t.Audio.Channels))
	total, packets := 0, 0
	lastTimestamp := int64(-1_000_000_001)
	for _, cluster := range s.Cluster {
		if cluster.Timecode > 61_000_000_000/scale {
			return 0, fmt.Errorf("audio: cluster exceeds 60 seconds")
		}
		blocks := append([]ebml.Block(nil), cluster.SimpleBlock...)
		for _, group := range cluster.BlockGroup {
			blocks = append(blocks, group.Block)
		}
		sort.SliceStable(blocks, func(i, j int) bool { return blocks[i].Timecode < blocks[j].Timecode })
		for _, block := range blocks {
			timestamp := (int64(cluster.Timecode) + int64(block.Timecode)) * int64(scale)
			if block.TrackNumber != t.TrackNumber || len(block.Data) == 0 || timestamp < lastTimestamp || timestamp < -1e9 || timestamp > 60e9 ||
				math.Abs(float64(timestamp)-float64(total-preSkip)*1e9/48000) > 100e6 {
				return 0, fmt.Errorf("audio: invalid block track or timing")
			}
			lastTimestamp = timestamp
			for _, packet := range block.Data {
				packets++
				if len(packet) == 0 || len(packet) > 1275*48 || packets > 24001 {
					return 0, fmt.Errorf("audio: invalid Opus packet size/count")
				}
				n, err := decoder.DecodeToFloat32(packet, pcm)
				if err != nil {
					return 0, fmt.Errorf("audio: Opus decoding failed: %w", err)
				}
				if n <= 0 {
					return 0, fmt.Errorf("audio: empty decoded Opus packet")
				}
				total += n
				if total > 48000*60+preSkip {
					return 0, fmt.Errorf("audio: clip exceeds 60 seconds")
				}
			}
		}
	}
	ms := (total - preSkip) * 1000 / 48000
	if ms <= 0 || (s.Info.Duration > 0 && math.Abs(s.Info.Duration*float64(scale)/1e6-float64(ms)) > 100) {
		return 0, fmt.Errorf("audio: empty clip or inconsistent duration")
	}
	return ms, nil
}
