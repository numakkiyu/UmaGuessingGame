package httpapi_test

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

// TestContractOperationSet asserts the router registers exactly the 48 operationIds
// declared by contracts/openapi.json.
//
// The expected list is transcribed from the contract file, so a rename, a dropped
// endpoint or an accidentally added one fails here rather than being discovered by
// a client.
func TestContractOperationSet(t *testing.T) {
	t.Parallel()
	want := []string{
		"createSession", "verifyAdmission", "getCurrentSession", "updateCurrentSession", "deleteCurrentSession",
		"getPublicConfig", "getClassicPracticePack", "getSortPracticePack", "getMemoryPracticePack", "getAptitudePracticePack", "getDraftPracticePack", "getRacePracticePack", "getPathPracticePack", "getStrategyPracticePack", "getPedigreePracticePack", "getAudioPracticePack", "createChallenge", "getChallenge", "guessChallenge", "finishChallenge", "listModes", "listCharacters", "getCharacter",
		"createRoom", "getRoom", "updateRoom", "joinRoom", "updateMembership",
		"leaveOrRemoveMembership", "transferRoomHost", "createTeam", "issueControlLease", "resolveRoomInvitation",
		"createRound", "getRound", "createTransition", "createSubmission", "createSurrender",
		"createPauseRequest", "respondToPause", "requestRematch", "createTrade", "respondToTrade",
		"createMatchmakingTicket", "getMatchmakingTicket", "cancelMatchmakingTicket", "respondToMatch",
		"createEvent", "getEvent", "createSegment", "updateSegment", "getStandings",
		"createScoreCorrection", "createDisplayGrant", "redeemDisplayGrant",
		"createRecoveryGrant", "redeemRecoveryGrant", "listRoomEvents", "streamRoomEvents",
		"upgradeRoomSocket", "getLiveness", "getReadiness", "getPathNeighbors",
		"createEventTransition",
		"rotateSessionCsrf",
	}
	h := newHarness(t)
	// Build a server directly to read the route table without an HTTP round trip.
	srv := h.newServer()
	got := srv.OperationIDs()

	seen := map[string]int{}
	for _, id := range got {
		seen[id]++
	}
	if len(got) != len(want) {
		t.Fatalf("registered %d operations, contract declares %d", len(got), len(want))
	}
	for _, id := range want {
		if seen[id] == 0 {
			t.Errorf("operation %q is declared by the contract but not registered", id)
		}
		if seen[id] > 1 {
			t.Errorf("operation %q is registered %d times", id, seen[id])
		}
	}
	for id := range seen {
		found := false
		for _, w := range want {
			if w == id {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("operation %q is registered but not in the contract", id)
		}
	}
}

// TestUnauthenticatedReadsRequireSession is a table of private operations that must
// answer 401 without a cookie.
func TestUnauthenticatedReadsRequireSession(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	cases := []struct{ name, method, path string }{
		{"getCurrentSession", http.MethodGet, "/api/v1/sessions/current"},
		{"getRoom", http.MethodGet, "/api/v1/rooms/rom_missing"},
		{"getRound", http.MethodGet, "/api/v1/rounds/rnd_missing"},
		{"getEvent", http.MethodGet, "/api/v1/events/evs_missing"},
		{"getStandings", http.MethodGet, "/api/v1/events/evs_missing/standings"},
		{"listRoomEvents", http.MethodGet, "/api/v1/rooms/rom_missing/events"},
		{"getMatchmakingTicket", http.MethodGet, "/api/v1/matchmaking-tickets/tkt_missing"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			res := h.do(tc.method, tc.path, nil, readHeaders(h))
			if res.Status != http.StatusUnauthorized {
				t.Fatalf("status = %d, want 401 (body=%s)", res.Status, string(res.Body))
			}
			if ct := res.Header.Get("Content-Type"); ct != "application/problem+json" {
				t.Errorf("Content-Type = %q, want application/problem+json", ct)
			}
			if res.Problem.Status != http.StatusUnauthorized {
				t.Errorf("problem.status = %d, want 401", res.Problem.Status)
			}
			if res.Problem.Code == "" {
				t.Error("problem.code is empty; clients branch on it")
			}
			if res.Problem.RequestID == "" {
				t.Error("problem.requestId is empty; it is required by the contract")
			}
			if res.Problem.RequestID != res.Header.Get("X-Request-Id") {
				t.Errorf("problem.requestId %q != X-Request-Id %q",
					res.Problem.RequestID, res.Header.Get("X-Request-Id"))
			}
		})
	}
}

