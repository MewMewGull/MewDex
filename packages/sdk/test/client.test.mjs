import { test } from "node:test";
import assert from "node:assert/strict";
import { MewDexClient, CommandOutcomeError } from "../src/index.ts";

const command = { id: "once", deviceId: "executor", kind: "start" };
const json = (body) =>
  new Response(JSON.stringify(body), {
    headers: { "Content-Type": "application/json" },
  });

test("retiring an old execution sends only its scoped review and keeps the command id on response loss", async () => {
  const posted = [];
  const session = {
    id: "old",
    deviceId: "mac",
    turnId: "old-turn",
    executionRecovery: { id: "private-review-reference", state: "eligible" },
  };
  const client = new MewDexClient({
    baseURL: "https://hub.example",
    fetch: async (_url, options) => {
      assert.equal(options.method, "POST");
      posted.push(JSON.parse(options.body));
      throw new TypeError("lost response");
    },
  });
  await assert.rejects(
    client.retireExecution(session),
    (error) =>
      error instanceof CommandOutcomeError &&
      error.commandId === posted[0].id &&
      error.state === "unknown",
  );
  assert.equal(posted.length, 1);
  assert.deepEqual(Object.keys(posted[0]).sort(), [
    "deviceId",
    "expectedTurnId",
    "id",
    "kind",
    "requestId",
    "sessionId",
  ]);
  assert.equal(posted[0].kind, "execution.retire");
  assert.equal(posted[0].requestId, session.executionRecovery.id);
  assert.equal(posted[0].expectedTurnId, session.turnId);
  await assert.rejects(
    client.retireExecution({ id: "new", deviceId: "mac" }),
    /no verified recovery review/,
  );
  assert.equal(posted.length, 1);
});

test("input restart recovery reads only existing receipt references", async () => {
  let reads = 0;
  const client = new MewDexClient({
    baseURL: "https://hub.example",
    fetch: async (url, options) => {
      assert.equal(options.method, undefined);
      const parsed = new URL(url);
      assert.equal(parsed.pathname, "/v1/pending-commands");
      assert.equal(parsed.searchParams.get("deviceId"), "target node");
      assert.equal(parsed.searchParams.get("after"), "9");
      reads++;
      return json({
        commands: [
          {
            id: "existing-id",
            deviceId: "target node",
            kind: "start",
            state: "unknown",
            createdAt: "",
          },
        ],
        after: 10,
        hasMore: false,
      });
    },
  });
  const page = await client.pendingCommands("target node", 9);
  assert.equal(page.commands[0].id, "existing-id");
  assert.equal(reads, 1);
});

test("queue cancellation uses the observed executor revision", async () => {
  let posted;
  const item = {
    id: "queued",
    deviceId: "linux",
    sessionId: "s",
    revision: 4,
    state: "queued",
  };
  const client = new MewDexClient({
    baseURL: "http://localhost",
    fetch: async (url, options) => {
      if (options.method !== "POST") {
        assert.equal(new URL(url).searchParams.get("sessionId"), "s");
        return json([item]);
      }
      posted = JSON.parse(options.body);
      return json({
        commandId: posted.id,
        state: "completed",
        data: { ...item, state: "cancelled", revision: 5 },
      });
    },
  });
  const [observed] = await client.queue("s");
  assert.equal((await client.cancelQueued(observed)).state, "cancelled");
  assert.equal(posted.requestId, "queued");
  assert.equal(posted.expectedRevision, 4);
  assert.equal(posted.kind, "queue.cancel");
});

test("an observation failure retains the command id and never resubmits", async () => {
  let posts = 0,
    reads = 0;
  const client = new MewDexClient({
    baseURL: "http://127.0.0.1:47830",
    fetch: async (_url, options) => {
      if (options.method === "POST") {
        posts++;
        return json({ commandId: "once", state: "accepted" });
      }
      reads++;
      throw new TypeError("connection lost");
    },
  });
  await assert.rejects(
    client.run(command),
    (error) =>
      error instanceof CommandOutcomeError &&
      error.commandId === "once" &&
      error.state === "unknown",
  );
  assert.equal(posts, 1);
  assert.equal(reads, 1);
});

