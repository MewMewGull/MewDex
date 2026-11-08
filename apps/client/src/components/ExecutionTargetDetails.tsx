import type { ExecutionTarget } from "@mewdex/sdk";
import { t } from "../localization.ts";
import { DeviceIcon } from "./DeviceIcon";

export function ExecutionTargetDetails({
  target,
}: {
  target?: ExecutionTarget;
}) {
  if (!target) return null;
  const accessLabels: Record<string, string> = {
    "read-only": t("只读访问"),
    workspace: t("工作区权限"),
    "workspace-network": t("工作区与网络"),
    "window-grant": t("授权窗口"),
  };
  const access = accessLabels[target.accessMode ?? ""];
  return (
    <div className="execution-target" data-executor-id={target.deviceId}>
      <span className="execution-target-device" title={target.deviceId}>
        <DeviceIcon platform={target.platform ?? ""} size={13} />
        {target.deviceName || target.deviceId}
      </span>
      {target.projectName && <span>{target.projectName}</span>}
      {access && <span>{access}</span>}
      {!!target.harnessRevision && (
        <span title={target.harnessVersion}>
          {t("项目配置 v{0}", target.harnessRevision)}
        </span>
      )}
      {target.workspace && (
        <code className="execution-target-path" title={target.workspace}>
          {target.workspace}
        </code>
      )}
    </div>
  );
}
