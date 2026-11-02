import { test } from "node:test";
import assert from "node:assert/strict";
import { MewDexClient } from "../src/index.ts";

test("attachment upload carries its original target and aborts the one authenticated request", async () => {
  const requests = [];
  const client = new MewDexClient({
    baseURL: "https://hub.example",
    token: "input-token",
    fetch: async (url, options) => {
      requests.push({ url, options });
      return new Promise((_resolve, reject) =>
        options.signal.addEventListener(
          "abort",
          () => reject(new DOMException("Cancelled", "AbortError")),
          { once: true },
        ),
      );
    },
  });
  const controller = new AbortController();
  const body = new Blob(["selected file"]);
  const request = client.upload(
    body,
    "报告.txt",
    { deviceId: "windows", projectId: "work", draftKey: "input-only-draft" },
    controller.signal,
  );
  controller.abort();
  await assert.rejects(request, (error) => error.name === "AbortError");
  assert.equal(requests.length, 1);
  const { url, options } = requests[0];
  assert.equal(new URL(url).searchParams.get("name"), "报告.txt");
  assert.equal(new URL(url).searchParams.get("deviceId"), "windows");
  assert.equal(new URL(url).searchParams.get("projectId"), "work");
  assert.deepEqual([...new URL(url).searchParams.keys()].sort(), [
    "deviceId",
    "name",
    "projectId",
  ]);
  assert.equal(url.includes("input-token"), false);
  assert.equal(options.headers.get("Authorization"), "Bearer input-token");
  assert.equal(options.body, body);
  assert.equal(options.signal, controller.signal);
});
