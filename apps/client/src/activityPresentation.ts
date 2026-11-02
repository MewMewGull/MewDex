import type { Device, SessionActivity } from "@mewdex/sdk";

export function currentActivity(item: SessionActivity, devices: readonly Device[]): boolean {
  const session = item.session;
  if (!devices.some((device) => device.id === session.deviceId && device.online) || session.state === "unknown" || session.executionRecovery?.state === "retired") return false;
  return item.pendingInteractions > 0 || item.queuedInputs > 0 || session.state === "running" || session.state === "stopping" || session.stopStatus === "background-running";
}
