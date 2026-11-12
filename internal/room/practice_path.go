package room

import (
	"github.com/bhcn/umaguessinggame/internal/httpapi/problem"
	"github.com/bhcn/umaguessinggame/internal/modes/coracepath"
)

type pathPracticeNode struct {
	coracepath.Node
	LocalizedNames map[string]string `json:"localizedNames,omitempty"`
}

func (s *Service) PathPracticePack() (PracticePack, error) {
	if err := s.practiceAdmission("co-race-path"); err != nil {
		return PracticePack{}, err
	}
	s.pathPracticeOnce.Do(func() {
		nodes, edges, err := coracepath.PracticeGraph(s.content)
		if err != nil {
			s.pathPracticeErr = problem.Internal(err)
			return
		}
		public := make([]pathPracticeNode, 0, len(nodes))
		for _, node := range nodes {
			public = append(public, pathPracticeNode{Node: node, LocalizedNames: s.practiceNames(node.CharacterID)})
		}
		s.pathPracticePack, s.pathPracticeErr = s.publicPracticeEnvelope("co-race-path", coracepath.RulesVersion, struct {
			Nodes []pathPracticeNode `json:"nodes"`
			Edges []coracepath.Edge  `json:"edges"`
		}{public, edges})
	})
	return s.pathPracticePack, s.pathPracticeErr
}
