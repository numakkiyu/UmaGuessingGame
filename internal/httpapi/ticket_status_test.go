package httpapi_test

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/bhcn/umaguessinggame/internal/modes"
)

// TestMatchmakingTicketStatusReachesTheWire pins the defect the frontend found:
// the service reported matched-pending-accept, the database stored it, and the
// HTTP response still said "queueing".
//
// The cause was mapTicketStatus switching on the PRE-migration vocabulary and
// falling back to "queueing" for anything unrecognised — so the new words
// (queueing / matched-pending-accept / ready) all collapsed to "queueing".
//
// This test deliberately reads the response BODY rather than the service return
// value, because a service-level test cannot see that mapping at all.
func TestMatchmakingTicketStatusReachesTheWire(t *testing.T) {
	h := newMatchmakingHarness(t)
	firstClient, first := h.newSession("first")
	secondClient, second := h.newSession("second")

	body := map[string]any{
		"modeConfig": map[string]any{"modeId": "classic", "config": map[string]any{}},
	}

	firstRec := h.doAs(firstClient, http.MethodPost, "/api/v1/matchmaking-tickets", body,
		map[string]string{"X-CSRF-Token": first.CSRFToken, "Idempotency-Key": "ticket-first-0001"})
	if firstRec.Status != http.StatusCreated {
		t.Fatalf("first ticket status = %d, want 201; body=%s", firstRec.Status, firstRec.Body)
	}
	var firstTicket struct {
		TicketID string `json:"ticketId"`
		Status   string `json:"status"`
	}
	if err := json.Unmarshal(firstRec.Body, &firstTicket); err != nil {
		t.Fatalf("decode first ticket: %v", err)
	}
	if firstTicket.Status != "queueing" {
		t.Errorf("a lone ticket must report queueing, got %q", firstTicket.Status)
	}

	secondRec := h.doAs(secondClient, http.MethodPost, "/api/v1/matchmaking-tickets", body,
		map[string]string{"X-CSRF-Token": second.CSRFToken, "Idempotency-Key": "ticket-second-001"})
	if secondRec.Status != http.StatusCreated {
		t.Fatalf("second ticket status = %d, want 201; body=%s", secondRec.Status, secondRec.Body)
	}
	var secondTicket struct {
		TicketID string `json:"ticketId"`
		Status   string `json:"status"`
	}
	if err := json.Unmarshal(secondRec.Body, &secondTicket); err != nil {
		t.Fatalf("decode second ticket: %v", err)
	}
	// The decisive assertion: the wire must report the pairing that already happened.
	if secondTicket.Status != "matched-pending-accept" {
		t.Errorf("the arriving ticket must report matched-pending-accept on the wire, got %q", secondTicket.Status)
	}

	// Re-reading must agree with the create response — an immediate GET that
	// disagreed was the second half of the same defect.
	reread := h.doAs(secondClient, http.MethodGet, "/api/v1/matchmaking-tickets/"+secondTicket.TicketID, nil, nil)
	if reread.Status != http.StatusOK {
		t.Fatalf("re-read status = %d, want 200", reread.Status)
	}
	var rereadTicket struct {
		Status string `json:"status"`
	}
	if err := json.Unmarshal(reread.Body, &rereadTicket); err != nil {
		t.Fatalf("decode re-read: %v", err)
	}
	if rereadTicket.Status != "matched-pending-accept" {
		t.Errorf("re-read must report matched-pending-accept, got %q", rereadTicket.Status)
	}
}

