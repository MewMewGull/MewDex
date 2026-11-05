import { test } from "node:test";
import assert from "node:assert/strict";
import { fileURLToPath } from "node:url";
import { createElement } from "react";
import { renderToStaticMarkup } from "react-dom/server";
import { createServer } from "vite";

test("the composer displays the selected conversation device even when a Hub is online", async () => {
  const server = await createServer({
    configFile: false,
    root: fileURLToPath(new URL("..", import.meta.url)),
    server: { middlewareMode: true, hmr: false, watch: null },
    optimizeDeps: { noDiscovery: true, include: [] },
    appType: "custom",
    esbuild: { jsx: "automatic" },
  });
  try {
    const { DevicePicker } = await server.ssrLoadModule(
      "/src/components/DevicePicker.tsx",
    );
    const hub = {
      id: "hub",
      name: "Current Hub",
      platform: "hub",
      roles: ["coordinator", "executor"],
      projects: [],
      online: true,
    };
    const legacy = {
      id: "legacy",
      name: "Previous Mac",
      platform: "darwin/arm64",
      roles: ["executor"],
      projects: [],
      online: false,
    };
    const render = (selectedId) =>
      renderToStaticMarkup(
        createElement(DevicePicker, {
          devices: [hub, legacy],
          selectedId,
          connected: true,
          onSelect() {},
        }),
      );
    const oldConversation = render(legacy.id);
    assert.ok(oldConversation.includes(legacy.name));
    assert.ok(oldConversation.includes('title="离线"'));
    assert.ok(!oldConversation.includes(hub.name));
    const currentConversation = render(hub.id);
    assert.ok(currentConversation.includes(hub.name));
    assert.ok(currentConversation.includes('title="在线"'));
    assert.ok(!render("missing-device").includes(hub.name));
  } finally {
    await server.close();
  }
});
