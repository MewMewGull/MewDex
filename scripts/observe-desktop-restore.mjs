// Observe the retained, synthetic Windows UI backup/restore fixture only.
// This never starts a service, decrypts an archive, submits a job or reads model credentials.
import {
  readFile,
  writeFile,
  realpath,
  readdir,
  stat,
  lstat,
  mkdir,
  mkdtemp,
  copyFile,
} from "node:fs/promises";
import { createHash } from "node:crypto";
import { DatabaseSync } from "node:sqlite";
import path from "node:path";

const [workArgument, phase, outputArgument, requestedPlanId] =
  process.argv.slice(2);
if (
  !workArgument?.startsWith(".state/") ||
  !["prepared", "applied", "reopened"].includes(phase) ||
  !outputArgument?.startsWith(".state/evidence/") ||
  (requestedPlanId && !/^[a-f0-9]{32}$/.test(requestedPlanId))
)
  throw new Error("Use an explicit synthetic fixture, phase and evidence path");
const inside = (parent, child) => {
  const relative = path.relative(parent, child);
  return (
    relative &&
    !relative.startsWith(".." + path.sep) &&
    relative !== ".." &&
    !path.isAbsolute(relative)
  );
};
const sha256 = (value) => createHash("sha256").update(value).digest("hex");
const json = async (file) => JSON.parse(await readFile(file, "utf8"));
const workspace = await realpath(".");
const output = path.resolve(outputArgument);
const evidence = await realpath(path.join(workspace, ".state", "evidence"));
const outputParent = await realpath(path.dirname(output));
if (outputParent !== evidence && !inside(evidence, outputParent))
  throw new Error("Evidence directory left workspace");
const copies = await mkdtemp(path.join(outputParent, "restore-observation-"));
const work = await realpath(workArgument);
if (!inside(path.join(workspace, ".state"), work))
  throw new Error("Fixture left its workspace");
const source = await json(path.join(work, "legacy-fixture.json"));
if (
  source.modelCalls !== 0 ||
  source.serviceStarted !== false ||
  source.name !== "升级恢复界面检查 · 模拟数据（无模型）"
)
  throw new Error("Only the retained no-model synthetic fixture is supported");
const root = await realpath(source.root);
if (!inside(work, root)) throw new Error("Installation root left its fixture");
const jobs = [];
for (const name of await readdir(path.join(root, "operations"))) {
  if (!/^[a-f0-9]{32}\.json$/.test(name)) continue;
  const record = await json(path.join(root, "operations", name));
  if (
    record.format !== 1 ||
    record.info?.id + ".json" !== name ||
    record.intent?.operation !== record.info?.operation
  )
    throw new Error("Unknown operation record format");
  jobs.push({ ...record.info, requestedRestoreId: record.intent.restoreId });
}
function oneJob(operation, matches = () => true) {
  const found = jobs.filter(
    (job) => job.operation === operation && matches(job),
  );
  if (found.length !== 1 || found[0].state !== "succeeded")
    throw new Error("Expected one succeeded " + operation + " job");
  return found[0];
}
const backup = oneJob("backup");
const prepare = oneJob(
  "restore.prepare",
  (job) => !requestedPlanId || job.restore?.id === requestedPlanId,
);
const upgrade = oneJob("upgrade.apply");
const planId = prepare.restore?.id;
if (!/^[a-f0-9]{32}$/.test(planId))
  throw new Error("Missing prepared restore ID");
const plan = await json(path.join(root, "restore-plans", planId + ".json"));
const staged = await realpath(path.join(root, "restores", planId));
if (!inside(path.join(root, "restores"), staged))
  throw new Error("Staged state left its owned area");
const currentRaw = await readFile(path.join(root, "device.json"));
const current = JSON.parse(currentRaw.toString());
const preparedRaw = await readFile(path.join(staged, "device.json"));
const prepared = JSON.parse(preparedRaw.toString());
const expectedApplied = phase !== "prepared";
const checks = {
  reviewedPlan:
    plan.format === 1 &&
    plan.info.id === planId &&
    plan.info.requiresPairing === true &&
    plan.info.requiresModelLogin === true &&
    plan.info.previousStatePreserved === true,
  preparedProfileMatches: sha256(preparedRaw) === plan.preparedProfile,
  activeProfileMatches:
    sha256(currentRaw) ===
    (expectedApplied ? plan.preparedProfile : plan.beforeProfile),
  devicePreserved:
    prepared.deviceId === source.deviceId &&
    current.deviceId === source.deviceId,
  projectsPreserved:
    prepared.projects.length === 1 &&
    prepared.projects[0].id === source.projectId &&
    prepared.projects[0].path === source.project,
  stagedDataBound: prepared.dataDir === path.join(staged, "data"),
  isolatedModelHome:
    prepared.engine.home === path.join(staged, "engine-home") &&
    prepared.engine.home !== source.engineHome,
  noRetainedNodeToken: !prepared.nodeTokenFile,
};
const stagedFiles = [];
for (const entry of plan.files) {
  const filename = await realpath(path.join(staged, entry.name));
  if (!inside(staged, filename))
    throw new Error("Prepared file left its stage");
  const raw = await readFile(filename);
  stagedFiles.push({
    name: entry.name,
    bytes: raw.length,
    matches: raw.length === entry.bytes && sha256(raw) === entry.sha256,
  });
}
checks.preparedFilesMatch = stagedFiles.every((entry) => entry.matches);
async function inventory(directory, relative = "") {
  const files = [];
  for (const name of (await readdir(path.join(directory, relative))).sort()) {
    const child = path.join(relative, name);
    if (child === path.join("data", "service.lock")) continue;
    const filename = path.join(directory, child);
    const info = await lstat(filename);
    if (info.isSymbolicLink())
      throw new Error("Links are not supported in this synthetic observation");
    if (info.isDirectory()) files.push(...(await inventory(directory, child)));
    else if (info.isFile()) {
      const raw = await readFile(filename);
      files.push({
        name: child.split(path.sep).join("/"),
        bytes: raw.length,
        sha256: sha256(raw),
      });
    } else throw new Error("Unexpected fixture file type");
  }
  return files.sort((a, b) => a.name.localeCompare(b.name, "en"));
}
const stageBefore = await inventory(staged);
checks.noUnexpectedPreparedFiles =
  JSON.stringify(stageBefore.map(({ name }) => name).sort()) ===
  JSON.stringify(plan.files.map(({ name }) => name).sort());
