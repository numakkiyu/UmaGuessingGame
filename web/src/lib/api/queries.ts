
import { useQuery, type UseQueryResult } from "@tanstack/react-query";
import { useApi } from "@/lib/api";
import type { Character, Mode, PublicConfig } from "@/lib/api/types";

export const queryKeys = {
  publicConfig: ["config"] as const,
  modes: ["modes"] as const,
  characters: (cursor: string | null) => ["characters", cursor ?? "first"] as const,
} as const;

export type ConfigQuery = UseQueryResult<PublicConfig, unknown>;
export type ModesQuery = UseQueryResult<Mode[], unknown>;
export type CharactersQuery = UseQueryResult<{ items: Character[]; datasetVersion: string; nextCursor: string | null }, unknown>;

/** 公开配置：站点名、部署模式、可用模式、素材地址等 */
export function usePublicConfig(): ConfigQuery {
  const { api } = useApi();
  return useQuery({
    queryKey: queryKeys.publicConfig,
    queryFn: async ({ signal }) => {
      const response = await api.getPublicConfig(signal);
      if (!response.data) throw new Error("公开配置为空");
      return response.data;
    },
  });
}

/**
 * 模式清单。
 * 服务端新增枚举的安全回退——这里不假设返回的模式都是前端认识的，
 * 过滤交给调用方用 toKnownModeId 判断，未知模式显示「需要刷新」。
 */
export function useModes(): ModesQuery {
  const { api } = useApi();
  return useQuery({
    queryKey: queryKeys.modes,
    queryFn: async ({ signal }) => {
      const response = await api.listModes(signal);
      return response.data?.items ?? [];
    },
  });
}

/** 角色公开投影（搜索用的小索引，不含答案） */
export function useCharacters(cursor: string | null = null): CharactersQuery {
  const { api } = useApi();
  return useQuery({
    queryKey: queryKeys.characters(cursor),
    queryFn: async ({ signal }) => {
      const query = cursor ? { cursor, limit: 100 } : { limit: 100 };
      const response = await api.listCharacters(query, signal);
      const body = response.data;
      if (!body) throw new Error("暂时没有找到角色资料");
      if (cursor) return body;
      const items = [...body.items];
      let next = body.nextCursor;
      const seen = new Set<string>();
      while (next) {
        if (seen.has(next) || seen.size >= 20) throw new Error("角色资料暂时没有准备完整，请稍后再试");
        seen.add(next);
        const page = (await api.listCharacters({cursor:next,limit:100},signal)).data;
        if (!page || page.datasetVersion !== body.datasetVersion) throw new Error("角色资料刚刚更新，请重新载入后再试");
        items.push(...page.items);
        next = page.nextCursor;
      }
      return {...body, items:[...new Map(items.map(c=>[c.characterId,c])).values()], nextCursor:null};
    },
    // 角色清单是版本化内容，短时间内不需要重新拉
    staleTime: 5 * 60_000,
  });
}
