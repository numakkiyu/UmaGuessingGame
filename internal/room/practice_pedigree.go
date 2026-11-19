package room

import (
	"github.com/bhcn/umaguessinggame/internal/httpapi/problem"
	"github.com/bhcn/umaguessinggame/internal/modes/pedigree"
)

type pedigreePracticeSubject struct {
	CharacterID    string            `json:"characterId"`
	CharacterName  string            `json:"characterName"`
	PrototypeName  string            `json:"prototypeName"`
	NKBID          string            `json:"nkbId"`
	LocalizedNames map[string]string `json:"localizedNames,omitempty"`
}
type pedigreePracticeFamily struct {
	pedigree.PracticeFamily
	Subject pedigreePracticeSubject `json:"subject"`
}
type pedigreePracticeLibrary struct {
	pedigree.PracticeLibrary
	Families []pedigreePracticeFamily `json:"families"`
}

func (s *Service) PedigreePracticePack() (PracticePack, error) {
	if err := s.practiceAdmission("pedigree"); err != nil {
		return PracticePack{}, err
	}
	s.pedigreePracticeOnce.Do(func() {
		library, err := pedigree.PracticeFacts(s.content)
		if err != nil {
			s.pedigreePracticeErr = problem.Internal(err)
			return
		}
		families := make([]pedigreePracticeFamily, 0, len(library.Families))
		for _, family := range library.Families {
			subject := family.Subject
			families = append(families, pedigreePracticeFamily{PracticeFamily: family, Subject: pedigreePracticeSubject{CharacterID: subject.CharacterID, CharacterName: subject.CharacterName, PrototypeName: subject.PrototypeName, NKBID: subject.NKBID, LocalizedNames: s.practiceNames(subject.CharacterID)}})
		}
		s.pedigreePracticePack, s.pedigreePracticeErr = s.publicPracticeEnvelope("pedigree", pedigree.RulesVersion, pedigreePracticeLibrary{PracticeLibrary: library, Families: families})
	})
	return s.pedigreePracticePack, s.pedigreePracticeErr
}
