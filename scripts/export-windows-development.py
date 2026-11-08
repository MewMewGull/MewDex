import hashlib
import json
from pathlib import Path
import shutil
import stat
import subprocess
import tempfile
import zipfile


ROOT = Path(__file__).resolve().parent.parent
PACKAGE_NAME = "MewDex-Windows-x64"
OUTPUT = ROOT / "dist" / "windows"
REPORT = ROOT / ".state" / "desktop-development-build.json"


def digest(path):
    checksum = hashlib.sha256()
    with path.open("rb") as stream:
        for block in iter(lambda: stream.read(1024 * 1024), b""):
            checksum.update(block)
    return checksum.hexdigest()


def copy_file(source, destination):
    if source.is_symlink() or not source.is_file():
        raise RuntimeError("Unexpected packaging input: " + str(source))
    destination.parent.mkdir(parents=True, exist_ok=True)
    shutil.copy2(source, destination)
    destination.chmod(destination.stat().st_mode | stat.S_IWRITE)


def remove_directory(path):
    def writable_retry(operation, target, error):
        mode = Path(target).stat().st_mode
        if not isinstance(error[1], PermissionError) or mode & stat.S_IWRITE:
            raise error[1]
        Path(target).chmod(mode | stat.S_IWRITE)
        operation(target)

    shutil.rmtree(path, onerror=writable_retry)


def package_payload(source, stage, report):
    for name in ["mewdex-client.exe", "mewdex-input-bridge.exe"]:
        copy_file(source / name, stage / name)
    if digest(stage / "mewdex-client.exe") != report["sha256"] or digest(
        stage / "mewdex-input-bridge.exe"
    ) != report["helperSha256"]:
        raise RuntimeError("Packaged binaries do not match the build report")
    for item in source.glob("*.dll"):
        copy_file(item, stage / item.name)
    product = json.loads((ROOT / "mewdex.product.json").read_text(encoding="utf-8"))
    version = json.loads((ROOT / "package.json").read_text(encoding="utf-8"))["version"]
    manifest = {
        "schemaVersion": 1,
        "distribution": "app",
        "components": product["distributions"]["app"]["components"],
        "version": version,
        "os": "windows",
        "arch": "x64",
        "configuration": "Debug",
    }
    (stage / "component.json").write_text(
        json.dumps(manifest, indent=2) + "\n",
        encoding="utf-8",
    )
    for name in [
        "LICENSE",
        "NOTICE",
        "README.md",
        "README.zh-CN.md",
        "mewdex.product.json",
        "docs/releases.md",
        "docs/architecture.md",
        "docs/protocol.md",
        "THIRD_PARTY_NOTICES.md",
        "docs/messaging.md",
        "docs/hub-startup-20261102.md",
        "docs/dependency-licenses.json",
        "docs/licensing.md",
        "docs/evaluations.md",
        "docs/evaluations/core.example.json",
        "docs/evaluations/ordered.example.json",
        "docs/linux-sandbox.md",
        "third_party/hermes-agent/LICENSE",
        "third_party/hermes-agent/README.md",
        "third_party/tauri-plugin-notification/LICENSE_APACHE-2.0",
        "third_party/tauri-plugin-notification/LICENSE_MIT",
        "third_party/tauri-plugin-notification/MEWDEX-PATCHES.md",
    ]:
        copy_file(ROOT / name, stage / name)

    inventory = json.loads((ROOT / "docs/dependency-licenses.json").read_text(encoding="utf-8"))
    raw = subprocess.check_output(["go", "list", "-m", "-json", "all"], cwd=ROOT, text=True)
    decoder = json.JSONDecoder()
    modules = {}
    while raw.strip():
        module, end = decoder.raw_decode(raw.lstrip())
        modules[module["Path"]] = module
        raw = raw.lstrip()[end:]
    for package in inventory["go"]:
        directory = Path(modules[package["name"]]["Dir"])
        for item in package["evidence"]:
            copy_file(directory / item["file"], stage / "licenses/go" / package["name"] / item["file"])

    lock = json.loads((ROOT / "package-lock.json").read_text(encoding="utf-8"))
    for name, package in lock["packages"].items():
        if not name or "node_modules/" not in name or package.get("link") or package.get("dev"):
            continue
        directory = ROOT / name
        if not directory.is_dir():
            continue
        for item in directory.iterdir():
            if item.is_file() and item.name.lower().startswith(("license", "licence", "copying", "notice")):
                copy_file(item, stage / "licenses/npm" / name.replace("node_modules/", "") / item.name)

    (stage / "START-HERE.txt").write_text(
        "MewDex Windows x64 Debug 开发测试包\n\n"
        "解压后启动 mewdex-client.exe。\n"
        "APP 与网页版可任选其一，均为连接独立 Hub 的输入终端。\n"
        "QQ、微信和飞书由本 APP 的消息连接器接入，使用时保持 APP 运行。\n"
        "在设置中的消息渠道选择本 APP，保存平台凭据和允许用户。\n"
        "在平台聊天中发送 /projects 查看项目，再用 /project 序号或项目名选择工作项目。\n"
        "Hub、网页输入端和执行端分别使用独立发布包部署。\n"
        "执行端通过 WebSocket 连接 Hub，并提供独立网页控制面板。\n"
        "此轮交由用户人工验收。\n",
        encoding="utf-8",
    )


