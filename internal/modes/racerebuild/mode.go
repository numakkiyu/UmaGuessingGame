// Package racerebuild implements G04 历史赛况复原 .
//
// A round publishes 4–7 recorded participants of one raceId. The player orders
// them; scoring compares every comparable character pair and awards
// floor(100×correctPairs/totalPairs). A real tie must land in the same tie group
// to count, and participants with an unknown rank are excluded rather than
// pushed to last place.
package racerebuild

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"time"

	"github.com/bhcn/umaguessinggame/internal/modes"
	"github.com/bhcn/umaguessinggame/internal/modes/modeutil"
)

// ModeID is the stable mode identifier.
const ModeID = "race-rebuild"

// RulesVersion is the rule revision this implementation satisfies.
const RulesVersion = "2.0.0-draft.1"

// Participant bounds .
const (
	MinParticipants = 4
	MaxParticipants = 7
)

// Timing .
const QuestionSeconds = 180

// Action kinds.
//
// The value is the contract discriminator (ModeAction → RaceOrder), not an
// internal name .
const (
	// ActionOrder submits a full ordering of the participants.
	ActionOrder = "race.order"
)

// ContractKinds returns the ModeAction discriminators this mode accepts.
func (*Mode) ContractKinds() []string { return []string{ActionOrder} }

// Rejection codes.
const (
	CodeInvalidPayload = "invalid_payload"
	CodeUnknownAction  = "unknown_action"
	CodeIncomplete     = "incomplete_order"
	CodeUnknownEntry   = "unknown_participant"
	CodeDuplicateEntry = "duplicate_participant"
)

// Config is the round configuration.
type Config struct {
	// QuestionSeconds is the per-question window.
	QuestionSeconds int `json:"questionSeconds"`
	// Cooperative selects the cooperative variant that supplies extra clues.
	// The mode never hides knowledge needed for the answer .
	Cooperative bool `json:"cooperative"`
	// OpenBook reports whether the round lets players consult the material.
	OpenBook bool `json:"openBook"`
}

// decodeConfig normalises the configuration.
func decodeConfig(raw json.RawMessage) (Config, error) {
	cfg := Config{QuestionSeconds: QuestionSeconds}
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &cfg); err != nil {
			return Config{}, fmt.Errorf("racerebuild: invalid config: %w", err)
		}
	}
	if cfg.QuestionSeconds == 0 {
		cfg.QuestionSeconds = QuestionSeconds
	}
	if cfg.QuestionSeconds < 0 {
		return Config{}, fmt.Errorf("racerebuild: negative questionSeconds %d", cfg.QuestionSeconds)
	}
	return cfg, nil
}

// Entry is one recorded participant as published: identity only.
//
// The published section deliberately carries no finishing rank, because the rank
// is the answer . The rank comes from the private
// race-rebuild.answers section and is never part of a public payload.
type Entry struct {
	CharacterID string `json:"characterId"`
	Name        string `json:"characterName"`
}

// Race is one published race with its unordered participant set.
type Race struct {
	RaceID   string `json:"raceId"`
	Name     string `json:"name"`
	Date     string `json:"date"`
	Course   string `json:"course,omitempty"`
	Surface  string `json:"surface,omitempty"`
	Grade    string `json:"grade,omitempty"`
	Distance string `json:"distance,omitempty"`
	Region   string `json:"region,omitempty"`
	// Starters is an unordered set sorted by character id. Its array position
	// carries no result hint, so a client cannot read the order off it.
	Starters     []Entry `json:"starters"`
	StarterCount int     `json:"starterCount,omitempty"`
}

// parseRace converts one decoded public record into a Race.
func parseRace(fm modeutil.FieldMap) (Race, bool) {
	id := fm.Str("raceId", "race_id", "id")
	if id == "" {
		return Race{}, false
	}
	race := Race{
		RaceID:   id,
		Name:     fm.Str("name", "raceName", "race_name"),
		Date:     fm.Str("date", "raceDate", "race_date"),
		Course:   fm.Str("course"),
		Surface:  fm.Str("surface"),
		Grade:    fm.Str("grade"),
		Distance: fm.Str("distance"),
		Region:   fm.Str("region"),
	}
	if n, ok := fm.Int("starterCount"); ok {
		race.StarterCount = n
	}
	records, ok := fm.List("starters", "entries", "participants", "runners")
	if !ok {
		return Race{}, false
	}
	for _, rec := range records {
		entryID := rec.Str("characterId", "character_id", "id")
		if entryID == "" {
			continue
		}
		race.Starters = append(race.Starters, Entry{
			CharacterID: entryID,
			Name:        rec.Str("characterName", "character_name", "name", "nameZh", "name_zh"),
		})
	}
	// Starters are published sorted by id; enforce it so the question set is
	// stable regardless of the pack's array order.
	sort.Slice(race.Starters, func(i, j int) bool {
		return race.Starters[i].CharacterID < race.Starters[j].CharacterID
	})
	return race, true
}

