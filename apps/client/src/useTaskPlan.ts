import { useQuery } from "@tanstack/react-query";
import type { MewDexClient } from "@mewdex/sdk";

export function useTaskPlan(
  client: MewDexClient,
  sessionId: string,
  enabled: boolean,
  live: boolean,
  historyAfter: number | undefined,
) {
  // An unresolved history page must not briefly display the live turn's plan.
  const before = live
    ? 0
    : historyAfter === undefined
      ? null
      : historyAfter + 1;
  return useQuery({
    queryKey: ["task-plan", sessionId, before],
    queryFn: ({ signal }) => client.taskPlan(sessionId, before ?? 0, signal),
    enabled: enabled && !!sessionId && before !== null,
    staleTime: Infinity,
    gcTime: 0,
    retry: false,
  });
}
