import { test } from "node:test";
import assert from "node:assert/strict";
import { hostedHubURL, hubDisplayURL } from "../src/platform.ts";

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
