// Package trade implements G13 档案交易会 .
//
// Three to eight players each hold four instance cards and two secret objectives.
// Over three rounds of sixty seconds they negotiate freely, then both sides confirm
// a swap of one card each. Hands are public; objectives are private.
//
// The invariants this package exists to guarantee:
//
// 1. An instance cannot be sold twice. A card instance id is unique and moves
// between exactly one pair of hands. Every proposal names a specific instance,
// and the exchange is validated against the current hands at commit time, so a
// second buyer of the same instance is refused rather than silently losing the
// card.
//
// 2. At most one unconfirmed proposal per player. A player who already has an
// open proposal must cancel it before opening another, which is the documented
// limit .
//
// 3. The exchange is atomic. Both cards move in the same fold, or neither
// does. The fold is a pure function of the pre-trade hands, so a retried
// transaction recomputes byte-identical hands.
//
// 4. Objectives are never public. A player's objectives live in their own
// state and in the server-only state; no projection ever includes another
// player's objectives.
//
// Settle reports objective completion. Scoring is disabled unless the round opts in.
package trade

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"time"

	"github.com/bhcn/umaguessinggame/internal/modes"
	"github.com/bhcn/umaguessinggame/internal/modes/modeutil"
)

// ModeID is the stable mode identifier (contract ModeId enum).
const ModeID = "trade"

// RulesVersion is the rule revision this implementation satisfies.
const RulesVersion = "2.0.0-draft.1"

// Action kinds.
//
// trade is the one G-mode whose wire shape is NOT a ModeAction discriminator. The
// contract models it as its own resource pair — POST /rounds/{roundId}/trades
// (createTrade) and POST /rounds/{roundId}/trades/{tradeId}/responses
// (respondToTrade) — with the TradeCreate / TradeResponse bodies. There is no
// trade entry in the ModeAction discriminator mapping, so a trade action cannot be
// posted through POST /rounds/{roundId}/submissions.
//
// The rule layer still needs typed kinds to distinguish the three operations, so the
// names below are the internal action vocabulary. Mode deliberately does NOT
// implement ContractKinds: declaring these strings as contract kinds would make the
// alignment test pass against a mapping that does not contain them, which is the
// exact drift that test exists to catch. The room layer is expected to route the two
// trade resources to Apply with these kinds.
const (
	// ActionKindPropose opens a proposal. Reached through createTrade.
	ActionKindPropose = "trade.propose"
	// ActionKindRespond accepts or rejects a proposal addressed to the caller.
	// Reached through respondToTrade.
	ActionKindRespond = "trade.respond"
	// ActionKindCancel withdraws the caller's own open proposal. The contract has no
	// dedicated operation for it in this build; it is listed so the rule layer has a
	// complete vocabulary, and the room layer may expose it as a DELETE on the trade
	// resource when that operation is added.
	ActionKindCancel = "trade.cancel"
)

// ContractResource documents the wire shape this mode is reached through, so a reader
// looking for ContractKinds finds the explanation instead of assuming it was omitted by
// mistake.
func (*Mode) ContractResource() string {
	return "POST /rounds/{roundId}/trades + POST /rounds/{roundId}/trades/{tradeId}/responses"
}

// Rejection codes.
const (
	CodeInvalidPayload = "invalid_payload"
	CodeUnknownAction  = "unknown_action"
	CodeNotOnRoster    = "not_on_roster"
	CodeUnknownTrade   = "unknown_trade"
	CodeTradeClosed    = "trade_closed"
	CodeTradeNotFound  = "trade_not_found"
	CodeAlreadyOpen    = "already_open_proposal"
	CodeNotYourTrade   = "not_your_trade"
	CodeSelfTrade      = "self_trade"
	CodeInstanceGone   = "instance_not_held"
	CodeSameInstance   = "same_instance"
	CodeRoundClosed    = "round_closed"
)

// Documented parameters .
const (
	// HandSize is how many instance cards each player holds.
	HandSize = 4
	// ObjectiveCount is how many secret objectives each player holds.
	ObjectiveCount = 2
	// TotalRounds is how many trading rounds a match has.
	TotalRounds = 3
	// RoundSeconds is the negotiation window per round.
	RoundSeconds = 60
	// ObjectivePoints is the value of one completed objective, in display points.
	ObjectivePoints = 50
	// MinSubjects and MaxSubjects bound the player count.
	MinSubjects = 3
	MaxSubjects = 8
)

// TradeStatus is a proposal's state.
type TradeStatus string

const (
	// StatusPending is an open proposal awaiting the recipient.
	StatusPending TradeStatus = "pending"
	// StatusAccepted is a completed exchange.
	StatusAccepted TradeStatus = "accepted"
	// StatusRejected is a declined proposal.
	StatusRejected TradeStatus = "rejected"
	// StatusCancelled is a withdrawn proposal.
	StatusCancelled TradeStatus = "cancelled"
	// StatusExpired is a proposal cancelled by the round boundary.
	StatusExpired TradeStatus = "expired"
)

// open reports whether the proposal still occupies its sender's single slot.
func (s TradeStatus) open() bool { return s == StatusPending }

// Config is the round configuration.
type Config struct {
	// Rounds is the number of trading rounds.
	Rounds int `json:"rounds"`
	// RoundSeconds is the negotiation window per round.
	RoundSeconds int `json:"roundSeconds"`
	// Competitive opts the mode into the formal cup. It defaults to false, which is
	// the documented behaviour: the experimental trade mode does not enter the
	// formal cup .
	Competitive bool `json:"competitive"`
	// AllowObjectiveReuse reports whether one card may satisfy several objectives.
	// The plan fixes this to "not allowed" for the first version
	// , so the field exists to reject a
	// request to change it rather than to allow one silently.
	AllowObjectiveReuse bool `json:"allowObjectiveReuse"`
}

// decodeConfig parses and normalises a configuration blob.
func decodeConfig(raw json.RawMessage) (Config, error) {
	cfg := Config{Rounds: TotalRounds, RoundSeconds: RoundSeconds}
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &cfg); err != nil {
			return Config{}, fmt.Errorf("trade: invalid config: %w", err)
		}
	}
	if cfg.Rounds == 0 {
		cfg.Rounds = TotalRounds
	}
	if cfg.RoundSeconds == 0 {
		cfg.RoundSeconds = RoundSeconds
	}
	if cfg.Rounds < 1 || cfg.Rounds > 6 {
		return Config{}, fmt.Errorf("trade: rounds %d outside [1,6]", cfg.Rounds)
	}
	if cfg.RoundSeconds < 20 || cfg.RoundSeconds > 300 {
		return Config{}, fmt.Errorf(
			"trade: roundSeconds %d outside [20,300]", cfg.RoundSeconds)
	}
	// The first version fixes the reuse rule. Accepting the flag would change a
	// published scoring rule mid-flight, so it is refused rather than honoured.
	if cfg.AllowObjectiveReuse {
		return Config{}, fmt.Errorf(
			"trade: one card may not satisfy several objectives in this rules version")
	}
	return cfg, nil
}

// objective is one secret scoring requirement.
//
// An objective is a set of card kinds the player must hold at the end. The first
// version uses kind sets rather than named cards so the deck can be themed without
// rewriting the objective language.
type objective struct {
	ObjectiveID string `json:"objectiveId"`
	// Text is the human-readable requirement.
	Text string `json:"text"`
	// Kinds lists the card kinds the hand must contain. A kind may appear once:
	// duplicates would be satisfied by a single card, which is exactly the reuse
	// the first version forbids.
	Kinds []string `json:"kinds"`
	// Points is the objective's value in display points.
	Points int `json:"points"`
	// Provenance records where the objective came from. It is carried into the public
	// state so a player and a host can both see that this is designed content rather
	// than official game data.
	Provenance provenance `json:"provenance"`
}

// instance is one physical card instance.
type instance struct {
	// InstanceID is the unique instance identity. It never changes hands twice for
	// the same trade, and it is the unit the trade moves.
	InstanceID string `json:"instanceId"`
	// CardID is the card kind this instance belongs to.
	CardID string `json:"cardId"`
	// Label is the display name.
	Label string `json:"label"`
}

// rosterEntry is one participant recorded in the public prepared state.
type rosterEntry struct {
	SubjectID   string            `json:"subjectId"`
	SubjectKind modes.SubjectKind `json:"subjectKind"`
}

