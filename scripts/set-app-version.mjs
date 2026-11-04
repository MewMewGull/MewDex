import { readFile, writeFile } from "node:fs/promises";
import path from "node:path";
import { fileURLToPath } from "node:url";

const root = path.resolve(import.meta.dirname, "..");
const number = "(?:0|[1-9][0-9]*)";
const identifier = "(?:" + number + "|[0-9]*[A-Za-z-][0-9A-Za-z-]*)";
export const appVersionPattern = new RegExp(
  "^" +
    number +
    "\\." +
    number +
    "\\." +
    number +
    "(?:-" +
    identifier +
    "(?:\\." +
    identifier +
    ")*)?" +
    "(?:\\+[0-9A-Za-z-]+(?:\\.[0-9A-Za-z-]+)*)?(?![\\s\\S])",
);

export async function setAppVersion(version, directory = root) {
  if (!appVersionPattern.test(version ?? ""))
    throw new Error("Provide a valid SemVer app version");
  const changes = new Map();
  for (const name of [
    "package.json",
    "packages/sdk/package.json",
    "apps/client/package.json",
    "apps/client/src-tauri/tauri.conf.json",
  ]) {
    const value = JSON.parse(
      await readFile(path.join(directory, name), "utf8"),
    );
    value.version = version;
    if (value.dependencies?.["@mewdex/sdk"])
      value.dependencies["@mewdex/sdk"] = version;
    changes.set(name, JSON.stringify(value, null, 2) + "\n");
  }
  const lock = JSON.parse(
    await readFile(path.join(directory, "package-lock.json"), "utf8"),
  );
  lock.version = version;
  for (const name of ["", "apps/client", "packages/sdk"]) {
    lock.packages[name].version = version;
    if (lock.packages[name].dependencies?.["@mewdex/sdk"])
      lock.packages[name].dependencies["@mewdex/sdk"] = version;
  }
  changes.set("package-lock.json", JSON.stringify(lock, null, 2) + "\n");
  for (const [name, pattern] of [
    [
      "apps/client/src-tauri/Cargo.toml",
      /(\[package\]\r?\nname = "mewdex-client"\r?\nversion = ")[^"]+(")/,
    ],
    [
      "apps/client/src-tauri/Cargo.lock",
      /(\[\[package\]\]\r?\nname = "mewdex-client"\r?\nversion = ")[^"]+(")/,
    ],
  ]) {
    const value = await readFile(path.join(directory, name), "utf8");
    if (!pattern.test(value))
      throw new Error("Cannot locate the app version in " + name);
    changes.set(
      name,
      value.replace(pattern, (_, before, after) => before + version + after),
    );
  }
  // Read and validate every input first. Re-running with the same version is safe.
  for (const [name, value] of changes)
    await writeFile(path.join(directory, name), value);
}

if (
  process.argv[1] &&
  path.resolve(process.argv[1]) === fileURLToPath(import.meta.url)
) {
  if (process.argv.length !== 3)
    throw new Error("Usage: node scripts/set-app-version.mjs <version>");
  await setAppVersion(process.argv[2]);
  console.log("App version set to " + process.argv[2]);
}
