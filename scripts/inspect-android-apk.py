"""Inspect a development APK without installing it or starting a device."""
import argparse
import hashlib
import json
import pathlib
import re
import struct
import subprocess
import zipfile

parser = argparse.ArgumentParser()
parser.add_argument('apk', type=pathlib.Path)
parser.add_argument('--sdk', type=pathlib.Path, required=True)
parser.add_argument('--java', type=pathlib.Path, required=True)
parser.add_argument('--output', type=pathlib.Path, required=True)
args = parser.parse_args()
tools = args.sdk / 'build-tools/37.0.0'
native = []
with args.apk.open('rb') as raw, zipfile.ZipFile(args.apk) as archive:
    entries = [info for info in archive.infolist() if info.filename.startswith('lib/') and info.filename.endswith('.so')]
    if not entries or len(entries) > 16:
        raise SystemExit('Unexpected native library count.')
    for info in entries:
        with archive.open(info) as library:
            header = library.read(64)
            if header[:6] != b'\x7fELF\x02\x01':
                raise SystemExit('Expected a little-endian ELF64 library.')
            phoff = struct.unpack_from('<Q', header, 32)[0]
            entsize, count = struct.unpack_from('<HH', header, 54)
            if entsize < 56 or phoff + entsize * count > 1024 * 1024:
                raise SystemExit('Unexpected ELF program headers.')
            library.seek(phoff)
            program_headers = library.read(entsize * count)
        alignments = []
        for index in range(count):
            kind, flags, offset, vaddr, paddr, size, memory, alignment = struct.unpack_from('<IIQQQQQQ', program_headers, index * entsize)
            if kind == 1:
                if alignment < 16384 or alignment & (alignment - 1) or (vaddr - offset) % alignment:
                    raise SystemExit('A native LOAD segment is not compatible with 16 KiB pages.')
                alignments.append(alignment)
        if not alignments:
            raise SystemExit('Native library contains no LOAD segments.')
        raw.seek(info.header_offset)
        local = raw.read(30)
        name_length, extra_length = struct.unpack_from('<HH', local, 26)
        data_offset = info.header_offset + 30 + name_length + extra_length
        if info.compress_type == zipfile.ZIP_STORED and data_offset % 16384:
            raise SystemExit('Stored native library is not aligned within the APK.')
        native.append({'path': info.filename, 'bytes': info.file_size, 'loadAlignments': alignments, 'compressed': info.compress_type != zipfile.ZIP_STORED, 'dataOffset': data_offset})
if {item['path'].split('/')[1] for item in native} != {'arm64-v8a'}:
    raise SystemExit('Development target unexpectedly contains another ABI.')

def run(command):
    result = subprocess.run([str(part) for part in command], capture_output=True, timeout=45)
    if result.returncode:
        raise SystemExit('Artifact inspection command failed: ' + pathlib.Path(command[0]).name)
    return (result.stdout + result.stderr).decode('utf-8', errors='replace')

alignment = run([tools / 'zipalign.exe', '-c', '-P', '16', '-v', '4', args.apk])
signature = run([args.java / 'bin/java.exe', '-jar', tools / 'lib/apksigner.jar', 'verify', '--verbose', '--print-certs', args.apk])
badging = run([tools / 'aapt2.exe', 'dump', 'badging', args.apk])
if "package: name='app.mewdex.client'" not in badging or 'application-debuggable' not in badging:
    raise SystemExit('Expected the MewDex development package.')
digest = hashlib.sha256()
with args.apk.open('rb') as source:
    for block in iter(lambda: source.read(1024 * 1024), b''):
        digest.update(block)
certificate = re.search(r'certificate SHA-256 digest: ([a-fA-F0-9]+)', signature)
report = {
    'apk': str(args.apk.resolve()), 'sha256': digest.hexdigest(), 'bytes': args.apk.stat().st_size,
    'platform': 'android', 'architecture': 'arm64', 'configuration': 'Debug',
    'finalCandidate': False, 'uiLaunched': False, 'installedOnDevice': False,
    'nativeLibraries': native, 'elf16KiBCompatible': True, 'apk16KiBAligned': True,
    'signatureVerified': True, 'signerSha256': certificate.group(1) if certificate else None,
    'packageMetadata': [line for line in badging.splitlines() if line.startswith(('package:', 'sdkVersion:', 'minSdkVersion:', 'targetSdkVersion:', 'native-code:'))],
}
args.output.parent.mkdir(parents=True, exist_ok=True)
args.output.write_text(json.dumps(report, indent=2) + '\n', encoding='utf-8')
args.output.with_suffix('.alignment.log').write_text(alignment, encoding='utf-8')
print(json.dumps(report))
