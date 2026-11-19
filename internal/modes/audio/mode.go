// Package audio implements G15 同句异声 / 听音配对 .
//
// Reviewed media admission and pairing rules live here. Browser play budgets are
// casual rules, not anti-recording guarantees. A reported media fault is persisted;
// official settlement waits for host cancellation/replacement instead of trusting a
// player's claim to remove their wrong answers from the denominator.
package audio

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/bhcn/umaguessinggame/internal/modes"
	"github.com/bhcn/umaguessinggame/internal/modes/modeutil"
)

// ModeID is the stable mode identifier (contract ModeId enum).
const ModeID = "audio"

// RulesVersion is the rule revision this implementation satisfies.
const RulesVersion = "2.0.0-draft.1"

// Action kinds.
//
// The value is the contract discriminator (ModeAction → AudioMatches), not an internal
// name .
const (
	// ActionKindMatches submits the clip → character pairing.
	ActionKindMatches = "audio.matches"
	ActionKindReady   = "audio.ready"
)

// ContractKinds returns the ModeAction discriminators this mode accepts.
func (*Mode) ContractKinds() []string { return []string{ActionKindMatches, ActionKindReady} }

// Rejection codes.
const (
	CodeInvalidPayload     = "invalid_payload"
	CodeUnknownAction      = "unknown_action"
	CodeNotOnRoster        = "not_on_roster"
	CodeUnknownClip        = "unknown_clip"
	CodeUnknownCharacter   = "unknown_character"
	CodeDuplicateClip      = "duplicate_clip"
	CodeDuplicateCharacter = "duplicate_character"
	CodeIncomplete         = "incomplete_matches"
	CodeRoundClosed        = "round_closed"
	CodePlayLimit          = "play_limit_reached"
	// CodePlaybackFailed records a media fault separately from an incorrect answer.
	CodePlaybackFailed = "playback_failed"
)

// Documented parameters .
const (
	// MinClips and MaxClips bound the number of clips per round.
	MinClips = 2
	MaxClips = 6
	// DefaultClips is the documented baseline.
	DefaultClips = 4
	// AnswerSeconds is the answering window.
	AnswerSeconds = 90
	// MaxPlaysPerClip is how many times a clip may be played. It is a client-honoured
	// casual rule, not a technical restriction .
	MaxPlaysPerClip = 2
	// MinSubjects and MaxSubjects bound the participant count.
	MinSubjects = 1
	MaxSubjects = 8
)

// Config is the round configuration.
type Config struct {
	// Clips is how many clips the round uses.
	Clips int `json:"clips"`
	// AnswerSeconds is the answering window.
	AnswerSeconds int `json:"answerSeconds"`
	// MaxPlays is how many times each clip may be played.
	MaxPlays int `json:"maxPlays"`
}

// decodeConfig parses and normalises a configuration blob.
func decodeConfig(raw json.RawMessage) (Config, error) {
	cfg := Config{Clips: DefaultClips, AnswerSeconds: AnswerSeconds, MaxPlays: MaxPlaysPerClip}
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &cfg); err != nil {
			return Config{}, fmt.Errorf("audio: invalid config: %w", err)
		}
	}
	if cfg.Clips == 0 {
		cfg.Clips = DefaultClips
	}
	if cfg.AnswerSeconds == 0 {
		cfg.AnswerSeconds = AnswerSeconds
	}
	if cfg.MaxPlays == 0 {
		cfg.MaxPlays = MaxPlaysPerClip
	}
	if cfg.Clips < MinClips || cfg.Clips > MaxClips {
		return Config{}, fmt.Errorf(
			"audio: clips %d outside [%d,%d]", cfg.Clips, MinClips, MaxClips)
	}
	if cfg.AnswerSeconds < 30 || cfg.AnswerSeconds > 600 {
		return Config{}, fmt.Errorf(
			"audio: answerSeconds %d outside [30,600]", cfg.AnswerSeconds)
	}
	// The play count is a documented casual rule, so the value is bounded to the
	// published baseline rather than being freely configurable: a round that allowed
	// unlimited plays would change the difficulty the plan fixes.
	if cfg.MaxPlays < 1 || cfg.MaxPlays > MaxPlaysPerClip {
		return Config{}, fmt.Errorf(
			"audio: maxPlays %d outside [1,%d]", cfg.MaxPlays, MaxPlaysPerClip)
	}
	return cfg, nil
}

