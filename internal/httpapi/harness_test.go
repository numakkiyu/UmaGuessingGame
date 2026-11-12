package httpapi_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/bhcn/umaguessinggame/internal/admission"
	"github.com/bhcn/umaguessinggame/internal/challenge"
	"github.com/bhcn/umaguessinggame/internal/deployment"
	"github.com/bhcn/umaguessinggame/internal/httpapi"
	"github.com/bhcn/umaguessinggame/internal/modes"
	"github.com/bhcn/umaguessinggame/internal/observability"
	"github.com/bhcn/umaguessinggame/internal/room"
	"github.com/bhcn/umaguessinggame/internal/session"
	"github.com/bhcn/umaguessinggame/internal/storage"
)

// harness is a real HTTP server backed by a real SQLite database in t.TempDir().
//
// Nothing is mocked at the storage or HTTP layer: the assertions below are about
// actual responses from an actual handler chain writing to actual files, which is
// what the delivery rules require ("测试用 t.TempDir() 建真实 SQLite").
type harness struct {
	t      *testing.T
	server *httptest.Server
	db     *storage.DB
	rooms  *room.Service

	mu     sync.Mutex
	client *http.Client
}

// harnessConfig customises the harness for a specific test.
type harnessConfig struct {
	Limiter         httpapi.LimiterConfig
	Admission       *admission.Service
	Content         room.ContentIndex
	Policy          deployment.Policy
	ChallengeSource modes.ContentSource
	// SecureCookies marks cookies Secure; false is needed so an httptest
	// http:// URL still carries them.
	SecureCookies bool
	// AllowedOrigins overrides the same-origin default.
	AllowedOrigins []string
	// AllowMissingOrigin lets a test omit Origin deliberately.
	AllowMissingOrigin bool
	// Modes, when set, is registered on the server so routes that need a game mode
	// work. Tests that do not care leave it nil.
	Modes *modes.Registry
	// QueueCapacity overrides the per-room queue bound.
	QueueCapacity int
	// Ready overrides readiness.
	Ready func(ctx context.Context) (bool, []string)
}

func newHarness(t *testing.T, mutate ...func(*harnessConfig)) *harness {
	t.Helper()
	cfg := harnessConfig{QueueCapacity: 128}
	for _, m := range mutate {
		m(&cfg)
	}
	dir := t.TempDir()
	db, err := storage.Open(context.Background(), storage.Config{Dir: dir, ReadConns: 2})
	if err != nil {
		t.Fatalf("open storage: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	sessions := session.New(db, session.Config{
		TTL:                time.Hour,
		DisplayTTL:         time.Hour,
		Secure:             cfg.SecureCookies,
		AllowedOrigins:     cfg.AllowedOrigins,
		AllowMissingOrigin: cfg.AllowMissingOrigin,
	})
	rooms := room.New(db, room.Config{QueueCapacity: cfg.QueueCapacity, Policy: cfg.Policy},
		room.Options{Epoch: "test-epoch", Modes: cfg.Modes, Content: cfg.Content})
	t.Cleanup(rooms.Close)

	// The logger writes to a discard target so a failing assertion is not buried in
	// structured noise, and so no test output can leak a request body.
	logger := slog.New(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelError}))

	var challenges *challenge.Service
	if cfg.ChallengeSource != nil {
		keys, err := challenge.LoadKeys(filepath.Join(dir, "verified-keys.json"))
		if err != nil {
			t.Fatal(err)
		}
		challenges = challenge.New(db, cfg.ChallengeSource, keys, deployment.Policy{}, true)
	}
	srv := httpapi.New(httpapi.Deps{
		Challenges: challenges,
		Admission:  cfg.Admission,
		// Cookie and origin policy is applied on the session service below; the
		// queue bound on the room service. httpapi.Config carries no copy of them,
		// so there is exactly one place a test can change and be sure it took effect.
		Config: httpapi.Config{
			Limiter:           cfg.Limiter,
			APIVersion:        "1",
			BuildVersion:      "test",
			SiteName:          "UmaGuessingGame",
			DeploymentMode:    "lan",
			MaxRoomMembers:    200,
			MaxRoomSpectators: 300,
			DefaultTransport:  "auto",
			MetricsLocalOnly:  true,
			RequestTimeout:    10 * time.Second,
		},
		Logger:   logger,
		Metrics:  observability.NewMetrics(),
		Sessions: sessions,
		Rooms:    rooms,
		Ready:    cfg.Ready,
	})

	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)

	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatalf("cookie jar: %v", err)
	}
	return &harness{
		t:      t,
		server: ts,
		db:     db,
		rooms:  rooms,
		client: &http.Client{Jar: jar, Timeout: 10 * time.Second},
	}
}

