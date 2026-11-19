// Package sortgame implements G06 档案排序 .
//
// A round publishes 2–6 cards (default 6) and the player orders them by a chosen
// key: height, month/day birthday, or a verified event date. Equal values are
// allowed and tie; scoring compares every card pair's relation
// (less / equal / greater) and awards floor(100×correctPairs/totalPairs). A
// question whose cards are all equal is never generated.
package sortgame

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/bhcn/umaguessinggame/internal/modes"
	"github.com/bhcn/umaguessinggame/internal/modes/modeutil"
)

// ModeID is the stable mode identifier. It is the mode id used in URLs and round
// records; the package is named sortgame because sort is a standard library name.
const ModeID = "sort"

// RulesVersion is the rule revision this implementation satisfies.
const RulesVersion = "2.0.0-draft.1"

// Card count bounds .
const (
	MinCards     = 2
	MaxCards     = 6
	DefaultCards = 6
	// QuestionSeconds is the per-question window.
	QuestionSeconds = 90
)

// Sort keys .
const (
	// KeyHeight sorts by height in centimetres.
	KeyHeight = "height"
	// KeyBirthday sorts by month/day birthday, never mixing in the prototype's
	// birth year .
	KeyBirthday = "birthday"
	// KeyEventDate sorts by a verified event date.
	KeyEventDate = "eventDate"
)

// Action kinds.
//
// The value is the contract discriminator (ModeAction → SortOrder), not an
// internal name .
const (
	// ActionSort submits a full ordering of the cards.
	ActionSort = "sort.order"
)

// ContractKinds returns the ModeAction discriminators this mode accepts.
func (*Mode) ContractKinds() []string { return []string{ActionSort} }

// Rejection codes.
const (
	CodeInvalidPayload = "invalid_payload"
	CodeUnknownAction  = "unknown_action"
	CodeIncomplete     = "incomplete_order"
	CodeUnknownCard    = "unknown_card"
	CodeDuplicateCard  = "duplicate_card"
)

// Config is the round configuration.
type Config struct {
	// Key selects the sort key.
	Key string `json:"key"`
	// CardCount is how many cards the round publishes.
	CardCount int `json:"cardCount"`
	// QuestionSeconds is the per-question window.
	QuestionSeconds int `json:"questionSeconds"`
	// EventRegion pins the region for event-date questions, because an
	// implementation date without a region and version is not admitted .
	EventRegion string `json:"eventRegion"`
	// InformationStyle is the shared open/closed switch . The published
	// schema requires it on every pack-backed mode, and the sort entry page sends
	// "open" for the practice form, but the mode used to drop it: an open round
	// behaved exactly like a closed one, which is a different game.
	InformationStyle string `json:"informationStyle"`
}

// InformationStyle values .
const (
	StyleClosed = "closed"
	StyleOpen   = "open"
)

// decodeConfig normalises the configuration.
func decodeConfig(raw json.RawMessage) (Config, error) {
	cfg := Config{
		Key:             KeyHeight,
		CardCount:       DefaultCards,
		QuestionSeconds: QuestionSeconds,
	}
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &cfg); err != nil {
			return Config{}, fmt.Errorf("sort: invalid config: %w", err)
		}
	}
	if cfg.Key == "" {
		cfg.Key = KeyHeight
	}
	cfg.EventRegion = strings.ToUpper(strings.TrimSpace(cfg.EventRegion))
	switch cfg.Key {
	case KeyHeight, KeyBirthday, KeyEventDate:
	default:
		return Config{}, fmt.Errorf("sort: unknown sort key %q", cfg.Key)
	}
	if cfg.CardCount == 0 {
		cfg.CardCount = DefaultCards
	}
	if cfg.CardCount < MinCards || cfg.CardCount > MaxCards {
		return Config{}, fmt.Errorf(
			"sort: cardCount %d outside [%d,%d]", cfg.CardCount, MinCards, MaxCards,
		)
	}
	if cfg.InformationStyle == "" {
		// Default to closed-book play without publishing sortable answer values
		cfg.InformationStyle = StyleClosed
	}
	switch cfg.InformationStyle {
	case StyleClosed, StyleOpen:
	default:
		return Config{}, fmt.Errorf(
			"sort: unknown informationStyle %q (want %q or %q)",
			cfg.InformationStyle, StyleClosed, StyleOpen,
		)
	}
	if cfg.QuestionSeconds == 0 {
		cfg.QuestionSeconds = QuestionSeconds
	}
	if cfg.QuestionSeconds < 0 {
		return Config{}, fmt.Errorf("sort: negative questionSeconds %d", cfg.QuestionSeconds)
	}
	return cfg, nil
}