test("a transport failure while sending is an unknown outcome", async () => {
  let calls = 0;
  const client = new MewDexClient({
    baseURL: "http://localhost",
    fetch: async () => {
      calls++;
      throw new TypeError("lost");
    },
  });
  await assert.rejects(
    client.run(command),
    (error) =>
      error instanceof CommandOutcomeError && error.state === "unknown",
  );
  assert.equal(calls, 1);
});

test("reply uses the stable interaction contract and targets its original turn", async () => {
  let posted;
  const client = new MewDexClient({
    baseURL: "http://localhost",
    fetch: async (_url, options) => {
      posted = JSON.parse(options.body);
      return json({ commandId: posted.id, state: "completed" });
    },
  });
  await client.respond(
    { deviceId: "linux", id: "session", turnId: "turn" },
    "question",
    { action: "accept", answers: { name: ["example"] } },
  );
  assert.equal(posted.kind, "respond");
  assert.equal(posted.deviceId, "linux");
  assert.equal(posted.expectedTurnId, "turn");
  assert.deepEqual(posted.response, {
    action: "accept",
    answers: { name: ["example"] },
  });
});

test("filtered stream checkpoints advance without becoming user events", async () => {
  const controller = new AbortController(),
    events = [];
  let socketURL;
  const client = new MewDexClient({
    baseURL: "http://localhost",
    token: "private",
    fetch: async () => json({ ticket: "short-lived" }),
    createWebSocket: (url) => {
      socketURL = new URL(url);
      const socket = {
        close() {
          queueMicrotask(() => socket.onclose?.());
        },
      };
      queueMicrotask(() => {
        socket.onopen?.();
        socket.onmessage?.({
          data: JSON.stringify({ sequence: 100, kind: "stream.checkpoint" }),
        });
        socket.onmessage?.({
          data: JSON.stringify({
            sequence: 101,
            id: "visible",
            kind: "message.user",
          }),
        });
        controller.abort();
      });
      return socket;
    },
  });
  await client.subscribe({
    signal: controller.signal,
    onEvent: (event) => events.push(event),
  });
  assert.equal(events.length, 1);
  assert.equal(events[0].sequence, 101);
  assert.equal(socketURL.searchParams.get("ticket"), "short-lived");
  assert.ok(!socketURL.toString().includes("private"));
});

test("policy writes and restores carry the observed version", async () => {
  const posted = [];
  const client = new MewDexClient({
    baseURL: "http://localhost",
    fetch: async (_url, options) => {
      const command = JSON.parse(options.body);
      posted.push(command);
      return json({
        commandId: command.id,
        state: "completed",
        session: { id: "s", deviceId: "node", policyVersion: "next" },
      });
    },
  });
  const session = { id: "s", deviceId: "node", policyVersion: "seen" };
  await client.savePolicy(session, {
    mode: "focused",
    noTests: true,
    instructions: "",
    completion: "",
  });
  await client.restorePolicy(session, "historical");
  assert.equal(posted[0].expectedPolicyVersion, "seen");
  assert.equal(posted[1].expectedPolicyVersion, "seen");
  assert.equal(posted[1].requestId, "historical");
  assert.equal(posted[1].sessionId, "s");
});

test("optional event kinds preserve cursors even when an older Hub ignores filters", async () => {
  const controller = new AbortController(),
    events = [],
    cursors = [];
  let socketURL;
  const client = new MewDexClient({
    baseURL: "http://localhost",
    token: "private-bearer",
    fetch: async (_url, options) => {
      assert.equal(options.method, "POST");
      assert.equal(
        options.body,
        undefined,
        "ticket format remains backward compatible",
      );
      return json({ ticket: "one-use" });
    },
    createWebSocket: (url) => {
      socketURL = new URL(url);
      const socket = {
        close() {
          queueMicrotask(() => socket.onclose?.());
        },
      };
      queueMicrotask(() => {
        socket.onopen?.();
        for (const [sequence, kind] of [
          [6, "message.user"],
          [7, "engine.turn/completed"],
          [7, "engine.turn/completed"],
          [8, "interaction.updated"],
          [9, "stream.checkpoint"],
          [10, "engine.item/agentMessage/delta"],
        ])
          socket.onmessage?.({ data: JSON.stringify({ sequence, kind }) });
        controller.abort();
      });
      return socket;
    },
  });
  await client.subscribe({
    after: 5,
    kinds: ["engine.turn/completed", "interaction.updated"],
    signal: controller.signal,
    onEvent: (event) => events.push(event),
    onCursor: (cursor) => cursors.push(cursor),
  });
  assert.deepEqual(
    events.map((event) => event.sequence),
    [7, 8],
  );
  assert.deepEqual(cursors, [6, 7, 8, 9, 10]);
  assert.deepEqual(socketURL.searchParams.getAll("kind"), [
    "engine.turn/completed",
    "interaction.updated",
  ]);
  assert.equal(socketURL.searchParams.has("sessionId"), false);
  assert.ok(!socketURL.toString().includes("private-bearer"));
});

