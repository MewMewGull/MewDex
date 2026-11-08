import * as Dialog from "@radix-ui/react-dialog";
import { useState } from "react";
import { MousePointer2, RefreshCw, X } from "lucide-react";
import type { Device, MewDexClient, Project, Session } from "@mewdex/sdk";
import { t } from "../localization.ts";
import { computerStopLabel } from "../computerUse";
import { devicePlatform, devicePresence } from "../devicePresence";
import { useComputerUse } from "../useComputerUse";
import { useDialogReturnFocus } from "../useDialogReturnFocus";
import { DeviceIcon } from "./DeviceIcon";

export function ComputerUseDialog({
  scope,
  client,
  project,
  session,
  devices,
  deviceId,
  connected,
  onCompose,
  onClose,
}: {
  scope: string;
  client: MewDexClient;
  project: Project;
  session?: Session;
  devices: Device[];
  deviceId: string;
  connected: boolean;
  onCompose?: (device: Device) => void;
  onClose: () => void;
}) {
  const [selectedID, setSelectedID] = useState(deviceId);
  const focus = useDialogReturnFocus();
  const computer = useComputerUse({
    scope,
    client,
    project,
    session,
    devices,
    connected,
  });
  const entry =
    computer.entries.find((item) => item.deviceId === selectedID) ??
    computer.entries[0];
  const view = entry?.view;
  const current = view?.current;
  const canCompose =
    !!entry?.supported &&
    connected &&
    entry.device?.online &&
    onCompose &&
    ["idle", "stopped", "expired"].includes(view?.state ?? "");
  return (
    <Dialog.Root
      open
      onOpenChange={(open) => {
        if (!open) onClose();
      }}
    >
      <Dialog.Portal>
        <Dialog.Overlay className="dialog-overlay" />
        <Dialog.Content
          className="settings-dialog computer-use-dialog"
          onOpenAutoFocus={focus.capture}
          onCloseAutoFocus={focus.restore}
        >
          <div className="dialog-heading">
            <Dialog.Title>
              <MousePointer2 size={19} aria-hidden="true" />
              {t("AI 电脑操作")}
            </Dialog.Title>
            <Dialog.Close className="icon-button" aria-label={t("关闭")}>
              <X size={18} />
            </Dialog.Close>
          </div>
          <Dialog.Description>
            {t(
              "让 AI 查看软件窗口，并通过点击、输入和滚动完成任务。先描述任务，需要时再授权窗口。",
            )}
          </Dialog.Description>
          <div
            className="computer-use-targets"
            role="group"
            aria-label={t("选择目标电脑")}
          >
            {computer.entries.map((target) => (
              <button
                key={target.deviceId}
                type="button"
                className="computer-use-target"
                aria-pressed={target.deviceId === entry?.deviceId}
                onClick={() => setSelectedID(target.deviceId)}
              >
                <DeviceIcon
                  platform={target.device?.platform ?? ""}
                  size={17}
                />
                <span>
                  <strong>{target.device?.name ?? target.deviceId}</strong>
                  <small>
                    {devicePlatform(target.device?.platform ?? "")} ·{" "}
                    {target.supported
                      ? devicePresence(target.device?.online, connected).label
                      : t("暂不支持")}
                  </small>
                </span>
              </button>
            ))}
          </div>
          {entry && view ? (
            <>
              <div
                className="computer-use-state"
                data-tone={view.tone}
                role="status"
              >
                <strong>{view.label}</strong>
                <p>{view.detail}</p>
              </div>
              {view.state === "pending" && (
                <div className="computer-use-handoff">
                  <strong>
                    {t(
                      "请在 {0} 上完成授权",
                      entry.device?.name ?? entry.deviceId,
                    )}
                  </strong>
                  <p>
                    {t(
                      "选择本次任务需要的窗口和授权时长。授权后回到对话，让 AI 继续任务。",
                    )}
                  </p>
                  <p>
                    {t(
                      "此处可以查看和取消请求；窗口选择和授权在目标电脑完成。",
                    )}
                  </p>
                </div>
              )}
              {current ? (
                <dl className="computer-use-details">
                  <dt>{t("对话")}</dt>
                  <dd>{session?.title}</dd>
                  <dt>
                    {view.state === "offline" || view.state === "unknown"
                      ? t("上次记录的窗口")
                      : t("授权窗口")}
                  </dt>
                  <dd>{current.window?.title || t("等待本机选择窗口")}</dd>
                  <dt>{t("请求原因")}</dt>
                  <dd>{current.reason}</dd>
                  <dt>
                    {current.state === "pending"
                      ? t("请求有效期")
                      : t("授权到期时间")}
                  </dt>
                  <dd>{new Date(current.expiresAt).toLocaleString()}</dd>
                </dl>
              ) : (
                entry.supported && (
                  <ol className="computer-use-steps">
                    <li>
                      <strong>{t("描述任务")}</strong>
                      <span>
                        {t("在对话中写明目标电脑、软件和希望完成的操作。")}
                      </span>
                    </li>
                    <li>
                      <strong>{t("按需授权")}</strong>
                      <span>
                        {t("收到请求后，在目标电脑选择一个窗口并限时授权。")}
                      </span>
                    </li>
                    <li>
                      <strong>{t("查看与停止")}</strong>
                      <span>
                        {t(
                          "这里会显示授权状态。你可随时停止，授权到期或设备断线也会结束控制。",
                        )}
                      </span>
                    </li>
                  </ol>
                )
              )}
              {entry.supported && (
                <p className="muted computer-use-desktop-note">
                  {t(
                    "操作时请保持目标电脑已解锁、窗口可见。电脑操作授权与工作区文件权限分别管理。",
                  )}
                </p>
              )}
              {current?.error && view.state === "unknown" && (
                <p className="error" role="alert">
                  {current.error}
                </p>
              )}
              {entry.stopError && (
                <p className="error" role="alert">
                  {entry.stopError}
                </p>
              )}
            </>
          ) : (
            <p className="muted">
              {t("请先为项目绑定执行端，再选择要操作的电脑。")}
            </p>
          )}
          <div className="project-dialog-footer">
            {entry?.supported && session && (
              <button
                className="computer-use-refresh"
                disabled={
                  !connected || !entry.device?.online || entry.query.isFetching
                }
                onClick={() => void entry.query.refetch()}
              >
                <RefreshCw size={14} aria-hidden="true" />
                {t("刷新状态")}
              </button>
            )}
            <Dialog.Close asChild>
              <button>{t("关闭")}</button>
            </Dialog.Close>
            {canCompose && (
              <button
                className="primary"
                onClick={() => {
                  onCompose?.(entry.device!);
                  onClose();
                }}
              >
                {t("填写操作任务")}
              </button>
            )}
            {view?.canStop && (
              <button
                className="computer-use-stop"
                disabled={computer.stopping}
                onClick={() => computer.stop(entry)}
              >
                {computer.stopping
                  ? t("正在停止…")
                  : computerStopLabel(current)}
              </button>
            )}
          </div>
        </Dialog.Content>
      </Dialog.Portal>
    </Dialog.Root>
  );
}