// tradeRecord is one proposal.
type tradeRecord struct {
	TradeID string `json:"tradeId"`
	// SenderID and RecipientID are subject ids.
	SenderID    string `json:"senderId"`
	RecipientID string `json:"recipientId"`
	// OfferedInstanceID and RequestedInstanceID name specific instances.
	OfferedInstanceID   string      `json:"offeredInstanceId"`
	RequestedInstanceID string      `json:"requestedInstanceId"`
	Status              TradeStatus `json:"status"`
	// Round is the trading round the proposal was opened in.
	Round int `json:"round"`
	// PhaseVersion is the round phase version at proposal time, so an acceptance
	// from a stale view is refused rather than applied to a different board.
	PhaseVersion int `json:"phaseVersion"`
}

// publicState is the non-secret round state.
//
// Hands are public . The objectives are NOT here: they are
// secret and live in the server-only state plus each owner's own state.
type publicState struct {
	ModeID       string        `json:"modeId"`
	RulesVersion string        `json:"rulesVersion"`
	Config       Config        `json:"config"`
	Subjects     []rosterEntry `json:"subjects"`
	// Hands maps subject id → the instance ids that subject currently holds, in a
	// stable order.
	Hands map[string][]string `json:"hands"`
	// Instances is the catalogue of every instance in play, so a client can show a
	// card's label without a second request.
	Instances []instance `json:"instances"`
	// Trades is the append-only proposal log, in a canonical order.
	Trades []tradeRecord `json:"trades"`
	// CurrentRound is the trading round now open, 1-based; TotalRounds+1 once the
	// match is over.
	CurrentRound int `json:"currentRound"`
	// Objectives is the FULL objective catalogue, published at start because
	// requires every scoring condition to be known before play. Which objective a
	// particular player holds is private and lives in the per-subject state.
	Objectives []objective `json:"objectives"`
	// CardKinds is the card-kind catalogue, so a client can render a task requirement
	// without a second request.
	CardKinds []cardKind `json:"cardKinds"`
	// Scoring explains the objective valuation.
	Scoring string `json:"scoring"`
}

// subjectPrivateState is one player's own private projection.
//
// It carries only that player's objectives, never the whole map: the room layer stores
// one row per subject and hands a viewer their own row alone.
type subjectPrivateState struct {
	ModeID     string      `json:"modeId"`
	Objectives []objective `json:"objectives"`
	// Hand is the instance ids this player currently holds.
	//
	// It is duplicated from the public projection on purpose: the room layer's
	// holdsInstance() reads the proposer's own private state as the authority for
	// what they hold, and it cannot see a mode's public blob. Without this field that
	// check always failed and every trade proposal was rejected with card-not-held.
	Hand []string `json:"hand"`
}

// privateState is the server-only round state: every player's objectives.
type privateState struct {
	ModeID string `json:"modeId"`
	// Objectives maps subject id → that subject's secret objectives.
	Objectives map[string][]objective `json:"objectives"`
	// ObjectivePool is the full objective catalogue, kept so the reveal can show
	// every objective and not only the ones a player held.
	ObjectivePool []objective `json:"objectivePool"`
}

// subjectState is one participant's own persisted state.
type subjectState struct {
	// Objectives is this participant's own secret objectives. It is duplicated
	// here so a projection can serve the owner without reading the private round
	// state, and so the room layer can hand the owner their own goals.
	Objectives []objective `json:"objectives"`
	// OpenTradeID is the proposal this participant has open, if any. A player may
	// hold at most one .
	OpenTradeID string `json:"openTradeId,omitempty"`
	// IncomingTradeIDs lists proposals addressed to this participant that are still
	// pending, in canonical order.
	IncomingTradeIDs []string `json:"incomingTradeIds,omitempty"`
}

// Mode implements modes.Mode for G13.
type Mode struct{}

// New returns the trade mode.
func New() *Mode { return &Mode{} }

// ID implements modes.Mode.
func (*Mode) ID() string { return ModeID }

// RulesVersion implements modes.Mode.
func (*Mode) RulesVersion() string { return RulesVersion }

// Title implements modes.Mode.
func (*Mode) Title() string { return "档案交易会" }

// Summary implements modes.Mode.
func (*Mode) Summary() string {
	return "手牌公开、任务保密：三轮谈判里各出一张卡，双方确认后一起交换，最后按手里的卡判定秘密任务。"
}

// InformationStyles reports which information forms this mode implements.
func (*Mode) InformationStyles() []string {
	return []string{"closed"}
}

// RoundWindow implements modes.Mode.
//
// 每轮谈判窗口 × 轮数。返回 0 表示本模式没有自己的窗口意见，交给房间层默认值。
func (*Mode) RoundWindow(raw json.RawMessage) time.Duration {
	cfg, err := decodeConfig(raw)
	if err != nil {
		return 0
	}
	return time.Duration(cfg.RoundSeconds) * time.Second * time.Duration(cfg.Rounds)
}

// TurnWindow supplies one independent negotiation window.
func (*Mode) TurnWindow(raw json.RawMessage) time.Duration {
	cfg, err := decodeConfig(raw)
	if err != nil {
		return 0
	}
	return time.Duration(cfg.RoundSeconds) * time.Second
}

func (*Mode) UsesIndividualSeats() bool { return true }

// Unfinished prevents early host transitions and historical partial settlements.
func Unfinished(public json.RawMessage) (bool, error) {
	pub, err := decodePublic(public)
	return pub.CurrentRound <= pub.Config.Rounds, err
}

// ExpireTurn closes every unfinished offer before opening the next negotiation.
func (*Mode) ExpireTurn(public json.RawMessage) (json.RawMessage, bool, error) {
	pub, err := decodePublic(public)
	if err != nil {
		return nil, false, err
	}
	if pub.CurrentRound > pub.Config.Rounds {
		return public, false, nil
	}
	for i := range pub.Trades {
		if pub.Trades[i].Status.open() {
			pub.Trades[i].Status = StatusExpired
		}
	}
	pub.CurrentRound++
	return modeutil.Marshal(pub), pub.CurrentRound <= pub.Config.Rounds, nil
}

// MinSubjects implements modes.Mode.
func (*Mode) MinSubjects() int { return MinSubjects }

// MaxSubjects implements modes.Mode.
func (*Mode) MaxSubjects() int { return MaxSubjects }

// Requirement implements modes.Mode.
func (*Mode) Requirement() modes.ContentRequirement {
	// Fields must name EVERY section loadCardKinds and loadObjectives read. A mode whose
	// declaration omits a section it actually needs is how this project produced an
	// "available but cannot start" defect: the declaration looked satisfied while Prepare
	// refused at runtime. The constants below are the same ones the loaders use, so the two
	// cannot drift apart.
	//
	// MinEntities is the smallest viable CONTENT POOL, counted in objectives: a round deals
	// ObjectiveCount objectives per player, and the table floor is MinSubjects, so the pool
	// floor is MinSubjects*ObjectiveCount. It was MinSubjects*HandSize, which counted CARDS
	// per hand against a pool of OBJECTIVES — a unit mismatch that happened to be close
	// enough to look right. HandSize is the card count, not the objective count.
	return modes.ContentRequirement{
		Fields:      []string{CardsSection, ObjectivesSection},
		MinEntities: MinSubjects * ObjectiveCount,
		NeedsAssets: false,
	}
}

// ValidateConfig implements modes.Mode.
func (*Mode) ValidateConfig(cfg json.RawMessage) error {
	_, err := decodeConfig(cfg)
	return err
}

// Section names this mode reads. They are the pack sections the content compiler
// publishes, and they are exactly the fields Requirement().Fields declares: a mode whose
// declared fields do not match what it reads is how this project produced two "available
// but cannot start" defects already.
const (
	// CardsSection carries the card-kind catalogue.
	CardsSection = "trade.cards"
	// ObjectivesSection carries the objective catalogue.
	ObjectivesSection = "trade.objectives"
)

// Provenance kinds. Only these three values are accepted; an entry carrying anything
// else is refused rather than published.
const (
	// ProvenanceAuthored is a value this project designed. It is not official game data.
	ProvenanceAuthored = "authored-design-value"
	// ProvenanceDerived is a value derived from a plan document by rule.
	ProvenanceDerived = "derived-from-doc"
	// ProvenanceOfficial is official game data. No entry uses it today.
	ProvenanceOfficial = "official-data"
)