// clip is one admissible audio clip.
//
// Every field here is part of the admission gate. A clip is usable only when the file
// is locally decodable, its duration is known, its speaker is verified, and its
// transcript has been reviewed — a URL alone proves none of that.
type clip struct {
	ClipID string `json:"clipId"`
	// CharacterID is the speaker the clip belongs to. It is the answer, so it is only
	// published at the reveal.
	CharacterID string `json:"characterId"`
	// AudioPath is the locally served path of the decoded file.
	AudioPath string `json:"audioPath"`
	// DurationMillis is the clip length.
	DurationMillis int    `json:"durationMillis"`
	ByteLength     int    `json:"byteLength"`
	SHA256         string `json:"sha256"`
	// CharacterName is the speaker's display name, published on the candidate list so
	// a player can identify who they are matching against. It is the answer, so it is
	// never attached to a clip — only to the candidate list after the draw.
	CharacterName string `json:"characterName"`
	// Transcript is the reviewed line text. It is never published before the reveal: a
	// published transcript would let a player search the wiki for the line and skip the
	// listening task entirely .
	Transcript string `json:"transcript"`
	// SpeakerVerified reports that the voice was confirmed to belong to CharacterID.
	SpeakerVerified bool `json:"speakerVerified"`
	// Reviewed reports that a human listened to the clip.
	Reviewed bool `json:"reviewed"`
	// Decodable reports that the file was decoded successfully.
	Decodable bool `json:"decodable"`
}

// usable reports whether a clip may be posed.
func (c clip) usable() bool {
	return c.ClipID != "" && c.CharacterID != "" && safeAudioPath(c.AudioPath) &&
		c.DurationMillis > 0 && c.Decodable && c.SpeakerVerified && c.Reviewed
}

// Only same-origin asset paths; reject protocol-relative URLs, encoded traversal,
// query strings, fragments and arbitrary schemes before publishing a media address.
var audioPathPattern = regexp.MustCompile(`^/assets/audio/[A-Za-z0-9_-]+\.webm$`)

func safeAudioPath(raw string) bool {
	return audioPathPattern.MatchString(raw)
}

// ClipsSection is the pack section carrying the candidate clips.
const ClipsSection = "audio.clips"

// AdmissionError is the refusal Prepare returns when no usable clip exists.
type AdmissionError struct {
	// Total is how many candidate rows the pack carries.
	Total int
	// NotDecodable, Unverified and Unreviewed break the shortfall down.
	NotDecodable int
	Unverified   int
	Unreviewed   int
	// NoLocalFile is how many rows carry a remote URL but no local file.
	NoLocalFile int
	// Reason is the short machine-readable cause.
	Reason string
}

// Error implements error.
func (e *AdmissionError) Error() string {
	if e.Total == 0 {
		return "audio: no clips are available in the loaded content pack"
	}
	return fmt.Sprintf(
		"audio: no usable clips (%d candidates, %d without a local decodable file, "+
			"%d unverified speakers, %d unreviewed); a decodable local file and a "+
			"listening review are required before play",
		e.Total, e.NoLocalFile, e.Unverified, e.Unreviewed)
}

// ReasonCode returns the machine-readable cause.
func (e *AdmissionError) ReasonCode() string {
	if e.Reason != "" {
		return e.Reason
	}
	if e.Total == 0 {
		return "no_clips"
	}
	return "no_usable_clips"
}

// loadUsableClips reads the pack's clip rows and returns the admissible ones with the
// rejection breakdown.
func loadUsableClips(src modes.ContentSource) (usable []clip, admission AdmissionError, err error) {
	if src == nil {
		return nil, AdmissionError{}, fmt.Errorf("audio: no content source")
	}
	raw, ok := src.Section(ClipsSection)
	if !ok || len(raw) == 0 {
		return nil, AdmissionError{}, nil
	}
	records, decErr := modeutil.Records(raw, "clips", "items")
	if decErr != nil {
		return nil, AdmissionError{}, fmt.Errorf("audio: decode section %q: %w", ClipsSection, decErr)
	}
	for _, rec := range records {
		admission.Total++
		c := clip{
			ClipID:          rec.Str("clipId", "clip_id", "id"),
			CharacterID:     rec.Str("characterId", "character_id"),
			CharacterName:   rec.Str("characterName", "character_name", "name"),
			AudioPath:       rec.Str("audioPath", "audio_path", "audioUrl", "audio_url"),
			DurationMillis:  intField(rec, "durationMillis", "duration_ms"),
			ByteLength:      intField(rec, "byteLength"),
			SHA256:          rec.Str("sha256"),
			Transcript:      rec.Str("transcript", "text", "chs"),
			SpeakerVerified: rec.Bool("speakerVerified", "speaker_verified"),
			Reviewed:        rec.Bool("reviewed"),
			Decodable:       rec.Bool("decodable"),
		}
		// A row that carries only a remote URL has no local file: the plan forbids
		// depending on fetching a temporary external link .
		if !safeAudioPath(c.AudioPath) {
			admission.NoLocalFile++
			continue
		}
		if !c.Decodable {
			admission.NotDecodable++
			continue
		}
		if !c.SpeakerVerified {
			admission.Unverified++
			continue
		}
		if !c.Reviewed {
			admission.Unreviewed++
			continue
		}
		if !c.usable() {
			admission.NotDecodable++
			continue
		}
		usable = append(usable, c)
	}
	// Deterministic order so a draw and a refusal message are stable.
	ids, paths := map[string]int{}, map[string]int{}
	for _, c := range usable {
		ids[c.ClipID]++
		paths[c.AudioPath]++
	}
	filtered := usable[:0]
	for _, c := range usable {
		if ids[c.ClipID] == 1 && paths[c.AudioPath] == 1 {
			filtered = append(filtered, c)
		}
	}
	usable = filtered
	sort.Slice(usable, func(i, j int) bool { return usable[i].ClipID < usable[j].ClipID })
	return usable, admission, nil
}

