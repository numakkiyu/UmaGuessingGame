// Package memory implements G07 学园记忆翻牌 .
//
// A 4×4 board holds eight pairs. After an 8-second viewing phase the cards are
// hidden; the player then flips two at a time, up to twenty pairing attempts. A
// match stays face up, a miss is shown briefly and covered again. Scoring is
// max(0, floor(100×matchedPairs/8) - 2×wrongAttempts).
//
// Card faces are opaque: the public payload never contains the hidden pairing
// map, and the mapping is confirmed by the server, never by the client.
package memory

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"path"
	"sort"
	"strconv"
	"time"

	"github.com/bhcn/umaguessinggame/internal/modes"
	"github.com/bhcn/umaguessinggame/internal/modes/modeutil"
)

// ModeID is the stable mode identifier.
const ModeID = "memory"

// RulesVersion is the rule revision this implementation satisfies.
const RulesVersion = "2.0.0-draft.1"

// Board and timing parameters .
const (
	// Rows and Cols define the 4×4 board.
	Rows = 4
	Cols = 4
	// CardCount is the total number of cards.
	CardCount = Rows * Cols
	// PairCount is the number of pairs on the board.
	PairCount = CardCount / 2
	// ViewSeconds is the initial full-board viewing window.
	ViewSeconds = 8
	// AnswerSeconds is the answering window.
	AnswerSeconds = 90
	// MaxAttempts is the maximum number of pairing attempts.
	MaxAttempts = 20
	// WrongPenalty is subtracted per wrong attempt, in display points.
	WrongPenalty = 2
)

// Action kinds.
//
// The value is the contract discriminator (ModeAction → MemoryFlip), not an
// internal name .
const (
	// ActionFlip reveals one card. The server confirms it; the client never
	// decides whether two cards match .
	ActionFlip = "memory.flip"
)

// ContractKinds returns the ModeAction discriminators this mode accepts.
func (*Mode) ContractKinds() []string { return []string{ActionFlip} }

// Rejection codes.
const (
	CodeInvalidPayload = "invalid_payload"
	CodeUnknownAction  = "unknown_action"
	CodeUnknownCard    = "unknown_card"
	CodeAlreadyMatched = "already_matched"
	CodeAlreadyFlipped = "already_flipped"
	CodeTooManyFlipped = "too_many_flipped"
	CodeOutOfAttempts  = "out_of_attempts"
	CodeViewingPhase   = "viewing_phase"
)

// Config is the round configuration. The documented values are fixed for the
// first version; the fields exist so a later rule version can change them
// explicitly rather than silently.
type Config struct {
	ViewSeconds   int `json:"viewSeconds"`
	AnswerSeconds int `json:"answerSeconds"`
	MaxAttempts   int `json:"maxAttempts"`
}

// decodeConfig normalises the configuration.
func decodeConfig(raw json.RawMessage) (Config, error) {
	cfg := Config{
		ViewSeconds:   ViewSeconds,
		AnswerSeconds: AnswerSeconds,
		MaxAttempts:   MaxAttempts,
	}
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &cfg); err != nil {
			return Config{}, fmt.Errorf("memory: invalid config: %w", err)
		}
	}
	if cfg.ViewSeconds == 0 {
		cfg.ViewSeconds = ViewSeconds
	}
	if cfg.AnswerSeconds == 0 {
		cfg.AnswerSeconds = AnswerSeconds
	}
	if cfg.MaxAttempts == 0 {
		cfg.MaxAttempts = MaxAttempts
	}
	if cfg.ViewSeconds < 0 || cfg.AnswerSeconds < 0 {
		return Config{}, fmt.Errorf("memory: negative timing in config")
	}
	if cfg.MaxAttempts < 1 {
		return Config{}, fmt.Errorf("memory: maxAttempts %d must be at least 1", cfg.MaxAttempts)
	}
	return cfg, nil
}

// boardPair is one usable pair: an identity, the two images its two cards show,
// and an optional label for the reveal.
//
// A pair is either self-describing (the pack publishes one image per pair, and
// both cards show that image) or split (the pack publishes individual faces and
// the private section says which two faces pair up). Supporting both keeps the
// mode working across pack revisions without weakening the hidden-mapping rule:
// in either shape the pairing a round actually uses is decided server-side.
type boardPair struct {
	PairID string
	// Images holds the two face images, one per card.
	Images [2]string
	// Label is the reviewed display label, used only on reveal.
	Label string
}

// PrivateSolutionSection is the private section carrying the pairing material.
const PrivateSolutionSection = "memory.solution"