test("invalid kind subscriptions fail before issuing a ticket or opening a socket", async () => {
  const client = new MewDexClient({
    baseURL: "http://localhost",
    fetch: async () => {
      assert.fail("invalid filter issued a request");
    },
    createWebSocket: () => {
      assert.fail("invalid filter opened a socket");
    },
  });
  for (const kinds of [
    [],
    [""],
    ["*"],
    ["contains space"],
    ["e\n"],
    ["汉字"],
    [1],
    ["a".repeat(129)],
    Array(17).fill("a"),
  ]) {
    await assert.rejects(
      client.subscribe({
        kinds,
        signal: new AbortController().signal,
        onEvent() {},
      }),
      /Invalid event kind filter/,
    );
  }
});

test("reconnect refreshes a scoped stream from its authoritative snapshot cursor", async () => {
  const controller = new AbortController();
  const urls = [],
    cursors = [],
    events = [];
  let reconnects = 0;
  const client = new MewDexClient({
    baseURL: "http://localhost",
    fetch: async (_url, options) => {
      assert.equal(options.method, "POST");
      return json({ ticket: "temporary" });
    },
    createWebSocket: (url) => {
      urls.push(new URL(url));
      const first = urls.length === 1;
      const socket = {
        close() {
          queueMicrotask(() => socket.onclose?.());
        },
      };
      queueMicrotask(() => {
        socket.onopen?.();
        if (first) {
          socket.onmessage?.({
            data: JSON.stringify({ sequence: 100, kind: "stream.checkpoint" }),
          });
          socket.close();
        } else {
          socket.onmessage?.({
            data: JSON.stringify({
              sequence: 13,
              id: "new",
              kind: "message.user",
            }),
          });
          controller.abort();
        }
      });
      return socket;
    },
  });
  await client.subscribe({
    after: 90,
    sessionId: "chosen",
    signal: controller.signal,
    onCursor: (cursor) => cursors.push(cursor),
    onEvent: (event) => events.push(event),
    onReconnect: async () => {
      reconnects++;
      return 12;
    },
  });
  assert.equal(reconnects, 1);
  assert.equal(urls[1].searchParams.get("after"), "12");
  assert.equal(urls[1].searchParams.get("sessionId"), "chosen");
  assert.deepEqual(cursors, [100, 12, 13]);
  assert.equal(events.length, 1);
});

test("Hub credentials require TLS outside explicit loopback or private development addresses", () => {
  for (const baseURL of [
    "http://example.com",
    "http://203.0.113.5",
    "https://user:password@example.com",
    "https://example.com/?token=x",
    "https://example.com/#token",
  ])
    assert.throws(() => new MewDexClient({ baseURL }));
  for (const baseURL of [
    "http://127.0.0.1",
    "http://[::1]",
    "https://example.com/hub",
  ])
    assert.doesNotThrow(() => new MewDexClient({ baseURL }));
  assert.throws(() => new MewDexClient({ baseURL: "http://192.168.1.20" }));
  assert.doesNotThrow(
    () =>
      new MewDexClient({
        baseURL: "http://192.168.1.20",
        allowPrivateHTTP: true,
      }),
  );
  assert.throws(
    () =>
      new MewDexClient({
        baseURL: "http://203.0.113.5",
        allowPrivateHTTP: true,
      }),
  );
});
