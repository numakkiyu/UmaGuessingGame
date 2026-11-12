package room

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"github.com/bhcn/umaguessinggame/internal/characternames"

	"github.com/bhcn/umaguessinggame/internal/httpapi/problem"
	"github.com/bhcn/umaguessinggame/internal/modes/classic"
)

// PracticeCharacter is a public profile vector, never an active online question.
type PracticeCharacter struct {
	SearchNames    *characternames.Names `json:"searchNames,omitempty"`
	LocalizedNames map[string]string     `json:"localizedNames,omitempty"`
	CharacterID    string                `json:"characterId"`
	DisplayName    string                `json:"displayName"`
	Aliases        []string              `json:"aliases"`
	ImagePath      string                `json:"imagePath"`
	Star           int                   `json:"star"`
	StarKnown      bool                  `json:"starKnown"`
	Surface        []string              `json:"surface"`
	Distance       []string              `json:"distance"`
	RunningStyle   []string              `json:"runningStyle"`
	Sex            string                `json:"sex"`
	G1             string                `json:"g1"`
	G23            string                `json:"g23"`
	G1HasJpn       bool                  `json:"g1HasJpn"`
	G23HasJpn      bool                  `json:"g23HasJpn"`
	Grade          string                `json:"grade"`
	Dormitory      string                `json:"dormitory"`
	AnswerEligible bool                  `json:"answerEligible"`
}

type PracticePack struct {
	SchemaVersion      int      `json:"schemaVersion"`
	ModeID             string   `json:"modeId"`
	RulesVersion       string   `json:"rulesVersion"`
	DatasetVersion     string   `json:"datasetVersion"`
	ContentPackVersion string   `json:"contentPackVersion"`
	SourceURLs         []string `json:"sourceUrls"`
	ReviewStatus       string   `json:"reviewStatus"`
	ContentJSON        string   `json:"contentJSON"`
	SHA256             string   `json:"sha256"`
	ByteLength         int      `json:"byteLength"`
}

// ClassicPracticePack is a whitelist export. The immutable content index makes
// it safe to compile once; instance policy is checked on every new download.
func (s *Service) ClassicPracticePack() (PracticePack, error) {
	if s == nil || !s.cfg.Policy.Allowed("classic", "local") {
		return PracticePack{}, problem.Forbidden("play-disabled", "local practice is closed on this instance")
	}
	if s.content == nil {
		return PracticePack{}, problem.NotImplemented("content-unavailable", "no public practice pack is loaded")
	}
	if adm, ok := s.content.ModeAdmission("classic"); !ok || !adm.Available {
		return PracticePack{}, problem.Unprocessable("practice-unavailable", "classic practice content is unavailable")
	}
	s.practiceOnce.Do(func() { s.practicePack, s.practiceErr = s.buildClassicPracticePack() })
	return s.practicePack, s.practiceErr
}

func (s *Service) buildClassicPracticePack() (PracticePack, error) {
	pool, err := classic.PracticeCharacters(s.content, true)
	if err != nil {
		return PracticePack{}, problem.Internal(fmt.Errorf("practice export: %w", err))
	}
	answers, err := classic.PracticeCharacters(s.content, false)
	if err != nil {
		return PracticePack{}, problem.Internal(err)
	}
	eligible := map[string]bool{}
	for _, c := range answers {
		eligible[c.ID] = c.AmbiguityGroup == ""
	}
	cards := make([]PracticeCharacter, 0, len(pool))
	for _, c := range pool {
		aliases := []string{}
		var names map[string]string
		var searchNames *characternames.Names
		if profile, ok := s.content.Character(c.ID); ok {
			aliases = append(aliases, profile.Aliases...)
			names = profile.LocalizedNames
			searchNames = profile.SearchNames
		}
		cards = append(cards, PracticeCharacter{SearchNames: searchNames, LocalizedNames: names, CharacterID: c.ID, DisplayName: c.Name, Aliases: aliases,
			ImagePath: c.ImagePath, Star: c.Star, StarKnown: c.StarKnown, Surface: append([]string{}, c.Surface...), Distance: append([]string{}, c.Distance...),
			RunningStyle: append([]string{}, c.RunningStyle...), Sex: c.Sex, G1: c.G1, G23: c.G23, G1HasJpn: c.G1HasJpn, G23HasJpn: c.G23HasJpn,
			Grade: c.Grade, Dormitory: c.Dormitory, AnswerEligible: eligible[c.ID]})
	}
	data, err := json.Marshal(struct {
		Characters []PracticeCharacter `json:"characters"`
	}{cards})
	if err != nil {
		return PracticePack{}, problem.Internal(err)
	}
	sum := sha256.Sum256(data)
	return PracticePack{SchemaVersion: 1, ModeID: "classic", RulesVersion: classic.RulesVersion,
		DatasetVersion: s.content.DatasetVersion(), ContentPackVersion: s.content.Version(),
		SourceURLs: []string{"https://wiki.biligame.com/umamusume/"}, ReviewStatus: "source-snapshot",
		ContentJSON: string(data), SHA256: hex.EncodeToString(sum[:]), ByteLength: len(data)}, nil
}
