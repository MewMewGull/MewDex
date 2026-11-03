import { copyFile, mkdir, readdir, rename, rm } from "node:fs/promises";
import path from "node:path";

const root = path.resolve(import.meta.dirname, "..");
const source = path.join(root, "apps/client/dist");
const destination = path.join(root, "internal/webui/static");
const generated = new Set([".keep"]);

// Copy assets first and publish the index last so it always references complete files.
async function copyDirectory(directory, prefix = "") {
  for (const entry of await readdir(directory, { withFileTypes: true })) {
    const name = path.posix.join(prefix, entry.name);
    if (name === "index.html") continue;
    const target = path.join(destination, name);
    if (entry.isDirectory()) {
      await mkdir(target, { recursive: true });
      await copyDirectory(path.join(directory, entry.name), name);
    } else if (entry.isFile()) {
      generated.add(name);
      await copyFile(path.join(directory, entry.name), target);
    } else {
      throw new Error("Web UI assets must be regular files");
    }
  }
}

await mkdir(destination, { recursive: true });
await copyDirectory(source);
const temporaryIndex = path.join(destination, "index.html.tmp");
await copyFile(path.join(source, "index.html"), temporaryIndex);
await rename(temporaryIndex, path.join(destination, "index.html"));
generated.add("index.html");

async function removeStale(directory, prefix = "") {
  for (const entry of await readdir(directory, { withFileTypes: true })) {
    const name = path.posix.join(prefix, entry.name);
    const target = path.resolve(directory, entry.name);
    const relative = path.relative(destination, target);
    if (!relative || relative.startsWith("..") || path.isAbsolute(relative))
      throw new Error(
        "Generated web assets must stay within their output directory",
      );
    if (entry.isDirectory()) await removeStale(target, name);
    else if (!generated.has(name)) await rm(target);
  }
}
await removeStale(destination);
console.log("Prepared shared web UI for the bundled Hub");
