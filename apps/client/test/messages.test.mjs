import { test } from "node:test";
import assert from "node:assert/strict";
import { messagesFrom } from "../src/messages.ts";

test("window control history retains its actual machine, permission and window", () => {
  const target = {
    deviceId: "windows",
    deviceName: "Windows executor",
    platform: "windows",
    projectId: "root",
    projectName: "Fixture",
    workspace: "D:/fixture",
    accessMode: "window-grant",
    harnessVersion: "revision-hash",
    harnessRevision: 3,
  };
  const messages = messagesFrom(
    [
      {
        id: "event",
        sequence: 1,
        sessionId: "conversation",
        kind: "engine.item/completed",
        payload: {
          item: {
            id: "window-action",
            type: "computerUse",
            operation: "act",
            window: "Owned fixture window",
            status: "unknown",
            executionTarget: target,
          },
        },
      },
    ],
    "conversation",
  );
  assert.equal(messages.length, 1);
  assert.equal(messages[0].toolKind, "computerUse");
  assert.ok(messages[0].text.includes("Owned fixture window"));
  assert.equal(messages[0].status, "unknown");
  assert.deepEqual(messages[0].executionTarget, target);
});

test("history fragments stay distinct from a complete reply snapshot", () => {
  let sequence = 10;
  const event = (id, kind, payload) => ({
    id,
    sequence: sequence++,
    kind,
    payload,
    sessionId: "conversation",
  });
  const fragment = event("fragment", "engine.item/agentMessage/delta", {
    itemId: "reply",
    delta: "中间片段",
  });
  const complete = event("complete", "engine.item/completed", {
    item: {
      id: "reply",
      type: "agentMessage",
      text: "完整回复，包含中间片段及后文。",
    },
  });
  const firstPage = messagesFrom([fragment], "conversation");
  assert.equal(firstPage[0].partial, true);
  assert.equal(firstPage[0].text, "中间片段");
  assert.equal(firstPage[0].sequence, fragment.sequence);
  const adjacentPage = messagesFrom([fragment, complete], "conversation");
  assert.equal(adjacentPage.length, 1);
  assert.equal(adjacentPage[0].partial, false);
  assert.equal(adjacentPage[0].text, "完整回复，包含中间片段及后文。");
  assert.equal(adjacentPage[0].sequence, complete.sequence);
  const restored = messagesFrom([complete], "conversation");
  assert.equal(restored[0].id, adjacentPage[0].id);
  assert.equal(restored[0].text, adjacentPage[0].text);
  assert.deepEqual(
    messagesFrom([fragment, complete], "other-conversation"),
    [],
  );
});

test("remote tool messages retain execution snapshots across partial and legacy events", () => {
  const target = {
    deviceId: "windows-node",
    deviceName: "Original machine",
    projectId: "source",
    workspace: "D:\\source",
    accessMode: "read-only",
  };
  const event = (sequence, extra) => ({
    id: `event-${sequence}`,
    sequence,
    sessionId: "conversation",
    kind: "engine.item/completed",
    payload: {
      item: {
        id: "command",
        type: "commandExecution",
        command: "build",
        status: "completed",
        ...extra,
      },
    },
  });
  const messages = messagesFrom(
    [event(1, { executionTarget: target }), event(2, {})],
    "conversation",
  );
  assert.equal(messages.length, 1);
  assert.equal(messages[0].executionTarget.deviceId, target.deviceId);
  assert.equal(messages[0].executionTarget.deviceName, target.deviceName);
  assert.equal(messages[0].executionTarget.workspace, target.workspace);
  assert.equal(messages[0].executionTarget.accessMode, "read-only");
  assert.equal(
    messagesFrom([event(1, {})], "conversation")[0].executionTarget,
    undefined,
  );
  assert.equal(
    messagesFrom(
      [event(1, { executionTarget: { deviceId: 42, deviceName: "invalid" } })],
      "conversation",
    )[0].executionTarget,
    undefined,
  );
});