// loadRaces reads the race-rebuild section.
func loadRaces(src modes.ContentSource) ([]Race, error) {
	raw, err := modeutil.Section(src, "race-rebuild.races", "raceRebuild.races", "races")
	if err != nil {
		return nil, err
	}
	records, err := modeutil.Records(raw, "races", "items")
	if err != nil {
		return nil, err
	}
	out := make([]Race, 0, len(records))
	for _, rec := range records {
		if race, ok := parseRace(rec); ok {
			out = append(out, race)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].RaceID < out[j].RaceID })
	return out, nil
}

// PrivateAnswersSection is the private section name carrying the settled ranks.
const PrivateAnswersSection = "race-rebuild.answers"

// privateRaceAnswer is one race's settled answer.
type privateRaceAnswer struct {
	RaceID string `json:"raceId"`
	// Order lists character ids in ascending finish rank.
	//
	// When Ranks is absent, position in Order is the rank, which yields the
	// contiguous 1..n sequence the current pack publishes. Ranks exists so a pack
	// can record the documented gaps (1,2,3,5,7) explicitly instead of renumbering
	// to a full field .
	Order []string `json:"order"`
	// Ranks optionally maps character id → recorded finish rank.
	Ranks map[string]int `json:"ranks,omitempty"`
	// Ties records rank groups that genuinely share a rank. The key is the rank
	// number and the value the tied character ids.
	Ties map[string][]string `json:"ties,omitempty"`
	// Dropped lists starters excluded for a missing rank, with the reason, so the
	// reveal can show that no renumbering happened .
	Dropped []string `json:"dropped,omitempty"`
}

// privateAnswers is the private section payload.
type privateAnswers struct {
	Mode  string              `json:"mode"`
	Races []privateRaceAnswer `json:"races"`
}

// loadAnswers reads the private answer section.
func loadAnswers(src modes.ContentSource) (map[string]privateRaceAnswer, error) {
	raw, err := modeutil.PrivateSection(src, PrivateAnswersSection)
	if err != nil {
		return nil, err
	}
	var payload privateAnswers
	if err := json.Unmarshal(raw, &payload); err != nil {
		return nil, fmt.Errorf("racerebuild: decode private answers: %w", err)
	}
	out := make(map[string]privateRaceAnswer, len(payload.Races))
	for _, answer := range payload.Races {
		out[answer.RaceID] = answer
	}
	return out, nil
}

// answerRanks derives each participant's rank from a settled answer.
//
// The rank of position i in Order is i+1, except that members of one tie group
// share the lowest rank of that group. Dropped starters are absent, which is what
// preserves the documented gaps (1,2,3,5,7) instead of renumbering to 1..n.
func answerRanks(answer privateRaceAnswer) (ranks map[string]int, tieGroups map[string]string, order []string) {
	ranks = make(map[string]int, len(answer.Order))
	tieGroups = make(map[string]string, len(answer.Order))

	// Build rank → tie-group-name so a tied participant gets a stable group key.
	tieName := make(map[int]string, len(answer.Ties))
	for rankKey, ids := range answer.Ties {
		rank, err := atoiStrict(rankKey)
		if err != nil || len(ids) == 0 {
			continue
		}
		tieName[rank] = "tie-" + rankKey
	}

	// A tie declared by id list is authoritative, because it states membership
	// directly instead of relying on the rank number matching the map key.
	byID := make(map[string]string, len(answer.Ties))
	for rankKey, ids := range answer.Ties {
		if len(ids) < 2 {
			continue
		}
		for _, id := range ids {
			byID[id] = "tie-" + rankKey
		}
	}

	for i, id := range answer.Order {
		rank := i + 1
		if recorded, ok := answer.Ranks[id]; ok && recorded > 0 {
			// An explicit recorded rank wins, so gaps in the real finishing order
			// survive instead of being renumbered .
			rank = recorded
		}
		ranks[id] = rank
		if group, ok := byID[id]; ok {
			tieGroups[id] = group
			continue
		}
		if group, ok := tieName[rank]; ok {
			tieGroups[id] = group
		}
	}
	order = append(order, answer.Order...)
	return ranks, tieGroups, order
}

