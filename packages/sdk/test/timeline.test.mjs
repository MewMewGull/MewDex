import { test } from "node:test";
import assert from "node:assert/strict";
import { EventWindow } from "../src/index.ts";

const event = (sequence, text = "x") => ({
  id: String(sequence),
  sequence,
  kind: "message.user",
  sessionId: "s",
  payload: { text },
  deviceId: "node",
  createdAt: "",
});

test("history and live events merge once and stay bounded in sequence order", () => {
  const window = new EventWindow({ maxEvents: 3, maxBytes: 10000 });
  window.append(event(5));
  window.merge([event(2), event(3), event(4), event(5)]);
  assert.deepEqual(
    window.events.map((e) => e.sequence),
    [3, 4, 5],
  );
  assert.equal(window.append(event(5)), false);
  window.append(event(1));
  assert.deepEqual(
    window.events.map((e) => e.sequence),
    [3, 4, 5],
  );
  assert.equal(window.truncated, true);
  window.clear();
  assert.deepEqual(window.events, []);
});

test("a single large event remains readable without retaining surrounding unbounded output", () => {
  const window = new EventWindow({ maxEvents: 100, maxBytes: 500 });
  window.merge([event(1), event(2, "large".repeat(1000))]);
  assert.deepEqual(
    window.events.map((e) => e.sequence),
    [2],
  );
  window.append(event(3));
  assert.deepEqual(
    window.events.map((e) => e.sequence),
    [3],
  );
});
