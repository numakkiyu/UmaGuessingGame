package content

import "testing"

func TestBluffFallbackAdmission(t *testing.T) {
	valid := []string{"备用答案甲", "备用答案乙", "备用答案丙", "备用答案丁", "备用答案戊", "备用答案己", "备用答案庚", "备用答案辛"}
	if !validBluffFallbacks(valid, "真相") {
		t.Fatal("reviewed fixture rejected")
	}
	for _, change := range []string{"真相", "备用答案乙", "a", "ＡＢ"} {
		v := append([]string(nil), valid...)
		v[0] = change
		if change == "ＡＢ" {
			v[1] = "ab"
		}
		if validBluffFallbacks(v, "真相") {
			t.Fatal("invalid fallback admitted", change)
		}
	}
	if validBluffFallbacks(valid[:7], "真相") {
		t.Fatal("insufficient fallback pool")
	}
}
