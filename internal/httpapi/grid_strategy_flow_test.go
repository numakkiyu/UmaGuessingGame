package httpapi_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/bhcn/umaguessinggame/internal/modes"
	"github.com/bhcn/umaguessinggame/internal/modes/aptitudegrid"
	"github.com/bhcn/umaguessinggame/internal/modes/strategyrace"
)

// TestHTTPAptitudeGridClaimsOneCellPerSubject drives G02 through the real board.
//
// The default challenge has independent boards: each subject can occupy the same
// position without seeing or overwriting the other's choice. Only repeat claims
// on one's own board are refused. Aptitude grades stay hidden until the reveal.
func TestHTTPAptitudeGridClaimsOneCellPerSubject(t *testing.T) {
	pack := loadRealPackSource(t)
	reg, _ := modes.NewRegistry(aptitudegrid.New())
	h := newHarness(t, func(c *harnessConfig) { c.Modes = reg; c.Content = pack })

	_, roundID, host, hostSess, player, playerSess := openModeRoom(t, h, aptitudegrid.ModeID, "grid")

	v := readRound(t, h, player, roundID)
	var pub struct {
		Cells []struct {
			CellID string `json:"cellId"`
			Index  int    `json:"index"`
			// The condition carries an axis and a value, not a label: the axis names
			// which aptitude row the cell tests.
			RowCond struct {
				Axis  string `json:"axis"`
				Value string `json:"value"`
			} `json:"rowCondition"`
			ColCond struct {
				Axis  string `json:"axis"`
				Value string `json:"value"`
			} `json:"colCondition"`
		} `json:"cells"`
		Pool []struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		} `json:"pool"`
		Solvable bool `json:"solvable"`
		// The graded aptitudes are the answer and must not be published before reveal.
		Cards  json.RawMessage `json:"cards"`
		Graded json.RawMessage `json:"graded"`
	}
	if err := json.Unmarshal(v.Public, &pub); err != nil {
		t.Fatalf("decode grid public state: %v (raw=%s)", err, v.Public)
	}
	if len(pub.Cells) != aptitudegrid.CellCount {
		t.Fatalf("board has %d cells, want %d", len(pub.Cells), aptitudegrid.CellCount)
	}
	if len(pub.Pool) < aptitudegrid.CellCount {
		t.Fatalf("pool holds %d cards, want at least %d", len(pub.Pool), aptitudegrid.CellCount)
	}
	if !pub.Solvable {
		t.Fatalf("the drawn board is not solvable, so it cannot be played")
	}
	if len(pub.Cards) > 0 || len(pub.Graded) > 0 {
		t.Fatalf("the public state publishes the graded aptitudes: %s", v.Public)
	}
	if !v.CanSubmit {
		t.Fatalf("the player cannot submit in collecting: %+v", v)
	}

	// Claim one cell with a card that actually satisfies it.
	//
	// The graded aptitudes are server-side until the reveal, so the test cannot pick
	// the right card by reading the answer. It submits candidates from the published
	// pool until one is accepted, which is exactly what a player does by trial — and
	// the assertion is that at least one pool card is accepted, because a board whose
	// cells no pool card can satisfy is unsolvable and would already have failed the
	// solvable check above.
	cell := pub.Cells[0]
	var acceptedCard string
	for _, candidate := range pub.Pool {
		res := submitAction(t, h, player, playerSess, roundID, v, map[string]any{
			"kind": "grid.claim", "cellId": cell.CellID, "characterId": candidate.ID,
		}, "grid-claim-"+candidate.ID)
		if res.Status == http.StatusCreated && strings.Contains(string(res.Body), `"accepted"`) {
			acceptedCard = candidate.ID
			break
		}
		// A refused candidate must not advance the revision, so the next attempt uses
		// the same envelope.
	}
	if acceptedCard == "" {
		t.Fatalf("no pool card satisfies cell %s (%s=%s, %s=%s); the board is unplayable",
			cell.CellID, cell.RowCond.Axis, cell.RowCond.Value, cell.ColCond.Axis, cell.ColCond.Value)
	}
	// Another participant must not inherit the player's independent board.
	other := readRound(t, h, host, roundID)
	var emptyBoard struct {
		Cells []struct {
			Claim json.RawMessage `json:"claim"`
		} `json:"cells"`
	}
	if err := json.Unmarshal(other.Public, &emptyBoard); err != nil {
		t.Fatal(err)
	}
	for _, c := range emptyBoard.Cells {
		if len(c.Claim) > 0 && string(c.Claim) != "null" {
			t.Fatalf("another subject's claim leaked into independent board: %s", other.Public)
		}
	}
	// The same card and position remain available on the host's own board.
	hostClaim := submitAction(t, h, host, hostSess, roundID, other, map[string]any{
		"kind": "grid.claim", "cellId": cell.CellID, "characterId": acceptedCard,
	}, "grid-independent-host")
	if hostClaim.Status != http.StatusCreated || !strings.Contains(string(hostClaim.Body), `"accepted"`) {
		t.Fatalf("independent host claim: %d %s", hostClaim.Status, hostClaim.Body)
	}
	shared := readRound(t, h, player, roundID)
	var board struct {
		Cells []struct {
			CellID string `json:"cellId"`
			Index  int    `json:"index"`
			Claim  *struct {
				CharacterID string `json:"characterId"`
				Name        string `json:"name"`
				Turn        int    `json:"turn"`
			} `json:"claim"`
		} `json:"cells"`
	}
	if err := json.Unmarshal(shared.Public, &board); err != nil {
		t.Fatalf("decode shared board: %v", err)
	}
	var occupied int
	for _, c := range board.Cells {
		if c.Claim == nil {
			continue
		}
		occupied++
		if c.CellID != cell.CellID {
			t.Fatalf("a claim landed on cell %q, want %q", c.CellID, cell.CellID)
		}
		if c.Claim.CharacterID != acceptedCard {
			t.Fatalf("the shared claim names %q, want %q", c.Claim.CharacterID, acceptedCard)
		}
		if c.Claim.Name == "" {
			t.Fatalf("the shared claim carries no display name")
		}
		if c.Claim.Turn < 1 {
			t.Fatalf("the shared claim carries turn %d, want at least 1", c.Claim.Turn)
		}
	}
	if occupied != 1 {
		t.Fatalf("the shared board has %d occupied cells, want 1", occupied)
	}

	// A repeat claim on the player's own board must be refused.
	v = readRound(t, h, player, roundID)
	reclaim := submitAction(t, h, player, playerSess, roundID, v, map[string]any{
		"kind": "grid.claim", "cellId": cell.CellID, "characterId": acceptedCard,
	}, "grid-reclaim")
	if reclaim.Status == http.StatusCreated && strings.Contains(string(reclaim.Body), `"accepted"`) {
		t.Fatalf("a taken cell was claimed twice: %s", reclaim.Body)
	}

	// Reveal, and confirm the graded aptitudes appear only now.
	advance := func(phase string, expected int64, key string) {
		t.Helper()
		r := h.doAs(host, http.MethodPost, "/api/v1/rounds/"+roundID+"/transitions",
			map[string]any{"target": phase, "expectedPhaseVersion": expected},
			writeHeaders(h, hostSess, idemKey(key)))
		if r.Status != http.StatusCreated && r.Status != http.StatusOK {
			t.Fatalf("transition %s: %d %s", phase, r.Status, r.Body)
		}
	}
	current := readRound(t, h, player, roundID)
	advance("locked", current.Round.Version, "grid-locked")
	advance("revealed", current.Round.Version+1, "grid-revealed")

	revealed := readRound(t, h, host, roundID)
	if revealed.Round.Phase != "revealed" {
		t.Fatalf("phase after reveal = %q", revealed.Round.Phase)
	}
	// The reveal must let a player check the board: the per-cell verdict and the full
	// aptitude table appear only now.
	var revealedState struct {
		Cells []struct {
			CellID string `json:"cellId"`
			Claim  *struct {
				CharacterID string `json:"characterId"`
				Satisfied   *bool  `json:"satisfied"`
			} `json:"claim"`
		} `json:"cells"`
		Aptitudes []struct {
			CharacterID string `json:"characterId"`
			Aptitudes   struct {
				Distance map[string]string `json:"distance"`
				Style    map[string]string `json:"style"`
			} `json:"aptitudes"`
		} `json:"aptitudes"`
	}
	if err := json.Unmarshal(revealed.Public, &revealedState); err != nil {
		t.Fatalf("decode revealed grid state: %v", err)
	}
	if len(revealedState.Aptitudes) == 0 {
		t.Fatalf("the reveal published no aptitude table")
	}
	var verdicts int
	for _, c := range revealedState.Cells {
		if c.Claim == nil {
			continue
		}
		if c.Claim.Satisfied == nil {
			t.Fatalf("the revealed claim on %s carries no verdict", c.CellID)
		}
		if !*c.Claim.Satisfied {
			t.Fatalf("the accepted claim on %s is graded unsatisfied", c.CellID)
		}
		verdicts++
	}
	if verdicts != 1 {
		t.Fatalf("the reveal graded %d claims, want 1", verdicts)
	}
	// The published aptitudes must agree with the accepted claim: this is the check
	// that the grade the player is shown matches the one the server enforced.
	var graded bool
	for _, table := range revealedState.Aptitudes {
		if table.CharacterID != acceptedCard {
			continue
		}
		graded = true
		if table.Aptitudes.Distance[cell.RowCond.Value] != "A" ||
			table.Aptitudes.Style[cell.ColCond.Value] != "A" {
			t.Fatalf("the published aptitudes for %s do not show A/A at %s=%s, %s=%s: %+v",
				acceptedCard, cell.RowCond.Axis, cell.RowCond.Value,
				cell.ColCond.Axis, cell.ColCond.Value, table.Aptitudes)
		}
	}
	if !graded {
		t.Fatalf("the reveal published no aptitudes for the accepted card %s", acceptedCard)
	}
}

