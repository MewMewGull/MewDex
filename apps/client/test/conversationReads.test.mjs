import { test } from "node:test";
import assert from "node:assert/strict";
import { ConversationReadLedger } from "../src/conversationReads.ts";

function storage() {
  const values = new Map();
  return {
    getItem: (key) => values.get(key) ?? null,
    setItem: (key, value) => values.set(key, value),
    removeItem: (key) => values.delete(key),
    values,
  };
}
const update = (sessionId, sequence, deviceId = "windows") => ({
  deviceId,
  sessionId,
  sequence,
});
const snapshot = (cursor, updates) => ({
  cursor,
  sessions: updates.map(({ sessionId, deviceId }) => ({
    id: sessionId,
    deviceId,
  })),
  conversationUpdates: updates,
});
const reply = (sessionId, sequence, deviceId = "windows") => ({
  id: `reply-${sequence}`,
  kind: "engine.item/completed",
  sequence,
  sessionId,
  deviceId,
  payload: {
    item: {
      id: `message-${sequence}`,
      type: "agentMessage",
      text: "sensitive reply body",
    },
  },
});

test("new replies stay unread until the matching rendered position is read", () => {
  const saved = storage();
  const ledger = new ConversationReadLedger(saved, "network:input");
  ledger.bootstrap(snapshot(10, [update("a", 8)]));
  assert.deepEqual([...ledger.unread], []);
  for (const event of [
    { ...reply("a", 11), kind: "engine.item/agentMessage/delta" },
    { ...reply("a", 12), kind: "message.user" },
    {
      ...reply("a", 13),
      payload: { item: { type: "commandExecution", text: "tool" } },
    },
  ])
    assert.equal(ledger.accept(event), false);
  assert.equal(ledger.accept(reply("a", 14)), true);
  assert.equal(ledger.accept(reply("a", 14)), false);
  assert.equal(ledger.read("mac", "a", 14), false);
  assert.equal(ledger.read("windows", "a", 13), false);
  assert.equal(ledger.unread.has("a"), true);
  assert.equal(ledger.read("windows", "a", 14), true);
  assert.equal(ledger.unread.has("a"), false);
  ledger.accept({
    ...reply("a", 18),
    kind: "conversation.changed",
    payload: update("a", 18),
  });
  assert.equal(ledger.read("windows", "a", 14), false);
  assert.equal(ledger.unread.has("a"), true);
  assert.equal(ledger.read("windows", "a", 18), true);
  assert.equal(
    JSON.stringify([...saved.values]).includes("sensitive reply body"),
    false,
  );
});

test("restart catches offline replies without sharing read positions between inputs", () => {
  const saved = storage();
  const ledger = new ConversationReadLedger(saved, "input-one");
  ledger.bootstrap(snapshot(20, [update("a", 10), update("b", 18, "mac")]));
  ledger.accept(reply("a", 23));
  ledger.read("windows", "a", 23);
  ledger.accept(reply("b", 25, "mac"));
  const reopened = new ConversationReadLedger(saved, "input-one");
  const fresh = snapshot(50, [
    update("a", 23),
    update("b", 47, "mac"),
    update("c", 49),
  ]);
  reopened.bootstrap(fresh);
  assert.deepEqual([...reopened.unread].sort(), ["b", "c"]);
  const secondInput = new ConversationReadLedger(saved, "input-two");
  secondInput.bootstrap(fresh);
  assert.deepEqual([...secondInput.unread], []);
  reopened.bootstrap(snapshot(50, [update("a", 23)]));
  assert.deepEqual([...reopened.unread], []);
  reopened.clear();
  assert.equal(saved.getItem("mewdex.read-positions.v1:input-one"), null);
});

test("bootstrap races, older Hubs and a restored event counter retain clear boundaries", () => {
  const ledger = new ConversationReadLedger(storage(), "input");
  assert.equal(ledger.bootstrap({ cursor: 10, sessions: [] }), false);
  assert.equal(ledger.accept(reply("a", 11)), false);
  ledger.bootstrap(snapshot(10, [update("a", 12)]));
  assert.equal(ledger.unread.has("a"), true);
  assert.equal(ledger.read("windows", "a", 10), false);
  ledger.bootstrap(snapshot(20, [update("a", 12)]));
  assert.equal(ledger.unread.has("a"), true);
  ledger.read("windows", "a", 12);
  ledger.bootstrap(snapshot(5, [update("a", 5)]));
  assert.deepEqual([...ledger.unread], []);
  ledger.accept(reply("a", 6));
  assert.equal(ledger.unread.has("a"), true);
});