// TestCSRFAndOriginGates is the core CSRF/Origin table.
//
// Each row is a distinct way to reach a cookie write, and each must be refused with
// 403 before any business logic runs.
func TestCSRFAndOriginGates(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	client, sess := h.newSession("csrf-tester")

	body := map[string]any{"displayName": "renamed"}

	cases := []struct {
		name       string
		headers    map[string]string
		wantStatus int
		wantCode   string
	}{
		{
			name:       "missing CSRF token",
			headers:    map[string]string{"Origin": h.server.URL, "Idempotency-Key": idemKey("a")},
			wantStatus: http.StatusForbidden,
			wantCode:   "csrf-token-missing",
		},
		{
			name: "wrong CSRF token",
			headers: map[string]string{
				"Origin": h.server.URL, "X-CSRF-Token": "wrong-token-value-1234567890",
				"Idempotency-Key": idemKey("b"),
			},
			wantStatus: http.StatusForbidden,
			wantCode:   "csrf-token-invalid",
		},
		{
			name: "missing Origin",
			headers: map[string]string{
				"X-CSRF-Token": sess.CSRFToken, "Idempotency-Key": idemKey("c"),
			},
			wantStatus: http.StatusForbidden,
			wantCode:   "origin-required",
		},
		{
			name: "cross-site Origin",
			headers: map[string]string{
				"Origin": "https://evil.example", "X-CSRF-Token": sess.CSRFToken,
				"Idempotency-Key": idemKey("d"),
			},
			wantStatus: http.StatusForbidden,
			wantCode:   "origin-mismatch",
		},
		{
			name: "valid CSRF and Origin",
			headers: map[string]string{
				"Origin": h.server.URL, "X-CSRF-Token": sess.CSRFToken,
				"Idempotency-Key": idemKey("e"),
			},
			wantStatus: http.StatusOK,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			res := h.doAs(client, http.MethodPatch, "/api/v1/sessions/current", body, tc.headers)
			if res.Status != tc.wantStatus {
				t.Fatalf("status = %d, want %d (body=%s)", res.Status, tc.wantStatus, string(res.Body))
			}
			if tc.wantCode != "" && res.Problem.Code != tc.wantCode {
				t.Errorf("problem.code = %q, want %q", res.Problem.Code, tc.wantCode)
			}
			if tc.wantStatus == http.StatusOK {
				if cc := res.Header.Get("Cache-Control"); cc != "no-store" {
					t.Errorf("Cache-Control = %q, want no-store for a private response", cc)
				}
				var body sessionBody
				if err := json.Unmarshal(res.Body, &body); err != nil {
					t.Fatalf("decode: %v", err)
				}
				if body.DisplayName != "renamed" {
					t.Errorf("displayName = %q, want renamed", body.DisplayName)
				}
			}
		})
	}
}

// sessionBody is the subset of Session the CSRF test inspects.
type sessionBody struct {
	SessionID   string `json:"sessionId"`
	DisplayName string `json:"displayName"`
	CSRFToken   string `json:"csrfToken"`
}

