import { test } from "node:test";
import assert from "node:assert/strict";
import { taskPlanPresentation } from "../src/taskPlanPresentation.ts";

test("plan progress reflects reported steps and viewed time without claiming a turn is still running", () => {
  const plan = {
    turnStatus: "inProgress",
    steps: [
      { step: "读取项目", status: "completed" },
      { step: "调整 Windows 布局", status: "inProgress" },
      { step: "整理修改", status: "pending" },
    ],
  };
  const context = {
    historical: false,
    connected: true,
    online: true,
    session: { state: "running", frozen: false },
  };
  const original = JSON.stringify(plan);
  const live = taskPlanPresentation(plan, context);
  assert.equal(live.active, true);
  assert.equal(live.completed, 1);
  assert.equal(live.total, 3);
  assert.equal(live.currentStep, "正在进行：调整 Windows 布局");
  const historical = taskPlanPresentation(plan, {
    ...context,
    historical: true,
  });
  assert.equal(historical.active, false);
  assert.equal(historical.label, "当时进行中");
  assert.match(historical.currentStep, /^当时步骤/);
  for (const update of [
    { connected: false },
    { online: false },
    { session: { state: "unknown", frozen: true } },
    { session: { state: "stopping", frozen: false } },
    {
      session: {
        state: "idle",
        frozen: false,
        executionRecovery: { state: "retired" },
      },
    },
  ]) {
    const cached = taskPlanPresentation(plan, { ...context, ...update });
    assert.equal(cached.active, false);
    assert.equal(cached.completed, 1);
    assert.notEqual(cached.label, "执行中");
  }
  for (const turnStatus of ["completed", "failed", "interrupted"]) {
    const ended = taskPlanPresentation(
      { ...plan, turnStatus },
      { ...context, session: { state: "idle", frozen: false } },
    );
    assert.equal(ended.completed, 1);
    assert.equal(ended.active, false);
    assert.notEqual(ended.tone, "complete");
    assert.match(ended.note, /部分步骤尚未标记完成/);
  }
  const finished = taskPlanPresentation(
    {
      ...plan,
      turnStatus: "completed",
      steps: plan.steps.map((step) => ({ ...step, status: "completed" })),
    },
    context,
  );
  assert.equal(finished.completed, 3);
  assert.equal(finished.tone, "complete");
  assert.equal(finished.note, "");
  const paused = { ...context, session: { state: "paused", frozen: true } };
  assert.equal(taskPlanPresentation(plan, paused).label, "已暂停");
  assert.equal(
    taskPlanPresentation({ ...plan, turnStatus: "interrupted" }, paused).label,
    "回合已中断",
  );
  assert.equal(JSON.stringify(plan), original);
});
