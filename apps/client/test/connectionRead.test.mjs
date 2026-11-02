import { test } from "node:test";
import assert from "node:assert/strict";
import { MewDexClient, MewDexError } from "@mewdex/sdk";
import {
  readConnection,
  retryableConnectionRead,
} from "../src/connectionRead.ts";

test("connection reads retain HTTP status from a proxy and retry only the existing identity", async () => {
  const requests = [];
  const client = new MewDexClient({
    baseURL: "https://hub.example",
    token: "existing-input-identity",
    fetch: async (url, options) => {
      requests.push({
        url,
        method: options.method ?? "GET",
        authorization: options.headers.get("Authorization"),
      });
      return requests.length === 1
        ? new Response("The service is starting", {
            status: 503,
            statusText: "Service Unavailable",
            headers: { "Content-Type": "text/plain" },
          })
        : new Response(
            JSON.stringify({ id: "original-input", role: "client" }),
            { headers: { "Content-Type": "application/json" } },
          );
    },
  });
  const signal = new AbortController().signal;
  await assert.rejects(
    readConnection((signal) => client.identity(signal), signal),
    (error) => {
      assert.equal(error instanceof MewDexError, true);
      assert.equal(error.status, 503);
      assert.equal(retryableConnectionRead(error), true);
      return true;
    },
  );
  const identity = await readConnection(
    (signal) => client.identity(signal),
    signal,
  );
  assert.equal(identity.id, "original-input");
  assert.deepEqual(
    requests,
    Array.from({ length: 2 }, () => ({
      url: "https://hub.example/v1/me",
      method: "GET",
      authorization: "Bearer existing-input-identity",
    })),
  );
});

test("connection reads stop automatic recovery for denied or malformed identity responses", async () => {
  for (const status of [401, 403, 404, 426]) {
    const client = new MewDexClient({
      baseURL: "https://hub.example",
      fetch: async () =>
        new Response("not json", {
          status,
          headers: { "Content-Type": "application/json" },
        }),
    });
    await assert.rejects(
      readConnection(
        (signal) => client.identity(signal),
        new AbortController().signal,
      ),
      (error) => {
        assert.equal(error.status, status);
        assert.equal(retryableConnectionRead(error), false);
        return true;
      },
    );
  }
  assert.equal(
    retryableConnectionRead(new SyntaxError("invalid identity response")),
    false,
  );
});

test("connection read timeout and caller cancellation abort one request without resending it", async () => {
  let reads = 0;
  const read = (signal) =>
    new Promise((_resolve, reject) => {
      reads++;
      signal.addEventListener("abort", () => reject(signal.reason), {
        once: true,
      });
    });
  await assert.rejects(
    readConnection(read, new AbortController().signal, 5),
    (error) => error.name === "TimeoutError" && retryableConnectionRead(error),
  );
  assert.equal(reads, 1);
  const caller = new AbortController();
  const pending = readConnection(read, caller.signal);
  caller.abort();
  await assert.rejects(
    pending,
    (error) => error.name === "AbortError" && !retryableConnectionRead(error),
  );
  assert.equal(reads, 2);
});
