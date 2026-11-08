import importlib.util
from pathlib import Path
import tempfile
import unittest
import zipfile


ROOT = Path(__file__).resolve().parents[2]
SPEC = importlib.util.spec_from_file_location("component_archive", ROOT / "scripts/archive-component.py")
BUILDER = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(BUILDER)


class ComponentArchiveTests(unittest.TestCase):
    def test_unix_entrypoints_and_nested_runtimes_retain_execution_bits(self):
        for platform in ["macos", "linux"]:
            with self.subTest(platform=platform), tempfile.TemporaryDirectory(dir=ROOT / ".state") as temporary:
                root = Path(temporary)
                source = root / "component"
                files = ["mewdex-executor", "sandbox/bin/mewdex-sandbox", "engine/bin/codex", "engine/codex-resources/voice/bin/codex-voice-host", "README.md", "sandbox/LICENSE"]
                for relative in files:
                    file = source / relative
                    file.parent.mkdir(parents=True, exist_ok=True)
                    file.write_text("fixture", encoding="utf-8")
                output = root / "component.zip"
                BUILDER.archive_component(source, output, platform, "mewdex-executor")
                with zipfile.ZipFile(output) as archive:
                    self.assertIsNone(archive.testzip())
                    for relative in files:
                        info = archive.getinfo("component/" + relative)
                        expected = 0o755 if relative in files[:4] else 0o644
                        self.assertEqual((info.external_attr >> 16) & 0o777, expected)
                        self.assertEqual(info.create_system, 3)
                        self.assertEqual(archive.read(info), b"fixture")
                with self.assertRaises(FileExistsError):
                    BUILDER.archive_component(source, output, platform, "mewdex-executor")


if __name__ == "__main__":
    unittest.main()