def output_path(name):
    target = OUTPUT / name
    if target.resolve().parent != OUTPUT.resolve() or not OUTPUT.resolve().is_relative_to(ROOT):
        raise RuntimeError("Package output must stay in dist/windows: " + str(target))
    return target


def main():
    report = json.loads(REPORT.read_text(encoding="utf-8-sig"))
    if report["state"] != "succeeded" or report["configuration"] != "Debug" or report.get("component") != "app":
        raise RuntimeError("Expected a completed Windows x64 Debug build")
    state = (ROOT / ".state").resolve()
    if not state.is_relative_to(ROOT):
        raise RuntimeError("Package staging must stay in the workspace")
    OUTPUT.mkdir(parents=True, exist_ok=True)
    directory = output_path(PACKAGE_NAME)
    archive = output_path(PACKAGE_NAME + ".zip")
    removed = []
    previous = None

    with tempfile.TemporaryDirectory(prefix="windows-package-", dir=state) as temporary:
        scratch = Path(temporary)
        stage = scratch / PACKAGE_NAME
        stage.mkdir()
        package_payload(Path(report["binary"]).parent, stage, report)
        staged_archive = stage.with_suffix(".zip")
        with zipfile.ZipFile(staged_archive, "x", compression=zipfile.ZIP_DEFLATED, compresslevel=6) as output:
            files = sorted(item for item in stage.rglob("*") if item.is_file())
            for item in files:
                output.write(item, PACKAGE_NAME + "/" + item.relative_to(stage).as_posix())

        with zipfile.ZipFile(staged_archive) as output:
            if output.testzip() is not None:
                raise RuntimeError("Package archive failed CRC validation")

        directory.mkdir(exist_ok=True)
        previous = Path(tempfile.mkdtemp(prefix="windows-package-previous-", dir=state))
        replaced = []
        legacy = []
        try:
            for item in files:
                relative = item.relative_to(stage)
                target = directory / relative
                if not target.resolve().is_relative_to(directory.resolve()):
                    raise RuntimeError("Package file escapes the output directory")
                if target.exists():
                    if target.is_symlink() or not target.is_file():
                        raise RuntimeError("Unexpected existing package file: " + str(target))
                    if digest(target) == digest(item):
                        continue
                    copy_file(target, previous / relative)
                target.parent.mkdir(parents=True, exist_ok=True)
                try:
                    item.replace(target)
                except PermissionError:
                    retained = previous / "in-use" / relative
                    retained.parent.mkdir(parents=True, exist_ok=True)
                    target.rename(retained)
                    try:
                        item.replace(target)
                    except OSError:
                        retained.rename(target)
                        raise
                replaced.append(relative)
            # Retire only the bundled service resources owned by the old APP export.
            for name in ["mewdex-node.exe", "engine"]:
                target = directory / name
                if target.exists():
                    if target.is_symlink() or not target.resolve().is_relative_to(directory.resolve()):
                        raise RuntimeError("Unexpected legacy service resource")
                    saved = previous / "legacy" / name
                    saved.parent.mkdir(parents=True, exist_ok=True)
                    target.rename(saved)
                    legacy.append((target, saved))
            staged_archive.replace(archive)
        except BaseException:
            for target, saved in reversed(legacy):
                saved.rename(target)
            for relative in reversed(replaced):
                target = directory / relative
                saved = previous / relative
                if saved.exists():
                    restore = scratch / "restore" / relative
                    copy_file(saved, restore)
                    restore.replace(target)
                else:
                    target.unlink()
            raise

        try:
            remove_directory(previous)
        except PermissionError:
            pass  # A running client can still hold the retained executable.

        for candidate in OUTPUT.glob(PACKAGE_NAME + "-*"):
            candidate = output_path(candidate.name)
            if candidate.is_dir():
                remove_directory(candidate)
            else:
                candidate.unlink()
            removed.append(str(candidate))

    result = {
        "directory": str(directory),
        "archive": str(archive),
        "sha256": digest(archive),
        "bytes": archive.stat().st_size,
        "files": len(files),
        "configuration": "Windows/x64/Debug",
        "clientSha256": report["sha256"],
        "helperSha256": report["helperSha256"],
        "component": "app",
        "removedOldPackages": removed,
        "crcValidated": True,
        "previousDirectoryBackup": str(previous) if previous.exists() else None,
        "installed": False,
        "manualAcceptancePending": True,
    }
    (ROOT / ".state/windows-package.json").write_text(
        json.dumps(result, ensure_ascii=False, indent=2) + "\n", encoding="utf-8"
    )
    print(json.dumps(result, ensure_ascii=False, indent=2))


if __name__ == "__main__":
    main()
