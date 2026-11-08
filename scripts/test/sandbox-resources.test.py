import hashlib
import importlib.util
import io
import json
from pathlib import Path
import tarfile
import tempfile
import unittest
from contextlib import redirect_stdout


ROOT = Path(__file__).resolve().parents[2]
SPEC = importlib.util.spec_from_file_location("sandbox_resources", ROOT / "scripts/prepare-sandbox-resources.py")
BUILDER = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(BUILDER)


class SandboxSourceTests(unittest.TestCase):
    def setUp(self):
        self.temporary = tempfile.TemporaryDirectory(prefix="sandbox-source-test-", dir=ROOT / ".state")
        self.root = Path(self.temporary.name).resolve()
        self.assertTrue(self.root.is_relative_to(ROOT / ".state"))
        self.original_root, self.original_lock = BUILDER.ROOT, BUILDER.LOCK
        self.commit = "1" * 40
        BUILDER.ROOT = self.root
        BUILDER.LOCK = {"sourceCommit": self.commit}
        (self.root / ".state").mkdir()

    def tearDown(self):
        BUILDER.ROOT, BUILDER.LOCK = self.original_root, self.original_lock
        self.temporary.cleanup()

    def archive(self, escaping=False):
        path = self.root / ".state" / ("codex-sandbox-" + self.commit + ".tar.gz")
        prefix = "codex-" + self.commit
        with tarfile.open(path, "w:gz") as package:
            for name, data in [("COPYING", b"fixture license"), ("src/lib.rs", b"fixture source")]:
                member = tarfile.TarInfo(prefix + "/" + name)
                member.size = len(data)
                package.addfile(member, io.BytesIO(data))
            link = tarfile.TarInfo(prefix + "/LICENSE")
            link.type = tarfile.SYMTYPE
            link.linkname = "../../outside" if escaping else "COPYING"
            package.addfile(link)
        BUILDER.LOCK["sourceArchiveSha256"] = hashlib.sha256(path.read_bytes()).hexdigest()

    def test_source_pin_and_portable_license_link(self):
        self.archive()
        source = BUILDER.prepare_source()
        self.assertEqual((source / "LICENSE").read_bytes(), b"fixture license")
        self.assertFalse((source / "LICENSE").is_symlink())
        self.assertEqual(BUILDER.prepare_source(), source)

    def test_modified_cached_source_is_rejected_and_preserved(self):
        self.archive()
        source = BUILDER.prepare_source()
        changed = source / "src/lib.rs"
        changed.write_bytes(b"changed source")
        with self.assertRaisesRegex(ValueError, "source changed"):
            BUILDER.prepare_source()
        self.assertEqual(changed.read_bytes(), b"changed source")

    def test_source_link_cannot_escape_the_archive(self):
        self.archive(escaping=True)
        with self.assertRaisesRegex(ValueError, "internal regular file"):
            BUILDER.prepare_source()
        self.assertFalse((self.root / "outside").exists())


