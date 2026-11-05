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
      if (url.endsWith("/v1/me")) {
        assert.equal(init.headers.get("Authorization"), "Bearer login-key");
        return Response.json(identity);
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
    [
      "https://hub.example/health",
      "https://hub.example/health",
      "https://hub.example/v1/login",
      "https://hub.example/v1/me",
    ],
  );
});

test("login and restore reject another Hub, proxy host, account or mismatched credential", async () => {
  const server = {
    name: "MewDex",
    protocolVersion: 1,
    hubDeviceId: "hub-device",
    inputProxy: { id: "hub-device", hostId: "serving-host" },
  };
  const identity = {
    id: "owner",
    accountId: "owner",
    role: "admin",
    deviceId: "input",
  };
  const calls = [];
  const options = {
    baseURL: "https://hub.example",
    token: "saved-key",
    fetch: async (url) => {
      calls.push(url);
      return Response.json(url.endsWith("/health") ? server : identity);
    },
  };
  await assert.rejects(
    new MewDexClient({
      ...options,
      expectedHubDeviceId: "other-hub",
    }).loginWithKey(),
    /Hub 身份已变化/,
  );
  assert.equal(calls.length, 1);
  await assert.rejects(
    new MewDexClient({
      ...options,
      expectedInputProxyHostId: "other-host",
    }).loginWithKey(),
    /代理服务的设备已变化/,
  );
  await assert.rejects(
    new MewDexClient({
      ...options,
      expectedIdentityId: "other-account",
    }).loginWithKey(),
    /保存的账号不一致/,
  );
  const client = new MewDexClient({
    baseURL: "https://hub.example",
    fetch: async (url) => {
      if (url.endsWith("/health")) return Response.json(server);
      if (url.endsWith("/v1/login"))
        return Response.json({
          token: "new-key",
          identity,
          protocolVersion: 1,
          hubDeviceId: server.hubDeviceId,
          inputProxy: server.inputProxy,
        });
      return Response.json({ ...identity, deviceId: "another-input" });
    },
  });
  await assert.rejects(
    client.login("owner", "password-fixture"),
    /账号或设备不一致/,
  );
});

test("localhost and domain browser logins retain the serving device proxy without pairing", async () => {
  const server = {
    name: "MewDex",
    protocolVersion: 1,
    hubDeviceId: "mac-hub",
    inputProxy: { id: "mac-hub", hostId: "mac-host", roles: ["input-proxy"] },
  };
  const identity = { id: "owner", accountId: "owner", role: "admin" };
  for (const baseURL of ["http://localhost:47830", "https://hub.example"]) {
    const calls = [];
    const client = new MewDexClient({
      baseURL,
      token: "key",
      fetch: async (url) => {
        calls.push(url);
        return Response.json(url.endsWith("/health") ? server : identity);
      },
    });
    const restored = await client.loginWithKey();
    assert.equal(restored.identity.id, "owner");
    assert.deepEqual(restored.inputProxy, server.inputProxy);
    assert.deepEqual(calls, [`${baseURL}/health`, `${baseURL}/v1/me`]);
  }
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
