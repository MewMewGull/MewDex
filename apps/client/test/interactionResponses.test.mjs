import { test } from "node:test";
import assert from "node:assert/strict";
import { CommandOutcomeError } from "../../../packages/sdk/src/index.ts";
import {
  InteractionResponses,
  initialInteractionResponse,
  interactionResponseKey,
} from "../src/interactionResponses.ts";

const session = (deviceId, id) => ({ deviceId, id });
const request = (sessionId, id = "question") => ({
  id,
  sessionId,
  kind: "questions",
  state: "pending",
  canAccept: true,
  questions: [
    { id: "text" },
    { id: "boolean", default: false },
    { id: "number", default: 0 },
    { id: "multi", default: ["first", "second"] },
  ],
});
const response = (manager, target, item) =>
  manager.getSnapshot().entries.get(interactionResponseKey(target, item)) ??
  initialInteractionResponse(item);
const deferred = () => {
  let resolve;
  const promise = new Promise((finish) => {
    resolve = finish;
  });
  return { promise, resolve };
};

test("question drafts survive navigation, submit once to the original conversation and retain answers after definite failure", async () => {
  const first = session("windows", "first");
  const second = session("mac", "second");
  const firstRequest = request(first.id);
  const secondRequest = request(second.id);
  const gate = deferred();
  const sent = [];
  const refreshed = [];
  let fail = true;
  const manager = new InteractionResponses(
    {
      respond: async (target, id, reply) => {
        sent.push({ target, id, reply });
        await gate.promise;
        if (fail) throw new Error("原请求暂时失败");
      },
      result: async () => {
        throw new Error("no receipt query expected");
      },
    },
    (id) => refreshed.push(id),
  );
  const initialSnapshot = manager.getSnapshot();
  manager.change(first, firstRequest, "text", "  Windows 补充要求  ");
  manager.change(second, secondRequest, "text", "Mac 的另一份回答");
  manager.retain(second, [secondRequest], manager.getSnapshot().epoch);
  assert.equal(
    response(manager, first, firstRequest).values.text,
    "  Windows 补充要求  ",
  );
  const pending = manager.reply(first, firstRequest, "accept");
  await manager.reply(first, firstRequest, "accept");
  assert.equal(sent.length, 1);
  assert.equal(response(manager, first, firstRequest).state, "submitting");
  assert.deepEqual(sent[0].reply.answers, {
    text: ["  Windows 补充要求  "],
    boolean: ["false"],
    number: ["0"],
    multi: ["first", "second"],
  });
  gate.resolve();
  await pending;
  assert.equal(response(manager, first, firstRequest).state, "idle");
  assert.equal(
    response(manager, first, firstRequest).values.text,
    "  Windows 补充要求  ",
  );
  assert.equal(response(manager, first, firstRequest).error, "原请求暂时失败");
  assert.equal(
    response(manager, second, secondRequest).values.text,
    "Mac 的另一份回答",
  );
  assert.deepEqual(refreshed, [first.id]);
  fail = false;
  await manager.reply(first, firstRequest, "accept");
  assert.equal(sent.length, 2);
  assert.equal(sent[1].target, first);
  assert.equal(response(manager, first, firstRequest).state, "sent");
  assert.deepEqual(response(manager, first, firstRequest).values, {});
  manager.retain(first, [], manager.getSnapshot().epoch);
  assert.equal(
    manager
      .getSnapshot()
      .entries.has(interactionResponseKey(first, firstRequest)),
    false,
  );
  assert.equal(
    response(manager, second, secondRequest).values.text,
    "Mac 的另一份回答",
  );
  assert.equal(initialSnapshot.entries.size, 0);
});

