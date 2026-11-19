package room

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"

	"github.com/bhcn/umaguessinggame/internal/httpapi/problem"
	"github.com/bhcn/umaguessinggame/internal/modes/aptitudegrid"
	"github.com/bhcn/umaguessinggame/internal/modes/draft"
)

func (s *Service) practiceAdmission(modeID string) error {
	if s == nil || !s.cfg.Policy.Allowed(modeID, "local") {
		return problem.Forbidden("play-disabled", "practice is closed")
	}
	if s.content == nil {
		return problem.NotImplemented("content-unavailable", "practice content unavailable")
	}
	if adm, ok := s.content.ModeAdmission(modeID); !ok || !adm.Available {
		return problem.Unprocessable("practice-unavailable", "practice unavailable")
	}
	return nil
}
func (s *Service) publicPracticeEnvelope(modeID, rules string, payload any) (PracticePack, error) {
	data, err := json.Marshal(payload)
	if err != nil {
		return PracticePack{}, problem.Internal(err)
	}
	if len(data) > 1_000_000 {
		return PracticePack{}, problem.Internal(fmt.Errorf("public practice pack exceeds byte limit"))
	}
	sum := sha256.Sum256(data)
	return PracticePack{SchemaVersion: 1, ModeID: modeID, RulesVersion: rules, DatasetVersion: s.content.DatasetVersion(), ContentPackVersion: s.content.Version(), SourceURLs: []string{"https://wiki.biligame.com/umamusume/"}, ReviewStatus: "source-snapshot", ContentJSON: string(data), SHA256: hex.EncodeToString(sum[:]), ByteLength: len(data)}, nil
}

// Display names are public metadata, independent from every grading vector.
type aptitudePracticeCard struct {
	aptitudegrid.Card
	LocalizedNames map[string]string `json:"localizedNames,omitempty"`
}
type draftPracticeCard struct {
	draft.Card
	LocalizedNames map[string]string `json:"localizedNames,omitempty"`
}

func (s *Service) practiceNames(id string) map[string]string {
	if card, ok := s.content.Character(id); ok {
		return card.LocalizedNames
	}
	return nil
}

func (s *Service) AptitudePracticePack() (PracticePack, error) {
	if err := s.practiceAdmission("aptitude-grid"); err != nil {
		return PracticePack{}, err
	}
	s.aptitudePracticeOnce.Do(func() {
		cards, err := aptitudegrid.PracticeCards(s.content)
		if err != nil {
			s.aptitudePracticeErr = problem.Internal(err)
			return
		}
		public := make([]aptitudePracticeCard, 0, len(cards))
		for _, card := range cards {
			public = append(public, aptitudePracticeCard{Card: card, LocalizedNames: s.practiceNames(card.ID)})
		}
		s.aptitudePracticePack, s.aptitudePracticeErr = s.publicPracticeEnvelope("aptitude-grid", aptitudegrid.RulesVersion, struct {
			Cards []aptitudePracticeCard `json:"cards"`
		}{public})
	})
	return s.aptitudePracticePack, s.aptitudePracticeErr
}
func (s *Service) DraftPracticePack() (PracticePack, error) {
	if err := s.practiceAdmission("draft"); err != nil {
		return PracticePack{}, err
	}
	s.draftPracticeOnce.Do(func() {
		library, err := draft.PracticeContent(s.content)
		if err != nil {
			s.draftPracticeErr = problem.Internal(err)
			return
		}
		public := make([]draftPracticeCard, 0, len(library.Cards))
		for _, card := range library.Cards {
			public = append(public, draftPracticeCard{Card: card, LocalizedNames: s.practiceNames(card.ID)})
		}
		s.draftPracticePack, s.draftPracticeErr = s.publicPracticeEnvelope("draft", draft.RulesVersion, struct {
			Cards []draftPracticeCard `json:"cards"`
			Tasks []draft.Task        `json:"tasks"`
		}{Cards: public, Tasks: library.Tasks})
	})
	return s.draftPracticePack, s.draftPracticeErr
}