// intField reads an integer field through the FieldMap accessor.
func intField(rec modeutil.FieldMap, keys ...string) int {
	if v, ok := rec.Int(keys...); ok {
		return v
	}
	return 0
}

// candidate is one published character the player may pair a clip with.
//
// The character list contains each clip's speaker exactly once, matching the
// published one-to-one pairing rules. Its order is independent of the clips.
type candidate struct {
	CharacterID string `json:"characterId"`
	Name        string `json:"name"`
	ImagePath   string `json:"imagePath,omitempty"`
}

// publicClip is one clip as the board publishes it.
//
// It carries an opaque clip id, a served audio route and a duration, and it never
// carries the transcript or the speaker: either would hand the answer over.
type publicClip struct {
	ClipID string `json:"clipId"`
	// AudioPath is the public, same-origin route used by the player.
	AudioPath      string `json:"audioPath"`
	DurationMillis int    `json:"durationMillis"`
	// MaxPlays is the per-clip play budget.
	MaxPlays int `json:"maxPlays"`
}

// rosterEntry is one participant recorded in the public prepared state.
type rosterEntry struct {
	SubjectID   string            `json:"subjectId"`
	SubjectKind modes.SubjectKind `json:"subjectKind"`
}

// publicState is the non-secret round state.
type publicState struct {
	ModeID       string        `json:"modeId"`
	RulesVersion string        `json:"rulesVersion"`
	Config       Config        `json:"config"`
	Subjects     []rosterEntry `json:"subjects"`
	// Clips is the board: opaque ids, served routes, durations, play budgets.
	Clips []publicClip `json:"clips"`
	// Candidates is the character list a player pairs clips with.
	Candidates []candidate `json:"candidates"`
	// PreloadRequired reports that everybody must preload successfully before the round
	// starts .
	PreloadRequired bool   `json:"preloadRequired"`
	Scoring         string `json:"scoring"`
}

// privateState is the server-only round state.
type privateState struct {
	ModeID string `json:"modeId"`
	// SpeakerOf maps clip id → the true character id.
	SpeakerOf map[string]string `json:"speakerOf"`
	// Transcripts maps clip id → the reviewed line, used at the reveal.
	Transcripts map[string]string `json:"transcripts"`
	// ClipOrder is the board order, so the reveal can present it consistently.
	ClipOrder []string `json:"clipOrder"`
}

// subjectState is one participant's own persisted state.
type subjectState struct {
	Ready bool `json:"ready"`
	// Matches is the current clip → character mapping.
	Matches []matchRecord `json:"matches"`
	// Plays counts how many times each clip was played, keyed by clip id.
	Plays map[string]int `json:"plays"`
	// PlaybackFailures lists the clips this participant reported as unplayable.
	PlaybackFailures []string `json:"playbackFailures,omitempty"`
	// Submissions counts accepted revisions.
	Submissions int `json:"submissions"`
}

// matchRecord is one clip → character pair.
type matchRecord struct {
	ClipID      string `json:"clipId"`
	CharacterID string `json:"characterId"`
}

// Mode implements modes.Mode for G15.
type Mode struct{}

type PlaybackReviewError struct{}

func (*PlaybackReviewError) Error() string {
	return "audio: unresolved playback failure; cancel and replace the round"
}
func (*PlaybackReviewError) ReasonCode() string { return "audio-playback-review-required" }

// New returns the audio mode.
func New() *Mode { return &Mode{} }

// ID implements modes.Mode.
func (*Mode) ID() string { return ModeID }

// RulesVersion implements modes.Mode.
func (*Mode) RulesVersion() string { return RulesVersion }

// Title implements modes.Mode.
func (*Mode) Title() string { return "同句异声" }

// Summary implements modes.Mode.
func (*Mode) Summary() string {
	return "听几段语音，把每段和说话的角色配对；每对正确得分，播放失败不算答错。"
}

// InformationStyles reports which information forms this mode implements.
func (*Mode) InformationStyles() []string {
	return []string{"closed"}
}

// RoundWindow implements modes.Mode.
//
// 整局只有一次作答窗口。返回 0 表示本模式没有自己的窗口意见，交给房间层默认值。
func (*Mode) RoundWindow(raw json.RawMessage) time.Duration {
	cfg, err := decodeConfig(raw)
	if err != nil {
		return 0
	}
	return time.Duration(cfg.AnswerSeconds) * time.Second
}