// privateSolution is the private section payload. It carries the eligible pair
// ids and, in the split shape, the face mapping.
type privateSolution struct {
	Mode string `json:"mode"`
	// PairIDs is the eligible pair set when the public section is self-describing.
	PairIDs []string `json:"pairIds,omitempty"`
	// Pairs is the face mapping when the public section publishes individual
	// faces.
	Pairs []struct {
		PairID string   `json:"pairId"`
		Faces  []string `json:"faces"`
	} `json:"pairs,omitempty"`
	// PairLabels maps pair id → the reviewed display name. The public section
	// publishes an opaque placeholder instead, so that a player cannot reverse the
	// whole pack into an image → name table and skip the memory task (:
	// 牌面 ID 是 opaque). The real name is only used on reveal.
	PairLabels map[string]string `json:"pairLabels,omitempty"`
	// SeedNamespace is mixed into the round seed to derive the board.
	SeedNamespace string `json:"seedNamespace,omitempty"`
}

// loadBoardPairs reads the public section and, when needed, the private mapping,
// returning the pairs a round may draw from.
func loadBoardPairs(src modes.ContentSource) ([]boardPair, error) {
	raw, err := modeutil.Section(src, "memory.pairs", "memory")
	if err != nil {
		return nil, err
	}
	records, err := modeutil.Records(raw, "pairs", "faces", "items")
	if err != nil {
		return nil, err
	}

	// The private section is optional for the self-describing shape and required
	// for the split shape.
	var solution privateSolution
	if rawPriv, errPriv := modeutil.PrivateSection(src, PrivateSolutionSection); errPriv == nil {
		if err := json.Unmarshal(rawPriv, &solution); err != nil {
			return nil, fmt.Errorf("memory: decode private solution: %w", err)
		}
	}

	// Classify the public records: split shape publishes faces, self-describing
	// shape publishes pairs.
	faceImage := make(map[string]string, len(records))
	type pairRecord struct {
		id    string
		image string
		label string
	}
	pairRecords := make([]pairRecord, 0, len(records))
	for _, rec := range records {
		if faceID := rec.Str("faceId", "face_id"); faceID != "" {
			if image := rec.Str("imagePath", "image_path"); image != "" {
				faceImage[faceID] = image
			}
			continue
		}
		id := rec.Str("pairId", "pair_id", "id")
		if id == "" {
			continue
		}
		pairRecords = append(pairRecords, pairRecord{
			id:    id,
			image: rec.Str("imagePath", "image_path", "image_local_path"),
			label: rec.Str("label", "name"),
		})
	}

	out := make([]boardPair, 0, len(pairRecords)+len(solution.Pairs))

	// Split shape: pair membership comes from the private section.
	if len(solution.Pairs) > 0 {
		for _, mapped := range solution.Pairs {
			if mapped.PairID == "" || len(mapped.Faces) != 2 {
				continue
			}
			a, b := faceImage[mapped.Faces[0]], faceImage[mapped.Faces[1]]
			if a == "" || b == "" {
				continue
			}
			out = append(out, boardPair{
				PairID: mapped.PairID,
				Images: [2]string{cardFaceRoute(mapped.PairID, a, 0), cardFaceRoute(mapped.PairID, b, 1)},
				Label:  solution.PairLabels[mapped.PairID],
			})
		}
		sort.Slice(out, func(i, j int) bool { return out[i].PairID < out[j].PairID })
		return out, nil
	}

	// Self-describing shape: each public record is a pair, and both of its cards
	// show the same reviewed image. When the private section lists eligible ids,
	// only those are drawn.
	eligible := make(map[string]struct{}, len(solution.PairIDs))
	for _, id := range solution.PairIDs {
		eligible[id] = struct{}{}
	}
	for _, rec := range pairRecords {
		if rec.image == "" {
			continue
		}
		if len(eligible) > 0 {
			if _, ok := eligible[rec.id]; !ok {
				continue
			}
		}
		// The public label is an opaque placeholder; the readable name only comes
		// from the private map, and only for the reveal.
		label := rec.label
		if real, ok := solution.PairLabels[rec.id]; ok && real != "" {
			label = real
		}
		out = append(out, boardPair{
			PairID: rec.id,
			Images: [2]string{cardFaceRoute(rec.id, rec.image, 0), cardFaceRoute(rec.id, rec.image, 1)},
			Label:  label,
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].PairID < out[j].PairID })
	return out, nil
}

// Card is one board position. CardID is opaque and deliberately unrelated to the
// pair: the id must not encode the pairing .
type Card struct {
	CardID   string `json:"cardId"`
	Position int    `json:"position"`
	// ImagePath is only populated when the card is visible to this viewer.
	ImagePath string `json:"imagePath,omitempty"`
	// Label is only populated when the card is visible.
	Label string `json:"label,omitempty"`
}

