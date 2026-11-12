package httpapi_test

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/bhcn/umaguessinggame/internal/modes"
	"github.com/bhcn/umaguessinggame/internal/modes/creative"
)

// TestRoundViewReportsRevisionPerSlot is the regression for the cross-slot defect.
func TestRoundViewReportsRevisionPerSlot(t *testing.T) {
	prompts := json.RawMessage(`{"scenes":[
		{"sceneId":"sc-01","text":"下雨天的校门口","provenance":{"kind":"authored-design-value","notOfficialData":true,"author":"test","basis":"docs/玩法与规则.md"}}
	],"constraints":[
		{"constraintId":"ct-01","text":"整段不能出现「跑」字","provenance":{"kind":"authored-design-value","notOfficialData":true,"author":"test","basis":"docs/玩法与规则.md"}}
	]}`)
	characters := json.RawMessage(`{"characters":[
		{"id":"1001","nameZh":"特别周"},{"id":"1002","nameZh":"无声铃鹿"},
		{"id":"1003","nameZh":"东海帝皇"},{"id":"1004","nameZh":"丸善斯基"}
	]}`)
	reg, _ := modes.NewRegistry(creative.New())
	h := newHarness(t, func(c *harnessConfig) {
		c.Modes = reg
		c.Content = creativeContent{prompts: prompts, characters: characters}
	})

	_, roundID, host, hostSess, player, playerSess := openModeRoom(t, h, creative.ModeID, "slotrev")

	// The player writes a pitch: that advances the WORK slot to revision 1.
	v := readRound(t, h, player, roundID)
	res := submitAction(t, h, player, playerSess, roundID, v, map[string]any{
		"kind": "creative.work", "text": "雨天校门口，她把伞借给不认识的学妹。",
	}, "slotrev-work")
	if res.Status != http.StatusCreated {
		t.Fatalf("work submission: %d %s", res.Status, res.Body)
	}

	// The view must now report work=1 and say nothing about vote (absent = 0).
	payload := readSlotRevisions(t, h, player, roundID)
	if payload.SubmissionRevisionsBySlot == nil {
		t.Fatalf("the round view published no per-slot revisions")
	}
	if got := payload.SubmissionRevisionsBySlot["work"]; got != 1 {
		t.Fatalf("work slot revision = %d, want 1 (all slots: %v)",
			got, payload.SubmissionRevisionsBySlot)
	}
	if got, present := payload.SubmissionRevisionsBySlot["vote"]; present && got != 0 {
		t.Fatalf("vote slot revision = %d before any ballot, want absent or 0", got)
	}

	// The player votes using the VOTE slot's own revision (absent → 0). This is the
	// submission that used to be impossible: the single-value field said 1.
	//
	// The ballot must name somebody ELSE's work, so the rule layer accepts it and the
	// vote slot actually advances. Voting for one's own work is refused for a
	// different reason and would leave the slot at 0, which would make this test
	// unable to tell "revision fixed" from "vote never happened".
	ownID := ownWorkID(t, h, player, roundID)
	ballot := readRound(t, h, player, roundID)
	ballotSlots := readSlotRevisions(t, h, player, roundID)
	voteRevision := ballotSlots.SubmissionRevisionsBySlot["vote"]
	peerID := peerWorkID(t, h, host, hostSess, roundID, ownID)
	ballot = readRound(t, h, player, roundID)
	res = submitActionWithRevision(t, h, player, playerSess, roundID, ballot, map[string]any{
		"kind": "creative.vote", "optionId": peerID,
	}, voteRevision, "slotrev-vote")
	if res.Status != http.StatusCreated {
		t.Fatalf("vote submission: %d %s", res.Status, res.Body)
	}
	if !strings.Contains(string(res.Body), `"accepted"`) {
		t.Fatalf("the vote was not accepted: %s", res.Body)
	}

	// After the ballot, the vote slot must read 1 while work stays 1.
	payload = readSlotRevisions(t, h, player, roundID)
	if got := payload.SubmissionRevisionsBySlot["vote"]; got != 1 {
		t.Fatalf("vote slot revision after the ballot = %d, want 1 (all slots: %v)",
			got, payload.SubmissionRevisionsBySlot)
	}
	if got := payload.SubmissionRevisionsBySlot["work"]; got != 1 {
		t.Fatalf("work slot revision = %d, want 1", got)
	}
	// The legacy single field still exists for single-slot modes.
	if payload.SubmissionRevision < 1 {
		t.Fatalf("submissionRevision = %d, want the legacy field to keep working",
			payload.SubmissionRevision)
	}

}