// TestMatchmakingPairingGivesEarlierTicketControl asserts the ownership rule over
// HTTP: the ticket created FIRST owns the room, whichever side accepts last.
func TestMatchmakingPairingGivesEarlierTicketControl(t *testing.T) {
	h := newMatchmakingHarness(t)
	firstClient, first := h.newSession("first")
	secondClient, second := h.newSession("second")
	body := map[string]any{
		"modeConfig": map[string]any{"modeId": "classic", "config": map[string]any{}},
	}

	create := func(client *http.Client, sess testSession, key string) (string, string) {
		rec := h.doAs(client, http.MethodPost, "/api/v1/matchmaking-tickets", body,
			map[string]string{"X-CSRF-Token": sess.CSRFToken, "Idempotency-Key": key})
		if rec.Status != http.StatusCreated {
			t.Fatalf("create ticket status = %d; body=%s", rec.Status, rec.Body)
		}
		var out struct {
			TicketID string `json:"ticketId"`
			Status   string `json:"status"`
		}
		if err := json.Unmarshal(rec.Body, &out); err != nil {
			t.Fatalf("decode ticket: %v", err)
		}
		return out.TicketID, out.Status
	}
	firstID, _ := create(firstClient, first, "owner-first-00001")
	secondID, _ := create(secondClient, second, "owner-second-0001")

	accept := func(client *http.Client, sess testSession, id, key string) string {
		rec := h.doAs(client, http.MethodPost, "/api/v1/matchmaking-tickets/"+id+"/responses",
			map[string]any{"accept": true},
			map[string]string{"X-CSRF-Token": sess.CSRFToken, "Idempotency-Key": key})
		if rec.Status != http.StatusOK && rec.Status != http.StatusCreated {
			t.Fatalf("accept status = %d; body=%s", rec.Status, rec.Body)
		}
		var out struct {
			RoomID string `json:"roomId"`
		}
		if err := json.Unmarshal(rec.Body, &out); err != nil {
			t.Fatalf("decode accept: %v", err)
		}
		return out.RoomID
	}

	// The earlier ticket accepts first — the order that used to hand ownership to
	// the wrong side.
	accept(firstClient, first, firstID, "accept-first-00001")
	roomID := accept(secondClient, second, secondID, "accept-second-0001")
	if roomID == "" {
		t.Fatal("both accepted but no room was returned")
	}

	readRoom := func(client *http.Client) map[string]any {
		rec := h.doAs(client, http.MethodGet, "/api/v1/rooms/"+roomID, nil, nil)
		if rec.Status != http.StatusOK {
			t.Fatalf("read room status = %d; body=%s", rec.Status, rec.Body)
		}
		var out map[string]any
		if err := json.Unmarshal(rec.Body, &out); err != nil {
			t.Fatalf("decode room: %v", err)
		}
		return out
	}
	hasManage := func(room map[string]any) bool {
		viewer, _ := room["viewer"].(map[string]any)
		caps, _ := viewer["capabilities"].([]any)
		for _, c := range caps {
			if s, _ := c.(string); s == "manage-room" {
				return true
			}
		}
		return false
	}
	if !hasManage(readRoom(firstClient)) {
		t.Error("the earlier ticket must hold manage-room over HTTP")
	}
	if hasManage(readRoom(secondClient)) {
		t.Error("the later ticket must NOT hold manage-room over HTTP")
	}
}

// stubMode is the smallest Mode that satisfies the registry so matchmaking can
// create a room. It is only used by the two matchmaking HTTP tests above.
type stubMode struct{}

func (stubMode) ID() string                                { return "classic" }
func (stubMode) RulesVersion() string                      { return "test" }
func (stubMode) Title() string                             { return "Classic" }
func (stubMode) Summary() string                           { return "stub" }
func (stubMode) InformationStyles() []string               { return []string{"closed"} }
func (stubMode) RoundWindow(json.RawMessage) time.Duration { return 0 }
func (stubMode) TurnWindow(json.RawMessage) time.Duration  { return 0 }
func (stubMode) MinSubjects() int                          { return 1 }
func (stubMode) MaxSubjects() int                          { return 8 }
func (stubMode) Requirement() modes.ContentRequirement {
	return modes.ContentRequirement{}
}
func (stubMode) ValidateConfig(json.RawMessage) error { return nil }
func (stubMode) Prepare(context.Context, modes.PrepareInput) (modes.Prepared, error) {
	return modes.Prepared{}, nil
}
func (stubMode) Apply(modes.ApplyInput) (modes.Decision, error) {
	return modes.Decision{}, nil
}
func (stubMode) Project(modes.ProjectInput) (json.RawMessage, error) {
	return json.RawMessage(`{}`), nil
}

// Settle returns one deterministic score so tests that need a settled programme
// item have something to assert. A stub that returned nothing would make the
// standings empty for reasons unrelated to the behaviour under test.
func (stubMode) Settle(in modes.SettleInput) ([]modes.Score, error) {
	// Score every participant that has state. A stub returning nothing would leave
	// the standings empty for reasons unrelated to the behaviour under test.
	// Score only subjects that actually have state. Inventing a subject would fail
	// the round_results foreign key, and the standings plumbing is already covered
	// end-to-end by the real-mode test in this file.
	out := make([]modes.Score, 0, len(in.States))
	for id := range in.States {
		out = append(out, modes.Score{
			Subject:       modes.Subject{ID: id},
			ScoreUnits:    7500,
			MaxScoreUnits: 10000,
		})
	}
	return out, nil
}

// newMatchmakingHarness builds a harness with a mode registered so the room layer
// can create a matched room.
func newMatchmakingHarness(t *testing.T) *harness {
	t.Helper()
	reg, err := modes.NewRegistry(stubMode{})
	if err != nil {
		t.Fatalf("registry: %v", err)
	}
	return newHarness(t, func(c *harnessConfig) {
		c.AllowMissingOrigin = true
		c.Modes = reg
	})
}