// provenance records where a piece of content came from.
//
// # The limit of what this can prove
//
// The rule layer can check that Kind is one of the three enumerated values, that
// NotOfficialData is true for anything this project authored, and that Basis is
// non-empty. It CANNOT check that Basis names a real document, or that the content really
// was designed rather than copied from somewhere. That is a human review boundary, and it
// is recorded here rather than papered over: a program that cannot verify a claim must not
// pretend it did. (The project already paid for this lesson once: a review ledger claimed
// a human had approved facts that nobody had read.)
type provenance struct {
	// Kind is the provenance category.
	Kind string `json:"kind"`
	// NotOfficialData must be true for authored and derived content.
	NotOfficialData bool `json:"notOfficialData"`
	// Author names who produced the content.
	Author string `json:"author"`
	// Basis names the document or reasoning the content rests on. Its truth cannot be
	// checked by the program; see the type comment.
	Basis string `json:"basis"`
}

// valid reports whether a provenance block may be published.
func (p provenance) valid() error {
	switch p.Kind {
	case ProvenanceAuthored, ProvenanceDerived, ProvenanceOfficial:
	default:
		return fmt.Errorf("unknown provenance kind %q", p.Kind)
	}
	// Anything this project authored is not official data. Allowing false here would let
	// an authored value masquerade as official game data, which is exactly the confusion
	// the plan requires us to avoid .
	if p.Kind != ProvenanceOfficial && !p.NotOfficialData {
		return fmt.Errorf("provenance kind %q must set notOfficialData=true", p.Kind)
	}
	if p.Author == "" {
		return fmt.Errorf("provenance carries no author")
	}
	if p.Basis == "" {
		return fmt.Errorf("provenance carries no basis")
	}
	return nil
}

// cardKind is one authored card kind from the pack.
type cardKind struct {
	Kind       string     `json:"cardId"`
	Label      string     `json:"label"`
	Provenance provenance `json:"provenance"`
}

// loadCardKinds reads the card-kind catalogue from the pack.
//
// There is deliberately no built-in fallback. A fallback would make "the compiler produced
// no content" indistinguishable from "the mode works", which is the failure this project
// keeps hitting. A pack without this section means the mode cannot start, and Prepare says
// so.
func loadCardKinds(src modes.ContentSource) ([]cardKind, error) {
	if src == nil {
		return nil, fmt.Errorf(
			"trade: no content source; %q is required and there is no built-in fallback",
			CardsSection)
	}
	raw, err := modeutil.Section(src, CardsSection)
	if err != nil {
		return nil, fmt.Errorf(
			"trade: %q is required: %w (there is no built-in fallback; the pack must "+
				"publish the card catalogue)", CardsSection, err)
	}
	records, err := modeutil.Records(raw, "cards", "items")
	if err != nil {
		return nil, fmt.Errorf("trade: decode %q: %w", CardsSection, err)
	}
	out := make([]cardKind, 0, len(records))
	seen := make(map[string]struct{}, len(records))
	for _, rec := range records {
		kind := cardKind{
			Kind:  rec.Str("cardId", "card_id", "kind", "id"),
			Label: rec.Str("label", "name"),
		}
		if kind.Kind == "" || kind.Label == "" {
			continue
		}
		if _, dup := seen[kind.Kind]; dup {
			return nil, fmt.Errorf("trade: card kind %q appears twice in %q",
				kind.Kind, CardsSection)
		}
		seen[kind.Kind] = struct{}{}
		provRaw, ok := rec.Object("provenance")
		if !ok {
			return nil, fmt.Errorf(
				"trade: card kind %q carries no provenance block", kind.Kind)
		}
		kind.Provenance = provenance{
			Kind:            provRaw.Str("kind"),
			NotOfficialData: provRaw.Bool("notOfficialData", "not_official_data"),
			Author:          provRaw.Str("author"),
			Basis:           provRaw.Str("basis"),
		}
		if err := kind.Provenance.valid(); err != nil {
			return nil, fmt.Errorf("trade: card kind %q: %w", kind.Kind, err)
		}
		out = append(out, kind)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("trade: %q carries no usable card kind", CardsSection)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Kind < out[j].Kind })
	return out, nil
}

// loadObjectives reads the objective catalogue from the pack.
func loadObjectives(src modes.ContentSource) ([]objective, error) {
	if src == nil {
		return nil, fmt.Errorf(
			"trade: no content source; %q is required and there is no built-in fallback",
			ObjectivesSection)
	}
	raw, err := modeutil.Section(src, ObjectivesSection)
	if err != nil {
		return nil, fmt.Errorf(
			"trade: %q is required: %w (there is no built-in fallback; the pack must "+
				"publish the objective catalogue)", ObjectivesSection, err)
	}
	records, err := modeutil.Records(raw, "objectives", "items")
	if err != nil {
		return nil, fmt.Errorf("trade: decode %q: %w", ObjectivesSection, err)
	}
	out := make([]objective, 0, len(records))
	seen := make(map[string]struct{}, len(records))
	for _, rec := range records {
		obj := objective{
			ObjectiveID: rec.Str("objectiveId", "objective_id", "id"),
			Text:        rec.Str("text", "description"),
			Kinds:       rec.StrList("kinds", "cardKinds"),
		}
		if obj.ObjectiveID == "" || obj.Text == "" || len(obj.Kinds) == 0 {
			continue
		}
		if points, ok := rec.Int("points"); ok {
			obj.Points = points
		} else {
			obj.Points = ObjectivePoints
		}
		if _, dup := seen[obj.ObjectiveID]; dup {
			return nil, fmt.Errorf("trade: objective %q appears twice in %q",
				obj.ObjectiveID, ObjectivesSection)
		}
		seen[obj.ObjectiveID] = struct{}{}
		provRaw, ok := rec.Object("provenance")
		if !ok {
			return nil, fmt.Errorf(
				"trade: objective %q carries no provenance block", obj.ObjectiveID)
		}
		obj.Provenance = provenance{
			Kind:            provRaw.Str("kind"),
			NotOfficialData: provRaw.Bool("notOfficialData", "not_official_data"),
			Author:          provRaw.Str("author"),
			Basis:           provRaw.Str("basis"),
		}
		if err := obj.Provenance.valid(); err != nil {
			return nil, fmt.Errorf("trade: objective %q: %w", obj.ObjectiveID, err)
		}
		out = append(out, obj)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("trade: %q carries no usable objective", ObjectivesSection)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ObjectiveID < out[j].ObjectiveID })
	return out, nil
}

// instanceIDFor builds a unique instance id. Instances are physical cards, so the
// id carries the owner's seat and the slot to make a duplicate impossible to
// construct accidentally, while remaining stable across replays.
func instanceIDFor(subjectID string, slot int) string {
	sum := sha256.Sum256([]byte(subjectID + "\x00trade.instance\x00" + fmt.Sprintf("%d", slot)))
	return "ti-" + hex.EncodeToString(sum[:6])
}

// tradeIDFor builds a unique proposal id from the seed and the proposal ordinal.
func tradeIDFor(seed string, ordinal int) string {
	sum := sha256.Sum256([]byte(seed + "\x00trade.proposal\x00" + fmt.Sprintf("%d", ordinal)))
	return "tr-" + hex.EncodeToString(sum[:8])
}

