import { test } from "node:test";
import assert from "node:assert/strict";
import {
  commandOutputsFrom,
  combinedCommandOutput,
  terminalCopyText,
  latestDiffFrom,
} from "../src/outputs.ts";

const event = (id, kind, payload, sessionId = "work") => ({
  id,
  kind,
  payload,
  sessionId,
});
const item = (id, command, extra = {}) => ({
  item: { id, type: "commandExecution", command, cwd: "D:\\work", ...extra },
});

test("command output keeps the owning machine and original permission snapshot", () => {
  const target = {
    deviceId: "node",
    deviceName: "Build machine",
    projectId: "project",
    accessMode: "workspace-network",
    workspace: "/work/project",
  };
  const commands = commandOutputsFrom(
    [
      event(
        "start",
        "engine.item/started",
        item("job", "build", { executionTarget: target, status: "inProgress" }),
      ),
      event(
        "end",
        "engine.item/completed",
        item("job", "build", { status: "completed", aggregatedOutput: "done" }),
      ),
    ],
    "work",
  );
  assert.equal(commands.length, 1);
  assert.equal(commands[0].executionTarget.deviceId, "node");
  assert.equal(commands[0].executionTarget.deviceName, "Build machine");
  assert.equal(commands[0].executionTarget.accessMode, "workspace-network");
  assert.equal(commands[0].executionTarget.workspace, "/work/project");
});

test("command snapshots replace fragments and simultaneous commands keep their own output", () => {
  const events = [
    event(
      "start-a",
      "engine.item/started",
      item("a", "build", { status: "inProgress" }),
    ),
    event(
      "start-b",
      "engine.item/started",
      item("b", "check", { status: "inProgress" }),
    ),
    event("delta-a", "engine.item/commandExecution/outputDelta", {
      itemId: "a",
      delta: "building",
    }),
    event("delta-b", "engine.item/commandExecution/outputDelta", {
      itemId: "b",
      delta: "checking",
    }),
    event(
      "end-a",
      "engine.item/completed",
      item("a", "build", {
        aggregatedOutput: "building done",
        status: "completed",
        exitCode: 0,
        durationMs: 125,
      }),
    ),
    event(
      "other",
      "engine.item/commandExecution/outputDelta",
      { itemId: "a", delta: "foreign" },
      "private",
    ),
  ];
  const commands = commandOutputsFrom(events, "work");
  assert.equal(commands.length, 2);
  assert.equal(commands[0].output, "building done");
  assert.equal(commands[0].exitCode, 0);
  assert.equal(commands[0].durationMs, 125);
  assert.equal(commands[0].partial, false);
  assert.equal(commands[1].output, "checking");
  assert.equal(
    combinedCommandOutput(commands),
    "$ build\nbuilding done\n\n$ check\nchecking",
  );
  const fragment = event(
    "fragment",
    "engine.item/commandExecution/outputDelta",
    { itemId: "old", delta: "middle" },
  );
  assert.equal(commandOutputsFrom([fragment], "work")[0].partial, true);
  const completed = event(
    "snapshot",
    "engine.item/completed",
    item("old", "old-build", {
      aggregatedOutput: "begin middle end",
      status: "completed",
    }),
  );
  assert.equal(
    commandOutputsFrom([fragment, completed], "work")[0].output,
    "begin middle end",
  );
  assert.equal(commandOutputsFrom([completed], "work")[0].partial, false);
  assert.equal(
    latestDiffFrom(
      [
        event("d1", "engine.turn/diff/updated", { diff: "own" }),
        event("d2", "engine.turn/diff/updated", { diff: "foreign" }, "private"),
      ],
      "work",
    ),
    "own",
  );
});

test("copying terminal output removes color and hidden hyperlink controls", () => {
  const text =
    "\x1b[32m通过\x1b[0m\r\n\x1b]8;;https://example.test/hidden\x1b\\链接\x1b]8;;\x1b\\\r下一行";
  assert.equal(terminalCopyText(text), "通过\n链接\n下一行");
});
