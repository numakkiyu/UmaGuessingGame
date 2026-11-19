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

type aptitudePracticeIndex struct{ practiceIndex }

func (p *aptitudePracticeIndex) Section(name string) (json.RawMessage, bool) {
	p.requested = append(p.requested, name)
	cards := []map[string]any{}
	for i, d := range []string{"短", "英", "中", "长"} {
		for j, s := range []string{"逃", "先", "差"} {
			cards = append(cards, map[string]any{"id": fmt.Sprintf("c%d%d", i, j), "name": "角色", "imagePath": "/assets/avatars/c.webp", "aptitudes": map[string]any{"surface": map[string]string{"草地": "A", "secret": "DO_NOT_EXPORT"}, "distance": map[string]string{d: "A"}, "style": map[string]string{s: "A"}}, "secret": "DO_NOT_EXPORT"})
		}
	}
	if name == "aptitude-grid.cards" {
		b, _ := json.Marshal(cards)
		return b, true
	}
	if name == "draft.market" {
		tasks := []map[string]string{}
		for i, d := range []string{"短", "英", "中", "长"} {
			tasks = append(tasks, map[string]string{"taskId": fmt.Sprintf("t%d", i), "surface": "草地", "distance": d, "style": "逃"})
		}
		b, _ := json.Marshal(map[string]any{"cards": cards, "tasks": tasks, "secret": "DO_NOT_EXPORT"})
		return b, true
	}
	return nil, false
}
func TestAptitudePracticeEnvelopeCacheAndPolicy(t *testing.T) {
	for _, mode := range []string{"aptitude-grid", "draft"} {
		t.Run(mode, func(t *testing.T) {
			index := &aptitudePracticeIndex{}
			s := New(nil, Config{}, Options{Content: index})
			defer s.Close()
			get := s.AptitudePracticePack
			if mode == "draft" {
				get = s.DraftPracticePack
			}
			p, err := get()
			if err != nil {
				t.Fatal(err)
			}
			sum := sha256.Sum256([]byte(p.ContentJSON))
			if p.ModeID != mode || p.SHA256 != hex.EncodeToString(sum[:]) || p.ByteLength != len(p.ContentJSON) {
				t.Fatal("invalid public manifest")
			}
			if strings.Contains(p.ContentJSON, "DO_NOT_EXPORT") || strings.Contains(p.ContentJSON, "secret") {
				t.Fatal("non-public content exported")
			}
			var payload struct {
				Cards []struct {
					ID             string            `json:"id"`
					LocalizedNames map[string]string `json:"localizedNames"`
				} `json:"cards"`
			}
			if err := json.Unmarshal([]byte(p.ContentJSON), &payload); err != nil {
				t.Fatal(err)
			}
			if len(payload.Cards) < 12 || payload.Cards[0].ID != "c00" || payload.Cards[0].LocalizedNames["ja"] != "スペシャルウィーク" || payload.Cards[0].LocalizedNames["zh-Hant"] != "特別週" {
				t.Fatal("names or original card identity lost", payload.Cards)
			}
			before := len(index.requested)
			if _, err = get(); err != nil || len(index.requested) != before {
				t.Fatal("immutable cache missed")
			}
			for _, name := range index.requested {
				if name != "aptitude-grid.cards" && name != "draft.market" {
					t.Fatal("unexpected private section", name)
				}
			}
			s.cfg.Policy = deployment.Policy{Forms: map[string]bool{"local": false}}
			if _, err = get(); err == nil {
				t.Fatal("cached export bypasses policy")
			}
		})
	}
}
