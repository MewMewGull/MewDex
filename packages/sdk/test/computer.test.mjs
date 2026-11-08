import { test } from "node:test";
import assert from "node:assert/strict";
import { MewDexClient } from "../src/index.ts";

test("computer status and stop keep the project, conversation, machine directory and original stop identity", async () => {
  const calls = [];
  const client = new MewDexClient({
    baseURL: "https://hub.example",
    fetch: async (url, options) => {
      calls.push({ url: new URL(url), options });
      return new Response("{}", {
        headers: { "Content-Type": "application/json" },
      });
    },
  });
  const controller = new AbortController();
  await client.computerStatus(
    "hp_project",
    "session / 原始",
    3,
    controller.signal,
  );
  const input = { id: "original-stop-id", sessionId: "original-control-id" };
  await client.stopComputer("hp_project", "session / 原始", 3, input);
  for (const call of calls) {
    assert.equal(call.url.pathname, "/v1/projects/hp_project/computer");
    assert.equal(call.url.searchParams.get("sessionId"), "session / 原始");
    assert.equal(call.url.searchParams.get("directory"), "3");
  }
  assert.equal(calls[0].options.signal, controller.signal);
  assert.equal(calls[1].options.method, "POST");
  assert.deepEqual(JSON.parse(calls[1].options.body), input);
});
