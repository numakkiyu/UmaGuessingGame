package audio

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/bhcn/umaguessinggame/internal/modes"
)

type PracticeClip struct {
	ClipID         string `json:"clipId"`
	CharacterID    string `json:"characterId"`
	CharacterName  string `json:"characterName"`
	AudioPath      string `json:"audioPath"`
	DurationMillis int    `json:"durationMillis"`
	ByteLength     int    `json:"byteLength"`
	SHA256         string `json:"sha256"`
	Transcript     string `json:"transcript"`
}
type PracticeLibrary struct {
	Clips      []PracticeClip `json:"clips"`
	MediaBytes int            `json:"mediaBytes"`
}

var practiceAudioDigest = regexp.MustCompile(`^[a-f0-9]{64}$`)

// Deliberately public, unranked practice facts. No live room data, review ledger,
// upstream URL, arbitrary source columns or private projection is exported.
func PracticeFacts(src modes.ContentSource) (PracticeLibrary, error) {
	clips, _, err := loadUsableClips(src)
	if err != nil {
		return PracticeLibrary{}, err
	}
	out := PracticeLibrary{Clips: []PracticeClip{}}
	speakers := map[string]bool{}
	duration := 0
	for _, c := range clips {
		if strings.TrimSpace(c.CharacterName) == "" || !practiceAudioDigest.MatchString(c.SHA256) || c.ByteLength <= 0 || c.ByteLength > 2_000_000 || c.DurationMillis > 60_000 || speakers[c.CharacterID] || out.MediaBytes+c.ByteLength > 12_000_000 || duration+c.DurationMillis > 180_000 {
			continue
		}
		out.Clips = append(out.Clips, PracticeClip{c.ClipID, c.CharacterID, c.CharacterName, c.AudioPath, c.DurationMillis, c.ByteLength, c.SHA256, c.Transcript})
		out.MediaBytes += c.ByteLength
		duration += c.DurationMillis
		speakers[c.CharacterID] = true
		if len(out.Clips) == 24 {
			break
		}
	}
	if len(out.Clips) < MinClips {
		return PracticeLibrary{}, fmt.Errorf("audio: insufficient reviewed practice media")
	}
	return out, nil
}
