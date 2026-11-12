// Package e2e exercises the assembled HTTP service with real storage, modes and content.
package e2e

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/bhcn/umaguessinggame/internal/httpapi"
	"github.com/bhcn/umaguessinggame/internal/observability"
	"github.com/bhcn/umaguessinggame/internal/realtime"
	"github.com/bhcn/umaguessinggame/internal/room"
	"github.com/bhcn/umaguessinggame/internal/session"
	"github.com/bhcn/umaguessinggame/internal/storage"
	"github.com/bhcn/umaguessinggame/internal/wiring"
)

// repoRoot walks up from the test's working directory to the module root.
func repoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	for i := 0; i < 6; i++ {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		dir = filepath.Dir(dir)
	}
	t.Fatalf("cannot locate module root from %s", dir)
	return ""
}

// harness is a running server plus a cookie-carrying client.
type harness struct {
	t      *testing.T
	base   string
	client *http.Client
	csrf   string
	db     *storage.DB
}

// newHarness assembles the real server. It skips when the content pack is absent.
func newHarness(t *testing.T) *harness {
	t.Helper()
	return newHarnessWith(t, nil)
}

// newHarnessWith assembles the real server with a caller-provided config mutation.
// It exists so address-limit tests can lower a rate without a second, drifting
// copy of this assembly.
func newHarnessWith(t *testing.T, mutate func(*httpapi.Config)) *harness {
	t.Helper()
	root := repoRoot(t)
	contentDir := filepath.Join(root, "content")
	if _, err := os.Stat(filepath.Join(contentDir, "current")); err != nil {
		t.Skip("no compiled content pack; run `make content-compile` first")
	}

	ctx := context.Background()
	db, err := storage.Open(ctx, storage.Config{Dir: t.TempDir()})
	if err != nil {
		t.Fatalf("open storage: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	modeSet, err := wiring.BuildModes()
	if err != nil {
		t.Fatalf("build modes: %v", err)
	}
	contentIndex, err := wiring.OpenContent(ctx, contentDir)
	if err != nil {
		t.Fatalf("open content: %v", err)
	}

	// Cookie and origin policy live on session.Config, which is the component that
	// actually writes cookies and validates Origin. httpapi.Config deliberately
	// does not duplicate them.
	// httptest assigns a random port, so a fixed Origin allow list cannot match.
	// The harness therefore relies on AllowMissingOrigin and sends no Origin
	// header. Origin enforcement itself is covered by the httpapi tests, which
	// assert that a mismatched Origin is rejected; this package deliberately does
	// not re-test that policy, only that the assembled stack works end to end.
	sessions := session.New(db, session.Config{
		AllowMissingOrigin: true,
	})
	rooms := room.New(db, room.Config{}, room.Options{
		Modes:   modeSet.Registry,
		Content: contentIndex,
		Epoch:   "e2e-epoch",
	})
	t.Cleanup(rooms.Close)

	realtimeHub, hubErr := realtime.NewHub(realtime.Config{})
	if hubErr != nil {
		t.Fatalf("build realtime hub: %v", hubErr)
	}
	t.Cleanup(func() { realtimeHub.Close() })

	cfg := httpapi.Config{
		APIVersion:        "1",
		BuildVersion:      "e2e",
		SiteName:          "UmaGuessingGame",
		DeploymentMode:    "lan",
		MaxRoomMembers:    200,
		MaxRoomSpectators: 300,
		DefaultTransport:  "auto",
		RequestTimeout:    20 * time.Second,
	}
	if mutate != nil {
		mutate(&cfg)
	}
	srv := httpapi.New(httpapi.Deps{
		Config:   cfg,
		Logger:   observability.NewLogger("error"),
		Metrics:  observability.NewMetrics(),
		Health:   &observability.Health{BuildVersion: "e2e"},
		Sessions: sessions,
		Rooms:    rooms,
		// The push hub is part of the real deployment, so the harness must include it
		// or streaming tests would exercise the polling fallback instead.
		Realtime: realtimeHub,
	})

	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)

	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatalf("cookie jar: %v", err)
	}
	return &harness{t: t, base: ts.URL, client: &http.Client{Jar: jar}, db: db}
}

