"""Archive a staged service component with target-platform file permissions."""

import argparse
from pathlib import Path
import shutil
import stat
import zipfile


def archive_component(source, output, platform, entrypoint):
    source = source.resolve()
    if Path(entrypoint).name != entrypoint or entrypoint in ["", ".", ".."]:
        raise ValueError("The component entrypoint must be a plain filename")
    with zipfile.ZipFile(output, "x", zipfile.ZIP_DEFLATED, compresslevel=6) as archive:
        for file in sorted(source.rglob("*")):
            if file.is_symlink():
                raise ValueError("Component archives require regular files and directories")
            if not file.is_file():
                continue
            relative = file.relative_to(source)
            info = zipfile.ZipInfo.from_file(file, file.relative_to(source.parent))
            info.compress_type = zipfile.ZIP_DEFLATED
            if platform != "windows":
                runtime_program = relative.parts[0] in ["engine", "sandbox"] and "bin" in relative.parts[1:-1]
                app_launcher = "Contents" in relative.parts and "MacOS" in relative.parts and file.name == "launch"
                executable = relative.as_posix() == entrypoint or runtime_program or app_launcher
                mode = 0o755 if executable else 0o644
                info.create_system = 3
                info.external_attr = (stat.S_IFREG | mode) << 16
            with file.open("rb") as reader, archive.open(info, "w") as writer:
                shutil.copyfileobj(reader, writer, 1 << 20)
    with zipfile.ZipFile(output) as archive:
        if archive.testzip() is not None:
            raise RuntimeError("Package archive failed CRC validation")


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("source", type=Path)
    parser.add_argument("output", type=Path)
    parser.add_argument("platform", choices=["windows", "macos", "linux"])
    parser.add_argument("entrypoint")
    args = parser.parse_args()
    archive_component(args.source, args.output, args.platform, args.entrypoint)
