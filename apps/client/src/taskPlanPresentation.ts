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
  let label = "上次计划";
  let tone: "muted" | "active" | "complete" | "attention" = "muted";
  let active = false;
  let note = "";

  if (context.historical) {
    label =
      plan.turnStatus === "completed"
        ? "当时已结束"
        : plan.turnStatus === "failed"
          ? "当时失败"
          : plan.turnStatus === "interrupted"
            ? "当时已中断"
            : plan.turnStatus === "inProgress"
              ? "当时进行中"
              : "当时的计划";
  } else if (context.session.executionRecovery?.state === "retired") {
    label = "执行已封存";
    note = "这里保留封存前最后记录的步骤。";
  } else if (context.session.state === "unknown") {
    label = "执行待核实";
    tone = "attention";
    note = "显示最后记录的进度，执行结果仍待确认。";
  } else if (terminal) {
    label =
      plan.turnStatus === "completed"
        ? "回合已结束"
        : plan.turnStatus === "failed"
          ? "回合失败"
          : "回合已中断";
    tone = plan.turnStatus === "completed" && complete ? "complete" : "muted";
  } else if (!context.connected || !context.online) {
    label = "上次进行中";
    note = "连接恢复后会更新步骤。";
  } else if (context.session.state === "stopping") {
    label = "正在停止";
  } else if (context.session.state === "paused" || context.session.frozen) {
    label = "已暂停";
  } else if (
    context.session.state === "running" &&
    plan.turnStatus === "inProgress"
  ) {
    label = "执行中";
    tone = "active";
    active = true;
  }
  if (terminal && !complete && !note) {
    note = context.historical
      ? "当时回合已结束，部分步骤尚未标记完成。"
      : "回合已结束，部分步骤尚未标记完成。";
  }
  const step = current ?? next;
  const stepLabel = context.historical
    ? "当时步骤"
    : active
      ? current
        ? "正在进行"
        : "下一步"
      : "最后记录";
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
        ? "计划中的步骤已全部标记完成"
        : "展开查看步骤",
  };
}

export function taskPlanStepLabel(status: string) {
  return status === "completed"
    ? "已完成"
    : status === "inProgress"
      ? "进行中"
      : status === "pending"
        ? "待进行"
        : status;
}
