package room

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"github.com/bhcn/umaguessinggame/internal/httpapi/problem"
	sortgame "github.com/bhcn/umaguessinggame/internal/modes/sort"
)

type sortPracticeProfile struct {
	sortgame.PracticeProfile
	LocalizedNames map[string]string `json:"localizedNames,omitempty"`
}

func (s *Service) SortPracticePack() (PracticePack, error) {
	if s == nil || !s.cfg.Policy.Allowed("sort", "local") {
		return PracticePack{}, problem.Forbidden("play-disabled", "practice is closed")
	}
	if s.content == nil {
		return PracticePack{}, problem.NotImplemented("content-unavailable", "practice content unavailable")
	}
	if adm, ok := s.content.ModeAdmission("sort"); !ok || !adm.Available {
		return PracticePack{}, problem.Unprocessable("practice-unavailable", "sort practice unavailable")
	}
	s.sortPracticeOnce.Do(func() {
		cards, err := sortgame.PracticeProfiles(s.content)
		if err != nil {
			s.sortPracticeErr = problem.Internal(err)
			return
		}
		public := make([]sortPracticeProfile, 0, len(cards))
		for _, card := range cards {
			public = append(public, sortPracticeProfile{PracticeProfile: card, LocalizedNames: s.practiceNames(card.CharacterID)})
		}
		data, err := json.Marshal(struct {
			Cards []sortPracticeProfile `json:"cards"`
		}{public})
		if err != nil {
			s.sortPracticeErr = problem.Internal(fmt.Errorf("sort practice export: %w", err))
			return
		}
		sum := sha256.Sum256(data)
		s.sortPracticePack = PracticePack{SchemaVersion: 1, ModeID: "sort", RulesVersion: sortgame.RulesVersion, DatasetVersion: s.content.DatasetVersion(), ContentPackVersion: s.content.Version(), SourceURLs: []string{"https://wiki.biligame.com/umamusume/"}, ReviewStatus: "source-snapshot", ContentJSON: string(data), SHA256: hex.EncodeToString(sum[:]), ByteLength: len(data)}
	})
	return s.sortPracticePack, s.sortPracticeErr
}
