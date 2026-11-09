// Export independent Hub, web input, executor and channel connector packages.
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
if (!["all", "hub", "executor", "web", "channel"].includes(selected))
  throw new Error("Select hub, executor, web, channel or all");
const components =
  selected === "all" ? ["hub", "executor", "web", "channel"] : [selected];
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
const nativeSandbox =
  JSON.parse(
    await readFile(path.join(root, "tools/sandbox.lock.json"), "utf8"),
  ).supportedTargets.includes(targetKey) && components.includes("executor");
const sandboxResources = path.join(
  root,
  ".state/sandbox-resources",
  target.label,
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
    "docs/extensions.md",
    "docs/licensing.md",
    "docs/evaluations.md",
    "docs/evaluations/core.example.json",
    "docs/evaluations/ordered.example.json",
    "docs/linux-sandbox.md",
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
if (
  components.some(
    (component) =>
      component === "hub" || (component === "executor" && !nativeSandbox),
  )
) {
  run(process.execPath, [
    path.join(root, "scripts/prepare-service-resources.mjs"),
    `--target=${targetKey}`,
    `--output=${path.relative(root, resources)}`,
  ]);
}
if (nativeSandbox) {
  run("python", [
    path.join(root, "scripts/prepare-sandbox-resources.py"),
    `--target=${targetKey}`,
    `--output=${path.relative(root, sandboxResources)}`,
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
    const title = {
      hub: "Hub",
      web: "Web",
      executor: "Executor",
      channel: "Channel",
    }[component];
    const name = `MewDex-${title}-${target.label}`;
    const stage = path.join(scratch, name);
    await mkdir(stage);
    const binary = `mewdex-${component}${target.extension}`;
    const iconRole =
      component === "web" || component === "channel" ? "input" : component;
    const goPackage =
      component === "channel"
        ? "./cmd/mewdex-input-bridge"
        : component === "web"
          ? "./cmd/mewdex-web"
          : "./cmd/mewdex";
    run(
      "go",
      [
        "build",
        "-trimpath",
        `-ldflags=${[
          ...(target.os === "windows" && component !== "channel"
            ? ["-H=windowsgui"]
            : []),
          ...(component === "web" || component === "channel"
            ? []
            : [`-X main.distributionRole=${component}`]),
        ].join(" ")}`,
        "-o",
        path.join(stage, binary),
        goPackage,
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
    if (target.os === "windows") {
      const iconBuild = path.join(scratch, `icon-${component}`);
      await mkdir(iconBuild);
      await copy(
        path.join(root, "internal/servicetray/icons", `${iconRole}.ico`),
        path.join(iconBuild, "icon.ico"),
      );
      const definition = path.join(iconBuild, "winres.json");
      await writeFile(
        definition,
        JSON.stringify({ RT_GROUP_ICON: { APP: { "0000": "icon.ico" } } }),
      );
      run("go", [
        "run",
        "github.com/tc-hib/go-winres@v0.3.3",
        "patch",
        "--in",
        definition,
        "--no-backup",
        path.join(stage, binary),
      ]);
    }
    if (component === "web")
      await copy(path.join(root, "apps/client/dist"), path.join(stage, "ui"));
    else if (component === "executor" && nativeSandbox)
      await copy(
        path.join(sandboxResources, "sandbox"),
        path.join(stage, "sandbox"),
      );
    else if (component !== "channel")
      await copy(path.join(resources, "engine"), path.join(stage, "engine"));
    if (target.os === "darwin" && component !== "channel") {
      const bundle = path.join(stage, `MewDex ${title}.app`, "Contents");
      await mkdir(path.join(bundle, "MacOS"), { recursive: true });
      await mkdir(path.join(bundle, "Resources"));
      await copy(
        path.join(root, "internal/servicetray/icons", `${iconRole}.icns`),
        path.join(bundle, "Resources/icon.icns"),
      );
      await writeFile(
        path.join(bundle, "Info.plist"),
        `<?xml version="1.0" encoding="UTF-8"?><plist version="1.0"><dict><key>CFBundleExecutable</key><string>launch</string><key>CFBundleIdentifier</key><string>app.mewdex.${component}</string><key>CFBundleName</key><string>MewDex ${title}</string><key>CFBundleIconFile</key><string>icon.icns</string><key>CFBundlePackageType</key><string>APPL</string><key>CFBundleShortVersionString</key><string>${version}</string><key>LSUIElement</key><true/></dict></plist>\n`,
      );
      await writeFile(
        path.join(bundle, "MacOS/launch"),
        `#!/bin/sh\nset -eu\ncomponent_dir=$(CDPATH= cd -- "$(dirname -- "$0")/../../.." && pwd -P)\nexec "$component_dir/${binary}"\n`,
        { mode: 0o755 },
      );
    }
    await notices(stage, component === "web");
    if (component === "channel" || component === "hub")
      await copy(
        path.join(root, "docs/messaging.md"),
        path.join(stage, "docs/messaging.md"),
      );
    const instructions = {
      hub: `双击 ${target.os === "darwin" ? "MewDex Hub.app" : binary} 打开本机控制面板，创建或启动 Hub。运行后从系统托盘查看状态、控制面板和开机自启动。\n\nHub 自带常驻消息连接器。在 APP 或网页的设置 → 消息渠道中选择“Hub 常驻连接器”，配置微信、QQ 或飞书。渠道随 Hub 服务保持连接，关闭 APP 或网页后继续收发消息。原输入端的渠道配置保留，迁移步骤见 docs/messaging.md。\n\n命令行部署\n${binary} init -config .local/hub/device.json\n${binary} account -config .local/hub/device.json\n${binary} serve -config .local/hub/device.json\naccount 命令从标准输入读取账号 JSON。按部署设置 listen 和 allowedOrigins。\n`,
      executor: `双击 ${target.os === "darwin" ? "MewDex Executor.app" : binary} 打开本机控制面板。填写 Hub 地址和执行端配对码，添加本机目录，再启动执行端。运行后从系统托盘查看状态、控制面板和开机自启动。\n\n指定已有配置目录\n${binary} panel -root .local/executor -open\n此节点通过 WebSocket 连接 Hub。\n`,
      web: `双击 ${target.os === "darwin" ? "MewDex Web.app" : binary}，填写 Hub 地址后进入工作台。运行后托盘显示“网页输入端”，可打开工作台、调整 Hub 地址、设置开机自启动或退出。\n\nAPP 与网页版任选其一。消息渠道默认由 Hub 常驻连接器保持连接。网页版保留原有输入端连接器以兼容已有配置。\n\n命令行部署\n${binary} -hub-url https://your-hub.example.com -listen 127.0.0.1:47831\n\n在 Hub 的 allowedOrigins 中允许网页入口。ui 目录随独立网页包部署。Windows 后台组件不创建控制台窗口。\n`,
      channel: `独立消息连接器，直接连接 Hub。可部署在能访问微信、QQ 或飞书的平台主机上。\n\n首次授权\n${binary} -setup -hub-url https://your-hub.example.com -config .local/channel/connection.json\n从标准输入提供 MewDex 管理员账号 JSON，例如 {"username":"your-name","password":"your-password"}。密码只用于登录，配置文件保存该账号签发的连接凭据。\n\n运行\n${binary} -config .local/channel/connection.json\n此模式独立于 APP、网页和标准输入生命周期。使用系统服务管理器常驻和开机启动，重启后读取原配置与渠道数据库。\n\n在 APP 或网页的设置 → 消息渠道中选择“独立消息连接器”并配置平台。凭据撤销后服务结束，重新授权后启动。详见 docs/messaging.md。\n`,
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
          engineVersion:
            component === "web" ||
            component === "channel" ||
            (component === "executor" && nativeSandbox)
              ? undefined
              : engineVersion,
          sandboxVersion:
            component === "executor" && nativeSandbox
              ? JSON.parse(
                  await readFile(
                    path.join(root, "tools/sandbox.lock.json"),
                    "utf8",
                  ),
                ).runtimeVersion
              : undefined,
        },
        null,
        2,
      ) + "\n",
    );
    const archive = path.join(scratch, `${name}.zip`);
    run(process.platform === "win32" ? "python" : "python3", [
      path.join(root, "scripts/archive-component.py"),
      stage,
      archive,
      platformDirectory,
      binary,
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
