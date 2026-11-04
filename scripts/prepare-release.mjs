import { createHash } from "node:crypto";
import { createReadStream } from "node:fs";
import { lstat, mkdir, readFile, realpath, writeFile } from "node:fs/promises";
import path from "node:path";
import { fileURLToPath } from "node:url";

const repositoryRoot = path.resolve(import.meta.dirname, "..");
export const product = JSON.parse(
  await readFile(path.join(repositoryRoot, "mewdex.product.json"), "utf8"),
);

export async function prepareRelease(
  definition,
  { root = repositoryRoot, checkOnly = false } = {},
) {
  if (!/^v\d+\.\d+\.\d+(?:[-.][A-Za-z0-9.-]+)?$/.test(definition.tag ?? ""))
    throw new Error(
      "Provide a version tag such as v0.1.0 or v0.1.0-preview.20261104",
    );
  if (
    typeof definition.prerelease !== "boolean" ||
    typeof definition.summary !== "string" ||
    !definition.summary.trim()
  )
    throw new Error("Declare prerelease and provide a release summary");
  const platforms = definition.platforms;
  if (
    !Array.isArray(platforms) ||
    !platforms.length ||
    new Set(platforms).size !== platforms.length ||
    platforms.some((os) => !product.platforms[os])
  )
    throw new Error("Declare the release platforms without duplicates");
  if (
    !definition.prerelease &&
    Object.keys(product.platforms).some((os) => !platforms.includes(os))
  )
    throw new Error(
      "A stable release requires Windows, macOS, Linux, Android and iOS installers",
    );
  if (!Array.isArray(definition.artifacts))
    throw new Error("Provide release artifacts");
  const repository = definition.repository ?? "MewMewGull/MewDex";
  if (!/^[A-Za-z0-9_.-]+\/[A-Za-z0-9_.-]+$/.test(repository))
    throw new Error("Invalid release repository");
  const dist = await realpath(path.join(root, "dist"));
  const assets = [];
  const names = new Set();
  for (const artifact of definition.artifacts) {
    const profile = product.platforms[artifact.os];
    if (
      !profile ||
      !platforms.includes(artifact.os) ||
      !profile.architectures.includes(artifact.arch)
    )
      throw new Error(
        "Artifact OS or architecture does not match a declared platform",
      );
    if (
      !["installer", "portable"].includes(artifact.kind) ||
      !["Debug", "Release"].includes(artifact.configuration) ||
      typeof artifact.signed !== "boolean"
    )
      throw new Error(
        "Declare artifact kind, configuration and signing status",
      );
    if (!definition.prerelease && artifact.configuration !== "Release")
      throw new Error(
        "Stable release installers must use Release configuration",
      );
    const components = artifact.components;
    if (
      !Array.isArray(components) ||
      [...components].sort().join(",") !==
        [...profile.components].sort().join(",")
    )
      throw new Error(`Incorrect components for ${profile.name}`);
    if (typeof artifact.file !== "string")
      throw new Error("Provide each artifact file path");
    const name = path.basename(artifact.file);
    const extensions =
      artifact.kind === "installer"
        ? profile.installerExtensions
        : [".zip", ".tar.gz"];
    if (
      !extensions.some((extension) =>
        name.toLowerCase().endsWith(extension.toLowerCase()),
      )
    )
      throw new Error(
        `An ${artifact.kind} for ${profile.name} requires ${extensions.join(" or ")}`,
      );
    if (profile.family === "mobile" && artifact.kind !== "installer")
      throw new Error("Mobile release artifacts must be installers");
    if (
      artifact.os === "ios" &&
      (!artifact.signed || !artifact.installation?.trim())
    )
      throw new Error(
        "iOS requires a signed IPA and installation instructions for its distribution method",
      );
    if (names.has(name)) throw new Error("Release asset names must be unique");
    names.add(name);
    const file = path.resolve(root, artifact.file);
    const info = await lstat(file);
    const actual = await realpath(file);
    const relative = path.relative(dist, actual);
    if (
      !info.isFile() ||
      !info.size ||
      relative.startsWith("..") ||
      path.isAbsolute(relative)
    )
      throw new Error(
        "Release assets must be nonempty regular files inside dist",
      );
    const digest = createHash("sha256");
    for await (const chunk of createReadStream(file)) digest.update(chunk);
    assets.push({
      os: artifact.os,
      arch: artifact.arch,
      kind: artifact.kind,
      configuration: artifact.configuration,
      signed: artifact.signed,
      components: [...profile.components],
      file: path.relative(root, file).split(path.sep).join("/"),
      name,
      bytes: info.size,
      sha256: digest.digest("hex"),
      installation: artifact.installation ?? profile.installation,
    });
  }
  for (const os of platforms) {
    if (!assets.some((asset) => asset.os === os && asset.kind === "installer"))
      throw new Error(
        `An installer is required for ${product.platforms[os].name}`,
      );
  }
  const notes = [
    `# MewDex ${definition.tag}`,
    "",
    definition.summary.trim(),
    "",
    "桌面 App 包含 Input Terminal、Execute Terminal、Hub 和 Hub Controller。本机执行与 Hub 角色由用户配置。",
    "移动 App 包含 Input Terminal 和 Hub Controller，通过已授权的 Hub 使用并管理远程终端。",
    "",
    "| 组件 | 职责 |",
    "| --- | --- |",
    ...Object.values(product.components).map(
      (component) =>
        `| ${component.name}（${component.label}） | ${component.purpose} |`,
    ),
    "",
    "## 平台与安装包",
    "",
    "| 系统 | 本次提供的安装包 |",
    "| --- | --- |",
    ...Object.entries(product.platforms).map(([os, profile]) => {
      const installers = assets.filter(
        (asset) => asset.os === os && asset.kind === "installer",
      );
      return `| ${profile.name} | ${installers.length ? installers.map((asset) => `[${asset.name}](https://github.com/${repository}/releases/download/${encodeURIComponent(definition.tag)}/${encodeURIComponent(asset.name)})`).join(" · ") : "待构建"} |`;
    }),
    "",
    "## 安装与构建信息",
    "",
    ...assets.map(
      (asset) =>
        `- ${product.platforms[asset.os].name} ${asset.arch}，${asset.configuration}，${asset.signed ? "已签名" : "无发行签名"}。${asset.installation}`,
    ),
    "",
    "## SHA-256",
    "",
    "```text",
    ...assets.map((asset) => `${asset.sha256}  ${asset.name}`),
    "```",
    "",
  ].join("\n");
  const manifest = {
    schemaVersion: 1,
    tag: definition.tag,
    prerelease: definition.prerelease,
    platforms,
    assets,
  };
  const output = path.join(root, "dist", "releases", definition.tag);
  if (!checkOnly) {
    await mkdir(output, { recursive: true });
    await writeFile(
      path.join(output, "release-manifest.json"),
      JSON.stringify(manifest, null, 2) + "\n",
    );
    await writeFile(path.join(output, "release-notes.md"), notes);
    await writeFile(
      path.join(output, "SHA256SUMS"),
      assets.map((asset) => `${asset.sha256}  ${asset.name}\n`).join(""),
    );
  }
  return { manifest, notes, output };
}

async function main() {
  const [definitionPath, option] = process.argv.slice(2);
  if (
    !definitionPath ||
    (option && option !== "--check") ||
    process.argv.length > 4
  )
    throw new Error("Usage: npm run release:prepare -- release.json [--check]");
  const definition = JSON.parse(
    await readFile(path.resolve(definitionPath), "utf8"),
  );
  const result = await prepareRelease(definition, {
    checkOnly: option === "--check",
  });
  console.log(
    JSON.stringify({
      tag: result.manifest.tag,
      platforms: result.manifest.platforms,
      assets: result.manifest.assets.length,
      output: result.output,
      checkedOnly: option === "--check",
    }),
  );
}

if (
  process.argv[1] &&
  path.resolve(process.argv[1]) === fileURLToPath(import.meta.url)
) {
  main().catch((error) => {
    console.error(error.message);
    process.exitCode = 1;
  });
}
