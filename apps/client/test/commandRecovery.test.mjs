import { test } from "node:test";
import assert from "node:assert/strict";
import { CommandOutcomeError } from "../../../packages/sdk/src/index.ts";
import {
  commandRecoveryView,
  isUnconfirmedCommand,
} from "../src/commandRecovery.ts";

test("pending operation groups put the current device first and identify only the original visible conversation or project", () => {
  const pending = [
    {
      command: {
        id: "mac",
        deviceId: "mac",
        sessionId: "mac-chat",
        kind: "stop",
      },
      draft: "private input",
    },
    {
      command: {
        id: "create",
        deviceId: "windows",
        projectId: "work",
        kind: "create",
      },
      draft: "private input",
    },
    {
      command: {
        id: "old",
        deviceId: "windows",
        sessionId: "removed-chat",
        kind: "start",
      },
      draft: "private input",
    },
    {
      command: {
        id: "unnamed",
        deviceId: "windows",
        sessionId: "unnamed-chat",
        kind: "stop",
      },
      draft: "private input",
    },
    {
      command: {
        id: "removed",
        deviceId: "removed",
        sessionId: "mac-chat",
        kind: "start",
      },
      draft: "private input",
    },
  ];
  const devices = [
    {
      id: "windows",
      name: "Windows 工作台",
      projects: [{ id: "work", name: "MewDex" }],
    },
    { id: "mac", name: "Mac Mini", projects: [] },
  ];
  const sessions = [
    { id: "mac-chat", deviceId: "mac", title: "Mac 部署讨论" },
    { id: "unnamed-chat", deviceId: "windows", title: "" },
  ];
  const before = JSON.stringify({ pending, devices, sessions });
  const view = commandRecoveryView(pending, "windows", devices, sessions);
  assert.equal(view.currentDeviceName, "Windows 工作台");
  assert.equal(view.currentCount, 3);
  assert.equal(view.otherCount, 2);
  assert.deepEqual(
    view.groups.map((group) => group.deviceId),
    ["windows", "mac", "removed"],
  );
  assert.equal(view.groups[0].items[0].targetLabel, "MewDex · 新对话");
  assert.equal(view.groups[0].items[1].targetLabel, "原对话暂不可用");
  assert.equal(view.groups[0].items[2].targetLabel, "未命名对话");
  assert.equal(view.groups[1].items[0].targetLabel, "Mac 部署讨论");
  assert.equal(view.groups[1].items[0].session, sessions[0]);
  assert.equal(view.groups[2].deviceName, "原设备暂不可用");
  assert.equal(view.groups[2].items[0].session, undefined);
  assert.equal(view.groups[2].items[0].targetLabel, "原对话暂不可用");
  assert.equal(JSON.stringify({ pending, devices, sessions }), before);
  const empty = commandRecoveryView([], "windows", devices, sessions);
  assert.equal(empty.groups.length, 0);
  assert.equal(empty.currentCount + empty.otherCount, 0);
});

test("only accepted or unknown outcomes move to operation recovery; ordinary and definite failures remain visible errors", () => {
  assert.equal(
    isUnconfirmedCommand(
      new CommandOutcomeError("original-id", "accepted", "still processing"),
    ),
    true,
  );
  assert.equal(
    isUnconfirmedCommand(
      new CommandOutcomeError("original-id", "unknown", "outcome unknown"),
    ),
    true,
  );
  assert.equal(
    isUnconfirmedCommand(
      new CommandOutcomeError("original-id", "failed", "definite failure"),
    ),
    false,
  );
  assert.equal(
    isUnconfirmedCommand(new Error("network configuration is invalid")),
    false,
  );
});