// Card is one sortable record. A missing value is represented by the ok flag
// being false; an unknown value is not admitted to a question .
type Card struct {
	CharacterID string `json:"characterId"`
	Name        string `json:"name"`
	ImagePath   string `json:"imagePath"`
	// HeightCm is the height in centimetres.
	HeightCm int  `json:"heightCm,omitempty"`
	HeightOK bool `json:"-"`
	// BirthMonth and BirthDay form the birthday. The prototype's birth year is
	// deliberately absent .
	BirthMonth int  `json:"birthMonth,omitempty"`
	BirthDay   int  `json:"birthDay,omitempty"`
	BirthdayOK bool `json:"-"`
	// EventDate is an ISO date with its region.
	EventDate   string `json:"eventDate,omitempty"`
	EventRegion string `json:"eventRegion,omitempty"`
	EventDateOK bool   `json:"-"`
}

// value returns the comparable value for the key, plus whether it is known.
//
// Height and birthday compare numerically. The event date compares by parsed
// year/month/day components, never by the raw string: forbids ordering date
// strings by language-literal collation.
func (c Card) value(key string) (sortValue, bool) {
	switch key {
	case KeyHeight:
		if !c.HeightOK {
			return sortValue{}, false
		}
		return sortValue{a: c.HeightCm}, true
	case KeyBirthday:
		if !c.BirthdayOK || !validMonthDay(c.BirthMonth, c.BirthDay) {
			return sortValue{}, false
		}
		// Encode as a single comparable integer so comparison is exact integer
		// arithmetic with no floating point.
		return sortValue{a: c.BirthMonth*100 + c.BirthDay}, true
	case KeyEventDate:
		if !c.EventDateOK {
			return sortValue{}, false
		}
		y, m, d, ok := parseISODate(c.EventDate)
		if !ok {
			return sortValue{}, false
		}
		return sortValue{a: y*10000 + m*100 + d}, true
	default:
		return sortValue{}, false
	}
}

// sortValue is a comparable value for one card.
type sortValue struct {
	a int
}

// validMonthDay reports whether month/day is a real calendar day.
func validMonthDay(month, day int) bool {
	if month < 1 || month > 12 {
		return false
	}
	if day < 1 {
		return false
	}
	// Days per month, February allowing 29 so a leap-day birthday is admitted.
	days := [...]int{31, 29, 31, 30, 31, 30, 31, 31, 30, 31, 30, 31}
	return day <= days[month-1]
}

// parseISODate parses a "YYYY-MM-DD" date into its components. It rejects any
// other shape so a locale-formatted string can never be sorted by accident.
func parseISODate(s string) (year, month, day int, ok bool) {
	parts := strings.Split(s, "-")
	if len(parts) != 3 {
		return 0, 0, 0, false
	}
	if len(parts[0]) != 4 || len(parts[1]) != 2 || len(parts[2]) != 2 {
		return 0, 0, 0, false
	}
	for _, part := range parts {
		for _, r := range part {
			if r < '0' || r > '9' {
				return 0, 0, 0, false
			}
		}
	}
	year = atoi(parts[0])
	month = atoi(parts[1])
	day = atoi(parts[2])
	if !validMonthDay(month, day) {
		return 0, 0, 0, false
	}
	if _, err := time.Parse("2006-01-02", s); err != nil || year == 0 {
		return 0, 0, 0, false
	}
	return year, month, day, true
}

// atoi converts a validated all-digit string to an int.
func atoi(s string) int {
	n := 0
	for i := 0; i < len(s); i++ {
		n = n*10 + int(s[i]-'0')
	}
	return n
}