// TurnWindow implements modes.Mode. This mode is not turn-based: it runs on the
// single RoundWindow deadline, so there is no per-turn clock to report.
func (*Mode) TurnWindow(json.RawMessage) time.Duration { return 0 }

// MinSubjects implements modes.Mode.
func (*Mode) MinSubjects() int { return MinSubjects }

// MaxSubjects implements modes.Mode.
func (*Mode) MaxSubjects() int { return MaxSubjects }

// Requirement implements modes.Mode.
//
// audio needs verified media: a decodable local file, a verified speaker and a
// listening review. The requirement declares NeedsAssets, and the admission gate
// refuses the round when the pack carries none .
func (*Mode) Requirement() modes.ContentRequirement {
	return modes.ContentRequirement{
		Fields:      []string{"audio.audioPath", "audio.durationMillis", "audio.speaker"},
		MinEntities: MinClips,
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
// This is the admission gate. It refuses a round unless the pack carries at least
// MinClips decodable, speaker-verified, listening-reviewed clips with a local file, and
// it reports the breakdown so the lobby can say why.
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
			return modes.Prepared{}, fmt.Errorf("audio: duplicate subject %q", subj.ID)
		}
		seen[subj.ID] = struct{}{}
		roster = append(roster, rosterEntry{SubjectID: subj.ID, SubjectKind: subj.Kind})
	}
	if len(roster) < MinSubjects || len(roster) > MaxSubjects {
		return modes.Prepared{}, fmt.Errorf(
			"audio: %d participants outside [%d,%d]",
			len(roster), MinSubjects, MaxSubjects)
	}

	usable, admission, err := loadUsableClips(in.Content)
	if err != nil {
		return modes.Prepared{}, err
	}
	if len(usable) < MinClips {
		// The refusal. It names the breakdown and never falls back to an unverified or
		// unplayable clip.
		admission.Reason = "no_usable_clips"
		return modes.Prepared{}, &AdmissionError{
			Total:        admission.Total,
			NotDecodable: admission.NotDecodable,
			Unverified:   admission.Unverified,
			Unreviewed:   admission.Unreviewed,
			NoLocalFile:  admission.NoLocalFile,
			Reason:       admission.Reason,
		}
	}

	// Draw the clips. Each clip must have a distinct speaker, because two clips of the
	// same character would make the pairing ambiguous: the player could not tell which
	// clip belongs to which slot.
	stream := modeutil.NewStream(in.Scope.Seed + "\x00audio.clips")
	indices := make([]int, len(usable))
	for i := range indices {
		indices[i] = i
	}
	stream.Shuffle(len(indices), func(i, j int) { indices[i], indices[j] = indices[j], indices[i] })
	chosen := make([]clip, 0, cfg.Clips)
	usedSpeakers := make(map[string]struct{}, cfg.Clips)
	for _, idx := range indices {
		if len(chosen) >= cfg.Clips {
			break
		}
		c := usable[idx]
		if _, dup := usedSpeakers[c.CharacterID]; dup {
			continue
		}
		usedSpeakers[c.CharacterID] = struct{}{}
		chosen = append(chosen, c)
	}
	if len(chosen) < cfg.Clips {
		return modes.Prepared{}, fmt.Errorf(
			"audio: only %d distinct speakers among %d usable clips, need %d",
			len(chosen), len(usable), cfg.Clips)
	}

	// The published rules use the same number of clips and characters. Candidate
	// order is independent of the shuffled clip order.
	candidates := make([]candidate, 0, len(chosen))
	for _, c := range chosen {
		candidates = append(candidates, candidate{CharacterID: c.CharacterID, Name: candidateName(c)})
	}
	// The candidate order must not put the answers first.
	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].CharacterID < candidates[j].CharacterID
	})

	// Build the board with opaque ids so the clip id does not name the speaker.
	publicClips := make([]publicClip, 0, len(chosen))
	speakerOf := make(map[string]string, len(chosen))
	transcripts := make(map[string]string, len(chosen))
	clipOrder := make([]string, 0, len(chosen))
	for _, c := range chosen {
		opaqueID := opaqueClipID(in.Scope.Seed, c.ClipID)
		publicClips = append(publicClips, publicClip{
			ClipID:         opaqueID,
			AudioPath:      c.AudioPath,
			DurationMillis: c.DurationMillis,
			MaxPlays:       cfg.MaxPlays,
		})
		speakerOf[opaqueID] = c.CharacterID
		transcripts[opaqueID] = c.Transcript
		clipOrder = append(clipOrder, opaqueID)
	}
	// Shuffle the board order so its position carries no information about the source
	// order.
	stream.Shuffle(len(publicClips), func(i, j int) {
		publicClips[i], publicClips[j] = publicClips[j], publicClips[i]
	})
	clipOrder = clipOrder[:0]
	for _, c := range publicClips {
		clipOrder = append(clipOrder, c.ClipID)
	}

	pub := publicState{
		ModeID:          ModeID,
		RulesVersion:    RulesVersion,
		Config:          cfg,
		Subjects:        roster,
		Clips:           publicClips,
		Candidates:      candidates,
		PreloadRequired: true,
		Scoring:         "按正确配对的比例计分，满分100分，最后取整数；不按下载速度奖励。播放失败不算答错，请告诉主持处理。",
	}
	priv := privateState{
		ModeID:      ModeID,
		SpeakerOf:   speakerOf,
		Transcripts: transcripts,
		ClipOrder:   clipOrder,
	}
	return modes.Prepared{
		Public:        modeutil.Marshal(pub),
		Private:       modeutil.Marshal(priv),
		QuestionCount: len(publicClips),
	}, nil
}

