package room

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"github.com/bhcn/umaguessinggame/internal/deployment"
	"github.com/bhcn/umaguessinggame/internal/httpapi/problem"
	"github.com/bhcn/umaguessinggame/internal/modes"
	"strings"
	"testing"
)

type racePracticeIndex struct {
	practiceIndex
	answer json.RawMessage
}

func (p *racePracticeIndex) Section(name string) (json.RawMessage, bool) {
	p.requested = append(p.requested, name)
	if name != "race-rebuild.races" {
		return nil, false
	}
	return json.RawMessage(`[{"raceId":"r1","name":"锦标","date":"2026-01-02","starters":[{"characterId":"a","characterName":"甲"},{"characterId":"b","characterName":"乙"},{"characterId":"c","characterName":"丙"},{"characterId":"d","characterName":"丁"}],"secret":"DO_NOT_EXPORT"}]`), true
}
func (p *racePracticeIndex) Secrets() modes.ContentSource { return racePracticeSecret{p} }

type racePracticeSecret struct{ index *racePracticeIndex }

func (s racePracticeSecret) Version() string { return "test" }
func (s racePracticeSecret) Section(name string) (json.RawMessage, bool) {
	s.index.requested = append(s.index.requested, name)
	if name != "race-rebuild.answers" {
		return nil, false
	}
	if s.index.answer != nil {
		return s.index.answer, true
	}
	return json.RawMessage(`{"races":[{"raceId":"r1","order":["a","b","c","d"],"ranks":{"a":1,"b":3,"c":3,"d":7},"ties":{"3":["b","c"]},"seed":"DO_NOT_EXPORT","playerState":"DO_NOT_EXPORT"}],"secret":"DO_NOT_EXPORT"}`), true
}

func TestRacePracticeUnavailableIsDistinctFromCorruptContent(t *testing.T) {
	for _, tc := range []struct {
		name, answer, code string
		status             int
	}{
		{"legacy", `{"races":[{"raceId":"r1","order":["a","b","c","d"]}]}`, "practice-unavailable", 422},
		{"empty", `{"races":[]}`, "practice-unavailable", 422},
		{"malformed", `{"races":`, "internal-error", 500},
		{"contradictory", `{"races":[{"raceId":"r1","order":["a","b","c","d"],"ranks":{"a":1,"b":2,"c":3,"d":4},"ties":{"2":["b","c"]}}]}`, "internal-error", 500},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := New(nil, Config{}, Options{Content: &racePracticeIndex{answer: json.RawMessage(tc.answer)}})
			defer s.Close()
			for i := 0; i < 2; i++ {
				pack, err := s.RacePracticePack()
				var p *problem.Error
				if !errors.As(err, &p) || p.Status != tc.status || p.Code != tc.code {
					t.Fatalf("unexpected error: %v", err)
				}
				if p.Unwrap() == nil || pack.ContentJSON != "" {
					t.Fatal("failed export lost diagnostic cause or returned partial facts")
				}
			}
		})
	}
}
func TestRacePracticeOnlyDeliberateHistoricalFactsCachedAndPolicyChecked(t *testing.T) {
	index := &racePracticeIndex{}
	s := New(nil, Config{}, Options{Content: index})
	defer s.Close()
	p, err := s.RacePracticePack()
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256([]byte(p.ContentJSON))
	if p.ModeID != "race-rebuild" || p.SHA256 != hex.EncodeToString(sum[:]) || p.ByteLength != len(p.ContentJSON) {
		t.Fatal("invalid practice manifest")
	}
	if strings.Contains(p.ContentJSON, "DO_NOT_EXPORT") || strings.Contains(p.ContentJSON, "seed") || !strings.Contains(p.ContentJSON, `"finishRank":7`) {
		t.Fatal("wrong historical fact boundary", p.ContentJSON)
	}
	var content struct {
		Races []racePracticeRace `json:"races"`
	}
	if err := json.Unmarshal([]byte(p.ContentJSON), &content); err != nil {
		t.Fatal(err)
	}
	if len(content.Races) != 1 || len(content.Races[0].Entries) != 4 || content.Races[0].Entries[0].CharacterID != "a" || content.Races[0].Entries[0].LocalizedNames["ja"] != "スペシャルウィーク" || content.Races[0].Entries[3].FinishRank != 7 {
		t.Fatal("regional metadata changed the recorded race")
	}
	before := len(index.requested)
	if _, err = s.RacePracticePack(); err != nil || len(index.requested) != before {
		t.Fatal("cache missed")
	}
	for _, name := range index.requested {
		if name != "race-rebuild.races" && name != "race-rebuild.answers" {
			t.Fatal("read unrelated secrets", name)
		}
	}
	s.cfg.Policy = deployment.Policy{Forms: map[string]bool{"local": false}}
	if _, err = s.RacePracticePack(); err == nil {
		t.Fatal("cached pack bypasses policy")
	}
}