// Prepare implements modes.Mode.
//
// The deal is seeded: every player receives four distinct instances and two secret
// objectives. The deal is verified so a round can never start with two players
// holding the same instance id, or with an objective whose kinds no card in the
// deck can satisfy.
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

	roster := make([]rosterEntry, 0, len(in.Subject))
	seen := make(map[string]struct{}, len(in.Subject))
	for _, subj := range in.Subject {
		if err := subj.Validate(); err != nil {
			return modes.Prepared{}, err
		}
		if _, dup := seen[subj.ID]; dup {
			return modes.Prepared{}, fmt.Errorf("trade: duplicate subject %q", subj.ID)
		}
		seen[subj.ID] = struct{}{}
		roster = append(roster, rosterEntry{SubjectID: subj.ID, SubjectKind: subj.Kind})
	}
	if len(roster) < MinSubjects || len(roster) > MaxSubjects {
		return modes.Prepared{}, fmt.Errorf(
			"trade: %d players outside [%d,%d]", len(roster), MinSubjects, MaxSubjects)
	}

	// Deal the hands. Each instance is created once and assigned to exactly one
	// hand, so uniqueness is structural rather than a later check.
	hands := make(map[string][]string, len(roster))
	// The catalogue and the objective set come from the pack, and only from the pack.
	// A missing section is a refusal, not a reason to fall back to built-in values:
	// a fallback would make "the compiler produced no content" look identical to "the
	// mode works", which is the exact failure this project has hit twice.
	cardKinds, err := loadCardKinds(in.Content)
	if err != nil {
		return modes.Prepared{}, err
	}
	objectivePool, err := loadObjectives(in.Content)
	if err != nil {
		return modes.Prepared{}, err
	}

	// The pool must be able to give every player two DISTINCT objectives. Reusing an
	// objective would give two players the identical goal, which forces them to either
	// cooperate or collide — a gameplay break, not a tolerable shortage. So the check is
	// a hard gate with the exact shortfall in the message, never a modulo wrap.
	needed := len(roster) * ObjectiveCount
	if len(objectivePool) < needed {
		return modes.Prepared{}, fmt.Errorf(
			"trade: %d objectives are needed (%d players × %d each) but %q carries %d; "+
				"publish at least %d distinct objectives",
			needed, len(roster), ObjectiveCount, ObjectivesSection, len(objectivePool), needed)
	}

	catalogue := make([]instance, 0, len(roster)*HandSize)
	allIDs := make(map[string]struct{}, len(roster)*HandSize)
	for i, entry := range roster {
		ids := make([]string, 0, HandSize)
		for slot := 0; slot < HandSize; slot++ {
			id := instanceIDFor(entry.SubjectID, slot)
			if _, dup := allIDs[id]; dup {
				return modes.Prepared{}, fmt.Errorf(
					"trade: instance id %q was generated twice", id)
			}
			allIDs[id] = struct{}{}
			// Rotate the kind by seat and slot so the deal is varied but never
			// depends on map iteration order.
			kind := cardKinds[(i*HandSize+slot)%len(cardKinds)]
			catalogue = append(catalogue, instance{
				InstanceID: id,
				CardID:     kind.Kind,
				Label:      kind.Label,
			})
			ids = append(ids, id)
		}
		sort.Strings(ids)
		hands[entry.SubjectID] = ids
	}

	// Deal the objectives: two distinct ones per player, drawn without replacement.
	//
	// The pool length was already checked against the total demand, so this loop can
	// never wrap: a wrap would silently hand the same objective to two players.
	objectives := make(map[string][]objective, len(roster))
	stream := modeutil.NewStream(in.Scope.Seed + "\x00trade.objectives")
	poolOrder := make([]int, len(objectivePool))
	for i := range poolOrder {
		poolOrder[i] = i
	}
	stream.Shuffle(len(poolOrder), func(i, j int) {
		poolOrder[i], poolOrder[j] = poolOrder[j], poolOrder[i]
	})
	kindCount := make(map[string]int, len(cardKinds))
	for _, inst := range catalogue {
		kindCount[inst.CardID]++
	}
	for _, obj := range objectivePool {
		if err := objectiveFeasible(obj, kindCount); err != nil {
			return modes.Prepared{}, fmt.Errorf("trade: objective %q: %w", obj.ObjectiveID, err)
		}
	}
	// Find disjoint objective pairs that can each fit a four-card hand. This
	// checks individual feasibility, not a promise that every player can win.
	usedObjectives := make(map[int]bool)
	pairs := make([][]objective, len(roster))
	attempts := 0
	var deal func(int) bool
	deal = func(seat int) bool {
		if seat == len(roster) {
			return true
		}
		for i, first := range poolOrder {
			if usedObjectives[first] {
				continue
			}
			for _, second := range poolOrder[i+1:] {
				if usedObjectives[second] {
					continue
				}
				attempts++
				if attempts > 100000 {
					return false
				}
				pair := []objective{objectivePool[first], objectivePool[second]}
				if !combinationFeasible(pair, kindCount) {
					continue
				}
				usedObjectives[first], usedObjectives[second] = true, true
				pairs[seat] = pair
				if deal(seat + 1) {
					return true
				}
				delete(usedObjectives, first)
				delete(usedObjectives, second)
			}
		}
		return false
	}
	if !deal(0) {
		return modes.Prepared{}, fmt.Errorf("trade: cannot deal feasible distinct objective combinations")
	}
	for i, entry := range roster {
		own := pairs[i]
		sort.Slice(own, func(a, b int) bool { return own[a].ObjectiveID < own[b].ObjectiveID })
		objectives[entry.SubjectID] = own
	}

	sort.Slice(catalogue, func(i, j int) bool {
		return catalogue[i].InstanceID < catalogue[j].InstanceID
	})

	pub := publicState{
		ModeID:       ModeID,
		RulesVersion: RulesVersion,
		Config:       cfg,
		Subjects:     roster,
		Hands:        hands,
		Instances:    catalogue,
		Trades:       []tradeRecord{},
		CurrentRound: 1,
		// The full objective catalogue is public: requires the scoring conditions
		// to be given at the start, so a player can plan. Which objectives a specific
		// player holds stays private.
		Objectives: objectivePool,
		CardKinds:  cardKinds,
		Scoring: "每个任务 " + fmt.Sprintf("%d", ObjectivePoints) +
			" 分；同一张卡不能同时满足多个任务；默认不进入正式杯",
	}
	priv := privateState{
		ModeID:        ModeID,
		Objectives:    objectives,
		ObjectivePool: objectivePool,
	}
	// Each player's own objectives are published as per-subject private state.
	//
	// A single round-level Private blob cannot carry this: it is written once and shared,
	// so projection would either hand every viewer the same (wrong) row or nothing at
	// all. Keying by subject lets the room layer store one row per player and project
	// only the caller's own.
	subjectPrivate := make(map[string]json.RawMessage, len(objectives))
	for subjectID, own := range objectives {
		subjectPrivate[subjectID] = modeutil.Marshal(subjectPrivateState{
			ModeID:     ModeID,
			Objectives: own,
			Hand:       hands[subjectID],
		})
	}
	return modes.Prepared{
		Public:         modeutil.Marshal(pub),
		Private:        modeutil.Marshal(priv),
		SubjectPrivate: subjectPrivate,
		// QuestionCount is the number of usable OBJECTIVES the pack provides, not the number of
		// players. The availability gate compares it against MinEntities, which is also a pool
		// floor, so reporting the table size made the two incomparable: an 8-player round
		// reported 8 while the pack held 16 objectives.
		QuestionCount: len(objectivePool),
	}, nil
}

// containsObjective reports whether list already holds an objective id.
func containsObjective(list []objective, objectiveID string) bool {
	for _, obj := range list {
		if obj.ObjectiveID == objectiveID {
			return true
		}
	}
	return false
}

// objectiveFeasible reports whether the deck can satisfy an objective at all.
func objectiveFeasible(obj objective, kindCount map[string]int) error {
	if len(obj.Kinds) == 0 {
		return fmt.Errorf("names no card kind")
	}
	seen := make(map[string]struct{}, len(obj.Kinds))
	for _, kind := range obj.Kinds {
		if _, dup := seen[kind]; dup {
			// A repeated kind would be satisfied by a single card, which is the
			// reuse the first version forbids.
			return fmt.Errorf("repeats the kind %q", kind)
		}
		seen[kind] = struct{}{}
		if kindCount[kind] == 0 {
			return fmt.Errorf("requires the kind %q which no card provides", kind)
		}
	}
	return nil
}

// proposePayload is the body of ActionKindPropose.
type proposePayload struct {
	Kind string `json:"kind"`
	// RecipientID is the counterparty.
	RecipientID string `json:"recipientId"`
	// OfferedInstanceID is the instance the sender gives away.
	OfferedInstanceID string `json:"offeredInstanceId"`
	// RequestedInstanceID is the instance the sender wants.
	RequestedInstanceID string `json:"requestedInstanceId"`
	// PhaseVersion is the round phase version the client saw.
	PhaseVersion int `json:"phaseVersion"`
}

// respondPayload is the body of ActionKindRespond.
type respondPayload struct {
	Kind    string `json:"kind"`
	TradeID string `json:"tradeId"`
	Accept  bool   `json:"accept"`
	// PhaseVersion is the round phase version the client saw.
	PhaseVersion int `json:"phaseVersion"`
}

// cancelPayload is the body of ActionKindCancel.
type cancelPayload struct {
	Kind    string `json:"kind"`
	TradeID string `json:"tradeId"`
}

