package draft

import (
	"fmt"
	"github.com/bhcn/umaguessinggame/internal/modes"
	"github.com/bhcn/umaguessinggame/internal/modes/modeutil"
)

type PracticeLibrary struct {
	Cards []Card `json:"cards"`
	Tasks []Task `json:"tasks"`
}

// PracticeContent exports only the full public market library, not a prepared
// competitive draw, reference lineup, private scope, seed or player state.
func PracticeContent(source modes.ContentSource) (PracticeLibrary, error) {
	m, err := loadMarket(source)
	if err != nil {
		return PracticeLibrary{}, err
	}
	out := PracticeLibrary{Cards: make([]Card, 0, len(m.Cards)), Tasks: m.Tasks}
	for _, c := range m.Cards {
		c.Aptitudes = Aptitudes{
			Surface:  modeutil.PublicGrades(c.Aptitudes.Surface, "草地", "泥地"),
			Distance: modeutil.PublicGrades(c.Aptitudes.Distance, "短", "英", "中", "长"),
			Style:    modeutil.PublicGrades(c.Aptitudes.Style, "逃", "先", "差", "追"),
		}
		if c.Price != PublishedPrice(c.Aptitudes) {
			return PracticeLibrary{}, fmt.Errorf("draft: public price disagrees with rules")
		}
		out.Cards = append(out.Cards, c)
	}
	if len(out.Cards) < MarketSize || len(out.Tasks) < TaskCount {
		return PracticeLibrary{}, fmt.Errorf("draft: public library too small")
	}
	return out, nil
}
