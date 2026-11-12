package room

// Settlement clears surrendered subjects' scores centrally so every mode applies the same rule.

import (
	"encoding/json"
	"testing"
)

func TestIsSurrenderedReadsTheFlag(t *testing.T) {
	cases := []struct {
		name string
		raw  string
		want bool
	}{
		{"surrendered", `{"surrendered":true}`, true},
		{"with other keys", `{"attempts":2,"surrendered":true,"guessed":["x"]}`, true},
		{"explicitly false", `{"surrendered":false}`, false},
		{"no flag", `{"attempts":2}`, false},
		{"empty", ``, false},
		{"malformed", `{not json`, false},
		{"null", `null`, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := isSurrendered(json.RawMessage(c.raw)); got != c.want {
				t.Fatalf("isSurrendered(%q) = %v, want %v", c.raw, got, c.want)
			}
		})
	}
}

func TestAnnotateSurrenderExplainsTheZero(t *testing.T) {
	t.Run("keeps the mode's own fields", func(t *testing.T) {
		got := annotateSurrender(json.RawMessage(`{"attempts":3,"solved":true}`))
		var obj map[string]any
		if err := json.Unmarshal(got, &obj); err != nil {
			t.Fatalf("result is not JSON: %v (%s)", err, got)
		}
		if obj["surrendered"] != true {
			t.Fatalf("surrendered flag missing: %s", got)
		}
		if obj["attempts"] != float64(3) || obj["solved"] != true {
			t.Fatalf("mode fields were dropped: %s", got)
		}
	})

	t.Run("empty explanation still explains", func(t *testing.T) {
		got := annotateSurrender(nil)
		var obj map[string]any
		if err := json.Unmarshal(got, &obj); err != nil {
			t.Fatalf("result is not JSON: %v (%s)", err, got)
		}
		if obj["surrendered"] != true {
			t.Fatalf("surrendered flag missing: %s", got)
		}
	})

	t.Run("non-object explanation is left alone", func(t *testing.T) {
		// A mode that explains with a bare string must not have it rewritten into an
		// object: the annotation would then be lying about the explanation's shape.
		in := json.RawMessage(`"a plain sentence"`)
		got := annotateSurrender(in)
		if string(got) != string(in) {
			t.Fatalf("annotateSurrender rewrote a non-object: %s", got)
		}
	})
}
