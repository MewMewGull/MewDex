"""Build and stage the pinned local sandbox using the project Rust toolchain."""

import argparse
import base64
import gzip
import hashlib
import io
import json
import os
from pathlib import Path
import posixpath
import platform
import re
import shutil
import subprocess
import tarfile
import time
import urllib.request
import uuid


ROOT = Path(__file__).resolve().parent.parent
LOCK = json.loads((ROOT / "tools/sandbox.lock.json").read_text(encoding="utf-8"))
TARGETS = {
    "win32:x64": ("Windows", ["amd64", "x86_64"], "windows-x64", "x86_64-pc-windows-msvc", ".exe"),
    "darwin:arm64": ("Darwin", ["arm64", "aarch64"], "macos-arm64", "aarch64-apple-darwin", ""),
    "linux:x64": ("Linux", ["x86_64", "amd64"], "linux-x64", "x86_64-unknown-linux-musl", ""),
}


def sha256(path):
    value = hashlib.sha256()
    with path.open("rb") as stream:
        for block in iter(lambda: stream.read(1 << 20), b""):
            value.update(block)
    return value.hexdigest()


def prepare_bubblewrap(target):
    pin = LOCK["linuxResources"][target[3]]
    integrity = base64.b64decode(pin["integrity"].removeprefix("sha512-"), validate=True)
    cache = ROOT / ".state/engine-packages" / (pin["packageVersion"] + "-" + integrity.hex()[:16])
    cache.mkdir(parents=True, exist_ok=True)
    archive = cache / "package.tgz"
    if not archive.exists():
        temporary = cache / ("package." + uuid.uuid4().hex + ".tgz")
        urllib.request.urlretrieve(pin["archiveURL"], temporary)
        with temporary.open("rb") as stream:
            if hashlib.file_digest(stream, "sha512").digest() != integrity:
                raise ValueError("Bubblewrap package failed its integrity check")
        temporary.rename(archive)
    with archive.open("rb") as stream:
        if hashlib.file_digest(stream, "sha512").digest() != integrity:
            raise ValueError("Cached Bubblewrap package changed and was preserved")
    binary = cache / "bwrap"
    if binary.exists():
        if binary.is_symlink() or sha256(binary) != pin["sha256"] or binary.stat().st_size != pin["bytes"]:
            raise ValueError("Cached Bubblewrap resource changed and was preserved")
        return binary
    with tarfile.open(archive) as package:
        member = package.getmember(pin["archivePath"])
        if not member.isfile() or member.size != pin["bytes"]:
            raise ValueError("Bubblewrap package has an unexpected resource")
        with package.extractfile(member) as stream:
            data = stream.read()
    if hashlib.sha256(data).hexdigest() != pin["sha256"]:
        raise ValueError("Bubblewrap resource failed its integrity check")
    with binary.open("xb") as stream:
        stream.write(data)
    return binary


def bubblewrap_sources(source, destination):
    vendor = source / "codex-rs/vendor/bubblewrap"
    output = destination / "licenses/bubblewrap"
    output.mkdir(parents=True, exist_ok=True)
    shutil.copyfile(vendor / "COPYING", output / "COPYING")
    # Include the vendored sources and their original build files, alongside
    # the digest and URL of the complete corresponding Codex source snapshot.
    with (output / "source.tar.gz").open("wb") as stream:
        with gzip.GzipFile(fileobj=stream, mode="wb", mtime=0) as compressed:
            with tarfile.open(fileobj=compressed, mode="w|") as archive:
                for path in sorted(vendor.rglob("*")):
                    if not path.is_file():
                        continue
                    data = path.read_bytes()
                    entry = tarfile.TarInfo("bubblewrap/" + path.relative_to(vendor).as_posix())
                    entry.size, entry.mtime = len(data), 0
                    entry.mode = 0o755 if path.stat().st_mode & 0o111 else 0o644
                    with io.BytesIO(data) as contents:
                        archive.addfile(entry, contents)
    provenance = {
        "sourceCommit": LOCK["sourceCommit"], "sourceArchiveSha256": LOCK["sourceArchiveSha256"],
        "sourceArchiveURL": "https://codeload.github.com/openai/codex/tar.gz/" + LOCK["sourceCommit"],
        "vendoredPath": "codex-rs/vendor/bubblewrap", "sourceBundleSha256": sha256(output / "source.tar.gz"),
    }
    (output / "source.json").write_text(json.dumps(provenance, indent=2) + "\n", encoding="utf-8")