// TestEventSegmentFollowsItsRound pins the programme wiring: a segment reports the
// round bound to it, and its status tracks that round's phase.
//
// Before this, a segment stayed on "planned" forever, so a host console could not
// tell a queued item from a running or finished one, and segmentDTO carried no
// roundId at all.
func TestEventSegmentFollowsItsRound(t *testing.T) {
	h := newMatchmakingHarness(t)
	client, sess := h.newSession("host")
	csrf := sess.CSRFToken

	roomRec := h.doAs(client, http.MethodPost, "/api/v1/rooms", map[string]any{
		"name": "programme", "kind": "friend",
		"modeConfig": map[string]any{"modeId": "classic", "config": map[string]any{}},
		"maxPlayers": 8, "maxSpectators": 2,
		"visibility": "invite", "allowSpectators": true,
	}, map[string]string{"X-CSRF-Token": csrf, "Idempotency-Key": "prog-room-000001"})
	if roomRec.Status != http.StatusCreated {
		t.Fatalf("create room = %d; body=%s", roomRec.Status, roomRec.Body)
	}
	var roomBody struct {
		Room struct {
			RoomID string `json:"roomId"`
		} `json:"room"`
	}
	if err := json.Unmarshal(roomRec.Body, &roomBody); err != nil {
		t.Fatalf("decode room: %v", err)
	}
	roomID := roomBody.Room.RoomID

	eventRec := h.doAs(client, http.MethodPost, "/api/v1/events", map[string]any{
		"roomId": roomID, "name": "programme", "tiePolicy": "shared-rank",
	}, map[string]string{"X-CSRF-Token": csrf, "Idempotency-Key": "prog-event-00001"})
	if eventRec.Status != http.StatusCreated {
		t.Fatalf("create event = %d; body=%s", eventRec.Status, eventRec.Body)
	}
	var eventBody struct {
		EventID string `json:"eventId"`
	}
	if err := json.Unmarshal(eventRec.Body, &eventBody); err != nil {
		t.Fatalf("decode event: %v", err)
	}
	eventID := eventBody.EventID

	segRec := h.doAs(client, http.MethodPost, "/api/v1/events/"+eventID+"/segments", map[string]any{
		"label":       "first item",
		"modeConfig":  map[string]any{"modeId": "classic", "config": map[string]any{}},
		"scoreBudget": 100,
		"roundCount":  1,
	}, map[string]string{"X-CSRF-Token": csrf, "Idempotency-Key": "prog-seg-0000001"})
	if segRec.Status != http.StatusCreated {
		t.Fatalf("create segment = %d; body=%s", segRec.Status, segRec.Body)
	}
	var segBody struct {
		SegmentID string `json:"segmentId"`
	}
	if err := json.Unmarshal(segRec.Body, &segBody); err != nil {
		t.Fatalf("decode segment: %v", err)
	}

	// Read the segment back through the event.
	readSegment := func() (status, roundID string) {
		rec := h.doAs(client, http.MethodGet, "/api/v1/events/"+eventID, nil, nil)
		if rec.Status != http.StatusOK {
			t.Fatalf("read event = %d; body=%s", rec.Status, rec.Body)
		}
		var out struct {
			SegmentDetails []struct {
				Status  string `json:"status"`
				RoundID string `json:"roundId"`
			} `json:"segmentDetails"`
		}
		if err := json.Unmarshal(rec.Body, &out); err != nil {
			t.Fatalf("decode event: %v", err)
		}
		if len(out.SegmentDetails) != 1 {
			t.Fatalf("segmentDetails length = %d, want 1", len(out.SegmentDetails))
		}
		return out.SegmentDetails[0].Status, out.SegmentDetails[0].RoundID
	}

	if status, roundID := readSegment(); status != "planned" || roundID != "" {
		t.Errorf("a new segment must be planned with no round, got status=%q roundId=%q", status, roundID)
	}

	roundRec := h.doAs(client, http.MethodPost, "/api/v1/rooms/"+roomID+"/rounds", map[string]any{
		"modeConfig": map[string]any{"modeId": "classic", "config": map[string]any{}},
		"segmentId":  segBody.SegmentID,
	}, map[string]string{"X-CSRF-Token": csrf, "Idempotency-Key": "prog-round-00001"})
	if roundRec.Status != http.StatusCreated {
		t.Fatalf("create round = %d; body=%s", roundRec.Status, roundRec.Body)
	}
	var roundBody struct {
		RoundID string `json:"roundId"`
	}
	if err := json.Unmarshal(roundRec.Body, &roundBody); err != nil {
		t.Fatalf("decode round: %v", err)
	}

	if _, roundID := readSegment(); roundID != roundBody.RoundID {
		t.Errorf("the segment must report its bound round %q, got %q", roundBody.RoundID, roundID)
	}

	transition := func(target string, version int) {
		rec := h.doAs(client, http.MethodPost, "/api/v1/rounds/"+roundBody.RoundID+"/transitions", map[string]any{
			"target": target, "expectedPhaseVersion": version,
		}, map[string]string{
			"X-CSRF-Token":    csrf,
			"Idempotency-Key": "prog-tr-" + target + "-01",
		})
		if rec.Status != http.StatusOK && rec.Status != http.StatusCreated {
			t.Fatalf("transition %s = %d; body=%s", target, rec.Status, rec.Body)
		}
	}

	transition("briefing", 1)
	transition("collecting", 2)
	if status, _ := readSegment(); status != "running" {
		t.Errorf("a collecting round must put its segment in running, got %q", status)
	}

	transition("revealed", 3)
	transition("settled", 4)
	if status, _ := readSegment(); status != "completed" {
		t.Errorf("a settled round must complete its segment, got %q", status)
	}

}