// parseCard converts one decoded record into a Card.
func parseCard(fm modeutil.FieldMap) (Card, bool) {
	id := fm.Str("characterId", "id", "character_id")
	if id == "" {
		return Card{}, false
	}
	card := Card{
		CharacterID: id,
		Name:        fm.Str("name", "nameZh", "name_zh"),
		ImagePath:   fm.Str("imagePath", "image_path", "image_local_path"),
	}
	if h, ok := fm.Int("heightCm", "height_cm", "height"); ok {
		card.HeightCm, card.HeightOK = h, true
	}
	if m, ok := fm.Int("birthMonth", "birth_month"); ok {
		card.BirthMonth = m
		if d, ok := fm.Int("birthDay", "birth_day"); ok {
			card.BirthDay, card.BirthdayOK = d, true
		}
	}
	if ev, ok := fm.Object("eventDate", "event_date"); ok {
		card.EventDate = ev.Str("date")
		card.EventRegion = ev.Str("region")
		card.EventDateOK = card.EventDate != ""
	} else if date := fm.Str("eventDate", "event_date"); date != "" {
		card.EventDate = date
		card.EventRegion = fm.Str("eventRegion", "event_region")
		card.EventDateOK = true
	}
	return card, true
}

// loadCards reads the sort section.
func loadCards(src modes.ContentSource) ([]Card, error) {
	raw, err := modeutil.Section(src, "sort.cards", "sort", "sort.cards")
	if err != nil {
		return nil, err
	}
	records, err := modeutil.Records(raw, "cards", "items")
	if err != nil {
		return nil, err
	}
	out := make([]Card, 0, len(records))
	for _, rec := range records {
		if card, ok := parseCard(rec); ok {
			out = append(out, card)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CharacterID < out[j].CharacterID })
	return out, nil
}

// publicPrepared is the non-secret prepared state. The values themselves are
// public: this is an open-book ordering task .
type publicPrepared struct {
	ModeID       string        `json:"modeId"`
	RulesVersion string        `json:"rulesVersion"`
	Config       Config        `json:"config"`
	Cards        []publicCard  `json:"cards"`
	Subjects     []rosterEntry `json:"subjects"`
	// TotalPairs is the scoring denominator.
	TotalPairs int `json:"totalPairs"`
}

// publicCard is one card with its displayed value for the active key.
type publicCard struct {
	CharacterID string `json:"characterId"`
	Name        string `json:"name"`
	ImagePath   string `json:"imagePath"`
	// Withhold formatted values too: they reveal the same ordering as raw numbers
	Display string `json:"display,omitempty"`
	// Raw values are withheld in closed-book play and published in open-book play
	Raw *int `json:"raw,omitempty"`
}

// rosterEntry is one participant.
type rosterEntry struct {
	SubjectID   string            `json:"subjectId"`
	SubjectKind modes.SubjectKind `json:"subjectKind"`
}

// privatePrepared carries the true ordering.
type privatePrepared struct {
	// Order lists card ids in ascending value order; cards with equal values are
	// listed in id order and marked as tied by EqualGroups.
	Order []string `json:"order"`
	// Values maps card id → comparable value.
	Values map[string]int `json:"values"`
	// Formatted values remain private in closed-book play because they reveal the ordering
	Displays map[string]string `json:"displays"`
}

// state is one subject's participant state.
type state struct {
	// Order is the submitted card order, ascending.
	Order []string `json:"order"`
	// Groups preserves the participant's ties, never inferred from the answer.
	Groups [][]string `json:"groups,omitempty"`
	// Revisions counts accepted submissions.
	Revisions int `json:"revisions"`
	// CorrectPairs and TotalPairs describe the last accepted submission.
	CorrectPairs int `json:"correctPairs"`
	TotalPairs   int `json:"totalPairs"`
	// Submitted reports whether any order was accepted.
	Submitted bool `json:"submitted"`
}

// Mode implements modes.Mode for G06.
type Mode struct{}

// New returns the sort mode.
func New() *Mode { return &Mode{} }

// ID implements modes.Mode.
func (*Mode) ID() string { return ModeID }

// RulesVersion implements modes.Mode.
func (*Mode) RulesVersion() string { return RulesVersion }

// Title implements modes.Mode.
func (*Mode) Title() string { return "档案排序" }

// Summary implements modes.Mode.
func (*Mode) Summary() string {
	return "按身高、月日生日或已核实事件日期把档案卡排成正确顺序，同值可并列。"
}

// InformationStyles reports which information forms this mode implements.
func (*Mode) InformationStyles() []string {
	return []string{"open", "closed"}
}

// RoundWindow implements modes.Mode.
//
// 整局一次排序窗口。返回 0 表示本模式没有自己的窗口意见，交给房间层默认值。
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
		Fields:      []string{"heightCm", "birthMonth", "birthDay", "eventDate"},
		MinEntities: MinCards,
		NeedsAssets: false,
	}
}

