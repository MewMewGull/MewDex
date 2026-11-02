import { test } from "node:test";
import assert from "node:assert/strict";
import { PolicyDrafts } from "../src/policyDrafts.ts";
import { defaultPolicy } from "../src/policyValues.ts";

const initial = (basis = "v1", policy = defaultPolicy()) => ({
  policy,
  baseline: policy,
  basis,
});

test("unapplied policy values keep their original target and basis across reopening and network updates", () => {
  const drafts = new PolicyDrafts();
  const windows = '["windows","conversation-a",""]';
  const mac = '["mac","conversation-b",""]';
  const project = '["windows","","other-project"]';
  drafts.change(windows, initial(), {
    collaborationMode: "plan",
    instructions: "  保留原始空白\n",
    baseInstructions: "",
  });
  drafts.change(mac, initial("mac-v2"), { completion: "完成界面" });
  drafts.change(project, initial(""), { noTests: true });
  const reopened = drafts.getSnapshot().get(windows);
  const latest = initial("v3", { ...defaultPolicy(), model: "latest-model" });
  drafts.change(windows, latest, { mode: "edit" });
  const edited = drafts.getSnapshot().get(windows);
  assert.equal(edited.basis, "v1");
  assert.equal(edited.baseline, reopened.baseline);
  assert.equal(edited.policy.instructions, "  保留原始空白\n");
  assert.equal(edited.policy.baseInstructions, "");
  assert.equal(edited.policy.collaborationMode, "plan");
  assert.equal(edited.policy.model, undefined);
  assert.equal(drafts.getSnapshot().get(mac).basis, "mac-v2");
  assert.equal(drafts.getSnapshot().get(project).policy.noTests, true);
  drafts.reset(windows);
  assert.equal(drafts.getSnapshot().get(windows), undefined);
  assert.equal(drafts.getSnapshot().size, 2);
});

test("successful completion only clears its submitted policy, and clearing the input scope removes window drafts", () => {
  const drafts = new PolicyDrafts();
  const seed = initial();
  drafts.change("a", seed, { instructions: "first" });
  const submitted = drafts.getSnapshot().get("a");
  drafts.change("a", seed, { instructions: "later edit" });
  drafts.change("b", seed, { model: "other-model" });
  drafts.complete("a", submitted);
  assert.equal(drafts.getSnapshot().get("a").policy.instructions, "later edit");
  const current = drafts.getSnapshot().get("a");
  drafts.complete("a", current);
  assert.equal(drafts.getSnapshot().get("a"), undefined);
  assert.equal(drafts.getSnapshot().get("b").policy.model, "other-model");
  drafts.change("c", seed, {
    model: "",
    reasoningEffort: undefined,
    baseInstructions: null,
  });
  assert.equal(drafts.getSnapshot().has("c"), false);
  drafts.clear();
  assert.equal(drafts.getSnapshot().size, 0);
  drafts.complete("b", submitted);
  assert.equal(drafts.getSnapshot().size, 0);
  assert.equal(new PolicyDrafts().getSnapshot().size, 0);
});
