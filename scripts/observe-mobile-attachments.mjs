// Observe only the retained mobile probe's uploaded synthetic attachments.
// No upload, model input, pairing, credential read, or application-state write.
import { createHash } from "node:crypto";
import {
  lstat,
  readFile,
  readdir,
  realpath,
  writeFile,
} from "node:fs/promises";
import path from "node:path";

const [planPath, fixturePath, reportPath] = process.argv.slice(2);
if (
  !planPath?.startsWith(".state/") ||
  !fixturePath?.startsWith(".state/") ||
  !reportPath?.startsWith(".state/evidence/")
)
  throw new Error(
    "Use explicit project-local probe, synthetic fixture, and evidence paths",
  );
const root = await realpath(".");
const local = await realpath(".local");
const inside = (parent, value) => {
  const relative = path.relative(parent, value);
  return (
    relative !== ".." &&
    !relative.startsWith(".." + path.sep) &&
    !path.isAbsolute(relative)
  );
};
if (!inside(root, local))
  throw new Error("Local development data left the workspace");
const output = path.resolve(root, reportPath);
if (
  !inside(
    path.join(root, ".state", "evidence"),
    await realpath(path.dirname(output)),
  )
)
  throw new Error("Attachment evidence left its owned directory");
for (const source of [planPath, fixturePath])
  if (!inside(path.join(root, ".state"), await realpath(source)))
    throw new Error("Probe source left its owned state directory");
const plan = JSON.parse(await readFile(planPath, "utf8"));
if (!/^[a-f0-9]{32}$/.test(plan.expectedInputIdentityId ?? ""))
  throw new Error("The retained mobile input identity is required");
const config = JSON.parse(await readFile(".local/device.json", "utf8"));
const directory = await realpath(
  path.resolve(local, config.dataDir, "attachments"),
);
if (!inside(local, directory))
  throw new Error(
    "Attachment observations must stay inside local development data",
  );
const expected = createHash("sha256")
  .update(await readFile(fixturePath))
  .digest("hex");
const names = await readdir(directory);
if (names.length > 20000)
  throw new Error("Attachment observation exceeds its bound");
const attachments = [];
for (const name of names) {
  if (!/^[a-f0-9]{32}\.json$/.test(name)) continue;
  const metaPath = path.join(directory, name),
    metaStat = await lstat(metaPath);
  if (!metaStat.isFile() || metaStat.isSymbolicLink() || metaStat.size > 65536)
    throw new Error("Unexpected attachment metadata file");
  const meta = JSON.parse(await readFile(metaPath, "utf8"));
  if (meta.ownerId !== plan.expectedInputIdentityId) continue;
  if (
    meta.id + ".json" !== name ||
    meta.targetDeviceId !== plan.deviceId ||
    meta.targetProjectId !== plan.projectId
  )
    throw new Error(
      "Mobile attachment has an unexpected identity or execution target",
    );
  const filePath = path.join(directory, meta.id),
    fileStat = await lstat(filePath);
  if (
    !fileStat.isFile() ||
    fileStat.isSymbolicLink() ||
    fileStat.size > 20 * 1024 * 1024
  )
    throw new Error("Unexpected mobile attachment content file");
  const actual = createHash("sha256")
    .update(await readFile(filePath))
    .digest("hex");
  attachments.push({
    id: meta.id,
    name: meta.name,
    mediaType: meta.mediaType,
    size: meta.size,
    ownerId: meta.ownerId,
    targetDeviceId: meta.targetDeviceId,
    targetProjectId: meta.targetProjectId,
    sha256: actual,
    integrityVerified: actual === meta.sha256 && fileStat.size === meta.size,
    matchesSyntheticFixture: actual === expected,
  });
}
const report = {
  at: new Date().toISOString(),
  readOnly: true,
  syntheticFixtureSha256: expected,
  expectedInputIdentityId: plan.expectedInputIdentityId,
  attachments,
  matchingAttachments: attachments.filter(
    (value) => value.matchesSyntheticFixture && value.integrityVerified,
  ).length,
  modelInputSubmitted: false,
  modelCredentialsRead: false,
  packagingValidation: false,
};
await writeFile(output, JSON.stringify(report, null, 2), { flag: "wx" });
console.log(JSON.stringify(report));
