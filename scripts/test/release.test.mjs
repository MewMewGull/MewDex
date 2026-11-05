import { test } from "node:test";
import assert from "node:assert/strict";
import { mkdtemp, mkdir, readFile, rm, writeFile } from "node:fs/promises";
import os from "node:os";
import path from "node:path";
import { prepareRelease, product } from "../prepare-release.mjs";
import { setAppVersion, appVersionPattern } from "../set-app-version.mjs";

async function workspace(t) {
  const root = await mkdtemp(path.join(os.tmpdir(), "mewdex-release-"));
  const relative = path.relative(os.tmpdir(), root);
  if (!relative.startsWith("mewdex-release-") || relative.includes(path.sep))
    throw new Error("Unexpected release test directory");
  t.after(() => rm(root, { recursive: true, force: true }));
  await mkdir(path.join(root, "dist"));
  return root;
}

function artifact(osName = "windows") {
  const profile = product.platforms[osName];
  return {
    os: osName,
    arch: profile.architectures.at(-1),
    kind: "installer",
    configuration: "Release",
    signed: true,
    components: [...profile.components],
    file: `dist/MewDex-${osName}${profile.installerExtensions[0]}`,
    installation: "Follow the signed distribution instructions.",
  };
}

function definition(artifacts) {
  return {
    tag: "v0.1.0-preview.20261104",
    summary: "Component packaging check",
    prerelease: true,
    platforms: [...new Set(artifacts.map((asset) => asset.os))],
    artifacts,
  };
}

test("release notes identify all four components and provide installer links and checksums", async (t) => {
  const root = await workspace(t);
  const asset = artifact();
  await writeFile(path.join(root, asset.file), "owned installer fixture");
  const result = await prepareRelease(definition([asset]), { root });
  for (const component of Object.values(product.components))
    assert.ok(result.notes.includes(component.name));
  assert.match(
    result.notes,
    /releases\/download\/v0.1.0-preview.20261104\/MewDex-windows.exe/,
  );
  assert.equal(result.manifest.assets[0].sha256.length, 64);
  assert.equal(result.manifest.assets[0].updateMode, "windows-nsis");
  assert.equal(result.manifest.version, "0.1.0-preview.20261104");
  assert.equal(result.manifest.summary, "Component packaging check");
  assert.match(
    await readFile(path.join(result.output, "SHA256SUMS"), "utf8"),
    /MewDex-windows.exe/,
  );
});

test("app versions use SemVer and stay aligned across native, UI and workspace locks", async (t) => {
  for (const version of ["1.2.3", "1.2.3-preview.1", "1.2.3+build"])
    assert.ok(appVersionPattern.test(version));
  for (const version of [
    "1.2.3\n",
    "1.02.3",
    "1.2.3-preview.01",
    "v1.2.3",
    "1.2",
  ])
    assert.equal(appVersionPattern.test(version), false, version);
  const root = await workspace(t);
  const initial = {
    name: "mewdex",
    version: "0.1.0",
    dependencies: { "@mewdex/sdk": "0.1.0" },
  };
  for (const name of [
    "package.json",
    "packages/sdk/package.json",
    "apps/client/package.json",
    "apps/client/src-tauri/tauri.conf.json",
  ]) {
    await mkdir(path.dirname(path.join(root, name)), { recursive: true });
    await writeFile(path.join(root, name), JSON.stringify(initial));
  }
  await writeFile(
    path.join(root, "package-lock.json"),
    JSON.stringify({
      version: "0.1.0",
      packages: Object.fromEntries(
        ["", "apps/client", "packages/sdk"].map((name) => [name, initial]),
      ),
    }),
  );
  await writeFile(
    path.join(root, "apps/client/src-tauri/Cargo.toml"),
    '[package]\nname = "mewdex-client"\nversion = "0.1.0"\n',
  );
  await writeFile(
    path.join(root, "apps/client/src-tauri/Cargo.lock"),
    '[[package]]\nname = "mewdex-client"\nversion = "0.1.0"\n',
  );
  await setAppVersion("1.2.3-preview.1", root);
  await setAppVersion("1.2.3-preview.1", root);
  assert.equal(
    JSON.parse(
      await readFile(path.join(root, "apps/client/package.json"), "utf8"),
    ).dependencies["@mewdex/sdk"],
    "1.2.3-preview.1",
  );
  assert.match(
    await readFile(path.join(root, "apps/client/src-tauri/Cargo.lock"), "utf8"),
    /version = "1.2.3-preview.1"/,
  );
  const lock = JSON.parse(
    await readFile(path.join(root, "package-lock.json"), "utf8"),
  );
  assert.equal(
    lock.packages["apps/client"].dependencies["@mewdex/sdk"],
    "1.2.3-preview.1",
  );
  await assert.rejects(setAppVersion("1.2", root), /valid SemVer/);
});

test("a portable ZIP cannot replace the required OS installer", async (t) => {
  const root = await workspace(t);
  const asset = {
    ...artifact(),
    kind: "portable",
    file: "dist/MewDex-Windows.zip",
  };
  await writeFile(path.join(root, asset.file), "owned archive fixture");
  await assert.rejects(
    prepareRelease(definition([asset]), { root }),
    /installer is required/,
  );
});

test("service distributions publish independently and keep their component boundaries", async (t) => {
  const root = await workspace(t);
  const asset = {
    ...artifact(),
    distribution: "hub",
    kind: "portable",
    components: ["hub"],
    file: "dist/MewDex-Hub-Windows.zip",
  };
  await writeFile(path.join(root, asset.file), "owned Hub package fixture");
  const result = await prepareRelease(definition([asset]), { root });
  assert.equal(result.manifest.assets[0].distribution, "hub");
  assert.equal(result.manifest.assets[0].updateMode, "none");
  await assert.rejects(
    prepareRelease(
      definition([
        { ...asset, components: product.platforms.windows.components },
      ]),
      { root },
    ),
    /Incorrect components/,
  );
});

test("mobile packages require exactly Input Terminal and Hub Controller", async (t) => {
  const root = await workspace(t);
  const asset = artifact("android");
  asset.components.push("execute-terminal");
  await assert.rejects(
    prepareRelease(definition([asset]), { root }),
    /Incorrect components/,
  );
});

test("a stable release requires an installer for every supported OS", async (t) => {
  const root = await workspace(t);
  await assert.rejects(
    prepareRelease(
      { ...definition([artifact()]), prerelease: false },
      { root },
    ),
    /stable release requires/,
  );
  const assets = Object.keys(product.platforms).map(artifact);
  for (const asset of assets)
    await writeFile(
      path.join(root, asset.file),
      "owned platform installer fixture",
    );
  const result = await prepareRelease(
    { ...definition(assets), tag: "v0.1.0", prerelease: false },
    { root, checkOnly: true },
  );
  assert.equal(result.manifest.assets.length, 5);
});

test("an iOS release requires a signed device package and distribution instructions", async (t) => {
  const root = await workspace(t);
  await assert.rejects(
    prepareRelease(definition([{ ...artifact("ios"), signed: false }]), {
      root,
    }),
    /signed IPA/,
  );
  await assert.rejects(
    prepareRelease(definition([{ ...artifact("ios"), installation: "" }]), {
      root,
    }),
    /installation instructions/,
  );
});
