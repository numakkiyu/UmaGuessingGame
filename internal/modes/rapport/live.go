package rapport

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/bhcn/umaguessinggame/internal/modes"
	"github.com/bhcn/umaguessinggame/internal/modes/modeutil"
)

type liveGroup struct {
	ID         string   `json:"groupId"`
	Team       bool     `json:"team"`
	Members    []string `json:"members"`
	Choosers   []string `json:"choosers"`
	TotalSlots int      `json:"totalSlots"`
}
type liveState struct {
	Groups  []liveGroup `json:"groups"`
	Current int         `json:"currentQuestion"`
	// Only participation markers are shared. Picks stay in member state rows.
	Answered map[string]bool `json:"answered"`
}
type groupProgress struct {
	GroupID    string `json:"groupId"`
	ChooserID  string `json:"chooserId"`
	Answered   int    `json:"answered"`
	Members    int    `json:"members"`
	TotalSlots int    `json:"totalSlots"`
}

func (*Mode) UsesIndividualSeats() bool { return true }
func (*Mode) IndividualSeatsForConfig(raw json.RawMessage) bool {
	cfg, err := decodeConfig(raw)
	return err == nil && cfg.SimultaneousTurns
}
func (*Mode) ValidateRoster(subjects []modes.Subject, participants []modes.Participant) error {
	groups, err := groupsFor(participants)
	if err != nil {
		return err
	}
	if len(subjects) != len(participants) {
		return fmt.Errorf("rapport: every player needs an individual seat")
	}
	seen := map[string]bool{}
	for _, subject := range subjects {
		if subject.Kind != modes.SubjectPlayer || seen[subject.ID] {
			return fmt.Errorf("rapport: invalid individual seat")
		}
		seen[subject.ID] = true
	}
	for _, g := range groups {
		for _, id := range g.Members {
			if !seen[id] {
				return fmt.Errorf("rapport: member missing from seats")
			}
		}
	}
	return nil
}
func (*Mode) ScoringSubjects(participants []modes.Participant) ([]modes.Subject, error) {
	groups, err := groupsFor(participants)
	if err != nil {
		return nil, err
	}
	var result []modes.Subject
	for _, g := range groups {
		if g.Team {
			result = append(result, modes.Subject{ID: g.ID, Kind: modes.SubjectTeam})
		} else {
			for _, id := range g.Members {
				result = append(result, modes.Subject{ID: id, Kind: modes.SubjectPlayer})
			}
		}
	}
	return result, nil
}
func groupsFor(participants []modes.Participant) ([]liveGroup, error) {
	if len(participants) < 2 || len(participants) > 64 {
		return nil, fmt.Errorf("rapport: needs 2–64 players")
	}
	groups := []liveGroup{}
	byID := map[string]int{}
	seen := map[string]bool{}
	hasTeam, hasSolo := false, false
	for _, p := range participants {
		if p.MembershipID == "" || seen[p.MembershipID] || p.Subject.Kind != modes.SubjectPlayer || p.Subject.ID != p.MembershipID {
			return nil, fmt.Errorf("rapport: invalid or duplicate member")
		}
		seen[p.MembershipID] = true
		id := p.TeamID
		if id == "" {
			id = "party"
			hasSolo = true
		} else {
			hasTeam = true
		}
		index, ok := byID[id]
		if !ok {
			index = len(groups)
			byID[id] = index
			groups = append(groups, liveGroup{ID: id, Team: p.TeamID != ""})
		}
		groups[index].Members = append(groups[index].Members, p.MembershipID)
	}
	if hasTeam && hasSolo {
		return nil, fmt.Errorf("rapport: assign every player to a team before starting")
	}
	if len(groups) > 8 {
		return nil, fmt.Errorf("rapport: at most 8 teams")
	}
	min, max := 8, 0
	for _, g := range groups {
		n := len(g.Members)
		if n < 2 || n > 8 {
			return nil, fmt.Errorf("rapport: each group needs 2–8 players")
		}
		if n < min {
			min = n
		}
		if n > max {
			max = n
		}
	}
	if max-min > 1 {
		return nil, fmt.Errorf("rapport: team sizes may differ by at most one player")
	}
	return groups, nil
}

