import { test } from "node:test";
import assert from "node:assert/strict";
import { MewDexClient } from "../src/index.ts";

test("project configuration APIs keep project scope, review revisions and literal argv", async () => {
  const calls = [];
  const client = new MewDexClient({
    baseURL: "https://hub.example",
    fetch: async (url, options) => {
      calls.push({ url: new URL(url), options });
      return new Response("{}", { headers: { "Content-Type": "application/json" } });
    },
  });
  const controller = new AbortController();
  await client.projectHarness("hp_project", 7, controller.signal);
  await client.projectHarnessHistory("hp_project", 12, controller.signal);
  await client.projectTurnContext("hp_project", "session / 原始", controller.signal);
  const input = {
    expectedRevision: 4,
    expectedProjectRevision: 8,
    configuration: { format: 1, instructions: "original\n", skills: [], scripts: [{ id: "check", command: ["tool", "", "a b", "$(literal)"] }], workflows: [] },
  };
  await client.saveProjectHarness("hp_project", input);
  assert.equal(calls[0].url.pathname, "/v1/projects/hp_project/harness");
  assert.equal(calls[0].url.searchParams.get("revision"), "7");
  assert.equal(calls[1].url.pathname, "/v1/projects/hp_project/harness/history");
  assert.equal(calls[1].url.searchParams.get("before"), "12");
  assert.equal(calls[2].url.searchParams.get("sessionId"), "session / 原始");
  assert.equal(calls[2].options.signal, controller.signal);
  assert.equal(calls[3].options.method, "PUT");
  assert.deepEqual(JSON.parse(calls[3].options.body), input);
});
