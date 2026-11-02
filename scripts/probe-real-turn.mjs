// Submit one labelled model-only turn through the real network. Retained IDs
// are observed on subsequent runs; a lost receipt never creates another turn.
import { appendFileSync } from "node:fs";
import { readFile, writeFile } from "node:fs/promises";
import { MewDexClient, commandID } from "../packages/sdk/src/index.ts";

const [deviceId, projectId, reportPath] = process.argv.slice(2);
if (!deviceId || !projectId || !reportPath?.startsWith(".state/evidence/"))
  throw new Error("Specify device, project and a project-local evidence path");
const baseURL =
  process.env.MEWDEX_PROBE_HUB ?? "https://mewdex.mewgullnest.top";
const auth = JSON.parse(await readFile(".local/client-auth.json", "utf8"));
const client = new MewDexClient({ baseURL, token: auth.token });
const eventsPath = reportPath.replace(/\.json$/, "") + ".events.jsonl";
let report;
try {
  report = JSON.parse(await readFile(reportPath, "utf8"));
  if (
    report.deviceId !== deviceId ||
    report.projectId !== projectId ||
    report.baseURL !== baseURL
  )
    throw new Error("Evidence already belongs to another target; preserve it");
} catch (error) {
  if (error.code !== "ENOENT") throw error;
  report = {
    at: new Date().toISOString(),
    baseURL,
    deviceId,
    projectId,
    create: { id: commandID(), submitted: false },
    start: { id: commandID(), submitted: false },
    expectedText: "MewDex 对话链路已接通。",
    toolsAuthorized: false,
    credentialsCopied: false,
    finalAcceptance: false,
  };
  await writeFile(reportPath, JSON.stringify(report, null, 2), {
    flag: "wx",
    mode: 0o600,
  });
}
const save = async () => {
  report.observedAt = new Date().toISOString();
  await writeFile(reportPath, JSON.stringify(report, null, 2), { mode: 0o600 });
};
const receipt = async (entry, command) => {
  let result;
  if (entry.submitted) {
    result = await client.result(entry.id);
  } else {
    entry.submitted = true;
    entry.submittedAt = new Date().toISOString();
    await save();
    // One transmission only, including when POST's response is lost.
    result = await client.command({ id: entry.id, deviceId, ...command });
  }
  for (let i = 0; result.state === "accepted" && i < 100; i++) {
    await new Promise((resolve) => setTimeout(resolve, 250));
    result = await client.result(entry.id);
  }
  entry.result = result;
  await save();
  if (result.state !== "completed")
    throw new Error(
      `Receipt ${entry.id} remains ${result.state}: ${result.error ?? "observe without replay"}`,
    );
  return result;
};

