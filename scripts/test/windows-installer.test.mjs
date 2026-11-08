import { test } from "node:test";
import assert from "node:assert/strict";
import { spawn, spawnSync } from "node:child_process";
import {
  access,
  copyFile,
  mkdir,
  mkdtemp,
  readFile,
  readdir,
  rm,
  writeFile,
} from "node:fs/promises";
import path from "node:path";

const root = path.resolve(import.meta.dirname, "../..");
const state = path.join(root, ".state");
const nsis = path.join(root, ".local/toolchains/nsis-3.13/makensis.exe");
const compiler = path.join(
  process.env.WINDIR ?? "C:\\Windows",
  "Microsoft.NET/Framework64/v4.0.30319/csc.exe",
);

test("Windows installer includes every document shipped by the APP archive", async () => {
  const exporter = await readFile(
    path.join(root, "scripts/export-windows-development.py"),
    "utf8",
  );
  const installer = await readFile(
    path.join(root, "scripts/build-windows-installer.mjs"),
    "utf8",
  );
  const documents = [...exporter.matchAll(/"(docs\/[^"]+)"/g)].map(
    (match) => match[1],
  );
  assert.ok(documents.length > 0);
  for (const document of documents) {
    assert.ok(installer.includes(`'${document}'`), document);
  }
});

function run(executable, args) {
  const result = spawnSync(executable, args, {
    encoding: "utf8",
    windowsHide: true,
    timeout: 60000,
  });
  assert.equal(
    result.status,
    0,
    result.stderr || result.stdout || result.error?.message,
  );
}

const windowSource = `
using System;
using System.IO;
using System.Windows.Forms;

class ShutdownWindow : Form {
    readonly string marker;
    public ShutdownWindow(string value) { marker = value; }
    protected override void WndProc(ref Message message) {
        if (message.Msg == 0x11) {
            message.Result = new IntPtr(1);
            return;
        }
        if (message.Msg == 0x16 && message.WParam != IntPtr.Zero) {
            File.WriteAllText(marker + ".closed", "restart-manager");
            Application.ExitThread();
            message.Result = IntPtr.Zero;
            return;
        }
        base.WndProc(ref message);
    }
    [STAThread]
    static void Main(string[] args) {
        File.WriteAllText(Path.Combine(AppDomain.CurrentDomain.BaseDirectory,
            "launch-" + System.Diagnostics.Process.GetCurrentProcess().Id + ".log"), "launch");
        if (args.Length == 0) return;
        using (var window = new ShutdownWindow(args[0])) {
            var handle = window.Handle;
            File.WriteAllText(args[0] + ".ready", "ready");
            Application.Run();
        }
    }
}
`;

function nsisString(value) {
  return value.replaceAll("$", "$$").replaceAll('"', '$\\"');
}

async function runningFixture(directory, fixture) {
  await mkdir(directory, { recursive: true });
  const executable = path.join(directory, "mewdex-client.exe");
  const marker = path.join(directory, "process");
  await copyFile(fixture, executable);
  const child = spawn(executable, [marker], { windowsHide: true });
  const exited = new Promise((resolve, reject) => {
    child.once("error", reject);
    child.once("exit", (code, signal) => resolve({ code, signal }));
  });
  for (let attempt = 0; attempt < 100; attempt++) {
    try {
      await access(marker + ".ready");
      return { child, exited, executable, marker };
    } catch {
      await new Promise((resolve) => setTimeout(resolve, 20));
    }
  }
  child.kill();
  await exited;
  throw new Error("The hidden installer fixture did not start");
}

