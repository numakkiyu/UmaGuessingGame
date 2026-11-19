import { useQuery } from "@tanstack/react-query";
import { accountApi } from "./account-api";

export const accountKey = ["account"] as const;
export function useAccountSession() {
  return useQuery({ queryKey: accountKey, queryFn: async ({ signal }) => {
    const response = await accountApi.getAccount(signal);
    if (!response.data) throw new Error("Missing account response");
    return response.data;
  }, staleTime: 30_000, retry: false, refetchOnWindowFocus: "always" });
}
