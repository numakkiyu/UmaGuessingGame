package classic

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestDefaultWinPolicyFollowsVariant(t *testing.T) {
	t.Parallel()
	cases := []struct {
		variant string
		want    WinPolicy
	}{
		{string(VariantSingle), WinPolicySolo},
		{string(VariantFriend), WinPolicyFirstCorrect},
		{string(VariantPublic), WinPolicyFirstCorrect},
	}
	for _, c := range cases {
		raw, err := json.Marshal(map[string]any{"variant": c.variant})
		if err != nil {
			t.Fatalf("%s: marshal: %v", c.variant, err)
		}
		cfg, err := decodeConfig(raw)
		if err != nil {
			t.Fatalf("%s: decode: %v", c.variant, err)
		}
		if cfg.WinPolicy != c.want {
			t.Errorf("%s: winPolicy = %q, 期望 %q（多人局先猜中胜）",
				c.variant, cfg.WinPolicy, c.want)
		}
	}
}

func TestFriendCannotOverrideFirstCorrect(t *testing.T) {
	for _, policy := range []WinPolicy{WinPolicySolo, WinPolicyWindowAttempts, WinPolicyFirstCorrect} {
		raw, err := json.Marshal(Config{Variant: VariantFriend, WinPolicy: policy})
		if err != nil {
			t.Fatal(err)
		}
		cfg, err := decodeConfig(raw)
		if err != nil {
			t.Fatal(err)
		}
		if cfg.WinPolicy != WinPolicyFirstCorrect {
			t.Fatalf("friend policy %q remained %q", policy, cfg.WinPolicy)
		}
	}
}

func TestFirstCorrectSetsRoundOver(t *testing.T) {
	t.Parallel()
	// 直接构造 Apply 的输入太重；这里用源码断言锁住「猜中且 first-correct → RoundOver」。
	src := mustReadSource(t, "internal/modes/classic/mode.go")
	for _, want := range []string{
		"st.Solved && pub.Config.WinPolicy == WinPolicyFirstCorrect",
		"dec.RoundOver = true",
	} {
		if !strings.Contains(src, want) {
			t.Errorf("mode.go 缺少 %q：first-correct 不会立刻结束整局", want)
		}
	}
}
