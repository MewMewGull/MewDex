import { test } from "node:test";
import assert from "node:assert/strict";
import { MewDexClient } from "../src/index.ts";

test("server discovery negotiates advertised authentication before sending account credentials", async () => {
  const calls = [];
  const identity = {
    id: "owner",
    accountId: "owner",
    role: "admin",
    access: { allDevices: true, targets: [] },
  };
  const client = new MewDexClient({
    baseURL: "https://hub.example",
    fetch: async (url, init) => {
      calls.push({ url, init });
      assert.equal(init.redirect, "error");
      if (url.endsWith("/health")) {
        assert.equal(init.headers.get("Authorization"), null);
        assert.equal(init.body, undefined);
        return Response.json({
          name: "MewDex",
          protocolVersion: 1,
          authentication: {
            methods: ["password", "key"],
            defaultMethod: "password",
            accountConfigured: true,
          },
        });
      }
      assert.deepEqual(JSON.parse(init.body), {
        username: "owner",
        password: "password-fixture",
      });
      return Response.json({
        token: "login-key",
        identity,
        protocolVersion: 1,
      });
    },
  });
  const info = await client.checkServer();
  assert.equal(info.authentication.defaultMethod, "password");
  const credentials = await client.login("owner", "password-fixture");
  assert.equal(credentials.identity.id, "owner");
  assert.deepEqual(
    calls.map((c) => c.url),
    ["https://hub.example/health", "https://hub.example/v1/login"],
  );
});

test("key-only, unsupported and legacy server authentication are handled explicitly", async () => {
  const connect = (body) =>
    new MewDexClient({
      baseURL: "https://hub.example",
      fetch: async () => Response.json(body),
    }).checkServer();
  const base = { name: "MewDex", protocolVersion: 1 };
  const key = await connect({
    ...base,
    authentication: {
      methods: ["key"],
      defaultMethod: "key",
      accountConfigured: true,
    },
  });
  assert.deepEqual(key.authentication.methods, ["key"]);
  const legacy = await connect(base);
  assert.equal(legacy.authentication.defaultMethod, "key");
  await assert.rejects(
    connect({ ...base, authentication: { methods: ["unknown"] } }),
    /认证方式/,
  );
  await assert.rejects(connect({ ...base, protocolVersion: 2 }), /中央后端/);
  await assert.rejects(connect({ ...base, name: "Other" }), /中央后端/);
});