// decodePublic unmarshals the public state.
func decodePublic(raw json.RawMessage) (publicState, error) {
	var pub publicState
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &pub); err != nil {
			return publicState{}, fmt.Errorf("trade: decode public state: %w", err)
		}
	}
	return pub, nil
}

// decodePrivate unmarshals the objectives.
func decodePrivate(raw json.RawMessage) (privateState, error) {
	var priv privateState
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &priv); err != nil {
			return privateState{}, fmt.Errorf("trade: decode private state: %w", err)
		}
	}
	return priv, nil
}

// decodeState unmarshals a participant state.
func decodeState(raw json.RawMessage) (subjectState, error) {
	var st subjectState
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &st); err != nil {
			return subjectState{}, fmt.Errorf("trade: decode participant state: %w", err)
		}
	}
	return st, nil
}

// Apply implements modes.Mode.
//
// Every branch validates against the round-level hands and proposal log, because
// those are the authoritative board. The caller's own state carries the single-open-
// proposal slot and their secret objectives.
func (*Mode) Apply(in modes.ApplyInput) (modes.Decision, error) {
	switch in.Action.Kind {
	case ActionKindPropose:
		return applyPropose(in)
	case ActionKindRespond:
		return applyRespond(in)
	case ActionKindCancel:
		return applyCancel(in)
	}
	return modes.Rejected(CodeUnknownAction,
		fmt.Sprintf("trade: unsupported action %q", in.Action.Kind)), nil
}

// applyPropose opens a proposal.
func applyPropose(in modes.ApplyInput) (modes.Decision, error) {
	if err := in.Action.Validate(); err != nil {
		return modes.Rejected(CodeInvalidPayload, err.Error()), nil
	}
	var payload proposePayload
	if err := json.Unmarshal(in.Action.Payload, &payload); err != nil {
		return modes.Rejected(CodeInvalidPayload,
			"trade: payload must carry recipientId, offeredInstanceId and requestedInstanceId"), nil
	}
	if payload.Kind != "" && payload.Kind != ActionKindPropose {
		return modes.Rejected(CodeInvalidPayload, fmt.Sprintf(
			"trade: payload kind %q does not match action %q",
			payload.Kind, ActionKindPropose)), nil
	}

	pub, err := decodePublic(in.Public)
	if err != nil {
		return modes.Decision{}, err
	}
	if !onRoster(pub.Subjects, in.Subject.ID) {
		return modes.Rejected(CodeNotOnRoster, fmt.Sprintf(
			"trade: subject %q is not on this roster", in.Subject.ID)), nil
	}
	if pub.CurrentRound > pub.Config.Rounds {
		return modes.Rejected(CodeRoundClosed, "trade: every trading round is over"), nil
	}
	if payload.RecipientID == "" || payload.OfferedInstanceID == "" || payload.RequestedInstanceID == "" {
		return modes.Rejected(CodeInvalidPayload,
			"trade: recipientId, offeredInstanceId and requestedInstanceId are required"), nil
	}
	if payload.RecipientID == in.Subject.ID {
		return modes.Rejected(CodeSelfTrade, "trade: you cannot trade with yourself"), nil
	}
	if !onRoster(pub.Subjects, payload.RecipientID) {
		return modes.Rejected(CodeNotOnRoster, fmt.Sprintf(
			"trade: %q is not on this roster", payload.RecipientID)), nil
	}
	if payload.OfferedInstanceID == payload.RequestedInstanceID {
		return modes.Rejected(CodeSameInstance,
			"trade: the offered and requested instances are the same card"), nil
	}

	st, err := decodeState(in.State)
	if err != nil {
		return modes.Decision{}, err
	}
	// One open proposal per player .
	if st.OpenTradeID != "" {
		if open, ok := findTrade(pub, st.OpenTradeID); ok && open.Status.open() {
			return modes.Rejected(CodeAlreadyOpen, fmt.Sprintf(
				"trade: you already have an open proposal (%s); cancel it first",
				st.OpenTradeID)), nil
		}
	}

	// The offered instance must be in the sender's hand, and the requested one in
	// the recipient's. Checking against the current hands is what makes a double
	// sale impossible: the second proposal is refused at creation, not at exchange.
	if !heldBy(pub, in.Subject.ID, payload.OfferedInstanceID) {
		return modes.Rejected(CodeInstanceGone, fmt.Sprintf(
			"trade: you do not hold the instance %q", payload.OfferedInstanceID)), nil
	}
	if !heldBy(pub, payload.RecipientID, payload.RequestedInstanceID) {
		return modes.Rejected(CodeInstanceGone, fmt.Sprintf(
			"trade: %q does not hold the instance %q",
			payload.RecipientID, payload.RequestedInstanceID)), nil
	}

	// A stale phase version means the client is proposing against a board that has
	// moved, so the proposal would be ambiguous; it is refused rather than guessed.
	if payload.PhaseVersion != 0 && payload.PhaseVersion < pub.CurrentRound {
		return modes.Rejected(CodeTradeClosed, fmt.Sprintf(
			"trade: the board moved to round %d; reload before proposing", pub.CurrentRound)), nil
	}

	tradeID := tradeIDFor(in.Scope.Seed, len(pub.Trades)+1)
	record := tradeRecord{
		TradeID:             tradeID,
		SenderID:            in.Subject.ID,
		RecipientID:         payload.RecipientID,
		OfferedInstanceID:   payload.OfferedInstanceID,
		RequestedInstanceID: payload.RequestedInstanceID,
		Status:              StatusPending,
		Round:               pub.CurrentRound,
		PhaseVersion:        pub.CurrentRound,
	}

	st.OpenTradeID = tradeID
	// The feedback names the proposal so the client can address it later, and it
	// repeats the single-open-proposal rule. The proposal itself is appended by
	// CommitShared, which is where the round-level log is folded.
	feedback := modeutil.Marshal(struct {
		TradeID     string      `json:"tradeId"`
		RecipientID string      `json:"recipientId"`
		Status      TradeStatus `json:"status"`
		Record      tradeRecord `json:"proposal"`
		Note        string      `json:"note"`
	}{
		TradeID:     tradeID,
		RecipientID: payload.RecipientID,
		Status:      StatusPending,
		Record:      record,
		Note:        "提案已发出；在你取消或对方回应之前，你不能发起新的提案",
	})
	return modes.Accepted(feedback, modeutil.Marshal(st)), nil
}

// applyRespond accepts or rejects a proposal addressed to the caller.
func applyRespond(in modes.ApplyInput) (modes.Decision, error) {
	if err := in.Action.Validate(); err != nil {
		return modes.Rejected(CodeInvalidPayload, err.Error()), nil
	}
	var payload respondPayload
	if err := json.Unmarshal(in.Action.Payload, &payload); err != nil {
		return modes.Rejected(CodeInvalidPayload,
			"trade: payload must carry tradeId and accept"), nil
	}
	if payload.Kind != "" && payload.Kind != ActionKindRespond {
		return modes.Rejected(CodeInvalidPayload, fmt.Sprintf(
			"trade: payload kind %q does not match action %q",
			payload.Kind, ActionKindRespond)), nil
	}
	if payload.TradeID == "" {
		return modes.Rejected(CodeInvalidPayload, "trade: empty tradeId"), nil
	}

	pub, err := decodePublic(in.Public)
	if err != nil {
		return modes.Decision{}, err
	}
	if !onRoster(pub.Subjects, in.Subject.ID) {
		return modes.Rejected(CodeNotOnRoster, fmt.Sprintf(
			"trade: subject %q is not on this roster", in.Subject.ID)), nil
	}
	record, ok := findTrade(pub, payload.TradeID)
	if !ok {
		return modes.Rejected(CodeTradeNotFound, fmt.Sprintf(
			"trade: proposal %q does not exist", payload.TradeID)), nil
	}
	// Only the recipient may respond. The sender cancels instead, which is a
	// different action with a different effect.
	if record.RecipientID != in.Subject.ID {
		return modes.Rejected(CodeNotYourTrade, fmt.Sprintf(
			"trade: proposal %q is addressed to %q, not to you",
			record.TradeID, record.RecipientID)), nil
	}
	// A proposal can only be answered once. A second acceptance would move the cards
	// a second time, which is the double-sale this mode exists to prevent.
	if !record.Status.open() {
		return modes.Rejected(CodeTradeClosed, fmt.Sprintf(
			"trade: proposal %q is already %s", record.TradeID, record.Status)), nil
	}
	if payload.Accept {
		// Re-validate the hands at commit time. Between the proposal and the
		// acceptance another trade may have moved the offered or requested instance,
		// so an acceptance against a stale board must fail rather than move a card
		// that is no longer there.
		if !heldBy(pub, record.SenderID, record.OfferedInstanceID) {
			return modes.Rejected(CodeInstanceGone, fmt.Sprintf(
				"trade: the sender no longer holds %q", record.OfferedInstanceID)), nil
		}
		if !heldBy(pub, record.RecipientID, record.RequestedInstanceID) {
			return modes.Rejected(CodeInstanceGone, fmt.Sprintf(
				"trade: you no longer hold %q", record.RequestedInstanceID)), nil
		}
	}

	st, err := decodeState(in.State)
	if err != nil {
		return modes.Decision{}, err
	}
	// The recipient's own incoming list is maintained so the client can show the
	// pending offers without scanning the whole log.
	st.IncomingTradeIDs = removeString(st.IncomingTradeIDs, record.TradeID)

	status := StatusRejected
	if payload.Accept {
		status = StatusAccepted
	}
	feedback := modeutil.Marshal(struct {
		TradeID string      `json:"tradeId"`
		Status  TradeStatus `json:"status"`
		Accept  bool        `json:"accept"`
		Note    string      `json:"note"`
	}{
		TradeID: record.TradeID,
		Status:  status,
		Accept:  payload.Accept,
		Note:    responseNote(status),
	})
	return modes.Accepted(feedback, modeutil.Marshal(st)), nil
}

