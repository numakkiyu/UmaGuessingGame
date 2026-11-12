package content

import (
	"crypto/sha256"
	"encoding/hex"
)

// Private (secret) sections. These carry the answer material a round is graded
// against, plus the deterministic seeds used to build a round's question order.
// They are written under content/<version>/private/ only, never under public/ and
// never under a Vite public directory .
const (
	PrivateClassicAnswerPool  = "classic.answer-pool"
	PrivateRaceRebuildAnswers = "race-rebuild.answers"
	PrivateMemorySolution     = "memory.solution"
)

// privateClassicPool is the server-only classic answer set. The public section
// publishes the same cards so a client can search and compare, but the answer
// selection and the ordering seed stay here.
type privateClassicPool struct {
	Mode string `json:"mode"`
	// AnswerIDs is the eligible answer set. A round picks from this; the client
	// only ever learns the one answer at reveal time.
	AnswerIDs []string `json:"answerIds"`
	// AmbiguityGroups lists the excluded vectors so a referee can explain why a
	// character is not a valid competitive answer .
	AmbiguityGroups map[string][]string `json:"ambiguityGroups,omitempty"`
	// SeedNamespace is mixed into the round seed to derive the answer order.
	SeedNamespace string `json:"seedNamespace"`
}

// privateRaceAnswers carries the settled rank order per race so a round can be
// graded server-side without trusting a client-submitted ordering.
type privateRaceAnswers struct {
	Mode  string              `json:"mode"`
	Races []privateRaceAnswer `json:"races"`
}

type privateRaceAnswer struct {
	RaceID string `json:"raceId"`
	// Ranks preserves source finish numbers, including gaps and genuine ties.
	Ranks map[string]int `json:"ranks"`
	// Order is the character ids in ascending finish rank.
	Order []string `json:"order"`
	// Ties records rank groups that really share a rank.
	Ties map[string][]string `json:"ties,omitempty"`
	// Dropped lists starters excluded for a missing rank, with the reason, so a
	// referee can see that no renumbering happened .
	Dropped []string `json:"dropped,omitempty"`
}

// privateMemorySolution is the server-only pair mapping. The public section
// publishes pair ids and image paths only; the mapping from board slot to pair
// id is derived per round from the seed and never shipped to the client .
type privateMemorySolution struct {
	Mode string `json:"mode"`
	// PairIDs are the pair identities a board may use.
	PairIDs []string `json:"pairIds"`
	// PairLabels maps pair id to the readable character name. It is published ONLY
	// here, never in the public section, so the reveal can show a name while the
	// public payload cannot be mined for one .
	PairLabels map[string]string `json:"pairLabels"`
	// SeedNamespace is mixed into the round seed to shuffle the board.
	SeedNamespace string `json:"seedNamespace"`
}

// seedNamespace derives a stable namespace string from the mode and dataset
// version, so a round seed can be mixed with it deterministically .
func seedNamespace(mode, datasetVersion string) string {
	sum := sha256.Sum256([]byte(mode + "\x00" + datasetVersion + "\x00" + SchemaVersion))
	return hex.EncodeToString(sum[:8])
}

// PrivateSectionNames exposes the private section list for a server to load. It
// deliberately takes the admission list, not the pack, so a caller cannot read
// secret content by asking for names.
func PrivateSectionNames(admissions []ModeAdmission) []string {
	return privateSectionNames(admissions)
}
