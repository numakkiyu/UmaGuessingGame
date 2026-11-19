package room

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"github.com/bhcn/umaguessinggame/internal/deployment"
	"github.com/bhcn/umaguessinggame/internal/modes"
	sortgame "github.com/bhcn/umaguessinggame/internal/modes/sort"
	"strings"
	"testing"
)

type sortPracticeIndex struct{ practiceIndex }

func (p *sortPracticeIndex) Section(name string) (json.RawMessage, bool) {
	p.requested = append(p.requested, name)
	if name != "sort.cards" {
		return nil, false
	}
	return json.RawMessage(`{"cards":[{"characterId":"a","name":"甲","heightCm":150,"birthMonth":2,"birthDay":29,"secret":"DO_NOT_EXPORT","eventDate":{"date":"2020-01-01","region":"jp"}},{"characterId":"b","name":"乙","heightCm":160,"birthMonth":2,"birthDay":30}]}`), true
}
func TestSortPracticeExportWhitelistAndCachedPolicy(t *testing.T) {
	registry, _ := modes.NewRegistry(sortgame.New())
	index := &sortPracticeIndex{}
	s := New(nil, Config{}, Options{Modes: registry, Content: index})
	defer s.Close()
	p, err := s.SortPracticePack()
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256([]byte(p.ContentJSON))
	if p.ModeID != "sort" || p.SHA256 != hex.EncodeToString(sum[:]) || p.ByteLength != len(p.ContentJSON) {
		t.Fatal("bad manifest")
	}
	if strings.Contains(p.ContentJSON, "DO_NOT_EXPORT") || strings.Contains(p.ContentJSON, "eventDate") {
		t.Fatal("arbitrary source fields exported")
	}
	var content struct {
		Cards []sortPracticeProfile `json:"cards"`
	}
	json.Unmarshal([]byte(p.ContentJSON), &content)
	if len(content.Cards) != 2 || content.Cards[0].Values["birthday"] != 229 {
		t.Fatal("public data missing")
	}
	if content.Cards[0].CharacterID != "a" || content.Cards[0].LocalizedNames["ja"] != "スペシャルウィーク" {
		t.Fatal("regional display metadata missing or changed identity")
	}
	if _, ok := content.Cards[1].Values["birthday"]; ok {
		t.Fatal("invalid birthday admitted")
	}
	before := len(index.requested)
	s.SortPracticePack()
	if len(index.requested) != before {
		t.Fatal("cached public facts rebuilt")
	}
	for _, name := range index.requested {
		if name != "sort.cards" {
			t.Fatal("read unrelated section")
		}
	}
	s.cfg.Policy = deployment.Policy{Forms: map[string]bool{"local": false}}
	if _, err = s.SortPracticePack(); err == nil {
		t.Fatal("cached pack bypassed closed form")
	}
}
