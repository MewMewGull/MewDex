import { t } from "./localization.ts";
import type { Session, TaskPlan } from "@mewdex/sdk";

type PlanContext = {
  historical: boolean;
  connected: boolean;
  online: boolean;
  session: Pick<Session, "state" | "frozen" | "executionRecovery">;
};

export function taskPlanPresentation(plan: TaskPlan, context: PlanContext) {
  const completed = plan.steps.filter(
    (step) => step.status === "completed",
  ).length;
  const current = plan.steps.find((step) => step.status === "inProgress");
  const next = plan.steps.find((step) => step.status === "pending");
  const complete = completed === plan.steps.length;
  const terminal = ["completed", "failed", "interrupted"].includes(
    plan.turnStatus ?? "",
  );
  let label = t("上次计划");
  let tone: "muted" | "active" | "complete" | "attention" = "muted";
  let active = false;
  let note = "";

  if (context.historical) {
    label =
      plan.turnStatus === "completed"
        ? t("当时已结束")
        : plan.turnStatus === "failed"
          ? t("当时失败")
          : plan.turnStatus === "interrupted"
            ? t("当时已中断")
            : plan.turnStatus === "inProgress"
              ? t("当时进行中")
              : t("当时的计划");
  } else if (context.session.executionRecovery?.state === "retired") {
    label = t("执行已封存");
    note = t("这里保留封存前最后记录的步骤。");
  } else if (context.session.state === "unknown") {
    label = t("执行待核实");
    tone = "attention";
    note = t("显示最后记录的进度，执行结果仍待确认。");
  } else if (terminal) {
    label =
      plan.turnStatus === "completed"
        ? t("回合已结束")
        : plan.turnStatus === "failed"
          ? t("回合失败")
          : t("回合已中断");
    tone = plan.turnStatus === "completed" && complete ? "complete" : "muted";
  } else if (!context.connected || !context.online) {
    label = t("上次进行中");
    note = t("连接恢复后会更新步骤。");
  } else if (context.session.state === "stopping") {
    label = t("正在停止");
  } else if (context.session.state === "paused" || context.session.frozen) {
    label = t("已暂停");
  } else if (
    context.session.state === "running" &&
    plan.turnStatus === "inProgress"
  ) {
    label = t("执行中");
    tone = "active";
    active = true;
  }
  if (terminal && !complete && !note) {
    note = context.historical
      ? t("当时回合已结束，部分步骤尚未标记完成。")
      : t("回合已结束，部分步骤尚未标记完成。");
  }
  const step = current ?? next;
  const stepLabel = context.historical
    ? t("当时步骤")
    : active
      ? current
        ? t("正在进行")
        : t("下一步")
      : t("最后记录");
  return {
    completed,
    total: plan.steps.length,
    label,
    tone,
    active,
    note,
    currentStep: step
      ? `${stepLabel}：${step.step}`
      : complete
        ? t("计划中的步骤已全部标记完成")
        : t("展开查看步骤"),
  };
}

export function taskPlanStepLabel(status: string) {
  return status === "completed"
    ? t("已完成")
    : status === "inProgress"
      ? t("进行中")
      : status === "pending"
        ? t("待进行")
        : status;
}
