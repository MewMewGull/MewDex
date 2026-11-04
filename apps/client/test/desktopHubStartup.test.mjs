import { test } from "node:test";
import assert from "node:assert/strict";
import { prepareDesktopTerminals } from "../src/hub-controller/desktopTerminals.ts";

test("desktop startup reuses ready terminals and accepts input-only installations", async () => {
  for (const snapshot of [
    { configured: false, hub: false, state: "unconfigured" },
    { configured: true, hub: false, state: "stopped" },
    { configured: true, hub: true, ready: true, state: "running" },
    {
      configured: true,
      hub: false,
      executor: true,
      ready: true,
      state: "running",
    },
  ]) {
    const operations = [];
    const result = await prepareDesktopTerminals(async (request) => {
      operations.push(request.operation);
      return { snapshot };
    });
    assert.deepEqual(operations, ["status"]);
    assert.equal(result.snapshot, snapshot);
  }
});

test("desktop startup launches an Execute Terminal configured to use a remote Hub", async () => {
  const requests = [];
  const result = await prepareDesktopTerminals(async (request) => {
    requests.push(request);
    return {
      snapshot: {
        configured: true,
        hub: false,
        executor: true,
        deviceId: "executor-only",
        ready: request.operation === "start",
        state: request.operation === "start" ? "running" : "stopped",
        hubUrl: "https://hub.example.com",
      },
    };
  });
  assert.deepEqual(requests, [
    { operation: "status" },
    { operation: "start", expectedDeviceId: "executor-only" },
  ]);
  assert.equal(result.started, true);
  assert.equal(result.snapshot.hub, false);
  assert.equal(result.snapshot.executor, true);
});

test("opening a desktop starts only its configured Hub and retains existing Hub discovery", async () => {
  for (const state of ["stopped", "starting"]) {
    const requests = [];
    const existingHub = {
      baseUrl: "http://127.0.0.1:47830",
      protocolVersion: 1,
    };
    const result = await prepareDesktopTerminals(async (request) => {
      requests.push(request);
      return request.operation === "status"
        ? {
            snapshot: {
              configured: true,
              hub: true,
              ready: false,
              state,
              deviceId: "owner",
            },
          }
        : {
            snapshot: { configured: true, hub: true, state: "stopped" },
            existingHub,
          };
    });
    assert.deepEqual(requests, [
      { operation: "status" },
      { operation: "start", expectedDeviceId: "owner" },
    ]);
    assert.equal(result.existingHub, existingHub);
    assert.equal(result.started, true);
  }
});

test("unconfirmed service states and failed starts never cause an automatic second mutation", async () => {
  for (const state of ["unknown", "stopping"]) {
    let reads = 0;
    await assert.rejects(
      prepareDesktopTerminals(async () => {
        reads++;
        return {
          snapshot: { configured: true, hub: true, ready: false, state },
        };
      }),
    );
    assert.equal(reads, 1);
  }
  const operations = [];
  await assert.rejects(
    prepareDesktopTerminals(async (request) => {
      operations.push(request.operation);
      if (request.operation === "start")
        throw new Error("startup remains unconfirmed");
      return {
        snapshot: {
          configured: true,
          hub: true,
          state: "stopped",
          deviceId: "owner",
        },
      };
    }),
    /unconfirmed/,
  );
  assert.deepEqual(operations, ["status", "start"]);
});