const originalFiles = [];
for (const entry of plan.beforeData) {
  const filename = await realpath(path.join(root, entry.name));
  if (!inside(root, filename))
    throw new Error("Previous data left its installation");
  const raw = await readFile(filename);
  originalFiles.push({
    name: entry.name,
    matches: raw.length === entry.bytes && sha256(raw) === entry.sha256,
  });
}
checks.previousDataPreserved = originalFiles.every((entry) => entry.matches);
const archivePath = await realpath(backup.backup.path);
if (!inside(work, archivePath) || inside(root, archivePath))
  throw new Error("Backup path left its fixture");
const archive = await readFile(archivePath);
checks.encryptedArchiveMatches =
  archive.subarray(0, 22).toString() ===
    "age-encryption.org/v1\n".slice(0, 22) &&
  sha256(archive) === plan.archiveSha256;
async function history(directory, label) {
  // A SQLite read-only connection can still create/change WAL/SHM sidecars.
  // Copy the stopped fixture's database and WAL first; SQLite sees copies only.
  const copy = path.join(copies, label);
  await mkdir(copy);
  for (const name of ["executor.db", "executor.db-wal"]) {
    const sourceFile = path.join(directory, name);
    try {
      const resolved = await realpath(sourceFile);
      if (!inside(root, resolved) || !(await lstat(sourceFile)).isFile())
        throw new Error("Database copy left its fixture");
      await copyFile(resolved, path.join(copy, name));
    } catch (error) {
      if (error.code !== "ENOENT") throw error;
    }
  }
  const db = new DatabaseSync(path.join(copy, "executor.db"), {
    readOnly: true,
  });
  try {
    const row = db
      .prepare(
        "SELECT data FROM objects WHERE bucket='sessions' AND id='ui-upgrade-20261029'",
      )
      .get();
    const session = JSON.parse(Buffer.from(row.data).toString());
    const count = (table) =>
      db.prepare("SELECT count(*) AS n FROM " + table).get().n;
    return {
      session,
      commands: count("commands"),
      identities: count("identities"),
      invitations: count("invitations"),
    };
  } finally {
    db.close();
  }
}
const histories = {
  original: await history(path.join(root, "data"), "original"),
  upgraded: await history(
    path.join(root, "upgrades", upgrade.upgrade.id, "data"),
    "upgraded",
  ),
  restored: await history(prepared.dataDir, "restored"),
};
checks.observationPreservedPreparedFiles =
  JSON.stringify(stageBefore) === JSON.stringify(await inventory(staged));
checks.historyPreservedAndFrozen = Object.values(histories).every(
  (value) =>
    value.commands === 0 &&
    value.session.frozen === true &&
    value.session.state === "paused" &&
    !value.session.engineId,
);
checks.noSyntheticAccessRevived = Object.values(histories).every(
  (value) => value.identities === 0 && value.invitations === 0,
);
const modelHomeEntries = await readdir(prepared.engine.home);
checks.emptyModelDirectory = modelHomeEntries.length === 0;
if (expectedApplied) {
  const apply = oneJob(
    "restore.apply",
    (job) => job.requestedRestoreId === planId,
  );
  checks.originalApplyResult =
    apply.restore.id === planId && apply.restore.applied === true;
  const previousRaw = await readFile(
    path.join(root, "restore-plans", planId + ".previous.json"),
  );
  checks.previousProfilePreserved = sha256(previousRaw) === plan.beforeProfile;
} else {
  checks.noApplySubmitted = jobs.every(
    (job) =>
      job.operation !== "restore.apply" || job.requestedRestoreId !== planId,
  );
}
const report = {
  at: new Date().toISOString(),
  phase,
  readOnlySource: true,
  sqliteObservationCopies: copies,
  syntheticOnly: true,
  fixtureRoot: root,
  deviceId: current.deviceId,
  planId,
  activeData: current.dataDir,
  stagedData: prepared.dataDir,
  backup: {
    path: archivePath,
    bytes: (await stat(archivePath)).size,
    sha256: sha256(archive),
    files: backup.backup.files,
  },
  checks,
  histories,
  stagedFiles,
  originalFiles,
  jobs: jobs.map(({ id, operation, state }) => ({ id, operation, state })),
  modelCredentialsRead: false,
  modelCalls: 0,
  serviceStarted: false,
  packagingValidation: false,
  passed: Object.values(checks).every(Boolean),
};
await writeFile(output, JSON.stringify(report, null, 2), { flag: "wx" });
console.log(
  JSON.stringify({
    passed: report.passed,
    phase,
    planId,
    checks,
    jobs: report.jobs,
  }),
);
if (!report.passed) process.exitCode = 1;