func prepareLive(ctx context.Context, in modes.PrepareInput) (modes.Prepared, error) {
	// Room commands carry an envelope; rule fixtures may pass the inner object.
	// Read the same options that are persisted and used by the turn clock.
	var envelope struct {
		Config json.RawMessage `json:"config"`
	}
	if len(in.Config) > 0 {
		if err := json.Unmarshal(in.Config, &envelope); err != nil {
			return modes.Prepared{}, err
		}
		if len(envelope.Config) > 0 {
			in.Config = envelope.Config
		}
	}
	if err := New().ValidateRoster(in.Subject, in.Participants); err != nil {
		return modes.Prepared{}, err
	}
	groups, err := groupsFor(in.Participants)
	if err != nil {
		return modes.Prepared{}, err
	}
	cfg, err := decodeConfig(in.Config)
	if err != nil {
		return modes.Prepared{}, err
	}
	largest := groups[0]
	for _, g := range groups {
		if len(g.Members) > len(largest.Members) {
			largest = g
		}
	}
	var requested struct {
		Questions int `json:"questions"`
	}
	if len(in.Config) > 0 {
		if err := json.Unmarshal(in.Config, &requested); err != nil {
			return modes.Prepared{}, err
		}
	}
	if requested.Questions == 0 && cfg.Questions < len(largest.Members) {
		cfg.Questions = len(largest.Members)
	}
	if cfg.Questions < len(largest.Members) {
		return modes.Prepared{}, fmt.Errorf("rapport: not enough questions for every member to choose")
	}
	cfg.SimultaneousTurns = true
	base := in
	base.Participants = nil
	base.Config = modeutil.Marshal(cfg)
	base.Subject = nil
	for _, id := range largest.Members {
		base.Subject = append(base.Subject, modes.Subject{ID: id, Kind: modes.SubjectPlayer})
	}
	prepared, err := New().Prepare(ctx, base)
	if err != nil {
		return modes.Prepared{}, err
	}
	pub, err := decodePublic(prepared.Public)
	if err != nil {
		return modes.Prepared{}, err
	}
	pub.Subjects = nil
	for _, p := range in.Participants {
		pub.Subjects = append(pub.Subjects, rosterEntry{SubjectID: p.MembershipID, SubjectKind: modes.SubjectPlayer})
	}
	pub.TotalSlots = 0
	for i := range groups {
		group := &groups[i]
		offset := modeutil.PickIndex(in.Scope.Seed, "rapport.group:"+group.ID, len(group.Members))
		for q := range pub.Questions {
			group.Choosers = append(group.Choosers, group.Members[(offset+q)%len(group.Members)])
		}
		group.TotalSlots = len(pub.Questions) * (len(group.Members) - 1)
		pub.TotalSlots += group.TotalSlots
	}
	pub.Live = &liveState{Groups: groups, Current: 1, Answered: map[string]bool{}}
	prepared.Public = modeutil.Marshal(pub)
	prepared.Config = modeutil.Marshal(cfg)
	return prepared, nil
}
func ownGroup(pub publicState, id string) (liveGroup, bool) {
	for _, g := range pub.Live.Groups {
		if contains(g.Members, id) {
			return g, true
		}
	}
	return liveGroup{}, false
}
func groupQuestion(q bankQuestion, g liveGroup, index int) bankQuestion {
	q.ChooserID = g.Choosers[index]
	q.PredictorIDs = nil
	for _, id := range g.Members {
		if id != q.ChooserID {
			q.PredictorIDs = append(q.PredictorIDs, id)
		}
	}
	q.PredictionSlots = len(q.PredictorIDs)
	return q
}
func applyLive(pub publicState, in modes.ApplyInput, payload answerPayload) (modes.Decision, error) {
	g, ok := ownGroup(pub, in.Subject.ID)
	if !ok || in.Subject.Kind != modes.SubjectPlayer || in.MembershipID != in.Subject.ID {
		return modes.Rejected(CodeNotOnRoster, "rapport: no frozen personal seat"), nil
	}
	index := pub.Live.Current - 1
	if index < 0 || index >= len(pub.Questions) {
		return modes.Rejected(CodeRoundFinished, "rapport: all questions are closed"), nil
	}
	q := groupQuestion(pub.Questions[index], g, index)
	valid := false
	for _, o := range q.Options {
		if o.OptionID == payload.OptionID {
			valid = true
		}
	}
	if !valid {
		return modes.Rejected(CodeQuestionClosed, "rapport: option is not in the current question"), nil
	}
	state, err := decodeState(in.State)
	if err != nil {
		return modes.Decision{}, err
	}
	if state.answered(q.QuestionID) || pub.Live.Answered[in.Subject.ID] {
		return modes.Rejected(CodeDuplicateAnswer, "rapport: this question is already confirmed"), nil
	}
	own := answerRecord{QuestionID: q.QuestionID, OptionID: payload.OptionID, AsChooser: q.ChooserID == in.Subject.ID}
	state.Answers = append(state.Answers, own)
	return modes.Accepted(modeutil.Marshal(own), modeutil.Marshal(state)), nil
}
func (*Mode) CommitShared(in modes.ApplyInput, decision modes.Decision) (json.RawMessage, json.RawMessage, error) {
	pub, err := decodePublic(in.Public)
	if err != nil {
		return nil, nil, err
	}
	if pub.Live == nil {
		return nil, nil, nil
	}
	if !decision.Accepted {
		return nil, nil, fmt.Errorf("rapport: cannot commit a rejected answer")
	}
	if _, ok := ownGroup(pub, in.Subject.ID); !ok || in.MembershipID != in.Subject.ID || in.Subject.Kind != modes.SubjectPlayer || pub.Live.Answered[in.Subject.ID] {
		return nil, nil, fmt.Errorf("rapport: unknown member")
	}
	index := pub.Live.Current - 1
	state, err := decodeState(decision.State)
	if err != nil || index < 0 || index >= len(pub.Questions) {
		return nil, nil, fmt.Errorf("rapport: no valid current answer")
	}
	if _, ok := state.option(pub.Questions[index].QuestionID); !ok {
		return nil, nil, fmt.Errorf("rapport: accepted answer missing from state")
	}
	if pub.Live.Answered == nil {
		pub.Live.Answered = map[string]bool{}
	}
	pub.Live.Answered[in.Subject.ID] = true
	if len(pub.Live.Answered) == len(pub.Subjects) {
		pub.Live.Current++
		pub.Live.Answered = map[string]bool{}
	}
	return modeutil.Marshal(pub), nil, nil
}
func (*Mode) OpenTurn(raw json.RawMessage) (string, bool, error) {
	pub, err := decodePublic(raw)
	if err != nil {
		return "", false, err
	}
	if pub.Live == nil {
		return "", false, nil
	}
	return fmt.Sprint(pub.Live.Current), pub.Live.Current <= len(pub.Questions), nil
}
func (*Mode) ExpireTurn(raw json.RawMessage) (json.RawMessage, bool, error) {
	pub, err := decodePublic(raw)
	if err != nil {
		return nil, false, err
	}
	if pub.Live == nil {
		return nil, false, nil
	}
	if pub.Live.Current <= len(pub.Questions) {
		pub.Live.Current++
		pub.Live.Answered = map[string]bool{}
	}
	return modeutil.Marshal(pub), pub.Live.Current <= len(pub.Questions), nil
}