test(
  "Windows upgrade confirms shutdown before extracting any application files",
  { skip: process.platform !== "win32", timeout: 120000 },
  async (t) => {
    try {
      await Promise.all([access(nsis), access(compiler)]);
    } catch {
      t.skip(
        "Requires the prepared NSIS toolchain and Windows fixture compiler",
      );
      return;
    }
    await mkdir(state, { recursive: true });
    const work = await mkdtemp(path.join(state, "installer-upgrade-test-"));
    const relative = path.relative(state, work);
    assert.ok(
      relative.startsWith("installer-upgrade-test-") &&
        !relative.includes(path.sep) &&
        !path.isAbsolute(relative),
    );
    const children = [];
    t.after(async () => {
      for (const fixture of children) {
        if (fixture.child.exitCode === null) fixture.child.kill();
        await fixture.exited;
      }
      await rm(work, { recursive: true, force: true });
    });

    const source = path.join(work, "ShutdownWindow.cs");
    const fixture = path.join(work, "fixture.exe");
    await writeFile(source, windowSource);
    run(compiler, [
      "/nologo",
      "/target:winexe",
      "/reference:System.Windows.Forms.dll",
      "/out:" + fixture,
      source,
    ]);
    const include = await readFile(
      path.join(root, "scripts/windows/workbench-upgrade.nsh"),
      "utf8",
    );
    assert.match(include, /MB_YESNO\|MB_ICONQUESTION\|MB_DEFBUTTON2/);
    assert.equal(include.split("/SD IDNO IDYES close_confirmed").length, 2);
    const replacement = "new application fixture";
    const payload = path.join(work, "replacement.bin");
    await writeFile(payload, replacement);

    async function installer(name, approve = false) {
      const script = path.join(work, name + ".nsi");
      const output = path.join(work, name + ".exe");
      const header = path.join(work, name + ".nsh");
      // Only the fixture's silent answer changes to model an explicit Yes.
      // The distributed installer always defaults to No, including /S.
      await writeFile(
        header,
        approve
          ? include.replace(
              "/SD IDNO IDYES close_confirmed",
              "/SD IDYES IDYES close_confirmed",
            )
          : include,
      );
      await writeFile(
        script,
        `Unicode true
RequestExecutionLevel user
SilentInstall silent
AutoCloseWindow true
OutFile "${nsisString(output)}"
Var UpdatePID
!include "${nsisString(header)}"
Section
  Call CloseInstalledWorkbench
  SetOutPath "$INSTDIR"
  File /oname=mewdex-client.exe "${nsisString(payload)}"
SectionEnd
`,
      );
      run(nsis, ["/INPUTCHARSET", "UTF8", "/V2", script]);
      return output;
    }

    const decline = await installer("decline");
    const approve = await installer("approve", true);

    await t.test(
      "declining keeps the old process and installation intact",
      async () => {
        const directory = path.join(work, "拒绝升级 · existing");
        const running = await runningFixture(directory, fixture);
        children.push(running);
        const before = await readFile(running.executable);
        const result = spawnSync(decline, ["/S", "/D=" + directory], {
          windowsHide: true,
          windowsVerbatimArguments: true,
          timeout: 30000,
        });
        assert.equal(result.status, 1223);
        assert.equal(running.child.exitCode, null);
        assert.deepEqual(await readFile(running.executable), before);
        await assert.rejects(access(running.marker + ".closed"));
      },
    );

    await t.test(
      "confirmation closes only the target installation before replacement",
      async () => {
        const directory = path.join(work, "确认升级 · target");
        const running = await runningFixture(directory, fixture);
        const other = await runningFixture(
          path.join(work, "other-install"),
          fixture,
        );
        children.push(running, other);
        const result = spawnSync(approve, ["/S", "/D=" + directory], {
          windowsHide: true,
          windowsVerbatimArguments: true,
          timeout: 60000,
        });
        assert.equal(result.status, 0, result.error?.message);
        let deadline;
        try {
          const exited = await Promise.race([
            running.exited,
            new Promise((_, reject) => {
              deadline = setTimeout(
                () => reject(new Error("The old fixture remained running")),
                5000,
              );
            }),
          ]);
          assert.deepEqual(exited, { code: 0, signal: null });
        } finally {
          clearTimeout(deadline);
        }
        assert.equal(
          await readFile(running.marker + ".closed", "utf8"),
          "restart-manager",
        );
        assert.equal(await readFile(running.executable, "utf8"), replacement);
        assert.equal(other.child.exitCode, null);
        await assert.rejects(access(other.marker + ".closed"));
      },
    );

    await t.test(
      "a fresh install continues without a shutdown confirmation",
      async () => {
        const directory = path.join(work, "fresh-install");
        const result = spawnSync(decline, ["/S", "/D=" + directory], {
          windowsHide: true,
          windowsVerbatimArguments: true,
          timeout: 30000,
        });
        assert.equal(result.status, 0, result.error?.message);
        assert.equal(
          await readFile(path.join(directory, "mewdex-client.exe"), "utf8"),
          replacement,
        );
      },
    );

    const installerSource = await readFile(
      path.join(root, "scripts/windows/installer.nsi"),
      "utf8",
    );
    const reopen = installerSource.match(
      /Function ReopenWorkbench\r?\n[\s\S]*?FunctionEnd/,
    )?.[0];
    assert.ok(reopen);
    const counterSource = path.join(work, "LaunchCounter.cs");
    const counter = path.join(work, "counter.exe");
    await writeFile(
      counterSource,
      `using System;
using System.IO;
class LaunchCounter {
    static void Main() {
        File.WriteAllText(Path.Combine(AppDomain.CurrentDomain.BaseDirectory,
            "launch-" + System.Diagnostics.Process.GetCurrentProcess().Id + ".log"), "launch");
    }
}`,
    );
    run(compiler, [
      "/nologo",
      "/target:winexe",
      "/out:" + counter,
      counterSource,
    ]);
    const restartScript = path.join(work, "restart.nsi");
    const restartInstaller = path.join(work, "restart.exe");
    const restartHeader = path.join(work, "restart.nsh");
    await writeFile(restartHeader, include);
    await writeFile(
      restartScript,
      `Unicode true
RequestExecutionLevel user
SilentInstall silent
AutoCloseWindow true
OutFile "${nsisString(restartInstaller)}"
Var UpdatePID
!include "${nsisString(restartHeader)}"
${reopen}
Function .onInit
  StrCpy $UpdatePID "0"
FunctionEnd
Function .onInstSuccess
  Call ReopenWorkbench
  Call ReopenWorkbench
FunctionEnd
Function .onInstFailed
  Call ReopenWorkbench
FunctionEnd
Section
  Call CloseInstalledWorkbench
  SetOutPath "$INSTDIR"
  File /oname=mewdex-client.exe "${nsisString(counter)}"
SectionEnd
`,
    );
    run(nsis, ["/INPUTCHARSET", "UTF8", "/V2", restartScript]);

    await t.test("repeated update callbacks launch the APP once", async () => {
      const directory = path.join(work, "restart-once");
      const result = spawnSync(restartInstaller, ["/S", "/D=" + directory], {
        windowsHide: true,
        windowsVerbatimArguments: true,
        timeout: 30000,
      });
      assert.equal(result.status, 0, result.error?.message);
      const launches = async () =>
        (await readdir(directory)).filter((name) =>
          /^launch-\d+\.log$/.test(name),
        );
      for (let attempt = 0; attempt < 100; attempt++) {
        if ((await launches()).length > 0) break;
        await new Promise((resolve) => setTimeout(resolve, 20));
      }
      await new Promise((resolve) => setTimeout(resolve, 500));
      assert.equal((await launches()).length, 1);
    });

    await t.test(
      "declining an update does not relaunch the running APP",
      async () => {
        const directory = path.join(work, "decline-update");
        const running = await runningFixture(directory, fixture);
        children.push(running);
        const before = await readFile(running.executable);
        const result = spawnSync(restartInstaller, ["/S", "/D=" + directory], {
          windowsHide: true,
          windowsVerbatimArguments: true,
          timeout: 30000,
        });
        assert.equal(result.status, 1223);
        assert.equal(running.child.exitCode, null);
        await new Promise((resolve) => setTimeout(resolve, 500));
        assert.equal(
          (await readdir(directory)).filter((name) =>
            /^launch-\d+\.log$/.test(name),
          ).length,
          1,
        );
        await assert.rejects(access(running.marker + ".closed"));
        assert.deepEqual(await readFile(running.executable), before);
      },
    );
  },
);
