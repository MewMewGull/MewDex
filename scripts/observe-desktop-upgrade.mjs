// Read back one explicitly owned synthetic installation after the real UI
// upgrade. No service, engine, command, credential or model round is started.
import { readFile, writeFile, realpath, readdir } from "node:fs/promises";
import path from "node:path";
import { DatabaseSync } from "node:sqlite";

const [workArgument, outputArgument] = process.argv.slice(2);
if (
  !workArgument?.startsWith(".state/") ||
  !outputArgument?.startsWith(".state/evidence/")
)
  throw new Error("Use explicit project-local fixture and evidence paths");
const workspace = await realpath(".");
const inside = (parent, value) => {
  const relative = path.relative(parent, value);
  return (
    relative &&
    !relative.startsWith(".." + path.sep) &&
    relative !== ".." &&
    !path.isAbsolute(relative)
  );
};
const work = await realpath(workArgument);
if (!inside(path.join(workspace, ".state"), work))
  throw new Error("Synthetic fixture left its workspace");
const source = JSON.parse(
  await readFile(path.join(work, "legacy-fixture.json"), "utf8"),
);
if (
  source.modelCalls !== 0 ||
  source.serviceStarted !== false ||
  source.name !== "升级恢复界面检查 · 模拟数据（无模型）"
)
  throw new Error("Only the retained synthetic upgrade fixture is supported");
const root = await realpath(source.root);
if (!inside(work, root)) throw new Error("Installation root changed");
const config = JSON.parse(
  await readFile(path.join(root, "device.json"), "utf8"),
);
const data = await realpath(config.dataDir);
const original = await realpath(path.join(root, "data"));
if (!inside(path.join(root, "upgrades"), data) || original === data)
  throw new Error("The adopted data must be a separate upgrade copy");
function history(directory) {
  const db = new DatabaseSync(path.join(directory, "executor.db"), {
    readOnly: true,
  });
  try {
    const row = db
      .prepare(
        "SELECT data FROM objects WHERE bucket='sessions' AND id='ui-upgrade-20261029'",
      )
      .get();
    const session = JSON.parse(Buffer.from(row.data).toString());
    const commands = db
      .prepare("SELECT count(*) AS count FROM commands")
      .get().count;
    return { session, commands };
  } finally {
    db.close();
  }
}
const currentHistory = history(data),
  originalHistory = history(original);
const files = (await readdir(path.join(root, "operations"))).filter((file) =>
  /^[a-f0-9]{32}\.json$/.test(file),
);
const jobs = [];
for (const file of files) {
  const record = JSON.parse(
    await readFile(path.join(root, "operations", file), "utf8"),
  );
  const job = record.info;
  if (
    record.format !== 1 ||
    job?.id + ".json" !== file ||
    job.operation !== record.intent?.operation
  )
    throw new Error("Unknown local operation record format");
  jobs.push({
    id: job.id,
    operation: job.operation,
    state: job.state,
    upgrade: job.upgrade,
  });
}
const engineManifest = JSON.parse(
  await readFile(
    path.join(
      path.dirname(config.engine.executable),
      "..",
      "mewdex-engine.json",
    ),
    "utf8",
  ),
);
const engineLock = JSON.parse(await readFile("engine.lock.json", "utf8"));
const report = {
  at: new Date().toISOString(),
  readOnly: true,
  fixtureRoot: root,
  deviceId: config.deviceId,
  dataDirectory: data,
  originalDataDirectory: original,
  engineExecutable: config.engine.executable,
  engineVersion: engineManifest.version,
  engineVersionMatchesCurrentComponent:
    engineManifest.version === engineLock.version,
  modelDirectoryReferencePreserved: config.engine.home === source.engineHome,
  projectsPreserved:
    config.projects.length === 1 &&
    config.projects[0].id === source.projectId &&
    config.projects[0].path === source.project,
  deviceIdPreserved: config.deviceId === source.deviceId,
  currentHistory,
  originalHistory,
  jobs,
  modelCredentialsRead: false,
  modelCalls: 0,
  packagingValidation: false,
};
report.passed =
  report.engineVersionMatchesCurrentComponent &&
  report.modelDirectoryReferencePreserved &&
  report.projectsPreserved &&
  report.deviceIdPreserved &&
  [currentHistory, originalHistory].every(
    (value) =>
      value.commands === 0 &&
      value.session.frozen === true &&
      value.session.state === "paused" &&
      !value.session.engineId,
  ) &&
  jobs.some(
    (job) => job.operation === "upgrade.prepare" && job.state === "succeeded",
  ) &&
  jobs.some(
    (job) =>
      job.operation === "upgrade.apply" &&
      job.state === "succeeded" &&
      job.upgrade?.applied === true,
  );
const output = path.resolve(outputArgument);
const evidence = await realpath(path.join(workspace, ".state", "evidence"));
const parent = await realpath(path.dirname(output));
if (parent !== evidence && !inside(evidence, parent))
  throw new Error("Evidence directory left its workspace");
await writeFile(output, JSON.stringify(report, null, 2), { flag: "wx" });
console.log(
  JSON.stringify({
    passed: report.passed,
    deviceIdPreserved: report.deviceIdPreserved,
    projectsPreserved: report.projectsPreserved,
    modelDirectoryReferencePreserved: report.modelDirectoryReferencePreserved,
    commands: currentHistory.commands,
    frozen: currentHistory.session.frozen,
    jobs: jobs.map(({ upgrade, ...job }) => job),
  }),
);
if (!report.passed) process.exitCode = 1;