func projectLive(pub publicState, in modes.ProjectInput) (json.RawMessage, error) {
	states := make(map[string]subjectState, len(pub.Subjects))
	for _, entry := range pub.Subjects {
		st, err := decodeState(in.States[entry.SubjectID])
		if err != nil {
			return nil, err
		}
		states[entry.SubjectID] = st
	}
	group, competing := ownGroup(pub, in.Viewer.Subject.ID)
	if in.Viewer.MembershipID != in.Viewer.Subject.ID {
		competing = false
	}
	out := projection{ModeID: ModeID, RulesVersion: RulesVersion, Phase: in.Phase, Config: pub.Config, TotalSlots: pub.TotalSlots, Scoring: pub.Scoring, Questions: []questionView{}, CurrentQuestion: pub.Live.Current, QuestionCount: len(pub.Questions), Groups: []groupProgress{}}
	index := pub.Live.Current - 1
	if index >= 0 && index < len(pub.Questions) {
		for _, g := range pub.Live.Groups {
			n := 0
			for _, id := range g.Members {
				if pub.Live.Answered[id] {
					n++
				}
			}
			out.Groups = append(out.Groups, groupProgress{GroupID: g.ID, ChooserID: g.Choosers[index], Answered: n, Members: len(g.Members), TotalSlots: g.TotalSlots})
		}
	}
	if competing {
		out.TotalSlots = group.TotalSlots
		out.GroupID = group.ID
		answers := states[in.Viewer.Subject.ID].Answers
		if answers == nil {
			answers = []answerRecord{}
		}
		out.Self = &selfView{SubjectID: in.Viewer.Subject.ID, Answers: answers}
	}
	for k, q := range pub.Questions {
		if k > index {
			break
		}
		if !competing {
			// Public observers get prompts and coarse progress, never team picks.
			out.Questions = append(out.Questions, questionView{QuestionID: q.QuestionID, Prompt: q.Prompt, Options: q.Options, Locked: k < index})
			continue
		}
		q = groupQuestion(q, group, k)
		view := questionView{QuestionID: q.QuestionID, Prompt: q.Prompt, Options: q.Options, ChooserID: q.ChooserID, SelfIsChooser: q.ChooserID == in.Viewer.Subject.ID, Locked: k < index}
		if own, ok := states[in.Viewer.Subject.ID].option(q.QuestionID); ok {
			view.Answered = true
			view.SelfOptionID = own.OptionID
		}
		if view.Locked {
			reveal := buildReveal(q, states)
			view.Reveal = &reveal
			if !view.SelfIsChooser {
				out.RevealedSlots++
				if own, ok := states[in.Viewer.Subject.ID].option(q.QuestionID); ok && own.OptionID == reveal.ChosenOptionID {
					out.RevealedHits++
				}
			}
		} else if !view.Answered && in.Phase == modes.PhaseCollecting {
			out.CanAnswer = true
		}
		out.Questions = append(out.Questions, view)
	}
	if pub.Live.Current > len(pub.Questions) && (in.Phase == modes.PhaseRevealed || in.Phase == modes.PhaseSettled) {
		scores, err := settleLive(pub, modes.SettleInput{Public: in.Public, States: in.States})
		if err != nil {
			return nil, err
		}
		out.Final = scores
	}
	return modeutil.Marshal(out), nil
}
func settleLive(pub publicState, in modes.SettleInput) ([]modes.Score, error) {
	if pub.Live.Current <= len(pub.Questions) {
		return nil, fmt.Errorf("rapport: finish all questions before settlement")
	}
	if in.Phase == modes.PhaseCancelled {
		return nil, fmt.Errorf("rapport: cancelled round has no score")
	}
	states := make(map[string]subjectState, len(pub.Subjects))
	for _, p := range pub.Subjects {
		st, err := decodeState(in.States[p.SubjectID])
		if err != nil {
			return nil, err
		}
		states[p.SubjectID] = st
	}
	scores := []modes.Score{}
	for _, g := range pub.Live.Groups {
		if g.TotalSlots <= 0 {
			return nil, fmt.Errorf("rapport: invalid prediction denominator")
		}
		hits := 0
		for i, q := range pub.Questions {
			q = groupQuestion(q, g, i)
			chosen, ok := states[q.ChooserID].option(q.QuestionID)
			if !ok {
				continue
			}
			for _, id := range q.PredictorIDs {
				if own, ok := states[id].option(q.QuestionID); ok && own.OptionID == chosen.OptionID {
					hits++
				}
			}
		}
		units := (100 * hits / g.TotalSlots) * 100
		subjects := []modes.Subject{{ID: g.ID, Kind: modes.SubjectTeam}}
		if !g.Team {
			subjects = nil
			for _, id := range g.Members {
				subjects = append(subjects, modes.Subject{ID: id, Kind: modes.SubjectPlayer})
			}
		}
		for _, subject := range subjects {
			score := modes.Score{Subject: subject, ScoreUnits: units, MaxScoreUnits: 10000, Explanation: modeutil.Marshal(map[string]any{"hits": hits, "totalSlots": g.TotalSlots, "formula": "floor(100*hits/totalSlots)"})}
			if err := score.Validate(); err != nil {
				return nil, err
			}
			scores = append(scores, score)
		}
	}
	return modes.RankScores(scores), nil
}
