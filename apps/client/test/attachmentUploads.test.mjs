import { test } from "node:test";
import assert from "node:assert/strict";
import {
  AttachmentUploads,
  maxAttachmentBytes,
} from "../src/attachmentUploads.ts";

const target = (draftKey, deviceId = "windows") => ({
  draftKey,
  deviceId,
  projectId: "work",
  sessionId: draftKey,
});
const file = (name) => new File([name], name);
const settle = () => new Promise(setImmediate);
function harness() {
  const calls = [],
    delivered = [];
  const queue = new AttachmentUploads(
    (file, target, signal) => {
      let resolve, reject;
      const promise = new Promise((yes, no) => {
        resolve = yes;
        reject = no;
      });
      calls.push({ file, target, signal, resolve, reject });
      return promise;
    },
    (target, attachment, source) =>
      delivered.push({ target, attachment, source }),
  );
  const succeed = (index, id, overrides = {}) =>
    calls[index].resolve({
      id,
      name: calls[index].file.name,
      targetDeviceId: calls[index].target.deviceId,
      targetProjectId: calls[index].target.projectId,
      ...overrides,
    });
  return { queue, calls, delivered, succeed };
}

test("parallel files retain selection order across an individual failure and retry", async () => {
  const { queue, calls, delivered, succeed } = harness();
  queue.add(
    [file("first.png"), file("second.png"), file("third.txt")],
    target("original-draft"),
  );
  assert.equal(calls.length, 2);
  succeed(1, "second");
  await settle();
  assert.equal(calls.length, 3);
  assert.deepEqual(delivered, []);
  assert.equal(queue.getSnapshot()[1].state, "ready");
  assert.equal(queue.getSnapshot()[1].preview, calls[1].file);
  calls[0].reject(new Error("connection interrupted"));
  succeed(2, "third");
  await settle();
  assert.equal(queue.getSnapshot()[0].state, "failed");
  assert.deepEqual(delivered, []);
  queue.retry(queue.getSnapshot()[0].id);
  assert.equal(calls.length, 4);
  assert.equal(calls[3].file.name, "first.png");
  succeed(3, "first");
  await settle();
  assert.deepEqual(
    delivered.map((item) => item.attachment.id),
    ["first", "second", "third"],
  );
  assert.equal(
    delivered.every((item) => item.target.draftKey === "original-draft"),
    true,
  );
  assert.deepEqual(queue.getSnapshot(), []);
  assert.equal(delivered[0].source, calls[0].file);
  assert.equal(delivered[1].source, calls[1].file);
});

test("other drafts complete independently and moved or cancelled uploads keep their original scope", async () => {
  const { queue, calls, delivered, succeed } = harness();
  queue.add([file("a.txt")], target("new-draft"));
  queue.add([file("b.txt")], target("other-draft", "mac"));
  queue.move("new-draft", "created-session", "session-id");
  succeed(1, "other");
  await settle();
  assert.equal(delivered[0].target.draftKey, "other-draft");
  assert.equal(
    queue.getSnapshot().some((item) => item.target.draftKey === "other-draft"),
    false,
  );
  succeed(0, "original");
  await settle();
  assert.equal(delivered[1].target.draftKey, "created-session");
  assert.equal(delivered[1].target.sessionId, "session-id");
  assert.equal(delivered[1].target.deviceId, "windows");
  queue.add([file("cancelled.txt")], target("created-session"));
  queue.remove(queue.getSnapshot()[0].id);
  assert.equal(calls[2].signal.aborted, true);
  succeed(2, "cancelled-late-reply");
  await settle();
  assert.equal(delivered.length, 2);
  queue.add([file("old-input.txt")], target("old-input"));
  queue.clear();
  assert.equal(calls[3].signal.aborted, true);
  queue.add([file("current-input.txt")], target("current-input"));
  succeed(3, "old-input-late");
  succeed(4, "current-input");
  await settle();
  assert.deepEqual(
    delivered.map((item) => item.attachment.id),
    ["other", "original", "current-input"],
  );
});

test("oversized files do not stop valid transfers and foreign target results never join a draft", async () => {
  const { queue, calls, delivered, succeed } = harness();
  queue.add(
    [
      new File([new Uint8Array(maxAttachmentBytes + 1)], "oversized.bin"),
      file("valid.txt"),
      file("scope.txt"),
    ],
    target("draft"),
  );
  assert.equal(calls.length, 2);
  const oversized = queue.getSnapshot()[0];
  assert.equal(oversized.state, "failed");
  assert.equal(oversized.retryable, false);
  assert.equal(oversized.preview, undefined);
  succeed(0, "valid");
  succeed(1, "foreign", { targetDeviceId: "foreign-device" });
  await settle();
  assert.deepEqual(delivered, []);
  queue.remove(oversized.id);
  assert.deepEqual(
    delivered.map((item) => item.attachment.id),
    ["valid"],
  );
  assert.equal(queue.getSnapshot()[0].state, "failed");
  assert.match(queue.getSnapshot()[0].error, /原执行设备或项目/);
});
