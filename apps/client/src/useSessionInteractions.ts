import { useQuery } from "@tanstack/react-query";
import type { MewDexClient, Session } from "@mewdex/sdk";

export function useSessionInteractions(
  client: MewDexClient,
  session: Session | undefined,
  ready: boolean,
  online: boolean,
) {
  return useQuery({
    queryKey: ["interactions", session?.id],
    queryFn: ({ signal }) => client.interactions(session!.id, signal),
    enabled: ready && !!session,
    refetchInterval: online ? 10000 : false,
  });
}
