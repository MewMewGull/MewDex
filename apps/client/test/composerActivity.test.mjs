import { test } from "node:test";
import assert from "node:assert/strict";
import { ComposerActivity } from "../src/composerActivity.ts";
import { composerKey, completeComposerDraft } from "../src/composerDrafts.ts";

test("a slow send blocks only its original draft while other devices and conversations can finish independently", async () => {
  const activity = new ComposerActivity();
  const windows = composerKey("windows", "a", "project");
  const otherConversation = composerKey("windows", "b", "project");
  const mac = composerKey("mac", "a", "project");
  let release;
  const gate = new Promise((resolve) => {
    release = resolve;
  });
  const original = activity.begin(windows);
  assert.ok(original);
  assert.equal(activity.begin(windows), undefined);
  const slow = gate.then(() =>
    activity.finish(original, "原 Windows 发送失败"),
  );
  const other = activity.begin(otherConversation);
  const remote = activity.begin(mac);
  assert.ok(other);
  assert.ok(remote);
  assert.equal(activity.finish(other), true);
  assert.equal(activity.finish(remote), true);
  assert.deepEqual([...activity.getSnapshot().sending], [windows]);
  assert.equal(activity.getSnapshot().errors.has(mac), false);
  release();
  await slow;
  assert.equal(activity.getSnapshot().sending.size, 0);
  assert.equal(
    activity.getSnapshot().errors.get(windows),
    "原 Windows 发送失败",
  );
  assert.equal(activity.getSnapshot().errors.has(otherConversation), false);
  assert.ok(activity.begin(windows));
  assert.equal(activity.getSnapshot().errors.has(windows), false);
});

test("creating a conversation transfers waiting state and errors; old completions cannot unlock another send or restore private feedback", () => {
  const activity = new ComposerActivity();
  const source = composerKey("windows", "", "project");
  const destination = composerKey("windows", "created", "project");
  const submission = activity.begin(source);
  activity.move(source, destination);
  assert.equal(submission.key, destination);
  assert.equal(activity.getSnapshot().sending.has(source), false);
  assert.equal(activity.getSnapshot().sending.has(destination), true);
  assert.equal(activity.begin(destination), undefined);
  assert.ok(activity.begin(source));
  activity.finish(submission, "新对话内的回执待确认");
  assert.equal(
    activity.getSnapshot().errors.get(destination),
    "新对话内的回执待确认",
  );
  assert.equal(activity.getSnapshot().errors.has(source), false);
  const replacement = activity.begin(destination);
  assert.equal(activity.finish(submission, "迟到的旧结果"), false);
  assert.equal(activity.getSnapshot().sending.has(destination), true);
  assert.equal(activity.getSnapshot().errors.has(destination), false);
  const epoch = activity.getSnapshot().epoch;
  activity.clear();
  const afterReset = activity.begin(destination);
  assert.equal(activity.finish(replacement, "旧访问范围的错误"), false);
  activity.report(destination, "旧异步控制指令的错误", epoch);
  assert.equal(activity.getSnapshot().errors.size, 0);
  assert.equal(activity.getSnapshot().sending.has(destination), true);
  assert.equal(activity.finish(afterReset), true);
  assert.equal(activity.getSnapshot().sending.size, 0);
});

test("receipt completion clears the submitted snapshot while preserving later edits, new attachments and another draft", () => {
  const submitted = "  请保留原文空白\n完成 Windows 操作  ";
  const original = {
    text: submitted,
    uploads: [{ id: "sent" }, { id: "later" }],
  };
  const other = { text: "另一个设备的新输入", uploads: [{ id: "other" }] };
  const before = JSON.stringify({ original, other });
  const completed = completeComposerDraft(original, submitted, ["sent"]);
  assert.equal(completed.text, "");
  assert.deepEqual(
    completed.uploads.map((file) => file.id),
    ["later"],
  );
  const edited = completeComposerDraft(
    { ...original, text: submitted + "\n补充要求" },
    submitted,
    ["sent"],
  );
  assert.equal(edited.text, submitted + "\n补充要求");
  assert.deepEqual(
    edited.uploads.map((file) => file.id),
    ["later"],
  );
  assert.equal(JSON.stringify({ original, other }), before);
});
