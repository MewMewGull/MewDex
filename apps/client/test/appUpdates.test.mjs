import { test } from "node:test";
import assert from "node:assert/strict";
import { createRequire } from "node:module";
import { fileURLToPath } from "node:url";
import {
  readUpdatePreferences,
  updateCheckDue,
} from "../src/appUpdatePreferences.ts";

test("update preferences tolerate older, damaged and unavailable settings", () => {
  const defaults = {
    automatic: true,
    channel: "stable",
    ignoredTag: "",
    lastAttempt: 0,
  };
  for (const value of [null, "null", "{", "false", "1", "{}"])
    assert.deepEqual(readUpdatePreferences(value), defaults);
  assert.deepEqual(
    readUpdatePreferences(
      JSON.stringify({
        automatic: false,
        channel: "preview",
        ignoredTag: "v1.2.3",
        lastAttempt: 123,
      }),
    ),
    {
      automatic: false,
      channel: "preview",
      ignoredTag: "v1.2.3",
      lastAttempt: 123,
    },
  );
  assert.deepEqual(
    readUpdatePreferences('{"channel":"unknown","lastAttempt":-1}'),
    defaults,
  );
  assert.equal(readUpdatePreferences('{"automatic":true}').automatic, true);
  assert.equal(
    updateCheckDue(readUpdatePreferences(null), 48 * 3600 * 1000),
    true,
  );
  assert.equal(
    updateCheckDue(readUpdatePreferences("{}"), 48 * 3600 * 1000, true),
    true,
  );
});

test("background checks run daily, back off on errors and recover from clock changes", () => {
  const preferences = readUpdatePreferences(
    '{"automatic":true,"lastAttempt":1000}',
  );
  assert.equal(updateCheckDue(preferences, 1001), false);
  assert.equal(updateCheckDue(preferences, 1000 + 24 * 3600 * 1000), true);
  assert.equal(updateCheckDue(preferences, 1000 + 3600 * 1000, true), true);
  assert.equal(
    updateCheckDue(preferences, 1000 + 3600 * 1000 - 1, true),
    false,
  );
  assert.equal(updateCheckDue(preferences, 500), true);
  assert.equal(
    updateCheckDue(
      { ...preferences, automatic: false },
      1000 + 48 * 3600 * 1000,
    ),
    false,
  );
});

test("the settings switch controls automatic checks while manual checks stay available", async (t) => {
  const { build } = await import("esbuild");
  const result = await build({
    stdin: {
      contents:
        'export { useAppUpdates, startAppUpdateChecks } from "./src/appUpdates.ts"; export { calls } from "@tauri-apps/api/core";',
      resolveDir: fileURLToPath(new URL("..", import.meta.url)),
    },
    bundle: true,
    write: false,
    platform: "node",
    format: "cjs",
    external: ["zustand"],
    plugins: [
      {
        name: "updater-test-transport",
        setup(builder) {
          builder.onResolve(
            { filter: /^@tauri-apps\/api\/core$|^\.\/platform$/ },
            (args) => ({
              path: args.path,
              namespace: "updater-test",
            }),
          );
          builder.onLoad(
            { filter: /.*/, namespace: "updater-test" },
            (args) => ({
              contents:
                args.path === "./platform"
                  ? "export function subscribeResume() { return () => {}; }"
                  : 'export const calls = []; export class Channel {} export function isTauri() { return true; } export async function invoke(command) { calls.push(command); return command === "app_update_info" ? {currentVersion:"0.1.0",platform:"windows",arch:"x64",canDownload:true} : null; }',
            }),
          );
        },
      },
    ],
  });
  const previousStorage = Object.getOwnPropertyDescriptor(
    globalThis,
    "localStorage",
  );
  const saved = new Map();
  Object.defineProperty(globalThis, "localStorage", {
    configurable: true,
    value: {
      getItem: (key) => saved.get(key) ?? null,
      setItem: (key, value) => saved.set(key, value),
    },
  });
  t.after(() => {
    if (previousStorage)
      Object.defineProperty(globalThis, "localStorage", previousStorage);
    else delete globalThis.localStorage;
  });
  const module = { exports: {} };
  new Function("require", "module", "exports", result.outputFiles[0].text)(
    createRequire(import.meta.url),
    module,
    module.exports,
  );
  const { useAppUpdates, startAppUpdateChecks, calls } = module.exports;
  const checks = () =>
    calls.filter((command) => command === "app_update_check").length;
  const settle = () => new Promise((resolve) => setImmediate(resolve));
  assert.equal(useAppUpdates.getState().preferences.automatic, true);
  useAppUpdates.getState().configure({ automatic: false });
  const stop = startAppUpdateChecks();
  t.after(stop);
  await settle();
  await useAppUpdates.getState().check();
  useAppUpdates.getState().configure({ channel: "preview" });
  await settle();
  assert.equal(checks(), 0);
  await useAppUpdates.getState().check(true);
  assert.equal(checks(), 1);
  assert.equal(useAppUpdates.getState().preferences.automatic, false);
  useAppUpdates.getState().configure({ channel: "stable" });
  useAppUpdates.getState().configure({ automatic: true });
  await settle();
  assert.equal(checks(), 2);
  useAppUpdates.getState().configure({ automatic: false });
  await useAppUpdates.getState().check();
  assert.equal(checks(), 2);
  await useAppUpdates.getState().check(true);
  assert.equal(checks(), 3);
  assert.equal(JSON.parse(saved.get("mewdex.app-updates")).automatic, false);
});
