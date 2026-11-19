// Shared interface copy; game facts, player input and transport diagnostics are handled separately

import type { Transport } from "@/lib/realtime/use-realtime";

/* 连接补充说明只显示房间名，不展示传输协议 */
export function connectionDetail(roomName: string, _transport: Transport): string {
  // transport 保留参数以便将来按通道给出更细提示，但当前不向玩家暴露通道名
  return roomName;
}
