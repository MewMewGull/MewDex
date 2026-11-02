import { test } from "node:test";
import assert from "node:assert/strict";
import {
  MewDexClient,
  CommandOutcomeError,
  MewDexError,
} from "../src/index.ts";

test("a proxy failure while exporting keeps the original command id and sends once", async () => {
  for (const status of [408, 502, 503, 504]) {
    const posts = [];
    const client = new MewDexClient({
      baseURL: "https://hub.example",
      fetch: async (_url, options) => {
        posts.push(JSON.parse(options.body));
        return new Response("no reliable upstream receipt", {
          status,
          headers: { "Content-Type": "text/plain" },
        });
      },
    });
    await assert.rejects(
      client.exportFile(
        { id: "session", deviceId: "executor" },
        "report.txt",
        "original-copy",
      ),
      (error) =>
        error instanceof CommandOutcomeError &&
        error.commandId === "original-copy" &&
        error.state === "unknown",
    );
    assert.deepEqual(posts, [
      {
        id: "original-copy",
        deviceId: "executor",
        sessionId: "session",
        kind: "file.export",
        file: { path: "report.txt" },
      },
    ]);
  }
});

test("a rejected export keeps the authorization error and never replays", async () => {
  let posts = 0;
  const client = new MewDexClient({
    baseURL: "https://hub.example",
    fetch: async () => {
      posts++;
      return new Response('{"error":"not allowed"}', {
        status: 403,
        headers: { "Content-Type": "application/json" },
      });
    },
  });
  await assert.rejects(
    client.exportFile(
      { id: "session", deviceId: "executor" },
      "report.txt",
      "rejected-copy",
    ),
    (error) =>
      error instanceof MewDexError &&
      !(error instanceof CommandOutcomeError) &&
      error.status === 403,
  );
  assert.equal(posts, 1);
});
