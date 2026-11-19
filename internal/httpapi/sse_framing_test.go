package httpapi

import (
	"net/http/httptest"
	"strings"
	"testing"
)

func TestSSEUntrustedCursorCannotInjectFrame(t *testing.T) {
	for _, cursor := range []string{"1\nevent: forged", "1\rid: forged", "1\x00"} {
		rec := httptest.NewRecorder()
		s := &Server{}
		s.writeSSE(rec, rec, "hello", map[string]any{"cursor": cursor})
		body := rec.Body.String()
		if strings.Contains(body, "\nid:") || strings.Contains(body, "\nevent: forged") {
			t.Fatalf("injected SSE frame: %q", body)
		}
		if strings.Count(body, "\n\n") != 1 {
			t.Fatalf("bad frame boundary: %q", body)
		}
	}
	rec := httptest.NewRecorder()
	s := &Server{}
	s.writeSSE(rec, rec, "state.changed", map[string]any{"cursor": "123"})
	if !strings.Contains(rec.Body.String(), "\nid: 123\n") {
		t.Fatal("valid cursor lost")
	}
}