test("unconfirmed form replies retain their original receipt across navigation and observation never resends answers", async () => {
  const target = session("windows", "form-chat");
  const form = { ...request(target.id), kind: "form" };
  const other = session("mac", "other");
  const receipts = [];
  const gate = deferred();
  let sends = 0;
  const manager = new InteractionResponses(
    {
      respond: async (_target, _id, reply) => {
        sends++;
        assert.equal(reply.values.text, "private form value");
        throw new CommandOutcomeError(
          "original-response",
          "unknown",
          "response lost",
        );
      },
      result: async (id) => {
        receipts.push(id);
        return gate.promise;
      },
    },
    () => {},
  );
  manager.change(target, form, "text", "private form value");
  await manager.reply(target, form, "accept");
  manager.retain(other, [], manager.getSnapshot().epoch);
  const unknown = response(manager, target, form);
  assert.equal(unknown.state, "unconfirmed");
  assert.equal(unknown.commandId, "original-response");
  assert.deepEqual(unknown.values, {});
  await manager.reply(target, form, "accept");
  assert.equal(sends, 1);
  const observation = manager.observe(target, form);
  await manager.observe(target, form);
  assert.deepEqual(receipts, ["original-response"]);
  assert.equal(response(manager, target, form).reading, true);
  // The operation recovery window can finish the same receipt while the card's
  // older read is still pending. Its late unknown result must not relock it.
  manager.resolve(
    "original-response",
    { commandId: "original-response", state: "completed" },
    manager.getSnapshot().epoch,
  );
  gate.resolve({ commandId: "original-response", state: "unknown" });
  await observation;
  assert.equal(response(manager, target, form).state, "sent");
  assert.equal(response(manager, target, form).reading, false);
  assert.equal(response(manager, target, form).commandId, "");
  assert.equal(sends, 1);
});

test("clearing the input scope discards answers and late old results without unlocking or overwriting a new response", async () => {
  const target = session("windows", "conversation");
  const item = request(target.id);
  const oldGate = deferred();
  const newGate = deferred();
  let calls = 0;
  const settled = [];
  const manager = new InteractionResponses(
    {
      respond: async () => {
        calls++;
        await (calls === 1 ? oldGate.promise : newGate.promise);
      },
      result: async () => ({ state: "completed" }),
    },
    (id) => settled.push(id),
  );
  manager.change(target, item, "text", "old private answer");
  const oldEpoch = manager.getSnapshot().epoch;
  const oldSend = manager.reply(target, item, "accept");
  manager.clear();
  assert.equal(manager.getSnapshot().entries.size, 0);
  manager.change(target, item, "text", "new private answer");
  const newSend = manager.reply(target, item, "accept");
  manager.report(target, item, "old external link failure", oldEpoch);
  manager.resolve(
    "original-response",
    { state: "failed", error: "old result" },
    oldEpoch,
  );
  oldGate.resolve();
  await oldSend;
  assert.equal(response(manager, target, item).state, "submitting");
  assert.equal(
    response(manager, target, item).values.text,
    "new private answer",
  );
  assert.equal(response(manager, target, item).error, "");
  assert.deepEqual(settled, []);
  newGate.resolve();
  await newSend;
  assert.equal(response(manager, target, item).state, "sent");
  assert.deepEqual(response(manager, target, item).values, {});
  assert.deepEqual(settled, [target.id]);
});

