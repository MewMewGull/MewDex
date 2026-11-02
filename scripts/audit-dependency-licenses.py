"""Record pinned package license metadata; this is not a binary-release clearance.

Requires Python 3.11+, Go, and Cargo. Cargo metadata may download package sources,
but does not build or run the application. No model or device credentials are read.
"""

import argparse
import collections
import datetime
import hashlib
import json
import os
from pathlib import Path
import shutil
import subprocess
import tomllib


ROOT = Path(__file__).resolve().parent.parent
RUST_MANIFESTS = {
    "client": "apps/client/src-tauri/Cargo.toml",
    "native-vault-risk": "scripts/native-vault-risk/Cargo.toml",
}
# These module-level declarations were reviewed at these exact versions.
# A version change or new module requires a new review rather than an assumption.
REVIEWED_GO_LICENSES = {
    ("filippo.io/age", "v1.3.2"): "BSD-3-Clause",
    ("filippo.io/hpke", "v0.4.0"): "BSD-3-Clause",
    ("github.com/coder/websocket", "v1.8.14"): "ISC",
    ("github.com/dustin/go-humanize", "v1.0.1"): "MIT",
    ("github.com/ebitengine/purego", "v0.11.2"): "Apache-2.0",
    ("github.com/google/uuid", "v1.6.0"): "BSD-3-Clause",
    ("github.com/mattn/go-isatty", "v0.0.20"): "MIT",
    ("github.com/ncruces/go-strftime", "v0.1.9"): "MIT",
    ("github.com/remyoudompheng/bigfft", "v0.0.0-20230129092748-24d4a6f8daec"): "BSD-3-Clause",
    ("golang.org/x/crypto", "v0.55.0"): "BSD-3-Clause",
    ("golang.org/x/exp", "v0.0.0-20250620022241-b7579e27df2b"): "BSD-3-Clause",
    ("golang.org/x/sys", "v0.47.0"): "BSD-3-Clause",
    ("modernc.org/libc", "v1.66.3"): "BSD-3-Clause",
    ("modernc.org/mathutil", "v1.7.1"): "BSD-3-Clause",
    ("modernc.org/memory", "v1.11.0"): "BSD-3-Clause",
    ("modernc.org/sqlite", "v1.38.2"): "BSD-3-Clause",
}


def sha256(path):
    return hashlib.sha256(path.read_bytes()).hexdigest()


def read_json(path):
    return json.loads(path.read_text(encoding="utf-8-sig"))


def run_json(arguments, env=None):
    return json.loads(subprocess.check_output(arguments, cwd=ROOT, env=env))


def json_stream(text):
    decoder = json.JSONDecoder()
    while text.strip():
        value, end = decoder.raw_decode(text.lstrip())
        yield value
        text = text.lstrip()[end:]


def npm_inventory():
    packages = []
    for location, package in sorted(read_json(ROOT / "package-lock.json")["packages"].items()):
        if "node_modules/" not in location or package.get("link"):
            continue
        license_expression = package.get("license")
        if not license_expression:
            raise ValueError(f"Missing npm license in lockfile: {location}")
        packages.append({
            "name": location.rsplit("node_modules/", 1)[1],
            "version": package["version"],
            "license": license_expression,
            "developmentOnly": bool(package.get("dev")),
            "optional": bool(package.get("optional")),
            "source": package.get("resolved"),
            "integrity": package.get("integrity"),
        })
    return packages