// atoiStrict parses an all-digit string.
func atoiStrict(s string) (int, error) {
	if s == "" {
		return 0, fmt.Errorf("empty")
	}
	n := 0
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return 0, fmt.Errorf("not a number")
		}
		n = n*10 + int(s[i]-'0')
	}
	return n, nil
}

// usableStarters returns the participants that can be part of a question: those
// the private answer actually ranks. A starter with no recorded rank is excluded
// rather than pushed to last place , and the published set is already
// sorted by id so the question set is deterministic.
func usableStarters(race Race, answer privateRaceAnswer) []Entry {
	ranked := make(map[string]struct{}, len(answer.Order))
	for _, id := range answer.Order {
		ranked[id] = struct{}{}
	}
	out := make([]Entry, 0, len(race.Starters))
	for _, entry := range race.Starters {
		if _, ok := ranked[entry.CharacterID]; !ok {
			continue
		}
		out = append(out, entry)
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].CharacterID < out[j].CharacterID
	})
	return out
}

// publicPrepared is the non-secret prepared state: the race metadata and the
// participants to order. The real ranking is secret until reveal.
type publicPrepared struct {
	ModeID       string        `json:"modeId"`
	RulesVersion string        `json:"rulesVersion"`
	Config       Config        `json:"config"`
	RaceID       string        `json:"raceId"`
	RaceName     string        `json:"raceName"`
	RaceDate     string        `json:"raceDate"`
	Entries      []Entry       `json:"entries"`
	Subjects     []rosterEntry `json:"subjects"`
	// TotalPairs is the number of comparable character pairs, which is the
	// scoring denominator.
	TotalPairs int `json:"totalPairs"`
	// Clues are the optional cooperative hints. They never contain the answer
	// order itself.
	Clues         []string       `json:"clues,omitempty"`
	ClueRelations []clueRelation `json:"clueRelations,omitempty"`
}

// Structured forms of the same public cooperative hints, so clients can use
// confirmed regional names without parsing prose or looking up secret ranks.
type clueRelation struct {
	FirstID  string `json:"firstId"`
	SecondID string `json:"secondId"`
	Kind     string `json:"kind"`
}

// rosterEntry is one participant.
type rosterEntry struct {
	SubjectID   string            `json:"subjectId"`
	SubjectKind modes.SubjectKind `json:"subjectKind"`
}

// privatePrepared carries the real ranking.
type privatePrepared struct {
	RaceID string `json:"raceId"`
	// Truth maps characterId → real rank.
	Truth map[string]int `json:"truth"`
	// TruthTieGroup maps characterId → tie group.
	TruthTieGroup map[string]string `json:"truthTieGroup"`
	// Names maps characterId → display name for the reveal.
	//
	// The names are already public (they are on the starter list the same viewer
	// can read), so storing them privately is not a disclosure — it just lets the
	// reveal payload carry them without re-reading the pack during projection.
	Names map[string]string `json:"names,omitempty"`
	// Excluded lists the recorded participants dropped for an unknown rank, so
	// the reveal can say so explicitly.
	Excluded []string `json:"excluded"`
}

// state is one subject's participant state.
type state struct {
	Groups [][]string `json:"groups,omitempty"`
	// Order is the submitted character order, best first.
	Order []string `json:"order"`
	// Revisions counts accepted submissions.
	Revisions int `json:"revisions"`
	// CorrectPairs and TotalPairs describe the last accepted submission.
	CorrectPairs int `json:"correctPairs,omitempty"`
	TotalPairs   int `json:"totalPairs"`
	// Submitted reports whether any order was accepted.
	Submitted bool `json:"submitted"`
}

// Mode implements modes.Mode for G04.
type Mode struct{}

// New returns the race-rebuild mode.
func New() *Mode { return &Mode{} }

// ID implements modes.Mode.
func (*Mode) ID() string { return ModeID }

// RulesVersion implements modes.Mode.
func (*Mode) RulesVersion() string { return RulesVersion }

// Title implements modes.Mode.
func (*Mode) Title() string { return "历史赛况复原" }

// Summary implements modes.Mode.
func (*Mode) Summary() string {
	return "把一场收录赛事中的参赛者按真实名次排列，按正确角色对计分。"
}