// do performs a request and returns the status and decoded body.
func (h *harness) do(method, path string, body any, key string) (int, map[string]any) {
	h.t.Helper()
	var rdr io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			h.t.Fatalf("marshal body: %v", err)
		}
		rdr = bytes.NewReader(raw)
	}
	req, err := http.NewRequest(method, h.base+path, rdr)
	if err != nil {
		h.t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if h.csrf != "" {
		req.Header.Set("X-CSRF-Token", h.csrf)
	}
	if key != "" {
		req.Header.Set("Idempotency-Key", key)
	}
	resp, err := h.client.Do(req)
	if err != nil {
		h.t.Fatalf("%s %s: %v", method, path, err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	var out map[string]any
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &out); err != nil {
			h.t.Fatalf("%s %s returned non-JSON (%d): %s", method, path, resp.StatusCode, truncate(string(raw)))
		}
	}
	return resp.StatusCode, out
}

func truncate(s string) string {
	if len(s) <= 200 {
		return s
	}
	return s[:200] + "..."
}

// idem returns a unique, contract-valid idempotency key (16..128 chars).
func idem(suffix string) string {
	return "e2e-idem-key-" + suffix + "-000000"
}

// createSession opens an anonymous session and stores the CSRF token.
func (h *harness) createSession(name string) string {
	h.t.Helper()
	status, body := h.do(http.MethodPost, "/api/v1/sessions", map[string]any{"displayName": name}, "")
	if status != http.StatusCreated && status != http.StatusOK {
		h.t.Fatalf("create session status = %d body=%v", status, body)
	}
	csrf, _ := body["csrfToken"].(string)
	if csrf == "" {
		h.t.Fatalf("create session returned no csrfToken: %v", body)
	}
	h.csrf = csrf
	id, _ := body["sessionId"].(string)
	return id
}

// TestModesAreActuallyPlayable is the regression test for the defect this package
// exists to catch.
//
// The contract documents mode availability as a lobby fact, but "available" is
// only meaningful if a round can be created and a submission accepted with the
// contract's own action kind. Asserting availability alone would have passed while
// no mode could be played.
func TestModesAreActuallyPlayable(t *testing.T) {
	h := newHarness(t)
	h.createSession("e2e-player")

	status, body := h.do(http.MethodGet, "/api/v1/modes", nil, "")
	if status != http.StatusOK {
		t.Fatalf("GET /modes status = %d", status)
	}
	items, _ := body["items"].([]any)
	if len(items) == 0 {
		t.Fatal("no modes listed")
	}

	var available []string
	for _, it := range items {
		m, _ := it.(map[string]any)
		if m["availability"] == "available" {
			id, _ := m["modeId"].(string)
			available = append(available, id)
		}
	}
	if len(available) == 0 {
		t.Fatal("no mode reports itself available against the compiled pack")
	}
	t.Logf("available modes: %v", available)

	// The critical assertion: the canonical classic flow must reach an accepted
	// submission. A 422 unknown_action here means the wire kind and the domain kind
	// have drifted apart again.
	roomID := h.createRoom("classic")
	roundID := h.createRound(roomID, "classic")
	h.transition(roundID, "briefing", 1)
	h.transition(roundID, "collecting", 2)

	status, sub := h.do(http.MethodPost, "/api/v1/rounds/"+roundID+"/submissions", map[string]any{
		"modeAction":         map[string]any{"kind": "classic.guess", "characterId": "1003"},
		"phaseVersion":       3,
		"submissionRevision": 0,
		"controlGeneration":  0,
	}, idem("guess1"))
	if status != http.StatusCreated {
		t.Fatalf("classic.guess submission status = %d, want 201; body=%v\n"+
			"the contract kind and the domain kind have drifted apart", status, sub)
	}
	if sub["outcome"] != "accepted" {
		t.Fatalf("submission outcome = %v, want accepted", sub["outcome"])
	}

	// The nine-field feedback must be present and complete.
	//
	// The contract declares GuessFeedback.cells as a named object keyed by field
	// (star, surface, ...), not an array, so the assertion addresses fields by
	// name. Asserting the shape matters: an array here would force every client to
	// re-key the values and would silently diverge from the published schema.
	feedback, _ := sub["feedback"].(map[string]any)
	row, _ := feedback["row"].(map[string]any)
	cells, ok := row["cells"].(map[string]any)
	if !ok {
		t.Fatalf("cells is %T, want a named object per the contract", row["cells"])
	}
	wantFields := []string{"star", "surface", "distance", "style", "sex", "g1", "g23", "grade", "dormitory"}
	if len(cells) != len(wantFields) {
		t.Fatalf("cells has %d keys, want the nine fields", len(cells))
	}
	for _, field := range wantFields {
		raw, present := cells[field]
		if !present {
			t.Errorf("cell %q is missing from the feedback", field)
			continue
		}
		cell, _ := raw.(map[string]any)
		status, _ := cell["status"].(string)
		if status != "correct" && status != "near" && status != "wrong" {
			t.Errorf("cell %q has status %q, want correct/near/wrong", field, status)
		}
		if v, _ := cell["value"].(string); v == "" {
			t.Errorf("cell %q has an empty value", field)
		}
	}
	if feedback["remainingWrong"] == nil {
		t.Error("feedback omits remainingWrong")
	}
}

