package session

import (
	"net/http/httptest"
	"testing"
)

func TestOriginRejectsURLSuffixesAndSchemeConfusion(t *testing.T) {
	s := &Service{cfg: Config{}}
	for _, origin := range []string{"http://game.example/path", "http://game.example?x=1", "http://game.example#x", "http://user@game.example", "file://game.example", "https://game.example", "game.example", "null"} {
		r := httptest.NewRequest("POST", "http://game.example/api/v1/rooms", nil)
		r.Header.Set("Origin", origin)
		if err := s.OriginAllowed(r); err == nil {
			t.Errorf("accepted %s", origin)
		}
	}
	// Explicit public origins support TLS termination without trusting forwarded headers
	s.cfg.AllowedOrigins = []string{"https://game.example"}
	r := httptest.NewRequest("POST", "http://internal:8080/api/v1/rooms", nil)
	r.Header.Set("Origin", "https://game.example")
	if err := s.OriginAllowed(r); err != nil {
		t.Fatal(err)
	}
}
