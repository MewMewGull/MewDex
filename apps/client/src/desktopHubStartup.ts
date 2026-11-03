import type { Request, Result } from "./localProtocol";

export async function prepareDesktopHub(
  request: (request: Request) => Promise<Result>,
): Promise<Result & { started?: boolean }> {
  const current = await request({ operation: "status" });
  const { snapshot } = current;
  if (!snapshot.configured || !snapshot.hub || snapshot.ready) return current;
  if (!["stopped", "starting"].includes(snapshot.state))
    throw new Error("本机中央后端状态需要确认，请打开本机设备查看。");
  const result = await request({
    operation: "start",
    expectedDeviceId: snapshot.deviceId,
  });
  return { ...result, started: true };
}
