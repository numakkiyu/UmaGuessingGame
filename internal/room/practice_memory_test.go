package room

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/bhcn/umaguessinggame/internal/deployment"
	"github.com/bhcn/umaguessinggame/internal/modes"
	memorygame "github.com/bhcn/umaguessinggame/internal/modes/memory"
)

type memoryPracticeIndex struct{ practiceIndex }

func (p *memoryPracticeIndex) Section(name string) (json.RawMessage, bool) {
	p.requested = append(p.requested, name)
	if name != "memory.pairs" {
		return nil, false
	}
	faces := make([]map[string]any, 8)
	for i := range faces {
		faces[i] = map[string]any{"pairId": fmt.Sprintf("pair-%d", i), "imagePath": fmt.Sprintf("/assets/faces/pair-%d.webp", i), "secret": "DO_NOT_EXPORT", "label": "DO_NOT_EXPORT"}
	}
	raw, _ := json.Marshal(map[string]any{"pairs": faces})
	return raw, true
}
func TestMemoryPracticeOnlyPublicFacesCachedAndPolicyChecked(t *testing.T) {
	registry, _ := modes.NewRegistry(memorygame.New())
	index := &memoryPracticeIndex{}
	s := New(nil, Config{}, Options{Modes: registry, Content: index})
	defer s.Close()
	p, err := s.MemoryPracticePack()
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256([]byte(p.ContentJSON))
	if p.ModeID != "memory" || p.SHA256 != hex.EncodeToString(sum[:]) || p.ByteLength != len(p.ContentJSON) {
		t.Fatal("invalid manifest")
	}
	if strings.Contains(p.ContentJSON, "DO_NOT_EXPORT") {
		t.Fatal("unreviewed fields exported")
	}
	var content struct {
		Faces []memorygame.PracticeFace `json:"faces"`
	}
	if err = json.Unmarshal([]byte(p.ContentJSON), &content); err != nil || len(content.Faces) != 8 {
		t.Fatal("wrong public face library")
	}
	before := len(index.requested)
	if _, err = s.MemoryPracticePack(); err != nil || len(index.requested) != before {
		t.Fatal("cache missed")
	}
	for _, name := range index.requested {
		if name != "memory.pairs" {
			t.Fatal("read private or unrelated data", name)
		}
	}
	s.cfg.Policy = deployment.Policy{Forms: map[string]bool{"local": false}}
	if _, err = s.MemoryPracticePack(); err == nil {
		t.Fatal("cached pack bypassed policy")
	}
}
