package memory

import (
	"encoding/json"
	"os"
	"testing"
)

func TestPracticeMemorySharedScoring(t *testing.T) {
	raw, err := os.ReadFile("../../../web/src/lib/domain/testdata/memory-scoring.json")
	if err != nil {
		t.Fatal(err)
	}
	var cases []struct {
		Name                       string
		Matched, Wrong, ScoreUnits int
	}
	if err = json.Unmarshal(raw, &cases); err != nil {
		t.Fatal(err)
	}
	for _, c := range cases {
		t.Run(c.Name, func(t *testing.T) {
			if got := scoreUnits(c.Matched, c.Wrong); got != c.ScoreUnits {
				t.Fatalf("got %d want %d", got, c.ScoreUnits)
			}
		})
	}
}