// TestFeedbackNeverLeaksTheAnswer proves the projection excludes the secret.
//
// The response is searched as raw JSON rather than by field name, because a leak
// through an unexpected nested key would otherwise go unnoticed.
func TestFeedbackNeverLeaksTheAnswer(t *testing.T) {
	h := newHarness(t)
	h.createSession("e2e-leak")
	roomID := h.createRoom("classic")
	roundID := h.createRound(roomID, "classic")
	h.transition(roundID, "briefing", 1)
	h.transition(roundID, "collecting", 2)

	h.do(http.MethodPost, "/api/v1/rounds/"+roundID+"/submissions", map[string]any{
		"modeAction":         map[string]any{"kind": "classic.guess", "characterId": "1003"},
		"phaseVersion":       3,
		"submissionRevision": 0,
		"controlGeneration":  0,
	}, idem("leak1"))

	status, view := h.do(http.MethodGet, "/api/v1/rounds/"+roundID, nil, "")
	if status != http.StatusOK {
		t.Fatalf("GET round status = %d", status)
	}
	raw, _ := json.Marshal(view)
	lowered := strings.ToLower(string(raw))
	for _, forbidden := range []string{"answercharacterid", "answercharacter", "secret"} {
		if strings.Contains(lowered, forbidden) {
			t.Errorf("round view contains %q; secret material must never be projected", forbidden)
		}
	}
}

// TestDuplicateGuessIsRejectedWithoutSpendingBudget proves the classic invariant
// that re-guessing a submitted character neither consumes budget nor wins .
func TestDuplicateGuessIsRejectedWithoutSpendingBudget(t *testing.T) {
	h := newHarness(t)
	h.createSession("e2e-dup")
	roomID := h.createRoom("classic")
	roundID := h.createRound(roomID, "classic")
	h.transition(roundID, "briefing", 1)
	h.transition(roundID, "collecting", 2)

	guess := map[string]any{"kind": "classic.guess", "characterId": "1003"}
	status, first := h.do(http.MethodPost, "/api/v1/rounds/"+roundID+"/submissions", map[string]any{
		"modeAction": guess, "phaseVersion": 3, "submissionRevision": 0, "controlGeneration": 0,
	}, idem("dup1"))
	if status != http.StatusCreated {
		t.Fatalf("first guess status = %d body=%v", status, first)
	}
	fb1, _ := first["feedback"].(map[string]any)
	remaining1, _ := fb1["remainingWrong"].(float64)

	status, second := h.do(http.MethodPost, "/api/v1/rounds/"+roundID+"/submissions", map[string]any{
		"modeAction": guess, "phaseVersion": 3, "submissionRevision": 1, "controlGeneration": 0,
	}, idem("dup2"))
	if status == http.StatusCreated {
		t.Fatalf("duplicate guess was accepted: %v", second)
	}
	code, _ := second["code"].(string)
	if code != "duplicate_guess" {
		t.Errorf("duplicate guess code = %q, want duplicate_guess", code)
	}

	// The budget must be untouched by the rejected attempt.
	_, view := h.do(http.MethodGet, "/api/v1/rounds/"+roundID, nil, "")
	priv, _ := view["privateState"].(map[string]any)
	if rem, ok := priv["remainingWrong"].(float64); ok && rem != remaining1 {
		t.Errorf("remainingWrong changed from %v to %v after a rejected duplicate", remaining1, rem)
	}
}

