package aptitudegrid

import (
	"fmt"
	"github.com/bhcn/umaguessinggame/internal/modes"
	"github.com/bhcn/umaguessinggame/internal/modes/modeutil"
)

// HasSolution checks whether nine distinct cards can fill the standard board.
// Transposing the board preserves the same matching, so one axis is sufficient.
func HasSolution(cards []Card) bool {
	return solvable(cards, buildBoard(Config{RowAxis: "distance"}))
}

// PracticeCards exports historical public aptitude facts, not any active board.
func PracticeCards(source modes.ContentSource) ([]Card, error) {
	cards, err := loadCards(source)
	if err != nil {
		return nil, err
	}
	out := make([]Card, 0, len(cards))
	for _, c := range cards {
		c.Aptitudes = Aptitudes{
			Surface:  modeutil.PublicGrades(c.Aptitudes.Surface, "草地", "泥地"),
			Distance: modeutil.PublicGrades(c.Aptitudes.Distance, "短", "英", "中", "长"),
			Style:    modeutil.PublicGrades(c.Aptitudes.Style, "逃", "先", "差", "追"),
		}
		out = append(out, c)
	}
	if !HasSolution(out) {
		return nil, fmt.Errorf("aptitudegrid: public practice library is not solvable")
	}
	return out, nil
}
