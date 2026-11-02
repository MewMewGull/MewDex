import { test } from "node:test";
import assert from "node:assert/strict";
import { createNativeNotificationAdapter } from "../src/nativeNotificationAdapter.ts";

function fakeRuntime() {
  const state = {
    permission: true,
    requests: 0,
    reads: 0,
    notices: [],
    actions: [],
    listeners: [],
    removed: 0,
  };
  return {
    state,
    supported: async () => true,
    permission: async () => {
      state.reads++;
      return state.permission;
    },
    request: async () => {
      state.requests++;
      return "granted";
    },
    activate: async () => {},
    registerActions: async (types) => {
      state.actions.push(types);
    },
    onAction: async (handler) => {
      state.listeners.push(handler);
      return {
        unregister: async () => {
          state.removed++;
        },
      };
    },
    notify: async (notice) => {
      state.notices.push(notice);
    },
  };
}
const alert = {
  key: "task",
  deviceId: "node",
  sessionId: "session",
  title: "MewDex",
  body: "一轮对话已完成。",
};
const send = (adapter, signal = new AbortController().signal) =>
  adapter.send(
    "scope",
    alert,
    () => {},
    () => {},
    signal,
  );

test("native permission is read afresh and no read or listener requests consent", async () => {
  const runtime = fakeRuntime(),
    adapter = createNativeNotificationAdapter(runtime);
  assert.equal(await adapter.permission(), "granted");
  runtime.state.permission = false;
  assert.equal(await adapter.permission(), "denied");
  runtime.state.permission = null;
  assert.equal(await adapter.permission(), "default");
  const off = await adapter.listen("scope", () => {});
  assert.equal(runtime.state.requests, 0);
  assert.equal(await adapter.request(), "granted");
  assert.equal(runtime.state.requests, 1);
  off();
});

test("a revoked native permission blocks dispatch and dispatch rejection reaches the caller", async () => {
  const runtime = fakeRuntime(),
    adapter = createNativeNotificationAdapter(runtime);
  await send(adapter);
  assert.equal(runtime.state.notices.length, 1);
  runtime.state.permission = false;
  await assert.rejects(send(adapter), /系统提醒权限尚未开启/);
  assert.equal(runtime.state.notices.length, 1);
  runtime.state.permission = true;
  runtime.notify = async () => {
    throw new Error("native dispatch rejected");
  };
  await assert.rejects(send(adapter), /native dispatch rejected/);
  assert.equal(runtime.state.requests, 0);
});

test("logout during an in-flight permission check cancels the pending native notice", async () => {
  const runtime = fakeRuntime();
  let release;
  const waiting = new Promise((resolve) => {
    release = resolve;
  });
  runtime.permission = () => waiting;
  const adapter = createNativeNotificationAdapter(runtime),
    controller = new AbortController();
  const delivery = send(adapter, controller.signal);
  controller.abort();
  release(true);
  await delivery;
  assert.equal(runtime.state.notices.length, 0);
  runtime.permission = () => {
    assert.fail("an already cancelled dispatch queried permission");
  };
  await send(adapter, controller.signal);
});

test("native action callbacks require matching input scope and typed conversation metadata", async () => {
  const runtime = fakeRuntime(),
    adapter = createNativeNotificationAdapter(runtime),
    opened = [];
  const off = await adapter.listen("scope", (...value) => opened.push(value));
  const listener = runtime.state.listeners[0];
  const action = (actionId, extra) => ({ actionId, notification: { extra } });
  const extra = { scope: "scope", deviceId: "node", sessionId: "session" };
  listener(action("dismiss", extra));
  listener(action("tap", { ...extra, scope: "other" }));
  listener(action("tap", { ...extra, sessionId: 8 }));
  listener({ actionId: "tap" });
  listener(action("tap", extra));
  listener(action("open", extra));
  assert.deepEqual(opened, [
    ["node", "session"],
    ["node", "session"],
  ]);
  assert.equal(
    runtime.state.actions[0][0].actions[0].requiresAuthentication,
    true,
  );
  off();
  await Promise.resolve();
  assert.equal(runtime.state.removed, 1);
});

test("unsupported native hosts do not call a new command or request permission", async () => {
  const runtime = fakeRuntime();
  runtime.supported = async () => false;
  runtime.permission = async () => {
    assert.fail("unsupported host called a new command");
  };
  const adapter = createNativeNotificationAdapter(runtime);
  assert.equal(await adapter.permission(), "unsupported");
  assert.equal(await adapter.request(), "unsupported");
  const off = await adapter.listen("scope", () => {});
  off();
  await assert.rejects(send(adapter), /系统提醒权限尚未开启/);
  assert.equal(runtime.state.requests, 0);
  assert.equal(runtime.state.actions.length, 0);
});