test("numeric form editing survives intermediate text and navigation while only accepted submissions create typed values", async () => {
  const target = session("windows", "numeric-form");
  const other = session("mac", "other-form");
  const item = {
    ...request(target.id, "numeric"),
    kind: "form",
    questions: [
      {
        id: "amount",
        title: "数额",
        kind: "number",
        required: true,
        minimum: -10,
        maximum: 10,
        default: -1,
      },
      {
        id: "count",
        title: "数量",
        kind: "integer",
        required: true,
        minimum: 0,
        maximum: 3,
        default: 0,
      },
      { id: "optional", kind: "number", required: false },
      { id: "zero", kind: "number", default: 0 },
      { id: "flag", kind: "boolean", default: false },
      { id: "text", kind: "text" },
    ],
  };
  const otherItem = { ...item, sessionId: other.id };
  const sent = [];
  const refreshed = [];
  let fail = true;
  const manager = new InteractionResponses(
    {
      respond: async (session, id, reply) => {
        sent.push({ session, id, reply });
        if (fail) throw new Error("本次传输失败");
      },
      result: async () => {
        throw new Error("No receipt expected");
      },
    },
    (id) => refreshed.push(id),
  );

  manager.change(other, otherItem, "amount", "-2.5");
  for (const text of ["-", "1.", "1e-"]) {
    manager.change(target, item, "amount", text);
    manager.retain(other, [otherItem], manager.getSnapshot().epoch);
    assert.equal(response(manager, target, item).values.amount, text);
  }
  await manager.reply(target, item, "accept");
  assert.equal(sent.length, 0);
  assert.equal(response(manager, target, item).state, "idle");
  assert.equal(
    response(manager, target, item).fieldErrors.amount,
    "请输入有效数字。",
  );
  assert.deepEqual(refreshed, []);

  manager.change(target, item, "amount", "1e-2");
  assert.deepEqual(response(manager, target, item).fieldErrors, {});
  assert.equal(response(manager, target, item).error, "");
  manager.change(target, item, "count", "2.");
  manager.change(target, item, "optional", "  ");
  manager.change(target, item, "text", "  保留原始要求  ");
  await manager.reply(target, item, "accept");
  assert.equal(sent.length, 1);
  assert.equal(sent[0].session, target);
  assert.deepEqual(sent[0].reply.values, {
    amount: 0.01,
    count: 2,
    zero: 0,
    flag: false,
    text: "  保留原始要求  ",
  });
  assert.equal(response(manager, target, item).state, "idle");
  assert.equal(response(manager, target, item).values.amount, "1e-2");
  assert.equal(response(manager, target, item).values.count, "2.");
  assert.equal(response(manager, target, item).values.optional, "  ");
  assert.equal(response(manager, other, otherItem).values.amount, "-2.5");

  manager.change(target, item, "amount", "0");
  assert.equal(response(manager, target, item).error, "本次传输失败");
  fail = false;
  await manager.reply(target, item, "accept");
  assert.equal(sent[1].reply.values.amount, 0);
  assert.deepEqual(response(manager, target, item).values, {});
  assert.deepEqual(response(manager, target, item).fieldErrors, {});
  assert.deepEqual(refreshed, [target.id, target.id]);
});

test("numeric validation reports bounds and integers locally while declining never sends partial answers", async () => {
  const target = session("windows", "numeric-bounds");
  const item = {
    ...request(target.id),
    kind: "form",
    questions: [
      {
        id: "count",
        title: "数量",
        kind: "integer",
        required: true,
        minimum: -2,
        maximum: 3,
      },
      { id: "secret", kind: "secret" },
    ],
  };
  const sent = [];
  const manager = new InteractionResponses(
    {
      respond: async (_target, _id, reply) => {
        sent.push(reply);
      },
      result: async () => {
        throw new Error("No receipt expected");
      },
    },
    () => {},
  );
  for (const [text, error] of [
    ["", "请填写这个数字。"],
    ["-", "请输入有效数字。"],
    ["1e", "请输入有效数字。"],
    ["1e9999", "请输入有效数字。"],
    ["0x10", "请输入有效数字。"],
    ["2.5", "请输入整数。"],
    ["-3", "不能小于 -2。"],
    ["4", "不能大于 3。"],
  ]) {
    manager.change(target, item, "count", text);
    assert.deepEqual(response(manager, target, item).fieldErrors, {});
    await manager.reply(target, item, "accept");
    assert.equal(response(manager, target, item).fieldErrors.count, error);
    assert.equal(response(manager, target, item).state, "idle");
    assert.equal(response(manager, target, item).values.count, text);
  }
  assert.equal(sent.length, 0);
  manager.change(target, item, "secret", "partial private input");
  await manager.reply(target, item, "decline");
  assert.deepEqual(sent, [{ action: "decline" }]);
  assert.deepEqual(response(manager, target, item).values, {});
});