// publicPrepared is the non-secret prepared state: the card ids and positions
// only. No pairing information is present.
type publicPrepared struct {
	ModeID       string        `json:"modeId"`
	RulesVersion string        `json:"rulesVersion"`
	Config       Config        `json:"config"`
	Rows         int           `json:"rows"`
	Cols         int           `json:"cols"`
	Cards        []Card        `json:"cards"`
	Subjects     []rosterEntry `json:"subjects"`
}

// rosterEntry is one participant.
type rosterEntry struct {
	SubjectID   string            `json:"subjectId"`
	SubjectKind modes.SubjectKind `json:"subjectKind"`
}

// privatePrepared carries the hidden pairing map, which never leaves the server
// until reveal.
type privatePrepared struct {
	// PairOf maps opaque card id → pair id.
	PairOf map[string]string `json:"pairOf"`
	// CardImage maps opaque card id → the face image that card shows.
	CardImage map[string]string `json:"cardImage"`
	// PairLabel maps pair id → the reviewed label, used only on reveal.
	PairLabel map[string]string `json:"pairLabel"`
}

// state is one subject's participant state.
type state struct {
	// Flipped holds the card ids currently face up but not yet resolved, at most
	// two.
	Flipped []string `json:"flipped"`
	// LastFlipped keeps only a completed mismatch, visible until RevealUntil.
	LastFlipped []string `json:"lastFlipped,omitempty"`
	RevealUntil int64    `json:"revealUntil,omitempty"`
	// Matched lists the card ids already paired, in discovery order.
	Matched []string `json:"matched"`
	// Attempts is how many pairing attempts were started.
	Attempts int `json:"attempts"`
	// WrongAttempts is how many attempts failed to match.
	WrongAttempts int `json:"wrongAttempts"`
	// ViewingDone reports that the initial viewing phase finished.
	ViewingDone bool `json:"viewingDone"`
	// Finished reports that the board is over for this subject.
	Finished bool `json:"finished"`
	// LastRevealed is the pair id of the last resolved attempt, exposed to the
	// subject so the UI can show the result of the flip.
	LastRevealed string `json:"lastRevealed,omitempty"`
	// LastMatch reports whether the last resolved attempt matched.
	LastMatch bool `json:"lastMatch,omitempty"`
}

// matchedPairs returns how many pairs are complete.
func (s state) matchedPairs() int { return len(s.Matched) / 2 }

// Mode implements modes.Mode for G07.
type Mode struct{}

// New returns the memory mode.
func New() *Mode { return &Mode{} }

// ID implements modes.Mode.
func (*Mode) ID() string { return ModeID }

// RulesVersion implements modes.Mode.
func (*Mode) RulesVersion() string { return RulesVersion }

// Title implements modes.Mode.
func (*Mode) Title() string { return "学园记忆翻牌" }

// Summary implements modes.Mode.
func (*Mode) Summary() string {
	return "观看 8 秒后盖牌，在最多 20 次尝试内把 4×4 牌面上的八对配对完成。"
}

// InformationStyles reports which information forms this mode implements.
func (*Mode) InformationStyles() []string {
	return []string{"closed"}
}

// RoundWindow implements modes.Mode.
//
// 看板 + 作答两段。返回 0 表示本模式没有自己的窗口意见，交给房间层默认值。
func (*Mode) RoundWindow(raw json.RawMessage) time.Duration {
	cfg, err := decodeConfig(raw)
	if err != nil {
		return 0
	}
	return time.Duration(cfg.ViewSeconds+cfg.AnswerSeconds) * time.Second
}

func (*Mode) PreviewWindow(raw json.RawMessage) time.Duration {
	cfg, err := decodeConfig(raw)
	if err != nil {
		return 0
	}
	return time.Duration(cfg.ViewSeconds) * time.Second
}

// TurnWindow implements modes.Mode. This mode is not turn-based: it runs on the
// single RoundWindow deadline, so there is no per-turn clock to report.
func (*Mode) TurnWindow(json.RawMessage) time.Duration { return 0 }

// MinSubjects implements modes.Mode.
func (*Mode) MinSubjects() int { return 1 }

// MaxSubjects implements modes.Mode.
func (*Mode) MaxSubjects() int { return 8 }

// Requirement implements modes.Mode .
func (*Mode) Requirement() modes.ContentRequirement {
	return modes.ContentRequirement{
		Fields:      []string{"pairId", "imagePath"},
		MinEntities: PairCount,
		NeedsAssets: true,
	}
}