// ValidateConfig implements modes.Mode.
func (*Mode) ValidateConfig(cfg json.RawMessage) error {
	_, err := decodeConfig(cfg)
	return err
}

// Prepare implements modes.Mode.
//
// It refuses a question whose cards are all equal, because such a question has
// no distinguishable answer and the documented rule forbids generating it
// .
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
	all, err := loadCards(in.Content)
	if err != nil {
		return modes.Prepared{}, err
	}
	if cfg.Key == KeyEventDate && cfg.EventRegion == "" {
		for _, card := range all {
			if _, known := card.value(KeyEventDate); !known || card.EventRegion == "" {
				continue
			}
			if cfg.EventRegion != "" && card.EventRegion != cfg.EventRegion {
				return modes.Prepared{}, fmt.Errorf("sort: eventRegion is required when the pool contains multiple regions")
			}
			cfg.EventRegion = card.EventRegion
		}
	}

	// Only cards with a known value for the active key are admissible .
	usable := make([]Card, 0, len(all))
	for _, card := range all {
		if _, ok := card.value(cfg.Key); !ok {
			continue
		}
		// An event-date question must pin the region, otherwise dates from
		// different regions would be mixed .
		if cfg.Key == KeyEventDate {
			if card.EventRegion == "" || card.EventRegion != cfg.EventRegion {
				continue
			}
		}
		usable = append(usable, card)
	}
	if len(usable) < cfg.CardCount {
		return modes.Prepared{}, fmt.Errorf(
			"sort: only %d cards have a known %s value, need %d",
			len(usable), cfg.Key, cfg.CardCount,
		)
	}

	// Draw the question deterministically from the seed.
	indices := make([]int, len(usable))
	for i := range indices {
		indices[i] = i
	}
	stream := modeutil.NewStream(in.Scope.Seed + "\x00sort.draw")
	stream.Shuffle(len(indices), func(i, j int) { indices[i], indices[j] = indices[j], indices[i] })

	cards := make([]Card, 0, cfg.CardCount)
	for i := 0; i < cfg.CardCount; i++ {
		cards = append(cards, usable[indices[i]])
	}
	// Replace a constant draw from the remaining shuffled cards in one pass.
	// Retrying with new seeds could fail repeatedly on pools dominated by ties.
	first, _ := cards[0].value(cfg.Key)
	allEqual := true
	for _, card := range cards[1:] {
		v, _ := card.value(cfg.Key)
		if v.a != first.a {
			allEqual = false
			break
		}
	}
	if allEqual {
		for _, index := range indices[cfg.CardCount:] {
			candidate := usable[index]
			value, _ := candidate.value(cfg.Key)
			if value != first {
				cards[len(cards)-1] = candidate
				allEqual = false
				break
			}
		}
		if allEqual {
			return modes.Prepared{}, fmt.Errorf("sort: all usable cards share the same %s value", cfg.Key)
		}
	}
	sort.Slice(cards, func(i, j int) bool { return cards[i].CharacterID < cards[j].CharacterID })

	roster := make([]rosterEntry, 0, len(in.Subject))
	seen := make(map[string]struct{}, len(in.Subject))
	for _, subj := range in.Subject {
		if err := subj.Validate(); err != nil {
			return modes.Prepared{}, err
		}
		if _, dup := seen[subj.ID]; dup {
			return modes.Prepared{}, fmt.Errorf("sort: duplicate subject %q", subj.ID)
		}
		seen[subj.ID] = struct{}{}
		roster = append(roster, rosterEntry{SubjectID: subj.ID, SubjectKind: subj.Kind})
	}
	if len(roster) < 1 || len(roster) > 6 {
		return modes.Prepared{}, fmt.Errorf("sort: %d subjects outside [1,6]", len(roster))
	}

	values := make(map[string]int, len(cards))
	displays := make(map[string]string, len(cards))
	pubCards := make([]publicCard, 0, len(cards))
	for _, card := range cards {
		v, _ := card.value(cfg.Key)
		values[card.CharacterID] = v.a
		displays[card.CharacterID] = displayValue(card, cfg.Key)
		pc := publicCard{
			CharacterID: card.CharacterID,
			Name:        card.Name,
			ImagePath:   card.ImagePath,
		}
		// Open-book publishes the comparable value on every card: says the
		// open form is "所有判定字段在局内卡面可查", so the player can see the key
		// and plan instead of recalling it. Closed-book publishes identities only,
		// because a client holding exact values can sort them and submit a perfect
		// answer without knowing anything about the characters .
		//
		// Display is the human-readable form; Raw is the number. Open-book ships
		// both so the card can show "160cm" while still sorting exactly, and
		// closed-book ships neither.
		if cfg.InformationStyle == StyleOpen {
			pc.Display = displays[card.CharacterID]
			raw := values[card.CharacterID]
			pc.Raw = &raw
		}
		pubCards = append(pubCards, pc)
	}

	// The true order: ascending value, then id, so ties are broken only for
	// presentation; equality is preserved through the values map.
	order := make([]string, 0, len(cards))
	for _, card := range cards {
		order = append(order, card.CharacterID)
	}
	sort.SliceStable(order, func(i, j int) bool {
		vi, vj := values[order[i]], values[order[j]]
		if vi != vj {
			return vi < vj
		}
		return order[i] < order[j]
	})

	pub := publicPrepared{
		ModeID:       ModeID,
		RulesVersion: RulesVersion,
		Config:       cfg,
		Cards:        pubCards,
		Subjects:     roster,
		TotalPairs:   pairCount(len(cards)),
	}
	priv := privatePrepared{Order: order, Values: values, Displays: displays}
	return modes.Prepared{
		Public:        modeutil.Marshal(pub),
		Private:       modeutil.Marshal(priv),
		QuestionCount: 1,
	}, nil
}

