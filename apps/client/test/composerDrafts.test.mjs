import { test } from "node:test";
import assert from "node:assert/strict";
import {
  composerKey,
  draftDestination,
  draftEntries,
  filterDraftEntries,
} from "../src/composerDrafts.ts";

const devices = [
  {
    id: "windows",
    name: "Windows 工作台",
    online: true,
    projects: [
      { id: "work", name: "主项目" },
      { id: "other", name: "其他项目" },
    ],
  },
  {
    id: "mac",
    name: "Mac Mini",
    online: false,
    projects: [{ id: "work", name: "文档项目" }],
  },
];
const sessions = [
  {
    id: "conversation",
    deviceId: "windows",
    projectId: "work",
    title: "部署讨论",
    archived: true,
  },
];
const upload = (draftKey, name, state = "queued") => ({
  id: name,
  name,
  state,
  target: { draftKey },
});

test("local draft library combines saved content and pending files without losing text or duplicating a draft", () => {
  const savedKey = composerKey(
    "windows",
    "conversation",
    "ignored-in-session-key",
  );
  const pendingKey = composerKey("mac", "", "work");
  const text = "  请保留这些空白\nDeploy PLAN 第二部分  ";
  const saved = {
    [savedKey]: { text, uploads: [{ id: "finished", name: "配置说明.md" }] },
  };
  const pending = [
    upload(savedKey, "模型.png", "failed"),
    upload(savedKey, "说明.txt", "ready"),
    upload(pendingKey, "手机参考图.png"),
  ];
  const original = JSON.stringify({ saved, pending });
  const entries = draftEntries(saved, pending, devices, sessions);
  assert.equal(entries.length, 2);
  const existing = entries.find((entry) => entry.key === savedKey);
  assert.equal(existing.title, "部署讨论");
  assert.equal(existing.projectId, "work");
  assert.equal(existing.projectName, "主项目");
  assert.equal(existing.text, text);
  assert.equal(existing.uploadedCount, 1);
  assert.equal(existing.pendingCount, 2);
  assert.equal(existing.failedCount, 1);
  assert.equal(existing.canEdit, true);
  assert.match(existing.notice, /已归档/);
  assert.equal(entries.find((entry) => entry.key === pendingKey).canEdit, true);
  assert.equal(filterDraftEntries(entries, "plan")[0].key, savedKey);
  assert.equal(filterDraftEntries(entries, "配置说明")[0].key, savedKey);
  assert.equal(filterDraftEntries(entries, "文档项目")[0].key, pendingKey);
  assert.equal(filterDraftEntries(entries, "手机参考图")[0].key, pendingKey);
  assert.equal(JSON.stringify({ saved, pending }), original);
});

test("resuming retains the exact project and device; missing targets and retired execution remain readable", () => {
  const wanted = draftDestination(
    composerKey("windows", "", "other"),
    devices,
    sessions,
  );
  assert.equal(wanted.projectId, "other");
  assert.equal(wanted.canEdit, true);
  const removedProjectKey = composerKey("windows", "", "removed");
  assert.equal(
    draftDestination(removedProjectKey, devices, sessions).canOpen,
    false,
  );
  assert.equal(
    draftDestination(composerKey("missing", "", "work"), devices, sessions)
      .canOpen,
    false,
  );
  assert.equal(
    draftDestination(
      composerKey("mac", "conversation", "work"),
      devices,
      sessions,
    ).canOpen,
    false,
  );
  const retiredSessions = [
    { ...sessions[0], executionRecovery: { state: "retired" } },
  ];
  const retired = draftDestination(
    composerKey("windows", "conversation", ""),
    devices,
    retiredSessions,
  );
  assert.equal(retired.canOpen, true);
  assert.equal(retired.canEdit, false);
  const drafts = {
    [removedProjectKey]: { text: "仍需保留的原项目草稿", uploads: [] },
  };
  const missing = draftEntries(drafts, [], devices, sessions)[0];
  assert.equal(missing.canOpen, false);
  assert.equal(missing.projectId, "removed");
  assert.equal(missing.text, drafts[removedProjectKey].text);
  assert.match(missing.notice, /原项目暂不可用/);
});
