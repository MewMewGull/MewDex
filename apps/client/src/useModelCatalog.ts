import { useMemo } from "react";
import { useInfiniteQuery } from "@tanstack/react-query";
import type { MewDexClient } from "@mewdex/sdk";

export function useModelCatalog(
  client: MewDexClient,
  target: { deviceId: string; projectId: string; sessionId?: string },
  enabled: boolean,
) {
  const catalog = useInfiniteQuery({
    queryKey: ["models", target.deviceId, target.projectId],
    queryFn: ({ pageParam }) => client.models(target, pageParam),
    initialPageParam: "",
    getNextPageParam: (page) => page.nextCursor || undefined,
    enabled: enabled && !!target.deviceId && !!target.projectId,
    staleTime: 60000,
    retry: false,
    refetchOnWindowFocus: false,
  });
  const models = useMemo(
    () => [
      ...new Map(
        (catalog.data?.pages.flatMap((page) => page.models) ?? []).map(
          (model) => [model.model, model],
        ),
      ).values(),
    ],
    [catalog.data],
  );
  return { catalog, models };
}
