// One explicit, temporary input identity. No model credential or existing
// native credential is read. The private pairing code is never printed.
import { readFile, writeFile } from "node:fs/promises";
import { MewDexClient, commandID } from "../packages/sdk/src/index.ts";

const path = ".state/ios-input-ui-plan.json";
let plan;
try {
  plan = JSON.parse(await readFile(path, "utf8"));
  if (!plan.pairCode)
    throw new Error(
      "The previous invitation result is unconfirmed; do not create another automatically",
    );
} catch (error) {
  if (error.code !== "ENOENT") throw error;
  const auth = JSON.parse(await readFile(".local/client-auth.json", "utf8"));
  const client = new MewDexClient({
    baseURL: "http://127.0.0.1:47830",
    token: auth.token,
  });
  const deviceId = "8def8f91e1bb3fc777ae0af2269829c7",
    projectId = "mewdex";
  const devices = await client.devices();
  if (
    !devices.some(
      (device) =>
        device.id === deviceId &&
        device.online &&
        device.projects.some((project) => project.id === projectId),
    )
  )
    throw new Error("The exact Windows executor/project is not available");
  if (
    (await client.sessions(deviceId)).some(
      (session) =>
        session.projectId === projectId &&
        (["running", "stopping", "unknown"].includes(session.state) ||
          ["background-running", "processes-unchecked", "unconfirmed"].includes(
            session.stopStatus,
          )),
    )
  )
    throw new Error(
      "The project has another active or unconfirmed task; no invitation was created",
    );
  const nonce = commandID().slice(0, 8);
  plan = {
    at: new Date().toISOString(),
    baseURL: "https://mewdex.mewgullnest.top",
    deviceId,
    projectId,
    inputName: `MewDex iOS UI Risk ${nonce}`,
    submitted: false,
    invitationRequested: true,
    prompt: `MewDex iOS input runtime check ${nonce}. Do not use tools, read or modify files, run commands, or access the network. Reply exactly: MewDex iOS input connected.`,
    expectedReply: "MewDex iOS input connected.",
    baselineIdentityIds: (await client.identities()).map(
      (identity) => identity.id,
    ),
    modelCredentialsCopied: false,
    finalCandidate: false,
  };
  await writeFile(path, JSON.stringify(plan, null, 2), {
    flag: "wx",
    mode: 0o600,
  });
  const invitation = await client.invite("client", {
    allDevices: false,
    targets: [{ deviceId, projectIds: [projectId] }],
    gateway: false,
  });
  plan.pairCode = invitation.code;
  await writeFile(path, JSON.stringify(plan, null, 2), { mode: 0o600 });
}
console.log(
  JSON.stringify({
    privatePlan: path,
    inputName: plan.inputName,
    deviceId: plan.deviceId,
    projectId: plan.projectId,
    submitted: plan.submitted,
  }),
);
