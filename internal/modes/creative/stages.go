package creative

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"

	"github.com/bhcn/umaguessinggame/internal/modes"
	"github.com/bhcn/umaguessinggame/internal/modes/modeutil"
)

// Stage and Windows are read by the room's authorised transition/deadline path.
func Stage(public json.RawMessage) (string, error) {
	p, err := decodePublic(public)
	if err != nil {
		return "", err
	}
	if !p.Config.SeparateWindows || p.Subphase == "" {
		return "legacy", nil
	}
	return p.Subphase, nil
}

func Windows(config json.RawMessage) (writeMS, voteMS int64, err error) {
	c, err := decodeConfig(config)
	return int64(c.WriteSeconds) * 1000, int64(c.VoteSeconds) * 1000, err
}

func CloseWriting(public json.RawMessage) (json.RawMessage, error) {
	p, err := decodePublic(public)
	if err != nil {
		return nil, err
	}
	if !p.Config.SeparateWindows || p.Subphase != "writing" {
		return nil, fmt.Errorf("creative: writing is not open")
	}
	p.Subphase = "review"
	return modeutil.Marshal(p), nil
}

// OpenVoting atomically freezes the host's exclusions. Review is not a writing
// window and never changes texts or adds a missing work on someone's behalf.
func OpenVoting(public, private json.RawMessage, excluded []string) (json.RawMessage, error) {
	p, err := decodePublic(public)
	if err != nil {
		return nil, err
	}
	priv, err := decodePrivate(private)
	if err != nil {
		return nil, err
	}
	if !p.Config.SeparateWindows || p.Subphase != "review" {
		return nil, fmt.Errorf("creative: review is not open")
	}
	if len(excluded) > MaxSubjects {
		return nil, fmt.Errorf("creative: too many exclusions")
	}
	drop := map[string]bool{}
	for _, id := range excluded {
		if drop[id] || !p.OnBallot[id] || priv.TextOf[id] == "" {
			return nil, fmt.Errorf("creative: unknown or duplicate exclusion")
		}
		drop[id] = true
	}
	for id := range drop {
		delete(p.OnBallot, id)
	}
	// Zero valid works is a legitimate skipped round: run the short ballot clock
	// without inventing an award or points, then reveal an empty result.
	p.Subphase = "voting"
	return modeutil.Marshal(p), nil
}

func CloseVoting(public json.RawMessage) (json.RawMessage, error) {
	p, err := decodePublic(public)
	if err != nil {
		return nil, err
	}
	if !p.Config.SeparateWindows || p.Subphase != "voting" {
		return nil, fmt.Errorf("creative: voting is not open")
	}
	p.Subphase = "closed"
	return modeutil.Marshal(p), nil
}

// VoteSubject isolates each authenticated session's vote from its shared team
// work. This opaque storage key is never a public nickname or human identifier.
func VoteSubject(sessionID string) modes.Subject {
	digest := sha256.Sum256([]byte("creative.vote\x00" + sessionID))
	return modes.Subject{ID: "cv_" + hex.EncodeToString(digest[:]), Kind: modes.SubjectPlayer}
}

