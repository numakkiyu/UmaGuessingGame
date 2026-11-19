package httpapi

import (
	"encoding/json"
	"testing"

	"github.com/bhcn/umaguessinggame/internal/modes"
	"github.com/bhcn/umaguessinggame/internal/room"
)

func TestResultsKeepStoredSubjectKind(t *testing.T) {
	items := toResultDTOs([]room.StoredResult{{SubjectID: "team", SubjectKind: modes.SubjectTeam}, {SubjectID: "individual", SubjectKind: modes.SubjectPlayer}})
	if items[0].SubjectType != "team" || items[1].SubjectType != "player" {
		t.Fatalf("subject types: %+v", items)
	}
}

func TestResultsRestoreCompetitionRanksFromStoredScores(t *testing.T) {
	input := []room.StoredResult{
		{SubjectID: "third", ScoreUnits: 1100},
		{SubjectID: "first-b", ScoreUnits: 2200},
		{SubjectID: "zero", ScoreUnits: 0},
		{SubjectID: "first-a", ScoreUnits: 2200},
	}
	out := toResultDTOs(input)
	for i, want := range []int{3, 1, 4, 1} {
		if out[i].Rank != want || out[i].SubjectID != input[i].SubjectID || out[i].ScoreUnits != input[i].ScoreUnits {
			t.Fatalf("result %d = %+v; want unchanged subject/score and rank %d", i, out[i], want)
		}
	}
	if out := toResultDTOs(nil); out == nil || len(out) != 0 {
		t.Fatalf("empty results must remain an empty array, got %+v", out)
	}
}

func TestOutcomeReflectsSolved(t *testing.T) {
	t.Parallel()
	out := toResultDTOs([]room.StoredResult{{
		SubjectID:   "mem_a",
		ScoreUnits:  70,
		Explanation: json.RawMessage(`{"attempts":3,"solved":true,"failed":false}`),
	}})
	if out[0].Outcome != "won" {
		t.Fatalf("outcome = %q, want won", out[0].Outcome)
	}
}

func TestOutcomeReflectsFailure(t *testing.T) {
	t.Parallel()
	out := toResultDTOs([]room.StoredResult{{
		SubjectID:   "mem_b",
		Explanation: json.RawMessage(`{"attempts":8,"solved":false,"failed":true}`),
	}})
	if out[0].Outcome != "lost" {
		t.Fatalf("outcome = %q, want lost", out[0].Outcome)
	}
}

func TestOutcomeDefaultsWhenNoExplanation(t *testing.T) {
	t.Parallel()
	out := toResultDTOs([]room.StoredResult{{SubjectID: "mem_c", ScoreUnits: 10}})
	if out[0].Outcome != "completed" {
		t.Fatalf("outcome = %q, want completed", out[0].Outcome)
	}
}

// TestDetailsStayEmptyUntilModesProduceBreakdown 钉住「不编造分项」。
//
// 如果将来模式层真的给出分项计分，这条测试应当被主动改写，
// 而不是被绕过——它存在就是为了防止有人用 earnedUnits:0 假装有明细。
func TestDetailsStayEmptyUntilModesProduceBreakdown(t *testing.T) {
	t.Parallel()
	out := toResultDTOs([]room.StoredResult{{
		SubjectID:   "mem_a",
		Explanation: json.RawMessage(`{"attempts":3,"remainingWrong":5,"wrongBudget":8,"solved":true}`),
	}})
	if len(out[0].Details) != 0 {
		t.Fatalf("Details 有 %d 行；模式层没有分项单位时不应编造（earnedUnits 只能填 0，等于谎称不得分）",
			len(out[0].Details))
	}
}
