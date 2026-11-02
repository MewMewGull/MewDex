// Stop and revoke only the development-only executor created by prepare-ui-fixture.
import { readFile, writeFile } from "node:fs/promises";
import { MewDexClient } from "../packages/sdk/src/index.ts";

const root = ".state/ui-fixture";
let runtime = JSON.parse(await readFile(`${root}/runtime.json`, "utf8"));
if (
  runtime.deviceId !== "mewdex-ui-fixture-20261028" ||
  !/^[a-f0-9]{32}$/.test(runtime.instanceId)
)
  throw new Error("Unexpected fixture instance");
if (runtime.state === "running") {
  await writeFile(`${root}/stop-${runtime.instanceId}`, "stop\n", {
    mode: 0o600,
  });
  const deadline = Date.now() + 10000;
  while (runtime.state === "running" && Date.now() < deadline) {
    await new Promise((resolve) => setTimeout(resolve, 250));
    runtime = JSON.parse(await readFile(`${root}/runtime.json`, "utf8"));
  }
}
if (runtime.state !== "stopped" || runtime.modelCalls !== 0)
  throw new Error(
    "Fixture did not stop without engine calls; inspect its retained state",
  );
const identity = JSON.parse(await readFile(`${root}/identity.json`, "utf8"));
if (identity.deviceId !== runtime.deviceId || !identity.identityId)
  throw new Error("Unexpected fixture identity");
const auth = JSON.parse(await readFile(".local/client-auth.json", "utf8"));
const client = new MewDexClient({
  baseURL: "http://127.0.0.1:47830",
  token: auth.token,
});
const result = await client.revoke(identity.identityId);
if (!result.revoked)
  throw new Error("Fixture identity revocation was not confirmed");
const report = {
  at: new Date().toISOString(),
  ...runtime,
  fixtureIdentityRevoked: true,
  historyAndQueueRetained: true,
  realDeviceServicesChanged: false,
};
await writeFile(
  ".state/evidence/ui-fixture-cleanup.json",
  JSON.stringify(report, null, 2),
);
console.log(JSON.stringify(report));
