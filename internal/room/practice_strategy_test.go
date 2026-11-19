package room

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/bhcn/umaguessinggame/internal/deployment"
)

type strategyPracticeIndex struct {
	practiceIndex
	baseline json.RawMessage
}

func (p *strategyPracticeIndex) Section(name string) (json.RawMessage, bool) {
	p.requested = append(p.requested, name)
	if name != "rules.track" {
		return nil, false
	}
	return p.baseline, true
}
func TestStrategyPracticeOnlyPublicRulesCachedAndPolicyChecked(t *testing.T) {
	raw, err := os.ReadFile("../../data/manual/rules-track.json")
	if err != nil {
		t.Fatal(err)
	}
	index := &strategyPracticeIndex{baseline: raw}
	s := New(nil, Config{}, Options{Content: index})
	defer s.Close()
	p, err := s.StrategyPracticePack()
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256([]byte(p.ContentJSON))
	if p.ModeID != "strategy-race" || p.SHA256 != hex.EncodeToString(sum[:]) || p.ByteLength != len(p.ContentJSON) {
		t.Fatal("invalid envelope")
	}
	for _, word := range []string{"provenance", "basis", "author", "_note", "docs/玩法与规则.md"} {
		if strings.Contains(p.ContentJSON, word) {
			t.Fatal("internal metadata exported", word)
		}
	}
	if len(index.requested) != 1 || index.requested[0] != "rules.track" {
		t.Fatal("read unrelated content", index.requested)
	}
	if _, err = s.StrategyPracticePack(); err != nil || len(index.requested) != 1 {
		t.Fatal("cache missed")
	}
	s.cfg.Policy = deployment.Policy{Forms: map[string]bool{"local": false}}
	if _, err = s.StrategyPracticePack(); err == nil {
		t.Fatal("cache bypassed deployment policy")
	}
}