// InformationStyles reports which information forms this mode implements.
func (*Mode) InformationStyles() []string {
	return []string{"open", "closed"}
}

// RoundWindow implements modes.Mode.
//
// 本模式没有题数配置，按单题窗口计。
func (*Mode) RoundWindow(raw json.RawMessage) time.Duration {
	cfg, err := decodeConfig(raw)
	if err != nil {
		return 0
	}
	return time.Duration(cfg.QuestionSeconds) * time.Second
}

// TurnWindow implements modes.Mode. This mode is not turn-based: it runs on the
// single RoundWindow deadline, so there is no per-turn clock to report.
func (*Mode) TurnWindow(json.RawMessage) time.Duration { return 0 }

// MinSubjects implements modes.Mode.
func (*Mode) MinSubjects() int { return 1 }

// MaxSubjects implements modes.Mode.
func (*Mode) MaxSubjects() int { return 6 }

// Requirement implements modes.Mode .
func (*Mode) Requirement() modes.ContentRequirement {
	return modes.ContentRequirement{
		Fields: []string{"raceId", "date", "entries", "finishRank"},
		// One usable race is enough to pose a round; MinParticipants is a room-start
		// floor, not a content-pool floor. Using it here made a single-race pack look
		// unusable to a mode that only ever needs one race at a time.
		MinEntities: 1,
		NeedsAssets: false,
	}
}

// ValidateConfig implements modes.Mode.
func (*Mode) ValidateConfig(cfg json.RawMessage) error {
	_, err := decodeConfig(cfg)
	return err
}

// Prepare implements modes.Mode.
func (*Mode) Prepare(ctx context.Context, in modes.PrepareInput) (modes.Prepared, error) {
	if err := in.Scope.Validate(); err != nil {
		return modes.Prepared{}, err
	}
	cfg, err := decodeConfig(in.Config)
	if err != nil {
		return modes.Prepared{}, err
	}
	if err := ctx.Err(); err != nil {
		return modes.Prepared{}, err
	}
	races, err := loadRaces(in.Content)
	if err != nil {
		return modes.Prepared{}, err
	}
	// The ranks live in the private tree, so a source without secrets cannot
	// grade this mode and must refuse the round rather than grade against
	// nothing .
	answers, err := loadAnswers(in.Content)
	if err != nil {
		return modes.Prepared{}, err
	}

	// Only races with a settled answer and enough rankable participants are
	// eligible.
	type candidate struct {
		race     Race
		answer   privateRaceAnswer
		starters []Entry
	}
	eligible := make([]candidate, 0, len(races))
	for _, race := range races {
		answer, ok := answers[race.RaceID]
		if !ok || len(answer.Order) == 0 {
			continue
		}
		starters := usableStarters(race, answer)
		if len(starters) < MinParticipants || len(starters) > MaxParticipants {
			continue
		}
		eligible = append(eligible, candidate{race: race, answer: answer, starters: starters})
	}
	if len(eligible) == 0 {
		return modes.Prepared{}, fmt.Errorf(
			"racerebuild: no race has a settled answer with %d–%d ranked participants",
			MinParticipants, MaxParticipants,
		)
	}

	chosen := eligible[modeutil.PickIndex(in.Scope.Seed, "racerebuild.race", len(eligible))]

	roster := make([]rosterEntry, 0, len(in.Subject))
	seen := make(map[string]struct{}, len(in.Subject))
	for _, subj := range in.Subject {
		if err := subj.Validate(); err != nil {
			return modes.Prepared{}, err
		}
		if _, dup := seen[subj.ID]; dup {
			return modes.Prepared{}, fmt.Errorf("racerebuild: duplicate subject %q", subj.ID)
		}
		seen[subj.ID] = struct{}{}
		roster = append(roster, rosterEntry{SubjectID: subj.ID, SubjectKind: subj.Kind})
	}
	if len(roster) < 1 || len(roster) > 6 {
		return modes.Prepared{}, fmt.Errorf("racerebuild: %d subjects outside [1,6]", len(roster))
	}

	// Derive the rank truth from the settled answer. The rank numbers keep their
	// gaps, because the order lists only ranked participants.
	truth, tieGroup, _ := answerRanks(chosen.answer)

	// Excluded starters are reported, not silently dropped: they appear in the
	// private answer's dropped list and are absent from the question.
	excluded := make([]string, 0, len(chosen.answer.Dropped))
	excluded = append(excluded, chosen.answer.Dropped...)

	totalPairs := pairCount(len(chosen.starters))

	pub := publicPrepared{
		ModeID:        ModeID,
		RulesVersion:  RulesVersion,
		Config:        cfg,
		RaceID:        chosen.race.RaceID,
		RaceName:      chosen.race.Name,
		RaceDate:      chosen.race.Date,
		Entries:       chosen.starters,
		Subjects:      roster,
		TotalPairs:    totalPairs,
		Clues:         buildClues(chosen.starters, truth, tieGroup, cfg),
		ClueRelations: buildClueRelations(chosen.starters, truth, tieGroup, cfg),
	}
	names := make(map[string]string, len(chosen.starters))
	for _, starter := range chosen.starters {
		if starter.CharacterID != "" && starter.Name != "" {
			names[starter.CharacterID] = starter.Name
		}
	}
	priv := privatePrepared{
		RaceID:        chosen.race.RaceID,
		Truth:         truth,
		TruthTieGroup: tieGroup,
		Names:         names,
		Excluded:      excluded,
	}
	return modes.Prepared{
		Public:        modeutil.Marshal(pub),
		Private:       modeutil.Marshal(priv),
		QuestionCount: 1,
	}, nil
}

