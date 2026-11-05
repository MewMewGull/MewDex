// Export independent Hub, web input and executor packages for one target.
import { spawnSync } from "node:child_process";
import { createHash } from "node:crypto";
import { createReadStream } from "node:fs";
import {
  cp,
  lstat,
  mkdir,
  mkdtemp,
  readFile,
  readdir,
  rename,
  rm,
  writeFile,
} from "node:fs/promises";
import path from "node:path";

const root = path.resolve(import.meta.dirname, "..");
const targets = {
  "win32:x64": {
    os: "windows",
    arch: "amd64",
    label: "Windows-x64",
    extension: ".exe",
  },
  "win32:arm64": {
    os: "windows",
    arch: "arm64",
    label: "Windows-arm64",
    extension: ".exe",
  },
  "darwin:x64": {
    os: "darwin",
    arch: "amd64",
    label: "macOS-x64",
    extension: "",
  },
  "darwin:arm64": {
    os: "darwin",
    arch: "arm64",
    label: "macOS-arm64",
    extension: "",
  },
  "linux:x64": {
    os: "linux",
    arch: "amd64",
    label: "Linux-x64",
    extension: "",
  },
  "linux:arm64": {
    os: "linux",
    arch: "arm64",
    label: "Linux-arm64",
    extension: "",
  },
};

const options = new Map();
for (const argument of process.argv.slice(2)) {
  const match = /^(--target|--component)=(.+)$/.exec(argument);
  if (!match || options.has(match[1]))
    throw new Error("Expected unique --target and --component options");
  options.set(match[1], match[2]);
}
const targetKey =
  options.get("--target") ?? `${process.platform}:${process.arch}`;
const target = targets[targetKey];
if (!target) throw new Error("Unsupported service target");
const selected = options.get("--component") ?? "all";
if (!["all", "hub", "executor", "web"].includes(selected))
  throw new Error("Select hub, executor, web or all");
const components = selected === "all" ? ["hub", "executor", "web"] : [selected];
const product = JSON.parse(
  await readFile(path.join(root, "mewdex.product.json"), "utf8"),
);
const version = JSON.parse(
  await readFile(path.join(root, "package.json"), "utf8"),
).version;
const engineVersion = JSON.parse(
  await readFile(path.join(root, "engine.lock.json"), "utf8"),
).version;
const resources = path.join(
  root,
  ".state",
  "service-resources",
  `${target.label}-${engineVersion}`,
);
const platformDirectory = target.os === "darwin" ? "macos" : target.os;
const output = path.join(root, "dist", platformDirectory);

function run(program, args, extra = {}) {
  const result = spawnSync(program, args, {
    cwd: root,
    stdio: "inherit",
    windowsHide: true,
    ...extra,
  });
  if (result.status !== 0)
    throw new Error(
      `${path.basename(program)} failed (${result.status ?? result.error?.message})`,
    );
}

async function copy(source, destination) {
  const info = await lstat(source);
  if (info.isSymbolicLink() || (!info.isDirectory() && !info.isFile()))
    throw new Error("Unsupported package input");
  await cp(source, destination, {
    recursive: true,
    filter: async (entry) => {
      const item = await lstat(entry);
      if (item.isSymbolicLink() || (!item.isDirectory() && !item.isFile()))
        throw new Error("Package inputs must be regular files");
      return true;
    },
  });
}

async function notices(destination, web) {
  for (const name of [
    "LICENSE",
    "NOTICE",
    "THIRD_PARTY_NOTICES.md",
    "mewdex.product.json",
    "docs/architecture.md",
    "docs/licensing.md",
    "docs/dependency-licenses.json",
  ]) {
    await mkdir(path.dirname(path.join(destination, name)), {
      recursive: true,
    });
    await copy(path.join(root, name), path.join(destination, name));
  }
  const inventory = JSON.parse(
    await readFile(path.join(root, "docs/dependency-licenses.json"), "utf8"),
  );
  const modules = spawnSync(
    "go",
    ["list", "-m", "-f", "{{.Path}}\t{{.Dir}}", "all"],
    { cwd: root, encoding: "utf8", windowsHide: true },
  );
  if (modules.status !== 0)
    throw new Error("Cannot resolve Go license sources");
  const directories = new Map(
    modules.stdout
      .trim()
      .split(/\r?\n/)
      .map((line) => line.split("\t")),
  );
  for (const module of inventory.go)
    for (const item of module.evidence) {
      const source = directories.get(module.name);
      if (!source) throw new Error(`Missing license source for ${module.name}`);
      const targetFile = path.join(
        destination,
        "licenses",
        "go",
        module.name,
        item.file,
      );
      await mkdir(path.dirname(targetFile), { recursive: true });
      await copy(path.join(source, item.file), targetFile);
    }
  if (!web) return;
  const lock = JSON.parse(
    await readFile(path.join(root, "package-lock.json"), "utf8"),
  );
  for (const [name, entry] of Object.entries(lock.packages)) {
    if (!name.includes("node_modules/") || entry.dev || entry.link) continue;
    const directory = path.join(root, name);
    for (const file of await readdir(directory, { withFileTypes: true }).catch(
      (error) => {
        if (error.code === "ENOENT") return [];
        throw error;
      },
    )) {
      if (
        !file.isFile() ||
        !/^(license|licence|copying|notice)/i.test(file.name)
      )
        continue;
      const targetFile = path.join(
        destination,
        "licenses",
        "npm",
        name.replaceAll("node_modules/", ""),
        file.name,
      );
      await mkdir(path.dirname(targetFile), { recursive: true });
      await copy(path.join(directory, file.name), targetFile);
    }
  }
}

