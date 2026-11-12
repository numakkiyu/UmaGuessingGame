package room

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"github.com/bhcn/umaguessinggame/internal/characternames"
	"github.com/bhcn/umaguessinggame/internal/deployment"
	"github.com/bhcn/umaguessinggame/internal/modes"
	"github.com/bhcn/umaguessinggame/internal/modes/classic"
	"strings"
	"sync"
	"testing"
)

type practiceIndex struct {
	mu        sync.Mutex
	requested []string
}

func (p *practiceIndex) Section(name string) (json.RawMessage, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.requested = append(p.requested, name)
	if name != "classic.characters" {
		return nil, false
	}
	return json.RawMessage(`{"cards":[{"id":"a","name":"甲","star":3,"surfaceGroup":["草地"],"imagePath":"/assets/avatars/a.webp"},{"id":"b","name":"乙","star":2,"surfaceGroup":["泥地"],"imagePath":"/assets/avatars/b.webp"}],"practiceCards":[{"id":"a","name":"甲","star":3,"surfaceGroup":["草地"],"imagePath":"/assets/avatars/a.webp","secret":"DO_NOT_EXPORT"},{"id":"b","name":"乙","star":2,"surfaceGroup":["泥地"],"imagePath":"/assets/avatars/b.webp"},{"id":"c","name":"丙","star":2,"surfaceGroup":["泥地"],"ambiguityGroup":"same","imagePath":"/assets/avatars/c.webp"}]}`), true
}
func (*practiceIndex) Version() string        { return "test-pack" }
func (*practiceIndex) DatasetVersion() string { return "test-data" }
func (*practiceIndex) AssetBaseURL() string   { return "/assets" }
func (*practiceIndex) Character(id string) (CharacterCard, bool) {
	return CharacterCard{SearchNames: &characternames.Names{Japanese: "スペシャルウィーク", English: "Special Week", Nicknames: []string{"小特"}}, CharacterID: id, Aliases: []string{"别名"}, LocalizedNames: map[string]string{"zh-Hans": "特别周", "zh-Hant": "特別週", "ja": "スペシャルウィーク"}}, true
}
func (*practiceIndex) Characters(string, int, string) ([]CharacterCard, string, error) {
	return nil, "", nil
}
func (*practiceIndex) PathNeighbors(string) ([]Neighbor, bool) { return nil, false }
func (*practiceIndex) ModeAdmission(string) (ModeAdmission, bool) {
	return ModeAdmission{Available: true, QuestionCount: 100}, true
}

func TestPracticeExportIsWhitelistedVersionedAndRespectsPolicy(t *testing.T) {
	registry, _ := modes.NewRegistry(classic.New())
	index := &practiceIndex{}
	s := New(nil, Config{}, Options{Modes: registry, Content: index})
	defer s.Close()
	pack, err := s.ClassicPracticePack()
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256([]byte(pack.ContentJSON))
	if pack.SHA256 != hex.EncodeToString(sum[:]) || pack.ByteLength != len([]byte(pack.ContentJSON)) {
		t.Fatal("invalid manifest digest")
	}
	data, _ := json.Marshal(pack)
	if strings.Contains(string(data), "DO_NOT_EXPORT") {
		t.Fatal("arbitrary source field leaked")
	}
	var content struct {
		Characters []PracticeCharacter `json:"characters"`
	}
	if err := json.Unmarshal([]byte(pack.ContentJSON), &content); err != nil {
		t.Fatal(err)
	}
	if len(content.Characters) != 3 || !content.Characters[0].AnswerEligible || content.Characters[2].AnswerEligible {
		t.Fatal("answer pool and guess pool mixed")
	}
	if content.Characters[0].CharacterID != "a" || content.Characters[0].LocalizedNames["ja"] != "スペシャルウィーク" || content.Characters[0].LocalizedNames["zh-Hant"] != "特別週" {
		t.Fatal("localized names lost or identity changed", content.Characters[0])
	}
	if names := content.Characters[0].SearchNames; names == nil || names.English != "Special Week" || len(names.Nicknames) != 1 {
		t.Fatal("search names lost", names)
	}
	for _, name := range index.requested {
		if name != "classic.characters" {
			t.Fatalf("practice export accessed other section %s", name)
		}
	}
	before := len(index.requested)
	s.ClassicPracticePack()
	if len(index.requested) != before {
		t.Fatal("immutable pack rebuilt on each download")
	}
	s.cfg.Policy = deployment.Policy{Forms: map[string]bool{"local": false}}
	if _, err := s.ClassicPracticePack(); err == nil {
		t.Fatal("cached data bypasses instance policy")
	}
}