// opaqueClipID builds an opaque board id for a clip.
//
// The id must not name the speaker: the source clip id may be derived from the
// character, and publishing it would let a player read the answer from the network tab.
func opaqueClipID(seed, sourceClipID string) string {
	sum := sha256.Sum256([]byte(seed + "\x00audio.clip\x00" + sourceClipID))
	return "ac-" + hex.EncodeToString(sum[:6])
}

// candidateName is the display name of a candidate.
//
// The compiled clip rows carry no display name, and inventing one would be a fabricated
// character fact. The id is shown instead, and the UI is expected to join it against the
// public character index, which is the authoritative name source.
func candidateName(c clip) string {
	// A candidate without a name is unusable: the board would show opaque ids and the
	// player could not tell who they are picking. The compiled section carries the
	// display name, so this only falls back when a pack predates that field.
	if name := strings.TrimSpace(c.CharacterName); name != "" {
		return name
	}
	return c.CharacterID
}

// matchesPayload is the body of ActionKindMatches, matching the contract's AudioMatches
// schema: {kind, matches[]} where each match is {clipId, characterId}.
type matchesPayload struct {
	Kind             string   `json:"kind"`
	PlaybackFailures []string `json:"playbackFailures,omitempty"`
	Matches          []struct {
		ClipID      string `json:"clipId"`
		CharacterID string `json:"characterId"`
	} `json:"matches"`
}

// decodePublic unmarshals the public state.
func decodePublic(raw json.RawMessage) (publicState, error) {
	var pub publicState
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &pub); err != nil {
			return publicState{}, fmt.Errorf("audio: decode public state: %w", err)
		}
	}
	return pub, nil
}

// decodePrivate unmarshals the answer.
func decodePrivate(raw json.RawMessage) (privateState, error) {
	var priv privateState
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &priv); err != nil {
			return privateState{}, fmt.Errorf("audio: decode private state: %w", err)
		}
	}
	return priv, nil
}

// decodeState unmarshals a participant state.
func decodeState(raw json.RawMessage) (subjectState, error) {
	var st subjectState
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &st); err != nil {
			return subjectState{}, fmt.Errorf("audio: decode participant state: %w", err)
		}
	}
	return st, nil
}

