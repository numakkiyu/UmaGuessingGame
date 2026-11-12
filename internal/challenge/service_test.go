package challenge

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/bhcn/umaguessinggame/internal/deployment"
	"github.com/bhcn/umaguessinggame/internal/httpapi/problem"
	"github.com/bhcn/umaguessinggame/internal/session"
	"github.com/bhcn/umaguessinggame/internal/storage"
)

type testContent struct{}

func (testContent) Version() string { return "test-pack" }
func (testContent) Section(name string) (json.RawMessage, bool) {
	if name != "classic.characters" {
		return nil, false
	}
	return json.RawMessage(`{"cards":[{"id":"a","name":"甲","star":3,"surfaceGroup":["草地"],"distanceGroup":["中"],"runningStyleGroup":["先"],"sexType":"牡","g1Bracket":"0","g23Bracket":"0","schoolGrade":"中等部","dormitory":"栗东宿舍","imagePath":"/assets/avatars/a.webp"},{"id":"b","name":"乙","star":1,"surfaceGroup":["泥地"],"distanceGroup":["短"],"runningStyleGroup":["逃"],"sexType":"牝","g1Bracket":"5+","g23Bracket":"7+","schoolGrade":"高等部","dormitory":"美浦宿舍","imagePath":"/assets/avatars/b.webp"}]}`), true
}
func setup(t *testing.T) (*Service, string, string) {
	t.Helper()
	dir := t.TempDir()
	db, err := storage.Open(context.Background(), storage.Config{Dir: dir})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	keys, err := LoadKeys(filepath.Join(dir, "keys.json"))
	if err != nil {
		t.Fatal(err)
	}
	sessions := session.New(db, session.Config{})
	sess, err := sessions.Create(context.Background(), "训练员")
	if err != nil {
		t.Fatal(err)
	}
	other, err := sessions.Create(context.Background(), "另一位")
	if err != nil {
		t.Fatal(err)
	}
	s := New(db, testContent{}, keys, deployment.Policy{}, true)
	return s, sess.Session.ID, other.Session.ID
}
func issue(t *testing.T, s *Service, owner, key string) View {
	t.Helper()
	v, err := s.Create(context.Background(), owner, key, CreateInput{ModeID: "classic", ClientTimestamp: millis(s.now()), Nonce: "abcdefgh12345678", WrongBudget: 4})
	if err != nil {
		t.Fatal(err)
	}
	return v
}
func answerID(t *testing.T, s *Service, id string) string {
	t.Helper()
	var idAnswer string
	if err := s.db.Read().QueryRow(`SELECT answer_id FROM verified_challenges WHERE id=?`, id).Scan(&idAnswer); err != nil {
		t.Fatal(err)
	}
	return idAnswer
}
func code(err error) string {
	if err == nil {
		return ""
	}
	return problem.As(err).Code
}
func TestChallengeKeepsAnswerPrivateAndUsesNoRooms(t *testing.T) {
	s, owner, _ := setup(t)
	v := issue(t, s, owner, "create-one")
	if v.Answer != nil || v.Status != "active" || v.RemainingWrong != 4 {
		t.Fatalf("invalid initial projection: %#v", v)
	}
	if strings.Contains(v.Ticket, "answer") || strings.Contains(v.Ticket, owner) {
		t.Fatal("plaintext claims exposed")
	}
	for _, table := range []string{"rooms", "memberships", "outbox_events"} {
		var count int
		s.db.Read().QueryRow("SELECT COUNT(*) FROM " + table).Scan(&count)
		if count != 0 {
			t.Fatalf("allocated %s", table)
		}
	}
	correct := answerID(t, s, v.ChallengeID)
	wrong := "a"
	if correct == wrong {
		wrong = "b"
	}
	next, err := s.Guess(context.Background(), owner, v.ChallengeID, "guess-one", GuessInput{Ticket: v.Ticket, CharacterID: wrong, ExpectedRevision: 0})
	if err != nil {
		t.Fatal(err)
	}
	if next.RemainingWrong != 3 || next.Attempts != 1 || next.Answer != nil {
		t.Fatal("wrong feedback/secret projection")
	}
	if _, err := s.Guess(context.Background(), owner, v.ChallengeID, "guess-duplicate", GuessInput{Ticket: v.Ticket, CharacterID: wrong, ExpectedRevision: 1}); code(err) != "duplicate-guess" {
		t.Fatalf("duplicate not refused: %v", err)
	}
	solved, err := s.Guess(context.Background(), owner, v.ChallengeID, "guess-win", GuessInput{Ticket: v.Ticket, CharacterID: correct, ExpectedRevision: 1})
	if err != nil {
		t.Fatal(err)
	}
	if solved.Status != "solved" || solved.RemainingWrong != 3 || solved.ScoreUnits != 9200 || solved.Answer.CharacterID != correct {
		t.Fatalf("bad score: %#v", solved)
	}
	s.now = func() time.Time { return time.Now().Add(time.Hour) }
	replay, err := s.Guess(context.Background(), owner, v.ChallengeID, "guess-win", GuessInput{Ticket: v.Ticket, CharacterID: correct, ExpectedRevision: 1})
	if err != nil {
		t.Fatal(err)
	}
	a, _ := json.Marshal(solved)
	b, _ := json.Marshal(replay)
	if string(a) != string(b) {
		t.Fatal("accepted receipt changed after expiry")
	}
}
func TestTamperingIdentityExpiryAndRevision(t *testing.T) {
	s, owner, other := setup(t)
	v := issue(t, s, owner, "create-one")
	correct := answerID(t, s, v.ChallengeID)
	for _, tc := range []struct {
		name, session, id, ticket string
		revision                  int
		want                      string
	}{
		{"other identity", other, v.ChallengeID, v.Ticket, 0, "challenge-ticket-invalid"},
		{"other question", owner, "chl_other", v.Ticket, 0, "challenge-ticket-invalid"},
		{"modified ciphertext", owner, v.ChallengeID, v.Ticket[:len(v.Ticket)-2] + "xx", 0, "challenge-ticket-invalid"},
		{"wrong revision", owner, v.ChallengeID, v.Ticket, 1, "challenge-revision-conflict"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := s.Guess(context.Background(), tc.session, tc.id, "guess-key", GuessInput{Ticket: tc.ticket, CharacterID: correct, ExpectedRevision: tc.revision})
			if code(err) != tc.want {
				t.Fatalf("got %v want %s", err, tc.want)
			}
		})
	}
	if _, err := s.Get(context.Background(), other, v.ChallengeID); code(err) != "challenge-not-found" {
		t.Fatal("other identity can read challenge")
	}
	s.now = func() time.Time { return time.Now().Add(time.Hour) }
	if _, err := s.Guess(context.Background(), owner, v.ChallengeID, "guess-expired", GuessInput{Ticket: v.Ticket, CharacterID: correct}); code(err) != "challenge-expired" {
		t.Fatal(err)
	}
	expired, err := s.Get(context.Background(), owner, v.ChallengeID)
	if err != nil || expired.Status != "expired" || expired.ScoreUnits != 0 || expired.Answer == nil {
		t.Fatal("expiry projection invalid")
	}
}
func TestConcurrentCreationAndGuessReplayIsAtomic(t *testing.T) {
	s, owner, _ := setup(t)
	now := millis(s.now())
	input := CreateInput{ModeID: "classic", ClientTimestamp: now, Nonce: "abcdefgh12345678", WrongBudget: 4}
	var wg sync.WaitGroup
	views := make(chan View, 10)
	errs := make(chan error, 10)
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			v, e := s.Create(context.Background(), owner, "same-create", input)
			views <- v
			errs <- e
		}()
	}
	wg.Wait()
	close(views)
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	var v View
	for got := range views {
		if v.ChallengeID != "" && v.ChallengeID != got.ChallengeID {
			t.Fatal("duplicate allocation")
		}
		v = got
	}
	correct := answerID(t, s, v.ChallengeID)
	errs = make(chan error, 10)
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, e := s.Guess(context.Background(), owner, v.ChallengeID, "same-guess", GuessInput{Ticket: v.Ticket, CharacterID: correct})
			errs <- e
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	got, err := s.Get(context.Background(), owner, v.ChallengeID)
	if err != nil || got.Attempts != 1 || got.ScoreUnits != 10000 {
		t.Fatal("concurrent retry changed score")
	}
	if _, err := s.Guess(context.Background(), owner, v.ChallengeID, "same-guess", GuessInput{Ticket: v.Ticket, CharacterID: "other"}); code(err) != "idempotency-payload-conflict" {
		t.Fatal("key reused for different payload")
	}
}
func TestLimitsAndPolicyOnlyBlockNewChallenges(t *testing.T) {
	s, owner, _ := setup(t)
	v := issue(t, s, owner, "create-one")
	issue(t, s, owner, "create-two")
	issue(t, s, owner, "create-three")
	if _, err := s.Create(context.Background(), owner, "create-four", CreateInput{ModeID: "classic", ClientTimestamp: millis(s.now()), Nonce: "abcdefgh12345678", WrongBudget: 4}); code(err) != "rate-limited" {
		t.Fatalf("active limit missing: %v", err)
	}
	s.policy = deployment.Policy{Forms: map[string]bool{"verified": false}}
	if _, err := s.Create(context.Background(), owner, "create-five", CreateInput{ModeID: "classic", ClientTimestamp: millis(s.now()), Nonce: "abcdefgh12345678", WrongBudget: 4}); code(err) != "play-disabled" {
		t.Fatal("policy bypass")
	}
	if _, err := s.Finish(context.Background(), owner, v.ChallengeID, "finish-one", FinishInput{Ticket: v.Ticket}); err != nil {
		t.Fatal("disabled new creation broke issued challenge", err)
	}
}
func TestPersistentKeyringAndRotation(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "keys.json")
	keys, err := LoadKeys(path)
	if err != nil {
		t.Fatal(err)
	}
	original, _ := os.ReadFile(path)
	ticket, err := keys.seal(ticketClaims{ID: "question", Session: "owner"})
	if err != nil {
		t.Fatal(err)
	}
	restored, err := LoadKeys(path)
	if err != nil {
		t.Fatal(err)
	}
	if c, err := restored.open(ticket); err != nil || c.ID != "question" {
		t.Fatal("restart invalidated ticket")
	}
	raw := make([]byte, 32)
	rand.Read(raw)
	restored.Active = "rotated"
	restored.Keys["rotated"] = base64.RawURLEncoding.EncodeToString(raw)
	changed, _ := json.Marshal(restored)
	os.WriteFile(path, changed, 0600)
	rotated, err := LoadKeys(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := rotated.open(ticket); err != nil {
		t.Fatal("rotation dropped retained key")
	}
	newTicket, _ := rotated.seal(ticketClaims{ID: "next"})
	if !strings.HasPrefix(newTicket, "v2.rotated.") {
		t.Fatal("active key not used")
	}
	if _, err := keys.open(newTicket); err == nil {
		t.Fatal("unknown key accepted")
	}
	os.WriteFile(path, []byte("broken"), 0600)
	if _, err := LoadKeys(path); err == nil {
		t.Fatal("corrupt keyring silently replaced")
	}
	after, _ := os.ReadFile(path)
	if string(after) != "broken" || string(original) == "broken" {
		t.Fatal("keyring overwritten")
	}
}
