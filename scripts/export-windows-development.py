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
    for name in ["mewdex-client.exe", "mewdex-node.exe"]:
        copy_file(source / name, stage / name)
    if digest(stage / "mewdex-client.exe") != report["sha256"] or digest(
        stage / "mewdex-node.exe"
    ) != report["helperSha256"]:
        raise RuntimeError("Packaged binaries do not match the build report")
    for item in (source / "engine").rglob("*"):
        if item.is_file():
            copy_file(item, stage / "engine" / item.relative_to(source / "engine"))
    for item in source.glob("*.dll"):
        copy_file(item, stage / item.name)
    for name in [
        "LICENSE",
        "NOTICE",
        "THIRD_PARTY_NOTICES.md",
        "docs/messaging.md",
        "docs/hub-startup-20261102.md",
        "docs/dependency-licenses.json",
        "docs/licensing.md",
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
        "桌面 APP 会启动已配置的本机 Hub，首次使用请完成本机设备配置。\n"
        "网页版为可选功能，在设置 → 连接与服务，或本机设备 → 概览中启动。\n"
        "使用此 Hub 的 MewDex 账号或登录密钥登录网页版。\n"
        "退出桌面 APP 后，中央后端与已启用的网页版继续后台运行。\n"
        "执行端没有项目时，会在其数据目录同级 workspaces/default 中准备默认工作区。\n"
        "已有后台服务请在本机设备 → 数据与更新中更新组件。\n"
        "设置弹窗支持调整大小，外观设置可调整主题和强调色。\n"
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
    if report["state"] != "succeeded" or report["configuration"] != "Debug":
        raise RuntimeError("Expected a completed Windows x64 Debug build")
    state = (ROOT / ".state").resolve()
    if not state.is_relative_to(ROOT):
        raise RuntimeError("Package staging must stay in the workspace")
    OUTPUT.mkdir(parents=True, exist_ok=True)
    directory = output_path(PACKAGE_NAME)
    archive = output_path(PACKAGE_NAME + ".zip")
    removed = []

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

        previous = scratch / "previous"
        if directory.exists():
            directory.rename(previous)
        try:
            stage.rename(directory)
            staged_archive.replace(archive)
        except OSError:
            if directory.exists():
                remove_directory(directory)
            if previous.exists():
                previous.rename(directory)
            raise

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
        "engineVersion": report["engineVersion"],
        "removedOldPackages": removed,
        "crcValidated": False,
        "installed": False,
        "manualAcceptancePending": True,
    }
    (ROOT / ".state/windows-package.json").write_text(
        json.dumps(result, ensure_ascii=False, indent=2) + "\n", encoding="utf-8"
    )
    print(json.dumps(result, ensure_ascii=False, indent=2))


if __name__ == "__main__":
    main()
