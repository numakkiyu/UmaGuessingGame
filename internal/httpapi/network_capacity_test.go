package httpapi_test

import (
	"encoding/json"
	"net/http"
	"strconv"
	"sync"
	"testing"

	"github.com/bhcn/umaguessinggame/internal/httpapi"
	"github.com/bhcn/umaguessinggame/internal/modes"
	"github.com/bhcn/umaguessinggame/internal/modes/classic"
)

// TestConcentratedArrivalGetsRetryAfter covers the 429 退避 half of H03.
//
// A venue's first arrivals all hit at once. A burst beyond the address budget must
// answer with a Retry-After the client can obey — a bare 429 leaves the client
// guessing — and a refusal must not mint an identity. Recovery after the window is
// covered in the e2e package, which owns a real SQLite database and a controllable
// limiter clock.
func TestConcentratedArrivalGetsRetryAfter(t *testing.T) {
	h := newHarness(t, func(c *harnessConfig) {
		// Four first arrivals fit; the fifth is the burst overflow.
		c.Limiter.SessionCreatePerIP = httpapi.Rate{PerMinute: 60, BurstTokens: 4}
	})

	for i := 0; i < 4; i++ {
		c, _ := h.newSession("入场训练员" + strconv.Itoa(i))
		if c == nil {
			t.Fatalf("arrival %d did not receive a client", i)
		}
	}

	// The fifth arrival in the same instant is refused, and the refusal must carry a
	// usable Retry-After rather than a bare 429.
	// newSession asserts 201 internally, so the overflow is probed with the harness's
	// shared client and a raw request instead.
	res := h.do(http.MethodPost, "/api/v1/sessions",
		map[string]any{"displayName": "超额训练员"}, map[string]string{"Origin": h.server.URL})
	if res.Status != http.StatusTooManyRequests {
		t.Fatalf("burst overflow status = %d, want 429; body=%s", res.Status, res.Body)
	}
	retryAfter := res.Header.Get("Retry-After")
	if retryAfter == "" {
		t.Fatalf("429 carried no Retry-After header; headers=%v", res.Header)
	}
	seconds, err := strconv.Atoi(retryAfter)
	if err != nil || seconds < 1 {
		t.Fatalf("Retry-After = %q, want a positive integer number of seconds", retryAfter)
	}
	// A refusal must not mint an identity.
	var sessions int
	if err := h.db.Read().QueryRow("SELECT COUNT(*) FROM sessions").Scan(&sessions); err != nil {
		t.Fatalf("count sessions: %v", err)
	}
	if sessions != 4 {
		t.Fatalf("sessions after a refused arrival = %d, want 4", sessions)
	}
}

// TestConcurrentJoinsDoNotOversubscribeTheRoom covers the 集中入场不超卖 half.
//
// A room with a fixed player capacity must not admit more players than that under
// concurrent joins. Overselling is what a venue sees as "the room says 4 seats but
// 7 people are in it", and a sequential test cannot catch it.
func TestConcurrentJoinsDoNotOversubscribeTheRoom(t *testing.T) {
	reg, err := modes.NewRegistry(classic.New())
	if err != nil {
		t.Fatalf("registry: %v", err)
	}
	h := newHarness(t, func(c *harnessConfig) { c.Modes = reg })
	host, owner := h.newSession("主持")
	const capacity = 4
	made := h.doAs(host, http.MethodPost, "/api/v1/rooms", map[string]any{
		"name":            "容量房",
		"kind":            "friend",
		"modeConfig":      map[string]any{"modeId": "classic", "config": map[string]any{}},
		"maxPlayers":      capacity,
		"maxSpectators":   0,
		"allowSpectators": false,
		"visibility":      "invite",
	}, writeHeaders(h, owner, idemKey("capacity-room")))
	if made.Status != http.StatusCreated {
		t.Fatalf("create room: %d %s", made.Status, made.Body)
	}
	var created struct {
		Room struct {
			ID string `json:"roomId"`
		} `json:"room"`
		Code string `json:"inviteCode"`
	}
	if err := json.Unmarshal(made.Body, &created); err != nil {
		t.Fatal(err)
	}

	// Ten clients race for four seats. The host already holds one play seat, so at
	// most capacity-1 joiners may be admitted.
	type outcome struct {
		status int
		body   string
	}
	results := make([]outcome, 10)
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			c, sess := h.newSession("抢座训练员" + strconv.Itoa(index))
			res := h.doAs(c, http.MethodPost, "/api/v1/rooms/"+created.Room.ID+"/memberships",
				map[string]any{"inviteCode": created.Code, "requestedRole": "player"},
				writeHeaders(h, sess, idemKey("capacity-join-"+strconv.Itoa(index))))
			results[index] = outcome{status: res.Status, body: string(res.Body)}
		}(i)
	}
	wg.Wait()

	admitted := 0
	for i, r := range results {
		switch r.status {
		case http.StatusCreated:
			admitted++
		case http.StatusUnprocessableEntity, http.StatusConflict:
			// Refused with a real reason (room full / already member). Either is a
			// correct refusal; the assertion below is about the total.
		default:
			t.Fatalf("joiner %d got an unexpected status %d: %s", i, r.status, r.body)
		}
	}
	if admitted > capacity-1 {
		t.Fatalf("room oversold: %d joiners admitted beyond the host for capacity %d",
			admitted, capacity)
	}
	// The stored member count must match what the server admitted: a capacity check
	// that passes but still writes the row is not a check.
	var active int
	if err := h.db.Read().QueryRow(
		`SELECT COUNT(*) FROM memberships WHERE room_id = ? AND role = 'player' AND left_at IS NULL AND revoked_at IS NULL`,
		created.Room.ID).Scan(&active); err != nil {
		t.Fatalf("count members: %v", err)
	}
	if active > capacity {
		t.Fatalf("active players = %d, capacity = %d", active, capacity)
	}
	if active != admitted+1 {
		t.Fatalf("active players = %d, admitted joiners = %d, host = 1", active, admitted)
	}
}
