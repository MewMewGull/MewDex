import { test } from "node:test";
import assert from "node:assert/strict";
import { fileURLToPath } from "node:url";
import { createServer as createHTTPServer } from "node:http";
import { createElement } from "react";
import { renderToStaticMarkup } from "react-dom/server";
import { createServer } from "vite";

test("executor presence follows the project, groups directories, and marks stale snapshots", async () => {
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
    const { ExecutorStatus } = await server.ssrLoadModule(
      "/src/components/ExecutorStatus.tsx",
    );
    const node = (id, name, online) => ({
      id,
      name,
      online,
      platform: "windows/amd64",
      roles: ["executor"],
      projects: [],
    });
    const win = node("win", "Windows machine", true);
    const mac = node("mac", "Mac machine", false);
    const unrelated = node("other", "Other machine", true);
    const hub = {
      ...node("hub", "Hub machine", true),
      roles: ["coordinator", "executor"],
    };
    const devices = [win, mac, unrelated, hub];
    const directory = (deviceId, path) => ({
      deviceId,
      projectId: path,
      name: path,
      path,
    });
    const project = {
      id: "work",
      name: "Work",
      path: "",
      directories: [
        directory("win", "D:/work"),
        directory("mac", "/work"),
        directory("win", "D:/engine"),
      ],
    };
    const render = (extra = {}) =>
      renderToStaticMarkup(
        createElement(ExecutorStatus, {
          devices,
          owner: hub,
          project,
          connected: true,
          ...extra,
        }),
      );
    const initial = render();
    assert.ok(initial.includes("1/2 在线"));
    assert.equal((initial.match(/data-executor-id="win"/g) || []).length, 1);
    assert.ok(initial.includes("D:/work · D:/engine"));
    assert.ok(initial.includes("Mac machine"));
    assert.ok(initial.includes("离线"));
    assert.ok(!initial.includes("Other machine"));
    assert.ok(!initial.includes("Hub machine"));
    const recovered = render({
      devices: [win, { ...mac, online: true }, unrelated, hub],
    });
    assert.ok(recovered.includes("2/2 在线"));
    const disconnected = render({ connected: false });
    assert.ok(disconnected.includes("节点状态待同步"));
    assert.ok(disconnected.includes("上次在线"));
    assert.ok(disconnected.includes("上次离线"));
    assert.ok(!disconnected.includes('data-online="true"'));
    const missing = render({ devices: [win] });
    assert.ok(missing.includes("未找到执行端"));
    assert.ok(missing.includes('data-executor-id="mac"'));
    const awaiting = render({ devices: [], connected: false });
    assert.ok(awaiting.includes("等待同步"));
    assert.ok(!awaiting.includes("未找到执行端"));
    const switched = render({
      project: { ...project, directories: [directory("other", "/other")] },
    });
    assert.ok(switched.includes("Other machine"));
    assert.ok(!switched.includes("Windows machine"));
    const overview = render({ project: undefined });
    assert.ok(overview.includes("2/3 在线"));
    assert.ok(overview.includes("Other machine"));
    assert.ok(!overview.includes("Hub machine"));
    assert.ok(
      render({ project: { ...project, directories: [] } }).includes(
        "当前项目未绑定执行端",
      ),
    );
    const legacy = render({
      owner: mac,
      project: { id: "old", name: "Old", path: "/old" },
    });
    assert.ok(legacy.includes("Mac machine"));
    assert.ok(legacy.includes("/old"));
  } finally {
    await server.close();
  }
});
