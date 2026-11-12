package room_test

import (
	"context"
	"encoding/json"
	"reflect"
	"sync"
	"testing"

	"github.com/bhcn/umaguessinggame/internal/httpapi/problem"
	"github.com/bhcn/umaguessinggame/internal/modes"
	"github.com/bhcn/umaguessinggame/internal/room"
)

func TestMemoryCommandSurvivesHandoffAndConcurrentConfirmation(t *testing.T) {
	f := newTeamRound(t, "memory")
	f.submit(t, "a1", "memory.flip", `{"cardId":"c0"}`)
	in := room.SubmissionInput{PhaseVersion: 1, SubmissionRevision: 1, ModeAction: modes.ModeAction{Kind: "memory.flip", Payload: json.RawMessage(`{"cardId":"c1"}`)}, Command: &room.CreationCommand{Key: "memory-recovery-0000001", Payload: []byte(`{"phaseVersion":1,"submissionRevision":1,"controlGeneration":0,"modeAction":{"kind":"memory.flip","cardId":"c1"}}`)}}
	const count = 4
	results := make([]room.SubmissionReceipt, count)
	errors := make([]error, count)
	var wg sync.WaitGroup
	for i := range count {
		wg.Add(1)
		go func() {
			defer wg.Done()
			results[i], errors[i] = f.svc.CreateSubmission(context.Background(), f.actor("a1"), f.roundID, in)
		}()
	}
	wg.Wait()
	for i := range count {
		if errors[i] != nil {
			t.Fatal(errors[i])
		}
		if !reflect.DeepEqual(results[0], results[i]) {
			t.Fatal("confirmation changed the original result")
		}
	}
	f.control(t, "mem_a", "a2", 1)
	if v := f.view(t, "a2"); v.SubmissionRevision != 2 {
		t.Fatalf("replayed a second flip: %d", v.SubmissionRevision)
	}
	// Both the phase and the representative have changed. Original confirmation
	// remains available, but changed payloads cannot reuse it.
	f.exec(t, `UPDATE rounds SET status='locked',phase_version=phase_version+1 WHERE id=?`, f.roundID)
	got, err := f.svc.CreateSubmission(context.Background(), f.actor("a1"), f.roundID, in)
	if err != nil || !reflect.DeepEqual(got, results[0]) {
		t.Fatalf("after phase/holder change: %+v %v", got, err)
	}
	in.Command = &room.CreationCommand{Key: in.Command.Key, Payload: []byte(`{"different":true}`)}
	if _, err := f.svc.CreateSubmission(context.Background(), f.actor("a1"), f.roundID, in); err == nil || problem.As(err).Code != "idempotency-key-reused" {
		t.Fatalf("changed command: %v", err)
	}
}

func TestMemoryCommandReceiptFailureRollsBackTheFlip(t *testing.T) {
	f := newTeamRound(t, "memory")
	f.exec(t, `CREATE TRIGGER fail_gameplay_receipt BEFORE INSERT ON idempotency_receipts WHEN NEW.route_scope LIKE 'gameplay:%' BEGIN SELECT RAISE(ABORT,'receipt storage failed'); END`)
	in := room.SubmissionInput{PhaseVersion: 1, ModeAction: modes.ModeAction{Kind: "memory.flip", Payload: json.RawMessage(`{"cardId":"c0"}`)}, Command: &room.CreationCommand{Key: "memory-rollback-000001", Payload: []byte(`{"cardId":"c0"}`)}}
	if _, err := f.svc.CreateSubmission(context.Background(), f.actor("a1"), f.roundID, in); err == nil {
		t.Fatal("mutation succeeded without its recovery record")
	}
	if v := f.view(t, "a1"); v.SubmissionRevision != 0 {
		t.Fatal("failed receipt left the flip committed")
	}
	f.exec(t, `DROP TRIGGER fail_gameplay_receipt`)
	if _, err := f.svc.CreateSubmission(context.Background(), f.actor("a1"), f.roundID, in); err != nil {
		t.Fatal(err)
	}
}
