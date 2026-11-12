package room_test

import (
	"encoding/json"
	"testing"

	"github.com/bhcn/umaguessinggame/internal/modes"
	"github.com/bhcn/umaguessinggame/internal/modes/classic"
	"github.com/bhcn/umaguessinggame/internal/room"
)

// practiceOnlyIndex serves just enough content for the classic practice pack.
type practiceOnlyIndex struct{}

func (practiceOnlyIndex) Section(name string) (json.RawMessage, bool) {
	if name != "classic.characters" {
		return nil, false
	}
	return json.RawMessage(`{"cards":[
		{"id":"a","name":"甲","star":3,"surfaceGroup":["草地"],"imagePath":"/assets/avatars/a.webp"},
		{"id":"b","name":"乙","star":2,"surfaceGroup":["泥地"],"imagePath":"/assets/avatars/b.webp"}
	]}`), true
}
func (practiceOnlyIndex) Version() string        { return "practice-test-pack" }
func (practiceOnlyIndex) DatasetVersion() string { return "practice-test-data" }
func (practiceOnlyIndex) AssetBaseURL() string   { return "/assets" }
func (practiceOnlyIndex) Character(string) (room.CharacterCard, bool) {
	return room.CharacterCard{}, false
}
func (practiceOnlyIndex) Characters(string, int, string) ([]room.CharacterCard, string, error) {
	return nil, "", nil
}
func (practiceOnlyIndex) PathNeighbors(string) ([]room.Neighbor, bool) { return nil, false }
func (practiceOnlyIndex) ModeAdmission(id string) (room.ModeAdmission, bool) {
	return room.ModeAdmission{Mode: id, Available: true, QuestionCount: 2}, id == classic.ModeID
}

// TestPracticePackWritesNothingToStorage is the boundary test: generating practice
// content must leave the database untouched, so no standings query can ever see a
// practice result.
func TestPracticePackWritesNothingToStorage(t *testing.T) {
	db := newDB(t)
	reg, err := modes.NewRegistry(classic.New())
	if err != nil {
		t.Fatal(err)
	}
	svc := room.New(db, room.Config{}, room.Options{Modes: reg, Content: practiceOnlyIndex{}})
	t.Cleanup(svc.Close)

	count := func(table string) int {
		t.Helper()
		var n int
		if err := db.Read().QueryRow(`SELECT COUNT(*) FROM ` + table).Scan(&n); err != nil {
			t.Fatalf("count %s: %v", table, err)
		}
		return n
	}

	// Tables a practice result could contaminate if the path ever wrote.
	tables := []string{"rounds", "memberships", "rooms", "sessions"}
	before := map[string]int{}
	for _, table := range tables {
		before[table] = count(table)
	}

	// Ask for the pack several times: a cache that wrote a row on first build would
	// still be caught, because we compare against the pre-call snapshot.
	for i := 0; i < 3; i++ {
		pack, perr := svc.ClassicPracticePack()
		if perr != nil {
			t.Fatalf("practice pack %d: %v", i, perr)
		}
		if pack.ContentJSON == "" {
			t.Fatalf("practice pack %d is empty", i)
		}
	}

	for _, table := range tables {
		if after := count(table); after != before[table] {
			t.Fatalf("generating practice content wrote to %s: %d -> %d; practice must "+
				"never create rows that standings could read", table, before[table], after)
		}
	}

	// And there is no standings surface for a practice result: standings are
	// event-scoped (event_segments), and nothing was created there either.
	var segments int
	if err := db.Read().QueryRow(`SELECT COUNT(*) FROM event_segments`).Scan(&segments); err != nil {
		t.Fatalf("count event segments: %v", err)
	}
	if segments != 0 {
		t.Fatalf("practice run created %d event segments; standings need a segment, "+
			"so practice could never be ranked — it must create none", segments)
	}
}

// TestPracticePackPublishesAnswersOnlyForLocalGrading documents the deliberate
// trade-off: the pack DOES carry the nine fields, because grading happens in the
// browser. This test exists so nobody "fixes" that by removing the fields without
// also changing the grading model — and so the coupling to "practice never enters
// standings" is visible in one place.
func TestPracticePackPublishesAnswersOnlyForLocalGrading(t *testing.T) {
	reg, err := modes.NewRegistry(classic.New())
	if err != nil {
		t.Fatal(err)
	}
	svc := room.New(nil, room.Config{}, room.Options{Modes: reg, Content: practiceOnlyIndex{}})
	t.Cleanup(svc.Close)

	pack, err := svc.ClassicPracticePack()
	if err != nil {
		t.Fatalf("practice pack: %v", err)
	}

	var doc struct {
		Cards []struct {
			CharacterID string   `json:"characterId"`
			Star        int      `json:"star"`
			Surface     []string `json:"surface"`
			Distance    []string `json:"distance"`
			Sex         string   `json:"sex"`
			Grade       string   `json:"grade"`
			Dormitory   string   `json:"dormitory"`
			// AnswerEligible marks cards whose nine fields are unambiguous, so the
			// local grader can be sure a "wrong" verdict is really wrong.
			AnswerEligible bool `json:"answerEligible"`
		} `json:"characters"`
	}
	if err := json.Unmarshal([]byte(pack.ContentJSON), &doc); err != nil {
		t.Fatalf("decode practice content: %v", err)
	}
	if len(doc.Cards) == 0 {
		t.Fatalf("practice pack carries no cards")
	}
	// The answer-bearing field must be present — that is what makes local grading
	// possible. If this ever disappears, local practice cannot judge anything.
	if doc.Cards[0].Star == 0 && doc.Cards[0].Sex == "" && len(doc.Cards[0].Surface) == 0 {
		t.Fatalf("practice pack carries no answer fields; local grading would be "+
			"impossible (cards: %+v)", doc.Cards[0])
	}
	t.Logf("practice pack publishes answers for local grading: %d cards, "+
		"first has star=%d surface=%v sex=%q — this is safe ONLY because practice "+
		"never enters standings", len(doc.Cards), doc.Cards[0].Star,
		doc.Cards[0].Surface, doc.Cards[0].Sex)
}