async function hash(file) {
  const digest = createHash("sha256");
  for await (const block of createReadStream(file)) digest.update(block);
  return digest.digest("hex");
}

async function exists(file) {
  try {
    await lstat(file);
    return true;
  } catch (error) {
    if (error.code === "ENOENT") return false;
    throw error;
  }
}

await mkdir(path.join(root, ".state"), { recursive: true });
await mkdir(output, { recursive: true });
if (components.some((component) => component !== "web")) {
  run(process.execPath, [
    path.join(root, "scripts/prepare-service-resources.mjs"),
    `--target=${targetKey}`,
    `--output=${path.relative(root, resources)}`,
  ]);
}
if (components.includes("web")) {
  const npmCLI =
    process.env.npm_execpath ??
    path.join(
      path.dirname(process.execPath),
      "node_modules/npm/bin/npm-cli.js",
    );
  run(process.execPath, [npmCLI, "run", "build:ui"]);
}

const scratch = await mkdtemp(path.join(root, ".state/component-package-"));
const report = [];
let preserveScratch = false;
try {
  for (const component of components) {
    const title =
      component === "hub" ? "Hub" : component === "web" ? "Web" : "Executor";
    const name = `MewDex-${title}-${target.label}`;
    const stage = path.join(scratch, name);
    await mkdir(stage);
    const binary = `mewdex-${component}${target.extension}`;
    run(
      "go",
      [
        "build",
        "-trimpath",
        ...(component === "web"
          ? []
          : [`-ldflags=-X main.distributionRole=${component}`]),
        "-o",
        path.join(stage, binary),
        component === "web" ? "./cmd/mewdex-web" : "./cmd/mewdex",
      ],
      {
        env: {
          ...process.env,
          GOOS: target.os,
          GOARCH: target.arch,
          CGO_ENABLED: "0",
        },
      },
    );
    if (component === "web")
      await copy(path.join(root, "apps/client/dist"), path.join(stage, "ui"));
    else await copy(path.join(resources, "engine"), path.join(stage, "engine"));
    await notices(stage, component === "web");
    const instructions = {
      hub: `${binary} init -config .local/hub/device.json\n${binary} account -config .local/hub/device.json\n${binary} serve -config .local/hub/device.json\n\nProvide account JSON on standard input for the account command. Configure listen and allowedOrigins in device.json for your deployment.\n`,
      executor: `${binary} panel -root .local/executor\n\nOpen http://127.0.0.1:47832 and use .local/executor/panel-token. Configure the Hub URL and an executor invitation, add local directories, then start the executor. This node connects to Hub over WebSocket.\n`,
      web: `${binary} -hub-url https://your-hub.example.com -listen 127.0.0.1:47831\n\nDeploy this web service independently. Set the public web origin in Hub allowedOrigins. The ui directory contains the shared input terminal.\n`,
    };
    await writeFile(
      path.join(stage, "START-HERE.txt"),
      instructions[component],
    );
    await writeFile(
      path.join(stage, "component.json"),
      JSON.stringify(
        {
          schemaVersion: 1,
          distribution: component,
          components: product.distributions[component].components,
          version,
          os: platformDirectory,
          arch: target.arch === "amd64" ? "x64" : target.arch,
          configuration: "Release",
          engineVersion: component === "web" ? undefined : engineVersion,
        },
        null,
        2,
      ) + "\n",
    );
    const archive = path.join(scratch, `${name}.zip`);
    const zipProgram = `
import pathlib
import sys
import zipfile

source = pathlib.Path(sys.argv[1])
with zipfile.ZipFile(sys.argv[2], "x", zipfile.ZIP_DEFLATED, compresslevel=6) as archive:
    for file in sorted(source.rglob("*")):
        if file.is_file():
            archive.write(file, file.relative_to(source.parent))
with zipfile.ZipFile(sys.argv[2]) as archive:
    if archive.testzip() is not None:
        raise RuntimeError("Package archive failed CRC validation")
`;
    run(process.platform === "win32" ? "python" : "python3", [
      "-c",
      zipProgram,
      stage,
      archive,
    ]);
    const destination = path.join(output, name);
    const destinationArchive = `${destination}.zip`;
    const previous = path.join(scratch, `${name}-previous`);
    const previousArchive = `${previous}.zip`;
    const hadDirectory = await exists(destination);
    const hadArchive = await exists(destinationArchive);
    if (hadDirectory) await rename(destination, previous);
    try {
      if (hadArchive) await rename(destinationArchive, previousArchive);
      await rename(stage, destination);
      await rename(archive, destinationArchive);
    } catch (error) {
      preserveScratch = true;
      if (await exists(destination)) await rename(destination, stage);
      if (hadDirectory) await rename(previous, destination);
      if (hadArchive && (await exists(previousArchive)))
        await rename(previousArchive, destinationArchive);
      preserveScratch = false;
      throw error;
    }
    report.push({
      distribution: component,
      directory: destination,
      archive: destinationArchive,
      sha256: await hash(destinationArchive),
    });
  }
  await writeFile(
    path.join(output, "component-packages.json"),
    JSON.stringify({ version, target: targetKey, packages: report }, null, 2) +
      "\n",
  );
  console.log(JSON.stringify(report, null, 2));
} finally {
  const relative = path.relative(path.join(root, ".state"), scratch);
  if (
    !preserveScratch &&
    relative.startsWith("component-package-") &&
    !relative.includes(path.sep)
  )
    await rm(scratch, { recursive: true });
}