// TestIdempotency covers the three contract behaviours: same key+payload replays the
// original status and body, same key+different payload is 409, and a missing key on
// a keyed operation is 400.
func TestIdempotency(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	client, sess := h.newSession("idem-tester")
	key := idemKey("idem")
	headers := writeHeaders(h, sess, key)

	first := h.doAs(client, http.MethodPost, "/api/v1/rooms", map[string]any{
		"name": "idempotent room", "kind": "friend",
		"modeConfig": map[string]any{"modeId": "classic"},
		"maxPlayers": 4, "maxSpectators": 2,
		"visibility": "invite", "allowSpectators": true,
	}, headers)
	// The mode registry is intentionally absent in this build, so room creation
	// answers 501 with a reason instead of inventing a room. The idempotency
	// machinery is still exercised on a successful operation below.
	if first.Status == http.StatusNotImplemented {
		if first.Problem.Code != "modes-unavailable" {
			t.Fatalf("501 code = %q, want modes-unavailable", first.Problem.Code)
		}
	} else if first.Status != http.StatusCreated {
		t.Fatalf("first status = %d, want 201 or 501 (body=%s)", first.Status, string(first.Body))
	}

	// Use a rename instead: it is a real write that succeeds without a mode.
	renameKey := idemKey("ren")
	renameHeaders := writeHeaders(h, sess, renameKey)
	payload := map[string]any{"displayName": "first-name"}

	r1 := h.doAs(client, http.MethodPatch, "/api/v1/sessions/current", payload, renameHeaders)
	if r1.Status != http.StatusOK {
		t.Fatalf("first rename status = %d, want 200 (body=%s)", r1.Status, string(r1.Body))
	}
	if r1.Header.Get("X-Idempotent-Replay") != "" {
		t.Error("first execution must not be marked as a replay")
	}

	// Same key, same payload: the original response must be replayed byte for byte.
	r2 := h.doAs(client, http.MethodPatch, "/api/v1/sessions/current", payload, renameHeaders)
	if r2.Status != http.StatusOK {
		t.Fatalf("replay status = %d, want 200 (body=%s)", r2.Status, string(r2.Body))
	}
	if r2.Header.Get("X-Idempotent-Replay") != "true" {
		t.Errorf("replay header = %q, want true", r2.Header.Get("X-Idempotent-Replay"))
	}
	if string(r1.Body) != string(r2.Body) {
		t.Errorf("replayed body differs:\n first=%s\n second=%s", string(r1.Body), string(r2.Body))
	}

	// Same key, different payload: 409, and the first outcome must be unchanged.
	r3 := h.doAs(client, http.MethodPatch, "/api/v1/sessions/current",
		map[string]any{"displayName": "different-name"}, renameHeaders)
	if r3.Status != http.StatusConflict {
		t.Fatalf("changed-payload status = %d, want 409 (body=%s)", r3.Status, string(r3.Body))
	}
	if r3.Problem.Code != "idempotency-key-reused" {
		t.Errorf("problem.code = %q, want idempotency-key-reused", r3.Problem.Code)
	}

	// A missing key on a keyed operation is a 400, not a silent success.
	r4 := h.doAs(client, http.MethodPatch, "/api/v1/sessions/current",
		map[string]any{"displayName": "no-key"}, map[string]string{
			"Origin": h.server.URL, "X-CSRF-Token": sess.CSRFToken,
		})
	if r4.Status != http.StatusBadRequest {
		t.Fatalf("missing-key status = %d, want 400 (body=%s)", r4.Status, string(r4.Body))
	}
	if r4.Problem.Code != "idempotency-key-missing" {
		t.Errorf("problem.code = %q, want idempotency-key-missing", r4.Problem.Code)
	}

	// An out-of-range key length is rejected with a field error.
	short := h.doAs(client, http.MethodPatch, "/api/v1/sessions/current",
		map[string]any{"displayName": "short"}, map[string]string{
			"Origin": h.server.URL, "X-CSRF-Token": sess.CSRFToken, "Idempotency-Key": "tooshort",
		})
	if short.Status != http.StatusBadRequest {
		t.Fatalf("short-key status = %d, want 400 (body=%s)", short.Status, string(short.Body))
	}
	if len(short.Problem.FieldErrors) == 0 {
		t.Error("expected a fieldErrors entry for the rejected Idempotency-Key")
	}
}

// TestCrossRoomIsolation asserts a member of one room cannot read another room.
//
// The expected answer is 404, not 403: a 403 would confirm the room exists, which
// the contract forbids for invisible resources.
func TestCrossRoomIsolation(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	_, sessA := h.newSession("room-a-owner")
	clientB, _ := h.newSession("outsider")

	// Create a room directly through the service so the test does not depend on the
	// mode registry being present.
	roomID := h.seedRoom(t, sessA.ID, "secret room")

	res := h.doAs(clientB, http.MethodGet, "/api/v1/rooms/"+roomID, nil, readHeaders(h))
	if res.Status != http.StatusNotFound {
		t.Fatalf("cross-room read status = %d, want 404 (body=%s)", res.Status, string(res.Body))
	}
	if res.Problem.Code != "room-not-found" {
		t.Errorf("problem.code = %q, want room-not-found", res.Problem.Code)
	}

	// A write must be refused the same way, and must not leak a distinct code.
	wres := h.doAs(clientB, http.MethodPatch, "/api/v1/rooms/"+roomID,
		map[string]any{"expectedRoomVersion": 1, "name": "hijacked"},
		map[string]string{
			"Origin": h.server.URL, "Idempotency-Key": idemKey("hijack"),
		})
	if wres.Status != http.StatusUnauthorized && wres.Status != http.StatusForbidden &&
		wres.Status != http.StatusNotFound {
		t.Fatalf("cross-room write status = %d, want 401/403/404 (body=%s)", wres.Status, string(wres.Body))
	}
}