// ValidateConfig implements modes.Mode.
func (*Mode) ValidateConfig(cfg json.RawMessage) error {
	_, err := decodeConfig(cfg)
	return err
}

// Prepare implements modes.Mode.
//
// It draws eight pairs from the pack and lays them out deterministically from the
// seed. Card ids are opaque tokens derived from the seed, never from the pair id,
// so the public payload cannot be decoded into the pairing.
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
	usable, err := loadBoardPairs(in.Content)
	if err != nil {
		return modes.Prepared{}, err
	}
	if len(usable) < PairCount {
		return modes.Prepared{}, fmt.Errorf(
			"memory: pack has %d usable pairs, need %d", len(usable), PairCount,
		)
	}

	// Draw eight pairs deterministically.
	indices := make([]int, len(usable))
	for i := range indices {
		indices[i] = i
	}
	stream := modeutil.NewStream(in.Scope.Seed + "\x00memory.draw")
	stream.Shuffle(len(indices), func(i, j int) { indices[i], indices[j] = indices[j], indices[i] })

	chosen := make([]boardPair, 0, PairCount)
	for i := 0; i < PairCount; i++ {
		chosen = append(chosen, usable[indices[i]])
	}
	sort.Slice(chosen, func(i, j int) bool { return chosen[i].PairID < chosen[j].PairID })

	// Each pair contributes two cards, one per face, and each card carries its
	// own face image: two faces of a pair are different pictures of the same
	// reviewed subject, never the same file twice .
	pairOf := make(map[string]string, CardCount)
	cardImage := make(map[string]string, CardCount)
	pairLabel := make(map[string]string, PairCount)
	slots := make([]string, 0, CardCount)
	for _, pair := range chosen {
		pairLabel[pair.PairID] = pair.Label
		for copyIndex := 0; copyIndex < 2; copyIndex++ {
			cardID := opaqueCardID(in.Scope.Seed, pair.PairID, copyIndex)
			pairOf[cardID] = pair.PairID
			// Each card gets its OWN image URL, even though both cards of a pair
			// show the same reviewed picture.
			//
			// This is a security requirement, not cosmetics: if both cards of a
			// pair returned the same URL, a player could flip one card, note the
			// URL, and identify its partner without remembering anything — the
			// memory mode would be defeated by reading the network tab. The URL
			// therefore carries a per-card token while the server resolves every
			// token of a pair back to one stored file, so the storage cost stays
			// at one image per pair .
			cardImage[cardID] = pair.Images[copyIndex]
			slots = append(slots, cardID)
		}
	}
	// Shuffle the slots so the board layout is not pair-adjacent.
	layout := modeutil.NewStream(in.Scope.Seed + "\x00memory.layout")
	layout.Shuffle(len(slots), func(i, j int) { slots[i], slots[j] = slots[j], slots[i] })

	cards := make([]Card, 0, CardCount)
	for position, cardID := range slots {
		cards = append(cards, Card{CardID: cardID, Position: position})
	}

	roster := make([]rosterEntry, 0, len(in.Subject))
	seen := make(map[string]struct{}, len(in.Subject))
	for _, subj := range in.Subject {
		if err := subj.Validate(); err != nil {
			return modes.Prepared{}, err
		}
		if _, dup := seen[subj.ID]; dup {
			return modes.Prepared{}, fmt.Errorf("memory: duplicate subject %q", subj.ID)
		}
		seen[subj.ID] = struct{}{}
		roster = append(roster, rosterEntry{SubjectID: subj.ID, SubjectKind: subj.Kind})
	}
	if len(roster) < 1 || len(roster) > 8 {
		return modes.Prepared{}, fmt.Errorf("memory: %d subjects outside [1,8]", len(roster))
	}

	pub := publicPrepared{
		ModeID:       ModeID,
		RulesVersion: RulesVersion,
		Config:       cfg,
		Rows:         Rows,
		Cols:         Cols,
		Cards:        cards,
		Subjects:     roster,
	}
	priv := privatePrepared{
		PairOf:    pairOf,
		CardImage: cardImage,
		PairLabel: pairLabel,
	}
	return modes.Prepared{
		Public:        modeutil.Marshal(pub),
		Private:       modeutil.Marshal(priv),
		QuestionCount: PairCount,
	}, nil
}

// opaqueCardID derives a stable, opaque card id. It is a hex digest of the seed,
// the pair id and the copy index, so it is deterministic but carries no readable
// structure.
func opaqueCardID(seed, pairID string, copy int) string {
	// Use the deterministic stream as a digest source; taking the first 64 bits
	// of a salted stream is enough for an opaque token and stays dependency-free.
	stream := modeutil.NewStream(
		"memory.card\x00" + seed + "\x00" + pairID + "\x00" + fmt.Sprint(copy),
	)
	v := stream.Next()
	return fmt.Sprintf("c%016x", v)
}

