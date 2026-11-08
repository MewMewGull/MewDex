import { test } from "node:test";
import assert from "node:assert/strict";
import {
  computerPresentation,
  computerTargets,
  computerTaskDraft,
} from "../src/computerUse.ts";

const win = {
  id: "win",
  name: "Windows 电脑",
  online: true,
  capabilities: ["computer-use-v1"],
};
const mac = { id: "mac", name: "Mac Mini", online: true, capabilities: [] };
const project = {
  id: "hub-project",
  directories: [
    { deviceId: "mac", projectId: "mac-root" },
    { deviceId: "win", projectId: "win-root" },
    { deviceId: "win", projectId: "win-other" },
  ],
};
const targets = computerTargets(project, [win, mac]);
const now = Date.parse("2026-11-09T00:00:00Z");
const control = {
  id: "request",
  deviceId: "win",
  ownerId: "conversation",
  state: "pending",
  reason: "检查目标页面",
  expiresAt: "2026-11-09T00:05:00Z",
  window: { title: "Browser" },
};
function present(state, extra = {}) {
  return computerPresentation({
    target: targets[1],
    connected: true,
    conversationId: "conversation",
    now,
    status: { supported: true, session: { ...control, state } },
    ...extra,
  });
}

test("computer targets use the executor's capability and preserve routing through duplicate directories", () => {
  assert.deepEqual(
    targets.map(({ deviceId, directory, supported }) => ({
      deviceId,
      directory,
      supported,
    })),
    [
      { deviceId: "mac", directory: 0, supported: false },
      { deviceId: "win", directory: 1, supported: true },
    ],
  );
  assert.equal(computerTargets(undefined, [win]).length, 0);
  assert.equal(computerTargets(project, [win])[0].supported, false);
  const unavailable = computerPresentation({
    target: targets[0],
    connected: true,
    now,
  });
  assert.equal(unavailable.state, "unsupported");
  assert.equal(unavailable.canStop, false);
  assert.equal(unavailable.attention, false);
});

test("authorization prompts name the target computer; valid authorization does not imply active input", () => {
  const pending = present("pending");
  assert.equal(pending.attention, true);
  assert.equal(pending.canStop, true);
  assert.match(pending.detail, /Windows 电脑.*系统托盘.*执行端面板/);
  for (const state of ["approved", "ready"]) {
    assert.equal(present(state).label, "窗口授权有效");
    assert.equal(present(state).canStop, true);
  }
  assert.equal(
    present("ready", {
      status: {
        supported: true,
        session: { ...control, state: "ready", lastActionState: "running" },
      },
    }).label,
    "正在操作窗口",
  );
  assert.equal(present("stopping").canStop, false);
  for (const state of ["stopped", "expired"]) {
    assert.equal(present(state).attention, false);
    assert.equal(present(state).canStop, false);
  }
});

test("stale, disconnected and foreign authorization never appears as confirmed control", () => {
  const disconnected = present("ready", { connected: false });
  assert.equal(disconnected.state, "offline");
  assert.equal(disconnected.canStop, false);
  assert.equal(disconnected.attention, true);
  assert.match(disconnected.detail, /仅供核对/);
  const failed = present("ready", { error: true });
  assert.equal(failed.state, "unknown");
  assert.equal(failed.canStop, true);
  assert.equal(present("ready", { now: now + 300_000 }).state, "expired");
  for (const update of [{ ownerId: "other" }, { deviceId: "mac" }]) {
    const foreign = present("ready", {
      status: { supported: true, session: { ...control, ...update } },
    });
    assert.equal(foreign.state, "unknown");
    assert.equal(foreign.current, undefined);
    assert.equal(foreign.canStop, false);
  }
  const invalidExpiry = present("approved", {
    status: {
      supported: true,
      session: { ...control, state: "approved", expiresAt: "invalid" },
    },
  });
  assert.equal(invalidExpiry.state, "unknown");
});

test("starting an operation keeps the user's draft and adds the selected device only once", () => {
  const draft = "请核对我打开的设置页。\n保留这个要求。";
  const text = computerTaskDraft(draft, win.name);
  assert.ok(text.endsWith(draft));
  assert.match(text, /Windows 电脑/);
  assert.equal(computerTaskDraft(text, win.name), text);
});

test("executor uncertainty takes precedence over cached authorization and device presence", () => {
  const unavailable = present("ready", {
    status: {
      supported: true,
      reason: "执行端离线，窗口控制状态尚未确认",
      session: { ...control, state: "ready" },
    },
  });
  assert.equal(unavailable.state, "unknown");
  assert.equal(unavailable.canStop, false);
  assert.match(unavailable.detail, /执行端离线/);
  const unknownAction = present("ready", {
    status: {
      supported: true,
      session: {
        ...control,
        state: "ready",
        lastActionState: "unknown",
        error: "输入结果待核对",
      },
    },
  });
  assert.equal(unknownAction.state, "unknown");
  assert.equal(unknownAction.canStop, true);
  assert.match(unknownAction.detail, /不要重复/);
});
