import { test } from "node:test";
import assert from "node:assert/strict";
import { setImmediate } from "node:timers/promises";
import { listenNativeNotificationHints } from "../src/nativeNotificationActions.ts";

const hint = (sessionId) => ({
  actionId: "tap",
  notificationId: 8,
  scope: "scope",
  deviceId: "node",
  sessionId,
});
function source() {
  const state = {
    hints: [],
    order: [],
    reads: 0,
    wake() {},
    resume() {},
    off: 0,
  };
  return {
    state,
    async subscribe(wake) {
      state.order.push("subscribe");
      state.wake = wake;
      return () => state.off++;
    },
    resume(wake) {
      state.order.push("resume");
      state.resume = wake;
      return () => state.off++;
    },
    async take() {
      state.order.push("take");
      state.reads++;
      return state.hints.shift() ?? null;
    },
  };
}

test("native startup, live actions, and resume all consume hints after subscribing", async () => {
  const input = source(),
    received = [];
  input.state.hints.push(hint("startup"));
  const listener = await listenNativeNotificationHints(input, (value) =>
    received.push(value),
  );
  assert.deepEqual(input.state.order, ["subscribe", "resume", "take"]);
  input.state.hints.push(hint("live"));
  input.state.wake();
  await setImmediate();
  input.state.hints.push(hint("resumed"));
  input.state.resume();
  await setImmediate();
  assert.deepEqual(
    received.map((value) => value.notification.extra.sessionId),
    ["startup", "live", "resumed"],
  );
  assert.ok(
    received.every(
      (value) => Object.keys(value.notification).sort().join() === "extra,id",
    ),
  );
  await listener.unregister();
  input.state.wake();
  input.state.resume();
  await setImmediate();
  assert.equal(input.state.reads, 3);
  assert.equal(input.state.off, 2);
});

test("native wakeups coalesce during an IPC read, and logout suppresses a late response", async () => {
  const input = source(),
    received = [];
  const listener = await listenNativeNotificationHints(input, (value) =>
    received.push(value),
  );
  let release;
  input.take = () => {
    input.state.reads++;
    return new Promise((resolve) => {
      release = resolve;
    });
  };
  input.state.wake();
  for (let i = 0; i < 1000; i++) input.state.wake();
  assert.equal(input.state.reads, 2);
  await listener.unregister();
  release(hint("logged-out"));
  await setImmediate();
  assert.deepEqual(received, []);
  assert.equal(input.state.reads, 2);
});

test("an unavailable native action inbox releases listeners and surfaces its initial failure", async () => {
  const input = source();
  input.take = async () => {
    throw new Error("inbox unavailable");
  };
  await assert.rejects(
    listenNativeNotificationHints(input, () => {}),
    /inbox unavailable/,
  );
  assert.equal(input.state.off, 2);
});
