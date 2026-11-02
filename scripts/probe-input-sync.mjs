// Existing development histories only. No create/start/steer/enqueue or model calls.
import { readFile, writeFile } from "node:fs/promises";
import { MewDexClient } from "../packages/sdk/src/index.ts";

const baseURL = "http://127.0.0.1:47830";
const auth = JSON.parse(await readFile(".local/client-auth.json", "utf8"));
const client = new MewDexClient({ baseURL, token: auth.token });
const previous = JSON.parse(await readFile(".state/evidence/remote-attachments-queue.json", "utf8"));
const snapshot = await client.bootstrap();
const pages = [];
for (const target of previous.reports) {
  let before = 0, count = 0;
  const seen = new Set();
  for (let pageNumber = 0; pageNumber < 30; pageNumber++) {
    const page = await client.historyPage(target.sessionId, before, 2);
    for (const event of page.events) {
      if (event.sessionId !== target.sessionId || seen.has(event.id)) throw new Error("History crossed a conversation or repeated a page");
      seen.add(event.id); count++;
    }
    if (!page.hasMore) break;
    if (!page.before || page.before === before) throw new Error("History cursor did not advance");
    before = page.before;
  }
  if (count < 4) throw new Error("Existing development history was not available");
  pages.push({ deviceId: target.deviceId, sessionId: target.sessionId, eventsRead: count, unique: true });
}

const selected = previous.reports[0];
const streamController = new AbortController();
let scopedEvents = 0, metadataEvents = 0, streamCursor = 0;
const streamTimer = setTimeout(() => streamController.abort(), 10000);
try {
  await client.subscribe({ after: 0, sessionId: selected.sessionId, signal: streamController.signal,
    onEvent: (event) => {
      if (event.sessionId && event.sessionId !== selected.sessionId && !["session.updated", "command.result"].includes(event.kind))
        throw new Error("Inactive conversation content entered a scoped stream");
      if (event.sessionId === selected.sessionId) scopedEvents++; else metadataEvents++;
    },
    onCursor: (cursor) => { streamCursor = cursor; if (cursor >= snapshot.cursor) streamController.abort(); },
  });
} finally { clearTimeout(streamTimer); }
if (streamCursor < snapshot.cursor || !scopedEvents) throw new Error("Scoped stream did not reach its network checkpoint");

const access = (target) => ({ allDevices: false, targets: [{ deviceId: target.deviceId, projectIds: ["risk"] }], gateway: false });
const invitation = await client.invite("client", access(previous.reports[0]));
const scoped = new MewDexClient({ baseURL });
const credentials = await scoped.pair(invitation.code, "输入同步开发探测（临时）");
let reconnects = 0, connections = 0, changed = false, oldScopeDenied = false, newlyGrantedHistory = false;
try {
  const initial = await scoped.bootstrap();
  if (initial.devices.length !== 1 || initial.devices[0].id !== previous.reports[0].deviceId)
    throw new Error("Scoped bootstrap disclosed another execution target");
  const controller = new AbortController();
  let failure;
  const timer = setTimeout(() => { failure = new Error("Scope refresh did not reconnect"); controller.abort(); }, 12000);
  try {
    await scoped.subscribe({ after: initial.cursor, sessionId: "", signal: controller.signal, onEvent: () => {},
      onReconnect: async () => {
        reconnects++;
        const current = await scoped.bootstrap();
        changed = current.devices.length === 1 && current.devices[0].id === previous.reports[1].deviceId;
        return current.cursor;
      },
      onState: (state) => {
        if (state !== "connected") return;
        connections++;
        if (connections === 1) void client.updateAccess(credentials.identity.id, access(previous.reports[1])).catch((error) => { failure = error; controller.abort(); });
        else controller.abort();
      },
    });
  } finally { clearTimeout(timer); }
  if (failure) throw failure;
  try { await scoped.historyPage(previous.reports[0].sessionId); }
  catch (error) { if (error.status === 404) oldScopeDenied = true; else throw error; }
  newlyGrantedHistory = (await scoped.historyPage(previous.reports[1].sessionId)).events.length > 0;
  if (!changed || !oldScopeDenied || !newlyGrantedHistory || connections !== 2) throw new Error("Access refresh did not replace the visible network");
} finally { await client.revoke(credentials.identity.id); }

const report = { at: new Date().toISOString(), pages, stream: { selectedSessionId: selected.sessionId, scopedEvents, metadataEvents, cursorCaughtUp: streamCursor >= snapshot.cursor }, accessRefresh: { reconnects, connections, changed, oldScopeDenied, newlyGrantedHistory, temporaryIdentityRevoked: true }, modelTurnsSubmitted: 0, inferenceRun: false, nativeUIObserved: false };
await writeFile(".state/evidence/input-sync.json", JSON.stringify(report, null, 2));
console.log(JSON.stringify(report));