// buildClues produces cooperative hints. Each clue is a relative statement that
// narrows the order without giving it away, and clues are only emitted for the
// cooperative variant .
func buildClues(entries []Entry, truth map[string]int, tieGroup map[string]string, cfg Config) []string {
	relations := buildClueRelations(entries, truth, tieGroup, cfg)
	if len(relations) == 0 {
		return nil
	}
	names := make(map[string]string, len(entries))
	for _, entry := range entries {
		names[entry.CharacterID] = entry.Name
	}
	clues := make([]string, 0, len(relations))
	for _, relation := range relations {
		if relation.Kind == "tied" {
			clues = append(clues, fmt.Sprintf("%s 与 %s 并列", names[relation.FirstID], names[relation.SecondID]))
		} else {
			clues = append(clues, fmt.Sprintf("%s 领先 %s", names[relation.FirstID], names[relation.SecondID]))
		}
	}
	return clues
}

func buildClueRelations(entries []Entry, truth map[string]int, tieGroup map[string]string, cfg Config) []clueRelation {
	if !cfg.Cooperative {
		return nil
	}
	sorted := make([]Entry, len(entries))
	copy(sorted, entries)
	sort.Slice(sorted, func(i, j int) bool {
		ri, rj := truth[sorted[i].CharacterID], truth[sorted[j].CharacterID]
		if ri != rj {
			return ri < rj
		}
		return sorted[i].CharacterID < sorted[j].CharacterID
	})
	clues := make([]clueRelation, 0, len(sorted))
	for i := 0; i+1 < len(sorted); i++ {
		a, b := sorted[i], sorted[i+1]
		ga, gb := tieGroup[a.CharacterID], tieGroup[b.CharacterID]
		if trueRelation(truth[a.CharacterID], truth[b.CharacterID], ga, gb) == relationTied {
			// A tie is described as a tie, not as an ordering.
			clues = append(clues, clueRelation{FirstID: a.CharacterID, SecondID: b.CharacterID, Kind: "tied"})
			continue
		}
		clues = append(clues, clueRelation{FirstID: a.CharacterID, SecondID: b.CharacterID, Kind: "before"})
	}
	return clues
}

// pairCount returns n×(n-1)/2, the number of unordered character pairs.
func pairCount(n int) int {
	if n < 2 {
		return 0
	}
	return n * (n - 1) / 2
}

// orderPayload is the body of ActionOrder, matching the contract's RaceOrder
// schema: {kind, groups[][]}.
//
// The outer array is the finishing order (best first) and each inner array is one
// rank group. A single-element group is a clear rank; a multi-element group states
// that those participants genuinely tied. This is exactly the documented rule that
// a real tie must land in the same group to count .
type orderPayload struct {
	Kind   string     `json:"kind"`
	Groups [][]string `json:"groups"`
}

// relation is one pair's relative order.
type relation int

const (
	relationAhead relation = iota
	relationTied
	relationBehind
)

// trueRelation returns the relation the recorded result asserts between a and b.
func trueRelation(rankA, rankB int, tieA, tieB string) relation {
	if tieA != "" && tieA == tieB {
		// A genuine tie: the two must be placed in the same group .
		return relationTied
	}
	if rankA == rankB {
		// Equal ranks without a shared tie group is still a genuine tie in the
		// recorded data, so treat it the same way.
		return relationTied
	}
	if rankA < rankB {
		return relationAhead
	}
	return relationBehind
}

