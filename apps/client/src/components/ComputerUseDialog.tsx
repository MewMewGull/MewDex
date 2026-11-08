import * as Dialog from "@radix-ui/react-dialog";
import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { X } from "lucide-react";
import {
  commandID,
  type ComputerStatus,
  type Device,
  type MewDexClient,
  type Project,
  type Session,
} from "@mewdex/sdk";
import { t } from "../localization.ts";
import { useDialogReturnFocus } from "../useDialogReturnFocus";

export function computerStateLabel(status?: ComputerStatus) {
  if (!status?.supported) return t("当前执行端不支持窗口控制");
  const states: Record<string, string> = {
    pending: t("等待本机窗口授权"),
    approved: t("窗口已授权"),
    ready: t("窗口控制已就绪"),
    stopping: t("窗口控制停止中"),
    stopped: t("窗口控制已停止"),
    expired: t("窗口授权已到期"),
    unknown: t("窗口操作结果待确认"),
  };
  return status.session
    ? states[status.session.state] || status.session.state
    : t("尚无窗口控制请求");
}

export function ComputerUseDialog({
  scope,
  client,
  project,
  session,
  device,
  connected,
  onClose,
}: {
  scope: string;
  client: MewDexClient;
  project: Project;
  session: Session;
  device: Device;
  connected: boolean;
  onClose: () => void;
}) {
  const directory =
    project.directories?.findIndex((item) => item.deviceId === device.id) ?? -1;
  const [stopping, setStopping] = useState(false);
  const [error, setError] = useState("");
  const focus = useDialogReturnFocus();
  const status = useQuery({
    queryKey: ["computer", scope, project.id, session.id, directory],
    queryFn: ({ signal }) =>
      client.computerStatus(project.id, session.id, directory, signal),
    enabled: connected && directory >= 0,
    refetchInterval: 2000,
  });
  const current = status.data?.session;
  const canStop =
    !!current &&
    !["stopped", "expired", "stopping"].includes(current.state) &&
    connected &&
    device.online;
  async function stop() {
    if (!current || stopping) return;
    setStopping(true);
    setError("");
    try {
      await client.stopComputer(project.id, session.id, directory, {
        id: "cu_ui_" + commandID(),
        sessionId: current.id,
      });
      await status.refetch();
    } catch (cause) {
      setError(
        cause instanceof Error
          ? cause.message
          : t("无法确认窗口停止结果，请刷新状态"),
      );
    } finally {
      setStopping(false);
    }
  }
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
              {t("窗口控制")} · {device.name}
            </Dialog.Title>
            <Dialog.Close className="icon-button" aria-label={t("关闭")}>
              <X size={18} />
            </Dialog.Close>
          </div>
          <Dialog.Description>
            {t(
              "由 Hub 对话申请控制，在目标执行端本机面板选择并授权窗口。授权持续到停止、到期或连接中断。",
            )}
          </Dialog.Description>
          <dl className="computer-use-details">
            <dt>{t("项目")}</dt>
            <dd>{project.name}</dd>
            <dt>{t("对话")}</dt>
            <dd>{session.title}</dd>
            <dt>{t("设备状态")}</dt>
            <dd>{connected && device.online ? t("在线") : t("离线")}</dd>
            <dt>{t("窗口控制状态")}</dt>
            <dd role="status">
              {!connected || !device.online || status.error
                ? t("窗口操作结果待确认")
                : status.isPending
                  ? t("正在读取…")
                  : computerStateLabel(status.data)}
            </dd>
            {current && (
              <>
                <dt>{t("授权窗口")}</dt>
                <dd>{current.window?.title || t("等待本机选择窗口")}</dd>
                <dt>{t("请求原因")}</dt>
                <dd>{current.reason}</dd>
                <dt>{t("到期时间")}</dt>
                <dd>{new Date(current.expiresAt).toLocaleString()}</dd>
                {current.lastActionState && (
                  <>
                    <dt>{t("最近窗口操作")}</dt>
                    <dd>{current.lastActionState}</dd>
                  </>
                )}
              </>
            )}
          </dl>
          {status.data?.reason && <p className="muted">{status.data.reason}</p>}
          {current?.error && <p className="muted">{current.error}</p>}
          {(error || status.error) && (
            <p className="error" role="alert">
              {error || String(status.error)}
            </p>
          )}
          <p className="muted">
            {t(
              "本机面板提供窗口授权与停止入口，物理 Escape 会立即结束控制。新建对话可使用 Computer Use 工具。",
            )}
          </p>
          <div className="project-dialog-footer">
            <button
              disabled={!connected || status.isFetching}
              onClick={() => void status.refetch()}
            >
              {t("刷新")}
            </button>
            <button disabled={!canStop || stopping} onClick={() => void stop()}>
              {t("停止窗口控制")}
            </button>
          </div>
        </Dialog.Content>
      </Dialog.Portal>
    </Dialog.Root>
  );
}
