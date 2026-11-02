// Development risk probe against a deliberately selected running Hub. Not an acceptance suite.
import { readFile, writeFile, mkdir } from 'node:fs/promises';
import { randomUUID } from 'node:crypto';

const base = process.env.MEWDEX_PROBE_HUB ?? 'http://127.0.0.1:47830';
const authPath = '.local/client-auth.json';
let auth;
try { auth = JSON.parse(await readFile(authPath, 'utf8')); } catch {
  const code = (await readFile('.local/pairing-code', 'utf8')).trim();
  const paired = await fetch(base + '/v1/pair', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ code, name: 'MewDex development probe' }) });
  if (!paired.ok) throw new Error(`Pairing failed: ${paired.status}`);
  auth = await paired.json(); await writeFile(authPath, JSON.stringify(auth), { mode: 0o600 });
}
async function api(path, body) {
  const response = await fetch(base + path, { method: body ? 'POST' : 'GET', headers: { Authorization: `Bearer ${auth.token}`, 'Content-Type': 'application/json' }, body: body ? JSON.stringify(body) : undefined });
  const result = await response.json(); if (!response.ok) throw new Error(`${response.status}: ${result.error}`); return result;
}
async function command(body) {
  const id = randomUUID(); let result = await api('/v1/commands', { id, ...body });
  for (let i = 0; result.state === 'accepted' && i < 120; i++) { await new Promise((resolve) => setTimeout(resolve, 250)); result = await api('/v1/commands/' + id); }
  if (result.state !== 'completed') throw new Error(JSON.stringify(result)); return result;
}
const device = (await api('/v1/devices')).find((d) => d.online && d.projects.length && (!process.env.MEWDEX_PROBE_DEVICE || d.id === process.env.MEWDEX_PROBE_DEVICE));
if (!device) throw new Error('No online executor');
const gatewayBefore = await api('/v1/gateway');
const created = await command({ deviceId: device.id, projectId: device.projects[0].id, kind: 'create', title: '开发风险验证：模型请求链路', policy: { mode: 'edit', noTests: true, instructions: '本回合仅验证模型消息链路，不运行命令或读取文件。', completion: '回复指定标记后结束。' } });
const sessionId = created.session.id;
await command({ deviceId: device.id, sessionId, kind: 'start', input: [{ type: 'text', text: '请只回复 MEWDEX_ROUTE_OK，不要调用工具或访问文件。' }] });
let events = [];
let session;
for (let i = 0; i < 100; i++) {
  events = await api('/v1/events?sessionId=' + sessionId);
  session = (await api('/v1/sessions')).find((s) => s.id === sessionId);
  if (session?.state !== 'running') break;
  await new Promise((resolve) => setTimeout(resolve, 500));
}
const final = events.filter((e) => e.kind === 'engine.item/completed').map((e) => e.payload.item).filter((i) => i?.type === 'agentMessage').map((i) => i.text).join('\n');
const gatewayAfter = await api('/v1/gateway');
const summary = { at: new Date().toISOString(), deviceId: device.id, sessionId, state: session?.state, eventCount: events.length, eventKinds: [...new Set(events.map((e) => e.kind))], exactMarker: final.trim() === 'MEWDEX_ROUTE_OK', finalText: final, inferenceRequested: true, terminalItems: events.filter((e) => e.payload?.item?.type === 'commandExecution').length, gatewayBefore, gatewayAfter };
await mkdir('.state/evidence', { recursive: true }); await writeFile(`.state/evidence/${process.env.MEWDEX_PROBE_LABEL ?? 'model-route'}.json`, JSON.stringify(summary, null, 2)); console.log(JSON.stringify(summary));
if (!summary.exactMarker) process.exitCode = 1;