// TestHTTPStrategyRaceAcceptsOneActionPerTurn drives G10 through the real track.
//
// The race is turn-based: each turn accepts one action per team, and the server
// owns the clock. The HTTP facts worth locking are that a legal action is accepted,
// the turn advances, and the action catalogue comes from the track the server
// published rather than from the client's imagination.
func TestHTTPStrategyRaceAcceptsOneActionPerTurn(t *testing.T) {
	pack := loadRealPackSource(t)
	reg, _ := modes.NewRegistry(strategyrace.New())
	h := newHarness(t, func(c *harnessConfig) { c.Modes = reg; c.Content = pack })

	_, roundID, host, hostSess, player, playerSess := openModeRoom(t, h, strategyrace.ModeID, "strategy")

	v := readRound(t, h, player, roundID)
	var pub struct {
		Track *struct {
			TotalTurns      int    `json:"totalTurns"`
			DecisionSeconds int    `json:"decisionSeconds"`
			DefaultAction   string `json:"defaultAction"`
			Actions         []struct {
				Action string `json:"action"`
				Label  string `json:"label"`
			} `json:"actions"`
		} `json:"track"`
		CurrentTurn int `json:"currentTurn"`
	}
	if err := json.Unmarshal(v.Public, &pub); err != nil {
		t.Fatalf("decode strategy public state: %v (raw=%s)", err, v.Public)
	}
	if pub.Track == nil {
		t.Fatalf("the strategy round published no track: %s", v.Public)
	}
	if pub.Track.TotalTurns < 1 || len(pub.Track.Actions) < 2 {
		t.Fatalf("the published track is not playable: %+v", pub.Track)
	}
	if pub.Track.DecisionSeconds < 1 {
		t.Fatalf("the track publishes no per-turn clock: %+v", pub.Track)
	}
	// The per-turn clock must be what the round deadline actually is, not a
	// whole-race window: the screen promises 每回合 N 秒.
	if v.Round.Deadline == "" {
		t.Fatalf("a collecting strategy round carries no deadline")
	}
	if !v.CanSubmit {
		t.Fatalf("the player cannot act in the first turn: %+v", v)
	}

	// Submit one legal action, chosen from the published catalogue. The turn number
	// is part of the contract payload: the server refuses a decision aimed at a turn
	// that is already resolved, so the client must name the open turn it saw.
	action := pub.Track.Actions[0].Action
	res := submitAction(t, h, player, playerSess, roundID, v, map[string]any{
		"kind": "strategy.action", "action": action, "turn": pub.CurrentTurn,
	}, "strategy-turn1")
	if res.Status != http.StatusCreated {
		t.Fatalf("turn 1 action %q status = %d, want 201; body=%s", action, res.Status, res.Body)
	}
	if !strings.Contains(string(res.Body), `"accepted"`) {
		t.Fatalf("turn 1 action %q was not accepted: %s", action, res.Body)
	}

	// An action outside the published catalogue must be refused, not silently
	// coerced to the default: that would let a client submit anything.
	v = readRound(t, h, player, roundID)
	res = submitAction(t, h, player, playerSess, roundID, v, map[string]any{
		"kind": "strategy.action", "action": "teleport", "turn": pub.CurrentTurn,
	}, "strategy-bogus")
	if res.Status == http.StatusCreated && strings.Contains(string(res.Body), `"accepted"`) {
		t.Fatalf("an action outside the track catalogue was accepted: %s", res.Body)
	}

	// The second attempt on the SAME turn must be refused: one action per team per
	// turn is the rule that makes the race a decision rather than a spam.
	v = readRound(t, h, player, roundID)
	res = submitAction(t, h, player, playerSess, roundID, v, map[string]any{
		"kind": "strategy.action", "action": action, "turn": pub.CurrentTurn,
	}, "strategy-turn1-again")
	if res.Status == http.StatusCreated && strings.Contains(string(res.Body), `"accepted"`) {
		t.Fatalf("a second action in the same turn was accepted: %s", res.Body)
	}

	// The host cannot turn a one-turn opening into a completed eight-turn race.
	current := readRound(t, h, player, roundID)
	early := h.doAs(host, http.MethodPost, "/api/v1/rounds/"+roundID+"/transitions",
		map[string]any{"target": "locked", "expectedPhaseVersion": current.Round.Version},
		writeHeaders(h, hostSess, idemKey("strategy-early-lock")))
	if early.Status != http.StatusConflict {
		t.Fatalf("early lock: %d %s", early.Status, early.Body)
	}
	// Both real participants complete each turn. Only then may results appear.
	for turn := 1; turn <= pub.Track.TotalTurns; turn++ {
		if turn > 1 {
			v = readRound(t, h, player, roundID)
			r := submitAction(t, h, player, playerSess, roundID, v, map[string]any{"kind": "strategy.action", "action": action, "turn": turn}, fmt.Sprintf("strategy-player-%d", turn))
			if r.Status != http.StatusCreated {
				t.Fatalf("player turn %d: %d %s", turn, r.Status, r.Body)
			}
		}
		hv := readRound(t, h, host, roundID)
		r := submitAction(t, h, host, hostSess, roundID, hv, map[string]any{"kind": "strategy.action", "action": action, "turn": turn}, fmt.Sprintf("strategy-host-%d", turn))
		if r.Status != http.StatusCreated {
			t.Fatalf("host turn %d: %d %s", turn, r.Status, r.Body)
		}
	}
	// Reveal and confirm the race reaches a readable terminal state with the
	// per-team result.
	advance := func(phase string, expected int64, key string) {
		t.Helper()
		r := h.doAs(host, http.MethodPost, "/api/v1/rounds/"+roundID+"/transitions",
			map[string]any{"target": phase, "expectedPhaseVersion": expected},
			writeHeaders(h, hostSess, idemKey(key)))
		if r.Status != http.StatusCreated && r.Status != http.StatusOK {
			t.Fatalf("transition %s: %d %s", phase, r.Status, r.Body)
		}
	}
	current = readRound(t, h, player, roundID)
	if current.Round.Phase != "locked" {
		t.Fatalf("complete race phase = %s", current.Round.Phase)
	}
	advance("revealed", current.Round.Version, "strategy-revealed")
	revealed := readRound(t, h, host, roundID)
	if revealed.Round.Phase != "revealed" {
		t.Fatalf("phase after reveal = %q", revealed.Round.Phase)
	}
}