// Apply implements modes.Mode.
//
// A submission pairs every clip with a character. It may be revised before the deadline,
// which is the documented rule . The submission is validated
// against the board and the candidate list; it is never compared against the answer here,
// because telling a player which pair is right would let them search the space.
func (*Mode) Apply(in modes.ApplyInput) (modes.Decision, error) {
	if in.Action.Kind == ActionKindReady {
		return applyReadiness(in)
	}
	if in.Action.Kind != ActionKindMatches {
		return modes.Rejected(CodeUnknownAction,
			fmt.Sprintf("audio: unsupported action %q", in.Action.Kind)), nil
	}
	if err := in.Action.Validate(); err != nil {
		return modes.Rejected(CodeInvalidPayload, err.Error()), nil
	}
	var payload matchesPayload
	if err := json.Unmarshal(in.Action.Payload, &payload); err != nil {
		return modes.Rejected(CodeInvalidPayload, "audio: payload must carry matches"), nil
	}
	if payload.Kind != "" && payload.Kind != ActionKindMatches {
		return modes.Rejected(CodeInvalidPayload, fmt.Sprintf(
			"audio: payload kind %q does not match action %q",
			payload.Kind, ActionKindMatches)), nil
	}

	pub, err := decodePublic(in.Public)
	if err != nil {
		return modes.Decision{}, err
	}
	if !onRoster(pub.Subjects, in.Subject.ID) {
		return modes.Rejected(CodeNotOnRoster, fmt.Sprintf(
			"audio: subject %q is not on this roster", in.Subject.ID)), nil
	}
	st, err := decodeState(in.State)
	if err != nil {
		return modes.Decision{}, err
	}

	knownClips := make(map[string]struct{}, len(pub.Clips))
	for _, c := range pub.Clips {
		knownClips[c.ClipID] = struct{}{}
	}
	knownCharacters := make(map[string]struct{}, len(pub.Candidates))
	for _, c := range pub.Candidates {
		knownCharacters[c.CharacterID] = struct{}{}
	}

	matches := make([]matchRecord, 0, len(payload.Matches))
	usedClips := make(map[string]struct{}, len(payload.Matches))
	usedCharacters := make(map[string]struct{}, len(payload.Matches))
	for _, item := range payload.Matches {
		if _, ok := knownClips[item.ClipID]; !ok {
			return modes.Rejected(CodeUnknownClip, fmt.Sprintf(
				"audio: clip %q is not on this board", item.ClipID)), nil
		}
		if _, ok := knownCharacters[item.CharacterID]; !ok {
			return modes.Rejected(CodeUnknownCharacter, fmt.Sprintf(
				"audio: character %q is not in the candidate list", item.CharacterID)), nil
		}
		if _, dup := usedClips[item.ClipID]; dup {
			return modes.Rejected(CodeDuplicateClip, fmt.Sprintf(
				"audio: clip %q is matched twice", item.ClipID)), nil
		}
		// Each character appears at most once, because the board has one clip per
		// speaker: pairing two clips with one character would guarantee one of them is
		// wrong without telling the player which.
		if other, dup := usedCharacters[item.CharacterID]; dup {
			return modes.Rejected(CodeDuplicateCharacter, fmt.Sprintf(
				"audio: character %q is used for both clip %q and clip %q",
				item.CharacterID, other, item.ClipID)), nil
		}
		usedClips[item.ClipID] = struct{}{}
		usedCharacters[item.CharacterID] = struct{}{}
		matches = append(matches, matchRecord{
			ClipID:      item.ClipID,
			CharacterID: item.CharacterID,
		})
	}
	failed := map[string]bool{}
	for _, id := range payload.PlaybackFailures {
		if _, ok := knownClips[id]; !ok {
			return modes.Rejected(CodeUnknownClip, "audio: unknown failed clip"), nil
		}
		if failed[id] {
			return modes.Rejected(CodeDuplicateClip, "audio: repeated failed clip"), nil
		}
		if _, matched := usedClips[id]; matched {
			return modes.Rejected(CodeInvalidPayload, "audio: failed clip must not have an answer"), nil
		}
		failed[id] = true
	}
	if len(matches)+len(failed) != len(knownClips) {
		return modes.Rejected(CodeIncomplete, "audio: every clip needs an answer or a failure report"), nil
	}
	// A report remains unresolved until the host replaces/voids the round. A later
	// answer revision must not silently erase a report the host still needs to see.
	for _, id := range st.PlaybackFailures {
		failed[id] = true
	}
	st.PlaybackFailures = []string{}
	for id := range failed {
		st.PlaybackFailures = append(st.PlaybackFailures, id)
	}
	sort.Strings(st.PlaybackFailures)

	// Canonical order so the stored bytes are stable regardless of submission order.
	sort.Slice(matches, func(i, j int) bool { return matches[i].ClipID < matches[j].ClipID })
	st.Matches = matches
	st.Submissions++

	feedback := modeutil.Marshal(struct {
		PairedClips int `json:"pairedClips"`
		TotalClips  int `json:"totalClips"`
		Submissions int `json:"submissions"`
		// Note explains the revision rule and, importantly, that the feedback does not
		// reveal correctness.
		Note string `json:"note"`
	}{
		PairedClips: len(matches),
		TotalClips:  len(knownClips),
		Submissions: st.Submissions,
		Note:        "已记录；截止前可以修改，揭晓时公布结果。有音频故障时请等主持处理。",
	})
	return modes.Accepted(feedback, modeutil.Marshal(st)), nil
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

// projection is the viewer-specific round state.
type projection struct {
	ModeID       string      `json:"modeId"`
	RulesVersion string      `json:"rulesVersion"`
	Phase        modes.Phase `json:"phase"`
	Config       Config      `json:"config"`
	Scoring      string      `json:"scoring"`
	// Clips is the board. The transcript and the speaker are never here.
	Clips []publicClip `json:"clips"`
	// Candidates is the character list.
	Candidates []candidate `json:"candidates"`
	// PreloadRequired repeats the gate.
	PreloadRequired bool `json:"preloadRequired"`
	// Self is the caller's own state.
	Self *selfView `json:"self,omitempty"`
	// Progress is the coarse per-participant progress.
	Progress []progressView `json:"progress"`
	// Answer is the pairing plus the transcripts, published only at the end.
	Answer *answerReveal `json:"answer,omitempty"`
}

// selfView is the caller's own state.
type selfView struct {
	Ready       bool           `json:"ready"`
	SubjectID   string         `json:"subjectId"`
	Matches     []matchRecord  `json:"matches"`
	Plays       map[string]int `json:"plays"`
	Submissions int            `json:"submissions"`
	// PlaybackFailures lets the client report a clip that would not play, which the
	// host uses to replace the clip rather than scoring the round as a loss.
	PlaybackFailures []string `json:"playbackFailures,omitempty"`
}

// progressView is another participant's progress.
type progressView struct {
	Ready            bool   `json:"ready"`
	SubjectID        string `json:"subjectId"`
	PairedClips      int    `json:"pairedClips"`
	Submissions      int    `json:"submissions"`
	PlaybackFailures int    `json:"playbackFailures"`
}

// answerReveal is the answer, published only when authorised.
type answerReveal struct {
	// Clips lists the pairing with the transcript, in board order.
	Clips []revealedClip `json:"clips"`
}

// revealedClip is one revealed clip.
type revealedClip struct {
	ClipID      string `json:"clipId"`
	CharacterID string `json:"characterId"`
	Transcript  string `json:"transcript"`
	AudioPath   string `json:"audioPath"`
}

// Project implements modes.Mode.
//
// The visibility rules:
//
// - The transcript is never published before the reveal: a published transcript would
// let a player search the wiki for the line and skip the listening task.
// - The speaker is never published before the reveal.
// - The audio route IS published, because a browser must fetch it. Hiding it would not
// be a security measure , so the payload does not pretend
// otherwise.
// - Another participant's pairings are not published before the reveal.
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
		ModeID:          ModeID,
		RulesVersion:    RulesVersion,
		Phase:           in.Phase,
		Config:          pub.Config,
		Scoring:         pub.Scoring,
		Clips:           pub.Clips,
		Candidates:      pub.Candidates,
		PreloadRequired: pub.PreloadRequired,
		Progress:        make([]progressView, 0, len(pub.Subjects)),
	}

	// Iterate the roster order recorded at prepare time, never the map, so the projection
	// is byte-identical across runs .
	for _, entry := range pub.Subjects {
		st, err := decodeState(in.States[entry.SubjectID])
		if err != nil {
			return nil, fmt.Errorf("audio: decode state of %q: %w", entry.SubjectID, err)
		}
		out.Progress = append(out.Progress, progressView{
			Ready:            st.Ready && len(st.PlaybackFailures) == 0,
			SubjectID:        entry.SubjectID,
			PairedClips:      len(st.Matches),
			Submissions:      st.Submissions,
			PlaybackFailures: len(st.PlaybackFailures),
		})
		if entry.SubjectID == in.Viewer.Subject.ID {
			matches := st.Matches
			if matches == nil {
				matches = []matchRecord{}
			}
			plays := st.Plays
			if plays == nil {
				plays = map[string]int{}
			}
			out.Self = &selfView{
				Ready:            st.Ready && len(st.PlaybackFailures) == 0,
				SubjectID:        entry.SubjectID,
				Matches:          matches,
				Plays:            plays,
				Submissions:      st.Submissions,
				PlaybackFailures: st.PlaybackFailures,
			}
		}
	}

	if in.Phase == modes.PhaseRevealed || in.Phase == modes.PhaseSettled || in.Viewer.SeesSecrets() {
		reveal := answerReveal{Clips: make([]revealedClip, 0, len(pub.Clips))}
		audioPath := make(map[string]string, len(pub.Clips))
		for _, c := range pub.Clips {
			audioPath[c.ClipID] = c.AudioPath
		}
		// Present the reveal in board order, which is the order the player saw.
		for _, clipID := range priv.ClipOrder {
			reveal.Clips = append(reveal.Clips, revealedClip{
				ClipID:      clipID,
				CharacterID: priv.SpeakerOf[clipID],
				Transcript:  priv.Transcripts[clipID],
				AudioPath:   audioPath[clipID],
			})
		}
		out.Answer = &reveal
	}
	return modeutil.Marshal(out), nil
}

