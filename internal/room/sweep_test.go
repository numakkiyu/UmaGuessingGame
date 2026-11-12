package room_test

import (
	"context"
	"testing"
	"time"
)

func TestSweepExpiredPicksUpDueRound(t *testing.T) {
	t.Parallel()
	db, svc, clock := newStrategyService(t)
	ctx := context.Background()

	now := *clock
	roomID, roundID := seedStrategyRound(t, db, now, now.Add(-time.Second))

	n, err := svc.SweepExpired(ctx)
	if err != nil {
		t.Fatalf("sweep: %v", err)
	}
	if n < 1 {
		t.Fatalf("deadline 已过的回合应被扫出，实际扫出 %d 条（round %s）", n, roundID)
	}

	// 策略赛是回合制：到点由 ExpireTurn 推进回合，不换阶段。
	// 所以这里断言「回合推进了」，不是「阶段变了」——后者是我一开始写错的。
	r, err := svc.CurrentRoundSummary(ctx, roomID)
	if err != nil {
		t.Fatalf("load round: %v", err)
	}
	if r == nil {
		t.Fatalf("到点后回合消失了（round %s）", roundID)
	}
	if r.DeadlineAt == nil || !r.DeadlineAt.After(now) {
		t.Fatalf("到点后 deadline 应被刷新到未来，实际 %v", r.DeadlineAt)
	}
	assertLatestDeadlineVersion(t, db, roomID)
}

func TestSweepExpiredSkipsPausedRound(t *testing.T) {
	t.Parallel()
	db, svc, clock := newStrategyService(t)
	ctx := context.Background()

	now := *clock
	roomID, roundID := seedStrategyRound(t, db, now, now.Add(-time.Second))

	if _, err := db.Write().ExecContext(ctx,
		`UPDATE rounds SET paused_at = ? WHERE id = ?`, now.UnixMilli(), roundID); err != nil {
		t.Fatalf("pause: %v", err)
	}

	if _, err := svc.SweepExpired(ctx); err != nil {
		t.Fatalf("sweep: %v", err)
	}
	r, err := svc.CurrentRoundSummary(ctx, roomID)
	if err != nil {
		t.Fatalf("load round: %v", err)
	}
	if r != nil && r.Status != "collecting" {
		t.Fatalf("暂停中的回合不该被收卷，实际状态 %q", r.Status)
	}
}
