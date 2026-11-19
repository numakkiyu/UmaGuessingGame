package httpapi_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
	"time"
)

type challengeSource struct{}

func (challengeSource) Version() string { return "pack" }
func (challengeSource) Section(name string) (json.RawMessage, bool) {
	if name != "classic.characters" {
		return nil, false
	}
	return json.RawMessage(`{"cards":[{"id":"a","name":"甲","star":3},{"id":"b","name":"乙","star":1}]}`), true
}
func TestChallengeHTTPRejectsClaimedScoresAndEnforcesIdentity(t *testing.T) {
	h := newHarness(t, func(c *harnessConfig) { c.ChallengeSource = challengeSource{} })
	client, sess := h.newSession("挑战者")
	other, otherSess := h.newSession("旁观者")
	request := map[string]any{"modeId": "classic", "clientTimestamp": time.Now().UnixMilli(), "nonce": "abcdefgh12345678", "wrongBudget": 4}
	// The caller cannot ask the endpoint to sign a chosen answer or score.
	request["answerId"] = "a"
	bad := h.doAs(client, http.MethodPost, "/api/v1/challenges", request, writeHeaders(h, sess, idemKey("bad-create")))
	if bad.Status != 400 {
		t.Fatalf("client answer accepted: %d %s", bad.Status, bad.Body)
	}
	delete(request, "answerId")
	created := h.doAs(client, http.MethodPost, "/api/v1/challenges", request, writeHeaders(h, sess, idemKey("challenge-create")))
	if created.Status != 201 {
		t.Fatalf("create %d %s", created.Status, created.Body)
	}
	var view struct {
		ChallengeID string `json:"challengeId"`
		Ticket      string `json:"ticket"`
		Answer      any    `json:"answer"`
	}
	if err := json.Unmarshal(created.Body, &view); err != nil {
		t.Fatal(err)
	}
	if view.Answer != nil {
		t.Fatal("answer leaked before reveal")
	}
	path := fmt.Sprintf("/api/v1/challenges/%s", view.ChallengeID)
	got := h.doAs(other, http.MethodGet, path, nil, nil)
	if got.Status != 404 {
		t.Fatalf("cross-session read: %d", got.Status)
	}
	body := map[string]any{"ticket": view.Ticket, "characterId": "a", "expectedRevision": 0, "correct": true, "scoreUnits": 10000}
	bad = h.doAs(client, http.MethodPost, path+"/guesses", body, writeHeaders(h, sess, idemKey("bad-score")))
	if bad.Status != 400 {
		t.Fatalf("client score accepted: %d %s", bad.Status, bad.Body)
	}
	delete(body, "correct")
	delete(body, "scoreUnits")
	got = h.doAs(other, http.MethodPost, path+"/guesses", body, writeHeaders(h, otherSess, idemKey("other-guess")))
	if got.Status != 403 {
		t.Fatalf("cross-session action: %d %s", got.Status, got.Body)
	}
	headers := writeHeaders(h, sess, idemKey("first-guess"))
	delete(headers, "X-CSRF-Token")
	got = h.doAs(client, http.MethodPost, path+"/guesses", body, headers)
	if got.Status != 403 {
		t.Fatalf("csrf missing: %d", got.Status)
	}
	got = h.doAs(client, http.MethodPost, path+"/guesses", body, writeHeaders(h, sess, idemKey("first-guess")))
	if got.Status != 200 {
		t.Fatalf("guess: %d %s", got.Status, got.Body)
	}
}
