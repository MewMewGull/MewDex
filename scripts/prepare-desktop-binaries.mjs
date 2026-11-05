// Build only the APP-owned message connector. Service resources are packaged separately.
import { mkdir, readFile, writeFile } from "node:fs/promises";
import { createHash } from "node:crypto";
import path from "node:path";
import { spawnSync } from "node:child_process";

const targets = {
  "win32:x64": ["windows", "amd64", "x86_64-pc-windows-msvc", ".exe"],
  "win32:arm64": ["windows", "arm64", "aarch64-pc-windows-msvc", ".exe"],
  "darwin:arm64": ["darwin", "arm64", "aarch64-apple-darwin", ""],
  "darwin:x64": ["darwin", "amd64", "x86_64-apple-darwin", ""],
  "linux:x64": ["linux", "amd64", "x86_64-unknown-linux-gnu", ""],
  "linux:arm64": ["linux", "arm64", "aarch64-unknown-linux-gnu", ""],
};
const root = path.resolve(import.meta.dirname, "..");
const options = new Map();
for (const argument of process.argv.slice(2)) {
  const match = /^(--target|--output)=(.+)$/.exec(argument);
  if (!match || options.has(match[1]))
    throw new Error("Expected unique --target and --output options");
  options.set(match[1], match[2]);
}
const host = `${process.platform}:${process.arch}`;
const targetKey = options.get("--target") ?? host;
const target = targets[targetKey];
if (!target) throw new Error("Unsupported APP target");
if (targetKey !== host && !options.has("--output"))
  throw new Error("Cross-target output must be explicit");
const output = path.resolve(
  root,
  options.get("--output") ?? "apps/client/src-tauri",
);
if (options.has("--output")) {
  const relative = path.relative(path.join(root, ".state"), output);
  if (!relative || relative.startsWith("..") || path.isAbsolute(relative))
    throw new Error("Explicit output must stay inside .state");
}
const [goos, goarch, rust, extension] = target;
const binaries = path.join(output, "binaries");
await mkdir(binaries, { recursive: true });
const helper = path.join(binaries, `mewdex-input-bridge-${rust}${extension}`);
const result = spawnSync(
  "go",
  ["build", "-trimpath", "-o", helper, "./cmd/mewdex-input-bridge"],
  {
    cwd: root,
    encoding: "utf8",
    windowsHide: true,
    env: { ...process.env, GOOS: goos, GOARCH: goarch, CGO_ENABLED: "0" },
  },
);
if (result.status !== 0)
  throw new Error(
    result.stderr || result.error?.message || "APP helper build failed",
  );
const report = {
  platform: rust,
  helper,
  helperSha256: createHash("sha256")
    .update(await readFile(helper))
    .digest("hex"),
  component: "app",
  finalCandidate: false,
};
await mkdir(path.join(root, ".state/evidence"), { recursive: true });
await writeFile(
  path.join(root, ".state/evidence/desktop-resources.json"),
  JSON.stringify(report, null, 2) + "\n",
);
console.log(JSON.stringify(report));
