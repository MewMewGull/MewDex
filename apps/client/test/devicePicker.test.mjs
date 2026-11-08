import { test } from "node:test";
import assert from "node:assert/strict";
import { fileURLToPath } from "node:url";
import { createServer as createHTTPServer } from "node:http";
import { createElement } from "react";
import { renderToStaticMarkup } from "react-dom/server";
import { createServer } from "vite";

test("the connected Hub opens management independently of the conversation's original device", async () => {
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
    const render = (selectedId, devices = [hub, legacy]) =>
      renderToStaticMarkup(
        createElement(DevicePicker, {
          devices,
          selectedId,
          connected: true,
          onSelect() {},
          onManageHub() {},
        }),
      );
    const oldConversation = render(legacy.id);
    assert.match(oldConversation, /^<button\b/);
    assert.match(oldConversation, /aria-haspopup="dialog"/);
    assert.ok(oldConversation.includes(hub.name));
    assert.ok(oldConversation.includes('aria-label="管理 Hub 与执行端"'));
    assert.ok(oldConversation.includes('title="在线"'));
    assert.ok(!oldConversation.includes(legacy.name));
    const currentConversation = render(hub.id);
    assert.match(currentConversation, /^<button\b/);
    assert.match(currentConversation, /aria-haspopup="dialog"/);
    assert.ok(currentConversation.includes(hub.name));
    assert.ok(currentConversation.includes('title="在线"'));
    assert.ok(render("missing-device").includes(hub.name));
    const newConversation = render("");
    assert.match(newConversation, /^<button\b/);
    assert.ok(newConversation.includes(hub.name));
    const legacyHub = render(legacy.id, [legacy]);
    assert.ok(legacyHub.includes(legacy.name));
    assert.ok(legacyHub.includes('title="离线"'));
    assert.ok(
      legacyHub.includes('aria-label="选择执行设备，当前为 Previous Mac"'),
    );
  } finally {
    await server.close();
  }
});