// responseNote explains the outcome.
func responseNote(status TradeStatus) string {
	if status == StatusAccepted {
		return "交易已完成：两张卡一起换了手，不能再撤回"
	}
	return "提案已拒绝；对方可以重新发起"
}

// applyCancel withdraws the caller's own open proposal.
func applyCancel(in modes.ApplyInput) (modes.Decision, error) {
	if err := in.Action.Validate(); err != nil {
		return modes.Rejected(CodeInvalidPayload, err.Error()), nil
	}
	var payload cancelPayload
	if err := json.Unmarshal(in.Action.Payload, &payload); err != nil {
		return modes.Rejected(CodeInvalidPayload, "trade: payload must carry tradeId"), nil
	}
	if payload.Kind != "" && payload.Kind != ActionKindCancel {
		return modes.Rejected(CodeInvalidPayload, fmt.Sprintf(
			"trade: payload kind %q does not match action %q",
			payload.Kind, ActionKindCancel)), nil
	}

	pub, err := decodePublic(in.Public)
	if err != nil {
		return modes.Decision{}, err
	}
	record, ok := findTrade(pub, payload.TradeID)
	if !ok {
		return modes.Rejected(CodeTradeNotFound, fmt.Sprintf(
			"trade: proposal %q does not exist", payload.TradeID)), nil
	}
	if record.SenderID != in.Subject.ID {
		return modes.Rejected(CodeNotYourTrade, fmt.Sprintf(
			"trade: proposal %q was opened by %q, not by you",
			record.TradeID, record.SenderID)), nil
	}
	if !record.Status.open() {
		return modes.Rejected(CodeTradeClosed, fmt.Sprintf(
			"trade: proposal %q is already %s", record.TradeID, record.Status)), nil
	}

	st, err := decodeState(in.State)
	if err != nil {
		return modes.Decision{}, err
	}
	st.OpenTradeID = ""

	feedback := modeutil.Marshal(struct {
		TradeID string      `json:"tradeId"`
		Status  TradeStatus `json:"status"`
		Note    string      `json:"note"`
	}{
		TradeID: record.TradeID,
		Status:  StatusCancelled,
		Note:    "提案已取消；你可以发起新的提案",
	})
	return modes.Accepted(feedback, modeutil.Marshal(st)), nil
}

// findTrade looks a proposal up by id.
func findTrade(pub publicState, tradeID string) (tradeRecord, bool) {
	for _, record := range pub.Trades {
		if record.TradeID == tradeID {
			return record, true
		}
	}
	return tradeRecord{}, false
}

// onRoster reports whether subjectID participates.
func onRoster(roster []rosterEntry, subjectID string) bool {
	for _, entry := range roster {
		if entry.SubjectID == subjectID {
			return true
		}
	}
	return false
}

// heldBy reports whether subjectID currently holds instanceID.
func heldBy(pub publicState, subjectID, instanceID string) bool {
	for _, id := range pub.Hands[subjectID] {
		if id == instanceID {
			return true
		}
	}
	return false
}

// removeString returns list without value, preserving order.
func removeString(list []string, value string) []string {
	out := make([]string, 0, len(list))
	for _, item := range list {
		if item != value {
			out = append(out, item)
		}
	}
	return out
}

// CommitShared implements modeutil.SharedStateMode.
//
// It folds an accepted action into the round-level hands and proposal log. The fold
// is the atomic exchange: both instances move in this one step, and the function
// returns before anything is persisted, so a failure leaves neither card moved.
//
// Replay safety: the fold reads the pre-trade hands from in.Public and applies the
// decision it is given, so replaying the same accepted action yields byte-identical
// hands. A second fold of the same acceptance cannot happen because Apply refuses a
// proposal that is no longer pending, and the log records the status change in the
// same transaction.
func (*Mode) CommitShared(in modes.ApplyInput, dec modes.Decision) (json.RawMessage, json.RawMessage, error) {
	if !dec.Accepted {
		return nil, nil, fmt.Errorf("trade: cannot commit a rejected action")
	}
	pub, err := decodePublic(in.Public)
	if err != nil {
		return nil, nil, err
	}
	priv, err := decodePrivate(in.Private)
	if err != nil {
		return nil, nil, err
	}
	// The caller's own bookkeeping (open proposal, incoming list) is persisted from
	// dec.State by the room layer in the same transaction; the round-level fold
	// below only touches hands and the proposal log, so it does not need to read it.
	switch in.Action.Kind {
	case ActionKindPropose:
		var payload proposePayload
		if err := json.Unmarshal(in.Action.Payload, &payload); err != nil {
			return nil, nil, fmt.Errorf("trade: decode propose payload: %w", err)
		}
		// The id derives from the pre-action log length, which is the same value
		// Apply saw, so the id reported to the client is the id recorded here.
		record := tradeRecord{
			TradeID:             tradeIDFor(in.Scope.Seed, len(pub.Trades)+1),
			SenderID:            in.Subject.ID,
			RecipientID:         payload.RecipientID,
			OfferedInstanceID:   payload.OfferedInstanceID,
			RequestedInstanceID: payload.RequestedInstanceID,
			Status:              StatusPending,
			Round:               pub.CurrentRound,
			PhaseVersion:        pub.CurrentRound,
		}
		// The id must match the one Apply reported, or the client and the log would
		// disagree about which proposal exists.
		pub.Trades = append(pub.Trades, record)

	case ActionKindRespond:
		var payload respondPayload
		if err := json.Unmarshal(in.Action.Payload, &payload); err != nil {
			return nil, nil, fmt.Errorf("trade: decode respond payload: %w", err)
		}
		idx := tradeIndex(pub, payload.TradeID)
		if idx < 0 {
			return nil, nil, fmt.Errorf("trade: proposal %q vanished before commit", payload.TradeID)
		}
		record := pub.Trades[idx]
		if payload.Accept {
			// The atomic exchange. Both hands are rewritten in one step, and the
			// pre-trade hands are validated again here so a concurrent move cannot
			// produce a half-swap.
			if !heldBy(pub, record.SenderID, record.OfferedInstanceID) {
				return nil, nil, fmt.Errorf(
					"trade: sender no longer holds %q at commit time", record.OfferedInstanceID)
			}
			if !heldBy(pub, record.RecipientID, record.RequestedInstanceID) {
				return nil, nil, fmt.Errorf(
					"trade: recipient no longer holds %q at commit time", record.RequestedInstanceID)
			}
			pub.Hands[record.SenderID] = swapInstance(
				pub.Hands[record.SenderID], record.OfferedInstanceID, record.RequestedInstanceID)
			pub.Hands[record.RecipientID] = swapInstance(
				pub.Hands[record.RecipientID], record.RequestedInstanceID, record.OfferedInstanceID)
			record.Status = StatusAccepted
		} else {
			record.Status = StatusRejected
		}
		pub.Trades[idx] = record
		// The sender's open-proposal slot is freed by the status change itself: a
		// later proposal is accepted because Apply checks the recorded status of the
		// proposal named in the sender's state, not a boolean marker.

	case ActionKindCancel:
		var payload cancelPayload
		if err := json.Unmarshal(in.Action.Payload, &payload); err != nil {
			return nil, nil, fmt.Errorf("trade: decode cancel payload: %w", err)
		}
		idx := tradeIndex(pub, payload.TradeID)
		if idx < 0 {
			return nil, nil, fmt.Errorf("trade: proposal %q vanished before commit", payload.TradeID)
		}
		record := pub.Trades[idx]
		record.Status = StatusCancelled
		pub.Trades[idx] = record

	default:
		return nil, nil, fmt.Errorf("trade: cannot commit action %q", in.Action.Kind)
	}

	// Canonical order so the log is byte-stable regardless of commit order.
	sort.SliceStable(pub.Trades, func(i, j int) bool {
		return pub.Trades[i].TradeID < pub.Trades[j].TradeID
	})
	return modeutil.Marshal(pub), modeutil.Marshal(priv), nil
}