// Settle scores confirmed pairings. A media-fault report cannot award extra
// points by removing selected clips; host cancellation writes no score at all.
func (*Mode) Settle(in modes.SettleInput) ([]modes.Score, error) {
	pub, err := decodePublic(in.Public)
	if err != nil {
		return nil, err
	}
	priv, err := decodePrivate(in.Private)
	if err != nil {
		return nil, err
	}
	total := len(pub.Clips)
	if total == 0 {
		return nil, fmt.Errorf("audio: the round has no clip to score")
	}

	scores := make([]modes.Score, 0, len(pub.Subjects))
	for _, entry := range pub.Subjects {
		st, err := decodeState(in.States[entry.SubjectID])
		if err != nil {
			return nil, fmt.Errorf("audio: decode state of %q: %w", entry.SubjectID, err)
		}
		if len(st.PlaybackFailures) > 0 {
			return nil, &PlaybackReviewError{}
		}
		scored := total
		correct := 0
		matched := make([]string, 0, len(st.Matches))
		for _, m := range st.Matches {
			matched = append(matched, m.ClipID)
			if want, ok := priv.SpeakerOf[m.ClipID]; ok && want == m.CharacterID {
				correct++
			}
		}
		// floor(100 × correct / scored) display points, stored as scoreUnits.
		display := 0
		if scored > 0 {
			display = 100 * correct / scored
		}
		units := display * modeutil.UnitsPerPoint
		maxUnits := modeutil.Units(100)

		explanation := modeutil.Marshal(struct {
			Correct        int      `json:"correctPairs"`
			ScoredClips    int      `json:"scoredClips"`
			TotalClips     int      `json:"totalClips"`
			PlaybackFailed []string `json:"playbackFailed"`
			Matched        []string `json:"matchedClips"`
			Formula        string   `json:"formula"`
			Note           string   `json:"note"`
		}{
			Correct:        correct,
			ScoredClips:    scored,
			TotalClips:     total,
			PlaybackFailed: nonNil(st.PlaybackFailures),
			Matched:        nonNil(matched),
			Formula:        "floor(100*correctPairs/scoredClips)",
			Note:           "按最后确认的配对计分。音频故障由主持处理，不当作答错结算。",
		})
		score := modes.Score{
			Subject:       modes.Subject{ID: entry.SubjectID, Kind: entry.SubjectKind},
			ScoreUnits:    units,
			MaxScoreUnits: maxUnits,
			Explanation:   explanation,
		}
		if err := score.Validate(); err != nil {
			return nil, err
		}
		scores = append(scores, score)
	}
	return modes.RankScores(scores), nil
}

