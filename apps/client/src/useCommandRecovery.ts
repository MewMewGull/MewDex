import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import type { MewDexClient } from "@mewdex/sdk";
import type { PendingCommand } from "./commandRecovery";

// The Hub owns durable references. Input devices recover metadata, never resend
// a command or reconstruct private input/form bodies from the receipt ledger.
export function useCommandRecovery(
  client: MewDexClient,
  identityId: string,
  deviceId: string,
  ready: boolean,
  local: PendingCommand[],
  dismissed: Set<string>,
  epoch: number,
) {
  const [position, setPosition] = useState({ deviceId: "", after: 0, epoch });
  const after =
    position.deviceId === deviceId && position.epoch === epoch
      ? position.after
      : 0;
  const query = useQuery({
    queryKey: ["pending-commands", identityId, deviceId, epoch, after],
    queryFn: ({ signal }) => client.pendingCommands(deviceId, after, signal),
    enabled: ready && !!deviceId,
    gcTime: 0,
  });
  const localIds = new Set(local.map((item) => item.command.id));
  const recovered: PendingCommand[] = (query.data?.commands ?? [])
    .filter((item) => !localIds.has(item.id) && !dismissed.has(item.id))
    .map((item) => ({
      command: {
        id: item.id,
        deviceId: item.deviceId,
        sessionId: item.sessionId,
        projectId: item.projectId,
        kind: item.kind,
      },
      draft: "",
    }));
  const pending = [...local, ...recovered];
  const visible = pending.some((item) => item.command.deviceId === deviceId);
  return {
    pending,
    epoch,
    after,
    hasMore: query.data?.hasMore ?? false,
    busy: query.isFetching,
    error: query.error?.message ?? "",
    blocked:
      !!deviceId &&
      (!ready ||
        query.isPending ||
        query.isError ||
        !!query.data?.hasMore ||
        visible),
    canAdvance: !!query.data?.hasMore && !visible && !query.isFetching,
    next: () => {
      if (query.data?.hasMore)
        setPosition({ deviceId, after: query.data.after, epoch });
    },
    first: () => setPosition({ deviceId, after: 0, epoch }),
    refresh: () => void query.refetch(),
  };
}
