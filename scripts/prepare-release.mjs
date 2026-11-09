import { createHash } from "node:crypto";
import { createReadStream } from "node:fs";
import { lstat, mkdir, readFile, realpath, writeFile } from "node:fs/promises";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { appVersionPattern } from "./set-app-version.mjs";

const repositoryRoot = path.resolve(import.meta.dirname, "..");
export const product = JSON.parse(
  await readFile(path.join(repositoryRoot, "mewdex.product.json"), "utf8"),
);

export async function prepareRelease(
  definition,
  { root = repositoryRoot, checkOnly = false } = {},
) {
  if (
    !definition.tag?.startsWith("v") ||
    !appVersionPattern.test(definition.tag.slice(1))
  )
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
    const distribution = artifact.distribution ?? "app";
    const distributionProfile = product.distributions[distribution];
    if (
      !distributionProfile ||
      (distribution !== "app" && profile?.family !== "desktop")
    )
      throw new Error("Unsupported distribution for this platform");
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
    const expectedComponents =
      distribution === "app"
        ? profile.components
        : distributionProfile.components;
    if (
      !Array.isArray(components) ||
      [...components].sort().join(",") !==
        [...expectedComponents].sort().join(",")
    )
      throw new Error(`Incorrect components for ${profile.name}`);
    if (distribution !== "app" && artifact.kind !== "portable")
      throw new Error(
        "Hub, web, executor and channel distributions use independent portable packages",
      );
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
      distribution,
      arch: artifact.arch,
      kind: artifact.kind,
      configuration: artifact.configuration,
      signed: artifact.signed,
      updateMode:
        distribution !== "app"
          ? "none"
          : artifact.os === "windows" && name.toLowerCase().endsWith(".exe")
            ? "windows-nsis"
            : "system",
      components: [...expectedComponents],
      file: path.relative(root, file).split(path.sep).join("/"),
      name,
      bytes: info.size,
      sha256: digest.digest("hex"),
      installation:
        artifact.installation ??
        (distribution === "app"
          ? profile.installation
          : `Deploy the independent ${distributionProfile.name} package using START-HERE.txt.`),
    });
  }
  for (const os of platforms) {
    const appRequired =
      !definition.prerelease ||
      assets.some((asset) => asset.os === os && asset.distribution === "app");
    if (
      appRequired &&
      !assets.some(
        (asset) =>
          asset.os === os &&
          asset.distribution === "app" &&
          asset.kind === "installer",
      )
    )
      throw new Error(
        `An installer is required for ${product.platforms[os].name}`,
      );
  }
  const notes = [
    `# MewDex ${definition.tag}`,
    "",
    definition.summary.trim(),
    "",
    "APP、网页输入端、Hub 和执行端分别发布。APP 与网页端共用输入与网络管理功能。",
    "桌面 APP 与独立网页输入服务均可保持消息渠道连接，Hub 管理模型与会话，执行端通过 WebSocket 连接 Hub 并提供独立网页控制面板。",
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
      const packages = assets.filter((asset) => asset.os === os);
      return `| ${profile.name} | ${packages.length ? packages.map((asset) => `${product.distributions[asset.distribution].name} [${asset.name}](https://github.com/${repository}/releases/download/${encodeURIComponent(definition.tag)}/${encodeURIComponent(asset.name)})`).join(" · ") : "待构建"} |`;
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
    version: definition.tag.slice(1),
    summary: definition.summary.trim(),
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
  const app = JSON.parse(
    await readFile(
      path.join(repositoryRoot, "apps/client/src-tauri/tauri.conf.json"),
      "utf8",
    ),
  );
  if (definition.tag !== "v" + app.version)
    throw new Error(
      "Release tag must match the built app version. Run npm run version:app before building.",
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
