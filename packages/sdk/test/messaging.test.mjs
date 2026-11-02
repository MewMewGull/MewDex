import { test } from "node:test";
import assert from "node:assert/strict";
import { MewDexClient } from "../src/index.ts";

test("channel settings preserve omitted secrets and QR polling is an authenticated POST", async () => {
  const calls = [];
  const client = new MewDexClient({
    baseURL: "https://hub.example",
    token: "admin-key",
    fetch: async (url, init) => {
      calls.push({ url, method: init.method ?? "GET", body: init.body });
      assert.equal(init.headers.get("Authorization"), "Bearer admin-key");
      assert.equal(init.redirect, "error");
      return Response.json(
        url.includes("login") ? { state: "wait", url: "qr-data" } : [],
      );
    },
  });
  await client.messagingChannels();
  await client.updateMessagingChannel("qq", {
    enabled: false,
    deviceId: "",
    projectId: "",
    appId: "app",
    botUserId: "",
    allowedUsers: [],
    allowedChats: [],
  });
  await client.beginWeixinLogin();
  await client.pollWeixinLogin();
  assert.equal(calls[0].url, "https://hub.example/v1/messaging");
  assert.equal(calls[1].method, "PUT");
  assert.equal(Object.hasOwn(JSON.parse(calls[1].body), "secret"), false);
  assert.deepEqual(
    calls.slice(2).map(({ url, method }) => ({ url, method })),
    [
      { url: "https://hub.example/v1/messaging/weixin/login", method: "POST" },
      {
        url: "https://hub.example/v1/messaging/weixin/login/poll",
        method: "POST",
      },
    ],
  );
});