// flipPayload is the body of ActionFlip, matching the contract's MemoryFlip
// schema: {kind, cardId}.
type flipPayload struct {
	Kind   string `json:"kind"`
	CardID string `json:"cardId"`
}

// Apply implements modes.Mode.
func (*Mode) Apply(in modes.ApplyInput) (modes.Decision, error) {
	if in.Action.Kind != ActionFlip {
		return modes.Rejected(CodeUnknownAction,
			fmt.Sprintf("memory: unsupported action %q", in.Action.Kind)), nil
	}
	if err := in.Action.Validate(); err != nil {
		return modes.Rejected(CodeInvalidPayload, err.Error()), nil
	}
	var payload flipPayload
	if err := json.Unmarshal(in.Action.Payload, &payload); err != nil {
		return modes.Rejected(CodeInvalidPayload,
			"memory: flip payload must carry cardId"), nil
	}
	if payload.Kind != "" && payload.Kind != ActionFlip {
		return modes.Rejected(CodeInvalidPayload, fmt.Sprintf(
			"memory: payload kind %q does not match action %q", payload.Kind, ActionFlip,
		)), nil
	}
	if payload.CardID == "" {
		return modes.Rejected(CodeInvalidPayload, "memory: empty cardId"), nil
	}

	var pub publicPrepared
	if len(in.Public) > 0 {
		if err := json.Unmarshal(in.Public, &pub); err != nil {
			return modes.Decision{}, fmt.Errorf("memory: decode public state: %w", err)
		}
	}
	var priv privatePrepared
	if len(in.Private) > 0 {
		if err := json.Unmarshal(in.Private, &priv); err != nil {
			return modes.Decision{}, fmt.Errorf("memory: decode private state: %w", err)
		}
	}
	var st state
	if len(in.State) > 0 {
		if err := json.Unmarshal(in.State, &st); err != nil {
			return modes.Decision{}, fmt.Errorf("memory: decode subject state: %w", err)
		}
	}
	if st.Finished {
		return modes.Rejected(CodeOutOfAttempts, "memory: this board is already finished for you"), nil
	}
	if st.RevealUntil > in.ReceivedAt {
		return modes.Rejected(CodeTooManyFlipped, "memory: wait for the two cards to turn back"), nil
	}

	// The card must exist on the board.
	if _, known := priv.PairOf[payload.CardID]; !known {
		return modes.Rejected(CodeUnknownCard,
			fmt.Sprintf("memory: card %q is not on this board", payload.CardID)), nil
	}
	// Flipping a matched card is refused: it is permanently face up and cannot be
	// part of another attempt .
	for _, matched := range st.Matched {
		if matched == payload.CardID {
			return modes.Rejected(CodeAlreadyMatched,
				fmt.Sprintf("memory: card %q is already matched", payload.CardID)), nil
		}
	}
	// Flipping the same card twice in one attempt is refused .
	for _, flipped := range st.Flipped {
		if flipped == payload.CardID {
			return modes.Rejected(CodeAlreadyFlipped,
				fmt.Sprintf("memory: card %q is already face up in this attempt", payload.CardID)), nil
		}
	}
	if len(st.Flipped) >= 2 {
		// Two cards are already face up and unresolved; the third flip must wait
		// for the resolution .
		return modes.Rejected(CodeTooManyFlipped,
			"memory: two cards are already face up, resolve them first"), nil
	}
	if st.Attempts >= pub.Config.MaxAttempts && len(st.Flipped) == 0 {
		return modes.Rejected(CodeOutOfAttempts, fmt.Sprintf(
			"memory: all %d attempts are used", pub.Config.MaxAttempts,
		)), nil
	}

	// Accept the flip.
	st.LastFlipped = nil
	st.RevealUntil = 0
	st.Flipped = append(st.Flipped, payload.CardID)
	st.LastRevealed = ""
	st.LastMatch = false

	var resolved bool
	if len(st.Flipped) == 2 {
		// The second card completes the attempt: the server decides the match.
		st.Attempts++
		a, b := st.Flipped[0], st.Flipped[1]
		if priv.PairOf[a] == priv.PairOf[b] {
			st.Matched = append(st.Matched, a, b)
			st.LastMatch = true
			st.LastRevealed = priv.PairOf[a]
		} else {
			st.WrongAttempts++
			st.LastFlipped = append([]string(nil), st.Flipped...)
			st.RevealUntil = in.ReceivedAt + 1000
		}
		st.Flipped = nil
		resolved = true
	}

	// The board ends when every pair is matched or the attempts run out
	// .
	if st.matchedPairs() >= PairCount || st.Attempts >= pub.Config.MaxAttempts {
		st.Finished = true
	}

	units := scoreUnits(st.matchedPairs(), st.WrongAttempts)
	feedback := modeutil.Marshal(struct {
		CardID        string `json:"cardId"`
		Position      int    `json:"position"`
		ImagePath     string `json:"imagePath,omitempty"`
		Label         string `json:"label,omitempty"`
		Resolved      bool   `json:"resolved"`
		Matched       bool   `json:"matched"`
		MatchedPairs  int    `json:"matchedPairs"`
		WrongAttempts int    `json:"wrongAttempts"`
		Attempts      int    `json:"attempts"`
		ScoreUnits    int    `json:"scoreUnits"`
		Finished      bool   `json:"finished"`
	}{
		CardID:        payload.CardID,
		Position:      positionOf(pub.Cards, payload.CardID),
		ImagePath:     cardImageOf(priv, payload.CardID),
		Resolved:      resolved,
		Matched:       st.LastMatch,
		MatchedPairs:  st.matchedPairs(),
		WrongAttempts: st.WrongAttempts,
		Attempts:      st.Attempts,
		ScoreUnits:    units,
		Finished:      st.Finished,
	})

	dec := modes.Accepted(feedback, modeutil.Marshal(st))
	dec.Finished = st.Finished
	return dec, nil
}

