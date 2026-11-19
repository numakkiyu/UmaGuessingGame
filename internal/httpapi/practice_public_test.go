package httpapi_test

import (
	"net/http"
	"testing"
)

// No content is loaded in this harness. Public preparation must reach its
// content check (501), rather than demand or create a player identity (401).
func TestPracticePreparationDoesNotRequirePlayerIdentity(t *testing.T) {
	h := newHarness(t)
	for _, path := range []string{"/api/v1/practice-packs/classic", "/api/v1/practice-packs/sort", "/api/v1/practice-packs/memory", "/api/v1/practice-packs/aptitude-grid", "/api/v1/practice-packs/draft", "/api/v1/practice-packs/race-rebuild", "/api/v1/practice-packs/co-race-path", "/api/v1/practice-packs/strategy-race", "/api/v1/practice-packs/pedigree", "/api/v1/practice-packs/audio"} {
		r := h.do(http.MethodGet, path, nil, nil)
		if r.Status != http.StatusNotImplemented {
			t.Fatalf("%s status=%d body=%s", path, r.Status, r.Body)
		}
		if r.Header.Get("Set-Cookie") != "" {
			t.Fatalf("%s created an identity", path)
		}
	}
}
