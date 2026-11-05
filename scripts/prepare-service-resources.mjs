// Prepare exactly one platform's development resources. No global install,
// model credentials, executable launch, installer or final candidate is involved.
import { createHash } from "node:crypto";
import { createReadStream } from "node:fs";
import {
  mkdir,
  readFile,
  writeFile,
  open,
  readdir,
  lstat,
  chmod,
  copyFile,
} from "node:fs/promises";
import path from "node:path";
import { spawnSync } from "node:child_process";

const targets = {
  "win32:x64": {
    goos: "windows",
    goarch: "amd64",
    rust: "x86_64-pc-windows-msvc",
    vendor: "x86_64-pc-windows-msvc",
    suffix: "win32-x64",
    exe: ".exe",
  },
  "win32:arm64": {
    goos: "windows",
    goarch: "arm64",
    rust: "aarch64-pc-windows-msvc",
    vendor: "aarch64-pc-windows-msvc",
    suffix: "win32-arm64",
    exe: ".exe",
  },
  "darwin:arm64": {
    goos: "darwin",
    goarch: "arm64",
    rust: "aarch64-apple-darwin",
    vendor: "aarch64-apple-darwin",
    suffix: "darwin-arm64",
    exe: "",
  },
  "darwin:x64": {
    goos: "darwin",
    goarch: "amd64",
    rust: "x86_64-apple-darwin",
    vendor: "x86_64-apple-darwin",
    suffix: "darwin-x64",
    exe: "",
  },
  "linux:x64": {
    goos: "linux",
    goarch: "amd64",
    rust: "x86_64-unknown-linux-gnu",
    vendor: "x86_64-unknown-linux-musl",
    suffix: "linux-x64",
    exe: "",
  },
  "linux:arm64": {
    goos: "linux",
    goarch: "arm64",
    rust: "aarch64-unknown-linux-gnu",
    vendor: "aarch64-unknown-linux-musl",
    suffix: "linux-arm64",
    exe: "",
  },
};
const root = path.resolve(import.meta.dirname, "..");
const hostKey = `${process.platform}:${process.arch}`;
const options = new Map();
for (const argument of process.argv.slice(2)) {
  const match = /^(--target|--output)=(.+)$/.exec(argument);
  if (!match || options.has(match[1]))
    throw new Error(
      "Expected unique --target=platform:architecture and --output=.state/name options",
    );
  options.set(match[1], match[2]);
}
const targetKey = options.get("--target") ?? hostKey;
const target = targets[targetKey];
if (!target)
  throw new Error(
    "This target has no configured desktop development resources",
  );
if (targetKey !== hostKey && !options.has("--output"))
  throw new Error(
    "Cross-platform preparation requires a separate project-local output directory",
  );
const output = path.resolve(
  root,
  options.get("--output") ?? `.state/service-resources/${target.suffix}`,
);
if (options.has("--output")) {
  const relative = path.relative(path.join(root, ".state"), output);
  if (
    !relative ||
    path.isAbsolute(relative) ||
    relative === ".." ||
    relative.startsWith(`..${path.sep}`)
  )
    throw new Error(
      "An explicit development output must be a named directory inside .state",
    );
}
process.chdir(root);
const lock = JSON.parse(await readFile("engine.lock.json", "utf8"));
const version = lock.version;
if (!/^\d+\.\d+\.\d+$/.test(version))
  throw new Error("Invalid pinned engine version");
const packageVersion = `${version}-${target.suffix}`;
const metadataURL = `https://registry.npmjs.org/@openai/codex/${packageVersion}`;
const metadataResponse = await fetch(metadataURL, {
  signal: AbortSignal.timeout(30000),
});
if (!metadataResponse.ok)
  throw new Error(
    `Official engine package metadata failed: ${metadataResponse.status}`,
  );
const metadata = await metadataResponse.json();
const integrity = metadata.dist?.integrity;
if (
  metadata.name !== "@openai/codex" ||
  metadata.version !== packageVersion ||
  !/^sha512-[A-Za-z0-9+/=]+$/.test(integrity)
)
  throw new Error("Unexpected official engine package metadata");
const tarballURL = new URL(metadata.dist.tarball);
if (
  tarballURL.protocol !== "https:" ||
  tarballURL.hostname !== "registry.npmjs.org"
)
  throw new Error("Engine package URL is outside the official npm registry");
const archiveHash = Buffer.from(integrity.slice(7), "base64").toString("hex");
const cache = path.join(
  root,
  ".state",
  "engine-packages",
  `${packageVersion}-${archiveHash.slice(0, 16)}`,
);
await mkdir(cache, { recursive: true, mode: 0o700 });
const archive = path.join(cache, "package.tgz");
async function hash(file, algorithm = "sha256") {
  const digest = createHash(algorithm);
  for await (const block of createReadStream(file)) digest.update(block);
  return digest.digest("hex");
}
let present = false;
try {
  present = (await hash(archive, "sha512")) === archiveHash;
} catch (error) {
  if (error.code !== "ENOENT") throw error;
}
if (!present) {
  try {
    await lstat(archive);
    throw new Error("A different engine archive exists and was preserved");
  } catch (error) {
    if (error.code !== "ENOENT") throw error;
  }
  const response = await fetch(tarballURL, {
    signal: AbortSignal.timeout(60000),
  });
  if (!response.ok || !response.body)
    throw new Error("Official engine package download failed");
  const file = await open(archive, "wx", 0o600);
  const digest = createHash("sha512");
  let bytes = 0;
  try {
    for await (const block of response.body) {
      bytes += block.length;
      if (bytes > 256 * 1024 * 1024)
        throw new Error("Engine archive exceeds its size limit");
      digest.update(block);
      await file.write(block);
    }
    await file.sync();
  } finally {
    await file.close();
  }
  if (digest.digest("hex") !== archiveHash)
    throw new Error(
      "Downloaded engine package failed npm integrity verification",
    );
}
function run(program, args, options = {}) {
  const result = spawnSync(program, args, {
    cwd: root,
    encoding: "utf8",
    maxBuffer: 8 * 1024 * 1024,
    ...options,
  });
  if (result.status !== 0)
    throw new Error(
      `${program} failed: ${result.stderr?.slice(-1000) ?? result.error?.message ?? result.status}`,
    );
  return result.stdout;
}
const listing = run("tar", ["-tzf", archive]).trim().split(/\r?\n/);
if (
  listing.some(
    (name) =>
      !name.startsWith("package/") ||
      name.split("/").includes("..") ||
      name.includes("\\"),
  )
)
  throw new Error("Engine archive has an unsafe path");
