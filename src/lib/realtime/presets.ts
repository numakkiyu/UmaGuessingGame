export const roomSyncPresetOptions = [
  {
    id: "live",
    label: "马上看",
    helper: "新线索会更快出现",
  },
  {
    id: "balanced",
    label: "平衡",
    helper: "顺手又省心",
  },
  {
    id: "save-data",
    label: "省流量",
    helper: "更省资源，更新会慢一点",
  },
] as const;

export type RoomSyncPresetId = (typeof roomSyncPresetOptions)[number]["id"];

export const defaultRoomSyncPreset: RoomSyncPresetId = "balanced";

export function resolveRoomSyncPollInterval(
  basePollIntervalMs: number,
  preset: RoomSyncPresetId,
) {
  if (preset === "live") {
    return Math.min(basePollIntervalMs, 1200);
  }

  if (preset === "save-data") {
    return Math.max(basePollIntervalMs, 6000);
  }

  return basePollIntervalMs;
}

