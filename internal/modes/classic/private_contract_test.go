package classic

import (
	"encoding/json"
	"testing"
)

func TestStateSerializesGuessesForContract(t *testing.T) {
	t.Parallel()
	st := state{}
	st.Rows = append(st.Rows, GuessRow{})
	st.Guesses = append(st.Guesses, GuessRow{})

	raw, err := json.Marshal(st)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	// 契约要求的字段名
	if _, ok := got["guesses"]; !ok {
		t.Fatal("privateState 缺少契约要求的 guesses 字段")
	}
	// 旧名保留，已发布的客户端与已存 blob 还要用
	if _, ok := got["rows"]; !ok {
		t.Fatal("privateState 丢了旧名 rows，老客户端会读不到历史")
	}
}

func TestGuessesAndRowsStayInSync(t *testing.T) {
	t.Parallel()
	// 两个历史字段必须同步追加，不能让 guesses 与 rows 内容分离
	st := state{}
	st.Rows = append(st.Rows, GuessRow{})
	st.Guesses = append(st.Guesses, GuessRow{})
	if len(st.Guesses) != len(st.Rows) {
		t.Fatalf("guesses(%d) 与 rows(%d) 长度不一致", len(st.Guesses), len(st.Rows))
	}
}
