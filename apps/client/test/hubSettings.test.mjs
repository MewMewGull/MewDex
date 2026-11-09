import { test } from "node:test";
import assert from "node:assert/strict";
import { createServer as createHTTPServer } from "node:http";
import { fileURLToPath } from "node:url";
import { createElement } from "react";
import { renderToStaticMarkup } from "react-dom/server";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { createServer } from "vite";

test("settings classify input preferences and Hub administration by terminal ownership", async () => {
  const previousWindow = globalThis.window;
  globalThis.window = { matchMedia: () => ({ matches: false }) };
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
    plugins: [
      {
        name: "settings-navigation-fixture",
        enforce: "pre",
        load(id) {
          if (
            !id.replaceAll("\\", "/").endsWith("/components/SettingsFrame.tsx")
          )
            return;
          // Inspect the controller's actual navigation without mounting a portal.
          return `import { createElement } from 'react';
export function SettingsFrame({ sections }) {
  return createElement('nav', null, sections.map(section => createElement('button', {
    key: section.id, 'data-section': section.id, 'data-group': section.group,
  }, section.label)));
}`;
        },
      },
    ],
  });
  try {
    const { HubController } = await server.ssrLoadModule(
      "/src/hub-controller/HubController.tsx",
    );
    for (const role of ["admin", "client"]) {
      const queries = new QueryClient();
      try {
        const html = renderToStaticMarkup(
          createElement(
            QueryClientProvider,
            { client: queries },
            createElement(HubController, {
              client: {},
              devices: [],
              identity: { id: "input", role },
              open: true,
              onOpenChange() {},
              alerts: {},
              baseURL: "https://hub.example.invalid",
              loginKey: "fixture-key",
              onDisconnect() {},
              onChangeHub() {},
              draftSettings: {},
              workspaceView: { sendKey: "enter" },
            }),
          ),
        );
        const entries = [
          ...html.matchAll(/data-section="([^"]+)" data-group="([^"]+)"/g),
        ].map((match) => ({ id: match[1], group: match[2] }));
        assert.deepEqual(
          [...new Set(entries.map((item) => item.group))],
          ["输入端设置", "Hub 与执行端设置"],
        );
        const groups = new Map(entries.map((item) => [item.id, item.group]));
        for (const id of [
          "general",
          "appearance",
          "drafts",
          "account",
          "updates",
        ])
          assert.equal(groups.get(id), "输入端设置");
        for (const id of [
          "connection",
          "models",
          "skills",
          "memory",
          "hooks",
          "plugins",
        ])
          assert.equal(groups.get(id), "Hub 与执行端设置");
        if (role === "admin") {
          assert.equal(groups.get("input-pairing"), "输入端设置");
          assert.equal(groups.get("messaging"), "输入端设置");
          assert.equal(groups.get("pairing"), "Hub 与执行端设置");
          assert.equal(groups.get("access"), "Hub 与执行端设置");
        } else {
          for (const id of ["input-pairing", "messaging", "pairing", "access"])
            assert.equal(groups.has(id), false);
        }
      } finally {
        queries.clear();
      }
    }
  } finally {
    await server.close();
    if (previousWindow === undefined) delete globalThis.window;
    else globalThis.window = previousWindow;
  }
});
