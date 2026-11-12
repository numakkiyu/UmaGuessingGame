package room

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"github.com/bhcn/umaguessinggame/internal/deployment"
	"strings"
	"testing"
)

type pedigreePracticeIndex struct{ practiceIndex }

func (p *pedigreePracticeIndex) Section(name string) (json.RawMessage, bool) {
	p.requested = append(p.requested, name)
	if name != "pedigree.entries" {
		return nil, false
	}
	rows := []any{}
	for i := 1; i <= 2; i++ {
		ped := map[string]string{}
		for _, s := range []string{"sire", "dam", "sireSire", "sireDam", "damSire", "damDam"} {
			ped[s] = fmt.Sprintf("ancestor-%d-%s", i, s)
		}
		rows = append(rows, map[string]any{"characterId": fmt.Sprint(i), "name": "角色", "nkbId": fmt.Sprint(100 + i), "prototypeNames": map[string]string{"ja": fmt.Sprintf("原型%d", i), "jaConfidence": "confirmed"}, "pedigree": ped, "private": "DO_NOT_EXPORT"})
	}
	raw, _ := json.Marshal(rows)
	return raw, true
}
func TestPedigreePracticeWhitelistCacheAndPolicy(t *testing.T) {
	index := &pedigreePracticeIndex{}
	s := New(nil, Config{}, Options{Content: index})
	defer s.Close()
	pack, err := s.PedigreePracticePack()
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256([]byte(pack.ContentJSON))
	if pack.ModeID != "pedigree" || pack.SHA256 != hex.EncodeToString(sum[:]) || pack.ByteLength != len(pack.ContentJSON) || strings.Contains(pack.ContentJSON, "DO_NOT_EXPORT") {
		t.Fatal("invalid envelope or private content exposed")
	}
	if len(index.requested) != 1 || index.requested[0] != "pedigree.entries" {
		t.Fatal("unrelated content accessed", index.requested)
	}
	var content pedigreePracticeLibrary
	if err := json.Unmarshal([]byte(pack.ContentJSON), &content); err != nil {
		t.Fatal(err)
	}
	if len(content.Families) != 2 || content.Families[0].Subject.CharacterID != "1" || content.Families[0].Subject.LocalizedNames["ja"] != "スペシャルウィーク" || content.Families[0].Subject.PrototypeName != "原型1" || len(content.Families[0].Relations) != 6 || len(content.Horses) != 12 {
		t.Fatal("regional character metadata altered historical identities or bloodline facts")
	}
	if _, err = s.PedigreePracticePack(); err != nil || len(index.requested) != 1 {
		t.Fatal("cache miss")
	}
	s.cfg.Policy = deployment.Policy{Forms: map[string]bool{"local": false}}
	if _, err = s.PedigreePracticePack(); err == nil {
		t.Fatal("cached facts bypassed deployment gate")
	}
}
