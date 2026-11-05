import { test } from "node:test";
import assert from "node:assert/strict";
import { MewDexClient } from "../src/index.ts";

test("a reusable login key reads the same identity without pairing or commands", async () => {
  const identity = {
    id: "original-input",
    role: "client",
    access: { allDevices: false, targets: [] },
  };
  const calls = [];
  const options = {
    baseURL: "https://hub.example",
    token: "saved-input-key",
    fetch: async (url, init) => {
      calls.push(url);
      assert.equal(init.headers.get("Authorization"), "Bearer saved-input-key");
      assert.equal(init.redirect, "error");
      assert.equal(init.body, undefined);
      if (url.endsWith("/health"))
        return Response.json({
          name: "MewDex",
          protocolVersion: 1,
          hubDeviceId: "hub-device",
        });
      return Response.json(identity);
    },
  };
  for (let i = 0; i < 2; i++) {
    const credentials = await new MewDexClient(options).loginWithKey();
    assert.deepEqual(credentials.identity, identity);
    assert.equal(credentials.token, options.token);
  }
  assert.deepEqual(calls, [
    "https://hub.example/health",
    "https://hub.example/v1/me",
    "https://hub.example/health",
    "https://hub.example/v1/me",
  ]);
});

test("missing, revoked and executor keys cannot create an input connection", async () => {
  await assert.rejects(
    new MewDexClient({ baseURL: "https://hub.example" }).loginWithKey(),
    /登录密钥/,
  );
  const client = (fetch) =>
    new MewDexClient({ baseURL: "https://hub.example", token: "key", fetch });
  await assert.rejects(
    client(async () => new Response(null, { status: 401 })).loginWithKey(),
    (e) => e.status === 401,
  );
  await assert.rejects(
    client(async (url) =>
      Response.json(
        url.endsWith("/health")
          ? { name: "MewDex", protocolVersion: 1 }
          : { role: "executor" },
      ),
    ).loginWithKey(),
    /输入端/,
  );
});
