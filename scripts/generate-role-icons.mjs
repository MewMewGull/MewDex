// Regenerate native role icons from the shared vector sources.
import { createRequire } from "node:module";
import { spawnSync } from "node:child_process";
import { cp, copyFile, mkdir, mkdtemp, readdir } from "node:fs/promises";
import path from "node:path";

const root = path.resolve(import.meta.dirname, "..");
const require = createRequire(import.meta.url);
const cli = path.join(
  path.dirname(require.resolve("@tauri-apps/cli/package.json")),
  "tauri.js",
);
const scratch = await mkdtemp(path.join(root, ".state/role-icons-"));
const tray = path.join(root, "internal/servicetray/icons");
const app = path.join(root, "apps/client/src-tauri/icons");
await mkdir(tray, { recursive: true });
await mkdir(path.join(root, "apps/client/src/assets"), { recursive: true });
for (const role of ["input", "executor", "hub"]) {
  const source = path.join(root, "assets/icons", `${role}.svg`);
  const output = path.join(scratch, role);
  const result = spawnSync(
    process.execPath,
    [cli, "icon", source, "--output", output],
    {
      cwd: root,
      encoding: "utf8",
      windowsHide: true,
    },
  );
  if (result.status !== 0)
    throw new Error(result.stderr || "Role icon generation failed");
  for (const extension of ["ico", "icns"])
    await copyFile(
      path.join(output, `icon.${extension}`),
      path.join(tray, `${role}.${extension}`),
    );
  await copyFile(
    path.join(output, "64x64.png"),
    path.join(tray, `${role}.png`),
  );
  if (role === "input") {
    await cp(path.join(output, "android"), path.join(app, "android"), {
      recursive: true,
    });
    for (const entry of await readdir(output, { withFileTypes: true }))
      if (entry.isFile())
        await copyFile(
          path.join(output, entry.name),
          path.join(app, entry.name),
        );
    await copyFile(source, path.join(app, "source.svg"));
    await copyFile(
      source,
      path.join(root, "apps/client/src/assets/input-icon.svg"),
    );
  }
}
console.log(
  "Generated input, executor and Hub icons for desktop and tray use.",
);
