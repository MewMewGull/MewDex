import { readFile, writeFile } from "node:fs/promises";
import { MewDexClient } from "../packages/sdk/src/index.ts";
const auth = JSON.parse(await readFile(".local/client-auth.json", "utf8"));
const client = new MewDexClient({
  baseURL: "http://127.0.0.1:47830",
  token: auth.token,
});
const completed = process.argv.includes("--completed");
const devices = await client.devices();
const device = devices.find((d) => d.id === "mewdex-ui-fixture-20261028");
const sessions = await client.sessions(device?.id);
const session = sessions.find((s) => s.id === "mewdex-ui-history-20261028");
const history = await client.historyPage(session?.id ?? "", 0, 80);
const queue = await client.queue(session?.id ?? "");
const policyProbe = completed
  ? (await client.sessions()).find((s) => s.title === "策略版本开发探测")
  : undefined;
const policyRestored =
  policyProbe?.policyVersion?.startsWith("e542128309") &&
  policyProbe.policy.mode === "focused" &&
  !policyProbe.policy.noTests &&
  policyProbe.policy.instructions === "MewDex 开发探测指令 A";
const report = {
  at: new Date().toISOString(),
  deviceId: device?.id,
  online: device?.online,
  sessionId: session?.id,
  frozen: session?.frozen,
  engineStartedDerivedFromSyntheticHistory: session?.engineStarted,
  upstreamThreadPresent: !!session?.engineId,
  historyEvents: history.events.length,
  hasMore: history.hasMore,
  historySynthetic: true,
  inferenceRun: false,
  queueStates: queue.map((item) => item.state),
  ...(completed
    ? {
        originalPolicyRestored: !!policyRestored,
      }
    : {}),
};
await writeFile(
  completed
    ? ".state/evidence/ui-fixture-completed.json"
    : ".state/evidence/ui-fixture-readiness.json",
  JSON.stringify(report, null, 2),
);
console.log(JSON.stringify(report));
if (
  !device?.online ||
  !session?.frozen ||
  session.engineId ||
  history.events.length !== 80 ||
  !history.hasMore ||
  (completed &&
    (!policyRestored || queue.length !== 1 || queue[0].state !== "cancelled"))
)
  process.exitCode = 1;
