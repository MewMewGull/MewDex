import type {
  ComputerSession,
  ComputerStatus,
  Device,
  Project,
} from "@mewdex/sdk";
import { t } from "./localization.ts";

export function computerTargets(
  project: Project | undefined,
  devices: Device[],
) {
  const seen = new Set<string>();
  return (project?.directories ?? []).flatMap((directory, index) => {
    if (seen.has(directory.deviceId)) return [];
    seen.add(directory.deviceId);
    const device = devices.find((item) => item.id === directory.deviceId);
    return [
      {
        deviceId: directory.deviceId,
        device,
        directory: index,
        supported: !!device?.capabilities?.includes("computer-use-v1"),
      },
    ];
  });
}

export type ComputerTarget = ReturnType<typeof computerTargets>[number];
export type ComputerTone = "muted" | "info" | "success" | "warning" | "danger";

export function computerPresentation({
  target,
  connected,
  conversationId,
  status,
  loading = false,
  error = false,
  now = Date.now(),
}: {
  target: ComputerTarget;
  connected: boolean;
  conversationId?: string;
  status?: ComputerStatus;
  loading?: boolean;
  error?: boolean;
  now?: number;
}) {
  const reported = status?.session;
  const current =
    reported?.ownerId === conversationId &&
    reported?.deviceId === target.deviceId
      ? reported
      : undefined;
  let state = current?.state ?? "idle";
  if (
    current?.lastActionState === "unknown" &&
    ["approved", "ready"].includes(state)
  )
    state = "unknown";
  if (current && ["pending", "approved", "ready"].includes(state)) {
    const expiry = Date.parse(current.expiresAt);
    if (!Number.isFinite(expiry)) state = "unknown";
    else if (expiry <= now) state = "expired";
  }
  const live = !!current && !["stopped", "expired"].includes(state);
  const canStop =
    connected &&
    !!target.device?.online &&
    !!current &&
    !["stopped", "expired", "stopping"].includes(state);
  const view = (
    value: string,
    label: string,
    detail: string,
    tone: ComputerTone = "muted",
    attention = live,
  ) => ({ state: value, label, detail, tone, attention, canStop, current });

  if (!connected || !target.device?.online) {
    return view(
      "offline",
      t("状态待确认"),
      !connected
        ? t("连接恢复后会重新确认电脑操作状态。当前显示的授权信息仅供核对。")
        : t("目标电脑离线。请在该电脑启动执行端并连接后再操作。"),
      live ? "warning" : "muted",
    );
  }
  if (!target.supported || status?.supported === false) {
    return view(
      "unsupported",
      t("暂不支持电脑操作"),
      status?.reason ||
        t(
          "此执行端尚未提供电脑操作能力。目前支持已登录桌面的 Windows 64 位执行端。",
        ),
    );
  }
  if (status?.reason) {
    return {
      ...view("unknown", t("状态待确认"), status.reason, "warning"),
      canStop: false,
    };
  }
  if (error || (reported && !current)) {
    return view(
      "unknown",
      t("状态待确认"),
      t("暂时无法确认授权状态。请刷新核对；已有请求仍可尝试停止。"),
      "warning",
    );
  }
  if (loading)
    return view("loading", t("正在读取…"), t("正在确认这台电脑的授权状态。"));
  switch (state) {
    case "pending":
      return view(
        state,
        t("等待窗口授权"),
        t(
          "请在 {0} 的系统托盘打开 MewDex 执行端面板，进入“电脑操作”，选择窗口并授权。",
          target.device.name,
        ),
        "warning",
      );
    case "approved":
    case "ready":
      return view(
        state,
        current?.lastActionState === "running"
          ? t("正在操作窗口")
          : t("窗口授权有效"),
        t("AI 可以查看所选窗口并点击、输入和滚动。你可随时停止并收回授权。"),
        "success",
      );
    case "stopping":
      return view(
        state,
        t("正在停止电脑操作"),
        t("正在收回窗口授权，等待执行端确认停止结果。"),
        "info",
      );
    case "stopped":
      return view(
        state,
        t("电脑操作已停止"),
        t("窗口授权已收回。再次操作时，AI 会重新请求授权。"),
      );
    case "expired":
      return view(
        state,
        t("窗口授权已到期"),
        t("再次操作时，请让 AI 重新申请窗口授权。"),
      );
    case "idle":
      return view(
        state,
        t("按需请求窗口授权"),
        t(
          "先在对话中描述目标电脑、软件和任务。AI 需要操作界面时，会请求你选择并授权窗口。",
        ),
      );
    default:
      return view(
        "unknown",
        t("操作结果待核对"),
        t("请先核对目标窗口的实际结果，再停止原授权；不要重复尚未确认的操作。"),
        "warning",
      );
  }
}

export type ComputerPresentation = ReturnType<typeof computerPresentation>;

export function computerStopLabel(current?: ComputerSession) {
  return current?.state === "pending" ? t("取消本次请求") : t("停止并收回授权");
}

export function computerTaskDraft(text: string, deviceName: string) {
  const prefix = t("请在 {0} 上使用电脑操作，帮我完成以下任务：", deviceName);
  return text.startsWith(prefix) ? text : `${prefix}\n${text}`;
}