// tradeIndex returns the index of a proposal, or -1.
func tradeIndex(pub publicState, tradeID string) int {
	for i, record := range pub.Trades {
		if record.TradeID == tradeID {
			return i
		}
	}
	return -1
}

// swapInstance replaces outgoing with incoming in a hand and re-sorts it.
func swapInstance(hand []string, outgoing, incoming string) []string {
	out := make([]string, 0, len(hand))
	for _, id := range hand {
		if id == outgoing {
			out = append(out, incoming)
			continue
		}
		out = append(out, id)
	}
	sort.Strings(out)
	return out
}

// proposalView is one proposal as a viewer sees it.
type proposalView struct {
	TradeID             string      `json:"tradeId"`
	SenderID            string      `json:"senderId"`
	RecipientID         string      `json:"recipientId"`
	OfferedInstanceID   string      `json:"offeredInstanceId"`
	RequestedInstanceID string      `json:"requestedInstanceId"`
	Status              TradeStatus `json:"status"`
	Round               int         `json:"round"`
}

// objectiveView is one secret objective as its owner sees it.
type objectiveView struct {
	ObjectiveID string   `json:"objectiveId"`
	Text        string   `json:"text"`
	Kinds       []string `json:"kinds"`
	Points      int      `json:"points"`
	// Satisfied reports whether the caller's CURRENT hand satisfies it. It is
	// computed for the owner only, so it never reveals another player's status.
	Satisfied bool `json:"satisfied"`
}

// projection is the viewer-specific round state.
type projection struct {
	ModeID       string      `json:"modeId"`
	RulesVersion string      `json:"rulesVersion"`
	Phase        modes.Phase `json:"phase"`
	Config       Config      `json:"config"`
	CurrentRound int         `json:"currentRound"`
	Scoring      string      `json:"scoring"`
	// Hands is the public hand board: every player's current instance ids
	// .
	Hands map[string][]string `json:"hands"`
	// Instances is the public catalogue so a client can render labels.
	Instances []instance `json:"instances"`
	// Trades is the public proposal log.
	//
	// It is empty until somebody proposes, which is the normal opening state. TradesEmptyReason
	// says so, so a client can render "no proposals yet" instead of a silent empty list.
	Trades            []proposalView `json:"trades"`
	TradesEmptyReason string         `json:"tradesEmptyReason,omitempty"`
	// Objectives is the full objective catalogue, published from the start because
	// requires every scoring condition to be known before play. Which objective
	// a given player holds is NOT here; it is in selfObjectives for that player only.
	Objectives []objective `json:"objectives"`
	// CardKinds is the card-kind catalogue.
	CardKinds []cardKind `json:"cardKinds"`
	// SelfObjectives is the caller's OWN secret objectives. It is omitted for any
	// viewer without a seat.
	SelfObjectives []objectiveView `json:"selfObjectives,omitempty"`
	// Self is the caller's own bookkeeping.
	Self *selfView `json:"self,omitempty"`
	// Progress is the coarse per-player progress.
	Progress []progressView `json:"progress"`
	// Result is the final objective valuation, published once the match is over.
	Result *resultView `json:"result,omitempty"`
}

// selfView is the caller's own state.
type selfView struct {
	SubjectID   string   `json:"subjectId"`
	Hand        []string `json:"hand"`
	OpenTradeID string   `json:"openTradeId,omitempty"`
	IncomingIDs []string `json:"incomingTradeIds,omitempty"`
}

// progressView is another player's coarse progress: hand size and whether they have
// an open proposal. It never includes their objectives.
type progressView struct {
	SubjectID    string `json:"subjectId"`
	HandSize     int    `json:"handSize"`
	HasOpenTrade bool   `json:"hasOpenTrade"`
}

// resultView is the settled valuation.
type resultView struct {
	// Players is the per-player objective result, in roster order.
	Players []playerResult `json:"players"`
	// Objectives is the full objective catalogue with its text, so the reveal can
	// explain every objective rather than only the ones that mattered.
	Objectives []objective `json:"objectives"`
}

// playerResult is one player's objective outcome.
type playerResult struct {
	SubjectID string `json:"subjectId"`
	// Completed and Total count this player's objectives.
	Completed int `json:"completed"`
	Total     int `json:"total"`
	// Points is the display points earned, before the competitive gate.
	Points int `json:"points"`
	// SatisfiedIDs names the objectives the final hand satisfied.
	SatisfiedIDs []string `json:"satisfiedIds"`
	// UnsatisfiedIDs names the rest.
	UnsatisfiedIDs []string `json:"unsatisfiedIds"`
}

// Project implements modes.Mode.
//
// Visibility rules:
//
// - Hands are public: every player sees every hand, which the plan fixes for the
// first version.
// - Objectives are private: a viewer receives only their own objective list, and
// the field is omitted rather than blanked for a viewer without a seat.
// - The final valuation is published only once the match is over.
func (*Mode) Project(in modes.ProjectInput) (json.RawMessage, error) {
	pub, err := decodePublic(in.Public)
	if err != nil {
		return nil, err
	}
	priv, err := decodePrivate(in.Private)
	if err != nil {
		return nil, err
	}

	out := projection{
		ModeID:       ModeID,
		RulesVersion: RulesVersion,
		Phase:        in.Phase,
		Config:       pub.Config,
		CurrentRound: min(pub.CurrentRound, pub.Config.Rounds),
		Scoring:      pub.Scoring,
		Hands:        pub.Hands,
		Instances:    pub.Instances,
		Trades:       make([]proposalView, 0, len(pub.Trades)),
		Objectives:   pub.Objectives,
		CardKinds:    pub.CardKinds,
		Progress:     make([]progressView, 0, len(pub.Subjects)),
	}
	if out.Hands == nil {
		out.Hands = map[string][]string{}
	}
	for _, record := range pub.Trades {
		out.Trades = append(out.Trades, proposalView{
			TradeID:             record.TradeID,
			SenderID:            record.SenderID,
			RecipientID:         record.RecipientID,
			OfferedInstanceID:   record.OfferedInstanceID,
			RequestedInstanceID: record.RequestedInstanceID,
			Status:              record.Status,
			Round:               record.Round,
		})
	}

	if len(out.Trades) == 0 {
		out.TradesEmptyReason = "本局还没有任何提案；发起提案后会显示在这里"
	}
	// Iterate the roster order recorded at prepare time, never the map, so the
	// projection is byte-identical across runs .
	for _, entry := range pub.Subjects {
		st, err := decodeState(in.States[entry.SubjectID])
		if err != nil {
			return nil, fmt.Errorf("trade: decode state of %q: %w", entry.SubjectID, err)
		}
		open := false
		if st.OpenTradeID != "" {
			if record, ok := findTrade(pub, st.OpenTradeID); ok && record.Status.open() {
				open = true
			}
		}
		out.Progress = append(out.Progress, progressView{
			SubjectID:    entry.SubjectID,
			HandSize:     len(pub.Hands[entry.SubjectID]),
			HasOpenTrade: open,
		})
		if entry.SubjectID == in.Viewer.Subject.ID {
			// Only the owner receives their objectives, and the satisfaction flag is
			// computed from their own hand so it cannot leak anybody else's status.
			// The objectives come from the private round state, which is the
			// authoritative copy. The participant state carries a duplicate so a
			// projection can serve the owner without reading the round secrets, but
			// it may legitimately be empty before the room layer writes it.
			own := priv.Objectives[entry.SubjectID]
			if len(own) == 0 {
				own = st.Objectives
			}
			views := make([]objectiveView, 0, len(own))
			hand := pub.Hands[entry.SubjectID]
			satisfied := satisfiedObjectives(pub, hand, own)
			for _, obj := range own {
				views = append(views, objectiveView{
					ObjectiveID: obj.ObjectiveID,
					Text:        obj.Text,
					Kinds:       obj.Kinds,
					Points:      obj.Points,
					Satisfied:   satisfied[obj.ObjectiveID],
				})
			}
			out.SelfObjectives = views
			incoming := st.IncomingTradeIDs
			if incoming == nil {
				incoming = []string{}
			}
			out.Self = &selfView{
				SubjectID:   entry.SubjectID,
				Hand:        append([]string{}, hand...),
				OpenTradeID: st.OpenTradeID,
				IncomingIDs: incoming,
			}
		}
	}

	if pub.CurrentRound > pub.Config.Rounds && (in.Phase == modes.PhaseRevealed || in.Phase == modes.PhaseSettled) {
		out.Result = &resultView{
			Players:    valuate(pub, priv, in.States),
			Objectives: priv.ObjectivePool,
		}
	}
	return modeutil.Marshal(out), nil
}

