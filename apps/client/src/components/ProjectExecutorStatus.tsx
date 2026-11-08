import { MousePointer2 } from "lucide-react";
import type { Device, MewDexClient, Project, Session } from "@mewdex/sdk";
import { t } from "../localization.ts";
import { computerStopLabel } from "../computerUse";
import { useComputerUse } from "../useComputerUse";
import { ExecutorStatus } from "./ExecutorStatus";

export function ProjectExecutorStatus(props: {
  scope: string;
  client: MewDexClient;
  devices: Device[];
  owner?: Device;
  project?: Project;
  session?: Session;
  connected: boolean;
  onComputer?: (deviceId: string) => void;
}) {
  const computer = useComputerUse({
    ...props,
    session: props.onComputer ? props.session : undefined,
  });
  const notices = computer.entries.filter((entry) => entry.view.attention);
  return (
    <ExecutorStatus
      {...props}
      computerNotice={
        props.onComputer &&
        notices.length > 0 && (
          <div className="computer-use-notices" aria-live="polite">
            {notices.map((entry) => (
              <div
                className="computer-use-notice"
                data-tone={entry.view.tone}
                key={entry.deviceId}
              >
                <MousePointer2 size={16} aria-hidden="true" />
                <div className="computer-use-notice-content">
                  <strong>
                    {entry.device?.name ?? entry.deviceId} · {entry.view.label}
                  </strong>
                  <p>
                    {["approved", "ready"].includes(entry.view.state)
                      ? entry.view.current?.window?.title || entry.view.detail
                      : entry.view.detail}
                  </p>
                  {entry.stopError && (
                    <p className="error" role="alert">
                      {entry.stopError}
                    </p>
                  )}
                </div>
                <div className="computer-use-notice-actions">
                  <button onClick={() => props.onComputer?.(entry.deviceId)}>
                    {entry.view.state === "pending"
                      ? t("查看授权步骤")
                      : t("查看详情")}
                  </button>
                  {entry.view.canStop && (
                    <button
                      disabled={computer.stopping}
                      onClick={() => computer.stop(entry)}
                    >
                      {computer.stopping
                        ? t("正在停止…")
                        : computerStopLabel(entry.view.current)}
                    </button>
                  )}
                </div>
              </div>
            ))}
          </div>
        )
      }
    />
  );
}