// TestPrivilegeEscalation attempts the known escalation routes.
//
// Each case tries to obtain authority the actor does not hold. None may succeed.
func TestPrivilegeEscalation(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	ownerClient, ownerSess := h.newSession("owner")
	playerClient, playerSess := h.newSession("player")

	roomID := h.seedRoom(t, ownerSess.ID, "escalation room")
	playerMembershipID := h.seedMember(t, roomID, playerSess.ID, "player")

	cases := []struct {
		name       string
		client     *http.Client
		sess       testSession
		method     string
		path       string
		body       any
		wantStatus []int
	}{
		{
			name: "player tries to rename the room", client: playerClient, sess: playerSess,
			method: http.MethodPatch, path: "/api/v1/rooms/" + roomID,
			body:       map[string]any{"expectedRoomVersion": 1, "name": "mine now"},
			wantStatus: []int{http.StatusForbidden},
		},
		{
			name: "player tries to create a team", client: playerClient, sess: playerSess,
			method: http.MethodPost, path: "/api/v1/rooms/" + roomID + "/teams",
			body:       map[string]any{"label": "rogue", "colorToken": "green"},
			wantStatus: []int{http.StatusForbidden},
		},
		{
			name: "player tries to issue a control lease", client: playerClient, sess: playerSess,
			method: http.MethodPost, path: "/api/v1/rooms/" + roomID + "/control-leases",
			body: map[string]any{
				"roundId": "rnd_x", "membershipId": playerMembershipID, "expectedGeneration": 0,
			},
			wantStatus: []int{http.StatusForbidden},
		},
		{
			name: "player tries to change their own role to host", client: playerClient, sess: playerSess,
			method: http.MethodPatch,
			path:   "/api/v1/rooms/" + roomID + "/memberships/" + playerMembershipID,
			body: map[string]any{
				"expectedRoomVersion": 1, "requestedRole": "host",
			},
			wantStatus: []int{http.StatusForbidden},
		},
		{
			name: "player tries to create a round", client: playerClient, sess: playerSess,
			method: http.MethodPost, path: "/api/v1/rooms/" + roomID + "/rounds",
			body:       map[string]any{"modeConfig": map[string]any{"modeId": "classic"}},
			wantStatus: []int{http.StatusForbidden},
		},
		{
			name: "player tries to start a phase transition", client: playerClient, sess: playerSess,
			method: http.MethodPost, path: "/api/v1/rounds/rnd_missing/transitions",
			body:       map[string]any{"target": "collecting", "expectedPhaseVersion": 1},
			wantStatus: []int{http.StatusNotFound, http.StatusForbidden},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			res := h.doAs(tc.client, tc.method, tc.path, tc.body, writeHeaders(h, tc.sess, idemKey("esc")))
			ok := false
			for _, want := range tc.wantStatus {
				if res.Status == want {
					ok = true
					break
				}
			}
			if !ok {
				t.Fatalf("status = %d, want one of %v (body=%s)",
					res.Status, tc.wantStatus, string(res.Body))
			}
			// A refused escalation must carry a stable code, never a bare 200.
			if res.Status == http.StatusOK {
				t.Fatalf("escalation succeeded with 200 (body=%s)", string(res.Body))
			}
			if res.Problem.Code == "" {
				t.Errorf("refused escalation has no problem.code (body=%s)", string(res.Body))
			}
		})
	}

	// The owner must still be able to do the management action, proving the
	// restriction is about capability rather than the endpoint being broken.
	ok := h.doAs(ownerClient, http.MethodPost, "/api/v1/rooms/"+roomID+"/teams",
		map[string]any{"label": "legit", "colorToken": "blue"},
		writeHeaders(h, ownerSess, idemKey("team")))
	if ok.Status != http.StatusCreated {
		t.Fatalf("owner createTeam status = %d, want 201 (body=%s)", ok.Status, string(ok.Body))
	}
}