// response is a decoded API response.
type response struct {
	Status  int
	Header  http.Header
	Body    []byte
	Problem struct {
		Type        string `json:"type"`
		Title       string `json:"title"`
		Status      int    `json:"status"`
		Detail      string `json:"detail"`
		Instance    string `json:"instance"`
		Code        string `json:"code"`
		RequestID   string `json:"requestId"`
		Retryable   *bool  `json:"retryable"`
		FieldErrors []struct {
			Field   string `json:"field"`
			Message string `json:"message"`
		} `json:"fieldErrors"`
	}
}

// do issues one request. headers are applied verbatim.
func (h *harness) do(method, path string, body any, headers map[string]string) response {
	h.t.Helper()
	var reader io.Reader
	if body != nil {
		switch v := body.(type) {
		case []byte:
			reader = bytes.NewReader(v)
		case string:
			reader = strings.NewReader(v)
		default:
			buf, err := json.Marshal(v)
			if err != nil {
				h.t.Fatalf("marshal body: %v", err)
			}
			reader = bytes.NewReader(buf)
		}
	}
	req, err := http.NewRequest(method, h.server.URL+path, reader)
	if err != nil {
		h.t.Fatalf("build request: %v", err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	res, err := h.client.Do(req)
	if err != nil {
		h.t.Fatalf("%s %s: %v", method, path, err)
	}
	defer res.Body.Close()
	buf, err := io.ReadAll(res.Body)
	if err != nil {
		h.t.Fatalf("read body: %v", err)
	}
	out := response{Status: res.StatusCode, Header: res.Header, Body: buf}
	if strings.HasPrefix(res.Header.Get("Content-Type"), "application/problem+json") {
		if err := json.Unmarshal(buf, &out.Problem); err != nil {
			h.t.Fatalf("decode problem: %v (%s)", err, string(buf))
		}
	}
	return out
}

// session is a created anonymous session with its CSRF token.
type testSession struct {
	ID          string `json:"sessionId"`
	DisplayName string `json:"displayName"`
	CSRFToken   string `json:"csrfToken"`
	ExpiresAt   string `json:"expiresAt"`
	ServerTime  string `json:"serverTime"`
}

// newSession creates a session on its own cookie jar so two identities can coexist.
func (h *harness) newSession(name string) (*http.Client, testSession) {
	h.t.Helper()
	jar, err := cookiejar.New(nil)
	if err != nil {
		h.t.Fatalf("cookie jar: %v", err)
	}
	client := &http.Client{Jar: jar, Timeout: 10 * time.Second}
	body, err := json.Marshal(map[string]string{"displayName": name})
	if err != nil {
		h.t.Fatalf("marshal: %v", err)
	}
	req, err := http.NewRequest(http.MethodPost, h.server.URL+"/api/v1/sessions", bytes.NewReader(body))
	if err != nil {
		h.t.Fatalf("build: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", h.server.URL)
	res, err := client.Do(req)
	if err != nil {
		h.t.Fatalf("create session: %v", err)
	}
	defer res.Body.Close()
	buf, err := io.ReadAll(res.Body)
	if err != nil {
		h.t.Fatalf("read: %v", err)
	}
	if res.StatusCode != http.StatusCreated {
		h.t.Fatalf("create session status = %d, want 201, body=%s", res.StatusCode, string(buf))
	}
	var sess testSession
	if err := json.Unmarshal(buf, &sess); err != nil {
		h.t.Fatalf("decode session: %v", err)
	}
	if sess.CSRFToken == "" {
		h.t.Fatalf("create session returned an empty csrfToken")
	}
	return client, sess
}

// doAs issues a request as a specific identity.
func (h *harness) doAs(client *http.Client, method, path string, body any, headers map[string]string) response {
	h.t.Helper()
	var reader io.Reader
	if body != nil {
		buf, err := json.Marshal(body)
		if err != nil {
			h.t.Fatalf("marshal body: %v", err)
		}
		reader = bytes.NewReader(buf)
	}
	req, err := http.NewRequest(method, h.server.URL+path, reader)
	if err != nil {
		h.t.Fatalf("build request: %v", err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	res, err := client.Do(req)
	if err != nil {
		h.t.Fatalf("%s %s: %v", method, path, err)
	}
	defer res.Body.Close()
	buf, err := io.ReadAll(res.Body)
	if err != nil {
		h.t.Fatalf("read body: %v", err)
	}
	out := response{Status: res.StatusCode, Header: res.Header, Body: buf}
	if strings.HasPrefix(res.Header.Get("Content-Type"), "application/problem+json") {
		if err := json.Unmarshal(buf, &out.Problem); err != nil {
			h.t.Fatalf("decode problem: %v (%s)", err, string(buf))
		}
	}
	return out
}

// writeHeaders builds the headers a cookie write requires.
func writeHeaders(h *harness, sess testSession, key string) map[string]string {
	headers := map[string]string{
		"X-CSRF-Token": sess.CSRFToken,
		"Origin":       h.server.URL,
	}
	if key != "" {
		headers["Idempotency-Key"] = key
	}
	return headers
}

// readHeaders builds the headers a read requires (same-origin only).
func readHeaders(h *harness) map[string]string {
	return map[string]string{"Origin": h.server.URL}
}

// idemKey returns a valid 32-character idempotency key.
func idemKey(suffix string) string {
	base := "test-idempotency-key-0000000000"
	if len(suffix) == 0 {
		return base
	}
	return base[:32-len(suffix)] + suffix
}

// createRoom creates a room as the given identity and returns the response.
func (h *harness) createRoom(client *http.Client, sess testSession, name string) (map[string]any, response) {
	h.t.Helper()
	res := h.doAs(client, http.MethodPost, "/api/v1/rooms", map[string]any{
		"name":            name,
		"kind":            "friend",
		"modeConfig":      map[string]any{"modeId": "classic"},
		"maxPlayers":      8,
		"maxSpectators":   10,
		"visibility":      "invite",
		"allowSpectators": true,
	}, writeHeaders(h, sess, idemKey("room")))
	var body map[string]any
	if res.Status == http.StatusCreated {
		if err := json.Unmarshal(res.Body, &body); err != nil {
			h.t.Fatalf("decode room creation: %v", err)
		}
	}
	return body, res
}

// TestMain keeps the harness's temporary directories out of the repository and
// verifies the test binary is not accidentally pointed at a real data directory.
func TestMain(m *testing.M) {
	if dir := os.Getenv("UMA_DATA_DIR"); dir != "" && !strings.Contains(dir, os.TempDir()) {
		// A test run must never touch a configured production data directory.
		os.Exit(1)
	}
	_ = filepath.Join // keep filepath referenced for future helpers
	os.Exit(m.Run())
}

// newServer builds a fresh server sharing the harness database, used by tests that
// need to inspect the route table without an HTTP round trip.
func (h *harness) newServer() *httpapi.Server {
	h.t.Helper()
	sessions := session.New(h.db, session.Config{TTL: time.Hour, DisplayTTL: time.Hour})
	rooms := room.New(h.db, room.Config{QueueCapacity: 128}, room.Options{Epoch: "test-epoch"})
	return httpapi.New(httpapi.Deps{
		Config:   httpapi.Config{BuildVersion: "test", RequestTimeout: 5 * time.Second},
		Logger:   slog.New(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelError})),
		Metrics:  observability.NewMetrics(),
		Sessions: sessions,
		Rooms:    rooms,
	})
}

// seedRoom inserts a room owned by sessionID directly, so tests that are not about
// room creation do not depend on the mode registry.
func (h *harness) seedRoom(t *testing.T, sessionID, name string) string {
	t.Helper()
	ctx := context.Background()
	roomID := "rom_seed_" + name
	roomID = strings.ReplaceAll(roomID, " ", "_")
	now := time.Now().UTC().UnixMilli()
	if _, err := h.db.Write().ExecContext(ctx, `
		INSERT INTO rooms (id, invite_code, owner_session_id, name, status, room_version,
		                   config_json, capacity, created_at, updated_at)
		VALUES (?, ?, ?, ?, 'lobby', 1, ?, 50, ?, ?)`,
		roomID, "CODE"+strings.ToUpper(name[:1])+"2345", sessionID, name,
		`{"kind":"friend","maxPlayers":8,"maxSpectators":10,"visibility":"invite","allowSpectators":true}`,
		now, now); err != nil {
		t.Fatalf("seed room: %v", err)
	}
	if _, err := h.db.Write().ExecContext(ctx, `
		INSERT INTO memberships (id, room_id, session_id, role, team_id, ready, joined_at)
		VALUES (?, ?, ?, 'player', NULL, 0, ?)`,
		"mem_seed_owner_"+roomID, roomID, sessionID, now); err != nil {
		t.Fatalf("seed owner membership: %v", err)
	}
	if _, err := h.db.Write().ExecContext(ctx, `
		INSERT INTO room_grants (id, room_id, session_id, capability, granted_at)
		VALUES (?, ?, ?, 'manage-room', ?)`,
		"grt_seed_owner_"+roomID, roomID, sessionID, now); err != nil {
		t.Fatalf("seed owner grant: %v", err)
	}
	return roomID
}

// seedMember adds a member to a seeded room and returns its membership id.
func (h *harness) seedMember(t *testing.T, roomID, sessionID, role string) string {
	t.Helper()
	membershipID := "mem_seed_" + role + "_" + sessionID
	now := time.Now().UTC().UnixMilli()
	if _, err := h.db.Write().ExecContext(context.Background(), `
		INSERT INTO memberships (id, room_id, session_id, role, team_id, ready, joined_at)
		VALUES (?, ?, ?, ?, NULL, 0, ?)`,
		membershipID, roomID, sessionID, role, now); err != nil {
		t.Fatalf("seed member: %v", err)
	}
	return membershipID
}

// seedRound inserts a prepared round with a secret answer in the server-only table.
func (h *harness) seedRound(t *testing.T, roomID, secret string) string {
	t.Helper()
	return h.seedRoundWithDeadlinePhase(t, roomID, secret, "PREPARED", time.Now().Add(10*time.Minute))
}

// seedRoundWithDeadline inserts a round in the given phase with an explicit deadline.
func (h *harness) seedRoundWithDeadline(t *testing.T, roomID, phase string, deadline time.Time) string {
	t.Helper()
	return h.seedRoundWithDeadlinePhase(t, roomID, "SECRET", phase, deadline)
}

func (h *harness) seedRoundWithDeadlinePhase(t *testing.T, roomID, secret, phase string, deadline time.Time) string {
	t.Helper()
	ctx := context.Background()
	roundID := "rnd_seed_" + roomID
	now := time.Now().UTC().UnixMilli()
	status := strings.ToLower(phase)
	if _, err := h.db.Write().ExecContext(ctx, `
		INSERT INTO rounds (id, room_id, ordinal, mode_id, rules_version, dataset_version,
		                    content_pack_version, status, phase_version, deadline_at, seed,
		                    config_json, created_at, updated_at)
		VALUES (?, ?, 1, 'classic', 'r1', 'd1', 'c1', ?, 1, ?, 'seed-1', '{}', ?, ?)`,
		roundID, roomID, status, deadline.UTC().UnixMilli(), now, now); err != nil {
		t.Fatalf("seed round: %v", err)
	}
	// The secret answer lives only in round_private_state; it must never appear in a
	// response before the reveal phase.
	if _, err := h.db.Write().ExecContext(ctx, `
		INSERT INTO round_private_state (round_id, scope, state_json) VALUES (?, 'round', ?)`,
		roundID, `{"answerCharacterId":"`+secret+`"}`); err != nil {
		t.Fatalf("seed private state: %v", err)
	}
	if _, err := h.db.Write().ExecContext(ctx, `
		INSERT INTO round_private_state (round_id, scope, state_json) VALUES (?, 'public', ?)`,
		roundID, `{"modeId":"classic","maxWrongGuesses":8,"winPolicy":"solo"}`); err != nil {
		t.Fatalf("seed public state: %v", err)
	}
	return roundID
}

// newDisplaySession creates a restricted display session on its own cookie jar.
func (h *harness) newDisplaySession(t *testing.T) (*http.Client, testSession) {
	t.Helper()
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatalf("cookie jar: %v", err)
	}
	client := &http.Client{Jar: jar, Timeout: 10 * time.Second}
	// A display session is created server-side and its token is set directly on the
	// jar, because the only public path is a grant redemption.
	sessions := session.New(h.db, session.Config{TTL: time.Hour, DisplayTTL: time.Hour})
	res, err := sessions.CreateDisplay(context.Background())
	if err != nil {
		t.Fatalf("create display session: %v", err)
	}
	u, err := url.Parse(h.server.URL)
	if err != nil {
		t.Fatalf("parse server url: %v", err)
	}
	jar.SetCookies(u, []*http.Cookie{{
		Name: "uma_display", Value: res.Token, Path: "/", HttpOnly: true,
	}})
	return client, testSession{
		ID:          res.Session.ID,
		DisplayName: res.Session.DisplayName,
		CSRFToken:   res.CSRFToken,
		ExpiresAt:   res.Session.ExpiresAt.Format(time.RFC3339),
	}
}
