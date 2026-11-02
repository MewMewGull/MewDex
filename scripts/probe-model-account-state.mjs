// Read only public account status through MewDex. No login, token refresh,
// credential copying or model turn. Reuse retained command IDs on observation.
import { readFile, writeFile } from "node:fs/promises";
import { MewDexClient, commandID } from "../packages/sdk/src/index.ts";

const args = process.argv.slice(2);
const reportIndex = args.indexOf("--report");
let reportPath = ".state/evidence/model-account-state.json";
if (reportIndex >= 0) {
  if (!args[reportIndex + 1])
    throw new Error("--report requires a retained result path");
  reportPath = args[reportIndex + 1];
  args.splice(reportIndex, 2);
}
const targetIDs = args;
if (!targetIDs.length) throw new Error("Specify the exact executor device IDs");
const auth = JSON.parse(await readFile(".local/client-auth.json", "utf8"));
const client = new MewDexClient({
  baseURL: "http://127.0.0.1:47830",
  token: auth.token,
});
let report;
try {
  report = JSON.parse(await readFile(reportPath, "utf8"));
  if (JSON.stringify(report.targetIDs) !== JSON.stringify(targetIDs))
    throw new Error(
      "A different account observation already exists; preserve it before changing targets",
    );
} catch (error) {
  if (error.code !== "ENOENT") throw error;
  report = {
    at: new Date().toISOString(),
    targetIDs,
    commands: targetIDs.map((deviceId) => ({
      deviceId,
      commandId: commandID(),
      submitted: false,
    })),
    inferenceRun: false,
    loginInitiated: false,
    tokenRefreshRequested: false,
    modelCredentialsCopied: false,
  };
  await writeFile(reportPath, JSON.stringify(report, null, 2), {
    flag: "wx",
    mode: 0o600,
  });
}
for (const entry of report.commands) {
  let result;
  try {
    if (entry.submitted) {
      result = await client.result(entry.commandId);
    } else {
      entry.submitted = true;
      await writeFile(reportPath, JSON.stringify(report, null, 2), {
        mode: 0o600,
      });
      result = await client.run({
        id: entry.commandId,
        deviceId: entry.deviceId,
        kind: "account.read",
      });
    }
    entry.state = result.state;
    if (result.state === "completed") {
      entry.account = {
        signedIn: !!result.data?.signedIn,
        type: result.data?.type ?? "",
        planType: result.data?.planType ?? "",
        loginSupported: !!result.data?.loginSupported,
      };
    }
  } catch {
    entry.state = "unconfirmed";
  }
  report.observedAt = new Date().toISOString();
  await writeFile(reportPath, JSON.stringify(report, null, 2), { mode: 0o600 });
  console.log(
    JSON.stringify({
      deviceId: entry.deviceId,
      state: entry.state,
      account: entry.account,
    }),
  );
}
