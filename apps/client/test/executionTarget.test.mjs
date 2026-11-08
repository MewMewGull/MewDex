import { test } from "node:test";
import assert from "node:assert/strict";
import { fileURLToPath } from "node:url";
import { createServer as createHTTPServer } from "node:http";
import { createElement } from "react";
import { renderToStaticMarkup } from "react-dom/server";
import { createServer } from "vite";
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

test("tool summaries and details show the machines, workspace and captured permissions", async () => {
  const server = await createServer({
    configFile: false,
    root: fileURLToPath(new URL("..", import.meta.url)),
    server: {
      middlewareMode: true,
      hmr: { server: createHTTPServer() },
      watch: null,
    },
    optimizeDeps: { noDiscovery: true, include: [] },
    appType: "custom",
    esbuild: { jsx: "automatic" },
  });
  try {
    const { MessageList } = await server.ssrLoadModule(
      "/src/input-terminal/MessageList.tsx",
    );
    const messages = [
      {
        id: "windows-job",
        sequence: 1,
        role: "tool",
        text: "build",
        status: "completed",
        toolKind: "commandExecution",
        executionTarget: {
          deviceId: "windows",
          deviceName: "Windows build",
          platform: "windows/amd64",
          projectId: "source",
          workspace: "D:\\source",
          accessMode: "workspace-network",
          harnessRevision: 7,
          harnessVersion: "a".repeat(64),
        },
      },
      {
        id: "mac-job",
        sequence: 2,
        role: "tool",
        text: "test",
        status: "inProgress",
        toolKind: "commandExecution",
        executionTarget: {
          deviceId: "mac",
          deviceName: "Mac test",
          platform: "darwin/arm64",
          projectId: "tests",
          workspace: "/work/tests",
          accessMode: "read-only",
        },
      },
    ];
    const html = renderToStaticMarkup(
      createElement(MessageList, { messages, client: {} }),
    );
    const summary = html.slice(
      html.indexOf("<summary"),
      html.indexOf("</summary>"),
    );
    assert.ok(summary.includes("Windows build"));
    assert.ok(summary.includes("Mac test"));
    assert.ok(html.includes('data-executor-id="windows"'));
    assert.ok(html.includes('data-executor-id="mac"'));
    assert.ok(html.includes("D:\\source"));
    assert.ok(html.includes("/work/tests"));
    assert.ok(html.includes("工作区与网络"));
    assert.ok(html.includes("只读访问"));
    assert.ok(html.includes("项目配置 v7"));
    assert.ok(html.includes('title="' + "a".repeat(64) + '"'));
    const legacy = renderToStaticMarkup(
      createElement(MessageList, {
        messages: [
          {
            id: "old",
            sequence: 1,
            role: "tool",
            text: "old command",
            toolKind: "commandExecution",
          },
        ],
        client: {},
      }),
    );
    assert.ok(legacy.includes("old command"));
    assert.ok(!legacy.includes("data-executor-id"));
    assert.ok(!legacy.includes("项目配置 v"));
  } finally {
    await server.close();
  }
});