// displayValue renders the card's value for the active key.
func displayValue(card Card, key string) string {
	switch key {
	case KeyHeight:
		return fmt.Sprintf("%dcm", card.HeightCm)
	case KeyBirthday:
		return fmt.Sprintf("%02d/%02d", card.BirthMonth, card.BirthDay)
	case KeyEventDate:
		return card.EventDate
	default:
		return ""
	}
}

// pairCount returns n×(n-1)/2.
func pairCount(n int) int {
	if n < 2 {
		return 0
	}
	return n * (n - 1) / 2
}

// sortPayload is the body of ActionSort, matching the contract's SortOrder schema:
// {kind, groups[][]}.
//
// The outer array is the ascending order and each inner array is one value group.
// A single-element group is a distinct value; a multi-element group states that
// those cards share one value, which is exactly how the documented equal-value
// tie must be expressed to score .
type sortPayload struct {
	Kind   string     `json:"kind"`
	Groups [][]string `json:"groups"`
}

// Apply implements modes.Mode.
func (*Mode) Apply(in modes.ApplyInput) (modes.Decision, error) {
	if in.Action.Kind != ActionSort {
		return modes.Rejected(CodeUnknownAction,
			fmt.Sprintf("sort: unsupported action %q", in.Action.Kind)), nil
	}
	if err := in.Action.Validate(); err != nil {
		return modes.Rejected(CodeInvalidPayload, err.Error()), nil
	}
	var payload sortPayload
	if err := json.Unmarshal(in.Action.Payload, &payload); err != nil {
		return modes.Rejected(CodeInvalidPayload,
			"sort: sort payload must carry a groups array"), nil
	}
	if payload.Kind != "" && payload.Kind != ActionSort {
		return modes.Rejected(CodeInvalidPayload, fmt.Sprintf(
			"sort: payload kind %q does not match action %q", payload.Kind, ActionSort,
		)), nil
	}

	var pub publicPrepared
	if len(in.Public) > 0 {
		if err := json.Unmarshal(in.Public, &pub); err != nil {
			return modes.Decision{}, fmt.Errorf("sort: decode public state: %w", err)
		}
	}
	var priv privatePrepared
	if len(in.Private) > 0 {
		if err := json.Unmarshal(in.Private, &priv); err != nil {
			return modes.Decision{}, fmt.Errorf("sort: decode private state: %w", err)
		}
	}
	var st state
	if len(in.State) > 0 {
		if err := json.Unmarshal(in.State, &st); err != nil {
			return modes.Decision{}, fmt.Errorf("sort: decode subject state: %w", err)
		}
	}

	known := make(map[string]struct{}, len(pub.Cards))
	for _, card := range pub.Cards {
		known[card.CharacterID] = struct{}{}
	}

	for _, group := range payload.Groups {
		if len(group) == 0 {
			return modes.Rejected(CodeIncomplete, "sort: empty value group"), nil
		}
	}
	order, slot := expandGroups(payload.Groups)
	if len(slot) != len(order) {
		return modes.Rejected(CodeDuplicateCard,
			"sort: the order lists the same card twice"), nil
	}
	if len(order) != len(pub.Cards) {
		return modes.Rejected(CodeIncomplete, fmt.Sprintf(
			"sort: order covers %d cards, need %d", len(order), len(pub.Cards),
		)), nil
	}
	for _, id := range order {
		if _, ok := known[id]; !ok {
			return modes.Rejected(CodeUnknownCard,
				fmt.Sprintf("sort: card %q is not in this question", id)), nil
		}
	}

	correct, total := compareOrder(order, slot, priv.Values)

	st.Order = order
	st.Groups = payload.Groups
	st.Revisions++
	st.CorrectPairs = correct
	st.TotalPairs = total
	st.Submitted = true

	units := scoreUnits(correct, total)
	feedbackFields := map[string]int{"totalPairs": total, "revisions": st.Revisions}
	// Closed-book revisions must not become an oracle for the hidden order.
	if pub.Config.InformationStyle == StyleOpen {
		feedbackFields["correctPairs"] = correct
		feedbackFields["scoreUnits"] = units
	}
	feedback := modeutil.Marshal(feedbackFields)

	// Revisions are allowed until the deadline .
	return modes.Accepted(feedback, modeutil.Marshal(st)), nil
}