// Select the highest-value satisfiable subset without reusing an instance.
// There are exactly two objectives; ties prefer their stable ID order. The same
// allocation is used by the owner's preview and final valuation.
func satisfiedObjectives(pub publicState, hand []string, objectives []objective) map[string]bool {
	ordered := append([]objective(nil), objectives...)
	sort.Slice(ordered, func(i, j int) bool { return ordered[i].ObjectiveID < ordered[j].ObjectiveID })
	counts := make(map[string]int)
	kindOf := make(map[string]string)
	for _, inst := range pub.Instances {
		kindOf[inst.InstanceID] = inst.CardID
	}
	seen := make(map[string]bool)
	for _, id := range hand {
		if !seen[id] && kindOf[id] != "" {
			counts[kindOf[id]]++
			seen[id] = true
		}
	}
	best := make(map[string]bool)
	bestPoints, bestCount := -1, -1
	var visit func(int, int, map[string]bool)
	visit = func(index, points int, chosen map[string]bool) {
		if index == len(ordered) {
			if points > bestPoints || points == bestPoints && len(chosen) > bestCount {
				bestPoints, bestCount = points, len(chosen)
				best = make(map[string]bool, len(chosen))
				for id := range chosen {
					best[id] = true
				}
			}
			return
		}
		obj := ordered[index]
		need := make(map[string]int)
		for _, kind := range obj.Kinds {
			need[kind]++
		}
		can := len(need) > 0
		for kind, n := range need {
			if counts[kind] < n {
				can = false
			}
		}
		if can {
			for kind, n := range need {
				counts[kind] -= n
			}
			chosen[obj.ObjectiveID] = true
			visit(index+1, points+obj.Points, chosen)
			delete(chosen, obj.ObjectiveID)
			for kind, n := range need {
				counts[kind] += n
			}
		}
		visit(index+1, points, chosen)
	}
	visit(0, 0, make(map[string]bool))
	return best
}

// combinationFeasible checks all requirements together without reusing cards.
func combinationFeasible(objectives []objective, counts map[string]int) bool {
	need := make(map[string]int)
	total := 0
	for _, obj := range objectives {
		for _, kind := range obj.Kinds {
			need[kind]++
			total++
		}
	}
	if total > HandSize {
		return false
	}
	for kind, n := range need {
		if counts[kind] < n {
			return false
		}
	}
	return true
}

// valuate computes every player's objective outcome from the final hands.
func valuate(pub publicState, priv privateState, states map[string]json.RawMessage) []playerResult {
	out := make([]playerResult, 0, len(pub.Subjects))
	for _, entry := range pub.Subjects {
		// The objectives come from the private round state, which is the
		// authoritative copy; the participant state carries a duplicate for the
		// owner's projection only.
		objectives := priv.Objectives[entry.SubjectID]
		if len(objectives) == 0 {
			st, err := decodeState(states[entry.SubjectID])
			if err == nil {
				objectives = st.Objectives
			}
		}
		hand := pub.Hands[entry.SubjectID]
		satisfiedSet := satisfiedObjectives(pub, hand, objectives)
		satisfied := make([]string, 0, len(objectives))
		unsatisfied := make([]string, 0, len(objectives))
		points := 0
		for _, obj := range objectives {
			if satisfiedSet[obj.ObjectiveID] {
				satisfied = append(satisfied, obj.ObjectiveID)
				points += obj.Points
				continue
			}
			unsatisfied = append(unsatisfied, obj.ObjectiveID)
		}
		sort.Strings(satisfied)
		sort.Strings(unsatisfied)
		out = append(out, playerResult{
			SubjectID:      entry.SubjectID,
			Completed:      len(satisfied),
			Total:          len(objectives),
			Points:         points,
			SatisfiedIDs:   satisfied,
			UnsatisfiedIDs: unsatisfied,
		})
	}
	return out
}

// Settle implements modes.Mode.
//
// the mode does not enter the formal cup by default, so every score is 0
// unless the round explicitly opts in with Config.Competitive. When it does opt in,
// the score is the objective points capped at the published maximum, and the
// explanation states the objective outcome so the result is explainable.
func (*Mode) Settle(in modes.SettleInput) ([]modes.Score, error) {
	pub, err := decodePublic(in.Public)
	if err != nil {
		return nil, err
	}
	if in.Phase == modes.PhaseCancelled || pub.CurrentRound <= pub.Config.Rounds {
		return nil, fmt.Errorf("trade: cannot settle cancelled or unfinished negotiation windows")
	}
	priv, err := decodePrivate(in.Private)
	if err != nil {
		return nil, err
	}
	results := valuate(pub, priv, in.States)
	bySubject := make(map[string]playerResult, len(results))
	for _, r := range results {
		bySubject[r.SubjectID] = r
	}

	// The maximum is the objective total: two objectives at the documented value.
	maxPoints := ObjectiveCount * ObjectivePoints
	scores := make([]modes.Score, 0, len(pub.Subjects))
	for _, entry := range pub.Subjects {
		r := bySubject[entry.SubjectID]
		units := 0
		if pub.Config.Competitive {
			units = r.Points * modeutil.UnitsPerPoint
			if units > maxPoints*modeutil.UnitsPerPoint {
				units = maxPoints * modeutil.UnitsPerPoint
			}
		}
		explanation := modeutil.Marshal(struct {
			Completed      int      `json:"completed"`
			Total          int      `json:"total"`
			Points         int      `json:"points"`
			SatisfiedIDs   []string `json:"satisfiedIds"`
			UnsatisfiedIDs []string `json:"unsatisfiedIds"`
			Hand           []string `json:"hand"`
			Competitive    bool     `json:"competitive"`
			Note           string   `json:"note"`
		}{
			Completed:      r.Completed,
			Total:          r.Total,
			Points:         r.Points,
			SatisfiedIDs:   nonNil(r.SatisfiedIDs),
			UnsatisfiedIDs: nonNil(r.UnsatisfiedIDs),
			Hand:           nonNil(pub.Hands[entry.SubjectID]),
			Competitive:    pub.Config.Competitive,
			Note:           settleNote(pub.Config),
		})
		score := modes.Score{
			Subject:       modes.Subject{ID: entry.SubjectID, Kind: entry.SubjectKind},
			ScoreUnits:    units,
			MaxScoreUnits: modeutil.Units(maxPoints),
			Explanation:   explanation,
		}
		if err := score.Validate(); err != nil {
			return nil, err
		}
		scores = append(scores, score)
	}
	return modes.RankScores(scores), nil
}

// settleNote explains the competitive gate.
func settleNote(cfg Config) string {
	if cfg.Competitive {
		return "本局已显式开启计分；任务分数按最终持卡判定"
	}
	return "交易会默认不进入正式杯，因此本局不计竞技分，仅公布任务完成情况"
}

// nonNil returns an empty slice for nil so the JSON carries [] rather than null.
func nonNil(in []string) []string {
	if in == nil {
		return []string{}
	}
	return in
}