// TestIdempotentReplayReturnsTheOriginalReceipt proves a retry with the same key
// and payload does not create a second guess.
func TestIdempotentReplayReturnsTheOriginalReceipt(t *testing.T) {
	h := newHarness(t)
	h.createSession("e2e-idem")
	roomID := h.createRoom("classic")
	roundID := h.createRound(roomID, "classic")
	h.transition(roundID, "briefing", 1)
	h.transition(roundID, "collecting", 2)

	body := map[string]any{
		"modeAction":         map[string]any{"kind": "classic.guess", "characterId": "1005"},
		"phaseVersion":       3,
		"submissionRevision": 0,
		"controlGeneration":  0,
	}
	key := idem("replay1")
	status1, first := h.do(http.MethodPost, "/api/v1/rounds/"+roundID+"/submissions", body, key)
	if status1 != http.StatusCreated {
		t.Fatalf("first submission status = %d body=%v", status1, first)
	}
	status2, second := h.do(http.MethodPost, "/api/v1/rounds/"+roundID+"/submissions", body, key)
	if status2 != http.StatusCreated {
		t.Fatalf("replay status = %d, want the original 201", status2)
	}
	if first["submissionId"] != second["submissionId"] {
		t.Errorf("replay produced a new submission: %v vs %v", first["submissionId"], second["submissionId"])
	}

	// A different payload under the same key must conflict, not silently apply.
	changed := map[string]any{
		"modeAction":         map[string]any{"kind": "classic.guess", "characterId": "1006"},
		"phaseVersion":       3,
		"submissionRevision": 0,
		"controlGeneration":  0,
	}
	status3, conflict := h.do(http.MethodPost, "/api/v1/rounds/"+roundID+"/submissions", changed, key)
	if status3 != http.StatusConflict {
		t.Errorf("same key with a different payload status = %d, want 409; body=%v", status3, conflict)
	}
}

// createRoom opens a room in the given mode and returns its id.
func (h *harness) createRoom(modeID string) string {
	h.t.Helper()
	status, body := h.do(http.MethodPost, "/api/v1/rooms", map[string]any{
		"name":            "e2e room",
		"kind":            "friend",
		"modeConfig":      map[string]any{"modeId": modeID, "config": map[string]any{}},
		"maxPlayers":      8,
		"maxSpectators":   0,
		"visibility":      "invite",
		"allowSpectators": false,
	}, idem("room-"+modeID))
	if status != http.StatusCreated {
		h.t.Fatalf("create room status = %d body=%v", status, body)
	}
	rm, _ := body["room"].(map[string]any)
	id, _ := rm["roomId"].(string)
	if id == "" {
		h.t.Fatalf("create room returned no roomId: %v", body)
	}

	// The owner must be a player with management rights, not a host-only account
	// . Capabilities
	// are reported on the caller's viewer projection, not on the room itself.
	viewer, _ := rm["viewer"].(map[string]any)
	caps, _ := viewer["capabilities"].([]any)
	var hasPlay, hasManage bool
	for _, c := range caps {
		switch c {
		case "play":
			hasPlay = true
		case "manage-room":
			hasManage = true
		}
	}
	if !hasPlay || !hasManage {
		h.t.Errorf("room owner capabilities = %v, want both play and manage-room", caps)
	}
	return id
}

// createRound starts a round in the given mode.
func (h *harness) createRound(roomID, modeID string) string {
	h.t.Helper()
	status, body := h.do(http.MethodPost, "/api/v1/rooms/"+roomID+"/rounds", map[string]any{
		"modeConfig": map[string]any{"modeId": modeID, "config": map[string]any{}},
	}, idem("round-"+modeID))
	if status != http.StatusCreated {
		h.t.Fatalf("create round status = %d body=%v", status, body)
	}
	id, _ := body["roundId"].(string)
	if id == "" {
		h.t.Fatalf("create round returned no roundId: %v", body)
	}
	if body["contentPackVersion"] == nil || body["contentPackVersion"] == "" {
		h.t.Error("round is not bound to a content pack version")
	}
	return id
}

// transition advances the round phase.
func (h *harness) transition(roundID, target string, expected int) {
	h.t.Helper()
	status, body := h.do(http.MethodPost, "/api/v1/rounds/"+roundID+"/transitions", map[string]any{
		"target":               target,
		"expectedPhaseVersion": expected,
	}, idem("tr-"+target))
	if status != http.StatusCreated {
		h.t.Fatalf("transition to %s status = %d body=%v", target, status, body)
	}
}

var _ = fmt.Sprintf
