import { useQuery } from "@tanstack/react-query";
import type { MewDexClient } from "@mewdex/sdk";

export function useNetworkActivity(
  client: MewDexClient,
  ready: boolean,
  open: boolean,
) {
  return useQuery({
    queryKey: ["activity"],
    queryFn: ({ signal }) => client.activity(signal),
    enabled: ready,
    staleTime: 2000,
    refetchInterval: open ? 5000 : 15000,
  });
}