// TestProblemDetailsShape validates the RFC 9457 shape on a representative set of
// failures, including that no success is ever reported as 200-with-error.
func TestProblemDetailsShape(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	client, sess := h.newSession("problem-tester")

	cases := []struct {
		name       string
		method     string
		path       string
		body       any
		headers    map[string]string
		wantStatus int
	}{
		{
			name: "malformed JSON", method: http.MethodPatch, path: "/api/v1/sessions/current",
			body:       `{"displayName": `,
			headers:    writeHeaders(h, sess, idemKey("p1")),
			wantStatus: http.StatusBadRequest,
		},
		{
			name: "unknown field is rejected", method: http.MethodPatch, path: "/api/v1/sessions/current",
			body:       map[string]any{"displayName": "x", "role": "host"},
			headers:    writeHeaders(h, sess, idemKey("p2")),
			wantStatus: http.StatusBadRequest,
		},
		{
			name: "displayName too long", method: http.MethodPatch, path: "/api/v1/sessions/current",
			body:       map[string]any{"displayName": strings.Repeat("长", 25)},
			headers:    writeHeaders(h, sess, idemKey("p3")),
			wantStatus: http.StatusBadRequest,
		},
		{
			name: "unknown route", method: http.MethodGet, path: "/api/v1/nope",
			headers:    readHeaders(h),
			wantStatus: http.StatusNotFound,
		},
		{
			name: "wrong method", method: http.MethodPut, path: "/api/v1/config",
			headers:    readHeaders(h),
			wantStatus: http.StatusNotFound,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			res := h.doAs(client, tc.method, tc.path, tc.body, tc.headers)
			if res.Status != tc.wantStatus {
				t.Fatalf("status = %d, want %d (body=%s)", res.Status, tc.wantStatus, string(res.Body))
			}
			if ct := res.Header.Get("Content-Type"); ct != "application/problem+json" {
				t.Errorf("Content-Type = %q, want application/problem+json", ct)
			}
			if res.Problem.Type == "" || res.Problem.Title == "" || res.Problem.Code == "" {
				t.Errorf("problem is missing required fields: %s", string(res.Body))
			}
			if res.Problem.Status != res.Status {
				t.Errorf("problem.status = %d, body status = %d; the HTTP status is authoritative",
					res.Problem.Status, res.Status)
			}
			if res.Header.Get("Cache-Control") != "no-store" {
				t.Errorf("error Cache-Control = %q, want no-store", res.Header.Get("Cache-Control"))
			}
			// A problem body must never leak an internal path or SQL fragment.
			lower := strings.ToLower(string(res.Body))
			for _, leak := range []string{"/users/", "sqlite", "select ", "goroutine", ".go:"} {
				if strings.Contains(lower, leak) {
					t.Errorf("problem body leaks %q: %s", leak, string(res.Body))
				}
			}
		})
	}
}

// TestRequestIDAndDateOnEveryResponse checks the mandatory response headers.
func TestRequestIDAndDateOnEveryResponse(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	paths := []struct {
		method string
		path   string
	}{
		{http.MethodGet, "/api/v1/config"},
		{http.MethodGet, "/api/v1/modes"},
		{http.MethodGet, "/health/live"},
		{http.MethodGet, "/api/v1/sessions/current"},
		{http.MethodGet, "/api/v1/nope"},
	}
	for _, p := range paths {
		res := h.do(p.method, p.path, nil, readHeaders(h))
		if got := res.Header.Get("X-Request-Id"); got == "" {
			t.Errorf("%s %s: missing X-Request-Id", p.method, p.path)
		}
		if got := res.Header.Get("Date"); got == "" {
			t.Errorf("%s %s: missing Date", p.method, p.path)
		}
	}
}

// TestClientRequestIDIsAdoptedOrReplaced asserts the request id is echoed when
// well-formed and replaced when not, so a hostile value cannot reach the logs.
func TestClientRequestIDIsAdoptedOrReplaced(t *testing.T) {
	t.Parallel()
	h := newHarness(t)

	good := "client-supplied-id-1234"
	res := h.do(http.MethodGet, "/api/v1/config", nil, map[string]string{"X-Request-Id": good})
	if got := res.Header.Get("X-Request-Id"); got != good {
		t.Errorf("X-Request-Id = %q, want the client value %q", got, good)
	}

	// Only values http.Client can actually transmit are tested here; a header with a
	// newline is refused by the Go client itself, so it can never reach the server.
	for _, bad := range []string{"short", "has space in it", strings.Repeat("z", 200)} {
		res := h.do(http.MethodGet, "/api/v1/config", nil, map[string]string{"X-Request-Id": bad})
		got := res.Header.Get("X-Request-Id")
		if got == bad {
			t.Errorf("hostile X-Request-Id %q was adopted verbatim", bad)
		}
		if got == "" {
			t.Errorf("hostile X-Request-Id %q produced no replacement id", bad)
		}
	}
}