let stream;
const controller = new AbortController();
try {
  const snapshot = await client.bootstrap();
  const device = snapshot.devices.find((item) => item.id === deviceId);
  if (!device?.online || !device.projects.some((item) => item.id === projectId))
    throw new Error("The exact executor/project is not available");
  if (
    !report.create.submitted &&
    snapshot.sessions.some(
      (item) =>
        item.deviceId === deviceId &&
        item.projectId === projectId &&
        (["running", "stopping", "unknown"].includes(item.state) ||
          ["background-running", "processes-unchecked", "unconfirmed"].includes(
            item.stopStatus,
          )),
    )
  )
    throw new Error(
      "This project has another active or unconfirmed task; no new turn was submitted",
    );
  const created = await receipt(report.create, {
    projectId,
    kind: "create",
    title: "真实模型链路开发检查",
    policy: {
      mode: "edit",
      noTests: true,
      instructions:
        "本会话只检查模型文本往返。不要调用任何工具，不要读取或修改文件，不要执行命令，不要访问网络或发起审批。",
      completion: "只给出用户要求的固定文本后结束。",
    },
  });
  const sessionId = created.session?.id;
  if (!sessionId) throw new Error("Create receipt has no session");
  report.sessionId = sessionId;
  report.model = created.session.model;
  await save();

  let connected;
  const ready = new Promise((resolve) => {
    connected = resolve;
  });
  const timer = setTimeout(() => controller.abort(), 95000);
  stream = client
    .subscribe({
      after: snapshot.cursor,
      sessionId,
      signal: controller.signal,
      onState: (state) => {
        if (state === "connected") {
          report.streamConnected = true;
          connected();
        }
      },
      onEvent: (event) => {
        if (event.sessionId !== sessionId) return;
        appendFileSync(eventsPath, JSON.stringify(event) + "\n", {
          mode: 0o600,
        });
        report.streamEvents = (report.streamEvents ?? 0) + 1;
        if (event.kind === "engine.item/agentMessage/delta")
          report.agentDeltaEvents = (report.agentDeltaEvents ?? 0) + 1;
      },
      onCursor: (cursor) => {
        report.cursor = cursor;
      },
    })
    .finally(() => clearTimeout(timer));
  await Promise.race([
    ready,
    stream.then(() => {
      throw new Error("Event stream was not connected");
    }),
  ]);
  await receipt(report.start, {
    sessionId,
    kind: "start",
    input: [
      {
        type: "text",
        text: `MewDex 实际模型链路开发检查。请只回复：${report.expectedText} 不要调用工具、读取或修改文件、执行命令或访问网络。`,
      },
    ],
  });
  const turnId = report.start.result.session?.turnId;
  if (!turnId) throw new Error("Start receipt has no turn identifier");
  console.log(
    JSON.stringify({
      sessionId,
      startReceipt: report.start.result.state,
      model: report.model,
    }),
  );
  const deadline = Date.now() + 85000;
  do {
    const sessions = await client.sessions(deviceId);
    report.session = sessions.find((item) => item.id === sessionId);
    const page = await client.historyPage(sessionId, 0, 200);
    const turnEvents = page.events.filter(
      (event) =>
        event.payload?.turnId === turnId || event.payload?.turn?.id === turnId,
    );
    const completed = turnEvents.filter(
      (event) => event.kind === "engine.turn/completed",
    );
    const messages = turnEvents.filter(
      (event) =>
        event.kind === "engine.item/completed" &&
        event.payload?.item?.type === "agentMessage",
    );
    report.turnStatus = completed.at(-1)?.payload?.turn?.status;
    report.turnError = completed.at(-1)?.payload?.turn?.error;
    report.assistantText = messages
      .map((event) => event.payload.item.text)
      .join("\n");
    report.historyUserMessages = page.events.filter(
      (event) =>
        event.kind === "message.user" && event.payload?.id === report.start.id,
    ).length;
    report.toolItems = turnEvents.filter(
      (event) =>
        event.kind === "engine.item/started" &&
        [
          "commandExecution",
          "fileChange",
          "webSearch",
          "mcpToolCall",
          "dynamicToolCall",
        ].includes(event.payload?.item?.type),
    ).length;
    report.pendingInteractions = (await client.interactions(sessionId)).filter(
      (item) => item.state === "pending",
    ).length;
    report.observations ??= [];
    report.observations.push({
      at: new Date().toISOString(),
      turnStatus: report.turnStatus,
      state: report.session?.state,
      turnId: report.session?.turnId,
    });
    report.observations = report.observations.slice(-60);
    await save();
    // Event persistence and the final session projection are separate writes.
    // Observe both; do not mistake the first completed event for settled state.
    if (
      (report.turnStatus &&
        !report.session?.turnId &&
        !["running", "stopping"].includes(report.session?.state)) ||
      report.pendingInteractions ||
      report.session?.state === "unknown"
    )
      break;
    await new Promise((resolve) => setTimeout(resolve, 1500));
  } while (Date.now() < deadline && !controller.signal.aborted);
  report.passed =
    report.turnStatus === "completed" &&
    report.assistantText.trim() === report.expectedText &&
    report.historyUserMessages === 1 &&
    report.toolItems === 0 &&
    report.pendingInteractions === 0 &&
    report.session?.state === "idle" &&
    !report.session?.turnId &&
    report.streamConnected &&
    report.agentDeltaEvents > 0;
  await save();
  console.log(
    JSON.stringify({
      passed: report.passed,
      sessionId,
      turnStatus: report.turnStatus,
      sessionState: report.session?.state,
      deltaEvents: report.agentDeltaEvents ?? 0,
      historyUserMessages: report.historyUserMessages,
      assistantText: report.assistantText,
      toolItems: report.toolItems,
      pendingInteractions: report.pendingInteractions,
      turnError: report.turnError,
    }),
  );
  if (!report.passed) process.exitCode = 1;
} catch (error) {
  report.observationError = error.message;
  await save();
  console.error(error.message);
  process.exitCode = 1;
} finally {
  controller.abort();
  await stream;
  await save();
}