// ControlTurn advances only after a complete pair attempt.
func (*Mode) ControlTurn(_ json.RawMessage, raw json.RawMessage) (string, bool, error) {
	var st state
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &st); err != nil {
			return "", false, fmt.Errorf("memory: decode control progress: %w", err)
		}
	}
	return strconv.Itoa(st.Attempts), !st.Finished, nil
}

// positionOf returns a card's board position.
func positionOf(cards []Card, cardID string) int {
	for _, card := range cards {
		if card.CardID == cardID {
			return card.Position
		}
	}
	return -1
}

// cardImageOf returns a card's face image.
func cardImageOf(priv privatePrepared, cardID string) string {
	return priv.CardImage[cardID]
}

// scoreUnits computes max(0, floor(100×matched/8) - 2×wrong) in integer units
// . The result is clamped at zero and never exceeds 100.
func scoreUnits(matchedPairs, wrongAttempts int) int {
	display := matchedPairs * 100 / PairCount
	display -= WrongPenalty * wrongAttempts
	if display < 0 {
		display = 0
	}
	if display > 100 {
		display = 100
	}
	return modeutil.Units(display)
}

// projection is the viewer-specific board state.
type projection struct {
	ModeID       string      `json:"modeId"`
	RulesVersion string      `json:"rulesVersion"`
	Phase        modes.Phase `json:"phase"`
	Config       Config      `json:"config"`
	Rows         int         `json:"rows"`
	Cols         int         `json:"cols"`
	// Cards carries only the ids and positions unless the viewer may see a face.
	Cards    []cardView    `json:"cards"`
	Subjects []progress    `json:"subjects"`
	Self     *selfMemory   `json:"self,omitempty"`
	Answer   *answerReveal `json:"answer,omitempty"`
}

// cardView is one board position as projected.
type cardView struct {
	CardID   string `json:"cardId"`
	Position int    `json:"position"`
	// State is one of hidden, flipped, matched.
	State string `json:"state"`
	// ImagePath and Label appear only when the face is legitimately visible.
	ImagePath string `json:"imagePath,omitempty"`
	Label     string `json:"label,omitempty"`
	// PairID appears only on reveal.
	PairID string `json:"pairId,omitempty"`
}

// Card states.
const (
	cardHidden  = "hidden"
	cardFlipped = "flipped"
	cardMatched = "matched"
)

// isMatched reports whether a card id is in the matched set.
func isMatched(set map[string]struct{}, cardID string) bool {
	_, ok := set[cardID]
	return ok
}

// isFlipped reports whether a card id is currently face up.
func isFlipped(set map[string]struct{}, cardID string) bool {
	_, ok := set[cardID]
	return ok
}

// progress is the coarse per-subject progress.
type progress struct {
	SubjectID string `json:"subjectId"`
	// Submitted reports whether this subject has flipped any card yet, so a host can
	// tell "not started" from "in progress" without inferring it from attempts.
	Submitted     bool `json:"submitted"`
	MatchedPairs  int  `json:"matchedPairs"`
	WrongAttempts int  `json:"wrongAttempts"`
	Attempts      int  `json:"attempts"`
	Finished      bool `json:"finished"`
}