// ApplySessionVote is called only after an active membership is re-read inside
// the room transaction. The client cannot choose its vote pool or author seat.
func ApplySessionVote(in modes.ApplyInput, sessionID string, spectator bool) (modes.Decision, error) {
	p, err := decodePublic(in.Public)
	if err != nil {
		return modes.Decision{}, err
	}
	if !p.Config.SeparateWindows || p.Subphase != "voting" {
		return modes.Rejected(CodeVoteClosed, "creative: voting is not open"), nil
	}
	if sessionID == "" || in.Subject != VoteSubject(sessionID) || in.Action.Kind != ActionKindVote {
		return modes.Rejected(CodeNotOnRoster, "creative: invalid voter"), nil
	}
	if err := in.Action.Validate(); err != nil {
		return modes.Rejected(CodeInvalidPayload, "creative: invalid vote"), nil
	}
	var payload votePayload
	if json.Unmarshal(in.Action.Payload, &payload) != nil || payload.OptionID == "" || (payload.Kind != "" && payload.Kind != ActionKindVote) {
		return modes.Rejected(CodeInvalidPayload, "creative: invalid vote"), nil
	}
	st, err := decodeState(in.State)
	if err != nil {
		return modes.Decision{}, err
	}
	if st.Voted || p.VotedSessions[sessionID] {
		return modes.Rejected(CodeAlreadyVoted, "creative: this session already voted"), nil
	}
	authorSeat := p.VoterSubjects[sessionID]
	kind := VoteTeam
	if authorSeat == "" {
		if !spectator || !p.Config.AllowAudienceVote {
			return modes.Rejected(CodeNotOnRoster, "creative: this viewer cannot vote"), nil
		}
		kind = VoteAudience
	}
	priv, err := decodePrivate(in.Private)
	if err != nil {
		return modes.Decision{}, err
	}
	if !p.OnBallot[payload.OptionID] || priv.TextOf[payload.OptionID] == "" {
		return modes.Rejected(CodeWorkRejected, "creative: this work is not on the ballot"), nil
	}
	if priv.AuthorOf[payload.OptionID] == authorSeat {
		return modes.Rejected(CodeOwnWork, "creative: you cannot vote for your team's work"), nil
	}
	st.VoterID = sessionID
	st.Voted = true
	st.VoteKind = kind
	st.VoteWorkID = payload.OptionID
	return modes.Accepted(modeutil.Marshal(struct {
		WorkID string   `json:"workId"`
		Kind   VoteKind `json:"voteKind"`
	}{payload.OptionID, kind}), modeutil.Marshal(st)), nil
}

type voterView struct {
	Eligible   bool     `json:"eligible"`
	CanVote    bool     `json:"canVote"`
	Voted      bool     `json:"voted"`
	VoteKind   VoteKind `json:"voteKind,omitempty"`
	OwnWorkID  string   `json:"ownWorkId,omitempty"`
	VoteWorkID string   `json:"voteWorkId,omitempty"`
}

func projectSessionVote(out *projection, p publicState, in modes.ProjectInput) error {
	v := in.Viewer
	seat := p.VoterSubjects[v.SessionID]
	eligible := v.SessionID != "" && ((seat != "" && (v.Role == "player" || v.Role == "team-captain")) || (v.Role == "spectator" && p.Config.AllowAudienceVote))
	// Room owners playing as players also retain their frozen personal vote.
	if seat != "" && v.Subject.ID != "" {
		eligible = true
	}
	if eligible {
		st, err := decodeState(in.States[VoteSubject(v.SessionID).ID])
		if err != nil {
			return err
		}
		kind := VoteAudience
		if seat != "" {
			kind = VoteTeam
		}
		own := p.WorkIDs[seat]
		available := false
		for _, w := range out.Ballot {
			if w.WorkID != own {
				available = true
			}
		}
		out.Vote = &voterView{Eligible: true, CanVote: in.Phase == modes.PhaseCollecting && p.Subphase == "voting" && !st.Voted && available, Voted: st.Voted, VoteKind: kind, OwnWorkID: own, VoteWorkID: st.VoteWorkID}
	}
	if in.Phase == modes.PhaseLocked && p.Subphase == "review" && v.CanManageRound {
		out.Review = []ballotEntry{}
		for _, entry := range p.Subjects {
			st, err := decodeState(in.States[entry.SubjectID])
			if err != nil {
				return err
			}
			if st.Submitted && st.WorkText != "" {
				out.Review = append(out.Review, ballotEntry{WorkID: st.WorkID, Text: st.WorkText, Prompt: p.Prompts[entry.SubjectID]})
			}
		}
		sort.Slice(out.Review, func(i, j int) bool { return out.Review[i].WorkID < out.Review[j].WorkID })
	}
	// Keep the compatible self fields personal; never return a teammate's vote.
	if out.Self != nil && out.Vote != nil {
		out.Self.Voted = out.Vote.Voted
		out.Self.VoteWorkID = out.Vote.VoteWorkID
		out.Self.VoteKind = out.Vote.VoteKind
	}
	return nil
}
