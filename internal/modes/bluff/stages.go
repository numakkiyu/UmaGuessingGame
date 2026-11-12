package bluff

import (
	"encoding/json"
	"fmt"
	"github.com/bhcn/umaguessinggame/internal/modes/modeutil"
	"golang.org/x/text/unicode/norm"
	"strings"
)

type scoreBreakdown struct {
	SubjectID       string `json:"subjectId"`
	TruthPoints     int    `json:"truthPoints"`
	DeceptionPoints int    `json:"deceptionPoints"`
	IdentifiedTruth bool   `json:"identifiedTruth"`
	Voted           bool   `json:"voted"`
	Submitted       bool   `json:"submitted"`
}

func Stage(public json.RawMessage) (string, error) {
	pub, err := decodePublic(public)
	if err != nil {
		return "", err
	}
	if pub.Subphase == "" {
		return "legacy", nil
	}
	return pub.Subphase, nil
}
func Windows(config json.RawMessage) (writeMS, voteMS int64, err error) {
	cfg, err := decodeConfig(config)
	return int64(cfg.WriteSeconds) * 1000, int64(cfg.VoteSeconds) * 1000, err
}
func WritingComplete(public json.RawMessage) (bool, error) {
	pub, err := decodePublic(public)
	return pub.Subphase == "writing" && pub.DecoyCount >= len(pub.Subjects), err
}
func VotingComplete(public, private json.RawMessage, states map[string]json.RawMessage) (bool, error) {
	pub, err := decodePublic(public)
	if err != nil {
		return false, err
	}
	priv, err := decodePrivate(private)
	if err != nil {
		return false, err
	}
	if pub.Subphase != "voting" {
		return false, nil
	}
	for _, entry := range pub.Subjects {
		st, err := decodeState(states[entry.SubjectID])
		if err != nil {
			return false, err
		}
		if st.DecoyCount > 0 && normalizeDecoy(st.DecoyText) == normalizeDecoy(priv.Truth) {
			continue
		}
		if !st.Voted {
			return false, nil
		}
	}
	return true, nil
}

// CloseWriting freezes all texts before anyone receives a ballot. Missing
// submissions use only reviewed alternatives from this fact, never invented text.
func CloseWriting(public, private json.RawMessage, seed string) (json.RawMessage, json.RawMessage, error) {
	pub, err := decodePublic(public)
	if err != nil {
		return nil, nil, err
	}
	priv, err := decodePrivate(private)
	if err != nil {
		return nil, nil, err
	}
	if pub.Subphase != "writing" {
		return nil, nil, fmt.Errorf("bluff: writing is already closed")
	}
	missing := len(pub.Subjects) - pub.DecoyCount
	for _, raw := range priv.FallbackAnswers {
		if missing <= 0 && len(priv.Decoys) > 0 {
			break
		}
		text := norm.NFC.String(strings.TrimSpace(raw))
		normalized := normalizeDecoy(text)
		if normalized == normalizeDecoy(priv.Truth) || countCodePoints(text) < 2 || countCodePoints(text) > 40 {
			continue
		}
		id := decoyIDFor(normalized)
		if decoyExists(priv, id) {
			continue
		}
		priv.Decoys = append(priv.Decoys, decoy{DecoyID: id, Normalized: normalized, Text: text, AuthorIDs: []string{}})
		missing--
	}
	pub.Options = buildOptions(priv, seed)
	pub.Subphase = "review"
	pub.OptionsGrouped = len(pub.Options) > MaxOptions
	return modeutil.Marshal(pub), modeutil.Marshal(priv), nil
}

// OpenVoting is called only by the authorised room transition transaction.
// Exclusions are all-or-nothing; the true answer can never be removed.
func OpenVoting(public, private json.RawMessage, seed string, excluded []string) (json.RawMessage, json.RawMessage, error) {
	pub, err := decodePublic(public)
	if err != nil {
		return nil, nil, err
	}
	priv, err := decodePrivate(private)
	if err != nil {
		return nil, nil, err
	}
	if pub.Subphase != "review" {
		return nil, nil, fmt.Errorf("bluff: review is not open")
	}
	known := map[string]bool{}
	for _, opt := range pub.Options {
		known[opt.OptionID] = true
	}
	drop := map[string]bool{}
	for _, id := range excluded {
		if !known[id] || drop[id] || id == priv.TruthOptionID {
			return nil, nil, fmt.Errorf("bluff: invalid or protected review option")
		}
		drop[id] = true
	}
	kept := []decoy{}
	for _, d := range priv.Decoys {
		if !drop[optionIDFor(d.DecoyID)] {
			kept = append(kept, d)
		} else {
			delete(priv.AuthorOf, d.DecoyID)
		}
	}
	priv.Decoys = kept
	pub.Options = buildOptions(priv, seed)
	if len(pub.Options) < 2 {
		return nil, nil, fmt.Errorf("bluff: reviewed ballot needs an alternative")
	}
	pub.Subphase = "voting"
	pub.OptionsGrouped = len(pub.Options) > MaxOptions
	return modeutil.Marshal(pub), modeutil.Marshal(priv), nil
}