class PreparedSandboxTests(unittest.TestCase):
    def setUp(self):
        self.temporary = tempfile.TemporaryDirectory(prefix="sandbox-prepared-test-", dir=ROOT / ".state")
        self.root = Path(self.temporary.name).resolve()
        self.original_root, self.original_lock = BUILDER.ROOT, BUILDER.LOCK
        BUILDER.ROOT = self.root
        for relative in ["apps/sandbox/Cargo.toml", "apps/sandbox/Cargo.lock", "apps/sandbox/.cargo/config.toml", "apps/sandbox/src/main.rs", "scripts/prepare-sandbox-resources.py", "tools/sandbox.lock.json"]:
            file = self.root / relative
            file.parent.mkdir(parents=True, exist_ok=True)
            file.write_text("fixture source", encoding="utf-8")
        self.destination = self.root / ".state/prepared/sandbox"
        self.binary = self.destination / "bin/mewdex-sandbox"
        self.binary.parent.mkdir(parents=True)
        self.binary.write_bytes(b"fixture binary")
        self.target = BUILDER.TARGETS["darwin:arm64"]
        runtime = {
            "kind": "sandbox", "version": BUILDER.LOCK["codexVersion"],
            "runtimeVersion": BUILDER.LOCK["runtimeVersion"], "target": self.target[3],
            "files": [{"path": "bin/mewdex-sandbox", "sha256": BUILDER.sha256(self.binary), "bytes": self.binary.stat().st_size}],
        }
        (self.destination / "mewdex-engine.json").write_text(json.dumps(runtime), encoding="utf-8")
        (self.destination / "source.json").write_text(json.dumps(BUILDER.LOCK), encoding="utf-8")
        (self.destination / "LICENSE").write_text("fixture license", encoding="utf-8")
        (self.destination / "dependency-licenses.json").write_text("[]", encoding="utf-8")
        files = {file.relative_to(self.destination).as_posix(): BUILDER.sha256(file) for file in self.destination.rglob("*") if file.is_file()}
        (self.destination / "build.json").write_text(json.dumps({"inputSha256": BUILDER.input_digest(), "files": files}), encoding="utf-8")

    def tearDown(self):
        BUILDER.ROOT, BUILDER.LOCK = self.original_root, self.original_lock
        self.temporary.cleanup()

    def verify(self):
        with redirect_stdout(io.StringIO()):
            BUILDER.verify_prebuilt(self.destination, self.target)

    def test_prepared_native_build_matches_source_and_target(self):
        self.verify()

    def test_changed_native_source_requires_rebuild(self):
        (self.root / "apps/sandbox/src/main.rs").write_text("changed source", encoding="utf-8")
        with self.assertRaisesRegex(ValueError, "Rebuild this native sandbox"):
            self.verify()

    def test_modified_runtime_or_license_resource_is_rejected(self):
        for resource in [self.binary, self.destination / "LICENSE"]:
            original = resource.read_bytes()
            resource.write_bytes(b"changed resource")
            with self.assertRaisesRegex(ValueError, "integrity check"):
                self.verify()
            resource.write_bytes(original)

    def test_prepared_metadata_cannot_select_another_target(self):
        with self.assertRaisesRegex(ValueError, "does not match this target"):
            with redirect_stdout(io.StringIO()):
                BUILDER.verify_prebuilt(self.destination, BUILDER.TARGETS["win32:x64"])

    def linux_bundle(self):
        self.target = BUILDER.TARGETS["linux:x64"]
        bubblewrap = self.destination / "bin/bwrap"
        bubblewrap.write_bytes(b"fixture bubblewrap")
        pin = {**BUILDER.LOCK["linuxResources"][self.target[3]], "sha256": BUILDER.sha256(bubblewrap), "bytes": bubblewrap.stat().st_size}
        BUILDER.LOCK = {**BUILDER.LOCK, "linuxResources": {self.target[3]: pin}}
        manifest = json.loads((self.destination / "mewdex-engine.json").read_text(encoding="utf-8"))
        manifest["target"] = self.target[3]
        manifest["files"].append({"path": "bin/bwrap", "sha256": pin["sha256"], "bytes": pin["bytes"]})
        (self.destination / "mewdex-engine.json").write_text(json.dumps(manifest), encoding="utf-8")
        (self.destination / "source.json").write_text(json.dumps(BUILDER.LOCK), encoding="utf-8")
        for name in ["COPYING", "source.tar.gz", "source.json"]:
            resource = self.destination / "licenses/bubblewrap" / name
            resource.parent.mkdir(parents=True, exist_ok=True)
            resource.write_bytes(b"fixture source and license")
        self.refresh_build()

    def refresh_build(self):
        files = {file.relative_to(self.destination).as_posix(): BUILDER.sha256(file) for file in self.destination.rglob("*") if file.is_file() and file.name != "build.json"}
        (self.destination / "build.json").write_text(json.dumps({"inputSha256": BUILDER.input_digest(), "files": files}), encoding="utf-8")

    def test_linux_bundle_retains_its_pinned_launcher_and_sources(self):
        self.linux_bundle()
        self.verify()
        resource = self.destination / "bin/bwrap"
        resource.write_bytes(b"changed bubblewrap")
        with self.assertRaisesRegex(ValueError, "integrity check"):
            self.verify()

    def test_linux_bundle_cannot_override_its_launcher_pin(self):
        self.linux_bundle()
        BUILDER.LOCK["linuxResources"][self.target[3]]["sha256"] = "0" * 64
        (self.destination / "source.json").write_text(json.dumps(BUILDER.LOCK), encoding="utf-8")
        self.refresh_build()
        with self.assertRaisesRegex(ValueError, "source pin"):
            self.verify()

    def test_linux_bundle_requires_the_source_and_license_record(self):
        self.linux_bundle()
        metadata = json.loads((self.destination / "build.json").read_text(encoding="utf-8"))
        del metadata["files"]["licenses/bubblewrap/source.tar.gz"]
        (self.destination / "build.json").write_text(json.dumps(metadata), encoding="utf-8")
        with self.assertRaisesRegex(ValueError, "missing a required resource"):
            self.verify()


if __name__ == "__main__":
    unittest.main()
