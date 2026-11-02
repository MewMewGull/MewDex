// Read the retained mobile input's actual history. Never pairs, creates or
// submits a task, including when an earlier UI test stopped after submission.
import { readFile, writeFile } from "node:fs/promises";
import { MewDexClient } from "../packages/sdk/src/index.ts";

const planPath = process.argv[2] ?? ".state/ios-input-ui-resume-plan.json";
const reportPath =
  process.argv[3] ?? ".state/evidence/ios-input-network-observation.json";
if (
  !planPath.startsWith(".state/") ||
  !reportPath.startsWith(".state/evidence/")
)
  throw new Error(
    "Use the retained project-local probe plan and evidence path",
  );
const plan = JSON.parse(await readFile(planPath, "utf8"));
const auth = JSON.parse(await readFile(".local/client-auth.json", "utf8"));
const client = new MewDexClient({ baseURL: plan.baseURL, token: auth.token });
const sessions = (await client.sessions(plan.deviceId)).filter(
  (session) => session.title === plan.prompt.slice(0, 40),
);
const rows = [];
for (const session of sessions) {
  const events = (await client.historyPage(session.id, 0, 200)).events;
  const users = events.filter((event) => event.kind === "message.user");
  const sent = users.filter((event) =>
    event.payload?.input?.some((input) => input.text === plan.prompt),
  );
  const completed = events.filter(
    (event) => event.kind === "engine.turn/completed",
  );
  rows.push({
    sessionId: session.id,
    state: session.state,
    turnId: session.turnId,
    totalUserMessages: users.length,
    matchingUserMessages: sent.length,
    commandIds: sent.map((event) => event.payload.id),
    sourceIds: sent.map((event) => event.payload.sourceId),
    completedTurns: completed.length,
    turnStatus: completed.at(-1)?.payload.turn.status,
    assistantText: events
      .filter(
        (event) =>
          event.kind === "engine.item/completed" &&
          event.payload?.item?.type === "agentMessage",
      )
      .map((event) => event.payload.item.text)
      .join("\n"),
    deltaEvents: events.filter(
      (event) => event.kind === "engine.item/agentMessage/delta",
    ).length,
    modelToolItems: events.filter(
      (event) =>
        event.kind === "engine.item/started" &&
        [
          "commandExecution",
          "fileChange",
          "mcpToolCall",
          "webSearch",
          "dynamicToolCall",
        ].includes(event.payload?.item?.type),
    ).length,
  });
}
const report = {
  at: new Date().toISOString(),
  inputName: plan.inputName,
  expectedInputIdentityId: plan.expectedInputIdentityId,
  sessions: rows,
  readOnly: true,
  finalAcceptance: false,
};
report.passed =
  rows.length === 1 &&
  rows[0].matchingUserMessages === 1 &&
  rows[0].totalUserMessages === 1 &&
  rows[0].sourceIds[0] === plan.expectedInputIdentityId &&
  rows[0].completedTurns === 1 &&
  rows[0].turnStatus === "completed" &&
  rows[0].assistantText === plan.expectedReply &&
  rows[0].state === "idle" &&
  !rows[0].turnId &&
  rows[0].modelToolItems === 0;
await writeFile(reportPath, JSON.stringify(report, null, 2));
console.log(JSON.stringify(report));
if (!report.passed) process.exitCode = 1;
