package httpapi_test

import (
	"context"
	"net/http"
	"strings"
	"testing"
)

func TestLivenessAlwaysOK(t *testing.T) {
	t.Parallel()
	// 即使依赖全挂，live 也必须 200——否则编排器会误重启一个只是依赖抖动的进程。
	h := newHarness(t, func(c *harnessConfig) {
		c.Ready = func(context.Context) (bool, []string) {
			return false, []string{"database", "migrations", "content"}
		}
	})

	res := h.do("GET", "/health/live", nil, nil)
	if res.Status != http.StatusOK {
		t.Fatalf("live 应为 200，实际 %d", res.Status)
	}
}

func TestReadinessFailsWhenDependenciesFail(t *testing.T) {
	t.Parallel()
	h := newHarness(t, func(c *harnessConfig) {
		c.Ready = func(context.Context) (bool, []string) {
			return false, []string{"database"}
		}
	})

	res := h.do("GET", "/health/ready", nil, nil)
	if res.Status != http.StatusServiceUnavailable {
		t.Fatalf("依赖失败时 ready 应为 503，实际 %d", res.Status)
	}
}

func TestReadinessDoesNotLeakProbeNames(t *testing.T) {
	t.Parallel()
	const probe = "secret_internal_probe_name"
	h := newHarness(t, func(c *harnessConfig) {
		c.Ready = func(context.Context) (bool, []string) {
			return false, []string{probe}
		}
	})

	res := h.do("GET", "/health/ready", nil, nil)
	body := string(res.Body)
	for _, leaked := range []string{probe, "database", "migrations", "content"} {
		if strings.Contains(body, leaked) {
			t.Fatalf("ready 响应体泄露了内部探针名 %q：%s", leaked, body)
		}
	}
}

func TestReadinessOKWhenDependenciesHealthy(t *testing.T) {
	t.Parallel()
	h := newHarness(t, func(c *harnessConfig) {
		c.Ready = func(context.Context) (bool, []string) { return true, nil }
	})

	res := h.do("GET", "/health/ready", nil, nil)
	if res.Status != http.StatusOK {
		t.Fatalf("依赖健康时 ready 应为 200，实际 %d", res.Status)
	}
}

func TestMetricsExposeRuntimeAggregateWithoutIdentifiers(t *testing.T) {
	h := newHarness(t)
	res := h.do("GET", "/metrics", nil, nil)
	if res.Status != http.StatusOK {
		t.Fatalf("metrics status = %d", res.Status)
	}
	body := string(res.Body)
	if !strings.Contains(body, "uma_runtime_goroutines ") {
		t.Fatalf("runtime count missing: %s", body)
	}
	if strings.Contains(body, "sessionId") || strings.Contains(body, "roomId") || strings.Contains(body, "uma_session") {
		t.Fatalf("identifiers leaked: %s", body)
	}
}
