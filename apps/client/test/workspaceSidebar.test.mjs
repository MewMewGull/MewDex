import { test } from "node:test";
import assert from "node:assert/strict";
import { fileURLToPath } from "node:url";
import { createServer as createHTTPServer } from "node:http";
import { createElement } from "react";
import { renderToStaticMarkup } from "react-dom/server";
import { createServer } from "vite";

test("the sidebar places unassigned conversations after projects and retains their conversation controls", async () => {
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
    const { WorkspaceSidebar } = await server.ssrLoadModule(
      "/src/input-terminal/WorkspaceSidebar.tsx",
    );
    const hub = {
      id: "hub",
      name: "Hub",
      platform: "hub",
      roles: ["coordinator"],
      projects: [{ id: "work", name: "Current project", path: "" }],
      online: true,
    };
    const oldDevice = {
      id: "old",
      name: "Previous Mac",
      platform: "darwin/arm64",
      roles: ["executor"],
      projects: [{ id: "risk", name: "Old workspace", path: "/old/work" }],
      online: false,
    };
    const recent = Array.from({ length: 8 }, (_, index) => ({
      id: `old-${index}`,
      title: `Unassigned conversation ${index}`,
      deviceId: index === 7 ? "missing-device" : oldDevice.id,
      projectId: "risk",
      state: "idle",
      updatedAt: `2026-11-0${index + 1}T00:00:00Z`,
    }));
    const current = {
      ...recent[0],
      id: "current",
      title: "Project conversation",
      deviceId: hub.id,
      projectId: "work",
    };
    const render = (devices, sessions) =>
      renderToStaticMarkup(
        createElement(WorkspaceSidebar, {
          open: true,
          brand: "MewDex",
          devices,
          sessions,
          deviceId: oldDevice.id,
          sessionId: "old-2",
          connected: "connected",
          draftSessions: new Set(["old-2"]),
          unreadSessions: new Set(["old-2"]),
          activeTasks: 0,
          attentionTasks: 0,
          canManageSessions: true,
          width: 280,
          projectId: "risk",
          account: null,
          ...Object.fromEntries(
            [
              "onActivity",
              "onSearch",
              "onSettings",
              "onManageHub",
              "onClose",
              "onDevice",
              "onSession",
              "onNew",
              "onUpdateSession",
              "onRenameSession",
              "onWidthChange",
              "onWorkspaces",
              "onProject",
              "onEditProject",
              "onProjectCategory",
            ].map((name) => [name, () => {}]),
          ),
        }),
      );
    const html = render([oldDevice, hub], [current, ...recent]);
    const recentStart = html.indexOf('class="recent-conversations"');
    assert.ok(recentStart > html.indexOf("Project conversation"));
    assert.equal((html.match(/class="workspace-group"/g) ?? []).length, 1);
    assert.equal((html.match(/class="session(?:\s|")/g) ?? []).length, 9);
    assert.ok(!html.includes("旧设备对话"));
    const recentHTML = html.slice(
      recentStart,
      html.indexOf("</section>", recentStart),
    );
    for (const session of recent) assert.ok(recentHTML.includes(session.title));
    assert.ok(
      recentHTML.indexOf(recent[7].title) < recentHTML.indexOf(recent[0].title),
    );
    assert.ok(recentHTML.includes('aria-current="page"'));
    assert.ok(recentHTML.includes('aria-label="有新回复"'));
    assert.ok(recentHTML.includes("草稿"));
    const onlyUnassigned = render([], recent);
    assert.ok(onlyUnassigned.includes('class="recent-conversations"'));
    assert.ok(!onlyUnassigned.includes("从新对话开始"));
  } finally {
    await server.close();
  }
});