def prepare_source(repository="openai/codex", pin=None):
    pin = LOCK if pin is None else pin
    name = repository.rsplit("/", 1)[1]
    state = ROOT / ".state"
    state.mkdir(exist_ok=True)
    archive = state / (name + "-sandbox-" + pin["sourceCommit"] + ".tar.gz")
    if not archive.exists():
        temporary = archive.with_name(archive.name + "." + uuid.uuid4().hex)
        url = "https://codeload.github.com/" + repository + "/tar.gz/" + pin["sourceCommit"]
        urllib.request.urlretrieve(url, temporary)
        if sha256(temporary) != pin["sourceArchiveSha256"]:
            raise ValueError("Sandbox source archive failed its integrity check")
        temporary.rename(archive)
    if sha256(archive) != pin["sourceArchiveSha256"]:
        raise ValueError("Cached sandbox source archive failed its integrity check")
    parent = state / "sandbox-source"
    parent.mkdir(exist_ok=True)
    directory = parent / (name + "-" + pin["sourceCommit"])
    with tarfile.open(archive) as package:
        members = package.getmembers()
        for member in members:
            relative = Path(member.name)
            if relative.is_absolute() or ".." in relative.parts or relative.parts[0] != directory.name:
                raise ValueError("Unexpected sandbox source archive path")
            if member.issym():
                linked = posixpath.normpath(posixpath.join(posixpath.dirname(member.name), member.linkname))
                if not linked.startswith(directory.name + "/") or not package.getmember(linked).isfile():
                    raise ValueError("Sandbox source link must name an internal regular file")
            elif not member.isdir() and not member.isfile():
                raise ValueError("Sandbox sources must contain regular files and directories")
        if not directory.exists():
            scratch = state / ("sandbox-source-" + uuid.uuid4().hex)
            scratch.mkdir()
            package.extractall(scratch, members=[member for member in members if not member.issym()], filter="data")
            for member in members:
                if member.issym():
                    linked = posixpath.normpath(posixpath.join(posixpath.dirname(member.name), member.linkname))
                    # Materialize repository license links for Windows builds.
                    with package.extractfile(linked) as stream:
                        (scratch / member.name).write_bytes(stream.read())
            for attempt in range(10):
                try:
                    (scratch / directory.name).rename(directory)
                    break
                except PermissionError as error:
                    if getattr(error, "winerror", None) not in [5, 32] or attempt == 9:
                        raise
                    time.sleep(0.2)
            scratch.rmdir()
        # Verify extracted sources on every build, including cached inputs.
        for member in members:
            target = parent / member.name
            if member.isfile() or member.issym():
                if not target.resolve().is_relative_to(directory):
                    raise ValueError("Extracted sandbox source escaped its directory")
                original = member
                if member.issym():
                    linked = posixpath.normpath(posixpath.join(posixpath.dirname(member.name), member.linkname))
                    original = package.getmember(linked)
                with package.extractfile(original) as stream:
                    expected = hashlib.sha256(stream.read()).hexdigest()
                if target.is_symlink() or not target.is_file() or sha256(target) != expected:
                    raise ValueError("Extracted sandbox source changed and was preserved")
    return directory


def licenses(metadata, destination, sources):
    inventory = []
    supplements = json.loads((ROOT / "tools/sandbox-license-evidence.json").read_text(encoding="utf-8"))
    supplements = {(item["package"], item["version"]): item for item in supplements["packages"]}
    for package in metadata["packages"]:
        directory = Path(package["manifest_path"]).parent
        if directory == ROOT / "apps/sandbox":
            continue
        candidates = [directory]
        # Source-pinned path dependencies inherit their repository licenses.
        for source in sources:
            if directory.is_relative_to(source):
                candidates.extend([source / "codex-rs", source])
        if str(package.get("source", "")).startswith("git+"):
            for parent in directory.parents:
                candidates.append(parent)
                if (parent / ".git").exists():
                    break
        files = []
        for candidate in candidates:
            if not candidate.is_dir():
                continue
            files = sorted(
                item for item in candidate.iterdir()
                if item.is_file() and re.match(r"^(LICENSE|LICENCE|COPYING|NOTICE)", item.name, re.I)
            )
            if files:
                break
        supplement = None
        if not files:
            supplement = supplements.get((package["name"], package["version"]))
            if supplement and supplement["license"] == package.get("license"):
                for evidence in supplement["files"]:
                    file = ROOT / evidence["path"]
                    if sha256(file) != evidence["sha256"]:
                        raise ValueError("Sandbox license evidence changed")
                    files.append(file)
        if not package.get("license") or not files:
            raise ValueError("Sandbox dependency needs license evidence: " + package["name"])
        if package["name"] == "openssl-src":
            embedded = directory / "openssl/LICENSE.txt"
            if not embedded.is_file():
                raise ValueError("Vendored OpenSSL needs its embedded library license")
            files.append(embedded)
        output = destination / "licenses" / (package["name"] + "-" + package["version"])
        output.mkdir(parents=True, exist_ok=True)
        for file in files:
            shutil.copyfile(file, output / file.name)
        inventory.append({
            "name": package["name"], "version": package["version"], "license": package["license"],
            "source": package.get("source"), "files": [file.name for file in files],
            "supplement": supplement,
        })
    (destination / "dependency-licenses.json").write_text(json.dumps(inventory, indent=2) + "\n", encoding="utf-8")