// expandGroups flattens the contract's rank groups into an ordered id list and a
// per-id slot index.
//
// Every member of one group shares that group's slot, which is how a genuine tie
// is expressed. The returned order preserves the group order so pair iteration is
// deterministic.
func expandGroups(groups [][]string) (order []string, slot map[string]int) {
	total := 0
	for _, group := range groups {
		total += len(group)
	}
	order = make([]string, 0, total)
	slot = make(map[string]int, total)
	for index, group := range groups {
		for _, id := range group {
			if id == "" {
				continue
			}
			order = append(order, id)
			slot[id] = index
		}
	}
	return order, slot
}

// Apply implements modes.Mode.
func (*Mode) Apply(in modes.ApplyInput) (modes.Decision, error) {
	if in.Action.Kind != ActionOrder {
		return modes.Rejected(CodeUnknownAction,
			fmt.Sprintf("racerebuild: unsupported action %q", in.Action.Kind)), nil
	}
	if err := in.Action.Validate(); err != nil {
		return modes.Rejected(CodeInvalidPayload, err.Error()), nil
	}
	var payload orderPayload
	if err := json.Unmarshal(in.Action.Payload, &payload); err != nil {
		return modes.Rejected(CodeInvalidPayload,
			"racerebuild: order payload must carry a groups array"), nil
	}
	if payload.Kind != "" && payload.Kind != ActionOrder {
		return modes.Rejected(CodeInvalidPayload, fmt.Sprintf(
			"racerebuild: payload kind %q does not match action %q", payload.Kind, ActionOrder,
		)), nil
	}

	var pub publicPrepared
	if len(in.Public) > 0 {
		if err := json.Unmarshal(in.Public, &pub); err != nil {
			return modes.Decision{}, fmt.Errorf("racerebuild: decode public state: %w", err)
		}
	}
	var priv privatePrepared
	if len(in.Private) > 0 {
		if err := json.Unmarshal(in.Private, &priv); err != nil {
			return modes.Decision{}, fmt.Errorf("racerebuild: decode private state: %w", err)
		}
	}
	var st state
	if len(in.State) > 0 {
		if err := json.Unmarshal(in.State, &st); err != nil {
			return modes.Decision{}, fmt.Errorf("racerebuild: decode subject state: %w", err)
		}
	}

	known := make(map[string]struct{}, len(pub.Entries))
	for _, entry := range pub.Entries {
		known[entry.CharacterID] = struct{}{}
	}
	for _, group := range payload.Groups {
		if len(group) == 0 {
			return modes.Rejected(CodeInvalidPayload, "racerebuild: an order group must not be empty"), nil
		}
	}

	order, position := expandGroups(payload.Groups)
	if len(position) != len(order) {
		// The same character appearing twice in the submission.
		return modes.Rejected(CodeDuplicateEntry,
			"racerebuild: the order lists the same participant twice"), nil
	}
	if len(order) != len(pub.Entries) {
		return modes.Rejected(CodeIncomplete, fmt.Sprintf(
			"racerebuild: order covers %d participants, need %d", len(order), len(pub.Entries),
		)), nil
	}
	for _, id := range order {
		if _, ok := known[id]; !ok {
			return modes.Rejected(CodeUnknownEntry, fmt.Sprintf(
				"racerebuild: participant %q is not in this question", id,
			)), nil
		}
	}

	correct, total := compareOrderWithSlots(order, position, priv.Truth, priv.TruthTieGroup)

	st.Order = order
	st.Groups = payload.Groups
	st.Revisions++
	st.CorrectPairs = correct
	st.TotalPairs = total
	st.Submitted = true

	// Revisable closed-book answers must not become a scoring oracle. Only
	// acknowledge the revision; correctness remains private until reveal.
	feedback := modeutil.Marshal(struct {
		Revisions int `json:"revisions"`
	}{st.Revisions})

	dec := modes.Accepted(feedback, modeutil.Marshal(st))
	// A revision replaces the previous submission, so the round stays open
	// until the deadline .
	return dec, nil
}

