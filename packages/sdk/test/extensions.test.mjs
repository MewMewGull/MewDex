import { test } from "node:test";
import assert from "node:assert/strict";
import { MewDexClient } from "../src/index.ts";

test("extension settings use scoped durable commands and retain the caller's mutation identity", async () => {
  const requests = [];
  const client = new MewDexClient({
    baseURL: "https://hub.example",
    token: "admin-key",
    fetch: async (url, init) => {
      assert.equal(url, "https://hub.example/v1/commands");
      const command = JSON.parse(init.body);
      requests.push(command);
      return Response.json({
        commandId: command.id,
        state: "completed",
        data:
          command.kind === "extension.read"
            ? { section: "skills", skills: [] }
            : { status: "ok" },
      });
    },
  });
  assert.deepEqual(
    (await client.extensions("hub", "skills", "project")).skills,
    [],
  );
  await client.updateExtension(
    "hub",
    {
      section: "hooks",
      action: "hook.set",
      expectedVersion: "revision",
      key: "hook-key",
      hash: "current-hash",
      enabled: true,
      generateMemory: false,
      useMemory: false,
    },
    "project",
    "same-operation",
  );
  assert.equal(requests[0].kind, "extension.read");
  assert.equal(requests[0].projectId, "project");
  assert.equal(requests[1].id, "same-operation");
  assert.equal(requests[1].kind, "extension.update");
  assert.equal(requests[1].extension.hash, "current-hash");
  assert.equal(requests[1].extension.expectedVersion, "revision");
});
