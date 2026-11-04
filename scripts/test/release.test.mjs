import { test } from "node:test";
import assert from "node:assert/strict";
import { mkdtemp, mkdir, readFile, rm, writeFile } from "node:fs/promises";
import os from "node:os";
import path from "node:path";
import { prepareRelease, product } from "../prepare-release.mjs";

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
  assert.match(
    await readFile(path.join(result.output, "SHA256SUMS"), "utf8"),
    /MewDex-windows.exe/,
  );
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
