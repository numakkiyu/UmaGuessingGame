package room

import (
	"encoding/json"
	"testing"
	"time"
)

func TestCollectingWindowReadsNestedDuration(t *testing.T) {
	t.Parallel()
	s := &Service{cfg: Config{DefaultRoundDuration: 5 * time.Minute}}
	r := Round{
		ModeID: "not-a-registered-mode", // 跳过 mode.RoundWindow 分支
		Config: json.RawMessage(`{"modeId":"classic","config":{"durationSeconds":3}}`),
	}
	if got := s.collectingWindowMS(r); got != 3000 {
		t.Fatalf("嵌套 config.durationSeconds=3 应得 3000ms，实际 %d（回退成默认窗了）", got)
	}
}

func TestCollectingWindowReadsTopLevelDuration(t *testing.T) {
	t.Parallel()
	s := &Service{cfg: Config{DefaultRoundDuration: 5 * time.Minute}}
	r := Round{
		ModeID: "not-a-registered-mode",
		Config: json.RawMessage(`{"durationSeconds":7}`),
	}
	if got := s.collectingWindowMS(r); got != 7000 {
		t.Fatalf("顶层 durationSeconds=7 应得 7000ms，实际 %d", got)
	}
}

func TestCollectingWindowFallsBackWhenUnset(t *testing.T) {
	t.Parallel()
	s := &Service{cfg: Config{DefaultRoundDuration: 5 * time.Minute}}
	r := Round{ModeID: "not-a-registered-mode", Config: json.RawMessage(`{}`)}
	if got := s.collectingWindowMS(r); got != int64((5 * time.Minute).Milliseconds()) {
		t.Fatalf("没配 durationSeconds 时应回退默认窗，实际 %d", got)
	}
}
