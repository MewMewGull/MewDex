// Focused development probe: transfer a synthetic attachment while the queue is
// frozen, verify the actual remote bytes, then cancel. Never submits a model turn.
import { readFile, writeFile } from "node:fs/promises";
import { createHash } from "node:crypto";
import { execFile } from "node:child_process";
import { promisify } from "node:util";
import { MewDexClient, commandID } from "../packages/sdk/src/index.ts";

const exec = promisify(execFile);
const baseURL = "http://127.0.0.1:47830";
const auth = JSON.parse(await readFile(".local/client-auth.json", "utf8"));
const client = new MewDexClient({ baseURL, token: auth.token });
const devices = await client.devices();
const targets = [
  { alias: "MewGullNest_1", id: "mewdex-risk-nest-1" },
  { alias: "MewGullNest_HighSpeed", id: "mewdex-risk-nest-highspeed" },
];
const reports = [];
for (const target of targets) {
  const device = devices.find((value) => value.id === target.id);
  if (!device?.online || !device.projects.some((p) => p.id === "risk"))
    throw new Error(`${target.id} development executor is unavailable`);
  const { session } = await client.run({ id: commandID(), deviceId: target.id, projectId: "risk", kind: "create", title: "附件与冻结队列开发探测", policy: { mode: "edit", noTests: true, instructions: "只验证文件传输，不执行模型回合", completion: "取消队列项后结束" } });
  if (!session) throw new Error("Create did not return a session");
  const stopped = await client.run({ id: commandID(), deviceId: target.id, sessionId: session.id, kind: "stop" });
  if (!stopped.session?.frozen || stopped.session.engineStarted)
    throw new Error("Probe conversation was not frozen before its first turn");
  const text = `MewDex 附件传输开发探测\n目标：${target.id}\n仅校验文件，不执行模型。\n`;
  const expectedSHA256 = createHash("sha256").update(text).digest("hex");
  const attachment = await client.upload(new Blob([text], { type: "text/plain" }), "跨端附件开发探测.txt", { deviceId: target.id, projectId: "risk" });
  if (!/^[a-f0-9]{32}$/.test(attachment.id) || attachment.sha256 !== expectedSHA256)
    throw new Error("Upload integrity metadata does not match its input");
  const queued = await client.run({ id: commandID(), deviceId: target.id, sessionId: session.id, kind: "enqueue", input: [{ type: "file", attachmentId: attachment.id }] });
  const item = queued.data;
  const [observed] = await client.queue(session.id);
  if (!observed || observed.id !== item.id || observed.state !== "queued" || !queued.session?.frozen || queued.session.engineStarted)
    throw new Error("Attachment input did not remain queued without execution");
  const remoteFile = `/root/MewDex/.state/risk-20261028/data/attachments/${attachment.id}`;
  const { stdout } = await exec("ssh", ["-T", "-o", "BatchMode=yes", "-o", "StrictHostKeyChecking=yes", target.alias, `sha256sum '${remoteFile}'`], { timeout: 20000 });
  if (stdout.trim().split(/\s+/)[0] !== expectedSHA256)
    throw new Error("Actual remote attachment bytes did not match");
  const other = targets.find((value) => value.id !== target.id);
  const otherToken = (await readFile(`.local/remotes/${other.alias}/node-token`, "utf8")).trim();
  const denied = await fetch(`${baseURL}/v1/attachments/${attachment.id}/content`, { headers: { Authorization: `Bearer ${otherToken}` }, redirect: "error" });
  await denied.arrayBuffer();
  if (denied.status !== 404) throw new Error("Another executor could download this attachment");
  const cancelled = await client.cancelQueued(observed);
  let staleCancellationBlocked = false;
  try { await client.cancelQueued(observed); }
  catch (error) { if (error.state === "failed") staleCancellationBlocked = true; else throw error; }
  const current = (await client.sessions(target.id)).find((value) => value.id === session.id);
  const result = { deviceId: target.id, sessionId: session.id, attachmentId: attachment.id, sha256: expectedSHA256, remoteSHA256Matches: true, otherExecutorDenied: true, cancelled: cancelled.state === "cancelled", staleCancellationBlocked, engineStarted: current?.engineStarted, frozen: current?.frozen, inferenceRun: false };
  reports.push(result);
  if (!result.cancelled || !result.staleCancellationBlocked || result.engineStarted || !result.frozen)
    throw new Error("Queue cancellation did not preserve the probe boundary");
}
const report = { at: new Date().toISOString(), reports, inferenceRun: false };
await writeFile(".state/evidence/remote-attachments-queue.json", JSON.stringify(report, null, 2));
console.log(JSON.stringify(report));
