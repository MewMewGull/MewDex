// Grant only the retained synthetic input access to the labelled no-model UI
// executor, then restore its exact original access after the native checks.
import { mkdir, readFile, writeFile } from "node:fs/promises";
import { MewDexClient } from "../packages/sdk/src/index.ts";

const mode = process.argv[2];
if (!["grant", "restore"].includes(mode))
  throw new Error("Use grant or restore");
const work = ".state/notification-input-runtime-20261029";
await mkdir(work, { recursive: true });
const plan = JSON.parse(
  await readFile(".state/ios-input-ui-resume-plan.json", "utf8"),
);
const auth = JSON.parse(await readFile(".local/client-auth.json", "utf8"));
const client = new MewDexClient({ baseURL: plan.baseURL, token: auth.token });
const target = (await client.identities()).find(
  (identity) => identity.id === plan.expectedInputIdentityId,
);
if (
  !target ||
  target.role !== "client" ||
  target.name !== plan.inputName ||
  !plan.alreadyPaired ||
  !plan.submitted
)
  throw new Error("The existing synthetic input could not be confirmed");
const path = work + "/temporary-access.json";
if (mode === "grant") {
  if (target.access.allDevices)
    throw new Error("The synthetic input must keep a limited access scope");
  const fixture = {
    deviceId: "mewdex-ui-fixture-20261028",
    projectIds: ["ui-fixture"],
  };
  if (
    target.access.targets.some((entry) => entry.deviceId === fixture.deviceId)
  )
    throw new Error("Observe the existing fixture grant before another change");
  const access = {
    ...target.access,
    targets: [...target.access.targets, fixture],
  };
  const record = {
    state: "armed",
    identityId: target.id,
    original: target.access,
    temporary: access,
  };
  await writeFile(path, JSON.stringify(record, null, 2), {
    flag: "wx",
    mode: 0o600,
  });
  await client.updateAccess(target.id, access);
  record.state = "granted";
  await writeFile(path, JSON.stringify(record, null, 2));
} else {
  const record = JSON.parse(await readFile(path, "utf8"));
  if (
    record.identityId !== target.id ||
    record.state !== "granted" ||
    JSON.stringify(record.temporary) !== JSON.stringify(target.access)
  )
    throw new Error(
      "Access changed since the temporary grant; preserve it for review",
    );
  await client.updateAccess(target.id, record.original);
  const after = (await client.identities()).find(
    (identity) => identity.id === target.id,
  );
  if (JSON.stringify(after?.access) !== JSON.stringify(record.original))
    throw new Error("Restored access does not match");
  record.state = "restored";
  await writeFile(path, JSON.stringify(record, null, 2));
}
console.log(
  JSON.stringify({
    state: mode === "grant" ? "granted" : "restored",
    identityId: target.id,
    modelInputsAdded: 0,
  }),
);
