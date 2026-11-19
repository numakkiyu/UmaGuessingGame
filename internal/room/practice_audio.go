package room

import (
	"github.com/bhcn/umaguessinggame/internal/httpapi/problem"
	"github.com/bhcn/umaguessinggame/internal/modes/audio"
)

type audioPracticeClip struct {
	audio.PracticeClip
	LocalizedNames map[string]string `json:"localizedNames,omitempty"`
}
type audioPracticeLibrary struct {
	audio.PracticeLibrary
	Clips []audioPracticeClip `json:"clips"`
}

func (s *Service) AudioPracticePack() (PracticePack, error) {
	if err := s.practiceAdmission("audio"); err != nil {
		return PracticePack{}, err
	}
	s.audioPracticeOnce.Do(func() {
		library, err := audio.PracticeFacts(s.content)
		if err != nil {
			s.audioPracticeErr = problem.Unprocessable("practice-unavailable", "audio practice content unavailable")
			return
		}
		clips := make([]audioPracticeClip, 0, len(library.Clips))
		for _, clip := range library.Clips {
			clips = append(clips, audioPracticeClip{PracticeClip: clip, LocalizedNames: s.practiceNames(clip.CharacterID)})
		}
		s.audioPracticePack, s.audioPracticeErr = s.publicPracticeEnvelope("audio", audio.RulesVersion, audioPracticeLibrary{PracticeLibrary: library, Clips: clips})
	})
	return s.audioPracticePack, s.audioPracticeErr
}
