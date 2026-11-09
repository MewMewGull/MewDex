import { test } from "node:test";
import assert from "node:assert/strict";
import {
  hostedHubURL,
  hubDisplayURL,
  subscribeForeground,
  subscribeResume,
} from "../src/platform.ts";

test("foreground polling follows window focus and visibility and removes its listeners", (context) => {
  const original = {
    document: globalThis.document,
    window: globalThis.window,
    isTauri: globalThis.isTauri,
  };
  context.after(() => Object.assign(globalThis, original));
  context.mock.timers.enable({ apis: ["setTimeout"] });
  let focused = true;
  const document = Object.assign(new EventTarget(), {
    visibilityState: "visible",
    hasFocus: () => focused,
  });
  const window = new EventTarget();
  Object.assign(globalThis, { document, window, isTauri: false });
  const states = [];
  const off = subscribeForeground((active) => states.push(active));
  assert.deepEqual(states, [true]);
  focused = false;
  window.dispatchEvent(new Event("blur"));
  assert.deepEqual(states, [true, false]);
  focused = true;
  window.dispatchEvent(new Event("focus"));
  context.mock.timers.tick(150);
  assert.deepEqual(states, [true, false, true]);
  document.visibilityState = "hidden";
  document.dispatchEvent(new Event("visibilitychange"));
  window.dispatchEvent(new Event("focus"));
  context.mock.timers.tick(150);
  assert.deepEqual(states, [true, false, true, false]);
  document.visibilityState = "visible";
  document.dispatchEvent(new Event("visibilitychange"));
  assert.deepEqual(states, [true, false, true, false, true]);
  off();
  focused = false;
  window.dispatchEvent(new Event("blur"));
  document.dispatchEvent(new Event("visibilitychange"));
  context.mock.timers.tick(150);
  assert.deepEqual(states, [true, false, true, false, true]);
});

test("focus and visibility resumes share one notification and disposal cancels pending reads", (context) => {
  const original = {
    document: globalThis.document,
    window: globalThis.window,
    isTauri: globalThis.isTauri,
  };
  context.after(() => Object.assign(globalThis, original));
  context.mock.timers.enable({ apis: ["setTimeout"] });
  const document = Object.assign(new EventTarget(), {
    visibilityState: "visible",
  });
  const window = new EventTarget();
  Object.assign(globalThis, { document, window, isTauri: false });
  let reads = 0;
  const off = subscribeResume(() => reads++);
  window.dispatchEvent(new Event("focus"));
  document.dispatchEvent(new Event("visibilitychange"));
  window.dispatchEvent(new Event("online"));
  context.mock.timers.tick(149);
  assert.equal(reads, 0);
  context.mock.timers.tick(1);
  assert.equal(reads, 1);
  window.dispatchEvent(new Event("focus"));
  off();
  context.mock.timers.tick(150);
  assert.equal(reads, 1);
});

test("hosted Hub labels use the configured upstream while requests retain their entry address", (context) => {
  const original = {
    document: globalThis.document,
    window: globalThis.window,
    isTauri: globalThis.isTauri,
  };
  context.after(() => Object.assign(globalThis, original));
  const metadata = new Map([
    ['meta[name="mewdex-hub"]', "same-origin"],
    ['meta[name="mewdex-hub-url"]', "https://hub.example.test/project-api/"],
  ]);
  globalThis.isTauri = false;
  globalThis.window = { location: { origin: "http://127.0.0.1:47831" } };
  globalThis.document = {
    querySelector(selector) {
      const content = metadata.get(selector);
      return content === undefined ? null : { getAttribute: () => content };
    },
  };
  assert.equal(hostedHubURL(), "http://127.0.0.1:47831");
  assert.equal(
    hubDisplayURL(hostedHubURL()),
    "https://hub.example.test/project-api/",
  );
  assert.equal(
    hubDisplayURL("http://127.0.0.1:47831/"),
    "https://hub.example.test/project-api/",
  );
  assert.equal(
    hubDisplayURL("HTTP://127.0.0.1:47831"),
    "https://hub.example.test/project-api/",
  );
  assert.equal(hubDisplayURL("not a URL"), "not a URL");
  assert.equal(hubDisplayURL(""), "");
  assert.equal(
    hubDisplayURL("https://other.example.test"),
    "https://other.example.test",
  );
  assert.equal(
    hubDisplayURL("http://127.0.0.1:47831/another-api"),
    "http://127.0.0.1:47831/another-api",
  );
  metadata.delete('meta[name="mewdex-hub-url"]');
  assert.equal(hubDisplayURL(hostedHubURL()), "http://127.0.0.1:47831");
  metadata.set('meta[name="mewdex-hub-url"]', "https://hub.example.test");
  globalThis.window.location.origin = "https://workbench.example.test";
  assert.equal(
    hubDisplayURL("https://WORKBENCH.example.test:443/"),
    "https://hub.example.test",
  );
  globalThis.isTauri = true;
  assert.equal(hostedHubURL(), undefined);
  assert.equal(
    hubDisplayURL("http://127.0.0.1:47831"),
    "http://127.0.0.1:47831",
  );
});