// slotRevisions is the part of a player round view this test reads.
type slotRevisions struct {
	SubmissionRevision        int64            `json:"submissionRevision"`
	SubmissionRevisionsBySlot map[string]int64 `json:"submissionRevisionsBySlot"`
}

// readSlotRevisions fetches the raw round view so the per-slot map can be inspected.
func readSlotRevisions(t *testing.T, h *harness, c *http.Client, roundID string) slotRevisions {
	t.Helper()
	res := h.doAs(c, http.MethodGet, "/api/v1/rounds/"+roundID, nil, readHeaders(h))
	if res.Status != http.StatusOK {
		t.Fatalf("read round: %d %s", res.Status, res.Body)
	}
	var out slotRevisions
	if err := json.Unmarshal(res.Body, &out); err != nil {
		t.Fatalf("decode round view: %v", err)
	}
	return out
}

// submitActionWithRevision posts one action with an explicit revision, so a test can
// exercise the per-slot number instead of the legacy single one.
func submitActionWithRevision(t *testing.T, h *harness, c *http.Client, sess testSession,
	roundID string, v roundEnvelope, action map[string]any, revision int64, key string) response {
	t.Helper()
	return h.doAs(c, http.MethodPost, "/api/v1/rounds/"+roundID+"/submissions", map[string]any{
		"modeAction":         action,
		"phaseVersion":       v.Round.Version,
		"submissionRevision": revision,
		"controlGeneration":  v.Generation,
	}, writeHeaders(h, sess, idemKey(key)))
}

// peerWorkID finds a work id on the ballot that is not the caller's own.
//
// The host has its own seat and its own work id, so reading the host's projection
// gives a ballot entry the player is allowed to vote for.
func peerWorkID(t *testing.T, h *harness, c *http.Client, sess testSession, roundID, ownID string) string {
	t.Helper()
	v := readRound(t, h, c, roundID)
	var pub struct {
		Ballot []struct {
			WorkID string `json:"workId"`
		} `json:"ballot"`
	}
	if err := json.Unmarshal(v.Public, &pub); err != nil {
		t.Fatalf("decode ballot: %v", err)
	}
	for _, entry := range pub.Ballot {
		if entry.WorkID != "" && entry.WorkID != ownID {
			return entry.WorkID
		}
	}
	// The host has not written a pitch yet, so have it write one and re-read.
	res := submitAction(t, h, c, sess, roundID, v, map[string]any{
		"kind": "creative.work", "text": "食堂最后一份胡萝卜蛋糕，她分给了学妹。",
	}, "slotrev-peer-work")
	if res.Status != http.StatusCreated {
		t.Fatalf("peer work submission: %d %s", res.Status, res.Body)
	}
	for _, target := range []string{"locked", "creative-voting"} {
		current := readRound(t, h, c, roundID)
		moved := h.doAs(c, http.MethodPost, "/api/v1/rounds/"+roundID+"/transitions", map[string]any{"target": target, "expectedPhaseVersion": current.Round.Version}, writeHeaders(h, sess, idemKey("slotrev-"+target)))
		if moved.Status != http.StatusCreated {
			t.Fatalf("open vote %s: %d %s", target, moved.Status, moved.Body)
		}
	}
	v = readRound(t, h, c, roundID)
	if err := json.Unmarshal(v.Public, &pub); err != nil {
		t.Fatalf("decode ballot: %v", err)
	}
	for _, entry := range pub.Ballot {
		if entry.WorkID != "" && entry.WorkID != ownID {
			return entry.WorkID
		}
	}
	t.Fatalf("no peer work on the ballot to vote for")
	return ""
}

// ownWorkID reads the caller's own work id from the projection.
func ownWorkID(t *testing.T, h *harness, c *http.Client, roundID string) string {
	t.Helper()
	v := readRound(t, h, c, roundID)
	var pub struct {
		Self *struct {
			WorkID string `json:"workId"`
		} `json:"self"`
	}
	if err := json.Unmarshal(v.Public, &pub); err != nil {
		t.Fatalf("decode public state: %v", err)
	}
	if pub.Self == nil || pub.Self.WorkID == "" {
		t.Fatalf("the caller has no work id: %s", v.Public)
	}
	return pub.Self.WorkID
}
