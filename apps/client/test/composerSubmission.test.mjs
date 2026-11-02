import { test } from "node:test";
import assert from "node:assert/strict";
import { composerSubmission } from "../src/composerSubmission.ts";

test("composer commands distinguish new turns, live steering and explicit waiting without starting stopping or unknown execution", () => {
  const session = { state: "idle", turnId: "", frozen: false, stopStatus: "" };
  assert.equal(composerSubmission().command, "start");
  assert.equal(composerSubmission(session).command, "start");
  assert.equal(
    composerSubmission({ ...session, state: "failed" }).command,
    "start",
  );

  const running = { ...session, state: "running", turnId: "original-turn" };
  const steer = composerSubmission(running);
  assert.equal(steer.command, "steer");
  assert.equal(steer.label, "追加到当前任务");
  assert.equal(steer.queueVisible, true);
  assert.equal(composerSubmission({ ...running, turnId: "" }).command, null);

  for (const state of ["stopping", "unknown", "paused"]) {
    const view = composerSubmission({ ...session, state });
    assert.equal(view.command, null);
    assert.ok(view.description.length > 0);
  }
  const paused = composerSubmission({ ...running, frozen: true });
  assert.equal(paused.command, null);
  assert.equal(paused.queueVisible, true);
  assert.equal(paused.queueLabel, "加入暂停队列");
  assert.equal(
    composerSubmission({ ...session, stopStatus: "unconfirmed" }).command,
    null,
  );
  assert.equal(
    composerSubmission({ ...session, archived: true }).command,
    null,
  );
  const retired = composerSubmission({
    ...running,
    executionRecovery: { state: "retired" },
  });
  assert.equal(retired.command, null);
  assert.equal(retired.queueVisible, false);
  const background = composerSubmission({
    ...session,
    stopStatus: "background-running",
  });
  assert.equal(background.command, "start");
  assert.equal(background.queueVisible, true);
  assert.match(background.queueDescription, /后台任务退出/);
});
