package room

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"

	"github.com/bhcn/umaguessinggame/internal/httpapi/problem"
	memorygame "github.com/bhcn/umaguessinggame/internal/modes/memory"
)

func (s *Service) MemoryPracticePack() (PracticePack, error) {
	if s == nil || !s.cfg.Policy.Allowed("memory", "local") {
		return PracticePack{}, problem.Forbidden("play-disabled", "practice is closed")
	}
	if s.content == nil {
		return PracticePack{}, problem.NotImplemented("content-unavailable", "practice content unavailable")
	}
	if adm, ok := s.content.ModeAdmission("memory"); !ok || !adm.Available {
		return PracticePack{}, problem.Unprocessable("practice-unavailable", "memory practice unavailable")
	}
	s.memoryPracticeOnce.Do(func() {
		faces, err := memorygame.PracticeFaces(s.content)
		if err != nil {
			s.memoryPracticeErr = problem.Internal(err)
			return
		}
		data, err := json.Marshal(struct {
			Faces []memorygame.PracticeFace `json:"faces"`
		}{faces})
		if err != nil {
			s.memoryPracticeErr = problem.Internal(err)
			return
		}
		sum := sha256.Sum256(data)
		s.memoryPracticePack = PracticePack{SchemaVersion: 1, ModeID: "memory", RulesVersion: memorygame.RulesVersion, DatasetVersion: s.content.DatasetVersion(), ContentPackVersion: s.content.Version(), SourceURLs: []string{"https://wiki.biligame.com/umamusume/"}, ReviewStatus: "source-snapshot", ContentJSON: string(data), SHA256: hex.EncodeToString(sum[:]), ByteLength: len(data)}
	})
	return s.memoryPracticePack, s.memoryPracticeErr
}
