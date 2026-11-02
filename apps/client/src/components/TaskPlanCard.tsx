import {
  CheckCircle2,
  ChevronDown,
  Circle,
  CircleDot,
  ListTodo,
} from "lucide-react";
import type { Session, TaskPlan } from "@mewdex/sdk";
import {
  taskPlanPresentation,
  taskPlanStepLabel,
} from "../taskPlanPresentation";

export function TaskPlanCard({
  plan,
  session,
  historical,
  connected,
  online,
  unavailable,
  refreshing,
  onRetry,
}: {
  plan: TaskPlan | null | undefined;
  session: Session;
  historical: boolean;
  connected: boolean;
  online: boolean;
  unavailable: boolean;
  refreshing: boolean;
  onRetry: () => void;
}) {
  if (!plan?.steps.length) {
    return unavailable && connected ? (
      <details className="task-plan-unavailable">
        <summary>任务计划暂时无法读取</summary>
        <button disabled={refreshing} onClick={onRetry}>
          重新读取
        </button>
      </details>
    ) : null;
  }
  const view = taskPlanPresentation(plan, {
    session,
    historical,
    connected,
    online,
  });
  return (
    <details className="task-plan-card" data-active={view.active || undefined}>
      <summary>
        <ListTodo size={17} className="task-plan-symbol" aria-hidden="true" />
        <span className="task-plan-heading">
          <span className="task-plan-title">
            {historical ? "当时的任务计划" : "任务计划"}
            <small>
              已完成 {view.completed}/{view.total}
            </small>
          </span>
          <span className="task-plan-current" title={view.currentStep}>
            {view.currentStep}
          </span>
        </span>
        <span className="task-plan-status" data-tone={view.tone}>
          {view.label}
        </span>
        <ChevronDown
          size={15}
          className="task-plan-chevron"
          aria-hidden="true"
        />
      </summary>
      <div className="task-plan-content">
        {plan.explanation && (
          <p className="task-plan-explanation">{plan.explanation}</p>
        )}
        <ol aria-label="任务步骤">
          {plan.steps.map((step, index) => {
            const Icon =
              step.status === "completed"
                ? CheckCircle2
                : step.status === "inProgress"
                  ? CircleDot
                  : Circle;
            return (
              <li key={index} data-status={step.status}>
                <Icon size={16} aria-hidden="true" />
                <span>{step.step}</span>
                <small>
                  {step.status === "inProgress" && !view.active
                    ? historical
                      ? "当时进行中"
                      : "上次进行中"
                    : taskPlanStepLabel(step.status)}
                </small>
              </li>
            );
          })}
        </ol>
        {view.note && <p className="task-plan-note">{view.note}</p>}
        {unavailable && (
          <p className="task-plan-note">
            暂时无法更新计划，已保留最后记录。
            <button disabled={!connected || refreshing} onClick={onRetry}>
              重新读取
            </button>
          </p>
        )}
      </div>
    </details>
  );
}
