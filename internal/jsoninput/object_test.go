package jsoninput

import (
	"strings"
	"testing"
)

func TestObjectRejectsAmbiguousInput(t *testing.T) {
	for _, raw := range []string{
		`null`, `[]`, `true`, `{} {}`, `{"name":1,"name":2}`,
		`{"name":1,"NAME":2}`, `{"name":1,"\u006eame":2}`,
		`{"config":{"budget":1,"budget":9}}`, `{"x":[{"a":1,"a":2}]}`,
		`{"x":` + strings.Repeat(`[`, 40) + `0` + strings.Repeat(`]`, 40) + `}`,
		string([]byte{'{', '"', 'x', '"', ':', '"', 0xff, '"', '}'}),
	} {
		if ValidateObject([]byte(raw)) == nil {
			t.Errorf("accepted %q", raw)
		}
	}
	for _, raw := range []string{`{}`, `{"name":"特别周","items":[null,1,{"x":true}]}`, `{"a":{"x":1},"b":{"x":2}}`} {
		if err := ValidateObject([]byte(raw)); err != nil {
			t.Errorf("rejected %s: %v", raw, err)
		}
	}
}
