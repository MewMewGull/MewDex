import { t } from "../localization.ts";
import type { Request, Result } from "./localProtocol";
import { isLocalHub } from "./hubConnection.ts";

export async function prepareDesktopTerminals(
  request: (request: Request) => Promise<Result>,
  target?: { baseURL: string; hubDeviceId?: string; confirmed: boolean },
): Promise<Result & { started?: boolean; suspended?: boolean }> {
  let current = await request({ operation: "status" });
  const { snapshot } = current;
  if (
    snapshot.configured &&
    snapshot.hub &&
    target &&
    !isLocalHub(snapshot, target.baseURL, target.hubDeviceId)
  ) {
    if (!target.confirmed) return current;
    const startup = await request({ operation: "startup.status" });
    current = startup;
    if (startup.startup?.requestedEnabled)
      current = await request({
        operation: "startup.apply",
        expectedDeviceId: snapshot.deviceId,
        startup: { enabled: false },
      });
    if (current.snapshot.state === "stopped")
      return { ...current, suspended: true };
    if (!current.snapshot.instanceId)
      throw new Error(
        t("已连接远端 Hub，本机 Hub 的停止状态需要在服务设置中核实。"),
      );
    const stopped = await request({
      operation: "stop",
      expectedDeviceId: snapshot.deviceId,
      expectedInstanceId: current.snapshot.instanceId,
    });
    if (stopped.snapshot.state !== "stopped")
      throw new Error(t("本机 Hub 的停止结果尚未确认，请刷新服务状态。"));
    return { ...stopped, suspended: true };
  }
  if (
    !snapshot.configured ||
    (!snapshot.hub && !snapshot.executor) ||
    snapshot.ready
  )
    return current;
  if (!["stopped", "starting"].includes(snapshot.state))
    throw new Error(t("本机终端状态需要确认，请打开本机设备查看。"));
  const result = await request({
    operation: "start",
    expectedDeviceId: snapshot.deviceId,
  });
  return { ...result, started: true };
}