// nonNil returns an empty slice for nil so the JSON carries [] rather than null.
func nonNil(in []string) []string {
	if in == nil {
		return []string{}
	}
	return in
}

// applyReadiness records one complete media preparation report without submitting
// guesses. Playback faults remain sticky and require cancellation, not rescaling.
func applyReadiness(in modes.ApplyInput) (modes.Decision, error) {
	if err := in.Action.Validate(); err != nil {
		return modes.Rejected(CodeInvalidPayload, err.Error()), nil
	}
	var payload struct {
		Kind             string   `json:"kind"`
		ReadyClipIDs     []string `json:"readyClipIds"`
		PlaybackFailures []string `json:"playbackFailures"`
	}
	if err := json.Unmarshal(in.Action.Payload, &payload); err != nil || (payload.Kind != "" && payload.Kind != ActionKindReady) {
		return modes.Rejected(CodeInvalidPayload, "audio: invalid preparation report"), nil
	}
	pub, err := decodePublic(in.Public)
	if err != nil {
		return modes.Decision{}, err
	}
	if !onRoster(pub.Subjects, in.Subject.ID) {
		return modes.Rejected(CodeNotOnRoster, "audio: subject not on roster"), nil
	}
	st, err := decodeState(in.State)
	if err != nil {
		return modes.Decision{}, err
	}
	known := map[string]bool{}
	for _, c := range pub.Clips {
		known[c.ClipID] = true
	}
	seen := map[string]bool{}
	failed := map[string]bool{}
	for _, ids := range [][]string{payload.ReadyClipIDs, payload.PlaybackFailures} {
		for _, id := range ids {
			if !known[id] {
				return modes.Rejected(CodeUnknownClip, "audio: unknown preparation clip"), nil
			}
			if seen[id] {
				return modes.Rejected(CodeDuplicateClip, "audio: duplicate preparation clip"), nil
			}
			seen[id] = true
		}
	}
	if len(seen) != len(known) || len(known) == 0 {
		return modes.Rejected(CodeIncomplete, "audio: prepare every clip"), nil
	}
	for _, id := range st.PlaybackFailures {
		failed[id] = true
	}
	for _, id := range payload.PlaybackFailures {
		failed[id] = true
	}
	st.PlaybackFailures = []string{}
	for id := range failed {
		st.PlaybackFailures = append(st.PlaybackFailures, id)
	}
	sort.Strings(st.PlaybackFailures)
	st.Ready = len(failed) == 0
	// Reporting a fault must not keep that clip as an apparently usable answer.
	retained := []matchRecord{}
	for _, m := range st.Matches {
		if !failed[m.ClipID] {
			retained = append(retained, m)
		}
	}
	st.Matches = retained
	return modes.Accepted(modeutil.Marshal(struct {
		Ready            bool `json:"ready"`
		PlaybackFailures int  `json:"playbackFailures"`
	}{st.Ready, len(failed)}), modeutil.Marshal(st)), nil
}

// AllPrepared checks the prepared roster, not current room ready flags. Spectators
// do not block a round; a missing/disconnected subject does, until cancellation.
func AllPrepared(public json.RawMessage, states map[string]json.RawMessage) (bool, error) {
	pub, err := decodePublic(public)
	if err != nil {
		return false, err
	}
	if len(pub.Subjects) == 0 {
		return false, nil
	}
	for _, entry := range pub.Subjects {
		st, err := decodeState(states[entry.SubjectID])
		if err != nil {
			return false, err
		}
		if !st.Ready || len(st.PlaybackFailures) > 0 {
			return false, nil
		}
	}
	return true, nil
}