// selfMemory is the caller's own state.
type selfMemory struct {
	SubjectID     string   `json:"subjectId"`
	Flipped       []string `json:"flipped"`
	Matched       []string `json:"matched"`
	MatchedPairs  int      `json:"matchedPairs"`
	WrongAttempts int      `json:"wrongAttempts"`
	Attempts      int      `json:"attempts"`
	MaxAttempts   int      `json:"maxAttempts"`
	Finished      bool     `json:"finished"`
	ScoreUnits    int      `json:"scoreUnits"`
	RevealUntil   int64    `json:"revealUntil,omitempty"`
}

// answerReveal is the hidden pairing map, released only on reveal.
type answerReveal struct {
	Pairs []revealedPair `json:"pairs"`
}

// revealedPair is one pair with its two cards and each card's face image.
//
// The two cards of a pair carry different face images of the same reviewed
// subject, so the reveal lists them per card instead of one image per pair
// (: 每对用相同已审核图 refers to the same reviewed subject, and the pack
// publishes one reviewed face per card).
type revealedPair struct {
	PairID string         `json:"pairId"`
	Label  string         `json:"label,omitempty"`
	Cards  []revealedCard `json:"cards"`
}

// revealedCard is one revealed card with the face image it showed.
type revealedCard struct {
	CardID    string `json:"cardId"`
	ImagePath string `json:"imagePath"`
}

// Project implements modes.Mode.
//
// The viewing phase shows every face, which is the documented 8-second full
// board view. Afterwards only the caller's own flipped and matched cards are
// visible; another subject's board never reveals faces, and the pairing map is
// server-side until reveal .
func (*Mode) Project(in modes.ProjectInput) (json.RawMessage, error) {
	var pub publicPrepared
	if len(in.Public) > 0 {
		if err := json.Unmarshal(in.Public, &pub); err != nil {
			return nil, fmt.Errorf("memory: decode public state: %w", err)
		}
	}
	var priv privatePrepared
	if len(in.Private) > 0 {
		if err := json.Unmarshal(in.Private, &priv); err != nil {
			return nil, fmt.Errorf("memory: decode private state: %w", err)
		}
	}

	reveal := in.Viewer.SeesSecrets() || (in.RevealNow && in.Phase.Terminal())

	// The viewing phase is a phase-level fact, not a per-subject one: while the
	// round is still in briefing every face is visible to everyone.
	viewing := in.Phase == modes.PhaseBriefing

	var selfState state
	if raw, ok := in.States[in.Viewer.Subject.ID]; ok && len(raw) > 0 {
		if err := json.Unmarshal(raw, &selfState); err != nil {
			return nil, fmt.Errorf("memory: decode own state: %w", err)
		}
	}

	flippedSet := make(map[string]struct{}, len(selfState.Flipped))
	for _, id := range selfState.Flipped {
		flippedSet[id] = struct{}{}
	}
	if selfState.RevealUntil > in.ServerTime {
		for _, id := range selfState.LastFlipped {
			flippedSet[id] = struct{}{}
		}
	}
	matchedSet := make(map[string]struct{}, len(selfState.Matched))
	for _, id := range selfState.Matched {
		matchedSet[id] = struct{}{}
	}

	out := projection{
		ModeID:       ModeID,
		RulesVersion: RulesVersion,
		Phase:        in.Phase,
		Config:       pub.Config,
		Rows:         pub.Rows,
		Cols:         pub.Cols,
		Cards:        make([]cardView, 0, len(pub.Cards)),
		Subjects:     make([]progress, 0, len(pub.Subjects)),
	}

	// Emit cards in position order, never map order.
	for _, card := range pub.Cards {
		view := cardView{CardID: card.CardID, Position: card.Position}
		pairID := priv.PairOf[card.CardID]
		image := cardImageOf(priv, card.CardID)
		switch {
		case reveal:
			view.State = cardMatched
			view.ImagePath = image
			view.PairID = pairID
		case viewing:
			view.State = cardFlipped
			view.ImagePath = image
		default:
			switch {
			case isMatched(matchedSet, card.CardID):
				view.State = cardMatched
				view.ImagePath = image
			case isFlipped(flippedSet, card.CardID):
				view.State = cardFlipped
				view.ImagePath = image
			default:
				view.State = cardHidden
			}
		}
		out.Cards = append(out.Cards, view)
	}

	for _, entry := range pub.Subjects {
		var st state
		if raw, ok := in.States[entry.SubjectID]; ok && len(raw) > 0 {
			if err := json.Unmarshal(raw, &st); err != nil {
				return nil, fmt.Errorf("memory: decode state of %q: %w", entry.SubjectID, err)
			}
		}
		out.Subjects = append(out.Subjects, progress{
			SubjectID:     entry.SubjectID,
			Submitted:     st.Attempts > 0 || st.Finished || len(st.Flipped) > 0,
			MatchedPairs:  st.matchedPairs(),
			WrongAttempts: st.WrongAttempts,
			Attempts:      st.Attempts,
			Finished:      st.Finished,
		})
		if entry.SubjectID == in.Viewer.Subject.ID {
			out.Self = &selfMemory{
				SubjectID:     entry.SubjectID,
				Flipped:       st.Flipped,
				Matched:       st.Matched,
				MatchedPairs:  st.matchedPairs(),
				WrongAttempts: st.WrongAttempts,
				Attempts:      st.Attempts,
				MaxAttempts:   pub.Config.MaxAttempts,
				Finished:      st.Finished,
				ScoreUnits:    scoreUnits(st.matchedPairs(), st.WrongAttempts),
			}
			if st.RevealUntil > in.ServerTime {
				out.Self.RevealUntil = st.RevealUntil
			}
		}
	}

	if reveal {
		// Group cards by pair, in position order, so the reveal is stable.
		byPair := make(map[string][]revealedCard, len(priv.PairLabel))
		for _, card := range pub.Cards {
			pairID := priv.PairOf[card.CardID]
			byPair[pairID] = append(byPair[pairID], revealedCard{
				CardID:    card.CardID,
				ImagePath: cardImageOf(priv, card.CardID),
			})
		}
		pairIDs := make([]string, 0, len(byPair))
		for pairID := range byPair {
			pairIDs = append(pairIDs, pairID)
		}
		sort.Strings(pairIDs)
		revealOut := &answerReveal{Pairs: make([]revealedPair, 0, len(pairIDs))}
		for _, pairID := range pairIDs {
			revealOut.Pairs = append(revealOut.Pairs, revealedPair{
				PairID: pairID,
				Label:  priv.PairLabel[pairID],
				Cards:  byPair[pairID],
			})
		}
		out.Answer = revealOut
	}
	return modeutil.Marshal(out), nil
}

