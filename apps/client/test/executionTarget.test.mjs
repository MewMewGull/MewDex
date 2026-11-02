import { test } from "node:test";
import assert from "node:assert/strict";
import { executionTarget } from "../src/executionTarget.ts";

test("execution requires the original visible project and executor role; offline drafts remain editable", () => {
  const device = {
    roles: ["executor"],
    projects: [{ id: "available", name: "Available", path: "D:/workspace" }],
    online: true,
  };
  const missingDevice = executionTarget(undefined, "available");
  assert.equal(missingDevice.issue, "device");
  assert.equal(missingDevice.canEdit, false);
  assert.equal(missingDevice.canExecute, false);

  const removedProject = executionTarget(device, "removed");
  assert.equal(removedProject.issue, "project");
  assert.equal(removedProject.project, undefined);
  assert.equal(removedProject.canEdit, false);
  assert.equal(removedProject.canExecute, false);
  assert.equal(
    executionTarget({ ...device, projects: [] }, "").canExecute,
    false,
  );

  const inputOnly = executionTarget(
    { ...device, roles: ["client"] },
    "available",
  );
  assert.equal(inputOnly.issue, "execution");
  assert.equal(inputOnly.canEdit, true);
  assert.equal(inputOnly.canExecute, false);

  const offline = executionTarget({ ...device, online: false }, "available");
  assert.equal(offline.issue, null);
  assert.equal(offline.canEdit, true);
  assert.equal(offline.canExecute, true);
  assert.equal(offline.project.id, "available");
});