// expandGroups flattens the contract's value groups into an ordered id list and a
// per-id slot index. Every member of one group shares that group's slot, which is
// how an equal-value tie is expressed.
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

// compareOrder grades an order by comparing every card pair's relation.
//
// The submitted relation comes from the slot indices (a lower slot means "less",
// an equal slot means "equal"); the true relation comes from the values. A pair
// counts only when the two relations agree, which is exactly how a genuine tie
// must be stated as a tie to score .
func compareOrder(order []string, slot map[string]int, values map[string]int) (correct, total int) {
	n := len(order)
	if n < 2 {
		return 0, 0
	}
	for i := 0; i < n; i++ {
		for j := i + 1; j < n; j++ {
			a, b := order[i], order[j]
			va, okA := values[a]
			vb, okB := values[b]
			if !okA || !okB {
				continue
			}
			total++
			if relationOf(slot[a], slot[b]) == relationOf(va, vb) {
				correct++
			}
		}
	}
	return correct, total
}

// relation is one pair's relative relation.
type relation int

const (
	less relation = iota
	equal
	greater
)

// relationOf returns the relation of a to b for two comparable integers.
func relationOf(a, b int) relation {
	switch {
	case a < b:
		return less
	case a > b:
		return greater
	default:
		return equal
	}
}

// scoreUnits converts correct/total into integer scoreUnits using the documented
// floor(100×correct/total).
func scoreUnits(correct, total int) int {
	if total <= 0 || correct <= 0 {
		return 0
	}
	return modeutil.Units(correct * 100 / total)
}

// projection is the viewer-specific state.
type projection struct {
	ModeID       string        `json:"modeId"`
	RulesVersion string        `json:"rulesVersion"`
	Phase        modes.Phase   `json:"phase"`
	Config       Config        `json:"config"`
	Cards        []publicCard  `json:"cards"`
	TotalPairs   int           `json:"totalPairs"`
	Subjects     []progress    `json:"subjects"`
	Self         *selfSort     `json:"self,omitempty"`
	Answer       *answerReveal `json:"answer,omitempty"`
}

// progress is the coarse per-subject progress.
type progress struct {
	SubjectID    string `json:"subjectId"`
	Submitted    bool   `json:"submitted"`
	Revisions    int    `json:"revisions"`
	CorrectPairs *int   `json:"correctPairs,omitempty"`
	TotalPairs   int    `json:"totalPairs"`
}

// selfSort is the caller's own state.
type selfSort struct {
	SubjectID    string     `json:"subjectId"`
	Order        []string   `json:"order"`
	Groups       [][]string `json:"groups,omitempty"`
	Submitted    bool       `json:"submitted"`
	Revisions    int        `json:"revisions"`
	CorrectPairs *int       `json:"correctPairs,omitempty"`
	TotalPairs   int        `json:"totalPairs"`
}

