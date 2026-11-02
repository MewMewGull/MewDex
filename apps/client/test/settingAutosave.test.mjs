import { test } from "node:test";
import assert from "node:assert/strict";
import { SettingAutosave } from "../src/settingAutosave.ts";

function deferred() {
  let resolve, reject;
  const promise = new Promise((yes, no) => { resolve = yes; reject = no; });
  return { promise, resolve, reject };
}

test("autosave coalesces valid edits, retains invalid input, and flushes without a button", async () => {
  const saved = [];
  const queue = new SettingAutosave("47830", async (value) => saved.push(value));
  queue.change("47831"); queue.change("47832");
  assert.equal(queue.state.pending, true);
  await queue.flush();
  assert.deepEqual(saved, ["47832"]);
  assert.equal(queue.state.phase, "saved");
  queue.change("", false); await queue.flush();
  assert.deepEqual(saved, ["47832"]);
  assert.equal(queue.state.phase, "invalid");
  assert.equal(queue.adopt("47830"), false);
});

test("an older write and polling cannot overwrite edits made while saving", async () => {
  const first = deferred(), saved = [];
  const queue = new SettingAutosave("47830", async (value) => {
    saved.push(value); if (saved.length === 1) await first.promise;
  });
  queue.change("47831"); const saving = queue.flush();
  queue.change("47832"); queue.change("47833");
  assert.equal(queue.adopt("47831"), false);
  first.resolve(); await saving;
  assert.deepEqual(saved, ["47831", "47833"]);
  assert.equal(queue.state.pending, false);
  assert.equal(queue.state.phase, "saved");
});

test("failed acknowledgments are not retried and a later user reversal is still persisted", async () => {
  let attempts = 0;
  const failed = new SettingAutosave("47830", async () => { attempts++; throw new Error("lost acknowledgment"); });
  failed.change("47831"); await failed.flush(); await failed.flush();
  assert.equal(attempts, 1);
  assert.equal(failed.state.phase, "error");
  const first = deferred(), saved = [];
  const queue = new SettingAutosave("47830", async (value) => {
    saved.push(value); if (saved.length === 1) await first.promise;
  });
  queue.change("47831"); const saving = queue.flush();
  queue.change("47830"); first.reject(new Error("lost acknowledgment")); await saving;
  assert.deepEqual(saved, ["47831", "47830"]);
});
