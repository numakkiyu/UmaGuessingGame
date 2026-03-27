export type RoomLatencyLevel = "good" | "ok" | "slow" | "poor" | "unknown";

export function resolveLatencyLevel(latencyMs?: number | null): RoomLatencyLevel {
  if (latencyMs == null || !Number.isFinite(latencyMs) || latencyMs < 0) {
    return "unknown";
  }

  if (latencyMs <= 250) {
    return "good";
  }

  if (latencyMs <= 500) {
    return "ok";
  }

  if (latencyMs <= 1000) {
    return "slow";
  }

  return "poor";
}

export function formatLatencyLabel(latencyMs?: number | null) {
  const level = resolveLatencyLevel(latencyMs);

  if (level === "good") {
    return "正常";
  }

  if (level === "ok") {
    return "良好";
  }

  if (level === "slow") {
    return "稍慢";
  }

  if (level === "poor") {
    return "延迟较大";
  }

  return "等待中";
}