// compareOrderWithSlots grades an order using explicit slot positions, which is
// what lets the "a|b" tie syntax express a genuine tie.
func compareOrderWithSlots(order []string, position map[string]int, truth map[string]int, truthTie map[string]string) (correct, total int) {
	n := len(order)
	if n < 2 {
		return 0, 0
	}
	for i := 0; i < n; i++ {
		for j := i + 1; j < n; j++ {
			a, b := order[i], order[j]
			rankA, okA := truth[a]
			rankB, okB := truth[b]
			if !okA || !okB {
				continue
			}
			total++
			var submitted relation
			switch {
			case position[a] == position[b]:
				submitted = relationTied
			case position[a] < position[b]:
				submitted = relationAhead
			default:
				submitted = relationBehind
			}
			if submitted == trueRelation(rankA, rankB, truthTie[a], truthTie[b]) {
				correct++
			}
		}
	}
	return correct, total
}

// scoreUnits converts correct/total pairs into integer scoreUnits using the
// documented floor(100×correct/total).
func scoreUnits(correct, total int) int {
	if total <= 0 || correct <= 0 {
		return 0
	}
	return modeutil.Units(correct * 100 / total)
}

// projection is the viewer-specific state.
type projection struct {
	ModeID        string         `json:"modeId"`
	RulesVersion  string         `json:"rulesVersion"`
	Phase         modes.Phase    `json:"phase"`
	Config        Config         `json:"config"`
	RaceID        string         `json:"raceId"`
	RaceName      string         `json:"raceName"`
	RaceDate      string         `json:"raceDate"`
	Entries       []Entry        `json:"entries"`
	TotalPairs    int            `json:"totalPairs"`
	Clues         []string       `json:"clues,omitempty"`
	ClueRelations []clueRelation `json:"clueRelations,omitempty"`
	Subjects      []progress     `json:"subjects"`
	Self          *selfRebuild   `json:"self,omitempty"`
	Answer        *answerReveal  `json:"answer,omitempty"`
}

// progress is the coarse per-subject progress.
type progress struct {
	SubjectID    string `json:"subjectId"`
	Submitted    bool   `json:"submitted"`
	Revisions    int    `json:"revisions"`
	CorrectPairs int    `json:"correctPairs,omitempty"`
	TotalPairs   int    `json:"totalPairs"`
}

// selfRebuild is the caller's own state.
type selfRebuild struct {
	Groups       [][]string `json:"groups,omitempty"`
	SubjectID    string     `json:"subjectId"`
	Order        []string   `json:"order"`
	Submitted    bool       `json:"submitted"`
	Revisions    int        `json:"revisions"`
	CorrectPairs int        `json:"correctPairs,omitempty"`
	TotalPairs   int        `json:"totalPairs"`
}

// answerReveal is the real ranking, released only on reveal.
type answerReveal struct {
	RaceID string        `json:"raceId"`
	Ranked []rankedEntry `json:"ranked"`
	// Gaps lists the recorded rank numbers actually present, so the reveal shows
	// the real gaps (1,2,3,5,7) instead of a renumbered 1..n .
	Gaps     []int    `json:"rankGaps"`
	Excluded []string `json:"excluded,omitempty"`
}

// rankedEntry is one participant with its real rank.
type rankedEntry struct {
	CharacterID string `json:"characterId"`
	Name        string `json:"name"`
	FinishRank  int    `json:"finishRank"`
	TieGroup    string `json:"tieGroup,omitempty"`
}

