package room_test

import (
	"context"
	"fmt"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/bhcn/umaguessinggame/internal/httpapi/problem"
	"github.com/bhcn/umaguessinggame/internal/modes"
	"github.com/bhcn/umaguessinggame/internal/room"
)

func seedSweepReceipts(t *testing.T, f *teamRound, count int, scope func(int) string, expires int64) {
	t.Helper()
	tx, err := f.db.Write().BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	for i := range count {
		if err := room.SaveReceipt(context.Background(), tx, "ses_a1", scope(i), fmt.Sprintf("cleanup-command-%08d", i), []byte(`{}`), 200, map[string]bool{"ok": true}, expires-1000, 1000); err != nil {
			t.Fatal(err)
		}
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
}

func countSweepReceipts(t *testing.T, f *teamRound) int {
	t.Helper()
	var n int
	if err := f.db.Read().QueryRow(`SELECT count(*) FROM idempotency_receipts`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestReceiptSweepPreservesLiveAndUnknownReplayGuards(t *testing.T) {
	for _, phase := range []string{"prepared", "briefing", "collecting", "locked", "revealed", "settled", "cancelled"} {
		t.Run(phase, func(t *testing.T) {
			f := newTeamRound(t, "memory")
			f.exec(t, `UPDATE rounds SET status=? WHERE id=?`, phase, f.roundID)
			// Pausing an active phase must not make it eligible for cleanup.
			if phase == "collecting" {
				f.exec(t, `UPDATE rounds SET paused_at=? WHERE id=?`, f.now.UnixMilli(), f.roundID)
			}
			scopes := []string{
				"renameSession", "gameplay:createSubmission:" + f.roundID,
				"gameplay:createTrade:" + f.roundID, "gameplay:respondToTrade:" + f.roundID + ":trade-1",
				"gameplay:futureCommand:" + f.roundID, "gameplay:createSubmission:missing-round",
				"gameplay:createSubmission:" + f.roundID + ":extra", "gameplay:respondToTrade:" + f.roundID,
				"gameplay:respondToTrade:" + f.roundID + ":trade-1:extra", "gameplay:createTrade:",
			}
			seedSweepReceipts(t, f, len(scopes), func(i int) string { return scopes[i] }, f.now.UnixMilli())
			seedSweepReceipts(t, f, 1, func(int) string { return "stillValid" }, f.now.Add(time.Hour).UnixMilli())
			got, err := f.svc.SweepReceipts(context.Background())
			wantDeleted := 1
			if phase == "settled" || phase == "cancelled" {
				wantDeleted = 4
			}
			if err != nil || got.Examined != len(scopes) || got.Deleted != wantDeleted {
				t.Fatalf("cleanup=%+v error=%v", got, err)
			}
			if n := countSweepReceipts(t, f); n != len(scopes)+1-wantDeleted {
				t.Fatalf("remaining=%d", n)
			}
			if rec, err := f.svc.LookupReceipt(context.Background(), "ses_a1", "stillValid", "cleanup-command-00000000"); err != nil || rec == nil {
				t.Fatalf("valid receipt lost: %v", err)
			}
		})
	}
}

func TestReceiptSweepBoundedCursorPassesRetainedRowsAndRevisitsThem(t *testing.T) {
	f := newTeamRound(t, "memory")
	const batch = room.ReceiptSweepBatchSize
	seedSweepReceipts(t, f, 2*batch+13, func(i int) string {
		if i < batch {
			return "gameplay:createSubmission:" + f.roundID
		}
		return "renameSession"
	}, f.now.UnixMilli())
	for _, want := range []room.ReceiptSweepResult{{Examined: batch}, {Examined: batch, Deleted: batch}, {Examined: 13, Deleted: 13}} {
		if got, err := f.svc.SweepReceipts(context.Background()); err != nil || got != want {
			t.Fatalf("got=%+v want=%+v err=%v", got, want, err)
		}
	}
	if n := countSweepReceipts(t, f); n != batch {
		t.Fatalf("active guards remaining=%d", n)
	}
	f.exec(t, `UPDATE rounds SET status='settled' WHERE id=?`, f.roundID)
	if got, err := f.svc.SweepReceipts(context.Background()); err != nil || got.Deleted != batch {
		t.Fatalf("retained rows were starved: %+v %v", got, err)
	}
}

func TestReceiptSweepRollbackCancellationAndRestart(t *testing.T) {
	f := newTeamRound(t, "memory")
	const count = room.ReceiptSweepBatchSize + 7
	seedSweepReceipts(t, f, count, func(int) string { return "renameSession" }, f.now.UnixMilli())
	f.exec(t, `CREATE TRIGGER fail_receipt_cleanup BEFORE DELETE ON idempotency_receipts WHEN OLD.idem_key='cleanup-command-00000001' BEGIN SELECT RAISE(ABORT,'cleanup failed'); END`)
	if got, err := f.svc.SweepReceipts(context.Background()); err == nil || got.Deleted != 0 {
		t.Fatalf("expected rollback: %+v %v", got, err)
	}
	if n := countSweepReceipts(t, f); n != count {
		t.Fatal("partial cleanup committed")
	}
	f.exec(t, `DROP TRIGGER fail_receipt_cleanup`)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := f.svc.SweepReceipts(ctx); err == nil {
		t.Fatal("cancelled cleanup succeeded")
	}
	if got, err := f.svc.SweepReceipts(context.Background()); err != nil || got.Deleted != room.ReceiptSweepBatchSize {
		t.Fatalf("failed transaction advanced cursor: %+v %v", got, err)
	}
	restarted := room.New(f.db, room.Config{}, room.Options{})
	defer restarted.Close()
	restarted.SetClock(func() time.Time { return f.now })
	if got, err := restarted.SweepReceipts(context.Background()); err != nil || got.Deleted != 7 {
		t.Fatalf("restart cleanup: %+v %v", got, err)
	}
}

func TestReceiptSweepCrossesTimestampGroupsAndRevisitsLateInsertions(t *testing.T) {
	f := newTeamRound(t, "memory")
	const batch = room.ReceiptSweepBatchSize
	seedSweepReceipts(t, f, batch-3, func(int) string { return "firstGroup" }, f.now.Add(-time.Second).UnixMilli())
	seedSweepReceipts(t, f, 10, func(int) string { return "secondGroup" }, f.now.UnixMilli())
	if got, err := f.svc.SweepReceipts(context.Background()); err != nil || got.Examined != batch || got.Deleted != batch {
		t.Fatalf("first batch: %+v %v", got, err)
	}
	// This write precedes the cursor and must be visited after the cursor wraps.
	seedSweepReceipts(t, f, 1, func(int) string { return "lateInsert" }, f.now.Add(-2*time.Second).UnixMilli())
	if got, err := f.svc.SweepReceipts(context.Background()); err != nil || got.Deleted != 7 {
		t.Fatalf("timestamp tie remainder: %+v %v", got, err)
	}
	if got, err := f.svc.SweepReceipts(context.Background()); err != nil || got.Deleted != 1 {
		t.Fatalf("late insert starved: %+v %v", got, err)
	}
}

func TestReceiptSweepCannotReviveExpiredMemoryOrTerminalTradeCommands(t *testing.T) {
	f := newTeamRound(t, "memory")
	in := room.SubmissionInput{PhaseVersion: 1, ModeAction: modes.ModeAction{Kind: "memory.flip", Payload: []byte(`{"cardId":"c0"}`)}, Command: &room.CreationCommand{Key: "memory-sweep-0000001", Payload: []byte(`{"cardId":"c0"}`)}}
	if _, err := f.svc.CreateSubmission(context.Background(), f.actor("a1"), f.roundID, in); err != nil {
		t.Fatal(err)
	}
	f.exec(t, `UPDATE idempotency_receipts SET expires_at=?`, f.now.UnixMilli())
	if got, err := f.svc.SweepReceipts(context.Background()); err != nil || got.Deleted != 0 {
		t.Fatalf("active guard deleted: %+v %v", got, err)
	}
	if _, err := f.svc.CreateSubmission(context.Background(), f.actor("a1"), f.roundID, in); err == nil || problem.As(err).Code != "confirmation-expired" {
		t.Fatalf("expired live action replay: %v", err)
	}
	for _, phase := range []string{"settled", "cancelled"} {
		f.exec(t, `UPDATE rounds SET status=? WHERE id=?`, phase, f.roundID)
		if _, err := f.svc.SweepReceipts(context.Background()); err != nil {
			t.Fatal(err)
		}
		if _, err := f.svc.CreateSubmission(context.Background(), f.actor("a1"), f.roundID, in); err == nil || problem.As(err).Code != "round-not-collecting" {
			t.Fatalf("terminal memory action replay: %v", err)
		}
		f.exec(t, `UPDATE rounds SET mode_id='trade' WHERE id=?`, f.roundID)
		seedSweepReceipts(t, f, 2, func(i int) string {
			if i == 0 {
				return "gameplay:createTrade:" + f.roundID
			}
			return "gameplay:respondToTrade:" + f.roundID + ":trade-1"
		}, f.now.UnixMilli())
		if got, err := f.svc.SweepReceipts(context.Background()); err != nil || got.Deleted != 2 {
			t.Fatalf("terminal trade cleanup: %+v %v", got, err)
		}
		command := &room.CreationCommand{Key: "cleanup-command-00000000", Payload: []byte(`{}`)}
		if _, err := f.svc.CreateTrade(context.Background(), f.actor("a1"), f.roundID, room.TradeInput{Command: command}); err == nil || problem.As(err).Code != "round-not-collecting" {
			t.Fatalf("terminal proposal replay: %v", err)
		}
		command.Key = "cleanup-command-00000001"
		if _, err := f.svc.RespondToTrade(context.Background(), f.actor("a1"), f.roundID, "trade-1", true, command); err == nil || problem.As(err).Code != "round-not-collecting" {
			t.Fatalf("terminal acceptance replay: %v", err)
		}
		f.exec(t, `UPDATE rounds SET mode_id='memory' WHERE id=?`, f.roundID)
	}
	if n := countSweepReceipts(t, f); n != 0 {
		t.Fatalf("terminal command wrote new receipt: %d", n)
	}
	var submissions int
	if err := f.db.Read().QueryRow(`SELECT count(*) FROM submissions WHERE round_id=?`, f.roundID).Scan(&submissions); err != nil || submissions != 1 {
		t.Fatalf("original flip changed: count=%d err=%v", submissions, err)
	}
}

func TestReceiptSweepConcurrentConfirmationPreservesOriginalResult(t *testing.T) {
	f := newTeamRound(t, "memory")
	in := room.SubmissionInput{PhaseVersion: 1, ModeAction: modes.ModeAction{Kind: "memory.flip", Payload: []byte(`{"cardId":"c0"}`)}, Command: &room.CreationCommand{Key: "memory-sweep-concurrent", Payload: []byte(`{"cardId":"c0"}`)}}
	want, err := f.svc.CreateSubmission(context.Background(), f.actor("a1"), f.roundID, in)
	if err != nil {
		t.Fatal(err)
	}
	seedSweepReceipts(t, f, 2*room.ReceiptSweepBatchSize, func(int) string { return "renameSession" }, f.now.UnixMilli())
	var wg sync.WaitGroup
	for range 6 {
		wg.Add(2)
		go func() {
			defer wg.Done()
			if _, err := f.svc.SweepReceipts(context.Background()); err != nil {
				t.Error(err)
			}
		}()
		go func() {
			defer wg.Done()
			got, err := f.svc.CreateSubmission(context.Background(), f.actor("a1"), f.roundID, in)
			if err != nil || !reflect.DeepEqual(got, want) {
				t.Errorf("confirmation changed: %+v %v", got, err)
			}
		}()
	}
	wg.Wait()
	if n := countSweepReceipts(t, f); n != 1 {
		t.Fatalf("remaining receipts=%d", n)
	}
}

func TestReceiptSweepRunnerStartsReportsFailureAndStops(t *testing.T) {
	f := newTeamRound(t, "memory")
	seedSweepReceipts(t, f, 1, func(int) string { return "renameSession" }, f.now.UnixMilli())
	f.exec(t, `CREATE TRIGGER fail_receipt_cleanup BEFORE DELETE ON idempotency_receipts BEGIN SELECT RAISE(ABORT,'cleanup failed'); END`)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	errors := make(chan error, 1)
	done := make(chan struct{})
	go func() {
		defer close(done)
		f.svc.RunReceiptSweeper(ctx, func(err error) { errors <- err; cancel() })
	}()
	select {
	case err := <-errors:
		if err == nil {
			t.Fatal("nil failure")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("startup cleanup did not report failure")
	}
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("runner did not stop")
	}
	if n := countSweepReceipts(t, f); n != 1 {
		t.Fatal("failed startup cleanup deleted data")
	}
	f.exec(t, `DROP TRIGGER fail_receipt_cleanup`)
	ctx, cancelSuccess := context.WithCancel(context.Background())
	defer cancelSuccess()
	doneSuccess := make(chan struct{})
	go func() {
		defer close(doneSuccess)
		f.svc.RunReceiptSweeper(ctx, nil)
	}()
	ticker := time.NewTicker(time.Millisecond)
	defer ticker.Stop()
	timeout := time.NewTimer(3 * time.Second)
	defer timeout.Stop()
	for countSweepReceipts(t, f) != 0 {
		select {
		case <-ticker.C:
		case <-timeout.C:
			t.Fatal("startup cleanup did not run")
		}
	}
	cancelSuccess()
	select {
	case <-doneSuccess:
	case <-time.After(3 * time.Second):
		t.Fatal("successful runner did not stop")
	}
}