def input_digest():
    digest = hashlib.sha256()
    directory = ROOT / "apps/sandbox"
    files = [directory / "Cargo.toml", directory / "Cargo.lock", directory / ".cargo/config.toml"]
    files.extend(sorted((directory / "src").rglob("*.rs")))
    files.extend([ROOT / "scripts/prepare-sandbox-resources.py", ROOT / "tools/sandbox.lock.json"])
    for file in files:
        digest.update(file.relative_to(ROOT).as_posix().encode("utf-8") + b"\0")
        digest.update(file.read_bytes().replace(b"\r\n", b"\n") + b"\0")
    return digest.hexdigest()


def verify_prebuilt(destination, target):
    runtime = json.loads((destination / "mewdex-engine.json").read_text(encoding="utf-8"))
    provenance = json.loads((destination / "source.json").read_text(encoding="utf-8"))
    build = json.loads((destination / "build.json").read_text(encoding="utf-8"))
    if provenance != LOCK or build.get("inputSha256") != input_digest():
        raise ValueError("Rebuild this native sandbox from the current source inputs")
    if (runtime.get("kind"), runtime.get("version"), runtime.get("runtimeVersion"), runtime.get("target")) != (
        "sandbox", LOCK["codexVersion"], LOCK["runtimeVersion"], target[3],
    ):
        raise ValueError("Prepared sandbox metadata does not match this target")
    expected_path = "bin/mewdex-sandbox" + target[4]
    expected_paths = {expected_path}
    if target[0] == "Linux":
        expected_paths.add("bin/bwrap")
    files = runtime.get("files", [])
    if len(files) != len(expected_paths) or {file.get("path") for file in files} != expected_paths:
        raise ValueError("Prepared sandbox has an unexpected runtime file list")
    for resource in files:
        binary = destination / resource["path"]
        if binary.is_symlink() or sha256(binary) != resource["sha256"] or binary.stat().st_size != resource["bytes"]:
            raise ValueError("Prepared native sandbox failed its integrity check")
    if target[0] == "Linux":
        bubblewrap = next(file for file in files if file["path"] == "bin/bwrap")
        pin = LOCK["linuxResources"][target[3]]
        if bubblewrap["sha256"] != pin["sha256"] or bubblewrap["bytes"] != pin["bytes"]:
            raise ValueError("Prepared Bubblewrap does not match its source pin")
    resources = build.get("files")
    if not isinstance(resources, dict) or not resources:
        raise ValueError("Prepared sandbox needs a complete resource integrity record")
    required = {"LICENSE", "dependency-licenses.json", "source.json", "mewdex-engine.json", *expected_paths}
    if target[0] == "Linux":
        required.update({"licenses/bubblewrap/COPYING", "licenses/bubblewrap/source.tar.gz", "licenses/bubblewrap/source.json"})
    if not required.issubset(resources):
        raise ValueError("Prepared sandbox is missing a required resource")
    for relative, expected in resources.items():
        path = Path(relative)
        if path.is_absolute() or ".." in path.parts or "\\" in relative or ":" in relative:
            raise ValueError("Prepared sandbox resource path is invalid")
        file = destination / path
        if file.is_symlink() or not file.resolve().is_relative_to(destination.resolve()) or sha256(file) != expected:
            raise ValueError("Prepared native sandbox resource failed its integrity check")
    entry = next(file for file in files if file["path"] == expected_path)
    print(json.dumps({"output": str(destination), "bytes": entry["bytes"], "sha256": entry["sha256"], "preparedBuildVerified": True}))


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--target", required=True, choices=LOCK["supportedTargets"])
    parser.add_argument("--output", required=True, type=Path)
    args = parser.parse_args()
    target = TARGETS[args.target]
    output = (ROOT / args.output).resolve()
    if not output.is_relative_to(ROOT / ".state") or output == ROOT / ".state":
        raise ValueError("Sandbox build output must stay in a named .state directory")
    engine = json.loads((ROOT / "engine.lock.json").read_text(encoding="utf-8"))
    if (engine["version"], engine["sourceCommit"]) != (LOCK["codexVersion"], LOCK["sourceCommit"]):
        raise ValueError("Sandbox source pin must match the Hub runtime source pin")
    manifest = ROOT / "apps/sandbox/Cargo.toml"
    if ('version = "' + LOCK["runtimeVersion"] + '"') not in manifest.read_text(encoding="utf-8"):
        raise ValueError("Sandbox runtime version is inconsistent")
    expected_go = 'const NativeSandboxVersion = "' + LOCK["runtimeVersion"] + '"'
    if expected_go not in (ROOT / "internal/engine/sandbox.go").read_text(encoding="utf-8"):
        raise ValueError("The Go sandbox protocol version is inconsistent")
    destination = output / "sandbox"
    if platform.system() != target[0] or platform.machine().lower() not in target[1]:
        if not destination.exists():
            raise ValueError("Build this sandbox on its native target and copy the prepared sandbox directory here")
        verify_prebuilt(destination, target)
        return
    source = prepare_source()
    mxc_source = prepare_source("microsoft/mxc", LOCK["mxcSource"])
    environment = dict(os.environ)
    tools = ROOT / ".local/toolchains"
    environment.update(
        CARGO_HOME=str(tools / "cargo"), RUSTUP_HOME=str(tools / "rustup"),
        CARGO_TARGET_DIR=str(ROOT / ".state/sandbox-target" / target[2]),
        CARGO_NET_GIT_FETCH_WITH_CLI="true",
    )
    environment["PATH"] = str(tools / "cargo/bin") + os.pathsep + environment["PATH"]
    cargo = tools / ("cargo/bin/cargo" + (".exe" if os.name == "nt" else ""))
    arguments = [str(cargo), "build", "--release", "--locked"]
    build_directory = Path(environment["CARGO_TARGET_DIR"])
    bubblewrap = None
    if target[0] == "Linux":
        bubblewrap = prepare_bubblewrap(target)
        environment["CODEX_BWRAP_SHA256"] = sha256(bubblewrap)
        environment.setdefault("CARGO_BUILD_JOBS", "1")
        compiler = tools / "musl/bin/musl-gcc"
        if compiler.exists():
            environment["CC_x86_64_unknown_linux_musl"] = str(compiler)
            environment["CARGO_TARGET_X86_64_UNKNOWN_LINUX_MUSL_LINKER"] = str(compiler)
        arguments.extend(["--target", target[3]])
        build_directory /= target[3]
    subprocess.run(arguments, cwd=manifest.parent, env=environment, check=True)
    executable = build_directory / ("release/mewdex-sandbox" + target[4])
    version = subprocess.check_output([str(executable), "--version"], text=True).strip()
    if version != "mewdex-sandbox " + LOCK["runtimeVersion"]:
        raise ValueError("Built sandbox version is inconsistent")
    (destination / "bin").mkdir(parents=True, exist_ok=True)
    binary = destination / ("bin/mewdex-sandbox" + target[4])
    shutil.copy2(executable, binary)
    runtime_files = [{"path": "bin/mewdex-sandbox" + target[4], "sha256": sha256(binary), "bytes": binary.stat().st_size}]
    if bubblewrap is not None:
        shutil.copy2(bubblewrap, destination / "bin/bwrap")
        (destination / "bin/bwrap").chmod(0o755)
        runtime_files.append({"path": "bin/bwrap", "sha256": sha256(bubblewrap), "bytes": bubblewrap.stat().st_size})
        bubblewrap_sources(source, destination)
    metadata = json.loads(subprocess.check_output(
        [str(cargo), "metadata", "--format-version=1", "--locked", "--filter-platform", target[3]],
        cwd=manifest.parent, env=environment,
    ))
    for package in metadata["packages"]:
        if package["name"].startswith("codex-") and not Path(package["manifest_path"]).is_relative_to(source):
            raise ValueError("A Codex sandbox module is outside its verified source snapshot")
        if package["name"] == "mxc-sdk" and not Path(package["manifest_path"]).is_relative_to(mxc_source):
            raise ValueError("The MXC sandbox module is outside its verified source snapshot")
    licenses(metadata, destination, [source, mxc_source])
    shutil.copyfile(ROOT / "LICENSE", destination / "LICENSE")
    shutil.copyfile(ROOT / "tools/sandbox.lock.json", destination / "source.json")
    runtime = {
        "format": 1, "version": LOCK["codexVersion"], "kind": "sandbox",
        "runtimeVersion": LOCK["runtimeVersion"], "target": target[3],
        "files": runtime_files,
    }
    (destination / "mewdex-engine.json").write_text(json.dumps(runtime, indent=2) + "\n", encoding="utf-8")
    resources = {file.relative_to(destination).as_posix(): sha256(file) for file in sorted(destination.rglob("*")) if file.is_file() and file.name != "build.json"}
    (destination / "build.json").write_text(json.dumps({"inputSha256": input_digest(), "files": resources}, indent=2) + "\n", encoding="utf-8")
    print(json.dumps({"output": str(destination), "bytes": binary.stat().st_size, "sha256": sha256(binary)}))


if __name__ == "__main__":
    main()