const kinds = run("tar", ["-tvzf", archive]).split(/\r?\n/).filter(Boolean);
if (kinds.some((line) => !["-", "d"].includes(line[0])))
  throw new Error("Engine archive contains unsupported links or special files");
const extracted = path.join(cache, "extracted");
await mkdir(extracted, { recursive: true, mode: 0o700 });
run("tar", ["-xzf", archive, "-C", extracted]);
const vendor = path.join(extracted, "package", "vendor", target.vendor);
const resources = path.join(output, "engine");
await mkdir(resources, { recursive: true, mode: 0o700 });
const files = [];
async function collect(directory, prefix = "") {
  for (const entry of (await readdir(directory, { withFileTypes: true })).sort(
    (a, b) => a.name.localeCompare(b.name, "en"),
  )) {
    const relative = prefix ? `${prefix}/${entry.name}` : entry.name;
    const source = path.join(directory, entry.name);
    if (entry.isDirectory()) await collect(source, relative);
    else if (entry.isFile()) {
      const info = await lstat(source);
      files.push({
        path: relative,
        sha256: await hash(source),
        bytes: info.size,
        source,
      });
    } else
      throw new Error(
        "Engine runtime contains an unsupported filesystem entry",
      );
  }
}
await collect(vendor);
for (const name of (await readdir(path.join(extracted, "package"))).filter(
  (name) => /^(LICENSE|NOTICE)(\.|$)/i.test(name),
)) {
  const source = path.join(extracted, "package", name);
  const info = await lstat(source);
  if (info.isFile())
    files.push({
      path: name,
      sha256: await hash(source),
      bytes: info.size,
      source,
    });
}
if (!files.some((file) => file.path === `bin/codex${target.exe}`))
  throw new Error("Native engine entry point is missing");
if (
  target.goos === "windows" &&
  ![
    "codex-resources/codex-command-runner.exe",
    "codex-resources/codex-windows-sandbox-setup.exe",
    "bin/codex-code-mode-host.exe",
  ].every((name) => files.some((file) => file.path === name))
)
  throw new Error("Official Windows sandbox companion programs are missing");
if (!/^[a-f0-9]{40}$/.test(lock.sourceCommit))
  throw new Error("Pinned engine source revision is required for its notices");
for (const name of ["LICENSE", "NOTICE"]) {
  const source = path.join(cache, `upstream-${name}`);
  try {
    await lstat(source);
  } catch (error) {
    if (error.code !== "ENOENT") throw error;
    const response = await fetch(
      `https://raw.githubusercontent.com/openai/codex/${lock.sourceCommit}/${name}`,
      { signal: AbortSignal.timeout(30000) },
    );
    if (!response.ok)
      throw new Error(`Pinned upstream ${name} could not be read`);
    const text = await response.text();
    if (text.length > 256 * 1024)
      throw new Error("Unexpected upstream notice size");
    await writeFile(source, text, { flag: "wx", mode: 0o600 });
  }
  const info = await lstat(source);
  files.push({
    path: `licenses/codex-${name}`,
    sha256: await hash(source),
    bytes: info.size,
    source,
  });
}
const allowed = new Set([
  ...files.map((file) => file.path),
  "mewdex-engine.json",
]);
async function existing(directory, prefix = "") {
  for (const entry of await readdir(directory, { withFileTypes: true })) {
    const relative = prefix ? `${prefix}/${entry.name}` : entry.name;
    const actual = path.join(directory, entry.name);
    if (entry.isDirectory()) await existing(actual, relative);
    else if (!entry.isFile() || !allowed.has(relative))
      throw new Error("Unexpected existing engine resource was preserved");
  }
}
await existing(resources);
for (const file of files) {
  const destination = path.join(resources, ...file.path.split("/"));
  await mkdir(path.dirname(destination), { recursive: true, mode: 0o700 });
  try {
    if ((await hash(destination)) !== file.sha256)
      throw new Error("A different engine resource exists and was preserved");
  } catch (error) {
    if (error.code !== "ENOENT") throw error;
    await copyFile(file.source, destination);
  }
  if (process.platform !== "win32") await chmod(destination, 0o700);
}
const manifest = {
  format: 1,
  version,
  target: target.vendor,
  package: metadata.name,
  packageVersion,
  integrity,
  files: files
    .map(({ source, ...file }) => file)
    .sort((a, b) => a.path.localeCompare(b.path, "en")),
};
await writeFile(
  path.join(resources, "mewdex-engine.json"),
  JSON.stringify(manifest, null, 2) + "\n",
  { mode: 0o600 },
);
console.log(
  JSON.stringify({
    output,
    engineVersion: version,
    engineFiles: files.length,
    packageIntegrityVerified: true,
  }),
);
