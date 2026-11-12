package room

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/bhcn/umaguessinggame/internal/deployment"
)

type audioPracticeIndex struct{ practiceIndex }

func (p *audioPracticeIndex) Section(name string) (json.RawMessage, bool) {
	p.requested = append(p.requested, name)
	if name != "audio.clips" {
		return nil, false
	}
	clips := []any{}
	for i := 0; i < 8; i++ {
		clips = append(clips, map[string]any{"clipId": fmt.Sprintf("clip-%d", i), "characterId": fmt.Sprintf("char-%d", i), "characterName": fmt.Sprintf("合成角色%d", i), "audioPath": fmt.Sprintf("/assets/audio/tone-%d.mp3", i), "durationMillis": 1000, "byteLength": 99, "sha256": strings.Repeat("a", 64), "transcript": "合成测试音，不是真实台词", "decodable": true, "speakerVerified": true, "reviewed": true, "audioUrl": "DO_NOT_EXPORT", "reviewer": "DO_NOT_EXPORT"})
	}
	b, _ := json.Marshal(clips)
	return b, true
}
func TestAudioPracticeWhitelistCacheAndPolicy(t *testing.T) {
	index := &audioPracticeIndex{}
	s := New(nil, Config{}, Options{Content: index})
	defer s.Close()
	pack, err := s.AudioPracticePack()
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256([]byte(pack.ContentJSON))
	if pack.ModeID != "audio" || pack.SHA256 != hex.EncodeToString(sum[:]) || pack.ByteLength != len(pack.ContentJSON) || strings.Contains(pack.ContentJSON, "DO_NOT_EXPORT") {
		t.Fatal("invalid envelope or non-whitelisted source exported")
	}
	if _, err = s.AudioPracticePack(); err != nil || len(index.requested) != 1 || index.requested[0] != "audio.clips" {
		t.Fatal("unexpected content access", err, index.requested)
	}
	var content audioPracticeLibrary
	if err := json.Unmarshal([]byte(pack.ContentJSON), &content); err != nil {
		t.Fatal(err)
	}
	if len(content.Clips) != 8 || content.Clips[0].CharacterID != "char-0" || content.Clips[0].LocalizedNames["ja"] != "スペシャルウィーク" || content.Clips[0].Transcript != "合成测试音，不是真实台词" || content.MediaBytes != 8*99 {
		t.Fatal("regional display names altered media identity transcript or byte budget")
	}
	s.cfg.Policy = deployment.Policy{Forms: map[string]bool{"local": false}}
	if _, err = s.AudioPracticePack(); err == nil {
		t.Fatal("cached export bypassed policy")
	}
}