// Settle implements modes.Mode.
//
// Score = max(0, floor(100×matchedPairs/8) - 2×wrongAttempts), with no time bonus
// .
func (*Mode) Settle(in modes.SettleInput) ([]modes.Score, error) {
	var pub publicPrepared
	if len(in.Public) > 0 {
		if err := json.Unmarshal(in.Public, &pub); err != nil {
			return nil, fmt.Errorf("memory: decode public state: %w", err)
		}
	}
	scores := make([]modes.Score, 0, len(pub.Subjects))
	for _, entry := range pub.Subjects {
		var st state
		if raw, ok := in.States[entry.SubjectID]; ok && len(raw) > 0 {
			if err := json.Unmarshal(raw, &st); err != nil {
				return nil, fmt.Errorf("memory: decode state of %q: %w", entry.SubjectID, err)
			}
		}
		matched := st.matchedPairs()
		units := scoreUnits(matched, st.WrongAttempts)
		explanation := modeutil.Marshal(struct {
			MatchedPairs  int `json:"matchedPairs"`
			PairCount     int `json:"pairCount"`
			WrongAttempts int `json:"wrongAttempts"`
			Attempts      int `json:"attempts"`
			MaxAttempts   int `json:"maxAttempts"`
		}{
			MatchedPairs:  matched,
			PairCount:     PairCount,
			WrongAttempts: st.WrongAttempts,
			Attempts:      st.Attempts,
			MaxAttempts:   pub.Config.MaxAttempts,
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

// cardFaceRoute derives a per-card image URL from a pair's stored image.
//
// Two cards of one pair must not share a URL: a player could otherwise read the
// URL from one flipped card and identify its partner from the network tab, which
// defeats the memory mode entirely . The route therefore carries a
// per-card token, and the server maps every token of a pair back to the one stored
// file, so storage stays at one image per pair.
//
// The token is derived from the card's own copy index plus the stored path, so it
// is deterministic for a given pack and does not need to be persisted.
func cardFaceRoute(pairID, storedPath string, copyIndex int) string {
	if storedPath == "" {
		return ""
	}
	// Use a distinct opaque filename per card; a shared pair directory would reveal matches.
	// The deterministic token includes the pair, stored path and copy index. Its file mapping remains in private prepared state and the static resolver.
	ext := path.Ext(storedPath)
	sum := sha256.Sum256([]byte(pairID + "\x00" + storedPath + "\x00" + strconv.Itoa(copyIndex)))
	token := "f" + hex.EncodeToString(sum[:10])
	return "/assets/faces/" + token + ext
}