def rust_inventory():
    env = os.environ.copy()
    local_tools = ROOT / ".local/toolchains"
    cargo_name = "cargo.exe" if os.name == "nt" else "cargo"
    local_cargo = local_tools / "cargo/bin" / cargo_name
    if local_cargo.is_file():
        env["CARGO_HOME"] = str(local_tools / "cargo")
        env["RUSTUP_HOME"] = str(local_tools / "rustup")
        env["PATH"] = str(local_cargo.parent) + os.pathsep + env.get("PATH", "")
        cargo = str(local_cargo)
    else:
        cargo = shutil.which("cargo")
        if not cargo:
            raise RuntimeError("Cargo is required to inspect pinned Rust package manifests")
    packages = {}
    for consumer, manifest in RUST_MANIFESTS.items():
        lock_path = Path(manifest).with_name("Cargo.lock")
        locked = tomllib.loads((ROOT / lock_path).read_text(encoding="utf-8"))
        checksums = {
            (p["name"], p["version"], p.get("source")): p.get("checksum")
            for p in locked["package"]
        }
        metadata = run_json([
            cargo, "metadata", "--locked", "--all-features", "--format-version", "1",
            "--manifest-path", str(ROOT / manifest),
        ], env=env)
        for package in metadata["packages"]:
            if not package.get("source"):
                continue
            key = (package["name"], package["version"], package["source"])
            if not package.get("license"):
                raise ValueError(f"Missing Rust license declaration: {key}")
            if key not in packages:
                packages[key] = {
                    "name": package["name"],
                    "version": package["version"],
                    "license": package["license"],
                    "source": package["source"],
                    "checksum": checksums.get(key),
                    "repository": package.get("repository"),
                    "usedBy": [],
                }
            packages[key]["usedBy"].append(consumer)
    return [packages[key] for key in sorted(packages)]


def go_inventory():
    requirements = run_json(["go", "mod", "edit", "-json"])["Require"]
    module_text = subprocess.check_output(["go", "list", "-m", "-json", "all"], cwd=ROOT).decode("utf-8")
    modules = {module["Path"]: module for module in json_stream(module_text)}
    packages = []
    for requirement in sorted(requirements, key=lambda p: p["Path"]):
        name, version = requirement["Path"], requirement["Version"]
        license_expression = REVIEWED_GO_LICENSES.get((name, version))
        if not license_expression:
            raise ValueError(f"Go module needs a license review: {name}@{version}")
        module = modules[name]
        if module.get("Replace"):
            raise ValueError(f"Go module replacement needs a license review: {name}")
        if not module.get("Dir"):
            module = run_json(["go", "mod", "download", "-json", f"{name}@{version}"])
        directory = Path(module["Dir"])
        evidence = [
            {"file": file.name, "sha256": sha256(file)}
            for file in sorted(directory.iterdir())
            if file.is_file() and file.name.upper().startswith(("LICENSE", "LICENCE", "COPYING", "NOTICE"))
        ]
        if not evidence:
            raise ValueError(f"No module-level license files: {name}@{version}")
        packages.append({
            "name": name, "version": version, "license": license_expression,
            "direct": not requirement.get("Indirect", False),
            "evidence": evidence,
        })
    return packages


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--review-date", type=datetime.date.fromisoformat, default=datetime.date.today())
    parser.add_argument("--output", type=Path, default=ROOT / "docs/dependency-licenses.json")
    args = parser.parse_args()
    inputs = [
        "package-lock.json", "go.mod", "go.sum", "engine.lock.json",
        *RUST_MANIFESTS.values(),
        *(str(Path(p).with_name("Cargo.lock")).replace("\\", "/") for p in RUST_MANIFESTS.values()),
        "third_party/tauri-plugin-notification/Cargo.toml",
    ]
    report = {
        "schemaVersion": 1,
        "reviewDate": args.review_date.isoformat(),
        "scope": "Package-level license declarations, all npm lockfile entries, all Rust targets/features, declared Go requirements; not binary-release clearance or an audit of every embedded file, SDK, system library, or engine dependency.",
        "inputHashFormat": "SHA-256 of UTF-8 files after CRLF-to-LF normalization, matching Git text storage",
        "inputs": {
            name: hashlib.sha256((ROOT / name).read_bytes().replace(b"\r\n", b"\n")).hexdigest()
            for name in sorted(inputs)
        },
        "npm": npm_inventory(),
        "rust": rust_inventory(),
        "go": go_inventory(),
        "vendored": [{
            "name": "tauri-plugin-notification", "version": "2.8.0",
            "license": "Apache-2.0 OR MIT", "path": "third_party/tauri-plugin-notification",
        }],
        "engine": read_json(ROOT / "engine.lock.json"),
    }
    args.output.parent.mkdir(parents=True, exist_ok=True)
    args.output.write_text(json.dumps(report, ensure_ascii=False, indent=2) + "\n", encoding="utf-8", newline="\n")
    print(json.dumps({
        ecosystem: {"packages": len(report[ecosystem]), "licenses": dict(sorted(collections.Counter(p["license"] for p in report[ecosystem]).items()))}
        for ecosystem in ["npm", "rust", "go"]
    }, indent=2))


if __name__ == "__main__":
    main()
