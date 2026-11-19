package httpapi

import (
	"strings"
	"testing"
)

func TestRoomSnapshotFallsBackToLatestRound(t *testing.T) {
	t.Parallel()
	src := readSource(t, "internal/httpapi/handlers_room.go")

	// 快照里必须两段都在：先活回合，再回落
	if !strings.Contains(src, "CurrentRoundSummary") {
		t.Fatal("快照没有用 CurrentRoundSummary")
	}
	if !strings.Contains(src, "LatestRoundSummary") {
		t.Fatal("快照没有回落到 LatestRoundSummary；结算后玩家会看不到这一局")
	}
	// 回落必须发生在 else 分支（活回合为 nil 时）
	idx := strings.Index(src, "} else if latest, lerr := s.rooms.LatestRoundSummary")
	if idx < 0 {
		t.Fatal("回落不在 else-if 分支：应当只在没有活回合时才回落")
	}
}

func TestCreateRoundStillUsesLiveOnly(t *testing.T) {
	t.Parallel()
	src := readSource(t, "internal/room/rounds.go")

	// 准入必须继续用 currentRoundQ（只认活回合），否则会在已结算局上再开一局
	if !strings.Contains(src, "s.currentRoundQ(ctx, tx, room.ID)") {
		t.Fatal("CreateRound 不再用 currentRoundQ：可能允许在已结算局上开新局")
	}
	if strings.Contains(src, "LatestRoundSummary") {
		t.Fatal("CreateRound 不应使用 LatestRoundSummary：那会拒绝一切新局")
	}
}

func TestCurrentRoundQueryExcludesSettled(t *testing.T) {
	t.Parallel()
	src := readSource(t, "internal/room/rooms.go")

	// 活回合查询必须继续排除 settled/cancelled
	if !strings.Contains(src, "status NOT IN ('settled','cancelled')") {
		t.Fatal("currentRoundQ 不再排除已结算回合")
	}
}
