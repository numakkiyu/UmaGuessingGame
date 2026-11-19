
import { useQuery, type UseQueryResult } from "@tanstack/react-query";
import { useApi } from "@/lib/api";
import type { RoomSnapshot } from "@/lib/api/types";

export const roomKeys = {
  detail: (roomId: string) => ["room", roomId] as const,
};

export type RoomQueryData = { room: RoomSnapshot };

/** Accept both direct snapshots and responses wrapped in a room field. */
export function useRoom(roomId: string | null): UseQueryResult<RoomQueryData, unknown> {
  const { api } = useApi();
  return useQuery({
    queryKey: roomKeys.detail(roomId ?? ""),
    enabled: Boolean(roomId),
    queryFn: async ({ signal }) => {
      const response = await api.getRoom(roomId!, signal);
      const raw = response.data as unknown;
      if (raw && typeof raw === "object" && "room" in raw) {
        return { room: (raw as { room: RoomSnapshot }).room };
      }
      return { room: raw as RoomSnapshot };
    },
    // 房间状态变化频繁，不做长时间缓存；realtime 通知会主动 refetch
    staleTime: 0,
  });
}