// Project implements modes.Mode.
func (*Mode) Project(in modes.ProjectInput) (json.RawMessage, error) {
	var pub publicPrepared
	if len(in.Public) > 0 {
		if err := json.Unmarshal(in.Public, &pub); err != nil {
			return nil, fmt.Errorf("racerebuild: decode public state: %w", err)
		}
	}
	var priv privatePrepared
	if len(in.Private) > 0 {
		if err := json.Unmarshal(in.Private, &priv); err != nil {
			return nil, fmt.Errorf("racerebuild: decode private state: %w", err)
		}
	}

	out := projection{
		ModeID:        ModeID,
		RulesVersion:  RulesVersion,
		Phase:         in.Phase,
		Config:        pub.Config,
		RaceID:        pub.RaceID,
		RaceName:      pub.RaceName,
		RaceDate:      pub.RaceDate,
		Entries:       pub.Entries,
		TotalPairs:    pub.TotalPairs,
		Clues:         pub.Clues,
		ClueRelations: pub.ClueRelations,
		Subjects:      make([]progress, 0, len(pub.Subjects)),
	}
	for _, entry := range pub.Subjects {
		var st state
		if raw, ok := in.States[entry.SubjectID]; ok && len(raw) > 0 {
			if err := json.Unmarshal(raw, &st); err != nil {
				return nil, fmt.Errorf("racerebuild: decode state of %q: %w", entry.SubjectID, err)
			}
		}
		out.Subjects = append(out.Subjects, progress{
			SubjectID:    entry.SubjectID,
			Submitted:    st.Submitted,
			Revisions:    st.Revisions,
			CorrectPairs: 0,
			TotalPairs:   st.TotalPairs,
		})
		if entry.SubjectID == in.Viewer.Subject.ID {
			out.Self = &selfRebuild{
				SubjectID:    entry.SubjectID,
				Groups:       st.Groups,
				Order:        st.Order,
				Submitted:    st.Submitted,
				Revisions:    st.Revisions,
				CorrectPairs: 0,
				TotalPairs:   st.TotalPairs,
			}
		}
		if in.Viewer.SeesSecrets() || (in.RevealNow && in.Phase.Terminal()) {
			out.Subjects[len(out.Subjects)-1].CorrectPairs = st.CorrectPairs
			if out.Self != nil && entry.SubjectID == in.Viewer.Subject.ID {
				out.Self.CorrectPairs = st.CorrectPairs
			}
		}
	}

	if in.Viewer.SeesSecrets() || (in.RevealNow && in.Phase.Terminal()) {
		reveal := &answerReveal{RaceID: priv.RaceID, Excluded: priv.Excluded}
		ids := make([]string, 0, len(priv.Truth))
		for id := range priv.Truth {
			ids = append(ids, id)
		}
		sort.Slice(ids, func(i, j int) bool {
			if priv.Truth[ids[i]] != priv.Truth[ids[j]] {
				return priv.Truth[ids[i]] < priv.Truth[ids[j]]
			}
			return ids[i] < ids[j]
		})
		// The reveal names each starter. Without it the table shows bare ids and a
		// player cannot tell who finished where — the rank would be readable but
		// useless (found in review: the table rendered "1072, 1071").
		//
		// Names come from the public starter list, which the same viewer can already
		// read, so this adds no disclosure: the ordering was the secret, not the
		// names. A lookup failure leaves Name empty rather than failing the reveal;
		// the client then falls back to its own id-to-name map.
		nameOf := priv.Names
		seenRank := make(map[int]struct{}, len(ids))
		for _, id := range ids {
			rank := priv.Truth[id]
			reveal.Ranked = append(reveal.Ranked, rankedEntry{
				CharacterID: id,
				Name:        nameOf[id],
				FinishRank:  rank,
				TieGroup:    priv.TruthTieGroup[id],
			})
			if _, dup := seenRank[rank]; !dup {
				seenRank[rank] = struct{}{}
				reveal.Gaps = append(reveal.Gaps, rank)
			}
		}
		sort.Ints(reveal.Gaps)
		out.Answer = reveal
	}
	return modeutil.Marshal(out), nil
}

// Settle implements modes.Mode.
func (*Mode) Settle(in modes.SettleInput) ([]modes.Score, error) {
	var pub publicPrepared
	if len(in.Public) > 0 {
		if err := json.Unmarshal(in.Public, &pub); err != nil {
			return nil, fmt.Errorf("racerebuild: decode public state: %w", err)
		}
	}
	scores := make([]modes.Score, 0, len(pub.Subjects))
	for _, entry := range pub.Subjects {
		var st state
		if raw, ok := in.States[entry.SubjectID]; ok && len(raw) > 0 {
			if err := json.Unmarshal(raw, &st); err != nil {
				return nil, fmt.Errorf("racerebuild: decode state of %q: %w", entry.SubjectID, err)
			}
		}
		units := 0
		if st.Submitted {
			units = scoreUnits(st.CorrectPairs, st.TotalPairs)
		}
		explanation := modeutil.Marshal(struct {
			Submitted    bool `json:"submitted"`
			CorrectPairs int  `json:"correctPairs,omitempty"`
			TotalPairs   int  `json:"totalPairs"`
			Revisions    int  `json:"revisions"`
		}{
			Submitted:    st.Submitted,
			CorrectPairs: st.CorrectPairs,
			TotalPairs:   st.TotalPairs,
			Revisions:    st.Revisions,
		})
		score := modes.Score{
			Subject:       modes.Subject{ID: entry.SubjectID, Kind: entry.SubjectKind},
			ScoreUnits:    units,
			MaxScoreUnits: modeutil.Units(100),
			Explanation:   explanation,
		}
		if err := score.Validate(); err != nil {
			return nil, err
		}
		scores = append(scores, score)
	}
	return modes.RankScores(scores), nil
}
