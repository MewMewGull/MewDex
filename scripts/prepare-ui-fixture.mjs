// A development-only executor identity. No gateway grant or model credential.
import { readFile, writeFile, mkdir } from "node:fs/promises";
import { resolve, join } from "node:path";
import { MewDexClient } from "../packages/sdk/src/index.ts";

const root = resolve(".state/ui-fixture");
await mkdir(join(root, "project"), { recursive: true });
const baseURL = "http://127.0.0.1:47830";
const name = "界面检查 · 模拟记录（无模型）";
const deviceId = "mewdex-ui-fixture-20261028";
const tokenPath = join(root, "node-token");
const auth = JSON.parse(await readFile(".local/client-auth.json", "utf8"));
const admin = new MewDexClient({ baseURL, token: auth.token });
const identity = await admin.identity();
if (identity.role !== "admin")
  throw new Error("Development administrator identity is required");
let token;
try {
  token = await readFile(tokenPath, "utf8");
} catch (error) {
  if (error.code !== "ENOENT") throw error;
  const invitation = await admin.invite("executor", {
    allDevices: false,
    targets: [],
    gateway: false,
  });
  const peer = new MewDexClient({ baseURL });
  const credentials = await peer.pair(invitation.code, name, deviceId);
  if (
    credentials.identity.role !== "executor" ||
    credentials.identity.deviceId !== deviceId
  )
    throw new Error("Unexpected fixture identity");
  token = credentials.token;
  await writeFile(tokenPath, token, { flag: "wx", mode: 0o600 });
  await writeFile(
    join(root, "identity.json"),
    JSON.stringify({ identityId: credentials.identity.id, deviceId }),
    { flag: "wx", mode: 0o600 },
  );
}
const peer = new MewDexClient({ baseURL, token: token.trim() });
const current = await peer.identity();
if (
  current.role !== "executor" ||
  current.deviceId !== deviceId ||
  current.access.gateway
)
  throw new Error("Existing fixture credential is unsuitable");
const config = {
  deviceId,
  name,
  dataDir: join(root, "data"),
  hub: false,
  executor: true,
  hubUrl: baseURL,
  nodeTokenFile: tokenPath,
  projects: [
    {
      id: "ui-fixture",
      name: "界面检查（无模型）",
      path: join(root, "project"),
    },
  ],
  engine: { home: join(root, "engine-home") },
};
await writeFile(join(root, "device.json"), JSON.stringify(config, null, 2), {
  mode: 0o600,
});
console.log(
  JSON.stringify({
    deviceId,
    root,
    modelCredentialsRead: false,
    gatewayGranted: false,
    inferenceRun: false,
  }),
);
