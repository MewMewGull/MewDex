import { test } from "node:test";
import assert from "node:assert/strict";
import {
  InputAlertLedger,
  alertScope,
  inputAlert,
} from "../src/inputAlerts.ts";

function memory() {
  const values = new Map();
  return {
    values,
    getItem: (key) => values.get(key) ?? null,
    setItem: (key, value) => values.set(key, value),
    removeItem: (key) => values.delete(key),
  };
}
function completed(sequence, id = "turn", extra = {}) {
  return {
    sequence,
    id: `event-${sequence}`,
    deviceId: "node",
    sessionId: "s",
    kind: "engine.turn/completed",
    createdAt: "",
    payload: { turn: { id, status: "completed", text: "private model text" } },
    ...extra,
  };
}
const pending = (sequence, id = "question") =>
  completed(sequence, "unused", {
    kind: "interaction.updated",
    payload: {
      id,
      sessionId: "s",
      state: "pending",
      answers: { password: "private answer" },
    },
  });

test("system summaries only describe completion or pending requests, never content", () => {
  const success = inputAlert(completed(1)),
    request = inputAlert(pending(2));
  assert.equal(success.title, "MewDex");
  assert.equal(success.body, "一轮对话已完成。");
  assert.equal(request.body, "一个对话有待处理请求。");
  assert.ok(!JSON.stringify([success, request]).includes("private"));
  for (const event of [
    completed(3, "", {}),
    completed(3, "t", { sessionId: "" }),
    completed(3, "t", { kind: "engine.item/completed" }),
    completed(3, "t", { payload: { turn: { id: "t", status: "inProgress" } } }),
    pending(3, ""),
    completed(3, "t", {
      kind: "interaction.updated",
      payload: { id: "q", sessionId: "other", state: "pending" },
    }),
    completed(3, "t", {
      kind: "interaction.updated",
      payload: { id: "q", sessionId: "s", state: "resolved" },
    }),
  ])
    assert.equal(inputAlert(event), null);
});

test("an enabled input catches up from its saved cursor and deduplicates republished requests", () => {
  const storage = memory();
  let ledger = new InputAlertLedger(storage, "scope");
  ledger.bootstrap(10);
  ledger.setEnabled(true);
  assert.ok(ledger.accept(completed(11)));
  assert.ok(ledger.accept(pending(12)));
  ledger = new InputAlertLedger(storage, "scope");
  assert.equal(ledger.bootstrap(100), 12);
  assert.equal(ledger.accept(pending(13)), null);
  assert.equal(ledger.accept(completed(14)), null);
  assert.equal(ledger.accept(completed(12, "another")), null);
  assert.ok(ledger.accept(completed(15, "another")));
  ledger.checkpoint(100);
  ledger.flush();
  ledger = new InputAlertLedger(storage, "scope");
  assert.equal(ledger.bootstrap(110), 100);
  assert.ok(ledger.accept(pending(101, "new")));
});

test("the empty-network cursor remains a valid resume point, and disabled periods start fresh", () => {
  const storage = memory();
  let ledger = new InputAlertLedger(storage, "scope");
  ledger.bootstrap(0);
  ledger.setEnabled(true);
  ledger = new InputAlertLedger(storage, "scope");
  assert.equal(ledger.bootstrap(8), 0);
  assert.ok(ledger.accept(completed(1)));
  ledger.setEnabled(false);
  assert.equal(ledger.bootstrap(30), 30);
  ledger.setEnabled(true);
  assert.equal(ledger.accept(completed(20, "disabled-period")), null);
  assert.ok(ledger.accept(completed(31, "new")));
  assert.equal(ledger.bootstrap(2), 2);
  assert.ok(
    ledger.accept(completed(3, "new")),
    "new timeline resets semantic deduplication",
  );
});

test("scope changes are isolated and logout cannot be undone by a late flush", () => {
  const storage = memory(),
    ledger = new InputAlertLedger(storage, "first");
  ledger.bootstrap(9);
  ledger.setEnabled(true);
  ledger.accept(completed(10));
  assert.equal(new InputAlertLedger(storage, "second").enabled, false);
  ledger.clear();
  ledger.checkpoint(20);
  ledger.bootstrap(21);
  ledger.accept(completed(22));
  ledger.flush();
  assert.equal(storage.values.size, 0);
  assert.equal(new InputAlertLedger(storage, "first").enabled, false);
  assert.equal(
    alertScope("https://user:secret@hub.example/prefix/?token=secret", "input"),
    JSON.stringify(["https://hub.example", "/prefix", "input"]),
  );
});

test("semantic keys are unambiguous and saved data remains bounded", () => {
  assert.notEqual(
    inputAlert(completed(1, "t", { deviceId: "a:b", sessionId: "c" })).key,
    inputAlert(completed(1, "t", { deviceId: "a", sessionId: "b:c" })).key,
  );
  const storage = memory(),
    ledger = new InputAlertLedger(storage, "scope");
  ledger.bootstrap(1);
  ledger.setEnabled(true);
  for (let i = 2; i <= 600; i++) ledger.accept(completed(i, String(i)));
  const saved = JSON.parse([...storage.values.values()][0]);
  assert.equal(saved.seen.length, 512);
  assert.equal(saved.cursor, 600);
  assert.equal(inputAlert(completed(601, "x".repeat(600))), null);
  for (const invalid of [NaN, Infinity, -1, 0.1, Number.MAX_SAFE_INTEGER + 1]) {
    assert.equal(ledger.accept(completed(invalid)), null);
    ledger.checkpoint(invalid);
  }
  assert.equal(ledger.bootstrap(601), 600);
});

test("unusable saved data or unavailable storage does not affect conversation functions", () => {
  const storage = memory();
  for (const raw of [
    "null",
    "{",
    JSON.stringify({ enabled: true, cursor: -1, seen: [] }),
    "x".repeat(512 * 1024 + 1),
  ]) {
    storage.values.set("mewdex.alerts.v1:scope", raw);
    assert.equal(new InputAlertLedger(storage, "scope").enabled, false);
  }
  const denied = () => {
    throw new Error("storage denied");
  };
  const ledger = new InputAlertLedger(
    { getItem: denied, setItem: denied, removeItem: denied },
    "scope",
  );
  ledger.bootstrap(5);
  ledger.setEnabled(true);
  assert.ok(ledger.accept(completed(6)));
  ledger.clear();
  ledger.flush();
  assert.equal(ledger.enabled, false);
});
