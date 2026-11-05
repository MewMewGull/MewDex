import { test } from "node:test";
import assert from "node:assert/strict";
import { prepareDesktopTerminals } from "../src/hub-controller/desktopTerminals.ts";
import { sameHubAddress } from "../src/hub-controller/hubConnection.ts";

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

test("a restored remote Hub never starts the configured local Hub", async () => {
  const requests = [];
  const snapshot = {
    configured: true,
    hub: true,
    deviceId: "local",
    baseUrl: "http://127.0.0.1:47830",
    state: "stopped",
  };
  const result = await prepareDesktopTerminals(
    async (request) => {
      requests.push(request);
      return { snapshot };
    },
    { baseURL: "https://hub.example.com", confirmed: false },
  );
  assert.deepEqual(requests, [{ operation: "status" }]);
  assert.equal(result.started, undefined);
});

test("a confirmed remote connection disables and stops only the identified local Hub", async () => {
  const requests = [];
  const running = {
    configured: true,
    hub: true,
    executor: true,
    deviceId: "local",
    baseUrl: "http://127.0.0.1:47830",
    state: "running",
    ready: true,
    instanceId: "original",
  };
  const result = await prepareDesktopTerminals(
    async (request) => {
      requests.push(request);
      return {
        snapshot:
          request.operation === "stop"
            ? { ...running, state: "stopped", ready: false }
            : running,
        startup: { requestedEnabled: true },
      };
    },
    {
      baseURL: "https://hub.example.com",
      confirmed: true,
      hubDeviceId: "remote",
    },
  );
  assert.deepEqual(requests, [
    { operation: "status" },
    { operation: "startup.status" },
    {
      operation: "startup.apply",
      expectedDeviceId: "local",
      startup: { enabled: false },
    },
    {
      operation: "stop",
      expectedDeviceId: "local",
      expectedInstanceId: "original",
    },
  ]);
  assert.equal(result.suspended, true);
});

test("a local Hub reached through its domain is identified by its Hub device ID", async () => {
  const requests = [];
  await prepareDesktopTerminals(
    async (request) => {
      requests.push(request.operation);
      return {
        snapshot: {
          configured: true,
          hub: true,
          deviceId: "local",
          baseUrl: "http://127.0.0.1:47830",
          ready: true,
          state: "running",
        },
      };
    },
    {
      baseURL: "https://my-hub.example.com",
      hubDeviceId: "local",
      confirmed: true,
    },
  );
  assert.deepEqual(requests, ["status"]);
});

test("remote Hub selection preserves the execution-node startup path", async () => {
  const requests = [];
  const result = await prepareDesktopTerminals(
    async (request) => {
      requests.push(request.operation);
      return {
        snapshot: {
          configured: true,
          hub: false,
          executor: true,
          deviceId: "executor",
          state: request.operation === "start" ? "running" : "stopped",
        },
      };
    },
    { baseURL: "https://hub.example.com", confirmed: true },
  );
  assert.deepEqual(requests, ["status", "start"]);
  assert.equal(result.started, true);
});

test("local Hub addresses accept loopback aliases and preserve distinct paths and ports", () => {
  assert.equal(
    sameHubAddress("http://localhost:47830/", "http://127.0.0.1:47830"),
    true,
  );
  assert.equal(
    sameHubAddress("http://[::1]:47830", "http://localhost:47830"),
    true,
  );
  assert.equal(
    sameHubAddress("http://localhost:47831", "http://127.0.0.1:47830"),
    false,
  );
  assert.equal(
    sameHubAddress(
      "https://hub.example.com/one",
      "https://hub.example.com/two",
    ),
    false,
  );
});

test("a failed local Hub shutdown is observed without replaying its stop request", async () => {
  const operations = [];
  await assert.rejects(
    prepareDesktopTerminals(
      async (request) => {
        operations.push(request.operation);
        if (request.operation === "stop")
          throw new Error("shutdown result unavailable");
        return {
          snapshot: {
            configured: true,
            hub: true,
            deviceId: "local",
            baseUrl: "http://127.0.0.1:47830",
            state: "running",
            ready: true,
            instanceId: "owned",
          },
          startup: { requestedEnabled: false },
        };
      },
      { baseURL: "https://hub.example.com", confirmed: true },
    ),
    /shutdown result/,
  );
  assert.deepEqual(operations, ["status", "startup.status", "stop"]);
});
