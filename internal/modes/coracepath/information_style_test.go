package coracepath

import (
	"encoding/json"
	"testing"

	"github.com/bhcn/umaguessinggame/internal/modes"
)

func TestInformationStyleControlsPreparedEdges(t *testing.T) {
	for _, tc := range []struct {
		raw  string
		open bool
	}{
		{`{}`, false}, {`{"informationStyle":"closed"}`, false},
		{`{"informationStyle":"open"}`, true}, {`{"openBook":true}`, true},
		{`{"informationStyle":"open","openBook":true}`, true},
	} {
		t.Run(tc.raw, func(t *testing.T) {
			_, pub, _ := prepare(t, fixedPairPack(), "style", tc.raw, modes.Subject{ID: "p1", Kind: modes.SubjectPlayer})
			if pub.Config.OpenBook != tc.open || (len(pub.Edges) > 0) != tc.open {
				t.Fatal("information form and question disagree")
			}
		})
	}
	for _, raw := range []string{`{"maxEdges":-1}`, `{"maxEdges":7}`, `{"informationStyle":"unknown"}`, `{"informationStyle":"closed","openBook":true}`, `{"informationStyle":"open","openBook":false}`} {
		if err := New().ValidateConfig(json.RawMessage(raw)); err == nil {
			t.Fatalf("accepted contradictory rules: %s", raw)
		}
	}
}

func TestSupportedPathLengthFitsActionContract(t *testing.T) {
	for _, max := range []int{1, 6} {
		raw, _ := json.Marshal(map[string]int{"maxEdges": max})
		cfg, err := decodeConfig(raw)
		if err != nil || cfg.MaxEdges != max {
			t.Fatalf("supported maxEdges %d: %v", max, err)
		}
	}
}
