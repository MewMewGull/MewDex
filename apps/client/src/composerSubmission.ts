import { t } from "./localization.ts";
import type { Session } from "@mewdex/sdk";

export function composerSubmission(session?: Session) {
  const retired = session?.executionRecovery?.state === "retired";
  const running = session?.state === "running";
  const command =
    retired ||
    session?.archived ||
    session?.frozen ||
    session?.stopStatus === "unconfirmed"
      ? null
      : !session || ["idle", "failed"].includes(session.state)
        ? ("start" as const)
        : running && session.turnId
          ? ("steer" as const)
          : null;
  const blocked = retired
    ? t("旧执行已结束，请新建对话继续。")
    : session?.archived
      ? t("恢复归档对话后可以继续发送。")
      : session?.state === "stopping"
        ? t("正在停止任务，完成后可继续发送。")
        : session?.state === "unknown" || session?.stopStatus === "unconfirmed"
          ? t("执行状态待核实，原输入保留。")
          : session?.frozen
            ? t("队列已暂停，恢复后可以继续发送。")
            : session && !command
              ? t("等待执行状态更新，可先编辑草稿。")
              : "";
  return {
    command,
    label: command === "steer" ? t("追加到当前任务") : t("发送消息"),
    description:
      blocked ||
      (command === "steer"
        ? t("将这条要求追加到当前正在执行的任务。")
        : t("发送这条要求，开始新的模型回合。")),
    queueVisible:
      !!session &&
      !retired &&
      (running ||
        session.state === "stopping" ||
        session.state === "unknown" ||
        session.frozen ||
        session.stopStatus === "background-running"),
    queueLabel: session?.frozen ? t("加入暂停队列") : t("排入下一条"),
    queueDescription: session?.frozen
      ? t("保存到原队列，手动恢复后再执行。")
      : session?.state === "unknown"
        ? t("原执行状态待核实；消息只加入队列，核实后再决定是否恢复。")
        : session?.stopStatus === "background-running"
          ? t("加入队列，等待后台任务退出后执行。")
          : t("加入队列，等待当前任务结束后执行；停止后需手动恢复。"),
  };
}

export type ComposerSubmission = ReturnType<typeof composerSubmission>;
