import { t } from "./localization.ts";
import {
  CommandOutcomeError,
  type Command,
  type Device,
  type Session,
} from "@mewdex/sdk";
import type { WorkspaceSelection } from "./state";

export interface PendingCommand {
  command: Omit<Command, "sourceId" | "createdAt">;
  draft: string;
  draftKey?: string;
  selection?: WorkspaceSelection;
}

type CommandGroup = {
  deviceId: string;
  deviceName: string;
  current: boolean;
  items: { pending: PendingCommand; targetLabel: string; session?: Session }[];
};

export function isUnconfirmedCommand(
  error: unknown,
): error is CommandOutcomeError {
  return (
    error instanceof CommandOutcomeError &&
    ["accepted", "unknown"].includes(error.state)
  );
}

// Labels use the current visible device/session metadata, never submitted input.
// Grouping does not query receipts, navigate or change pending operations.
export function commandRecoveryView(
  pending: readonly PendingCommand[],
  deviceId: string,
  devices: readonly Device[],
  sessions: readonly Session[],
) {
  const deviceMap = new Map(devices.map((device) => [device.id, device]));
  const sessionMap = new Map(sessions.map((session) => [session.id, session]));
  const groups = new Map<string, CommandGroup>();
  for (const item of pending) {
    const command = item.command;
    const device = deviceMap.get(command.deviceId);
    const candidate = sessionMap.get(command.sessionId ?? "");
    const session =
      candidate?.deviceId === command.deviceId ? candidate : undefined;
    const project = device?.projects.find(
      (project) => project.id === command.projectId,
    );
    let group = groups.get(command.deviceId);
    if (!group) {
      group = {
        deviceId: command.deviceId,
        deviceName: device?.name ?? t("原设备暂不可用"),
        current: command.deviceId === deviceId,
        items: [],
      };
      groups.set(command.deviceId, group);
    }
    group.items.push({
      pending: item,
      session,
      targetLabel: session
        ? session.title || t("未命名对话")
        : command.sessionId
          ? t("原对话暂不可用")
          : project
            ? t("{0} · 新对话", project.name)
            : t("新对话"),
    });
  }
  const ordered = [...groups.values()].sort(
    (a, b) => Number(b.current) - Number(a.current),
  );
  const currentCount = groups.get(deviceId)?.items.length ?? 0;
  return {
    groups: ordered,
    currentCount,
    otherCount: pending.length - currentCount,
    currentDeviceName: deviceMap.get(deviceId)?.name ?? t("当前设备"),
  };
}

export type CommandRecoveryView = ReturnType<typeof commandRecoveryView>;