// answerReveal is the true ordering, released on reveal.
type answerReveal struct {
	Order  []string        `json:"order"`
	Values []revealedValue `json:"values"`
}

// revealedValue is one card's true value.
type revealedValue struct {
	CharacterID string `json:"characterId"`
	Value       int    `json:"value"`
}

// Project implements modes.Mode.
func (*Mode) Project(in modes.ProjectInput) (json.RawMessage, error) {
	var pub publicPrepared
	if len(in.Public) > 0 {
		if err := json.Unmarshal(in.Public, &pub); err != nil {
			return nil, fmt.Errorf("sort: decode public state: %w", err)
		}
	}
	var priv privatePrepared
	if len(in.Private) > 0 {
		if err := json.Unmarshal(in.Private, &priv); err != nil {
			return nil, fmt.Errorf("sort: decode private state: %w", err)
		}
	}

	out := projection{
		ModeID:       ModeID,
		RulesVersion: RulesVersion,
		Phase:        in.Phase,
		Config:       pub.Config,
		Cards:        pub.Cards,
		TotalPairs:   pub.TotalPairs,
		Subjects:     make([]progress, 0, len(pub.Subjects)),
	}
	revealing := in.Viewer.SeesSecrets() || (in.RevealNow && in.Phase.Terminal())
	for _, entry := range pub.Subjects {
		var st state
		if raw, ok := in.States[entry.SubjectID]; ok && len(raw) > 0 {
			if err := json.Unmarshal(raw, &st); err != nil {
				return nil, fmt.Errorf("sort: decode state of %q: %w", entry.SubjectID, err)
			}
		}
		var correctPairs *int
		if revealing || pub.Config.InformationStyle == StyleOpen {
			correctPairs = &st.CorrectPairs
		}
		out.Subjects = append(out.Subjects, progress{
			SubjectID:    entry.SubjectID,
			Submitted:    st.Submitted,
			Revisions:    st.Revisions,
			CorrectPairs: correctPairs,
			TotalPairs:   st.TotalPairs,
		})
		if entry.SubjectID == in.Viewer.Subject.ID {
			out.Self = &selfSort{
				SubjectID:    entry.SubjectID,
				Order:        st.Order,
				Groups:       st.Groups,
				Submitted:    st.Submitted,
				Revisions:    st.Revisions,
				CorrectPairs: correctPairs,
				TotalPairs:   st.TotalPairs,
			}
		}
	}

	// Raw values are attached only on reveal, from the same source the answer uses.
	if revealing {
		for i := range out.Cards {
			if v, ok := priv.Values[out.Cards[i].CharacterID]; ok {
				val := v
				out.Cards[i].Raw = &val
			}
			// The display string is released with the value it describes.
			out.Cards[i].Display = priv.Displays[out.Cards[i].CharacterID]
		}
		reveal := &answerReveal{Order: priv.Order}
		for _, id := range priv.Order {
			reveal.Values = append(reveal.Values, revealedValue{
				CharacterID: id,
				Value:       priv.Values[id],
			})
		}
		out.Answer = reveal
	}
	return modeutil.Marshal(out), nil
}

// Settle implements modes.Mode.
func (*Mode) Settle(in modes.SettleInput) ([]modes.Score, error) {
	var pub publicPrepared
	if len(in.Public) > 0 {
		if err := json.Unmarshal(in.Public, &pub); err != nil {
			return nil, fmt.Errorf("sort: decode public state: %w", err)
		}
	}
	scores := make([]modes.Score, 0, len(pub.Subjects))
	for _, entry := range pub.Subjects {
		var st state
		if raw, ok := in.States[entry.SubjectID]; ok && len(raw) > 0 {
			if err := json.Unmarshal(raw, &st); err != nil {
				return nil, fmt.Errorf("sort: decode state of %q: %w", entry.SubjectID, err)
			}
		}
		units := 0
		if st.Submitted {
			units = scoreUnits(st.CorrectPairs, st.TotalPairs)
		}
		explanation := modeutil.Marshal(struct {
			Submitted    bool   `json:"submitted"`
			CorrectPairs int    `json:"correctPairs"`
			TotalPairs   int    `json:"totalPairs"`
			Key          string `json:"key"`
			Revisions    int    `json:"revisions"`
		}{
			Submitted:    st.Submitted,
			CorrectPairs: st.CorrectPairs,
			TotalPairs:   st.TotalPairs,
			Key:          pub.Config.Key,
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
